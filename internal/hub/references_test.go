package hub

import (
	"strings"
	"testing"
)

// Criterion 1: a body can carry a reference to a person, to a group and to another note, and each
// is recoverable as a reference — distinguishable from ordinary body text that happens to contain
// the same characters.
func TestParseReferencesFindsAllThreeKinds(t *testing.T) {
	body := "we rewrote login with [[person:alice]] and [[group:platform]]; see [[note:note-3]]."
	got := ParseReferences(body)
	if len(got) != 3 {
		t.Fatalf("got %d references, want 3: %+v", len(got), got)
	}
	want := []Reference{
		{Kind: RefPerson, Target: "alice"},
		{Kind: RefGroup, Target: "platform"},
		{Kind: RefNote, Target: "note-3"},
	}
	for i, w := range want {
		if !got[i].SameTarget(w) {
			t.Errorf("reference %d is %+v, want %+v", i, got[i], w)
		}
		if got[i].Start >= got[i].End || body[got[i].Start:got[i].End] == "" {
			t.Errorf("reference %d has no span in the body: %+v", i, got[i])
		}
	}
}

// The other half of criterion 1: prose containing the same characters is NOT a reference, and an
// escaped token is prose.
func TestOrdinaryTextIsNotAReference(t *testing.T) {
	for _, body := range []string{
		"alice and platform and note-3 are words in a sentence",
		"person:alice is how you would write it if there were no brackets",
		`the syntax is \[[person:alice]], which you write with a backslash`,
		"[[not-a-kind:alice]] names no kind this product has",
		"[[person:]] names nobody",
		"[[person:with a space]] is not a target",
	} {
		if got := ParseReferences(body); len(got) != 0 {
			t.Errorf("body %q parsed %d references, want none: %+v", body, len(got), got)
		}
	}
}

// An escaped token renders as the characters the author asked for, with the backslash gone — and
// what it renders as is NOT what a resolved reference to the same target renders as.
func TestEscapedTokenRendersAsItsOwnCharacters(t *testing.T) {
	body := `write it as \[[note:note-3]] in your draft`
	got := RenderBody(body, func(Reference) RefState { return StateResolved })
	if want := "write it as [[note:note-3]] in your draft"; got != want {
		t.Fatalf("escaped token rendered %q, want %q", got, want)
	}
	resolved := RenderBody("write it as [[note:note-3]] in your draft", func(Reference) RefState { return StateResolved })
	if got == resolved {
		t.Errorf("an escaped token and a resolved reference render identically as %q; criterion 1 is that\n"+
			"body text containing the same characters is distinguishable from a reference", got)
	}
}

// CRITERION 17, AND CRITERIA 11, 12 AND 14 THROUGH IT: no two of these renderings may be the same,
// and none of them may be the same as ordinary prose.
//
// COMPARED PAIRWISE, NOT AGAINST LITERALS. Asserting each rendering against a string literal passes
// just as happily after two of them have been edited into the same wording, which is the defect
// this test exists to catch.
func TestReferenceRenderingsArePairwiseDistinct(t *testing.T) {
	all := AllReferenceRenderings()
	if len(all) < 4 {
		t.Fatalf("only %d renderings offered; this comparison would be nearly vacuous", len(all))
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if all[a] == all[b] {
				t.Errorf("%q and %q render identically as %q; they are different facts and must read as\n"+
					"different things", a, b, all[a])
			}
		}
		if strings.TrimSpace(all[names[i]]) == "" {
			t.Errorf("%q renders as nothing; silence is not one of the answers", names[i])
		}
	}

	// The hidden state is the one rendering that IS nothing, and it is asserted here rather than
	// in the map above, where the "silence is not an answer" check would contradict it. It must be
	// EMPTY: not a marker, not a placeholder, and — criterion 12 — not the unresolved wording.
	// A mutation that made it render like an unresolved reference passed the pairwise loop above,
	// because that loop compared renderings of DIFFERENT targets. This is what caught it.
	r := Reference{Kind: RefNote, Target: "note-9"}
	if got := RenderReference(r, StateHidden); got != "" {
		t.Errorf("a reference the reader may not see rendered as %q; criterion 7 forbids a title, an\n"+
			"identifier, a slug, a placeholder and a marker alike", got)
	}
}

// The four states of ONE reference, compared pairwise against each other. Comparing states of
// different targets is not this assertion: two states can differ only because their targets do.
func TestTheFourStatesOfOneReferenceAllDiffer(t *testing.T) {
	r := Reference{Kind: RefNote, Target: "note-9"}
	states := []RefState{StateResolved, StateUnresolved, StateUndetermined, StateHidden}
	for i := 0; i < len(states); i++ {
		for j := i + 1; j < len(states); j++ {
			a, b := RenderReference(r, states[i]), RenderReference(r, states[j])
			if a == b {
				t.Errorf("the %s and %s states of the SAME reference both render as %q; they are\n"+
					"different facts about it", states[i], states[j], a)
			}
		}
	}
}

// Criterion 2: a person reference and a note reference to targets with the same display name are
// distinguishable in output WITHOUT the reader inspecting the target.
func TestSameNameDifferentKindRendersDifferently(t *testing.T) {
	const name = "platform"
	seen := map[string]RefKind{}
	for _, k := range []RefKind{RefPerson, RefGroup, RefNote} {
		r := Reference{Kind: k, Target: name}
		for _, st := range []RefState{StateResolved, StateUnresolved} {
			out := RenderReference(r, st)
			key := st.String() + "|" + out
			if prev, dup := seen[key]; dup {
				t.Errorf("a %s reference and a %s reference to %q both render as %q in state %s",
					prev, k, name, out, st)
			}
			seen[key] = k
		}
		if !strings.Contains(RenderReference(r, StateResolved), string(k)) {
			t.Errorf("a resolved %s reference does not say its kind: %q", k, RenderReference(r, StateResolved))
		}
	}
	// And the kind is on the value itself, so a caller need not parse the rendering either.
	if (Reference{Kind: RefPerson, Target: name}).SameTarget(Reference{Kind: RefNote, Target: name}) {
		t.Error("a person and a note with the same name compare as the same target")
	}
}

// The undetermined rendering names neither the kind nor the target. If we could not work out
// whether this reader may see it, saying what it is would be a disclosure made on not knowing.
func TestUndeterminedRenderingNamesNothing(t *testing.T) {
	r := Reference{Kind: RefNote, Target: "note-secret"}
	out := RenderReference(r, StateUndetermined)
	if strings.Contains(out, "note-secret") {
		t.Errorf("the undetermined rendering %q names its target", out)
	}
	if strings.Contains(out, string(RefNote)) {
		t.Errorf("the undetermined rendering %q names its kind", out)
	}
}

// CRITERION 7, at the level of the rendered body: a hidden reference leaves NOTHING — no
// placeholder, no marker, and no gap in the prose either.
func TestHiddenReferenceLeavesTheProseAsIfItWasNeverThere(t *testing.T) {
	cases := []struct{ with, without string }{
		{"the rewrite is explained in [[note:note-9]] and in the wiki.", "the rewrite is explained in and in the wiki."},
		{"ask [[person:alice]].", "ask."},
		{"[[note:note-9]] explains it", "explains it"},
		{"see [[note:note-9]]", "see"},
	}
	for _, c := range cases {
		got := RenderBody(c.with, func(Reference) RefState { return StateHidden })
		if got != c.without {
			t.Errorf("hiding the reference in %q gave %q, want %q — a reader must not be able to see\n"+
				"that something was taken out", c.with, got, c.without)
		}
		// And the control: prose that never had a reference renders unchanged, so the two bodies
		// are byte-identical for the reader who may not see the target.
		if again := RenderBody(c.without, func(Reference) RefState { return StateHidden }); again != c.without {
			t.Errorf("prose with no reference rendered as %q, want %q unchanged", again, c.without)
		}
	}
}

// A dangling reference does not take the rest of the listing with it (the Issue's "must not break
// the listing"), tested here at the rendering level and again against the store in
// references_read_test.go.
func TestOneUnresolvedReferenceDoesNotAffectTheOthers(t *testing.T) {
	body := "[[note:note-1]] and [[note:gone]] and [[person:alice]]"
	got := RenderBody(body, func(r Reference) RefState {
		if r.Target == "gone" {
			return StateUnresolved
		}
		return StateResolved
	})
	for _, want := range []string{
		RenderReference(Reference{Kind: RefNote, Target: "note-1"}, StateResolved),
		RenderReference(Reference{Kind: RefNote, Target: "gone"}, StateUnresolved),
		RenderReference(Reference{Kind: RefPerson, Target: "alice"}, StateResolved),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered body %q is missing %q", got, want)
		}
	}
}

// The zero RefState is undetermined, for the reason tri.Undetermined is the zero tri.Value: a state
// nobody set has not been worked out.
func TestZeroRefStateIsUndetermined(t *testing.T) {
	var zero RefState
	if zero != StateUndetermined {
		t.Fatalf("the zero RefState is %v; a state nobody set must not read as a real one", zero)
	}
	if RenderBody("[[note:x]]", nil) != RenderReference(Reference{Kind: RefNote, Target: "x"}, StateUndetermined) {
		t.Error("rendering with no state function did not answer undetermined")
	}
}

func TestDistinctReferencesKeepsFirstAppearanceOrder(t *testing.T) {
	body := "[[person:bo]] [[note:n1]] [[person:bo]]"
	got := DistinctReferences(body)
	if len(got) != 2 || got[0].Target != "bo" || got[1].Target != "n1" {
		t.Fatalf("got %+v, want bo then n1, each once", got)
	}
	if occurrences := ParseReferences(body); len(occurrences) != 3 {
		t.Errorf("ParseReferences collapsed duplicates: got %d, want 3", len(occurrences))
	}
}
