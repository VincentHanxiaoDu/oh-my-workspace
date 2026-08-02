// Issue #11 — the store's half of the version surface.
//
// These are methods on Issue #12's [Store], added in a NEW FILE rather than by editing store.go.
// Go lets a type's methods live anywhere in its package, and two Issues that each add a file to a
// package never conflict, whereas two Issues that each add a method to one file always do.
//
// EVERY METHOD HERE ROUTES THROUGH [Store.Read]. That is not a style choice: Issue #12's
// [Store.ReadVersion] routes through Read deliberately so the visibility gate cannot be skipped,
// and these methods are new doors into the same room. A method that reached into s.notes and
// evaluated readability itself would be a second gate, and a second gate is one that stops
// agreeing with the first the next time [CanRead] changes.
package hub

// Timeline implements [VersionSource]. It returns every version of the note, oldest first, IF the
// reader may read the note.
//
// The note's CURRENT visibility governs, because that is the only visibility there is (see
// [CanReadNote]). A reader narrowed out of a note today cannot enumerate the versions written while
// they were included — the timeline is not a bypass.
func (s *Store) Timeline(id NoteID, reader PersonID) ([]Version, error) {
	n, err := s.Read(id, reader)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// A COPY. The caller must not be able to append to, reorder or truncate the store's own slice —
	// PRD §5.4 says nothing expires, and a history a caller can shorten by accident is a retention
	// mechanism nobody meant to build.
	out := make([]Version, len(n.Versions))
	copy(out, n.Versions)
	return out, nil
}

// VersionAt implements [VersionSource]: one point on the timeline, subject to the note's current
// visibility.
//
// CRITERION 9 IS THE RETURN TYPE. An out-of-range version answers [ErrNoSuchVersion], which is a
// different code from [ErrNoSuchNote] and a different code from success — so "there is no version
// 7" and "version 7 is empty" are told apart by a caller that never looks at the body. Issue #12's
// [Note.Version] answers ErrNoSuchNote for this case; that method is left alone and its test with
// it, and this door answers the more precise code.
func (s *Store) VersionAt(id NoteID, num int, reader PersonID) (Version, error) {
	n, err := s.Read(id, reader)
	if err != nil {
		return Version{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if num < 1 || num > len(n.Versions) {
		return Version{}, Refusedf(ErrNoSuchVersion, "note %q has no version %d; it has %d",
			string(id), num, len(n.Versions))
	}
	return n.Versions[num-1], nil
}

// AuthorOf names a note's author, for the archived marking of criterion 7. It is gated like
// everything else: a reader who may not read the note does not learn who wrote it.
func (s *Store) AuthorOf(id NoteID, reader PersonID) (PersonID, error) {
	n, err := s.Read(id, reader)
	if err != nil {
		return "", err
	}
	return n.Author, nil
}
