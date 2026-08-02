package hub

import (
	"sort"
	"strconv"
	"sync"
)

// Scope names a capability, not a surface (PRD §4.5).
//
// ONE VOCABULARY, THREE SURFACES. Criterion 13: "the same scope name means the same thing on the
// CLI, the agent API and the hub — a scope that permits publishing a company-wide note permits it
// identically on all three, and a scope that does not permits it on none." The way to make that
// true is for there to be one list and one [Permits], with the CLI, the agent API schema and the
// store all reading from it. A per-surface table is how two of them drift.
type Scope string

const (
	// ScopeReadOwn — read notes this person authored.
	ScopeReadOwn Scope = "notes:read:own"
	// ScopeReadVisible — read what this person is permitted to read, which is exactly what
	// [CanRead] says and no more. This is the widest READ scope a colleague can hold or delegate.
	ScopeReadVisible Scope = "notes:read:visible"
	// ScopePublish — publish a note as this person, at any of the four visibilities.
	ScopePublish Scope = "notes:publish"
	// ScopeSetVisibility — change the visibility of a note this person authored.
	ScopeSetVisibility Scope = "notes:visibility:set"
	// ScopeManageGroups — change the hub's membership record (PRD §5.3).
	ScopeManageGroups Scope = "groups:manage"
	// ScopeReadAll — read EVERY note the hub holds, regardless of visibility.
	//
	// This is the operator scope, and PRD §2.4 is why it is written down instead of being an
	// unspoken capability of whoever has the database: "The hub is not exempt. Whoever operates it
	// can read what is published to it — that is stated because it is true, and no scope pretends
	// otherwise." Naming it is also what makes criterion 10 testable, because a colleague asking
	// for a grant carrying it is asking for more than they hold.
	ScopeReadAll Scope = "notes:read:all"
)

// vocabulary is the whole scope vocabulary. There is no second one.
var vocabulary = []Scope{
	ScopeReadOwn, ScopeReadVisible, ScopePublish, ScopeSetVisibility, ScopeManageGroups, ScopeReadAll,
}

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
	if !Permits(g.Scopes, ScopeReadVisible) && !Permits(g.Scopes, ScopeReadOwn) {
		return nil, Refusedf(ErrRefused, "grant %q carries no read scope", string(g.ID))
	}
	n, err := s.Read(id, g.Holder)
	if err != nil {
		return nil, err
	}
	if !Permits(g.Scopes, ScopeReadVisible) && n.Author != g.Holder {
		// The grant is the narrower "read my own notes". The note is readable by the person but
		// this grant was not asked to read it.
		return nil, Refusedf(ErrRefused, "grant %q may read only its holder's own notes", string(g.ID))
	}
	return n, nil
}
