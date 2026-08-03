package hub

import (
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func restorable() *Note {
	return &Note{
		ID:         "note-abc",
		Author:     "alice",
		Title:      "the login outage",
		Visibility: CompanyWide(),
		Versions:   []Version{NoteAt(1, "restart the auth pods", time.Unix(1, 0).UTC())},
	}
}

// TestRestoreNoteKeepsTheIdItWasGiven is why RestoreNote exists at all: a hub replaying its record
// must produce the SAME corpus, not an equivalent one under new names.
func TestRestoreNoteKeepsTheIdItWasGiven(t *testing.T) {
	s := NewStore(NewRecord())
	if err := s.RestoreNote(restorable()); err != nil {
		t.Fatalf("RestoreNote: %v", err)
	}
	n, err := s.Read("note-abc", "alice")
	if err != nil {
		t.Fatalf("the restored note is not addressable by the id it was stored under: %v", err)
	}
	if n.Latest().Body != "restart the auth pods" {
		t.Errorf("body = %q, want %q", n.Latest().Body, "restart the auth pods")
	}
	if n.Latest().At != time.Unix(1, 0).UTC() {
		t.Errorf("the restored version's timestamp is %v, want %v; recency answers depend on it", n.Latest().At, time.Unix(1, 0).UTC())
	}
}

// TestRestoreNoteRefusesWhatItCannotHonourAndChangesNothing. Every one of these is a state a
// damaged or newer durable record can be in, and for every one of them the right answer is a
// refusal — a partially-restored corpus is a corpus whose answers disagree with what is on the disk.
func TestRestoreNoteRefusesWhatItCannotHonourAndChangesNothing(t *testing.T) {
	group, err := ToGroup("ghosts")
	if err != nil {
		t.Fatalf("ToGroup: %v", err)
	}
	cases := map[string]*Note{
		"a nil note":                    nil,
		"no id":                         &Note{Author: "alice", Visibility: CompanyWide(), Versions: []Version{NoteAt(1, "b", time.Unix(1, 0))}},
		"no author":                     &Note{ID: "x", Visibility: CompanyWide(), Versions: []Version{NoteAt(1, "b", time.Unix(1, 0))}},
		"no versions":                   &Note{ID: "x", Author: "alice", Visibility: CompanyWide()},
		"a timeline starting at 2":      &Note{ID: "x", Author: "alice", Visibility: CompanyWide(), Versions: []Version{NoteAt(2, "b", time.Unix(1, 0))}},
		"a timeline with a gap":         &Note{ID: "x", Author: "alice", Visibility: CompanyWide(), Versions: []Version{NoteAt(1, "b", time.Unix(1, 0)), NoteAt(3, "c", time.Unix(2, 0))}},
		"a group the hub does not know": &Note{ID: "x", Author: "alice", Visibility: group, Versions: []Version{NoteAt(1, "b", time.Unix(1, 0))}},
		"no visibility at all":          &Note{ID: "x", Author: "alice", Versions: []Version{NoteAt(1, "b", time.Unix(1, 0))}},
	}
	for name, n := range cases {
		t.Run(name, func(t *testing.T) {
			s := NewStore(NewRecord())
			if err := s.RestoreNote(restorable()); err != nil {
				t.Fatalf("setting up: %v", err)
			}
			before := s.Count()
			if err := s.RestoreNote(n); err == nil {
				t.Fatalf("%s was restored; a hub that starts having half-understood its record answers differently from the corpus it holds", name)
			}
			if after := s.Count(); after != before {
				t.Errorf("a refused restoration changed the corpus from %d notes to %d; refusal is total", before, after)
			}
		})
	}
}

// TestRestoreNoteRefusesASecondNoteUnderOneID — two notes under one id is a corpus that answers
// differently depending on which one won.
func TestRestoreNoteRefusesASecondNoteUnderOneID(t *testing.T) {
	s := NewStore(NewRecord())
	if err := s.RestoreNote(restorable()); err != nil {
		t.Fatalf("RestoreNote: %v", err)
	}
	second := restorable()
	second.Versions = []Version{NoteAt(1, "a different body entirely", time.Unix(2, 0).UTC())}
	if err := s.RestoreNote(second); err == nil {
		t.Fatal("a second note was restored under an id the hub already holds")
	}
	n, err := s.Read("note-abc", "alice")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n.Latest().Body != "restart the auth pods" {
		t.Errorf("the refused restoration overwrote the note it collided with: body is now %q", n.Latest().Body)
	}
}

// TestARestoredNoteIsSubjectToTheSameVisibilityRule — restoration is not a way round CanRead.
func TestARestoredNoteIsSubjectToTheSameVisibilityRule(t *testing.T) {
	members := NewRecord()
	members.AddPerson("alice")
	members.AddPerson("bob")
	members.DefineGroup("platform", "alice")
	s := NewStore(members)

	group, err := ToGroup("platform")
	if err != nil {
		t.Fatalf("ToGroup: %v", err)
	}
	for _, tc := range []struct {
		id   NoteID
		v    Visibility
		want tri.Value
	}{
		{"self-note", SelfOnly(), tri.No},
		{"group-note", group, tri.No},
		{"company-note", CompanyWide(), tri.Yes},
	} {
		n := restorable()
		n.ID = tc.id
		n.Visibility = tc.v
		if err := s.RestoreNote(n); err != nil {
			t.Fatalf("RestoreNote(%s): %v", tc.id, err)
		}
		if got := CanReadNote(n, "bob", members); got != tc.want {
			t.Errorf("bob's readability of the restored %s is %v, want %v; a restart must not change who can read a note", tc.id, got, tc.want)
		}
	}

	// And the roster is deliberately NOT consulted: a departed person's notes are archived, not
	// deleted (PRD §3.3), so a restart must not drop them.
	roster := NewRoster()
	roster.Register("alice")
	roster.Deactivate("alice")
	s.SetRoster(roster)
	departed := restorable()
	departed.ID = "departed-note"
	if err := s.RestoreNote(departed); err != nil {
		t.Errorf("a departed colleague's note could not be restored: %v; their notes are archived, not deleted, and a hub restart must not be the thing that deletes them", err)
	}
}
