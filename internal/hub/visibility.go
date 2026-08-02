package hub

import (
	"fmt"
	"sort"
	"strings"
)

// PersonID identifies a colleague in the hub's own record of people. It is the hub's identifier,
// not an email address the company's directory happens to own (PRD §5.3).
type PersonID string

// GroupID identifies a group in the hub's own membership record.
type GroupID string

// Kind is which of the four visibility states a note is in — plus the zero value, which is none of
// them.
type Kind int

const (
	// KindUnset is the ZERO VALUE, and it is deliberately not KindCompany.
	//
	// Criterion 1 says a note published with no choice expressed is company-wide, and it is
	// tempting to make the zero value do that work. That would be wrong in the other direction: a
	// Visibility left zero by an error path, a partial decode or a half-built struct would then
	// read as "the whole company may read this", and the failure mode of that mistake is a note
	// exposed to people the author excluded. So the zero value is not an audience at all, and
	// [Store.Publish] is the one place that turns "no choice expressed" into company-wide. Nothing
	// stored in the hub is ever KindUnset, and a test asserts it.
	KindUnset Kind = iota
	// KindCompany — every colleague at this company may read it. The product default (PRD §3.3).
	KindCompany
	// KindPeople — only the named people.
	KindPeople
	// KindGroup — only the current members of one group, per the hub's own membership record.
	KindGroup
	// KindSelf — only the author.
	KindSelf
)

// Visibility is who may read a note. It is a value: comparable by [Visibility.Equal], safe to copy,
// and immutable once built because every field is unexported and every constructor copies its input.
//
// It carries no author. "Yourself" is relative to the note's author, and a Visibility that carried
// its own owner could disagree with the note it is attached to. [CanRead] takes the author
// explicitly for that reason.
type Visibility struct {
	kind   Kind
	people []PersonID // sorted and deduplicated by newPeople; nil unless kind == KindPeople
	group  GroupID    // empty unless kind == KindGroup
}

// CompanyWide is the product default (PRD §3.3): "a knowledge system that defaults to private has
// no knowledge in it".
func CompanyWide() Visibility { return Visibility{kind: KindCompany} }

// Default is what a publication with no visibility choice expressed means.
//
// It exists as a NAMED function rather than as a comment on CompanyWide because criterion 1 is
// about the default specifically, and a later change that wants a different default must change
// one obvious thing and watch a test that is about the default go red.
func Default() Visibility { return CompanyWide() }

// SelfOnly narrows a note to its author. Every other colleague account is excluded, including one
// that belongs to every group (criterion 4).
func SelfOnly() Visibility { return Visibility{kind: KindSelf} }

// ToPeople narrows a note to the named people. The author may always read their own note, so the
// author need not be named.
//
// Naming nobody is refused rather than accepted as an audience of zero.
func ToPeople(people ...PersonID) (Visibility, error) {
	p := normalisePeople(people)
	if len(p) == 0 {
		return Visibility{}, ErrEmptyAudience
	}
	return Visibility{kind: KindPeople, people: p}, nil
}

// ToGroup narrows a note to one group. Whether the hub KNOWS the group is not decided here — this
// is a value constructor, and refusing an unknown group is a publication-time act with a store to
// check against (see [Store.Publish] and criterion 15).
func ToGroup(g GroupID) (Visibility, error) {
	if strings.TrimSpace(string(g)) == "" {
		return Visibility{}, Refusedf(ErrUnknownVisibility, "a group narrowing needs a group name")
	}
	return Visibility{kind: KindGroup, group: g}, nil
}

func normalisePeople(in []PersonID) []PersonID {
	seen := map[PersonID]bool{}
	out := make([]PersonID, 0, len(in))
	for _, p := range in {
		p = PersonID(strings.TrimSpace(string(p)))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Kind reports which of the four states this is, or KindUnset.
func (v Visibility) Kind() Kind { return v.kind }

// IsUnset reports whether no choice has been expressed. Callers use this to apply [Default];
// nothing else should branch on it, because a stored note is never unset.
func (v Visibility) IsUnset() bool { return v.kind == KindUnset }

// People returns a copy of the named people, or nil.
func (v Visibility) People() []PersonID {
	if v.kind != KindPeople {
		return nil
	}
	out := make([]PersonID, len(v.people))
	copy(out, v.people)
	return out
}

// Group returns the group, or "".
func (v Visibility) Group() GroupID {
	if v.kind != KindGroup {
		return ""
	}
	return v.group
}

// Equal reports whether two visibilities are the same choice. Slices make == unavailable.
func (v Visibility) Equal(o Visibility) bool {
	if v.kind != o.kind || v.group != o.group || len(v.people) != len(o.people) {
		return false
	}
	for i := range v.people {
		if v.people[i] != o.people[i] {
			return false
		}
	}
	return true
}

// Token is the short machine-readable name of the state: "company", "people", "group", "self", or
// "unset".
//
// Criterion 1 asks that reading a note's visibility back report `company` — "not an empty value,
// not 'unset', not a null that a caller must interpret". A stored note never reports "unset"
// because a stored note is never unset; the word exists here only so that an unset value in a
// caller's hand renders as something other than a real state.
func (v Visibility) Token() string {
	switch v.kind {
	case KindCompany:
		return "company"
	case KindPeople:
		return "people"
	case KindGroup:
		return "group"
	case KindSelf:
		return "self"
	default:
		return "unset"
	}
}

// Describe renders the state for a person to read.
//
// CRITERION 5 IS ABOUT THIS FUNCTION: "two different narrowings never render identically". The
// test for it compares the renderings PAIRWISE rather than each against a literal, because a
// literal-by-literal test passes just as happily after two of them have been edited into the same
// sentence.
//
// [UndeterminedDescription] is the fifth rendering and is not produced here — a Visibility is a
// choice, and "could not be determined" is an outcome of evaluating one. The pairwise test includes
// it anyway, because criterion 16 is precisely that it differs from company-wide and from self-only.
func (v Visibility) Describe() string {
	switch v.kind {
	case KindCompany:
		return "company-wide — every colleague at this company can read this note"
	case KindPeople:
		names := make([]string, 0, len(v.people))
		for _, p := range v.people {
			names = append(names, string(p))
		}
		return fmt.Sprintf("named people — only the %d named (%s) and the author can read this note",
			len(names), strings.Join(names, ", "))
	case KindGroup:
		return fmt.Sprintf("group — only the current members of %q, per the hub's own membership record, and the author can read this note", string(v.group))
	case KindSelf:
		return "yourself — no other colleague can read this note, not even a member of every group"
	default:
		return "no visibility choice has been expressed"
	}
}

// UndeterminedDescription is how a visibility that could not be determined renders.
//
// It is a constant rather than a call into tri because it must read as a sentence about visibility
// next to the four above, and because criterion 16 requires it be distinguishable from BOTH a real
// value and a negative one. Its wording deliberately contains neither "company" nor "only you".
const UndeterminedDescription = "could not be determined — this is not company-wide and it is not self-only; the hub, the group's membership, or the record itself could not be read"

// AllDescriptions returns the five renderings — the four states and the undetermined one — for the
// pairwise-distinctness test and for the CLI's list of choices.
func AllDescriptions() map[string]string {
	return map[string]string{
		"company":      CompanyWide().Describe(),
		"people":       mustPeople("alice", "bo").Describe(),
		"group":        mustGroup("platform").Describe(),
		"self":         SelfOnly().Describe(),
		"undetermined": UndeterminedDescription,
	}
}

// mustPeople and mustGroup build example values for [AllDescriptions]. They panic on an error that
// cannot happen with the literals above; a panic in a package init path is found by every test.
func mustPeople(p ...PersonID) Visibility {
	v, err := ToPeople(p...)
	if err != nil {
		panic("hub: example people visibility is invalid: " + err.Error())
	}
	return v
}

func mustGroup(g GroupID) Visibility {
	v, err := ToGroup(g)
	if err != nil {
		panic("hub: example group visibility is invalid: " + err.Error())
	}
	return v
}

// ParseChoice turns a person's words into a Visibility. It is the ONE parser, shared by the CLI and
// by the agent API, so that "group:platform" cannot mean two things (PRD §4.5 read across to the
// choice itself).
//
// Accepted:
//
//	""                     -> Default() (company-wide; criterion 1)
//	"company"              -> company-wide
//	"self" | "me"          -> yourself
//	"group:<name>"         -> that group
//	"people:<a>,<b>"       -> those named people
func ParseChoice(s string) (Visibility, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return Default(), nil
	}
	switch {
	case strings.EqualFold(t, "company"):
		return CompanyWide(), nil
	case strings.EqualFold(t, "self"), strings.EqualFold(t, "me"):
		return SelfOnly(), nil
	}
	name, rest, found := strings.Cut(t, ":")
	if !found {
		return Visibility{}, Refusedf(ErrUnknownVisibility, "%q", s)
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "group":
		return ToGroup(GroupID(strings.TrimSpace(rest)))
	case "people":
		parts := strings.Split(rest, ",")
		ids := make([]PersonID, 0, len(parts))
		for _, p := range parts {
			ids = append(ids, PersonID(strings.TrimSpace(p)))
		}
		return ToPeople(ids...)
	default:
		return Visibility{}, Refusedf(ErrUnknownVisibility, "%q", s)
	}
}

// ChoiceSyntax is the accepted spelling of a choice, shown wherever a choice is offered.
var ChoiceSyntax = []string{
	"company            every colleague at this company (the default if you say nothing)",
	"people:a,b         only the people you name",
	"group:<name>       only the current members of that group, per the hub's membership record",
	"self               only you",
}

// IsNarrowing reports whether a choice narrows a note away from company-wide. The §2.4 statement is
// required wherever a narrowing can be CHOSEN — which in practice means wherever any of the four
// can be chosen, since the surface offers them together.
func (v Visibility) IsNarrowing() bool {
	return v.kind == KindPeople || v.kind == KindGroup || v.kind == KindSelf
}
