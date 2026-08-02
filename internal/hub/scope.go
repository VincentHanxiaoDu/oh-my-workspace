package hub

import (
	"sort"
	"strconv"
	"sync"
)

// Scope names a capability, not a surface (PRD §4.5).
//
// THE VOCABULARY IS RULED, NOT CHOSEN HERE. Issue #12's `## Ruled` section fixes it at three names —
// `read`, `write`, `publish` — and Issue #19 owns it:
//
//	read     see tickets, drafts, and the hub content you are permitted to see
//	write    change local drafts and tickets
//	publish  send a note to the hub — a grant that must be asked for on purpose
//
// An earlier revision of this branch invented six longer names of its own. That was wrong: this
// Issue's criteria name these three concretely (criterion 13 spells out that `publish` permits
// publishing on all three surfaces and `read` and `write` permit it on none), and a vocabulary is
// exactly the kind of thing that must not be decided twice. Adopted as ruled.
//
// ONE VOCABULARY, THREE SURFACES. There is one list and one [Permits], read by the CLI, by the
// agent API schema and by the store. A per-surface table is how two of them drift.
//
// WHY `publish` IS ITS OWN GRANT AND NOT PART OF `write`: PRD §3.10 — "a token that can do the
// second was asked for on purpose". Writing a draft on your own machine and putting a note in front
// of the company are different acts, and the second is irreversible in the way that matters.
type Scope string

const (
	// ScopeRead — see tickets, drafts, and the hub content this person is permitted to see. What
	// "permitted to see" means is exactly [CanRead] and nothing wider.
	ScopeRead Scope = "read"
	// ScopeWrite — change local drafts and tickets. It does NOT reach the hub: a draft's intended
	// visibility can be edited under `write`, but making that draft a published note cannot.
	ScopeWrite Scope = "write"
	// ScopePublish — send a note to the hub, and change who can see one already there.
	//
	// CRITERION 10a PUTS SETTING VISIBILITY UNDER THIS SCOPE, not under `write`: "Setting a note's
	// visibility, or narrowing it later, is part of publishing that note and therefore requires the
	// publish scope." Widening a note to the whole company is a publication of it to everyone the
	// widening newly includes, and it would be strange for the act that first exposed a note to
	// need a deliberate grant while the act that exposes it to more people did not.
	ScopePublish Scope = "publish"
)

// vocabulary is the whole scope vocabulary. There is no second one.
var vocabulary = []Scope{ScopeRead, ScopeWrite, ScopePublish}

// Vocabulary returns every scope name, ordered. The CLI prints this, the agent API schema enumerates
// it, and the store validates against it — criterion 13 is the assertion that those are the same set.
func Vocabulary() []Scope {
	out := make([]Scope, len(vocabulary))
	copy(out, vocabulary)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// KnownScope reports whether s is in the vocabulary.
func KnownScope(s Scope) bool {
	for _, k := range vocabulary {
		if k == s {
			return true
		}
	}
	return false
}

// Permits reports whether a set of held scopes permits a capability.
//
// It is deliberately exact — holding ScopePublish permits publishing and nothing else. There is no
// hierarchy in which one scope quietly implies another, because "implied" is how something ends up
// wider than what was asked for, which §4.5 forbids in as many words.
func Permits(held []Scope, want Scope) bool {
	for _, h := range held {
		if h == want {
			return true
		}
	}
	return false
}

// Holder is whoever a grant would be issued on behalf of: a person and the scopes they themselves
// hold.
type Holder struct {
	Person PersonID
	Scopes []Scope
}

// EvaluateGrantRequest decides a grant request, and is the rule PRD §4.5 states:
//
//	"Nothing is implicitly wider than what was asked for. A grant that would let something read
//	 more than its holder can is not narrowed at the edge; it is refused when it is requested."
//
// So a request for a scope the holder does not hold is refused ENTIRELY — criterion 11: "the
// refused request never results in a narrower grant being issued instead; the caller does not
// receive a token at all". Returning the intersection would be the natural, helpful, wrong thing:
// the caller would get a token, believe it carries what it asked for, and discover the difference
// at the edge, later, in whatever the token was pointed at.
//
// On success it returns the scopes to issue, which are exactly the ones requested.
//
// THIS IS THE WHOLE OF THIS ISSUE'S AUTHORITY WORK. Issue #19 owns sign-in and token material; it
// must call this function to decide a request and must not re-derive the rule.
func EvaluateGrantRequest(h Holder, requested []Scope) ([]Scope, error) {
	if len(requested) == 0 {
		return nil, Refusedf(ErrUnknownScope, "a grant request named no scopes")
	}
	for _, r := range requested {
		if !KnownScope(r) {
			return nil, Refusedf(ErrUnknownScope, "%q", string(r))
		}
	}
	for _, r := range requested {
		if !Permits(h.Scopes, r) {
			return nil, Refusedf(ErrGrantWiderThanHolder, "%q holds no %q", string(h.Person), string(r))
		}
	}
	out := make([]Scope, len(requested))
	copy(out, requested)
	return out, nil
}

// GrantID identifies an issued grant.
type GrantID string

// Grant is an issued grant: who it acts as, and what it may do.
//
// IT IS NOT A TOKEN. There is no secret here, no expiry and no signature — Issue #19 owns those and
// will attach them. What this type exists for is criterion 11's assertion: "the set of tokens
// attributable to the person is unchanged after the refused request" needs a set to look at, and
// this ledger is the smallest honest one. #19 replaces the issuance, keeps the ledger's shape, and
// must keep calling [EvaluateGrantRequest].
type Grant struct {
	ID     GrantID
	Holder PersonID
	Scopes []Scope
}

// Ledger records issued grants so that a person can see what has been signed in as them (PRD §3.10)
// and so that a refusal can be shown to have issued nothing.
type Ledger struct {
	mu     sync.Mutex
	grants []Grant
	next   int
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger { return &Ledger{} }

// Request evaluates and, if permitted, records a grant.
//
// On refusal it returns a zero Grant and the refusal, having recorded NOTHING. That is criterion
// 11, and it is why evaluation happens before the counter is touched.
func (l *Ledger) Request(h Holder, requested []Scope) (Grant, error) {
	scopes, err := EvaluateGrantRequest(h, requested)
	if err != nil {
		return Grant{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	g := Grant{ID: GrantID(string(h.Person) + "-grant-" + strconv.Itoa(l.next)), Holder: h.Person, Scopes: scopes}
	l.grants = append(l.grants, g)
	return g, nil
}

// Grants returns the grants attributable to a person.
func (l *Ledger) Grants(p PersonID) []Grant {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Grant
	for _, g := range l.grants {
		if g.Holder == p {
			out = append(out, g)
		}
	}
	return out
}

// ReadThrough is what a grant holder — a person's own AI, a script — may read (PRD §3.12: "an agent
// cannot read what its person cannot").
//
// It is [Store.Read] with the grant's scopes checked first, and it reads AS THE GRANT'S HOLDER, not
// as the grant. Criterion 12: a note narrowed to exclude the person is not readable through their
// agent API or any token they hold, and the refusal is distinguishable from "no such note" —
// both of which fall out of delegating to Read rather than reimplementing the check.
func ReadThrough(s *Store, g Grant, id NoteID) (*Note, error) {
	if !Permits(g.Scopes, ScopeRead) {
		return nil, Refusedf(ErrReadScopeRequired, "grant %q carries no %q scope", string(g.ID), string(ScopeRead))
	}
	return s.Read(id, g.Holder)
}

// PublishThrough is the write-path analogue of [ReadThrough]: publishing a note through a grant.
//
// CRITERION 10a. A grant carrying only `read` — or only `write` — cannot publish. The scope is
// checked BEFORE anything is stored, so a refusal leaves the hub exactly as it was; and the refusal
// carries its own code, so a caller tells it from success without parsing prose.
//
// The note is published AS THE GRANT'S HOLDER. A publication naming a different author is refused
// rather than quietly re-attributed: PRD §3.10 says a client "authenticates as them, and so does
// anything they delegate to", and silently rewriting the author would make a grant a way to put
// words in somebody else's mouth.
func PublishThrough(s *Store, g Grant, p Publication) (*Note, error) {
	if !Permits(g.Scopes, ScopePublish) {
		return nil, Refusedf(ErrPublishScopeRequired, "grant %q carries no %q scope", string(g.ID), string(ScopePublish))
	}
	if p.Author == "" {
		p.Author = g.Holder
	}
	if p.Author != g.Holder {
		return nil, Refusedf(ErrRefused, "grant %q acts as %q and cannot publish as %q",
			string(g.ID), string(g.Holder), string(p.Author))
	}
	return s.Publish(p)
}

// SetVisibilityThrough changes who can see a note, through a grant.
//
// CRITERION 10a, AND ITS THIRD CLAUSE IS THE ONE TO BE CAREFUL ABOUT: "the note's visibility is
// unchanged afterwards". A refusal that has already written is a refusal in name only — it passes
// "was refused" and "looks different from success" while having done the thing. So the scope check
// is the FIRST statement in this function, before any call that can mutate, and the test for it
// asserts the stored visibility rather than only the returned error.
func SetVisibilityThrough(s *Store, g Grant, id NoteID, v Visibility) (*Note, error) {
	if !Permits(g.Scopes, ScopePublish) {
		return nil, Refusedf(ErrPublishScopeRequired,
			"grant %q carries no %q scope, and changing who can see a note is part of publishing it",
			string(g.ID), string(ScopePublish))
	}
	return s.SetVisibility(id, g.Holder, v)
}
