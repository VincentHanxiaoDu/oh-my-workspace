// Issue #11 — the version-facing half of search.
//
// ISSUE #15 OWNS SEARCH. Ranking, corpus statistics, scoping to a person or a group, and whatever
// index sits behind them are all its work, and none of them are here. What IS here is the one thing
// Issue #11 criterion 4 makes testable now and #15 must not change later: search matches the
// CURRENT version and names which version each result refers to.
//
// #11's `Related` section says the two Issues have to agree on what "latest" means for a note with
// a history. This file is that agreement, written once, so that #15 builds on it instead of
// deriving "latest" a second time.
package hub

import (
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// SearchHit is one result. It names a VERSION, not a note.
//
// CRITERION 4: "search results identify which version they refer to, and for an unmodified note
// that is the same identifier the timeline's sole entry carries". A result that named only a note
// would leave the reader to assume they are being shown today's text, which is exactly the silent
// substitution the Issue's journey paragraph objects to.
type SearchHit struct {
	Ref VersionRef
	// Title of the note as it stands now.
	Title string
	// Standing is always [tri.Yes] for a hit, and it is carried anyway rather than implied: the
	// surface renders it with the same [StandingLine] a direct read uses, so a person cannot end up
	// reading a search result and a version read side by side and having to work out whether the
	// silence in one of them meant the same as the sentence in the other.
	Standing tri.Value
}

// SearchLatest finds notes whose CURRENT version matches the term.
//
// TWO PROPERTIES, BOTH FROM CRITERION 4:
//
//   - Superseded text is not matched. A term that appears only in a version that has since been
//     amended away does not produce a hit. The obvious wrong implementation searches every version
//     and returns the note — which resurrects superseded text under a heading that says "here is
//     what this note says".
//   - Every hit names the current version's ref, which for a note that was never amended is
//     precisely the ref its one timeline entry carries.
//
// Visibility precedes everything (PRD §3.5). It filters with [Store.ListReadable] rather than
// walking the store, so notes the searcher may not read are not matched, not counted, and not
// mentioned — and the ids whose readability could not be worked out are RETURNED, not dropped, for
// the same reason ListReadable returns them: a searcher told "no results" when the truth is "I
// could not check three of them" has been handed a determined negative that nobody determined.
func SearchLatest(s *Store, reader PersonID, term string) (hits []SearchHit, undetermined []NoteID) {
	readable, undetermined := s.ListReadable(reader)
	t := strings.ToLower(strings.TrimSpace(term))
	if t == "" {
		return nil, undetermined
	}
	for _, n := range readable {
		latest := n.Latest()
		if !strings.Contains(strings.ToLower(latest.Body), t) && !strings.Contains(strings.ToLower(n.Title), t) {
			continue
		}
		hits = append(hits, SearchHit{
			Ref:      VersionRef{Note: n.ID, Number: latest.Number},
			Title:    n.Title,
			Standing: tri.Yes,
		})
	}
	return hits, undetermined
}
