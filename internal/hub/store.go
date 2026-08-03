package hub

import (
	"sort"
	"sync"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// NoteID identifies a published note.
type NoteID string

// Version is one point on a note's addressable timeline (PRD §3.3).
//
// IT DELIBERATELY CARRIES NO VISIBILITY. Criterion 6: "a colleague who could read version N of a
// note that is then narrowed to exclude them can no longer read the note, nor its earlier versions
// through the addressable timeline. The timeline is not a bypass around visibility." A per-version
// visibility field is exactly how that bypass gets built — someone stamps the note's visibility
// onto each version at write time for auditing, and history stays readable to everyone who was ever
// included. Visibility belongs to the note. Issue #11 owns versions and may add authorship, message
// and diff fields here; it must not add a visibility one.
type Version struct {
	// Number is 1 for the first published body and increments on each amendment.
	Number int
	// Body is the note as it stood.
	Body string
	// At is when this version was written.
	At time.Time
}

// Note is a published unit of knowledge (PRD §3.3).
//
// The struct is exported with exported fields because Issues #11, #13, #14, #15 and #22 all read
// notes and none of them should need an accessor per field. Visibility is changed through
// [Store.SetVisibility] rather than by assignment, because narrowing to an unknown group must be
// refused there just as it is at publication.
type Note struct {
	ID     NoteID
	Author PersonID
	Title  string
	// Visibility governs the note and all of its versions. Never [KindUnset] for a stored note.
	Visibility Visibility
	// Versions is oldest-first and never empty for a stored note.
	Versions []Version
}

// Latest returns the most recent version.
func (n *Note) Latest() Version { return n.Versions[len(n.Versions)-1] }

// Version returns version number num.
func (n *Note) Version(num int) (Version, error) {
	if num < 1 || num > len(n.Versions) {
		return Version{}, Refusedf(ErrNoSuchNote, "note %q has no version %d", string(n.ID), num)
	}
	return n.Versions[num-1], nil
}

// Store is the hub's note store: published notes, their versions, and the membership record
// visibility is evaluated against.
//
// It is in-memory and safe for concurrent use. Durability is not this Issue's — no Issue has yet
// chosen the hub's persistence — and the interface here is the one a persistent implementation will
// have to satisfy. What IS this Issue's, and what a persistent version must keep, is that every
// read goes through [CanRead] and that a refused publication changes nothing.
type Store struct {
	mu      sync.RWMutex
	members *Record
	notes   map[NoteID]*Note
	order   []NoteID
	now     func() time.Time
	// roster is Issue #15's record of who is still with the company, attached by [Store.SetRoster]
	// (Issue #22). It gates the WRITE paths only — publishing and amending as somebody who has
	// left — and is NEVER consulted when deciding who may READ a note. Nil means no roster is
	// attached and every author is treated as active, which is the state of a hub nobody has told
	// about a departure.
	roster *Roster
}

// NewStore returns a store over the given membership record. A nil record means the hub knows no
// groups; group narrowings are then refused at publication (there is no such group) rather than
// evaluated against nothing.
func NewStore(members *Record) *Store {
	if members == nil {
		members = NewRecord()
	}
	return &Store{members: members, notes: map[NoteID]*Note{}, now: time.Now}
}

// SetClock replaces the store's clock. For tests; a deterministic timeline is worth more than a
// real one when what is under test is ordering.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Members returns the hub's membership record, which is also the [Membership] that visibility is
// evaluated against. Downstream Issues pass this to [CanRead].
func (s *Store) Members() *Record { return s.members }

// Publication is a request to publish a note.
//
// Visibility is a VALUE, and its zero value means "no choice expressed". Criterion 1: that means
// company-wide, decided here, once. A caller does not have to remember to fill it in and a caller
// that forgets does not create a note with no audience.
type Publication struct {
	Author     PersonID
	Title      string
	Body       string
	Visibility Visibility
}

// Publish stores a note and returns it.
//
// REFUSAL IS TOTAL. If the visibility cannot be honoured — an unknown group (criterion 15), an
// audience of nobody — nothing is stored: not the note with a narrower audience, not the note
// company-wide as a fallback, not the note with an empty audience. The error carries a code a
// caller can branch on without reading prose. The client-side consequence of a refusal — PRD §3.11,
// "a note that did not arrive is still in the outbox" — belongs to the outbox, which is Issue #9;
// this side's contract is that a refused publication leaves the hub exactly as it was, and a test
// asserts the store is unchanged.
func (s *Store) Publish(p Publication) (*Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.Author == "" {
		return nil, ErrNoAuthor
	}
	// ISSUE #22 CRITERION 16. Nothing publishes a new note as a person who has left. Checked here,
	// in the one function that stores a note, rather than in a wrapper a caller has to remember —
	// and checked BEFORE the id counter is touched, so a refusal stores nothing.
	if err := s.checkAuthorWritableLocked(p.Author); err != nil {
		return nil, err
	}

	v := p.Visibility
	if v.IsUnset() {
		// CRITERION 1, and the only place it is decided.
		v = Default()
	}
	if err := s.checkVisibilityLocked(v); err != nil {
		return nil, err
	}

	// UNGUESSABLE, NOT SEQUENTIAL — Issue #15's ruling, binding #10, #12, #14 and #15. The old
	// `note-%d` counter combined with criterion 12's required "refused" vs "no such note"
	// distinction into an enumeration oracle, and it also let a reader locate a note hidden from
	// them by counting from one they hold. See noteid.go for the ruling and the two decisions it
	// left open. Minted BEFORE anything is stored, so a mint that fails publishes nothing.
	id, err := s.mintUnusedNoteIDLocked()
	if err != nil {
		return nil, err
	}
	n := &Note{
		ID:         id,
		Author:     p.Author,
		Title:      p.Title,
		Visibility: v,
		Versions:   []Version{{Number: 1, Body: p.Body, At: s.now()}},
	}
	s.notes[id] = n
	s.order = append(s.order, id)
	return n, nil
}

// checkVisibilityLocked refuses a visibility the hub cannot honour. Called by both Publish and
// SetVisibility so that narrowing an existing note to a group the hub does not know is refused for
// the same reason and with the same code as publishing to one.
func (s *Store) checkVisibilityLocked(v Visibility) error {
	switch v.kind {
	case KindGroup:
		known, err := s.members.Knows(v.group)
		if err != nil {
			return Refusedf(ErrUndetermined, "the hub's membership record could not be read for group %q", string(v.group))
		}
		if !known {
			return Refusedf(ErrUnknownGroup, "%q", string(v.group))
		}
	case KindPeople:
		if len(v.people) == 0 {
			return ErrEmptyAudience
		}
	case KindUnset:
		// Unreachable from Publish, which applies the default first. Reachable from SetVisibility,
		// where "unset" is not a thing a person can ask for: changing a note's visibility to
		// nothing is not one of the four choices.
		return Refusedf(ErrUnknownVisibility, "a note's visibility cannot be set to no choice")
	}
	return nil
}

// Amend adds a version to a note. Issue #11 owns versions; this exists so that criterion 6 — the
// timeline is not a bypass — can be driven now rather than asserted structurally.
func (s *Store) Amend(id NoteID, body string) (*Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	if !ok {
		return nil, Refusedf(ErrNoSuchNote, "%q", string(id))
	}
	// ISSUE #22 CRITERION 16, the other half: the archive is readable, not writable. No version is
	// added to a departed person's note — not by them, and not by anything acting as them.
	if err := s.checkAuthorWritableLocked(n.Author); err != nil {
		return nil, err
	}
	n.Versions = append(n.Versions, Version{Number: len(n.Versions) + 1, Body: body, At: s.now()})
	return n, nil
}

// SetVisibility changes a note's visibility, which changes it for every version of the note
// (criterion 6).
//
// Only the author may change it. A non-author's attempt is refused with ErrRefused, and a
// non-existent note with ErrNoSuchNote — the same distinguishable pair as reading.
func (s *Store) SetVisibility(id NoteID, by PersonID, v Visibility) (*Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	if !ok {
		return nil, Refusedf(ErrNoSuchNote, "%q", string(id))
	}
	if n.Author != by {
		return nil, Refusedf(ErrRefused, "only the author may change a note's visibility")
	}
	if err := s.checkVisibilityLocked(v); err != nil {
		// UNCHANGED ON REFUSAL, the same rule as Publish. A rejected narrowing must not leave the
		// note wider than the author asked for and believing it is narrower.
		return nil, err
	}
	n.Visibility = v
	return n, nil
}

// VisibilityOf reports a note's visibility back.
//
// Criterion 1 in its "reading it back" half: the answer for a note published with no choice is
// company-wide, and never an empty value the caller has to interpret.
func (s *Store) VisibilityOf(id NoteID) (Visibility, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notes[id]
	if !ok {
		return Visibility{}, Refusedf(ErrNoSuchNote, "%q", string(id))
	}
	return n.Visibility, nil
}

// Read returns a note's latest version if the reader may read it.
//
// THE THREE OUTCOMES ARE THREE OUTCOMES:
//
//	(note, nil)                     readable
//	(nil, ErrRefused)               exists, not readable — distinguishable from the next line
//	(nil, ErrNoSuchNote)            no such note (criterion 12)
//	(nil, ErrUndetermined)          could not be worked out — never rendered as either of the above
func (s *Store) Read(id NoteID, reader PersonID) (*Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notes[id]
	if !ok {
		return nil, Refusedf(ErrNoSuchNote, "%q", string(id))
	}
	switch CanReadNote(n, reader, s.members) {
	case tri.Yes:
		return n, nil
	case tri.No:
		return nil, Refusedf(ErrRefused, "note %q", string(id))
	default:
		return nil, Refusedf(ErrUndetermined, "note %q", string(id))
	}
}

// ReadVersion returns one point on the timeline, subject to the note's CURRENT visibility.
//
// This is criterion 6's teeth: it consults the note, not the version, so narrowing a note takes the
// history with it.
func (s *Store) ReadVersion(id NoteID, num int, reader PersonID) (Version, error) {
	n, err := s.Read(id, reader)
	if err != nil {
		return Version{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return n.Version(num)
}

// ListReadable returns the notes the reader may read, in publication order, together with the ids
// whose visibility could not be determined.
//
// THE UNDETERMINED IDS ARE RETURNED, NOT DROPPED. This is the function Issue #15's search will
// filter with before it ranks anything (PRD §3.5), and #13's corpus statistics will count with. A
// list that silently omitted the notes it could not evaluate would report a smaller corpus with
// complete confidence — "could not determine" quietly becoming "determined to be nothing", one
// package over from where tri makes it hard. Callers must say something about the second return
// value; they must not add it to the first.
func (s *Store) ListReadable(reader PersonID) (readable []*Note, undetermined []NoteID) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.order {
		n := s.notes[id]
		switch CanReadNote(n, reader, s.members) {
		case tri.Yes:
			readable = append(readable, n)
		case tri.No:
			// Not readable, and not mentioned. §3.5: ranking never surfaces the existence of
			// something the searcher cannot read.
		default:
			undetermined = append(undetermined, id)
		}
	}
	return readable, undetermined
}

// Count returns how many notes the hub holds. Used by tests asserting that a refused publication
// stored nothing.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.notes)
}

// IDs returns every note id the hub holds, ordered. This is the hub's own view and is NOT filtered
// by visibility — it exists for the store's operator-side accounting, and callers serving a person
// must use [Store.ListReadable].
func (s *Store) IDs() []NoteID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]NoteID, len(s.order))
	copy(out, s.order)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
