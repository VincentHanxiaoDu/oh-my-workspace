// Merging scattered traffic about one problem into a single ticket, and taking it apart again
// (PRD §2.3, §3.2, §3.14, §4.3, §5.4; Issue #7).
//
// THE SENTENCE THIS FILE IMPLEMENTS. "Five emails, a chat thread and a follow-up ping about one
// broken login are ONE ticket" — the package comment already says so; this is the operation that
// makes it true of a person's actual inbox. Everything below follows from two demands in §3.2 that
// pull against each other, and the tension is the whole design:
//
//   - EVERY MERGE IS REVERSIBLE, EXACTLY. Not approximately, not "a ticket with the same title".
//     Unmerging restores each source with the content it had, and that is checkable by comparing
//     against a snapshot taken before the merge. So the merge record keeps the source's stored bytes
//     VERBATIM — the very payload [Put] wrote — and unmerge writes those bytes back. Nothing is
//     re-derived, re-encoded or reconstructed from parts, because a reconstruction is only as exact
//     as the last person to add a field to [Ticket] remembered to make it.
//   - A MERGED-THEN-UNMERGED TICKET IS DISTINGUISHABLE FROM ONE THAT WAS NEVER MERGED (criterion 6).
//     Which sounds like the opposite of the above, and is not: the two facts live in different
//     places. The TICKET is restored byte-for-byte; the fact that it was merged and unmerged is a
//     separate record, [UnmergedKind], which the ticket's rendering reads and shows. Content
//     equality holds and the two situations still never render identically. Putting the trace inside
//     the ticket would have broken exact restoration; leaving it nowhere would have broken
//     criterion 6.
//
// A MERGE IS ONE WRITE OR NONE (criterion 10). It writes the merged ticket, writes the merge record
// and deletes N source tickets, and those N+2 changes go through [store.Store.Apply] as one batch.
// See store/batch.go for why that had to be in the store: no ordering of individual writes can keep
// a killed process from leaving the inbox half-merged, and the reader who must never see the half
// state is `omw inbox list`, which knows nothing about merging.
//
// NO PRIORITY, STILL (criterion 11). Nothing here adds a rank, and nothing here can turn a piece of
// traffic that was never a ticket into one: [Merge] takes IDENTIFIERS OF STORED TICKETS, resolves
// them with [Get], and has no parameter through which raw traffic could enter. The merged ticket is
// itself written through [Put], so it faces the same refusal an acknowledgement always faces. There
// is no low-priority shelf for a merged-in "ok" because there is no shelf.
//
// NOTHING EXPIRES (§5.4, criterion 16). No function here reads the clock to decide what exists. A
// merge record carries when the merge happened so a person can see it, and is removed only by an
// unmerge or by the person deleting it.

package inbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// MergeKind is the store kind a merge record is written under: what was folded in, where each piece
// came from, why, and the bytes needed to put it all back.
const MergeKind = store.Kind("ticket-merge")

// UnmergedKind is the store kind that records that a particular ticket was once merged away and has
// since been restored. It is keyed by the RESTORED TICKET's identifier, which is what lets criterion
// 6 be answered by inspecting that ticket rather than by knowing to go looking for a merge.
const UnmergedKind = store.Kind("ticket-unmerged")

// mergeFormat and unmergedFormat are the on-disk envelope versions, inside the store's own.
const (
	mergeFormat    = 1
	unmergedFormat = 1
)

// Errors this file returns, as distinct errors.Is-able values. Criteria 8 and 9 are stated as "fails
// in a way distinguishable from success by exit status alone", and the CLI can only choose a code
// from a value.
var (
	// ErrTooFewInputs means fewer than two tickets were named. Merging one thing is not a merge, and
	// criterion 8 requires it not silently produce a merged ticket.
	ErrTooFewInputs = errors.New("a merge needs two or more tickets")

	// ErrRepeatedInput means the same ticket was named twice in one merge. Folding a thing into
	// itself would write one snapshot over the other and make the merge unreversible in exactly the
	// silent way criterion 5 forbids.
	ErrRepeatedInput = errors.New("the same ticket was named more than once")

	// ErrTicketIDTaken means the merged ticket's identifier is already in the inbox and is not one of
	// the tickets being merged. Overwriting it would destroy a ticket nobody asked to touch.
	ErrTicketIDTaken = errors.New("a different ticket already has that identifier")

	// ErrNotWritten means the merged ticket has no written title, or no written summary, or a summary
	// that is a bare concatenation of the source titles (§3.2, criterion 3). "not five items titled
	// `yes`, `ok` and `Hii`" is not satisfied by one item titled "yes ok Hii".
	ErrNotWritten = errors.New("a merged ticket needs a written title and a written summary")

	// ErrNotMerged means the ticket named has no merge record: it was never merged, so there is
	// nothing to take apart (criterion 9).
	ErrNotMerged = errors.New("this ticket is not a merged ticket")

	// ErrAlreadyMerged means a merge record already exists under that identifier.
	ErrAlreadyMerged = errors.New("a merge already exists under that identifier")

	// ErrIncompleteWorking means a merge record has an input that does not carry all of what it was,
	// where it came from, and why it was merged — criterion 4, which calls that a failure rather than
	// a blank line. NOTE WHAT IT IS NOT: a field recorded as [Undetermined] is COMPLETE. An origin
	// that could not be resolved and a "why" that was not recorded are undetermined and render as
	// such (criterion 12); this error is for a field that is not in the record at all.
	ErrIncompleteWorking = errors.New("this merge does not show its working")

	// ErrUnreadableMerge means a merge record is present and cannot be understood — distinct from
	// ErrNotMerged for the reason the store distinguishes unreadable from absent.
	ErrUnreadableMerge = errors.New("this merge record cannot be read")
)

// MergeInput is one thing that was folded into a merged ticket, and is criterion 4 as a type: (a)
// what it was, (b) which channel and which source it came from, (c) why it was merged.
//
// THE FOUR FIELDS ARE [Field] AND NOT string, and that is criterion 12. An origin that could not be
// resolved and a reason nobody recorded are UNDETERMINED — a state that renders as its own sentence,
// distinguishable both from a real value and from a recorded absence. A string could only have
// offered "" for all three of those, which is the blank criterion 12 forbids.
type MergeInput struct {
	// TicketID is the identifier the source ticket had, and will have again after an unmerge.
	TicketID string
	// What is what the thing was, taken from the source ticket at the moment of the merge.
	What Field
	// Channel is where it reached the person — "email", "teams". Undetermined when the source
	// ticket's own channel could not be determined; criterion 12 requires that be visible.
	Channel Field
	// Source is the identifier the piece had in that channel. Undetermined when nothing recorded one.
	Source Field
	// Why is why this was folded in. Undetermined when the person did not say; never blank.
	Why Field
	// Snapshot is the source ticket's stored payload, VERBATIM, as it stood immediately before the
	// merge. It is what makes criterion 5 exact rather than best-effort. Nothing reads it as a
	// structure; unmerge hands these bytes straight back to the store.
	Snapshot []byte
}

// MergeRecord is one merge: the ticket it produced, everything folded into it, and when.
type MergeRecord struct {
	// ID is the merged ticket's identifier. A merge record and its ticket share an identifier so
	// that "is this ticket a merge" is one lookup and not a scan.
	ID string
	// Inputs are the things folded in, in the order they were named.
	Inputs []MergeInput
	// Merged is when the merge happened. IT IS NOT AN EXPIRY CLOCK — §5.4 is ruled, and nothing in
	// this file compares it to the present. The zero value renders as undetermined.
	Merged time.Time
}

// MergedRender is when the merge happened, as a person reads it, with the zero time rendered as
// undetermined rather than as the first second of 1970.
func (m MergeRecord) MergedRender() string {
	if m.Merged.IsZero() {
		return tri.Undetermined.Render("", "")
	}
	return m.Merged.UTC().Format(time.RFC3339)
}

// Unmerged is the trace criterion 6 asks for: this ticket was folded into a merged ticket, and the
// merge was undone. It is a record ABOUT a restored ticket and is deliberately not part of the
// ticket, so that the ticket itself comes back byte-identical (criterion 5).
type Unmerged struct {
	// TicketID is the restored ticket.
	TicketID string
	// MergedInto is the identifier the merged ticket had.
	MergedInto string
	// Merged and Undone are when the merge happened and when it was taken apart. Zero renders as
	// undetermined; neither is ever compared to the present.
	Merged time.Time
	Undone time.Time
	// Alongside is how many other things were in that merge.
	Alongside int
}

// MergeSpec is what a person asks for. It is separate from [MergeRecord] because a person supplies
// intent — a title, a summary, reasons — and the record additionally holds the snapshots and the
// findings this package made, which are not a person's to state.
type MergeSpec struct {
	// ID is the merged ticket's identifier.
	ID string
	// Title and Summary are the written statement of the one problem. Both must be written and
	// non-empty (criterion 3).
	Title   Field
	Summary Field
	// Channel is the merged ticket's own channel field. A merge crosses channels by design (§3.2), so
	// this is usually undetermined or a written note that it came from several; it is a [Field] and
	// this package does not choose its wording.
	Channel Field
	// Inputs name the tickets to fold in, in the order the person named them.
	Inputs []InputSpec
	// When is the moment to record. Zero means the caller did not say, and renders as undetermined —
	// this package does not reach for the clock on a caller's behalf.
	When time.Time
}

// InputSpec is one ticket to fold in, plus what the person can say about it that the ticket cannot.
type InputSpec struct {
	// TicketID is the ticket to fold in. It must already be in the inbox.
	TicketID string
	// Source is the identifier this piece had in its channel, when the person knows it. The ZERO
	// VALUE IS UNDETERMINED and is recorded as such — §4.3 through [Field].
	Source Field
	// Why is why this is being folded in. The zero value is undetermined and is recorded as such:
	// criterion 12 names "a merge whose 'why' was not recorded" as exactly the case that must render
	// undetermined rather than blank.
	Why Field
}

// Merge folds two or more tickets into one, atomically, and records everything needed to undo it.
//
// It returns the merged ticket. Every refusal below happens BEFORE anything is written, so a merge
// that fails leaves the inbox exactly as it was (criterion 8).
func Merge(s *store.Store, spec MergeSpec) (Ticket, error) {
	if len(spec.Inputs) < 2 {
		return Ticket{}, fmt.Errorf("%w: %d named", ErrTooFewInputs, len(spec.Inputs))
	}
	seen := map[string]bool{}
	for _, in := range spec.Inputs {
		if seen[in.TicketID] {
			return Ticket{}, fmt.Errorf("%w: %q", ErrRepeatedInput, in.TicketID)
		}
		seen[in.TicketID] = true
	}
	if !validID(spec.ID) {
		return Ticket{}, fmt.Errorf("%w: %q is not usable as a ticket identifier", ErrInvalidTicket, spec.ID)
	}
	title, titleWritten := spec.Title.Value()
	summary, summaryWritten := spec.Summary.Value()
	if !titleWritten || strings.TrimSpace(title) == "" {
		return Ticket{}, fmt.Errorf("%w: the title is %s", ErrNotWritten, spec.Title.Render())
	}
	if !summaryWritten || strings.TrimSpace(summary) == "" {
		return Ticket{}, fmt.Errorf("%w: the summary is %s", ErrNotWritten, spec.Summary.Render())
	}

	// The merged identifier must not silently overwrite a bystander. It MAY be one of the inputs:
	// folding five things into the identifier of the first is a reasonable thing to want, and the
	// batch's puts-before-deletes ordering is not relied on for it — the input's delete is dropped
	// below instead, which is the version that does not depend on an ordering rule staying put.
	if !seen[spec.ID] {
		if _, err := Get(s, spec.ID); err == nil {
			return Ticket{}, fmt.Errorf("%w: %q", ErrTicketIDTaken, spec.ID)
		} else if !errors.Is(err, ErrNoSuchTicket) {
			return Ticket{}, err
		}
	}
	if _, err := LoadMerge(s, spec.ID); err == nil {
		return Ticket{}, fmt.Errorf("%w: %q", ErrAlreadyMerged, spec.ID)
	} else if !errors.Is(err, ErrNotMerged) {
		return Ticket{}, err
	}

	inputs := make([]MergeInput, 0, len(spec.Inputs))
	sourceTitles := make([]string, 0, len(spec.Inputs))
	var earliest time.Time
	for _, in := range spec.Inputs {
		// THE SNAPSHOT IS THE STORED PAYLOAD, NOT A RE-ENCODING OF A DECODED TICKET. Criterion 5 is
		// checkable against what was there, so what is kept is what was there.
		rec, err := s.Get(Kind, in.TicketID)
		if err != nil {
			if errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, store.ErrInvalidName) {
				return Ticket{}, fmt.Errorf("%w: %q", ErrNoSuchTicket, in.TicketID)
			}
			return Ticket{}, err
		}
		t, err := decode(in.TicketID, rec.Data)
		if err != nil {
			return Ticket{}, err
		}
		snapshot := make([]byte, len(rec.Data))
		copy(snapshot, rec.Data)
		if v, ok := t.Title.Value(); ok {
			sourceTitles = append(sourceTitles, v)
		}
		if !t.Arrived.IsZero() && (earliest.IsZero() || t.Arrived.Before(earliest)) {
			earliest = t.Arrived
		}
		inputs = append(inputs, MergeInput{
			TicketID: in.TicketID,
			What:     whatItWas(t),
			// FAITHFULLY COPIED, ALL FOUR STATES. A source whose channel was recorded as undetermined
			// stays undetermined in the merge's working — criterion 12 — and one that was recorded as
			// having none stays a recorded absence. Flattening either into the other here would be
			// the exact collapse §4.3 is about, performed by the code that reports on it.
			Channel:  t.Channel,
			Source:   in.Source,
			Why:      in.Why,
			Snapshot: snapshot,
		})
	}

	// CRITERION 3, THE SECOND HALF. A summary that is the source titles run together is not a
	// written summary; it is the five items the PRD says a person must not be left with, on one line.
	if isBareConcatenation(summary, sourceTitles) {
		return Ticket{}, fmt.Errorf("%w: the summary is the source titles run together, which is the "+
			"list of fragments the merge was supposed to replace", ErrNotWritten)
	}

	merged := Ticket{
		ID:      spec.ID,
		Title:   spec.Title,
		Summary: spec.Summary,
		Channel: spec.Channel,
		// The earliest determined arrival among the inputs: the problem has been owed since the first
		// piece of it landed, not since the person got round to merging. Undetermined when no input
		// had a determined arrival — never "now", which would be a fact nobody established.
		Arrived: earliest,
	}
	// CRITERION 11, ENFORCED AND NOT ASSUMED. The merged ticket goes through the same validation Put
	// applies, so a merge cannot mint an acknowledgement as a ticket by another route.
	if err := merged.Validate(); err != nil {
		return Ticket{}, err
	}

	record := MergeRecord{ID: spec.ID, Inputs: inputs, Merged: spec.When}
	if err := record.Validate(); err != nil {
		return Ticket{}, err
	}
	mergedBody, err := encode(merged)
	if err != nil {
		return Ticket{}, fmt.Errorf("the merged ticket could not be encoded: %w", err)
	}
	recordBody, err := encodeMerge(record)
	if err != nil {
		return Ticket{}, fmt.Errorf("the merge record could not be encoded: %w", err)
	}

	ops := []store.Op{
		{Kind: Kind, ID: merged.ID, Data: mergedBody},
		{Kind: MergeKind, ID: record.ID, Data: recordBody},
	}
	for _, in := range inputs {
		if in.TicketID == merged.ID {
			// The merged ticket has taken this input's identifier, so there is nothing to remove: the
			// put above IS the replacement. Emitting a delete for it and relying on puts happening
			// first would make the outcome depend on an ordering rule rather than on this batch.
			continue
		}
		ops = append(ops, store.Op{Kind: Kind, ID: in.TicketID, Delete: true})
	}
	// ONE BATCH. Interrupted anywhere, the inbox holds either the inputs or the merged ticket.
	if err := s.Apply(batchName("merge", spec.ID), ops); err != nil {
		return Ticket{}, err
	}
	return merged, nil
}

// Unmerge takes a merged ticket apart, restoring every source EXACTLY, and leaves a trace that the
// merge happened and was undone.
//
// It returns the restored tickets in the order they were merged. A ticket that was never merged is
// [ErrNotMerged] and NOTHING IS TOUCHED (criterion 9).
func Unmerge(s *store.Store, id string, when time.Time) ([]Ticket, error) {
	record, err := LoadMerge(s, id)
	if err != nil {
		return nil, err
	}
	if err := record.Validate(); err != nil {
		return nil, err
	}

	ops := make([]store.Op, 0, 2*len(record.Inputs)+2)
	restored := make([]Ticket, 0, len(record.Inputs))
	for _, in := range record.Inputs {
		t, derr := decode(in.TicketID, in.Snapshot)
		if derr != nil {
			return nil, fmt.Errorf("%w: the snapshot of %q in merge %q does not decode: %v",
				ErrUnreadableMerge, in.TicketID, id, derr)
		}
		restored = append(restored, t)
		// THE SNAPSHOT'S OWN BYTES GO BACK. Not encode(t) — a round trip through the struct would
		// silently drop anything a future field added to the file and not to the struct, and
		// criterion 5 is "the content it had", not "the content this build knows how to hold".
		ops = append(ops, store.Op{Kind: Kind, ID: in.TicketID, Data: in.Snapshot})

		trace := Unmerged{
			TicketID:   in.TicketID,
			MergedInto: record.ID,
			Merged:     record.Merged,
			Undone:     when,
			Alongside:  len(record.Inputs) - 1,
		}
		body, terr := encodeUnmerged(trace)
		if terr != nil {
			return nil, terr
		}
		ops = append(ops, store.Op{Kind: UnmergedKind, ID: in.TicketID, Data: body})
	}
	// The merged ticket goes, unless a source is restoring over its identifier — in which case the
	// put above is already the replacement and a delete would remove it.
	drop := true
	for _, in := range record.Inputs {
		if in.TicketID == record.ID {
			drop = false
		}
	}
	if drop {
		ops = append(ops, store.Op{Kind: Kind, ID: record.ID, Delete: true})
	}
	ops = append(ops, store.Op{Kind: MergeKind, ID: record.ID, Delete: true})

	if err := s.Apply(batchName("unmerge", record.ID), ops); err != nil {
		return nil, err
	}
	return restored, nil
}

// LoadMerge returns the merge that produced a ticket. A ticket that was never merged is
// [ErrNotMerged]; a merge record that is there and will not decode is [ErrUnreadableMerge], never an
// empty merge.
func LoadMerge(s *store.Store, id string) (MergeRecord, error) {
	rec, err := s.Get(MergeKind, id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, store.ErrInvalidName) {
			return MergeRecord{}, fmt.Errorf("%w: %q", ErrNotMerged, id)
		}
		return MergeRecord{}, err
	}
	return decodeMerge(id, rec.Data)
}

// IsMerged reports, in three values, whether a ticket is a merged ticket. Undetermined is a real
// answer here: a merge record that cannot be read is not evidence that the ticket is an ordinary
// one, and answering No would be §4.3's forbidden collapse.
func IsMerged(s *store.Store, id string) (tri.Value, error) {
	_, err := LoadMerge(s, id)
	switch {
	case err == nil:
		return tri.Yes, nil
	case errors.Is(err, ErrNotMerged):
		return tri.No, nil
	default:
		return tri.Undetermined, err
	}
}

// LoadUnmerged returns the trace of a merge this ticket was in and out of, and whether there is one.
// The three-valued answer is criterion 6's: "was this ticket ever merged" must not answer No because
// the trace could not be read.
func LoadUnmerged(s *store.Store, id string) (Unmerged, tri.Value, error) {
	rec, err := s.Get(UnmergedKind, id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, store.ErrInvalidName) {
			return Unmerged{}, tri.No, nil
		}
		return Unmerged{}, tri.Undetermined, err
	}
	u, derr := decodeUnmerged(id, rec.Data)
	if derr != nil {
		return Unmerged{}, tri.Undetermined, derr
	}
	return u, tri.Yes, nil
}

// ListMerges returns every merge record, ordered by identifier — for the same reason [List] orders
// by identifier, and with the same disclaimer: it is a presentation order, not a ranking.
//
// NOTHING IS FILTERED BY AGE (§5.4, criterion 16). A merge made three years ago is in this list.
func ListMerges(s *store.Store) ([]MergeRecord, error) {
	recs, err := s.List(MergeKind)
	if err != nil {
		return nil, err
	}
	out := make([]MergeRecord, 0, len(recs))
	for _, r := range recs {
		m, derr := decodeMerge(r.ID, r.Data)
		if derr != nil {
			return nil, derr
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Validate is criterion 4 as a check: for every input, what it was, where it came from and why must
// all be in the record. It is called on the way in AND on the way out, because a record written by
// another build is not this build's to trust.
//
// AN UNDETERMINED FIELD PASSES. That is the point of criterion 12: "the origin could not be
// resolved" is a recorded answer and a complete one. What fails is a field that is not there at all.
//
// WHERE THE PER-FIELD CHECK ACTUALLY LIVES, stated because it is not here and a reader will look. A
// [MergeInput] in memory always has its four fields — they are values, not pointers, and Go does not
// permit one to be missing. The state a field CAN be missing in is on disk, so the check is in
// [decodeMerge], where a JSON key that was never written arrives as a nil pointer and is refused
// with [ErrIncompleteWorking]. Repeating it here would be a condition no input could ever fail: a
// check that cannot go red, which is worse than none because it reads like cover.
func (m MergeRecord) Validate() error {
	if len(m.Inputs) < 2 {
		return fmt.Errorf("%w: merge %q records %d inputs", ErrTooFewInputs, m.ID, len(m.Inputs))
	}
	for i, in := range m.Inputs {
		if in.TicketID == "" {
			return fmt.Errorf("%w: input %d of merge %q names no ticket", ErrIncompleteWorking, i+1, m.ID)
		}
		if len(in.Snapshot) == 0 {
			return fmt.Errorf("%w: input %q of merge %q has no snapshot, so the merge could not be "+
				"undone exactly and is not reversible", ErrIncompleteWorking, in.TicketID, m.ID)
		}
	}
	return nil
}

// whatItWas is criterion 4(a) for one input: a statement of the thing that was folded in, taken from
// the source ticket as it stood.
//
// A DECISION THE ISSUE DID NOT SETTLE, STATED SO IT CAN BE OVERRULED. Criterion 4 requires "what it
// was" for every input, and [Ticket] has no field called that — it has a title and a summary, either
// of which may be written, written-empty, absent or undetermined. This takes the title when there is
// one to take, falls back to the summary, and answers UNDETERMINED when the ticket recorded neither.
// The alternative — reporting an absent title as "(not recorded)" — would satisfy the letter of
// criterion 4 while telling a person nothing, and the alternative to THAT — inventing a description
// from the identifier — would be the product writing a title, which §3.2 gives to a person.
func whatItWas(t Ticket) Field {
	if v, ok := t.Title.Value(); ok && strings.TrimSpace(v) != "" {
		return Text(v)
	}
	if v, ok := t.Summary.Value(); ok && strings.TrimSpace(v) != "" {
		return Text(v)
	}
	return Undetermined("the source ticket recorded neither a title nor a summary, so what it was " +
		"could not be determined from the ticket")
}

// isBareConcatenation reports whether summary is nothing more than the source titles stuck together.
//
// It compares on letters and digits alone, so that changing the separator or sprinkling punctuation
// between the fragments does not get past it, and it is INDIFFERENT TO THE ORDER they appear in —
// the first version compared against the titles joined in their given order and against them sorted,
// which let "second floor printer, printer jam" through while refusing the same two titles the other
// way round. A rule a person gets past by swapping two words is not a rule.
//
// The test is: the summary is exactly as long as the titles run together, and every title is inside
// it. Two strings of the same total length that each contain all the fragments are the fragments,
// in some order, with nothing added — which is what §3.2 says a merged summary must not be.
//
// It deliberately does NOT try to judge whether a summary is good. That is a person's job and not a
// classifier's; this catches exactly the failure §3.2 names, the fragments surviving the merge.
func isBareConcatenation(summary string, titles []string) bool {
	got := alphanumeric(summary)
	if got == "" {
		return false
	}
	var fragments []string
	total := 0
	for _, t := range titles {
		if a := alphanumeric(t); a != "" {
			fragments = append(fragments, a)
			total += len(a)
		}
	}
	if len(fragments) < 2 || total != len(got) {
		return false
	}
	// Every fragment must be accounted for, and each occurrence consumed once, so that two titles
	// which happen to be the same word do not both match the one copy of it in the summary.
	remaining := got
	for _, f := range fragments {
		i := strings.Index(remaining, f)
		if i < 0 {
			return false
		}
		remaining = remaining[:i] + remaining[i+len(f):]
	}
	return remaining == ""
}

func alphanumeric(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// batchName is the journal name a merge or an unmerge commits under. Two different merges never
// share one; a retried merge of the same identifier does, which is correct — it is the same batch.
func batchName(op, id string) string { return op + "-" + id }

// mergeFile is the on-disk shape. THE FOUR FIELDS OF AN INPUT ARE POINTERS, and that is criterion 4
// at the storage layer: a key that is not there decodes to nil and is reported as a merge that does
// not show its working, rather than to a zero Field which would render as "could not be determined"
// and be indistinguishable from an origin this build honestly failed to resolve.
type mergeFile struct {
	Format int             `json:"format"`
	ID     string          `json:"id"`
	Merged string          `json:"merged,omitempty"`
	Inputs []mergeFileItem `json:"inputs"`
}

type mergeFileItem struct {
	TicketID string      `json:"ticket_id"`
	What     *mergeField `json:"what"`
	Channel  *mergeField `json:"channel"`
	Source   *mergeField `json:"source"`
	Why      *mergeField `json:"why"`
	Snapshot []byte      `json:"snapshot"`
}

// mergeField wraps a [Field] in an object so that A RECORDED ABSENCE AND A KEY THAT WAS NEVER
// WRITTEN ARE DIFFERENT BYTES.
//
// WHY THE WRAPPER IS NOT DECORATION. A Field marshals an absence as JSON `null` — that is
// [Field.MarshalJSON] and it is right, because for a ticket `null` means "recorded as having none".
// But encoding/json decodes `null` into a *Field as a NIL POINTER, so with a bare pointer the two
// states criterion 4 and criterion 12 have to tell apart — "this input records no source" and "this
// merge does not say where this input came from" — arrive as the same nil and the second, a failure,
// reads as the first, a legitimate answer.
//
// Found by driving criterion 12: an input whose why was Absent() came back as ErrIncompleteWorking.
// The wrapper object is never null, so a nil pointer now means only what it should: the key is not
// in the record at all.
type mergeField struct {
	Field Field `json:"field"`
}

func encodeMerge(m MergeRecord) ([]byte, error) {
	f := mergeFile{Format: mergeFormat, ID: m.ID}
	if !m.Merged.IsZero() {
		f.Merged = m.Merged.UTC().Format(time.RFC3339Nano)
	}
	for i := range m.Inputs {
		in := m.Inputs[i]
		f.Inputs = append(f.Inputs, mergeFileItem{
			TicketID: in.TicketID,
			What:     &mergeField{in.What},
			Channel:  &mergeField{in.Channel},
			Source:   &mergeField{in.Source},
			Why:      &mergeField{in.Why},
			Snapshot: in.Snapshot,
		})
	}
	return json.Marshal(f)
}

func decodeMerge(id string, body []byte) (MergeRecord, error) {
	var f mergeFile
	if err := json.Unmarshal(body, &f); err != nil {
		return MergeRecord{}, fmt.Errorf("%w: merge %q is damaged: %v", ErrUnreadableMerge, id, err)
	}
	if f.Format != mergeFormat {
		return MergeRecord{}, fmt.Errorf("%w: merge %q is format %d, which this build does not understand",
			ErrUnreadableMerge, id, f.Format)
	}
	m := MergeRecord{ID: f.ID}
	if m.ID == "" {
		m.ID = id
	}
	for i, item := range f.Inputs {
		in := MergeInput{TicketID: item.TicketID, Snapshot: item.Snapshot}
		for _, p := range []struct {
			what string
			from *mergeField
			to   *Field
		}{
			{"what it was", item.What, &in.What},
			{"which channel it came from", item.Channel, &in.Channel},
			{"which source it came from", item.Source, &in.Source},
			{"why it was merged", item.Why, &in.Why},
		} {
			if p.from == nil {
				// CRITERION 4. Not there at all is a failure, and is NOT quietly promoted to
				// undetermined: undetermined means this build looked and could not tell, which is a
				// claim nobody made about a key that was never written.
				return MergeRecord{}, fmt.Errorf("%w: input %d of merge %q does not record %s",
					ErrIncompleteWorking, i+1, m.ID, p.what)
			}
			*p.to = p.from.Field
		}
		m.Inputs = append(m.Inputs, in)
	}
	if f.Merged != "" {
		if when, err := time.Parse(time.RFC3339Nano, f.Merged); err == nil {
			m.Merged = when
		}
		// A timestamp that will not parse leaves Merged zero, which renders as undetermined — never
		// as the epoch and never silently as now.
	}
	return m, nil
}

type unmergedFile struct {
	Format     int    `json:"format"`
	TicketID   string `json:"ticket_id"`
	MergedInto string `json:"merged_into"`
	Merged     string `json:"merged,omitempty"`
	Undone     string `json:"undone,omitempty"`
	Alongside  int    `json:"alongside"`
}

func encodeUnmerged(u Unmerged) ([]byte, error) {
	f := unmergedFile{
		Format:     unmergedFormat,
		TicketID:   u.TicketID,
		MergedInto: u.MergedInto,
		Alongside:  u.Alongside,
	}
	if !u.Merged.IsZero() {
		f.Merged = u.Merged.UTC().Format(time.RFC3339Nano)
	}
	if !u.Undone.IsZero() {
		f.Undone = u.Undone.UTC().Format(time.RFC3339Nano)
	}
	return json.Marshal(f)
}

func decodeUnmerged(id string, body []byte) (Unmerged, error) {
	var f unmergedFile
	if err := json.Unmarshal(body, &f); err != nil {
		return Unmerged{}, fmt.Errorf("%w: the merge history of %q is damaged: %v", ErrUnreadableMerge, id, err)
	}
	if f.Format != unmergedFormat {
		return Unmerged{}, fmt.Errorf("%w: the merge history of %q is format %d, which this build does not understand",
			ErrUnreadableMerge, id, f.Format)
	}
	u := Unmerged{TicketID: f.TicketID, MergedInto: f.MergedInto, Alongside: f.Alongside}
	if u.TicketID == "" {
		u.TicketID = id
	}
	if when, err := time.Parse(time.RFC3339Nano, f.Merged); err == nil {
		u.Merged = when
	}
	if when, err := time.Parse(time.RFC3339Nano, f.Undone); err == nil {
		u.Undone = when
	}
	return u, nil
}

// MergedRender is when the merge happened, or undetermined when that was never recorded.
func (u Unmerged) MergedRender() string { return renderTime(u.Merged) }

// UndoneRender is when the merge was taken apart, or undetermined.
func (u Unmerged) UndoneRender() string { return renderTime(u.Undone) }

func renderTime(t time.Time) string {
	if t.IsZero() {
		return tri.Undetermined.Render("", "")
	}
	return t.UTC().Format(time.RFC3339)
}
