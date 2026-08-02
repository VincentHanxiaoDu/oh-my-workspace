package inbox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// CRITERION 1 AND CRITERION 12, AS ONE ASSERTION, AND PAIRWISE ON PURPOSE.
//
// Issue #8 criterion 12 is explicit that the three renderings must be compared with each other.
// Asserting each against a string literal — want "(not recorded)" — passes just as happily after
// two of them have been edited to the same wording, because each literal was edited alongside its
// assertion. The only assertion that survives that edit is one that never names the wording.
//
// Four states here rather than three, because criterion 1 needs a written empty value to differ
// from an absence as well.
func TestFieldRendersItsFourStatesPairwiseDistinctly(t *testing.T) {
	cases := []struct {
		what  string
		field Field
	}{
		{"a written value", Text("Reset Ana's login")},
		{"a written but empty value", Text("")},
		{"never recorded", Absent()},
		{"could not be determined", Undetermined("the source channel could not be read")},
	}
	for i, a := range cases {
		if a.field.Render() == "" {
			t.Errorf("%s renders as nothing at all; silence is not one of the answers (PRD §4.3)", a.what)
		}
		for j := i + 1; j < len(cases); j++ {
			b := cases[j]
			if a.field.Render() == b.field.Render() {
				t.Errorf("%s and %s render identically as %q — a real value and a missing value "+
					"must never produce the same output", a.what, b.what, a.field.Render())
			}
		}
	}
}

// A SURVIVING MUTANT, FOUND BY PRODUCT ON PR #29, AND WHY THE TEST ABOVE DID NOT CATCH IT.
//
// Deleting the written-empty branch from Render left the whole suite green. With it gone, a summary
// somebody wrote as the empty string falls through to `tri.Yes.Render("", absentText)`, which
// returns tri's neutral wording for a yes — so the person reads `summary: yes`, the raw name of an
// internal state, where an empty summary should be. It is still distinguishable from the other
// three, which is exactly why the pairwise test passes: pairwise distinctness asserts that the
// four differ and never what any one of them IS.
//
// So this pins the written-empty case specifically. It is the one place a literal-free assertion is
// not enough, and it is asserted against the package's own constant rather than a copy of its text,
// so the wording can still be changed in one place.
func TestAWrittenEmptyValueRendersAsTheWrittenEmptyWordingAndNotAsAStateName(t *testing.T) {
	got := Text("").Render()
	if got != emptyText {
		t.Errorf("a field written as the empty string renders as %q; want %q", got, emptyText)
	}
	// AND NOT AS AN INTERNAL STATE NAME. This is what the deleted branch actually produced, and it
	// is the failure worth naming: a person reading their inbox does not know what "yes" means as
	// the value of a summary.
	for _, state := range []tri.Value{tri.Yes, tri.No} {
		if got == state.String() {
			t.Errorf("a field written as the empty string renders as %q — the bare name of an "+
				"internal state, shown to a person in place of their empty summary", got)
		}
	}
}

// The undetermined rendering is tri's and not this package's: the third answer having one wording
// across the whole product is the reason package tri exists. Compared against tri rather than
// against a literal, so a change in tri's wording moves this with it instead of failing.
func TestUndeterminedFieldUsesTheProductsOneWordingForTheThirdAnswer(t *testing.T) {
	if got, want := Undetermined("why").Render(), tri.Undetermined.Render("", ""); got != want {
		t.Errorf("an undetermined field renders %q; the product's third answer is %q", got, want)
	}
}

// A field's STATE must not collapse the two ways of having no value either — a caller branching on
// State() to decide whether to go looking for the value must not be told "no" by a failure.
func TestFieldStateDistinguishesAbsentFromUndetermined(t *testing.T) {
	if Absent().State() != tri.No {
		t.Errorf("an absent field's state is %v; a determined absence is No", Absent().State())
	}
	if Undetermined("").State() != tri.Undetermined {
		t.Errorf("an undetermined field's state is %v", Undetermined("").State())
	}
	if Text("").State() != tri.Yes {
		t.Errorf("a written empty value's state is %v; it was written, so it is Yes", Text("").State())
	}
	var zero Field
	if zero.State() != tri.Undetermined {
		t.Errorf("the zero Field is %v; a field nobody set has not been determined, and must not "+
			"read as a confident absence", zero.State())
	}
}

// THE DISTINCTION HAS TO SURVIVE THE DISK. A renderer that can tell a written "" from an absence is
// useless if the encoding threw the difference away before it got there — which is exactly what
// `string` with `omitempty` does.
func TestTheFourStatesRoundTripThroughTheOnDiskForm(t *testing.T) {
	for _, f := range []Field{Text("real"), Text(""), Absent(), Undetermined("because")} {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("encoding %q: %v", f.Render(), err)
		}
		var back Field
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("decoding %s: %v", b, err)
		}
		if back.Render() != f.Render() {
			t.Errorf("%q survived the disk as %q (bytes %s)", f.Render(), back.Render(), b)
		}
		if back.State() != f.State() {
			t.Errorf("%q survived the disk in state %v, was %v", f.Render(), back.State(), f.State())
		}
	}
	// And the two states that have no value must be DIFFERENT BYTES, not merely different in
	// memory. If they were the same bytes the round trip above would still pass, by decoding both
	// to whichever one the decoder prefers.
	empty, _ := json.Marshal(Text(""))
	absent, _ := json.Marshal(Absent())
	if string(empty) == string(absent) {
		t.Errorf("a written empty value and an absent field are the same bytes on disk (%s)", empty)
	}
}

// A field the build does not understand is an error, never a best guess. Decoding it as absent
// would be a store that reports damage as "this ticket has no title".
func TestAnUnrecognisedFieldFormIsAnErrorAndNotAnAbsence(t *testing.T) {
	var f Field
	if err := f.UnmarshalJSON([]byte(`[1,2,3]`)); err == nil {
		t.Fatalf("a field of an unrecognised shape decoded without complaint, as %q", f.Render())
	}
}

func TestReasonIsCarriedOnlyByTheUndeterminedState(t *testing.T) {
	if got := Undetermined("the source channel could not be read").Reason(); !strings.Contains(got, "channel") {
		t.Errorf("the reason was lost: %q", got)
	}
	if got := Text("x").Reason(); got != "" {
		t.Errorf("a written field reports a reason %q; it has nothing to explain", got)
	}
}
