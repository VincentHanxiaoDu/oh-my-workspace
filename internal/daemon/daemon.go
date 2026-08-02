package daemon

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// DefaultInterval is how often the daemon proves it can still write.
//
// It is short because the cost of being wrong is asymmetric: a person whose disk filled wants to
// hear about it, and a probe that is cheap (one small record, replaced in place) has no reason to
// be rare.
const DefaultInterval = 2 * time.Second

// heartbeatKind and heartbeatID are the record the write probe writes.
//
// THE PROBE IS A REAL WRITE THROUGH THE STORE, not a stat and not a check of free space. §4.3 says
// the daemon stops when it cannot write; the only thing that establishes that it can is having
// done it, through the same code path everything else writes through. A full disk, a read-only
// remount, a revoked permission and a damaged store all show up here for free, and none of them
// would show up in a capability check.
const (
	heartbeatKind = store.Kind("daemon")
	heartbeatID   = "heartbeat"
)

// Options configures a daemon.
type Options struct {
	// StorePath is the store this daemon takes the lock on. Required: there is no "the store" here
	// because the daemon may be one of several on a machine (criterion 7).
	StorePath string

	// Write is the write-capability probe. Nil means the real one — a record written through the
	// store.
	//
	// IT IS INJECTABLE BECAUSE CRITERIA 15–17 ARE OTHERWISE UNDRIVABLE. Making a real store stop
	// accepting writes means filling a disk or revoking a permission mid-test, which is a test
	// about the machine it runs on rather than about the daemon. This seam is the same call the
	// real probe makes, so a test that fails it exercises the same path.
	Write func() error

	// Interval is how often Write is called. Zero means DefaultInterval.
	Interval time.Duration

	// ConfirmOwnerOnly overrides the owner-only confirmation. Nil means the real one.
	//
	// It exists so that criterion 23 — the refusal — can be driven WITHOUT naming a platform on
	// which it happens to occur. A test that says "skip unless Windows" tests nothing anywhere the
	// suite actually runs.
	ConfirmOwnerOnly func(path string) (tri.Value, string)
}

// phase is the daemon's own view of itself. Its zero value is deliberately not "healthy".
type phase int

const (
	phaseStarting phase = iota
	phaseHealthy
	phaseCannotWrite
	phaseStopping
	phaseStoppedPhase
)

// Daemon is a running daemon: it holds one store's lock, proves it can write, and answers for its
// own state.
type Daemon struct {
	opts    Options
	paths   runPaths
	lock    *lockHandle
	control *Control

	// StaleLockFound is set when this daemon took over a lock left by a process that is gone
	// (criterion 8). Reported so a person hears what happened; never a refusal.
	StaleLockFound  bool
	StaleLockDetail string

	startedAt time.Time
	prev      Ending
	prevText  string

	controlState  tri.Value
	controlDetail string

	// mu guards everything below AND is the mutex every state read takes. That is the whole of
	// criterion 17: the write probe's failure and the phase change happen inside one hold of this
	// lock, so no reader can be scheduled between them.
	mu     sync.Mutex
	ph     phase
	reason string

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
	endOnce  sync.Once
	// serving records whether Serve was ever entered, so that Close on a daemon that was started
	// and never served releases the store instead of waiting forever for a loop nobody ran.
	serving atomic.Bool
}

// Start takes the store's lock, records that a run has begun, and opens the control API.
//
// IT NEVER CREATES A STORE (§4.2, criterion 3). A missing store comes back as store.ErrNotFound,
// unwrapped and unchanged, so `omw daemon start` and `omw store status` report the same fact in
// the same words — which is what Issue #2's "Related" note about Issue #3 asks for.
//
// A control API that declines to open is NOT an error from Start. The daemon runs, watches, and
// records how its run ended regardless; §4.6's refusal removes an interface, not the product. That
// is Issue #1's carried-forward criterion 14 made drivable.
func Start(opts Options) (*Daemon, error) {
	if opts.StorePath == "" {
		return nil, errors.New("a daemon needs the path of the store it runs against")
	}
	// THE STORE MUST ALREADY EXIST, and this is where that is established. store.Open never
	// creates one, so this is a read of the truth rather than a policy applied on top of it.
	if _, err := store.Open(opts.StorePath); err != nil {
		return nil, err
	}
	p := pathsFor(opts.StorePath)

	// HOW THE PREVIOUS RUN ENDED IS READ BEFORE THE RECORD IS OVERWRITTEN. It is then carried
	// inside the new record, so criterion 13's "the last-run fact survives the process that
	// produced it" holds across any number of runs, and both the control API and a purely on-disk
	// reader answer with the same value (criterion 14).
	prevEnding, prevDetail := previousEnding(p)

	lock, err := acquireLock(p)
	if err != nil {
		return nil, err
	}

	d := &Daemon{
		opts:            opts,
		paths:           p,
		lock:            lock,
		StaleLockFound:  lock.StaleFound,
		StaleLockDetail: lock.StaleDetail,
		startedAt:       time.Now().UTC(),
		prev:            prevEnding,
		prevText:        prevDetail,
		ph:              phaseStarting,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	if err := writeRunRecord(p, runRecord{
		Format:     runFormat,
		PID:        os.Getpid(),
		StartedAt:  d.startedAt,
		Phase:      phaseRun,
		Prev:       codeFor(prevEnding),
		PrevDetail: prevDetail,
	}); err != nil {
		// A DAEMON THAT CANNOT RECORD THAT IT STARTED WILL NOT START. It would be a run whose
		// ending nothing can be said about, which is the state §4.3 exists to prevent.
		lock.release()
		return nil, fmt.Errorf("the daemon could not record that it is running: %w", err)
	}

	control, state, detail, cerr := openControl(p, d.Report, opts.ConfirmOwnerOnly)
	d.control, d.controlState, d.controlDetail = control, state, detail
	if cerr != nil && !errors.Is(cerr, ErrControlNotOwnerOnly) {
		d.controlState, d.controlDetail = tri.Undetermined, cerr.Error()
	}
	// The control state is recorded on disk too, so a reader that CANNOT reach the control API can
	// still be told why — "not open, owner-only could not be confirmed" rather than silence.
	d.recordControlState()

	// STARTING INCLUDES PROVING IT CAN WRITE, and that is not tidiness.
	//
	// Without it there is a window between `omw daemon start` returning and the loop's first probe
	// in which the daemon's health is honestly UNDETERMINED — so `omw daemon status`, run
	// immediately after a successful start, answered "could not determine" about a daemon that was
	// perfectly fine. Found by exactly that sequence in
	// TestStartStopAndStatusThroughTheRealBinary. Criterion 1 says the daemon is running and
	// reports itself as running after start returns; a start that returns before it knows whether
	// it can do its job has returned too early.
	//
	// A daemon that cannot write at this point does not start at all: it records that it ended
	// because it could not write, releases everything, and Start fails. Criterion 15 with no
	// running-but-broken interval in front of it.
	if err := d.pumpOnce(); err != nil {
		d.control.Close()
		d.lock.release()
		return nil, fmt.Errorf("the daemon could not write to the store, so it did not start: %w", err)
	}
	return d, nil
}

// previousEnding reads how the run before this one ended.
func previousEnding(p runPaths) (Ending, string) {
	rec, err := readRunRecord(p)
	switch {
	case errors.Is(err, errNoRunRecord):
		return EndingNeverRun, ""
	case err != nil:
		return EndingUndetermined, err.Error()
	}
	held, _, lerr := probeLock(p)
	running := tri.FromError(held, lerr)
	return endingOf(rec, running)
}

func (d *Daemon) recordControlState() {
	rec, err := readRunRecord(d.paths)
	if err != nil {
		return
	}
	rec.Control = d.controlState.Render("open", "not open")
	rec.ControlDetail = d.controlDetail
	_ = writeRunRecord(d.paths, rec)
}

// Control returns the daemon's control API, which is nil when it declined to open.
func (d *Daemon) Control() *Control { return d.control }

// ControlState is whether the control API is open, in three values (criterion 26).
func (d *Daemon) ControlState() (tri.Value, string) { return d.controlState, d.controlDetail }

// Report is the daemon's own state, and is what the control API serves.
//
// It does NOT go through Inspect. Inspect asks the control API when one is open, and a daemon
// answering its own control API by asking its own control API is a loop; more importantly, the
// daemon knows things the disk does not yet — notably that a write has just failed, which is the
// one fact criterion 17 forbids being late with.
func (d *Daemon) Report() Report {
	d.mu.Lock()
	ph, reason := d.ph, d.reason
	d.mu.Unlock()

	rep := Report{
		StorePath:     d.opts.StorePath,
		Running:       tri.Yes,
		PID:           os.Getpid(),
		StartedAt:     d.startedAt,
		LastRun:       d.prev,
		LastRunDetail: d.prevText,
		Control:       d.controlState,
		ControlDetail: d.controlDetail,
		ControlSocket: d.control.Path(),
	}
	switch ph {
	case phaseHealthy:
		rep.Healthy = tri.Yes
	case phaseStarting:
		// Not yet proven. Undetermined is the honest answer and it is not "fine".
		rep.Healthy = tri.Undetermined
		rep.HealthDetail = "the daemon has started and has not yet proved it can write"
	default:
		rep.Healthy = tri.No
		rep.HealthDetail = reason
	}
	rep.wire()
	return rep
}

// pumpOnce proves once that the daemon can still write, and stops the daemon if it cannot.
//
// THE FAILURE AND THE STATE CHANGE ARE ONE CRITICAL SECTION. Read that literally: d.mu is taken
// before Write's error is acted on and the phase is changed before it is released, so there is no
// interleaving in which a concurrent Report observes phaseHealthy after this call has seen a write
// fail. Criterion 17 is that sentence.
//
// It returns the write error so that Serve can stop; callers other than Serve are tests.
func (d *Daemon) pumpOnce() error {
	write := d.opts.Write
	if write == nil {
		write = d.storeWrite
	}
	err := write()

	d.mu.Lock()
	if err != nil {
		d.ph = phaseCannotWrite
		d.reason = "it could not write to the store, so it is stopping: " + err.Error()
	} else if d.ph == phaseStarting {
		d.ph = phaseHealthy
	}
	d.mu.Unlock()

	if err != nil {
		// Recorded immediately, so that a reader who never reaches the control API still learns
		// WHY the run ended (criterion 16) rather than inferring a crash.
		d.finish(EndingCannotWrite, err.Error())
		d.signalStop()
	}
	return err
}

// storeWrite is the real write probe: a record, written through the store, replaced in place.
func (d *Daemon) storeWrite() error {
	s, err := store.Open(d.opts.StorePath)
	if err != nil {
		return err
	}
	return s.PutJSON(heartbeatKind, heartbeatID, map[string]any{
		"pid": os.Getpid(), "at": time.Now().UTC(),
	})
}

// Serve runs the daemon until it is stopped or until it cannot write. It returns the reason it
// stopped: nil for an explicit stop, the write error otherwise.
func (d *Daemon) Serve() error {
	d.serving.Store(true)
	defer close(d.doneCh)
	// THE CAPABILITIES THAT ARE A PROPERTY OF THIS RUNNING (Issue #6). Registered background work
	// starts here and is stopped and waited for before Serve returns, so "the daemon is stopped"
	// and "ingestion is not happening" are the same instant rather than two nearby ones.
	stopBackground := d.startBackground()
	defer stopBackground()
	interval := d.opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	// PROVED BEFORE ANY WAITING. A daemon that reports itself healthy for one interval before its
	// first write is a daemon that reports healthy while writes fail, for a bounded time — which
	// criterion 17 does not carve out an exception for.
	if err := d.pumpOnce(); err != nil {
		return err
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-d.stopCh:
			d.mu.Lock()
			if d.ph != phaseCannotWrite {
				d.ph = phaseStopping
			}
			d.mu.Unlock()
			return nil
		case <-t.C:
			if err := d.pumpOnce(); err != nil {
				return err
			}
		}
	}
}

func (d *Daemon) signalStop() { d.stopOnce.Do(func() { close(d.stopCh) }) }

// Stop ends this run explicitly and records that that is how it ended (criterion 10's first
// rendering). It is safe to call more than once.
func (d *Daemon) Stop() {
	d.mu.Lock()
	if d.ph != phaseCannotWrite {
		d.ph = phaseStopping
		d.reason = "it is stopping because it was asked to"
	}
	d.mu.Unlock()
	d.signalStop()
}

// finish records how this run ended. The FIRST ending recorded wins: a daemon that stopped because
// it could not write, and is then also asked to stop, ended because it could not write.
func (d *Daemon) finish(e Ending, detail string) {
	d.endOnce.Do(func() {
		rec, err := readRunRecord(d.paths)
		if err != nil {
			rec = runRecord{Format: runFormat, PID: os.Getpid(), StartedAt: d.startedAt}
		}
		rec.Format = runFormat
		rec.Phase = phaseEnded
		rec.Code = codeFor(e)
		rec.EndedAt = time.Now().UTC()
		rec.Detail = detail
		// A FAILURE TO RECORD IS NOT HIDDEN AND NOT FATAL. If this write fails too — which is
		// likely, since the reason for stopping may be that nothing can be written — the run
		// record keeps saying "running" and the next Inspect reports a crash. That is the honest
		// reading of a process that could not say how it ended.
		_ = writeRunRecord(d.paths, rec)
	})
}

// Close ends the run, closes the control API and releases the store's lock, in that order.
//
// THE LOCK GOES LAST. Between the ending being recorded and the lock being released, a reader sees
// "running, and its run has ended because X" — odd for a moment, but never "running, fine". If the
// lock went first, another daemon could take the store before this one had said how it ended.
func (d *Daemon) Close() {
	d.Stop()
	if d.serving.Load() {
		<-d.doneCh
	}
	d.mu.Lock()
	e, reason := EndingStopped, d.reason
	if d.ph == phaseCannotWrite {
		e = EndingCannotWrite
	}
	d.ph = phaseStoppedPhase
	d.mu.Unlock()
	d.finish(e, reason)
	d.control.Close()
	d.lock.release()
}
