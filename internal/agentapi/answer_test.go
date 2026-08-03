package agentapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

const (
	me        = hub.PersonID("dana")
	colleague = hub.PersonID("sam")
)

type fixture struct {
	t      *testing.T
	store  *store.Store
	outbox *drafts.Outbox
	hub    *hub.Store
	src    Sources
	grant  hub.Grant
}

// newFixture builds a machine: a real local store, a real outbox, a real in-memory hub, and a real
// grant ledger. Nothing here is a double of a thing this Issue is asserting about — the doubles are
// only for the hub's TRANSPORT, which this build does not have.
func newFixture(t *testing.T, personScopes ...hub.Scope) *fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("could not create a store to test against: %v", err)
	}
	o, err := drafts.Create(filepath.Join(root, "outbox"))
	if err != nil {
		t.Fatalf("could not create an outbox: %v", err)
	}
	members := hub.NewRecord()
	members.AddPerson(me)
	members.AddPerson(colleague)
	h := hub.NewStore(members)
	if len(personScopes) == 0 {
		personScopes = []hub.Scope{hub.ScopeRead}
	}
	f := &fixture{t: t, store: s, outbox: o, hub: h}
	f.src = Sources{
		Person:       me,
		PersonScopes: personScopes,
		Grants:       NewStoreGrants(s),
		Tickets:      func() ([]inbox.Ticket, error) { return inbox.List(s) },
		Drafts:       func() ([]DraftView, error) { return f.listDrafts() },
		ReviseDraft:  func(id, body string) (DraftView, error) { return f.revise(id, body) },
		Hub:          func() (*hub.Store, hub.Membership, error) { return h, members, nil },
		// THE FIXTURE'S STORE IS A REAL ONE, AND THE MODEL SOURCE READS IT. It used to pass nil
		// here, which model.Read documents as "this caller has no store" — so no test in this
		// package exercised the model against a store at all, and a daemon seam that discarded its
		// store went unnoticed on this side of the boundary too. An empty real store answers the
		// same "no provider is chosen" these tests already expect; what changes is that the store
		// is now on the path.
		Model: func() model.Config { return model.Read(func(string) string { return "" }, s) },
	}
	return f
}

func (f *fixture) listDrafts() ([]DraftView, error) {
	ids, err := f.outbox.Drafts()
	if err != nil {
		return nil, err
	}
	out := make([]DraftView, 0, len(ids))
	for _, id := range ids {
		d := DraftView{ID: string(id), State: DraftedState}
		if vs, verr := f.outbox.Timeline(id, ""); verr == nil {
			d.Revisions = len(vs)
			d.Latest = vs[len(vs)-1].Body
		}
		out = append(out, d)
	}
	return out, nil
}

func (f *fixture) revise(id, body string) (DraftView, error) {
	if _, err := f.outbox.Revise(hub.NoteID(id), body); err != nil {
		return DraftView{}, err
	}
	vs, err := f.outbox.Timeline(hub.NoteID(id), "")
	if err != nil {
		return DraftView{}, err
	}
	return DraftView{ID: id, State: DraftedState, Revisions: len(vs), Latest: vs[len(vs)-1].Body}, nil
}

// issue asks for a grant the way an agent does, through the surface under test.
func (f *fixture) issue(scopes ...hub.Scope) hub.Grant {
	f.t.Helper()
	r := Answer(Request{Op: OpGrant, Scopes: scopes}, f.src)
	if r.Outcome != OutcomeOK {
		f.t.Fatalf("issuing a grant for %v was %s (%s): %s", scopes, r.Outcome, r.Code, r.Message)
	}
	f.grant = hub.Grant{ID: hub.GrantID(r.Grant.ID), Holder: me}
	return f.grant
}

func (f *fixture) putTicket(id, title string) {
	f.t.Helper()
	err := inbox.Put(f.store, inbox.Ticket{
		ID: id, Title: inbox.Text(title), Summary: inbox.Text("s"),
		Channel: inbox.Text("teams"), Arrived: time.Unix(1700000000, 0).UTC(),
	})
	if err != nil {
		f.t.Fatalf("staging ticket %q: %v", id, err)
	}
}

func (f *fixture) publish(author hub.PersonID, title string, v hub.Visibility) hub.NoteID {
	f.t.Helper()
	n, err := f.hub.Publish(hub.Publication{Author: author, Title: title, Body: title + " body", Visibility: v})
	if err != nil {
		f.t.Fatalf("staging note %q: %v", title, err)
	}
	return n.ID
}

// ---------------------------------------------------------------------------
// Criterion 1, 2 — the person's own material
// ---------------------------------------------------------------------------

func TestTheAgentAPIServesThePersonsTicketsAndDrafts(t *testing.T) {
	f := newFixture(t)
	g := f.issue(hub.ScopeRead)
	f.putTicket("t-1", "the quota question")
	if _, err := f.outbox.Revise("d-1", "half a thought"); err != nil {
		t.Fatal(err)
	}

	r := Answer(Request{Op: OpTickets, Grant: g.ID}, f.src)
	if r.Outcome != OutcomeOK || len(r.Tickets) != 1 || r.Tickets[0].ID != "t-1" {
		t.Fatalf("criterion 1: the agent API did not serve the person's ticket: %+v", r)
	}
	// THE SAME TICKETS THE CLI READS, because it is the same call. This is asserted end-to-end
	// against `omw inbox list` over a real socket in
	// TestTheAgentAPIAndTheCLIAnswerWithTheSameTicketsAndDrafts (package commands); here the point
	// is that inbox.List is what was called at all.
	viaInbox, err := inbox.List(f.store)
	if err != nil || len(viaInbox) != len(r.Tickets) {
		t.Fatalf("the agent API returned %d ticket(s) and inbox.List returned %d", len(r.Tickets), len(viaInbox))
	}

	d := Answer(Request{Op: OpDrafts, Grant: g.ID}, f.src)
	if d.Outcome != OutcomeOK || len(d.Drafts) != 1 {
		t.Fatalf("criterion 2: the agent API did not serve the person's draft: %+v", d)
	}
	// CRITERION 2: identifiable as UNPUBLISHED, and not by the reader inferring it.
	if d.Drafts[0].State != DraftedState || d.Drafts[0].Published {
		t.Errorf("criterion 2: draft %q is served as state %q published=%t; a draft in the outbox is drafted "+
			"and unpublished (PRD §3.11), and a reader must not have to infer which",
			d.Drafts[0].ID, d.Drafts[0].State, d.Drafts[0].Published)
	}
}

// ---------------------------------------------------------------------------
// Criteria 3 and 5 — scoped to that person and nothing wider
// ---------------------------------------------------------------------------

func TestANoteRestrictedAwayIsAbsentFromTheAgentAPIEntirely(t *testing.T) {
	f := newFixture(t)
	g := f.issue(hub.ScopeRead)
	mine := f.publish(me, "my own note", hub.CompanyWide())
	theirs := f.publish(colleague, "SECRET-TITLE", mustPeople(t, colleague))

	r := Answer(Request{Op: OpHub, Grant: g.ID}, f.src)
	if r.Outcome != OutcomeOK {
		t.Fatalf("criterion 3: a hub read was %s (%s): %s", r.Outcome, r.Code, r.Message)
	}
	if len(r.Notes) != 1 || r.Notes[0].ID != string(mine) {
		t.Fatalf("criterion 3: want exactly the one note this person may read, got %+v", r.Notes)
	}
	// CRITERION 5: not its body, not its title, NOT AN IDENTIFIER, anywhere in the response.
	body, err := MarshalResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"SECRET-TITLE", string(theirs), "SECRET-TITLE body"} {
		if containsBytes(body, forbidden) {
			t.Errorf("criterion 5: the serialised response contains %q, which belongs to a note this "+
				"person cannot read:\n%s", forbidden, body)
		}
	}

	// AND READING IT DIRECTLY IS REFUSED, DISTINGUISHABLY FROM "no such note" (#12 criterion 12).
	refused := Answer(Request{Op: OpNote, Grant: g.ID, NoteID: string(theirs)}, f.src)
	absent := Answer(Request{Op: OpNote, Grant: g.ID, NoteID: "note-that-does-not-exist"}, f.src)
	if refused.Code != hub.ErrRefused.Code {
		t.Errorf("reading a colleague's restricted note answered code %q, want %q", refused.Code, hub.ErrRefused.Code)
	}
	if absent.Code != hub.ErrNoSuchNote.Code {
		t.Errorf("reading a nonexistent note answered code %q, want %q", absent.Code, hub.ErrNoSuchNote.Code)
	}
	if refused.Code == absent.Code {
		t.Error("criterion 12: a refusal and a missing note share a code, so a caller cannot tell them apart")
	}
	if refused.Note != nil || absent.Note != nil {
		t.Error("a refused or absent note read returned a note anyway")
	}
	if bodyOf(t, refused) == bodyOf(t, absent) {
		t.Error("the refusal and the missing-note answer serialise identically")
	}
}

// TestTwoHubsDifferingOnlyByARestrictedNoteProduceIdenticalOutput is criterion 5's hardest clause,
// and the one an eyeball cannot check: "not a count that changes when such a note exists versus
// when it does not. Two hubs identical except for one restricted-away note must produce
// byte-identical agent API output for that person."
//
// # It compares bytes, not fields
//
// A field-by-field comparison passes while a new field somebody adds next month leaks the
// difference. The serialised form is what actually reaches the AI, so that is what is compared.
//
// # Why the note ids are normalised, and why that is not a weakening
//
// Note ids used to be `note-%d` from a shared counter, and this test was the thing that would have
// caught what that cost: an unreadable note consumed an id, so the reader's second visible note was
// `note-3` rather than `note-2`, and the gap disclosed the existence of a note they could not read.
// The identifier was a function of the reader's own blind spots. That is fixed on main — ids are
// now minted from crypto/rand — and the fix has a consequence for this test: two runs of an
// IDENTICAL corpus now differ byte-wise, so a raw comparison measures the random number generator
// rather than any information.
//
// So the ids are replaced by the order in which they first appear. What survives normalisation is
// everything criterion 5 is actually about — WHICH notes, HOW MANY, in what ORDER, with which
// fields — and what is removed is only the randomness. A leaked note still adds a token and still
// fails.
//
// # Two corpora, and both controls
//
// A single corpus cannot tell "ids do not leak" from "these particular ids happened to line up", so
// the whole comparison runs over two different corpora. And two controls run alongside, because a
// normalisation that erased everything would pass the main assertion silently:
//
//   - CONTROL A: two IDENTICAL corpora must normalise identical. If they do not, the normalisation
//     is not removing the randomness and every pass below is luck.
//   - CONTROL B: a corpus with one extra READABLE note must NOT normalise identical. If it does,
//     the normalisation has flattened the response and the main assertion proves nothing.
//   - CONTROL C, in TestNormalisingNoteIDsKeepsWhatCriterion5IsAbout: the normalisation itself maps
//     DISTINCT ids to DISTINCT tokens. This one is separate because it is the sharp one, and I
//     found that out by driving it: a normalisation collapsing every id to a single token leaves
//     CONTROL B GREEN, because an extra readable note also differs by its title. CONTROL B proves
//     the response is still live; only CONTROL C proves the ids were not flattened. Recording that
//     here because a control believed to be stronger than it is would be worse than none.
//
// # And it runs over every operation, not only the hub one
//
// Criterion 5 says "in any form". `tickets`, `drafts` and `model` do not consult the hub at all, so
// a difference there would mean a restricted note had reached somewhere it has no business being.
func TestTwoHubsDifferingOnlyByARestrictedNoteProduceIdenticalOutput(t *testing.T) {
	// corpus is the readable material both sides of a comparison share.
	type corpus struct {
		name  string
		notes []string
	}
	corpora := []corpus{
		{"one readable note", []string{"public one"}},
		{"three readable notes", []string{"public one", "public two", "public three"}},
	}

	// build serves one operation against a hub holding the corpus, plus optionally one note
	// restricted away from this person, plus optionally one extra readable note (control B).
	build := func(t *testing.T, c corpus, op Op, withRestricted, withExtraReadable bool) []byte {
		t.Helper()
		f := newFixture(t)
		g := f.issue(hub.ScopeRead)
		f.putTicket("t-1", "a ticket")
		if _, err := f.outbox.Revise("d-1", "a draft"); err != nil {
			t.Fatal(err)
		}
		for _, title := range c.notes {
			f.publish(me, title, hub.CompanyWide())
			if withRestricted {
				// INTERLEAVED, not appended. A restricted note published between two readable ones
				// is what used to shift the ids of everything after it; publishing them all at the
				// end would have missed exactly the defect this test is for.
				f.publish(colleague, "ZARQUON restricted away", mustPeople(t, colleague))
			}
		}
		if withExtraReadable {
			f.publish(me, "an extra readable note", hub.CompanyWide())
		}
		r := Answer(Request{Op: op, Grant: g.ID}, f.src)
		// The grant id and the note ids are minted from crypto/rand and differ between the two
		// fixtures by construction. Person is blanked and ids are normalised; nothing else is.
		r.Person = ""
		b, err := MarshalResponse(r)
		if err != nil {
			t.Fatal(err)
		}
		return normaliseNoteIDs(b)
	}

	for _, c := range corpora {
		for _, op := range []Op{OpHub, OpTickets, OpDrafts, OpModel} {
			name := c.name + "/" + string(op)

			// --- CONTROL A: identical corpora normalise identical. -----------------
			if a, b := build(t, c, op, false, false), build(t, c, op, false, false); string(a) != string(b) {
				t.Fatalf("%s: CONTROL A failed — two IDENTICAL corpora do not normalise identical, so the "+
					"normalisation is not removing crypto/rand and nothing below this line proves anything.\n"+
					"  run 1: %s\n  run 2: %s", name, a, b)
			}

			// --- CONTROL B: an extra READABLE note must still show. ----------------
			if a, b := build(t, c, op, false, false), build(t, c, op, false, true); op == OpHub && string(a) == string(b) {
				t.Fatalf("%s: CONTROL B failed — adding a note this person CAN read changed nothing, so the "+
					"normalisation has erased the signal and the assertion below is vacuous.\n"+
					"  without: %s\n  with:    %s", name, a, b)
			}

			// --- THE ASSERTION. -----------------------------------------------------
			with, without := build(t, c, op, true, false), build(t, c, op, false, false)
			if string(with) != string(without) {
				t.Errorf("criterion 5 (%s): a note restricted away from this person changes the agent API's "+
					"output.\n  with it:    %s\n  without it: %s\n"+
					"  The existence of a note the person cannot read is itself something they cannot read "+
					"(PRD §3.5), and that includes the gaps it leaves in what they can.", name, with, without)
			}
			// AND NO FRAGMENT OF IT, in the un-normalised bytes.
			raw := build(t, c, op, true, false)
			if containsBytes(raw, "ZARQUON") {
				t.Errorf("criterion 5 (%s): the restricted note's title reached the response:\n%s", name, raw)
			}
		}
	}
}

// normaliseNoteIDs replaces every minted note id with the order in which it first appears.
//
// IT IS DELIBERATELY NOT A BLANKET REDACTION. `note-<32 hex>` and nothing else is matched, and each
// DISTINCT id gets a DISTINCT token, so the count of ids, their order, and which fields refer to
// which all survive. Replacing them all with one constant would pass this test with a leak present,
// which is the failure mode CONTROL B exists to catch.
func normaliseNoteIDs(b []byte) []byte {
	seen := map[string]string{}
	return noteIDPattern.ReplaceAllFunc(b, func(m []byte) []byte {
		id := string(m)
		if tok, ok := seen[id]; ok {
			return []byte(tok)
		}
		tok := "note-#" + strconv.Itoa(len(seen))
		seen[id] = tok
		return []byte(tok)
	})
}

// TestNormalisingNoteIDsKeepsWhatCriterion5IsAbout is CONTROL C, and it is the control that a
// blanket redaction actually fails.
//
// The property the byte comparison rests on: how many distinct note ids a response mentions, and in
// what order, survives normalisation. Only the 128 random bits are removed. A normalisation that
// mapped every id to one token would satisfy the whole test above — driven and confirmed — and
// would then also pass with a leaked note present.
func TestNormalisingNoteIDsKeepsWhatCriterion5IsAbout(t *testing.T) {
	const a = "note-" + "00112233445566778899aabbccddeeff"
	const b = "note-" + "ffeeddccbbaa99887766554433221100"
	const c = "note-" + "0123456789abcdef0123456789abcdef"

	got := string(normaliseNoteIDs([]byte(`{"x":"` + a + `","y":"` + b + `","z":"` + a + `","w":"` + c + `"}`)))

	// THREE DISTINCT IDS MUST STAY THREE DISTINCT TOKENS.
	tokens := map[string]bool{}
	for _, m := range regexp.MustCompile(`note-#\d+`).FindAllString(got, -1) {
		tokens[m] = true
	}
	if len(tokens) != 3 {
		t.Errorf("three distinct note ids normalised to %d distinct token(s): %s\n"+
			"  Collapsing them would make the criterion 5 comparison pass with a leak present.", len(tokens), got)
	}
	// THE REPEATED ID MUST STAY THE SAME TOKEN, so "which field refers to which note" survives.
	if x, z := fieldOf(got, "x"), fieldOf(got, "z"); x != z {
		t.Errorf("the same id normalised to %q and %q; a response's internal references are part of "+
			"what the comparison must see", x, z)
	}
	if x, y := fieldOf(got, "x"), fieldOf(got, "y"); x == y {
		t.Errorf("two different ids both normalised to %q", x)
	}
	// FIRST-APPEARANCE ORDER, not hash order, so the token sequence is stable across runs.
	if fieldOf(got, "x") != "note-#0" || fieldOf(got, "y") != "note-#1" || fieldOf(got, "w") != "note-#2" {
		t.Errorf("tokens are not assigned in first-appearance order: %s", got)
	}
	// AND NOTHING ELSE IS TOUCHED. A blanket redaction over the whole payload would erase the
	// titles and bodies the comparison also depends on.
	if !strings.Contains(got, `"x":`) || strings.Contains(got, a) {
		t.Errorf("the normalisation changed something other than the ids, or missed one: %s", got)
	}
}

// fieldOf reads back one string field from the tiny JSON above.
func fieldOf(s, key string) string {
	var m map[string]string
	if json.Unmarshal([]byte(s), &m) != nil {
		return ""
	}
	return m[key]
}

// noteIDPattern matches an id as internal/hub mints it: the `note-` prefix and 128 bits of hex.
// The length is pinned so that a future shorter or non-hex id fails to normalise and shows up as a
// CONTROL A failure, rather than being silently left in place.
var noteIDPattern = regexp.MustCompile(`note-[0-9a-f]{32}`)

// ---------------------------------------------------------------------------
// Criteria 6, 8 — an agent's token can never exceed its person's
// ---------------------------------------------------------------------------

func TestAScopeThePersonDoesNotHoldIsRefusedWhenItIsRequested(t *testing.T) {
	// The person holds `read` alone — Issue #16's `## Ruled`: everyday use is `read`, and `publish`
	// is asked for on purpose.
	f := newFixture(t, hub.ScopeRead)

	r := Answer(Request{Op: OpGrant, Scopes: []hub.Scope{hub.ScopeRead, hub.ScopePublish}}, f.src)
	if r.Outcome != OutcomeRefused || r.Code != hub.ErrGrantWiderThanHolder.Code {
		t.Fatalf("criterion 6: asking for a grant wider than its holder was %s (%s), want refused/%s",
			r.Outcome, r.Code, hub.ErrGrantWiderThanHolder.Code)
	}
	// NOT NARROWED AT THE EDGE (§4.5). No token at all, not a `read` one.
	if r.Grant != nil {
		t.Errorf("criterion 6: a refused grant request issued %+v anyway; §4.5 says it is refused, "+
			"not narrowed to what the holder can do", r.Grant)
	}
	// DISTINGUISHABLE FROM AN EMPTY SUCCESSFUL RESULT BY THE RESPONSE ITSELF.
	ok := Answer(Request{Op: OpGrant, Scopes: []hub.Scope{hub.ScopeRead}}, f.src)
	if ok.Outcome == r.Outcome || ok.Outcome.Exit() == r.Outcome.Exit() {
		t.Error("criterion 6: a refusal and a success share an outcome or an exit code")
	}
}

func TestPresentingAScopeTheAgentWasNotGrantedIsRefusedAtRequestTime(t *testing.T) {
	// The PERSON holds read and write; the AGENT was granted read alone. Criterion 6 names both
	// halves, and this is the second: "or that was not granted to the agent".
	f := newFixture(t, hub.ScopeRead, hub.ScopeWrite)
	g := f.issue(hub.ScopeRead)

	r := Answer(Request{Op: OpTickets, Grant: g.ID, Scopes: []hub.Scope{hub.ScopeWrite}}, f.src)
	if r.Outcome != OutcomeRefused || r.Code != ErrScopeNotGranted.Code {
		t.Fatalf("presenting an ungranted scope was %s (%s), want refused/%s", r.Outcome, r.Code, ErrScopeNotGranted.Code)
	}
	if len(r.Tickets) != 0 {
		t.Error("the refused request served tickets anyway; the authority check must come first")
	}
	if r.Code == hub.ErrGrantWiderThanHolder.Code {
		t.Error("criterion 6: 'the person does not hold it' and 'the agent was not granted it' share a code; " +
			"the first is not fixable and the second is fixed by asking for a wider grant")
	}
}

// TestTheReadScopeConfersNeitherWriteNorPublish is criterion 8, and it asserts the WORLD as well as
// the answer: "the draft is unchanged and nothing appears on the hub".
func TestTheReadScopeConfersNeitherWriteNorPublish(t *testing.T) {
	f := newFixture(t, hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish)
	readOnly := f.issue(hub.ScopeRead)
	if _, err := f.outbox.Revise("d-1", "the original text"); err != nil {
		t.Fatal(err)
	}
	notesBefore := f.hub.Count()

	write := Answer(Request{Op: OpDraftWrite, Grant: readOnly.ID, NoteID: "d-1", Body: "OVERWRITTEN"}, f.src)
	if write.Outcome != OutcomeRefused || write.Code != ErrWriteScopeRequired.Code {
		t.Errorf("criterion 8: a read-scoped grant writing a draft was %s (%s), want refused/%s",
			write.Outcome, write.Code, ErrWriteScopeRequired.Code)
	}
	// THE DRAFT IS UNCHANGED. A refusal that has already written is a refusal in name only.
	vs, err := f.outbox.Timeline("d-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Body != "the original text" {
		t.Errorf("criterion 8: the refused write changed the draft: %+v", vs)
	}

	pub := Answer(Request{Op: OpPublish, Grant: readOnly.ID, Title: "t", Body: "b"}, f.src)
	if pub.Outcome != OutcomeRefused || pub.Code != hub.ErrPublishScopeRequired.Code {
		t.Errorf("criterion 8: a read-scoped grant publishing was %s (%s), want refused/%s",
			pub.Outcome, pub.Code, hub.ErrPublishScopeRequired.Code)
	}
	// NOTHING APPEARS ON THE HUB.
	if f.hub.Count() != notesBefore {
		t.Errorf("criterion 8: the refused publish put a note on the hub (%d -> %d)", notesBefore, f.hub.Count())
	}

	// CRITERION 8's LAST CLAUSE: each refusal is distinguishable from an empty result and from a
	// failure to reach what the operation needs — criterion 7's three outcomes, for both refusals.
	//
	// THE THIRD ANSWER IS TAKEN WITH A GRANT THAT CARRIES THE SCOPE, which is the only way to
	// obtain it: with a read-only grant the authority check comes first and correctly refuses
	// before anything is reached for, so comparing against that would compare the refusal with
	// itself and pass vacuously. An earlier revision of this test did exactly that and had to be
	// driven into failing before it said anything.
	adequate := f.issue(hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish)

	// `draft.write` never touches a hub, so its "could not reach what it needed" is the LOCAL one:
	// an outbox that could not be written.
	brokenLocal := f.src
	brokenLocal.ReviseDraft = func(string, string) (DraftView, error) {
		return DraftView{}, errors.New("the outbox could not be written")
	}
	assertThreeOutcomesAreDistinguishable(t, "draft.write", write,
		Answer(Request{Op: OpDrafts, Grant: readOnly.ID}, f.src),
		Answer(Request{Op: OpDraftWrite, Grant: adequate.ID, NoteID: "d-1", Body: "x"}, brokenLocal))

	assertThreeOutcomesAreDistinguishable(t, "publish", pub,
		Answer(Request{Op: OpHub, Grant: readOnly.ID}, f.src),
		unreachableAnswer(t, f, OpPublish, adequate.ID))
}

// unreachableAnswer re-asks with the hub failing the way a transport will.
func unreachableAnswer(t *testing.T, f *fixture, op Op, g hub.GrantID) Response {
	t.Helper()
	src := f.src
	src.Hub = func() (*hub.Store, hub.Membership, error) { return nil, nil, hub.ErrHubUnreachable }
	return Answer(Request{Op: op, Grant: g, NoteID: "d-1", Title: "t", Body: "b"}, src)
}

// assertThreeOutcomesAreDistinguishable is criterion 7, factored so criterion 8 can invoke it too:
// a refusal, an empty successful result and an unreachable hub are three distinguishable outputs.
func assertThreeOutcomesAreDistinguishable(t *testing.T, what string, refused, empty, unreachable Response) {
	t.Helper()
	trio := map[string]Response{"refused": refused, "empty": empty, "unreachable": unreachable}
	seenCode := map[string]string{}
	for name, r := range trio {
		key := string(r.Outcome) + "/" + r.Code
		if other, dup := seenCode[key]; dup {
			t.Errorf("criterion 7 (%s): %q and %q both answer %s — three outcomes, three outputs",
				what, name, other, key)
		}
		seenCode[key] = name
	}
	if unreachable.Outcome != OutcomeUndetermined {
		t.Errorf("criterion 7 (%s): an unreachable hub answered %s; a hub that cannot be reached is not a "+
			"determined anything (PRD §3.11)", what, unreachable.Outcome)
	}
	if unreachable.Outcome.Exit() == refused.Outcome.Exit() {
		t.Errorf("criterion 7 (%s): an unreachable hub and a refusal share exit code %d",
			what, refused.Outcome.Exit())
	}
	if empty.Outcome.Exit() == refused.Outcome.Exit() {
		t.Errorf("criterion 7 (%s): an empty result and a refusal share exit code %d",
			what, refused.Outcome.Exit())
	}
}

// ---------------------------------------------------------------------------
// Criterion 7 and 17 — no hub, unreachable hub, empty hub, refused: four answers
// ---------------------------------------------------------------------------

func TestNoHubUnreachableHubEmptyHubAndARefusalAreFourDistinguishableAnswers(t *testing.T) {
	f := newFixture(t, hub.ScopeRead, hub.ScopeWrite)
	readGrant := f.issue(hub.ScopeRead)
	writeOnly := f.issue(hub.ScopeWrite)

	empty := Answer(Request{Op: OpHub, Grant: readGrant.ID}, f.src)

	noHubSrc := f.src
	noHubSrc.Hub = func() (*hub.Store, hub.Membership, error) { return nil, nil, hub.ErrNoHubConfigured }
	noHub := Answer(Request{Op: OpHub, Grant: readGrant.ID}, noHubSrc)

	downSrc := f.src
	downSrc.Hub = func() (*hub.Store, hub.Membership, error) { return nil, nil, hub.ErrHubUnreachable }
	down := Answer(Request{Op: OpHub, Grant: readGrant.ID}, downSrc)

	refused := Answer(Request{Op: OpHub, Grant: writeOnly.ID}, f.src)

	answers := map[string]Response{"empty": empty, "no hub": noHub, "unreachable": down, "refused for scope": refused}
	seen := map[string]string{}
	for name, r := range answers {
		key := string(r.Outcome) + "/" + r.Code
		if other, dup := seen[key]; dup {
			t.Errorf("criteria 7 and 17: %q and %q both answer %q", name, other, key)
		}
		seen[key] = name
	}
	if empty.Outcome != OutcomeOK || len(empty.Notes) != 0 {
		t.Errorf("an empty hub should be a successful read of nothing, got %s with %d notes", empty.Outcome, len(empty.Notes))
	}
	// CRITERION 17: "it does not return empty as though the hub held nothing".
	if noHub.Outcome == OutcomeOK {
		t.Errorf("criterion 17: with no hub configured the hub read succeeded, which reads as a hub holding nothing")
	}
	if noHub.Code != hub.ErrNoHubConfigured.Code {
		t.Errorf("criterion 17: with no hub configured the code is %q, want %q", noHub.Code, hub.ErrNoHubConfigured.Code)
	}
	// TWO QUESTIONS, ASSERTED SEPARATELY. "No hub configured" is a determined NO to both; "a hub is
	// configured and did not answer" is a determined YES to the first and undetermined only to the
	// second. Collapsing them is what made a refusal report an unknown hub on a machine with none.
	if noHub.HubConfigured != tri.No || noHub.HubContacted != tri.No {
		t.Errorf("criterion 17: with no hub configured, configured=%v contacted=%v; both are determined negatives",
			noHub.HubConfigured, noHub.HubContacted)
	}
	if down.HubConfigured != tri.Yes || down.HubContacted != tri.Undetermined {
		t.Errorf("criterion 7: with a hub configured and unreachable, configured=%v contacted=%v; "+
			"a hub that could not be reached is still a hub that is configured",
			down.HubConfigured, down.HubContacted)
	}
	if down.Outcome.Exit() != 3 {
		t.Errorf("an unreachable hub exits %d; `could not determine` and `determined to be nothing` never share a code",
			down.Outcome.Exit())
	}

	// CRITERION 17's OTHER HALF, AND §4.4: with no hub configured the LOCAL half works in full.
	f.putTicket("t-1", "still here")
	tix := Answer(Request{Op: OpTickets, Grant: readGrant.ID}, noHubSrc)
	if tix.Outcome != OutcomeOK || len(tix.Tickets) != 1 {
		t.Errorf("criterion 17 / §4.4: with no hub configured, tickets were %s: %+v", tix.Outcome, tix)
	}
	dr := Answer(Request{Op: OpDrafts, Grant: readGrant.ID}, noHubSrc)
	if dr.Outcome != OutcomeOK {
		t.Errorf("criterion 17 / §4.4: with no hub configured, drafts were %s (%s)", dr.Outcome, dr.Code)
	}
}

// ---------------------------------------------------------------------------
// Criterion 4 — what the agent reads, it can be told to write up
// ---------------------------------------------------------------------------

func TestADraftWrittenFromHubMaterialLandsUnpublished(t *testing.T) {
	f := newFixture(t, hub.ScopeRead, hub.ScopeWrite)
	g := f.issue(hub.ScopeRead, hub.ScopeWrite)
	f.publish(me, "what I worked out", hub.CompanyWide())
	before := f.hub.Count()

	read := Answer(Request{Op: OpHub, Grant: g.ID}, f.src)
	if read.Outcome != OutcomeOK || len(read.Notes) != 1 {
		t.Fatalf("the fixture did not read anything to write up: %+v", read)
	}
	w := Answer(Request{Op: OpDraftWrite, Grant: g.ID, NoteID: "written-up", Body: "from " + read.Notes[0].Title}, f.src)
	if w.Outcome != OutcomeOK || len(w.Drafts) != 1 {
		t.Fatalf("criterion 4: writing up what was read was %s (%s): %s", w.Outcome, w.Code, w.Message)
	}
	if w.Drafts[0].State != DraftedState || w.Drafts[0].Published {
		t.Errorf("criterion 4: the written-up note is state %q published=%t, want an unpublished draft",
			w.Drafts[0].State, w.Drafts[0].Published)
	}
	// AND NOTHING PUBLISHED AS A SIDE EFFECT OF HAVING BEEN READ (§3.3, `manual` is the default).
	if f.hub.Count() != before {
		t.Errorf("criterion 4: reading hub material and drafting from it published something (%d -> %d)",
			before, f.hub.Count())
	}
	list := Answer(Request{Op: OpDrafts, Grant: g.ID}, f.src)
	found := false
	for _, d := range list.Drafts {
		if d.ID == "written-up" {
			found = true
		}
	}
	if !found {
		t.Errorf("criterion 4: the written-up note is not in the outbox: %+v", list.Drafts)
	}
}

// ---------------------------------------------------------------------------
// Criterion 9 — revocation
// ---------------------------------------------------------------------------

func TestRevokingAGrantRefusesTheNextRequestUnderIt(t *testing.T) {
	f := newFixture(t)
	g := f.issue(hub.ScopeRead)
	f.putTicket("t-1", "before")

	before := Answer(Request{Op: OpTickets, Grant: g.ID}, f.src)
	if before.Outcome != OutcomeOK {
		t.Fatalf("the grant did not work before revocation: %s (%s)", before.Outcome, before.Code)
	}

	rev := Answer(Request{Op: OpRevoke, Grant: g.ID}, f.src)
	if rev.Outcome != OutcomeOK {
		t.Fatalf("revoking was %s (%s): %s", rev.Outcome, rev.Code, rev.Message)
	}

	after := Answer(Request{Op: OpTickets, Grant: g.ID}, f.src)
	if after.Outcome != OutcomeRefused || after.Code != ErrGrantRevoked.Code {
		t.Fatalf("criterion 9: after revocation the next request was %s (%s), want refused/%s.\n"+
			"  A request that succeeded before revocation must not keep a later one alive.",
			after.Outcome, after.Code, ErrGrantRevoked.Code)
	}
	if len(after.Tickets) != 0 {
		t.Error("criterion 9: the refused request served tickets anyway")
	}
	// A REVOKED GRANT IS NOT AN UNKNOWN ONE. The person who revoked it needs to see that it took.
	unknown := Answer(Request{Op: OpTickets, Grant: "grant-never-issued"}, f.src)
	if unknown.Code == after.Code {
		t.Errorf("criterion 9: a revoked grant and a grant that never existed share code %q", after.Code)
	}
}

// ---------------------------------------------------------------------------
// Criteria 13, 14, 15 — the key, and the third answer
// ---------------------------------------------------------------------------

func TestAConfiguredAndAnUnconfiguredModelAreDistinguishableAndNeitherLeaksTheKey(t *testing.T) {
	f := newFixture(t)
	g := f.issue(hub.ScopeRead)

	none := Answer(Request{Op: OpModel, Grant: g.ID}, f.src)

	configured := f.src
	configured.Model = func() model.Config {
		return model.Read(withEnv(map[string]string{model.EnvProvider: "acme", model.EnvCredential: "sk-x"}), nil)
	}
	some := Answer(Request{Op: OpModel, Grant: g.ID}, configured)

	unreadable := f.src
	// A REAL UNDETERMINED, PRODUCED BY A REAL CONDITION: a credential file path that names
	// something present and unreadable. A DIRECTORY is used because it fails on every platform and
	// without depending on which user the test runs as; a chmod 000 file is a no-op for root.
	//
	// NOT A MISSING FILE — internal/model calls that a determined NO, and it is right: the
	// filesystem answered. My own earlier reading of this configuration called a missing file
	// undetermined, which was wrong, and consuming the one answer fixed it.
	keyDir := filepath.Join(t.TempDir(), "not-a-file")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unreadable.Model = func() model.Config {
		return model.Read(withEnv(map[string]string{
			model.EnvProvider:       "acme",
			model.EnvCredentialFile: keyDir,
		}), nil)
	}
	unsure := Answer(Request{Op: OpModel, Grant: g.ID}, unreadable)

	// CRITERION 14: configured and not configured are distinguishable.
	if none.Model.Chosen() != tri.No || some.Model.Present() != tri.Yes {
		t.Errorf("criterion 14: none chosen=%v, configured present=%v; a person's AI must be able to learn which",
			none.Model.Chosen(), some.Model.Present())
	}
	// CRITERION 15: and the third answer is neither of them.
	if unsure.Model.Present() != tri.Undetermined || unsure.Outcome != OutcomeUndetermined {
		t.Errorf("criterion 15: an unreadable credential configuration is %v/%s, want undetermined",
			unsure.Model.Present(), unsure.Outcome)
	}
	if unsure.Outcome.Exit() == none.Outcome.Exit() {
		t.Errorf("criterion 15: `could not determine whether a model is configured` and `none is` share exit code %d",
			none.Outcome.Exit())
	}
	// CRITERION 13: none of the three answers carries the credential. The type has no field for
	// one — asserted structurally in TestTheModelViewServedHasNoFieldACredentialCouldBeAssignedTo —
	// and these are the values, checked.
	for name, r := range map[string]Response{"none": none, "configured": some, "undetermined": unsure} {
		if containsBytes(bodyOfBytes(t, r), "sk-x") {
			t.Errorf("criterion 13: the %s model answer carries the credential", name)
		}
	}

	// CRITERION 14's LAST CLAUSE: with none configured, the other reads still succeed.
	f.putTicket("t-1", "unaffected")
	if r := Answer(Request{Op: OpTickets, Grant: g.ID}, f.src); r.Outcome != OutcomeOK || len(r.Tickets) != 1 {
		t.Errorf("criterion 14: with no model configured, reading tickets was %s — 'no model configured is not "+
			"a broken client' (PRD §3.13)", r.Outcome)
	}
}

// ---------------------------------------------------------------------------
// Criterion 15 — undetermined notes are neither listed nor ruled out
// ---------------------------------------------------------------------------

func TestANoteWhoseReadabilityIsUndeterminedIsNeitherListedNorRuledOut(t *testing.T) {
	f := newFixture(t)
	g := f.issue(hub.ScopeRead)
	f.publish(me, "a note somebody wrote", hub.CompanyWide())

	// THE UNDETERMINED CASE IS PRODUCED BY A REAL CONDITION, NOT A STUB: nobody told this daemon
	// who it serves, so hub.CanRead has an unidentified reader and answers Undetermined — its own
	// first branch, and the one that says "we were not told who is asking, so we did not work out
	// whether they may read it". This is also the honest default on a machine where sign-in
	// (Issue #19) has not happened.
	src := f.src
	src.Person = ""

	r := Answer(Request{Op: OpHub, Grant: g.ID}, src)
	// ABSENT AND ZERO ARE DIFFERENT ANSWERS. Absent means the hub was never examined; this path
	// examined it, so the count must be present as well as non-zero.
	if r.UndeterminedNotes == nil {
		t.Fatalf("the hub was read and the undetermined count is absent, which claims it was never examined: %+v", r)
	}
	if *r.UndeterminedNotes == 0 {
		t.Fatalf("the fixture did not produce an undetermined note, so this test proves nothing: %+v", r)
	}
	if r.Outcome != OutcomeUndetermined {
		t.Errorf("criterion 15: %d note(s) could not be evaluated and the outcome is %s; "+
			"a list that could not be completed is not a complete list", *r.UndeterminedNotes, r.Outcome)
	}
	if r.Outcome.Exit() != 3 {
		t.Errorf("criterion 15: exit code %d; the undetermined answer has its own", r.Outcome.Exit())
	}
	// NEITHER LISTED NOR RULED OUT. Not in Notes, and not silently dropped from the accounting.
	if len(r.Notes) != 0 {
		t.Errorf("criterion 15: a note whose readability could not be determined was listed as readable: %+v", r.Notes)
	}
	// AND IT IS NOT AN EMPTY HUB. The determined-empty answer and this one differ in outcome, in
	// code and in exit code — the collapse §4.3 forbids, checked rather than assumed.
	empty := Answer(Request{Op: OpHub, Grant: g.ID}, func() Sources {
		s2 := f.src
		s2.Hub = func() (*hub.Store, hub.Membership, error) { return hub.NewStore(hub.NewRecord()), nil, nil }
		return s2
	}())
	if empty.Outcome == r.Outcome || empty.Outcome.Exit() == r.Outcome.Exit() {
		t.Errorf("criterion 15: an empty hub (%s/%d) and an undetermined one (%s/%d) are not distinguishable",
			empty.Outcome, empty.Outcome.Exit(), r.Outcome, r.Outcome.Exit())
	}
}

// ---------------------------------------------------------------------------
// Authority is established before anything is read
// ---------------------------------------------------------------------------

func TestNoRequestIsServedWithoutALiveGrant(t *testing.T) {
	f := newFixture(t)
	f.putTicket("t-1", "private")
	f.publish(me, "mine", hub.CompanyWide())

	for _, tc := range []struct {
		name string
		req  Request
		code string
	}{
		{"no grant at all", Request{Op: OpTickets}, ErrNoGrant.Code},
		{"a grant nobody issued", Request{Op: OpTickets, Grant: "grant-made-up"}, ErrUnknownGrant.Code},
		{"a grant nobody issued, on the hub", Request{Op: OpHub, Grant: "grant-made-up"}, ErrUnknownGrant.Code},
	} {
		r := Answer(tc.req, f.src)
		if r.Outcome != OutcomeRefused || r.Code != tc.code {
			t.Errorf("%s: %s (%s), want refused/%s", tc.name, r.Outcome, r.Code, tc.code)
		}
		if len(r.Tickets) != 0 || len(r.Notes) != 0 || r.Note != nil {
			t.Errorf("%s: the refusal served material anyway — the authority check is not first", tc.name)
		}
	}
}

// TestALedgerThatCannotBeReadIsUndeterminedAndNotARefusal is §4.3 at the authority layer.
func TestALedgerThatCannotBeReadIsUndeterminedAndNotARefusal(t *testing.T) {
	f := newFixture(t)
	src := f.src
	src.Grants = brokenGrants{}
	r := Answer(Request{Op: OpTickets, Grant: "grant-anything"}, src)
	if r.Outcome != OutcomeUndetermined || r.Code != ErrGrantUndetermined.Code {
		t.Fatalf("an unreadable ledger answered %s (%s), want undetermined/%s — a confident negative built "+
			"from a failure is the defect the whole tri package exists to make hard",
			r.Outcome, r.Code, ErrGrantUndetermined.Code)
	}
	if r.Outcome.Exit() == OutcomeRefused.Exit() {
		t.Error("an unreadable ledger and a refusal share an exit code")
	}
}

type brokenGrants struct{}

func (brokenGrants) Issue(hub.Holder, []hub.Scope) (hub.Grant, error) {
	return hub.Grant{}, errors.New("the ledger could not be written")
}
func (brokenGrants) Lookup(hub.GrantID) (hub.Grant, GrantState, error) {
	return hub.Grant{}, GrantUnknown, errors.New("the ledger could not be read")
}
func (brokenGrants) Revoke(hub.GrantID) error { return errors.New("the ledger could not be written") }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustPeople(t *testing.T, p ...hub.PersonID) hub.Visibility {
	t.Helper()
	v, err := hub.ToPeople(p...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func bodyOfBytes(t *testing.T, r Response) []byte {
	t.Helper()
	b, err := MarshalResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// withEnv makes a getenv over a fixed map, so model.Read can be driven without touching the
// process environment.
func withEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func bodyOf(t *testing.T, r Response) string {
	t.Helper()
	b, err := MarshalResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func containsBytes(b []byte, s string) bool {
	if s == "" {
		return false
	}
	return len(b) >= len(s) && indexOf(string(b), s) >= 0
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
