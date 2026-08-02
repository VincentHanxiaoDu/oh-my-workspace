package hub

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"

// CanRead is THE visibility predicate. Everything that decides whether somebody may see something
// calls this, and nothing reimplements it.
//
// PRD §3.5: "Visibility is a precondition of ranking. What a searcher may see is settled before how
// results are ordered; ranking never surfaces the existence of something the searcher cannot read."
// Issue #15 builds search on top of this function — that is why it is a free function over values
// rather than a method on [Store]: ranking can call it in a loop without holding a store, and
// cannot end up with a second rule that agrees with this one only until one of them changes.
// Issues #13 (corpus statistics), #14 (references) and #22 (departed colleagues' notes) filter with
// it too. Do not add a second path.
//
// THE ANSWER IS THREE-VALUED, and the third value is load-bearing:
//
//	tri.Yes           reader may read it
//	tri.No            reader may not read it — a determined negative
//	tri.Undetermined  it could not be worked out: the group's membership could not be resolved,
//	                  no membership record was supplied, the visibility was never set, or the
//	                  reader is unidentified
//
// An Undetermined answer must NEVER be rendered or acted on as either of the other two
// (criteria 16 and 17). A caller that treats Undetermined as Yes leaks; a caller that treats it as
// No has turned "I could not check" into "the answer is no", which is the defect the whole tri
// package exists to make hard.
//
// author is the note's author. reader is who is asking. m is the hub's membership record, and may
// be nil — a nil record cannot resolve a group, so a group narrowing is Undetermined rather than
// No. The other three states do not consult m at all, which is what makes them evaluable with no
// membership record and, per criterion 14, with no directory integration anywhere in the process.
func CanRead(v Visibility, author, reader PersonID, m Membership) tri.Value {
	// AN UNIDENTIFIED READER IS NOT A REFUSAL. We were not told who is asking, so we did not work
	// out whether they may read it. Answering No here would let an empty PersonID — the zero value
	// of a field somebody forgot to fill — read as a clean, confident denial.
	if reader == "" {
		return tri.Undetermined
	}

	// The author always reads their own note. This holds in every state including KindSelf, and it
	// is what "yourself" means.
	if author != "" && reader == author {
		return tri.Yes
	}

	switch v.kind {
	case KindCompany:
		return tri.Yes

	case KindPeople:
		for _, p := range v.people {
			if p == reader {
				return tri.Yes
			}
		}
		return tri.No

	case KindGroup:
		if m == nil {
			// NO RECORD IS NOT AN EMPTY RECORD. Without the hub's membership record we cannot say
			// who is in the group, so we have not determined anything. Criterion 21's "no hub
			// configured" surface renders from this.
			return tri.Undetermined
		}
		// tri.FromError is the only sanctioned (bool, error) conversion: an error — an unknown
		// group, an unreadable record — is Undetermined, never No.
		return tri.FromError(m.IsMember(v.group, reader))

	case KindSelf:
		// Reached only when reader != author, which is a determined No. A membership record that
		// claims everybody is in every group cannot change this, because m is never consulted —
		// criterion 4, and it is a property of the code shape, not of a lucky ordering.
		return tri.No

	case KindUnset:
		// A note whose visibility was never expressed has not been determined. [Store.Publish]
		// makes this unreachable for stored notes by applying [Default]; it is reachable for a
		// Visibility a caller built by hand or left zero, and for that caller the honest answer is
		// that nobody has said yet.
		return tri.Undetermined

	default:
		// An impossible Kind is, precisely, a state that could not be determined.
		return tri.Undetermined
	}
}

// CanReadNote applies [CanRead] to a note, using the note's CURRENT visibility.
//
// CRITERION 6 LIVES HERE. A note's earlier versions are addressable (PRD §3.3), and it would be an
// easy and entirely natural mistake to evaluate a version against the visibility the note had when
// that version was written — which would leave every reader who was ever included able to read the
// history of a note that has since been narrowed away from them. The timeline is not a bypass:
// there is only one visibility on a note and it governs the whole note, every version of it. That
// is why Version carries no visibility field at all — see [Version].
func CanReadNote(n *Note, reader PersonID, m Membership) tri.Value {
	if n == nil {
		return tri.Undetermined
	}
	return CanRead(n.Visibility, n.Author, reader, m)
}
