package inbox

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// CRITERION 10, DRIVEN BY KILLING A PROCESS IN THE MIDDLE OF A MERGE.
//
// "An interrupted merge leaves the store readable and leaves the inbox in exactly one of the two
// states — all inputs still separate, or one merged ticket — never a half-merged state with
// duplicated or missing tickets."
//
// A test that calls Merge and then inspects a temporary file has not tested that. It has tested the
// code's intentions, in a process that was never interrupted, with every buffer flushed by the
// runtime on the way out. So this file re-invokes the TEST BINARY ITSELF as a child, has the child
// seed N source tickets that COMPLETE, announce itself, and begin a merge; the parent SIGKILLs it
// partway — a signal no deferred function, no flush and no cleanup handler survives — reopens the
// store, and asserts the inbox is at one of the two endpoints and at neither halfway house.
//
// IT KILLS AT SIX DIFFERENT MOMENTS, from one millisecond after the merge begins to nearly a second
// in, because the two endpoints sit either side of a single instant — the rename of the batch
// journal — and a test that only ever kills after it would never observe the "inputs still separate"
// outcome at all.
//
// THE TEST'S OWN HONESTY CHECKS, and there are three, because every one of them is a way this could
// go green having proved nothing:
//
//   - Every cycle's child must have been KILLED, not have exited. A child that finished the merge
//     under its own power was not interrupted.
//   - At least one cycle must land on each endpoint. Only observing one of them means the window was
//     never straddled and the assertion never had two cases to tell apart.
//   - At least one cycle must be killed with a committed batch journal on disk, so the recovery path
//     that makes the guarantee true is actually exercised rather than assumed.

const (
	crashChildEnv = "OMW_MERGE_CRASH_CHILD"
	crashRootEnv  = "OMW_MERGE_CRASH_ROOT"

	// crashInputs is large enough that applying the batch takes real time — every put and every
	// delete in it fsyncs twice — so there is a middle for the kill to land in.
	crashInputs = 120

	crashMergedID = "themerge"
)

func TestMain(m *testing.M) {
	if os.Getenv(crashChildEnv) != "" {
		mergeCrashChild()
		return
	}
	os.Exit(m.Run())
}

func crashSourceID(i int) string { return fmt.Sprintf("src%03d", i) }

// mergeCrashChild runs inside the doomed process. It never returns normally: the parent kills it.
// Its exit codes are all failures the parent reports, because a child that exits by itself has not
// been interrupted and its cycle proved nothing.
func mergeCrashChild() {
	root := os.Getenv(crashRootEnv)
	s, err := store.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "child: Open(%s): %v\n", root, err)
		os.Exit(11)
	}
	spec := MergeSpec{
		ID:      crashMergedID,
		Title:   Text("one broken login, reported many times"),
		Summary: Text("Everything below is the same authentication failure, reported by different people."),
		Channel: Undetermined("merged from more than one channel"),
		When:    time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
	}
	for i := 0; i < crashInputs; i++ {
		spec.Inputs = append(spec.Inputs, InputSpec{
			TicketID: crashSourceID(i),
			Why:      Text("the same broken login"),
			Source:   Text(crashSourceID(i) + "@example.invalid"),
		})
	}

	fmt.Fprintln(os.Stdout, "READY")
	os.Stdout.Sync()

	_, err = Merge(s, spec)
	fmt.Fprintf(os.Stderr, "child: the merge RETURNED (%v) — it was supposed to be killed\n", err)
	os.Exit(14)
}

func TestAMergeKilledPartwayLeavesTheInboxAtOneEndpointOrTheOther(t *testing.T) {
	if testing.Short() {
		t.Skip("this test kills subprocesses and takes seconds")
	}
	// HOW LONG A MERGE TAKES IS A PROPERTY OF THE MACHINE, NOT OF THE PRODUCT, so the kill times are
	// measured rather than written down. The first version used a fixed list ending at 900ms; on this
	// developer's machine the merge finished in less than that and the last cycle killed a process
	// that had already returned. The test caught it — and on a slower machine the same list would
	// have failed at the other end, as a flake.
	full := timeAnUninterruptedMerge(t)
	t.Logf("an uninterrupted merge of %d tickets takes %v on this machine", crashInputs, full)

	separate, mergedOutcome, journalSeen := 0, 0, 0
	cycle := 0

	// one kill, one verdict. It fatals on the thing criterion 10 forbids — a half-merged inbox — and
	// otherwise reports which endpoint was reached, or "" when the child outran the kill.
	run := func(delay time.Duration) string {
		cycle++
		root := filepath.Join(t.TempDir(), "store")
		want := seedCrashInputs(t, root)
		hadJournal, killed := killMidMerge(t, cycle, root, delay)
		if !killed {
			return ""
		}
		if hadJournal {
			journalSeen++
		}

		// A READER THAT KNOWS NOTHING ABOUT MERGING. This is `omw inbox list`'s path exactly.
		reopened, err := store.Open(root)
		if err != nil {
			t.Fatalf("cycle %d: reopening the store after the kill = %v — criterion 10 requires it "+
				"stays readable", cycle, err)
		}
		got, err := List(reopened)
		if err != nil {
			t.Fatalf("cycle %d: listing the inbox after the kill = %v", cycle, err)
		}
		present := map[string]bool{}
		for _, tk := range got {
			present[tk.ID] = true
		}

		switch {
		case len(got) == crashInputs && !present[crashMergedID]:
			// ENDPOINT A: all inputs still separate. Every one of them, with the content it had.
			for id, body := range want {
				rec, gerr := reopened.Get(Kind, id)
				if gerr != nil {
					t.Fatalf("cycle %d: the inputs are separate but %s is gone: %v", cycle, id, gerr)
				}
				if string(rec.Data) != string(body) {
					t.Fatalf("cycle %d: input %s came back changed by an interrupted merge", cycle, id)
				}
			}
			if _, merr := LoadMerge(reopened, crashMergedID); !errors.Is(merr, ErrNotMerged) {
				t.Fatalf("cycle %d: the inputs are separate and a merge record exists anyway: %v", cycle, merr)
			}
			separate++
			return "inputs separate"

		case len(got) == 1 && present[crashMergedID]:
			// ENDPOINT B: one merged ticket, showing its working and still reversible.
			record, merr := LoadMerge(reopened, crashMergedID)
			if merr != nil {
				t.Fatalf("cycle %d: the merge happened and its record is unreadable: %v", cycle, merr)
			}
			if len(record.Inputs) != crashInputs {
				t.Fatalf("cycle %d: the merged ticket records %d inputs; %d went in",
					cycle, len(record.Inputs), crashInputs)
			}
			if _, uerr := Unmerge(reopened, crashMergedID, time.Now()); uerr != nil {
				t.Fatalf("cycle %d: the merge that survived the kill is not reversible: %v", cycle, uerr)
			}
			for id, body := range want {
				rec, gerr := reopened.Get(Kind, id)
				if gerr != nil || string(rec.Data) != string(body) {
					t.Fatalf("cycle %d: %s did not come back exactly after unmerging a merge that "+
						"completed through a crash: %v", cycle, id, gerr)
				}
			}
			mergedOutcome++
			return "one merged ticket"

		default:
			missing := 0
			for i := 0; i < crashInputs; i++ {
				if !present[crashSourceID(i)] {
					missing++
				}
			}
			t.Fatalf("cycle %d (killed %v in): the inbox holds %d tickets — %d of the %d inputs are "+
				"missing and the merged ticket is present: %v. That is a HALF-MERGED inbox, which "+
				"criterion 10 forbids: it must hold all the inputs or the one merged ticket, and "+
				"nothing in between",
				cycle, delay, len(got), missing, crashInputs, present[crashMergedID])
			return ""
		}
	}

	// The spread of kills. It is deliberately weighted towards the start: the commit point is one
	// rename that happens after the merge has only READ its inputs, so the window in which the batch
	// has not yet been committed is a small fraction of the whole and a uniform spread misses it.
	for _, fraction := range []float64{0.002, 0.01, 0.05, 0.2, 0.5, 0.75} {
		run(time.Duration(float64(full) * fraction))
	}

	// AND THEN THE SEARCH, because the spread above is a guess about where a boundary is and the
	// test must not depend on the guess being right on somebody else's disk. Halving the delay must
	// eventually land before the commit point, and doubling it must eventually land after; each is
	// tried until it does. This is what turns "the kills happened to straddle the boundary" into
	// "the boundary was found", and it is why the honesty check below can be a hard failure.
	for delay := time.Duration(float64(full) * 0.002); separate == 0 && delay > 0; delay /= 2 {
		if r := run(delay); r == "" {
			t.Fatalf("a kill %v into the merge found the child already finished, which cannot be: "+
				"the whole merge takes %v", delay, full)
		}
	}
	for delay := time.Duration(float64(full) * 0.5); mergedOutcome == 0 && delay < 10*full; delay *= 2 {
		run(delay)
	}

	if separate == 0 || mergedOutcome == 0 {
		t.Fatalf("across %d kills the outcome was 'inputs separate' %d times and 'one merged ticket' "+
			"%d times. Both endpoints must be observed or the kill never straddled the commit point, "+
			"and this test has told us nothing about the one that was never reached.",
			cycle, separate, mergedOutcome)
	}
	if journalSeen == 0 {
		t.Fatalf("no cycle was killed with a committed batch on disk, so the recovery that makes the "+
			"guarantee true was never exercised — this test proved less than it claims")
	}
	t.Logf("%d kills: %d left the inputs separate, %d left one merged ticket, %d were killed with a "+
		"committed batch still on disk", cycle, separate, mergedOutcome, journalSeen)
}

// seedCrashInputs builds a store holding the source tickets, and returns their stored payloads —
// the snapshot every assertion about "the content it had" is made against.
func seedCrashInputs(t *testing.T, root string) map[string][]byte {
	t.Helper()
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := map[string][]byte{}
	for i := 0; i < crashInputs; i++ {
		id := crashSourceID(i)
		tk := Ticket{
			ID:      id,
			Title:   Text("login fails for person " + id),
			Summary: Text(strings.Repeat("the same authentication failure, described at length. ", 40)),
			Channel: Text("email"),
			Arrived: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
		}
		if err := Put(s, tk); err != nil {
			t.Fatalf("seeding %s: %v", id, err)
		}
		rec, err := s.Get(Kind, id)
		if err != nil {
			t.Fatal(err)
		}
		want[id] = append([]byte(nil), rec.Data...)
	}
	return want
}

// timeAnUninterruptedMerge measures how long the child's merge takes when nobody kills it, so the
// kills can be placed inside it on any machine.
func timeAnUninterruptedMerge(t *testing.T) time.Duration {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	seedCrashInputs(t, root)
	started := time.Now()
	if _, killed := killMidMerge(t, -1, root, time.Minute); killed {
		t.Fatal("the calibration child was killed; it was supposed to be left alone")
	}
	took := time.Since(started)
	if took <= 0 {
		t.Fatal("the calibration merge took no measurable time, so the kills below cannot be placed inside it")
	}
	return took
}

// killMidMerge runs one doomed child and SIGKILLs it `after` into its merge. It reports whether a
// committed batch was on disk at the moment of the kill, and whether the kill landed at all — a
// child that returned under its own power was not interrupted and its cycle proved nothing.
func killMidMerge(t *testing.T, cycle int, root string, after time.Duration) (journal, killed bool) {
	t.Helper()

	cmd := exec.Command(os.Args[0])
	// BOTH XDG_DATA_HOME AND HOME ARE SANDBOXED. The device-store pointer resolves from the first
	// and falls back to the second, and a spawn that inherits the developer's own environment
	// repoints their real store at a t.TempDir() that is then deleted. Nothing in the child asks for
	// the device store today; this is set so that it stays true if the child ever changes.
	sandbox := t.TempDir()
	cmd.Env = append(os.Environ(),
		crashChildEnv+"=1",
		crashRootEnv+"="+root,
		"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var childErr strings.Builder
	cmd.Stderr = &childErr
	if err := cmd.Start(); err != nil {
		t.Fatalf("cycle %d: starting the doomed child: %v", cycle, err)
	}

	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if sc.Text() == "READY" {
				ready <- nil
				io.Copy(io.Discard, stdout)
				return
			}
		}
		ready <- errors.New("the child never announced itself")
	}()

	select {
	case err := <-ready:
		if err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatalf("cycle %d: %v; child stderr:\n%s", cycle, err, childErr.String())
		}
	case <-time.After(60 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("cycle %d: the child never became ready; stderr:\n%s", cycle, childErr.String())
	}

	// WAITING AND KILLING ARE ONE SELECT, not a sleep followed by a kill. A sleep cannot tell the
	// difference between "the merge is still going" and "the merge finished forty milliseconds ago",
	// and the caller needs that difference: a child that outran the kill is a cycle to retry, not a
	// defect. It is also what lets the calibration run pass an enormous delay and simply be told when
	// the merge ended.
	done := make(chan *os.ProcessState, 1)
	go func() {
		st, werr := cmd.Process.Wait()
		if werr != nil {
			st = nil
		}
		done <- st
	}()

	timer := time.NewTimer(after)
	defer timer.Stop()
	select {
	case state := <-done:
		if state == nil {
			t.Fatalf("cycle %d: waiting for the child failed", cycle)
		}
		if state.ExitCode() != 14 {
			// The child's own assertions failed — it never even reached the merge.
			t.Fatalf("cycle %d: the child exited with code %d before the merge; stderr:\n%s",
				cycle, state.ExitCode(), childErr.String())
		}
		return committedBatchOnDisk(root), false
	case <-timer.C:
	}

	journal = committedBatchOnDisk(root)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cycle %d: killing the child: %v", cycle, err)
	}
	state := <-done
	if state == nil {
		t.Fatalf("cycle %d: waiting for the killed child failed", cycle)
	}
	if state.Exited() {
		// It exited under its own power in the instant between the timer and the kill. Not
		// interrupted, so the caller retries rather than believing anything about this cycle.
		if state.ExitCode() != 14 {
			t.Fatalf("cycle %d: the child exited with code %d; stderr:\n%s",
				cycle, state.ExitCode(), childErr.String())
		}
		return journal, false
	}
	return journal, true
}

// committedBatchOnDisk reports whether a batch has passed its commit point and not yet been cleaned
// up. It reads the store's directory layout directly rather than through the store, because at this
// instant the store is exactly what is on disk and nothing else.
func committedBatchOnDisk(root string) bool {
	entries, err := os.ReadDir(filepath.Join(root, "journal"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".rec") && !strings.HasPrefix(name, ".") {
			return true
		}
	}
	return false
}

