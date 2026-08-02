package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

func extStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(t.TempDir() + "/store")
	if err != nil {
		t.Fatalf("creating a store: %v", err)
	}
	return s
}

// brokenProvider is a model-interface extension that will not load.
type brokenProvider struct {
	name string
	err  error
}

func (b brokenProvider) Name() string                   { return b.name }
func (b brokenProvider) Interface() extension.Interface { return extension.Model }
func (b brokenProvider) Load() error                    { return b.err }

// CRITERION 10 — THE OTHER FAILURE THIS ISSUE IS REALLY ABOUT.
//
// "A model provider that failed to load is never reported as 'no model configured'. §3.13's
// no-model-configured state and the failed-to-load state are distinguishable in output by
// inspection alone, and a capability that needs a model says which of the two situations it is in."
//
// PRD §3.13 says no-model-configured is not a broken client. Issue #21: that sentence "becomes a
// lie the moment a failed load is dressed up as an unconfigured one".
func TestAProviderThatFailedToLoadIsNeverNoModelConfigured(t *testing.T) {
	const boom = "the acme extension needs a newer omw"

	s := extStore(t)
	r := extension.NewRegistry()
	r.Offer(brokenProvider{name: "acme", err: errors.New(boom)})
	if err := extension.Register(s, r, "acme", nil); err != nil {
		t.Fatalf("registering acme: %v", err)
	}
	if err := Use(s, "acme"); err != nil {
		t.Fatalf("choosing acme: %v", err)
	}

	// A person who HAS configured a model: provider chosen, credential in their environment.
	getenv := func(k string) string {
		if k == EnvCredential {
			return "sk-live-whatever"
		}
		return ""
	}
	view := Read(getenv, s).View()

	broken := Readiness(s, r, view)
	if broken.Situation != SituationExtensionFailedToLoad {
		t.Fatalf("a chosen provider whose extension failed to load is reported as situation %v.\n"+
			"reason: %s\nThis is the exact defect criterion 10 names.", broken.Situation, broken.Reason)
	}
	if broken.Code == ErrNoModel.Code {
		t.Errorf("it carries the no-model-configured code %q. A caller with no English to inspect "+
			"— a script, an agent, the control API — cannot tell the two situations apart.", broken.Code)
	}
	if !strings.Contains(broken.Reason, boom) {
		t.Errorf("the reason %q does not carry the extension's own failure %q", broken.Reason, boom)
	}

	// NOW THE OTHER SITUATION, ON THE SAME MACHINERY. Nothing chosen at all.
	empty := extStore(t)
	unconfigured := Readiness(empty, extension.NewRegistry(), Read(func(string) string { return "" }, empty).View())
	if unconfigured.Situation != SituationNoModelConfigured {
		t.Fatalf("a machine with no provider chosen is situation %v, want SituationNoModelConfigured:\n%s",
			unconfigured.Situation, unconfigured.Reason)
	}

	// "DISTINGUISHABLE IN OUTPUT BY INSPECTION ALONE." Compared pairwise, and by code.
	if broken.Reason == unconfigured.Reason {
		t.Errorf("the two situations produce the same sentence:\n%s", broken.Reason)
	}
	if broken.Code == unconfigured.Code {
		t.Errorf("the two situations share the code %q", broken.Code)
	}
	if strings.TrimSpace(broken.Reason) == "" || strings.TrimSpace(unconfigured.Reason) == "" {
		t.Error("one of the two situations renders as nothing (criterion 21)")
	}
	// The broken client says it is broken, and says it is NOT the other thing.
	if !strings.Contains(strings.ToLower(broken.Reason), "not 'no model configured'") {
		t.Errorf("the failed-to-load sentence does not deny being the unconfigured one, so a "+
			"person skimming it will read it as one:\n%s", broken.Reason)
	}
	// And the unconfigured client says it is not broken — §3.13's actual sentence.
	if !strings.Contains(unconfigured.Reason, "not a broken client") {
		t.Errorf("the no-model-configured sentence does not say §3.13's 'not a broken client':\n%s",
			unconfigured.Reason)
	}
}

// The four situations are pairwise distinct in code and in sentence, including the third answer.
func TestTheModelSituationsArePairwiseDistinct(t *testing.T) {
	answers := map[string]Answer{}

	// No provider chosen.
	empty := extStore(t)
	answers["unconfigured"] = Readiness(empty, extension.NewRegistry(),
		Read(func(string) string { return "" }, empty).View())

	// Chosen, extension broken.
	broken := extStore(t)
	rb := extension.NewRegistry()
	rb.Offer(brokenProvider{name: "acme", err: errors.New("no")})
	mustReg(t, broken, rb, "acme")
	if err := Use(broken, "acme"); err != nil {
		t.Fatalf("use: %v", err)
	}
	answers["broken"] = Readiness(broken, rb, Read(withKey, broken).View())

	// Chosen, extension load undetermined.
	undet := extStore(t)
	ru := extension.NewRegistry()
	ru.Offer(brokenProvider{name: "acme", err: extension.ErrLoadUndetermined})
	mustReg(t, undet, ru, "acme")
	if err := Use(undet, "acme"); err != nil {
		t.Fatalf("use: %v", err)
	}
	answers["undetermined"] = Readiness(undet, ru, Read(withKey, undet).View())

	// Chosen, extension fine, no credential.
	nokey := extStore(t)
	rk := extension.NewRegistry()
	rk.Offer(brokenProvider{name: "acme"})
	mustReg(t, nokey, rk, "acme")
	if err := Use(nokey, "acme"); err != nil {
		t.Fatalf("use: %v", err)
	}
	answers["nocredential"] = Readiness(nokey, rk, Read(func(string) string { return "" }, nokey).View())

	// Chosen, extension fine, credential present.
	ready := extStore(t)
	rr := extension.NewRegistry()
	rr.Offer(brokenProvider{name: "acme"})
	mustReg(t, ready, rr, "acme")
	if err := Use(ready, "acme"); err != nil {
		t.Fatalf("use: %v", err)
	}
	answers["ready"] = Readiness(ready, rr, Read(withKey, ready).View())

	wantSituations := map[string]Situation{
		"unconfigured": SituationNoModelConfigured,
		"broken":       SituationExtensionFailedToLoad,
		"undetermined": SituationUndetermined,
		"nocredential": SituationNoModelConfigured,
		"ready":        SituationReady,
	}
	for label, want := range wantSituations {
		if got := answers[label].Situation; got != want {
			t.Errorf("%s is situation %v, want %v:\n%s", label, got, want, answers[label].Reason)
		}
	}

	// Pairwise on the sentence: no two arrangements of the machine say the same thing.
	seen := map[string]string{}
	for label, a := range answers {
		if strings.TrimSpace(a.Reason) == "" {
			t.Errorf("%s has an empty reason (criterion 21)", label)
		}
		if other, dup := seen[a.Reason]; dup {
			t.Errorf("%s and %s say exactly the same thing:\n%s", label, other, a.Reason)
		}
		seen[a.Reason] = label
	}

	// And undetermined never shares a code with a determined negative.
	if answers["undetermined"].Code == answers["unconfigured"].Code ||
		answers["undetermined"].Code == answers["broken"].Code {
		t.Errorf("the undetermined answer shares a code with a determined one: %q. "+
			"'could not determine' and 'determined to be nothing' are different values.",
			answers["undetermined"].Code)
	}
}

// Criterion 10's remaining clause, on the state Issue #18 already owns: a provider that is chosen
// but whose extension is NOT REGISTERED is still not "no model configured". Their configuration is
// intact; the code to talk to it is not registered, which is a different thing to go and fix.
func TestAChosenProviderWithNoRegisteredExtensionIsNotUnconfigured(t *testing.T) {
	s := extStore(t)
	if err := Use(s, "acme"); err != nil {
		t.Fatalf("use: %v", err)
	}
	a := Readiness(s, extension.NewRegistry(), Read(withKey, s).View())
	if a.Situation == SituationNoModelConfigured {
		t.Fatalf("reported as no model configured:\n%s", a.Reason)
	}
	if !strings.Contains(a.Reason, "acme") {
		t.Errorf("the reason does not name the provider the person chose:\n%s", a.Reason)
	}
	if !strings.Contains(a.Reason, "omw ext register acme") {
		t.Errorf("the reason does not say what to do about it:\n%s", a.Reason)
	}
}

// A provider is registered ONCE and becomes known to both `omw model use` and `omw ext list`.
// Two registries that can disagree is the "two systems" §2.5 forbids, rebuilt inside the fix.
func TestRegisteringAProviderMakesItKnownToTheOneMechanism(t *testing.T) {
	p := stubProvider{name: "one-registration"}
	Register(p)
	t.Cleanup(func() { unregister(p.name) })

	if _, ok := Lookup(p.name); !ok {
		t.Fatal("the provider is not in the model registry")
	}
	e, ok := extension.Default.Find(p.name)
	if !ok {
		t.Fatalf("the provider is in the model registry and NOT offered to the extension "+
			"mechanism; `omw model use %s` would work while `omw ext list` never mentioned it", p.name)
	}
	if e.Interface() != extension.Model {
		t.Errorf("it is offered as interface %q, want %q", e.Interface(), extension.Model)
	}
	if err := e.Load(); err != nil {
		t.Errorf("a registered provider does not load: %v", err)
	}

	// And removing it removes it from BOTH, or the next Register of the same name panics.
	unregister(p.name)
	if _, ok := extension.Default.Find(p.name); ok {
		t.Error("unregistering left the extension mechanism holding a provider the model registry " +
			"no longer has")
	}
	Register(p) // must not panic on a duplicate
}

type stubProvider struct{ name string }

func (s stubProvider) Name() string { return s.name }
func (s stubProvider) Open(string) (Session, error) {
	return nil, errors.New("not opened in this test")
}

func withKey(k string) string {
	if k == EnvCredential {
		return "sk-live-whatever"
	}
	return ""
}

func mustReg(t *testing.T, s *store.Store, r *extension.Registry, name string) {
	t.Helper()
	if err := extension.Register(s, r, name, nil); err != nil {
		t.Fatalf("registering %s: %v", name, err)
	}
}
