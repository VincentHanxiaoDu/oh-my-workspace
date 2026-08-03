package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// RunDir is the directory inside a store that holds the daemon's runtime files.
//
// It sits beside the store's `records/` rather than inside it because the store's Kinds() lists
// every directory under `records/` as a record kind, and the daemon's lock is not a record. It is
// also why this package writes these files itself instead of through store.Put: a lock has to be
// openable by a process that has NOT been able to open the store, and the run record has to be
// writable at the moment the store has stopped accepting writes — which is precisely when
// criterion 16 requires the ending to be recorded.
const RunDir = "run"

const (
	lockName   = "daemon.lock"
	stateName  = "last-run.json"
	socketName = "control.sock"
)

// ownerOnlyDir and ownerOnlyFile are the permissions everything this package creates is created
// with. They are not a nicety: the control API's confirmation (§4.6) asserts exactly these, so a
// widened constant here turns into a daemon that declines to open its control API — loudly, which
// is the intended direction of that failure.
const (
	ownerOnlyDir  fs.FileMode = 0o700
	ownerOnlyFile fs.FileMode = 0o600
)

// Ending is how a run of the daemon ended. Five values, five distinct renderings, none of them
// silence (PRD §4.3; Issue #2 criteria 10–12).
type Ending int

const (
	// EndingUndetermined is the ZERO VALUE, for the same reason tri.Undetermined is: an Ending
	// nobody managed to establish must not read as a confident statement about the last run. A
	// record that exists and cannot be parsed lands here, and criterion 12 requires that it is
	// tellable apart from a clean stop, from a crash and from "never run".
	EndingUndetermined Ending = iota
	// EndingNeverRun means this store's daemon has genuinely never run. A determined answer, and
	// not the same fact as an unreadable record (criterion 11).
	EndingNeverRun
	// EndingStopped means the previous run ended because somebody stopped it on purpose.
	EndingStopped
	// EndingCannotWrite means the previous run ended because it could not write to the store
	// (§4.3, criteria 15–16).
	EndingCannotWrite
	// EndingCrashed means the previous run ended WITHOUT recording an ending. Inferred, never
	// stored: see the package comment.
	EndingCrashed
)

// String renders the ending. Every value returns a distinct, non-empty sentence.
//
// THIS IS THE ONLY PLACE THESE ARE SPELLED. Criterion 10 needs three of them to be distinct
// renderings and criteria 11 and 12 add two more; a second spelling anywhere is how two of them
// quietly converge. An Ending outside the five is rendered as undetermined rather than blank, on
// the same reasoning as tri.Value.String.
func (e Ending) String() string {
	switch e {
	case EndingNeverRun:
		return "never run — this store's daemon has not run before"
	case EndingStopped:
		return "ended by an explicit stop"
	case EndingCannotWrite:
		return "ended because it could not write to the store"
	case EndingCrashed:
		return "ended without recording an ending (crash or power loss)"
	default:
		// The one wording for the third answer, taken from tri rather than respelled here.
		return "how it ended " + tri.Undetermined.String()
	}
}

// Determined reports whether the ending is a real answer. "Never run" IS one: the product knows
// exactly what happened, which is nothing.
func (e Ending) Determined() bool {
	return e == EndingNeverRun || e == EndingStopped || e == EndingCannotWrite || e == EndingCrashed
}

// endingCode is how an ending is spelled on disk. Deliberately not Ending's numeric value: a
// reordering of the constants must not silently reinterpret every record already written. It is
// also not the rendered sentence: rewording a sentence for a person must not change what a record
// written yesterday means.
type endingCode string

const (
	codeStopped      endingCode = "stopped"
	codeCannotWrite  endingCode = "cannot-write"
	codeNeverRun     endingCode = "never-run"
	codeCrashed      endingCode = "crashed"
	codeUndetermined endingCode = "undetermined"
)

// ending decodes a stored code. A code this build does not know is UNDETERMINED — a record written
// by a newer build says something, and this one cannot tell what, which is precisely the third
// answer rather than a reason to guess at one of the others.
func (c endingCode) ending() Ending {
	switch c {
	case codeStopped:
		return EndingStopped
	case codeCannotWrite:
		return EndingCannotWrite
	case codeNeverRun:
		return EndingNeverRun
	case codeCrashed:
		return EndingCrashed
	default:
		return EndingUndetermined
	}
}

func codeFor(e Ending) endingCode {
	switch e {
	case EndingStopped:
		return codeStopped
	case EndingCannotWrite:
		return codeCannotWrite
	case EndingNeverRun:
		return codeNeverRun
	case EndingCrashed:
		return codeCrashed
	default:
		return codeUndetermined
	}
}

// runRecord is the on-disk shape of "what the daemon's current or last run is doing".
//
// Phase is written as "running" at start and rewritten to "ended" with a Code on the way out. A
// record left saying "running" by a process that no longer holds the lock is what a crash looks
// like from the outside — see readRunRecord's callers.
type runRecord struct {
	Format    int        `json:"format"`
	PID       int        `json:"pid"`
	StartedAt time.Time  `json:"started_at"`
	Phase     string     `json:"phase"`
	Code      endingCode `json:"ending,omitempty"`
	EndedAt   time.Time  `json:"ended_at,omitempty"`
	Detail    string     `json:"detail,omitempty"`

	// Prev is how the run BEFORE this one ended, carried forward by whoever wrote this record.
	//
	// IT IS CARRIED RATHER THAN LOOKED UP because criterion 13 asks that the last-run fact survive
	// the process that produced it, and criterion 14 asks that the control API and the CLI agree.
	// Without it, a reader who finds a run in progress has nothing to say about the run before —
	// and the running daemon, which read it at startup, would say something different.
	Prev       endingCode `json:"previous_ending,omitempty"`
	PrevDetail string     `json:"previous_detail,omitempty"`

	// Control is what the running daemon's control API did: "open", "not open", or the
	// undetermined wording. Recorded so a reader who cannot reach the control API is told WHY
	// rather than being left to infer it from the socket's absence (criteria 23, 26).
	Control       string `json:"control,omitempty"`
	ControlDetail string `json:"control_detail,omitempty"`
}

const (
	runFormat  = 1
	phaseRun   = "running"
	phaseEnded = "ended"
)

// errNoRunRecord means no run has ever been recorded for this store. Distinct from an unreadable
// one, because criterion 11 and criterion 12 are two different renderings.
var errNoRunRecord = errors.New("no run has been recorded for this store")

// errRunRecordUnreadable means a run record is present and could not be understood.
var errRunRecordUnreadable = errors.New("the run record is present and cannot be read")

// ErrControlNotOwnerOnly means the control API declined to open because it could not confirm its
// socket is owner-only (§4.6, criterion 23). It is its own value so that a caller can tell it apart
// from "the daemon is not running" without matching on prose.
var ErrControlNotOwnerOnly = errors.New("the control API did not open: owner-only permissions could not be confirmed")

// ErrLockHeld means another daemon holds this store (criterion 5).
var ErrLockHeld = errors.New("another daemon already holds this store")

// ErrLockUndetermined means whether the store's lock is held could not be determined — a platform
// with no advisory locking this package can use, or a lock file that will not open. Undetermined,
// never reported as "free" and never as "held" (§4.3).
var ErrLockUndetermined = errors.New("could not determine whether another daemon holds this store")

// runPaths are the four paths this package uses inside one store.
type runPaths struct {
	root  string
	dir   string
	lock  string
	state string
	// socketDir is usually dir, and is not always dir: see socketFor. It is a separate field
	// because it is the directory whose owner-only permissions the control API confirms, and
	// confirming the wrong directory would be a confirmation that proves nothing.
	socketDir string
	socket    string
}

func pathsFor(storeRoot string) runPaths {
	dir := filepath.Join(storeRoot, RunDir)
	sockDir, sock := socketFor(storeRoot)
	return runPaths{
		root:      storeRoot,
		dir:       dir,
		lock:      filepath.Join(dir, lockName),
		state:     filepath.Join(dir, stateName),
		socketDir: sockDir,
		socket:    sock,
	}
}

// ensureRunDir creates the run directory owner-only, and TIGHTENS an existing one.
//
// The chmod is not redundant with the Mkdir: a directory left behind by an earlier version, or by a
// person's tar extraction, keeps whatever permissions it had, and the control API's confirmation
// would then refuse to open for a reason nobody could act on. Tighten first, refuse only if the
// tightening did not take.
func ensureRunDir(p runPaths) error { return ensureOwnerOnlyDir(p.dir) }

func ensureOwnerOnlyDir(dir string) error {
	if err := os.MkdirAll(dir, ownerOnlyDir); err != nil {
		return fmt.Errorf("could not prepare the daemon's directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, ownerOnlyDir); err != nil {
		return fmt.Errorf("could not make %s owner-only: %w", dir, err)
	}
	return nil
}

// readRunRecord reads the store's run record.
//
// Three outcomes and they are three: a record; errNoRunRecord (nothing has ever run); and
// errRunRecordUnreadable (something is there and cannot be understood). The third must never be
// answered with the first two — that is criterion 12 in one function.
func readRunRecord(p runPaths) (runRecord, error) {
	body, err := os.ReadFile(p.state)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return runRecord{}, errNoRunRecord
		}
		return runRecord{}, fmt.Errorf("%w: %v", errRunRecordUnreadable, err)
	}
	var rec runRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return runRecord{}, fmt.Errorf("%w: %v", errRunRecordUnreadable, err)
	}
	// A record whose format or phase is not one this build wrote is INCOMPLETE, not absent, and
	// criterion 12 names incompleteness explicitly as an undetermined ending.
	if rec.Format != runFormat || (rec.Phase != phaseRun && rec.Phase != phaseEnded) {
		return runRecord{}, fmt.Errorf("%w: it is not a record this build understands", errRunRecordUnreadable)
	}
	return rec, nil
}

// writeRunRecord replaces the run record atomically.
//
// Temporary in the same directory, fsynced, renamed, directory fsynced — the store's invariant 4,
// restated here because these files are deliberately not store records (see RunDir). A run record
// half-written by a process being killed would read back as an unreadable record, which renders as
// undetermined; correct, but it would hide the crash it was in the middle of recording.
func writeRunRecord(p runPaths, rec runRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return writeFileAtomic(p.state, body)
}

func writeFileAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // A no-op after a successful rename; the recovery when anything below fails.
	if err := tmp.Chmod(ownerOnlyFile); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// A directory that will not fsync is not a reason to fail a write that has already landed on
	// some filesystems that do not support it; the rename itself is what carries the guarantee.
	_ = d.Sync()
	return nil
}

// Report is the daemon's state, as one value.
//
// IT IS ONE TYPE ON PURPOSE (criterion 14). The control API serves this and the CLI renders this,
// and both go through Report.WriteTo — so "the state reported over the control API and the state
// reported by the CLI" cannot drift, because there is only one of it.
type Report struct {
	// StorePath is the store this report is about. A report with no store named cannot be checked
	// against anything.
	StorePath string `json:"store_path"`
	// Running is three-valued. No means it was established that nothing holds this store's lock;
	// Undetermined means that could not be established, which is NOT the same as "not running".
	Running tri.Value `json:"-"`
	// RunningText carries Running over the wire. tri.Value is an int whose zero value is
	// meaningful, so it is transmitted as its own rendering rather than as a number that a future
	// reordering would reinterpret.
	RunningText string `json:"running"`
	// Healthy is whether the daemon is running AND still able to write, in three values.
	//
	// IT IS A SEPARATE FIELD FROM Running, AND THAT IS CRITERION 17. "Something holds the lock" and
	// "that something is fine" are two facts, and only the daemon itself knows the second. A reader
	// with no control API therefore gets Running=yes and Healthy=UNDETERMINED — never a claim that
	// the daemon is fine, which is the claim that must not be made while writes are failing. The
	// daemon's own answer flips to No inside the same critical section that observes a failed
	// write, so there is no moment at which it says yes after knowing otherwise.
	Healthy tri.Value `json:"-"`
	// HealthyText carries Healthy over the wire.
	HealthyText string `json:"healthy"`
	// HealthDetail says what is wrong, or why health could not be determined. Never empty when
	// Healthy is not Yes.
	HealthDetail string `json:"health_detail,omitempty"`
	// PID is the running daemon's process id, or zero.
	PID int `json:"pid,omitempty"`
	// StartedAt is when the running daemon started, if it is running.
	StartedAt time.Time `json:"started_at,omitempty"`
	// LastRun is how the PREVIOUS run ended when nothing is running, and how the run before this
	// one ended when something is.
	LastRun Ending `json:"-"`
	// LastRunText carries LastRun over the wire, for the reason RunningText does.
	LastRunText string `json:"last_run"`
	// LastRunDetail is what specifically was found — the write error, the parse failure. May be
	// empty; it is colour, never the answer.
	LastRunDetail string `json:"last_run_detail,omitempty"`
	// Control is whether the control API is open, in three values. Criterion 26: a control state
	// that could not be determined is reported as undetermined, never as "closed".
	Control tri.Value `json:"-"`
	// ControlText carries Control over the wire.
	ControlText string `json:"control"`
	// ControlDetail names why the control API is not open when it is not.
	ControlDetail string `json:"control_detail,omitempty"`
	// ControlSocket is the path the control API listens on when it is open.
	ControlSocket string `json:"control_socket,omitempty"`
	// Auth is this machine's sign-in state, as one line (Issue #19 criterion 23, PRD §4.3).
	//
	// IT IS A STRING RATHER THAN A tri BECAUSE IT IS FOUR FACTS, NOT THREE: signed in, not signed
	// in, no hub configured, and could not be determined. `internal/auth` owns the wording and both
	// this report and the CLI take it from the same function — see authstate.go.
	Auth string `json:"auth"`
	// AuthCode is the stable machine-readable code behind Auth, so a script reading the control
	// API tells "no hub configured" from "not signed in" without matching prose.
	AuthCode string `json:"auth_code,omitempty"`
	// AuthDetail says more. May be empty.
	AuthDetail string `json:"auth_detail,omitempty"`
}

// wire fills the text fields from the tri values. Called on every path that produces a Report, so
// that a Report round-tripped through JSON equals the one that was sent.
func (r *Report) wire() {
	r.RunningText = r.Running.Render("running", "not running")
	r.LastRunText = r.LastRun.String()
	r.ControlText = r.Control.Render("open", "not open")
	r.HealthyText = r.Healthy.Render("healthy", "not healthy")
}

// unwire restores the tri values from the text fields after a decode, so that a Report read off
// the control API behaves like one produced locally. An unrecognised word becomes Undetermined,
// which is the correct answer about a word this build does not know.
func (r *Report) unwire() {
	r.Running = triFromText(r.RunningText, "running", "not running")
	r.Control = triFromText(r.ControlText, "open", "not open")
	r.Healthy = triFromText(r.HealthyText, "healthy", "not healthy")
	r.LastRun = endingFromText(r.LastRunText)
}

func triFromText(text, yes, no string) tri.Value {
	switch text {
	case yes:
		return tri.Yes
	case no:
		return tri.No
	default:
		return tri.Undetermined
	}
}

func endingFromText(text string) Ending {
	for _, e := range []Ending{EndingNeverRun, EndingStopped, EndingCannotWrite, EndingCrashed} {
		if e.String() == text {
			return e
		}
	}
	return EndingUndetermined
}

// WriteTo renders the report the way a person reads it.
//
// THE CLI DOES NOT HAVE ITS OWN RENDERER. Criterion 14 asks that the control API and the CLI report
// the same state for the same daemon at the same moment, and the cheapest way to be wrong about
// that is two format strings that were the same the day they were written.
func (r Report) WriteTo(w io.Writer) (int64, error) {
	n, err := fmt.Fprintf(w,
		"store:    %s\n"+
			"daemon:   %s\n",
		r.StorePath, r.Running.Render("running", "not running"))
	total := int64(n)
	if err != nil {
		return total, err
	}
	if r.Running == tri.Yes && r.PID != 0 {
		n, err = fmt.Fprintf(w, "pid:      %d\n", r.PID)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
	if r.Running != tri.No {
		// HEALTH IS PRINTED SEPARATELY FROM "RUNNING", and only when something may be running.
		// Criterion 17: the line a person would read as "running, fine" is this one, and it says
		// "healthy" only when the daemon itself has said so.
		n, err = fmt.Fprintf(w, "health:   %s\n", r.Healthy.Render("healthy — it can write to the store", "NOT healthy"))
		total += int64(n)
		if err != nil {
			return total, err
		}
		if r.HealthDetail != "" {
			n, err = fmt.Fprintf(w, "          %s\n", r.HealthDetail)
			total += int64(n)
			if err != nil {
				return total, err
			}
		}
	}
	n, err = fmt.Fprintf(w, "last run: %s\n", r.LastRun)
	total += int64(n)
	if err != nil {
		return total, err
	}
	if r.LastRunDetail != "" {
		n, err = fmt.Fprintf(w, "          %s\n", r.LastRunDetail)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
	// THE CONTROL API'S THREE STATES, EACH SAID. Criterion 23 wants "could not confirm owner-only"
	// distinguishable from "not running" and from "running normally", and criterion 26 wants an
	// undetermined control state reported as undetermined rather than as closed.
	n, err = fmt.Fprintf(w, "control:  %s\n", r.Control.Render("open, and its socket was confirmed owner-only", "not open"))
	total += int64(n)
	if err != nil {
		return total, err
	}
	if r.ControlDetail != "" {
		n, err = fmt.Fprintf(w, "          %s\n", r.ControlDetail)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
	if r.Control == tri.Yes && r.ControlSocket != "" {
		n, err = fmt.Fprintf(w, "          %s\n", r.ControlSocket)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// Inspect reports a store's daemon state WITHOUT starting anything (criteria 13, 18).
//
// It reads the lock and the run record and nothing else, so it works for a store whose daemon has
// never run, one whose daemon is running, and one whose daemon died — and it never so much as
// looks at whether starting one would be a good idea.
func Inspect(storeRoot string) Report {
	p := pathsFor(storeRoot)

	// WHETHER SOMETHING IS RUNNING IS DECIDED BY THE LOCK, not by the run record and not by a pid.
	// A record saying "running" proves only that something once started.
	held, holder, lockErr := probeLock(p)
	running := tri.Undetermined
	switch {
	case lockErr != nil:
	case held:
		running = tri.Yes
	default:
		running = tri.No
	}

	// ASK THE DAEMON ITSELF FIRST. Criterion 14 wants one state, and the only way two readers
	// cannot disagree is for one of them to be the other. When the control API is open, its answer
	// IS this function's answer — including the health it knows and the disk does not.
	if running == tri.Yes {
		if rep, err := queryControl(p.socket); err == nil && rep.StorePath != "" {
			return rep
		}
	}

	rep := Report{StorePath: storeRoot, Running: running, PID: holder}
	if lockErr != nil {
		rep.HealthDetail = lockErr.Error()
	}

	rec, err := readRunRecord(p)
	switch {
	case errors.Is(err, errNoRunRecord):
		rep.LastRun = EndingNeverRun
	case err != nil:
		// PRESENT AND UNREADABLE (criterion 12). Undetermined, and the reason is said out loud so
		// it is never silence.
		rep.LastRun = EndingUndetermined
		rep.LastRunDetail = err.Error()
	default:
		rep.LastRun, rep.LastRunDetail = endingOf(rec, running)
		if running == tri.Yes {
			rep.StartedAt = rec.StartedAt
			if rep.PID == 0 {
				rep.PID = rec.PID
			}
		}
	}

	rep.Healthy, rep.HealthDetail = healthFromDisk(running, rep.HealthDetail)
	rep.Control, rep.ControlDetail = controlFromDisk(p, running, rec, err)
	rep.Auth, rep.AuthDetail, rep.AuthCode = authStateFor(storeRoot)
	rep.wire()
	return rep
}

// healthFromDisk is the health answer available to a reader who is NOT the daemon.
//
// It is never Yes. A reader outside the process can establish that something holds the lock; it
// cannot establish that that something can still write, because the only evidence for that is a
// write the reader did not do. Criterion 17 forbids a "running, fine" that is not backed by the
// daemon's own answer, so this returns UNDETERMINED and says why — which is a real answer, and not
// the one a person would act on as healthy.
func healthFromDisk(running tri.Value, detail string) (tri.Value, string) {
	switch running {
	case tri.No:
		return tri.No, "the daemon is not running"
	case tri.Yes:
		return tri.Undetermined, "something holds this store's lock; whether it can still write " +
			"could not be determined without its control API"
	default:
		if detail == "" {
			detail = "whether a daemon holds this store could not be determined"
		}
		return tri.Undetermined, detail
	}
}

// controlFromDisk answers the control API's state for a reader that could not reach it.
//
// The three answers stay three (criterion 26). Not running means the control API is determinedly
// not open. Running with a record that says the daemon declined means a determined "not open",
// with the daemon's own reason. Running with a socket that did not answer, or a record that cannot
// be read, is UNDETERMINED — never "closed", because nothing established that.
func controlFromDisk(p runPaths, running tri.Value, rec runRecord, recErr error) (tri.Value, string) {
	switch running {
	case tri.No:
		return tri.No, "the daemon is not running, so nothing is listening on its control API"
	case tri.Undetermined:
		return tri.Undetermined, "whether a daemon is running could not be determined, so neither could its control API"
	}
	if recErr != nil {
		return tri.Undetermined, "a daemon holds this store and its run record could not be read, " +
			"so the state of its control API " + tri.Undetermined.String()
	}
	switch rec.Control {
	case "not open":
		return tri.No, rec.ControlDetail
	case "open":
		// It said it opened, and it did not answer us. That is not a closed control API and it is
		// not an open one.
		return tri.Undetermined, errControlSilent.Error() + " (" + p.socket + ")"
	default:
		if rec.ControlDetail != "" {
			return tri.Undetermined, rec.ControlDetail
		}
		return tri.Undetermined, "the daemon did not record whether its control API opened"
	}
}

// endingOf turns a run record into an ending, given what the lock says about liveness.
//
// THE CRASH INFERENCE LIVES HERE, and it is the one place the two facts meet: a record that still
// says "running" while nothing holds the lock is a run that ended without recording an ending. If
// liveness itself could not be determined, the ending cannot be either — inferring "crashed" from
// an unknown would be manufacturing a determined answer out of an undetermined one.
func endingOf(rec runRecord, running tri.Value) (Ending, string) {
	if rec.Phase == phaseRun {
		switch running {
		case tri.Yes:
			// The record describes the run happening NOW, so "the last run" is the one before it —
			// which this record carries because the running daemon read it before overwriting it.
			return rec.Prev.ending(), rec.PrevDetail
		case tri.No:
			// A RUN RECORD THAT STILL SAYS "RUNNING" WHILE NOTHING HOLDS THE LOCK IS A CRASH. It
			// is the only way that pair of facts can arise: a daemon that stopped for any reason
			// it knew about would have rewritten this record on the way out.
			return EndingCrashed, rec.Detail
		default:
			return EndingUndetermined, "whether a daemon is running could not be determined, so how the last run ended cannot be either"
		}
	}
	switch rec.Code {
	case codeStopped:
		return EndingStopped, rec.Detail
	case codeCannotWrite:
		return EndingCannotWrite, rec.Detail
	default:
		// Ended, with no code this build knows. Incomplete, so undetermined (criterion 12).
		return EndingUndetermined, "the run record says the daemon ended but does not say how"
	}
}
