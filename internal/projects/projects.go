package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Record kinds this package owns in the device's store.
//
// Three kinds and not one, because they have three different lifetimes and three different owners.
// KindProject is written by a person's explicit act and outlives everything. KindState is the
// daemon's polled result and is meaningless once stale. KindHeartbeat is the daemon's liveness and
// is meaningless the moment the daemon stops. Folding the state into the project record would mean
// the daemon rewriting a person's registry entry every couple of seconds — a poll that can lose a
// project to a bad write.
const (
	KindProject   = store.Kind("project")
	KindState     = store.Kind("project-state")
	KindHeartbeat = store.Kind("project-watch")
)

// PollInterval is how often a running daemon re-examines each watched directory.
//
// PRD §3.6 says "every couple of seconds", and Issue #4's ruling is explicit that the PRD wins over
// the reference project's 8s. Criterion 4 is stated in terms of this value: a re-read at t≈0 may
// see the old state, a re-read after this interval must see the new one.
const PollInterval = 2 * time.Second

// WatchTimeout is how long after a heartbeat the client still believes something is watching.
//
// WHY A MULTIPLE AND NOT THE INTERVAL ITSELF. A daemon that is alive and simply slow to get its
// next tick scheduled must not flicker into "nothing is watching", because that would make the
// provenance of criterion 6 depend on scheduler luck rather than on what actually produced the
// state. Three intervals is long enough that a healthy daemon never trips it and short enough that
// a killed one is disbelieved within a few seconds — criterion 5's "wait well beyond the poll
// interval" is well beyond this too.
const WatchTimeout = 3 * PollInterval

// ErrNotAProject is returned when a path a person offers cannot be a project directory: it does not
// exist, or it exists and is not a directory.
//
// Criterion 13 allows either refusing or accepting-and-marking-missing, but not accepting and
// rendering as ordinary. This build refuses, and the refusal reaches the person as an exit code —
// "distinguishable from success without parsing prose" is the criterion's own wording.
var ErrNotAProject = errors.New("not a directory that can be a project")

// Project is one directory a person has pointed the client at.
type Project struct {
	// ID is derived from Path and is stable: adding the same directory twice yields the same id,
	// which is how criterion 1's "does not produce two entries" is a property of the storage rather
	// than of a de-duplicating pass somebody can forget to run.
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	AddedAt time.Time `json:"added_at"`
}

// ProjectID is the id a path maps to. Cleaned and absolute first, so that /a/b, /a/b/ and /a/./b are
// one project — criterion 1 again, at the only layer that can enforce it.
func ProjectID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

// Provenance is where the state in a listing entry came from. Criterion 6.
type Provenance int

const (
	// ProvenanceUnrecorded is the zero value ON PURPOSE, and is not one of the two real answers.
	//
	// The failure this shape prevents: a struct built on an error path, or by a future caller who
	// did not know provenance existed, would otherwise default to whichever real answer happened to
	// be the zero — silently telling a person a scan was a daemon poll or the reverse. It renders as
	// a defect marker, never as either answer. Same reasoning as tri.Undetermined being tri's zero.
	ProvenanceUnrecorded Provenance = iota
	// DaemonPolled means a running daemon produced this state on one of its polls, before this
	// command was run.
	DaemonPolled
	// ExaminedNow means nothing was watching, so this command walked the directory itself.
	ExaminedNow
)

// String renders provenance for the listing. Every branch is a distinct non-empty phrase: criterion
// 6 is satisfied by the OUTPUT, so a provenance that rendered as "" would fail it while the struct
// looked correct.
func (p Provenance) String() string {
	switch p {
	case DaemonPolled:
		return "watched by the daemon"
	case ExaminedNow:
		return "examined during this command"
	default:
		return "PROVENANCE NOT RECORDED — this is a defect, not a state"
	}
}

// Add registers a directory as a project.
//
// It refuses a path that is not an existing directory (criterion 13) and is idempotent for a path
// already registered (criterion 1): re-adding returns the EXISTING record untouched, so a second add
// cannot quietly reset the AddedAt a person may be sorting by.
func Add(s *store.Store, path string) (Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, fmt.Errorf("resolving %s: %w", path, err)
	}
	abs = filepath.Clean(abs)

	// Lstat, not Stat: a symlink pointing at a directory is not itself a directory, and criterion 17
	// says links are not followed. Accepting one here would make the project root the one link the
	// walk is forbidden to descend.
	if st, err := os.Lstat(abs); err != nil || !st.IsDir() {
		return Project{}, fmt.Errorf("%s: %w", abs, ErrNotAProject)
	}

	p := Project{ID: ProjectID(abs), Path: abs, AddedAt: time.Now().UTC()}
	var existing Project
	switch err := s.GetJSON(KindProject, p.ID, &existing); {
	case err == nil:
		return existing, nil
	case errors.Is(err, store.ErrRecordNotFound):
		// not registered yet; fall through and write it.
	default:
		return Project{}, err
	}
	if err := s.PutJSON(KindProject, p.ID, p); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Remove unregisters a project. It touches nothing on disk outside the store — criterion 3's "the
// directory itself on disk is unaffected" is true because there is no code here that could make it
// false.
//
// It reports whether a project was actually registered, so the CLI can tell a person that the thing
// they asked to remove was not there. Removing an unregistered project is not an error: they asked
// for it to be gone and it is gone.
func Remove(s *store.Store, path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolving %s: %w", path, err)
	}
	id := ProjectID(filepath.Clean(abs))

	var existing Project
	err = s.GetJSON(KindProject, id, &existing)
	switch {
	case errors.Is(err, store.ErrRecordNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	if err := s.Delete(KindProject, id); err != nil {
		return false, err
	}
	// The polled state and the project are separate records, so removing the project must remove the
	// state too. Left behind, it would be served to whoever re-adds the same path later as a poll
	// result from a daemon run that ended weeks ago.
	if err := deleteIfAny(s, KindState, id); err != nil {
		return false, err
	}
	return true, nil
}

// deleteIfAny is Delete over a kind that may never have been written.
//
// A GAP IN #3's STORE, WORKED AROUND ON THIS SIDE. store.Delete documents that "removing one that is
// not there is not an error", and that holds for a record inside an existing kind — but a kind whose
// directory has never been created returns the raw ENOENT from the attempted unlink. On a machine
// where the daemon has never run, no project-state record has ever been written, so EVERY `omw
// projects remove` fails on a store that is in perfectly good order.
//
// It is not fixed in internal/store because that package belongs to Issue #3 and is in review; an
// edit there conflicts with a branch under review for no reason a reviewer of THIS branch can weigh.
// Reported on the PR so #3 can decide whether Delete should absorb it.
func deleteIfAny(s *store.Store, kind store.Kind, id string) error {
	err := s.Delete(kind, id)
	if err == nil || errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// List returns every registered project, ordered by path.
//
// Ordered by path and not by id, because the id is a hash and a listing ordered by it is ordered
// randomly as far as the person reading it is concerned.
func List(s *store.Store) ([]Project, error) {
	recs, err := s.List(KindProject)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Project, 0, len(recs))
	for _, r := range recs {
		var p Project
		if err := json.Unmarshal(r.Data, &p); err != nil {
			return nil, fmt.Errorf("project record %s: %w", r.ID, err)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// heartbeat is what a running daemon writes each poll so the client can tell something is watching.
type heartbeat struct {
	At time.Time `json:"at"`
}

// Watching reports whether something is currently watching the projects.
//
// IT RETURNS A tri.Value, NOT A BOOL. "The store could not be read" is not "nothing is watching",
// and collapsing the two would make a listing claim it had walked the directories itself when it
// had in fact failed to find out (PRD §4.3). The caller decides what to do with the third answer;
// this function will not decide it by returning No.
//
// It reads a heartbeat and NEVER writes one. Nothing in the reading path can turn the client into a
// watcher — criterion 11.
func Watching(s *store.Store, now time.Time) tri.Value {
	var hb heartbeat
	err := s.GetJSON(KindHeartbeat, "daemon", &hb)
	switch {
	case errors.Is(err, store.ErrRecordNotFound):
		return tri.No // no heartbeat has ever been written: determinedly, nothing is watching.
	case err != nil:
		return tri.Undetermined // the record is there and unreadable. We do not know.
	}
	if hb.At.IsZero() {
		return tri.Undetermined
	}
	if now.Sub(hb.At) > WatchTimeout {
		return tri.No // a daemon that stopped. Determined, and the reason criterion 5 is drivable.
	}
	return tri.Yes
}
