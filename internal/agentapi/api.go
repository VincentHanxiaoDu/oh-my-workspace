package agentapi

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Op is one agent API operation.
//
// THE LIST IS CLOSED AND ENUMERATED. [Operations] is the whole of it, the CLI's help is built from
// it, and [ScopeFor] answers for every member — so an operation added without a scope decision is a
// red test rather than an ungated surface.
type Op string

const (
	// OpTickets — the person's inbox (criterion 1).
	OpTickets Op = "tickets"
	// OpDrafts — the person's outbox, each draft identifiable as unpublished (criterion 2).
	OpDrafts Op = "drafts"
	// OpHub — the hub notes this person is permitted to read, and nothing wider (criteria 3, 5).
	OpHub Op = "hub"
	// OpNote — one hub note by id (criterion 5's negative half, and #12's criterion 12).
	OpNote Op = "note"
	// OpDraftWrite — revise a local draft (criterion 4, and criterion 8's first refusal).
	OpDraftWrite Op = "draft.write"
	// OpPublish — put a note on the hub (criterion 8's second refusal).
	OpPublish Op = "publish"
	// OpModel — whether a model is configured. NEVER the credential (criteria 13, 14).
	OpModel Op = "model"
	// OpGrant — issue a grant to an agent. Evaluated by [hub.EvaluateGrantRequest].
	OpGrant Op = "grant"
	// OpRevoke — revoke one (PRD §3.10, criterion 9).
	OpRevoke Op = "revoke"
)

// Operations is every operation, ordered. There is no tenth hiding anywhere.
func Operations() []Op {
	return []Op{OpTickets, OpDrafts, OpHub, OpNote, OpDraftWrite, OpPublish, OpModel, OpGrant, OpRevoke}
}

// ScopeFor is the scope an operation needs, and the second return says whether it needs one at all.
//
// THE VOCABULARY IS RULED AND IS EXACTLY THREE (Issue #19, restated in Issue #16's `## Ruled`).
// This function maps operations onto [hub.ScopeRead], [hub.ScopeWrite] and [hub.ScopePublish] and
// invents nothing: a fourth scope for "administer this machine's grants" would be the natural,
// tempting, forbidden thing. Issuing and revoking a grant need no scope because they are not
// exercised through a grant — they are the person acting on their own machine, over a socket that
// did not open until it was confirmed owner-only (§4.6). That is the same reasoning §2.4 uses for
// the hub operator: a deployment fact, not a scope.
func ScopeFor(op Op) (hub.Scope, bool) {
	switch op {
	case OpTickets, OpDrafts, OpHub, OpNote, OpModel:
		return hub.ScopeRead, true
	case OpDraftWrite:
		return hub.ScopeWrite, true
	case OpPublish:
		return hub.ScopePublish, true
	case OpGrant, OpRevoke:
		return "", false
	}
	return "", false
}

// KnownOp reports whether op is one of [Operations].
func KnownOp(op Op) bool {
	for _, o := range Operations() {
		if o == op {
			return true
		}
	}
	return false
}

// Request is one agent API call.
type Request struct {
	Op Op `json:"op"`
	// Grant is the authority the caller acts under. Required for everything except OpGrant and
	// OpRevoke.
	Grant hub.GrantID `json:"grant,omitempty"`
	// Scopes is what the caller PRESENTS: the scopes it claims for this request, or asks for when
	// issuing a grant. Criterion 6 turns on this being checked at request time.
	Scopes []hub.Scope `json:"scopes,omitempty"`
	// NoteID names a hub note or a local draft, depending on the operation.
	NoteID string `json:"note_id,omitempty"`
	// Title and Body are a publication's or a revision's content.
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	// Visibility is a publication's visibility token, parsed by [hub.ParseChoice].
	Visibility string `json:"visibility,omitempty"`
}

// Outcome is which of the three kinds of answer this is.
//
// THREE, NOT TWO, AND THEY DO NOT SHARE AN EXIT CODE. [Outcome.Exit] is the mapping, and
// "undetermined" is [cli.ExitUndetermined]'s 3 — restated as a literal here only because importing
// internal/cli from a package the daemon serves would drag the command registry into the daemon.
type Outcome string

const (
	// OutcomeOK — the operation answered. It does NOT mean the answer was affirmative: a hub read
	// that determined you may read nothing has succeeded.
	OutcomeOK Outcome = "ok"
	// OutcomeRefused — a determined negative. You may not, or there is not one.
	OutcomeRefused Outcome = "refused"
	// OutcomeUndetermined — the answer could not be worked out. Never rendered as either of the
	// other two (§4.3).
	OutcomeUndetermined Outcome = "undetermined"
)

// exitUndetermined mirrors cli.ExitUndetermined. Asserted equal by a test in package commands, so
// the duplication cannot drift.
const (
	exitSuccess      = 0
	exitFailure      = 1
	exitUndetermined = 3
)

// Exit is the process exit code this outcome must produce.
func (o Outcome) Exit() int {
	switch o {
	case OutcomeOK:
		return exitSuccess
	case OutcomeRefused:
		return exitFailure
	default:
		return exitUndetermined
	}
}

// TicketView is one inbox ticket as the agent API serves it.
//
// It embeds [inbox.Ticket] rather than restating its fields, so the three-valued fields keep their
// three-valued JSON — a missing title and an empty title stay different answers on this surface
// exactly as they are on `omw inbox read`. THERE IS NO PRIORITY FIELD, here or anywhere: Issue #8
// settled that tickets have no priority and a reflection test in package inbox enforces it.
type TicketView struct {
	inbox.Ticket
}

// DraftView is one unpublished draft.
//
// State IS ALWAYS "drafted" AND IS ALWAYS PRESENT (criterion 2). PRD §3.11 names four states —
// drafted, in flight, published, refused — and the outbox holds exactly the first: a note is in the
// outbox or published, never both and never neither. Serving a draft with no state field would
// leave the reader to infer which, and inference is how "drafted" becomes "published" in somebody's
// summary.
type DraftView struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Published bool   `json:"published"`
	Revisions int    `json:"revisions"`
	Latest    string `json:"latest,omitempty"`
}

// DraftedState is the one spelling of a draft's state, so criterion 2's assertion has one string to
// look for.
const DraftedState = "drafted"

// NoteView is one hub note, as this person is permitted to see it.
//
// A NOTE THE PERSON CANNOT READ NEVER BECOMES ONE OF THESE (criterion 5). The filtering happens in
// [hub.Store.ListReadable] and [hub.Store.Read] — which is to say in [hub.CanRead] — before this
// type is constructed, so there is no narrowed rendering to leak a title through.
type NoteView struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
	Version    int    `json:"version"`
	Body       string `json:"body,omitempty"`
}

// ModelView is what the agent API says about the model (PRD §3.13, criteria 13 and 14).
//
// THERE IS NO FIELD FOR THE CREDENTIAL, and that is the design rather than a discipline. A
// `Credential string` field left empty by today's code is one careless assignment away from being
// populated; a type with no such field cannot carry one however careless the next change is.
//
// Configured is three-valued because §4.3 applies here too: an unreadable configuration is not "no
// model configured", and criterion 15 names this case specifically.
type ModelView struct {
	Configured     tri.Value `json:"-"`
	ConfiguredText string    `json:"configured"`
	// Provider is the provider's NAME, which is not a secret and is what makes criterion 14's
	// "can learn that a model is configured" useful rather than a bare yes.
	Provider string `json:"provider,omitempty"`
	// CredentialReadable is always false and is stated rather than omitted. §3.13 is a promise the
	// surface should make out loud, so that a reader does not go looking for the field.
	CredentialReadable bool `json:"credential_readable"`
	// Detail says why, when Configured is undetermined.
	Detail string `json:"detail,omitempty"`
}

// GrantView is an issued grant, as reported back. It carries no secret: PRD §3.10's token material
// is Issue #19's, and this is the ledger entry.
type GrantView struct {
	ID     string   `json:"id"`
	Holder string   `json:"holder"`
	Scopes []string `json:"scopes"`
	Live   bool     `json:"live"`
}

// Response is one answer.
type Response struct {
	Op      Op      `json:"op"`
	Outcome Outcome `json:"outcome"`
	// Code is the stable machine-readable code. Empty only on OutcomeOK.
	Code string `json:"code,omitempty"`
	// Message is the sentence a person reads. Never the only signal.
	Message string `json:"message,omitempty"`
	// Person is who this answer was computed for. Present so that a reader can see the answer is
	// scoped to somebody, and see WHO — an answer computed for nobody is a different answer.
	Person string `json:"person,omitempty"`

	Tickets []TicketView `json:"tickets,omitempty"`
	Drafts  []DraftView  `json:"drafts,omitempty"`
	Notes   []NoteView   `json:"notes,omitempty"`
	Note    *NoteView    `json:"note,omitempty"`
	Model   *ModelView   `json:"model,omitempty"`
	Grant   *GrantView   `json:"grant,omitempty"`

	// UndeterminedNotes is how many hub notes this person's readability could not be worked out
	// for. NOT DROPPED AND NOT ADDED TO Notes (§4.3; hub.Store.ListReadable's own comment).
	//
	// IT IS A COUNT AND NOT A LIST OF IDS, and that is deliberate: an undetermined note is one
	// whose readability is unknown, so serving its id would be serving something that may turn out
	// to be unreadable. The count is what criterion 15 needs — the answer is distinguishable from
	// both a real value and a negative one — without deciding a question the hub has not answered.
	// IT IS A POINTER FOR THE SAME REASON Person AND HubConfigured ARE FILLED IN BY finalise. A
	// plain int is 0 on every refusal and on every operation that never looks at a hub, and
	// `"undetermined_notes":0` reads as "I examined the hub and none were undetermined" — a claim
	// about work that was not done. Absent means not examined; 0 means examined and none were.
	UndeterminedNotes *int `json:"undetermined_notes,omitempty"`

	// HubConfigured is whether a hub is configured ON THIS MACHINE, and HubContacted is whether one
	// was reached DURING THIS REQUEST. They are two fields because they are two questions, and one
	// field answering both is how this surface came to tell people something false.
	//
	// IT WAS ONE FIELD AND THAT WAS THE BUG. A single `HubState` was set only by the paths that
	// actually consulted a hub, so every refusal — which is refused before anything is consulted —
	// carried the zero value. The zero tri.Value is Undetermined, which is the right default in
	// isolation and was wrong here: on a machine with no hub configured, a refused `tickets`
	// request reported "whether you have a hub could not be determined" when the truth was
	// criterion 17's "there is no hub". A determined fact rendered as undetermined, with a
	// confident wrong reason, on every refusal.
	//
	// The lesson generalises past this field and is why [Response.finalise] exists: the question is
	// not "do we say undetermined when we should", it is "does every field say what we actually
	// know about it".
	HubConfigured     tri.Value `json:"-"`
	HubConfiguredText string    `json:"hub_configured,omitempty"`
	// HubContacted is Yes when the hub answered this request, Undetermined when one is configured
	// and could not be reached, and No when none was contacted — including every local operation
	// and every refusal, for which "nothing was contacted" is a determined fact and not a mystery.
	HubContacted     tri.Value `json:"-"`
	HubContactedText string    `json:"hub_contacted,omitempty"`
	// hubContactedSet records whether a path actually decided HubContacted, so that
	// [Response.finalise] can tell "nobody said" from "somebody said undetermined".
	//
	// IT EXISTS BECAUSE THE ZERO VALUE IS NOT THE TRUTH FOR THIS FIELD, and I wrote the opposite in
	// a comment here before a test corrected me. tri's zero is Undetermined — deliberately, so that
	// an unset answer never reads as a confident no — but for "was a hub contacted during this
	// request" the truth when nothing set it is a determined NO: nothing was contacted. This is the
	// one field where the package-wide default is the wrong default, so it is tracked explicitly
	// rather than inferred. Unexported, so it never reaches the wire.
	hubContactedSet bool
}

// Refuse builds a determined refusal carrying err's code.
//
// IT CARRIES NO FACTS ABOUT THE MACHINE, ON PURPOSE. Every response leaves [Answer] through
// [Response.finalise], which is the one place those are filled in — a constructor that took them
// would be a second place to forget them, and forgetting them here is exactly what shipped.
func Refuse(op Op, err error) Response {
	return Response{Op: op, Outcome: OutcomeRefused, Code: hub.Code(err), Message: err.Error()}
}

// Undetermined builds the third answer. Separate constructor from [Refuse] because the whole point
// is that a caller cannot produce one where it meant the other by changing an argument.
func Undetermined(op Op, err error) Response {
	return Response{Op: op, Outcome: OutcomeUndetermined, Code: hub.Code(err), Message: err.Error()}
}

// finalise stamps onto a response the facts that are true of this request whatever happened in it,
// and it runs on EVERY response [Answer] produces — successes, refusals and undetermined alike.
//
// # Why this is a single funnel and not a line in each branch
//
// The defect this exists to prevent was one branch out of a dozen not setting a field, and the
// field's zero value being a plausible-looking answer. Filling them in per branch is the shape that
// failed; a funnel every return passes through is the shape that cannot. There are only two rules
// in it and both are about saying what is known:
//
//   - Person is who this answer was computed for. A refusal is still computed FOR somebody, and
//     reporting nobody made the surface claim, on every refusal, that no person was configured.
//   - HubConfigured is a fact about the machine, not about this request, so it is knowable even
//     when the request was refused before anything was consulted. It is asked for cheaply and
//     WITHOUT DIALLING ANYTHING (§4.2) — reading configuration is not reaching out.
//
// A path that already determined a field keeps its answer: finalise fills gaps, it does not
// overwrite.
func (r *Response) finalise(src Sources) {
	if r.Person == "" {
		r.Person = string(src.Person)
	}
	if r.HubConfigured == tri.Undetermined {
		r.HubConfigured = hubConfiguredState(src)
	}
	if !r.hubContactedSet {
		// NOTHING WAS CONTACTED, WHICH IS A DETERMINED FACT. Leaving the zero value here would
		// report "whether a hub was contacted could not be determined" for a request that never
		// went near one — the same shape of false statement this funnel exists to remove.
		r.HubContacted = tri.No
	}
}

// setHubContacted records what a path established about reaching the hub. Every path that consults
// a hub — or establishes that it cannot — goes through this rather than assigning the field, so
// that "somebody decided undetermined" is distinguishable from "nobody decided".
func (r *Response) setHubContacted(v tri.Value) {
	r.HubContacted = v
	r.hubContactedSet = true
}

// hubConfiguredState answers whether a hub is configured, without contacting one.
//
// A nil source is genuinely Undetermined — nobody told us — and that is left as the honest answer
// rather than being turned into a "no". This is the one place the third value is correct for this
// field, and it is reachable only when the daemon wired no configuration reader at all.
func hubConfiguredState(src Sources) tri.Value {
	if src.HubConfigured == nil {
		return tri.Undetermined
	}
	return src.HubConfigured()
}

// wire fills the text forms of the three-valued fields before serialisation. tri.Value is an int
// and would go over the wire as 0, 1 or 2 — where 0 is Undetermined and reads, to anything that did
// not know, as a falsy nothing. The text is what crosses; [Response.unwire] reads it back.
func (r *Response) wire() {
	r.HubConfiguredText = r.HubConfigured.String()
	r.HubContactedText = r.HubContacted.String()
	if r.Model != nil {
		r.Model.ConfiguredText = r.Model.Configured.String()
	}
}

func (r *Response) unwire() {
	r.HubConfigured = triFromText(r.HubConfiguredText)
	r.HubContacted = triFromText(r.HubContactedText)
	if r.Model != nil {
		r.Model.Configured = triFromText(r.Model.ConfiguredText)
	}
}

// triFromText is the inverse of [tri.Value.String]. Anything it does not recognise is Undetermined,
// never No — a text this build cannot parse has not determined anything.
func triFromText(s string) tri.Value {
	switch strings.TrimSpace(s) {
	case tri.Yes.String():
		return tri.Yes
	case tri.No.String():
		return tri.No
	default:
		return tri.Undetermined
	}
}

// MarshalResponse renders a response for the wire.
func MarshalResponse(r Response) ([]byte, error) {
	r.wire()
	return json.Marshal(r)
}

// UnmarshalResponse reads one back.
func UnmarshalResponse(b []byte) (Response, error) {
	var r Response
	if err := json.Unmarshal(b, &r); err != nil {
		return Response{}, err
	}
	r.unwire()
	return r, nil
}

// ParseScopes turns a comma-separated list into scopes, refusing anything outside the vocabulary.
//
// IT REFUSES RATHER THAN DROPPING. A caller that asked for `read,admin` and silently received
// `read` has been narrowed at the edge, which §4.5 forbids in as many words.
func ParseScopes(s string) ([]hub.Scope, error) {
	var out []hub.Scope
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sc := hub.Scope(part)
		if !hub.KnownScope(sc) {
			return nil, hub.Refusedf(hub.ErrUnknownScope, "%q is not one of %s", part, scopeList())
		}
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func scopeList() string {
	var parts []string
	for _, s := range hub.Vocabulary() {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, ", ")
}
