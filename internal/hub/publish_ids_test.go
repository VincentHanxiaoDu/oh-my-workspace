package hub

import (
	"errors"
	"math/big"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Issue #10 criteria 19–23: the owner's unguessable-identifier ruling, driven at the point ids are
// MINTED, which is publication and therefore this Issue.
//
// The ruling binds #10, #12, #14 and #15 together and says none may be satisfied in a way that
// breaks another. `noteid.go` is taken byte-for-byte from Issue #15's branch so that the two
// branches cannot land two different id schemes; these tests are Issue #10's own and are named so
// that both files can exist after the merge.

func idPublish(t *testing.T, s *Store, author PersonID, title string, v Visibility) *Note {
	t.Helper()
	n, err := s.Publish(Publication{Author: author, Title: title, Body: "body of " + title, Visibility: v})
	if err != nil {
		t.Fatalf("publishing %q: %v", title, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Criterion 19 — not derivable from another id, nor from order, nor from count
// ---------------------------------------------------------------------------

// TestPublishedIDsAreNotDerivableFromEachOtherOrFromOrderOrCount asserts the PROPERTY rather than a
// length, an encoding or an alphabet, as the criterion insists.
//
// Three separate derivations are tried, because each catches a different wrong scheme:
//
//   - NEIGHBOURS. Treat the id as a number and step by ±1..±8. A counter, a counter in hex, a
//     counter with a random-looking prefix, and a "random" id seeded once and incremented all fail
//     here, and none of them fails the distinctness check on its own.
//   - ORDER. Sorting the ids must not reproduce the publication order. A monotonic scheme — a
//     timestamp, a ULID, a counter — reproduces it exactly.
//   - COUNT. The corpus size must not be recoverable from an id. A scheme that embeds the ordinal
//     anywhere leaks it, and the strongest cheap check is that consecutive gaps are not constant.
func TestPublishedIDsAreNotDerivableFromEachOtherOrFromOrderOrCount(t *testing.T) {
	const n = 120
	s := NewStore(nil)
	ids := make([]NoteID, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, idPublish(t, s, "ada", "note "+strconv.Itoa(i), CompanyWide()).ID)
	}

	// DISTINCT. A scheme that repeats has failed before the interesting checks start.
	seen := map[NoteID]int{}
	for i, id := range ids {
		if prev, dup := seen[id]; dup {
			t.Fatalf("publications %d and %d both got the id %q", prev, i, id)
		}
		seen[id] = i
	}

	// NEIGHBOURS. No id is reachable from any other by a small step.
	for i, id := range ids {
		for _, near := range publishNeighbours(t, id) {
			if j, taken := seen[near]; taken && j != i {
				t.Fatalf("id %q (publication %d) is one small step from %q (publication %d):\n"+
					"  an id derivable from another id is an enumeration oracle, which is what the\n"+
					"  owner's ruling exists to close.", id, i, near, j)
			}
		}
	}

	// ORDER. The publication order must not be recoverable by sorting.
	sorted := append([]NoteID(nil), ids...)
	for a := 0; a < len(sorted); a++ {
		for b := a + 1; b < len(sorted); b++ {
			if sorted[b] < sorted[a] {
				sorted[a], sorted[b] = sorted[b], sorted[a]
			}
		}
	}
	same := true
	for i := range ids {
		if ids[i] != sorted[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("sorting the ids reproduces the publication order exactly; the id is a monotonic\n" +
			"sequence wearing whatever costume it is wearing, and publication order is derivable from it")
	}

	// COUNT. The gaps between consecutive ids, as numbers, must not be constant — a constant gap is
	// a counter with a stride, from which the corpus size follows immediately.
	gaps := map[string]int{}
	for i := 1; i < len(ids); i++ {
		gaps[new(big.Int).Sub(publishIDNumber(t, ids[i]), publishIDNumber(t, ids[i-1])).String()]++
	}
	for gap, count := range gaps {
		if count > 2 {
			t.Fatalf("the gap %s between consecutive ids occurs %d times in %d publications;\n"+
				"a fixed stride makes the corpus size derivable from any two ids", gap, count, len(ids))
		}
	}
}

// publishNeighbours returns ids reachable from id by a small step, preserving its shape. It treats
// the suffix as a hex number, which also covers a decimal counter: `note-1` has the hex suffix 1,
// so +1 is `note-2`.
func publishNeighbours(t *testing.T, id NoteID) []NoteID {
	t.Helper()
	suffix, ok := strings.CutPrefix(string(id), noteIDPrefix)
	if !ok || suffix == "" {
		t.Fatalf("id %q has no %q prefix, so this test cannot do arithmetic on it and would pass without checking anything", id, noteIDPrefix)
	}
	n := publishIDNumber(t, id)
	width := len(suffix)
	var out []NoteID
	for step := -8; step <= 8; step++ {
		if step == 0 {
			continue
		}
		v := new(big.Int).Add(n, big.NewInt(int64(step)))
		if v.Sign() < 0 {
			continue
		}
		h := v.Text(16)
		for len(h) < width {
			h = "0" + h
		}
		out = append(out, NoteID(noteIDPrefix+h))
	}
	return out
}

func publishIDNumber(t *testing.T, id NoteID) *big.Int {
	t.Helper()
	suffix, _ := strings.CutPrefix(string(id), noteIDPrefix)
	n, ok := new(big.Int).SetString(suffix, 16)
	if !ok {
		t.Fatalf("id suffix %q is not a number this test can work with, so it would pass vacuously", suffix)
	}
	return n
}

// ---------------------------------------------------------------------------
// Criterion 20 — publishing something a reader cannot see changes no id they can see
// ---------------------------------------------------------------------------

// TestPublishingAnUnreadableNoteChangesNoIdentifierTheReaderCanObserve is the DIFFERENTIAL the
// criterion asks for, run against one store rather than two.
//
// Two stores would differ in every id, because the ids are random — which would make the test pass
// for the wrong reason and would also pass against the old counter. So the control corpus and the
// test corpus are the SAME corpus before and after the one additional unreadable note, and the
// claim is that every id the reader can observe is unchanged. Under the old shared counter it was
// not: publishing a hidden note shifted the readable ones.
func TestPublishingAnUnreadableNoteChangesNoIdentifierTheReaderCanObserve(t *testing.T) {
	const reader = PersonID("grace")
	s := NewStore(nil)

	for i := 0; i < 5; i++ {
		idPublish(t, s, "ada", "readable "+strconv.Itoa(i), CompanyWide())
	}
	control := observableIDs(s, reader)
	if len(control) != 5 {
		t.Fatalf("the reader can observe %d ids before the differential; this test is not set up", len(control))
	}

	// THE ONE ADDITIONAL NOTE THE READER MAY NOT READ.
	hidden := idPublish(t, s, "ada", "not for grace", SelfOnly())
	if got := CanReadNote(mustRead(t, s, hidden.ID, "ada"), reader, s.Members()); got.String() == "yes" {
		t.Fatalf("the note this test needs to be unreadable is readable; the differential proves nothing")
	}

	after := observableIDs(s, reader)
	if len(after) != len(control) {
		t.Fatalf("the reader observes %d ids after the hidden publication and %d before", len(after), len(control))
	}
	for i := range control {
		if control[i] != after[i] {
			t.Fatalf("identifier %d changed when a note the reader may not read was published:\n"+
				"  before %q\n  after  %q\n"+
				"An identifier that is a function of the reader's own blind spots is not an identifier.",
				i, control[i], after[i])
		}
	}
	// And the hidden note's id is not among them, which is the other half of §3.5.
	for _, id := range after {
		if id == hidden.ID {
			t.Fatalf("the hidden note's id %q is observable to a reader who may not read it", id)
		}
	}

	// THE DIFFERENTIAL AS WORDED IS ALSO SATISFIED BY A COUNTER, AND THAT IS WHY THIS IS HERE.
	//
	// "Every identifier the reader can observe is the same in both" holds for any scheme that
	// assigns an id once and never revises it — including the sequential counter the ruling exists
	// to remove, under which nothing above goes red. What the criterion is FOR is that the hidden
	// note is not locatable from what the reader can see, so that is asserted directly: no id the
	// reader can observe is a small step away from the one they may not.
	for _, id := range after {
		for _, near := range publishNeighbours(t, id) {
			if near == hidden.ID {
				t.Fatalf("the hidden note %q is one small step from %q, which the reader can observe:\n"+
					"  the reader can locate a note they may not read by counting from one they may.",
					hidden.ID, id)
			}
		}
	}
}

func observableIDs(s *Store, reader PersonID) []NoteID {
	readable, _ := s.ListReadable(reader)
	out := make([]NoteID, 0, len(readable))
	for _, n := range readable {
		out = append(out, n.ID)
	}
	return out
}

func mustRead(t *testing.T, s *Store, id NoteID, as PersonID) *Note {
	t.Helper()
	n, err := s.Read(id, as)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Criterion 21 — stable for the life of the note
// ---------------------------------------------------------------------------

// Unguessable must not mean unstable: a link captured today keeps resolving after later versions,
// later publications and a narrowing.
func TestAnIdentifierCapturedOnceKeepsResolvingToTheSameNote(t *testing.T) {
	s := NewStore(nil)
	n := idPublish(t, s, "ada", "the one to keep", CompanyWide())
	captured := n.ID

	// LATER VERSIONS.
	for i := 0; i < 5; i++ {
		if _, err := s.Amend(captured, "amended "+strconv.Itoa(i)); err != nil {
			t.Fatalf("amending: %v", err)
		}
	}
	// LATER PUBLICATIONS, including ones the reader may not read.
	for i := 0; i < 10; i++ {
		v := CompanyWide()
		if i%2 == 0 {
			v = SelfOnly()
		}
		idPublish(t, s, "ada", "noise "+strconv.Itoa(i), v)
	}
	// A NARROWING AND A WIDENING.
	if _, err := s.SetVisibility(captured, "ada", SelfOnly()); err != nil {
		t.Fatalf("narrowing: %v", err)
	}
	if _, err := s.SetVisibility(captured, "ada", CompanyWide()); err != nil {
		t.Fatalf("widening: %v", err)
	}

	got, err := s.Read(captured, "ada")
	if err != nil {
		t.Fatalf("the captured identifier no longer resolves: %v", err)
	}
	if got.ID != captured {
		t.Fatalf("the note's id changed from %q to %q", captured, got.ID)
	}
	if got.Title != "the one to keep" {
		t.Fatalf("the captured identifier resolves to a different note: %q", got.Title)
	}
	// The whole timeline is still addressable through the same id.
	if len(got.Versions) != 6 {
		t.Fatalf("the note has %d versions; want 6", len(got.Versions))
	}
	if v, verr := s.ReadVersion(captured, 1, "ada"); verr != nil || v.Body != "body of the one to keep" {
		t.Fatalf("version 1 through the captured identifier: %v / %q", verr, v.Body)
	}
}

// ---------------------------------------------------------------------------
// Criterion 22 — "not permitted" and "no such note" stay distinguishable
// ---------------------------------------------------------------------------

// Unguessability REPLACES enumeration resistance; it does not replace the distinction Issue #12
// requires. Somebody holding a legitimate link they may not use must be told which of the two they
// have met.
func TestHoldingAnIdentifierYouMayNotReadIsDistinctFromNoSuchNote(t *testing.T) {
	s := NewStore(nil)
	hidden := idPublish(t, s, "ada", "not for grace", SelfOnly())

	_, refused := s.Read(hidden.ID, "grace")
	if Code(refused) != ErrRefused.Code {
		t.Fatalf("reading a note you may not read gives code %q, want %q", Code(refused), ErrRefused.Code)
	}
	_, absent := s.Read("note-00000000000000000000000000000000", "grace")
	if Code(absent) != ErrNoSuchNote.Code {
		t.Fatalf("reading a note that does not exist gives code %q, want %q", Code(absent), ErrNoSuchNote.Code)
	}
	if Code(refused) == Code(absent) {
		t.Fatal("the two answers share a code")
	}
	if refused.Error() == absent.Error() {
		t.Fatalf("the two answers share a message: %q", refused.Error())
	}
}

// ---------------------------------------------------------------------------
// Criterion 23 — usable as a path segment, safe to print
// ---------------------------------------------------------------------------

func TestAnIdentifierIsOnePathSegmentAndSafeToPrint(t *testing.T) {
	s := NewStore(nil)
	for i := 0; i < 40; i++ {
		id := string(idPublish(t, s, "ada", "n"+strconv.Itoa(i), CompanyWide()).ID)

		if strings.ContainsAny(id, `/\`) {
			t.Fatalf("id %q contains a path separator", id)
		}
		if filepath.Base(id) != id || filepath.Clean(id) != id {
			t.Fatalf("id %q is not a single path segment (Base=%q Clean=%q)", id, filepath.Base(id), filepath.Clean(id))
		}
		if id == "." || id == ".." || strings.HasPrefix(id, ".") || strings.HasPrefix(id, "-") {
			t.Fatalf("id %q is a dot segment or would be read as an option", id)
		}
		if url.PathEscape(id) != id {
			t.Fatalf("id %q needs escaping to appear in a path", id)
		}
		// NO SHELL METACHARACTER, NO WHITESPACE, NO CONTROL OR ESCAPE SEQUENCE. The last is the one
		// that matters most: an id containing an ANSI escape can rewrite a person's terminal.
		if strings.ContainsAny(id, " \t\n\r\v\f") {
			t.Fatalf("id %q contains whitespace", id)
		}
		if strings.ContainsAny(id, "*?[]{}()$`\"'|&;<>!#~^") {
			t.Fatalf("id %q contains a shell metacharacter", id)
		}
		for _, r := range id {
			if r > unicode.MaxASCII || unicode.IsControl(r) || !unicode.IsPrint(r) {
				t.Fatalf("id %q contains the non-printable or non-ASCII rune %q", id, r)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// No fallback when there is no randomness, and no overwrite on a collision
// ---------------------------------------------------------------------------

// A machine with no randomness gets a REFUSED publication, not a guessable id. A fallback here
// would reintroduce enumerable ids at exactly the moment nobody is watching, and a note published
// with a guessable id stays guessable forever.
func TestWithNoRandomnessThePublicationIsRefusedAndNothingIsStored(t *testing.T) {
	s := NewStore(nil)
	prev := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy on this machine") }
	t.Cleanup(func() { randRead = prev })

	n, err := s.Publish(Publication{Author: "ada", Title: "t", Body: "b"})
	if err == nil {
		t.Fatalf("a publication succeeded with no randomness, giving id %q", n.ID)
	}
	if Code(err) != ErrIDUnavailable.Code {
		t.Fatalf("code = %q, want %q", Code(err), ErrIDUnavailable.Code)
	}
	if s.Count() != 0 {
		t.Fatalf("the hub holds %d notes after a refused mint", s.Count())
	}
}

// A collision is REFUSED, never overwritten. At 128 bits it will not happen, which is exactly why
// the branch would otherwise rot into a silent overwrite of somebody's note.
func TestACollidingIdentifierIsRefusedAndTheOriginalSurvives(t *testing.T) {
	s := NewStore(nil)
	first := idPublish(t, s, "ada", "the original", CompanyWide())

	prev := randRead
	fixed, _ := strings.CutPrefix(string(first.ID), noteIDPrefix)
	randRead = func(b []byte) (int, error) {
		raw, derr := hexBytes(fixed)
		if derr != nil {
			return 0, derr
		}
		copy(b, raw)
		return len(b), nil
	}
	t.Cleanup(func() { randRead = prev })

	second, err := s.Publish(Publication{Author: "ada", Title: "the collider", Body: "overwrite me"})
	if err == nil {
		t.Fatalf("a colliding publication succeeded and returned %q", second.ID)
	}
	if Code(err) != ErrIDUnavailable.Code {
		t.Fatalf("code = %q, want %q", Code(err), ErrIDUnavailable.Code)
	}
	if s.Count() != 1 {
		t.Fatalf("the hub holds %d notes; want 1", s.Count())
	}
	got := mustRead(t, s, first.ID, "ada")
	if got.Title != "the original" || got.Latest().Body != "body of the original" {
		t.Fatalf("the original was overwritten by the collider: %+v", got)
	}
}

func hexBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd-length hex")
	}
	out := make([]byte, len(s)/2)
	for i := range out {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}
