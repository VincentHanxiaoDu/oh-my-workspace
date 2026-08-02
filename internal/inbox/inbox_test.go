package inbox

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("creating a store to test against: %v", err)
	}
	return s
}

func mustPut(t *testing.T, s *store.Store, tk Ticket) {
	t.Helper()
	if err := Put(s, tk); err != nil {
		t.Fatalf("putting ticket %q: %v", tk.ID, err)
	}
}

func ids(ts []Ticket) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

// ---------------------------------------------------------------------------
// CRITERIA 4 AND 5 — a ticket is a thing you have to act on, not a message.
// ---------------------------------------------------------------------------

// The driver seeds the SOURCE MATERIAL Issue #8 names: a broken login discussed across five emails,
// a chat thread and a follow-up ping, whose message bodies include `yes`, `ok` and `Hii`. What the
// inbox is asked to hold is one obligation. The assertion is that no listed title is any of those
// bodies — and, because a rule that only the test knows is not a rule, that the inbox REFUSES to
// store one.
func TestNoTicketTitleIsTheVerbatimBodyOfAMessage(t *testing.T) {
	s := newStore(t)

	// The traffic about one broken login. Bodies, exactly as they were typed.
	traffic := []string{
		"Hii", "hi", "Ana can't log in after the SSO change", "ok", "yes",
		"Thanks", "any update?", "ok 👍", "OK!",
	}
	// What ingestion is supposed to produce from all of that: ONE ticket, written.
	mustPut(t, s, Ticket{
		ID:      "ana-sso-login",
		Title:   Text("Restore Ana's login after the SSO change"),
		Summary: Text("Ana has been locked out since the SSO cutover; she has asked twice."),
		Channel: Text("email"),
	})

	// And what must not be storable: the bodies themselves, as titles.
	for _, body := range traffic {
		err := Put(s, Ticket{ID: "seeded", Title: Text(body), Summary: Text("from the same thread")})
		isAck := IsAcknowledgement(body)
		switch {
		case isAck && !errors.Is(err, ErrNotAnObligation):
			t.Errorf("a ticket titled %q was accepted (err=%v). An acknowledgement is not a ticket "+
				"at any priority — PRD §3.2", body, err)
		case !isAck && err != nil && errors.Is(err, ErrNotAnObligation):
			t.Errorf("a ticket titled %q was refused as an acknowledgement; it is a real request", body)
		case !isAck:
			// A real request is storable. Remove it again so the listing below is about the one
			// written ticket.
			if err == nil {
				if derr := Delete(s, "seeded"); derr != nil {
					t.Fatalf("cleaning up: %v", derr)
				}
			}
		}
	}

	listed, err := List(s)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("the inbox holds %d tickets from one broken login: %v. It is one obligation, not "+
			"a row per message", len(listed), ids(listed))
	}
	for _, tk := range listed {
		title, _ := tk.Title.Value()
		for _, body := range traffic {
			if IsAcknowledgement(body) && title == body {
				t.Errorf("the inbox surfaced the raw message %q as a ticket title", body)
			}
		}
	}
}

// CRITERION 5, STRUCTURALLY: an acknowledgement cannot be a low-priority ticket because there is no
// priority. A test asserting "a low-priority ticket was created" must FAIL, and it fails here at
// compile time for lack of a field — so this asserts the field's continued absence instead.
func TestATicketHasNoPriorityRankOrOrderingValue(t *testing.T) {
	banned := []string{"priority", "rank", "order", "severity", "urgency", "score", "weight",
		"importance", "position", "index", "tier", "level", "acknowledg", "smalltalk"}
	rt := reflect.TypeOf(Ticket{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.ToLower(rt.Field(i).Name)
		for _, b := range banned {
			if strings.Contains(name, b) {
				t.Errorf("Ticket has a field %q. Acknowledgements are not low-priority tickets; "+
					"they are not tickets, and there is to be no value one could be given (PRD §3.2)",
					rt.Field(i).Name)
			}
		}
	}
	// The same question asked of everything this package exports, because a priority reachable
	// through a function or a constant is a priority.
	for _, name := range exportedNames(t, ".") {
		low := strings.ToLower(name)
		for _, b := range banned {
			if strings.Contains(low, b) && name != "IsAcknowledgement" && name != "ErrNotAnObligation" {
				t.Errorf("package inbox exports %q — the inbox has no ranking of obligations", name)
			}
		}
	}
}

// The listing order is by identifier and is a presentation order, not a judgement. Asserted by
// seeding out of order and observing the order restored — and by there being nothing to sort BY
// except the identifier, which the test above establishes.
func TestListingOrderIsByIdentifierAndNotByAnyRanking(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"c-third", "a-first", "b-second"} {
		mustPut(t, s, Ticket{ID: id, Title: Text("do " + id), Summary: Text("...")})
	}
	got, err := List(s)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	want := []string{"a-first", "b-second", "c-third"}
	if !reflect.DeepEqual(ids(got), want) {
		t.Errorf("listing order %v; want %v", ids(got), want)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 6 — enumerate the operations, and assert none publishes.
// ---------------------------------------------------------------------------

func TestNoOperationOnATicketPublishesSharesOrSendsIt(t *testing.T) {
	ops := Operations()
	if len(ops) == 0 {
		t.Fatal("the enumeration is empty, so asserting over it asserts nothing")
	}
	leaving := []string{"publish", "share", "send", "upload", "post", "push", "sync", "export",
		"hub", "remote", "transmit", "mail", "forward"}
	for _, op := range ops {
		if op.LeavesTheMachine {
			t.Errorf("operation %q transfers a ticket off this machine. Tickets are never "+
				"published (PRD §2.3)", op.Name)
		}
		for _, word := range leaving {
			if strings.Contains(strings.ToLower(op.Name), word) {
				t.Errorf("there is an operation named %q", op.Name)
			}
		}
	}
	// AND NOTHING UNDER ANOTHER NAME. The enumeration is only trustworthy if the package has no
	// second door — so every exported identifier is asked the same question.
	for _, name := range exportedNames(t, ".") {
		for _, word := range leaving {
			if strings.Contains(strings.ToLower(name), word) {
				t.Errorf("package inbox exports %q; the inbox has no route to a hub at all", name)
			}
		}
	}
}

// CRITERIA 6, 7, 8 AND 13, STRUCTURALLY AND HONESTLY LABELLED. This does not observe a socket; it
// observes that there is nothing here that could open one. The behavioural half — a hub configured
// and reachable, and zero requests arriving at it — is TestNoInboxOperationTouchesAConfiguredHub in
// internal/commands, which drives the real command against a real listening server.
func TestTheInboxPackageImportsNothingThatCouldReachAHub(t *testing.T) {
	networking := []string{"net", "net/http", "net/url", "net/rpc", "os/exec", "net/smtp"}
	for file, imports := range importsOf(t, ".") {
		for _, imp := range imports {
			for _, n := range networking {
				if imp == n || strings.HasPrefix(imp, n+"/") {
					t.Errorf("%s imports %q. There is no configuration under which the inbox "+
						"reaches a hub, and that is a property of what it imports", file, imp)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 9 AND 10 — nothing expires.
// ---------------------------------------------------------------------------

// The clock is advanced past any plausible window by BACKDATING the tickets rather than by moving
// the machine's clock: a ticket that arrived a century ago is indistinguishable, from the code's
// point of view, from one that arrives a century before a clock that has been wound forward — and
// this way the assertion does not depend on the test's own environment.
func TestNothingExpiresNoMatterHowLongATicketHasBeenOwed(t *testing.T) {
	s := newStore(t)
	ages := map[string]time.Time{
		"ancient": time.Now().Add(-100 * 365 * 24 * time.Hour),
		"old":     time.Now().Add(-400 * 24 * time.Hour),
		"recent":  time.Now().Add(-time.Hour),
		"undated": {},
	}
	for id, when := range ages {
		mustPut(t, s, Ticket{ID: id, Title: Text("owed since " + id), Summary: Text("still owed"), Arrived: when})
	}

	before, err := List(s)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	// Exercise every other operation — criterion 10 says the set is identical after all of them.
	for _, id := range ids(before) {
		if _, err := Get(s, id); err != nil {
			t.Fatalf("reading %q: %v", id, err)
		}
	}
	_ = Operations()
	if _, err := List(s); err != nil {
		t.Fatalf("listing again: %v", err)
	}

	after, err := List(s)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if !reflect.DeepEqual(ids(before), ids(after)) {
		t.Fatalf("the ticket set changed without anybody deleting anything: %v then %v",
			ids(before), ids(after))
	}
	if len(after) != len(ages) {
		t.Fatalf("%d of %d tickets survived; nothing expires (PRD §5.4)", len(after), len(ages))
	}
	// The hundred-year-old one is not merely counted, it is readable.
	if _, err := Get(s, "ancient"); err != nil {
		t.Errorf("a ticket owed for a century is no longer readable: %v", err)
	}
}

// A structural companion to the above, and weaker on purpose: it asserts that no code in this
// package consults the present time at all, which is why no elapsed time can change what is listed.
// It is the reason the test above can be confident rather than merely lucky about the window it
// chose.
func TestNothingInThisPackageConsultsThePresentTime(t *testing.T) {
	for file, src := range sourcesOf(t, ".") {
		for _, call := range []string{"time.Now(", "time.Since(", "time.Until("} {
			if strings.Contains(src, call) {
				t.Errorf("%s calls %s. Nothing decides what is in the inbox from the clock; "+
					"tickets stay until the person deletes them (PRD §5.4)", file, call)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERION 11 — delete removes exactly what was named, and refuses what is not there.
// ---------------------------------------------------------------------------

func TestDeleteRemovesExactlyThatTicketAndLeavesTheRest(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"one", "two", "three"} {
		mustPut(t, s, Ticket{ID: id, Title: Text("do " + id), Summary: Text("...")})
	}
	if err := Delete(s, "two"); err != nil {
		t.Fatalf("deleting a ticket that is there: %v", err)
	}
	if _, err := Get(s, "two"); !errors.Is(err, ErrNoSuchTicket) {
		t.Errorf("the deleted ticket is still readable (err=%v)", err)
	}
	got, err := List(s)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if !reflect.DeepEqual(ids(got), []string{"one", "three"}) {
		t.Errorf("after deleting %q the inbox holds %v", "two", ids(got))
	}
}

// The store's Delete is idempotent by design. The INBOX's must not be: a person who names a ticket
// that is not there has made a mistake, and being told it was deleted is the wrong answer.
func TestDeletingATicketThatIsNotThereIsRefused(t *testing.T) {
	s := newStore(t)
	mustPut(t, s, Ticket{ID: "real", Title: Text("a real obligation"), Summary: Text("...")})
	if err := Delete(s, "never-existed"); !errors.Is(err, ErrNoSuchTicket) {
		t.Fatalf("deleting an unknown identifier returned %v; it must be refused as ErrNoSuchTicket", err)
	}
	got, err := List(s)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if !reflect.DeepEqual(ids(got), []string{"real"}) {
		t.Errorf("a refused delete changed the ticket set to %v", ids(got))
	}
}

// ---------------------------------------------------------------------------
// Reading, and damage.
// ---------------------------------------------------------------------------

func TestReadingAnIdentifierThatIsNotThereIsNotAnEmptyTicket(t *testing.T) {
	s := newStore(t)
	got, err := Get(s, "nope")
	if !errors.Is(err, ErrNoSuchTicket) {
		t.Fatalf("reading an unknown identifier returned %v", err)
	}
	if got.ID != "" {
		t.Errorf("a failed read handed back a ticket: %+v", got)
	}
}

// A damaged ticket fails the listing. Skipping it would report an inbox with one fewer thing to do,
// and the person would act on a list that is quietly short.
func TestADamagedTicketFailsTheListingRatherThanBeingSkipped(t *testing.T) {
	s := newStore(t)
	mustPut(t, s, Ticket{ID: "good", Title: Text("a real obligation"), Summary: Text("...")})
	if err := s.Put(store.Record{Kind: Kind, ID: "damaged", Data: []byte(`{"format":99}`)}); err != nil {
		t.Fatalf("seeding a damaged ticket: %v", err)
	}
	got, err := List(s)
	if err == nil {
		t.Fatalf("listing succeeded with %d tickets and a damaged one on disk: %v", len(got), ids(got))
	}
	if !errors.Is(err, ErrUnreadableTicket) {
		t.Errorf("a damaged ticket reported as %v; it must be unreadable, never absent", err)
	}
}

// A ticket record that simply has no title key decodes as ABSENT — a determined "this ticket has
// none" — and not as an empty string. Criterion 1 at the layer where it is easiest to lose.
func TestATicketRecordWithNoTitleKeyDecodesAsAbsentAndNotAsEmpty(t *testing.T) {
	s := newStore(t)
	if err := s.Put(store.Record{Kind: Kind, ID: "keyless",
		Data: []byte(`{"format":1,"id":"keyless","summary":""}`)}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	got, err := Get(s, "keyless")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got.Title.Render() == got.Summary.Render() {
		t.Errorf("a title that was never recorded and a summary written as the empty string both "+
			"render as %q", got.Title.Render())
	}
	if _, ok := got.Title.Value(); ok {
		t.Errorf("a title that was never recorded reports a value")
	}
	if v, ok := got.Summary.Value(); !ok || v != "" {
		t.Errorf("a summary written as the empty string reads as %q, ok=%v", v, ok)
	}
}

func TestATicketWithoutAUsableIdentifierIsRefused(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"", "../escape", "with/slash", "."} {
		if err := Put(s, Ticket{ID: id, Title: Text("x"), Summary: Text("y")}); !errors.Is(err, ErrInvalidTicket) {
			t.Errorf("a ticket with identifier %q was accepted (err=%v)", id, err)
		}
	}
}

// An empty title is storable and renderable — it is a written value. Refusing it here would be this
// package deciding that criterion 1's case cannot arise, which is not the same as handling it.
func TestAnEmptyTitleIsStorableBecauseItIsAWrittenValue(t *testing.T) {
	s := newStore(t)
	if err := Put(s, Ticket{ID: "blank", Title: Text(""), Summary: Absent()}); err != nil {
		t.Fatalf("an empty title was refused: %v", err)
	}
	got, err := Get(s, "blank")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got.Title.Render() == got.Summary.Render() {
		t.Errorf("the empty title and the absent summary both render as %q", got.Title.Render())
	}
}

// A TEST THAT COULD NOT FAIL FOR THE REASON IT WAS NAMED FOR, until QA drove it on PR #29.
//
// It asserted `!strings.Contains(got, "1970")`. Go's zero time.Time is year **0001**, not 1970 —
// the Unix epoch is 1970 and the zero Time is not the Unix epoch — so removing the undetermined
// branch from ArrivedRender made the listing say `0001-01-01T00:00:00Z` and this test stayed green.
// It reported success having examined nothing, on the one field where a wrong value looks most
// plausible, because a date looks like a fact.
//
// It now asserts EQUALITY with tri's own wording rather than the absence of one substring. An
// absence assertion is only as good as the guess about what would be present; an equality
// assertion against the product's third answer fails for every wrong rendering there is, including
// the ones nobody thought of. Compared against `tri` rather than a literal so it moves with the
// product's wording instead of failing on it.
func TestArrivedRendersAnUnknownTimeAsTheProductsThirdAnswer(t *testing.T) {
	got := Ticket{ID: "x"}.ArrivedRender()
	if want := tri.Undetermined.Render("", ""); got != want {
		t.Errorf("a ticket with no arrival time renders as %q; want the product's third answer, %q. "+
			"A date renders as a fact, so a wrong one here is a missing value shown as a real one", got, want)
	}
	// And named explicitly, because these two are what a zero time actually formats as and they are
	// the renderings this test exists to keep out of the output.
	for _, wrong := range []string{"0001", "1970"} {
		if strings.Contains(got, wrong) {
			t.Errorf("a ticket with no arrival time renders as %q, which contains %q — a "+
				"real-looking date for a fact nobody knows", got, wrong)
		}
	}
	if got == "" {
		t.Error("a ticket with no arrival time renders as nothing; silence is not an answer")
	}
	// The control: a ticket that DOES have an arrival time must not render as the third answer, or
	// the assertion above would be satisfied by an ArrivedRender that says "could not be
	// determined" for everything.
	known := Ticket{ID: "x", Arrived: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}.ArrivedRender()
	if known == got {
		t.Errorf("a ticket with a known arrival time renders identically to one without: %q", known)
	}
	if !strings.Contains(known, "2026") {
		t.Errorf("a known arrival time renders as %q and does not show the year it happened", known)
	}
}

// EVERY ENTRY IN THE LIST MUST BE REACHABLE, or the list claims a coverage it does not have.
//
// Found on PR #29: `"+1"` was declared and `IsAcknowledgement("+1")` returned false, because the
// normaliser trimmed `+` as a symbol and reduced it to `"1"`. A ticket titled `+1` was accepted by
// the real Put. Nothing failed, because every test named a body it already knew was covered. This
// asks the question of the whole map instead, so the next dead entry fails on the commit that adds
// it rather than on a reviewer's afternoon.
func TestEveryDeclaredAcknowledgementIsReachable(t *testing.T) {
	if len(acknowledgements) == 0 {
		t.Fatal("the list is empty, so asserting over it asserts nothing")
	}
	for body := range acknowledgements {
		if !IsAcknowledgement(body) {
			t.Errorf("%q is declared an acknowledgement and IsAcknowledgement says it is not — "+
				"the list claims a coverage it does not have", body)
		}
	}
	// And the forms a person actually types, each of which must reduce to a declared entry.
	for _, body := range []string{"+1", "OK!", "ok 👍", "  Yes  ", "THANKS.", "Got it!"} {
		if !IsAcknowledgement(body) {
			t.Errorf("%q is not recognised as an acknowledgement", body)
		}
	}
	// The control: a real obligation must not be swept up by the normalisation above.
	for _, body := range []string{"Restore Ana's login", "any update?", "Approve the Q3 invoice", "1"} {
		if IsAcknowledgement(body) {
			t.Errorf("%q was classed as an acknowledgement; it is a real request", body)
		}
	}
}

// ---------------------------------------------------------------------------
// Presence — probed, never named. PRD §4.2, §4.6, §5.1.
// ---------------------------------------------------------------------------

func TestWithNoControlSocketTheDaemonIsDeterminedNotToBeRunning(t *testing.T) {
	root := t.TempDir()
	p := Probe(root)
	if p.Running.String() != "no" {
		t.Errorf("with no control socket the daemon reports %v; that is determinable and it is no", p.Running)
	}
	if p.ControlAPIOpen.String() != "no" {
		t.Errorf("with nothing listening the control API reports %v", p.ControlAPIOpen)
	}
	// And probing started nothing.
	if _, err := os.Stat(filepath.Join(root, ControlSocketName)); err == nil {
		t.Error("probing created a control socket; nothing may start the daemon (PRD §4.2)")
	}
}

// A path that exists and is not a socket is UNDETERMINED — not "not running". The distinction is
// the whole of §4.3 applied to §4.2, and this is the case where a bool would have said "no".
func TestSomethingThatIsNotASocketIsUndeterminedAndNotANegative(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ControlSocketName), []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	p := Probe(root)
	if p.Running.Determined() {
		t.Errorf("a control socket path holding a regular file was answered as %v", p.Running)
	}
	if p.ControlAPIOpen.Determined() {
		t.Errorf("whether the control API is open was answered as %v", p.ControlAPIOpen)
	}
	if p.RunningWhy == "" || p.ControlWhy == "" {
		t.Error("undetermined without a reason is close to silence; both must name what was found")
	}
}
