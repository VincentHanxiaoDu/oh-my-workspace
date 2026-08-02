package hub

import (
	"reflect"
	"strings"
	"testing"
)

// corpus builds a hub with two colleagues who see different things.
//
//	alice   the author; in group platform
//	bo      a colleague NOT in group platform
//
// secret is a note narrowed to platform: alice may read it, bo may not, and bo must never learn
// that it exists.
type corpus struct {
	store  *Store
	rec    *Record
	secret NoteID
}

func newCorpus(t *testing.T) *corpus {
	t.Helper()
	rec := NewRecord()
	rec.DefineGroup("platform", "alice")
	rec.AddPerson("bo")
	s := NewStore(rec)
	group, err := ToGroup("platform")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.Publish(Publication{Author: "alice", Title: "the auth migration", Body: "how it went", Visibility: group})
	if err != nil {
		t.Fatalf("publishing the restricted note: %v", err)
	}
	return &corpus{store: s, rec: rec, secret: secret.ID}
}

// publish is a helper that fails the test rather than returning an error nobody reads.
func (c *corpus) publish(t *testing.T, author PersonID, title, body string) NoteID {
	t.Helper()
	n, err := PublishWithReferences(c.store, Publication{Author: author, Title: title, Body: body})
	if err != nil {
		t.Fatalf("publishing %q: %v", title, err)
	}
	return n.ID
}

// CRITERION 7, DRIVEN NEGATIVELY AND THE POINT OF THE WHOLE ISSUE.
//
// alice and bo retrieve the SAME company-wide note, which references a note only alice may read.
// bo's output must not differ, in ANY way, from the output for a note that never referenced
// anything restricted: no title, no identifier, no slug, no placeholder, no "restricted" marker,
// no per-reference error, and no count that differs from bo's own visible total.
func TestAReferenceToAnUnreadableNoteIsInvisible(t *testing.T) {
	c := newCorpus(t)
	withRef := c.publish(t, "alice", "the rewrite",
		"we rewrote login. the background is in [[note:"+string(c.secret)+"]] and in the wiki.")
	// The control: the same prose, written by someone who referenced nothing restricted.
	control := c.publish(t, "alice", "the rewrite, again",
		"we rewrote login. the background is in and in the wiki.")

	bosView, err := OutboundReferences(c.store, withRef, 0, "bo")
	if err != nil {
		t.Fatalf("bo cannot read the referencing note at all: %v", err)
	}
	bosControl, err := OutboundReferences(c.store, control, 0, "bo")
	if err != nil {
		t.Fatal(err)
	}

	if bosView.Count() != bosControl.Count() {
		t.Errorf("bo sees %d references on the note that references a restricted note and %d on the one\n"+
			"that references nothing; the count itself discloses that something is there",
			bosView.Count(), bosControl.Count())
	}
	if bosView.Count() != 0 {
		t.Errorf("bo's listing contains %d references, want none: %+v", bosView.Count(), bosView.Refs)
	}
	if bosView.Body != bosControl.Body {
		t.Errorf("bo's rendering of the referencing note is\n  %q\nand of the control note is\n  %q\n"+
			"they must be indistinguishable", bosView.Body, bosControl.Body)
	}
	for _, leak := range []string{string(c.secret), "the auth migration", "restricted", "unresolved", "undetermined", "…", "..."} {
		if strings.Contains(bosView.Body, leak) {
			t.Errorf("bo's rendering %q contains %q", bosView.Body, leak)
		}
	}

	// And alice, who may read it, sees it — otherwise this test would pass against an
	// implementation that showed nobody anything.
	alices, err := OutboundReferences(c.store, withRef, 0, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if alices.Count() != 1 || alices.Refs[0].State != StateResolved {
		t.Fatalf("alice sees %+v, want one resolved reference", alices.Refs)
	}
	if !strings.Contains(alices.Body, string(c.secret)) {
		t.Errorf("alice's rendering %q does not carry the reference she is permitted to follow", alices.Body)
	}
}

// CRITERION 12 AND 17: unresolved and invisible are different renderings of different facts, and a
// dangling reference does not abort the listing.
//
// COMPARED PAIRWISE. The three views a reader can have of a reference are compared against each
// other, not against literals.
func TestUnresolvedInvisibleAndResolvedAreThreeDifferentThings(t *testing.T) {
	c := newCorpus(t)
	target := c.publish(t, "alice", "a note bo may read", "anyone can read this")
	body := "resolved [[note:" + string(target) + "]], gone [[note:note-404]], hidden [[note:" + string(c.secret) + "]]."
	n := c.publish(t, "alice", "three at once", body)

	bos, err := OutboundReferences(c.store, n, 0, "bo")
	if err != nil {
		t.Fatal(err)
	}
	// The dangling reference did NOT take the listing with it: the readable one is still there.
	if bos.Count() != 2 {
		t.Fatalf("bo sees %d references, want 2 (the resolved one and the unresolved one): %+v", bos.Count(), bos.Refs)
	}
	states := map[string]RefState{}
	for _, v := range bos.Refs {
		states[v.Ref.Target] = v.State
	}
	if states[string(target)] != StateResolved {
		t.Errorf("the readable target is %v, want resolved", states[string(target)])
	}
	if states["note-404"] != StateUnresolved {
		t.Errorf("the missing target is %v, want unresolved", states["note-404"])
	}
	if _, present := states[string(c.secret)]; present {
		t.Errorf("the restricted target appears in bo's listing as %v", states[string(c.secret)])
	}

	// Pairwise, over ONE target. Comparing the states of three DIFFERENT targets would pass
	// because their names differ, which is not the property under test — it is how a mutation
	// making the hidden state render exactly like the unresolved one first went unnoticed.
	one := Reference{Kind: RefNote, Target: string(target)}
	renderings := map[string]string{
		"resolved":   RenderReference(one, StateResolved),
		"unresolved": RenderReference(one, StateUnresolved),
		"hidden":     RenderReference(one, StateHidden),
	}
	names := []string{"resolved", "unresolved", "hidden"}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if renderings[names[i]] == renderings[names[j]] {
				t.Errorf("%s and %s both render as %q; rendering an invisible target as unresolved is a\n"+
					"defect, because it discloses existence", names[i], names[j], renderings[names[i]])
			}
		}
	}
}

// Criterion 11's other half, at the store level: the target existed and went away. The reference is
// still there, marked, and the surrounding references still render.
func TestAReferenceToANoteThatIsGoneIsShownAsUnresolved(t *testing.T) {
	c := newCorpus(t)
	n := c.publish(t, "alice", "with a dangling reference",
		"first [[note:note-404]] then [[person:bo]]")
	l, err := OutboundReferences(c.store, n, 0, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if l.Count() != 2 {
		t.Fatalf("got %d references, want 2 — a dangling reference must not break the listing: %+v", l.Count(), l.Refs)
	}
	if l.Refs[0].State != StateUnresolved {
		t.Errorf("the dangling reference is %v, want unresolved", l.Refs[0].State)
	}
	if l.Refs[1].State != StateResolved {
		t.Errorf("the reference AFTER the dangling one is %v, want resolved — one broken edge must not\n"+
			"take the others with it", l.Refs[1].State)
	}
	if !strings.Contains(l.Body, "unresolved") {
		t.Errorf("the rendered body %q does not mark the unresolved reference", l.Body)
	}
}

// CRITERION 14: where whether a reference resolves could not be determined, it is rendered as
// undetermined — never as "does not exist" and never omitted.
//
// Two paths reach it, and both are driven. A hub that could not be reached is the nil store. A
// group that was DISSOLVED after a note was narrowed to it makes that note's readability
// unresolvable, which is the third answer and not a "no".
func TestUndeterminedIsItsOwnAnswer(t *testing.T) {
	c := newCorpus(t)
	r := Reference{Kind: RefNote, Target: string(c.secret)}

	if st := ResolveReference(nil, r, "alice"); st != StateUndetermined {
		t.Errorf("with no hub to reach, the reference is %v; a hub that cannot be reached is not a\n"+
			"reference that does not exist", st)
	}

	// The group is dissolved. The note narrowed to it is now unreadable-in-the-third-sense: its
	// membership cannot be resolved, so CanRead answers Undetermined.
	delete(c.rec.groups, "platform")
	if st := ResolveReference(c.store, r, "bo"); st != StateUndetermined {
		t.Errorf("with the group dissolved the reference is %v, want undetermined — an unresolvable\n"+
			"group is not a determined refusal and not a missing note", st)
	}
	if st := ResolveReference(c.store, Reference{Kind: RefGroup, Target: "platform"}, "bo"); st != StateUnresolved {
		t.Errorf("a reference to the dissolved group itself is %v, want unresolved", st)
	}
}

// An undetermined reference is COUNTED AND SHOWN, not dropped — dropping it would be treating "I
// could not check" as "there is nothing there".
func TestAnUndeterminedReferenceIsNeitherHiddenNorResolved(t *testing.T) {
	c := newCorpus(t)
	n := c.publish(t, "alice", "points at it", "see [[note:"+string(c.secret)+"]]")
	delete(c.rec.groups, "platform")

	l, err := OutboundReferences(c.store, n, 0, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if l.Count() != 1 {
		t.Fatalf("got %d references, want the undetermined one to be present: %+v", l.Count(), l.Refs)
	}
	if l.Undetermined() != 1 {
		t.Errorf("Undetermined() is %d, want 1 — a caller must be able to say the answer is partial", l.Undetermined())
	}
	if strings.Contains(l.Body, string(c.secret)) {
		t.Errorf("the undetermined reference disclosed its target in %q", l.Body)
	}
}

// CRITERION 3: references belong to a version. Retrieving v1 yields v1's references, not v3's.
func TestReferencesAreThoseOfTheVersionAsked(t *testing.T) {
	c := newCorpus(t)
	a := c.publish(t, "alice", "a", "a")
	b := c.publish(t, "alice", "b", "b")
	n := c.publish(t, "alice", "history", "v1 references [[note:"+string(a)+"]]")
	if _, err := AmendWithReferences(c.store, n, "alice", "v2 references [[note:"+string(b)+"]]"); err != nil {
		t.Fatal(err)
	}

	v1, err := OutboundReferences(c.store, n, 1, "bo")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := OutboundReferences(c.store, n, 2, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if len(v1.Refs) != 1 || v1.Refs[0].Ref.Target != string(a) {
		t.Errorf("v1's references are %+v, want exactly the one added in v1", v1.Refs)
	}
	if len(v2.Refs) != 1 || v2.Refs[0].Ref.Target != string(b) {
		t.Errorf("v2's references are %+v, want exactly the one added in v2", v2.Refs)
	}
	// Both halves of the criterion: added in v2 is absent from v1, removed in v2 is present in v1.
	for _, r := range v1.Refs {
		if r.Ref.Target == string(b) {
			t.Error("a reference added in v2 appears in v1's reference set")
		}
	}
	for _, r := range v2.Refs {
		if r.Ref.Target == string(a) {
			t.Error("a reference removed in v2 still appears in v2's reference set")
		}
	}
	// And the version asked for is the version answered about.
	if v1.Ref.Number != 1 || v2.Ref.Number != 2 {
		t.Errorf("versions came back as %s and %s", v1.Ref, v2.Ref)
	}
	// The ref is Issue #11's, and it round-trips: what a listing names can be pasted back in.
	back, err := ParseVersionRef(v1.Ref.String())
	if err != nil || back != v1.Ref {
		t.Errorf("the listing's ref %q did not parse back to itself: %v, %v", v1.Ref, back, err)
	}
}

// CRITERION 6: the reverse question is answerable, for a note, a person and a group.
func TestWhatElseWasWrittenAboutThis(t *testing.T) {
	c := newCorpus(t)
	subject := c.publish(t, "alice", "the subject", "the original")
	c.publish(t, "alice", "one", "builds on [[note:"+string(subject)+"]]")
	c.publish(t, "alice", "two", "also [[note:"+string(subject)+"]] and [[person:bo]]")
	c.publish(t, "alice", "unrelated", "nothing to do with it")

	for _, tc := range []struct {
		target Reference
		want   []string
	}{
		{Reference{Kind: RefNote, Target: string(subject)}, []string{"one", "two"}},
		{Reference{Kind: RefPerson, Target: "bo"}, []string{"two"}},
		{Reference{Kind: RefGroup, Target: "platform"}, nil},
	} {
		b, err := ReferencesTo(c.store, tc.target, "bo")
		if err != nil {
			t.Fatalf("%v: %v", tc.target, err)
		}
		var got []string
		for _, n := range b.Notes {
			got = append(got, n.Title)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("what references %s %q: got %v, want %v", tc.target.Kind, tc.target.Target, got, tc.want)
		}
		if b.Count() != len(tc.want) {
			t.Errorf("count is %d, want %d", b.Count(), len(tc.want))
		}
	}
}

// CRITERION 8: a note that references the subject but that bo may not read is ABSENT from bo's
// reverse result — not a redaction, not a stub, not a gap in any numbering bo can observe.
func TestTheReverseQueryOmitsNotesTheReaderMayNotRead(t *testing.T) {
	c := newCorpus(t)
	subject := c.publish(t, "alice", "the subject", "the original")
	group, err := ToGroup("platform")
	if err != nil {
		t.Fatal(err)
	}
	// A note bo may NOT read, which references the subject.
	if _, err := PublishWithReferences(c.store, Publication{
		Author: "alice", Title: "restricted commentary",
		Body: "more about [[note:" + string(subject) + "]]", Visibility: group,
	}); err != nil {
		t.Fatal(err)
	}
	c.publish(t, "alice", "open commentary", "also about [[note:"+string(subject)+"]]")

	bos, err := ReferencesTo(c.store, Reference{Kind: RefNote, Target: string(subject)}, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if bos.Count() != 1 {
		t.Fatalf("bo sees %d referencing notes, want 1: %+v", bos.Count(), bos.Notes)
	}
	if bos.Notes[0].Title != "open commentary" {
		t.Errorf("bo sees %q", bos.Notes[0].Title)
	}
	if bos.Undetermined != 0 {
		t.Errorf("bo's result reports %d undetermined; a determined refusal is not undetermined", bos.Undetermined)
	}
	// alice sees both, so the restricted note really does reference the subject.
	alices, err := ReferencesTo(c.store, Reference{Kind: RefNote, Target: string(subject)}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if alices.Count() != 2 {
		t.Fatalf("alice sees %d, want 2 — otherwise this test proves nothing about bo", alices.Count())
	}
}

// CRITERION 9: asking about a target that exists but is invisible, and asking about a target that
// does not exist, produce the SAME observable outcome. Two different failures must not be told
// apart by the reader.
func TestAnInvisibleTargetAndAMissingTargetAnswerIdentically(t *testing.T) {
	c := newCorpus(t)
	c.publish(t, "alice", "some note", "unrelated prose")

	invisible, err := ReferencesTo(c.store, Reference{Kind: RefNote, Target: string(c.secret)}, "bo")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := ReferencesTo(c.store, Reference{Kind: RefNote, Target: "note-does-not-exist"}, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if invisible.Count() != missing.Count() || invisible.Undetermined != missing.Undetermined {
		t.Errorf("asking about an invisible target gave (%d, %d undetermined) and asking about a\n"+
			"nonexistent one gave (%d, %d undetermined); the two must be indistinguishable",
			invisible.Count(), invisible.Undetermined, missing.Count(), missing.Undetermined)
	}
	if len(invisible.Notes) != 0 || len(missing.Notes) != 0 {
		t.Errorf("both answers should be empty for bo: %+v / %+v", invisible.Notes, missing.Notes)
	}
	// The rendered answers, not only the counts — a surface could differ where the values do not.
	if renderBacklinks(invisible) != renderBacklinks(missing) {
		t.Errorf("the two answers render differently:\n  %q\n  %q", renderBacklinks(invisible), renderBacklinks(missing))
	}
}

func renderBacklinks(b Backlinks) string {
	var sb strings.Builder
	sb.WriteString("count=")
	sb.WriteString(string(rune('0' + b.Count())))
	sb.WriteString(" undetermined=")
	sb.WriteString(string(rune('0' + b.Undetermined)))
	for _, n := range b.Notes {
		sb.WriteString(" " + string(n.ID) + ":" + n.Title)
	}
	return sb.String()
}

// CRITERION 10: following a reference grants no read the reader did not already hold.
func TestFollowingAReferenceWidensNothing(t *testing.T) {
	c := newCorpus(t)
	n := c.publish(t, "alice", "points at the restricted note", "see [[note:"+string(c.secret)+"]]")

	if _, err := OutboundReferences(c.store, n, 0, "bo"); err != nil {
		t.Fatalf("bo cannot even list: %v", err)
	}
	// Having listed, bo still cannot read the target — by id, or through a grant.
	if _, err := c.store.Read(c.secret, "bo"); Code(err) != ErrRefused.Code {
		t.Errorf("after following, bo's read of the restricted note answered %v (code %q), want refused",
			err, Code(err))
	}
	g, err := NewLedger().Request(Holder{Person: "bo", Scopes: []Scope{ScopeRead}}, []Scope{ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadThrough(c.store, g, c.secret); Code(err) != ErrRefused.Code {
		t.Errorf("bo's agent read answered %v (code %q), want refused — an agent cannot read what its\n"+
			"person cannot", err, Code(err))
	}
}

// CRITERION 13: a reference to a note by a person who has been deactivated still resolves.
// Archiving is not unresolution.
//
// DRIVEN FOR REAL, against Issue #11's Archive, which merged to main after this branch was cut.
// Until then this could only be asserted structurally — that resolution consults the note's
// existence and [CanRead] and nothing about the author's standing. Now the author is actually
// marked as having left, which is the state criterion 13 is about.
func TestANoteByADepartedColleagueStillResolves(t *testing.T) {
	c := newCorpus(t)
	departed, err := PublishWithReferences(c.store, Publication{Author: "quinn", Title: "written before leaving", Body: "the knowledge was the point"})
	if err != nil {
		t.Fatal(err)
	}
	arch := NewArchive()
	arch.Deactivate("quinn")
	if !arch.IsDeactivated("quinn") {
		t.Fatal("the author was not marked as departed, so this test is not about a departed colleague")
	}
	n := c.publish(t, "alice", "cites them", "as [[note:"+string(departed.ID)+"]] says")

	l, err := OutboundReferences(c.store, n, 0, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if l.Count() != 1 || l.Refs[0].State != StateResolved {
		t.Errorf("the reference to the departed colleague's note is %+v, want one resolved reference —\n"+
			"archiving is not unresolution", l.Refs)
	}
}

// A reader who may not read the REFERENCING note learns nothing about its edges: the refusal is the
// note's own, and it is not a listing.
func TestListingANoteYouCannotReadIsRefused(t *testing.T) {
	c := newCorpus(t)
	if _, err := OutboundReferences(c.store, c.secret, 0, "bo"); Code(err) != ErrRefused.Code {
		t.Errorf("got %v (code %q), want the note's own refusal", err, Code(err))
	}
	if _, err := OutboundReferences(c.store, "note-nope", 0, "bo"); Code(err) != ErrNoSuchNote.Code {
		t.Errorf("got %v (code %q), want no-such-note", err, Code(err))
	}
}

// An unidentified reader is undetermined, never a resolution and never a refusal — the same
// judgement CanRead makes.
func TestAnUnidentifiedReaderDeterminesNothing(t *testing.T) {
	c := newCorpus(t)
	// EVERY KIND, and the last two are the reason this loop exists rather than one assertion about
	// a note. A note reaches the same answer through CanRead anyway, so a test that only asked
	// about notes passed with the guard removed — the person and group branches do not consult
	// CanRead at all and would have answered "resolved" to nobody in particular.
	for _, r := range []Reference{
		{Kind: RefNote, Target: string(c.secret)},
		{Kind: RefPerson, Target: "bo"},
		{Kind: RefGroup, Target: "platform"},
	} {
		if st := ResolveReference(c.store, r, ""); st != StateUndetermined {
			t.Errorf("an unidentified reader got %v for a %s reference, want undetermined", st, r.Kind)
		}
	}
	if _, err := ReferencesTo(c.store, Reference{Kind: RefNote, Target: string(c.secret)}, ""); Code(err) != ErrUndetermined.Code {
		t.Errorf("the reverse query for an unidentified reader answered %v (code %q), want undetermined",
			err, Code(err))
	}
}

// ---------------------------------------------------------------------------
// CRITERION 19 (owner ruling, added to the Issue at 14:07:17Z after this branch's head)
//
// "The identifier a reference renders is not derivable from any other note's identifier, nor from
// publication order or count; and a reference to a target the reader may not see remains
// indistinguishable from a reference to a target that does not exist — criterion 9's equivalence
// must survive the identifier being visible in the output."
//
// WHAT IS MINE AND WHAT IS NOT, stated here because the ruling binds four Issues. MINTING an
// unguessable identifier is #10's, at the one place ids are created (store.go's `note-%d`); the
// implementation is on #15's branch and is NOT on main as this is written. What is mine is that
// REFERENCES never derive an identifier and never leak one, and that they hold up whatever the
// identifier scheme is. These tests drive that half and are written so that they keep passing —
// and keep meaning the same thing — once ids become random.
// ---------------------------------------------------------------------------

// The rendered output for a reference is a function of ITS OWN target's identifier and nothing
// else: not another note's identifier, not publication order, not how many notes exist.
//
// Two corpora are built in which the very same note is minted with DIFFERENT identifiers, by
// publishing a different number of notes before it. If anything in reference rendering depended on
// order or count, the two outputs would differ by more than the identifier itself.
func TestCriterion19ReferenceOutputDependsOnNoOtherNotesIdentifier(t *testing.T) {
	build := func(fillers int) (rendered string, targetID NoteID) {
		rec := NewRecord()
		rec.AddPerson("bo")
		s := NewStore(rec)
		for i := 0; i < fillers; i++ {
			if _, err := PublishWithReferences(s, Publication{Author: "alice", Title: "filler", Body: "filler"}); err != nil {
				t.Fatal(err)
			}
		}
		target, err := PublishWithReferences(s, Publication{Author: "alice", Title: "the target", Body: "the target"})
		if err != nil {
			t.Fatal(err)
		}
		n, err := PublishWithReferences(s, Publication{
			Author: "alice", Title: "references it",
			Body: "the background is in [[note:" + string(target.ID) + "]] and in the wiki.",
		})
		if err != nil {
			t.Fatal(err)
		}
		l, err := OutboundReferences(s, n.ID, 0, "bo")
		if err != nil {
			t.Fatal(err)
		}
		return l.Body, target.ID
	}

	few, fewID := build(0)
	many, manyID := build(7)
	if fewID == manyID {
		t.Fatalf("both corpora minted the same identifier %q, so this comparison proves nothing about\n"+
			"order or count", fewID)
	}
	// Substitute each corpus's own target identifier out. What remains must be identical: the
	// identifier is the ONLY thing about the target that reaches the output.
	normalise := func(s string, id NoteID) string { return strings.ReplaceAll(s, string(id), "<target>") }
	if normalise(few, fewID) != normalise(many, manyID) {
		t.Errorf("the same reference renders differently depending on how many notes were published\n"+
			"before its target:\n  %q\n  %q", normalise(few, fewID), normalise(many, manyID))
	}
}

// CRITERION 19's second half: criterion 9's equivalence survives identifiers being visible.
//
// A control/test differential over a corpus that differs by EXACTLY ONE unreadable reference
// target. Everything bo can observe — the notes, their identifiers, the count — must be the same in
// both, so the presence of the unreadable note is not inferable from bo's answer.
//
// A LIMIT OF THIS TEST ON TODAY'S BASE, MEASURED RATHER THAN ASSUMED. The unreadable note is
// published LAST here. With another ordering it would fail, and not because of anything in this
// change: `main` still mints ids from a shared counter, so an unreadable note published BEFORE a
// readable one shifts the readable one's id. Measured on this tree — the readable note is
// "note-1" without the hidden note and "note-2" with it. That is the owner's own worked example,
// it is the half of criterion 19 that belongs to #10 (where ids are minted; the implementation is
// on #15's branch and not yet on main), and this test will hold for EVERY ordering the day that
// lands, with nothing here needing to change. What it drives today is that references add no
// disclosure of their own on top of it.
func TestCriterion19DifferentialOverOneUnreadableTarget(t *testing.T) {
	build := func(withUnreadable bool) Backlinks {
		rec := NewRecord()
		rec.DefineGroup("platform", "alice")
		rec.AddPerson("bo")
		s := NewStore(rec)
		subject, err := PublishWithReferences(s, Publication{Author: "alice", Title: "the subject", Body: "the original"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PublishWithReferences(s, Publication{
			Author: "alice", Title: "open commentary",
			Body: "about [[note:" + string(subject.ID) + "]]",
		}); err != nil {
			t.Fatal(err)
		}
		if withUnreadable {
			// The one difference between the two corpora.
			group, err := ToGroup("platform")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := PublishWithReferences(s, Publication{
				Author: "alice", Title: "restricted commentary",
				Body: "also about [[note:" + string(subject.ID) + "]]", Visibility: group,
			}); err != nil {
				t.Fatal(err)
			}
		}
		b, err := ReferencesTo(s, Reference{Kind: RefNote, Target: string(subject.ID)}, "bo")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	with, without := build(true), build(false)
	if renderBacklinks(with) != renderBacklinks(without) {
		t.Errorf("bo's answer differs depending on whether a note bo may not read exists:\n  %q\n  %q\n"+
			"the identifier being visible must not reintroduce the disclosure",
			renderBacklinks(with), renderBacklinks(without))
	}
	if with.Count() != 1 {
		t.Fatalf("bo should see exactly the one readable commentary, got %d — otherwise the two answers\n"+
			"could be equal by both being empty", with.Count())
	}
	// The identifiers themselves, not only the count.
	for i := range with.Notes {
		if with.Notes[i].ID != without.Notes[i].ID {
			t.Errorf("the readable note is %q in one corpus and %q in the other; publishing something bo\n"+
				"cannot read shifted an identifier bo can see", with.Notes[i].ID, without.Notes[i].ID)
		}
	}
}
