package hub

import (
	"sort"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Reading references, forwards and in reverse (Issue #14, PRD §3.4).
//
// A REFERENCE LISTING IS A READ. PRD §3.5 — "ranking never surfaces the existence of something the
// searcher cannot read" — is about search, but a reference is a second doorway into the same
// corpus and it obeys the same rule. Nothing in this file decides readability for itself: every
// decision goes through [CanReadNote] (via [Store.Read] and [Store.ListReadable]), which is
// [CanRead]. A second implementation that agreed today is exactly what Issue #12's package comment
// forbids.

// ReferenceView is one reference as one reader sees it.
//
// It is NEVER in [StateHidden]. A reference whose target the reader may not read is removed before
// a listing exists, so there is no way for a surface to render one by mistake, no count that
// includes one, and no index it could leave a gap in. Criterion 7 is a property of the data a
// caller receives, not of the discipline of the caller.
type ReferenceView struct {
	Ref   Reference
	State RefState
}

// Listing is a note's outbound references as one reader sees them, together with that reader's
// rendering of the body.
type Listing struct {
	// Ref names exactly which version was listed, in Issue #11's spelling — `note-1@v3` — so the
	// answer to "which version does this reference set belong to" can be copied out of one command
	// and pasted into another. Issue #14's own Related note says a reference to a note with new
	// versions must say which version it means, and that "that answer comes from #11's definition
	// of latest": this branch was cut before #11 merged, and adopting VersionRef after merging it
	// is what honouring that note looks like. It is never the zero ref for a real listing.
	Ref VersionRef
	// Body is the note as this reader sees it: references rendered in their state for this reader,
	// and references this reader may not see removed without a trace.
	Body string
	// Refs are the references this reader may see, in the order they appear. Hidden ones are not
	// here and are not counted.
	Refs []ReferenceView
}

// Count is how many references this note has, FOR THIS READER.
//
// CRITERION 18: "Reference counts, 'N notes reference this', and any ordering derived from
// reference structure are computed over the reader's visible set only. A global count computed
// before visibility filtering is a disclosure and fails criterion 7 even if no title is shown."
// There is no other count on this type, so there is nothing for a surface to print by accident.
func (l Listing) Count() int { return len(l.Refs) }

// Undetermined is how many of this reader's references could not be worked out. A caller must say
// something about this — it is what makes an answer partial, and a partial answer reported as a
// complete one is the defect PRD §4.3 exists to prevent.
func (l Listing) Undetermined() int {
	n := 0
	for _, v := range l.Refs {
		if v.State == StateUndetermined {
			n++
		}
	}
	return n
}

// ResolveReference decides one reference's state for one reader.
//
// THE FOUR OUTCOMES ARE FOUR OUTCOMES, and the two that are easiest to confuse are the two the
// Issue names: a target that is GONE is [StateUnresolved] and is SHOWN (criterion 11); a target
// that EXISTS and this reader may not read is [StateHidden] and is shown as nothing at all
// (criterion 7). Rendering the second as the first would disclose that it exists, which criterion
// 12 calls a defect in as many words.
//
// A nil store is not an empty hub. It is a hub that could not be reached, so every reference is
// undetermined (criterion 14) — never "does not exist".
func ResolveReference(s *Store, r Reference, reader PersonID) RefState {
	if s == nil {
		return StateUndetermined
	}
	if reader == "" {
		// An unidentified reader is not a refusal and is not a resolution. The same judgement
		// [CanRead] makes, made here so that a caller who forgot to fill in a field does not get a
		// confident answer.
		return StateUndetermined
	}
	switch r.Kind {
	case RefNote:
		// DELEGATED, NOT REIMPLEMENTED. Store.Read runs CanReadNote, so the rule that decides
		// whether this reference is hidden is the same rule that decides whether the note itself
		// can be opened — there is no path by which a listing is more permissive than a read.
		_, err := s.Read(r.Note(), reader)
		switch {
		case err == nil:
			return StateResolved
		case Code(err) == ErrNoSuchNote.Code:
			return StateUnresolved
		case Code(err) == ErrRefused.Code:
			return StateHidden
		default:
			return StateUndetermined
		}

	case RefGroup:
		// The hub owns group membership (PRD §5.3) and a group is not itself a secret — every
		// narrowing to a group names one. A group the hub has no record of is GONE, which is
		// unresolved; a record that cannot be read is undetermined, via the only sanctioned
		// conversion.
		switch tri.FromError(s.Members().Knows(r.Group())) {
		case tri.Yes:
			return StateResolved
		case tri.No:
			return StateUnresolved
		default:
			return StateUndetermined
		}

	case RefPerson:
		// NOTE A LIMIT OF THE MODEL, RECORDED RATHER THAN PAPERED OVER. The hub's record of people
		// (Issue #12) has no per-person visibility: §5.3 gives the hub group membership and
		// nothing else, and no Issue has yet said that one colleague may be invisible to another.
		// So a person the hub knows is resolved for every reader, and a person it does not know is
		// unresolved. When a person's visibility exists, THIS is the branch that must consult it,
		// and it must produce StateHidden — not StateUnresolved, which would disclose them.
		known := false
		for _, p := range s.Members().People() {
			if p == r.Person() {
				known = true
				break
			}
		}
		if known {
			return StateResolved
		}
		return StateUnresolved

	default:
		// Not one of the three kinds. It was never a reference, and we have determined nothing.
		return StateUndetermined
	}
}

// OutboundReferences lists a note's references as reader may see them.
//
// version is a point on the note's timeline; 0 means the latest. CRITERION 3 falls out of where the
// references come from: they are parsed from THAT VERSION's body, so a reference added in v3 is
// simply not in v2's text. Nothing has to remember to keep a per-version index in step.
//
// The referencing note is read through [Store.Read] FIRST, so a reader who may not see the note
// gets the note's own refusal and learns nothing about its edges.
func OutboundReferences(s *Store, id NoteID, version int, reader PersonID) (Listing, error) {
	if s == nil {
		return Listing{}, Refusedf(ErrHubUnreachable, "note %q", string(id))
	}
	n, err := s.Read(id, reader)
	if err != nil {
		return Listing{}, err
	}
	v := n.Latest()
	if version != 0 {
		v, err = n.Version(version)
		if err != nil {
			return Listing{}, err
		}
	}

	// VISIBILITY IS SETTLED BEFORE ANYTHING IS RENDERED OR COUNTED (criterion 7). states is
	// computed for every reference first; the body rendering and the listing are both built from
	// it, so there is no ordering in which a surface sees a reference before its state.
	states := map[Reference]RefState{}
	for _, r := range ParseReferences(v.Body) {
		key := Reference{Kind: r.Kind, Target: r.Target}
		if _, done := states[key]; done {
			continue
		}
		states[key] = ResolveReference(s, r, reader)
	}
	stateOf := func(r Reference) RefState {
		return states[Reference{Kind: r.Kind, Target: r.Target}]
	}

	l := Listing{Ref: VersionRef{Note: n.ID, Number: v.Number}, Body: RenderBody(v.Body, stateOf)}
	for _, r := range ParseReferences(v.Body) {
		st := stateOf(r)
		if st == StateHidden {
			// Not listed, not counted, not numbered. The listing a reader receives is the listing
			// they would have received from a corpus in which the target does not exist.
			continue
		}
		l.Refs = append(l.Refs, ReferenceView{Ref: r, State: st})
	}
	return l, nil
}

// Backlinks is the answer to "what else was written about this" (PRD §3.4).
type Backlinks struct {
	// Target is what was asked about, echoed back.
	Target Reference
	// Notes are the referencing notes this reader may read, in publication order.
	Notes []*Note
	// Undetermined is how many notes could not be examined because whether this reader may read
	// THEM could not be worked out.
	//
	// IT IS A COUNT OF NOTES, NOT OF MATCHES, and that is not a rounding: to know whether such a
	// note references the target we would have to read it, and we have not established that this
	// reader may. Reporting it as a match would be a disclosure; reporting it as nothing would turn
	// "could not determine" into "determined to be nothing". It is neither, and a caller that
	// prints the list must print this too.
	Undetermined int
}

// ReferencesTo answers what references a target — a note, a person, or a group (criterion 6).
//
// IT NEVER LOOKS THE TARGET UP. That is criterion 9, and it is a property of the code shape rather
// than of a careful ordering: "asking about a target that exists but is invisible to the reader and
// asking about a target that does not exist produce the same observable outcome". This function has
// no branch on the target's existence because it never asks, so there is no timing difference, no
// error difference, and no result-size hint to tell the two apart. A version that validated the
// target first would be friendlier and would leak.
//
// The corpus is filtered by [Store.ListReadable] — that is [CanRead] again — BEFORE any body is
// parsed, so a note the reader may not read is never even examined for a match.
// An unreachable hub or an unidentified reader is an ERROR rather than an empty answer, because an
// empty answer is a determined "nothing was written about this" and neither of those established
// that. The error is undetermined-flavoured and its exit code is not the one an empty answer uses.
func ReferencesTo(s *Store, target Reference, reader PersonID) (Backlinks, error) {
	out := Backlinks{Target: Reference{Kind: target.Kind, Target: target.Target}}
	if s == nil {
		return out, Refusedf(ErrHubUnreachable, "what references a target is the hub's answer")
	}
	if reader == "" {
		return out, Refusedf(ErrUndetermined, "no reader was identified, so nothing was examined")
	}
	readable, undetermined := s.ListReadable(reader)
	out.Undetermined = len(undetermined)
	for _, n := range readable {
		for _, r := range ParseReferences(n.Latest().Body) {
			if r.SameTarget(target) {
				out.Notes = append(out.Notes, n)
				break
			}
		}
	}
	return out, nil
}

// Count is how many notes reference the target, FOR THIS READER — criterion 18 again. There is no
// global count on this type for the same reason there is none on [Listing].
func (b Backlinks) Count() int { return len(b.Notes) }

// TargetsOf returns the distinct targets a body references, ordered, for callers that want the set
// rather than the occurrences. Used by publication checking, which asks a question per target.
func TargetsOf(body string) []Reference {
	out := DistinctReferences(body)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Target < out[j].Target
	})
	return out
}
