// Issue #22 — a departed colleague's notes: archived, not deleted, and still attributed to them.
//
// PRD §3.3: "Notes outlive employment. A deactivated person's notes are archived, not deleted — the
// knowledge was the point." The failure this file is written against is the quiet one: a note that
// becomes unfindable, or unattributed, or attributed to nobody, because its author left. All three
// are the same loss.
//
// # What this file does NOT do, on purpose
//
//   - It does not touch [CanRead]. Deactivation is not a visibility change, in either direction, so
//     the predicate that decides who may read a note does not know this file exists. That is not an
//     oversight to be tidied up later: the moment readability consults deactivation, a departure
//     starts moving notes in or out of people's reach, which is criteria 5 and 6 both broken at
//     once. Every read path here goes through [Store.Read] / [Store.ListReadable], which call
//     [CanRead]. There is no second predicate.
//   - It does not add a fourth scope. Reading an archived note is reading; publishing as a departed
//     person is refused, not granted a scope of its own. The vocabulary stays `read`/`write`/
//     `publish`, and the hub operator's read-everything remains a deployment fact (§2.4).
//   - It does not add a visibility to [Version]. One visibility governs a note and every version of
//     it, before and after its author leaves — which is precisely why criterion 13's "attribution is
//     stable across versions" is cheap here: there is one note, one author, one state.
//   - It deletes nothing and expires nothing (PRD §5.4). There is no window after which an archived
//     note goes away, because there is no window at all.
//
// # Three answers about a person, not two
//
// Issue #11 landed [Archive] with a two-valued IsDeactivated, which was the minimum its criterion 7
// needed. Criterion 12 and criterion 18 of this Issue need three: active, deactivated, and
// COULD NOT BE DETERMINED. So this file adds the third value rather than replacing #11's type — a
// [tri.Value], as everything three-valued in this product is, so that the third value cannot pick up
// a second, softer wording in one more package.
package hub

import (
	"fmt"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// The refusals this Issue adds. Checked against the other two lists for pairwise distinctness by
// TestDepartedErrorsArePairwiseDistinct.
var (
	// ErrPersonDeactivated — the person has left, and what was asked would create new authority for
	// them: accepting a session, issuing a grant, publishing a note, adding a version.
	//
	// IT IS NOT ErrRefused. "You may not read this note" and "the person this acts as has left the
	// company" are fixed by different things — the second is not fixable at all, and a caller that
	// cannot tell them apart will retry forever. Criterion 16 also wants this refused when it is
	// REQUESTED rather than narrowed at the edge (§4.5), and a code is what lets a test assert
	// which of the two happened.
	ErrPersonDeactivated = &Error{Code: "person-deactivated", Msg: "refused: that person has left the company, and nothing new can be signed in, published or amended as them"}

	// ErrPersonStateUndetermined — whether the person is still with the company could not be
	// worked out.
	//
	// UNDETERMINED, NEVER A "NO" AND NEVER A "YES" (§4.3, criterion 18). It is not
	// ErrPersonDeactivated with a softer sentence: treating it as a departure archives somebody who
	// is at their desk, and treating it as active hands a departed person's session straight
	// through. Both are wrong in a way the reader cannot see, so the answer is that nothing was
	// established.
	ErrPersonStateUndetermined = &Error{Code: "person-state-undetermined", Msg: "whether that person is still with the company could not be determined"}
)

// departedErrors is this Issue's additions, for the distinctness test. Issue #12's allErrors and
// Issue #11's versionErrors are left alone; the test reads all three lists.
var departedErrors = []*Error{ErrPersonDeactivated, ErrPersonStateUndetermined}

// PeopleStatus answers one question about one person: have they left?
//
// It is deliberately as narrow as [Membership], and for the same §5.3 reason — the hub owns its
// record of people, and there is no place here for a company directory to plug itself in. Criterion
// 17 is that deactivation is an act performed against the hub and never a side effect of some
// client-side event or channel signal; an interface that answers synchronously, about one person,
// with no context and no network shape, gives such a signal nowhere to arrive.
//
// The (bool, error) pair is the third value's entry point. An error is NOT a false: a record that
// could not be read is [tri.Undetermined], converted by [tri.FromError] in [AuthorActive], which is
// the only sanctioned conversion in this project.
type PeopleStatus interface {
	// HasLeft reports whether the person has left. The error means the answer could not be worked
	// out; it never means "no".
	//
	// It is NOT called Deactivated, because [Archive] already has a Deactivated() that lists
	// everyone who has gone. Two methods one letter apart on the same type is how a caller reaches
	// for the wrong one and gets a plausible answer.
	HasLeft(p PersonID) (bool, error)
}

// HasLeft makes [Archive] a [PeopleStatus].
//
// A real archive can always answer, so the error is always nil — the pair exists for the interface,
// and for sources that cannot always answer (see [Archive.MarkUnreadable] and, later, a persistent
// hub whose people record is behind a store that can fail).
func (a *Archive) HasLeft(p PersonID) (bool, error) {
	if a == nil {
		// A nil archive reports nobody as deactivated, matching [Archive.IsDeactivated]. It is a
		// hub that knows of no departures, which is a determined answer about the record it has.
		return false, nil
	}
	a.mu.RLock()
	unreadable := a.unreadable[p]
	dead := a.deactivated[p]
	a.mu.RUnlock()
	if unreadable {
		return false, Refusedf(ErrPersonStateUndetermined, "the hub's record of %q could not be read", string(p))
	}
	return dead, nil
}

// MarkUnreadable records that the hub's record of this person cannot be read.
//
// THIS IS NOT A THIRD KIND OF DEPARTURE. It is the state criterion 18 is about: the lookup failed,
// so nothing was established. It exists as a real, settable state rather than only as a test double
// because criterion 18 must be driven through the same code path a production failure would take —
// a rendering proven only against a hand-built struct is a rendering proven against nothing.
func (a *Archive) MarkUnreadable(p PersonID) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.unreadable == nil {
		a.unreadable = map[PersonID]bool{}
	}
	a.unreadable[p] = true
}

// AuthorActive is the three-valued answer about a person, and the ONE place the conversion happens.
//
//	tri.Yes           still with the company
//	tri.No            deactivated — they have left, and their notes are archived
//	tri.Undetermined  it could not be worked out: no people record was supplied, the record could
//	                  not be read, or nobody said who the person is
//
// A nil PeopleStatus is UNDETERMINED, not active. This is the asymmetry that matters and it is the
// opposite of [Archive]'s nil behaviour on purpose: a nil archive is a hub that knows of no
// departures, whereas a nil PeopleStatus is no hub record at all in the caller's hand. Answering
// "active" for the second would render every author of every note as present at their desk on a
// client that never asked anybody.
func AuthorActive(ps PeopleStatus, p PersonID) tri.Value {
	if ps == nil {
		return tri.Undetermined
	}
	if strings.TrimSpace(string(p)) == "" {
		// We were not told who. That is not a departure and it is not a person at their desk.
		return tri.Undetermined
	}
	gone, err := ps.HasLeft(p)
	if err != nil {
		return tri.Undetermined
	}
	if gone {
		return tri.No
	}
	return tri.Yes
}

// Attribution is who wrote a note and what is known about whether they are still here.
//
// CRITERION 9, 10 AND 12 ALL LAND ON THIS TYPE. The author is carried as itself — the same
// [PersonID] the note was published under, not a display name derived somewhere else that could come
// back empty — and the state is carried beside it rather than folded into it. Folding is how "author:
// deactivated" ends up in a field that is supposed to hold a name, which is criterion 10's
// "placeholder indistinguishable from no author" arriving by the side door.
type Attribution struct {
	// Author is who wrote the note, identified exactly as they were while active.
	Author PersonID
	// Active is Yes for a person still with the company, No for a deactivated one, and
	// Undetermined when it could not be worked out. The zero value is Undetermined, so an
	// Attribution an error path left half-built renders as "not established" rather than as a
	// colleague at their desk.
	Active tri.Value
}

// AttributionFor reads a note's attribution. It never invents an author and never drops one.
func AttributionFor(n *Note, ps PeopleStatus) Attribution {
	if n == nil {
		return Attribution{}
	}
	return Attribution{Author: n.Author, Active: AuthorActive(ps, n.Author)}
}

// The four attribution renderings. They are compared PAIRWISE by test — not each against a literal,
// because a literal-by-literal test passes just as happily after two of them have been edited into
// the same sentence — and none of them is empty.
//
// The first three are criterion 12's "three distinct renderings, no two identical, none of them
// blank". The fourth is criterion 10's backstop: a note that reached a surface with no author on it
// is a defect in the record, and it renders as one, loudly, rather than as a blank field a reader
// takes for "nobody wrote this".
const (
	// attributionActiveClause — the author is still here.
	attributionActiveClause = "still with the company"
	// attributionDepartedClause — the author has left. It says archived-and-kept in as many words,
	// because criterion 11's whole point is that archived is not absent.
	attributionDepartedClause = "has left the company — this note is archived, not deleted: it is kept, it stays readable to exactly whoever could read it before, and it stays theirs"
	// attributionUndeterminedClause — the third answer. It contains neither of the other two
	// clauses' leading words, and it says out loud that it is not a departure, because a reader who
	// skims will otherwise take any hedged sentence next to a name as bad news.
	attributionUndeterminedClause = "whether they are still with the company could not be determined — this is not a report that they have left, and not a report that they are here"
	// AttributionNoAuthorLine is the fourth rendering and it is not a state of a person; it is a
	// state of the record.
	AttributionNoAuthorLine = "author: NOT RECORDED — this note reached this surface carrying no author, which is a defect in the hub's record and not a note that nobody wrote"
)

// Line renders the attribution. It is NEVER empty and it always names the author.
//
// Criterion 10 is the reason there is no branch here that omits the name: not for a departed author,
// not for an undetermined one, not for an error path. The only branch without a name is the one
// where there was no name to print, and it says so in those words.
func (a Attribution) Line() string {
	if strings.TrimSpace(string(a.Author)) == "" {
		return AttributionNoAuthorLine
	}
	return fmt.Sprintf("author: %s — %s", string(a.Author),
		a.Active.Render(attributionActiveClause, attributionDepartedClause))
}

// AllAttributionLines returns the four renderings for the pairwise-distinctness test.
//
// The three person-state lines are built for the SAME author, so that the test compares the state
// clauses rather than accidentally passing because the names differ.
func AllAttributionLines() map[string]string {
	const who = PersonID("priya")
	return map[string]string{
		"active":       Attribution{Author: who, Active: tri.Yes}.Line(),
		"deactivated":  Attribution{Author: who, Active: tri.No}.Line(),
		"undetermined": Attribution{Author: who, Active: tri.Undetermined}.Line(),
		"no-author":    Attribution{Author: "", Active: tri.Yes}.Line(),
	}
}

// Render writes the attribution as a surface shows it: the line, plus — for a departed author —
// the retention sentence, so a reader meets §5.4 where the departure is reported rather than in a
// document.
func (a Attribution) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", a.Line())
	if a.Active == tri.No {
		fmt.Fprintf(&b, "%s\n", RetentionLine)
	}
	return b.String()
}

// RetentionLine is PRD §5.4, said where it matters.
//
// It is a constant rather than prose in each surface for the same reason [RestrictionStatement] is:
// two surfaces each writing their own sentence is how one of them ends up implying a deletion
// schedule that does not exist.
const RetentionLine = "retention: nothing expires — this note and every version of it are kept, and no window ends that"

// --- Deactivation as an act against the hub -----------------------------------------------------

// Deactivate marks a person as having left, and is the WHOLE of what deactivating does to the
// archive: it adds a name to a set. It removes no note, no version and no attribution.
//
// Criterion 17 — deactivation is an act performed against the hub, not a side effect — is a property
// of there being exactly one function that does it, taking no event, no channel and no directory
// record. See [Archive.Deactivate] for the act itself; this comment is where the criterion is
// recorded, and TestOnlyTheHubDeactivatesAPerson asserts the package exposes no other way.

// CheckActive is the gate every act that would create NEW authority for a person passes through.
//
// THREE OUTCOMES, THREE ERRORS, AND THE THIRD IS NOT A REFUSAL:
//
//	nil                            the person is here; carry on
//	ErrPersonDeactivated           they have left — refused (criteria 14 and 16)
//	ErrPersonStateUndetermined     nothing was established, so nothing is done either
//
// The undetermined branch REFUSES THE ACT while reporting that it refused for want of an answer,
// not for a determined no. That direction is chosen deliberately and it is the one asymmetry in this
// file: for READS, an undetermined author state changes nothing at all (an archived note is read the
// same way whatever is known about its author), whereas for WRITES, proceeding on an unestablished
// person is how a departed person's script keeps publishing through a flaky people record.
func CheckActive(ps PeopleStatus, p PersonID) error {
	switch AuthorActive(ps, p) {
	case tri.Yes:
		return nil
	case tri.No:
		return Refusedf(ErrPersonDeactivated, "%q", string(p))
	default:
		return Refusedf(ErrPersonStateUndetermined, "%q, so nothing has been signed in, published or amended as them", string(p))
	}
}

// SetPeopleStatus attaches the hub's record of who has left to the store, so that the WRITE paths
// refuse a deactivated author.
//
// WHY ON THE STORE AND NOT IN A WRAPPER. Criterion 16 says "nothing can publish a new note as that
// person or add a version to their existing notes". A wrapper makes that true of callers who
// remember the wrapper, which is a rule about diligence rather than about the hub. Put here, the one
// function that stores a note is the one function that checks, and a caller reaching [Store.Publish]
// directly gets the same refusal.
//
// The default is nil, which means no people record is attached and every author is treated as
// active — the state every existing test runs in, and the honest reading of a store nobody has told
// about any departures.
func (s *Store) SetPeopleStatus(ps PeopleStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.people = ps
}

// checkAuthorWritableLocked is the write gate. It is a no-op when no people record is attached.
func (s *Store) checkAuthorWritableLocked(p PersonID) error {
	if s.people == nil {
		return nil
	}
	return CheckActive(s.people, p)
}

// peopleStatusLocked returns the attached record, or nil.
func (s *Store) peopleStatusLocked() PeopleStatus { return s.people }

// PeopleStatusOf returns the store's attached people record, which surfaces pass to
// [AttributionFor] so that the attribution a reader sees comes from the same record the write gate
// enforces. Two records is how a note gets refused as departed and rendered as active.
func (s *Store) PeopleStatusOf() PeopleStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.peopleStatusLocked()
}

// --- Sessions: ended, without anything being removed --------------------------------------------

// AcceptGrant decides whether a grant may still be used. Criterion 14: after deactivation, no token
// issued to that person is accepted, FOR ANY SCOPE — not "read my own notes", not "publish as me".
//
// It is checked before the scope is, so that a departed person holding a read grant is told they
// have left rather than that their scope is wrong. The set of scopes on the grant is not consulted
// at all, which is what makes "for any scope" a property of the shape.
func AcceptGrant(ps PeopleStatus, g Grant) error { return CheckActive(ps, g.Holder) }

// ReadThroughLive is [ReadThrough] with the holder's standing checked first.
//
// CRITERION 15 IS THE PAIR OF THIS AND [Store.Read]: the same deactivation that makes this refuse
// leaves the note readable to everybody else, because this checks the HOLDER and [CanRead] checks the
// AUDIENCE, and neither has been taught about the other.
func ReadThroughLive(s *Store, ps PeopleStatus, g Grant, id NoteID) (*Note, error) {
	if err := AcceptGrant(ps, g); err != nil {
		return nil, err
	}
	return ReadThrough(s, g, id)
}

// PublishThroughLive is [PublishThrough] with the holder's standing checked first (criterion 16).
func PublishThroughLive(s *Store, ps PeopleStatus, g Grant, p Publication) (*Note, error) {
	if err := AcceptGrant(ps, g); err != nil {
		return nil, err
	}
	return PublishThrough(s, g, p)
}

// SetVisibilityThroughLive is [SetVisibilityThrough] with the holder's standing checked first.
//
// Changing who can see a note is part of publishing it (#12 criterion 10a), so a departed person
// cannot do it — and, importantly, neither can anybody else on their behalf: [Store.SetVisibility]
// already refuses a non-author. The combination is criterion 5 from the other side: nobody widens or
// narrows a departed colleague's note after they have gone.
func SetVisibilityThroughLive(s *Store, ps PeopleStatus, g Grant, id NoteID, v Visibility) (*Note, error) {
	if err := AcceptGrant(ps, g); err != nil {
		return nil, err
	}
	return SetVisibilityThrough(s, g, id, v)
}

// EvaluateGrantRequestLive is [EvaluateGrantRequest] with the holder's standing checked FIRST.
//
// CRITERION 16'S SECOND HALF: "a grant that would allow it is refused when it is requested, not
// narrowed at the edge (§4.5)". So the standing check precedes the scope evaluation and precedes any
// issuance, and [Ledger.RequestLive] records nothing on refusal — the ledger of grants attributable
// to the person is unchanged.
func EvaluateGrantRequestLive(ps PeopleStatus, h Holder, requested []Scope) ([]Scope, error) {
	if err := CheckActive(ps, h.Person); err != nil {
		return nil, err
	}
	return EvaluateGrantRequest(h, requested)
}

// RequestLive is [Ledger.Request] for a person who may have left. It issues nothing on refusal.
func (l *Ledger) RequestLive(ps PeopleStatus, h Holder, requested []Scope) (Grant, error) {
	if err := CheckActive(ps, h.Person); err != nil {
		return Grant{}, err
	}
	return l.Request(h, requested)
}

// --- Whose notes, and how many ------------------------------------------------------------------

// AuthoredView is one note as the departed-colleague surfaces show it: what it is, who wrote it, and
// what is known about them.
//
// It carries no body. This is an index, and a listing that carried bodies would be a second way to
// read a note — a second way being a second place the gate has to be right.
type AuthoredView struct {
	Note NoteID
	// Title as it stands now.
	Title string
	// By is the attribution. Always populated; see [Attribution.Line].
	By Attribution
	// Current names the current version, so a reader can paste it straight into `omw note read`.
	Current VersionRef
	// Versions is how many points the timeline has. Never zero for a stored note, and it does not
	// shrink when the author leaves (criterion 2).
	Versions int
}

// Render writes one entry.
func (v AuthoredView) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "note: %s\n", string(v.Note))
	if v.Title != "" {
		fmt.Fprintf(&b, "title: %s\n", v.Title)
	}
	fmt.Fprint(&b, v.By.Render())
	fmt.Fprintf(&b, "current: %s\n", v.Current)
	fmt.Fprintf(&b, "versions: %d\n", v.Versions)
	return b.String()
}

// AuthoredListing is every note by one person that the reader may read, plus what could not be
// worked out.
type AuthoredListing struct {
	// Author is who was asked about.
	Author PersonID
	// AuthorState is what is known about them — the same three-valued answer every entry carries,
	// stated once at the top so a listing of zero notes still reports it (criterion 21: a genuine
	// zero is not silence).
	AuthorState tri.Value
	// Notes is what the reader may read, in publication order.
	Notes []AuthoredView
	// Undetermined names the notes whose readability could not be worked out. RETURNED, NOT
	// DROPPED, for the reason [Store.ListReadable] gives: a reader told "no notes" when the truth
	// is "I could not check three of them" has been handed a determined negative nobody determined.
	Undetermined []NoteID
}

// NotesBy lists the notes a person published that the reader may read.
//
// VISIBILITY IS SETTLED FIRST AND BY [Store.ListReadable], which calls [CanRead]. The author filter
// is applied to what came back, never instead of it — filtering first and gating second is how a
// listing ends up naming a note the reader may not see, which is §3.5's "ranking never surfaces the
// existence of something the searcher cannot read".
//
// Deactivation is not consulted when deciding what comes back. A departed person's notes are in this
// listing on exactly the same terms as a present person's; that is criteria 4 and 6, and it is true
// because there is no branch here that could make it false.
func NotesBy(s *Store, ps PeopleStatus, author, reader PersonID) AuthoredListing {
	out := AuthoredListing{Author: author, AuthorState: AuthorActive(ps, author)}
	readable, undetermined := s.ListReadable(reader)
	out.Undetermined = undetermined
	for _, n := range readable {
		if n.Author != author {
			continue
		}
		out.Notes = append(out.Notes, viewOf(n, ps))
	}
	return out
}

// viewOf builds an [AuthoredView] from a note. One place, so that a listing and a single read
// cannot disagree about how a note is described.
func viewOf(n *Note, ps PeopleStatus) AuthoredView {
	latest := n.Latest()
	return AuthoredView{
		Note:     n.ID,
		Title:    n.Title,
		By:       AttributionFor(n, ps),
		Current:  VersionRef{Note: n.ID, Number: latest.Number},
		Versions: len(n.Versions),
	}
}

// AttributedRead reads a note and returns it with its attribution.
//
// It is [Store.Read] plus [AttributionFor] and nothing else — criterion 11: a request for an
// archived note must not produce a not-found, an error, or an empty body, and the way to guarantee
// that is for the archived path to BE the ordinary path. A refusal for visibility and a note whose
// author is deactivated therefore come back as what they are: the first an error with
// ErrRefused's code, the second a note.
func AttributedRead(s *Store, ps PeopleStatus, id NoteID, reader PersonID) (*Note, Attribution, error) {
	n, err := s.Read(id, reader)
	if err != nil {
		return nil, Attribution{}, err
	}
	return n, AttributionFor(n, ps), nil
}

// AttributedVersion reads one point on the timeline with the note's attribution.
//
// CRITERION 13 IS THIS FUNCTION'S REASON TO EXIST SEPARATELY. Attribution is read from the NOTE, not
// from the version, so every version of a departed colleague's note shows the same departed author.
// The wrong shape — an author stamped onto each version at write time — is how the latest version
// says "deactivated" and the older ones say something else, and it is the same shape that would have
// put a visibility on a version.
func AttributedVersion(s *Store, ps PeopleStatus, ref VersionRef, reader PersonID) (Version, Attribution, error) {
	n, err := s.Read(ref.Note, reader)
	if err != nil {
		return Version{}, Attribution{}, err
	}
	v, verr := n.Version(ref.Number)
	if verr != nil {
		return Version{}, AttributionFor(n, ps), verr
	}
	return v, AttributionFor(n, ps), nil
}

// CorpusSummary is what the hub tells an agent about the corpus it may ground itself on (§3.5:
// what exists, how much, how recent).
//
// CRITERION 8: it counts archived notes the agent's person is permitted to read, and does not count
// archived notes they are not. Both halves fall out of counting [Store.ListReadable]'s first return
// value and nothing else — an agent that grounds on these numbers and then searches cannot find the
// corpus smaller than promised, because the numbers were produced by the same filter the search uses.
type CorpusSummary struct {
	// Notes is how many notes the reader may read.
	Notes int
	// Archived is how many of those were written by somebody who has left. It is a SUBSET of Notes,
	// not an addition to it, and the test asserts that.
	Archived int
	// AuthorsUndetermined is how many of those have an author whose state could not be worked out.
	// Also a subset. It is counted separately rather than folded into Archived, because folding is
	// exactly the collapse §4.3 forbids, arriving as an off-by-one in a statistic.
	AuthorsUndetermined int
	// Versions is how many versions across those notes. It only ever grows (§5.4).
	Versions int
	// Undetermined names the notes whose readability could not be worked out — not counted in
	// Notes, and not silently dropped either.
	Undetermined []NoteID
}

// Summarise counts the corpus as this reader may see it.
func Summarise(s *Store, ps PeopleStatus, reader PersonID) CorpusSummary {
	readable, undetermined := s.ListReadable(reader)
	out := CorpusSummary{Undetermined: undetermined}
	for _, n := range readable {
		out.Notes++
		out.Versions += len(n.Versions)
		switch AuthorActive(ps, n.Author) {
		case tri.No:
			out.Archived++
		case tri.Undetermined:
			out.AuthorsUndetermined++
		}
	}
	return out
}

// Render writes the summary. The archived count is stated even when it is zero, because "no
// archived notes you can read" and "we did not look" are different facts and an omitted line is the
// second one wearing the first one's clothes.
func (c CorpusSummary) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "notes you can read: %d\n", c.Notes)
	fmt.Fprintf(&b, "  of those, written by someone who has left: %d\n", c.Archived)
	fmt.Fprintf(&b, "  of those, author state could not be determined: %d\n", c.AuthorsUndetermined)
	fmt.Fprintf(&b, "versions across them: %d\n", c.Versions)
	fmt.Fprintf(&b, "notes whose readability could not be determined: %d\n", len(c.Undetermined))
	return b.String()
}

// --- References across a departure --------------------------------------------------------------

// RefIndex records that one note refers to another (PRD §3.4), in both directions.
//
// CRITERION 3 IS WHY IT IS HERE AND WHY IT IS SO SMALL. "Archival must not silently break existing
// references": a note by a still-active person that referenced an archived note must still resolve
// it, and the archived note must still list what referenced it. The way that breaks is not by
// anybody deleting an edge — it is by the resolver treating an archived target as absent. So this
// index does not know what an archive is, and resolution goes through [Store.Read], which asks
// [CanRead] and nothing else.
//
// Issue #14 owns references properly and will replace this with whatever it needs. What must survive
// that replacement is the property, not the type.
type RefIndex struct {
	out map[NoteID][]NoteID
	in  map[NoteID][]NoteID
}

// NewRefIndex returns an empty index.
func NewRefIndex() *RefIndex {
	return &RefIndex{out: map[NoteID][]NoteID{}, in: map[NoteID][]NoteID{}}
}

// Link records that from refers to to. Recording it twice records it once.
func (x *RefIndex) Link(from, to NoteID) {
	if x == nil || from == "" || to == "" || from == to {
		return
	}
	if !contains(x.out[from], to) {
		x.out[from] = append(x.out[from], to)
	}
	if !contains(x.in[to], from) {
		x.in[to] = append(x.in[to], from)
	}
}

func contains(ids []NoteID, want NoteID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// Resolution is what a reference resolved to, three-valued at the edges.
type Resolution struct {
	// Resolved are the referenced notes the reader may read.
	Resolved []AuthoredView
	// Refused are the referenced notes that exist and the reader may not read. They are NAMED as
	// refused rather than dropped, because "this note refers to something you cannot see" is a
	// different fact from "this note refers to nothing", and only one of them is true.
	Refused []NoteID
	// Undetermined are the referenced notes whose readability could not be worked out.
	Undetermined []NoteID
}

// Resolve answers "what does this note refer to", as this reader.
//
// An archived target resolves exactly as a live one does. There is no branch on the target's
// author's state, which is criterion 3 expressed as an absence of code.
func (x *RefIndex) Resolve(s *Store, ps PeopleStatus, from NoteID, reader PersonID) Resolution {
	if x == nil {
		return Resolution{}
	}
	return x.walk(s, ps, x.out[from], reader)
}

// Backlinks answers "what else was written about this", as this reader (§3.4). An archived note
// still lists what referenced it.
func (x *RefIndex) Backlinks(s *Store, ps PeopleStatus, to NoteID, reader PersonID) Resolution {
	if x == nil {
		return Resolution{}
	}
	return x.walk(s, ps, x.in[to], reader)
}

func (x *RefIndex) walk(s *Store, ps PeopleStatus, ids []NoteID, reader PersonID) Resolution {
	var out Resolution
	sorted := make([]NoteID, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, id := range sorted {
		n, err := s.Read(id, reader)
		switch {
		case err == nil:
			out.Resolved = append(out.Resolved, viewOf(n, ps))
		case Code(err) == ErrRefused.Code:
			out.Refused = append(out.Refused, id)
		case Code(err) == ErrNoSuchNote.Code:
			// A reference to a note the hub does not have. Not a departure, and not silence: it is
			// reported as undetermined rather than swallowed, because a dangling edge is something
			// the reader should be told about and nothing here established what happened to it.
			out.Undetermined = append(out.Undetermined, id)
		default:
			out.Undetermined = append(out.Undetermined, id)
		}
	}
	return out
}
