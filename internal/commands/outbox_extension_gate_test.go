package commands

import (
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// =================================================================================================
// CRITERION 10, IN THE STATE A PERSON IS MOST LIKELY TO BE IN — a broken extension AND no
// credential recorded.
//
// The criterion was already carried by `model.Readiness` and by the `outboxReviewer` wrap in
// `extension_cmd.go`, and both were driven by tests that passed. What neither reached is the review
// GATE: `outboxReviewGate` returned on `cfg.Configured() == tri.No` before any reviewer was built,
// so the person whose extension is broken and who has not yet recorded a credential — the ordinary
// way this goes wrong — was told "no model is configured" at the very moment `omw ext list` said
// FAILED TO LOAD, and was sent to fix a credential that would not have helped them.
//
// # WHY BOTH DIRECTIONS ARE HERE AND WHY NEITHER IS ENOUGH ALONE
//
// A build that always named the extension would pass the first test and be a new defect: a person
// with a WORKING extension and no credential must still be told `no-model`, because that is then
// the true fact about their machine. The pair is what pins the ordering `model.Readiness`
// documents — "the extension is consulted BEFORE the credential" — rather than pinning a preference
// for one sentence over the other.
//
// # THESE DRIVE THE COMMAND A PERSON TYPES
//
// `omw outbox review`, through `cli.Run`, on a real store, with the same registry `omw ext list`
// reads. A test that called `model.Readiness` directly would pass over the gate that is the whole
// defect — which is exactly how this reached UAT green.
// =================================================================================================

// obExtWorld is a machine in review mode with one draft, one registered model provider extension,
// and NO credential recorded anywhere.
//
// It returns the environment. `broken` decides the single fact under test: whether the registered
// extension loads. Nothing else differs between the two directions.
func obExtWorld(t *testing.T, provider string, broken error) map[string]string {
	t.Helper()
	env := obWorld(t)
	root := obStorePath(t, env)

	// ONE MACHINE, TWO SURFACES. This is the registry `omw ext list` reads, so the fact this test
	// asserts about `omw outbox review` is the same fact a person would see from `omw ext list`.
	xtRegistry(t, xtFake{name: provider, iface: extension.Model, err: broken})
	// The exit code is NOT asserted: registering an extension that will not load is a deliberate act
	// that succeeds and then reports the failed load, so it exits non-zero in the broken direction
	// and zero in the working one. What must hold in both is that the registration happened — an
	// unregistered provider is a different state with a different answer.
	if reg := runExtCmd(t, env, "register", provider); !strings.Contains(reg.all(), "registered: "+provider) {
		t.Fatalf("the provider extension was not registered, so this test would prove nothing:\n%s", reg.all())
	}

	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("opening the store this test drives against: %v", err)
	}
	// The person chose a provider — they configured what they were asked to configure. What they
	// have NOT done is record a credential, and `OMW_MODEL_KEY` is absent from this environment.
	if err := model.Use(s, provider); err != nil {
		t.Fatalf("recording the chosen provider: %v", err)
	}

	mustRun(t, env, "mode", "set", "review")
	// The draft itself refuses in review mode without a model, which is criterion 14 and not what
	// this test is about; the draft is written either way, and `review` is what is under test.
	if got := runOutboxCmd(t, env, "draft", "d1", "a draft"); got.stdout == "" {
		t.Fatalf("no draft was written, so a review of it would prove nothing:\n%s", got.all())
	}
	// CONTROL: the mode really is review. QA's own reproduction was defeated once by a draft still
	// in manual mode, where `review` says "not run" and proves nothing.
	if out := mustRun(t, env, "mode").all(); !strings.Contains(out, "mode: review") {
		t.Fatalf("control failed: the mode in effect is not review, so this test proves nothing:\n%s", out)
	}
	return env
}

// A BROKEN EXTENSION WITH NO CREDENTIAL NAMES THE EXTENSION FAILURE.
func TestReviewWithNoCredentialNamesTheBrokenExtensionRatherThanTheMissingModel(t *testing.T) {
	const boom = "libacme.so was built against a different interface"
	env := obExtWorld(t, "acme", loadErr(boom))

	got := runOutboxCmd(t, env, "review", "d1")
	all := got.all()

	if got.code == cli.Success {
		t.Fatalf("a broken extension let the review succeed:\n%s", all)
	}
	if !strings.Contains(all, model.ErrProviderFailedToLoad.Code) {
		t.Errorf("the extension failure is not named. `omw ext list` says this provider FAILED TO LOAD, "+
			"and this surface must say the same thing with the code %q:\n%s", model.ErrProviderFailedToLoad.Code, all)
	}
	if !strings.Contains(all, boom) {
		t.Errorf("the extension's own reason %q is not carried, so the person cannot act on it:\n%s", boom, all)
	}
	if strings.Contains(all, model.ErrNoModel.Code) {
		t.Errorf("this says %q — the sentence criterion 10 forbids — to a person whose extension is "+
			"broken. Recording a credential will not fix their machine:\n%s", model.ErrNoModel.Code, all)
	}
}

// THE SAME BROKEN EXTENSION WITH A CREDENTIAL RECORDED, THROUGH THE COMMAND A PERSON TYPES.
//
// This is the half `extension_cmd.go`'s wrap of `outboxReviewer` carries, and it had no test that
// reached it: neutering that wrap left `internal/commands` and `internal/model` fully green, so the
// only thing standing between this criterion and a silent regression was that nobody had edited the
// file. With the wrap disabled this goes red on `outboxReviewer`'s own fallback sentence — "this
// build has no adapter for the provider acme" — which is a fact about the BUILD offered where the
// fact about THIS MACHINE is that a registered extension is broken.
func TestReviewWithACredentialNamesTheBrokenExtensionRatherThanTheMissingAdapter(t *testing.T) {
	const boom = "libacme.so was built against a different interface"
	env := obExtWorld(t, "acme", loadErr(boom))
	env["OMW_MODEL_KEY"] = xtSecret // the person HAS finished configuring; their extension is still broken

	got := runOutboxCmd(t, env, "review", "d1")
	all := got.all()

	if got.code == cli.Success {
		t.Fatalf("a broken extension let the review succeed:\n%s", all)
	}
	if !strings.Contains(all, "FAILED TO LOAD") || !strings.Contains(all, boom) {
		t.Errorf("the registered extension's failure and its reason %q are not named:\n%s", boom, all)
	}
	if strings.Contains(all, "has no adapter for") {
		t.Errorf("this answers with a fact about the BUILD — true of every machine running this binary — "+
			"where the fact about THIS machine is a registered extension that failed to load:\n%s", all)
	}
	if strings.Contains(all, xtSecret) {
		t.Fatalf("the person's credential is in the output of this capability (criterion 22):\n%s", all)
	}
}

// A WORKING EXTENSION WITH NO CREDENTIAL STILL SAYS no-model, BECAUSE THAT IS THEN TRUE.
func TestReviewWithNoCredentialAndAWorkingExtensionStillSaysNoModel(t *testing.T) {
	env := obExtWorld(t, "acme", nil)

	got := runOutboxCmd(t, env, "review", "d1")
	all := got.all()

	if got.code == cli.Success {
		t.Fatalf("no credential is recorded and the review succeeded anyway:\n%s", all)
	}
	if !strings.Contains(all, model.ErrNoModel.Code) {
		t.Errorf("the extension loads and no credential is recorded, so %q is the true answer and it "+
			"is missing:\n%s", model.ErrNoModel.Code, all)
	}
	if strings.Contains(all, model.ErrProviderFailedToLoad.Code) {
		t.Errorf("this blames the extension on a machine whose extension is fine. That is the same "+
			"defect with the directions swapped:\n%s", all)
	}
}

// =================================================================================================
// THE SECOND FINDING, ON THE MODEL SIDE — a fact about THE BUILD where the fact about THIS MACHINE
// is a registered extension that will not load.
//
//	ext list       -> FAILED TO LOAD (interface mismatch, named)
//	omw model show -> "this build has no adapter for teams"
//
// The first sentence is about the person's machine; the second is true of every machine running
// this binary, and it sent them to look at the wrong thing. Commit f55a176 fixed exactly this on
// the CHANNEL side (`omw channels list` versus `omw ext list`); this is the model side, and
// `model.ViewOn` is the fix.
// =================================================================================================

func TestModelShowNamesTheFailedLoadRatherThanTheMissingAdapter(t *testing.T) {
	const boom = "libacme.so was built against a different interface"
	env := obExtWorld(t, "acme", loadErr(boom))

	// CONTROL, AND IT IS THE WHOLE POINT: the other surface's answer about this same machine. An
	// assertion about `omw model show` alone would pass on a build where BOTH surfaces had been
	// taught to say nothing useful.
	list := runExtCmd(t, env, "list")
	if !strings.Contains(list.all(), "FAILED TO LOAD") {
		t.Fatalf("control failed: `omw ext list` does not report this machine's extension as failed, "+
			"so there is no disagreement for this test to be about:\n%s", list.all())
	}

	show := runModelCmd(t, env, "show").all()
	if !strings.Contains(show, "FAILED TO LOAD") || !strings.Contains(show, boom) {
		t.Errorf("`omw ext list` says this machine's acme extension FAILED TO LOAD and `omw model show` "+
			"does not say so, nor carry its reason %q:\n%s", boom, show)
	}
	if strings.Contains(show, "has no adapter for") {
		t.Errorf("`omw model show` answers with a fact about THE BUILD — true of every machine running "+
			"this binary — where the fact about THIS machine is a registered extension that failed to "+
			"load. This is f55a176's defect on the model side:\n%s", show)
	}
}
