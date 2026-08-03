package status

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"

// State is what one subsystem's line says.
//
// FOUR VALUES, AND THE ZERO ONE IS UNDETERMINED. That ordering is criterion 5 defended at the type
// level: a Subsystem an error path returned without setting State must not read as a confident "it
// is not working". [tri.Value] makes the same choice for the same reason, and this type follows it
// rather than inventing a second convention — see that package's comment.
//
// It is not a tri.Value plus a bool, because "not configured" is neither a yes nor a no about
// whether the thing works: a hub nobody has set up is not a broken hub. Criterion 1 requires it to
// be distinguishable from BOTH running and failing, and three renderings out of a two-valued type
// plus a flag is how two of them eventually collapse.
type State int

const (
	// Undetermined is the ZERO VALUE on purpose: nothing established this subsystem's state.
	// §4.3 — never rendered as a negative and never as silence.
	Undetermined State = iota
	// Working means the subsystem was checked and it is running.
	Working
	// NotWorking means the subsystem was checked and it is NOT running. A determined negative.
	NotWorking
	// NotConfigured means there is nothing here to run because the person has not set it up. A
	// determined answer, and not a failure.
	NotConfigured
)

// Word is the machine-readable state, one token per value.
//
// IT IS THE ONE PLACE THE FOUR ARE SPELLED FOR A MACHINE, and both surfaces call it: the CLI line
// carries this token and the control API's JSON carries this token. Criteria 9–12 ask that the two
// surfaces agree subsystem-by-subsystem including which are undetermined, and one shared function
// is how that is true by construction rather than by two tables that match today.
//
// A State outside the four is undetermined rather than blank, on [tri.Value.String]'s reasoning.
func (s State) Word() string {
	switch s {
	case Working:
		return "working"
	case NotWorking:
		return "not_working"
	case NotConfigured:
		return "not_configured"
	default:
		return "undetermined"
	}
}

// String is the four states as a person reads them. Each is its own sentence and none is empty.
//
// TestStateRendersFourWaysPairwiseDistinctly compares every pair against every other pair rather
// than against string literals, because asserting each against its own literal keeps passing after
// two of them have been edited into the same wording.
func (s State) String() string {
	switch s {
	case Working:
		return "working"
	case NotWorking:
		return "NOT working"
	case NotConfigured:
		return "not configured"
	default:
		// The one wording for the third answer, taken from tri rather than respelled here — so
		// that a person meets the same phrase in status as in health, projects and devices.
		return tri.Undetermined.String()
	}
}

// Determined reports whether this state is a real answer. "Not configured" IS one: the product
// knows exactly what is going on, which is that there is nothing there.
//
// A summary asks this to decide whether it may lead with "everything is fine" (criterion 8).
func (s State) Determined() bool {
	return s == Working || s == NotWorking || s == NotConfigured
}

// stateFromWord is the inverse of [State.Word], for a reader of the control API's JSON.
//
// A token this build does not know becomes Undetermined — which is the correct answer about a word
// whose meaning was not established, and never a negative (criterion 11).
// It decodes by asking [State.Word] rather than by repeating the tokens, so a reworded token
// cannot make the encoder and the decoder disagree about the same subsystem.
func stateFromWord(w string) State {
	for _, s := range []State{Working, NotWorking, NotConfigured} {
		if s.Word() == w {
			return s
		}
	}
	return Undetermined
}

// fromTri lifts the product's three-valued answer into a State. There is no branch that turns an
// undetermined tri into a NotWorking state, which is the whole point of having the conversion in
// one function instead of at each call site.
func fromTri(v tri.Value) State {
	switch v {
	case tri.Yes:
		return Working
	case tri.No:
		return NotWorking
	default:
		return Undetermined
	}
}
