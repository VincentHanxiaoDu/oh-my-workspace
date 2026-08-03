package channels

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func extStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(t.TempDir() + "/store")
	if err != nil {
		t.Fatalf("creating a store: %v", err)
	}
	return s
}

// brokenKind is a channel-interface extension that will not load, standing in for an adapter a
// person installed that is broken.
type brokenKind struct {
	kind Kind
	err  error
}

func (b brokenKind) Name() string                   { return string(b.kind) }
func (b brokenKind) Interface() extension.Interface { return extension.Channel }
func (b brokenKind) Load() error                    { return b.err }
func (b brokenKind) Adapter(Connection) (Adapter, error) {
	// NEVER REACHED, and that is the assertion. A broken extension must not be asked for an
	// adapter; if this runs, the load state was consulted too late or not at all.
	panic("the adapter of an extension that failed to load was constructed")
}

// connect puts a usable channel in the store: a credential that has not expired, so that nothing
// short-circuits before the factory is reached.
func connect(t *testing.T, s *store.Store, id string, kind Kind) {
	t.Helper()
	err := Connect(s, Connection{
		ID: id, Kind: kind, Account: "someone@example.com",
		Credential: "a-token", CredentialExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("connecting %s: %v", id, err)
	}
}

// CRITERION 9 — THE FAILURE THIS ISSUE IS REALLY ABOUT.
//
// "A channel adapter that failed to load never causes ingestion to report 'no traffic on this
// channel'. A test that breaks a registered adapter and then asks what channels are ingesting sees
// that adapter reported as failed, not as quiet."
//
// The person's fear, in the Issue's own words: "they install the Slack adapter, nothing appears in
// their tickets, and the product tells them there is no Slack traffic."
func TestABrokenAdapterIsReportedAsFailedAndNeverAsQuiet(t *testing.T) {
	const boom = "libemail.so could not be opened"

	s := extStore(t)
	connect(t, s, "work", KindEmail)

	// A machine that offers a BROKEN email adapter, registered by shipping — the state a person is
	// in after an upgrade that half-installed.
	r := extension.NewRegistry()
	r.Offer(brokenKind{kind: KindEmail, err: errors.New(boom)})
	// REGISTERED BY A DELIBERATE ACT, which is what criterion 9 says: "a test that BREAKS A
	// REGISTERED ADAPTER". An adapter nobody registered is a different state with a different
	// answer, and conflating the two here would have this test pass on the not-registered path
	// while the failed-to-load path went unexercised.
	if err := extension.Register(s, r, string(KindEmail), nil); err != nil {
		t.Fatalf("registering the email adapter: %v", err)
	}

	res, err := Ingest(s, ExtensionFactory(s, r), time.Now())
	if err != nil {
		t.Fatalf("ingesting: %v", err)
	}
	if len(res.Channels) != 1 {
		t.Fatalf("the run reported %d channel(s), want 1 — a channel whose adapter is broken must "+
			"not be dropped from the report", len(res.Channels))
	}
	cr := res.Channels[0]

	if cr.Outcome == OutcomeReached {
		t.Fatalf("the broken channel is reported as REACHED with %d message(s). This is the exact "+
			"defect criterion 9 names: a failed load rendered as a quiet channel.", cr.Messages)
	}
	if cr.Outcome != OutcomeUnreachable {
		t.Fatalf("the outcome is %v; a broken adapter must be a determined negative, not 'not attempted yet'", cr.Outcome)
	}
	if res.AdaptersBuilt != 0 {
		t.Errorf("%d adapter(s) were constructed for an extension that failed to load", res.AdaptersBuilt)
	}
	if !strings.Contains(cr.Detail, boom) {
		t.Errorf("the reason %q does not name the load failure %q", cr.Detail, boom)
	}
	if !strings.Contains(strings.ToUpper(cr.Detail), "FAILED TO LOAD") {
		t.Errorf("the reason %q does not say the adapter failed to load; a person reading it "+
			"cannot tell a broken install from a flaky network", cr.Detail)
	}

	// AND THE RENDERED LINE — what a person actually reads.
	//
	// COMPARED AGAINST THE REAL QUIET RENDERING, not against substrings. A substring scan for
	// "reached" fires on "COULD NOT BE REACHED", and one for "nothing arrived" fires on the
	// sentence that exists to DENY it — so a naive scan would have to be satisfied by deleting the
	// denial, which is the wrong way round. What must not happen is that the broken channel renders
	// the way a reached-and-empty channel renders, so that is what is compared.
	rendered := Ingestion{Outcome: cr.Outcome, OutcomeDetail: cr.Detail}.RenderOutcome()
	quiet := Ingestion{Outcome: OutcomeReached, Messages: 0, Tickets: 0}.RenderOutcome()
	if rendered == quiet {
		t.Errorf("the broken channel renders exactly as a channel that was reached and had "+
			"nothing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "COULD NOT BE REACHED") {
		t.Errorf("the rendered outcome does not say the channel was not reached:\n%s", rendered)
	}
	// The one phrase a person must never see about a channel whose adapter is broken, in the
	// affirmative: "it saw N messages".
	if strings.Contains(rendered, "it saw") {
		t.Errorf("the rendered outcome claims to have seen messages on a channel that was never "+
			"attempted:\n%s", rendered)
	}

	// The stored record agrees, so the NEXT `omw channels list` says the same thing.
	got, err := Get(s, "work")
	if err != nil {
		t.Fatalf("reading the channel back: %v", err)
	}
	if got.Last.Outcome != OutcomeUnreachable {
		t.Errorf("the recorded outcome is %v, want OutcomeUnreachable", got.Last.Outcome)
	}
	if got.Last.State != tri.No {
		t.Errorf("the recorded last-success state is %v; a failed load must not invent a success", got.Last.State)
	}
}

// The third answer on the same path: an adapter whose load could not be DETERMINED is also not a
// quiet channel, and its reason does not read as a failure.
func TestAnUndeterminedAdapterLoadIsAlsoNotAQuietChannel(t *testing.T) {
	s := extStore(t)
	connect(t, s, "work", KindEmail)

	r := extension.NewRegistry()
	r.Offer(brokenKind{kind: KindEmail, err: extension.ErrLoadUndetermined})
	if err := extension.Register(s, r, string(KindEmail), nil); err != nil {
		t.Fatalf("registering the email adapter: %v", err)
	}

	res, err := Ingest(s, ExtensionFactory(s, r), time.Now())
	if err != nil {
		t.Fatalf("ingesting: %v", err)
	}
	cr := res.Channels[0]
	if cr.Outcome == OutcomeReached {
		t.Fatal("an adapter whose load could not be determined was reported as reached")
	}
	if !strings.Contains(strings.ToLower(cr.Detail), "could not be determined") {
		t.Errorf("the reason %q does not say the load state could not be determined", cr.Detail)
	}
	if strings.Contains(strings.ToUpper(cr.Detail), "FAILED TO LOAD") {
		t.Errorf("an undetermined load is reported as a failure: %q. §4.3: 'could not determine' "+
			"and 'determined to be nothing' are different values.", cr.Detail)
	}
}

// The control: a WORKING extension still reaches its adapter, so the assertions above are about a
// broken one rather than about the factory refusing everything.
func TestAWorkingExtensionStillReachesItsAdapter(t *testing.T) {
	s := extStore(t)
	connect(t, s, "work", KindEmail)

	res, err := Ingest(s, ExtensionFactory(s, extension.Default), time.Now())
	if err != nil {
		t.Fatalf("ingesting: %v", err)
	}
	if res.AdaptersBuilt != 1 {
		t.Fatalf("%d adapter(s) built for a healthy built-in extension, want 1 — the factory is "+
			"refusing everything and the broken-adapter tests above prove nothing", res.AdaptersBuilt)
	}
	cr := res.Channels[0]
	// This build has no transport, so the attempt is still unreachable — but for the TRANSPORT's
	// reason, not a load failure. The two must not be confused, which is why this is asserted.
	if strings.Contains(strings.ToUpper(cr.Detail), "FAILED TO LOAD") {
		t.Errorf("a healthy extension with no transport is reported as a failed load: %q", cr.Detail)
	}
	if !strings.Contains(cr.Detail, "no transport") {
		t.Errorf("the reason %q does not name the missing transport", cr.Detail)
	}
}

// CRITERION 6. "The built-in channels (Teams and email, §3.1) appear in the same listing as
// anything registered through the extension point, so a person sees one inventory of what can reach
// them, not 'the built-ins' plus 'the extensions'."
func TestTheBuiltInChannelsAreInTheOneInventory(t *testing.T) {
	s := extStore(t)
	entries, err := extension.Inventory(s, extension.Default)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	for _, k := range Kinds() {
		e := extension.Find(entries, string(k))
		if e.Resolved() != extension.Loaded {
			t.Errorf("the built-in %s channel is %v in the one inventory, want Loaded. §3.1 ships "+
				"it built in.", k, e.StateText)
		}
		if e.Interface != extension.Channel {
			t.Errorf("%s reports interface %q, want %q", k, e.Interface, extension.Channel)
		}
	}
	if len(Kinds()) == 0 {
		t.Fatal("this build has no channel kinds; the loop above examined nothing")
	}

	// ONE INVENTORY, NOT TWO SECTIONS. The built-ins are sorted in among everything else by name,
	// with nothing in their rendering that marks them as a separate class — a person reading the
	// listing sees one list of what can reach them.
	for _, e := range entries {
		for _, split := range []string{"built-in", "built in", "builtin", "(shipped)"} {
			if strings.Contains(strings.ToLower(e.Render()), split) {
				t.Errorf("%s's entry contains %q, which splits the one inventory back into 'the "+
					"built-ins' plus 'the extensions' — the thing criterion 6 forbids:\n%s",
					e.Name, split, e.Render())
			}
		}
	}
}

// Nothing but the built-in channel kinds claims to have been registered by shipping. That marker is
// the one exemption from criterion 17's "not registered by a deliberate act is not registered", and
// an exemption nobody watches is a hole.
func TestOnlyTheBuiltInChannelKindsAreShipped(t *testing.T) {
	builtin := map[string]bool{}
	for _, k := range Kinds() {
		builtin[string(k)] = true
	}
	offered := extension.Default.Offered()
	if len(offered) == 0 {
		t.Fatal("the default registry offers nothing; this check examined nothing")
	}
	shipped := 0
	for _, e := range offered {
		s, ok := e.(extension.Shipped)
		if !ok || !s.Shipped() {
			continue
		}
		shipped++
		if !builtin[e.Name()] {
			t.Errorf("%q claims to be shipped-and-registered and is not one of this build's "+
				"channel kinds. Criterion 17: an extension present on disk is NOT registered "+
				"until a person says so.", e.Name())
		}
	}
	if shipped != len(builtin) {
		t.Errorf("%d extension(s) are shipped-and-registered, want %d (one per built-in kind)", shipped, len(builtin))
	}
}

// This file plugs the channel interface into the one mechanism and RESTATES NONE OF IT.
//
// The failure it guards against is the one §2.5 is about: somebody adds a channel-specific state,
// a channel-specific rendering or a channel-specific vocabulary here, and the product quietly has
// two systems again. Every state word a person reads about a channel extension must come from
// `internal/extension`.
func TestThisFileRestatesNoneOfTheSharedMechanism(t *testing.T) {
	src, err := readSource("extension.go")
	if err != nil {
		t.Fatalf("reading extension.go: %v", err)
	}
	for _, forbidden := range []string{
		"type State", "func (s State)", "type Entry", "func (e Entry) Render",
		"registered, and it loaded", "not registered —",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("channels/extension.go contains %q — the shared half of the mechanism is "+
				"being restated here, which is how one mechanism becomes two (§2.5)", forbidden)
		}
	}
	if !strings.Contains(src, "extension.Default.Offer") {
		t.Fatal("this file no longer offers the built-in kinds into the one registry; the scan " +
			"above is reading something else")
	}
}

// readSource reads one of this package's files verbatim, comments included: the scan above is
// looking for restated PRODUCT WORDING, and a copy of the shared vocabulary pasted into a comment
// here would be the first step towards a copy in the code.
func readSource(name string) (string, error) {
	b, err := os.ReadFile(name)
	return string(b), err
}
