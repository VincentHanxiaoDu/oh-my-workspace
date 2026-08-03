package extension

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Test doubles. ONE type serves both interfaces, which is itself the point.
// ---------------------------------------------------------------------------

// fake is an extension whose interface and load outcome a test chooses.
//
// IT IS ONE TYPE FOR BOTH INTERFACES ON PURPOSE. Two fakes — a fakeChannel and a fakeProvider —
// would let the two halves of a "these behave identically" test diverge in the fixture rather than
// in the product, and the fixture would then be what made them agree.
type fake struct {
	name  string
	iface Interface
	err   error

	loads   int
	adapter int
}

func (f *fake) Name() string         { return f.name }
func (f *fake) Interface() Interface { return f.iface }
func (f *fake) Load() error          { f.loads++; return f.err }

func registryWith(exts ...Extension) *Registry {
	r := NewRegistry()
	for _, e := range exts {
		r.Offer(e)
	}
	return r
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(t.TempDir() + "/store")
	if err != nil {
		t.Fatalf("creating a store: %v", err)
	}
	return s
}

// mustRegister registers and fails the test if it did not happen, so that a later assertion about
// state cannot pass because nothing was registered at all.
func mustRegister(t *testing.T, s *store.Store, r *Registry, name string) {
	t.Helper()
	if err := Register(s, r, name, nil); err != nil {
		t.Fatalf("registering %s: %v", name, err)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 3 — THE TEST THIS ISSUE EXISTS FOR
// ---------------------------------------------------------------------------

// CRITERION 3. "The state vocabulary is identical across the two interfaces. … A test that captures
// a failed-to-load channel adapter line and a failed-to-load model provider line and diffs them,
// with names normalised, finds no difference."
//
// # THIS IS PRD §2.5 MADE MACHINE-CHECKABLE
//
// "A company adding a channel and a company choosing a model do the same kind of thing, and should
// not learn two systems." Two systems do not announce themselves as two systems. They announce
// themselves as one product in which the channel error says "adapter failed to load: %v" and the
// model error says "could not initialise provider %s (%v)" — both perfectly reasonable, written
// months apart by people reading different files, and between them they are two vocabularies a
// person has to learn.
//
// So the assertion is a DIFF and not a pair of substring checks. A substring check on each line
// passes forever after the two have drifted apart; a diff of the two, normalised only for the
// things criterion 3 says may differ, fails the moment anybody adds a word to one of them.
func TestAFailedChannelLineAndAFailedModelLineDiffToNothing(t *testing.T) {
	const boom = "the shared object could not be opened: no such file"

	s := newStore(t)
	r := registryWith(
		&fake{name: "slack", iface: Channel, err: errors.New(boom)},
		&fake{name: "acme", iface: Model, err: errors.New(boom)},
	)
	mustRegister(t, s, r, "slack")
	mustRegister(t, s, r, "acme")

	entries, err := Inventory(s, r)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	channelLine := Find(entries, "slack")
	modelLine := Find(entries, "acme")

	// THE FIXTURE IS CHECKED BEFORE THE DIFF. A diff of two empty strings is also "no difference",
	// and a test that stopped resolving anything would pass loudest of all.
	for _, e := range []Entry{channelLine, modelLine} {
		if e.Resolved() != FailedToLoad {
			t.Fatalf("%s resolved to %v, want FailedToLoad — the fixture is not exercising the "+
				"state this test is about", e.Name, e.StateText)
		}
		if !strings.Contains(e.Detail, boom) {
			t.Fatalf("%s carries detail %q, which does not contain the failure it was given; the "+
				"diff below would be comparing something other than a real failure", e.Name, e.Detail)
		}
	}
	if channelLine.Interface == modelLine.Interface {
		t.Fatalf("both entries claim interface %q; this test is diffing two lines of the same "+
			"interface and proves nothing about the two being the same", channelLine.Interface)
	}

	// Normalise ONLY what criterion 3 permits to differ: "byte-identical apart from the extension's
	// own name and interface".
	normalise := func(e Entry) string {
		out := e.Render()
		out = strings.ReplaceAll(out, e.Name, "<NAME>")
		out = strings.ReplaceAll(out, string(e.Interface), "<INTERFACE>")
		return out
	}
	got, want := normalise(channelLine), normalise(modelLine)
	if got != want {
		t.Errorf("a failed-to-load channel adapter and a failed-to-load model provider do not "+
			"render alike. PRD §2.5: they are one mechanism and a person must not learn two "+
			"vocabularies.\n\nchannel adapter:\n%s\nmodel provider:\n%s", got, want)
	}
}

// The same diff, for EVERY state and not only the failed one.
//
// Criterion 3 names failed-to-load because that is the one people get wrong. The claim it is an
// instance of is "the rendering of a given state is byte-identical apart from the extension's own
// name and interface", and that claim is about all four.
func TestEveryStateRendersAlikeAcrossTheTwoInterfaces(t *testing.T) {
	cases := []struct {
		state State
		err   error
	}{
		{Loaded, nil},
		{FailedToLoad, errors.New("it would not open")},
		{Undetermined, fmt.Errorf("the loader hung: %w", ErrLoadUndetermined)},
	}
	for _, c := range cases {
		t.Run(c.state.String()[:12], func(t *testing.T) {
			s := newStore(t)
			r := registryWith(
				&fake{name: "slack", iface: Channel, err: c.err},
				&fake{name: "acme", iface: Model, err: c.err},
			)
			mustRegister(t, s, r, "slack")
			mustRegister(t, s, r, "acme")
			entries, _ := Inventory(s, r)
			ch, md := Find(entries, "slack"), Find(entries, "acme")
			if ch.Resolved() != c.state || md.Resolved() != c.state {
				t.Fatalf("fixture did not produce %v: channel=%v model=%v", c.state, ch.StateText, md.StateText)
			}
			norm := func(e Entry) string {
				out := strings.ReplaceAll(e.Render(), e.Name, "<NAME>")
				return strings.ReplaceAll(out, string(e.Interface), "<INTERFACE>")
			}
			if norm(ch) != norm(md) {
				t.Errorf("state %v renders differently for the two interfaces:\n%s\nvs\n%s",
					c.state, norm(ch), norm(md))
			}
		})
	}

	// NotRegistered, which cannot come from a registration and so is built the other way.
	s := newStore(t)
	r := registryWith(
		&fake{name: "slack", iface: Channel},
		&fake{name: "acme", iface: Model},
	)
	entries, _ := Inventory(s, r)
	ch, md := Find(entries, "slack"), Find(entries, "acme")
	if ch.Resolved() != NotRegistered || md.Resolved() != NotRegistered {
		t.Fatalf("offered-and-unregistered did not produce NotRegistered: %v / %v", ch.StateText, md.StateText)
	}
	norm := func(e Entry) string {
		out := strings.ReplaceAll(e.Render(), e.Name, "<NAME>")
		return strings.ReplaceAll(out, string(e.Interface), "<INTERFACE>")
	}
	if norm(ch) != norm(md) {
		t.Errorf("not-registered renders differently for the two interfaces:\n%s\nvs\n%s", norm(ch), norm(md))
	}
}

// CRITERION 1, at the level below the command. "Registering a channel adapter and registering a
// model provider are the same act … A test that registers one and then the other, changing only the
// extension identifier, passes both times."
//
// The strongest form of that claim is a table driven over the two interfaces with ONE body: if the
// body needed an `if` on the interface, the criterion would already be false.
func TestRegisteringIsOneActForBothInterfaces(t *testing.T) {
	for _, name := range []string{"slack", "acme"} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			r := registryWith(
				&fake{name: "slack", iface: Channel},
				&fake{name: "acme", iface: Model},
			)
			// THE ONLY THING THAT CHANGES BETWEEN THE TWO RUNS IS `name`.
			if err := Register(s, r, name, map[string]string{"endpoint": "https://example.invalid"}); err != nil {
				t.Fatalf("registering %s: %v", name, err)
			}
			reg, err := Get(s, name)
			if err != nil {
				t.Fatalf("reading %s back: %v", name, err)
			}
			if reg.Name != name {
				t.Errorf("registered name is %q, want %q", reg.Name, name)
			}
			if reg.Settings["endpoint"] != "https://example.invalid" {
				t.Errorf("settings did not survive: %v", reg.Settings)
			}
			entries, _ := Inventory(s, r)
			if got := Find(entries, name).Resolved(); got != Loaded {
				t.Errorf("%s is %v after registering, want Loaded", name, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CRITERION 7 and 13 — four states, compared PAIRWISE
// ---------------------------------------------------------------------------

// CRITERIA 7 AND 13. Three distinct renderings plus undetermined as a fourth.
//
// # COMPARED PAIRWISE, NOT AGAINST STRING LITERALS
//
// Asserting each state against the literal it was written next to passes just as happily after two
// of them have been edited into the same wording — which is precisely the defect ("failed-to-load
// looks like not-registered") the criterion exists to prevent. Every pair against every other pair
// is the only shape that catches it.
func TestTheFourStatesRenderPairwiseDistinctly(t *testing.T) {
	states := States()
	if len(states) != 4 {
		t.Fatalf("there are %d states, want 4 — criterion 7's three plus criterion 13's fourth", len(states))
	}
	for i, a := range states {
		if strings.TrimSpace(a.String()) == "" {
			t.Errorf("state %d renders empty. Criterion 21: no state is ever an empty string, and "+
				"none of the answers is silence.", a)
		}
		for j, b := range states {
			if i >= j {
				continue
			}
			if a.String() == b.String() {
				t.Errorf("states %d and %d both render as %q — a person cannot tell them apart, "+
					"and criterion 7 is exactly the requirement that they can", a, b, a.String())
			}
		}
	}
}

// The zero State must be Undetermined. Reversing the iota order is a one-character change that
// would make every state nobody assigned read as a confident "not registered".
func TestTheZeroStateIsUndetermined(t *testing.T) {
	var s State
	if s != Undetermined {
		t.Fatalf("the zero State is %d, want Undetermined (%d)", s, Undetermined)
	}
	var e Entry
	if e.Resolved() != Undetermined {
		t.Fatalf("a zero Entry resolves to %v; an entry nobody filled in must not read as a "+
			"determined answer", e.Resolved())
	}
}

// CRITERION 7's substance: the three states are distinguishable IN A REAL INVENTORY, not merely as
// constants. Registered-and-loaded, failed-to-load and not-registered, produced by three different
// arrangements of the product and then compared.
func TestFailedNotRegisteredAndIdleAreThreeDistinctRenderings(t *testing.T) {
	s := newStore(t)
	r := registryWith(
		&fake{name: "good", iface: Channel},
		&fake{name: "broken", iface: Channel, err: errors.New("it would not open")},
		&fake{name: "idle", iface: Channel},
	)
	mustRegister(t, s, r, "good")
	mustRegister(t, s, r, "broken")
	// `idle` is offered and deliberately NOT registered.

	entries, _ := Inventory(s, r)
	loaded := Find(entries, "good")
	failed := Find(entries, "broken")
	absent := Find(entries, "idle")

	if loaded.Resolved() != Loaded || failed.Resolved() != FailedToLoad || absent.Resolved() != NotRegistered {
		t.Fatalf("the fixture did not produce three states: %v / %v / %v",
			loaded.StateText, failed.StateText, absent.StateText)
	}
	renders := map[string]string{
		"registered-and-loaded": loaded.Render(),
		"failed-to-load":        failed.Render(),
		"not-registered":        absent.Render(),
	}
	// Names differ between the three, so normalise them out: what must differ is the STATE, and a
	// test that passed only because the names differ would pass on three identical states too.
	norm := map[string]string{}
	for k, v := range renders {
		norm[k] = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(v,
			"good", "<NAME>"), "broken", "<NAME>"), "idle", "<NAME>")
	}
	seen := map[string]string{}
	for label, text := range norm {
		if other, dup := seen[text]; dup {
			t.Errorf("%q and %q render identically once the name is removed:\n%s\n"+
				"Criterion 7: three distinct renderings, not two.", label, other, text)
		}
		seen[text] = label
		if strings.TrimSpace(text) == "" {
			t.Errorf("%q renders as nothing (criterion 21)", label)
		}
	}
}

// CRITERION 13. "A test forcing an indeterminate load result sees a fourth distinct rendering,
// never a blank line and never the not-registered rendering."
func TestAnIndeterminateLoadIsAFourthRenderingAndNotNotRegistered(t *testing.T) {
	s := newStore(t)
	r := registryWith(
		&fake{name: "hangs", iface: Model, err: fmt.Errorf("the loader did not answer: %w", ErrLoadUndetermined)},
		&fake{name: "absent", iface: Model},
		&fake{name: "broken", iface: Model, err: errors.New("it would not open")},
		&fake{name: "fine", iface: Model},
	)
	mustRegister(t, s, r, "hangs")
	mustRegister(t, s, r, "broken")
	mustRegister(t, s, r, "fine")

	entries, _ := Inventory(s, r)
	undet := Find(entries, "hangs")
	if undet.Resolved() != Undetermined {
		t.Fatalf("an indeterminate load resolved to %v, want Undetermined. A (bool, error) whose "+
			"error became a negative is exactly what §4.3 forbids.", undet.StateText)
	}
	if strings.TrimSpace(undet.Render()) == "" {
		t.Fatal("the undetermined rendering is blank — never a blank line (criterion 13)")
	}
	if strings.TrimSpace(undet.Detail) == "" {
		t.Error("the undetermined entry carries no detail; a person told 'could not be determined' " +
			"with no reason has been told nothing they can act on")
	}
	for _, other := range []string{"absent", "broken", "fine"} {
		o := Find(entries, other)
		a := strings.ReplaceAll(undet.Render(), undet.Name, "<NAME>")
		b := strings.ReplaceAll(o.Render(), o.Name, "<NAME>")
		if a == b {
			t.Errorf("the undetermined rendering is indistinguishable from %q's (%v):\n%s",
				other, o.StateText, a)
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 8, 11, 14, 21
// ---------------------------------------------------------------------------

// CRITERION 8. "A test that registers a deliberately broken extension sees non-empty failure detail
// attributable to that extension by name."
func TestAFailedExtensionCarriesItsReasonAndItsName(t *testing.T) {
	const boom = "libslack.so is built for the wrong architecture"
	s := newStore(t)
	r := registryWith(&fake{name: "slack", iface: Channel, err: errors.New(boom)})
	mustRegister(t, s, r, "slack")

	entries, _ := Inventory(s, r)
	e := Find(entries, "slack")
	if e.Resolved() != FailedToLoad {
		t.Fatalf("state is %v, want FailedToLoad", e.StateText)
	}
	if e.Detail == "" {
		t.Fatal("the failure detail is empty; 'it failed' with no reason is barely better than silence")
	}
	if !strings.Contains(e.Detail, boom) {
		t.Errorf("the detail %q does not carry the extension's own reason %q", e.Detail, boom)
	}
	if !strings.Contains(e.Render(), "slack") {
		t.Errorf("the rendered entry does not name slack, so the failure is not attributable:\n%s", e.Render())
	}
}

// CRITERION 11. "One extension failing to load does not suppress the reporting of the others."
func TestOneBrokenExtensionDoesNotSuppressTheRest(t *testing.T) {
	s := newStore(t)
	r := registryWith(
		&fake{name: "broken", iface: Channel, err: errors.New("no")},
		&fake{name: "good", iface: Model},
	)
	mustRegister(t, s, r, "broken")
	mustRegister(t, s, r, "good")

	entries, err := Inventory(s, r)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("the inventory has %d entries, want 2 — one broken extension must not take the "+
			"others down with it: %v", len(entries), entries)
	}
	if got := Find(entries, "broken").Resolved(); got != FailedToLoad {
		t.Errorf("broken is %v, want FailedToLoad", got)
	}
	if got := Find(entries, "good").Resolved(); got != Loaded {
		t.Errorf("good is %v, want Loaded — it was reported alongside a failure, with its own state", got)
	}
}

// CRITERION 14. "Undetermined is never omitted. Every registered extension appears in the listing
// whatever its state; a registered extension whose state is unknown is present with an undetermined
// state, not dropped."
//
// Driven with a DAMAGED RECORD, which is the way this actually happens: not a state somebody chose,
// but a file the product could not read. `channels.List` fails the whole call in that situation and
// is right to; here criterion 11 pulls the other way and the record must come back as an entry.
func TestARegistrationThatCannotBeReadIsListedUndeterminedAndNotDropped(t *testing.T) {
	s := newStore(t)
	r := registryWith(&fake{name: "good", iface: Channel})
	mustRegister(t, s, r, "good")

	// A record no build understands: valid JSON, wrong envelope version.
	if err := s.Put(store.Record{Kind: RecordKind, ID: "mystery", Data: []byte(`{"format":9999,"name":"mystery"}`)}); err != nil {
		t.Fatalf("planting a damaged record: %v", err)
	}

	entries, _ := Inventory(s, r)
	e := Find(entries, "mystery")
	if e.Resolved() != Undetermined {
		t.Errorf("an unreadable registration is %v, want Undetermined. It is not a determined "+
			"negative: we failed to read it, which establishes nothing about the extension.", e.StateText)
	}
	if strings.TrimSpace(e.Render()) == "" {
		t.Error("it rendered as nothing (criteria 14 and 21)")
	}
	if got := Find(entries, "good").Resolved(); got != Loaded {
		t.Errorf("the readable extension is %v; one damaged record must not suppress it", got)
	}
	names := map[string]bool{}
	for _, x := range entries {
		names[x.Name] = true
	}
	if !names["mystery"] {
		t.Error("the damaged registration was DROPPED from the listing. Criterion 14: a registered " +
			"extension whose state is unknown is present with an undetermined state, not absent.")
	}
}

// CRITERION 21. "No extension state is ever rendered as an empty string, an empty listing row, or
// an absent line. A test asserting that every registered extension produces exactly one non-empty
// entry passes for every state including undetermined."
func TestEveryStateProducesExactlyOneNonEmptyEntry(t *testing.T) {
	s := newStore(t)
	r := registryWith(
		&fake{name: "aloaded", iface: Channel},
		&fake{name: "bfailed", iface: Model, err: errors.New("no")},
		&fake{name: "cundet", iface: Channel, err: fmt.Errorf("hung: %w", ErrLoadUndetermined)},
		&fake{name: "dunreg", iface: Model},
	)
	for _, n := range []string{"aloaded", "bfailed", "cundet"} {
		mustRegister(t, s, r, n)
	}
	entries, _ := Inventory(s, r)

	sum := Summarise(entries)
	if sum.Loaded != 1 || sum.Failed != 1 || sum.Undetermined != 1 || sum.NotRegistered != 1 {
		t.Fatalf("the fixture did not produce one of each state: %+v", sum)
	}
	seenNames := map[string]int{}
	for _, e := range entries {
		seenNames[e.Name]++
		text := e.Render()
		if strings.TrimSpace(text) == "" {
			t.Errorf("%s (%v) rendered as nothing", e.Name, e.StateText)
		}
		if strings.TrimSpace(e.StateText) == "" {
			t.Errorf("%s has an empty state text", e.Name)
		}
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				t.Errorf("%s (%v) rendered an empty line:\n%q", e.Name, e.StateText, text)
			}
		}
	}
	for name, n := range seenNames {
		if n != 1 {
			t.Errorf("%s produced %d entries, want exactly one", name, n)
		}
	}

	// And the whole-listing rendering says something even when there is nothing to say.
	if got := Render(nil); strings.TrimSpace(got) == "" {
		t.Error("an empty inventory renders as nothing; an empty section reads as a rendering bug " +
			"rather than an answer")
	}
}

// ---------------------------------------------------------------------------
// CRITERION 12 — the exit-code answer, at the level that computes it
// ---------------------------------------------------------------------------

// CRITERION 12's substance in this package: `AllLoaded` distinguishes "every registered extension
// loaded" from "at least one failed", and an unregistered extension is neither.
func TestAllLoadedIsAboutRegisteredExtensionsOnly(t *testing.T) {
	cases := []struct {
		name string
		sum  Summary
		want bool
	}{
		{"nothing at all", Summary{}, true},
		{"all loaded", Summary{Total: 2, Loaded: 2}, true},
		{"loaded plus something merely offered", Summary{Total: 2, Loaded: 1, NotRegistered: 1}, true},
		{"one failed", Summary{Total: 2, Loaded: 1, Failed: 1}, false},
		{"one undetermined", Summary{Total: 2, Loaded: 1, Undetermined: 1}, false},
	}
	for _, c := range cases {
		if got := c.sum.AllLoaded(); got != c.want {
			t.Errorf("%s: AllLoaded()=%v, want %v", c.name, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 16, 17, 19, 22
// ---------------------------------------------------------------------------

// CRITERION 16. "Registering a model provider in particular does not contact the provider's
// endpoint as a side effect of registration."
//
// Asserted by COUNTING CALLS on the extension, which is stronger than watching for connections: a
// build that dials only when a credential is present looks exactly like a well-behaved one for the
// duration of a test that has no credential.
func TestRegisteringContactsNothing(t *testing.T) {
	s := newStore(t)
	f := &fake{name: "acme", iface: Model}
	r := registryWith(f)

	if err := Register(s, r, "acme", map[string]string{"endpoint": "https://acme.invalid"}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if f.loads != 0 {
		t.Errorf("Register called Load %d time(s). Registering is a store write; whether an "+
			"extension loads is answered when somebody ASKS (§4.2, criterion 16).", f.loads)
	}

	// Configuring must not either.
	if err := Configure(s, "acme", map[string]string{"endpoint": "https://elsewhere.invalid"}); err != nil {
		t.Fatalf("configuring: %v", err)
	}
	if f.loads != 0 {
		t.Errorf("Configure called Load %d time(s)", f.loads)
	}

	// And asking the inventory IS when it happens — otherwise the zero above proves only that
	// nothing ever calls Load.
	if _, err := Inventory(s, r); err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if f.loads == 0 {
		t.Fatal("Load was never called at all, so the assertions above prove nothing about WHEN " +
			"it is called")
	}
}

// CRITERION 17. "An extension present on disk but not registered by a deliberate act is reported as
// not registered, and does not begin ingesting or serving a model."
func TestPresentOnDiskIsNotRegistered(t *testing.T) {
	s := newStore(t)
	f := &fake{name: "slack", iface: Channel}
	r := registryWith(f)

	entries, _ := Inventory(s, r)
	e := Find(entries, "slack")
	if e.Resolved() != NotRegistered {
		t.Errorf("an offered, unregistered extension is %v, want NotRegistered", e.StateText)
	}
	if strings.TrimSpace(e.Detail) == "" {
		t.Error("it says it is not registered and does not say what to do about it")
	}
	// It is LISTED, though: criterion 17 says it is "reported as not registered", which it cannot
	// be if it is missing from the listing.
	found := false
	for _, x := range entries {
		if x.Name == "slack" {
			found = true
		}
	}
	if !found {
		t.Error("it is absent from the listing entirely, so nothing reports it as not registered")
	}
	// Nothing else was consulted, and in particular nothing was started: the only call an
	// unregistered extension may see is none.
	if _, err := Get(s, "slack"); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("Get says %v; being present on disk must not amount to a registration", err)
	}
}

// CRITERION 19. "A partially-completed registration is never left behind as a half-registered
// entry." Every refusal path is driven and the store is checked afterwards.
func TestARefusedRegistrationLeavesNothingBehind(t *testing.T) {
	cases := []struct {
		name     string
		ext      string
		settings map[string]string
		wantErr  *struct{ code string }
	}{
		{name: "nothing offers it", ext: "ghost"},
		{name: "the name is unusable", ext: "../escape"},
		{name: "a setting is a credential", ext: "acme", settings: map[string]string{"api_key": "sk-live-1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t)
			r := registryWith(&fake{name: "acme", iface: Model})
			if err := Register(s, r, c.ext, c.settings); err == nil {
				t.Fatalf("registering %q was accepted; this case is meant to be refused", c.ext)
			}
			regs, err := Registered(s)
			if err != nil {
				t.Fatalf("listing registrations: %v", err)
			}
			if len(regs) != 0 {
				t.Errorf("a refused registration left %d record(s) behind: %+v. Criterion 19: "+
					"never a half-registered entry.", len(regs), regs)
			}
		})
	}
}

// CRITERION 22, STRUCTURALLY. A provider's credential can never appear in a listing because there
// is nowhere in the listing's type to put one, and the store refuses to record one.
func TestNoCredentialCanReachAnEntryOrTheStore(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-8f31"

	s := newStore(t)
	r := registryWith(&fake{name: "acme", iface: Model})

	// The RECORD refuses it. Refusing at the point of record beats redacting at the point of
	// display, because there is then nothing for a surface written later — #20's diagnostics
	// bundle, #16's agent API — to leak.
	for _, key := range []string{"key", "api_key", "API-KEY", "secret", "providerToken", "password", "credential"} {
		if err := Register(s, r, "acme", map[string]string{key: secret}); err == nil {
			t.Errorf("a setting called %q holding a credential was ACCEPTED", key)
		} else if !errors.Is(err, ErrSettingLooksLikeASecret) {
			t.Errorf("a setting called %q was refused for the wrong reason: %v", key, err)
		}
	}

	// And nothing reached the store, so nothing can reach an entry.
	mustRegister(t, s, r, "acme")
	entries, _ := Inventory(s, r)
	for _, e := range entries {
		if strings.Contains(e.Render(), secret) {
			t.Errorf("%s's rendering contains the credential:\n%s", e.Name, e.Render())
		}
	}
	regs, _ := Registered(s)
	for _, reg := range regs {
		for k, v := range reg.Settings {
			if strings.Contains(v, secret) {
				t.Errorf("the store holds the credential under %q", k)
			}
		}
	}
}

// The other half of criterion 22: the Entry type has no field a credential FITS in, so the property
// survives a field added six months from now for an unrelated reason. Issue #18 made the same
// assertion about model.View and said why: "the difference between a rule and a guarantee".
func TestNoEntryFieldCanHoldACredential(t *testing.T) {
	allowed := map[string]bool{"Name": true, "Interface": true, "State": true, "StateText": true, "Detail": true}
	ty := entryType()
	if ty.NumField() == 0 {
		t.Fatal("Entry has no fields; this check examined nothing")
	}
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		if !allowed[f.Name] {
			t.Errorf("Entry has a field %q that criterion 22 has not been reasoned about for. "+
				"Every field on this type crosses the control API and lands in a diagnostics "+
				"bundle; add it to the allow-list here only after deciding it can never hold a "+
				"credential.", f.Name)
		}
	}
	forbidden := []string{"credential", "secret", "token", "key", "password", "auth"}
	for i := 0; i < ty.NumField(); i++ {
		low := strings.ToLower(ty.Field(i).Name)
		for _, bad := range forbidden {
			if strings.Contains(low, bad) {
				t.Errorf("Entry has a field %q — there must be nowhere for a credential to go", ty.Field(i).Name)
			}
		}
	}
}

// Every refusal this package defines is distinguishable from every other, by code and by message.
func TestTheRefusalsArePairwiseDistinct(t *testing.T) {
	if len(allErrors) == 0 {
		t.Fatal("allErrors is empty; this check examined nothing")
	}
	codes, msgs := map[string]bool{}, map[string]bool{}
	for _, e := range allErrors {
		if e.Code == "" || e.Msg == "" {
			t.Errorf("%+v has an empty code or message", e)
		}
		if codes[e.Code] {
			t.Errorf("two refusals share the code %q; a caller cannot tell them apart", e.Code)
		}
		if msgs[e.Msg] {
			t.Errorf("two refusals share the message %q", e.Msg)
		}
		codes[e.Code], msgs[e.Msg] = true, true
	}
}

// entryType is Entry's reflect.Type, in one place so the structural tests above cannot drift onto
// a different type.
func entryType() reflect.Type { return reflect.TypeOf(Entry{}) }

// ---------------------------------------------------------------------------
// AN INCOMPLETE READ NEVER PRODUCES A DETERMINED ANSWER
// ---------------------------------------------------------------------------

// corruptRecord damages one record's stored checksum in place, the way a bad disk or a half-synced
// file does — the record file is still valid JSON and still parses; its content no longer matches
// what it says its content is.
func corruptRecord(t *testing.T, s *store.Store, kind store.Kind, id string) {
	t.Helper()
	// THE FILE IS FOUND, NOT SPELLED. The store's record suffix is unexported and this test has no
	// business knowing it — a hardcoded one silently stops matching the day it changes, and a test
	// that damages nothing passes.
	dir := filepath.Join(s.Path(), "records", string(kind))
	matches, err := filepath.Glob(filepath.Join(dir, id+".*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one record file for %q in %s, found %v (err %v)", id, dir, matches, err)
	}
	path := matches[0]
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s to damage it: %v", path, err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("the record envelope is not JSON, so this test cannot damage it as intended: %v", err)
	}
	if _, ok := envelope["sha256"]; !ok {
		t.Fatalf("the record envelope has no sha256 field (%v); this test is damaging the wrong "+
			"thing and would prove nothing", keysOf(envelope))
	}
	envelope["sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	out, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("writing the damaged record: %v", err)
	}
	// THE FIXTURE IS CHECKED. A "damaged" record the store still reads happily would make every
	// assertion below pass for the wrong reason.
	if _, err := s.Get(kind, id); err == nil {
		t.Fatalf("%s still reads cleanly after being damaged; this test is not exercising a "+
			"damaged record", id)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ONE DAMAGED RECORD DOES NOT ERASE THE OTHERS (criteria 11 and 14).
//
// # THE DEFECT THIS EXISTS FOR
//
// `Registered` was built on `store.List`, which refuses the whole kind when any single record's
// checksum is bad — correct for its own contract, and fatal here. One damaged record made
// `Registered` return `nil, err`, EVERY registration vanished from the inventory, and `omw ext list`
// printed "every registered extension loaded" over an inventory it had just failed to read. Two
// registered, failed-to-load extensions reported as absent, beneath a footer saying all was well.
//
// The per-record `readErr` path below `Registered` was already written and was UNREACHABLE for this
// case, which is why reading the code did not find it and driving a damaged record did.
func TestOneDamagedRecordDoesNotEraseTheOtherRegistrations(t *testing.T) {
	s := newStore(t)
	r := registryWith(
		&fake{name: "broken", iface: Channel, err: errors.New("libslack.so is missing")},
		&fake{name: "fine", iface: Model},
		&fake{name: "damaged", iface: Channel},
	)
	for _, n := range []string{"broken", "fine", "damaged"} {
		mustRegister(t, s, r, n)
	}
	corruptRecord(t, s, RecordKind, "damaged")

	listing := Read(s, r)

	// EVERY registration is still present, including the two that have nothing to do with the
	// damaged one.
	for _, name := range []string{"broken", "fine", "damaged"} {
		found := false
		for _, e := range listing.Entries {
			if e.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%q vanished from the inventory because a DIFFERENT record was damaged. "+
				"Criterion 11: one extension failing does not suppress the reporting of the others.", name)
		}
	}
	if got := Find(listing.Entries, "broken").Resolved(); got != FailedToLoad {
		t.Errorf("the failed-to-load extension is %v, want FailedToLoad — a broken extension "+
			"reported as absent is this Issue's opening story", got)
	}
	if got := Find(listing.Entries, "fine").Resolved(); got != Loaded {
		t.Errorf("the healthy extension is %v, want Loaded", got)
	}
	if got := Find(listing.Entries, "damaged").Resolved(); got != Undetermined {
		t.Errorf("the damaged record is %v, want Undetermined — we failed to read it, which "+
			"establishes nothing about the extension", got)
	}

	// AND THE SUMMARY DOES NOT CLAIM EVERYTHING IS FINE.
	if listing.Summary().AllLoaded() {
		t.Errorf("the summary claims every registered extension loaded, over an inventory "+
			"containing a failure and an unreadable record: %+v", listing.Summary())
	}
}

// AN INVENTORY THAT COULD NOT BE ENUMERATED NEVER RENDERS AS A COMPLETE ONE.
//
// The residual case after the fix above: the per-record path handles a damaged record, but a
// registrations directory that cannot be read at all is a failure to enumerate that no per-record
// degradation can rescue. It must not be reported as "nothing is registered", and nothing computed
// from it may claim completeness.
func TestAnInventoryThatCouldNotBeReadNeverClaimsCompleteness(t *testing.T) {
	s := newStore(t)
	r := registryWith(&fake{name: "fine", iface: Model})
	mustRegister(t, s, r, "fine")

	// Make the registrations directory unreadable. This is the enumeration failing, not a record.
	dir := filepath.Join(s.Path(), "records", string(RecordKind))
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Skipf("cannot make the directory unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := s.IDs(RecordKind); err == nil {
		t.Skip("the directory is still readable (running as root?), so this test would prove nothing")
	}

	listing := Read(s, r)

	if listing.Complete() {
		t.Fatal("the listing reports itself complete after the enumeration failed")
	}
	if strings.TrimSpace(listing.Incomplete) == "" {
		t.Error("it is incomplete and says nothing about why")
	}
	if listing.Summary().AllLoaded() {
		t.Error("the summary claims every registered extension loaded over an inventory it could " +
			"not read. That is a determined answer from an incomplete read.")
	}
	// THE RENDERING CARRIES IT. This is what makes the CLI and the control API unable to disagree:
	// the warning is inside the value both of them render.
	out := listing.Render()
	if !strings.Contains(out, tri.Undetermined.String()) {
		t.Errorf("the rendered listing does not say the inventory could not be read in full:\n%s", out)
	}
	if !strings.Contains(out, "may not be all of them") {
		t.Errorf("the rendered listing does not warn that entries may be missing:\n%s", out)
	}
	// And the built-ins are STILL listed — an unreadable registration list is not a machine with
	// no channels.
	if len(listing.Entries) == 0 {
		t.Error("the listing is empty; a failure to read registrations is not a build that ships nothing")
	}
}
