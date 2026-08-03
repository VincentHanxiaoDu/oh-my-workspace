package hub

// Corpus statistics — Issue #13. The shape of the corpus, so an agent grounds itself instead of
// guessing.
//
// # Why this file exists at all, and why it is not "search with the results removed"
//
// PRD §3.5: agents "search to ground themselves, and need corpus statistics — what exists, how
// much, how recent — to search well rather than guess." An agent that fires a query, gets three
// results and cannot tell whether that is the whole corpus on the topic or the thin edge of two
// hundred notes it phrased badly is guessing, and the guess is PRD §1's fourth diagnosis: it is in
// there somewhere and nobody can find it.
//
// # The leak arrives through the statistics door
//
// PRD §3.5 again: "ranking never surfaces the existence of something the searcher cannot read." A
// statistic is ranking's raw material, and HOW MUCH EXISTS is exactly the number that leaks
// existence. A count of 40 where the reader may read 12 has told them 28 things exist that they
// are not allowed to know exist.
//
// So every statistic in this file is a method on [Corpus] and reads c.notes and nothing else.
// [Corpus] is already visibility-settled — [Settle] filters through [Store.ListReadable], which
// calls [CanReadNote] — and its fields are unexported with no other constructor. There is no
// function here that takes a *[Store]. Computing a statistic over the raw store is not a mistake
// this file can make by reordering two lines: the wider set is not in scope.
//
// # Undetermined is not zero, and this is where that is easiest to get wrong
//
// PRD §4.3. `count: 0` says "I looked and there is nothing there", and an agent will build a plan
// on it. A statistic that could not be computed says nothing of the kind. The two are separate
// STATES OF A TYPE here, not a sentinel value: [Count], [Recency] and [Subjects] each carry a
// [tri.Value] whose ZERO IS UNDETERMINED, so a statistic nobody set cannot read as a determined
// zero, and each renders its third state in tri's one fixed wording.
//
// [Recency] uses all three of tri's values, and the third one is the point of criterion 13:
// tri.Yes is an instant, tri.No is a DETERMINED "there is no readable note in scope", and
// tri.Undetermined is "could not work it out". [Count] uses two — for a count, zero is a perfectly
// good determined number, so there is nothing for tri.No to mean and it is never set.
//
// # Partial determination is the normal case, not an edge
//
// Criterion 8: a request in which some statistics are determined and others are not returns both,
// labelled, in one response. That is why the three statistics have three DIFFERENT and independent
// determinacy rules (see [Corpus.Statistics]) rather than one shared flag. A note whose scope
// membership cannot be resolved makes the count undetermined — it may or may not belong — while
// recency stays determined if that note is older than the newest note we can see, because then it
// cannot change the answer whichever way it falls.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// RecencySemantics is the ONE definition of recency, stated once and identical at every scope.
//
// Criterion 14: recency is defined against a stated version semantics and that definition is
// stable across scopes rather than varying by scope. It is a constant rather than prose in three
// renderers so that "latest version at company scope, first version at group scope" is not a thing
// a later edit can produce without deleting this line.
const RecencySemantics = "recency is the timestamp of the LATEST VERSION of the most recently written readable note in scope (PRD §3.3); this definition does not vary by scope"

// UndeterminedToken is the one machine-readable spelling of the third value in statistics output.
//
// It is a token, not prose, because criterion 6 says a consumer must tell undetermined from zero
// BY INSPECTING THE OUTPUT ALONE. A consumer parsing `notes: 0` and `notes: undetermined` cannot
// collapse them; a consumer parsing a number with a footnote elsewhere can.
const UndeterminedToken = "undetermined"

// NoneToken is the determined "there is nothing here" spelling, deliberately distinct from
// [UndeterminedToken] in every rendering.
const NoneToken = "none"

// Statistics-specific refusals, declared here rather than in errors.go for the reason search.go
// gives: errors.go is Issue #12's merged work and two Issues adding an error must not edit the same
// line. TestStatisticsErrorsAreDistinguishable checks them against that file's list.
var (
	// ErrNoLocalStore — there is no local store or outbox on this machine to compute local corpus
	// statistics from.
	//
	// IT IS A DETERMINED FACT AND IT IS STILL NOT A ZERO. "There is nowhere to look" is not "I
	// looked and there is nothing" — the same distinction ErrNoHubConfigured draws for the hub half
	// — so it appears as the REASON on an undetermined statistic rather than as a count of 0. PRD
	// §4.2: omw does not conjure a store, so the honest answer is that local material is unknown.
	ErrNoLocalStore = &Error{Code: "no-local-store", Msg: "no local outbox here (a directory becomes one when it carries the .omw-outbox marker, which `omw note draft create` writes), so local corpus statistics could not be computed"}

	// ErrDaemonLivenessUndetermined — whether the daemon is running could not be established, so
	// nothing was established about the hub corpus either.
	//
	// IT IS NOT ErrDaemonNotRunning AND THAT IS THE WHOLE POINT (Issue #41, PRD §4.3). "The daemon
	// is not running" is a determined fact a person can act on by starting it; "I could not tell"
	// is not, and a surface that renders the second as the first has made a confident false
	// negative. Its Code is asserted equal to package commands' own constant for the third answer,
	// so the two surfaces cannot drift into two spellings of one state.
	ErrDaemonLivenessUndetermined = &Error{Code: "daemon-liveness-undetermined", Msg: "whether the daemon is running could not be determined, which is not a report that it is stopped"}
)

// statisticsErrors is this file's contribution to the pairwise-distinctness test.
var statisticsErrors = []*Error{ErrNoLocalStore, ErrDaemonLivenessUndetermined}

// notesByID resolves note ids to the notes themselves, for [SettleWith].
//
// It is a method on *Store defined in THIS file rather than in store.go, because it exists for one
// caller and one reason: statistics must be able to ask [Corpus.inScope] about a note whose
// READABILITY could not be determined, so that a narrowed scope does not report material sitting
// outside it. It performs no visibility check and it is unexported — the corpus it feeds keeps
// these notes out of the readable set exactly as before.
func (s *Store) notesByID(ids []NoteID) []*Note {
	if len(ids) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Note, 0, len(ids))
	for _, id := range ids {
		if n := s.notes[id]; n != nil {
			out = append(out, n)
		}
	}
	return out
}

// Count is how much: either a determined number or an explicit undetermined.
//
// THE ZERO VALUE IS UNDETERMINED. A Count nobody set has not been computed, and a struct field
// left alone by an error path must not read as "there are none". That is [tri]'s rule applied to a
// number, and it is why this is a struct with an unexported state rather than an int with -1
// meaning unknown: an int cannot refuse to be printed as a number.
type Count struct {
	state  tri.Value // tri.Yes: n holds a determined number. tri.Undetermined: it does not.
	n      int
	reason *Error
}

// DeterminedCount is a number that was actually computed. Zero is a fine value for it.
func DeterminedCount(n int) Count { return Count{state: tri.Yes, n: n} }

// UndeterminedCount is a count that could not be computed, carrying WHY.
//
// The reason is a *[Error] rather than a string so that a consumer reads a stable code — criterion
// 11 and 12 both turn on telling "no hub is configured" from "the hub could not be reached", and
// prose is translated and reworded while a code is not.
func UndeterminedCount(reason *Error) Count {
	if reason == nil {
		reason = ErrUndetermined
	}
	return Count{state: tri.Undetermined, reason: reason}
}

// Determined reports whether this count holds a real number.
func (c Count) Determined() bool { return c.state == tri.Yes }

// Value returns the number and whether it is one at all. The bool is not droppable: a caller that
// ignores it and uses the int has read an undetermined statistic as zero.
func (c Count) Value() (int, bool) { return c.n, c.state == tri.Yes }

// Reason is why the count is undetermined, or nil when it is determined.
func (c Count) Reason() *Error {
	if c.state == tri.Yes {
		return nil
	}
	if c.reason == nil {
		return ErrUndetermined
	}
	return c.reason
}

// Token is the short machine-readable rendering: a decimal number, or [UndeterminedToken].
func (c Count) Token() string {
	if c.state == tri.Yes {
		return fmt.Sprintf("%d", c.n)
	}
	return UndeterminedToken
}

// Render is what a person reads. The undetermined branch names its code, so criterion 6 holds on
// the rendered line by itself with no reference to an exit code or a second command.
func (c Count) Render() string {
	if c.state == tri.Yes {
		return fmt.Sprintf("%d", c.n)
	}
	r := c.Reason()
	return fmt.Sprintf("%s (%s: %s)", UndeterminedToken, r.Code, r.Msg)
}

// Recency is how recent: an instant, a determined "none", or undetermined.
//
// All three of [tri]'s values are used and all three mean something different here. Criterion 13
// is exactly the distinction between the second and the third.
type Recency struct {
	state  tri.Value // Yes: at holds an instant. No: determined none. Undetermined: not worked out.
	at     time.Time
	reason *Error
}

// DeterminedRecency is an instant drawn from a note the reader may read.
func DeterminedRecency(at time.Time) Recency { return Recency{state: tri.Yes, at: at.UTC()} }

// NoRecency is the DETERMINED answer that there is no readable note in scope. Criterion 13: it is
// a real answer and it renders distinguishably from undetermined recency.
func NoRecency() Recency { return Recency{state: tri.No} }

// UndeterminedRecency is recency that could not be worked out, carrying why.
func UndeterminedRecency(reason *Error) Recency {
	if reason == nil {
		reason = ErrUndetermined
	}
	return Recency{state: tri.Undetermined, reason: reason}
}

// Determined reports whether this is a real answer either way — an instant or a determined none.
func (r Recency) Determined() bool { return r.state.Determined() }

// At returns the instant and whether there is one.
func (r Recency) At() (time.Time, bool) { return r.at, r.state == tri.Yes }

// Reason is why recency is undetermined, or nil otherwise.
func (r Recency) Reason() *Error {
	if r.state.Determined() {
		return nil
	}
	if r.reason == nil {
		return ErrUndetermined
	}
	return r.reason
}

// Token is the machine-readable rendering: RFC3339, [NoneToken], or [UndeterminedToken].
func (r Recency) Token() string {
	switch r.state {
	case tri.Yes:
		return r.at.UTC().Format(time.RFC3339)
	case tri.No:
		return NoneToken
	default:
		return UndeterminedToken
	}
}

// Render is what a person reads.
func (r Recency) Render() string {
	switch r.state {
	case tri.Yes:
		return r.at.UTC().Format(time.RFC3339)
	case tri.No:
		return NoneToken + " (determined: there is no note in this scope you can read)"
	default:
		e := r.Reason()
		return fmt.Sprintf("%s (%s: %s)", UndeterminedToken, e.Code, e.Msg)
	}
}

// Subjects is what exists: which people and which groups have material in scope.
//
// IT CARRIES NO NOTE IDENTIFIERS, TITLES OR EXCERPTS, and that is criterion 15 rather than an
// oversight. A statistic that named a note would be a search result wearing a statistic's clothes,
// and it would be one obtained without the reader searching. The people and groups here are all
// drawn from notes in the settled corpus, so every one of them is something the reader could reach
// by searching the same scope as themselves.
type Subjects struct {
	state  tri.Value // Yes: the lists are complete. Undetermined: they may be missing somebody.
	people []PersonID
	groups []GroupID
	reason *Error
}

// DeterminedSubjects is a complete list. Empty is a determined answer — it renders as [NoneToken].
func DeterminedSubjects(people []PersonID, groups []GroupID) Subjects {
	p := append([]PersonID(nil), people...)
	g := append([]GroupID(nil), groups...)
	sort.Slice(p, func(i, j int) bool { return p[i] < p[j] })
	sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
	return Subjects{state: tri.Yes, people: p, groups: g}
}

// UndeterminedSubjects is a list that could not be completed, carrying why.
func UndeterminedSubjects(reason *Error) Subjects {
	if reason == nil {
		reason = ErrUndetermined
	}
	return Subjects{state: tri.Undetermined, reason: reason}
}

// Determined reports whether the lists are complete.
func (s Subjects) Determined() bool { return s.state == tri.Yes }

// People returns a copy of the people with material, and whether the list is determined.
func (s Subjects) People() ([]PersonID, bool) {
	return append([]PersonID(nil), s.people...), s.state == tri.Yes
}

// Groups returns a copy of the groups with material, and whether the list is determined.
func (s Subjects) Groups() ([]GroupID, bool) {
	return append([]GroupID(nil), s.groups...), s.state == tri.Yes
}

// Reason is why the subjects are undetermined, or nil otherwise.
func (s Subjects) Reason() *Error {
	if s.state == tri.Yes {
		return nil
	}
	if s.reason == nil {
		return ErrUndetermined
	}
	return s.reason
}

// Token is the machine-readable rendering.
func (s Subjects) Token() string {
	if s.state != tri.Yes {
		return UndeterminedToken
	}
	var parts []string
	for _, p := range s.people {
		parts = append(parts, "person:"+string(p))
	}
	for _, g := range s.groups {
		parts = append(parts, "group:"+string(g))
	}
	if len(parts) == 0 {
		return NoneToken
	}
	return strings.Join(parts, " ")
}

// Render is what a person reads.
func (s Subjects) Render() string {
	if s.state != tri.Yes {
		e := s.Reason()
		return fmt.Sprintf("%s (%s: %s)", UndeterminedToken, e.Code, e.Msg)
	}
	if len(s.people) == 0 && len(s.groups) == 0 {
		return NoneToken + " (determined: nothing in this scope that you can read)"
	}
	return s.Token()
}

// Statistics is the shape of one corpus at one scope for one reader.
//
// EVERY FIELD IS INDEPENDENTLY READABLE AND INDEPENDENTLY CAPABLE OF BEING UNDETERMINED —
// criterion 3 and criterion 8. There is no single "ok" flag, because one flag is how a partially
// determined answer becomes a wholly failed one.
type Statistics struct {
	// Scope is which of the three subjects these statistics are about.
	Scope SearchScope
	// Reader is whose readable set they were computed over. Criterion 4: two readers with
	// different visibility get different numbers, and the number says who it belongs to.
	Reader PersonID
	// Notes is how much.
	Notes Count
	// Subjects is what exists.
	Subjects Subjects
	// Recency is how recent.
	Recency Recency
	// UndeterminedNotes is how many notes could not be evaluated for readability at all.
	//
	// It is NEVER folded into Notes — a note whose readability is unknown is not a note the reader
	// may read — and never silent, which is why [Corpus.UndeterminedIDs] returns them separately.
	// It counts only notes whose READABILITY is undetermined; a note determined to be unreadable
	// never reaches the corpus and is not counted here, so this number does not move when
	// unreadable material is added (criterion 5).
	UndeterminedNotes Count
	// Coverage is tri.Yes only when every statistic above is determined and nothing was skipped.
	Coverage tri.Value
}

// Statistics computes the corpus statistics for one scope, over the settled corpus and nothing
// wider.
//
// THE ONLY SOURCE OF NOTES IS c.notes. There is no *[Store] parameter and no package-level store
// to reach for, so "count the whole corpus and subtract" is not reachable from here.
//
// The three determinacy rules, which are deliberately different from one another so that criterion
// 8's partial determination is a real state and not a theoretical one:
//
//   - NOTES is determined when every note in the corpus was decided in-or-out of scope and no
//     note's readability was unknown. One unresolvable membership and the count is undetermined,
//     because it might belong.
//   - RECENCY is determined when no note we could not place could possibly change it — that is,
//     when every unplaceable note is OLDER than the newest note we can see. Then it does not
//     matter which way it falls. This is what makes an undetermined count with a determined
//     recency reachable.
//   - SUBJECTS is determined when every unplaceable note's author, and its group if it has one, is
//     already on the list. Then including it would add nobody.
//
// A note whose READABILITY is unknown carries no information at all — the corpus holds only its
// id — so it makes all three undetermined. That is not a leak: it is a count of notes the hub
// could not evaluate, and it is unmoved by notes determined to be unreadable.
func (c Corpus) Statistics(s SearchScope) (Statistics, error) {
	// Criterion 2: a scope the hub does not know is REFUSED, not widened to company and not
	// narrowed to nothing. Same resolver search uses, so the two cannot drift apart.
	// THE DOOR, AND IT IS SHUT BEFORE ANYTHING IS COUNTED.
	//
	// An unidentified requester is NOT a requester whose readability is undetermined; it is a
	// request that names nobody, and those are different facts. Letting it through produced a real
	// oracle: [CanRead] answers Undetermined for an empty reader, so every note in the store lands
	// in c.undetermined, and a DETERMINED count of them is a determined count of the whole store —
	// "a count of 40 where I can read 12 has told me 28 things exist that I'm not allowed to know
	// exist", arriving through the undetermined-notes door. A count is not made safe by being a
	// count rather than a list of ids: here the count IS the disclosure, because it tracks corpus
	// size. So this is refused at the door rather than softened downstream.
	if strings.TrimSpace(string(c.reader)) == "" {
		return Statistics{}, Refusedf(ErrNotSignedIn,
			"corpus statistics are computed over one identity's readable set, and this request names none")
	}
	if err := c.resolveScope(s); err != nil {
		return Statistics{}, err
	}

	// The unevaluable notes, RESTRICTED TO THE SCOPE THAT WAS ASKED ABOUT.
	//
	// A store-wide figure at a narrowed scope tells the asker that material exists outside it, and
	// criterion 5 says a scope they cannot see into must be indistinguishable from one that is
	// genuinely empty. A note whose readability is unknown but whose scope membership is a
	// determined NO cannot be in this scope however its readability falls, so it is not counted
	// here. Anything else — in scope, or unplaceable — is.
	unevaluable := 0
	for _, n := range c.unevaluableNotes() {
		if c.inScope(n, s) != tri.No {
			unevaluable++
		}
	}

	st := Statistics{
		Scope:             s,
		Reader:            c.reader,
		UndeterminedNotes: DeterminedCount(unevaluable),
	}
	unknownReadability := unevaluable > 0

	var (
		inScope     []*Note
		unplaceable []*Note
	)
	for _, n := range c.notes {
		switch c.inScope(n, s) {
		case tri.Yes:
			inScope = append(inScope, n)
		case tri.No:
			// determined not in scope; contributes to nothing
		default:
			unplaceable = append(unplaceable, n)
		}
	}

	// --- how much ---
	if unknownReadability || len(unplaceable) > 0 {
		st.Notes = UndeterminedCount(ErrUndetermined)
	} else {
		st.Notes = DeterminedCount(len(inScope))
	}

	// --- how recent ---
	var newest time.Time
	for _, n := range inScope {
		if at := n.Latest().At; at.After(newest) {
			newest = at
		}
	}
	switch {
	case unknownReadability:
		st.Recency = UndeterminedRecency(ErrUndetermined)
	case couldBeNewer(unplaceable, newest, len(inScope) > 0):
		st.Recency = UndeterminedRecency(ErrUndetermined)
	case len(inScope) == 0:
		// CRITERION 13. A determined "there is nothing readable here", not a zero instant and not
		// an undetermined one.
		st.Recency = NoRecency()
	default:
		st.Recency = DeterminedRecency(newest)
	}

	// --- what exists ---
	people, groups := subjectsOf(inScope)
	switch {
	case unknownReadability, addsASubject(unplaceable, people, groups):
		st.Subjects = UndeterminedSubjects(ErrUndetermined)
	default:
		st.Subjects = DeterminedSubjects(people, groups)
	}

	st.Coverage = tri.Yes
	if !st.Notes.Determined() || !st.Recency.Determined() || !st.Subjects.Determined() || unknownReadability {
		st.Coverage = tri.Undetermined
	}
	return st, nil
}

// unevaluableNotes are the notes whose readability could not be determined, as notes.
//
// If the pointer list and the id list ever disagree in length — a corpus built by some future path
// that fills one and not the other — this refuses to under-report: it is better to count a note
// that turned out to be elsewhere than to tell somebody a scope is clean when nothing established
// that. It returns nil only when there is genuinely nothing to count.
func (c Corpus) unevaluableNotes() []*Note {
	if len(c.undeterminedNotes) == len(c.undetermined) {
		return c.undeterminedNotes
	}
	out := make([]*Note, 0, len(c.undetermined))
	out = append(out, c.undeterminedNotes...)
	for len(out) < len(c.undetermined) {
		// A note we cannot even look at is one we cannot place, so it counts everywhere.
		out = append(out, &Note{})
	}
	return out
}

// couldBeNewer reports whether a note we could not place in scope might be more recent than the
// newest note we could. With nothing placed at all, any unplaceable note could be the answer.
func couldBeNewer(unplaceable []*Note, newest time.Time, haveOne bool) bool {
	for _, n := range unplaceable {
		if !haveOne || n.Latest().At.After(newest) {
			return true
		}
	}
	return false
}

// subjectsOf collects the distinct authors and audience groups of a set of notes.
func subjectsOf(notes []*Note) ([]PersonID, []GroupID) {
	seenP := map[PersonID]bool{}
	seenG := map[GroupID]bool{}
	var people []PersonID
	var groups []GroupID
	for _, n := range notes {
		if !seenP[n.Author] {
			seenP[n.Author] = true
			people = append(people, n.Author)
		}
		if n.Visibility.Kind() == KindGroup {
			if g := n.Visibility.Group(); !seenG[g] {
				seenG[g] = true
				groups = append(groups, g)
			}
		}
	}
	return people, groups
}

// addsASubject reports whether an unplaceable note could add a person or a group not already on
// the determined lists.
func addsASubject(unplaceable []*Note, people []PersonID, groups []GroupID) bool {
	has := map[PersonID]bool{}
	for _, p := range people {
		has[p] = true
	}
	hasG := map[GroupID]bool{}
	for _, g := range groups {
		hasG[g] = true
	}
	for _, n := range unplaceable {
		if !has[n.Author] {
			return true
		}
		if n.Visibility.Kind() == KindGroup && !hasG[n.Visibility.Group()] {
			return true
		}
	}
	return false
}

// UndeterminedStatistics is a whole half of a report that could not be computed at all, with one
// reason on every statistic.
//
// IT IS NOT AN EMPTY [Statistics], and that is the point of having a constructor. A zero-valued
// half would carry no reason and no reader, and a later field added to Statistics would default to
// its own zero rather than to undetermined. Criterion 12 turns on the reason surviving: "could not
// reach the hub" and "the hub reports nothing readable here" are the same shape of answer with
// different reasons, and only the reason tells them apart.
func UndeterminedStatistics(scope SearchScope, reader PersonID, reason *Error) Statistics {
	if reason == nil {
		reason = ErrUndetermined
	}
	return Statistics{
		Scope:             scope,
		Reader:            reader,
		Notes:             UndeterminedCount(reason),
		Subjects:          UndeterminedSubjects(reason),
		Recency:           UndeterminedRecency(reason),
		UndeterminedNotes: UndeterminedCount(reason),
		Coverage:          tri.Undetermined,
	}
}

// StatisticsThrough is [Corpus.Statistics] behind a grant. PRD §4.5: refused when requested, never
// narrowed at the edge — a grant without `read` does not get a smaller set of statistics, it gets
// a refusal.
func StatisticsThrough(s *Store, g Grant, reader PersonID, scope SearchScope) (Statistics, error) {
	if !Permits(g.Scopes, ScopeRead) {
		return Statistics{}, Refusedf(ErrReadScopeRequired,
			"reading corpus statistics needs %q; this grant holds %s", string(ScopeRead), describeScopes(g.Scopes))
	}
	if reader == "" {
		reader = g.Holder
	}
	// REFUSED BEFORE SETTLING. Grant is a plain struct with exported fields, so a holder left at
	// its zero value is exactly the "field somebody forgot to fill" case — and with both the reader
	// and the holder empty the identity check below is vacuously satisfied. This is the outer of
	// two layers; [Corpus.Statistics] shuts the same door again for any caller that settles a
	// corpus without going through a grant at all.
	if strings.TrimSpace(string(reader)) == "" {
		return Statistics{}, Refusedf(ErrNotSignedIn,
			"corpus statistics are computed over one identity's readable set, and this grant names none")
	}
	if reader != g.Holder {
		return Statistics{}, Refusedf(ErrGrantWiderThanHolder,
			"this grant acts as %q and cannot read statistics as %q", string(g.Holder), string(reader))
	}
	return Settle(s, reader).Statistics(scope)
}

// Report is the whole answer to one statistics request: the local half and the hub half.
//
// THE TWO HALVES ARE SEPARATE BECAUSE PRD §4.4 SAYS THE LOCAL HALF STANDS ALONE. With no hub
// configured the local statistics are determined values and the hub statistics are undetermined
// with `no-hub-configured` as the reason — criterion 11, which is unsatisfiable by any shape that
// has one set of numbers. Neither half is ever omitted, which is criterion 7: undetermined is
// present and labelled, not silence.
type Report struct {
	Scope SearchScope
	Local Statistics
	Hub   Statistics
}

// Determined reports whether every statistic in both halves is a real answer.
func (r Report) Determined() bool {
	return r.Local.Coverage == tri.Yes && r.Hub.Coverage == tri.Yes
}

// Render is the output a person reads, and the bytes criteria 6 and 7 are asserted on.
//
// EVERY LINE IS UNCONDITIONAL. A field that appeared only when it was determined would make its
// ABSENCE the signal, and criterion 7 forbids exactly that: an undetermined statistic is present,
// carrying its marker, in the same place a determined one would be.
func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scope: %s\n", r.Scope.Token())
	fmt.Fprintf(&b, "recency semantics: %s\n", RecencySemantics)
	renderHalf(&b, "local", r.Local)
	renderHalf(&b, "hub", r.Hub)
	if !r.Determined() {
		b.WriteString("some statistics could not be determined. An undetermined statistic is NOT a zero: nothing has been established about what is or is not there.\n")
	}
	return b.String()
}

func renderHalf(b *strings.Builder, name string, s Statistics) {
	fmt.Fprintf(b, "%s:\n", name)
	fmt.Fprintf(b, "  reader: %s\n", readerToken(s.Reader))
	fmt.Fprintf(b, "  notes: %s\n", s.Notes.Render())
	fmt.Fprintf(b, "  subjects: %s\n", s.Subjects.Render())
	fmt.Fprintf(b, "  recency: %s\n", s.Recency.Render())
	fmt.Fprintf(b, "  notes whose readability could not be determined: %s\n", s.UndeterminedNotes.Render())
	fmt.Fprintf(b, "  coverage: %s\n", s.Coverage.Render("complete", "incomplete"))
}

func readerToken(p PersonID) string {
	if strings.TrimSpace(string(p)) == "" {
		return UndeterminedToken
	}
	return string(p)
}

// --- the agent API half ------------------------------------------------------------------------

// statJSON is one statistic on the wire. STATE IS ALWAYS PRESENT AND VALUE IS ALWAYS PRESENT —
// criterion 7 again, in JSON, where the temptation to `omitempty` an undetermined field is
// strongest and where a missing key parses as absence rather than as unknown.
type statJSON struct {
	State  string `json:"state"`
	Value  any    `json:"value"`
	Reason string `json:"reason"`
}

func countJSON(c Count) statJSON {
	if n, ok := c.Value(); ok {
		return statJSON{State: "determined", Value: n, Reason: ""}
	}
	return statJSON{State: UndeterminedToken, Value: nil, Reason: c.Reason().Code}
}

func recencyJSON(r Recency) statJSON {
	switch {
	case !r.Determined():
		return statJSON{State: UndeterminedToken, Value: nil, Reason: r.Reason().Code}
	default:
		if at, ok := r.At(); ok {
			return statJSON{State: "determined", Value: at.UTC().Format(time.RFC3339), Reason: ""}
		}
		return statJSON{State: "determined", Value: NoneToken, Reason: ""}
	}
}

func subjectsJSON(s Subjects) statJSON {
	if !s.Determined() {
		return statJSON{State: UndeterminedToken, Value: nil, Reason: s.Reason().Code}
	}
	people, _ := s.People()
	groups, _ := s.Groups()
	out := []string{}
	for _, p := range people {
		out = append(out, "person:"+string(p))
	}
	for _, g := range groups {
		out = append(out, "group:"+string(g))
	}
	return statJSON{State: "determined", Value: out, Reason: ""}
}

type statisticsJSON struct {
	Reader            string   `json:"reader"`
	Notes             statJSON `json:"notes"`
	Subjects          statJSON `json:"subjects"`
	Recency           statJSON `json:"recency"`
	UndeterminedNotes statJSON `json:"undetermined_notes"`
	Coverage          string   `json:"coverage"`
}

type reportJSON struct {
	Scope            string         `json:"scope"`
	RecencySemantics string         `json:"recency_semantics"`
	Local            statisticsJSON `json:"local"`
	Hub              statisticsJSON `json:"hub"`
}

func halfJSON(s Statistics) statisticsJSON {
	return statisticsJSON{
		Reader:            readerToken(s.Reader),
		Notes:             countJSON(s.Notes),
		Subjects:          subjectsJSON(s.Subjects),
		Recency:           recencyJSON(s.Recency),
		UndeterminedNotes: countJSON(s.UndeterminedNotes),
		Coverage:          s.Coverage.String(),
	}
}

// JSON is the agent API's rendering of the very same [Report] the CLI prints.
//
// CRITERION 9 IS A PROPERTY OF THERE BEING ONE VALUE. The CLI and the agent API do not each
// compute statistics; they each render a Report that was computed once, so "the same statistics,
// including which are undetermined" is true by construction and the test that drives it is
// checking that nobody has added a second computation.
func (r Report) JSON() (string, error) {
	b, err := json.MarshalIndent(reportJSON{
		Scope:            r.Scope.Token(),
		RecencySemantics: RecencySemantics,
		Local:            halfJSON(r.Local),
		Hub:              halfJSON(r.Hub),
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// StatisticsTool is the agent API's description of this capability.
//
// IT NAMES `read` AND NOTHING ELSE. The scope vocabulary is exactly read / write / publish (PRD
// §4.5) and statistics added no fourth one — there is no `stats` scope and no admin scope. The
// hub operator's ability to read everything published to it is a DEPLOYMENT FACT (§2.4), stated in
// the PRD, and it is not a capability anybody can be granted here.
func StatisticsTool() ToolSchema {
	return ToolSchema{
		Tool: "corpus_statistics",
		Description: "The shape of the corpus you may read: what exists, how much, and how recent. " +
			"Every statistic is either a determined value or the explicit undetermined marker; an " +
			"undetermined statistic is never rendered as zero. " + RecencySemantics,
		Scopes: scopeStrings(ScopeRead),
		Fields: []FieldSchema{{
			Name:     "scope",
			Type:     "string",
			Required: false,
			Default:  "company",
			Enum:     []string{"company", "person:<id>", "group:<id>"},
			Description: "which corpus to describe. Any other value is refused as unknown-search-scope: " +
				"a scope the hub has no record of is neither widened to the company nor narrowed to an " +
				"empty answer.",
		}},
	}
}

// StatsAPISchema is this capability's contribution to the agent API surface. It is its own
// function rather than an edit to [AgentAPISchema] so that two Issues adding tools never touch the
// same line.
func StatsAPISchema() []ToolSchema { return []ToolSchema{StatisticsTool()} }
