// Package publish is Issue #10: the transfer that takes a draft out of the outbox and puts it on
// the hub, and the client's answer to "where is my note".
//
// # The one sentence
//
// PRD §3.11: "A note that did not arrive is still in the outbox — never both, never neither."
//
// That is an INVARIANT, not a behaviour. A behaviour holds while the code runs; an invariant has to
// hold across a power cut. So this package is organised around a single durable record per note —
// one small file in the [Ledger], written with one atomic rename — and every question about where a
// note is is answered from that record. There is no second place to look and therefore no pair of
// places that can disagree.
//
// # The four states
//
//	drafted     nothing has been attempted, or an attempt never left this machine
//	in flight   an attempt was sent and its outcome is not known to this client
//	published   the hub has it, and this client has been told so
//	refused     the hub was asked and said no, and said why
//
// [State] is a closed set and the values here are the only spellings. They are compared PAIRWISE in
// the tests rather than each against a literal: three assertions of the form `got == "drafted"`
// pass just as happily after two of the four renderings are edited to the same wording, which is
// the collapse the whole project exists to prevent.
//
// # Two things that look the same and are not
//
//   - A HUB THAT CANNOT BE REACHED IS NOT A REFUSED NOTE. Nothing was sent, so nothing was
//     considered; the note is `drafted`, the exit code is the undetermined one, and the code on the
//     wire is [hub.ErrHubUnreachable]. A refusal is a determined answer from the hub about this
//     note, it carries the hub's reason, and it leaves the note `refused`.
//   - NO HUB CONFIGURED IS NOT AN UNREACHABLE HUB. The first is a determined fact about this
//     machine and is settled without opening anything (PRD §4.2). See [Transfer].
package publish

import (
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// State is one of the four states a note this client knows about can be in.
type State string

const (
	// StateDrafted — in the outbox, nothing outstanding. The state of a note never attempted, and
	// the state a note returns to when an attempt provably never left this machine.
	StateDrafted State = "drafted"
	// StateInFlight — an attempt was sent and its outcome is NOT known here. Not "not published":
	// this client does not know, and criterion 13 says it must not claim to.
	StateInFlight State = "in flight"
	// StatePublished — the hub has it. The only state in which the note is not in the outbox.
	StatePublished State = "published"
	// StateRefused — the hub was asked and refused, and [Report.Reason] says why. A refusal with no
	// reason is a defect (criterion 7) and [Report.Render] will say so out loud rather than print a
	// blank.
	StateRefused State = "refused"
)

// States returns the four, in the order a person meets them. There is no fifth.
func States() []State { return []State{StateDrafted, StateInFlight, StatePublished, StateRefused} }

// Container is which of PRD §2.3's two containers holds the note. Exactly one always does.
type Container string

const (
	// ContainerOutbox — on this machine, unpublished.
	ContainerOutbox Container = "outbox"
	// ContainerHub — published, and therefore gone from the outbox.
	ContainerHub Container = "hub"
)

// Report is everything this client knows about one note's publication.
//
// It is the SINGLE COMPUTATION behind every surface: [Report.Render] is what the CLI prints and
// [Report.Wire] is what the control endpoint serialises, so criterion 16's "the control API and the
// CLI report the same state" is a property of there being one of them rather than of two of them
// having been checked against each other today.
type Report struct {
	// Note is the draft's local name.
	Note hub.NoteID
	// Known is Yes when the state was established, Undetermined when it could not be. It is never
	// No: a note this client knows about is always somewhere.
	Known tri.Value
	// Exists is Yes when this client knows about the note at all.
	Exists tri.Value
	// State is one of the four, meaningful when Known is Yes.
	State State
	// HubID is the id the hub minted, set only in StatePublished.
	HubID hub.NoteID
	// Reason is the hub's stated reason, set in StateRefused.
	Reason string
	// Code is the stable error code behind a refusal or an undetermined outcome.
	Code string
	// Why says what could not be read, when Known is Undetermined.
	Why string
	// Attempt is the idempotency key of the outstanding or last attempt. It is what makes a retry
	// a retry; it is reported so that a person can see the two attempts are the same one.
	Attempt string
}

// Published answers "is it on the hub" in three values.
//
// THE THIRD VALUE IS THE POINT (criterion 13). An in-flight note is not `no`: the client does not
// know, and rendering "not published" for something that may well be published is the lie §4.3
// exists to prevent. Criterion 4 is satisfied by the state being anything other than
// [StatePublished] and by the note still being in the outbox — not by claiming knowledge.
func (r Report) Published() tri.Value {
	switch {
	case r.Known != tri.Yes:
		return tri.Undetermined
	case r.State == StatePublished:
		return tri.Yes
	case r.State == StateInFlight:
		return tri.Undetermined
	default:
		return tri.No
	}
}

// Container is which container holds the note. NEVER BOTH AND NEVER NEITHER is enforced by this
// being a total function of one field: there is no combination of states in which it returns
// nothing, and no combination in which it returns two.
func (r Report) Container() Container {
	if r.Known == tri.Yes && r.State == StatePublished {
		return ContainerHub
	}
	return ContainerOutbox
}

// InOutbox is the §2.3 half of the invariant, stated the way PRD §3.11 states it.
func (r Report) InOutbox() bool { return r.Container() == ContainerOutbox }

// missingReason is what a refusal with no reason renders as.
//
// It is a SENTENCE ABOUT THE DEFECT rather than a blank, because criterion 7 makes "refused with no
// reason attached" a defect and a person meeting one needs to know they have met a bug and not a
// hub that simply had nothing to say.
const missingReason = "the hub refused this note and this client recorded no reason, which is a defect in this client"

// Render is the one rendering, and every line of it is machine-checkable on purpose.
//
// Criterion 8 asks that a refusal and an unreachable hub differ "in a machine-checkable way, not
// only in prose wording", and criterion 6 asks that the four states be told apart "by inspection of
// the output alone". Prose alone satisfies neither, so the first four lines are `key: value` with a
// closed vocabulary on the right, and the prose follows underneath for the person.
func (r Report) Render() string {
	var b strings.Builder
	b.WriteString("note: " + string(r.Note) + "\n")
	if r.Exists == tri.No {
		b.WriteString("state: no such note on this client\n")
		return b.String()
	}
	if r.Known != tri.Yes {
		b.WriteString("state: " + tri.Undetermined.String() + "\n")
		b.WriteString("published: " + tri.Undetermined.String() + "\n")
		b.WriteString("container: " + string(ContainerOutbox) + "\n")
		b.WriteString("  where this note stands could not be read, so this is neither drafted nor\n")
		b.WriteString("  published nor refused")
		if r.Why != "" {
			b.WriteString(": " + r.Why)
		}
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString("state: " + string(r.State) + "\n")
	b.WriteString("published: " + r.Published().String() + "\n")
	b.WriteString("container: " + string(r.Container()) + "\n")
	if r.Code != "" {
		b.WriteString("code: " + r.Code + "\n")
	}
	switch r.State {
	case StateDrafted:
		b.WriteString("  In your outbox and nothing is outstanding. No attempt to publish it is\n")
		b.WriteString("  in progress and none has been left half-done.\n")
	case StateInFlight:
		// THE CAREFUL ONE. Criterion 4 wants an interrupted publish to read as not published;
		// criterion 13 forbids rendering "not published" when the client does not know. Both are
		// satisfied by saying exactly what is true: it is not published as far as this client has
		// been told, and whether the hub has it was never established.
		b.WriteString("  An attempt was sent and its outcome was never established. This note is NOT\n")
		b.WriteString("  published as far as this client has been told, and whether the hub received\n")
		b.WriteString("  it " + tri.Undetermined.String() + ". It is still in your outbox.\n")
		b.WriteString("  Publishing it again resolves this and cannot make a second copy: the retry\n")
		b.WriteString("  carries the same attempt " + r.Attempt + ".\n")
	case StatePublished:
		b.WriteString("  On the hub as " + string(r.HubID) + ", and no longer in your outbox.\n")
	case StateRefused:
		reason := r.Reason
		if strings.TrimSpace(reason) == "" {
			reason = missingReason
		}
		b.WriteString("  The hub was asked and refused. This is NOT 'the hub could not be reached'.\n")
		b.WriteString("  reason: " + reason + "\n")
		b.WriteString("  It is still in your outbox, unchanged, and can be published again once the\n")
		b.WriteString("  reason above no longer holds.\n")
	default:
		b.WriteString("  the recorded state is not one this build knows\n")
	}
	return b.String()
}

// Wire is the control endpoint's serialisation of a [Report].
//
// It is strings all the way down, deliberately. A tri.Value is an int whose zero value carries
// meaning, and an int on a wire is exactly the thing that survives a version skew looking valid and
// meaning something else.
type Wire struct {
	Note      string `json:"note"`
	State     string `json:"state"`
	Published string `json:"published"`
	Container string `json:"container"`
	HubID     string `json:"hub_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Code      string `json:"code,omitempty"`
	Why       string `json:"why,omitempty"`
	Attempt   string `json:"attempt,omitempty"`
	Exists    string `json:"exists"`
}

// Wire renders the report for the control endpoint.
func (r Report) Wire() Wire {
	w := Wire{
		Note:      string(r.Note),
		Published: r.Published().String(),
		Container: string(r.Container()),
		HubID:     string(r.HubID),
		Reason:    r.Reason,
		Code:      r.Code,
		Why:       r.Why,
		Attempt:   r.Attempt,
		Exists:    r.Exists.String(),
	}
	if r.Known == tri.Yes {
		w.State = string(r.State)
	} else {
		w.State = tri.Undetermined.String()
	}
	return w
}
