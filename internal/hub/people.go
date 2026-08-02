package hub

import (
	"sync"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Roster records which colleagues have been deactivated (PRD §5.4: "deactivated people's notes are
// archived (§3.3)", and §5.4's headline, "nothing expires").
//
// WHY THIS IS A SEPARATE TYPE FROM [Record] AND NOT A FLAG ON IT. Issue #15 criterion 13 is a
// NEGATIVE requirement: a deactivated author's notes "remain findable exactly to the extent their
// visibility allows". The way that requirement gets broken is by search growing a branch on
// activity — dropping a departed colleague's notes, or, worse, widening them because "they are
// gone anyway". Keeping activity out of [Record] keeps it out of [CanRead]'s reach entirely, so
// there is no field for such a branch to read. Search consults the roster for exactly ONE thing:
// telling a person that the colleague they scoped to has left, so that a thin result set is not
// mistaken for a broken search. It never consults it to decide what is readable.
//
// Issue #22 owns departed colleagues' notes properly and will build on this; the surface here is
// the minimum criterion 13 can be driven against.
//
// The zero Roster is not usable — use [NewRoster] — because a nil map would make Register panic,
// and a roster that silently swallowed registrations would answer Undetermined for everybody while
// looking configured.
type Roster struct {
	mu       sync.Mutex
	known    map[PersonID]bool // registered at all
	inactive map[PersonID]bool
}

// NewRoster returns a roster that has heard of nobody.
func NewRoster() *Roster {
	return &Roster{known: map[PersonID]bool{}, inactive: map[PersonID]bool{}}
}

// Register records a colleague as active.
func (r *Roster) Register(p PersonID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.known[p] = true
	delete(r.inactive, p)
}

// Deactivate records that a colleague has left. Their notes are NOT touched: §5.4 says archived,
// not deleted, and this type has no access to the store to delete anything with even if it wanted.
func (r *Roster) Deactivate(p PersonID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.known[p] = true
	r.inactive[p] = true
}

// Active reports whether a colleague is still with the company, three-valued.
//
// A nil roster, or a person this roster has never heard of, is Undetermined — NOT active and not
// departed. Answering Yes for an unknown person would let a missing registration read as a
// confident "still here"; answering No would announce departures the roster never recorded.
func (r *Roster) Active(p PersonID) tri.Value {
	if r == nil {
		return tri.Undetermined
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.known[p] {
		return tri.Undetermined
	}
	if r.inactive[p] {
		return tri.No
	}
	return tri.Yes
}

// Dissolve removes a group from the hub's record.
//
// IT IS NOT A DELETE OF ANYTHING ELSE. Notes narrowed to a dissolved group are not deleted, not
// widened and not narrowed: they become UNDETERMINED, because [CanRead] asks [Record.IsMember],
// which no longer has a record to answer from. That is the honest state and it is exactly the
// state Issue #15 needs to be able to reach in order to drive PRD §4.3 through search — a real
// note whose readability genuinely cannot be worked out, produced by a real sequence of events
// rather than by a mock.
func (r *Record) Dissolve(g GroupID) {
	delete(r.groups, g)
}

// KnowsPerson reports whether the hub has a record of this colleague at all.
//
// It exists so that search can tell "you named somebody who does not exist" from "that person has
// written nothing you can read" (Issue #15 criterion 4). Like [Record.Knows] for groups it returns
// (bool, error) so that a record which cannot be read becomes Undetermined rather than "no such
// person" — this record is in memory and cannot fail, but the [Directory] interface search depends
// on is written for one that can.
func (r *Record) KnowsPerson(p PersonID) (bool, error) {
	return r.people[p], nil
}
