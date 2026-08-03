package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// --- fixtures -----------------------------------------------------------------------------------

// statsWorld is a hub with a clock a test controls, so recency assertions are about the code and
// not about how long the test took to run.
type statsWorld struct {
	store  *Store
	record *Record
	at     map[NoteID]time.Time
}

func newStatsWorld(t *testing.T) *statsWorld {
	t.Helper()
	r := NewRecord()
	s := NewStore(r)
	n := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { n = n.Add(time.Hour); return n })
	return &statsWorld{store: s, record: r, at: map[NoteID]time.Time{}}
}

func (w *statsWorld) publish(t *testing.T, author PersonID, title string, v Visibility) *Note {
	t.Helper()
	n, err := w.store.Publish(Publication{Author: author, Title: title, Body: title + " body", Visibility: v})
	if err != nil {
		t.Fatalf("publish %q: %v", title, err)
	}
	w.at[n.ID] = n.Latest().At
	return n
}

func (w *statsWorld) amend(t *testing.T, id NoteID, body string) *Note {
	t.Helper()
	n, err := w.store.Amend(id, body)
	if err != nil {
		t.Fatalf("amend %q: %v", string(id), err)
	}
	w.at[id] = n.Latest().At
	return n
}

// statsDirectory answers scope EXISTENCE from a real record while failing MEMBERSHIP for one
// group.
//
// THIS IS A REAL STATE, NOT A CONVENIENCE. The hub knows the group exists but cannot read who is
// in it — the membership record is unavailable — which is exactly the state PRD §4.3 says must
// come out as undetermined rather than as "nobody is in it". It is the only way to reach a note
// that is READABLE but whose SCOPE MEMBERSHIP cannot be settled, which is what criterion 8's
// partial determination is driven with.
type statsDirectory struct {
	rec      *Record
	failsFor GroupID
}

func (d statsDirectory) Knows(g GroupID) (bool, error)        { return d.rec.Knows(g) }
func (d statsDirectory) KnowsPerson(p PersonID) (bool, error) { return d.rec.KnowsPerson(p) }
func (d statsDirectory) IsMember(g GroupID, p PersonID) (bool, error) {
	if g == d.failsFor {
		return false, errors.New("the membership record could not be read")
	}
	return d.rec.IsMember(g, p)
}

// --- criteria 4 and 5: every count is per-reader, and the narrow reader's numbers are consistent
// with a corpus in which the unreadable notes do not exist ---------------------------------------

// TestCountIsTheReadableSubset drives criterion 4 and, with its second half, criterion 5.
//
// The unreadable note is the NEWEST note in the store. That is on purpose: a recency figure taken
// from the store rather than the corpus would be drawn from a note the requester cannot read, and
// criterion 5 names that leak in as many words. So this test fails on three separate defects — a
// count over the store, a recency over the store, and an author facet over the store.
func TestCountIsTheReadableSubset(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("ada")
	w.record.AddPerson("searcher")
	open := w.publish(t, "ada", "open note", CompanyWide())
	hidden := w.publish(t, "ada", "hidden note", SelfOnly())

	if !w.at[hidden.ID].After(w.at[open.ID]) {
		t.Fatalf("fixture is not driving the leak: the hidden note must be the newest note in the store")
	}

	narrow, err := Settle(w.store, "searcher").Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	n, ok := narrow.Notes.Value()
	if !ok || n != 1 {
		t.Fatalf("notes = %s, want a determined 1 — the count must be the READABLE subset, not the store's %d notes",
			narrow.Notes.Render(), w.store.Count())
	}
	at, hasAt := narrow.Recency.At()
	if !hasAt || !at.Equal(w.at[open.ID]) {
		t.Fatalf("recency = %s, want %s — recency must be drawn from a note the requester may read, and the newest note in the store is one they may not",
			narrow.Recency.Render(), w.at[open.ID].Format(time.RFC3339))
	}

	// The same request as the author, whose readable set is strictly wider.
	wide, err := Settle(w.store, "ada").Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("statistics as author: %v", err)
	}
	if wn, _ := wide.Notes.Value(); wn != 2 {
		t.Fatalf("notes as author = %s, want 2 — statistics that do not differ per reader are not per-reader at all", wide.Notes.Render())
	}

	// Criterion 5: the narrow reader's answer is byte-identical to the same request against a
	// corpus in which the unreadable note was never published.
	c := newStatsWorld(t)
	c.record.AddPerson("ada")
	c.record.AddPerson("searcher")
	c.publish(t, "ada", "open note", CompanyWide())
	control, err := Settle(c.store, "searcher").Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("control statistics: %v", err)
	}
	got := Report{Scope: CompanyScope(), Hub: narrow}.Render()
	want := Report{Scope: CompanyScope(), Hub: control}.Render()
	if got != want {
		t.Fatalf("the presence of an unreadable note changed the statistics.\nwith hidden note:\n%s\nwithout it:\n%s", got, want)
	}
}

// TestStatisticsForAnInvisibleScopeLookLikeAnEmptyOne is the second half of criterion 5: asking
// about a scope you have no visibility into is indistinguishable from asking about a scope that is
// genuinely empty of readable material.
func TestStatisticsForAnInvisibleScopeLookLikeAnEmptyOne(t *testing.T) {
	full := newStatsWorld(t)
	full.record.AddPerson("searcher")
	full.record.DefineGroup("platform", "ada")
	full.publish(t, "ada", "a platform note", mustGroup("platform"))
	full.publish(t, "ada", "another platform note", mustGroup("platform"))

	empty := newStatsWorld(t)
	empty.record.AddPerson("searcher")
	empty.record.AddPerson("ada")
	empty.record.DefineGroup("platform", "ada")

	scope, err := GroupScope("platform")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	loaded, err := Settle(full.store, "searcher").Statistics(scope)
	if err != nil {
		t.Fatalf("statistics over the loaded corpus: %v", err)
	}
	bare, err := Settle(empty.store, "searcher").Statistics(scope)
	if err != nil {
		t.Fatalf("statistics over the empty corpus: %v", err)
	}
	a := Report{Scope: scope, Hub: loaded}.Render()
	b := Report{Scope: scope, Hub: bare}.Render()
	if a != b {
		t.Fatalf("a scope full of material the requester cannot read must look exactly like an empty one.\ninvisible:\n%s\nempty:\n%s", a, b)
	}
	if !strings.Contains(a, "notes: 0") {
		t.Fatalf("the invisible scope must report a DETERMINED zero — we determined there is nothing readable:\n%s", a)
	}
}

// --- criterion 3 and 13: three statistics, and a determined "none" ------------------------------

func TestEmptyScopeReportsDeterminedNoneNotUndetermined(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")

	st, err := Settle(w.store, "searcher").Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if !st.Recency.Determined() {
		t.Fatalf("recency over a corpus with nothing readable is DETERMINED none, not undetermined: %s", st.Recency.Render())
	}
	if _, ok := st.Recency.At(); ok {
		t.Fatalf("recency claimed an instant over an empty corpus: %s", st.Recency.Render())
	}
	if st.Recency.Token() != NoneToken {
		t.Fatalf("recency token = %q, want %q", st.Recency.Token(), NoneToken)
	}
	if st.Recency.Token() == UndeterminedToken {
		t.Fatalf("determined none and undetermined must not share a token")
	}
	if st.Coverage != tri.Yes {
		t.Fatalf("coverage = %v, want complete: we determined every statistic", st.Coverage)
	}
}

// TestNoneAndUndeterminedNeverPrintTheSame is criterion 6 at the type level, over every statistic
// and every rendering the package offers. Criterion 6 says a consumer must tell the two apart BY
// INSPECTING THE OUTPUT ALONE, so it is asserted on the rendered strings, the tokens and the JSON.
func TestNoneAndUndeterminedNeverPrintTheSame(t *testing.T) {
	pairs := []struct {
		what                  string
		zeroish, undetermined string
	}{
		{"count render", DeterminedCount(0).Render(), UndeterminedCount(ErrHubUnreachable).Render()},
		{"count token", DeterminedCount(0).Token(), UndeterminedCount(ErrHubUnreachable).Token()},
		{"recency render", NoRecency().Render(), UndeterminedRecency(ErrHubUnreachable).Render()},
		{"recency token", NoRecency().Token(), UndeterminedRecency(ErrHubUnreachable).Token()},
		{"subjects render", DeterminedSubjects(nil, nil).Render(), UndeterminedSubjects(ErrHubUnreachable).Render()},
		{"subjects token", DeterminedSubjects(nil, nil).Token(), UndeterminedSubjects(ErrHubUnreachable).Token()},
	}
	for _, p := range pairs {
		if strings.Contains(p.zeroish, UndeterminedToken) {
			// STRICTER THAN INEQUALITY, and deliberately so: a mutation that renders the determined
			// nothing as the bare undetermined token still differs from the undetermined rendering,
			// because that one carries a trailing reason. Inequality alone would let it past the
			// test named for the property.
			t.Fatalf("%s: the determined nothing renders as %q, which carries the undetermined marker", p.what, p.zeroish)
		}
		if p.zeroish == p.undetermined {
			t.Fatalf("%s: a determined nothing and an undetermined statistic rendered identically as %q", p.what, p.zeroish)
		}
		if strings.TrimSpace(p.zeroish) == "" || strings.TrimSpace(p.undetermined) == "" {
			t.Fatalf("%s: a statistic rendered as silence (%q / %q) — criterion 7", p.what, p.zeroish, p.undetermined)
		}
		if !strings.Contains(p.undetermined, UndeterminedToken) {
			t.Fatalf("%s: the undetermined rendering %q does not carry the undetermined marker", p.what, p.undetermined)
		}
	}
}

// TestTheZeroStatisticIsUndetermined pins the property the whole file rests on: a statistic nobody
// set has NOT been determined to be nothing.
func TestTheZeroStatisticIsUndetermined(t *testing.T) {
	var c Count
	if c.Determined() {
		t.Fatalf("the zero Count reported itself determined — an unset statistic must never read as zero")
	}
	if _, ok := c.Value(); ok {
		t.Fatalf("the zero Count handed out a number")
	}
	var r Recency
	if r.Determined() {
		t.Fatalf("the zero Recency reported itself determined")
	}
	var s Subjects
	if s.Determined() {
		t.Fatalf("the zero Subjects reported itself determined")
	}
	var st Statistics
	if st.Coverage != tri.Undetermined {
		t.Fatalf("the zero Statistics claimed coverage %v", st.Coverage)
	}
}

// --- criterion 8: partial determination ----------------------------------------------------------

// TestPartialDetermination is the criterion that a shared "ok" flag cannot satisfy: one request,
// one response, some statistics determined and some not.
//
// The corpus holds two notes the searcher may read. One is published TO the scoped group, so it is
// in scope by its visibility alone. The other is company-wide by the same author, and whether it
// belongs to the group's scope depends on a membership record that cannot be read — so it cannot
// be placed. It is OLDER than the placed one, and that is what separates the three statistics:
//
//   - the count cannot be determined, because the unplaceable note might belong
//   - recency CAN be determined, because the unplaceable note is older either way
//   - the subjects CAN be determined, because the unplaceable note would add nobody new
func TestPartialDetermination(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.DefineGroup("platform", "ada", "searcher")
	old := w.publish(t, "ada", "older company note", CompanyWide())
	grp := w.publish(t, "ada", "group note", mustGroup("platform"))
	if !w.at[grp.ID].After(w.at[old.ID]) {
		t.Fatalf("fixture: the placed note must be the newer one")
	}

	scope, err := GroupScope("platform")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	c := SettleWith(w.store, "searcher", statsDirectory{rec: w.record, failsFor: "platform"}, nil)
	st, err := c.Statistics(scope)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}

	if st.Notes.Determined() {
		t.Fatalf("notes = %s, want undetermined: a note that might belong to this scope was not placed", st.Notes.Render())
	}
	at, ok := st.Recency.At()
	if !ok {
		t.Fatalf("recency = %s, want a determined instant: the unplaceable note is older than the placed one, so it cannot change the answer",
			st.Recency.Render())
	}
	if !at.Equal(w.at[grp.ID]) {
		t.Fatalf("recency = %s, want %s", at.Format(time.RFC3339), w.at[grp.ID].Format(time.RFC3339))
	}
	if !st.Subjects.Determined() {
		t.Fatalf("subjects = %s, want determined: the unplaceable note would add nobody new", st.Subjects.Render())
	}
	if st.Coverage == tri.Yes {
		t.Fatalf("coverage claimed complete while a statistic was undetermined")
	}

	// And the response carries BOTH, labelled, in one rendering.
	out := Report{Scope: scope, Hub: st}.Render()
	if !strings.Contains(out, "notes: "+UndeterminedToken) {
		t.Fatalf("the undetermined count is not present and labelled in the response:\n%s", out)
	}
	if !strings.Contains(out, "recency: "+at.Format(time.RFC3339)) {
		t.Fatalf("the determined recency was dragged down with the undetermined count:\n%s", out)
	}
}

// TestAnUnplaceableNoteThatCouldBeNewerMakesRecencyUndetermined is the other half of the rule
// above, and it is what stops the previous test from being an assertion that recency is simply
// never undetermined.
func TestAnUnplaceableNoteThatCouldBeNewerMakesRecencyUndetermined(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.DefineGroup("platform", "ada", "searcher")
	grp := w.publish(t, "ada", "group note", mustGroup("platform"))
	newer := w.publish(t, "ada", "newer company note", CompanyWide())
	if !w.at[newer.ID].After(w.at[grp.ID]) {
		t.Fatalf("fixture: the unplaceable note must be the newer one")
	}

	scope, _ := GroupScope("platform")
	c := SettleWith(w.store, "searcher", statsDirectory{rec: w.record, failsFor: "platform"}, nil)
	st, err := c.Statistics(scope)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if st.Recency.Determined() {
		t.Fatalf("recency = %s, want undetermined: a note that might be in scope is NEWER than anything we placed",
			st.Recency.Render())
	}
}

// --- the undetermined-readability note ------------------------------------------------------------

// TestUndeterminedReadabilityIsNeverCountedAsReadable pins the constraint #15 left: the
// undetermined ids are not folded into the count, and they are not silently dropped either.
func TestUndeterminedReadabilityIsNeverCountedAsReadable(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.AddPerson("ada")
	w.record.DefineGroup("platform", "ada")
	w.publish(t, "ada", "readable", CompanyWide())
	w.publish(t, "ada", "unresolvable", mustGroup("platform"))
	w.record.Dissolve("platform")

	c := Settle(w.store, "searcher")
	if c.Size() != 1 {
		t.Fatalf("Corpus.Size() = %d, want 1 — the fixture is not producing an undetermined note", c.Size())
	}
	st, err := c.Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if n, ok := st.UndeterminedNotes.Value(); !ok || n != 1 {
		t.Fatalf("undetermined notes = %s, want a determined 1 — they must be reported, not dropped", st.UndeterminedNotes.Render())
	}
	if st.Notes.Determined() {
		t.Fatalf("notes = %s, want undetermined: a note whose readability is unknown might be readable and in scope, so the count is not known",
			st.Notes.Render())
	}
	if got := st.Notes.Token(); got == "1" || got == "2" {
		t.Fatalf("notes rendered as the number %q — the undetermined note was folded into the count one way or the other", got)
	}
}

// --- criterion 2: three scopes, and only three ----------------------------------------------------

func TestStatisticsAtEachOfTheThreeScopes(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.DefineGroup("platform", "ada", "searcher")
	w.publish(t, "ada", "company note", CompanyWide())
	w.publish(t, "ada", "group note", mustGroup("platform"))
	w.publish(t, "bo", "another company note", CompanyWide())
	w.record.AddPerson("bo")

	person, err := PersonScope("ada")
	if err != nil {
		t.Fatalf("person scope: %v", err)
	}
	group, err := GroupScope("platform")
	if err != nil {
		t.Fatalf("group scope: %v", err)
	}
	c := Settle(w.store, "searcher")
	for _, tc := range []struct {
		scope SearchScope
		want  int
	}{
		{CompanyScope(), 3},
		{person, 2},
		{group, 2}, // the group note, plus ada's company-wide note as a current member
	} {
		st, err := c.Statistics(tc.scope)
		if err != nil {
			t.Fatalf("statistics at %s: %v", tc.scope.Token(), err)
		}
		got, ok := st.Notes.Value()
		if !ok || got != tc.want {
			t.Fatalf("notes at %s = %s, want %d", tc.scope.Token(), st.Notes.Render(), tc.want)
		}
	}
}

// TestAFourthScopeIsRefused is criterion 2's negative: a scope that is not one of the three is
// refused, not silently widened to the company and not narrowed to nothing.
func TestAFourthScopeIsRefused(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.publish(t, "ada", "company note", CompanyWide())
	c := Settle(w.store, "searcher")

	for _, bad := range []string{"team:platform", "everything", "org:acme", "person:nobody", "group:nosuch"} {
		scope, err := ParseSearchScope(bad)
		if err != nil {
			continue // refused at the parser, which is the same refusal one step earlier
		}
		if _, err := c.Statistics(scope); err == nil {
			t.Fatalf("scope %q was accepted; it must be refused rather than resolved to something else", bad)
		} else if Code(err) != ErrUnknownSearchScope.Code {
			t.Fatalf("scope %q refused with code %q, want %q", bad, Code(err), ErrUnknownSearchScope.Code)
		}
	}
}

// TestStatisticsAddedNoFourthCapabilityScope. PRD §4.5: one scope vocabulary, and it is exactly
// three. A statistics capability is not a fourth one, and the hub operator's ability to read
// everything published to it is §2.4's deployment fact, not a grantable scope.
func TestStatisticsAddedNoFourthCapabilityScope(t *testing.T) {
	if got := len(Vocabulary()); got != 3 {
		t.Fatalf("the scope vocabulary has %d entries, want exactly 3: %v", got, Vocabulary())
	}
	for _, tool := range StatsAPISchema() {
		for _, s := range tool.Scopes {
			if !KnownScope(Scope(s)) {
				t.Fatalf("tool %q names scope %q, which is not in the vocabulary %v", tool.Tool, s, Vocabulary())
			}
		}
	}
}

// --- criterion 14: one recency semantics, stable across scopes -------------------------------------

// TestRecencyIsTheLatestVersionAtEveryScope drives both halves of criterion 14: recency follows the
// LATEST version of a note (PRD §3.3), and the definition does not vary by scope.
func TestRecencyIsTheLatestVersionAtEveryScope(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.DefineGroup("platform", "ada", "searcher")
	first := w.publish(t, "ada", "note one", CompanyWide())
	firstPublished := w.at[first.ID]
	w.publish(t, "ada", "note two", mustGroup("platform"))
	// Amending the OLDER note makes it the most recently written one. A recency reading the
	// original publication instant, or version 1's timestamp, gets this wrong.
	w.amend(t, first.ID, "revised body")
	amended := w.at[first.ID]
	if !amended.After(firstPublished) {
		t.Fatalf("fixture: the amendment must be later than the publication")
	}

	person, _ := PersonScope("ada")
	group, _ := GroupScope("platform")
	c := Settle(w.store, "searcher")
	for _, scope := range []SearchScope{CompanyScope(), person, group} {
		st, err := c.Statistics(scope)
		if err != nil {
			t.Fatalf("statistics at %s: %v", scope.Token(), err)
		}
		at, ok := st.Recency.At()
		if !ok {
			t.Fatalf("recency at %s = %s, want an instant", scope.Token(), st.Recency.Render())
		}
		if !at.Equal(amended) {
			t.Fatalf("recency at %s = %s, want the LATEST version's instant %s — recency must follow §3.3's versions and must not vary by scope",
				scope.Token(), at.Format(time.RFC3339), amended.Format(time.RFC3339))
		}
	}
}

// --- criterion 15: a statistic is not a search result ---------------------------------------------

func TestStatisticsCarryNoNoteIdentifiersOrTitles(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.DefineGroup("platform", "ada", "searcher")
	n := w.publish(t, "ada", "distinctivetitle", mustGroup("platform"))

	st, err := Settle(w.store, "searcher").Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	rendered := Report{Scope: CompanyScope(), Hub: st}.Render()
	js, err := Report{Scope: CompanyScope(), Hub: st}.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, surface := range []string{rendered, js} {
		if strings.Contains(surface, string(n.ID)) {
			t.Fatalf("a statistic disclosed a note identifier:\n%s", surface)
		}
		if strings.Contains(surface, "distinctivetitle") {
			t.Fatalf("a statistic disclosed a note title:\n%s", surface)
		}
		if strings.Contains(surface, "body") {
			t.Fatalf("a statistic disclosed note text:\n%s", surface)
		}
	}
}

// --- criteria 7 and 9: the two surfaces agree, and undetermined is present in both -----------------

// TestTheAgentAPIAndTheRenderingAgree is criterion 9 driven at the level this package owns: the
// JSON and the human rendering are two views of ONE computed value, so they cannot disagree about
// which statistics are undetermined.
func TestTheAgentAPIAndTheRenderingAgree(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("searcher")
	w.record.AddPerson("ada")
	w.record.DefineGroup("platform", "ada")
	w.publish(t, "ada", "readable", CompanyWide())
	w.publish(t, "ada", "unresolvable", mustGroup("platform"))
	w.record.Dissolve("platform")

	st, err := Settle(w.store, "searcher").Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	rep := Report{
		Scope: CompanyScope(),
		Local: Statistics{Reader: "searcher", Notes: DeterminedCount(0), Subjects: DeterminedSubjects(nil, nil), Recency: NoRecency(), UndeterminedNotes: DeterminedCount(0), Coverage: tri.Yes},
		Hub:   st,
	}
	js, err := rep.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var decoded reportJSON
	if err := json.Unmarshal([]byte(js), &decoded); err != nil {
		t.Fatalf("the agent API emitted unparseable JSON: %v\n%s", err, js)
	}

	// Criterion 7: the field is PRESENT and carries its marker. Not omitted, not null-as-absent.
	if !strings.Contains(js, `"notes"`) {
		t.Fatalf("an undetermined statistic was omitted from the agent API response:\n%s", js)
	}
	if decoded.Hub.Notes.State != UndeterminedToken {
		t.Fatalf("hub notes state = %q, want %q\n%s", decoded.Hub.Notes.State, UndeterminedToken, js)
	}
	if decoded.Hub.Notes.Reason == "" {
		t.Fatalf("an undetermined statistic carried no reason code:\n%s", js)
	}
	if decoded.Local.Notes.State != "determined" {
		t.Fatalf("local notes state = %q, want determined\n%s", decoded.Local.Notes.State, js)
	}

	// The two surfaces agree, statistic by statistic, on determinacy.
	rendered := rep.Render()
	for _, tc := range []struct {
		line  string
		state string
	}{
		{"  notes: " + st.Notes.Render(), decoded.Hub.Notes.State},
		{"  recency: " + st.Recency.Render(), decoded.Hub.Recency.State},
		{"  subjects: " + st.Subjects.Render(), decoded.Hub.Subjects.State},
	} {
		if !strings.Contains(rendered, tc.line) {
			t.Fatalf("the rendering is missing %q:\n%s", tc.line, rendered)
		}
		undeterminedInText := strings.Contains(tc.line, UndeterminedToken)
		undeterminedInJSON := tc.state == UndeterminedToken
		if undeterminedInText != undeterminedInJSON {
			t.Fatalf("the two surfaces disagree about %q: text says undetermined=%v, JSON says %q",
				tc.line, undeterminedInText, tc.state)
		}
	}
	if decoded.RecencySemantics != RecencySemantics {
		t.Fatalf("the agent API did not state the recency semantics: %q", decoded.RecencySemantics)
	}
}

// --- PRD §4.5: refused when requested, never narrowed at the edge ---------------------------------

func TestStatisticsThroughRequiresRead(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("ada")
	w.publish(t, "ada", "note", CompanyWide())

	_, err := StatisticsThrough(w.store, Grant{ID: "g", Holder: "ada", Scopes: []Scope{ScopeWrite}}, "ada", CompanyScope())
	if Code(err) != ErrReadScopeRequired.Code {
		t.Fatalf("a grant without read got %v (code %q), want %q — refused when requested, not handed smaller numbers",
			err, Code(err), ErrReadScopeRequired.Code)
	}

	_, err = StatisticsThrough(w.store, Grant{ID: "g", Holder: "ada", Scopes: []Scope{ScopeRead}}, "bo", CompanyScope())
	if Code(err) != ErrGrantWiderThanHolder.Code {
		t.Fatalf("reading statistics as somebody else got code %q, want %q", Code(err), ErrGrantWiderThanHolder.Code)
	}

	st, err := StatisticsThrough(w.store, Grant{ID: "g", Holder: "ada", Scopes: []Scope{ScopeRead}}, "ada", CompanyScope())
	if err != nil {
		t.Fatalf("a grant with read was refused: %v", err)
	}
	if n, ok := st.Notes.Value(); !ok || n != 1 {
		t.Fatalf("notes = %s, want 1", st.Notes.Render())
	}
}

// --- the new refusal is distinguishable from every existing one ------------------------------------

// TestStatisticsErrorsAreDistinguishable reads Issue #12's allErrors and Issue #15's searchErrors
// rather than restating them, so a code this file collides with is caught here rather than by a
// caller that switches on the wrong one.
func TestStatisticsErrorsAreDistinguishable(t *testing.T) {
	all := append(append(append([]*Error{}, allErrors...), searchErrors...), statisticsErrors...)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			a, b := all[i], all[j]
			if a.Code == b.Code {
				t.Fatalf("two errors share the code %q: %q and %q", a.Code, a.Msg, b.Msg)
			}
			if a.Msg == b.Msg {
				t.Fatalf("two errors share the message %q", a.Msg)
			}
		}
	}
	for _, e := range statisticsErrors {
		if e.Code == "" || e.Msg == "" {
			t.Fatalf("an error is missing a code or a message: %#v", e)
		}
	}
}

// --- the empty-identity door -----------------------------------------------------------------------

// TestAnUnidentifiedRequesterIsRefusedBeforeAnythingIsCounted closes an oracle that a count, rather
// than a list of ids, was wrongly assumed to be safe from.
//
// [CanRead] answers Undetermined for an empty reader — correctly: nobody has been named, so nothing
// about readability has been established. The consequence is that Settle puts EVERY note in the
// store into c.undetermined, and a determined count of those is a determined count of the whole
// store. The reader learns how much exists without being anybody, which is criterion 4 and 5 word
// for word: "a count of 40 where I can read 12 has told me 28 things exist that I'm not allowed to
// know exist".
//
// THE SIZE CONTROL IS THE TEST. "An identity-less caller is refused" passes against a build that
// hands everybody a determined zero, and against a broken fixture. What must hold is that a 5-note
// store and a 2-note store are INDISTINGUISHABLE to a caller who names nobody.
func TestAnUnidentifiedRequesterIsRefusedBeforeAnythingIsCounted(t *testing.T) {
	build := func(t *testing.T, n int) *statsWorld {
		t.Helper()
		w := newStatsWorld(t)
		w.record.AddPerson("ada")
		w.record.DefineGroup("platform", "ada")
		for i := 0; i < n; i++ {
			switch i % 4 {
			case 0:
				w.publish(t, "ada", fmt.Sprintf("note%d", i), CompanyWide())
			case 1:
				w.publish(t, "ada", fmt.Sprintf("note%d", i), SelfOnly())
			case 2:
				w.publish(t, "ada", fmt.Sprintf("note%d", i), mustGroup("platform"))
			default:
				w.publish(t, "ada", fmt.Sprintf("note%d", i), mustPeople("ada"))
			}
		}
		return w
	}
	big, small := build(t, 5), build(t, 2)
	if big.store.Count() != 5 || small.store.Count() != 2 {
		t.Fatalf("fixture: stores hold %d and %d notes, want 5 and 2", big.store.Count(), small.store.Count())
	}

	describe := func(t *testing.T, w *statsWorld) string {
		t.Helper()
		st, err := Settle(w.store, "").Statistics(CompanyScope())
		if err == nil {
			t.Fatalf("an identity-less request was ANSWERED: undetermined notes = %s, notes = %s — over a store of %d",
				st.UndeterminedNotes.Render(), st.Notes.Render(), w.store.Count())
		}
		if Code(err) != ErrNotSignedIn.Code {
			t.Fatalf("refused with code %q, want %q", Code(err), ErrNotSignedIn.Code)
		}
		return err.Error() + "|" + Report{Scope: CompanyScope(), Hub: st}.Render()
	}
	if a, b := describe(t, big), describe(t, small); a != b {
		t.Fatalf("a 5-note store and a 2-note store are distinguishable to a caller who names nobody:\n%s\nvs\n%s", a, b)
	}
}

// TestAGrantThatNamesNobodyIsRefused is the same door, one layer out. Grant is a plain struct with
// exported fields, so a holder left at its zero value reaches StatisticsThrough — and the
// reader-versus-holder check is vacuously satisfied when both are empty.
func TestAGrantThatNamesNobodyIsRefused(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("ada")
	for i := 0; i < 3; i++ {
		w.publish(t, "ada", fmt.Sprintf("note%d", i), CompanyWide())
	}
	st, err := StatisticsThrough(w.store, Grant{Scopes: []Scope{ScopeRead}}, "", CompanyScope())
	if err == nil {
		t.Fatalf("an empty-holder grant was answered: undetermined notes = %s over a %d-note store",
			st.UndeterminedNotes.Render(), w.store.Count())
	}
	if Code(err) != ErrNotSignedIn.Code {
		t.Fatalf("refused with code %q, want %q", Code(err), ErrNotSignedIn.Code)
	}
}

// --- the unevaluable notes belong to the scope that was asked about ----------------------------------

// TestUndeterminedNotesAreScopedToTheRequest is criterion 5's closing sentence: a scope the
// requester has no visibility into must be indistinguishable from one that is genuinely empty of
// readable material.
//
// A store-wide count of unevaluable notes breaks that on its own. Asked about bob's scope, over a
// corpus whose only unevaluable notes are alice's, the honest answer is that bob's scope holds
// none — however those notes' readability falls, they are not bob's.
func TestUndeterminedNotesAreScopedToTheRequest(t *testing.T) {
	w := newStatsWorld(t)
	w.record.AddPerson("bob")
	w.record.AddPerson("alice")
	w.record.DefineGroup("platform", "alice")
	w.publish(t, "alice", "unresolvable one", mustGroup("platform"))
	w.publish(t, "alice", "unresolvable two", mustGroup("platform"))
	w.record.Dissolve("platform")

	c := Settle(w.store, "bob")
	if len(c.UndeterminedIDs()) != 2 {
		t.Fatalf("fixture: %d unevaluable notes, want 2", len(c.UndeterminedIDs()))
	}

	company, err := c.Statistics(CompanyScope())
	if err != nil {
		t.Fatalf("company statistics: %v", err)
	}
	if n, ok := company.UndeterminedNotes.Value(); !ok || n != 2 {
		t.Fatalf("at company scope undetermined notes = %s, want 2 — they ARE in this scope and must not be dropped",
			company.UndeterminedNotes.Render())
	}

	bobScope, err := PersonScope("bob")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	mine, err := c.Statistics(bobScope)
	if err != nil {
		t.Fatalf("person statistics: %v", err)
	}
	if n, ok := mine.UndeterminedNotes.Value(); !ok || n != 0 {
		t.Fatalf("at person:bob undetermined notes = %s, want a determined 0 — those notes are alice's and cannot be in bob's scope however their readability falls",
			mine.UndeterminedNotes.Render())
	}

	// The whole point: indistinguishable from a scope that is genuinely empty.
	empty := newStatsWorld(t)
	empty.record.AddPerson("bob")
	empty.record.AddPerson("alice")
	bare, err := Settle(empty.store, "bob").Statistics(bobScope)
	if err != nil {
		t.Fatalf("statistics over the empty corpus: %v", err)
	}
	got := Report{Scope: bobScope, Hub: mine}.Render()
	want := Report{Scope: bobScope, Hub: bare}.Render()
	if got != want {
		t.Fatalf("a scope holding nothing of the requester's, next to material they cannot evaluate, is distinguishable from a genuinely empty one:\n%s\nvs\n%s", got, want)
	}
}
