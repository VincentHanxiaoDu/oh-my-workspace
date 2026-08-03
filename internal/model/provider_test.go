package model

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// recordingProvider records every method anybody calls on it, including on the sessions it hands
// out. It is the instrument for "registering a provider contacts nothing".
type recordingProvider struct {
	name     string
	opened   atomic.Int32
	asked    atomic.Int32
	contacts atomic.Int32 // incremented by anything that would be a network round trip
	openErr  error
}

func (p *recordingProvider) Name() string { return p.name }

func (p *recordingProvider) Open(credential string) (Session, error) {
	p.opened.Add(1)
	if p.openErr != nil {
		return nil, p.openErr
	}
	return &recordingSession{p: p, credential: credential}, nil
}

type recordingSession struct {
	p          *recordingProvider
	credential string
	answer     string
	askErr     error
}

func (s *recordingSession) Ask(prompt string) (string, error) {
	s.p.asked.Add(1)
	// THIS IS THE STAND-IN FOR THE NETWORK, and it is the only place in this package's tests where
	// anything pretends to reach an endpoint. Nothing dials; the counter is what "contacted the
	// endpoint" means here, which is what lets the whole suite run with no network at all (§4.2).
	s.p.contacts.Add(1)
	return s.answer, s.askErr
}

func withProvider(t *testing.T, p Provider) {
	t.Helper()
	Register(p)
	t.Cleanup(func() { unregister(p.Name()) })
}

// §4.2 AND §4.4: REGISTERING A PROVIDER CONTACTS NOTHING.
//
// A registry that reached out on registration would open a connection at init time on a machine
// with no hub and no model configured, and would make this package's tests need a live network.
// The provider counts every call it receives; after registering, looking up and listing, the only
// method that may have been called is Name.
func TestRegisteringAProviderContactsNothing(t *testing.T) {
	p := &recordingProvider{name: "recording"}
	withProvider(t, p)

	if _, ok := Lookup("recording"); !ok {
		t.Fatal("the provider did not register, so this test is not exercising a registry")
	}
	found := false
	for _, n := range Names() {
		if n == "recording" {
			found = true
		}
	}
	if !found {
		t.Error("Names() does not list the registered provider")
	}

	if got := p.opened.Load(); got != 0 {
		t.Errorf("registering, looking up and listing called Open %d time(s); registration must not bind a credential", got)
	}
	if got := p.asked.Load(); got != 0 {
		t.Errorf("registering, looking up and listing called Ask %d time(s)", got)
	}
	if got := p.contacts.Load(); got != 0 {
		t.Errorf("registering, looking up and listing contacted the provider's endpoint %d time(s); "+
			"§4.2 forbids a connection nobody asked for", got)
	}

	// THE CONTROL. The counter must be capable of moving, or its staying at zero above proves
	// nothing. Asking a session is the one thing that is allowed to contact anything.
	sess, err := p.Open("a-credential")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Ask("hello"); err != nil {
		t.Fatal(err)
	}
	if p.contacts.Load() != 1 {
		t.Fatal("asking the session did not move the contact counter, so the assertions above were vacuous")
	}
}

// Reading a model configuration does not open a provider either. This is the same rule one level
// up: `omw model` answers "am I configured?" without anything being bound or dialled.
func TestReadingTheConfigurationDoesNotOpenTheProvider(t *testing.T) {
	p := &recordingProvider{name: "recording-read"}
	withProvider(t, p)

	cfg := Read(envOf(map[string]string{EnvProvider: "recording-read", EnvCredential: theSecret}), nil)
	if cfg.Configured() != tri.Yes || cfg.Name != "recording-read" {
		t.Fatalf("the configuration under test did not resolve: %v", cfg.Render())
	}
	_ = cfg.Render()
	_ = cfg.View().Render()

	if p.opened.Load() != 0 || p.contacts.Load() != 0 {
		t.Errorf("reading the configuration opened the provider %d time(s) and contacted it %d time(s)",
			p.opened.Load(), p.contacts.Load())
	}
}

// The registry refuses the two things that would make a name ambiguous.
func TestTheRegistryRefusesNamelessAndDuplicateProviders(t *testing.T) {
	mustPanic(t, "a nil provider", func() { Register(nil) })
	mustPanic(t, "a provider with no name", func() { Register(&recordingProvider{name: "  "}) })

	p := &recordingProvider{name: "dup"}
	withProvider(t, p)
	mustPanic(t, "a duplicate provider name", func() { Register(&recordingProvider{name: "dup"}) })
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("registering %s did not panic; it would be kept silently and a person would reach whichever won", what)
		}
	}()
	f()
}

// A provider this build does not have is a DETERMINED fact, and it is not "no provider chosen".
// The distinction matters because the two need different things from the person: one needs them to
// choose, the other needs an adapter they cannot write.
func TestAnUnknownProviderIsNotTheSameAsNoProvider(t *testing.T) {
	if _, ok := Lookup("a-provider-no-build-has"); ok {
		t.Fatal("a name nothing registered was found")
	}
	chosen := Read(envOf(map[string]string{EnvProvider: "a-provider-no-build-has"}), nil)
	if chosen.Provider != tri.Yes {
		t.Errorf("choosing an unregistered provider reports provider %v; the person did choose one", chosen.Provider)
	}
}

// Opening a provider that refuses is an error the caller sees, not a nil Session it would
// dereference and not a silent success.
func TestAProviderThatRefusesToOpenSaysSo(t *testing.T) {
	want := errors.New("this machine has no credentials helper")
	p := &recordingProvider{name: "refusing", openErr: want}
	withProvider(t, p)
	sess, err := p.Open(theSecret)
	if !errors.Is(err, want) {
		t.Errorf("Open returned %v, want the provider's own reason", err)
	}
	if sess != nil {
		t.Error("Open returned both an error and a session")
	}
}
