package hub

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	rec := NewRecord()
	rec.DefineGroup("platform", "alice", "bo")
	rec.AddPerson("carol")
	rec.AddPerson("dan")
	return NewStore(rec)
}

// CRITERION 1, read back off a stored note.
func TestPublishWithNoChoiceIsCompanyWide(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b"}) // no Visibility field set
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	v, err := s.VisibilityOf(n.ID)
	if err != nil {
		t.Fatalf("VisibilityOf: %v", err)
	}
	if v.Token() != "company" {
		t.Errorf("visibility read back as %q, want %q — criterion 1: not empty, not 'unset', not a null the caller interprets", v.Token(), "company")
	}
	if v.IsUnset() {
		t.Error("a stored note's visibility is unset")
	}
	if got := CanRead(v, n.Author, "dan", s.Members()); got != tri.Yes {
		t.Errorf("a colleague cannot read a defaulted note: %v", got)
	}
}

// CRITERION 2. A colleague outside the named set cannot read it and does not see it in a listing —
// PRD §3.5, visibility is a precondition of ranking.
func TestNarrowedToNamedPeople(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: mustPeople("carol")})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	v, _ := s.VisibilityOf(n.ID)
	if v.Token() != "people" || len(v.People()) != 1 || v.People()[0] != "carol" {
		t.Errorf("visibility read back as %v / %v, want exactly [carol]", v.Token(), v.People())
	}
	if _, err := s.Read(n.ID, "carol"); err != nil {
		t.Errorf("a named person cannot read it: %v", err)
	}
	if _, err := s.Read(n.ID, "dan"); !errors.Is(err, ErrRefused) {
		t.Errorf("an unnamed colleague read it: %v", err)
	}
	readable, und := s.ListReadable("dan")
	if len(readable) != 0 {
		t.Errorf("the note appears in an excluded colleague's listing (%d entries) — §3.5 says ranking never surfaces what the searcher cannot read", len(readable))
	}
	if len(und) != 0 {
		t.Errorf("unexpected undetermined ids: %v", und)
	}
}

// CRITERION 3.
func TestNarrowedToGroup(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "carol", Title: "t", Body: "b", Visibility: mustGroup("platform")})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for _, p := range []PersonID{"alice", "bo"} {
		if _, err := s.Read(n.ID, p); err != nil {
			t.Errorf("member %s cannot read it: %v", p, err)
		}
	}
	if _, err := s.Read(n.ID, "dan"); !errors.Is(err, ErrRefused) {
		t.Errorf("a non-member read it: %v", err)
	}
	if err := s.Members().Join("platform", "dan"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := s.Read(n.ID, "dan"); err != nil {
		t.Errorf("a new member cannot read it: %v — criterion 3 is about CURRENT members", err)
	}
}

// CRITERION 4.
func TestNarrowedToSelf(t *testing.T) {
	rec := NewRecord()
	rec.DefineGroup("everyone", "alice", "bo", "carol")
	s := NewStore(rec)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: SelfOnly()})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := s.Read(n.ID, "alice"); err != nil {
		t.Errorf("the publisher cannot read their own self-only note: %v", err)
	}
	for _, p := range []PersonID{"bo", "carol"} {
		if _, err := s.Read(n.ID, p); !errors.Is(err, ErrRefused) {
			t.Errorf("%s read a self-only note: %v", p, err)
		}
		readable, _ := s.ListReadable(p)
		if len(readable) != 0 {
			t.Errorf("a self-only note appears in %s's listing", p)
		}
	}
}

// CRITERION 6 — THE TIMELINE IS NOT A BYPASS.
func TestNarrowingExcludesEarlierVersionsToo(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "v1", Visibility: mustPeople("carol", "dan")})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := s.Amend(n.ID, "v2"); err != nil {
		t.Fatalf("Amend: %v", err)
	}

	// dan could read version 1 while included.
	if _, err := s.ReadVersion(n.ID, 1, "dan"); err != nil {
		t.Fatalf("dan could not read version 1 while included: %v", err)
	}

	// Narrow to exclude dan.
	if _, err := s.SetVisibility(n.ID, "alice", mustPeople("carol")); err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}

	if _, err := s.Read(n.ID, "dan"); !errors.Is(err, ErrRefused) {
		t.Errorf("dan can still read the note after being narrowed out: %v", err)
	}
	for _, num := range []int{1, 2} {
		if _, err := s.ReadVersion(n.ID, num, "dan"); !errors.Is(err, ErrRefused) {
			t.Errorf("dan can still read version %d through the timeline after being narrowed out: %v — criterion 6: the timeline is not a bypass", num, err)
		}
	}
	// carol, still included, keeps the whole history.
	if _, err := s.ReadVersion(n.ID, 1, "carol"); err != nil {
		t.Errorf("carol lost the history she is entitled to: %v", err)
	}
}

// A Version must not carry its own visibility — a per-version visibility field is how the timeline
// bypass gets rebuilt, most likely by Issue #11, which owns versions and cannot ask.
//
// This is a structural assertion on purpose. The behavioural test above proves today's code does
// not bypass; this one fails the moment somebody adds the field that would let it.
func TestVersionCarriesNoVisibilityField(t *testing.T) {
	ty := reflect.TypeOf(Version{})
	for i := 0; i < ty.NumField(); i++ {
		name := strings.ToLower(ty.Field(i).Name)
		if strings.Contains(name, "visib") || ty.Field(i).Type == reflect.TypeOf(Visibility{}) {
			t.Errorf("Version has field %q: visibility belongs to the note, not to a version, or the timeline becomes a bypass around it (criterion 6)", ty.Field(i).Name)
		}
	}
}

// CRITERION 15, hub side. Refusal is total: nothing published, not company-wide, not to nobody.
func TestUnknownGroupPublicationIsRefusedAndStoresNothing(t *testing.T) {
	s := testStore(t)
	before := s.Count()

	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: mustGroup("ghosts")})
	if err == nil {
		t.Fatalf("publishing to an unknown group succeeded, note %v", n.ID)
	}
	if Code(err) != ErrUnknownGroup.Code {
		t.Errorf("code = %q, want %q — the refusal must be distinguishable without parsing prose", Code(err), ErrUnknownGroup.Code)
	}
	if n != nil {
		t.Error("a note was returned alongside the refusal")
	}
	if s.Count() != before {
		t.Errorf("the hub holds %d notes after a refused publication, was %d — nothing may be stored", s.Count(), before)
	}
	for _, id := range s.IDs() {
		v, _ := s.VisibilityOf(id)
		if v.Token() == "company" {
			t.Errorf("note %s was published company-wide as a fallback — criterion 15 forbids it", id)
		}
	}
}

// The same refusal when narrowing an EXISTING note, and the note keeps the visibility it had.
func TestSetVisibilityToUnknownGroupChangesNothing(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: SelfOnly()})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := s.SetVisibility(n.ID, "alice", mustGroup("ghosts")); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("SetVisibility to an unknown group = %v, want ErrUnknownGroup", err)
	}
	v, _ := s.VisibilityOf(n.ID)
	if v.Token() != "self" {
		t.Errorf("visibility is now %q; a refused narrowing must leave the note exactly as it was", v.Token())
	}
}

// CRITERION 12. "Refused" and "no such note" are distinguishable, by code and by wording.
func TestRefusedAndNoSuchNoteAreDistinguishable(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: SelfOnly()})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	_, refused := s.Read(n.ID, "bo")
	_, missing := s.Read("note-does-not-exist", "bo")

	if Code(refused) == Code(missing) {
		t.Fatalf("both answer with code %q — criterion 12 requires them distinguishable", Code(refused))
	}
	if Code(refused) != ErrRefused.Code {
		t.Errorf("refused code = %q, want %q", Code(refused), ErrRefused.Code)
	}
	if Code(missing) != ErrNoSuchNote.Code {
		t.Errorf("missing code = %q, want %q", Code(missing), ErrNoSuchNote.Code)
	}
	if refused.Error() == missing.Error() {
		t.Error("the two refusals read identically")
	}
}

// CRITERION 16 / 17 at the store: a note whose group cannot be resolved is undetermined on read and
// is neither counted as readable nor silently dropped from a listing.
func TestUnresolvableGroupIsUndeterminedNotNo(t *testing.T) {
	rec := NewRecord()
	rec.DefineGroup("platform", "alice")
	s := NewStore(rec)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: mustGroup("platform")})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// The record loses the group after publication — an unreadable record, criterion 16.
	s.members = &Record{} // no groups at all

	_, err = s.Read(n.ID, "bo")
	if Code(err) != ErrUndetermined.Code {
		t.Errorf("read code = %q, want %q — an unresolvable group is undetermined, never a refusal", Code(err), ErrUndetermined.Code)
	}
	if Code(err) == ErrRefused.Code || Code(err) == ErrNoSuchNote.Code {
		t.Error("undetermined collapsed into a determined answer")
	}

	readable, und := s.ListReadable("bo")
	if len(readable) != 0 {
		t.Error("an undetermined note was listed as readable")
	}
	if len(und) != 1 || und[0] != n.ID {
		t.Errorf("undetermined ids = %v, want [%s] — a listing must not silently drop what it could not evaluate", und, n.ID)
	}
}

// Only the author changes a note's visibility, and the two refusals stay distinguishable.
func TestOnlyTheAuthorChangesVisibility(t *testing.T) {
	s := testStore(t)
	n, _ := s.Publish(Publication{Author: "alice", Title: "t", Body: "b"})
	if _, err := s.SetVisibility(n.ID, "bo", SelfOnly()); !errors.Is(err, ErrRefused) {
		t.Errorf("a non-author changed a note's visibility: %v", err)
	}
	if _, err := s.SetVisibility("nope", "alice", SelfOnly()); !errors.Is(err, ErrNoSuchNote) {
		t.Errorf("SetVisibility on a missing note = %v, want ErrNoSuchNote", err)
	}
}

func TestPublishRefusesAnAuthorlessNote(t *testing.T) {
	s := testStore(t)
	if _, err := s.Publish(Publication{Title: "t", Body: "b"}); !errors.Is(err, ErrNoAuthor) {
		t.Errorf("Publish with no author = %v, want ErrNoAuthor", err)
	}
	if s.Count() != 0 {
		t.Error("an authorless note was stored")
	}
}

// No stored note is ever KindUnset. The store's own invariant, checked across every path that
// writes one.
func TestNoStoredNoteIsUnset(t *testing.T) {
	s := testStore(t)
	if _, err := s.Publish(Publication{Author: "alice", Title: "a", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.Publish(Publication{Author: "alice", Title: "c", Body: "d", Visibility: SelfOnly()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetVisibility(n.ID, "alice", Visibility{}); err == nil {
		t.Error("a note's visibility was set to no choice at all")
	}
	for _, id := range s.IDs() {
		v, err := s.VisibilityOf(id)
		if err != nil {
			t.Fatal(err)
		}
		if v.IsUnset() {
			t.Errorf("note %s is stored with no visibility", id)
		}
	}
}
