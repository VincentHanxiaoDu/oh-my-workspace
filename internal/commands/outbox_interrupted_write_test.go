package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Issue #69: an interrupted write left a DESTROYED draft reporting itself as a HEALTHY one.
//
// WHY THIS TEST KILLS A REAL PROCESS AND DOES NOT SIMULATE ONE.
//
// The defect was not that a reader mishandled a shape somebody typed into a directory. It was that
// the WRITER made a shape the reader was documented to believe could not exist — `StateOf` mapped a
// missing `.state` to `drafted`, and that mapping was sound only while a draft directory could not
// be visible before its state file. `Revise` made the directory first. So the test has to produce
// the damaged state THE WAY THE PRODUCT PRODUCES IT: run the real `omw` binary, and SIGKILL it
// inside the write.
//
// The kill is aimed, not timed. The parent spins on the draft's directory and pulls the plug the
// instant it appears — which, before the fix, is the instant the half-written draft becomes
// visible, and after it is an instant when the draft is already whole. A wall-clock delay would
// have made this a flake; the directory's own appearance is the event that matters.
//
// AND IT ASSERTS THE KILL LANDED. A build with no crash safety could pass a badly written version
// of this test by being killed before it touched the store. So the run fails if it never once
// killed a process with the draft's directory on disk: that is an undetermined result, not a pass.

// obKillBin builds the real binary these tests drive.
func obKillBin(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("this test builds the binary and kills processes")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	build := exec.Command(goTool, "build", "-o", bin, "./cmd/omw")
	build.Dir = repoRoot(t)
	if out, berr := build.CombinedOutput(); berr != nil {
		t.Fatalf("building omw: %v\n%s", berr, out)
	}
	return bin
}

// obKillWorld is a sandboxed machine with a store, driven only through the real binary.
type obKillWorld struct {
	bin  string
	root string
	env  []string
}

func newObKillWorld(t *testing.T) obKillWorld {
	t.Helper()
	w := obKillWorld{bin: obKillBin(t), root: filepath.Join(t.TempDir(), "store")}
	sandbox := t.TempDir()
	w.env = append(os.Environ(),
		store.PathEnv+"="+w.root, "OMW_HUB=", "OMW_MODEL=", "OMW_MODEL_KEY=",
		"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
	)
	if code, out := w.run("store", "create"); code != 0 {
		t.Fatalf("omw store create exited %d:\n%s", code, out)
	}
	return w
}

func (w obKillWorld) run(args ...string) (int, string) {
	cmd := exec.Command(w.bin, args...)
	cmd.Env = w.env
	out, _ := cmd.CombinedOutput()
	return cmd.ProcessState.ExitCode(), string(out)
}

func (w obKillWorld) draftDir(id string) string {
	return filepath.Join(w.root, drafts.OutboxDirName, id)
}

// killDuringDraft starts a draft and SIGKILLs it the moment its directory appears on disk. It
// reports whether the kill landed with the directory present — the only case that proves the
// process was interrupted after it had begun to make the draft visible.
func (w obKillWorld) killDuringDraft(id, body string) (interrupted bool) {
	cmd := exec.Command(w.bin, "outbox", "draft", id, body)
	cmd.Env = w.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return false
	}
	dir := w.draftDir(id)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir); err == nil {
			interrupted = true
			break
		}
		if cmd.ProcessState != nil {
			break
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return interrupted
}

// TestADraftInterruptedMidWriteNeverReportsItselfHealthy is Issue #69 criteria 1, 2 and 3.
func TestADraftInterruptedMidWriteNeverReportsItselfHealthy(t *testing.T) {
	w := newObKillWorld(t)
	const rounds = 60
	const body = "WHOLE-DRAFT-the-entire-text-and-nothing-missing"

	landed := 0
	for i := 0; i < rounds; i++ {
		if w.killDuringDraft(fmt.Sprintf("d%03d", i), body) {
			landed++
		}
	}
	if landed == 0 {
		t.Fatalf("no draft directory was ever observed while its writer was alive, so nothing here was "+
			"interrupted and this run has determined NOTHING about crash safety (%d rounds)", rounds)
	}
	t.Logf("%d of %d writes were killed with the draft's directory already on disk", landed, rounds)

	// A SECOND PROCESS READS EVERY DRAFT. This is the surface a person uses.
	lied := 0
	for i := 0; i < rounds; i++ {
		id := fmt.Sprintf("d%03d", i)
		dir := w.draftDir(id)
		if _, err := os.Stat(dir); err != nil {
			continue // Nothing became visible. That is the honest outcome of an interrupted write.
		}
		code, out := w.run("outbox", "state", id)
		healthy := code == 0 && strings.Contains(out, "state: "+string(drafts.StateDrafted))
		if !healthy {
			continue
		}
		// It says it is fine. Then it had better BE fine, read from disk.
		got, err := os.ReadFile(filepath.Join(dir, "000001.body"))
		if err != nil || string(got) != body {
			lied++
			t.Errorf("%s reports itself HEALTHY and is DESTROYED: %q exit 0, but its first revision is %q (%v)",
				id, strings.TrimSpace(out), string(got), err)
		}
	}
	if lied == 0 {
		t.Logf("no draft reported itself healthy while damaged")
	}
}

// TestADraftDirectoryIsNeverVisibleBeforeItsStateFile is criterion 4 asserted directly.
//
// The comment in state.go says a draft with no state file is `drafted` because every draft that
// exists is somewhere. That reasoning holds only while this invariant does, and until #69 nothing
// in the tree checked it — the comment was false and the build was green.
func TestADraftDirectoryIsNeverVisibleBeforeItsStateFile(t *testing.T) {
	w := newObKillWorld(t)
	const rounds = 60
	for i := 0; i < rounds; i++ {
		w.killDuringDraft(fmt.Sprintf("k%03d", i), "some words a person typed")
	}
	entries, err := os.ReadDir(filepath.Join(w.root, drafts.OutboxDirName))
	if err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}
	seen := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		seen++
		st := filepath.Join(w.root, drafts.OutboxDirName, e.Name(), ".state")
		if _, serr := os.Stat(st); serr != nil {
			t.Errorf("draft %q is visible in the outbox and has no state file: %v", e.Name(), serr)
		}
	}
	t.Logf("%d draft directories became visible across %d interrupted writes", seen, rounds)
}

// TestADamagedDraftIsUndeterminedAndAnAbsentOneIsNot is criteria 2 and 3.
//
// The three shapes are the leftovers Issue #69 measured on disk, put there directly so that each
// is exercised on every run rather than whenever the timing happens to produce it. `d008` — an
// entirely empty directory — is the one that used to say `drafted` and exit 0.
//
// AND THE EXIT CODES ARE ASSERTED APART. "This draft is damaged and I could not determine where it
// stands" and "there is no such draft" are different answers, and a script that cannot tell them
// apart will delete work on the strength of the wrong one.
func TestADamagedDraftIsUndeterminedAndAnAbsentOneIsNot(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "draft", "healthy", "worth keeping")
	outbox := filepath.Join(obStorePath(t, env), drafts.OutboxDirName)

	damaged := map[string]func(dir string) error{
		// The directory exists and nothing is in it: the write was killed after the mkdir.
		"empty-directory": func(dir string) error { return nil },
		// A body landed and the state record never did.
		"body-without-state": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "000001.body"), []byte("half a thought"), 0o600)
		},
		// The state record is there and truncated to nothing.
		"truncated-state": func(dir string) error {
			if err := os.WriteFile(filepath.Join(dir, "000001.body"), []byte("words"), 0o600); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, ".state"), nil, 0o600)
		},
	}
	for id, damage := range damaged {
		dir := filepath.Join(outbox, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("staging the %s damage: %v", id, err)
		}
		if err := damage(dir); err != nil {
			t.Fatalf("staging the %s damage: %v", id, err)
		}
	}

	for id := range damaged {
		got := runOutboxCmd(t, env, "state", id)
		if got.code != cli.ExitUndetermined {
			t.Errorf("a %s draft exited %d, want %d (could not be determined)\n%s",
				id, got.code, cli.ExitUndetermined, got.all())
		}
		if strings.Contains(got.all(), "state: "+string(drafts.StateDrafted)) {
			t.Errorf("a %s draft reports itself HEALTHY:\n%s", id, got.all())
		}
	}

	// A draft that is not there is a DETERMINED answer, and it is not the same one.
	absent := runOutboxCmd(t, env, "state", "never-written")
	if absent.code == cli.ExitUndetermined {
		t.Errorf("an absent draft exits %d, the same code as one that could not be determined:\n%s",
			absent.code, absent.all())
	}
	if absent.code == cli.Success {
		t.Errorf("an absent draft exits 0:\n%s", absent.all())
	}

	// And listing an outbox holding damaged drafts is non-zero, as `d026` and `d030` already were.
	if got := runOutboxCmd(t, env, "list"); got.code != cli.ExitUndetermined {
		t.Errorf("listing an outbox with damaged drafts exited %d, want %d\n%s",
			got.code, cli.ExitUndetermined, got.all())
	}
	// The intact one is still intact and still reads as drafted.
	if got := runOutboxCmd(t, env, "state", "healthy"); got.code != cli.Success ||
		!strings.Contains(got.all(), "state: "+string(drafts.StateDrafted)) {
		t.Errorf("the intact draft no longer reports drafted (exit %d):\n%s", got.code, got.all())
	}
}

// TestAnIntactDraftStillReportsDraftedAndExitsZero is criterion 3's control.
//
// Without it, a fix that reports every draft undetermined satisfies criterion 2 perfectly and
// destroys the outbox.
func TestAnIntactDraftStillReportsDraftedAndExitsZero(t *testing.T) {
	w := newObKillWorld(t)
	if code, out := w.run("outbox", "draft", "whole", "worth keeping"); code != 0 {
		t.Fatalf("omw outbox draft exited %d:\n%s", code, out)
	}
	code, out := w.run("outbox", "state", "whole")
	if code != 0 || !strings.Contains(out, "state: "+string(drafts.StateDrafted)) {
		t.Fatalf("an intact draft does not report drafted (exit %d):\n%s", code, out)
	}
	if code, out := w.run("outbox", "list"); code != 0 || !strings.Contains(out, "whole") {
		t.Fatalf("an intact draft is not listed (exit %d):\n%s", code, out)
	}
}
