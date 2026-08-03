package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/agentapi"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// A STORE THAT WOULD NOT OPEN IS NOT A STORE WITH NO MODEL IN IT (§4.3, criterion 18).
//
// # What this test is for, and why it is not in internal/model
//
// internal/model is already three-valued and a control below re-proves it. The defect this test
// exists for was in the SEAM: `agentSources` opened the store, dropped the error, and passed the
// nil store to model.Read — where nil documents "this caller has no store", a determined fact. So
// a machine whose store could not be read reported, through the agent API, the same bytes as a
// machine with nothing configured: "model: no provider is chosen". Testing internal/model again
// would have proved nothing about that, because internal/model was never wrong.
//
// So the arms are driven through `agentSources`, and compared on `Model().View().Render()` —
// verbatim the bytes agentapi.answerModel puts in `Response.Model`, and on the Outcome the agent
// API reaches through agentapi.Answer.
func TestAnUnreadableStoreIsNotReportedAsNoModelThroughTheAgentAPI(t *testing.T) {
	noEnv := func(string) string { return "" }

	// ARM A: a REAL store, created by store.Create, that opens fine and has no model recorded.
	// A bare directory would not do: store.Open rejects one as ErrNotFound, "no store here", which
	// is a third state and would compare the wrong thing.
	readable := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(readable); err != nil {
		t.Fatalf("could not create the readable store: %v", err)
	}

	// ARM B: the same kind of real store, made unreadable.
	unreadable := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(unreadable); err != nil {
		t.Fatalf("could not create the store that is about to be unreadable: %v", err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("could not make the store unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	// CONTROL: DID THE STAGING ACTUALLY TAKE? As root, 0o000 is not a barrier and arm B would open
	// perfectly — the two arms would then differ or agree for a reason that has nothing to do with
	// the code under test. Ask store.Open directly and say so rather than reporting either way.
	if _, err := store.Open(unreadable); err == nil {
		t.Skip("this environment can open a 0o000 store (running as root?), so the unreadable arm is not staged here")
	}

	// ARM C: a real, readable store with a provider RECORDED IN IT. Arms A and B alone do not
	// notice a seam that ignores the store entirely; this one does, because the recorded name can
	// only come from the store that was opened.
	recorded := filepath.Join(t.TempDir(), "store")
	rs, err := store.Create(recorded)
	if err != nil {
		t.Fatalf("could not create the store with a recorded choice: %v", err)
	}
	if err := model.Use(rs, "acme"); err != nil {
		t.Fatalf("could not record a provider: %v", err)
	}

	render := func(root string) string {
		return agentSources(root, noEnv).Model().View().Render()
	}
	armA, armB, armC := render(readable), render(unreadable), render(recorded)

	// THE POINT: the two arms must not be the same bytes.
	if armA == armB {
		t.Errorf("a store that could not be read renders exactly as a readable store with no model in it,\n"+
			"so an agent cannot tell 'nothing is configured' from 'I could not find out'; both said:\n%s", armA)
	}
	if got := agentSources(readable, noEnv).Model().Configured(); got != tri.No {
		t.Errorf("a readable store with nothing configured is a determined no; got %v", got)
	}
	if got := agentSources(unreadable, noEnv).Model().Configured(); got != tri.Undetermined {
		t.Errorf("a store that could not be read is undetermined, never a no; got %v", got)
	}
	if strings.Contains(armB, "no provider is chosen") {
		t.Errorf("the unreadable store is reported as a determined negative:\n%s", armB)
	}
	// THE CLI'S OWN SENTENCE, NOT A FOURTH VOCABULARY. internal/commands' modelStore prints this
	// on the same condition, and criterion 18 is that these two surfaces report one state.
	if !strings.Contains(armB, "An unreadable store is not one with no model recorded in it.") {
		t.Errorf("the unreadable store's answer does not carry the CLI's own distinction:\n%s", armB)
	}
	if !strings.Contains(armB, unreadable) {
		t.Errorf("the unreadable store's answer does not name the store that could not be read:\n%s", armB)
	}

	// ARM C: the store is genuinely consulted. This is the assertion QA's surviving mutation M3
	// (`model.Read(getenv, s)` → `model.Read(getenv, nil)`) has to walk through.
	if !strings.Contains(armC, "acme") {
		t.Errorf("a provider recorded in the store does not reach the agent API's answer, so the store "+
			"this seam opened is not the store the model answer came from:\n%s", armC)
	}
	if armC == armA {
		t.Errorf("a recorded provider renders exactly as nothing configured:\n%s", armC)
	}
}

// THE EXIT CODES MUST DIFFER TOO, and that is the half a byte comparison cannot see: two surfaces
// may word a state differently and still hand a caller the same number. `omw model show` exits 3
// (cli.ExitUndetermined) on a store it could not read; OutcomeOK.Exit() is 0. "could not determine"
// and "determined to be nothing" never share an exit code.
func TestTheAgentAPIDoesNotAnswerOKForAStoreItCouldNotRead(t *testing.T) {
	noEnv := func(string) string { return "" }

	readable := filepath.Join(t.TempDir(), "store")
	gs, err := store.Create(readable)
	if err != nil {
		t.Fatalf("could not create the readable store: %v", err)
	}
	unreadable := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(unreadable); err != nil {
		t.Fatalf("could not create the store that is about to be unreadable: %v", err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("could not make the store unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })
	if _, err := store.Open(unreadable); err == nil {
		t.Skip("this environment can open a 0o000 store (running as root?), so the unreadable arm is not staged here")
	}

	// THE GRANT LEDGER IS DELIBERATELY NOT THE ARM'S OWN STORE. A store nobody can read holds no
	// readable grant ledger either, so a full request against arm B is refused for lack of
	// authority long before the model is consulted — an honest answer to a different question, and
	// it would hide this one. Authority is therefore held constant across both arms, in a store
	// that opens, so that the only thing varying is the model source under test.
	ledger := agentapi.NewStoreGrants(gs)
	grant, err := ledger.Issue(hub.Holder{Person: hub.PersonID("me"), Scopes: []hub.Scope{hub.ScopeRead}},
		[]hub.Scope{hub.ScopeRead})
	if err != nil {
		t.Fatalf("could not issue a grant: %v", err)
	}

	ask := func(root string) agentapi.Response {
		src := agentSources(root, noEnv)
		src.Person = hub.PersonID("me")
		src.PersonScopes = []hub.Scope{hub.ScopeRead}
		src.Grants = ledger
		return agentapi.Answer(agentapi.Request{Op: agentapi.OpModel, Grant: grant.ID}, src)
	}

	ok, unsure := ask(readable), ask(unreadable)

	if ok.Outcome != agentapi.OutcomeOK {
		t.Fatalf("a readable store with nothing configured is a determined answer and should succeed at "+
			"answering; got %v (%s: %s)", ok.Outcome, ok.Code, ok.Message)
	}
	if unsure.Outcome != agentapi.OutcomeUndetermined {
		t.Errorf("a store that could not be read is answered %v — the CLI calls this undetermined and exits 3",
			unsure.Outcome)
	}
	if ok.Outcome.Exit() == unsure.Outcome.Exit() {
		t.Errorf("'could not determine' and 'determined to be nothing' share exit code %d", ok.Outcome.Exit())
	}
	if unsure.Model == nil || unsure.Model.Chosen() != tri.Undetermined {
		t.Errorf("the response's model field does not say the provider was undetermined: %+v", unsure.Model)
	}
}
