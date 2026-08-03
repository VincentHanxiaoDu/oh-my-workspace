package agentapi

import (
	"errors"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// GrantState is whether a grant is live, revoked, or unknown to this machine.
type GrantState int

const (
	// GrantUnknown is the zero value ON PURPOSE, following tri's reasoning: a grant nobody looked
	// up has not been established to be live, and a struct field an error path left alone must not
	// read as an authority.
	GrantUnknown GrantState = iota
	GrantLive
	GrantRevoked
)

// Grants is the ledger of grants issued on this machine, and the one place revocation is decided
// (PRD §3.10, criterion 9).
//
// Lookup's error return is not the same as GrantUnknown: a ledger that could not be READ has not
// told us the grant is absent, and [Answer] turns those into two different outcomes.
type Grants interface {
	// Issue evaluates a grant request and records it. It MUST decide with
	// [hub.EvaluateGrantRequest] and must not re-derive the §4.5 rule.
	Issue(h hub.Holder, requested []hub.Scope) (hub.Grant, error)
	// Lookup returns the grant and whether it is live.
	Lookup(id hub.GrantID) (hub.Grant, GrantState, error)
	// Revoke marks a grant revoked. Revoking an unknown grant is not an error — the outcome a
	// person wanted (that id cannot act) is the outcome either way.
	Revoke(id hub.GrantID) error
}

// Sources is everything [Answer] is allowed to look at.
//
// IT IS A STRUCT OF FUNCTIONS BECAUSE THE DAEMON OWNS THE STORE AND THIS PACKAGE MUST NOT. The
// daemon holds the store's write lock (PRD §2.1) and wires these to the SAME functions the CLI
// calls — [inbox.List] for tickets, the outbox for drafts, [hub.Store.ListReadable] for hub
// material. That is what makes the agent API and the CLI the same answer rather than two answers
// that agree today.
type Sources struct {
	// Person is who the agent acts for. Empty means we were not told, and that is Undetermined
	// everywhere it matters — [hub.CanRead] answers Undetermined for an unidentified reader, and
	// nothing here overrides it into a yes or a no.
	Person hub.PersonID
	// PersonScopes is what the PERSON holds. A grant request wider than this is refused when it is
	// requested (§4.5).
	PersonScopes []hub.Scope

	Grants Grants

	// Tickets is the person's inbox. Wire it to inbox.List over the daemon's store.
	Tickets func() ([]inbox.Ticket, error)
	// Drafts is the person's outbox. It returns [ErrNoOutbox] when there is not one, which is a
	// determined fact and not an empty list.
	Drafts func() ([]DraftView, error)
	// ReviseDraft is the local write path (criterion 4).
	ReviseDraft func(id, body string) (DraftView, error)
	// HubConfigured says whether a hub is configured on this machine, WITHOUT contacting one.
	//
	// IT IS SEPARATE FROM Hub BECAUSE IT IS A DIFFERENT QUESTION AND A CHEAPER ONE. Whether a hub
	// is configured is a fact about the machine that is true of a refused request as much as of a
	// successful one, so it must be answerable without consulting anything — reading configuration
	// is not reaching out (§4.2). Wiring this to something that dials would break criterion 16.
	HubConfigured func() tri.Value
	// Hub reaches the hub. It returns [hub.ErrNoHubConfigured] when there is none (§4.4,
	// criterion 17) and [hub.ErrHubUnreachable] when there is one that did not answer (§3.11,
	// criterion 7) — two different errors because they are two different facts.
	Hub func() (*hub.Store, hub.Membership, error)
	// Model reports whether a model is configured. It CANNOT return the credential: [ModelView]
	// has no field for one (§3.13, criterion 13).
	Model func() ModelView
}

// Answer serves one request. It is the whole of the agent API's behaviour.
//
// # The order is the feature
//
//  1. The operation must exist.
//  2. The authority is established BEFORE anything is read or written. A refusal that has already
//     read has leaked; a refusal that has already written is a refusal in name only. Both were
//     available as easy mistakes here and the ordering is what forecloses them — the same reasoning
//     [hub.SetVisibilityThrough] records for its own first statement.
//  3. Only then does the operation run.
//
// # It decides no visibility of its own
//
// The hub paths call [hub.Store.ListReadable] and [hub.Store.Read], which call [hub.CanRead]. The
// grant paths call [hub.EvaluateGrantRequest] and [hub.Permits]. Nothing in this file compares a
// [hub.Visibility] to anything or walks a group.
func Answer(req Request, src Sources) Response {
	// ONE EXIT, AND EVERY ANSWER GOES THROUGH IT. See [Response.finalise]: the facts that are true
	// of this request whatever happened in it are stamped in one place, because filling them in per
	// branch is the shape that shipped a refusal claiming no person was configured.
	resp := answer(req, src)
	resp.finalise(src)
	return resp
}

func answer(req Request, src Sources) Response {
	if !KnownOp(req.Op) {
		return Refuse(req.Op, hub.Refusedf(ErrUnknownOperation, "%q", string(req.Op)))
	}

	// ---- Administrative operations: the person, on their own machine. ------------------
	switch req.Op {
	case OpGrant:
		return answerGrant(req, src)
	case OpRevoke:
		return answerRevoke(req, src)
	}

	// ---- Authority, before anything is looked at. --------------------------------------
	grant, resp, ok := establishAuthority(req, src)
	if !ok {
		return resp
	}

	base := Response{Op: req.Op, Outcome: OutcomeOK, Person: string(src.Person)}
	switch req.Op {
	case OpTickets:
		return answerTickets(base, src)
	case OpDrafts:
		return answerDrafts(base, src)
	case OpHub:
		return answerHub(base, grant, src)
	case OpNote:
		return answerNote(base, req, grant, src)
	case OpDraftWrite:
		return answerDraftWrite(base, req, src)
	case OpPublish:
		return answerPublish(base, req, grant, src)
	case OpModel:
		return answerModel(base, src)
	}
	// Unreachable while Operations and this switch agree, and
	// TestEveryEnumeratedAgentOperationIsAnswered makes adding one to the enumeration without
	// wiring it a red test rather than this line at runtime.
	return Refuse(req.Op, hub.Refusedf(ErrUnknownOperation, "%q is enumerated and not wired up", string(req.Op)))
}

// establishAuthority is criteria 6, 8 and 9, and it runs before every non-administrative operation.
//
// THE TWO HALVES OF CRITERION 6 ARE TWO CHECKS WITH TWO CODES, in this order:
//
//  1. Is the presented scope one the PERSON holds? That is [hub.EvaluateGrantRequest] — the §4.5
//     rule, called and not re-derived — and it refuses ENTIRELY rather than narrowing.
//  2. Is it one the AGENT was granted? That is [hub.Permits] against the grant's own scopes.
//
// A request that presents no scopes is not thereby unscoped: the operation's own required scope
// (step 3) is still checked against the grant. Presenting scopes is how an agent asks to be
// refused early and loudly; not presenting them is not a way around the gate.
func establishAuthority(req Request, src Sources) (hub.Grant, Response, bool) {
	if src.Grants == nil {
		return hub.Grant{}, Undetermined(req.Op, ErrGrantUndetermined), false
	}
	if req.Grant == "" {
		return hub.Grant{}, Refuse(req.Op, ErrNoGrant), false
	}
	grant, state, err := src.Grants.Lookup(req.Grant)
	if err != nil {
		// NOT A REFUSAL. A ledger that could not be read has not told us the grant is absent, and
		// answering "unknown grant" here would be a confident negative built from a failure.
		return hub.Grant{}, Undetermined(req.Op, hub.Refusedf(ErrGrantUndetermined, "%v", err)), false
	}
	switch state {
	case GrantRevoked:
		// CRITERION 9. Its own code, so that "I revoked it and it stopped working" is observable
		// rather than inferred from a generic failure.
		return hub.Grant{}, Refuse(req.Op, hub.Refusedf(ErrGrantRevoked, "grant %q", string(req.Grant))), false
	case GrantLive:
	default:
		return hub.Grant{}, Refuse(req.Op, hub.Refusedf(ErrUnknownGrant, "grant %q", string(req.Grant))), false
	}

	// CRITERION 6, HALF ONE: refused at request time, never narrowed at the edge.
	if len(req.Scopes) > 0 {
		if _, err := hub.EvaluateGrantRequest(hub.Holder{Person: src.Person, Scopes: src.PersonScopes}, req.Scopes); err != nil {
			return hub.Grant{}, Refuse(req.Op, err), false
		}
		// CRITERION 6, HALF TWO: the person holds it and this agent was not given it.
		for _, s := range req.Scopes {
			if !hub.Permits(grant.Scopes, s) {
				return hub.Grant{}, Refuse(req.Op,
					hub.Refusedf(ErrScopeNotGranted, "grant %q carries no %q", string(grant.ID), string(s))), false
			}
		}
	}

	// CRITERION 8: `read` confers neither `write` nor `publish`. The operation's own scope is
	// checked against the GRANT, with the code package hub already defines for each so that the
	// CLI and the agent API refuse a publish with one code between them.
	need, needs := ScopeFor(req.Op)
	if needs && !hub.Permits(grant.Scopes, need) {
		return hub.Grant{}, Refuse(req.Op, scopeRefusal(need, grant)), false
	}
	// AND THE PERSON MUST STILL HOLD IT. A grant that outlived its holder's authority — the person
	// lost `publish` after the grant was issued — must not keep acting. §3.12: an agent cannot read
	// what its person cannot, read forward in time as well as sideways.
	if needs && !hub.Permits(src.PersonScopes, need) {
		return hub.Grant{}, Refuse(req.Op,
			hub.Refusedf(hub.ErrGrantWiderThanHolder, "%q holds no %q, so no grant of theirs may exercise it",
				string(src.Person), string(need))), false
	}
	return grant, Response{}, true
}

// scopeRefusal picks the code for a missing scope. The read and publish codes are package hub's,
// because those refusals must read identically whether they came from `omw` or from an agent; the
// write one is this Issue's, because the local write path is this Issue's.
func scopeRefusal(need hub.Scope, g hub.Grant) error {
	switch need {
	case hub.ScopeRead:
		return hub.Refusedf(hub.ErrReadScopeRequired, "grant %q carries no %q scope", string(g.ID), string(need))
	case hub.ScopePublish:
		return hub.Refusedf(hub.ErrPublishScopeRequired, "grant %q carries no %q scope", string(g.ID), string(need))
	default:
		return hub.Refusedf(ErrWriteScopeRequired, "grant %q carries no %q scope", string(g.ID), string(need))
	}
}

func answerGrant(req Request, src Sources) Response {
	if src.Grants == nil {
		return Undetermined(OpGrant, ErrGrantUndetermined)
	}
	g, err := src.Grants.Issue(hub.Holder{Person: src.Person, Scopes: src.PersonScopes}, req.Scopes)
	if err != nil {
		// THE REFUSAL ISSUES NOTHING (§4.5, Issue #12 criterion 11). That is hub.Ledger.Request's
		// property, kept by calling it rather than by re-implementing the evaluation here.
		return Refuse(OpGrant, err)
	}
	return Response{
		Op: OpGrant, Outcome: OutcomeOK, Person: string(src.Person),
		Grant: &GrantView{ID: string(g.ID), Holder: string(g.Holder), Scopes: scopeTexts(g.Scopes), Live: true},
	}
}

func answerRevoke(req Request, src Sources) Response {
	if src.Grants == nil {
		return Undetermined(OpRevoke, ErrGrantUndetermined)
	}
	if req.Grant == "" {
		return Refuse(OpRevoke, ErrNoGrant)
	}
	if err := src.Grants.Revoke(req.Grant); err != nil {
		return Undetermined(OpRevoke, hub.Refusedf(ErrGrantUndetermined, "%v", err))
	}
	return Response{
		Op: OpRevoke, Outcome: OutcomeOK, Person: string(src.Person),
		Grant:   &GrantView{ID: string(req.Grant), Holder: string(src.Person), Live: false},
		Message: "revoked; the next request under this grant is refused",
	}
}

func answerTickets(base Response, src Sources) Response {
	if src.Tickets == nil {
		return Undetermined(OpTickets, ErrLocalUndetermined)
	}
	ts, err := src.Tickets()
	if err != nil {
		// NOT AN EMPTY INBOX (Issue #8's first careful thing, restated on this surface). A person's
		// AI summarising "you have nothing to act on" off an unreadable store is exactly the harm.
		return Undetermined(OpTickets, hub.Refusedf(ErrLocalUndetermined, "%v", err))
	}
	base.Tickets = make([]TicketView, 0, len(ts))
	for _, t := range ts {
		base.Tickets = append(base.Tickets, TicketView{Ticket: t})
	}
	// §4.2 AND §4.4: tickets are local. Nothing was dialled, and the hub answer says so rather
	// than being left blank for a reader to fill in.
	base.setHubContacted(tri.No)
	base.Message = "tickets are held on this machine and are never published; no hub was contacted"
	return base
}

func answerDrafts(base Response, src Sources) Response {
	if src.Drafts == nil {
		return Undetermined(OpDrafts, ErrLocalUndetermined)
	}
	ds, err := src.Drafts()
	if err != nil {
		if errors.Is(err, ErrNoOutbox) {
			// A DETERMINED FACT, NOT AN EMPTY OUTBOX (§4.2). The outbox package makes the same
			// distinction with its marker file, for the same reason.
			return Refuse(OpDrafts, err)
		}
		return Undetermined(OpDrafts, hub.Refusedf(ErrLocalUndetermined, "%v", err))
	}
	base.Drafts = ds
	base.setHubContacted(tri.No)
	base.Message = "every draft here is unpublished; nothing in the outbox has been sent to a hub"
	return base
}

// reachHub applies §4.2's order and produces criterion 7's three distinguishable outcomes.
func reachHub(op Op, src Sources) (*hub.Store, hub.Membership, Response, bool) {
	if src.Hub == nil {
		return nil, nil, Refuse(op, hub.ErrNoHubConfigured), false
	}
	st, mem, err := src.Hub()
	switch {
	case errors.Is(err, hub.ErrNoHubConfigured):
		// A DETERMINED FACT ABOUT THIS MACHINE (criterion 17). Refused, not undetermined, and
		// emphatically not an empty list of notes.
		r := Refuse(op, err)
		r.HubConfigured = tri.No
		r.setHubContacted(tri.No)
		r.Message = "no hub is configured on this machine, so this is not an empty hub and nothing was looked at; " +
			"tickets and drafts do not need one"
		return nil, nil, r, false
	case err != nil:
		// A FAILURE TO DETERMINE (§3.11, criterion 7). Undetermined, its own exit code.
		r := Undetermined(op, err)
		// A HUB IS CONFIGURED — that is precisely what makes this "unreachable" rather than "none".
		// Reporting it as unknown here would lose the one thing this branch has established.
		r.HubConfigured = tri.Yes
		r.setHubContacted(tri.Undetermined)
		r.Message = "a hub is configured and could not be reached; this is not a hub with nothing in it, " +
			"and it is not a refusal"
		return nil, nil, r, false
	case st == nil:
		r := Undetermined(op, hub.ErrHubUnreachable)
		r.HubConfigured = tri.Yes
		r.setHubContacted(tri.Undetermined)
		return nil, nil, r, false
	}
	return st, mem, Response{}, true
}

func answerHub(base Response, _ hub.Grant, src Sources) Response {
	st, _, r, ok := reachHub(OpHub, src)
	if !ok {
		return r
	}
	// THE ONE PREDICATE. ListReadable calls CanReadNote calls CanRead, and a note this person may
	// not read is neither in readable nor in undetermined — it is absent, which is criterion 5.
	readable, undetermined := st.ListReadable(src.Person)
	base.Notes = make([]NoteView, 0, len(readable))
	for _, n := range readable {
		base.Notes = append(base.Notes, viewOf(n, false))
	}
	n := len(undetermined)
	base.UndeterminedNotes = &n
	base.HubConfigured = tri.Yes
	base.setHubContacted(tri.Yes)
	if len(undetermined) > 0 {
		// NOT DROPPED AND NOT COUNTED AS RESULTS. Saying "and N I could not evaluate" is what keeps
		// "no results" from absorbing "I could not check".
		base.Outcome = OutcomeUndetermined
		base.Code = hub.ErrUndetermined.Code
		base.Message = "some notes' readability could not be determined; they are neither listed nor ruled out"
	}
	return base
}

func answerNote(base Response, req Request, grant hub.Grant, src Sources) Response {
	if req.NoteID == "" {
		return Refuse(OpNote, hub.Refusedf(hub.ErrNoSuchNote, "no note was named"))
	}
	st, _, r, ok := reachHub(OpNote, src)
	if !ok {
		return r
	}
	// READ THROUGH THE GRANT, AS THE GRANT'S HOLDER. hub.ReadThrough checks the scope and then
	// delegates to Store.Read, which is CanRead. The three outcomes below are its three outcomes,
	// carried through unchanged rather than re-decided:
	//
	//   ErrNoSuchNote      there is no such note
	//   ErrRefused         it exists and this person may not read it — DISTINGUISHABLE from the
	//                      line above by its code, which is Issue #12's criterion 12 and is a
	//                      decision that Issue took deliberately
	//   ErrUndetermined    readability could not be worked out
	//
	// Note ids are unguessable (crypto/rand, 128 bits, owner ruling), which is what lets those two
	// be distinguishable without the distinction being a way to enumerate the hub.
	n, err := hub.ReadThrough(st, grant, hub.NoteID(req.NoteID))
	if err != nil {
		if hub.Code(err) == hub.ErrUndetermined.Code {
			return Undetermined(OpNote, err)
		}
		return Refuse(OpNote, err)
	}
	v := viewOf(n, true)
	base.Note = &v
	base.HubConfigured = tri.Yes
	base.setHubContacted(tri.Yes)
	return base
}

func answerDraftWrite(base Response, req Request, src Sources) Response {
	if src.ReviseDraft == nil {
		return Undetermined(OpDraftWrite, ErrLocalUndetermined)
	}
	if req.NoteID == "" {
		return Refuse(OpDraftWrite, hub.Refusedf(ErrUnknownOperation, "no draft was named"))
	}
	d, err := src.ReviseDraft(req.NoteID, req.Body)
	if err != nil {
		if errors.Is(err, ErrNoOutbox) {
			return Refuse(OpDraftWrite, err)
		}
		return Undetermined(OpDraftWrite, hub.Refusedf(ErrLocalUndetermined, "%v", err))
	}
	base.Drafts = []DraftView{d}
	base.setHubContacted(tri.No)
	// CRITERION 4, SAID OUT LOUD. Writing a draft is not a publication and does not become one by
	// the material having been read from the hub. `manual` is the default (§3.3).
	base.Message = "written to the outbox as an unpublished draft; nothing has been published, and " +
		"reading hub material does not publish anything"
	return base
}

func answerPublish(base Response, req Request, grant hub.Grant, src Sources) Response {
	st, _, r, ok := reachHub(OpPublish, src)
	if !ok {
		return r
	}
	v := hub.Default()
	if req.Visibility != "" {
		parsed, err := hub.ParseChoice(req.Visibility)
		if err != nil {
			return Refuse(OpPublish, err)
		}
		v = parsed
	}
	// PUBLISHED THROUGH THE GRANT, which checks hub.ScopePublish before anything is stored and
	// refuses a publication naming a different author. Both are hub.PublishThrough's, called.
	n, err := hub.PublishThrough(st, grant, hub.Publication{
		Author: src.Person, Title: req.Title, Body: req.Body, Visibility: v,
	})
	if err != nil {
		if hub.Code(err) == hub.ErrUndetermined.Code {
			return Undetermined(OpPublish, err)
		}
		return Refuse(OpPublish, err)
	}
	view := viewOf(n, true)
	base.Note = &view
	base.HubConfigured = tri.Yes
	base.setHubContacted(tri.Yes)
	return base
}

func answerModel(base Response, src Sources) Response {
	if src.Model == nil {
		// CRITERION 15 NAMES THIS CASE: whether a credential is configured, undetermined. Not "no
		// model configured", which is a determined answer criterion 14 also requires be available.
		m := ModelView{Configured: tri.Undetermined, Detail: "no model configuration could be read on this machine"}
		base.Model = &m
		base.Outcome = OutcomeUndetermined
		base.Code = hub.ErrUndetermined.Code
		return base
	}
	m := src.Model()
	// NEVER THE VALUE, WHATEVER THE SOURCE PUT THERE. The type has no credential field, so there is
	// nothing to clear; this line states the promise on the wire so a reader meets it.
	m.CredentialReadable = false
	base.Model = &m
	base.setHubContacted(tri.No)
	base.Message = "whether a model is configured is readable here; the credential is not, and there is no " +
		"agent API operation that returns it (PRD §3.13)"
	if !m.Configured.Determined() {
		base.Outcome = OutcomeUndetermined
		base.Code = hub.ErrUndetermined.Code
	}
	return base
}

// viewOf renders a note the person HAS ALREADY BEEN FOUND PERMITTED TO READ. It is never called on
// a note that failed the predicate — that is why it takes a *hub.Note and not an id.
func viewOf(n *hub.Note, withBody bool) NoteView {
	v := NoteView{
		ID:         string(n.ID),
		Author:     string(n.Author),
		Title:      n.Title,
		Visibility: n.Visibility.Token(),
		Version:    n.Latest().Number,
	}
	if withBody {
		v.Body = n.Latest().Body
	}
	return v
}

func scopeTexts(ss []hub.Scope) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, string(s))
	}
	return out
}
