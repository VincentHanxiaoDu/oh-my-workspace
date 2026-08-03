package hub

import "time"

// RestoreNote reinstates a note whose id was already minted, for a hub that is replaying its own
// durable record at start-up (Issue #103, criterion 1).
//
// WHY THIS EXISTS AND WHY IT IS NOT [Store.Publish]. Publish mints an unguessable id (see
// noteid.go). Replaying a journal through Publish would therefore give every note a NEW id on every
// restart, which breaks two things at once: a note id a person holds stops resolving after the hub
// is restarted, and Issue #103 criterion 8 — "two hubs holding the same published corpus return the
// same answers, so a reader cannot tell which they are talking to" — becomes impossible, because
// the two hubs' answers would differ in every id they mention. Restoration therefore takes the id
// as given. Minting is still the only way a note comes into existence; this is the only way one
// comes BACK into existence.
//
// IT IS NOT A BACK DOOR AROUND VISIBILITY. A restored note carries the visibility it was stored
// with, is checked against the same rules a publication is, and every subsequent read of it goes
// through [CanReadNote] exactly as before. What it deliberately does NOT check is the roster: a
// person who has left the company still has their notes (PRD §3.3, "notes outlive employment", and
// they are archived rather than deleted), so a hub restart must not silently drop them. Publishing
// a NEW note as a departed person remains refused, in Publish, where it belongs.
//
// The store is left completely unchanged on any refusal, the same rule Publish holds to.
func (s *Store) RestoreNote(n *Note) error {
	if n == nil {
		return Refusedf(ErrNoSuchNote, "a nil note cannot be restored")
	}
	if n.ID == "" {
		return Refusedf(ErrNoSuchNote, "a note with no id cannot be restored")
	}
	if n.Author == "" {
		return ErrNoAuthor
	}
	if len(n.Versions) == 0 {
		// A stored note is never version-less. Restoring one would produce a note whose Latest()
		// panics — a hub that starts and then crashes on the first read is worse than one that
		// refuses to start.
		return Refusedf(ErrNoSuchNote, "note %q has no versions", string(n.ID))
	}
	for i, v := range n.Versions {
		if v.Number != i+1 {
			return Refusedf(ErrNoSuchNote, "note %q has version %d where version %d was expected; its timeline is not readable as it stood",
				string(n.ID), v.Number, i+1)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.notes[n.ID]; taken {
		// Two notes under one id is a corpus that answers differently depending on which one won.
		return Refusedf(ErrNoSuchNote, "note %q is already held by this hub", string(n.ID))
	}
	// The SAME visibility check a publication gets. A record naming a group the hub no longer knows
	// is refused here rather than restored as an unreadable note or quietly widened to the company.
	if err := s.checkVisibilityLocked(n.Visibility); err != nil {
		return err
	}

	versions := make([]Version, len(n.Versions))
	copy(versions, n.Versions)
	restored := &Note{
		ID:         n.ID,
		Author:     n.Author,
		Title:      n.Title,
		Visibility: n.Visibility,
		Versions:   versions,
	}
	s.notes[restored.ID] = restored
	// Publication order is the order the notes were restored in, which is the order they were
	// published in — the journal is append-only. Recency answers depend on this being right.
	s.order = append(s.order, restored.ID)
	return nil
}

// NoteAt is a version as a restorer supplies it, so that a caller outside this package can build a
// timeline without the [Version] literal's field order being part of its API.
func NoteAt(number int, body string, at time.Time) Version {
	return Version{Number: number, Body: body, At: at}
}

// RestoreVersion appends a version to a note WITH THE TIME IT WAS ORIGINALLY WRITTEN, for a hub
// replaying an amendment out of its durable record.
//
// WHY [Store.Amend] IS NOT ENOUGH, AND HOW THIS WAS FOUND. Amend stamps the store's clock, which
// during a replay is the moment the hub restarted rather than the moment the person wrote. Two hubs
// replaying the same record therefore reported different recency for the same corpus — Issue #103
// criterion 8, "a reader cannot tell which hub they are talking to", broken by a clock. The test
// that caught it compares two hubs' corpus statistics; it was written before this function existed
// and it failed.
//
// The version number must be the next one. A replay that could insert a version out of order would
// produce a timeline that reads differently from the one that was written, which is PRD §3.3's
// "a claim someone acted on last month can still be read as it stood" quietly untrue.
func (s *Store) RestoreVersion(id NoteID, v Version) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	if !ok {
		return Refusedf(ErrNoSuchNote, "%q", string(id))
	}
	if want := len(n.Versions) + 1; v.Number != want {
		return Refusedf(ErrNoSuchNote, "note %q was given version %d where version %d was expected", string(id), v.Number, want)
	}
	n.Versions = append(n.Versions, v)
	return nil
}
