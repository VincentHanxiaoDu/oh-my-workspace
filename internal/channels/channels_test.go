package channels

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
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

var day = time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)

// connected is a channel with a credential that is good for a year, so that no test accidentally
// exercises the expiry path while meaning to exercise something else.
func connected(t *testing.T, s *store.Store, id string, k Kind) Connection {
	t.Helper()
	c := Connection{
		ID: id, Kind: k, Account: id + "@example.com", ConnectedAt: day,
		Credential: "token-for-" + id, CredentialExpiresAt: day.AddDate(1, 0, 0),
	}
	if err := Connect(s, c); err != nil {
		t.Fatalf("connecting %s: %v", id, err)
	}
	return c
}

// fixed returns a factory that hands the same messages to every channel, and counts how often it
// was asked for an adapter.
func fixed(msgs ...Message) (Factory, *int) {
	built := new(int)
	return func(c Connection) (Adapter, error) {
		*built++
		return AdapterFunc(func(Connection) ([]Message, error) { return msgs, nil }), nil
	}, built
}

func ticketsIn(t *testing.T, s *store.Store) []inbox.Ticket {
	t.Helper()
	ts, err := inbox.List(s)
	if err != nil {
		t.Fatalf("listing the inbox: %v", err)
	}
	return ts
}

// =================================================================================================
// CRITERION 1 — both kinds are built in, and the two are distinguishable from each other.
// =================================================================================================

func TestBothKindsAreBuiltInAndRenderDistinguishably(t *testing.T) {
	got := map[Kind]bool{}
	for _, k := range Kinds() {
		got[k] = true
		if !k.Valid() {
			t.Errorf("%q is enumerated by Kinds and is not Valid", k)
		}
		if _, err := Builtin(Connection{Kind: k}); err != nil {
			t.Errorf("%q is enumerated and has no built-in adapter: %v — criterion 1 says nothing is installed", k, err)
		}
	}
	for _, want := range []Kind{KindTeams, KindEmail} {
		if !got[want] {
			t.Errorf("%q is not built in; criterion 1 names Teams and email as the two that are", want)
		}
	}
	// PAIRWISE, NOT AGAINST LITERALS. Asserting each against its own sentence passes just as
	// happily after both have been edited into the same one.
	if KindTeams.String() == KindEmail.String() {
		t.Errorf("Teams and email render identically as %q — a listing that shows both cannot say which is which", KindTeams)
	}
}

// =================================================================================================
// CRITERION 3 — last successful ingestion renders three distinguishable ways.
// =================================================================================================

func TestLastIngestionRendersItsThreeStatesPairwiseDistinctly(t *testing.T) {
	renders := map[string]string{
		"a real timestamp": Ingestion{State: tri.Yes, At: day}.Render(),
		"never ingested":   Ingestion{State: tri.No}.Render(),
		"undetermined":     Ingestion{State: tri.Undetermined}.Render(),
	}
	for name, r := range renders {
		if strings.TrimSpace(r) == "" {
			t.Errorf("%s renders as silence; §4.3 says none of the three is silence", name)
		}
	}
	// COMPARED PAIRWISE. This is the assertion; a literal-by-literal check cannot catch two of them
	// being edited into agreement.
	for a, ra := range renders {
		for b, rb := range renders {
			if a < b && ra == rb {
				t.Errorf("%q and %q both render as %q — criterion 3 requires all three be distinguishable", a, b, ra)
			}
		}
	}
}

func TestNeverIngestedSurvivesStorageAsNeverAndNotAsUndetermined(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	got, err := Get(s, "email-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Last.State != tri.No {
		t.Fatalf("a freshly connected channel reports its last ingestion as %v; a channel that has "+
			"never ingested is a DETERMINED answer, not an unknown one (criterion 3)", got.Last.State)
	}
	if got.Last.Render() == (Ingestion{State: tri.Undetermined}).Render() {
		t.Error("never-ingested renders identically to undetermined")
	}
}

func TestAnUnreadableIngestionTimeIsUndeterminedAndNeverNever(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	// A recorded time that will not parse: the fact is present and could not be read.
	raw, err := s.Get(RecordKind, "email-a")
	if err != nil {
		t.Fatal(err)
	}
	damaged := strings.Replace(string(raw.Data), `"format":1`, `"format":1,"last_success_at":"the day before yesterday"`, 1)
	if damaged == string(raw.Data) {
		t.Fatal("the record did not have the shape this test damages; fix the test, not the product")
	}
	if err := s.Put(store.Record{Kind: RecordKind, ID: "email-a", Data: []byte(damaged)}); err != nil {
		t.Fatal(err)
	}
	got, err := Get(s, "email-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Last.State != tri.Undetermined {
		t.Fatalf("a last-ingestion time that could not be read reports as %v; §4.3 forbids rendering "+
			"a failure to read as a determined negative", got.Last.State)
	}
}

// =================================================================================================
// CRITERION 8 — tickets, not a mirror of the traffic.
// =================================================================================================

// oneMatter is six messages about one broken login: an email thread, replies, and two
// acknowledgements folded into it.
func oneMatter() []Message {
	return []Message{
		{ID: "m1", From: "ana@example.com", Subject: "Login broken", Body: "I cannot sign in since this morning, can you reset it?", At: day, Thread: "T1"},
		{ID: "m2", From: "ana@example.com", Subject: "RE: Login broken", Body: "Still failing after clearing the cache.", At: day.Add(time.Hour), Thread: "T1"},
		{ID: "m3", From: "sam@example.com", Subject: "Re: login broken", Body: "Same for me, the SSO cutover looks involved.", At: day.Add(2 * time.Hour), Thread: "T1"},
		{ID: "m4", From: "ana@example.com", Subject: "Re: Login broken", Body: "thanks", At: day.Add(3 * time.Hour), Thread: "T1"},
		{ID: "m5", From: "sam@example.com", Subject: "Re: Login broken", Body: "ok", At: day.Add(4 * time.Hour), Thread: "T1"},
		{ID: "m6", From: "ana@example.com", Subject: "Fwd: RE: Login broken", Body: "Adding the error text we get.", At: day.Add(5 * time.Hour), Thread: "T1"},
	}
}

func TestSeveralMessagesAboutOneMatterProduceOneTicketNotOneEach(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	msgs := oneMatter()
	fac, _ := fixed(msgs...)

	res, err := Ingest(s, fac, day.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ticketsIn(t, s)); got != 1 {
		t.Fatalf("%d messages about one matter produced %d tickets; criterion 8 says one matter is "+
			"one ticket, and one ticket per message is the worse email client the Issue names", len(msgs), got)
	}
	// THE TWO NUMBERS ARE SEPARATELY OBSERVABLE, which is the other half of criterion 8.
	if res.Messages != len(msgs) {
		t.Errorf("the run reports %d messages; it saw %d", res.Messages, len(msgs))
	}
	if res.Tickets != 1 {
		t.Errorf("the run reports %d tickets; it wrote 1", res.Tickets)
	}
	if res.Messages == res.Tickets {
		t.Errorf("the message count and the ticket count are the same number (%d), so a person "+
			"cannot see that ingestion is not a mirror of the traffic", res.Messages)
	}
	// And the channel record carries both, so `omw channels list` can show them.
	c, err := Get(s, "email-a")
	if err != nil {
		t.Fatal(err)
	}
	if c.Last.Messages != len(msgs) || c.Last.Tickets != 1 {
		t.Errorf("the channel records %d messages and %d tickets; want %d and 1",
			c.Last.Messages, c.Last.Tickets, len(msgs))
	}
}

func TestNoTicketCarriesAMessageBodyOrASubjectLineVerbatim(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	msgs := oneMatter()
	fac, _ := fixed(msgs...)
	if _, err := Ingest(s, fac, day); err != nil {
		t.Fatal(err)
	}
	tickets := ticketsIn(t, s)
	if len(tickets) != 1 {
		t.Fatalf("want one ticket, got %d", len(tickets))
	}
	title, _ := tickets[0].Title.Value()
	summary, _ := tickets[0].Summary.Value()
	if strings.TrimSpace(title) == "" || strings.TrimSpace(summary) == "" {
		t.Fatalf("criterion 8 requires a WRITTEN title and summary; got title %q summary %q", title, summary)
	}
	for _, m := range msgs {
		if title == m.Subject {
			t.Errorf("the ticket's title is message %s's subject line verbatim (%q) — criterion 8 "+
				"says written, not copied", m.ID, title)
		}
		if title == m.Body || summary == m.Body {
			t.Errorf("message %s's body was copied into the ticket verbatim", m.ID)
		}
		// The whole point of "the messages are not stored": no body text appears anywhere in the
		// ticket, so there is no second copy of the person's inbox on disk (§2.3, criterion 14).
		//
		// ONLY BODIES LONG ENOUGH TO BE EVIDENCE. "ok" is a substring of "broken" and of a dozen
		// ordinary words, so a containment check on a two-letter acknowledgement fails on prose
		// that copied nothing — a false red, which trains a reader to ignore this test. The
		// verbatim-equality checks above still cover the short ones.
		if len(m.Body) < 12 {
			continue
		}
		if strings.Contains(summary, m.Body) || strings.Contains(title, m.Body) {
			t.Errorf("message %s's body text appears inside the ticket: %q", m.ID, m.Body)
		}
	}
	// It IS about the matter: the people waiting and the count are in it.
	for _, want := range []string{"ana@example.com", "6 messages"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the written summary does not mention %q; it reads: %s", want, summary)
		}
	}
}

func TestASecondPassOverTheSameConversationDoesNotAddASecondTicket(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	fac, _ := fixed(oneMatter()...)
	for i := 0; i < 3; i++ {
		if _, err := Ingest(s, fac, day.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(ticketsIn(t, s)); got != 1 {
		t.Fatalf("three passes over one conversation left %d tickets; continuous ingestion that "+
			"accumulates a ticket per pass is the firehose criterion 8 forbids", got)
	}
}

// =================================================================================================
// CRITERION 9 — acknowledgements produce NOTHING. Negative and load-bearing.
// =================================================================================================

func TestAcknowledgementsAndSmallTalkProduceZeroTicketsAtAnyPriority(t *testing.T) {
	s := newStore(t)
	connected(t, s, "teams-a", KindTeams)
	// Eleven people saying nothing, on a channel with no subject lines — the Issue's own example.
	var msgs []Message
	for i, body := range []string{"ok", "thanks", "Hii", "OK!", "ty", "np", "👍 ok", "cheers", "yes", "got it", "+1"} {
		msgs = append(msgs, Message{
			ID: fmt.Sprintf("m%d", i), From: fmt.Sprintf("p%d@example.com", i),
			Body: body, At: day.Add(time.Duration(i) * time.Minute),
		})
	}
	fac, _ := fixed(msgs...)
	res, err := Ingest(s, fac, day)
	if err != nil {
		t.Fatal(err)
	}
	if res.Messages != len(msgs) {
		t.Errorf("the run saw %d messages, reported %d", len(msgs), res.Messages)
	}
	// ZERO. Not zero-at-the-top and eleven-at-the-bottom; zero.
	if res.Tickets != 0 {
		t.Errorf("acknowledgement traffic produced %d tickets", res.Tickets)
	}
	got := ticketsIn(t, s)
	if len(got) != 0 {
		for _, tk := range got {
			title, _ := tk.Title.Value()
			t.Errorf("acknowledgement traffic created ticket %q titled %q. PRD §3.2: these are not "+
				"low-priority tickets, they are not tickets — and there is no priority to put one at.",
				tk.ID, title)
		}
	}
}

// =================================================================================================
// CRITERION 10 — unreachable is not empty.
// =================================================================================================

func TestAnUnreachableChannelRendersDifferentlyFromAnEmptyOne(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-empty", KindEmail)
	connected(t, s, "teams-broken", KindTeams)

	fac := func(c Connection) (Adapter, error) {
		return AdapterFunc(func(c Connection) ([]Message, error) {
			if c.ID == "teams-broken" {
				return nil, fmt.Errorf("%w: the credential was rejected by the service", ErrUnreachable)
			}
			return nil, nil
		}), nil
	}
	res, err := Ingest(s, fac, day)
	if err != nil {
		t.Fatal(err)
	}
	// BOTH PRODUCE ZERO TICKETS. That is the premise, not the finding.
	if res.Tickets != 0 {
		t.Fatalf("want zero tickets from both, got %d", res.Tickets)
	}
	empty, err := Get(s, "email-empty")
	if err != nil {
		t.Fatal(err)
	}
	broken, err := Get(s, "teams-broken")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Last.Outcome == broken.Last.Outcome {
		t.Fatalf("a channel that was reached and had nothing and a channel that could not be "+
			"reached are both %v; criterion 10 says these are different facts", empty.Last.Outcome)
	}
	// AND THEY RENDER DIFFERENTLY, compared against each other rather than against literals.
	er := empty.Render(day, tri.Yes)
	br := broken.Render(day, tri.Yes)
	if er == br {
		t.Fatalf("the two render identically:\n%s", er)
	}
	if !strings.Contains(strings.ToLower(br), "could not be reached") {
		t.Errorf("the unreachable channel's rendering does not say it could not be reached:\n%s", br)
	}
	// The reached-and-empty channel has a real last-success time; the unreachable one does not
	// get one, because it did not succeed.
	if empty.Last.State != tri.Yes {
		t.Errorf("a channel that was reached and found nothing did not record a successful ingestion")
	}
	if broken.Last.State != tri.No {
		t.Errorf("a channel that could not be reached recorded a successful ingestion (%v)", broken.Last.State)
	}
}

func TestAFailedChannelDoesNotStopTheOthersBeingIngested(t *testing.T) {
	s := newStore(t)
	connected(t, s, "aaa-broken", KindTeams)
	connected(t, s, "zzz-working", KindEmail)
	fac := func(c Connection) (Adapter, error) {
		if c.ID == "aaa-broken" {
			return nil, fmt.Errorf("%w: no route to the service", ErrUnreachable)
		}
		return AdapterFunc(func(Connection) ([]Message, error) { return oneMatter(), nil }), nil
	}
	res, err := Ingest(s, fac, day)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tickets != 1 {
		t.Fatalf("the working channel produced %d tickets after the first channel failed; want 1", res.Tickets)
	}
}

// =================================================================================================
// CRITERION 11 — no network without cause.
// =================================================================================================

func TestIngestionWithNoChannelConnectedBuildsNoAdapterAtAll(t *testing.T) {
	s := newStore(t)
	built := 0
	fac := func(c Connection) (Adapter, error) {
		built++
		return nil, errors.New("this must never be called")
	}
	res, err := Ingest(s, fac, day)
	if err != nil {
		t.Fatal(err)
	}
	// COUNTED, NOT OBSERVED. "We watched and saw no traffic" also passes on a build that dials
	// sometimes; "nothing that could dial was constructed" does not.
	if built != 0 || res.AdaptersBuilt != 0 {
		t.Fatalf("with no channel connected, ingestion constructed %d adapter(s) — criterion 11 says "+
			"connecting a channel is the only thing that authorises reaching a service", built)
	}
	if len(res.Channels) != 0 || res.Messages != 0 || res.Tickets != 0 {
		t.Errorf("a run with nothing connected reported %+v", res)
	}
}

// =================================================================================================
// CRITERION 13 — an explicit act, and three distinguishable connection states.
// =================================================================================================

func TestConnectingRefusesWhenNoCredentialWasSupplied(t *testing.T) {
	s := newStore(t)
	err := Connect(s, Connection{ID: "email-a", Kind: KindEmail, Account: "a@example.com"})
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("connecting with no credential returned %v; criterion 13 says no credential is "+
			"obtained on the person's behalf, so a connect without one is refused", err)
	}
	if _, gerr := Get(s, "email-a"); !errors.Is(gerr, ErrNoSuchChannel) {
		t.Errorf("a refused connect left a channel behind: %v", gerr)
	}
}

func TestTheFourConnectionStatesRenderPairwiseDistinctly(t *testing.T) {
	states := map[string]Health{
		"connected and healthy": HealthConnected,
		"credential expired":    HealthCredentialExpired,
		"disconnected":          HealthDisconnected,
		"undetermined":          HealthUndetermined,
	}
	for a, ha := range states {
		if strings.TrimSpace(ha.String()) == "" {
			t.Errorf("%s renders as silence", a)
		}
		for b, hb := range states {
			if a < b && ha.String() == hb.String() {
				t.Errorf("%q and %q render identically as %q — criterion 13 requires an expired "+
					"credential be a distinct state from disconnected AND from connected-and-healthy",
					a, b, ha)
			}
		}
	}
}

func TestAnExpiredCredentialIsNotAttemptedAndIsReportedAsExpired(t *testing.T) {
	s := newStore(t)
	c := Connection{
		ID: "email-a", Kind: KindEmail, Account: "a@example.com", ConnectedAt: day,
		Credential: "stale", CredentialExpiresAt: day.Add(time.Hour),
	}
	if err := Connect(s, c); err != nil {
		t.Fatal(err)
	}
	after := day.Add(2 * time.Hour)
	if h, _ := c.Health(after); h != HealthCredentialExpired {
		t.Fatalf("an expired credential reports as %v", h)
	}
	built := 0
	fac := func(Connection) (Adapter, error) {
		built++
		return AdapterFunc(func(Connection) ([]Message, error) { return oneMatter(), nil }), nil
	}
	res, err := Ingest(s, fac, after)
	if err != nil {
		t.Fatal(err)
	}
	if built != 0 {
		t.Errorf("ingestion reached out with a credential it knew had expired")
	}
	if res.Tickets != 0 {
		t.Errorf("an expired channel produced %d tickets", res.Tickets)
	}
	got, err := Get(s, "email-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Last.Outcome != OutcomeUnreachable {
		t.Errorf("an expired channel's last attempt is %v; it must not read as reached-and-empty", got.Last.Outcome)
	}
}

func TestACredentialThatDoesNotSayWhenItExpiresIsUndeterminedNotHealthy(t *testing.T) {
	c := Connection{ID: "a", Kind: KindEmail, Account: "a@example.com", Credential: "t"}
	h, why := c.Health(day)
	if h != HealthUndetermined {
		t.Fatalf("a credential with no stated expiry reports as %v; a (bool, error) with the error "+
			"dropped is exactly this shape", h)
	}
	if why == "" {
		t.Error("the undetermined state is rendered without saying why")
	}
}

// =================================================================================================
// CRITERION 14 — ingested material is local, and the credential is never rendered.
// =================================================================================================

func TestNothingRendersTheCredential(t *testing.T) {
	s := newStore(t)
	c := connected(t, s, "email-a", KindEmail)
	got, err := Get(s, "email-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{
		got.Render(day, tri.Yes),
		got.Render(day, tri.No),
		got.Render(day, tri.Undetermined),
		got.Last.Render(), got.Last.RenderOutcome(), got.Kind.String(),
	} {
		if strings.Contains(rendered, c.Credential) {
			t.Errorf("a rendering contains the credential %q:\n%s", c.Credential, rendered)
		}
	}
}

// =================================================================================================
// CRITERION 6 — a stale last-ingestion time is never presented as current.
// =================================================================================================

func TestAStaleIngestionTimeIsNeverPresentedAsCurrent(t *testing.T) {
	i := Ingestion{State: tri.Yes, At: day}
	running := i.RenderAsOf(tri.Yes)
	stopped := i.RenderAsOf(tri.No)
	unknown := i.RenderAsOf(tri.Undetermined)
	if running == stopped {
		t.Fatalf("the same timestamp renders identically whether or not ingestion is running: %q", running)
	}
	if stopped == unknown {
		t.Fatalf("'ingestion is stopped' and 'whether ingestion is running could not be determined' "+
			"render the same: %q", stopped)
	}
	if !strings.Contains(stopped, "NOT CURRENT") {
		t.Errorf("with ingestion stopped the timestamp does not say it is not current: %q", stopped)
	}
}
