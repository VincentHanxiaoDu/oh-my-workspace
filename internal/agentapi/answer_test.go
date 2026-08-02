package agentapi

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
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
		Model:        func() ModelView { return ModelView{Configured: tri.No, Detail: "none configured"} },
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
// IT COMPARES BYTES, NOT FIELDS. A field-by-field comparison passes while a new field somebody adds
// next month leaks the difference; the serialised form is what actually reaches the AI.
func TestTwoHubsDifferingOnlyByARestrictedNoteProduceIdenticalOutput(t *testing.T) {
	build := func(withRestricted bool) []byte {
		f := newFixture(t)
		g := f.issue(hub.ScopeRead)
		f.publish(me, "shared", hub.CompanyWide())
		if withRestricted {
			f.publish(colleague, "restricted away", mustPeople(t, colleague))
		}
		r := Answer(Request{Op: OpHub, Grant: g.ID}, f.src)
		// The grant id is minted from crypto/rand and differs between the two fixtures by
		// construction, so it is blanked: it is not part of what the hub's contents determine.
		r.Person = ""
		b, err := MarshalResponse(r)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	with, without := build(true), build(false)
	if string(with) != string(without) {
		t.Errorf("criterion 5: a note restricted away from this person changes the agent API's output.\n"+
			"  with it:    %s\n  without it: %s\n"+
			"  The existence of a note the person cannot read is itself something they cannot read (PRD §3.5).",
			with, without)
	}
}

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
	if noHub.HubState != tri.No || down.HubState != tri.Undetermined {
		t.Errorf("criterion 15: no-hub is %v and unreachable is %v; want a determined no and an undetermined",
			noHub.HubState, down.HubState)
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
	configured.Model = func() ModelView { return ModelView{Configured: tri.Yes, Provider: "acme"} }
	some := Answer(Request{Op: OpModel, Grant: g.ID}, configured)

	unreadable := f.src
	unreadable.Model = func() ModelView {
		return ModelView{Configured: tri.Undetermined, Detail: "the credential file could not be read"}
	}
	unsure := Answer(Request{Op: OpModel, Grant: g.ID}, unreadable)

	// CRITERION 14: configured and not configured are distinguishable.
	if none.Model.Configured != tri.No || some.Model.Configured != tri.Yes {
		t.Errorf("criterion 14: none=%v some=%v; a person's AI must be able to learn which",
			none.Model.Configured, some.Model.Configured)
	}
	// CRITERION 15: and the third answer is neither of them.
	if unsure.Model.Configured != tri.Undetermined || unsure.Outcome != OutcomeUndetermined {
		t.Errorf("criterion 15: an unreadable credential configuration is %v/%s, want undetermined",
			unsure.Model.Configured, unsure.Outcome)
	}
	if unsure.Outcome.Exit() == none.Outcome.Exit() {
		t.Errorf("criterion 15: `could not determine whether a model is configured` and `none is` share exit code %d",
			none.Outcome.Exit())
	}
	for name, r := range map[string]Response{"none": none, "configured": some, "undetermined": unsure} {
		if r.Model.CredentialReadable {
			t.Errorf("criterion 13: the %s model answer claims the credential is readable", name)
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
	if r.UndeterminedNotes == 0 {
		t.Fatalf("the fixture did not produce an undetermined note, so this test proves nothing: %+v", r)
	}
	if r.Outcome != OutcomeUndetermined {
		t.Errorf("criterion 15: %d note(s) could not be evaluated and the outcome is %s; "+
			"a list that could not be completed is not a complete list", r.UndeterminedNotes, r.Outcome)
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
