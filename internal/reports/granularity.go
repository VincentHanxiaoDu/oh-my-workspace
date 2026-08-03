package reports

import (
	"errors"
	"fmt"
	"strings"
)

// Granularity is how much detail a selector asks for.
//
// THE ZERO VALUE IS NOT A GRANULARITY. It is [GranularityUnspecified], and it is deliberately not
// Full: a selector whose granularity failed to parse, or a struct field nobody set, must not read
// as a request for the most detailed report the product can produce. Nothing in this package
// renders an unspecified granularity — [Parse] refuses instead.
type Granularity int

const (
	// GranularityUnspecified is the zero value, ON PURPOSE. See the type comment.
	GranularityUnspecified Granularity = iota
	Full
	Event
	Digest
	Summary
	Count
)

// byDetail is THE ordering, most detailed first, and it is written once.
//
// Everything that depends on the ordering — [Granularity.Detail], [Ordered], the containment
// property test — reads it from here, and nothing else does. Rendering does NOT: [renderBody]
// switches on the constants, so a swap in this slice cannot quietly move a rendering with it. That
// separation is what lets the property test catch a swap; if rendering were driven by this index,
// swapping two entries would swap their output too and the test would stay green.
var byDetail = []Granularity{Full, Event, Digest, Summary, Count}

// Ordered returns the five granularities, most detailed first.
func Ordered() []Granularity {
	out := make([]Granularity, len(byDetail))
	copy(out, byDetail)
	return out
}

// Detail is the granularity's position in the ordering, 0 being the most detailed.
//
// A granularity that is not one of the five has no position and reports -1, rather than 0 — which
// would make an unparsed granularity the most detailed one there is.
func (g Granularity) Detail() int {
	for i, o := range byDetail {
		if o == g {
			return i
		}
	}
	return -1
}

// String is a switch, not a lookup into byDetail. See the comment on byDetail.
func (g Granularity) String() string {
	switch g {
	case Full:
		return "full"
	case Event:
		return "event"
	case Digest:
		return "digest"
	case Summary:
		return "summary"
	case Count:
		return "count"
	default:
		return "unspecified"
	}
}

// ErrUnknownGranularity is returned when a token is not one of the five. It is errors.Is-able so
// the CLI can pick its exit code from the value rather than by matching prose.
var ErrUnknownGranularity = errors.New("not one of the five granularities")

// ParseGranularity turns a token into a granularity, or REFUSES.
//
// It never coerces to a neighbour and never falls back to a default: `git:enormous` is a typo, and
// the report a person would get from a guessed granularity is a report they did not ask for and
// cannot tell apart from one they did (criterion 11).
func ParseGranularity(s string) (Granularity, error) {
	for _, g := range []Granularity{Full, Event, Digest, Summary, Count} {
		if s == g.String() {
			return g, nil
		}
	}
	return GranularityUnspecified, fmt.Errorf("%q is %w (%s)", s, ErrUnknownGranularity, GranularityNames())
}

// GranularityNames lists the five in order, for an error message that tells a person what to type.
func GranularityNames() string {
	names := make([]string, 0, len(byDetail))
	for _, g := range byDetail {
		names = append(names, g.String())
	}
	return strings.Join(names, ", ")
}
