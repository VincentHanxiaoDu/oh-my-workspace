package hub

// Search — Issue #15. Finding an answer in the corpus, scoped to a person, a group, or the company.
//
// # The whole design is one sentence from the PRD
//
// PRD §3.5: "Visibility is a precondition of ranking. What a searcher may see is settled before how
// results are ordered; ranking never surfaces the existence of something the searcher cannot read."
//
// That sentence is implemented as a TYPE, not as a comment and not as an ordering of statements
// that a later edit can swap. [Corpus] is the settled set of notes a searcher may read. Its fields
// are unexported and there is exactly one way to build one — [Settle] / [SettleWith], which filter
// through [CanRead] by way of [Store.ListReadable]. Every ranking, counting, snippeting,
// suggesting and faceting function in this file is a method on Corpus and takes no other source of
// notes. So "filter after ranking" is not a bug that can be introduced here by reordering two
// lines: there is nothing to rank but the corpus, and the corpus has already been filtered.
//
// # Why that is not enough on its own, and what makes it observable
//
// A structural guarantee that nobody can drive is a claim. The observable consequence chosen here
// is that RANKING IS CORPUS-RELATIVE: a term's weight is its inverse document frequency over the
// settled corpus (see [Corpus.Search]). If an unreadable note were present while scores were
// computed and removed afterwards, it would change the document frequency of the terms it
// contains, and the RELATIVE ORDER OF TWO READABLE NOTES would change with it. That is a real leak
// channel — criterion 9 names it, "no positional artefact ... from which the presence of a withheld
// note could be inferred" — and it is also the thing that makes filtering-before and
// filtering-after distinguishable from the outside. TestOrderingDoesNotLeak drives exactly that
// flip.
//
// # The three-valued answer, in a place it is easy to get wrong
//
// [Store.ListReadable] returns the ids whose readability could not be determined SEPARATELY from
// the readable notes. Search neither includes them nor drops them:
//
//   - they are NOT in [Corpus.Notes], so they cannot be returned, snippeted or suggested from;
//   - they are NOT in the corpus statistics, so they cannot influence ordering;
//   - they ARE counted in [Outcome.Undetermined] and they make [Outcome.Coverage] undetermined,
//     which makes the whole search report as INCOMPLETE and exit non-zero.
//
// That is a decision the Issue did not settle and it is recorded in the pull request body. The
// alternative — saying nothing — would render "I could not tell whether there is more" as "there is
// no more", which is the one collapse this project forbids everywhere else.
//
// # What a "search scope" is, and what it is not
//
// [SearchScope] (person / group / company) is the SUBJECT of a search. It is NOT the capability
// vocabulary. That vocabulary is ruled at exactly three names — read, write, publish (see [Scope])
// — and this file adds nothing to it. Searching requires [ScopeRead] and nothing else; there is no
// search-admin scope and the hub operator's ability to read everything remains a deployment fact.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Directory is what search needs in order to answer "does the scope you named exist?".
//
// It is a narrow interface rather than *[Record] so that a record which CANNOT BE READ is
// expressible. Criterion 19: where search cannot determine whether a scope exists, that is
// undetermined, never "no such group". An in-memory record never fails; the interface is written
// for the persistent one that will.
type Directory interface {
	Knows(g GroupID) (bool, error)
	KnowsPerson(p PersonID) (bool, error)
	IsMember(g GroupID, p PersonID) (bool, error)
}

// Search-specific refusals.
//
// They are declared here rather than in errors.go because errors.go is Issue #12's merged work.
// TestSearchErrorsAreDistinguishable asserts them pairwise-distinct against that file's list, so
// living in a second file costs nothing in coverage.
var (
	// ErrUnknownSearchScope — the search named a person or a group the hub has no record of.
	//
	// CRITERION 4 REQUIRES THIS TO EXIST AND THERE IS A REAL TENSION IN IT, recorded rather than
	// quietly resolved: an unknown-scope answer confirms, by contrast, which names DO exist. The
	// Issue asks for the distinction in as many words ("must not produce identical output"), so it
	// is implemented; it discloses the existence of a PERSON OR GROUP in the hub's roster, never
	// the existence of a NOTE, which is what criteria 6-10 protect.
	ErrUnknownSearchScope = &Error{Code: "unknown-search-scope", Msg: "refused: the hub has no record of that person or group"}

	// ErrNotSignedIn — there is no identity to search as. Criterion 16, and distinct from
	// ErrReadScopeRequired: "I do not know who you are" and "I know who you are and your token
	// cannot do this" are fixed by different actions.
	ErrNotSignedIn = &Error{Code: "not-signed-in", Msg: "not signed in, so there is no identity to search as"}
)

// searchErrors is this file's contribution to the pairwise-distinctness test.
var searchErrors = []*Error{ErrUnknownSearchScope, ErrNotSignedIn}

// SearchScopeKind is which of the three subjects a search is scoped to (PRD §3.5).
type SearchScopeKind int

const (
	// SearchCompany is the whole company — every author and every group the searcher may read.
	// It is the ZERO VALUE, and here that is right: "company-wide" is what an unscoped search
	// means, exactly as company-wide is what an unnarrowed note means (PRD §3.3). Unlike
	// [KindUnset] this cannot expose anything, because the searcher still only ever sees what
	// [CanRead] says they may.
	SearchCompany SearchScopeKind = iota
	// SearchPerson narrows to one author.
	SearchPerson
	// SearchGroup narrows to one group.
	SearchGroup
)

// SearchScope is the subject of a search: one person, one group, or the company.
type SearchScope struct {
	kind   SearchScopeKind
	person PersonID
	group  GroupID
}

// CompanyScope searches everything the searcher may read.
func CompanyScope() SearchScope { return SearchScope{kind: SearchCompany} }

// PersonScope searches one author's notes.
func PersonScope(p PersonID) (SearchScope, error) {
	if strings.TrimSpace(string(p)) == "" {
		return SearchScope{}, Refusedf(ErrUnknownSearchScope, "a person scope needs a name")
	}
	return SearchScope{kind: SearchPerson, person: p}, nil
}

// GroupScope searches one group.
func GroupScope(g GroupID) (SearchScope, error) {
	if strings.TrimSpace(string(g)) == "" {
		return SearchScope{}, Refusedf(ErrUnknownSearchScope, "a group scope needs a name")
	}
	return SearchScope{kind: SearchGroup, group: g}, nil
}

// Kind reports which of the three this is.
func (s SearchScope) Kind() SearchScopeKind { return s.kind }

// Person returns the scoped author, or "".
func (s SearchScope) Person() PersonID { return s.person }

// Group returns the scoped group, or "".
func (s SearchScope) Group() GroupID { return s.group }

// Token is the short machine-readable name: "company", "person:<id>", "group:<id>".
func (s SearchScope) Token() string {
	switch s.kind {
	case SearchPerson:
		return "person:" + string(s.person)
	case SearchGroup:
		return "group:" + string(s.group)
	default:
		return "company"
	}
}

// ParseSearchScope turns a person's words into a scope. It is the ONE parser, for the same reason
// [ParseChoice] is: two spellings of "group:platform" is two behaviours waiting to diverge.
//
//	""                 -> company-wide
//	"company"          -> company-wide
//	"person:<id>"      -> that author
//	"group:<id>"       -> that group
func ParseSearchScope(s string) (SearchScope, error) {
	t := strings.TrimSpace(s)
	if t == "" || strings.EqualFold(t, "company") {
		return CompanyScope(), nil
	}
	name, rest, found := strings.Cut(t, ":")
	if !found {
		return SearchScope{}, Refusedf(ErrUnknownSearchScope, "%q is not a scope; say company, person:<id> or group:<id>", s)
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "person":
		return PersonScope(PersonID(strings.TrimSpace(rest)))
	case "group":
		return GroupScope(GroupID(strings.TrimSpace(rest)))
	default:
		return SearchScope{}, Refusedf(ErrUnknownSearchScope, "%q is not a scope; say company, person:<id> or group:<id>", s)
	}
}

// SearchScopeSyntax is the accepted spelling, shown wherever a scope is offered.
var SearchScopeSyntax = []string{
	"company            everything you may read, across every author and group (the default)",
	"person:<id>        notes written by that colleague",
	"group:<id>         notes belonging to that group, per the hub's own membership record",
}

// Corpus is WHAT THE SEARCHER MAY READ, settled before anything is ordered.
//
// Its fields are unexported and [Settle]/[SettleWith] are the only constructors, so a caller cannot
// hand the ranker a set of notes that has not been through [CanRead]. That is the type-level form
// of PRD §3.5.
type Corpus struct {
	reader       PersonID
	notes        []*Note
	undetermined []NoteID
	dir          Directory
	roster       *Roster
}

// Settle filters a store's notes through the visibility predicate for one reader.
//
// THIS IS THE PRECONDITION IN PRD §3.5, and it happens here, once, before any Search method exists
// to be called. It delegates to [Store.ListReadable], which is the merged Issue #12 path that calls
// [CanReadNote] — search does NOT reimplement the rule and does not have a second copy of it.
func Settle(s *Store, reader PersonID) Corpus {
	return SettleWith(s, reader, s.Members(), nil)
}

// SettleWith is [Settle] with the directory and roster supplied explicitly, for a hub whose people
// record is not the store's own and for tests that need a directory which fails.
func SettleWith(s *Store, reader PersonID, dir Directory, roster *Roster) Corpus {
	readable, undetermined := s.ListReadable(reader)
	if dir == nil {
		dir = s.Members()
	}
	return Corpus{reader: reader, notes: readable, undetermined: undetermined, dir: dir, roster: roster}
}

// Reader is who this corpus was settled for.
func (c Corpus) Reader() PersonID { return c.reader }

// Size is how many notes the searcher may read. It is the N in the corpus statistics, and it
// deliberately excludes both the unreadable and the undetermined.
func (c Corpus) Size() int { return len(c.notes) }

// UndeterminedIDs are the notes whose readability could not be worked out. Returned so a caller
// must say something about them; never merged into the readable set.
func (c Corpus) UndeterminedIDs() []NoteID {
	out := make([]NoteID, len(c.undetermined))
	copy(out, c.undetermined)
	return out
}

// Query is a search.
type Query struct {
	// Terms is what the person typed.
	Terms string
	// Scope is the subject: a person, a group, or the company.
	Scope SearchScope
}

// Result is one hit. Every field on it is derived from a note in the [Corpus] and therefore from a
// note the searcher may read — criterion 7 is a property of where this struct can be built.
type Result struct {
	ID      NoteID
	Title   string
	Author  PersonID
	Version int
	Score   float64
	Snippet string
}

// Outcome is everything a search emits. EVERY FIELD IS AN OBSERVABLE, and criteria 6-10 are the
// requirement that every one of them is identical between a corpus with a hidden note and one
// without.
type Outcome struct {
	Terms   string
	Scope   SearchScope
	Results []Result
	// Total is the number of matches. There is no "showing N of M": M would be a second number
	// that a filtering-after implementation would get wrong, and criterion 6 forbids exactly that.
	Total int
	// Authors is the author facet.
	Authors []PersonID
	// Suggestions are "did you mean" terms, drawn ONLY from the corpus vocabulary (criterion 8).
	Suggestions []string
	// Undetermined is how many notes could not be evaluated for readability. Never folded into
	// Total, never silent.
	Undetermined int
	// Coverage is whether the whole corpus was covered. tri.Yes is a complete answer; anything else
	// means the result is INCOMPLETE (criterion 17) and must not be presented as a complete one.
	Coverage tri.Value
	// DepartedScope is set when the scope named a colleague the roster reports as departed. It is a
	// fact about the SCOPE, not about any note, so it is identical between control and test runs.
	DepartedScope tri.Value
}

// Search runs a query over the settled corpus.
//
// THE ORDER OF WORK HERE IS THE ISSUE. Visibility is already settled — it happened in [Settle],
// before this method could be called at all. What remains:
//
//  1. resolve the scope (does the person or group exist? criterion 4, criterion 19)
//  2. restrict the corpus to the scope
//  3. compute corpus statistics over the SETTLED corpus
//  4. score and order
//
// Step 3 reads c.notes and nothing else. That is what makes criterion 9 drivable: an
// implementation that computed statistics over all notes and filtered at step 4 would order two
// readable notes differently, and TestOrderingDoesNotLeak asserts it does not.
func (c Corpus) Search(q Query) (Outcome, error) {
	if err := c.resolveScope(q.Scope); err != nil {
		return Outcome{}, err
	}

	out := Outcome{
		Terms:         q.Terms,
		Scope:         q.Scope,
		Coverage:      tri.Yes,
		Undetermined:  len(c.undetermined),
		DepartedScope: tri.Undetermined,
		Results:       []Result{},
		Authors:       []PersonID{},
		Suggestions:   []string{},
	}
	if out.Undetermined > 0 {
		// COULD NOT DETERMINE, NOT DETERMINED TO BE NOTHING. There may be more that matches; we
		// were unable to find out. The whole result is therefore incomplete.
		out.Coverage = tri.Undetermined
	}
	if q.Scope.kind == SearchPerson {
		out.DepartedScope = c.roster.Active(q.Scope.person)
	}

	// (2) Scope restriction. A note whose scope membership cannot be resolved is neither included
	// nor silently excluded: it makes the coverage undetermined, exactly like an unreadable-unknown.
	inScope := make([]*Note, 0, len(c.notes))
	for _, n := range c.notes {
		switch c.inScope(n, q.Scope) {
		case tri.Yes:
			inScope = append(inScope, n)
		case tri.No:
			// determined not in scope
		default:
			out.Undetermined++
			out.Coverage = tri.Undetermined
		}
	}

	terms := tokenise(q.Terms)
	if len(terms) == 0 {
		return out, nil
	}

	// (3) Corpus statistics over the SETTLED corpus. c.notes, never a wider set.
	n := float64(len(c.notes))
	df := map[string]int{}
	for _, note := range c.notes {
		for t := range termSet(noteText(note)) {
			df[t]++
		}
	}

	// (4) Score and order.
	type scored struct {
		r   Result
		ord int
	}
	var hits []scored
	for i, note := range inScope {
		text := noteText(note)
		counts := termCounts(text)
		score := 0.0
		matched := false
		for _, t := range terms {
			tf := counts[t]
			if tf == 0 {
				continue
			}
			matched = true
			d := df[t]
			if d == 0 {
				continue
			}
			score += float64(tf) * math.Log(n/float64(d))
		}
		if !matched {
			continue
		}
		hits = append(hits, scored{
			r: Result{
				ID:      note.ID,
				Title:   note.Title,
				Author:  note.Author,
				Version: note.Latest().Number,
				Score:   score,
				Snippet: snippet(note.Latest().Body, terms),
			},
			ord: i,
		})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].r.Score != hits[b].r.Score {
			return hits[a].r.Score > hits[b].r.Score
		}
		return hits[a].ord < hits[b].ord
	})

	authorSeen := map[PersonID]bool{}
	for _, h := range hits {
		out.Results = append(out.Results, h.r)
		if !authorSeen[h.r.Author] {
			authorSeen[h.r.Author] = true
			out.Authors = append(out.Authors, h.r.Author)
		}
	}
	out.Total = len(out.Results)
	sort.Slice(out.Authors, func(a, b int) bool { return out.Authors[a] < out.Authors[b] })
	out.Suggestions = c.suggest(terms, df)
	return out, nil
}

// resolveScope answers criterion 4 and criterion 19: an unknown person or group is refused as an
// UNKNOWN SCOPE, and a directory that cannot answer makes the question undetermined rather than
// producing "no such group".
func (c Corpus) resolveScope(s SearchScope) error {
	switch s.kind {
	case SearchPerson:
		known, err := c.dir.KnowsPerson(s.person)
		if err != nil {
			return Refusedf(ErrUndetermined, "whether %q is a colleague here could not be determined", string(s.person))
		}
		if !known {
			return Refusedf(ErrUnknownSearchScope, "person %q", string(s.person))
		}
	case SearchGroup:
		known, err := c.dir.Knows(s.group)
		if err != nil {
			return Refusedf(ErrUndetermined, "whether %q is a group here could not be determined", string(s.group))
		}
		if !known {
			return Refusedf(ErrUnknownSearchScope, "group %q", string(s.group))
		}
	}
	return nil
}

// inScope decides whether a note belongs to the search's subject, three-valued.
//
// THE GROUP CASE IS A JUDGEMENT THE ISSUE DID NOT SETTLE, and it is recorded in the pull request
// body. Criterion 2 says a group-scoped search returns "a note published to group A" and not one
// published to group B; criterion 3 says a company-wide search returns notes "across all authors
// and groups". Neither says what a group-scoped search does with a COMPANY-WIDE note written by a
// member of that group. Taken here as: a note is in group G's scope if its audience is G, OR its
// author is a current member of G. The narrower reading (audience only) would make "search the
// platform group" unable to find the platform group's own company-wide write-ups, which is the
// fourth diagnosis §1 exists to prevent.
func (c Corpus) inScope(n *Note, s SearchScope) tri.Value {
	switch s.kind {
	case SearchPerson:
		if n.Author == s.person {
			return tri.Yes
		}
		return tri.No
	case SearchGroup:
		if n.Visibility.Kind() == KindGroup && n.Visibility.Group() == s.group {
			return tri.Yes
		}
		// tri.FromError, never a bare bool: a membership record that cannot answer has not said no.
		return tri.FromError(c.dir.IsMember(s.group, n.Author))
	default:
		return tri.Yes
	}
}

// suggest offers "did you mean" terms for query terms that matched nothing.
//
// CRITERION 8 IS A PROPERTY OF ITS INPUT: the candidate vocabulary is df, which was built from
// c.notes. A term appearing only in a note the searcher cannot read is not in df, so it cannot be
// suggested, corrected to, or completed. There is no second vocabulary anywhere in this package.
func (c Corpus) suggest(terms []string, df map[string]int) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range terms {
		if df[t] > 0 {
			continue
		}
		for cand := range df {
			if seen[cand] || !editDistanceAtMostOne(t, cand) {
				continue
			}
			seen[cand] = true
			out = append(out, cand)
		}
	}
	sort.Strings(out)
	if len(out) > 3 {
		out = out[:3]
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// Render is the output a person reads, and the bytes criteria 6-10 compare.
//
// EVERY LINE IS UNCONDITIONAL. A line that appears only sometimes is a line whose presence is
// itself a signal, and "the empty-result message differs when a hidden match exists" is precisely
// the leak criterion 6 names. So the counts, the facets, the suggestions and the coverage are
// always printed, with an explicit rendering for zero.
func (o Outcome) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "query: %s\n", o.Terms)
	fmt.Fprintf(&b, "scope: %s\n", o.Scope.Token())
	fmt.Fprintf(&b, "results: %d\n", o.Total)
	if o.Total == 0 {
		// SAID, NOT LEFT BLANK, and said in words that do not vary with anything hidden.
		b.WriteString("found nothing: this search ran and matched no notes you can read.\n")
	}
	for i, r := range o.Results {
		fmt.Fprintf(&b, "  %d. %s  %q  by %s  v%d  score %.4f\n", i+1, string(r.ID), r.Title, string(r.Author), r.Version, r.Score)
		fmt.Fprintf(&b, "     %s\n", r.Snippet)
	}
	fmt.Fprintf(&b, "authors: %s\n", joinPeople(o.Authors))
	fmt.Fprintf(&b, "suggestions: %s\n", joinStrings(o.Suggestions))
	fmt.Fprintf(&b, "notes whose readability could not be determined: %d\n", o.Undetermined)
	fmt.Fprintf(&b, "coverage: %s\n", o.Coverage.Render("complete", "incomplete"))
	if o.Coverage != tri.Yes {
		// CRITERION 17. An incomplete result is never presented as a complete answer, and the
		// sentence says which way the uncertainty runs.
		b.WriteString("this result is INCOMPLETE: part of the corpus could not be evaluated, so there may be matches this search did not reach.\n")
	}
	if o.DepartedScope == tri.No {
		fmt.Fprintf(&b, "note: %s has been deactivated; their notes are archived, not deleted, and remain findable exactly as their visibility allows.\n",
			string(o.Scope.person))
	}
	return b.String()
}

// describeScopes renders the scopes a grant holds, for a refusal that says what you have as well
// as what you lack. "no scopes at all" is spelled out rather than rendered as an empty list.
func describeScopes(ss []Scope) string {
	if len(ss) == 0 {
		return "no scopes at all"
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	sort.Strings(out)
	return "the " + strings.Join(out, ", ") + " scope(s)"
}

func joinPeople(ps []PersonID) string {
	if len(ps) == 0 {
		return "(none)"
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return strings.Join(out, ", ")
}

func joinStrings(ss []string) string {
	if len(ss) == 0 {
		return "(none)"
	}
	return strings.Join(ss, ", ")
}

// SearchThrough is search issued through a grant — the edge PRD §4.5 talks about.
//
// CRITERION 12: REFUSED WHEN REQUESTED, NOT NARROWED AT THE EDGE. Two refusals live here and
// neither of them quietly does a smaller thing instead:
//
//   - a grant with no `read` scope cannot search at all. It does not get an empty result set, which
//     would be indistinguishable from a search that ran and found nothing.
//   - a query asking to search AS somebody else is refused outright. The tempting, helpful, wrong
//     behaviour is to narrow it to the grant's own holder and run that instead; the caller would
//     then read somebody else's name at the top of their own results.
//
// CRITERION 12a falls out of the shape: the corpus is settled for the grant's HOLDER, so holding
// `read` is not itself permission to see anything. `read` is permission to ask.
func SearchThrough(s *Store, g Grant, reader PersonID, q Query) (Outcome, error) {
	return SearchThroughWith(s, g, reader, q, nil, nil)
}

// SearchThroughWith is [SearchThrough] with an explicit directory and roster.
func SearchThroughWith(s *Store, g Grant, reader PersonID, q Query, dir Directory, roster *Roster) (Outcome, error) {
	if g.Holder == "" {
		return Outcome{}, Refusedf(ErrNotSignedIn, "the grant names no holder")
	}
	if !Permits(g.Scopes, ScopeRead) {
		// THE REFUSAL NAMES WHAT THE GRANT DOES CARRY. Found by this Issue's own pairwise test: a
		// token holding only `write` and a token holding only `publish` produced byte-identical
		// output, so a person was told what they could not do and never what they had. Both are
		// the same refusal, but they are not the same situation, and the fix for each is different.
		return Outcome{}, Refusedf(ErrReadScopeRequired,
			"grant %q carries %s and no %q scope, and searching is reading",
			string(g.ID), describeScopes(g.Scopes), string(ScopeRead))
	}
	if reader != "" && reader != g.Holder {
		return Outcome{}, Refusedf(ErrGrantWiderThanHolder,
			"grant %q acts as %q and cannot search as %q; this is refused, not narrowed to %q",
			string(g.ID), string(g.Holder), string(reader), string(g.Holder))
	}
	return SettleWith(s, g.Holder, dir, roster).Search(q)
}

// --- text ---------------------------------------------------------------------------------

// noteText is what a note contributes to the index: its title and its LATEST version's body.
//
// CRITERION 5. Only the latest version is indexed, so a term removed by an amendment stops being a
// current result. The older bodies remain addressable through the timeline (PRD §3.3) and are not
// searched — searching history is a different feature and no Issue has asked for it.
func noteText(n *Note) string { return n.Title + " " + n.Latest().Body }

func tokenise(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func termCounts(s string) map[string]int {
	out := map[string]int{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		out[f]++
	}
	return out
}

func termSet(s string) map[string]bool {
	out := map[string]bool{}
	for t := range termCounts(s) {
		out[t] = true
	}
	return out
}

// snippet is the fragment shown under a result. It is cut from a body that is already in the
// corpus, which is the whole of criterion 7.
func snippet(body string, terms []string) string {
	const width = 100
	lower := strings.ToLower(body)
	at := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if at < 0 {
		at = 0
	}
	start := at - width/2
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(body) {
		end = len(body)
	}
	out := strings.TrimSpace(body[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(body) {
		out += "…"
	}
	return strings.Join(strings.Fields(out), " ")
}

// editDistanceAtMostOne reports whether a and b differ by at most one insertion, deletion or
// substitution. Enough for "did you mean"; deliberately not a full Levenshtein, because a wider
// radius over a corpus vocabulary starts suggesting words nobody typed a near-miss of.
func editDistanceAtMostOne(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := len(a), len(b)
	if la > lb {
		a, b = b, a
		la, lb = lb, la
	}
	if lb-la > 1 {
		return false
	}
	if la == lb {
		diff := 0
		for i := range a {
			if a[i] != b[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		return diff == 1
	}
	// lb == la+1: b with one character removed must equal a.
	for i := 0; i < lb; i++ {
		if b[:i]+b[i+1:] == a {
			return true
		}
	}
	return false
}
