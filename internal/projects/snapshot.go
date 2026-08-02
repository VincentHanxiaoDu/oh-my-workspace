package projects

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Entry is one project and its state, together with WHERE THAT STATE CAME FROM.
//
// The provenance is a field of the entry and not of the listing, because the two can differ per
// project inside one listing: a daemon that has polled two of three projects so far leaves the third
// to be examined by the command. A listing-wide banner would then be a lie about one row.
type Entry struct {
	Project    Project    `json:"project"`
	State      State      `json:"state"`
	Provenance Provenance `json:"provenance"`
	// PolledAt is when a daemon poll produced this state. Zero for ExaminedNow. It is reported so a
	// person can see the staleness they were promised was "at most a couple of seconds" — but it is
	// NOT what distinguishes the two cases. Criterion 6 forbids inferring provenance from timing, so
	// the distinction is Provenance, which is stated outright.
	PolledAt time.Time `json:"polled_at,omitempty"`
}

// Snapshot is a whole project listing at one moment: the single determination both surfaces render.
type Snapshot struct {
	// Watching is whether something was watching when this snapshot was taken. tri, not bool: a
	// store that could not be read has not told us that nothing is watching.
	Watching tri.Value `json:"watching"`
	Entries  []Entry   `json:"entries"`
	TakenAt  time.Time `json:"taken_at"`
}

// storedState is the daemon's polled result as it lives in the store.
type storedState struct {
	State    State     `json:"state"`
	PolledAt time.Time `json:"polled_at"`
}

// Take produces the listing. This is the whole determination; everything else renders it.
//
// THE PROVENANCE DECISION, WHICH IS CRITERION 6. Something is watching, so the state on screen was
// produced by that watcher — read it and stamp DaemonPolled. Nothing is watching, so nothing has
// produced any state since the last command — walk the directories here and stamp ExaminedNow.
// There is no third path in which state appears without a stamp.
//
// When something IS watching but has not yet written a state for a project (a project added seconds
// ago), this examines that project itself and stamps ExaminedNow on it — the honest answer for that
// row. Reporting DaemonPolled for a poll that has not happened would be the criterion failed in the
// one case where a person most needs the truth.
//
// IT NEVER STARTS ANYTHING (criterion 11). Take reads a heartbeat; it does not write one, does not
// call Poll, and does not spawn. Running a listing with the daemon stopped leaves it stopped.
func Take(s *store.Store, getenv func(string) string, now time.Time) (Snapshot, error) {
	projects, err := List(s)
	if err != nil {
		return Snapshot{}, err
	}
	watching := Watching(s, now)
	depth := DepthFor(getenv)

	snap := Snapshot{Watching: watching, TakenAt: now, Entries: make([]Entry, 0, len(projects))}
	for _, p := range projects {
		e := Entry{Project: p}
		if watching == tri.Yes {
			var ss storedState
			if err := s.GetJSON(KindState, p.ID, &ss); err == nil {
				e.State, e.Provenance, e.PolledAt = ss.State, DaemonPolled, ss.PolledAt
				snap.Entries = append(snap.Entries, e)
				continue
			} else if !errors.Is(err, store.ErrRecordNotFound) {
				return Snapshot{}, err
			}
		}
		e.State, e.Provenance = Scan(p.Path, depth), ExaminedNow
		snap.Entries = append(snap.Entries, e)
	}
	return snap, nil
}

// MarshalSnapshot is the snapshot's wire form, and is the contract Issue #2's control API is to
// serve so that criterion 14 holds by construction.
//
// It exists as a named function rather than as "call json.Marshal on it" so that the boundary is a
// thing a future reader can find, and so that the field names the control API publishes cannot drift
// from the ones this package's own tests assert on.
func MarshalSnapshot(snap Snapshot) ([]byte, error) { return json.Marshal(snap) }

// UnmarshalSnapshot is the inverse, for a client of the control API.
func UnmarshalSnapshot(b []byte) (Snapshot, error) {
	var s Snapshot
	err := json.Unmarshal(b, &s)
	return s, err
}
