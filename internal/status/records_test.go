// Issue #101, blocker 1, at the package that owns the judgement.
//
// The command-level drive lives in internal/commands and asserts what a person sees. This asserts
// the precedence that was already written here and was never reached: an undetermined member
// carries up through [worse] to the subsystem and through [Summarise] to the summary. It is in this
// package because a change here — and this package is edited by more than one Issue — should go red
// here rather than in whichever command notices first.
package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// TestAStoreHoldingAnUnreadableRecordIsNotAWorkingStore drives both directions over one store.
func TestAStoreHoldingAnUnreadableRecordIsNotAWorkingStore(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where chmod 000 does not deny a read")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.Record{Kind: store.Kind("ticket"), ID: "t-one", Data: []byte(`{"id":"t-one"}`)}); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(root, "records", "ticket", "t-one.rec")

	// BOTH XDG_DATA_HOME AND HOME ARE SANDBOXED, for the reason internal/commands' statusSandbox
	// records: anything falling back to the home directory would otherwise reach the developer's
	// real one, and an unset HOME leaves the devices line undetermined for a reason that has
	// nothing to do with what this test is about.
	env := map[string]string{store.PathEnv: root, "XDG_DATA_HOME": dir, "HOME": dir}
	getenv := func(k string) string { return env[k] }
	collect := func() Screen {
		// THE LIVENESS ANSWER IS AN INPUT to this package (Issue #41), and the zero tri.Value is
		// Undetermined — correct in general, and it would make this screen undetermined for a
		// reason that is not the one under test. A determined "no daemon" is what the command
		// hands over for a store nothing was started against.
		return Collect(Query{Getenv: getenv, Now: time.Unix(1700000000, 0).UTC(), Daemon: tri.No})
	}

	// ---- Control: readable. ------------------------------------------------
	before := collect()
	if got := stateOf(t, before, Store); got != Working {
		t.Fatalf("the control run reports the local store as %v rather than working; the comparison below would be vacuous", got)
	}
	if before.AnyUndetermined() {
		t.Fatalf("the control run already has something undetermined on it:\n%s", before.Render())
	}

	// ---- Test: the record, chmod 000, still there. -------------------------
	if err := os.Chmod(rec, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(rec, 0o600) })
	if _, err := os.ReadFile(rec); err == nil {
		t.Skip("the record is still readable at mode 000 on this filesystem, so the condition under test does not exist")
	}

	after := collect()
	if got := stateOf(t, after, Store); got != Undetermined {
		t.Errorf("the local store holding a record that could not be read reports as %v.\n%s", got, after.Render())
	}
	if !after.AnyUndetermined() {
		t.Errorf("nothing on the screen is undetermined, so `omw status` would exit 0 over a store it could not read:\n%s", after.Render())
	}
	if after.Summary == SummaryAllWorking || after.Summary == SummaryAllConfiguredWorking {
		t.Errorf("the summary is %q over a store holding an unreadable record", after.Summary)
	}
	if after.Render() == before.Render() {
		t.Errorf("the screen is identical to the all-readable control:\n%s", after.Render())
	}
}

func stateOf(t *testing.T, s Screen, name string) State {
	t.Helper()
	for _, sub := range s.Subsystems {
		if sub.Name == name {
			return sub.State
		}
	}
	t.Fatalf("the screen has no %q line at all:\n%s", name, s.Render())
	return Undetermined
}
