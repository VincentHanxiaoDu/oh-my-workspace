package daemon

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// A STORE THAT COULD NOT BE READ AND A STORE WITH NO MODEL IN IT MUST NOT READ THE SAME (Issue #68).
//
// This is the assertion Issue #68 was filed with, in the shape it was filed in: a CONTROL and a
// TEST, driven through the surface a person actually consumes — [Inspect], the on-disk resolution
// behind `omw daemon status` — and compared as the bytes a reader sees rather than as struct
// fields. Without the control the whole thing passes vacuously, because "the unreadable arm says
// something" is true of the bug too.
//
// BOTH ARMS ARE A REAL STORE, made with store.Create. A bare directory is not a weaker version of
// the same setup: store.Open rejects it with store.ErrNotFound, which is the DETERMINED "there is
// no store here" answer and takes the same branch as the control. Comparing that against the
// control compares two spellings of "no store" and proves nothing about unreadability.
func TestAnUnreadableStoreDoesNotRenderAsAStoreWithNoModel(t *testing.T) {
	// The environment is emptied so that neither arm's answer can come from it. What is under test
	// is what the STORE contributes, and a provider in the environment would give both arms a
	// determined Yes and hide the collapse.
	t.Setenv(model.EnvProvider, "")
	t.Setenv(model.EnvCredential, "")
	t.Setenv(model.EnvCredentialFile, "")

	// CONTROL: a readable store with no model recorded. A determined negative here is CORRECT, and
	// it is the sentence the bug wrongly reused for the arm below.
	readable := newTestStore(t)
	control := Inspect(readable).Model
	if control.Chosen() != tri.No {
		t.Fatalf("the control is not the case it is meant to be: a readable store with no model should be a determined no, got %+v", control)
	}
	if !strings.Contains(control.Render(), "no provider is chosen") {
		t.Fatalf("the control does not render the determined negative this test is about: %q", control.Render())
	}

	// TEST: the same shape of store, made unreadable.
	unreadable := newTestStore(t)
	makeUnreadable(t, unreadable)
	got := Inspect(unreadable).Model

	if got.Render() == control.Render() {
		t.Errorf("a store that could not be read renders byte for byte as a store with no model in it:\n  %s", got.Render())
	}
	if got.Chosen() != tri.Undetermined {
		t.Errorf("a store that could not be read reports which provider is chosen as %v, not %v:\n  %+v",
			got.Chosen(), tri.Undetermined, got)
	}
	if got.Present() != tri.Undetermined {
		t.Errorf("a store that could not be read reports whether a credential is present as %v, not %v:\n  %+v",
			got.Present(), tri.Undetermined, got)
	}
	// The reason is said out loud rather than left as a bare "undetermined", and it is the CLI's
	// own sentence — `omw model show` prints this one, so the two surfaces cannot drift.
	if !strings.Contains(got.Detail, "could not be read") ||
		!strings.Contains(got.Detail, "An unreadable store is not one with no model recorded in it.") {
		t.Errorf("the undetermined answer does not carry the CLI's reason: %q", got.Detail)
	}
}

// The rendering a person reads out of the full report differs too, and not only the View — the
// `model:` line is the line Issue #68 says a reader will quote.
func TestTheRenderedReportDistinguishesAnUnreadableStore(t *testing.T) {
	t.Setenv(model.EnvProvider, "")
	t.Setenv(model.EnvCredential, "")
	t.Setenv(model.EnvCredentialFile, "")

	readable := newTestStore(t)
	unreadable := newTestStore(t)
	makeUnreadable(t, unreadable)

	control := modelLinesOf(t, Inspect(readable))
	got := modelLinesOf(t, Inspect(unreadable))

	if control == "" {
		t.Fatal("the control report has no model block, so the comparison below proves nothing")
	}
	if got == control {
		t.Errorf("the `model:` block is identical for a readable store with no model and a store that could not be read:\n%s", got)
	}
}

// modelLinesOf renders a Report and returns its model block: the `model:` line and the indented
// lines under it.
func modelLinesOf(t *testing.T, rep Report) string {
	t.Helper()
	var out strings.Builder
	if _, err := rep.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	var block []string
	in := false
	for _, line := range strings.Split(out.String(), "\n") {
		switch {
		case strings.HasPrefix(line, "model:"):
			in = true
			block = append(block, line)
		case in && strings.HasPrefix(line, " "):
			block = append(block, line)
		case in:
			return strings.Join(block, "\n")
		}
	}
	return strings.Join(block, "\n")
}

// makeUnreadable is the `chmod 000` of Issue #68's reproduction.
//
// It refuses to pretend on a platform where it did not work: root ignores the mode bits, and
// Windows does not have them at all, so rather than run the assertions against a store that is
// still perfectly readable — which would pass by testing nothing — the test skips and says why.
func makeUnreadable(t *testing.T, root string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mode bits do not make a directory unreadable on windows, so there is no unreadable store to test")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the mode bits, so the store would still be readable")
	}
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatalf("could not make the store unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	// THE ARRANGEMENT IS CONFIRMED, NOT ASSUMED. If store.Open still succeeds, or fails with
	// ErrNotFound, this is not the case the test means to drive.
	if _, err := store.Open(root); err == nil {
		t.Skipf("the store at %s is still readable after chmod 000, so there is nothing unreadable to test", root)
	} else if errors.Is(err, store.ErrNotFound) {
		t.Skipf("chmod 000 made the store look absent rather than unreadable here (%v)", err)
	}
}
