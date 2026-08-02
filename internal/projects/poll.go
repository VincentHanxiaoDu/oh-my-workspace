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
// A failing poll stops the loop and returns the error rather than continuing. The project's standing
// rule: "the daemon stops when it cannot write rather than continuing in a state a person reads as
// healthy" — and a poll loop that keeps ticking over a store it cannot write to keeps refreshing
// nothing while the store's lock still says a daemon is up, so the listing would report
// daemon-polled provenance over state that stopped advancing. The caller is owed the error.
func Run(ctx context.Context, s *store.Store, getenv func(string) string) error {
	if err := Poll(s, getenv, time.Now().UTC()); err != nil {
		return err
	}
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-t.C:
			if err := Poll(s, getenv, now.UTC()); err != nil {
				return err
			}
		}
	}
}
