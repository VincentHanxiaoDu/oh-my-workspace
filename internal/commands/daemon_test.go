package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// daemonTestStore makes a real store and an environment pointing at it.
func daemonTestStore(t *testing.T) (root string, getenv func(string) string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("could not create a store to test against: %v", err)
	}
	env := map[string]string{store.PathEnv: root}
	return root, func(k string) string { return env[k] }
}

// runOMW drives a command in-process and gives back what a person would see.
func runOMW(t *testing.T, getenv func(string) string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(args, &out, &errb, getenv)
	return code, out.String(), errb.String()
}

// TestStatusOfANeverRunDaemonSaysSoAndStartsNothing is criteria 9, 11, 13 and 18 at the CLI.
func TestStatusOfANeverRunDaemonSaysSoAndStartsNothing(t *testing.T) {
	root, getenv := daemonTestStore(t)

	code, out, errOut := runOMW(t, getenv, "daemon", "status")
	if code != cli.Success {
		t.Errorf("`omw daemon status` against a store whose daemon never ran exited %d; answering is a success.\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, daemon.EndingNeverRun.String()) {
		t.Errorf("status did not report the last run as never run:\n%s", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("status did not say the daemon is not running:\n%s", out)
	}
	if rep := daemon.Inspect(root); rep.Running != tri.No {
		t.Errorf("asking for status left a daemon running (%v); PRD §4.2 says no command starts it", rep.Running)
	}
}

// TestNoRegisteredCommandStartsTheDaemon is criterion 18, stated over EVERY command this build has
// rather than over the ones this file happened to think of.
//
// A command added by another Issue is covered the moment it registers itself, which is the only
// way a rule about "no command in the product" can stay true as the product grows.
func TestNoRegisteredCommandStartsTheDaemon(t *testing.T) {
	for _, c := range cli.Commands() {
		root, getenv := daemonTestStore(t)
		for _, args := range [][]string{
			{c.Name},
			{c.Name, "status"},
			{c.Name, "path"},
		} {
			var out, errb bytes.Buffer
			_ = cli.Run(args, &out, &errb, getenv)
			if rep := daemon.Inspect(root); rep.Running != tri.No {
				t.Errorf("`omw %s` left the daemon %v against %s; no command starts it on a person's behalf",
					strings.Join(args, " "), rep.Running, root)
			}
		}
	}
}

// TestStoppingADaemonThatIsNotRunningIsItsOwnAnswer is criterion 4's first half. The other half —
// that it does not read the same as a stop that terminated a running daemon — is asserted in
// TestStartStopAndStatusThroughTheRealBinary, where a real stop exists to compare against.
func TestStoppingADaemonThatIsNotRunningIsItsOwnAnswer(t *testing.T) {
	_, getenv := daemonTestStore(t)
	code, out, errOut := runOMW(t, getenv, "daemon", "stop")
	if code != cli.Success {
		t.Errorf("stopping a daemon that is not running exited %d; it answered, so it succeeded.\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "not running") || !strings.Contains(out, "nothing to stop") {
		t.Errorf("a stop with nothing to stop did not say so:\n%s", out)
	}
}

// TestStartingAgainstAMissingStoreNamesTheStoreAndCreatesNothing is criterion 3 at the CLI.
func TestStartingAgainstAMissingStoreNamesTheStoreAndCreatesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-store-here")
	getenv := func(k string) string {
		if k == store.PathEnv {
			return missing
		}
		return ""
	}
	code, out, errOut := runOMW(t, getenv, "daemon", "start")
	if code == cli.Success {
		t.Fatalf("starting against a missing store succeeded; criterion 3 requires a non-zero exit.\n%s%s", out, errOut)
	}
	if !strings.Contains(errOut, missing) {
		t.Errorf("the failure does not name the store as the missing thing:\n%s", errOut)
	}
	if !strings.Contains(errOut, store.ErrNotFound.Error()) {
		t.Errorf("the failure does not use the store's own wording for 'no store here', so this and `omw store` say the same fact differently:\n%s", errOut)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("something was created at %s; §4.2 says the store is created explicitly and by nothing else", missing)
	}
}

// buildOMW builds the real binary, or says precisely why it could not and skips.
//
// PROBED, NOT NAMED. The suite must run where a Go toolchain is not on PATH — some CI images run
// tests from a prebuilt cache — so this asks whether `go` is there rather than assuming.
func buildOMW(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no `go` on PATH, so the real binary cannot be built and the start/stop criteria cannot be driven here: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	cmd := exec.Command(goBin, "build", "-o", bin, "github.com/VincentHanxiaoDu/oh-my-workspace/cmd/omw")
	cmd.Dir = moduleRootForDaemonTests(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the real binary failed, which is a build failure and not a finding about the daemon:\n%s", out)
	}
	return bin
}

func moduleRootForDaemonTests(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find the module root from the test's working directory")
		}
		dir = parent
	}
}

type runResult struct {
	code   int
	stdout string
	stderr string
}

func runBinary(t *testing.T, bin, storePath string, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), store.PathEnv+"="+storePath)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("could not run %s %v: %v", bin, args, err)
	}
	return runResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// TestStartStopAndStatusThroughTheRealBinary is criteria 1, 2, 4, 5, 6 and 13, driven the way a
// person drives them: separate processes, an explicit start, an explicit stop.
//
// IN-PROCESS TESTS CANNOT REACH THESE. Criterion 1 says the daemon is running after the start
// command RETURNS, which is a statement about two processes; criterion 5's "leaves the first
// daemon running and unaffected" needs a first daemon that is not the test binary itself.
func TestStartStopAndStatusThroughTheRealBinary(t *testing.T) {
	bin := buildOMW(t)
	root, _ := daemonTestStore(t)

	// CRITERION 1. After start returns successfully, it is running.
	start := runBinary(t, bin, root, "daemon", "start")
	if start.code != 0 {
		t.Fatalf("`omw daemon start` exited %d\nstdout: %s\nstderr: %s", start.code, start.stdout, start.stderr)
	}
	t.Cleanup(func() { runBinary(t, bin, root, "daemon", "stop") })
	if rep := daemon.Inspect(root); rep.Running != tri.Yes {
		t.Fatalf("after `start` returned successfully the daemon reports %v", rep.Running)
	}

	status := runBinary(t, bin, root, "daemon", "status")
	if !strings.Contains(status.stdout, "running") {
		t.Errorf("status does not report the running daemon:\n%s", status.stdout)
	}

	// CRITERIA 5 AND 6. A second start is refused, names the conflict, and leaves the first alone.
	second := runBinary(t, bin, root, "daemon", "start")
	if second.code == 0 {
		t.Errorf("a second daemon against the same store started; PRD §2.1 says one daemon per store")
	}
	if !strings.Contains(second.stderr, daemon.ErrLockHeld.Error()) {
		t.Errorf("the refusal does not name the lock conflict:\n%s", second.stderr)
	}
	if strings.Contains(second.stderr, store.ErrNotFound.Error()) {
		t.Errorf("the lock conflict reads like a missing store; criterion 6 requires them told apart:\n%s", second.stderr)
	}
	if rep := daemon.Inspect(root); rep.Running != tri.Yes || rep.Healthy != tri.Yes {
		t.Errorf("the first daemon was affected by the refused second one: running=%v healthy=%v", rep.Running, rep.Healthy)
	}

	// CRITERION 2. An explicit stop, after which the lock is free and a start succeeds again.
	stop := runBinary(t, bin, root, "daemon", "stop")
	if stop.code != 0 {
		t.Fatalf("`omw daemon stop` exited %d\nstdout: %s\nstderr: %s", stop.code, stop.stdout, stop.stderr)
	}
	if rep := daemon.Inspect(root); rep.Running != tri.No {
		t.Fatalf("after `stop` returned the daemon reports %v", rep.Running)
	}
	if rep := daemon.Inspect(root); rep.LastRun != daemon.EndingStopped {
		t.Errorf("after an explicit stop the last run reads as %q; expected %q", rep.LastRun, daemon.EndingStopped)
	}

	// CRITERION 4. The two stops must not produce identical output.
	stopAgain := runBinary(t, bin, root, "daemon", "stop")
	if stopAgain.stdout == stop.stdout {
		t.Errorf("stopping a running daemon and stopping nothing produce identical output:\n%s", stop.stdout)
	}
	if !strings.Contains(stopAgain.stdout, "nothing to stop") {
		t.Errorf("the second stop does not say there was nothing to stop:\n%s", stopAgain.stdout)
	}

	// CRITERION 2's last clause: a subsequent start against the same store succeeds.
	restart := runBinary(t, bin, root, "daemon", "start")
	if restart.code != 0 {
		t.Fatalf("a start after a clean stop failed (%d):\n%s%s", restart.code, restart.stdout, restart.stderr)
	}
	runBinary(t, bin, root, "daemon", "stop")
}

// TestACrashedDaemonIsReportedAsCrashedByTheRealBinary is criterion 10's third rendering, driven
// by actually killing a daemon rather than by writing the record a crash would leave.
func TestACrashedDaemonIsReportedAsCrashedByTheRealBinary(t *testing.T) {
	bin := buildOMW(t)
	root, _ := daemonTestStore(t)

	if r := runBinary(t, bin, root, "daemon", "start"); r.code != 0 {
		t.Fatalf("start failed: %s%s", r.stdout, r.stderr)
	}
	rep := daemon.Inspect(root)
	if rep.PID <= 0 {
		t.Fatalf("the running daemon's pid is not known, so it cannot be killed to produce a crash")
	}
	proc, err := os.FindProcess(rep.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("could not kill the daemon to simulate a crash: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && daemon.Inspect(root).Running != tri.No {
		time.Sleep(20 * time.Millisecond)
	}

	after := runBinary(t, bin, root, "daemon", "status")
	if !strings.Contains(after.stdout, daemon.EndingCrashed.String()) {
		t.Errorf("a killed daemon does not come back reporting that it ended without recording an ending:\n%s", after.stdout)
	}
	if strings.Contains(after.stdout, daemon.EndingStopped.String()) {
		t.Errorf("a killed daemon reports an explicit stop; the two must not collapse:\n%s", after.stdout)
	}
	if strings.Contains(after.stdout, daemon.EndingNeverRun.String()) {
		t.Errorf("a killed daemon reports as never having run:\n%s", after.stdout)
	}
}

// TestNothingReachesTheNetworkWithNoHubConfigured is criteria 19 and 20's local half.
//
// HONESTLY LABELLED: this establishes that the daemon's whole lifecycle completes with no hub in
// the environment and that nothing in it names a network transport, which is a source-level
// property (see the daemon package's TestNoNetworkTransportExistsInThisPackage). It does NOT
// observe the machine's sockets, so "zero outbound connections" is argued rather than measured.
func TestNothingReachesTheNetworkWithNoHubConfigured(t *testing.T) {
	bin := buildOMW(t)
	root, _ := daemonTestStore(t)

	// The environment carries no hub of any kind: runBinary passes only the store's path on top of
	// the inherited environment, and nothing in this build reads a hub setting at all.
	for _, step := range [][]string{
		{"daemon", "start"},
		{"daemon", "status"},
		{"daemon", "stop"},
	} {
		r := runBinary(t, bin, root, step...)
		if r.code != 0 && r.code != cli.Success {
			t.Errorf("`omw %s` with no hub configured exited %d; criterion 20 says each capability completes or names what is missing\nstdout: %s\nstderr: %s",
				strings.Join(step, " "), r.code, r.stdout, r.stderr)
		}
		if strings.TrimSpace(r.stdout)+strings.TrimSpace(r.stderr) == "" {
			t.Errorf("`omw %s` said nothing at all; a capability that half-works silently is what criterion 20 forbids", strings.Join(step, " "))
		}
	}
}
