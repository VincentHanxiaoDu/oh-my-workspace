package hub

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// company is the fixture every test here starts from: a hub with three colleagues, one group, and
// four notes by Priya under all four visibilities — plus one note by a colleague who stays.
//
// It is built ONCE and used by every criterion, so that "before the deactivation" and "after the
// deactivation" are two observations of the same corpus rather than two corpora that happen to
// resemble each other.
type company struct {
	store *Store
	arch  *Archive
	// priya's notes, by the visibility they carry.
	wide, grouped, named, self NoteID
	// a note by somebody who does not leave, which references priya's company-wide note.
	byRavi NoteID
	refs   *RefIndex
}

func newCompany(t *testing.T) *company {
	t.Helper()
	rec := NewRecord()
	rec.DefineGroup("billing", "priya", "sam")
	rec.AddPerson("ravi") // not in billing
	s := NewStore(rec)
	arch := NewArchive()
	s.SetPeopleStatus(arch)

	c := &company{store: s, arch: arch, refs: NewRefIndex()}
	pub := func(title string, v Visibility) NoteID {
		t.Helper()
		n, err := s.Publish(Publication{Author: "priya", Title: title, Body: "billing reconciliation runs twice because " + title, Visibility: v})
		if err != nil {
			t.Fatalf("Publish %s: %v", title, err)
		}
		return n.ID
	}
	c.wide = pub("company-wide", CompanyWide())
	g, err := ToGroup("billing")
	if err != nil {
		t.Fatal(err)
	}
	c.grouped = pub("grouped", g)
	p, err := ToPeople("sam")
	if err != nil {
		t.Fatal(err)
	}
	c.named = pub("named", p)
	c.self = pub("self", SelfOnly())

	n, err := s.Publish(Publication{Author: "ravi", Title: "ravi's note", Body: "see priya's writeup", Visibility: CompanyWide()})
	if err != nil {
		t.Fatal(err)
	}
	c.byRavi = n.ID
	c.refs.Link(c.byRavi, c.wide)
	return c
}

// amend adds a version, so that the timeline criteria have a timeline to be about.
func (c *company) amend(t *testing.T, id NoteID, body string) {
	t.Helper()
	if _, err := c.store.Amend(id, body); err != nil {
		t.Fatalf("Amend %s: %v", id, err)
	}
}

// -------------------------------------------------------------------------------------------
// Archived, not deleted — criteria 1, 2, 3
// -------------------------------------------------------------------------------------------

// TestADirectReferenceResolvesToTheSameNoteAfterTheAuthorLeaves is criterion 1: fetch before,
// fetch after, compare.
func TestADirectReferenceResolvesToTheSameNoteAfterTheAuthorLeaves(t *testing.T) {
	c := newCompany(t)
	before, err := c.store.Read(c.wide, "ravi")
	if err != nil {
		t.Fatalf("reading before the departure: %v", err)
	}
	beforeBody := before.Latest().Body

	c.arch.Deactivate("priya")

	after, err := c.store.Read(c.wide, "ravi")
	if err != nil {
		t.Fatalf("criterion 1: a note that resolved before the deactivation must resolve after it, "+
			"and this one answered %v (code %s). archived is not deleted.", err, Code(err))
	}
	if after.ID != before.ID {
		t.Errorf("criterion 1: the reference resolved to %q before and %q after", before.ID, after.ID)
	}
	if got := after.Latest().Body; got != beforeBody {
		t.Errorf("criterion 1: the body changed across the departure:\n  before: %q\n  after:  %q", beforeBody, got)
	}
	if after.Author != before.Author {
		t.Errorf("criterion 1: the author changed across the departure: %q -> %q", before.Author, after.Author)
	}
}

// TestEveryVersionIsStillAddressableAfterTheAuthorLeaves is criterion 2.
func TestEveryVersionIsStillAddressableAfterTheAuthorLeaves(t *testing.T) {
	c := newCompany(t)
	c.amend(t, c.wide, "second: it runs twice because the retry is idempotent")
	c.amend(t, c.wide, "third: and the second run is a no-op")

	type snap struct {
		body string
		at   time.Time
	}
	before := map[int]snap{}
	for i := 1; i <= 3; i++ {
		v, _, err := AttributedVersion(c.store, c.arch, VersionRef{Note: c.wide, Number: i}, "ravi")
		if err != nil {
			t.Fatalf("reading version %d before the departure: %v", i, err)
		}
		before[i] = snap{v.Body, v.At}
	}

	c.arch.Deactivate("priya")

	for i := 1; i <= 3; i++ {
		v, by, err := AttributedVersion(c.store, c.arch, VersionRef{Note: c.wide, Number: i}, "ravi")
		if err != nil {
			t.Fatalf("criterion 2: version %d was addressable before the deactivation and answered "+
				"%v (code %s) after it. a claim a colleague acted on last month must still be readable.",
				i, err, Code(err))
		}
		if v.Body != before[i].body {
			t.Errorf("criterion 2: version %d's body changed across the departure:\n  before: %q\n  after:  %q",
				i, before[i].body, v.Body)
		}
		if !v.At.Equal(before[i].at) {
			t.Errorf("criterion 2: version %d's timestamp changed across the departure", i)
		}
		// Criterion 13 rides along: EVERY version, not only the latest, is attributed the same way.
		if by.Author != "priya" || by.Active != tri.No {
			t.Errorf("criterion 13: version %d is attributed %q/%v; every version of a departed "+
				"colleague's note must show the same departed author", i, by.Author, by.Active)
		}
	}
}

// TestAttributionIsIdenticalOnEveryVersionOfADepartedColleaguesNote is criterion 13, compared
// entry-to-entry rather than each against a literal.
func TestAttributionIsIdenticalOnEveryVersionOfADepartedColleaguesNote(t *testing.T) {
	c := newCompany(t)
	c.amend(t, c.wide, "v2")
	c.amend(t, c.wide, "v3")
	c.arch.Deactivate("priya")

	var lines []string
	for i := 1; i <= 3; i++ {
		_, by, err := AttributedVersion(c.store, c.arch, VersionRef{Note: c.wide, Number: i}, "ravi")
		if err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
		lines = append(lines, by.Line())
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] != lines[0] {
			t.Errorf("criterion 13: version 1 renders %q and version %d renders %q — attribution must "+
				"not say 'deactivated' on the latest and something else on the older ones", lines[0], i+1, lines[i])
		}
	}
	if !strings.Contains(lines[0], "priya") {
		t.Errorf("criterion 13/10: the attribution %q does not name the author", lines[0])
	}
}

// TestReferencesSurviveADepartureInBothDirections is criterion 3.
func TestReferencesSurviveADepartureInBothDirections(t *testing.T) {
	c := newCompany(t)

	fwdBefore := c.refs.Resolve(c.store, c.arch, c.byRavi, "sam")
	backBefore := c.refs.Backlinks(c.store, c.arch, c.wide, "sam")
	if len(fwdBefore.Resolved) != 1 || len(backBefore.Resolved) != 1 {
		t.Fatalf("fixture: expected one reference each way before the departure, got %d forward and %d back",
			len(fwdBefore.Resolved), len(backBefore.Resolved))
	}

	c.arch.Deactivate("priya")

	fwd := c.refs.Resolve(c.store, c.arch, c.byRavi, "sam")
	if len(fwd.Resolved) != 1 || fwd.Resolved[0].Note != c.wide {
		t.Errorf("criterion 3: a note by a still-active person referenced an archived note and the "+
			"reference no longer resolves: resolved=%v refused=%v undetermined=%v",
			fwd.Resolved, fwd.Refused, fwd.Undetermined)
	}
	back := c.refs.Backlinks(c.store, c.arch, c.wide, "sam")
	if len(back.Resolved) != 1 || back.Resolved[0].Note != c.byRavi {
		t.Errorf("criterion 3: the archived note no longer lists what referenced it: %v", back.Resolved)
	}
	// And the resolved target is still attributed to the person who left.
	if got := fwd.Resolved[0].By; got.Author != "priya" || got.Active != tri.No {
		t.Errorf("criterion 3/9: the resolved archived note is attributed %q/%v", got.Author, got.Active)
	}
}

// -------------------------------------------------------------------------------------------
// Findable by exactly whoever could see it before — criteria 4, 5, 6, 7, 8
// -------------------------------------------------------------------------------------------

// TestTheSameSearchByTheSameSearcherReturnsTheSameNotesAfterTheAuthorLeaves is criteria 4 and 6
// together: the identical query as the identical searcher, before and after.
//
// It runs the comparison for every reader and every scope in the fixture, so "person, group and
// company-wide" is covered by the data rather than by three near-identical test functions.
func TestTheSameSearchByTheSameSearcherReturnsTheSameNotesAfterTheAuthorLeaves(t *testing.T) {
	c := newCompany(t)
	const term = "billing reconciliation"

	readers := []PersonID{"priya", "sam", "ravi", ""}
	before := map[PersonID][]NoteID{}
	beforeUndet := map[PersonID]int{}
	for _, r := range readers {
		hits, undet := SearchLatest(c.store, r, term)
		before[r] = hitNotes(hits)
		beforeUndet[r] = len(undet)
	}

	c.arch.Deactivate("priya")

	for _, r := range readers {
		hits, undet := SearchLatest(c.store, r, term)
		got := hitNotes(hits)
		if !sameIDs(got, before[r]) {
			t.Errorf("criteria 4 and 6: searcher %q got %v before the deactivation and %v after. "+
				"deactivation must neither widen nor narrow what a search returns.", r, before[r], got)
		}
		if len(undet) != beforeUndet[r] {
			t.Errorf("criterion 4: searcher %q had %d undetermined notes before and %d after",
				r, beforeUndet[r], len(undet))
		}
	}
	// The fixture must actually have exercised all three scopes, or the loop above proved nothing.
	if len(before["sam"]) != 3 {
		t.Fatalf("fixture: sam should see the company-wide, group and named notes (3); saw %v", before["sam"])
	}
	if len(before["ravi"]) != 1 {
		t.Fatalf("fixture: ravi should see only the company-wide note; saw %v", before["ravi"])
	}
	if len(before["priya"]) != 4 {
		t.Fatalf("fixture: priya authored four notes and should see all of them; saw %v", before["priya"])
	}
}

// TestArchivalNeverWidensANarrowedNote is criterion 5.
func TestArchivalNeverWidensANarrowedNote(t *testing.T) {
	c := newCompany(t)
	for _, tc := range []struct {
		name string
		id   NoteID
	}{
		{"narrowed to a group ravi is not in", c.grouped},
		{"narrowed to named people ravi is not among", c.named},
		{"narrowed to the author alone", c.self},
	} {
		if _, err := c.store.Read(tc.id, "ravi"); Code(err) != ErrRefused.Code {
			t.Fatalf("fixture %s: ravi should be refused before the deactivation, got %v", tc.name, err)
		}
	}

	c.arch.Deactivate("priya")

	for _, tc := range []struct {
		name string
		id   NoteID
	}{
		{"narrowed to a group ravi is not in", c.grouped},
		{"narrowed to named people ravi is not among", c.named},
		{"narrowed to the author alone", c.self},
	} {
		_, err := c.store.Read(tc.id, "ravi")
		if Code(err) != ErrRefused.Code {
			t.Errorf("criterion 5: a note %s became readable to ravi once its author left "+
				"(answer: %v). archival never widens reach.", tc.name, err)
		}
		// And it is not merely refused at the store — it is absent from what search returns.
		hits, _ := SearchLatest(c.store, "ravi", "billing")
		for _, h := range hits {
			if h.Ref.Note == tc.id {
				t.Errorf("criterion 5/7: the note %s appears in ravi's search results after the "+
					"deactivation; ranking must never surface a note the searcher cannot read", tc.name)
			}
		}
	}
}

// TestArchivalNeverNarrowsACompanyWideNote is criterion 6's own assertion, kept separate from the
// before/after comparison so that a failure names the right defect.
func TestArchivalNeverNarrowsACompanyWideNote(t *testing.T) {
	c := newCompany(t)
	c.arch.Deactivate("priya")
	for _, r := range []PersonID{"ravi", "sam"} {
		if _, err := c.store.Read(c.wide, r); err != nil {
			t.Errorf("criterion 6: a company-wide note by a deactivated person must still be "+
				"returned to any company-wide searcher; %q got %v (code %s)", r, err, Code(err))
		}
	}
}

// TestUnreadableArchivedNotesDoNotChangeASearchersResultCount is criterion 7: compare the
// searcher's total against a corpus with and without archived notes they cannot read.
func TestUnreadableArchivedNotesDoNotChangeASearchersResultCount(t *testing.T) {
	// Corpus A: only the company-wide note and ravi's.
	a := NewStore(NewRecord())
	archA := NewArchive()
	a.SetPeopleStatus(archA)
	publishAs(t, a, "priya", "wide", CompanyWide())
	publishAs(t, a, "ravi", "ravi", CompanyWide())
	archA.Deactivate("priya")

	// Corpus B: the same, plus three archived notes ravi may not read.
	b := newCompany(t)
	b.arch.Deactivate("priya")

	hitsA, undetA := SearchLatest(a, "ravi", "wide")
	sumA := Summarise(a, archA, "ravi")
	sumB := Summarise(b.store, b.arch, "ravi")

	if len(undetA) != 0 {
		t.Fatalf("fixture: corpus A should have no undetermined notes for ravi, got %v", undetA)
	}
	if len(hitsA) != 1 {
		t.Fatalf("fixture: expected one hit in corpus A, got %d", len(hitsA))
	}
	if sumA.Notes != sumB.Notes {
		t.Errorf("criterion 7: ravi's readable-note count is %d in a corpus without the archived "+
			"notes he cannot read and %d in one with them. the count must not change with "+
			"unreadable archived notes present.", sumA.Notes, sumB.Notes)
	}
	if sumB.Archived != 1 {
		t.Errorf("criterion 7/8: ravi should see exactly one archived note he can read; the summary says %d", sumB.Archived)
	}
}

// TestCorpusStatisticsCountExactlyWhatASearchWillFind is criterion 8: an agent that grounds itself
// on the statistics and then searches must not find the corpus smaller than promised.
func TestCorpusStatisticsCountExactlyWhatASearchWillFind(t *testing.T) {
	c := newCompany(t)
	c.arch.Deactivate("priya")
	for _, r := range []PersonID{"priya", "sam", "ravi"} {
		sum := Summarise(c.store, c.arch, r)
		readable, _ := c.store.ListReadable(r)
		if sum.Notes != len(readable) {
			t.Errorf("criterion 8: the statistics promise %q %d notes and the corpus holds %d for them",
				r, sum.Notes, len(readable))
		}
		if sum.Archived > sum.Notes {
			t.Errorf("criterion 8: archived (%d) is counted as a subset of notes (%d), not in addition to it",
				sum.Archived, sum.Notes)
		}
		archived := 0
		versions := 0
		for _, n := range readable {
			versions += len(n.Versions)
			if n.Author == "priya" {
				archived++
			}
		}
		if sum.Archived != archived {
			t.Errorf("criterion 8: %q is promised %d archived notes and can read %d", r, sum.Archived, archived)
		}
		if sum.Versions != versions {
			t.Errorf("criterion 8: %q is promised %d versions and can read %d", r, sum.Versions, versions)
		}
	}
	// The negative half: an archived note ravi may not read is not counted for him.
	if got := Summarise(c.store, c.arch, "ravi").Archived; got != 1 {
		t.Errorf("criterion 8: ravi may read one of priya's four notes; the statistics count %d archived", got)
	}
}

// -------------------------------------------------------------------------------------------
// Shown as written by someone deactivated — criteria 9, 10, 11, 12, 18
// -------------------------------------------------------------------------------------------

// TestAnArchivedNoteRendersItsAuthorAndItIsTheSameAuthorAsBefore is criteria 9 and 10.
func TestAnArchivedNoteRendersItsAuthorAndItIsTheSameAuthorAsBefore(t *testing.T) {
	c := newCompany(t)
	_, byBefore, err := AttributedRead(c.store, c.arch, c.wide, "ravi")
	if err != nil {
		t.Fatal(err)
	}
	c.arch.Deactivate("priya")
	_, byAfter, err := AttributedRead(c.store, c.arch, c.wide, "ravi")
	if err != nil {
		t.Fatalf("criterion 11: an archived note must not answer an error; got %v", err)
	}
	if byAfter.Author == "" {
		t.Fatalf("criterion 10: an archived note rendered an empty author")
	}
	if byAfter.Author != byBefore.Author {
		t.Errorf("criterion 9: the author was %q before the deactivation and %q after", byBefore.Author, byAfter.Author)
	}
	line := byAfter.Line()
	for _, bad := range []string{"unknown", "deleted user", "anonymous", "nobody"} {
		if strings.Contains(strings.ToLower(line), bad) {
			t.Errorf("criterion 10: the attribution %q uses the placeholder %q, which is "+
				"indistinguishable from 'this note has no author'", line, bad)
		}
	}
	if !strings.Contains(line, string(byBefore.Author)) {
		t.Errorf("criterion 9: the rendering %q does not identify the author the same way they were "+
			"identified while active (%q)", line, byBefore.Author)
	}
	if byAfter.Active != tri.No {
		t.Errorf("criterion 9: the archived note carries no deactivated indication (state %v)", byAfter.Active)
	}
}

// TestAnArchivedNoteIsNeverReportedAsMissing is criterion 11: by reference, by version, and via a
// search result, none of them answers not-found, an error, or an empty body.
func TestAnArchivedNoteIsNeverReportedAsMissing(t *testing.T) {
	c := newCompany(t)
	c.amend(t, c.wide, "amended before she left")
	c.arch.Deactivate("priya")

	if n, _, err := AttributedRead(c.store, c.arch, c.wide, "ravi"); err != nil || n.Latest().Body == "" {
		t.Errorf("criterion 11: by reference — err=%v body=%q", err, bodyOf(n))
	}
	if v, _, err := AttributedVersion(c.store, c.arch, VersionRef{Note: c.wide, Number: 1}, "ravi"); err != nil || v.Body == "" {
		t.Errorf("criterion 11: by version — err=%v body=%q", err, v.Body)
	}
	hits, _ := SearchLatest(c.store, "ravi", "amended")
	if len(hits) != 1 {
		t.Fatalf("criterion 11: via a search result — the archived note did not appear (%d hits)", len(hits))
	}
	v, _, err := AttributedVersion(c.store, c.arch, hits[0].Ref, "ravi")
	if err != nil || v.Body == "" {
		t.Errorf("criterion 11: following a search result — err=%v body=%q", err, v.Body)
	}

	// AND THE TWO FACTS STAY DIFFERENT FACTS. A note refused for visibility and a note whose author
	// is deactivated are reported differently.
	_, _, rerr := AttributedRead(c.store, c.arch, c.self, "ravi")
	if Code(rerr) != ErrRefused.Code {
		t.Errorf("criterion 11: a refusal for visibility must still report as %q; got %q",
			ErrRefused.Code, Code(rerr))
	}
}

// TestTheThreeAuthorStatesRenderPairwiseDistinctly is criterion 12 and criterion 18.
//
// It compares PAIRWISE rather than against string literals, because a literal-by-literal test
// passes just as happily after two of the sentences have been edited into the same one.
func TestTheThreeAuthorStatesRenderPairwiseDistinctly(t *testing.T) {
	lines := AllAttributionLines()
	if len(lines) != 4 {
		t.Fatalf("expected four renderings (three states plus the no-author defect), got %d", len(lines))
	}
	for name, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Errorf("criterion 12: the %q rendering is blank; none of them may be silence", name)
		}
	}
	for a, la := range lines {
		for b, lb := range lines {
			if a >= b {
				continue
			}
			if la == lb {
				t.Errorf("criteria 12 and 18: the %q and %q renderings are identical:\n  %q", a, b, la)
			}
		}
	}
	// The undetermined one must not read as either of the other two. It is checked by containment
	// as well as by inequality: "deactivated (could not confirm)" would be distinct and still wrong.
	undet := lines["undetermined"]
	if strings.Contains(undet, attributionDepartedClause) || strings.Contains(undet, attributionActiveClause) {
		t.Errorf("criterion 18: the undetermined rendering %q contains one of the determined clauses", undet)
	}
}

// TestAnUndeterminableAuthorStateIsNoneOfTheOtherThree is criterion 18, driven through the real
// lookup path rather than a hand-built struct: the hub's record of the person is made unreadable.
func TestAnUndeterminableAuthorStateIsNoneOfTheOtherThree(t *testing.T) {
	c := newCompany(t)
	c.arch.MarkUnreadable("priya")

	n, by, err := AttributedRead(c.store, c.arch, c.wide, "ravi")
	if err != nil {
		t.Fatalf("criterion 18: the note must still be readable while its author's state is unknown; got %v", err)
	}
	if by.Active != tri.Undetermined {
		t.Fatalf("criterion 18: the author state is %v, not undetermined", by.Active)
	}
	if by.Author != "priya" {
		t.Errorf("criterion 18: an indeterminate author state must never render as NO author; got %q", by.Author)
	}
	// Distinct from all three others, compared as renderings.
	got := by.Line()
	others := map[string]string{
		"active":      Attribution{Author: n.Author, Active: tri.Yes}.Line(),
		"deactivated": Attribution{Author: n.Author, Active: tri.No}.Line(),
		"no author":   Attribution{Author: "", Active: tri.Undetermined}.Line(),
	}
	for name, other := range others {
		if got == other {
			t.Errorf("criterion 18: an undetermined author state renders identically to %q:\n  %q", name, got)
		}
	}
	if strings.TrimSpace(got) == "" {
		t.Error("criterion 18: an undetermined author state rendered as silence")
	}
}

// TestAnUndeterminedAuthorStateDoesNotChangeWhoCanRead is the other half of criterion 18: the third
// value is not a quiet refusal either.
func TestAnUndeterminedAuthorStateDoesNotChangeWhoCanRead(t *testing.T) {
	c := newCompany(t)
	before, _ := c.store.ListReadable("ravi")
	c.arch.MarkUnreadable("priya")
	after, _ := c.store.ListReadable("ravi")
	if len(before) != len(after) {
		t.Errorf("an unreadable people record changed what ravi may read: %d -> %d. author state is "+
			"not an input to CanRead.", len(before), len(after))
	}
}

// -------------------------------------------------------------------------------------------
// Deactivation ends sessions without removing publications — criteria 14, 15, 16, 17
// -------------------------------------------------------------------------------------------

// TestDeactivationRefusesEveryTokenForEveryScope is criterion 14.
func TestDeactivationRefusesEveryTokenForEveryScope(t *testing.T) {
	c := newCompany(t)
	l := NewLedger()
	held := Vocabulary()
	g, err := l.RequestLive(c.arch, Holder{Person: "priya", Scopes: held}, held)
	if err != nil {
		t.Fatalf("issuing a grant to an active person: %v", err)
	}
	// It works before.
	if _, err := ReadThroughLive(c.store, c.arch, g, c.self); err != nil {
		t.Fatalf("fixture: priya should be able to read her own note through her own grant: %v", err)
	}

	c.arch.Deactivate("priya")

	// "not read my own notes":
	if _, err := ReadThroughLive(c.store, c.arch, g, c.self); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 14: a read of her OWN note through her own token answered %v (code %q); "+
			"after deactivation no token issued to that person is accepted, for any scope",
			err, Code(err))
	}
	// "not publish as me":
	if _, err := PublishThroughLive(c.store, c.arch, g, Publication{Author: "priya", Title: "new", Body: "b"}); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 14: publishing through her token answered code %q", Code(err))
	}
	if _, err := SetVisibilityThroughLive(c.store, c.arch, g, c.wide, SelfOnly()); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 14: changing a note's visibility through her token answered code %q", Code(err))
	}
	// The scope on the grant is not what decided it — a grant carrying only `read` is refused the
	// same way, which is what "for any scope" means.
	readOnly, err := l.Request(Holder{Person: "priya", Scopes: held}, []Scope{ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadThroughLive(c.store, c.arch, readOnly, c.wide); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 14: a read-only token answered code %q", Code(err))
	}
}

// TestTokenRefusedAndNoteStillFindableInASingleRun is criterion 15, in one run, as the criterion
// asks.
func TestTokenRefusedAndNoteStillFindableInASingleRun(t *testing.T) {
	c := newCompany(t)
	l := NewLedger()
	g, err := l.RequestLive(c.arch, Holder{Person: "priya", Scopes: Vocabulary()}, []Scope{ScopeRead})
	if err != nil {
		t.Fatal(err)
	}

	c.arch.Deactivate("priya")

	if _, err := ReadThroughLive(c.store, c.arch, g, c.wide); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 15, first half: the token was not refused (code %q)", Code(err))
	}
	n, by, rerr := AttributedRead(c.store, c.arch, c.wide, "ravi")
	if rerr != nil {
		t.Errorf("criterion 15, second half: the note is no longer findable (%v). ending sessions is "+
			"not deleting notes, and neither half may be implemented by doing the other.", rerr)
	} else if by.Author != "priya" || n.Latest().Body == "" {
		t.Errorf("criterion 15, second half: the note is findable but attributed %q with body %q",
			by.Author, n.Latest().Body)
	}
	hits, _ := SearchLatest(c.store, "ravi", "billing")
	if len(hits) == 0 {
		t.Error("criterion 15, second half: the note is no longer returned by search")
	}
}

// TestNoNewAuthorityIsCreatedForADeactivatedPerson is criterion 16, including that the refusal
// happens when the grant is REQUESTED and not narrowed at the edge (§4.5).
func TestNoNewAuthorityIsCreatedForADeactivatedPerson(t *testing.T) {
	c := newCompany(t)
	l := NewLedger()
	c.arch.Deactivate("priya")

	before := len(l.Grants("priya"))
	if _, err := l.RequestLive(c.arch, Holder{Person: "priya", Scopes: Vocabulary()}, []Scope{ScopeRead}); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 16: a grant request for a departed person answered code %q", Code(err))
	}
	if after := len(l.Grants("priya")); after != before {
		t.Errorf("criterion 16: the refused request issued a grant anyway (%d -> %d); a grant that "+
			"would allow it is refused when it is requested, not narrowed at the edge", before, after)
	}

	// Publishing and amending are refused AT THE STORE, not only through a wrapper.
	countBefore := c.store.Count()
	if _, err := c.store.Publish(Publication{Author: "priya", Title: "posthumous", Body: "b"}); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 16: publishing directly as a departed person answered code %q", Code(err))
	}
	if c.store.Count() != countBefore {
		t.Errorf("criterion 16: the refused publication stored a note anyway")
	}
	n, err := c.store.Read(c.wide, "priya")
	if err != nil {
		t.Fatalf("reading before counting versions: %v", err)
	}
	versionsBefore := len(n.Versions)
	if _, err := c.store.Amend(c.wide, "a new version after she left"); Code(err) != ErrPersonDeactivated.Code {
		t.Errorf("criterion 16: amending a departed person's note answered code %q; the archive is "+
			"readable, not writable", Code(err))
	}
	n, _ = c.store.Read(c.wide, "priya")
	if len(n.Versions) != versionsBefore {
		t.Errorf("criterion 16: the refused amendment added a version anyway (%d -> %d)", versionsBefore, len(n.Versions))
	}
	// A still-active colleague is unaffected — the gate is about the person, not about writing.
	if _, err := c.store.Publish(Publication{Author: "ravi", Title: "still here", Body: "b"}); err != nil {
		t.Errorf("criterion 16: an active colleague was refused too: %v", err)
	}
}

// TestOnlyTheHubDeactivatesAPerson is criterion 17, asserted structurally: the package offers
// exactly one way to deactivate somebody, and it takes no event, channel or directory record.
func TestOnlyTheHubDeactivatesAPerson(t *testing.T) {
	// The one act. It takes a PersonID and nothing else — no timestamp from elsewhere, no source,
	// no signal object that some client-side path could construct.
	c := newCompany(t)
	if c.arch.IsDeactivated("priya") {
		t.Fatal("fixture: nobody has left yet")
	}
	c.arch.Deactivate("priya")
	if !c.arch.IsDeactivated("priya") {
		t.Fatal("Deactivate did not take effect")
	}
	// PeopleStatus — the interface every OTHER path in the product consults — is read-only. A
	// surface, a channel ingester or a directory syncer holding one cannot deactivate anybody
	// through it. That is what makes "no client-side event deactivates a person on its own" a
	// property of the shape rather than of everybody's restraint: the only writer is whoever holds
	// the concrete *Archive, which is the hub.
	iface := reflect.TypeOf((*PeopleStatus)(nil)).Elem()
	if iface.NumMethod() != 1 || iface.Method(0).Name != "HasLeft" {
		var names []string
		for i := 0; i < iface.NumMethod(); i++ {
			names = append(names, iface.Method(i).Name)
		}
		t.Errorf("criterion 17: PeopleStatus has methods %v. It must expose exactly one, read-only "+
			"question. A write method here is a way for something other than the hub to deactivate "+
			"a person.", names)
	}

	// And the one act takes a PersonID AND NOTHING ELSE — no event, no signal object, no directory
	// record a mirror could hand it. Criterion 17 is that deactivation is performed against the
	// hub; a Deactivate that accepted a payload is where a channel signal would arrive.
	act, ok := reflect.TypeOf(c.arch).MethodByName("Deactivate")
	if !ok {
		t.Fatal("Archive has no Deactivate")
	}
	// Method value on a *Archive: receiver plus arguments.
	if act.Type.NumIn() != 2 || act.Type.In(1) != reflect.TypeOf(PersonID("")) {
		t.Errorf("criterion 17: Archive.Deactivate has signature %v; it must take a PersonID and "+
			"nothing else, so there is nowhere for a directory record or a client-side event to be "+
			"passed in", act.Type)
	}
}

// -------------------------------------------------------------------------------------------
// §5.4 — nothing expires
// -------------------------------------------------------------------------------------------

// TestNothingExpiresLongAfterTheDeparture advances the clock past any plausible retention window
// and asserts the archived notes and every version are still there and still attributed.
func TestNothingExpiresLongAfterTheDeparture(t *testing.T) {
	c := newCompany(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	c.store.SetClock(func() time.Time { return now })
	c.amend(t, c.wide, "as it stood in March")
	c.arch.Deactivate("priya")

	for _, after := range []time.Duration{
		30 * 24 * time.Hour,        // a month
		365 * 24 * time.Hour,       // a year
		7 * 365 * 24 * time.Hour,   // seven years, the longest window anybody quotes
		100 * 365 * 24 * time.Hour, // and past any of them
	} {
		now = now.Add(after)
		c.store.SetClock(func() time.Time { return now })
		n, by, err := AttributedRead(c.store, c.arch, c.wide, "ravi")
		if err != nil {
			t.Fatalf("§5.4: %v after the departure the note answered %v (code %s). nothing expires.",
				after, err, Code(err))
		}
		if len(n.Versions) != 2 {
			t.Errorf("§5.4: %v after the departure the note has %d versions, not 2", after, len(n.Versions))
		}
		for i := 1; i <= 2; i++ {
			if _, _, verr := AttributedVersion(c.store, c.arch, VersionRef{Note: c.wide, Number: i}, "ravi"); verr != nil {
				t.Errorf("§5.4: %v after the departure version %d answered %v", after, i, verr)
			}
		}
		if by.Author != "priya" || by.Active != tri.No {
			t.Errorf("§5.4: %v after the departure the note is attributed %q/%v", after, by.Author, by.Active)
		}
	}
}

// -------------------------------------------------------------------------------------------
// Structure
// -------------------------------------------------------------------------------------------

// TestDepartedErrorsArePairwiseDistinct checks this Issue's errors against the other two lists
// rather than restating them, so a code that collides with an existing one is a test failure.
func TestDepartedErrorsArePairwiseDistinct(t *testing.T) {
	var all []*Error
	all = append(all, allErrors...)
	all = append(all, versionErrors...)
	all = append(all, departedErrors...)
	codes := map[string]bool{}
	msgs := map[string]bool{}
	for _, e := range all {
		if codes[e.Code] {
			t.Errorf("two errors share the code %q", e.Code)
		}
		if msgs[e.Msg] {
			t.Errorf("two errors share the message %q", e.Msg)
		}
		codes[e.Code] = true
		msgs[e.Msg] = true
	}
	// The two answers this Issue is most about must not share an exit-code-shaped signal either.
	if ErrPersonDeactivated.Code == ErrPersonStateUndetermined.Code {
		t.Error("`could not determine` and `determined to be nothing` share a code")
	}
}

// TestDeactivationAddsNoScope is the standing constraint: the vocabulary is exactly three.
func TestDeactivationAddsNoScope(t *testing.T) {
	if got := len(Vocabulary()); got != 3 {
		t.Fatalf("the scope vocabulary has %d names; it is exactly three — read, write, publish. "+
			"The hub operator's read-everything is a deployment fact, not a scope.", got)
	}
	for _, s := range Vocabulary() {
		switch s {
		case ScopeRead, ScopeWrite, ScopePublish:
		default:
			t.Errorf("unexpected scope %q", s)
		}
	}
}

// TestDeactivationIsNotAnInputToCanRead is the hard constraint of this Issue expressed directly:
// CanRead's answer for every note and every reader is identical before and after a departure.
func TestDeactivationIsNotAnInputToCanRead(t *testing.T) {
	c := newCompany(t)
	readers := []PersonID{"priya", "sam", "ravi", "nobody", ""}
	ids := c.store.IDs()
	before := map[string]tri.Value{}
	for _, id := range ids {
		n, _ := c.store.Read(id, "priya") // priya authored most; use the store's own view
		for _, r := range readers {
			if n == nil {
				continue
			}
			before[string(id)+"/"+string(r)] = CanReadNote(n, r, c.store.Members())
		}
	}
	c.arch.Deactivate("priya")
	c.arch.Deactivate("ravi")
	c.arch.MarkUnreadable("sam")
	for _, id := range ids {
		n := noteFor(t, c.store, id)
		for _, r := range readers {
			key := string(id) + "/" + string(r)
			if got := CanReadNote(n, r, c.store.Members()); got != before[key] {
				t.Errorf("deactivation changed CanRead for %s: %v -> %v. visibility is not a "+
					"function of who still works here.", key, before[key], got)
			}
		}
	}
}

// -------------------------------------------------------------------------------------------
// helpers
// -------------------------------------------------------------------------------------------

func publishAs(t *testing.T, s *Store, author PersonID, title string, v Visibility) NoteID {
	t.Helper()
	n, err := s.Publish(Publication{Author: author, Title: title, Body: "billing reconciliation " + title, Visibility: v})
	if err != nil {
		t.Fatalf("Publish %s: %v", title, err)
	}
	return n.ID
}

func hitNotes(hits []SearchHit) []NoteID {
	out := make([]NoteID, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Ref.Note)
	}
	return out
}

func sameIDs(a, b []NoteID) bool {
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

func bodyOf(n *Note) string {
	if n == nil {
		return ""
	}
	return n.Latest().Body
}

// noteFor reaches the stored note whatever its visibility, for a structural assertion about
// CanRead. It uses the author's own read, which every note permits.
func noteFor(t *testing.T, s *Store, id NoteID) *Note {
	t.Helper()
	for _, who := range []PersonID{"priya", "ravi", "sam"} {
		if n, err := s.Read(id, who); err == nil {
			return n
		}
	}
	t.Fatalf("no reader in the fixture can reach note %q", id)
	return nil
}
