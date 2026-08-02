// Package tri holds the product's three-valued answer.
//
// PRD §4.3: "A state that could not be determined is shown as undetermined, never as a 'no'. A
// missing project directory, a device that has never checked in, a disk whose encryption cannot be
// read: each is a distinct answer, and none of them is silence."
//
// This package exists once so that the distinction exists once. Every capability in the product
// restates it — health's encryption answer, the daemon's last-run ending, a project's directory
// state, a channel's last ingestion, a status line's subsystem — and a per-package reinvention is
// how "could not determine" quietly becomes "determined to be nothing" in one of them.
//
// TWO PROPERTIES CARRY THE RULE, AND BOTH ARE TESTED:
//
//   - The zero value is Undetermined, not No. A Value nobody set has not been determined, and a
//     struct field left alone by an error path must not read as a negative. Ordering the constants
//     the other way round would make every unset answer a confident "no".
//   - The three render distinguishably, in every rendering this package offers. Rendering is not
//     left to callers with a fmt verb, because that is where two of them collapse into one.
package tri

// Value is an answer that may be yes, no, or genuinely not determinable.
//
// It is deliberately not a bool, not a *bool, and not a (bool, error). A (bool, error) whose error
// is dropped has silently produced a "no" from a failure, which is the exact defect this type is
// here to make unrepresentable.
type Value int

const (
	// Undetermined is the zero value ON PURPOSE. See the package comment.
	Undetermined Value = iota
	Yes
	No
)

// undeterminedText is the one wording for the third value. It is a single constant so that no
// caller can render the third value as something a reader might mistake for a negative.
const undeterminedText = "could not be determined"

// String renders the value in neutral wording. Every branch returns a non-empty, distinct string:
// there is no case in which a Value renders as "" — silence is not one of the three answers.
//
// A Value outside the three constants is reported as undetermined rather than panicking or
// rendering blank. An impossible value is, precisely, a state that could not be determined.
func (v Value) String() string {
	switch v {
	case Yes:
		return "yes"
	case No:
		return "no"
	case Undetermined:
		return undeterminedText
	default:
		return undeterminedText
	}
}

// Render gives the yes and no branches domain wording while keeping the third answer's wording
// fixed. Health says Render("enabled", "not enabled"); a project says Render("present", "missing").
//
// The third value is NOT a caller's choice. Allowing it would let one capability spell it as
// "unknown" and another as "none", and "none" is a negative — which is the collapse §4.3 forbids.
//
// A caller passing an empty string for yes or no gets the neutral wording for that branch rather
// than an empty render, because a blank line is silence and silence is not an answer.
func (v Value) Render(yes, no string) string {
	switch v {
	case Yes:
		if yes == "" {
			return "yes"
		}
		return yes
	case No:
		if no == "" {
			return "no"
		}
		return no
	default:
		return undeterminedText
	}
}

// Determined reports whether the value is a real answer either way.
//
// Callers use this to decide whether a summary may claim everything is fine — PRD §4.3 read across
// to §3.9's status screen, where "at least one thing I could not check" must not lead with "all
// good". It deliberately does not report WHICH answer: a caller wanting that must look at the Value.
func (v Value) Determined() bool { return v == Yes || v == No }

// FromError turns a check's outcome into a Value, and is the only sanctioned way to build one from
// a (bool, error) pair.
//
// An error means the check could not be completed, so the answer is Undetermined — NEVER No. The
// project's own conventions put it first: "a (bool, error) whose error is dropped has broken it".
func FromError(ok bool, err error) Value {
	if err != nil {
		return Undetermined
	}
	if ok {
		return Yes
	}
	return No
}
