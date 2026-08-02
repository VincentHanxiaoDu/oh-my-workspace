package hub

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// --- fixtures -------------------------------------------------------------------------------

// newTestHub builds a store with a deterministic clock, so that publication order — which is the
// tie-break in ranking — is a fact about the test and not about how fast the machine is.
// deterministicIDs makes id minting reproducible FOR THE DURATION OF ONE TEST.
//
// Note ids are unguessable and random (see noteid.go), which is right and which would otherwise
// make the leak tests impossible: a control corpus and a test corpus are two separate stores, so
// the same note would carry a different random id in each and the byte-for-byte comparison would
// differ for a reason that is not a leak. Seeding both stores identically means the same
// publications mint the same ids, and any remaining difference between the two renders is a real
// one.
//
// UNGUESSABILITY ITSELF IS NEVER ASSERTED AGAINST THIS. noteid_test.go uses the real crypto/rand
// generator, precisely so that a test double cannot make the property appear to hold.
func deterministicIDs(t *testing.T) {
	t.Helper()
	orig := randRead
	var seq byte
	randRead = func(b []byte) (int, error) {
		seq++
		for i := range b {
			b[i] = seq
		}
		return len(b), nil
	}
	t.Cleanup(func() { randRead = orig })
}

func newTestHub(t *testing.T) (*Store, *Record) {
	t.Helper()
	deterministicIDs(t)
	r := NewRecord()
	s := NewStore(r)
	n := time.Unix(0, 0).UTC()
	s.SetClock(func() time.Time {
		n = n.Add(time.Second)
		return n
	})
	return s, r
}

// mustPublish is NOT defined here. Issue #11's versions_test.go defines it for this package and
// that copy is the merged, reviewed one, so it wins; this file had an identical helper that
// differed only in its failure wording. Both branches were cut from a main that did not yet contain
// the other, so the collision appeared only on merge — a clean merge is not a working merge.

func mustScope(t *testing.T, s string) SearchScope {
	t.Helper()
	sc, err := ParseSearchScope(s)
	if err != nil {
		t.Fatalf("parse scope %q: %v", s, err)
	}
	return sc
}

func readGrant(p PersonID) Grant {
	return Grant{ID: GrantID(string(p) + "-g"), Holder: p, Scopes: []Scope{ScopeRead}}
}

func ids(o Outcome) []string {
	out := make([]string, 0, len(o.Results))
	for _, r := range o.Results {
		out = append(out, string(r.ID))
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- criterion 1, 2, 3: scoping -------------------------------------------------------------

func TestPersonScopeReturnsOnlyThatAuthor(t *testing.T) {
	// Criterion 1: two people each publish a company-wide note containing the same distinctive
	// term; a person-scoped search naming one author returns exactly one of them.
	s, r := newTestHub(t)
	r.AddPerson("ada")
	r.AddPerson("bo")
	r.AddPerson("cai")
	adas := mustPublish(t, s, Publication{Author: "ada", Title: "ada's write-up", Body: "the sessiondrop cause"})
	mustPublish(t, s, Publication{Author: "bo", Title: "bo's write-up", Body: "another sessiondrop cause"})

	out, err := SearchThrough(s, readGrant("cai"), "", Query{Terms: "sessiondrop", Scope: mustScope(t, "person:ada")})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := ids(out); !eq(got, []string{string(adas.ID)}) {
		t.Fatalf("person-scoped search returned %v, want exactly ada's note; a person scope that\n"+
			"returns another author's note is not a person scope", got)
	}
	if out.Total != 1 {
		t.Fatalf("total = %d, want 1", out.Total)
	}
}

func TestGroupScopeReturnsOnlyThatGroup(t *testing.T) {
	// Criterion 2: a note published to group A and one to group B, same term; scoped to A returns
	// the first and not the second.
	s, r := newTestHub(t)
	r.DefineGroup("platform", "ada", "searcher")
	r.DefineGroup("billing", "bo", "searcher")
	toA, _ := ToGroup("platform")
	toB, _ := ToGroup("billing")
	inA := mustPublish(t, s, Publication{Author: "ada", Title: "A", Body: "the sessiondrop cause", Visibility: toA})
	mustPublish(t, s, Publication{Author: "bo", Title: "B", Body: "the sessiondrop cause", Visibility: toB})

	out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "sessiondrop", Scope: mustScope(t, "group:platform")})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := ids(out); !eq(got, []string{string(inA.ID)}) {
		t.Fatalf("group-scoped search returned %v, want exactly the platform note %s", got, inA.ID)
	}
}

func TestCompanyScopeSpansAuthorsAndGroups(t *testing.T) {
	// Criterion 3: notes from two authors in two groups, both returned by one company-wide search.
	s, r := newTestHub(t)
	r.DefineGroup("platform", "ada", "searcher")
	r.DefineGroup("billing", "bo", "searcher")
	toA, _ := ToGroup("platform")
	toB, _ := ToGroup("billing")
	mustPublish(t, s, Publication{Author: "ada", Title: "A", Body: "the sessiondrop cause", Visibility: toA})
	mustPublish(t, s, Publication{Author: "bo", Title: "B", Body: "the sessiondrop cause", Visibility: toB})

	out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "sessiondrop", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) != 2 {
		t.Fatalf("company-wide search returned %v, want both authors' notes", ids(out))
	}
}

// --- criterion 4: an unknown scope is not an empty result -------------------------------------

func TestUnknownScopeIsNotAnEmptyResult(t *testing.T) {
	s, r := newTestHub(t)
	r.AddPerson("ada")
	r.DefineGroup("platform", "ada")
	mustPublish(t, s, Publication{Author: "ada", Title: "A", Body: "the sessiondrop cause"})

	// A valid scope that matched nothing: this must SUCCEED with zero results.
	empty, err := SearchThrough(s, readGrant("ada"), "", Query{Terms: "nosuchterm", Scope: mustScope(t, "person:ada")})
	if err != nil {
		t.Fatalf("a valid scope matching nothing must not be an error: %v", err)
	}
	if empty.Total != 0 {
		t.Fatalf("total = %d, want 0", empty.Total)
	}

	for _, bad := range []string{"person:nobody", "group:nosuchgroup"} {
		_, err := SearchThrough(s, readGrant("ada"), "", Query{Terms: "sessiondrop", Scope: mustScope(t, bad)})
		if Code(err) != ErrUnknownSearchScope.Code {
			t.Fatalf("scope %q gave code %q, want %q — an unknown scope and a scope that matched\n"+
				"nothing must not produce identical output", bad, Code(err), ErrUnknownSearchScope.Code)
		}
	}
}

// --- criterion 5: the latest version -----------------------------------------------------------

func TestSearchReflectsTheLatestVersion(t *testing.T) {
	s, r := newTestHub(t)
	r.AddPerson("ada")
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "the termxray cause"})
	if _, err := s.Amend(n.ID, "the termyankee cause"); err != nil {
		t.Fatalf("amend: %v", err)
	}

	found, err := SearchThrough(s, readGrant("ada"), "", Query{Terms: "termyankee", Scope: CompanyScope()})
	if err != nil || len(found.Results) != 1 {
		t.Fatalf("searching for the NEW term returned %v (err %v), want the note", ids(found), err)
	}
	if found.Results[0].Version != 2 {
		t.Fatalf("result reports version %d, want 2 — a result must name the version it matched", found.Results[0].Version)
	}
	stale, err := SearchThrough(s, readGrant("ada"), "", Query{Terms: "termxray", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(stale.Results) != 0 {
		t.Fatalf("searching for the REPLACED term returned %v, want nothing — a superseded term is\n"+
			"not a current result (criterion 5)", ids(stale))
	}
}

// --- criteria 6-11, 13: the negative criteria --------------------------------------------------

// leakCase is a control corpus and a test corpus identical to it plus one note the searcher may
// not read. Every observable must be byte-identical between the two.
type leakCase struct {
	name   string
	hidden func(t *testing.T, s *Store, r *Record) *Note // publishes the unreadable note into the test corpus
}

// buildControl publishes the notes the searcher can fully read. It is deliberately the SAME
// function for both runs, so the only difference between them is the hidden note.
//
// THE BODIES ARE CHOSEN TO MAKE ORDERING SENSITIVE TO THE CORPUS. Query "alpha beta": note-1 has
// alpha twice, note-2 has beta once. With a two-note corpus both terms have document frequency 1
// and note-1 outscores note-2. The hidden note below contains alpha, so if it were present while
// scores were computed alpha would be the commoner term and NOTE-2 WOULD SORT FIRST. That flip is
// what distinguishes filtering-before-ranking from filtering-after, and asserting the order is
// therefore an assertion about the ORDER OF WORK, not merely about absence.
func buildControl(t *testing.T, s *Store, r *Record) (first, second *Note) {
	t.Helper()
	r.AddPerson("searcher")
	r.AddPerson("ada")
	r.AddPerson("bo")
	r.AddPerson("dana")
	r.DefineGroup("platform", "ada", "bo", "dana")
	first = mustPublish(t, s, Publication{Author: "ada", Title: "first", Body: "alpha alpha gamma"})
	second = mustPublish(t, s, Publication{Author: "bo", Title: "second", Body: "beta gamma"})
	return first, second
}

var leakCases = []leakCase{
	{
		// Criterion 11: restricted to its author alone.
		name: "self-only",
		hidden: func(t *testing.T, s *Store, r *Record) *Note {
			return mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "alpha zzarquin", Visibility: SelfOnly()})
		},
	},
	{
		// Criterion 11: restricted to a named group the searcher is not in.
		name: "group-the-searcher-is-not-in",
		hidden: func(t *testing.T, s *Store, r *Record) *Note {
			v, err := ToGroup("platform")
			if err != nil {
				t.Fatalf("to group: %v", err)
			}
			return mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "alpha zzarquin", Visibility: v})
		},
	},
	{
		// Criterion 11: restricted to named people not including the searcher.
		name: "named-people-excluding-the-searcher",
		hidden: func(t *testing.T, s *Store, r *Record) *Note {
			v, err := ToPeople("ada", "bo")
			if err != nil {
				t.Fatalf("to people: %v", err)
			}
			return mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "alpha zzarquin", Visibility: v})
		},
	},
	{
		// Criterion 13: the hidden note's author has been deactivated. Archived, not deleted; the
		// note is still in the store and still unreadable, and none of criteria 6-10 change.
		name: "deactivated-author",
		hidden: func(t *testing.T, s *Store, r *Record) *Note {
			n := mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "alpha zzarquin", Visibility: SelfOnly()})
			if err := r.Leave("platform", "dana"); err != nil {
				t.Fatalf("leave: %v", err)
			}
			return n
		},
	},
}

// runPair runs the same query against the control corpus and the test corpus and returns both
// rendered outcomes.
func runPair(t *testing.T, lc leakCase, q Query) (control, test Outcome, want []string) {
	t.Helper()
	cs, cr := newTestHub(t)
	c1, c2 := buildControl(t, cs, cr)
	c, err := SearchThrough(cs, readGrant("searcher"), "", q)
	if err != nil {
		t.Fatalf("control search: %v", err)
	}

	ts, trr := newTestHub(t)
	t1, t2 := buildControl(t, ts, trr)
	hidden := lc.hidden(t, ts, trr)
	// THE TWO STORES MUST MINT THE SAME IDS FOR THE SAME NOTES, or the byte comparison below would
	// differ for a reason that is not a leak. deterministicIDs makes that so; this asserts it,
	// because a silent divergence here would turn every leak test into a guaranteed failure or,
	// worse after somebody "fixes" it by loosening the comparison, into no test at all.
	if c1.ID != t1.ID || c2.ID != t2.ID {
		t.Fatalf("the control and test corpora minted different ids for the same notes (%s/%s vs %s/%s)",
			c1.ID, c2.ID, t1.ID, t2.ID)
	}
	// The hidden note must genuinely be in the store and genuinely unreadable, or this whole test
	// is asserting nothing. Both halves are checked.
	if ts.Count() != cs.Count()+1 {
		t.Fatalf("%s: the test corpus does not hold one extra note (%d vs %d) — the fixture is broken\n"+
			"and the comparison below would pass vacuously", lc.name, ts.Count(), cs.Count())
	}
	if hidden == nil || hidden.ID == "" {
		t.Fatalf("%s: the leak case published no hidden note", lc.name)
	}
	if hidden.ID == c1.ID || hidden.ID == c2.ID {
		t.Fatalf("%s: the hidden note reused a control note's id (%s)", lc.name, hidden.ID)
	}
	if _, rerr := ts.Read(hidden.ID, "searcher"); Code(rerr) != ErrRefused.Code {
		t.Fatalf("%s: the 'hidden' note is readable by the searcher (err %v) — the fixture proves nothing", lc.name, rerr)
	}
	tt, err := SearchThrough(ts, readGrant("searcher"), "", q)
	if err != nil {
		t.Fatalf("test search: %v", err)
	}
	return c, tt, []string{string(c1.ID), string(c2.ID)}
}

func TestNothingAboutAHiddenNoteIsObservable(t *testing.T) {
	// Criteria 6, 7, 9, 10, 11, 13 in one comparison: the ENTIRE rendered output, byte for byte.
	// A per-field assertion would let a field added later leak unwatched.
	for _, lc := range leakCases {
		t.Run(lc.name, func(t *testing.T) {
			c, tt, _ := runPair(t, lc, Query{Terms: "alpha beta", Scope: CompanyScope()})
			if c.Render() != tt.Render() {
				t.Fatalf("the output differs when an unreadable note exists — this leaks its existence.\n"+
					"control:\n%s\ntest:\n%s", c.Render(), tt.Render())
			}
			if strings.Contains(tt.Render(), "zzarquin") || strings.Contains(tt.Render(), "hidden") {
				t.Fatalf("criterion 7: output carries text originating in the unreadable note:\n%s", tt.Render())
			}
		})
	}
}

func TestOrderingDoesNotLeak(t *testing.T) {
	// CRITERION 9, AND THE ONE THAT DISTINGUISHES FILTER-BEFORE FROM FILTER-AFTER.
	//
	// The expected order is asserted against a LITERAL, not only against the control run, because
	// comparing the two runs alone would still pass if BOTH were ranked over the wider corpus.
	q := Query{Terms: "alpha beta", Scope: CompanyScope()}
	for _, lc := range leakCases {
		t.Run(lc.name, func(t *testing.T) {
			c, tt, want := runPair(t, lc, q)
			if !eq(ids(c), want) {
				t.Fatalf("control order is %v, want %v — the fixture no longer makes ordering\n"+
					"corpus-sensitive, so this test cannot detect the leak it exists for", ids(c), want)
			}
			if !eq(ids(tt), want) {
				t.Fatalf("order with an unreadable note present is %v, want %v.\n"+
					"The unreadable note changed the relative order of two readable notes, which means\n"+
					"it was in the corpus when scores were computed: ranking happened BEFORE the\n"+
					"visibility filter (PRD §3.5).", ids(tt), want)
			}
			for i := range c.Results {
				if c.Results[i].Score != tt.Results[i].Score {
					t.Fatalf("relevance score for %s differs (%v vs %v) — a score computed over a\n"+
						"corpus containing the hidden note discloses that note",
						c.Results[i].ID, c.Results[i].Score, tt.Results[i].Score)
				}
			}
		})
	}
}

func TestOrderingIsGenuinelyCorpusSensitive(t *testing.T) {
	// THE GUARD ON THE GUARD. TestOrderingDoesNotLeak is only meaningful if adding a note to the
	// READABLE corpus really would reorder these two. Here the same third note is published
	// COMPANY-WIDE, so the searcher can read it — and the order must flip. If it does not, ranking
	// is not corpus-relative and the leak test above is asserting nothing.
	s, r := newTestHub(t)
	_, second := buildControl(t, s, r)
	mustPublish(t, s, Publication{Author: "dana", Title: "visible", Body: "alpha zzarquin"})

	out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "alpha beta", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) < 2 || out.Results[0].ID != second.ID {
		t.Fatalf("with a READABLE third note containing alpha, order is %v; expected the beta note first.\n"+
			"Ranking is not sensitive to corpus composition, so TestOrderingDoesNotLeak cannot fail\n"+
			"for the reason it claims to.", ids(out))
	}
}

func TestSuggestionsDoNotLeak(t *testing.T) {
	// CRITERION 8. "zzarquim" is one edit from "zzarquin", which appears ONLY in the hidden note.
	q := Query{Terms: "zzarquim", Scope: CompanyScope()}
	for _, lc := range leakCases {
		t.Run(lc.name, func(t *testing.T) {
			c, tt, _ := runPair(t, lc, q)
			if len(tt.Suggestions) != 0 {
				t.Fatalf("suggested %v — a term that appears only in an unreadable note was offered\n"+
					"as a correction, which discloses that note", tt.Suggestions)
			}
			if c.Render() != tt.Render() {
				t.Fatalf("the suggestion output differs:\ncontrol:\n%s\ntest:\n%s", c.Render(), tt.Render())
			}
		})
	}
}

func TestSuggestionsWorkAtAllWhenTheTermIsReadable(t *testing.T) {
	// THE GUARD ON THE GUARD, again: a suggestion mechanism that never suggests anything would pass
	// TestSuggestionsDoNotLeak trivially.
	s, r := newTestHub(t)
	buildControl(t, s, r)
	mustPublish(t, s, Publication{Author: "dana", Title: "visible", Body: "zzarquin"})

	out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "zzarquim", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Suggestions) == 0 || out.Suggestions[0] != "zzarquin" {
		t.Fatalf("suggestions = %v, want zzarquin — with the term in a READABLE note the correction\n"+
			"must be offered, or the no-leak test proves only that the feature is absent", out.Suggestions)
	}
}

func TestEmptyResultMessageDoesNotDependOnAHiddenMatch(t *testing.T) {
	// CRITERION 6's sharpest form and criterion 14's: the query matches ONLY the hidden note, so
	// both runs must render the identical "found nothing" block. An implementation that says
	// "no results you can see" here and "no results" there has leaked.
	q := Query{Terms: "zzarquin", Scope: CompanyScope()}
	for _, lc := range leakCases {
		t.Run(lc.name, func(t *testing.T) {
			c, tt, _ := runPair(t, lc, q)
			if c.Total != 0 || tt.Total != 0 {
				t.Fatalf("expected zero results on both runs, got %d and %d", c.Total, tt.Total)
			}
			if c.Render() != tt.Render() {
				t.Fatalf("the empty-result output differs when a hidden match exists:\ncontrol:\n%s\ntest:\n%s",
					c.Render(), tt.Render())
			}
			if !strings.Contains(c.Render(), "found nothing") {
				t.Fatalf("an empty result must SAY it found nothing:\n%s", c.Render())
			}
		})
	}
}

func TestDeactivatedAuthorsReadableNotesRemainFindable(t *testing.T) {
	// Criterion 13's positive half: archived, not deleted. A departed colleague's company-wide note
	// is still found, and the scope reports the departure so a thin result set is not read as a
	// broken search.
	s, r := newTestHub(t)
	r.AddPerson("dana")
	r.AddPerson("searcher")
	mustPublish(t, s, Publication{Author: "dana", Title: "t", Body: "the sessiondrop cause"})
	roster := NewRoster()
	roster.Register("searcher")
	roster.Deactivate("dana")

	out, err := SearchThroughWith(s, readGrant("searcher"), "",
		Query{Terms: "sessiondrop", Scope: mustScope(t, "person:dana")}, nil, roster)
	if err != nil {
		t.Fatalf("searching a departed colleague's notes must work: %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("a deactivated author's readable note was not found (%v) — §5.4 says archived, not deleted", ids(out))
	}
	if out.DepartedScope != tri.No {
		t.Fatalf("DepartedScope = %v, want tri.No for a deactivated person", out.DepartedScope)
	}
	if !strings.Contains(out.Render(), "deactivated") {
		t.Fatalf("the output does not say the colleague has left:\n%s", out.Render())
	}
}

func TestRosterIsThreeValued(t *testing.T) {
	roster := NewRoster()
	roster.Register("ada")
	roster.Deactivate("dana")
	if got := roster.Active("ada"); got != tri.Yes {
		t.Fatalf("Active(ada) = %v, want Yes", got)
	}
	if got := roster.Active("dana"); got != tri.No {
		t.Fatalf("Active(dana) = %v, want No", got)
	}
	if got := roster.Active("stranger"); got != tri.Undetermined {
		t.Fatalf("Active(stranger) = %v, want Undetermined — a person the roster never heard of has\n"+
			"not been determined to be present or departed", got)
	}
	var nilRoster *Roster
	if got := nilRoster.Active("ada"); got != tri.Undetermined {
		t.Fatalf("a nil roster answered %v, want Undetermined", got)
	}
}

// --- the Corpus accessors, and the claims their doc comments make ------------------------------

// mixedCorpus builds a corpus containing ALL THREE KINDS of note for one searcher: one they may
// read, one they determinedly may not, and one whose readability genuinely cannot be worked out.
//
// Anything asserting "excludes the unreadable AND the undetermined" needs all three present, or it
// cannot tell which of the two exclusions it is actually checking.
func mixedCorpus(t *testing.T) (c Corpus, store *Store, unevaluableID NoteID) {
	t.Helper()
	s, r := newTestHub(t)
	r.AddPerson("searcher")
	r.AddPerson("dana")
	r.DefineGroup("platform", "ada")
	toGroup, err := ToGroup("platform")
	if err != nil {
		t.Fatalf("to group: %v", err)
	}
	readable := mustPublish(t, s, Publication{Author: "ada", Title: "readable", Body: "alpha"})
	unreadable := mustPublish(t, s, Publication{Author: "dana", Title: "unreadable", Body: "alpha", Visibility: SelfOnly()})
	unevaluable := mustPublish(t, s, Publication{Author: "ada", Title: "unevaluable", Body: "alpha", Visibility: toGroup})
	r.Dissolve("platform")

	// The fixture is checked against CanReadNote directly, so that a change which quietly turns one
	// of the three into another kind fails HERE rather than making the assertions below vacuous.
	for id, want := range map[NoteID]tri.Value{readable.ID: tri.Yes, unreadable.ID: tri.No, unevaluable.ID: tri.Undetermined} {
		if got := CanReadNote(s.notes[id], "searcher", r); got != want {
			t.Fatalf("fixture: CanReadNote(%s) = %v, want %v — the corpus no longer holds one of each\n"+
				"kind, so the exclusion assertions below would prove nothing", id, got, want)
		}
	}
	return Settle(s, "searcher"), s, unevaluable.ID
}

func TestCorpusSizeCountsOnlyWhatTheSearcherMayRead(t *testing.T) {
	// THE DOC COMMENT ON Size() MAKES A SAFETY CLAIM — "the N in the corpus statistics, and it
	// deliberately excludes both the unreadable and the undetermined" — and until this test existed
	// nothing pinned it. Issue #13 (corpus statistics an agent grounds itself on) consumes exactly
	// this number: a count that included a note the searcher cannot read would tell an agent that
	// something exists which it may not see, which is PRD §3.5's leak arriving through the
	// statistics door rather than the results door.
	c, s, _ := mixedCorpus(t)

	if s.Count() != 3 {
		t.Fatalf("the store holds %d notes, want 3", s.Count())
	}
	if got := c.Size(); got != 1 {
		t.Fatalf("Corpus.Size() = %d, want 1. The store holds three notes: one readable, one the\n"+
			"searcher may NOT read, and one whose readability could not be determined. Size() is the\n"+
			"N in the corpus statistics and must count only the first.", got)
	}
	// Each exclusion named separately, so a failure says WHICH one broke.
	if c.Size()+len(c.undetermined) == s.Count() {
		t.Fatalf("Size() plus the undetermined count equals the whole store, so the UNREADABLE note\n" +
			"is being counted somewhere it should not be")
	}
	if c.Size() != len(c.notes) {
		t.Fatalf("Size() = %d but the corpus holds %d readable notes; Size() counts something else", c.Size(), len(c.notes))
	}
}

func TestCorpusUndeterminedIDsAreReportedAndAreACopy(t *testing.T) {
	// Two claims in one doc comment, both pinned. The IDs are RETURNED (not dropped), and the
	// accessor hands out a copy — a caller that sorts or truncates the result must not be editing
	// the corpus's own record of what it could not evaluate.
	c, _, unevaluableID := mixedCorpus(t)

	got := c.UndeterminedIDs()
	if len(got) != 1 || got[0] != unevaluableID {
		t.Fatalf("UndeterminedIDs() = %v, want [note-3] — a note whose readability could not be\n"+
			"worked out must be reported, never silently dropped", got)
	}
	got[0] = "clobbered"
	if again := c.UndeterminedIDs(); again[0] != unevaluableID {
		t.Fatalf("UndeterminedIDs() = %v after a caller wrote to the previous result; the accessor\n"+
			"handed out the corpus's own slice, so any caller can edit what search could not evaluate", again)
	}
	// And the undetermined id is not also in the readable set — the two lists are disjoint.
	for _, n := range c.notes {
		if n.ID == unevaluableID {
			t.Fatalf("%s is both readable and undetermined; undetermined must never be treated as readable", unevaluableID)
		}
	}
}

func TestCorpusReaderIsWhoItWasSettledFor(t *testing.T) {
	// Small, but it is the value every exclusion above was computed against. A Corpus that
	// misreports its reader is a Corpus whose safety claims are about somebody else.
	c, _, _ := mixedCorpus(t)
	if got := c.Reader(); got != "searcher" {
		t.Fatalf("Corpus.Reader() = %q, want %q", got, "searcher")
	}
}

// --- criterion 12, 12a: not narrowed at the edge -----------------------------------------------

func TestSearchRequiresTheReadScopeAndIsRefusedNotNarrowed(t *testing.T) {
	s, r := newTestHub(t)
	buildControl(t, s, r)

	for _, held := range [][]Scope{{ScopeWrite}, {ScopePublish}, {ScopeWrite, ScopePublish}, nil} {
		g := Grant{ID: "g", Holder: "searcher", Scopes: held}
		out, err := SearchThrough(s, g, "", Query{Terms: "alpha", Scope: CompanyScope()})
		if Code(err) != ErrReadScopeRequired.Code {
			t.Fatalf("scopes %v gave code %q, want %q — a search without the read scope must be\n"+
				"refused, not run", held, Code(err), ErrReadScopeRequired.Code)
		}
		if out.Total != 0 || len(out.Results) != 0 {
			t.Fatalf("a refused search returned %d results; a refusal must not also be a result set", out.Total)
		}
	}
}

func TestSearchingAsSomebodyElseIsRefusedNotNarrowed(t *testing.T) {
	// CRITERION 12. The helpful wrong thing is to run the search as the grant's own holder instead.
	s, r := newTestHub(t)
	buildControl(t, s, r)

	out, err := SearchThrough(s, readGrant("searcher"), "ada", Query{Terms: "alpha", Scope: CompanyScope()})
	if Code(err) != ErrGrantWiderThanHolder.Code {
		t.Fatalf("code %q, want %q", Code(err), ErrGrantWiderThanHolder.Code)
	}
	if len(out.Results) != 0 {
		t.Fatalf("the refused request produced %d results — it was narrowed, not refused", len(out.Results))
	}
}

func TestReadScopeDoesNotWidenVisibility(t *testing.T) {
	// CRITERION 12a. Holding `read` is permission to ask, never permission to see.
	s, r := newTestHub(t)
	first, _ := buildControl(t, s, r)
	hidden := mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "alpha zzarquin", Visibility: SelfOnly()})

	out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "alpha zzarquin", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, id := range ids(out) {
		if id == string(hidden.ID) {
			t.Fatalf("a read-scoped grant returned a note its holder may not read (%v)", ids(out))
		}
	}
	if out.Total != 1 || out.Results[0].ID != first.ID {
		t.Fatalf("results = %v, want only %s", ids(out), first.ID)
	}
}

func TestTheVocabularyIsStillExactlyThree(t *testing.T) {
	// The scope vocabulary is ruled. Search added a SEARCH scope (person/group/company); it must
	// not have added a capability.
	got := Vocabulary()
	want := []Scope{ScopePublish, ScopeRead, ScopeWrite}
	if len(got) != 3 {
		t.Fatalf("the capability vocabulary is %v (%d names); it is ruled at exactly three", got, len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("vocabulary = %v, want %v", got, want)
		}
	}
	for _, invented := range []Scope{"search", "search-admin", "read-all", "corpus"} {
		if KnownScope(invented) {
			t.Fatalf("%q is in the vocabulary; the hub operator's reach is a deployment fact, not a scope", invented)
		}
	}
}

// --- criteria 14, 17, 19: undetermined is a third value ----------------------------------------

// failingDirectory cannot answer anything. It exists to drive criterion 19.
type failingDirectory struct{ boom error }

func (f failingDirectory) Knows(GroupID) (bool, error)              { return false, f.boom }
func (f failingDirectory) KnowsPerson(PersonID) (bool, error)       { return false, f.boom }
func (f failingDirectory) IsMember(GroupID, PersonID) (bool, error) { return false, f.boom }

func TestAnUnresolvableScopeIsUndeterminedNotUnknown(t *testing.T) {
	// CRITERION 19. "I could not find out whether that group exists" is not "there is no such
	// group", and the two carry different codes.
	s, r := newTestHub(t)
	buildControl(t, s, r)
	c := SettleWith(s, "searcher", failingDirectory{boom: errors.New("record unreadable")}, nil)

	for _, sc := range []string{"group:platform", "person:ada"} {
		_, err := c.Search(Query{Terms: "alpha", Scope: mustScope(t, sc)})
		if Code(err) != ErrUndetermined.Code {
			t.Fatalf("scope %q with an unreadable directory gave code %q, want %q — this must not\n"+
				"collapse into %q", sc, Code(err), ErrUndetermined.Code, ErrUnknownSearchScope.Code)
		}
	}
}

func TestAnUndeterminedNoteIsNeitherIncludedNorSilent(t *testing.T) {
	// The Issue does not settle what search does with a note whose READABILITY cannot be worked
	// out. Decided: excluded from results and from corpus statistics, counted separately, and the
	// whole result reported INCOMPLETE (criterion 17) — never folded into the total, never silent.
	// The sequence is a real one: a note is narrowed to a group, and the group is later dissolved.
	// Nobody can now say whether the searcher was in it, so CanRead answers Undetermined.
	s, r := newTestHub(t)
	r.AddPerson("searcher")
	r.DefineGroup("platform", "ada")
	v, err := ToGroup("platform")
	if err != nil {
		t.Fatalf("to group: %v", err)
	}
	mustPublish(t, s, Publication{Author: "ada", Title: "first", Body: "alpha alpha gamma"})
	unevaluable := mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "alpha zzarquin", Visibility: v})
	r.Dissolve("platform")

	if got := CanReadNote(s.notes[unevaluable.ID], "searcher", r); got != tri.Undetermined {
		t.Fatalf("the fixture does not produce an undetermined note: CanReadNote = %v", got)
	}

	// The query deliberately does NOT contain the hidden note's distinctive term, so that the echo
	// of what the person typed cannot be mistaken for a leak of what they could not read.
	out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "alpha", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if out.Undetermined != 1 {
		t.Fatalf("Undetermined = %d, want 1 — a note whose readability could not be worked out must\n"+
			"be counted, not silently dropped.\n%s", out.Undetermined, out.Render())
	}
	if out.Coverage != tri.Undetermined {
		t.Fatalf("Coverage = %v, want Undetermined — an unevaluable note makes the answer incomplete", out.Coverage)
	}
	for _, id := range ids(out) {
		if id == string(unevaluable.ID) {
			t.Fatalf("the unevaluable note was RETURNED (%v) — undetermined must never be treated as readable", ids(out))
		}
	}
	for _, leaked := range []string{"zzarquin", "hidden"} {
		if strings.Contains(out.Render(), leaked) {
			t.Fatalf("the unevaluable note's text (%q) reached the output:\n%s", leaked, out.Render())
		}
	}
	r2 := out.Render()
	if !strings.Contains(r2, "could not be determined: 1") || !strings.Contains(r2, "INCOMPLETE") {
		t.Fatalf("the output does not distinguish the undetermined state:\n%s", r2)
	}
}

func TestACompleteEmptySearchIsDistinguishableFromAnIncompleteOne(t *testing.T) {
	// Criterion 17: a partial result is distinguishable from a complete result set of the same size.
	s, r := newTestHub(t)
	buildControl(t, s, r)
	complete, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "nosuchterm", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	v, _ := ToGroup("platform")
	partialStore, pr := newTestHub(t)
	pr.DefineGroup("platform", "ada")
	mustPublish(t, partialStore, Publication{Author: "dana", Title: "hidden", Body: "alpha", Visibility: v})
	pr.Dissolve("platform")
	partial, err := SearchThrough(partialStore, readGrant("searcher"), "", Query{Terms: "nosuchterm", Scope: CompanyScope()})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if complete.Total != partial.Total {
		t.Fatalf("the two runs must have the same number of results to make this comparison mean anything")
	}
	if complete.Render() == partial.Render() {
		t.Fatalf("a complete empty result and an incomplete one render identically:\n%s", complete.Render())
	}
	if complete.Coverage != tri.Yes || partial.Coverage != tri.Undetermined {
		t.Fatalf("coverage: complete=%v partial=%v", complete.Coverage, partial.Coverage)
	}
}

// --- criterion 23: the three-cause invariant ---------------------------------------------------

func TestEveryPermittedNoteIsReachableUnderEveryScopeItBelongsTo(t *testing.T) {
	// CRITERION 23. A permitted note that no search returns is a defect against PRD §1.
	s, r := newTestHub(t)
	r.DefineGroup("platform", "ada", "searcher")
	v, err := ToGroup("platform")
	if err != nil {
		t.Fatalf("to group: %v", err)
	}
	mustPublish(t, s, Publication{Author: "ada", Title: "why sessions drop", Body: "the staging cluster drops sessiondrop", Visibility: v})

	for _, sc := range []string{"company", "person:ada", "group:platform"} {
		out, err := SearchThrough(s, readGrant("searcher"), "", Query{Terms: "sessiondrop", Scope: mustScope(t, sc)})
		if err != nil {
			t.Fatalf("scope %q: %v", sc, err)
		}
		if len(out.Results) != 1 {
			t.Fatalf("scope %q returned %v; a note the searcher may read must be reachable under every\n"+
				"scope it belongs to (PRD §1)", sc, ids(out))
		}
	}
}

// --- structural: no second visibility rule, no network -----------------------------------------

func TestSearchDoesNotReimplementTheVisibilityRule(t *testing.T) {
	// The rule lives in CanRead. Search must reach it, and must contain no comparison of its own
	// against a Visibility's kind for the purpose of deciding readability.
	src := readSource(t, "search.go")
	for _, forbidden := range []string{"KindSelf", "KindPeople", "v.people", "== KindCompany"} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("search.go mentions %q — deciding readability is CanRead's job and there must not\n"+
				"be a second copy of the rule", forbidden)
		}
	}
	if !strings.Contains(src, "ListReadable") {
		t.Fatalf("search.go does not call ListReadable, which is how it reaches CanRead")
	}
}

func TestSearchOpensNoNetwork(t *testing.T) {
	// CRITERION 21, as far as this package can answer it: there is no network client here to open a
	// connection with. Asserted on the source's imports rather than on a runtime counter, because a
	// counter only observes the calls somebody remembered to route through it.
	for _, f := range []string{"search.go", "people.go"} {
		src := readSource(t, f)
		for _, pkg := range []string{`"net"`, `"net/http"`, `"net/url"`, `"crypto/tls"`, `"os/exec"`} {
			if strings.Contains(src, pkg) {
				t.Fatalf("%s imports %s; search must open no network connection and start no process", f, pkg)
			}
		}
	}
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestSearchErrorsAreDistinguishable(t *testing.T) {
	all := append(append([]*Error{}, allErrors...), searchErrors...)
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[i].Code == all[j].Code {
				t.Fatalf("two errors share the code %q", all[i].Code)
			}
			if all[i].Msg == all[j].Msg {
				t.Fatalf("two errors share the message %q", all[i].Msg)
			}
		}
	}
}

func TestParseSearchScopeIsOneParser(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "company"},
		{"company", "company"},
		{"COMPANY", "company"},
		{"person:ada", "person:ada"},
		{"group: platform ", "group:platform"},
	} {
		got, err := ParseSearchScope(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got.Token() != tc.want {
			t.Fatalf("%q parsed to %q, want %q", tc.in, got.Token(), tc.want)
		}
	}
	for _, bad := range []string{"platform", "team:platform", "person:", "group:"} {
		if _, err := ParseSearchScope(bad); Code(err) != ErrUnknownSearchScope.Code {
			t.Fatalf("%q gave code %q, want %q", bad, Code(err), ErrUnknownSearchScope.Code)
		}
	}
}
