package projects

import (
	"context"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Poll examines every project once and records the results as the daemon's polled state.
//
// THIS IS THE ONLY THING IN THE PRODUCT THAT ADVANCES PROJECT STATE IN THE BACKGROUND, and that is
// criterion 5 expressed as a property of the code: a client in which nobody calls Poll is a client
// in which no state anywhere advances between commands, no matter how long a person waits. There is
// no init, no package-level goroutine and no lazy start that could make it otherwise.
//
// IT WRITES NO HEARTBEAT. An earlier version did, and read it back to decide whether anything was
// watching — a second answer to "is the daemon running" of exactly the kind Issue #41 removed. The
// daemon holds the store's lock, and daemon.Inspect reading that lock is the one answer; a poll
// that also announced itself would be a second thing to disagree with it.
func Poll(s *store.Store, getenv func(string) string, now time.Time) error {
	projects, err := List(s)
	if err != nil {
		return err
	}
	depth := DepthFor(getenv)
	for _, p := range projects {
		st := Scan(p.Path, depth)
		if err := s.PutJSON(KindState, p.ID, storedState{State: st, PolledAt: now}); err != nil {
			return err
		}
	}
	return nil
}

// Run polls until ctx is cancelled, and is the entire contract Issue #2's daemon has with this
// package: run this, and projects are watched; do not, and nothing is.
//
// It polls once immediately so that a daemon which has just started is not reporting a stale state
// for a whole interval, then on a [PollInterval] ticker — PRD §3.6's "every couple of seconds",
// which criterion 4 is stated in terms of.
//
// A FAILING POLL DOES NOT STOP THE LOOP, AND DOES NOT STOP THE DAEMON. Deciding to stop is the
// daemon's own business and it already has the means: PRD §4.3, "the daemon stops when it cannot
// write", is enforced by the daemon's own write probe against the same store. A poller that also
// exited on a write error would be a second thing making that decision, on less evidence — and one
// that exited on a TRANSIENT error would silently stop watching a person's directories for the rest
// of the run while the daemon went on reporting itself healthy.
//
// A poll that fails writes no state record, and a project with no record is examined by the listing
// and stamped ExaminedNow. So a run whose polls are all failing degrades to exactly what a person
// would see with no daemon at all, per row, honestly — rather than to a stale number wearing a
// daemon-polled stamp.
func Run(ctx context.Context, s *store.Store, getenv func(string) string) error {
	_ = Poll(s, getenv, time.Now().UTC())
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-t.C:
			// The error is deliberately not propagated; see the comment above.
			_ = Poll(s, getenv, now.UTC())
		}
	}
}
