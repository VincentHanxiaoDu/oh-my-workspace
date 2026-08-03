package devices

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Label is the name a machine is registered under. PRD §3.8: "a label unique to the person".
//
// IT IS COMPARED BYTE-EXACTLY, AND THAT IS A REFUSAL TO GUESS. The Issue settles the uniqueness
// SCOPE (unique to the person) and does not settle the label's FORMAT. So this build does not case
// fold, does not trim interior whitespace, does not normalise Unicode and does not transliterate:
// "Laptop" and "laptop" are two labels, because deciding they are one would silently merge two of
// a person's machines, and §3.8 says devices are shown as separate. See [CheckLabel] for the only
// labels this build refuses, and why each refusal is loud.
type Label string

// Machine identifies WHICH machine a label is registered to.
//
// It is the machine's store id. Issue #17 states the relation outright — "each device has exactly
// one store, so device identity and store identity are the same question asked twice" — so this
// build does not mint a second identity scheme beside the one #3 already has.
type Machine string

// CheckIn is a device's check-in state: the three-valued answer of PRD §4.3, applied to §3.8's
// "a device that has not checked in".
//
// # ITS FIELDS ARE UNEXPORTED, AND THAT IS THE WHOLE DESIGN
//
// A CheckIn is built only by the three constructors below, so an invalid one cannot be written —
// not by this package, not by a caller, not by a future consumer. That is a stronger statement than
// "the constructors are careful", and it was bought with a real defect:
//
// A check-in recorded at the ZERO INSTANT used to render as "could not be determined" while
// State reported tri.Yes. The rendering function knew something the value did not, so every OTHER
// consumer disagreed with it: the control API's machine-readable field said `checked_in`, the exit
// code said determined, and a person read "could not be determined" off the same device at the same
// moment. The two surfaces contradicted each other (PRD §4.3), and the person's script and the
// person were told different things.
//
// THE LESSON, WHICH IS WHY THE FIX IS NOT A SECOND GUARD. Patching the machine-readable field and
// the exit-code predicate would have left the fourth consumer to be found later — the defect was
// never in any one consumer, it was that the undetermined-ness lived in a RENDERING instead of in
// the VALUE. So the value carries it: a check-in with no instant is not a valid "yes", and
// [CheckedInAt] returns the third answer rather than an invalid one. Every consumer, present and
// future, now agrees by construction because there is nothing to disagree with.
//
// It is reachable from disk, which is why it is not a theoretical worry: Go's zero time.Time is
// year 0001 and RFC3339 encodes it happily, so `{"state":"at","at":"0001-01-01T00:00:00Z"}` is a
// record a real inventory can hold.
type CheckIn struct {
	// state is the answer. Yes: it checked in, and at says when — an invariant, not a hope.
	// No: it was registered and has never checked in. Undetermined: this could not be worked out.
	state tri.Value
	at    time.Time
	why   string
}

// State is the three-valued answer. It is the ONE place any consumer asks what this check-in is,
// and it can never report Yes for a check-in with no instant.
func (c CheckIn) State() tri.Value { return c.state }

// At is when the device checked in. It is the zero time unless State is Yes, and it is never the
// zero time when State is Yes.
func (c CheckIn) At() time.Time { return c.at }

// Why is what stopped the determination. Empty unless State is Undetermined.
func (c CheckIn) Why() string { return c.why }

// Determined reports whether this check-in is a real answer either way. Derived from State, so it
// cannot drift from it.
func (c CheckIn) Determined() bool { return c.state.Determined() }

// NeverCheckedIn is the state of a machine that was registered and never started. It is a VALUE,
// constructed on purpose, not the zero CheckIn — the zero CheckIn is Undetermined, because a
// struct nobody filled in has not determined anything.
func NeverCheckedIn() CheckIn { return CheckIn{state: tri.No} }

// CheckedInAt is the state of a machine that reported in at t.
//
// IT REFUSES TO BUILD A CHECK-IN WITH NO INSTANT. "It checked in, and I cannot say when" is not a
// check-in a person can act on and not a fact this product has established; it is the third answer,
// so that is what comes back. Returning the undetermined value here rather than rendering it later
// is what keeps every consumer in agreement — see the type's comment.
func CheckedInAt(t time.Time) CheckIn {
	if t.IsZero() {
		return UndeterminedCheckIn("this device is recorded as having checked in, but not when — " +
			"a check-in with no instant is not something this product has established")
	}
	return CheckIn{state: tri.Yes, at: t.UTC()}
}

// UndeterminedCheckIn is the third answer, carrying what stopped the determination.
func UndeterminedCheckIn(why string) CheckIn { return CheckIn{state: tri.Undetermined, why: why} }

// checkInTimeFormat is the one rendering of a check-in instant. RFC3339 in UTC, so two listings of
// the same device taken on two machines produce the same bytes.
const checkInTimeFormat = time.RFC3339

// Describe renders the check-in state, and it is THE ONLY RENDERING of it in the product.
//
// Criterion 4 and criterion 9 ask that never-checked-in, a real check-in, and undetermined be
// three distinct things. Distinctness is a property of a set of strings, so it can only be held
// where the whole set is produced — here. A caller given the raw fields and a fmt verb is a caller
// who can render two of them the same, and that has to be impossible rather than discouraged.
//
// No branch returns the empty string, and no branch returns a value that could be read as blank:
// silence is not one of the three answers (§4.3).
func (c CheckIn) Describe() string {
	switch c.state {
	case tri.Yes:
		// NO ZERO-INSTANT GUARD HERE, ON PURPOSE. There used to be one, and it was the defect: it
		// made this function disagree with State, CheckInWord, the exit codes and every future
		// consumer. CheckedInAt will not build a Yes without an instant, so this branch always has
		// one, and a guard here would be a second opinion about a question the value already
		// settles.
		return "checked in at " + c.at.UTC().Format(checkInTimeFormat)
	case tri.No:
		// THE SENTENCE PRD §3.8 IS ABOUT. It states two things a reader needs and neither is an
		// absence: the machine IS registered, and it has never reported in.
		return "registered, and has never checked in"
	default:
		why := strings.TrimSpace(c.why)
		if why == "" {
			why = "no reason was recorded"
		}
		return "whether this device has ever checked in " + tri.Undetermined.String() + ": " + why
	}
}

// Device is one machine in the person's inventory.
type Device struct {
	Label        Label
	Machine      Machine
	RegisteredAt time.Time
	CheckIn      CheckIn
	// Source says where this entry was learnt from — this machine's registry, or the hub. It is
	// carried so a reader can tell a locally-known device from one the hub reported, and so a
	// merge can never present a hub entry as a local registration.
	Source string
}

// Sources.
const (
	SourceLocal = "this machine"
	SourceHub   = "the hub"
)

// Render is one device, as one line, for a person.
func (d Device) Render() string {
	return fmt.Sprintf("%s  —  %s  [%s]", d.Label, d.CheckIn.Describe(), d.Source)
}

// Errors this package returns. Each is its own value because each is a different fact and the
// commands turn them into different sentences and different exit codes.
var (
	// ErrDuplicateLabel is criterion 7: a label already registered to this person.
	ErrDuplicateLabel = errors.New("that label is already registered to another machine")
	// ErrMachineAlreadyRegistered is the other half of "each machine is registered under A label":
	// registering the same machine a second time, under a different name, would put one machine in
	// the inventory twice.
	ErrMachineAlreadyRegistered = errors.New("this machine is already registered under another label")
	// ErrNoSuchDevice is criterion 5: a label that was never registered. It is deliberately NOT
	// reported as a device whose check-in is unknown — that would report a machine that does not
	// exist as one that does.
	ErrNoSuchDevice = errors.New("no device is registered under that label")
	// ErrLabelRefused is a label this build will not register. See CheckLabel.
	ErrLabelRefused = errors.New("that label cannot be registered")
	// ErrRegistryUnreadable is the inventory itself failing to read. Never an empty inventory.
	ErrRegistryUnreadable = errors.New("this machine's device inventory could not be read")
	// ErrMachineUndetermined means which machine this is could not be worked out.
	ErrMachineUndetermined = errors.New("which machine this is could not be determined")
)

// CheckLabel is every refusal this build makes about a label, and there are exactly three.
//
// WHAT THIS DELIBERATELY DOES NOT DO. Issue #17 fixes the uniqueness scope and says nothing about
// the format, so no length cap, no character class, no case folding and no normalisation is
// invented here — an invented scheme is a decision made silently on the person's behalf, and the
// standing instruction is to implement what is settled and refuse loudly where it is not.
//
// The three refusals are not format opinions; each is a case where accepting the label would break
// something already settled:
//
//   - The empty label, and a label that is only whitespace. There is nothing to be unique against,
//     and the listing would carry an entry that renders as a blank — the silence §4.3 forbids.
//   - A label containing a newline or a carriage return. The listing is one device per line, so
//     such a label can forge an extra entry in the output a person reads. A device that does not
//     exist appearing in the inventory is the opposite of what this Issue is for.
//   - A label containing a NUL. It cannot survive the round trip through the tools a person will
//     pipe this listing into, so it would not be the label they registered.
func CheckLabel(l Label) error {
	s := string(l)
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%w: a label is required and cannot be blank; name the machine explicitly", ErrLabelRefused)
	}
	if strings.ContainsAny(s, "\n\r") {
		return fmt.Errorf("%w: a label cannot contain a line break — the device listing is one device "+
			"per line, and a label that breaks the line can make the listing show a machine that does not exist", ErrLabelRefused)
	}
	if strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: a label cannot contain a NUL byte", ErrLabelRefused)
	}
	return nil
}
