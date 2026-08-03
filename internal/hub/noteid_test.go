package hub

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests use the REAL crypto/rand generator except where they deliberately inject a failing or
// colliding one. That is the point: unguessability asserted against a test double is a property of
// the double. newTestHub is NOT used here, because it seeds ids deterministically so that the
// search leak tests can compare two corpora byte for byte.
func realIDStore(t *testing.T) (*Store, *Record) {
	t.Helper()
	if randRead == nil {
		t.Fatal("randRead is nil")
	}
	r := NewRecord()
	s := NewStore(r)
	at := time.Unix(0, 0).UTC()
	s.SetClock(func() time.Time { at = at.Add(time.Second); return at })
	return s, r
}

// TestTheIDSpaceCannotBeEnumerated is the ruling, driven directly.
//
// The owner's demonstration on main walked note-1, note-2, note-3, note-4 and learned from the
// pattern of "refused" against "no such note" exactly how many notes were hidden from the reader.
// This walks a far wider plausible space and requires that it yields NOTHING — not a single hit,
// and in particular not a single "refused", which is the answer that discloses a note's existence.
func TestTheIDSpaceCannotBeEnumerated(t *testing.T) {
	s, r := realIDStore(t)
	r.AddPerson("searcher")
	r.AddPerson("dana")

	mine := mustPublish(t, s, Publication{Author: "ada", Title: "readable", Body: "alpha"})
	hidden := mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "beta", Visibility: SelfOnly()})
	mustPublish(t, s, Publication{Author: "ada", Title: "also readable", Body: "gamma"})

	var found, refused []string
	for i := 0; i <= 50; i++ {
		for _, guess := range []NoteID{
			NoteID("note-" + strconv.Itoa(i)),
			NoteID(fmt.Sprintf("note-%06d", i)),
			NoteID(fmt.Sprintf("note-%06x", i)),
		} {
			_, err := s.Read(guess, "searcher")
			switch Code(err) {
			case ErrNoSuchNote.Code:
				// The only acceptable answer to a guess.
			case ErrRefused.Code:
				refused = append(refused, string(guess))
			case "":
				found = append(found, string(guess))
			}
		}
	}
	// BOTH are reported in one failure. Reporting only the first would hide the more serious of
	// the two behind the more obvious one: reading a note you were guessing at is bad, but the
	// REFUSALS are the enumeration oracle the ruling is actually about, and a run that only said
	// "READ 2 notes" would let somebody fix the reads and think they were done.
	if len(found) > 0 || len(refused) > 0 {
		t.Fatalf("the id space is walkable.\n"+
			"  read outright (%d): %v\n"+
			"  answered %q (%d): %v\n"+
			"The second list is the enumeration oracle: that answer means 'this note exists and you\n"+
			"may not read it', so a caller can count exactly how many notes are hidden from them —\n"+
			"PRD §3.5 through the id door rather than the results door.",
			len(found), found, ErrRefused.Msg, len(refused), refused)
	}

	// AND THE OTHER HALF OF THE RULING, which unguessability is what makes affordable: someone
	// holding a legitimate id still gets the two answers Issue #12's criterion 12 requires, and
	// they are still distinguishable. Closing the oracle by collapsing these would satisfy this
	// Issue by breaking a closed one.
	if _, err := s.Read(mine.ID, "searcher"); err != nil {
		t.Fatalf("a legitimately held id no longer reads: %v", err)
	}
	if got := Code(s.readErr(t, hidden.ID, "searcher")); got != ErrRefused.Code {
		t.Fatalf("a held-but-unreadable id answered %q, want %q — criterion 12 requires 'refused' and\n"+
			"'no such note' stay distinguishable", got, ErrRefused.Code)
	}
	if got := Code(s.readErr(t, "note-does-not-exist", "searcher")); got != ErrNoSuchNote.Code {
		t.Fatalf("a nonexistent id answered %q, want %q", got, ErrNoSuchNote.Code)
	}
}

// readErr is a tiny helper so the assertions above read as one line each.
func (s *Store) readErr(t *testing.T, id NoteID, reader PersonID) error {
	t.Helper()
	_, err := s.Read(id, reader)
	return err
}

// TestANoteIDDoesNotDependOnWhatTheReaderCannotSee is the SECOND consequence the ruling names, and
// it is not the same defect as enumeration.
//
// With a shared counter, publishing a note nobody else may read shifted the ids of the readable
// ones, so the same note was note-1 to one reader and note-2 to another. A fix aimed only at
// guessability would not necessarily have fixed that, so it is pinned separately.
func TestANoteIDDoesNotDependOnWhatTheReaderCannotSee(t *testing.T) {
	s, r := realIDStore(t)
	r.AddPerson("searcher")
	r.AddPerson("dana")

	// A readable note, then one the searcher cannot see, then another readable one. This is the
	// exact shape the owner demonstrated: with a counter the reader holds note-1 and note-3 and the
	// GAP tells them note-2 exists.
	first := mustPublish(t, s, Publication{Author: "ada", Title: "readable", Body: "alpha"})
	idBefore := first.ID
	mustPublish(t, s, Publication{Author: "dana", Title: "hidden", Body: "beta", Visibility: SelfOnly()})
	last := mustPublish(t, s, Publication{Author: "ada", Title: "also readable", Body: "gamma"})

	if first.ID != idBefore {
		t.Fatalf("publishing an unreadable note changed an existing note's id: %s -> %s", idBefore, first.ID)
	}

	// FROM AN ID THE READER LEGITIMATELY HOLDS, ITS NEIGHBOURS MUST NOT BE DERIVABLE. This is what
	// makes the test bite: guessing at random is hopeless either way, but arithmetic on an id you
	// were GIVEN is not, and that is how the gap is found in practice.
	for _, held := range []NoteID{first.ID, last.ID} {
		for _, neighbour := range neighbouringIDs(t, held) {
			_, err := s.Read(neighbour, "searcher")
			switch Code(err) {
			case ErrNoSuchNote.Code:
			case ErrRefused.Code:
				t.Fatalf("from the held id %s, arithmetic reaches %s, which answers %q.\n"+
					"A note the searcher may not read was located by counting from one they may —\n"+
					"the id encodes its position among notes the reader cannot see.",
					held, neighbour, ErrRefused.Msg)
			default:
				t.Fatalf("from the held id %s, arithmetic reaches %s, which READS", held, neighbour)
			}
		}
	}

	// Every reader — including one who cannot see the hidden note, and one who can — must name the
	// readable note by the same id.
	for _, reader := range []PersonID{"searcher", "dana", "ada"} {
		got, err := s.Read(idBefore, reader)
		if err != nil {
			t.Fatalf("%s cannot read the company-wide note by its id: %v", reader, err)
		}
		if got.ID != idBefore {
			t.Fatalf("%s sees the note as %s, but it is %s — an id that is a function of the reader's\n"+
				"own blind spots is not an identifier", reader, got.ID, idBefore)
		}
	}
}

func TestNoteIDsAreUnguessableAndDistinct(t *testing.T) {
	s, _ := realIDStore(t)
	seen := map[NoteID]bool{}
	var ids []NoteID
	for i := 0; i < 200; i++ {
		n := mustPublish(t, s, Publication{Author: "ada", Title: "t", Body: "b"})
		if seen[n.ID] {
			t.Fatalf("id %s was minted twice", n.ID)
		}
		seen[n.ID] = true
		ids = append(ids, n.ID)
	}

	for _, id := range ids {
		rest, ok := strings.CutPrefix(string(id), noteIDPrefix)
		if !ok {
			t.Fatalf("id %q does not begin with %q", id, noteIDPrefix)
		}
		if len(rest) != noteIDBytes*2 {
			t.Fatalf("id %q carries %d hex characters, want %d (%d bits)", id, len(rest), noteIDBytes*2, noteIDBytes*8)
		}
		if _, err := hex.DecodeString(rest); err != nil {
			t.Fatalf("id %q is not hex: %v", id, err)
		}
		// NOT A COUNTER. The defect being fixed is precisely an id that parses as a small integer.
		if v, err := strconv.Atoi(rest); err == nil && v < 1_000_000 {
			t.Fatalf("id %q is a small decimal number — that is a counter, not a random id", id)
		}
	}

	// NON-SEQUENTIAL, asserted rather than assumed: consecutive ids must not differ by a constant.
	// A "random" generator that increments would pass every check above.
	var diffs []string
	for i := 1; i < len(ids); i++ {
		a, _ := hex.DecodeString(strings.TrimPrefix(string(ids[i-1]), noteIDPrefix))
		b, _ := hex.DecodeString(strings.TrimPrefix(string(ids[i]), noteIDPrefix))
		d := make([]byte, len(a))
		for j := range a {
			d[j] = b[j] - a[j]
		}
		diffs = append(diffs, hex.EncodeToString(d))
	}
	same := 0
	for _, d := range diffs {
		if d == diffs[0] {
			same++
		}
	}
	if same == len(diffs) {
		t.Fatalf("every consecutive pair of ids differs by the same amount (%s) — the ids are a\n"+
			"sequence wearing a hex costume", diffs[0])
	}
}

func TestAMintFailureRefusesAndPublishesNothing(t *testing.T) {
	// NO FALLBACK. If the system has no randomness, publication is refused — it does not quietly
	// fall back to a counter, which would reintroduce guessable ids exactly when nobody is looking.
	s, _ := realIDStore(t)
	orig := randRead
	t.Cleanup(func() { randRead = orig })
	boom := errors.New("no entropy available")
	randRead = func([]byte) (int, error) { return 0, boom }

	before := s.Count()
	n, err := s.Publish(Publication{Author: "ada", Title: "t", Body: "b"})
	if Code(err) != ErrIDUnavailable.Code {
		t.Fatalf("code %q, want %q (err %v)", Code(err), ErrIDUnavailable.Code, err)
	}
	if n != nil {
		t.Fatalf("a refused publication returned a note: %+v", n)
	}
	if s.Count() != before {
		t.Fatalf("the store grew from %d to %d on a refused publication", before, s.Count())
	}
	if !strings.Contains(err.Error(), boom.Error()) {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

func TestACollisionIsRefusedNotOverwritten(t *testing.T) {
	// At 128 bits this will not happen, which is exactly why the branch must be driven: an
	// unexercised collision branch rots into a silent overwrite of somebody's published note.
	s, _ := realIDStore(t)
	orig := randRead
	t.Cleanup(func() { randRead = orig })
	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0xAB
		}
		return len(b), nil
	}

	first, err := s.Publish(Publication{Author: "ada", Title: "the original", Body: "keep me"})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	second, err := s.Publish(Publication{Author: "bo", Title: "the collider", Body: "clobber"})
	if Code(err) != ErrIDUnavailable.Code {
		t.Fatalf("code %q, want %q — a collision must be refused", Code(err), ErrIDUnavailable.Code)
	}
	if second != nil {
		t.Fatalf("the colliding publication returned a note")
	}
	if s.Count() != 1 {
		t.Fatalf("the store holds %d notes, want 1", s.Count())
	}
	got, err := s.Read(first.ID, "ada")
	if err != nil {
		t.Fatalf("the original is gone: %v", err)
	}
	if got.Title != "the original" || got.Latest().Body != "keep me" {
		t.Fatalf("the original was overwritten by the collider: %+v", got)
	}
}

func TestIDUnavailableIsDistinguishableFromEveryOtherRefusal(t *testing.T) {
	all := append(append([]*Error{}, allErrors...), searchErrors...)
	all = append(all, ErrIDUnavailable)
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

// neighbouringIDs returns the ids reachable by adding or subtracting a small amount from id,
// preserving its width. It treats the suffix as a hex number, which also covers a decimal counter:
// "note-1" has the hex suffix 1, so +1 yields "note-2" — the very id the owner's demonstration
// found refused.
func neighbouringIDs(t *testing.T, id NoteID) []NoteID {
	t.Helper()
	suffix, ok := strings.CutPrefix(string(id), noteIDPrefix)
	if !ok || suffix == "" {
		t.Fatalf("id %q has no %q prefix, so this test cannot do arithmetic on it and would pass\n"+
			"without checking anything", id, noteIDPrefix)
	}
	n, ok := new(big.Int).SetString(suffix, 16)
	if !ok {
		t.Fatalf("id suffix %q is not hex; this test cannot derive neighbours and would pass vacuously", suffix)
	}
	var out []NoteID
	for _, delta := range []int64{-3, -2, -1, 1, 2, 3} {
		v := new(big.Int).Add(n, big.NewInt(delta))
		if v.Sign() < 0 {
			continue
		}
		out = append(out, NoteID(noteIDPrefix+fmt.Sprintf("%0*s", len(suffix), v.Text(16))))
	}
	if len(out) == 0 {
		t.Fatalf("no neighbours derived from %q — the test would pass without checking anything", id)
	}
	return out
}
