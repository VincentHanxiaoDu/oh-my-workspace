// Issue #11 — a note's timeline, read as it stood.
//
// THIS FILE IS NEW AND EDITS NOTHING. Issue #12 built the note, its single visibility and the read
// gate; this Issue builds the version-facing surface on top of them. Everything here is either a
// new type or a free function over Issue #12's values, for the same reason [CanRead] is a free
// function: #15's search must be able to name a version without holding a store, and a second
// implementation of "which version is current" is exactly the kind of thing that agrees until it
// does not.
//
// # What this file deliberately does NOT do
//
//   - It does not add a visibility to [Version]. See [Version]'s own comment: per-version
//     visibility is how the timeline becomes a bypass around a narrowing. There is one visibility,
//     it belongs to the note, and it governs every version. Every entry point here routes through
//     [Store.Read], so the gate cannot be skipped by reaching for a version directly.
//   - It does not add a fourth scope. Reading a version is reading; [ScopeRead] already says so.
//     The hub operator's ability to read everything is a deployment fact (PRD §2.4,
//     [RestrictionStatement]), not a capability anybody is granted.
//   - It does not delete anything, ever. PRD §5.4 is ruled: nothing expires. There is no prune, no
//     truncate, no retention window and no maximum count, and [TestNoRetentionMechanism] asserts
//     that by walking the package's exported surface rather than by trusting this paragraph.
package hub

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// The refusals this Issue adds.
//
// They are checked against Issue #12's own list by TestVersionErrorsArePairwiseDistinct, which
// reads that list rather than restating it — a new error that collides with an existing one is a
// test failure, not a review catch.
var (
	// ErrNoSuchVersion — the note exists and is readable, but it has no such point on its timeline.
	//
	// IT IS ITS OWN CODE, AND CRITERION 9 IS WHY. "There is no version 7" and "version 7's body is
	// the empty string" must be tellable apart BY EXIT STATUS, without parsing the body. Reusing
	// ErrNoSuchNote would have been close enough to pass a careless test and wrong for a caller,
	// who can fix one of those by asking for a different version and neither of them by retrying.
	ErrNoSuchVersion = &Error{Code: "no-such-version", Msg: "no such version of that note"}

	// ErrVersionUnreadable — the version exists and the reader may read it, but its content could
	// not be retrieved.
	//
	// UNDETERMINED, NEVER EMPTY. Criterion 8: a body that could not be read is not a body that is
	// blank. This is the code a persistent store returns when the blob behind a version cannot be
	// fetched; the in-memory store here never produces it, and a test double does.
	ErrVersionUnreadable = &Error{Code: "version-unreadable", Msg: "the content of that version could not be read"}

	// ErrBadVersionRef — a version reference that could not be parsed.
	ErrBadVersionRef = &Error{Code: "bad-version-ref", Msg: "that is not a version reference"}
)

// versionErrors is this Issue's additions, for the distinctness test. Issue #12's allErrors is left
// alone: appending to it would edit another Issue's reviewed file for no gain, and the test can
// read both lists.
var versionErrors = []*Error{ErrNoSuchVersion, ErrVersionUnreadable, ErrBadVersionRef}

// VersionRef is a note's version, named in a way a person can copy out of one command and paste
// into another.
//
// CRITERION 2 ASKS FOR AN IDENTIFIER THAT CAN BE "FED STRAIGHT BACK IN", AND CRITERION 3 THAT IT BE
// STABLE. A bare version number is neither: it is meaningless without the note beside it, and a
// person who kept "3" from a month ago cannot tell what it refers to. A ref carries both halves,
// prints as `note-1@v3`, and parses back to the same value.
//
// It is stable because the timeline is append-only and nothing is ever removed (PRD §5.4). Version
// 3 of a note is version 3 of that note forever; publishing versions 4 and 5 does not renumber it.
// That is a property of [Store.Amend] never deleting and never reordering, and it is driven by
// TestARefObtainedEarlyReadsTheSameContentAfterManyLaterPublications rather than assumed.
type VersionRef struct {
	Note   NoteID
	Number int
}

// refSeparator is the one spelling. Both String and ParseVersionRef use it, so a change to the
// format cannot make the two disagree.
const refSeparator = "@v"

// String renders the ref. Never empty: a zero ref renders as something a reader can see is wrong
// rather than as a blank a reader reads as absence.
func (r VersionRef) String() string {
	if r.Note == "" && r.Number == 0 {
		return "(no version reference)"
	}
	return string(r.Note) + refSeparator + strconv.Itoa(r.Number)
}

// ParseVersionRef reads a ref back. It is the inverse of [VersionRef.String] and a test drives the
// round trip.
func ParseVersionRef(s string) (VersionRef, error) {
	t := strings.TrimSpace(s)
	i := strings.LastIndex(t, refSeparator)
	if i <= 0 {
		return VersionRef{}, Refusedf(ErrBadVersionRef, "%q — a reference looks like note-1%s3", s, refSeparator)
	}
	num, err := strconv.Atoi(t[i+len(refSeparator):])
	if err != nil || num < 1 {
		return VersionRef{}, Refusedf(ErrBadVersionRef, "%q — the version part is not a version number", s)
	}
	return VersionRef{Note: NoteID(t[:i]), Number: num}, nil
}

// VersionSource is what the version surface reads from.
//
// WHY AN INTERFACE AND NOT *Store DIRECTLY. Criterion 8 and criterion 12 are both about what
// happens when the answer CANNOT be established — an unreachable hub while listing, a version whose
// content cannot be retrieved. The in-memory store of this build can never fail that way, so a
// surface written against the concrete store has no way to be driven down those paths, and "it
// renders undetermined" would be a claim about unexecuted code. With an interface, a test double
// returns the failures a real transport will return, and the rendering is driven for real.
//
// It is deliberately reader-taking on every method. There is no way to ask this interface for a
// version without saying who is asking, which is what keeps criterion 14 a property of the shape
// rather than of every caller remembering.
type VersionSource interface {
	// Timeline returns every version of the note, oldest first, if the reader may read the note.
	Timeline(id NoteID, reader PersonID) ([]Version, error)
	// VersionAt returns one version, if the reader may read the note.
	VersionAt(id NoteID, num int, reader PersonID) (Version, error)
}

// Archive records who has left.
//
// PRD §3.3 and criterion 7: "a deactivated person's notes and their full version history remain
// readable to those permitted to read them, marked archived rather than absent". Note what that
// sentence does NOT say — it does not say the notes become less readable, and it does not say the
// history is trimmed. So deactivation is a LABEL and nothing else: it is consulted when rendering a
// view and never when deciding readability. [CanRead] does not know this type exists, on purpose;
// a deactivation that could refuse a read would be a retention mechanism wearing a different hat.
//
// Issue #22 owns departed colleagues' notes properly. This is the minimum criterion 7 needs, and it
// is a separate type rather than a field on [Record] so that #22 can replace it without unpicking
// Issue #12's membership record.
//
// A nil *Archive is usable and reports nobody as deactivated.
type Archive struct {
	mu          sync.RWMutex
	deactivated map[PersonID]bool
	// unreadable is Issue #22's third value: people whose record could not be read, so whether they
	// have left was NOT established. It is a separate set from deactivated on purpose — a person in
	// it is neither active nor departed, and merging the two would make "I could not check" render
	// as one of the answers. See [Archive.MarkUnreadable] and [AuthorActive].
	unreadable map[PersonID]bool
}

// NewArchive returns an empty archive.
func NewArchive() *Archive { return &Archive{} }

// Deactivate marks a person as no longer with the company. It removes nothing.
func (a *Archive) Deactivate(p PersonID) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deactivated == nil {
		a.deactivated = map[PersonID]bool{}
	}
	a.deactivated[p] = true
}

// IsDeactivated reports whether the person has left. Nil-safe: no archive means nobody has.
func (a *Archive) IsDeactivated(p PersonID) bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.deactivated[p]
}

// Deactivated returns everyone marked as having left, ordered.
func (a *Archive) Deactivated() []PersonID {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]PersonID, 0, len(a.deactivated))
	for p := range a.deactivated {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Standing wording. There are THREE, they are compared PAIRWISE by test, and none of them is a
// substring of another.
//
// Criterion 5 wants a superseded version distinguishable "by content and not merely by the
// identifier the caller happened to pass" — so these sentences go in the OUTPUT, next to the body,
// on every read, including reads where the caller named no version at all. Criterion 8 wants the
// third distinguishable from both of the others; it is not "no" dressed up, and it does not contain
// the words the other two lead with.
const (
	// StandingCurrentLine is what a reader of the current version sees.
	StandingCurrentLine = "standing: current — this is the note as it stands now"
	// StandingSupersededLine is what a reader of an older version sees, whether or not they asked
	// for one by name.
	StandingSupersededLine = "standing: superseded — the note has been amended since this was written, and what you are reading is not what it says today"
	// StandingUndeterminedLine is the third answer. It is neither of the above and it is not silence.
	StandingUndeterminedLine = "standing: could not be determined — whether the note has been amended since this was written was not established, so treat this as neither confirmed nor stale"
)

// StandingLine renders a standing.
//
// The standing is a [tri.Value] rather than a new two-plus-one enum, because the product already
// has exactly one three-valued answer and a second one is how the third value gets a second, softer
// wording somewhere. Yes means current, No means superseded, and the zero value — the value a
// struct field nobody set carries — is undetermined, which is the right default for "we have not
// worked out whether this is stale".
func StandingLine(v tri.Value) string {
	switch v {
	case tri.Yes:
		return StandingCurrentLine
	case tri.No:
		return StandingSupersededLine
	default:
		return StandingUndeterminedLine
	}
}

// AllStandingLines returns the three renderings for the pairwise-distinctness test.
func AllStandingLines() map[string]string {
	return map[string]string{
		"current":      StandingLine(tri.Yes),
		"superseded":   StandingLine(tri.No),
		"undetermined": StandingLine(tri.Undetermined),
	}
}

// BodyUnreadableLine is what stands in for a body that could not be retrieved.
//
// CRITERION 8 AND CRITERION 9 MEET HERE. An unreadable body must not render as an empty one, and
// the way that goes wrong is not malice: a fetch returns ("", err), the error is logged, and the
// empty string is printed under a success. So the view carries a BodyKnown flag, the renderer
// prints this instead of the body, and the exit code is the undetermined one.
const BodyUnreadableLine = "body: could not be read — this is not an empty body, and no content is being shown for it"

// VersionView is one version, as a surface renders it.
//
// IT CARRIES NO VISIBILITY, for the same reason [Version] does not: a view is produced only after
// [Store.Read] has already allowed the note through, and a visibility on the view is the field a
// later change starts filtering on instead of re-asking the gate.
type VersionView struct {
	// Ref names this version and can be fed straight back in (criterion 2).
	Ref VersionRef
	// Body is the note as it stood. Meaningful only when BodyKnown.
	Body string
	// BodyKnown is false when the content could not be retrieved. Its zero value is false, so a
	// view built by an error path that forgot to fill it in renders as unreadable rather than as
	// empty — the safe direction.
	BodyKnown bool
	// At is when this version was written; zero when that was not established.
	At time.Time
	// Standing is Yes for the current version, No for a superseded one, Undetermined when it was
	// not established which (criterion 8). Zero value is Undetermined.
	Standing tri.Value
	// Archived marks a version whose author has left (criterion 7). It is a label; it never makes
	// the version less readable.
	Archived bool
}

// Determined reports whether everything the view claims was actually established. A surface uses it
// to choose between the success and the undetermined exit code, and never to decide whether to
// print — an undetermined view still prints, saying so.
func (v VersionView) Determined() bool { return v.BodyKnown && v.Standing.Determined() }

// Render writes the view the way both surfaces show it. ONE renderer, called by the CLI and by the
// control API's text form, because criterion 13 is that the two agree and the cheapest way to make
// two things agree is for there to be one of them.
func (v VersionView) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "version: %s\n", v.Ref)
	fmt.Fprintf(&b, "%s\n", StandingLine(v.Standing))
	if v.Archived {
		fmt.Fprintf(&b, "%s\n", ArchivedLine)
	}
	if !v.At.IsZero() {
		fmt.Fprintf(&b, "written: %s\n", v.At.UTC().Format(time.RFC3339))
	}
	if !v.BodyKnown {
		fmt.Fprintf(&b, "%s\n", BodyUnreadableLine)
		return b.String()
	}
	fmt.Fprintf(&b, "body:\n%s\n", v.Body)
	return b.String()
}

// ArchivedLine marks a note whose author has left. It says "still readable" in as many words,
// because criterion 7's whole point is that archived is not absent.
const ArchivedLine = "archived: the author of this note has left; the note and its full history are kept and remain readable"

// TimelineView is a note's whole timeline as a surface renders it.
type TimelineView struct {
	Note NoteID
	// Entries is oldest-first and, for a readable note, is NEVER empty — criterion 2: a
	// single-version note lists exactly one entry.
	Entries []VersionView
	// Current is the ref of the current version. Zero when Determined is false.
	Current VersionRef
	// Determined is false when the timeline could not be established — an unreachable hub, a store
	// that could not answer. Criterion 12: that is a DIFFERENT report from a short timeline, and it
	// is why this field exists instead of an empty Entries slice meaning two things.
	Determined bool
	// Archived marks a note whose author has left.
	Archived bool
	// Why carries the reason when Determined is false, so a surface can print the code.
	Why error
}

// UndeterminedTimelineLine is what an unestablished timeline prints instead of a list.
//
// CRITERION 12 IS THIS CONSTANT'S REASON TO EXIST. "The hub could not be reached" and "this note
// has one version" are different facts, and the failure mode is that the first renders as an empty
// or one-line list and a person reads a complete history. So the undetermined timeline prints no
// entries at all and prints this instead, and the test compares the two renderings against each
// other rather than against a literal.
const UndeterminedTimelineLine = "timeline: could not be determined — no version list is being shown, and this is not a note with no history"

// Render writes the timeline. One renderer, both surfaces (criterion 13).
func (t TimelineView) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "note: %s\n", string(t.Note))
	if !t.Determined {
		fmt.Fprintf(&b, "%s\n", UndeterminedTimelineLine)
		if t.Why != nil {
			fmt.Fprintf(&b, "reason: %v (code: %s)\n", t.Why, Code(t.Why))
		}
		return b.String()
	}
	if t.Archived {
		fmt.Fprintf(&b, "%s\n", ArchivedLine)
	}
	fmt.Fprintf(&b, "current: %s\n", t.Current)
	fmt.Fprintf(&b, "versions: %d\n", len(t.Entries))
	for _, e := range t.Entries {
		mark := "superseded"
		if e.Standing == tri.Yes {
			mark = "current"
		}
		when := "time not established"
		if !e.At.IsZero() {
			when = e.At.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "  %s  %-10s  %s\n", e.Ref, mark, when)
	}
	return b.String()
}

// gate collapses what a reader is allowed to know about a note they may not read.
//
// CRITERION 14, AND IT IS A REAL TENSION WITH ISSUE #12, RECORDED RATHER THAN SMOOTHED OVER.
// Issue #12 criterion 12 requires ErrRefused and ErrNoSuchNote be DISTINGUISHABLE, and #12
// implements exactly that in [Store.Read]. Issue #11 criterion 14 requires that for a reader who
// may not read a note, "their result is indistinguishable from that for a note that does not
// exist". Both cannot hold on one surface.
//
// The resolution taken here: Issue #12's store is left exactly as it is — [Store.Read] still
// answers the two distinguishably, and #12's test for that still passes untouched. The collapse
// happens HERE, on the version surfaces this Issue adds, which is where criterion 14 is written
// about ("including through the timeline, through search, or through a version identifier they
// obtained some other way"). The consequence is that `omw visibility show` and `omw note` answer
// differently for the same refused note, and that is flagged in the pull request as something the
// two Issues do not jointly settle.
// It normalises BOTH branches rather than only the refusal, because a refusal rewritten into
// "no such note" is still distinguishable if the two sentences are assembled differently — the id
// is echoed by one and not the other, or the wording differs by a word. So both come out of one
// format string with the id the caller supplied, and a test compares the two outputs for the same
// id character for character.
func gate(id NoteID, err error) error {
	switch Code(err) {
	case ErrRefused.Code, ErrNoSuchNote.Code:
		return Refusedf(ErrNoSuchNote, "%q", string(id))
	default:
		return err
	}
}

// ListTimeline is criterion 2: every version of a note the reader may read, in order, each with an
// identifier that can be fed straight back in.
//
// A single-version note yields exactly one entry. A note the reader may not read, and a note that
// does not exist, yield the same refusal (see [gate]). A timeline that could not be established
// yields a view with Determined false and NO entries — never a partial list presented as whole.
func ListTimeline(src VersionSource, arch *Archive, id NoteID, reader PersonID) (TimelineView, error) {
	vs, err := src.Timeline(id, reader)
	if err != nil {
		switch Code(err) {
		case ErrUndetermined.Code, ErrHubUnreachable.Code, ErrVersionUnreadable.Code:
			// NOT AN ERROR RETURN. Criterion 12 wants a REPORT that differs from a real timeline
			// and from a refusal, and a caller that only ever sees an error prints whatever its
			// error branch prints. So the undetermined timeline is a first-class view.
			return TimelineView{Note: id, Determined: false, Why: err}, nil
		default:
			return TimelineView{Note: id}, gate(id, err)
		}
	}
	if len(vs) == 0 {
		// A readable note with no versions is not a note with an empty history; it is a store that
		// did not tell us its history. Criterion 2 forbids rendering it as an empty timeline.
		return TimelineView{Note: id, Determined: false,
			Why: Refusedf(ErrUndetermined, "note %q returned no versions at all", string(id))}, nil
	}
	current := VersionRef{Note: id, Number: vs[len(vs)-1].Number}
	archived := arch.IsDeactivated(authorOf(src, id, reader))
	out := TimelineView{Note: id, Current: current, Determined: true, Archived: archived}
	for _, v := range vs {
		standing := tri.No
		if v.Number == current.Number {
			standing = tri.Yes
		}
		out.Entries = append(out.Entries, VersionView{
			Ref:       VersionRef{Note: id, Number: v.Number},
			Body:      v.Body,
			BodyKnown: true,
			At:        v.At,
			Standing:  standing,
			Archived:  archived,
		})
	}
	return out, nil
}

// authorer is the optional half of [VersionSource]: a source that can name a note's author, so that
// criterion 7's archived marking can be applied. A source that cannot is not an error — the marking
// is simply not applied, which is honest, rather than guessed.
type authorer interface {
	AuthorOf(id NoteID, reader PersonID) (PersonID, error)
}

func authorOf(src VersionSource, id NoteID, reader PersonID) PersonID {
	a, ok := src.(authorer)
	if !ok {
		return ""
	}
	p, err := a.AuthorOf(id, reader)
	if err != nil {
		return ""
	}
	return p
}

// ReadView is criterion 3 and criterion 5: read the version a ref names, and say in the output
// whether it is the current one.
//
// The standing is worked out from the TIMELINE, not from the ref the caller passed. That is the
// whole of criterion 5's "not merely by the identifier the caller happened to pass": a surface that
// printed "superseded" because the caller typed a version number would say "current" for a caller
// who typed the current number by hand and for a caller who typed an old one after an amendment it
// had not noticed.
func ReadView(src VersionSource, arch *Archive, ref VersionRef, reader PersonID) (VersionView, error) {
	standing := tri.Undetermined
	tl, terr := src.Timeline(ref.Note, reader)
	switch {
	case terr != nil:
		switch Code(terr) {
		case ErrUndetermined.Code, ErrHubUnreachable.Code, ErrVersionUnreadable.Code:
			// Standing stays undetermined; we still try for the body below, because "I could not
			// tell you whether this is stale" is not a reason to withhold what was asked for.
		default:
			return VersionView{Ref: ref}, gate(ref.Note, terr)
		}
	case len(tl) == 0:
		// Leave undetermined. Never "current" by default: criterion 6 forbids presenting anything
		// as current when current could not be identified.
	default:
		standing = tri.No
		if ref.Number == tl[len(tl)-1].Number {
			standing = tri.Yes
		}
	}

	v, err := src.VersionAt(ref.Note, ref.Number, reader)
	if err != nil {
		switch Code(err) {
		case ErrUndetermined.Code, ErrHubUnreachable.Code, ErrVersionUnreadable.Code:
			// CRITERION 8: the body could not be read. BodyKnown stays false, so the renderer
			// prints [BodyUnreadableLine] and the surface exits undetermined. It does NOT return
			// an empty body under a success, and it does NOT return "no such version".
			return VersionView{Ref: ref, Standing: standing,
				Archived: arch.IsDeactivated(authorOf(src, ref.Note, reader))}, nil
		default:
			return VersionView{Ref: ref, Standing: standing}, gate(ref.Note, err)
		}
	}
	return VersionView{
		Ref:       ref,
		Body:      v.Body,
		BodyKnown: true,
		At:        v.At,
		Standing:  standing,
		Archived:  arch.IsDeactivated(authorOf(src, ref.Note, reader)),
	}, nil
}

// CurrentView is criterion 6: a request for a note that names no version.
//
// IT NEVER FALLS BACK. If the current version cannot be identified, this returns an undetermined
// view — criterion 6 says so in as many words ("if the current version cannot be identified,
// criterion 8 applies rather than falling back to any older version"). The tempting shape is
// `if err != nil { return oldest }` or `return versions[len-1]` computed from a list that might be
// partial; both hand a person superseded text labelled current.
func CurrentView(src VersionSource, arch *Archive, id NoteID, reader PersonID) (VersionView, error) {
	tl, err := ListTimeline(src, arch, id, reader)
	if err != nil {
		return VersionView{}, err
	}
	if !tl.Determined {
		return VersionView{Ref: VersionRef{Note: id}, Standing: tri.Undetermined, Archived: tl.Archived}, nil
	}
	return ReadView(src, arch, tl.Current, reader)
}
