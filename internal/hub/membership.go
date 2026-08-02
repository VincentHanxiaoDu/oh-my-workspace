package hub

import "sort"

// Membership is the hub's own record of who is in which group.
//
// PRD §5.3 rules it: "The hub owns group membership. No directory integration needed for the first
// company; mirroring can be added later as a sync source." So this interface is deliberately narrow
// enough that no directory client could hide behind it usefully — it answers one question about one
// pair, synchronously, with no context and no network shape. Criterion 14 is that evaluating a
// group narrowing performs NO directory/LDAP/SSO lookup and works with no directory integration
// present; the way to make that true is to give the code no way to do otherwise.
//
// IsMember returns (bool, error). The error is not decoration: an unresolvable membership makes the
// whole visibility question UNDETERMINED, never a "no" (criterion 16). [CanRead] converts it with
// tri.FromError, which is the only sanctioned conversion.
type Membership interface {
	IsMember(g GroupID, p PersonID) (bool, error)
	// Knows reports whether the hub has a membership record for this group at all. A group the hub
	// does not know is refused at publication (criterion 15) rather than published to nobody.
	Knows(g GroupID) (bool, error)
}

// Record is the hub's in-memory membership record: the whole of §5.3's "defined in the product".
//
// It is not a cache of anything. There is no upstream. Adding a sync source later means writing to
// this record from a syncer, not replacing the evaluation path — which is what keeps criterion 14
// true after the feature §5.3 leaves open ("mirroring can be added later") is built.
//
// The zero Record is usable and knows no groups.
type Record struct {
	groups map[GroupID]map[PersonID]bool
	people map[PersonID]bool
}

// NewRecord returns an empty membership record.
func NewRecord() *Record { return &Record{} }

// AddPerson registers a colleague. A person the hub does not know is still a valid reader argument
// to [CanRead] — evaluation does not depend on registration — but the hub's own listings use this.
func (r *Record) AddPerson(p PersonID) {
	if r.people == nil {
		r.people = map[PersonID]bool{}
	}
	r.people[p] = true
}

// People returns the registered colleagues, ordered.
func (r *Record) People() []PersonID {
	out := make([]PersonID, 0, len(r.people))
	for p := range r.people {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// DefineGroup creates a group with the given members. Defining a group with no members is allowed —
// an empty group is a real state of a company's record, and it is different from a group the hub
// has never heard of. The first is a group nobody is in; the second is refused at publication.
func (r *Record) DefineGroup(g GroupID, members ...PersonID) {
	if r.groups == nil {
		r.groups = map[GroupID]map[PersonID]bool{}
	}
	set := map[PersonID]bool{}
	for _, m := range members {
		set[m] = true
		r.AddPerson(m)
	}
	r.groups[g] = set
}

// Join adds a person to an existing group. It does not create the group: criterion 3 is about
// "the current members of that group per the hub's membership record", and a typo that conjures a
// group would make a narrowing to a misspelled name succeed against an audience of one.
func (r *Record) Join(g GroupID, p PersonID) error {
	set, ok := r.groups[g]
	if !ok {
		return Refusedf(ErrUnknownGroup, "%q", string(g))
	}
	set[p] = true
	r.AddPerson(p)
	return nil
}

// Leave removes a person from a group.
func (r *Record) Leave(g GroupID, p PersonID) error {
	set, ok := r.groups[g]
	if !ok {
		return Refusedf(ErrUnknownGroup, "%q", string(g))
	}
	delete(set, p)
	return nil
}

// IsMember implements [Membership] against the hub's own record and nothing else.
//
// A group the hub does not know is an ERROR here, not a false. "This group has no members" and
// "there is no such group" are different facts, and answering false for the second would make a
// narrowing to a mistyped group silently readable by nobody — the empty audience criterion 15
// forbids.
func (r *Record) IsMember(g GroupID, p PersonID) (bool, error) {
	set, ok := r.groups[g]
	if !ok {
		return false, Refusedf(ErrUnknownGroup, "%q", string(g))
	}
	return set[p], nil
}

// Knows implements [Membership].
func (r *Record) Knows(g GroupID) (bool, error) {
	_, ok := r.groups[g]
	return ok, nil
}

// Members returns a group's current members, ordered.
func (r *Record) Members(g GroupID) ([]PersonID, error) {
	set, ok := r.groups[g]
	if !ok {
		return nil, Refusedf(ErrUnknownGroup, "%q", string(g))
	}
	out := make([]PersonID, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// Groups returns the groups the hub knows, ordered.
func (r *Record) Groups() []GroupID {
	out := make([]GroupID, 0, len(r.groups))
	for g := range r.groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
