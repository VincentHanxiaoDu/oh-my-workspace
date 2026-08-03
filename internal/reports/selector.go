package reports

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMalformedSelector is the class of failure that must be REFUSED rather than matched-nothing.
//
// A malformed selector and a selector that legitimately matches nothing are the project's
// could-not-determine / determined-to-be-nothing distinction wearing different clothes, and they
// get different treatment end to end: this one is refused at write time and stores nothing; the
// other is stored, reported, and named in the report. They never share an exit code.
var ErrMalformedSelector = errors.New("this selector cannot be read")

// SelectorError names the offending selector and says what is wrong with it.
type SelectorError struct {
	Raw    string
	Reason string
	Err    error
}

func (e *SelectorError) Error() string {
	return fmt.Sprintf("selector %q: %s", e.Raw, e.Reason)
}
func (e *SelectorError) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	return ErrMalformedSelector
}

func malformed(raw, reason string) error {
	return &SelectorError{Raw: raw, Reason: reason, Err: ErrMalformedSelector}
}

// Wildcard is the subject path that means "every root subject".
const Wildcard = "*"

// DefaultGranularity is what a selector that names no granularity asks for.
//
// NOT SETTLED BY THE PRD OR BY ISSUE #23. `*, !channel` is written without granularities and reads
// as "everything except channel traffic"; nothing says at what detail. `full` is chosen because a
// selector that asked for no rolling-up gets nothing rolled up, and because the alternative —
// defaulting to `summary` — would make the PRD's own `*:summary` example a synonym for `*`, which
// would be a strange thing for the PRD to list separately. It is written down here, as one
// constant, so that changing the answer is one line and one test.
const DefaultGranularity = Full

// Selector names a subject and a granularity, or excludes a subject.
type Selector struct {
	// Negated marks an exclusion (`!channel`).
	Negated bool
	// Subject is a dotted path, or [Wildcard].
	Subject string
	// Gran is the granularity. It is [GranularityUnspecified] on a negation and never elsewhere.
	Gran Granularity
}

// String renders the selector in the form a person would have written.
//
// A DOTTED PATH IS PRESERVED, not collapsed to its parent (criterion 4): this is the function a
// subscription is stored and read back through, so a collapse here would silently rewrite what
// somebody asked for.
func (s Selector) String() string {
	if s.Negated {
		return "!" + s.Subject
	}
	return s.Subject + ":" + s.Gran.String()
}

// IsWildcard reports whether this selector names every root subject.
func (s Selector) IsWildcard() bool { return s.Subject == Wildcard }

// ParseSelectors reads a comma-separated selector list, ALL OR NOTHING.
//
// If any selector is malformed the whole list is refused and the caller stores nothing (criterion
// 24). Parsing selector by selector and keeping the good ones is the outcome that leaves half a
// subscription on disk, which is the failure a person cannot see and cannot undo.
func ParseSelectors(list string) ([]Selector, error) {
	parts := strings.Split(list, ",")
	out := make([]Selector, 0, len(parts))
	for _, p := range parts {
		raw := strings.TrimSpace(p)
		if raw == "" {
			// An empty element is a stray comma. Saying so beats silently accepting `git:full,`
			// and beats accepting `git:full,,token_usage:count` as if the gap were intended.
			return nil, malformed(list, "there is an empty selector in this list (a stray comma?)")
		}
		s, err := ParseSelector(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, malformed(list, "a subscription needs at least one selector")
	}
	return out, nil
}

// ParseSelector reads one selector: [!]subject[:granularity].
func ParseSelector(raw string) (Selector, error) {
	s := Selector{}
	body := raw
	if strings.HasPrefix(body, "!") {
		s.Negated = true
		body = body[1:]
	}
	subject, granTok := body, ""
	if i := strings.IndexByte(body, ':'); i >= 0 {
		subject, granTok = body[:i], body[i+1:]
	}
	hasGran := strings.IndexByte(body, ':') >= 0

	if subject == "" {
		// `:full` — REFUSED, not read as a wildcard. A granularity with nothing to apply it to is
		// a person who meant `*:full` or meant a subject they forgot to type, and guessing which
		// produces a report they cannot tell from the one they wanted.
		return s, malformed(raw, "there is no subject before the ':'")
	}
	if err := validSubjectPath(raw, subject); err != nil {
		return s, err
	}
	s.Subject = subject

	if s.Negated {
		if hasGran {
			// A NEGATION HAS NO GRANULARITY, and this is refused rather than ignored. `!channel:count`
			// reads as "exclude the counting of channel" — which is not a thing the product does, and
			// accepting it would silently exclude channel at every granularity while the text on
			// screen said otherwise.
			return s, malformed(raw, "an exclusion has no granularity: it excludes the subject at every granularity, so write '!"+subject+"'")
		}
		s.Gran = GranularityUnspecified
		return s, nil
	}
	if !hasGran {
		s.Gran = DefaultGranularity
		return s, nil
	}
	g, err := ParseGranularity(granTok)
	if err != nil {
		return s, &SelectorError{Raw: raw, Reason: err.Error(), Err: ErrUnknownGranularity}
	}
	s.Gran = g
	return s, nil
}

// validSubjectPath enforces the shape of a subject path WITHOUT consulting the catalog.
//
// The two questions are separate on purpose. "This is not a subject path" is a malformed selector
// and is refused. "This is a well-formed path naming a subject I do not know" is a selector that
// gets STORED and then reported as unmatched (criteria 12, 17) — because the person may be right
// and the build may be behind, and because a refusal there would make a typo indistinguishable
// from an unsupported subject only by exit code and never in the report.
func validSubjectPath(raw, subject string) error {
	if subject == Wildcard {
		return nil
	}
	if strings.Contains(subject, Wildcard) {
		// Partial wildcards (`git.*`) are not in the PRD's grammar and are refused rather than
		// silently treated as the literal name `git.*`, which would match nothing forever.
		return malformed(raw, "'*' is the whole-subject wildcard; a partial wildcard like 'git.*' is not something this build reads")
	}
	if strings.HasPrefix(subject, ".") || strings.HasSuffix(subject, ".") || strings.Contains(subject, "..") {
		return malformed(raw, "a dotted subject path has a name either side of every dot")
	}
	for _, r := range subject {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.':
		default:
			return malformed(raw, fmt.Sprintf("a subject path is lower-case letters, digits, '_' and '.'; %q is not", string(r)))
		}
	}
	return nil
}
