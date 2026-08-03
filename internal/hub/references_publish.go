package hub

// Publication-time checking of a note's references (Issue #14, criterion 4).
//
// WHY THIS IS A WRAPPER AND NOT AN EDIT TO Store.Publish. Issue #12's Publish is merged, reviewed
// work, and Issue #11 (versions) is in review against the same file on another branch; editing it
// would put a conflict between two branches for a change that does not need to be there. More to
// the point, the check needs to be somewhere a caller can SEE: a Publish that silently grew a
// reference check would be a behaviour change invisible at every call site, and every existing
// caller — including #12's own tests — would start exercising a rule they never asked for.
//
// The consequence is stated rather than hidden: calling [Store.Publish] directly does NOT check
// references, so the hub's publication path must call this instead. There is a test that names
// every caller of Publish in this repository, so a new one that skips the check is a red build and
// not a discovery. When a real publication endpoint exists, this is the function it calls.

var (
	// ErrReferenceNotVisibleToAuthor — criterion 4. A note whose body references something its
	// author cannot see is refused AT PUBLICATION: not narrowed, not stripped, not downgraded, and
	// not published with the reference quietly turned into prose.
	//
	// It says WHICH reference (PRD §3.11, "refused says why"), and it can, because the author is
	// being told about something the author cannot see — which discloses nothing to them that
	// their own draft did not already say.
	ErrReferenceNotVisibleToAuthor = &Error{
		Code: "reference-not-visible-to-author",
		Msg:  "refused: this note references something you cannot see, and it has not been published",
	}

	// ErrReferenceUndetermined — the author's access to a referenced target could not be worked out.
	//
	// ITS OWN CODE, AND THAT IS THE POINT. Publishing anyway would treat undetermined as permitted;
	// refusing with ErrReferenceNotVisibleToAuthor would tell the author we established they may
	// not see it, which we did not. So the publication does not happen AND the reason is the third
	// answer, with a code a caller can branch on.
	ErrReferenceUndetermined = &Error{
		Code: "reference-undetermined",
		Msg:  "whether you can see everything this note references could not be determined, so it has not been published",
	}
)

// referenceErrors is this Issue's additions, for the test that asserts they collide with nothing in
// Issue #12's set. errors.go is merged work and this Issue does not edit it.
var referenceErrors = []*Error{ErrReferenceNotVisibleToAuthor, ErrReferenceUndetermined}

// CheckReferences decides whether author may publish this body.
//
// CRITERION 4, AND CRITERION 10 IS THE REASON IT IS THE AUTHOR AND NOT THE HUB WHO IS ASKED. If the
// hub checked with its own eyes — it can read everything published to it (PRD §2.4) — then every
// reference would pass, and a note could carry an edge into material its author never had. A
// reference is never a widening: what the author could read is what the author may point at.
//
// A dangling reference is NOT refused here. A target that does not exist discloses nothing by being
// pointed at, and criterion 11 exists precisely because targets go away after publication — a
// publication path that refused them would make that state unreachable for the wrong reason.
func CheckReferences(s *Store, author PersonID, body string) error {
	if author == "" {
		return ErrNoAuthor
	}
	for _, r := range TargetsOf(body) {
		switch ResolveReference(s, r, author) {
		case StateHidden:
			return Refusedf(ErrReferenceNotVisibleToAuthor, "reference to %s %q", string(r.Kind), r.Target)
		case StateUndetermined:
			return Refusedf(ErrReferenceUndetermined, "reference to %s %q", string(r.Kind), r.Target)
		}
	}
	return nil
}

// PublishWithReferences is [Store.Publish] with criterion 4 applied first.
//
// REFUSAL IS TOTAL, the same rule Publish itself keeps: the check runs before anything is stored,
// so a refused publication leaves the hub exactly as it was — and a test asserts the store's count
// rather than only the returned error, because a refusal that has already written is a refusal in
// name only.
func PublishWithReferences(s *Store, p Publication) (*Note, error) {
	if s == nil {
		return nil, Refusedf(ErrHubUnreachable, "a note cannot be published to a hub that could not be reached")
	}
	if err := CheckReferences(s, p.Author, p.Body); err != nil {
		return nil, err
	}
	return s.Publish(p)
}

// AmendWithReferences is [Store.Amend] with the same check.
//
// It takes the author explicitly and verifies it, because Amend does not: an amendment that
// introduced a reference would otherwise be checked against whoever happened to be passed in.
// Issue #11 owns versions and will own authorship of an amendment properly; until it does, this
// refuses an amendment by anyone but the author rather than assuming.
func AmendWithReferences(s *Store, id NoteID, author PersonID, body string) (*Note, error) {
	if s == nil {
		return nil, Refusedf(ErrHubUnreachable, "note %q", string(id))
	}
	n, err := s.Read(id, author)
	if err != nil {
		return nil, err
	}
	if n.Author != author {
		return nil, Refusedf(ErrRefused, "only the author may amend a note")
	}
	if err := CheckReferences(s, author, body); err != nil {
		return nil, err
	}
	return s.Amend(id, body)
}
