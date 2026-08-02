package hub

import (
	"errors"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// everyGroupMembership says yes to every group for everybody. It exists for criterion 4: a
// colleague who is a member of every group still cannot read a note narrowed to yourself.
type everyGroupMembership struct{}

func (everyGroupMembership) IsMember(GroupID, PersonID) (bool, error) { return true, nil }
func (everyGroupMembership) Knows(GroupID) (bool, error)              { return true, nil }

// unreadableMembership cannot answer. Criterion 16: that is undetermined, never a no.
type unreadableMembership struct{}

func (unreadableMembership) IsMember(GroupID, PersonID) (bool, error) {
	return false, errors.New("membership record could not be read")
}
func (unreadableMembership) Knows(GroupID) (bool, error) {
	return false, errors.New("membership record could not be read")
}

// CRITERION 2 and CRITERION 4's core, plus criterion 14's "works with no directory integration".
func TestCanRead(t *testing.T) {
	rec := NewRecord()
	rec.DefineGroup("platform", "alice", "bo")
	rec.AddPerson("carol")

	cases := []struct {
		name   string
		v      Visibility
		author PersonID
		reader PersonID
		m      Membership
		want   tri.Value
	}{
		{"company-wide lets any colleague in", CompanyWide(), "alice", "carol", rec, tri.Yes},
		{"company-wide needs no membership record at all", CompanyWide(), "alice", "carol", nil, tri.Yes},
		{"named people: a named colleague reads it", mustPeople("carol"), "alice", "carol", rec, tri.Yes},
		{"named people: an unnamed colleague does not", mustPeople("carol"), "alice", "dan", rec, tri.No},
		{"named people needs no membership record", mustPeople("carol"), "alice", "dan", nil, tri.No},
		{"group: a member reads it", mustGroup("platform"), "carol", "bo", rec, tri.Yes},
		{"group: a non-member does not", mustGroup("platform"), "carol", "dan", rec, tri.No},
		{"self: the author reads it", SelfOnly(), "alice", "alice", rec, tri.Yes},
		{"self: nobody else does", SelfOnly(), "alice", "bo", rec, tri.No},
		{"self: not even a member of every group", SelfOnly(), "alice", "bo", everyGroupMembership{}, tri.No},
		{"the author always reads their own note", mustPeople("carol"), "alice", "alice", rec, tri.Yes},
		{"an unidentified reader is undetermined, not refused", CompanyWide(), "alice", "", rec, tri.Undetermined},
		{"a group with no membership record is undetermined, not refused", mustGroup("platform"), "alice", "bo", nil, tri.Undetermined},
		{"a group the record cannot resolve is undetermined", mustGroup("platform"), "alice", "bo", unreadableMembership{}, tri.Undetermined},
		{"a group the hub does not know is undetermined, never an empty audience", mustGroup("ghosts"), "alice", "bo", rec, tri.Undetermined},
		{"an unset visibility is undetermined, not company-wide", Visibility{}, "alice", "bo", rec, tri.Undetermined},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CanRead(c.v, c.author, c.reader, c.m)
			if got != c.want {
				t.Errorf("CanRead(%s) = %v, want %v", c.v.Token(), got, c.want)
			}
		})
	}
}

// CRITERION 14, said on its own: evaluating a group narrowing consults the hub's record and nothing
// else, and works with no directory integration present.
//
// The test PROBES for a directory rather than naming one: it hands CanRead a membership record it
// built itself, with no network, no environment and no configuration, and asserts a determined
// answer both ways. If a directory lookup were ever added to this path it would have nothing to
// look up and could only fail, which would turn these determined answers undetermined.
func TestGroupResolutionUsesTheHubsOwnRecordOnly(t *testing.T) {
	rec := NewRecord()
	rec.DefineGroup("platform", "alice")
	rec.AddPerson("bo")

	if got := CanRead(mustGroup("platform"), "zoe", "alice", rec); got != tri.Yes {
		t.Errorf("member: got %v, want Yes — group evaluation must work with no directory present", got)
	}
	if got := CanRead(mustGroup("platform"), "zoe", "bo", rec); got != tri.No {
		t.Errorf("non-member: got %v, want No", got)
	}

	// CRITERION 3: "the current members", so a change to the record changes who reads it.
	if err := rec.Join("platform", "bo"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if got := CanRead(mustGroup("platform"), "zoe", "bo", rec); got != tri.Yes {
		t.Errorf("after joining: got %v, want Yes — criterion 3 is about CURRENT members", got)
	}
	if err := rec.Leave("platform", "alice"); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if got := CanRead(mustGroup("platform"), "zoe", "alice", rec); got != tri.No {
		t.Errorf("after leaving: got %v, want No", got)
	}
}

// A group the hub never heard of is not a group with no members. Answering false for the first
// would make a mistyped narrowing quietly readable by nobody.
func TestUnknownGroupIsRefused(t *testing.T) {
	rec := NewRecord()
	rec.DefineGroup("platform")

	if _, err := rec.IsMember("ghosts", "alice"); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("IsMember on an unknown group = %v, want ErrUnknownGroup", err)
	}
	// An EMPTY known group is a different thing and answers cleanly.
	in, err := rec.IsMember("platform", "alice")
	if err != nil {
		t.Fatalf("IsMember on a known empty group errored: %v", err)
	}
	if in {
		t.Error("alice is in an empty group")
	}
}

func TestCanReadNoteOnNilNoteIsUndetermined(t *testing.T) {
	if got := CanReadNote(nil, "alice", nil); got != tri.Undetermined {
		t.Errorf("CanReadNote(nil) = %v, want Undetermined", got)
	}
}
