package daemon

import (
	"errors"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// THE CONTROL API ANSWERS THE ADAPTER QUESTION ABOUT THIS MACHINE, NOT ABOUT THE BUILD
// (Issue #21 criteria 10 and 20; PRD §4.3).
//
// `omw ext list` reporting a registered model provider extension as FAILED TO LOAD while a second
// surface says "this build has no adapter for it" is one fact with two answers and the wrong layer
// giving one of them — the defect f55a176 fixed on the channel side. The CLI half is guarded in
// `internal/commands`; this is the control API half, and without it `modelViewFor` could be put
// back to `.View()` with every test in this repository still green.
//
// # WHY THE REGISTRY IS SWAPPED RATHER THAN extension.Default MUTATED
//
// `extension.Registry` is a value precisely so "what does this machine offer" is not a statement
// about whichever test ran last. [ModelRegistry] is the seam, exactly as `channels.LoopRegistry`
// is for the daemon's ingestion.

type brokenModelExt struct {
	name string
	why  error
}

func (b brokenModelExt) Name() string                   { return b.name }
func (b brokenModelExt) Interface() extension.Interface { return extension.Model }
func (b brokenModelExt) Load() error                    { return b.why }

func TestTheControlAPINamesAFailedModelExtensionRatherThanTheMissingAdapter(t *testing.T) {
	const boom = "libacme.so was built against a different interface"

	// Nothing may come from the environment: what is under test is what the STORE and the REGISTRY
	// contribute together.
	t.Setenv(model.EnvProvider, "")
	t.Setenv(model.EnvCredential, "")
	t.Setenv(model.EnvCredentialFile, "")

	r := extension.NewRegistry()
	r.Offer(brokenModelExt{name: "acme", why: errors.New(boom)})
	prev := ModelRegistry
	ModelRegistry = r
	t.Cleanup(func() { ModelRegistry = prev })

	root := newTestStore(t)
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("opening the store this test drives against: %v", err)
	}
	// The person chose a provider AND registered its extension, by two deliberate acts. An
	// unregistered provider is a different state with a different answer.
	if err := model.Use(s, "acme"); err != nil {
		t.Fatalf("recording the chosen provider: %v", err)
	}
	if err := extension.Register(s, r, "acme", nil); err != nil {
		t.Fatalf("registering the provider extension: %v", err)
	}

	// CONTROL: this machine's own inventory says the extension failed. Without this, an assertion
	// about the model report is an assertion about a machine that might not be in the state the
	// test believes.
	entries := extension.Read(s, r).Entries
	e := extension.FindAs(entries, "acme", extension.Model)
	if e.Resolved() != extension.FailedToLoad {
		t.Fatalf("control failed: this machine's acme model extension resolves as %v, not FailedToLoad, "+
			"so there is nothing for the model report to disagree with", e.Resolved())
	}

	got := Inspect(root).Model
	if got.Chosen() != tri.Yes {
		t.Fatalf("the provider is not reported as chosen, so this test is not about the state it says: %+v", got)
	}

	rendered := got.Render()
	if !strings.Contains(rendered, "FAILED TO LOAD") || !strings.Contains(rendered, boom) {
		t.Errorf("`omw ext list` reports this machine's acme extension as FAILED TO LOAD and the control "+
			"API does not say so, nor carry its reason %q:\n  %s", boom, rendered)
	}
	if strings.Contains(rendered, "has no adapter for") {
		t.Errorf("the control API answers with a fact about THE BUILD — true of every machine running "+
			"this binary — where the fact about THIS machine is a registered extension that failed to "+
			"load. That is f55a176's defect on the model side:\n  %s", rendered)
	}
}

// AN UNREADABLE STORE STILL DOES NOT ACQUIRE AN ADAPTER SENTENCE (Issue #68 alongside criterion 10).
//
// The two rules meet in `modelViewFor`, and the way they could fight is for the extension lookup to
// claim something about a machine whose store nobody could read. It cannot: `model.ViewOn` returns
// before consulting any registry unless a provider is CHOSEN, and #68's unreadable arm reports the
// provider as undetermined precisely because it must not claim one. This pins that.
func TestAnUnreadableStoreGainsNoAdapterSentence(t *testing.T) {
	t.Setenv(model.EnvProvider, "")
	t.Setenv(model.EnvCredential, "")
	t.Setenv(model.EnvCredentialFile, "")

	r := extension.NewRegistry()
	r.Offer(brokenModelExt{name: "acme", why: errors.New("libacme.so was built against a different interface")})
	prev := ModelRegistry
	ModelRegistry = r
	t.Cleanup(func() { ModelRegistry = prev })

	unreadable := newTestStore(t)
	makeUnreadable(t, unreadable)

	got := Inspect(unreadable).Model
	if got.Chosen() != tri.Undetermined {
		t.Fatalf("#68's rule is not in effect here, so this test is not about the state it says: %+v", got)
	}
	if got.AdapterDetail != "" || strings.Contains(got.Render(), "FAILED TO LOAD") {
		t.Errorf("a store nobody could read acquired a claim about this machine's extensions:\n  %s", got.Render())
	}
}
