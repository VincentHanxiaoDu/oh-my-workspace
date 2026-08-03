package commands

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Issue #69, second finding: the stale-lock notice fired on every restart, so it said nothing.
//
// "a lock left behind by pid N was found and taken over" appeared on 10 of 10 crash recoveries AND
// on 5 of 5 clean stop-then-start cycles. A person could not tell the two apart, which is the only
// thing the sentence is for. `release()` unlocked and closed the file and left the previous
// holder's pid inside it, and `acquireLock` treats any pid it finds that is not its own as a
// leftover — so a lock deliberately handed back looked exactly like one abandoned by a corpse.
//
// # THE CLEAN DIRECTION IS THE TEST THAT MATTERS
//
// A notice that always fires passes every test that only checks it appears, which is why this
// shipped green. Both directions are asserted here in one function so that neither can be dropped
// on its own, and both are driven through the real binary: the defect lives in the handover
// between two processes and an in-process acquire never sees it — `acquireLock` compares against
// os.Getpid(), which in one process is the pid that wrote the body.

// staleNotice is the sentence under test, matched on the part that does not vary.
const staleNotice = "was found and taken over"

func TestTheStaleLockNoticeDistinguishesACrashFromACleanRestart(t *testing.T) {
	bin := buildOMW(t)
	root, _ := daemonTestStore(t)

	mustStart := func(what string) runResult {
		t.Helper()
		got := runBinary(t, bin, root, "daemon", "start")
		if got.code != 0 {
			t.Fatalf("%s: `omw daemon start` exited %d\n%s%s", what, got.code, got.stdout, got.stderr)
		}
		return got
	}
	mustStop := func() {
		t.Helper()
		if got := runBinary(t, bin, root, "daemon", "stop"); got.code != 0 {
			t.Fatalf("`omw daemon stop` exited %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	}
	pid := func() int {
		t.Helper()
		rep := daemon.Inspect(root)
		if rep.Running != tri.Yes || rep.PID == 0 {
			t.Fatalf("no daemon is running to kill: running=%v pid=%d", rep.Running, rep.PID)
		}
		return rep.PID
	}

	// The very first start on a store that has never held a lock.
	if got := mustStart("first start"); strings.Contains(got.all(), staleNotice) {
		t.Errorf("the first start on a fresh store reported a lock left behind:\n%s", got.all())
	}

	// A CLEAN STOP, THEN A START. This is the direction that was broken, and it is asserted twice
	// because the measured defect was 5 of 5 — one cycle passing by luck would prove nothing.
	for cycle := 1; cycle <= 2; cycle++ {
		mustStop()
		got := mustStart("clean restart")
		if strings.Contains(got.all(), staleNotice) {
			t.Errorf("clean restart %d: a daemon stopped on purpose left a notice that reads as crash "+
				"recovery, so the notice cannot tell the two apart:\n%s", cycle, got.all())
		}
	}

	// AN UNCLEAN EXIT. The notice must still fire — a fix that simply stops speaking is worse than
	// the defect, because then nothing distinguishes a crash either.
	killed := pid()
	if err := syscall.Kill(killed, syscall.SIGKILL); err != nil {
		t.Fatalf("killing the daemon (pid %d): %v", killed, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for daemon.Inspect(root).Running == tri.Yes && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	after := mustStart("after a crash")
	t.Cleanup(func() { runBinary(t, bin, root, "daemon", "stop") })
	if !strings.Contains(after.all(), staleNotice) {
		t.Errorf("a start after pid %d was SIGKILLed said nothing about the lock it took over:\n%s",
			killed, after.all())
	}
}

func (r runResult) all() string { return r.stdout + r.stderr }
