package projects_test

// Issue #72: is the watch itself load-sensitive, or only the harness that observes it?
//
// The Issue records a test that goes red under parallel load. A false red on a suite is expensive —
// a reader learns to re-run until green — but the DANGEROUS reading is the inverse one: if the
// timing sensitivity lives in the WATCH rather than in its harness, then a loaded machine makes the
// daemon MISS A CHANGE, which is invisible and looks exactly like a directory that did not change.
//
// These tests settle that question about this watcher, and they are in the project's own suite
// rather than in a note somewhere because it is the kind of claim that has to keep being true.
//
// THE PROPERTY IS THAT THE WATCHER IS LEVEL-TRIGGERED. Each pass calls [projects.Scan] afresh and
// records what is there NOW; it accumulates nothing from one pass to the next and subscribes to no
// change events. So a tick that Go's ticker drops under load — and it does drop them, it does not
// queue them — costs LATENCY AND NOTHING ELSE: the next pass records the same current truth the
// missed one would have. There is no state a missed pass was carrying.
//
// An edge-triggered watcher — one that emitted or counted a change once, when it saw it — would
// fail both tests below, and that is the build these are here to catch. If a later Issue makes this
// poller incremental for cost reasons, these go red, and that is the point: the cost win would come
// with a silent-drop failure mode, and it should not be possible to take it by accident.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// TestAPollRecordsWhatIsThereNowRatherThanWhatItWasToldChanged starves the watcher completely —
// every tick between these polls is dropped, because none is ever delivered — and requires that
// nothing is lost by it.
//
// THIS IS THE WORST CASE AND NOT AN APPROXIMATION OF ONE. "Load made the daemon miss ticks" cannot
// be more severe than "no tick arrived at all", so a watcher that survives this survives any amount
// of load with latency as its only symptom. Driving it this way also removes the wall clock from the
// assertion: there is no interval to be slower than.
func TestAPollRecordsWhatIsThereNowRatherThanWhatItWasToldChanged(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Three files appear with NO poll running. An edge-triggered watcher hears about them here, and
	// this is the moment it would have to have been listening.
	writeFiles(t, dir, "a.txt", "b.txt", "c.txt")
	poll(t, s)
	if got := storedFiles(t, s, dir); got != 3 {
		t.Fatalf("after three files appeared unobserved, one poll recorded %d files, want 3: "+
			"this watcher is not reporting what is there now, so a pass missed under load loses a change", got)
	}

	// Two more, again with nothing running, and again in one pass.
	writeFiles(t, dir, "d.txt", "e.txt")
	poll(t, s)
	if got := storedFiles(t, s, dir); got != 5 {
		t.Fatalf("after two more files appeared unobserved, one poll recorded %d files, want 5", got)
	}

	// AND IT GOES BACK DOWN. A watcher that only ever added would pass everything above while still
	// being incapable of noticing a removal it did not witness.
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	poll(t, s)
	if got := storedFiles(t, s, dir); got != 4 {
		t.Fatalf("after a file was removed unobserved, one poll recorded %d files, want 4", got)
	}

	// A pass over an unchanged directory records the same thing. Two polls in a row cannot disagree,
	// so nothing here depends on how many passes ran.
	poll(t, s)
	if got := storedFiles(t, s, dir); got != 4 {
		t.Fatalf("a second poll over an unchanged directory recorded %d files, want 4: polls are not idempotent", got)
	}
}

// TestTheRunningWatcherConvergesOnTheTruthAfterAChurn drives the real [projects.Run] loop rather
// than single passes, and requires the same property of it end to end.
//
// IT WAITS FOR A POLL, NOT FOR THE ANSWER. The wait is bounded by a STALL — a span in which the
// watcher recorded nothing at all — and every poll observed starts that budget again, so a machine
// under any amount of load takes longer here and does not fail. Then a poll stamped after the last
// write must report the final truth exactly, first time, because such a poll's scan ran after every
// one of those files existed. Waiting instead for the count to reach 12 would have turned a watcher
// that records the wrong number forever into a timeout rather than a wrong count.
func TestTheRunningWatcherConvergesOnTheTruthAfterAChurn(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatalf("add: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _ = runUntil(s, stop) }()
	t.Cleanup(func() { close(stop); <-done })

	// A churn far faster than the poll interval: most of these changes are never separately observed
	// by any pass, which is exactly the condition a loaded machine produces.
	var names []string
	for i := 0; i < 12; i++ {
		names = append(names, "churn-"+string(rune('a'+i))+".txt")
	}
	writeFiles(t, dir, names...)
	mark := time.Now().UTC()

	at, files := waitForPollAfter(t, s, dir, mark)
	if files != 12 {
		t.Fatalf("a poll stamped %s — after every one of the 12 files existed — recorded %d files, want 12: "+
			"the watcher dropped changes it did not separately observe", at.Format(time.RFC3339Nano), files)
	}
}

// poll runs one pass and fails if it could not.
func poll(t *testing.T, s *store.Store) {
	t.Helper()
	if err := projects.Poll(s, noEnv, time.Now().UTC()); err != nil {
		t.Fatalf("poll: %v", err)
	}
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pollStallBudget is how long the watcher may record NO poll at all before that is a failure.
//
// It bounds a STALL and not the test, and the two are not the same rule. A budget on total elapsed
// time fails a slow machine running a correct watcher — the flake Issue #72 records. This one is
// reset by every poll observed, so slowness costs time and never a verdict, while a watcher that
// records nothing trips it whatever the machine is doing.
const pollStallBudget = 60 * time.Second

// waitForPollAfter waits for a poll the watcher recorded strictly after `after`, and returns when it
// ran and what it saw.
func waitForPollAfter(t *testing.T, s *store.Store, path string, after time.Time) (time.Time, int) {
	t.Helper()
	var last time.Time
	stallUntil := time.Now().Add(pollStallBudget)
	for {
		at, files, ok := storedPoll(t, s, path)
		if ok && at.After(after) {
			return at, files
		}
		if ok && at.After(last) {
			last = at
			stallUntil = time.Now().Add(pollStallBudget)
		}
		if time.Now().After(stallUntil) {
			if last.IsZero() {
				t.Fatalf("the watcher recorded no poll at all within %v: nothing is advancing project state", pollStallBudget)
			}
			t.Fatalf("the watcher stopped recording polls for %v (its last was stamped %s, and this waited for one after %s)",
				pollStallBudget, last.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// storedPoll reads when a poll ran and what it saw, straight out of the store. ok is false when no
// poll has recorded anything yet — distinct from a recorded zero.
func storedPoll(t *testing.T, s *store.Store, path string) (at time.Time, files int, ok bool) {
	t.Helper()
	var ss struct {
		State    projects.State `json:"state"`
		PolledAt time.Time      `json:"polled_at"`
	}
	if err := s.GetJSON(projects.KindState, projects.ProjectID(path), &ss); err != nil {
		return time.Time{}, 0, false
	}
	return ss.PolledAt, ss.State.Files, true
}
