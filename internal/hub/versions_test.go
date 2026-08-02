package hub

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// fixedClock returns a clock that advances by a day each time it is called, so that version
// timestamps are ordered and deterministic. A test about ordering that depends on the wall clock is
// a test that fails on a fast machine.
func fixedClock() func() time.Time {
	t := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(24 * time.Hour)
		return t
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	r := NewRecord()
	s := NewStore(r)
	s.SetClock(fixedClock())
	return s
}

func mustPublish(t *testing.T, s *Store, p Publication) *Note {
	t.Helper()
	n, err := s.Publish(p)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return n
}

func mustAmend(t *testing.T, s *Store, id NoteID, body string) {
	t.Helper()
	if _, err := s.Amend(id, body); err != nil {
		t.Fatalf("amend: %v", err)
	}
}

// failingSource is a [VersionSource] that fails the way a real transport fails.
//
// The in-memory store cannot be unreachable, so without this the undetermined branches of the
// version surface would be unexecuted code with a comment claiming they work.
type failingSource struct {
	timelineErr error
	// versions, when non-nil, is served for Timeline so that a test can have the timeline succeed
	// and the BODY fail — which is the shape criterion 8 is really about.
	versions   []Version
	versionErr error
}

func (f failingSource) Timeline(NoteID, PersonID) ([]Version, error) {
	if f.timelineErr != nil {
		return nil, f.timelineErr
	}
	return f.versions, nil
}

func (f failingSource) VersionAt(_ NoteID, num int, _ PersonID) (Version, error) {
	if f.versionErr != nil {
		return Version{}, f.versionErr
	}
	for _, v := range f.versions {
		if v.Number == num {
			return v, nil
		}
	}
	return Version{}, Refusedf(ErrNoSuchVersion, "no version %d", num)
}

// ---------------------------------------------------------------------------
// Criterion 1 — editing creates a version, it does not overwrite
// ---------------------------------------------------------------------------

func TestAmendingCreatesAVersionAndKeepsTheEarlierBodyVerbatim(t *testing.T) {
	s := newStore(t)
	// A body with trailing whitespace and a newline, because "byte-identical to what was published"
	// is exactly the claim a helpful TrimSpace somewhere quietly breaks.
	const first = "  the runbook says restart the indexer\n"
	const second = "the runbook says drain first, then restart the indexer"

	n := mustPublish(t, s, Publication{Author: "ada", Title: "indexer runbook", Body: first})
	mustAmend(t, s, n.ID, second)

	v1, err := s.VersionAt(n.ID, 1, "ada")
	if err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if v1.Body != first {
		t.Fatalf("version 1 body = %q, want the earlier text byte-identical %q", v1.Body, first)
	}
	if v1.Body == second {
		t.Fatalf("version 1 returned the LATER body: the amendment overwrote instead of appending")
	}
	v2, err := s.VersionAt(n.ID, 2, "ada")
	if err != nil {
		t.Fatalf("version 2: %v", err)
	}
	if v2.Body != second {
		t.Fatalf("version 2 body = %q, want %q", v2.Body, second)
	}
}

// ---------------------------------------------------------------------------
// Criterion 2 — the timeline is enumerable, and one version is one entry
// ---------------------------------------------------------------------------

func TestTimelineListsEveryVersionWithARefThatCanBeFedBackIn(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "one"})
	mustAmend(t, s, n.ID, "two")
	mustAmend(t, s, n.ID, "three")

	tl, err := ListTimeline(s, nil, n.ID, "ada")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if !tl.Determined {
		t.Fatalf("timeline of a readable note reported undetermined")
	}
	if len(tl.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(tl.Entries))
	}
	want := []string{"one", "two", "three"}
	for i, e := range tl.Entries {
		// FED STRAIGHT BACK IN: the ref is printed, parsed, and used to read. Anything less would
		// pass with an identifier a person cannot actually retype.
		ref, perr := ParseVersionRef(e.Ref.String())
		if perr != nil {
			t.Fatalf("entry %d ref %q does not parse back: %v", i, e.Ref, perr)
		}
		got, rerr := ReadView(s, nil, ref, "ada")
		if rerr != nil {
			t.Fatalf("reading back entry %d: %v", i, rerr)
		}
		if got.Body != want[i] {
			t.Fatalf("entry %d read back %q, want %q", i, got.Body, want[i])
		}
	}
}

func TestSingleVersionNoteListsExactlyOneEntryAndIsNeverAnEmptyTimeline(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "only"})

	tl, err := ListTimeline(s, nil, n.ID, "ada")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(tl.Entries) != 1 {
		t.Fatalf("entries = %d, want exactly 1", len(tl.Entries))
	}
	if !tl.Determined {
		t.Fatalf("a one-version note must be a determined timeline, not an undetermined one")
	}
	out := tl.Render()
	if strings.Contains(out, UndeterminedTimelineLine) {
		t.Fatalf("a one-version note rendered the undetermined-timeline line:\n%s", out)
	}
	if !strings.Contains(out, "versions: 1") {
		t.Fatalf("rendering does not state the one version:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Criterion 3 — a ref is addressable and stable
// ---------------------------------------------------------------------------

func TestRefRoundTrips(t *testing.T) {
	in := VersionRef{Note: "note-42", Number: 7}
	out, err := ParseVersionRef(in.String())
	if err != nil {
		t.Fatalf("parse %q: %v", in, err)
	}
	if out != in {
		t.Fatalf("round trip gave %+v, want %+v", out, in)
	}
	for _, bad := range []string{"", "note-1", "note-1@v", "note-1@vx", "@v3", "note-1@v0", "note-1@v-2"} {
		if _, err := ParseVersionRef(bad); Code(err) != ErrBadVersionRef.Code {
			t.Fatalf("ParseVersionRef(%q) code = %q, want %q", bad, Code(err), ErrBadVersionRef.Code)
		}
	}
}

func TestARefObtainedEarlyReadsTheSameContentAfterManyLaterPublications(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "as it stood"})

	// The ref a person kept a month ago.
	kept := VersionRef{Note: n.ID, Number: 1}
	before, err := ReadView(s, nil, kept, "ada")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	for i := 0; i < 200; i++ {
		mustAmend(t, s, n.ID, fmt.Sprintf("revision %d", i))
	}

	after, err := ReadView(s, nil, kept, "ada")
	if err != nil {
		t.Fatalf("read after 200 amendments: %v", err)
	}
	if after.Body != before.Body || after.Body != "as it stood" {
		t.Fatalf("kept ref now reads %q, want %q — the ref did not stay addressable", after.Body, "as it stood")
	}
	if after.Ref != kept {
		t.Fatalf("ref changed to %v, want %v", after.Ref, kept)
	}
	// And it is now correctly reported as superseded, without the caller saying so.
	if after.Standing != tri.No {
		t.Fatalf("standing = %v, want superseded", after.Standing)
	}
}

// ---------------------------------------------------------------------------
// Criterion 4 — search finds the latest and says which version
// ---------------------------------------------------------------------------

func TestSearchDoesNotResurrectSupersededTextAndNamesTheVersion(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "quota", Body: "the limit is fourhundred"})
	mustAmend(t, s, n.ID, "the limit is ninehundred")

	if hits, _ := SearchLatest(s, "bo", "fourhundred"); len(hits) != 0 {
		t.Fatalf("a term that exists only in a superseded version produced %d hit(s): superseded text was resurrected", len(hits))
	}
	hits, undetermined := SearchLatest(s, "bo", "ninehundred")
	if len(undetermined) != 0 {
		t.Fatalf("unexpected undetermined ids: %v", undetermined)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if want := (VersionRef{Note: n.ID, Number: 2}); hits[0].Ref != want {
		t.Fatalf("hit ref = %v, want the current version %v", hits[0].Ref, want)
	}
	if hits[0].Standing != tri.Yes {
		t.Fatalf("a hit must be stated as current, got %v", hits[0].Standing)
	}
}

func TestForAnUnmodifiedNoteSearchAndTheTimelineNameTheSameVersion(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "unchanged since publication"})

	hits, _ := SearchLatest(s, "bo", "unchanged")
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	tl, err := ListTimeline(s, nil, n.ID, "bo")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(tl.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(tl.Entries))
	}
	// COMPARED TO EACH OTHER, not each to "note-1@v1". Two literals edited apart is the failure
	// this criterion is about.
	if hits[0].Ref != tl.Entries[0].Ref {
		t.Fatalf("search says %v, the timeline's sole entry says %v", hits[0].Ref, tl.Entries[0].Ref)
	}
	if hits[0].Ref != tl.Current {
		t.Fatalf("search says %v, the timeline's current is %v", hits[0].Ref, tl.Current)
	}
}

func TestSearchDoesNotSurfaceNotesTheSearcherCannotRead(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "a secret quota number", Visibility: SelfOnly()})
	if hits, _ := SearchLatest(s, "bo", "quota"); len(hits) != 0 {
		t.Fatalf("search surfaced %d hit(s) from a note %q may not read", len(hits), "bo")
	}
	if hits, _ := SearchLatest(s, "ada", "quota"); len(hits) != 1 || hits[0].Ref.Note != n.ID {
		t.Fatalf("the author's own search should find it: %v", hits)
	}
}

// ---------------------------------------------------------------------------
// Criteria 5 and 8 — the three standings, compared pairwise
// ---------------------------------------------------------------------------

func TestTheThreeStandingsRenderPairwiseDistinctly(t *testing.T) {
	lines := AllStandingLines()
	if len(lines) != 3 {
		t.Fatalf("standings = %d, want 3", len(lines))
	}
	names := make([]string, 0, len(lines))
	for k := range lines {
		names = append(names, k)
	}
	for i := 0; i < len(names); i++ {
		a := lines[names[i]]
		if strings.TrimSpace(a) == "" {
			t.Fatalf("standing %q renders as silence", names[i])
		}
		for j := i + 1; j < len(names); j++ {
			b := lines[names[j]]
			// PAIRWISE, and not merely "not equal": one being a substring of the other means a
			// person scanning output cannot tell them apart either.
			if a == b {
				t.Fatalf("standings %q and %q render identically: %q", names[i], names[j], a)
			}
			if strings.Contains(a, b) || strings.Contains(b, a) {
				t.Fatalf("standing %q contains standing %q:\n  %q\n  %q", names[i], names[j], a, b)
			}
		}
	}
	// And the undetermined one must not be readable as either of the determined answers.
	u := lines["undetermined"]
	for _, w := range []string{"is current", "is the note as it stands"} {
		if strings.Contains(u, w) {
			t.Fatalf("the undetermined standing contains %q and can be read as a determined answer: %q", w, u)
		}
	}
}

func TestAReaderWhoNamedNoVersionIsStillToldWhichTheyAreHolding(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})

	// Current, without naming a version.
	cur, err := CurrentView(s, nil, n.ID, "ada")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if !strings.Contains(cur.Render(), StandingCurrentLine) {
		t.Fatalf("reading the current version does not say it is current:\n%s", cur.Render())
	}

	mustAmend(t, s, n.ID, "v2")
	old, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: 1}, "ada")
	if err != nil {
		t.Fatalf("read v1: %v", err)
	}
	out := old.Render()
	if !strings.Contains(out, StandingSupersededLine) {
		t.Fatalf("reading a superseded version does not say so:\n%s", out)
	}
	if strings.Contains(out, StandingCurrentLine) {
		t.Fatalf("a superseded read also claims to be current:\n%s", out)
	}
	// The distinction is in the CONTENT, not in the ref the caller passed: strip the ref line and
	// the two must still differ.
	stripRef := func(s string) string {
		var keep []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.HasPrefix(l, "version: ") {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	curNow, err := CurrentView(s, nil, n.ID, "ada")
	if err != nil {
		t.Fatalf("current after amend: %v", err)
	}
	if stripRef(out) == stripRef(curNow.Render()) {
		t.Fatalf("with the identifier removed, a superseded read and a current read are identical")
	}
}

func TestStandingIsWorkedOutFromTheTimelineNotFromTheNumberTheCallerTyped(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})

	// The caller types 1. Before the amendment that is current; after it, it is superseded, and the
	// caller typed the same thing both times.
	ref := VersionRef{Note: n.ID, Number: 1}
	before, _ := ReadView(s, nil, ref, "ada")
	if before.Standing != tri.Yes {
		t.Fatalf("version 1 of a one-version note is current, got %v", before.Standing)
	}
	mustAmend(t, s, n.ID, "v2")
	after, _ := ReadView(s, nil, ref, "ada")
	if after.Standing != tri.No {
		t.Fatalf("version 1 after an amendment is superseded, got %v", after.Standing)
	}
}

// ---------------------------------------------------------------------------
// Criterion 6 — an unqualified request never yields superseded content
// ---------------------------------------------------------------------------

func TestAnUnqualifiedRequestReturnsTheCurrentVersionWhateverTheOrderOfPublications(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})
	for i := 2; i <= 12; i++ {
		mustAmend(t, s, n.ID, fmt.Sprintf("v%d", i))
		v, err := CurrentView(s, nil, n.ID, "ada")
		if err != nil {
			t.Fatalf("current after amendment %d: %v", i, err)
		}
		if want := fmt.Sprintf("v%d", i); v.Body != want {
			t.Fatalf("after amendment %d the unqualified read returned %q, want %q", i, v.Body, want)
		}
		if v.Standing != tri.Yes {
			t.Fatalf("after amendment %d the unqualified read is not labelled current", i)
		}
	}
}

func TestAnUnqualifiedRequestNeverFallsBackToAnOlderVersion(t *testing.T) {
	// The timeline cannot be established. Criterion 6 says criterion 8 applies — an undetermined
	// answer — rather than serving whatever version happens to be in hand.
	src := failingSource{timelineErr: Refusedf(ErrHubUnreachable, "no route"),
		versions: []Version{{Number: 1, Body: "stale text"}, {Number: 2, Body: "less stale text"}}}

	v, err := CurrentView(src, nil, "note-1", "ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Standing != tri.Undetermined {
		t.Fatalf("standing = %v, want undetermined", v.Standing)
	}
	if v.BodyKnown {
		t.Fatalf("an unqualified read with no establishable current version served a body: %q", v.Body)
	}
	out := v.Render()
	for _, leaked := range []string{"stale text", "less stale text"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("the unqualified read fell back to a version it could not confirm:\n%s", out)
		}
	}
	if !strings.Contains(out, StandingUndeterminedLine) {
		t.Fatalf("output does not say the state could not be determined:\n%s", out)
	}
}

func TestConcurrentAmendmentsNeverProduceASupersededBodyLabelledCurrent(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			if _, err := s.Amend(n.ID, fmt.Sprintf("v%d", i+2)); err != nil {
				return
			}
		}
	}()
	for i := 0; i < 300; i++ {
		v, err := CurrentView(s, nil, n.ID, "ada")
		if err != nil {
			t.Fatalf("current: %v", err)
		}
		if v.Standing != tri.Yes {
			continue // undetermined is allowed; a wrong claim is not
		}
		tl, err := ListTimeline(s, nil, n.ID, "ada")
		if err != nil {
			t.Fatalf("timeline: %v", err)
		}
		// The body served as current must be a body that was, at some point, the last one — never
		// an earlier one. Since amendments only append, checking the number is enough.
		if v.Ref.Number > tl.Current.Number {
			t.Fatalf("served version %d as current, but the timeline's current is %d", v.Ref.Number, tl.Current.Number)
		}
		got, err := s.VersionAt(n.ID, v.Ref.Number, "ada")
		if err != nil {
			t.Fatalf("version %d: %v", v.Ref.Number, err)
		}
		if got.Body != v.Body {
			t.Fatalf("version %d served body %q but stores %q", v.Ref.Number, v.Body, got.Body)
		}
	}
	<-done
}

// ---------------------------------------------------------------------------
// Criterion 7 — nothing expires
// ---------------------------------------------------------------------------

func TestNothingExpiresNoMatterHowFarTheClockIsAdvanced(t *testing.T) {
	s := NewStore(NewRecord())
	// A clock that leaps a decade per call. Any age-based window — a day, a year, seven years — is
	// crossed many times over during this test.
	now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time {
		now = now.AddDate(10, 0, 0)
		return now
	})

	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "version 1"})
	const amendments = 250
	for i := 2; i <= amendments; i++ {
		mustAmend(t, s, n.ID, fmt.Sprintf("version %d", i))
	}
	// The clock is now some 2,500 years past the first version.
	if now.Year() < 4000 {
		t.Fatalf("the clock only reached %v; this test is not exercising age at all", now)
	}

	tl, err := ListTimeline(s, nil, n.ID, "ada")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(tl.Entries) != amendments {
		t.Fatalf("timeline has %d entries after %d publications; versions were aged out", len(tl.Entries), amendments)
	}
	// EVERY version, not a sample. "Nothing expires" is a claim about all of them.
	for i := 1; i <= amendments; i++ {
		v, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: i}, "ada")
		if err != nil {
			t.Fatalf("version %d is no longer addressable: %v", i, err)
		}
		if want := fmt.Sprintf("version %d", i); v.Body != want {
			t.Fatalf("version %d reads %q, want %q", i, v.Body, want)
		}
	}
}

// TestNoRetentionMechanism walks the package's own declarations.
//
// PRD §5.4 says the product "exposes no mechanism that deletes or truncates a note's history". That
// is a claim about the SURFACE, not about one code path, so the test reads the surface: every
// function and method declared in this package's non-test files, and every method reachable on the
// store. A future change that adds `func (s *Store) Prune(...)` fails here even if nothing calls it,
// which is the point — a mechanism nobody calls today is a mechanism.
func TestNoRetentionMechanism(t *testing.T) {
	// Words that name removal. "Leave" is not among them: leaving a group is a membership fact, not
	// a retention one, and Issue #12 owns it.
	banned := []string{"prune", "expire", "expiry", "evict", "purge", "truncate", "trim",
		"retention", "reap", "vacuum", "compact", "forget", "discard", "drop"}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("parsed no packages; this test would pass vacuously")
	}
	seen := 0
	for _, p := range pkgs {
		for name, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				seen++
				lower := strings.ToLower(fd.Name.Name)
				for _, b := range banned {
					if strings.Contains(lower, b) {
						t.Fatalf("%s declares %q — a retention mechanism; PRD §5.4 is ruled: nothing expires", name, fd.Name.Name)
					}
				}
			}
		}
	}
	if seen < 20 {
		t.Fatalf("only %d declarations walked; the parse is not covering the package", seen)
	}

	// And the store itself, including anything embedded.
	st := reflect.TypeOf(&Store{})
	for i := 0; i < st.NumMethod(); i++ {
		lower := strings.ToLower(st.Method(i).Name)
		for _, b := range append(banned, "delete", "remove") {
			if strings.Contains(lower, b) {
				t.Fatalf("*Store exposes %q, a way to remove a note or a version", st.Method(i).Name)
			}
		}
	}
}

func TestADeactivatedAuthorsHistoryIsMarkedArchivedAndStillFullyReadable(t *testing.T) {
	s := newStore(t)
	arch := NewArchive()
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})
	mustAmend(t, s, n.ID, "v2")
	mustAmend(t, s, n.ID, "v3")

	arch.Deactivate("ada")

	tl, err := ListTimeline(s, arch, n.ID, "bo")
	if err != nil {
		t.Fatalf("a departed colleague's note must stay readable: %v", err)
	}
	if len(tl.Entries) != 3 {
		t.Fatalf("entries = %d after deactivation, want 3 — history was trimmed", len(tl.Entries))
	}
	if !tl.Archived {
		t.Fatalf("the timeline is not marked archived")
	}
	if !strings.Contains(tl.Render(), ArchivedLine) {
		t.Fatalf("archived is not stated in the output:\n%s", tl.Render())
	}
	for i := 1; i <= 3; i++ {
		v, err := ReadView(s, arch, VersionRef{Note: n.ID, Number: i}, "bo")
		if err != nil {
			t.Fatalf("version %d of a departed colleague's note is not readable: %v", i, err)
		}
		if !v.Archived || !strings.Contains(v.Render(), ArchivedLine) {
			t.Fatalf("version %d is not marked archived:\n%s", i, v.Render())
		}
		if v.Body != fmt.Sprintf("v%d", i) {
			t.Fatalf("version %d reads %q", i, v.Body)
		}
	}
	// ARCHIVED IS A LABEL, NOT AN ABSENCE. It must not have made anything unreadable, and it must
	// not have changed who can read it.
	if _, err := s.Read(n.ID, "bo"); err != nil {
		t.Fatalf("deactivation changed readability: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Criterion 8 — an undetermined version state renders as undetermined
// ---------------------------------------------------------------------------

func TestAVersionWhoseContentCannotBeReadIsUndeterminedNotEmpty(t *testing.T) {
	src := failingSource{
		versions:   []Version{{Number: 1, Body: "unreachable"}, {Number: 2, Body: "also unreachable"}},
		versionErr: Refusedf(ErrVersionUnreadable, "the blob store did not answer"),
	}
	v, err := ReadView(src, nil, VersionRef{Note: "note-1", Number: 1}, "ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.BodyKnown {
		t.Fatalf("an unreadable body was reported as known")
	}
	if v.Determined() {
		t.Fatalf("an unreadable body reported the view as determined; its surface would exit 0")
	}
	out := v.Render()
	if !strings.Contains(out, BodyUnreadableLine) {
		t.Fatalf("output does not say the body could not be read:\n%s", out)
	}

	// COMPARED AGAINST A GENUINELY EMPTY BODY, pairwise, not against a literal.
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: ""})
	empty, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: 1}, "ada")
	if err != nil {
		t.Fatalf("reading an empty version: %v", err)
	}
	if !empty.Determined() {
		t.Fatalf("a version whose body is genuinely empty must be a determined answer")
	}
	if empty.Render() == out {
		t.Fatalf("an unreadable body and an empty body render identically:\n%s", out)
	}
	if strings.Contains(empty.Render(), BodyUnreadableLine) {
		t.Fatalf("an empty body claims to be unreadable:\n%s", empty.Render())
	}
}

func TestATimelineThatCouldNotBeEstablishedIsNotAnEmptyHistory(t *testing.T) {
	// Criterion 12 as well as 8: three reports, compared pairwise.
	unreachable, err := ListTimeline(failingSource{timelineErr: Refusedf(ErrHubUnreachable, "no route")}, nil, "note-1", "ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unreachable.Determined {
		t.Fatalf("an unreachable hub produced a determined timeline")
	}
	if len(unreachable.Entries) != 0 {
		t.Fatalf("an unreachable hub produced %d entries; a partial list presented as whole", len(unreachable.Entries))
	}

	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "one"})
	legit, err := ListTimeline(s, nil, n.ID, "ada")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}

	_, refusal := ListTimeline(s, nil, "note-does-not-exist", "ada")
	if refusal == nil {
		t.Fatalf("a missing note produced no refusal")
	}

	renderings := map[string]string{
		"unreachable": unreachable.Render(),
		"one-version": legit.Render(),
		"refusal":     fmt.Sprintf("%v (code: %s)", refusal, Code(refusal)),
	}
	names := []string{"unreachable", "one-version", "refusal"}
	for i := range names {
		if strings.TrimSpace(renderings[names[i]]) == "" {
			t.Fatalf("%s renders as silence", names[i])
		}
		for j := i + 1; j < len(names); j++ {
			if renderings[names[i]] == renderings[names[j]] {
				t.Fatalf("%s and %s render identically:\n%s", names[i], names[j], renderings[names[i]])
			}
		}
	}
	if !strings.Contains(renderings["unreachable"], UndeterminedTimelineLine) {
		t.Fatalf("the unreachable report does not say the timeline could not be determined:\n%s", renderings["unreachable"])
	}
	if strings.Contains(renderings["unreachable"], "versions: 0") {
		t.Fatalf("the unreachable report renders as a note with no versions:\n%s", renderings["unreachable"])
	}
}

// ---------------------------------------------------------------------------
// Criterion 9 — a version that does not exist vs a version that is empty
// ---------------------------------------------------------------------------

func TestAMissingVersionIsDistinguishableFromAnEmptyOneByCodeAlone(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: ""})

	_, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: 7}, "ada")
	if Code(err) != ErrNoSuchVersion.Code {
		t.Fatalf("unknown version code = %q, want %q", Code(err), ErrNoSuchVersion.Code)
	}
	_, err = ReadView(s, nil, VersionRef{Note: "no-such", Number: 1}, "ada")
	if Code(err) != ErrNoSuchNote.Code {
		t.Fatalf("unknown note code = %q, want %q", Code(err), ErrNoSuchNote.Code)
	}
	// And the empty version succeeds — WITHOUT PARSING THE BODY, the caller can tell: one has an
	// error with a code, the other has no error at all.
	v, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: 1}, "ada")
	if err != nil {
		t.Fatalf("reading a version whose body is empty must succeed: %v", err)
	}
	if !v.BodyKnown || v.Body != "" {
		t.Fatalf("empty version read back as %+v", v)
	}
	if ErrNoSuchVersion.Code == ErrNoSuchNote.Code {
		t.Fatalf("no-such-version and no-such-note share a code")
	}
}

func TestVersionErrorsArePairwiseDistinctFromIssue12s(t *testing.T) {
	// allErrors is Issue #12's list, READ rather than restated — a collision with an existing code
	// is caught here rather than by someone noticing.
	all := append(append([]*Error{}, allErrors...), versionErrors...)
	for i := range all {
		if all[i].Code == "" || all[i].Msg == "" {
			t.Fatalf("error %d has an empty code or message: %+v", i, all[i])
		}
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

// ---------------------------------------------------------------------------
// Criterion 14 — visibility precedes version access; the timeline is not a bypass
// ---------------------------------------------------------------------------

func TestNarrowingANoteTakesItsWholeHistoryWithIt(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1 while bo was included"})
	mustAmend(t, s, n.ID, "v2 while bo was included")

	// Bo could read both, and could enumerate them.
	if _, err := ListTimeline(s, nil, n.ID, "bo"); err != nil {
		t.Fatalf("precondition: bo should be able to list the timeline: %v", err)
	}
	before, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: 1}, "bo")
	if err != nil {
		t.Fatalf("precondition: bo should be able to read version 1: %v", err)
	}
	if before.Body == "" {
		t.Fatalf("precondition: bo read an empty body")
	}

	// The note is narrowed away from bo.
	if _, err := s.SetVisibility(n.ID, "ada", SelfOnly()); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	mustAmend(t, s, n.ID, "v3 after narrowing")

	// EVERY DOOR. A bypass is whichever one somebody forgot.
	if _, err := ListTimeline(s, nil, n.ID, "bo"); err == nil {
		t.Fatalf("the timeline is a bypass: bo can still enumerate versions of a note narrowed away from them")
	}
	for i := 1; i <= 3; i++ {
		v, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: i}, "bo")
		if err == nil {
			t.Fatalf("version %d is a bypass: bo read %q from a note narrowed away from them", i, v.Body)
		}
		if v.BodyKnown || v.Body != "" {
			t.Fatalf("version %d leaked a body alongside its refusal: %q", i, v.Body)
		}
	}
	if _, err := CurrentView(s, nil, n.ID, "bo"); err == nil {
		t.Fatalf("the unqualified read is a bypass")
	}
	if _, err := s.VersionAt(n.ID, 1, "bo"); err == nil {
		t.Fatalf("Store.VersionAt is a bypass")
	}
	if _, err := s.Timeline(n.ID, "bo"); err == nil {
		t.Fatalf("Store.Timeline is a bypass")
	}
	if _, err := s.AuthorOf(n.ID, "bo"); err == nil {
		t.Fatalf("Store.AuthorOf is a bypass")
	}
	if hits, _ := SearchLatest(s, "bo", "bo was included"); len(hits) != 0 {
		t.Fatalf("search is a bypass: %d hit(s)", len(hits))
	}
	// The author is unaffected: narrowing is not deletion.
	for i := 1; i <= 3; i++ {
		if _, err := ReadView(s, nil, VersionRef{Note: n.ID, Number: i}, "ada"); err != nil {
			t.Fatalf("the author lost version %d: %v", i, err)
		}
	}
}

func TestARefusedReadersResultDoesNotRevealThatTheNoteOrItsVersionsExist(t *testing.T) {
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})
	mustAmend(t, s, n.ID, "v2")
	if _, err := s.SetVisibility(n.ID, "ada", SelfOnly()); err != nil {
		t.Fatalf("set visibility: %v", err)
	}

	_, refused := ListTimeline(s, nil, n.ID, "bo")
	_, missing := ListTimeline(s, nil, "note-9999", "bo")
	if Code(refused) != Code(missing) {
		t.Fatalf("a refused reader gets code %q and a missing note gets %q: the refusal reveals the note exists",
			Code(refused), Code(missing))
	}
	// The two messages differ only by the id the CALLER supplied, which the caller already knows.
	// Substituting it back makes the comparison exact: any other difference — an extra clause, a
	// different verb — is a signal about the note, and criterion 14 forbids one.
	normalise := func(err error, id NoteID) string { return strings.ReplaceAll(err.Error(), string(id), "<id>") }
	if normalise(refused, n.ID) != normalise(missing, "note-9999") {
		t.Fatalf("a refused reader reads %q and a missing note reads %q", refused, missing)
	}

	// A version ref they obtained some other way must not confirm the version either — including a
	// version number that DOES exist versus one that does not.
	_, existsButRefused := ReadView(s, nil, VersionRef{Note: n.ID, Number: 2}, "bo")
	_, doesNotExist := ReadView(s, nil, VersionRef{Note: n.ID, Number: 99}, "bo")
	if Code(existsButRefused) != Code(doesNotExist) || existsButRefused.Error() != doesNotExist.Error() {
		t.Fatalf("version 2 (exists) answers %q and version 99 (does not) answers %q: the existence of a version leaked",
			existsButRefused, doesNotExist)
	}
}

// ---------------------------------------------------------------------------
// The two constraints Issue #12 left for this Issue, asserted rather than promised
// ---------------------------------------------------------------------------

func TestVersionStillCarriesNoVisibility(t *testing.T) {
	// Issue #12 forbids a per-version visibility because it is how the timeline becomes a bypass.
	// This is a second, independent guard on top of #12's own: a field added here is caught even if
	// nothing yet reads it.
	vt := reflect.TypeOf(Version{})
	for i := 0; i < vt.NumField(); i++ {
		name := strings.ToLower(vt.Field(i).Name)
		if strings.Contains(name, "visib") || strings.Contains(name, "audience") || strings.Contains(name, "scope") {
			t.Fatalf("Version has field %q — a note has ONE visibility governing every version of it",
				vt.Field(i).Name)
		}
		if vt.Field(i).Type == reflect.TypeOf(Visibility{}) {
			t.Fatalf("Version has a Visibility-typed field %q", vt.Field(i).Name)
		}
	}
	// And VersionView, which is this Issue's own type and the tempting place to put one back.
	vvt := reflect.TypeOf(VersionView{})
	for i := 0; i < vvt.NumField(); i++ {
		if vvt.Field(i).Type == reflect.TypeOf(Visibility{}) {
			t.Fatalf("VersionView has a Visibility-typed field %q", vvt.Field(i).Name)
		}
	}
}

func TestTheScopeVocabularyIsStillExactlyThree(t *testing.T) {
	got := Vocabulary()
	if len(got) != 3 {
		t.Fatalf("vocabulary = %v, want exactly three scopes", got)
	}
	want := map[Scope]bool{ScopeRead: true, ScopeWrite: true, ScopePublish: true}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("vocabulary gained %q; the hub operator's reach is a deployment fact, not a scope", s)
		}
	}
	// Every version operation this Issue adds is READ, not a new capability.
	for _, tool := range VersionAPISchema() {
		if len(tool.Scopes) != 1 || tool.Scopes[0] != string(ScopeRead) {
			t.Fatalf("tool %q declares scopes %v, want exactly [%q]", tool.Tool, tool.Scopes, ScopeRead)
		}
	}
}

func TestReadVersionStillRoutesThroughTheVisibilityGate(t *testing.T) {
	// Issue #12's Store.ReadVersion must keep routing through Store.Read. Driven, not asserted by
	// reading the source: a note narrowed away from the reader must refuse at that door too.
	s := newStore(t)
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1", Visibility: SelfOnly()})
	if _, err := s.ReadVersion(n.ID, 1, "bo"); err == nil {
		t.Fatalf("Store.ReadVersion no longer routes through the visibility gate")
	}
}

// ---------------------------------------------------------------------------
// Criterion 13 — the CLI's core and the control API agree
// ---------------------------------------------------------------------------

func TestTheTextAndJSONSurfacesReportTheSameVersionState(t *testing.T) {
	s := newStore(t)
	arch := NewArchive()
	n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "v1"})
	mustAmend(t, s, n.ID, "v2")
	mustAmend(t, s, n.ID, "v3")

	view, err := ListTimeline(s, arch, n.ID, "bo")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	raw, err := TimelineJSON(s, arch, n.ID, "bo")
	if err != nil {
		t.Fatalf("timeline json: %v", err)
	}
	var got TimelineAnswer
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding the control API answer: %v", err)
	}

	// COMPARED TO EACH OTHER. Neither is compared to a fixture, because two fixtures is how two
	// surfaces drift while both their tests stay green.
	if got.Determined != view.Determined {
		t.Fatalf("determined: json %v, view %v", got.Determined, view.Determined)
	}
	if got.Current != view.Current.String() {
		t.Fatalf("current: json %q, view %q", got.Current, view.Current)
	}
	if len(got.Versions) != len(view.Entries) {
		t.Fatalf("one surface shows %d versions and the other %d", len(got.Versions), len(view.Entries))
	}
	text := view.Render()
	for i, e := range view.Entries {
		if got.Versions[i].Ref != e.Ref.String() {
			t.Fatalf("entry %d: json %q, view %q", i, got.Versions[i].Ref, e.Ref)
		}
		if got.Versions[i].Standing != StandingToken(e.Standing) {
			t.Fatalf("entry %d standing: json %q, view %q", i, got.Versions[i].Standing, StandingToken(e.Standing))
		}
		if !strings.Contains(text, e.Ref.String()) {
			t.Fatalf("the text surface does not show version %q that the control API does", e.Ref)
		}
	}

	// And one version, both ways.
	ref := VersionRef{Note: n.ID, Number: 2}
	v, err := ReadView(s, arch, ref, "bo")
	if err != nil {
		t.Fatalf("read view: %v", err)
	}
	rawV, err := VersionJSON(s, arch, ref, "bo")
	if err != nil {
		t.Fatalf("version json: %v", err)
	}
	var gotV VersionAnswer
	if err := json.Unmarshal([]byte(rawV), &gotV); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if gotV.Standing != StandingToken(v.Standing) || gotV.Ref != v.Ref.String() || gotV.Body != v.Body {
		t.Fatalf("the two surfaces disagree about %v:\n json %+v\n view %+v", ref, gotV, v)
	}
	if gotV.Standing != "superseded" {
		t.Fatalf("version 2 of a three-version note is superseded, json says %q", gotV.Standing)
	}
}

func TestTheControlAPINeverEncodesAnUnreadableBodyAsAnEmptyOne(t *testing.T) {
	src := failingSource{
		versions:   []Version{{Number: 1, Body: "x"}},
		versionErr: Refusedf(ErrVersionUnreadable, "blob store"),
	}
	raw, err := VersionJSON(src, nil, VersionRef{Note: "note-1", Number: 1}, "ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got VersionAnswer
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got.BodyKnown {
		t.Fatalf("body_known true for an unreadable body")
	}
	if strings.Contains(raw, `"body"`) {
		t.Fatalf("the answer carries a body field for a body it could not read:\n%s", raw)
	}
	if got.Note == "" {
		t.Fatalf("the answer says nothing about why there is no body:\n%s", raw)
	}
}

// ---------------------------------------------------------------------------
// The §2.4 rule still holds on this Issue's surfaces
// ---------------------------------------------------------------------------

func TestVersionSchemaDoesNotOverclaim(t *testing.T) {
	s, err := VersionAPISchemaJSON()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	// Not a point of choice — no visibility is chosen here — but criterion 8 of Issue #12 binds any
	// surface that uses an overclaiming word, and CheckSurface is that one rule.
	if err := CheckSurface("notes version schema", s, false); err != nil {
		t.Fatalf("%v", err)
	}
}
