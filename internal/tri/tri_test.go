package tri

import "testing"

// The zero value must be Undetermined. A struct field an error path never assigned must not read
// as a confident "no" — reversing the iota order is a one-character change that would do exactly
// that, and nothing else in the package would notice.
func TestZeroValueIsUndetermined(t *testing.T) {
	var v Value
	if v != Undetermined {
		t.Fatalf("the zero Value is %d, want Undetermined (%d)", v, Undetermined)
	}
	if v == No {
		t.Fatal("the zero Value is No — an answer nobody gave is being reported as a negative")
	}
	// A struct is how this actually reaches production: a field left alone by an early return.
	var s struct{ Encrypted Value }
	if s.Encrypted.Determined() {
		t.Fatal("an unset struct field reports itself as determined")
	}
}

// PAIRWISE, NOT AGAINST LITERALS. Asserting String() == "yes" and String() == "no" and
// String() == "could not be determined" passes just as happily if two of them are later edited to
// the same wording — the test would be checking the constants against a copy of themselves. What
// the criterion actually asks is that no two of the three collapse, so compare the three to each
// other, and to the empty string, which is the silence §4.3 forbids.
func TestThreeRenderingsAreMutuallyDistinct(t *testing.T) {
	renderings := map[string]string{
		"Undetermined": Undetermined.String(),
		"Yes":          Yes.String(),
		"No":           No.String(),
	}
	seen := map[string]string{}
	for name, got := range renderings {
		if got == "" {
			t.Errorf("%s renders as the empty string — silence is not one of the three answers", name)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s and %s both render as %q — two answers collapsed into one", name, other, got)
		}
		seen[got] = name
	}
}

// The same property must hold for Render, which is what capabilities actually call. The neutral
// String() staying distinct is no comfort if the domain wording collapses.
func TestRenderKeepsTheThreeDistinct(t *testing.T) {
	// Health's wording, from PRD §4.1 — the case the product principle is written about.
	got := map[string]string{
		"Undetermined": Undetermined.Render("enabled", "not enabled"),
		"Yes":          Yes.Render("enabled", "not enabled"),
		"No":           No.Render("enabled", "not enabled"),
	}
	if got["Undetermined"] == got["No"] {
		t.Errorf("undetermined renders as %q, the same as not-enabled — §4.3's exact prohibition", got["No"])
	}
	if got["Undetermined"] == got["Yes"] || got["Yes"] == got["No"] {
		t.Errorf("two of the three renderings collapsed: %#v", got)
	}
	for name, s := range got {
		if s == "" {
			t.Errorf("%s rendered as the empty string", name)
		}
	}
}

// A caller must not be able to spell the third answer itself. If Render ever grew an
// undetermined parameter, one capability would say "unknown" and another "none" — and "none" is a
// negative.
func TestRenderIgnoresCallerWordingForUndetermined(t *testing.T) {
	if got := Undetermined.Render("enabled", "not enabled"); got != undeterminedText {
		t.Errorf("Undetermined.Render = %q, want the fixed wording %q", got, undeterminedText)
	}
}

// Empty domain wording must not produce a blank line.
func TestRenderFallsBackRatherThanRenderingSilence(t *testing.T) {
	if got := Yes.Render("", ""); got == "" {
		t.Error("Yes.Render with empty wording produced silence")
	}
	if got := No.Render("", ""); got == "" {
		t.Error("No.Render with empty wording produced silence")
	}
	if Yes.Render("", "") == No.Render("", "") {
		t.Error("the fallback wording collapsed yes and no into one string")
	}
}

// An error is not a "no". This is the project's first named convention and the reason the type
// exists at all.
func TestFromErrorNeverProducesNo(t *testing.T) {
	boom := errString("the underlying query is unavailable")
	// Both truth values, because a check that fails having computed `false` is the tempting case:
	// the bool looks usable and dropping the error looks harmless.
	for _, ok := range []bool{true, false} {
		if got := FromError(ok, boom); got != Undetermined {
			t.Errorf("FromError(%v, err) = %v, want Undetermined — a failed check reported an answer", ok, got)
		}
		if FromError(ok, boom) == No {
			t.Errorf("FromError(%v, err) produced No — 'could not determine' became 'determined to be nothing'", ok)
		}
	}
	if got := FromError(true, nil); got != Yes {
		t.Errorf("FromError(true, nil) = %v, want Yes", got)
	}
	if got := FromError(false, nil); got != No {
		t.Errorf("FromError(false, nil) = %v, want No", got)
	}
}

// Determined must be false for exactly the third value, so a status summary can ask "may I say
// everything is fine?" without re-deriving the rule.
func TestDetermined(t *testing.T) {
	if !Yes.Determined() || !No.Determined() {
		t.Error("a real answer reports itself as undetermined")
	}
	if Undetermined.Determined() {
		t.Error("Undetermined reports itself as determined")
	}
}

// A value outside the three constants is undetermined, not a panic and not a blank.
func TestOutOfRangeValueIsUndetermined(t *testing.T) {
	v := Value(99)
	if got := v.String(); got != undeterminedText {
		t.Errorf("Value(99).String() = %q, want %q", got, undeterminedText)
	}
	if v.Determined() {
		t.Error("Value(99) reports itself as determined")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
