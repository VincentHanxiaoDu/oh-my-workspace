package store

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WHY THIS TEST SPAWNS A PROCESS AND KILLS IT.
//
// Criteria 10, 11 and 12 are about a machine that dies mid-sentence. A test that calls Put and then
// pokes at the temporary file has not tested that: it has tested the code's intentions, in a
// process that was never interrupted, with every buffer flushed by the runtime on the way out.
//
// So this file re-invokes the TEST BINARY ITSELF as a child (TestMain picks the child up from an
// environment variable), has the child open the store, write records that COMPLETE, announce
// itself, and then begin a write that deliberately takes seconds. The parent kills the child — a
// SIGKILL, which no deferred function, no flush and no cleanup handler survives — reopens the store
// and asserts what criterion 10 and 11 demand.
//
// It then does it again, and again (criterion 12 asks for N > 1), each cycle checking that
// everything completed in every EARLIER cycle is still there.
//
// THE TEST ALSO ASSERTS THAT THE KILL LANDED MID-WRITE. Without that, a build with no crash safety
// at all could pass by being killed before it opened its output file, and the whole exercise would
// be theatre.

const (
	crashChildEnv = "OMW_STORE_CRASH_CHILD"
	crashRootEnv  = "OMW_STORE_CRASH_ROOT"
	crashCycleEnv = "OMW_STORE_CRASH_CYCLE"

	crashKind = Kind("ticket")
)

// completeBody is the content of a record the child finishes writing. Distinctive so that a partial
// read cannot be mistaken for it.
func completeBody(cycle int) string {
	return "COMPLETE-TICKET-" + strconv.Itoa(cycle) + "-the-whole-body-and-nothing-missing"
}

func completeID(cycle int) string { return "done" + strconv.Itoa(cycle) }
func partialID(cycle int) string  { return "interrupted" + strconv.Itoa(cycle) }

func TestMain(m *testing.M) {
	if os.Getenv(crashChildEnv) != "" {
		crashChild()
		return
	}
	os.Exit(m.Run())
}

// crashChild runs inside the doomed process. It never returns: the parent kills it.
//
// Its exit codes below 100 are all failures the parent reports, because a child that exits by
// itself has not been interrupted and the cycle proved nothing.
func crashChild() {
	root := os.Getenv(crashRootEnv)
	cycle, _ := strconv.Atoi(os.Getenv(crashCycleEnv))

	s, err := Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "child: Open(%s): %v\n", root, err)
		os.Exit(11)
	}
	// Everything completed in an earlier cycle must still be here, read from a process that has
	// just started — no caches, no benefit of the doubt.
	for earlier := 0; earlier < cycle; earlier++ {
		rec, err := s.Get(crashKind, completeID(earlier))
		if err != nil || string(rec.Data) != completeBody(earlier) {
			fmt.Fprintf(os.Stderr, "child: record from cycle %d is gone or wrong: %v\n", earlier, err)
			os.Exit(12)
		}
	}
	if err := s.Put(Record{Kind: crashKind, ID: completeID(cycle), Data: []byte(completeBody(cycle))}); err != nil {
		fmt.Fprintf(os.Stderr, "child: Put: %v\n", err)
		os.Exit(13)
	}

	fmt.Fprintln(os.Stdout, "READY")
	os.Stdout.Sync()

	// A write with a middle to be killed in. The reader hands over a chunk at a time and then
	// blocks forever, so the temporary file on disk is genuinely half a record when the parent
	// pulls the plug.
	err = s.PutStream(crashKind, partialID(cycle), &trickleReader{chunks: 4096, pause: 20 * time.Millisecond})
	fmt.Fprintf(os.Stderr, "child: the interrupted write RETURNED (%v) — it was supposed to be killed\n", err)
	os.Exit(14)
}

// trickleReader yields a chunk, waits, yields another, and after enough of them blocks forever.
type trickleReader struct {
	chunks int
	pause  time.Duration
	n      int
}

func (r *trickleReader) Read(p []byte) (int, error) {
	if r.n >= r.chunks {
		select {} // The plug is pulled here.
	}
	r.n++
	time.Sleep(r.pause)
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestKilledMidWriteLeavesEveryCompleteRecordIntact drives criteria 10, 11 and 12.
func TestKilledMidWriteLeavesEveryCompleteRecordIntact(t *testing.T) {
	if testing.Short() {
		t.Skip("this test kills subprocesses and takes seconds")
	}
	root := filepath.Join(t.TempDir(), "store")
	if _, err := Create(root); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const cycles = 3 // criterion 12 asks for N > 1
	interruptedMidWrite := 0

	for cycle := 0; cycle < cycles; cycle++ {
		if killMidWrite(t, root, cycle) {
			interruptedMidWrite++
		}

		s, err := Open(root)
		if err != nil {
			t.Fatalf("cycle %d: reopening the store after the kill = %v — criterion 10 requires it opens", cycle, err)
		}

		// CRITERION 10 and 12: every record complete before any interruption is listed.
		recs, err := s.List(crashKind)
		if err != nil {
			t.Fatalf("cycle %d: List after the kill = %v — a store interrupted mid-write must still list", cycle, err)
		}
		listed := map[string]string{}
		for _, r := range recs {
			listed[r.ID] = string(r.Data)
		}
		for earlier := 0; earlier <= cycle; earlier++ {
			body, ok := listed[completeID(earlier)]
			if !ok {
				t.Fatalf("cycle %d: the record completed in cycle %d is missing after the kill; listed: %v", cycle, earlier, keysOf(listed))
			}
			if body != completeBody(earlier) {
				t.Fatalf("cycle %d: the record from cycle %d came back as %q, not what was written", cycle, earlier, body)
			}
		}

		// CRITERION 11: the interrupted record is ABSENT. Not short, not empty, not present with a
		// missing field — absent.
		for earlier := 0; earlier <= cycle; earlier++ {
			id := partialID(earlier)
			if body, ok := listed[id]; ok {
				t.Fatalf("cycle %d: the interrupted write is readable as a record (%d bytes: %.60q) — a record must be absent or complete, never partial",
					cycle, len(body), body)
			}
			if _, err := s.Get(crashKind, id); !errors.Is(err, ErrRecordNotFound) {
				t.Fatalf("cycle %d: Get(%s) after an interrupted write = %v; want ErrRecordNotFound — an interrupted write must leave nothing readable", cycle, id, err)
			}
		}
		if len(listed) != cycle+1 {
			t.Fatalf("cycle %d: the store lists %d records; want %d — nothing but the completed records may appear", cycle, len(listed), cycle+1)
		}
	}

	// THE TEST'S OWN HONESTY CHECK. If no cycle was killed while a partial file was on disk then
	// nothing above was tested, and a green run would mean nothing.
	if interruptedMidWrite == 0 {
		t.Fatalf("no cycle was killed with a partial write on disk, so crash safety was never exercised — this test proved nothing and must not pass")
	}
	t.Logf("%d of %d cycles were killed with a partial write on disk", interruptedMidWrite, cycles)
}

// killMidWrite runs one doomed child and SIGKILLs it while it is writing. It reports whether a
// partial write was observed on disk at the moment of the kill.
func killMidWrite(t *testing.T, root string, cycle int) bool {
	t.Helper()

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		crashChildEnv+"=1",
		crashRootEnv+"="+root,
		crashCycleEnv+"="+strconv.Itoa(cycle),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var childErr strings.Builder
	cmd.Stderr = &childErr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the doomed child: %v", err)
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
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("cycle %d: the child never became ready; stderr:\n%s", cycle, childErr.String())
	}

	// Let the interrupted write get properly under way, then look: is there half a record on disk?
	time.Sleep(400 * time.Millisecond)
	partialOnDisk := hasPartialWriteOnDisk(t, root)

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cycle %d: killing the child: %v", cycle, err)
	}
	state, err := cmd.Process.Wait()
	if err != nil {
		t.Fatalf("cycle %d: waiting for the killed child: %v", cycle, err)
	}
	if state.Exited() {
		// It exited under its own power, so it was not interrupted — and the child's exit codes say
		// which of its own assertions failed.
		t.Fatalf("cycle %d: the child exited with code %d instead of being killed mid-write; stderr:\n%s",
			cycle, state.ExitCode(), childErr.String())
	}
	return partialOnDisk
}

// hasPartialWriteOnDisk reports whether an unfinished write is visible in the store right now.
//
// It also asserts the shape of the guarantee: an unfinished write must be a TEMPORARY, sitting
// beside the record it will become, and never the record's own path. A build that wrote straight to
// the destination would be caught here as well as by the reopen above.
func hasPartialWriteOnDisk(t *testing.T, root string) bool {
	t.Helper()
	dir := filepath.Join(root, recordsDir, string(crashKind))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	found := false
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, tempPrefix) {
			if info, ierr := e.Info(); ierr == nil && info.Size() > 0 {
				found = true
			}
			continue
		}
		if isRecordFile(name) {
			// A completed-record path that holds an unfinished write is the defect itself.
			body, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil {
				continue
			}
			if _, derr := decodeRecord(name, body); derr != nil {
				t.Fatalf("%s is at a record's own path and does not decode — a write is happening in place, so a record can be read half-written", name)
			}
		}
	}
	return found
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
