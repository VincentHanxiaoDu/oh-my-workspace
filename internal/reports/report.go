package reports

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// State is what a report could establish about one subject. The four are deliberately four.
type State int

const (
	// StateReported: the subject was read and had activity.
	StateReported State = iota
	// StateNoActivity: the subject was read and had nothing. A DETERMINED, SUCCESSFUL answer.
	StateNoActivity
	// StateUndetermined: the subject could not be read. Never empty, never `count: 0` (§4.3).
	StateUndetermined
	// StateNoHub: the subject is supplied by a hub and no hub is configured (criterion 23).
	StateNoHub
	// StateNoProducer: nothing in this build writes activity for the subject, so its emptiness is a
	// fact about this client and not about the period (Issue #67, Blocker 1). It is NOT
	// StateNoActivity, which is a determined quiet day, and it is not StateUndetermined, which is a
	// subject that could not be read — this one was read perfectly well and there was never
	// anything to read.
	StateNoProducer
)

// SubjectReport is one subject's line in a report.
type SubjectReport struct {
	Subject string
	Gran    Granularity
	State   State
	Items   []Item
	// Reason is the detail behind StateUndetermined or StateNoHub. It is never the whole answer —
	// the state is — so a caller that ignores it still cannot render an undetermined as an empty.
	Reason string
}

// Unmatched is a selector that named no subject this client knows.
type Unmatched struct {
	Selector Selector
	// Written is the selector exactly as the person typed it, so the report can point at it.
	Written string
}

// Report is what a subscription produced on one run.
type Report struct {
	Subjects  []SubjectReport
	Unmatched []Unmatched
}

// resolve turns selectors into the subjects to report on, and the selectors that named nothing.
//
// PRECEDENCE, STATED ONCE: a subject is included when some positive selector matches it AND no
// negation matches it. That is a set rule, not a fold over the written list, so it cannot depend on
// the order the selectors were written in (criterion 6). `*, !channel` and `!channel, *` resolve to
// the same set because there is nowhere in this function for the order to be consulted.
//
// When two positive selectors both match a subject the MOST DETAILED granularity wins. A person who
// wrote `*:count, git:full` asked for full on git; taking the last one written would make the
// answer depend on an evaluation order again, and taking the least detailed would silently discard
// the more specific request.
func resolve(sels []Selector) ([]SubjectReport, []Unmatched) {
	excluded := map[string]bool{}
	excludeAll := false
	var unmatched []Unmatched

	for _, s := range sels {
		if !s.Negated {
			continue
		}
		if s.IsWildcard() {
			excludeAll = true
			continue
		}
		if _, ok := LookupSubject(s.Subject); !ok {
			// CRITERION 17. Excluding something that was never there is a typo, not a no-op.
			unmatched = append(unmatched, Unmatched{Selector: s, Written: s.String()})
			continue
		}
		excluded[s.Subject] = true
	}

	chosen := map[string]Granularity{}
	pick := func(name string, g Granularity) {
		if cur, ok := chosen[name]; !ok || g.Detail() < cur.Detail() {
			chosen[name] = g
		}
	}
	for _, s := range sels {
		if s.Negated {
			continue
		}
		if s.IsWildcard() {
			// CRITERION 16: a wildcard is never unmatched. It names every root subject, and
			// whether any of them has activity today is a different question answered per subject.
			for _, sub := range RootSubjects() {
				pick(sub.Name, s.Gran)
			}
			continue
		}
		if _, ok := LookupSubject(s.Subject); !ok {
			unmatched = append(unmatched, Unmatched{Selector: s, Written: s.String()})
			continue
		}
		pick(s.Subject, s.Gran)
	}

	var out []SubjectReport
	for _, sub := range Catalog() {
		g, ok := chosen[sub.Name]
		if !ok {
			continue
		}
		if excludeAll || isExcluded(sub.Name, excluded) {
			continue
		}
		out = append(out, SubjectReport{Subject: sub.Name, Gran: g})
	}
	return out, unmatched
}

// isExcluded reports whether name is excluded, directly or by an excluded ancestor.
func isExcluded(name string, excluded map[string]bool) bool {
	for ex := range excluded {
		if under(name, ex) {
			return true
		}
	}
	return false
}

// Build runs a subscription's selectors against a source.
//
// NOTHING IS SKIPPED. An undetermined subject does not abort the report and does not drop out of it
// (criterion 19); an unmatched selector does not suppress the selectors that did match (criterion
// 15). Every selector the person wrote is accounted for in the result.
func Build(sels []Selector, src Source) Report {
	subjects, unmatched := resolve(sels)
	excluded := excludedSet(sels)
	for i := range subjects {
		items, err := src.Activity(subjects[i].Subject)
		switch {
		case errors.Is(err, ErrNoProducer):
			subjects[i].State = StateNoProducer
			subjects[i].Reason = err.Error()
			continue
		case errors.Is(err, ErrNoHubConfigured):
			subjects[i].State = StateNoHub
			subjects[i].Reason = err.Error()
			continue
		case err != nil:
			subjects[i].State = StateUndetermined
			subjects[i].Reason = err.Error()
			continue
		}
		// An excluded narrower path is excluded from its parent's items too: `git:full, !git.commit`
		// reports git without its commits, which is what it reads as.
		kept := items[:0:0]
		for _, it := range items {
			if isExcluded(it.Subject, excluded) {
				continue
			}
			kept = append(kept, it)
		}
		subjects[i].Items = kept
		if len(kept) == 0 {
			subjects[i].State = StateNoActivity
		} else {
			subjects[i].State = StateReported
		}
	}
	return Report{Subjects: subjects, Unmatched: unmatched}
}

func excludedSet(sels []Selector) map[string]bool {
	out := map[string]bool{}
	for _, s := range sels {
		if s.Negated && !s.IsWildcard() {
			out[s.Subject] = true
		}
	}
	return out
}

// Determined reports whether every subject in the report was established one way or the other.
//
// StateNoProducer IS NOT DETERMINED. A subject nobody writes has not been established to be empty;
// it has not been established at all. This is what carries Issue #67's Blocker 1 into the exit code
// a script reads, and it is why the two states are separate rather than one "nothing here".
func (r Report) Determined() bool {
	for _, s := range r.Subjects {
		if s.State == StateUndetermined || s.State == StateNoProducer {
			return false
		}
	}
	return true
}

// HasUnmatched reports whether any selector named no known subject.
func (r Report) HasUnmatched() bool { return len(r.Unmatched) > 0 }

// HasMissingHub reports whether any subject went unanswered for want of a hub.
func (r Report) HasMissingHub() bool {
	for _, s := range r.Subjects {
		if s.State == StateNoHub {
			return true
		}
	}
	return false
}

// The four per-subject verdict lines and the unmatched line. They are constants because criteria 13
// and 14 are assertions about BYTES: three facts, three outputs, and no rewording of one of them
// into the shape of another.
const (
	noActivityLine   = "no activity in this period"
	// noProducerLine does NOT contain the words "no activity": the difference between it and the
	// line above is the whole of Issue #67's Blocker 1, and a rewording of one into the shape of
	// the other puts it straight back.
	noProducerLine = "could not be determined: nothing in this build produces activity for this subject, " +
		"so this is not a quiet period — it is a subject nobody has ever observed"
	undeterminedLine = "could not be determined: this subject was not read, so it is neither empty nor full"
	unmatchedPrefix  = "unmatched selector"
)

// Render writes the report a person reads.
func (r Report) Render() string {
	var b strings.Builder
	if len(r.Subjects) == 0 && len(r.Unmatched) == 0 {
		// Not blank. A subscription that resolves to nothing at all (everything excluded) is a real
		// state, and silence is not one of the answers this product gives.
		b.WriteString("this subscription selects no subject at all\n")
		return b.String()
	}
	for _, s := range r.Subjects {
		fmt.Fprintf(&b, "%s:%s\n", s.Subject, s.Gran)
		switch s.State {
		case StateNoActivity:
			fmt.Fprintf(&b, "  %s\n", noActivityLine)
		case StateUndetermined:
			fmt.Fprintf(&b, "  %s\n", undeterminedLine)
			if s.Reason != "" {
				fmt.Fprintf(&b, "  reason: %s\n", s.Reason)
			}
		case StateNoProducer:
			fmt.Fprintf(&b, "  %s\n", noProducerLine)
			fmt.Fprintf(&b, "  when something starts writing this subject, this line becomes a real answer either way\n")
		case StateNoHub:
			fmt.Fprintf(&b, "  %s\n", ErrNoHubConfigured.Error())
			fmt.Fprintf(&b, "  this is not an emptiness and not an unknown subject: nothing has been established here\n")
		default:
			for _, line := range renderBody(s.Gran, s.Items) {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
	}
	for _, u := range r.Unmatched {
		// CRITERION 12: named, and said to have matched no known subject. Not silence, and not an
		// empty section that reads like a quiet day.
		fmt.Fprintf(&b, "%s %q: no subject by that name is known to this client\n", unmatchedPrefix, u.Written)
	}
	return b.String()
}

// renderBody IS THE WHOLE OF WHAT A GRANULARITY MEANS, and it has no subject in scope.
//
// That is the design constraint from §3.7 made structural: a granularity means the same thing for
// every subject because there is one function, it switches on the granularity alone, and it cannot
// branch on a subject it was never given. `*:summary` is therefore exactly equal to naming every
// root subject at `summary`, and the test asserts that equality on the rendered bytes.
//
// The ordering full ⊃ event ⊃ digest ⊃ summary ⊃ count is visible here as four monotone steps:
// full carries item text, event drops the text and keeps the items, digest drops the items and
// keeps the kinds, summary drops to one unit for the subject, count drops the kinds too and keeps
// only the quantity.
func renderBody(g Granularity, items []Item) []string {
	kinds, counts := kindBreakdown(items)
	switch g {
	case Full:
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, fmt.Sprintf("- %s %s: %s", it.ID, it.Kind, it.Text))
		}
		return out
	case Event:
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, fmt.Sprintf("- %s %s", it.ID, it.Kind))
		}
		return out
	case Digest:
		out := make([]string, 0, len(kinds))
		for _, k := range kinds {
			out = append(out, fmt.Sprintf("- %s: %d", k, counts[k]))
		}
		return out
	case Summary:
		return []string{fmt.Sprintf("%d item(s) across %d kind(s): %s.",
			len(items), len(kinds), strings.Join(kinds, ", "))}
	case Count:
		// A QUANTITY AND NO PER-ITEM CONTENT (criterion 8). It is reached only from StateReported,
		// so a `count: 0` here means a subject that was read; a subject that could not be read took
		// the undetermined branch above and never arrives at this line.
		return []string{fmt.Sprintf("count: %d", len(items))}
	default:
		// An unspecified granularity is not rendered as anything. It cannot arrive here from a
		// parsed selector, and inventing a rendering for it is how it would start to.
		return []string{"granularity unspecified: nothing has been rendered"}
	}
}

// kindBreakdown returns the distinct kinds in order, with their counts.
func kindBreakdown(items []Item) ([]string, map[string]int) {
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds, counts
}
