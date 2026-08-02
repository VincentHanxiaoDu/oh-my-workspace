package inbox

import (
	"encoding/json"
	"errors"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Field is one piece of a ticket's text, in the four states such a thing can actually be in.
//
// WHY NOT A string. Issue #8 criterion 1: "a real value and a missing value never produce the same
// output". A plain string cannot hold that distinction — the zero value of a string is the empty
// string, so a title nobody wrote and a title somebody wrote as "" are one value, and whichever
// sentence the renderer picks is wrong for the other case. A *string fixes half of it and leaves
// criterion 12, where a summary that has not been written yet and a channel that could not be read
// must render as undetermined rather than as absent.
//
// So there are four states, and every one of them renders as its own sentence:
//
//	Text("Reset Ana's login")  written, with a value          → the value
//	Text("")                   written, and empty             → "(recorded as empty)"
//	Absent()                   never recorded on this ticket  → "(not recorded)"
//	Undetermined("…")          could not be determined        → tri's fixed wording
//
// THE ZERO VALUE IS UNDETERMINED, following [tri.Value] and for the same reason: a Field that
// nobody set has not been determined, and a struct field left alone by an error path must not read
// as a confident "there is nothing here". A caller that means "there is nothing here" says
// [Absent].
type Field struct {
	// state is Yes when a value was recorded (possibly empty), No when the field was recorded as
	// having no value at all, and Undetermined when it could not be determined.
	state tri.Value
	value string
	// reason is why the value could not be determined. Carried so the CLI can name the specific
	// finding — "the source channel could not be read" — without this package writing the sentence
	// around it. It is only meaningful when state is Undetermined.
	reason string
}

// The renderings of the two states that have no value to show. They are constants rather than
// literals at the call sites because their whole job is to differ from each other, and two literals
// in two files are how they stop differing.
const (
	emptyText  = "(recorded as empty)"
	absentText = "(not recorded)"
)

// Text is a field somebody wrote. An empty string is a real, written, empty value — NOT an absence.
func Text(s string) Field { return Field{state: tri.Yes, value: s} }

// Absent is a field this ticket does not have. It is a determined answer: we know there is none.
func Absent() Field { return Field{state: tri.No} }

// Undetermined is a field whose value could not be worked out — the source channel unreadable, the
// summary not yet written. reason may be empty; the rendering never is.
func Undetermined(reason string) Field { return Field{state: tri.Undetermined, reason: reason} }

// State reports which of the three answers this field is: Yes it was recorded, No there is none,
// Undetermined it could not be determined. It deliberately does not distinguish a written empty
// value from a written non-empty one — that is a question about the value, so ask [Value].
func (f Field) State() tri.Value { return f.state }

// Value returns the recorded text and whether there was any to return. ok is false for both the
// absent and the undetermined states, which is exactly why a caller that renders must use [Render]
// rather than this: `v, _ := f.Value()` collapses all three of "", absent and undetermined.
func (f Field) Value() (string, bool) {
	if f.state == tri.Yes {
		return f.value, true
	}
	return "", false
}

// Reason is why the value could not be determined, or "" when the field is not undetermined.
func (f Field) Reason() string {
	if f.state == tri.Undetermined {
		return f.reason
	}
	return ""
}

// Render is the field as a person reads it, and it is the only rendering this package offers.
//
// Rendering is not left to callers with a fmt verb for the reason stated in package tri: that is
// where two of the states collapse into one. All four returns are non-empty and pairwise distinct
// for any input — asserted by TestFieldRendersItsFourStatesPairwiseDistinctly, which compares them
// against each other rather than against string literals, because comparing each against a literal
// passes just as happily after two of them are edited to the same wording.
func (f Field) Render() string {
	shown := f.value
	if f.state == tri.Yes && shown == "" {
		shown = emptyText
	}
	// The undetermined branch's wording belongs to tri and is not a choice made here.
	return f.state.Render(shown, absentText)
}

// MarshalJSON writes the field in the on-disk form: a JSON string when it was recorded, null when
// it is absent, and an object naming the state when it could not be determined.
//
// A WRITTEN EMPTY STRING AND AN ABSENT FIELD ARE DIFFERENT BYTES ON DISK — `""` and `null`. The
// distinction criterion 1 asks for has to survive the round trip, or the renderer is preserving a
// difference the storage layer already threw away.
func (f Field) MarshalJSON() ([]byte, error) {
	switch f.state {
	case tri.Yes:
		return json.Marshal(f.value)
	case tri.No:
		return []byte("null"), nil
	default:
		return json.Marshal(struct {
			Undetermined bool   `json:"undetermined"`
			Reason       string `json:"reason,omitempty"`
		}{true, f.reason})
	}
}

// errFieldForm is what a field that is none of the three encodable forms decodes to. It is an error
// and not a best guess: a field this build does not understand is not evidence of an absence.
var errFieldForm = errors.New("this is not a recognised ticket field")

// UnmarshalJSON reads the three on-disk forms back. Note what it does NOT do: it never turns an
// unreadable field into an absent one.
func (f *Field) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = Absent()
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = Text(s)
		return nil
	}
	var u struct {
		Undetermined bool   `json:"undetermined"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal(b, &u); err == nil && u.Undetermined {
		*f = Undetermined(u.Reason)
		return nil
	}
	return errFieldForm
}
