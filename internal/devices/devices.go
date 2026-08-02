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
type CheckIn struct {
	// State is the answer. Yes: it checked in, At says when. No: it was registered and has never
	// checked in. Undetermined: this could not be worked out, and Why says what stopped it.
	State tri.Value
	// At is when it checked in. Meaningful only when State is Yes.
	At time.Time
	// Why carries the reason when State is Undetermined.
	Why string
}

// NeverCheckedIn is the state of a machine that was registered and never started. It is a VALUE,
// constructed on purpose, not the zero CheckIn — the zero CheckIn is Undetermined, because a
// struct nobody filled in has not determined anything.
func NeverCheckedIn() CheckIn { return CheckIn{State: tri.No} }

// CheckedInAt is the state of a machine that reported in at t.
func CheckedInAt(t time.Time) CheckIn { return CheckIn{State: tri.Yes, At: t.UTC()} }

// UndeterminedCheckIn is the third answer, carrying what stopped the determination.
func UndeterminedCheckIn(why string) CheckIn { return CheckIn{State: tri.Undetermined, Why: why} }

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
	switch c.State {
	case tri.Yes:
		if c.At.IsZero() {
			// A "yes" with no instant is not a check-in anyone can point at. Reported as the third
			// answer rather than as a checked-in device with a blank time.
			return tri.Undetermined.String() + ": this device is recorded as checked in, but not when"
		}
		return "checked in at " + c.At.UTC().Format(checkInTimeFormat)
	case tri.No:
		// THE SENTENCE PRD §3.8 IS ABOUT. It states two things a reader needs and neither is an
		// absence: the machine IS registered, and it has never reported in.
		return "registered, and has never checked in"
	default:
		why := strings.TrimSpace(c.Why)
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
