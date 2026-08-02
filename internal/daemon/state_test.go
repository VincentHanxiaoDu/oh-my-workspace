package daemon

import (
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// allEndings is every value Ending has. A value added without being added here is caught by
// TestEveryEndingIsCoveredByTheseTests.
var allEndings = []Ending{
	EndingUndetermined,
	EndingNeverRun,
	EndingStopped,
	EndingCannotWrite,
	EndingCrashed,
}

// TestEndingRenderingsAreDistinctPairwise is the test criteria 10–12 actually need.
//
// ASSERTING EACH RENDERING AGAINST ITS OWN STRING LITERAL WOULD NOT DO IT. Five assertions of the
// form `if EndingCrashed.String() != "crashed"` all still pass after somebody edits two of the
// cases to the same sentence, because each assertion only ever looks at one of them. Comparing
// every pair is the property "these are distinguishable", stated as a property.
func TestEndingRenderingsAreDistinctPairwise(t *testing.T) {
	for i, a := range allEndings {
		if strings.TrimSpace(a.String()) == "" {
			t.Errorf("Ending(%d) renders as blank; silence is not one of the answers (PRD §4.3)", a)
		}
		for j, b := range allEndings {
			if i >= j {
				continue
			}
			if a.String() == b.String() {
				t.Errorf("Ending(%d) and Ending(%d) both render as %q; criteria 10–12 require them to be told apart",
					a, b, a.String())
			}
		}
	}
}

// TestUndeterminedEndingUsesTheProductsOneWordingForIt keeps the third answer spelled the way the
// rest of the product spells it. A second wording is how "could not be determined" in one place
// stops matching "could not be determined" in another, and a person stops being able to tell
// whether they are the same state.
func TestUndeterminedEndingUsesTheProductsOneWordingForIt(t *testing.T) {
	if !strings.Contains(EndingUndetermined.String(), tri.Undetermined.String()) {
		t.Errorf("the undetermined ending renders as %q, which does not contain the product's wording %q",
			EndingUndetermined.String(), tri.Undetermined.String())
	}
	for _, e := range allEndings {
		if e == EndingUndetermined {
			continue
		}
		if strings.Contains(e.String(), tri.Undetermined.String()) {
			t.Errorf("Ending(%d) renders as %q, which contains the undetermined wording; a determined answer must not read as the third one", e, e)
		}
	}
}

// TestNeverRunIsNotUndeterminedAndNotEmpty is criterion 11 as a unit: "never run" is a DETERMINED
// answer, distinguishable from every recorded ending and from an empty value.
func TestNeverRunIsNotUndeterminedAndNotEmpty(t *testing.T) {
	if !EndingNeverRun.Determined() {
		t.Error("never run is a determined answer: the product knows exactly what happened, which is nothing")
	}
	if EndingUndetermined.Determined() {
		t.Error("the undetermined ending must not report as determined")
	}
	if EndingNeverRun.String() == "" {
		t.Error("never run rendered as an empty value, which criterion 11 forbids")
	}
}

// TestEndingCodesRoundTripAndUnknownCodesAreUndetermined pins the on-disk spelling.
//
// The codes are separate from the renderings so that rewording a sentence for a person does not
// reinterpret a record written yesterday; this asserts they really are separate and that a code
// this build does not know becomes the third answer rather than a guessed one.
func TestEndingCodesRoundTripAndUnknownCodesAreUndetermined(t *testing.T) {
	for _, e := range allEndings {
		if got := codeFor(e).ending(); got != e {
			t.Errorf("Ending(%d) stored as %q and read back as Ending(%d)", e, codeFor(e), got)
		}
	}
	if got := endingCode("something-a-later-build-wrote").ending(); got != EndingUndetermined {
		t.Errorf("an unknown ending code read back as Ending(%d); it must be undetermined, never a guess", got)
	}
}

// TestEveryEndingIsCoveredByTheseTests catches a sixth Ending added without a rendering.
//
// It works by scanning upward from the highest value this file knows: an Ending one past the end
// that does NOT render as undetermined means somebody added a constant and a String case and did
// not add it to allEndings, so the pairwise test above silently stopped covering it.
func TestEveryEndingIsCoveredByTheseTests(t *testing.T) {
	next := Ending(len(allEndings))
	if next.String() != EndingUndetermined.String() {
		t.Errorf("Ending(%d) renders as %q, so a value exists that allEndings does not list — "+
			"the pairwise distinctness test is not covering it", next, next.String())
	}
}

// TestReportRoundTripsThroughItsWireForm is criterion 14's mechanism.
//
// The tri values travel as their own renderings rather than as integers, because an integer whose
// zero value means "undetermined" is reinterpreted by any future reordering of the constants — and
// the reinterpretation would land on the value the product most cares about not getting wrong.
func TestReportRoundTripsThroughItsWireForm(t *testing.T) {
	for _, want := range []Report{
		{Running: tri.Yes, Healthy: tri.Yes, Control: tri.Yes, LastRun: EndingStopped},
		{Running: tri.No, Healthy: tri.No, Control: tri.No, LastRun: EndingNeverRun},
		{Running: tri.Undetermined, Healthy: tri.Undetermined, Control: tri.Undetermined, LastRun: EndingUndetermined},
		{Running: tri.Yes, Healthy: tri.No, Control: tri.No, LastRun: EndingCannotWrite},
		{Running: tri.No, Healthy: tri.No, Control: tri.No, LastRun: EndingCrashed},
	} {
		w := want
		w.wire()
		got := Report{RunningText: w.RunningText, HealthyText: w.HealthyText, ControlText: w.ControlText, LastRunText: w.LastRunText}
		got.unwire()
		if got.Running != want.Running || got.Healthy != want.Healthy || got.Control != want.Control || got.LastRun != want.LastRun {
			t.Errorf("a report of %+v came back as running=%v healthy=%v control=%v lastRun=%v",
				want, got.Running, got.Healthy, got.Control, got.LastRun)
		}
	}
}
