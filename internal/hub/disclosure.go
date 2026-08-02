package hub

import (
	"fmt"
	"strings"
)

// RestrictionStatement is PRD §2.4, said where a person chooses.
//
// WHY IT IS A CONSTANT AND NOT PROSE IN EACH SURFACE. Issue #12 criterion 7 requires it at EVERY
// point of choice — the CLI path and the agent API's schema description — and criterion 9 requires
// it on the hundredth publication as much as the first, so it cannot live in onboarding. Two
// surfaces each writing their own sentence is how one of them ends up softer than the other; one
// constant means the test that greps output surfaces is checking the same words a person reads.
//
// It says three things on purpose: what restriction DOES control (which colleagues see the note),
// what it does NOT (the operator), and why (the hub indexes it, so it must read it).
const RestrictionStatement = "Restriction controls which colleagues can see this note. " +
	"It is not a wall against whoever operates the hub: the hub stores and indexes every published note, " +
	"including notes narrowed to named people, to a group, or to yourself. " +
	"The genuinely private note is the one never published."

// overclaimingWords are the words criterion 8 names.
//
// A surface may use them — "private" is a normal English word and forbidding it outright would
// push surfaces into weasel phrasing — but a surface that uses one MUST carry
// [RestrictionStatement] in the same view, so the person reads the qualification in the same
// breath as the claim.
var overclaimingWords = []string{
	"private",
	"encrypted",
	"secret",
	"only you can see this",
	"nobody else can see",
	"invisible to the hub",
}

// CheckSurface is the §2.4 rule, applied to one rendered surface.
//
// It returns an error when text either (a) uses one of the overclaiming words without
// [RestrictionStatement] present, or (b) offers a narrowing choice without it. Both are criterion 8
// and criterion 7 respectively, and both are checked here so that there is ONE rule rather than a
// habit.
//
// offersChoice says whether this surface is a point of choice. A surface that merely reports a
// note's visibility back is not offering a choice, but it is still bound by (a).
func CheckSurface(name, text string, offersChoice bool) error {
	lower := strings.ToLower(text)
	hasStatement := strings.Contains(text, RestrictionStatement)
	for _, w := range overclaimingWords {
		if strings.Contains(lower, w) && !hasStatement {
			return fmt.Errorf("surface %q uses %q without the §2.4 statement in the same view", name, w)
		}
	}
	if offersChoice && !hasStatement {
		return fmt.Errorf("surface %q offers a visibility choice without the §2.4 statement at the point of choice", name)
	}
	return nil
}

// ChoiceBlock is the point-of-choice text every surface prints: the four choices and the §2.4
// statement, together, every time.
//
// Surfaces call this rather than assembling their own, so "the statement was dropped from one of
// them" is not a thing that can happen in a code review's blind spot.
func ChoiceBlock() string {
	var b strings.Builder
	b.WriteString("Who can see this note?\n")
	for _, line := range ChoiceSyntax {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	b.WriteString("\n")
	b.WriteString(RestrictionStatement)
	b.WriteString("\n")
	return b.String()
}
