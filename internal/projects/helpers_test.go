package projects_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// runUntil runs the watcher until stop is closed. This is the whole of what Issue #2's daemon must
// do to watch projects, and running it here is how criteria 4 and 5 are driven without importing a
// daemon that lives on another branch.
func runUntil(s *store.Store, stop <-chan struct{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	return projects.Run(ctx, s, noEnv)
}

// storedFiles reads the file count a poll RECORDED for a project, straight out of the store.
//
// It deliberately does not go through projects.Take. Criterion 4 says "reflecting a change only when
// a listing command is run is a failure", so the observation must not be capable of causing the
// thing observed — and Take, with nothing watching, would scan the directory itself and report the
// new count on a build where the poller does nothing at all.
//
// It returns -1 when no state has been recorded yet, which is distinct from a recorded zero.
func storedFiles(t *testing.T, s *store.Store, path string) int {
	t.Helper()
	var ss struct {
		State    projects.State `json:"state"`
		PolledAt time.Time      `json:"polled_at"`
	}
	if err := s.GetJSON(projects.KindState, projects.ProjectID(path), &ss); err != nil {
		return -1
	}
	return ss.State.Files
}

// waitFor polls a condition until it holds or the deadline passes, and fails naming what it was
// waiting for.
//
// It exists so that criteria 4 and 5 are not driven by a fixed sleep tuned to one machine: a sleep
// long enough for a loaded CI box is a slow suite everywhere else, and a sleep short enough to be
// quick is a flake. The DIRECTION of the wait carries the meaning — criterion 4 waits for a change
// to appear, criterion 5 sleeps a fixed span and requires that nothing appeared, and only the second
// of those can honestly be a sleep.
func waitFor(t *testing.T, limit time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", limit, what)
}

// wholeStore is every file in the store with its contents, so a test can assert that NOTHING
// anywhere advanced — not the project state, not the heartbeat, not anything a future Issue adds.
//
// A check written as "the recorded file count did not change" would pass on a build whose poller
// kept writing a fresh timestamp every two seconds with the daemon stopped, and that build IS
// something watching between commands. Criterion 5 says "no state anywhere", so this reads
// everywhere.
func wholeStore(t *testing.T, s *store.Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(s.Path(), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(s.Path(), p)
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the store at %s: %v", s.Path(), err)
	}
	if len(out) == 0 {
		t.Fatalf("the store at %s holds no files, so comparing it before and after says nothing", s.Path())
	}
	return out
}
