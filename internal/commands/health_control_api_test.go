package commands

import (
	"bytes"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// TestHealthIsUnaffectedByTheControlAPIDeclining is Issue #1's criterion 14, carried forward into
// Issue #2 and DRIVEN here rather than inherited.
//
// #1 closed with that criterion recorded as "met by construction, never observed": no control API
// existed, so nothing could refuse to open and nothing could be driven. Issue #2 is the first place
// a refusal exists, and its instruction is explicit — drive this rather than inheriting #1's pass.
//
// It could not be driven when this branch was cut: `omw health` did not exist yet and landed on
// main afterwards. It is drivable now only because main is merged in. The assertion is the
// criterion word for word: with the control API declining, health's output is IDENTICAL to a run
// where the control API opened normally, and its exit code is unaffected.
//
// The refusal is produced through the injected confirmation seam, NOT by naming a platform on which
// owner-only permissions happen to be unconfirmable — a test that skips unless it is on such a
// platform never runs on either platform this product ships for.
func TestHealthIsUnaffectedByTheControlAPIDeclining(t *testing.T) {
	runHealth := func(getenv func(string) string) (int, string, string) {
		var out, errb bytes.Buffer
		code := cli.Run([]string{"health"}, &out, &errb, getenv)
		return code, out.String(), errb.String()
	}

	// A daemon whose control API opened normally — the baseline the criterion compares against.
	normalRoot, normalEnv := daemonTestStore(t)
	normal, err := daemon.Start(daemon.Options{
		StorePath: normalRoot, Interval: 5 * time.Millisecond,
		Write: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("the baseline daemon did not start: %v", err)
	}
	defer normal.Close()
	if normal.Control() == nil {
		_, why := normal.ControlState()
		t.Skipf("the control API did not open in this environment, so there is no 'opened normally' "+
			"baseline to compare against: %s", why)
	}
	codeOpen, outOpen, errOpen := runHealth(normalEnv)

	// A daemon that declined to open its control API, for the reason §4.6 names.
	refusedRoot, refusedEnv := daemonTestStore(t)
	refused, err := daemon.Start(daemon.Options{
		StorePath: refusedRoot, Interval: 5 * time.Millisecond,
		Write:            func() error { return nil },
		ConfirmOwnerOnly: func(string) (tri.Value, string) { return tri.Undetermined, "the owner of the socket could not be read" },
	})
	if err != nil {
		t.Fatalf("a daemon whose control API declines should still start: %v", err)
	}
	defer refused.Close()
	if refused.Control() != nil {
		t.Fatal("the control API opened despite an unconfirmable socket, so this test is not " +
			"exercising the state criterion 14 is about")
	}
	codeDeclined, outDeclined, errDeclined := runHealth(refusedEnv)

	if codeOpen != codeDeclined {
		t.Errorf("health exits %d with the control API open and %d with it declined; criterion 14 "+
			"says the exit code is unaffected", codeOpen, codeDeclined)
	}
	if outOpen != outDeclined {
		t.Errorf("health's output differs when the control API declines.\nopen:\n%s\ndeclined:\n%s", outOpen, outDeclined)
	}
	if errOpen != errDeclined {
		t.Errorf("health's stderr differs when the control API declines.\nopen:\n%s\ndeclined:\n%s", errOpen, errDeclined)
	}
	// A CONTROL. Two empty outputs are identical, and would satisfy every assertion above while
	// proving that health said nothing in both runs.
	if outOpen == "" {
		t.Error("health reported nothing at all, so the comparison above established nothing")
	}
}
