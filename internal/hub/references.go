package hub

import (
	"strings"
)

// This file is Issue #14: a note's inline references to people, groups and other notes, and the
// four states one can be in when a particular reader looks at it.
//
// WHY REFERENCES ARE DERIVED FROM A VERSION'S BODY RATHER THAN STORED BESIDE IT.
//
// Criterion 3 says a reference set belongs to a VERSION: "a reference added in v3 is absent from
// v2's reference set, and a reference removed in v3 is present in v2's". A stored index would have
// to be written at publication time, kept in step with every amendment, and migrated whenever the
// syntax changed — three ways for the index and the text to disagree, and the disagreement would
// be invisible until somebody read v2 and got v3's edges. The body of a version is already stored
// and is already immutable, so parsing it is the one representation that cannot drift from what
// the author actually wrote. It also means Issue #11 can add versions without teaching an index
// about them.
//
// The cost is stated rather than hidden: resolving a note's references is O(body), and answering
// "what else was written about this" is O(corpus). A hub with a large corpus will want an index;
// building one is a performance change, and the test for criterion 3 is what will keep it honest.

// RefKind is which of the three things a reference points at (PRD §3.4: "people, groups and other
// notes").
//
// It is part of what a listing returns — criterion 2: "A person reference and a note reference to
// targets with the same display name are distinguishable from each other in output without the
// reader inspecting the target." Two references that differ only in kind must therefore never
// render identically, and a test compares those renderings pairwise.
type RefKind string

const (
	// RefPerson points at a colleague in the hub's own record of people (PRD §5.3).
	RefPerson RefKind = "person"
	// RefGroup points at a group in the hub's own membership record.
	RefGroup RefKind = "group"
	// RefNote points at another published note.
	RefNote RefKind = "note"
)

// KnownRefKind reports whether k is one of the three. There is no fourth, and an unrecognised kind
// is not a reference at all — see [ParseReferences].
func KnownRefKind(k RefKind) bool {
	return k == RefPerson || k == RefGroup || k == RefNote
}

// Reference is one inline reference, as the author wrote it.
//
// Start and End are the byte range of the whole token in the body it was parsed from. They exist so
// the body can be re-rendered for a particular reader — including with a reference the reader may
// not see removed, which is criterion 7 and cannot be done from the token alone.
type Reference struct {
	Kind   RefKind
	Target string
	Start  int
	End    int
}

// Person, Group and Note give the target its typed identity, and answer "" for a reference of
// another kind. A caller that wants a PersonID out of a group reference has a bug, and getting ""
// is easier to notice than getting a PersonID that happens to spell a group's name.
func (r Reference) Person() PersonID {
	if r.Kind != RefPerson {
		return ""
	}
	return PersonID(r.Target)
}

func (r Reference) Group() GroupID {
	if r.Kind != RefGroup {
		return ""
	}
	return GroupID(r.Target)
}

func (r Reference) Note() NoteID {
	if r.Kind != RefNote {
		return ""
	}
	return NoteID(r.Target)
}

// SameTarget reports whether two references point at the same thing. Kind is part of the identity:
// a person called "platform" and a group called "platform" are not the same target, which is
// criterion 2 read as an equality rather than as a rendering.
func (r Reference) SameTarget(o Reference) bool {
	return r.Kind == o.Kind && r.Target == o.Target
}

// The reference syntax.
//
// WHY A MARKER AND NOT A BARE NAME. Criterion 1 requires a reference be "distinguishable, in the
// client's output, from ordinary body text that happens to contain the same characters". A syntax
// that promoted any occurrence of a colleague's name to a reference would make a note ABOUT a
// person indistinguishable from a note that references them, and would make the reference set of a
// note change whenever the hub learned a new name — the body would stop being the record of what
// the author wrote. So a reference is written, deliberately, as a token.
const (
	refOpen  = "[["
	refClose = "]]"
	// refEscape before the opening marker means the author wanted those characters and not a
	// reference. Without it there is no way to write about the syntax inside a note, and criterion
	// 1's "body text that happens to contain the same characters" would be unwritable rather than
	// merely distinguishable.
	refEscape = '\\'
)

// ReferenceSyntax is the accepted spelling, shown wherever a person is told how to write one. One
// list, printed by the CLI, so the syntax cannot be documented in two places and drift.
var ReferenceSyntax = []string{
	"[[person:<id>]]    a colleague, by the hub's own identifier for them",
	"[[group:<name>]]   a group, by the hub's own membership record",
	"[[note:<id>]]      another published note",
	`\[[person:<id>]]   a backslash writes the characters and not a reference`,
}

// span is one reference-shaped token found in a body, escaped or not.
type span struct {
	start, end int // byte range of the whole token, including a leading backslash if escaped
	ref        Reference
	escaped    bool
}

// scanBody finds every reference-shaped token, in the order they appear.
//
// A token whose contents are not `kind:target` with a known kind is NOT a reference and is not
// returned: it is ordinary body text that happens to contain brackets, and rewriting it would be
// this code editing a person's prose on a guess.
func scanBody(body string) []span {
	var out []span
	for i := 0; i+len(refOpen) <= len(body); {
		j := strings.Index(body[i:], refOpen)
		if j < 0 {
			break
		}
		open := i + j
		closeAt := strings.Index(body[open+len(refOpen):], refClose)
		if closeAt < 0 {
			break
		}
		contentStart := open + len(refOpen)
		contentEnd := contentStart + closeAt
		end := contentEnd + len(refClose)

		kind, target, ok := parseRefContent(body[contentStart:contentEnd])
		if !ok {
			// Not a reference. Continue AFTER the opening marker rather than after the token, so
			// that "[[ [[note:x]]" still finds the real one inside it.
			i = contentStart
			continue
		}
		escaped := open > 0 && body[open-1] == refEscape
		start := open
		if escaped {
			start = open - 1
		}
		out = append(out, span{
			start:   start,
			end:     end,
			escaped: escaped,
			ref:     Reference{Kind: kind, Target: target, Start: start, End: end},
		})
		i = end
	}
	return out
}

func parseRefContent(s string) (RefKind, string, bool) {
	k, target, found := strings.Cut(s, ":")
	if !found {
		return "", "", false
	}
	kind := RefKind(strings.ToLower(strings.TrimSpace(k)))
	target = strings.TrimSpace(target)
	if !KnownRefKind(kind) || target == "" || strings.ContainsAny(target, " \t\n[]") {
		return "", "", false
	}
	return kind, target, true
}

// ParseReferences returns the references a body carries, in the order they appear, escaped tokens
// excluded.
//
// Duplicates are KEPT. A note that names the same colleague twice referenced them twice, and the
// positions differ, which the renderer needs. [DistinctReferences] is for callers that want the set.
func ParseReferences(body string) []Reference {
	spans := scanBody(body)
	out := make([]Reference, 0, len(spans))
	for _, sp := range spans {
		if sp.escaped {
			continue
		}
		out = append(out, sp.ref)
	}
	return out
}

// DistinctReferences returns each distinct target once, in first-appearance order.
func DistinctReferences(body string) []Reference {
	seen := map[Reference]bool{}
	var out []Reference
	for _, r := range ParseReferences(body) {
		key := Reference{Kind: r.Kind, Target: r.Target}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// RefState is what a particular reader's view of a reference is. It is NOT a property of the
// reference — the same reference is resolved for one colleague and hidden from another, which is
// the whole of criterion 7.
//
// The ZERO VALUE IS StateUndetermined, for the reason tri.Undetermined is: a state nobody set has
// not been worked out, and a state left zero by an error path must not read as a resolved
// reference or as a missing one.
type RefState int

const (
	// StateUndetermined — whether this reference resolves could not be worked out: the hub could
	// not be reached, the membership record could not be read, or the reader is unidentified.
	// Criterion 14. It is never rendered as "does not exist" and never omitted.
	StateUndetermined RefState = iota
	// StateResolved — the target exists and this reader may read it.
	StateResolved
	// StateUnresolved — the target is GONE, and the reader would have been permitted to see it.
	// Criterion 11: shown, marked, never dropped.
	StateUnresolved
	// StateHidden — the target exists and this reader may not read it. Criterion 7: NOTHING about
	// it appears in this reader's output, including its absence.
	//
	// A [ReferenceView] is never in this state: hidden references are removed before a listing is
	// built, so a caller cannot render one by accident. The constant exists because the renderer
	// needs to be told to elide, and because criterion 12 is precisely that this and
	// StateUnresolved are different facts with different renderings.
	StateHidden
)

func (s RefState) String() string {
	switch s {
	case StateResolved:
		return "resolved"
	case StateUnresolved:
		return "unresolved"
	case StateHidden:
		return "hidden"
	default:
		return "undetermined"
	}
}

// RenderReference is how one reference reads in a body, in each state.
//
// CRITERIA 11, 12, 14 AND 17 ARE ALL ABOUT THIS FUNCTION, and they are all of the form "these two
// must not look the same". The test for it compares the renderings PAIRWISE rather than each
// against a literal, because asserting each against a literal passes just as happily after two of
// them have been edited into the same wording.
//
// The undetermined rendering names NEITHER the kind NOR the target. If we could not work out
// whether this reader may see the target, saying what it is would be a disclosure made on the
// strength of not knowing.
func RenderReference(r Reference, st RefState) string {
	switch st {
	case StateResolved:
		return "[" + string(r.Kind) + " " + r.Target + "]"
	case StateUnresolved:
		return "[" + string(r.Kind) + " " + r.Target + " — unresolved reference: the target is no longer there]"
	case StateHidden:
		// NOT A PLACEHOLDER, NOT A MARKER, NOT AN ELLIPSIS. Criterion 7 lists those by name.
		return ""
	default:
		return "[a reference whose target could not be determined]"
	}
}

// RenderBody re-renders a body for one reader, replacing each reference with its rendering in that
// reader's view and unescaping the tokens the author escaped.
//
// THE SEAM IS THE INTERESTING PART. A hidden reference is removed, and removing it from
// "explained in [[note:n]] and in the wiki" naively leaves two spaces where one was — a gap that
// tells the reader something was taken out, which is the "gap in the numbering" criterion 7
// forbids in its other form. So the whitespace either side of a removed token is closed up, and a
// test asserts that the result is byte-identical to the same prose written with no reference in it.
func RenderBody(body string, stateOf func(Reference) RefState) string {
	if stateOf == nil {
		stateOf = func(Reference) RefState { return StateUndetermined }
	}
	spans := scanBody(body)
	out := make([]byte, 0, len(body))
	cursor := 0
	for _, sp := range spans {
		out = append(out, body[cursor:sp.start]...)
		cursor = sp.end
		if sp.escaped {
			// The characters the author asked for, without the backslash that asked for them.
			out = append(out, body[sp.start+1:sp.end]...)
			continue
		}
		st := stateOf(sp.ref)
		// EVERY STATE GOES THROUGH RenderReference, including the hidden one — which renders as
		// nothing. Special-casing hidden here instead left that rendering unreachable from the
		// only path that produces a body, so a change making it render as a marker was invisible
		// to every test that read a body. Found by mutating it and watching the suite stay green.
		out = append(out, RenderReference(sp.ref, st)...)
		if st == StateHidden {
			out, cursor = closeSeam(out, body, cursor)
		}
	}
	out = append(out, body[cursor:]...)
	return string(out)
}

// closeSeam removes the whitespace that a removed token would otherwise have left doubled or
// stranded. It returns the possibly-trimmed output and the possibly-advanced cursor.
func closeSeam(out []byte, body string, cursor int) ([]byte, int) {
	leftSpace := len(out) > 0 && (out[len(out)-1] == ' ' || out[len(out)-1] == '\t')
	// A reference at the start of a line has no space to its left, and the space to its RIGHT is
	// the one that would be left stranded. Missing this case left a leading space, which is exactly
	// the kind of trace criterion 7 is about.
	lineStart := len(out) == 0 || out[len(out)-1] == '\n'
	if !leftSpace && !lineStart {
		return out, cursor
	}
	rest := body[cursor:]
	if lineStart && !leftSpace {
		if strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t") {
			return out, cursor + 1
		}
		return out, cursor
	}
	switch {
	case strings.HasPrefix(rest, " "), strings.HasPrefix(rest, "\t"):
		// One space either side becomes one space.
		return out, cursor + 1
	case rest == "" || strings.ContainsRune(".,;:!?)\n", rune(rest[0])):
		// Nothing follows that wants a space before it.
		return out[:len(out)-1], cursor
	}
	return out, cursor
}

// AllReferenceRenderings returns one rendering per state, plus a piece of ordinary body text that
// contains the same characters, for the pairwise-distinctness test and for the CLI's syntax help.
//
// PLAIN TEXT IS IN THE SET ON PURPOSE. Criterion 1 is that a reference is distinguishable from
// prose that happens to contain the same characters, and a distinctness test over only the three
// reference states would pass while a resolved reference rendered as bare prose.
func AllReferenceRenderings() map[string]string {
	r := Reference{Kind: RefNote, Target: "note-9"}
	return map[string]string{
		"resolved":     RenderReference(r, StateResolved),
		"unresolved":   RenderReference(r, StateUnresolved),
		"undetermined": RenderReference(r, StateUndetermined),
		"plain text":   "note-9",
	}
}
