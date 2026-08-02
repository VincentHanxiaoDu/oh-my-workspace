package status

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The six subsystems §2.1 names as part of a running client. Criterion 1: every one of them
// appears on the screen, each as its own named line, and none is silently omitted.
//
// THEY ARE CONSTANTS AND THERE IS A LIST OF THEM because "no subsystem is silently omitted" is a
// property a test has to be able to state. TestEverySubsystemNamedBySection21Appears iterates
// [Required] against a rendered screen; a Collect that stopped producing one of these fails there
// rather than in whichever downstream Issue notices the missing line first.
const (
	Daemon   = "daemon"
	Store    = "local store"
	Channels = "configured channels"
	Projects = "watched projects"
	Devices  = "devices registration"
	Hub      = "hub connection"
)

// Required is the six, in the order a person reads them: the process, then what it owns locally,
// then what reaches off the machine.
func Required() []string {
	return []string{Daemon, Store, Channels, Projects, Devices, Hub}
}

// Item is one thing INSIDE a subsystem that has a state of its own: a channel, a project, a
// device.
//
// CRITERION 6 IS THIS TYPE. "An unreachable channel adapter, a project directory that has gone
// missing, and a device that has never checked in each appear on the screen, each with its own
// state, and none of the three is rendered identically to a subsystem that is confirmed not
// working." A subsystem line that collapsed its members into a count could not do that: three
// projects of which one is missing is not "projects: not working", it is one missing directory and
// two fine ones, and the person needs to know which.
type Item struct {
	// Name identifies the member — a channel id, a project path, a device label.
	Name string `json:"name"`
	// State is the member's own state, in the same four values as a subsystem's.
	State State `json:"-"`
	// StateWord carries State over the wire. It is the word and not the number for the reason
	// daemon.Report gives: a reordering of the constants must not reinterpret a message already
	// sent.
	StateWord string `json:"state"`
	// Detail is this member's own sentence. It is never empty, because a member with a state and
	// no account of it is the "empty field" criterion 5 refuses to let stand in for an answer.
	Detail string `json:"detail"`
	// Advisory marks a member that is a REPORTED FACT rather than part of a subsystem's state.
	//
	// THERE IS EXACTLY ONE OF THESE TODAY and it is full-disk encryption. Issue #5's Related note
	// asks that status render §4.1's three values; §4.1 says that report is "a report, never a
	// blocker", and §3.9 keeps the health report a separate capability. So the value is shown, in
	// all three of its forms, and it does not decide whether the store is working and does not
	// decide the summary — a machine whose FileVault state could not be read has not thereby
	// failed to answer "is everything running?".
	//
	// It is a field on the member rather than a name the summary knows, so that the summary still
	// has no switch on names in it.
	Advisory bool `json:"advisory,omitempty"`
}

// Subsystem is one line of the screen.
type Subsystem struct {
	// Name is what the line is called. Criterion 1: its own named line.
	Name string `json:"name"`
	// State is the line's answer, in four values.
	State State `json:"-"`
	// StateWord carries State over the wire, and is the token BOTH surfaces print.
	StateWord string `json:"state"`
	// Detail says what was found, or why nothing could be. It is never empty: criterion 5 forbids
	// distinguishing undetermined from not-working by an empty field, and criterion 19 forbids a
	// subsystem that could not be rendered from being silent about it.
	Detail string `json:"detail"`
	// ObservedAt is when this state was observed. ZERO MEANS THERE IS NO OBSERVATION TIME, and that
	// is rendered as having none — criterion 3 says outright that a substituted or default time is
	// not allowed, so nothing in this package fills a zero in with "now".
	ObservedAt time.Time `json:"observed_at,omitempty"`
	// Items are the members that have states of their own. May be empty; a subsystem with no
	// members still has a state and a line.
	Items []Item `json:"items,omitempty"`
}

// ObservedText renders the observation time in its two forms, and the second of them is a
// sentence rather than a blank (criterion 3).
func (s Subsystem) ObservedText() string {
	if s.ObservedAt.IsZero() {
		return "no observation time was recorded for this state"
	}
	return "observed " + s.ObservedAt.UTC().Format(time.RFC3339)
}

// Summary is the one line the screen leads with.
type Summary int

const (
	// SummaryUndetermined is the ZERO VALUE, and criterion 8 is why. A summary nobody computed
	// must not lead with "everything is fine".
	SummaryUndetermined Summary = iota
	// SummaryAllWorking means every subsystem was checked and every one of them is working.
	SummaryAllWorking
	// SummaryNotAllWorking means everything was established and at least one thing is not running.
	SummaryNotAllWorking
	// SummaryAllConfiguredWorking means everything established, nothing failing, and at least one
	// subsystem is simply not set up. Distinct from SummaryAllWorking because a person who has not
	// configured their hub should not be told their hub is fine.
	SummaryAllConfiguredWorking
)

// String is the summary sentence. The four are pairwise distinct and only ONE of them leads with
// everything being fine — TestSummaryNeverLeadsWithAllGoodWhenAnythingIsUndetermined holds that.
func (s Summary) String() string {
	switch s {
	case SummaryAllWorking:
		return "everything is running."
	case SummaryAllConfiguredWorking:
		return "everything you have configured is running; some subsystems are not configured."
	case SummaryNotAllWorking:
		return "NOT everything is running — see the lines marked NOT working below."
	default:
		return "SOMETHING COULD NOT BE CHECKED, so this screen cannot tell you everything is fine. " +
			"See the lines marked " + Undetermined.String() + " below."
	}
}

// Word is the summary as one machine token, for the control API. Same reasoning as [State.Word]:
// both surfaces take the summary from here, so neither can be more optimistic than the other
// (criterion 12).
func (s Summary) Word() string {
	switch s {
	case SummaryAllWorking:
		return "all_working"
	case SummaryAllConfiguredWorking:
		return "all_configured_working"
	case SummaryNotAllWorking:
		return "not_all_working"
	default:
		return "something_undetermined"
	}
}

// Screen is one whole status answer: the single determination both surfaces render.
type Screen struct {
	// Summary is the leading line, derived from the subsystems by [Summarise] and never set by
	// hand.
	Summary Summary `json:"-"`
	// SummaryWord carries Summary over the wire.
	SummaryWord string `json:"summary"`
	// SummaryText is the sentence, carried too, so that the control API's reader and the CLI's
	// reader are looking at the same words and not at two renderings of one token.
	SummaryText string `json:"summary_text"`
	// Subsystems are the lines, in the order they are rendered.
	Subsystems []Subsystem `json:"subsystems"`
	// TakenAt is when this screen was taken.
	TakenAt time.Time `json:"taken_at"`
	// Pointers are the neighbouring capabilities a person may want next. Status POINTS at the
	// health report and the diagnostics bundle (§3.9); it does not implement either.
	Pointers []string `json:"pointers,omitempty"`
}

// Summarise derives the leading line from the subsystems, and it is the ONLY place that decision
// is made.
//
// CRITERION 8, IN ORDER OF PRECEDENCE, AND THE ORDER IS THE RULE:
//
//  1. ANY undetermined subsystem wins outright. A screen that could not check something may not
//     lead with "everything is fine", whatever the other five say. This is first because it is the
//     one a hurried reader would put last, and putting it last is how "all good" gets printed over
//     a subsystem nobody could reach.
//  2. Otherwise any not-working subsystem makes it "not everything is running".
//  3. Otherwise, if anything is merely not configured, say so rather than claiming it is fine.
//  4. Only with every subsystem confirmed working does the screen lead with everything running.
//
// An EMPTY screen is undetermined, not all-working: nothing was checked, so nothing is fine.
func Summarise(subs []Subsystem) Summary {
	if len(subs) == 0 {
		return SummaryUndetermined
	}
	notWorking, notConfigured := false, false
	for _, s := range subs {
		if !s.State.Determined() {
			return SummaryUndetermined
		}
		switch s.State {
		case NotWorking:
			notWorking = true
		case NotConfigured:
			notConfigured = true
		}
	}
	// A member that could not be determined is a thing that could not be checked, even when the
	// subsystem around it has an answer: two channels connected and a third unreachable is not a
	// screen that may say everything is fine.
	for _, s := range subs {
		for _, it := range s.Items {
			if !it.Advisory && !it.State.Determined() {
				return SummaryUndetermined
			}
		}
	}
	switch {
	case notWorking:
		return SummaryNotAllWorking
	case notConfigured:
		return SummaryAllConfiguredWorking
	default:
		return SummaryAllWorking
	}
}

// wire fills every text field from its typed field and recomputes the summary. Called on every
// path that produces a Screen, so that a Screen round-tripped through the control API equals the
// one that was sent (criterion 11).
func (s *Screen) wire() {
	s.Summary = Summarise(s.Subsystems)
	s.SummaryWord = s.Summary.Word()
	s.SummaryText = s.Summary.String()
	for i := range s.Subsystems {
		s.Subsystems[i].StateWord = s.Subsystems[i].State.Word()
		for j := range s.Subsystems[i].Items {
			s.Subsystems[i].Items[j].StateWord = s.Subsystems[i].Items[j].State.Word()
		}
	}
}

// AnyUndetermined reports whether anything on this screen could not be worked out — a subsystem or
// one of its members. It is what decides the exit code, and it is deliberately NOT the same
// question as "is anything not working".
func (s Screen) AnyUndetermined() bool {
	for _, sub := range s.Subsystems {
		if !sub.State.Determined() {
			return true
		}
		for _, it := range sub.Items {
			if !it.Advisory && !it.State.Determined() {
				return true
			}
		}
	}
	return false
}

// States is the screen as a map from subsystem name to state word.
//
// IT EXISTS FOR CRITERION 9. "A test can obtain both and compare them" — this is the comparable
// form, and the CLI's rendered text is parsed back into the same shape by [ParseRendered], so the
// two surfaces are compared to EACH OTHER rather than each to a string literal somebody wrote
// twice.
func (s Screen) States() map[string]string {
	out := make(map[string]string, len(s.Subsystems))
	for _, sub := range s.Subsystems {
		out[sub.Name] = sub.StateWord
	}
	return out
}

// Render is the screen a person reads.
//
// IT IS DATA-DRIVEN OVER THE SLICE, WHICH IS CRITERION 10. There is no switch on subsystem names
// here, so there is no default arm for an unknown subsystem to fall into and be dropped: a line
// this build's renderer has never heard of is printed exactly like the six it has, with whatever
// state and detail it arrived with. That is also criterion 7 — a subsystem that could not be
// determined is one iteration of this loop, and the loop does not stop for it.
func (s Screen) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", s.Summary)
	for _, sub := range s.Subsystems {
		// THE STATE WORD IS ON THE LINE, and it is the same token the control API publishes. It is
		// there so a person can tell undetermined from not-working by reading THIS line — not by
		// noticing a line is missing, and not by finding a field empty (criterion 5).
		fmt.Fprintf(&b, "%s: [%s] %s\n", sub.Name, sub.StateWord, sub.State)
		fmt.Fprintf(&b, "  %s\n", detailOrSilenceGuard(sub.Detail))
		fmt.Fprintf(&b, "  %s\n", sub.ObservedText())
		for _, it := range sub.Items {
			fmt.Fprintf(&b, "  - %s: [%s] %s\n", it.Name, it.StateWord, detailOrSilenceGuard(it.Detail))
		}
	}
	if len(s.Pointers) > 0 {
		b.WriteString("\n")
		for _, p := range s.Pointers {
			fmt.Fprintf(&b, "%s\n", p)
		}
	}
	return b.String()
}

// detailOrSilenceGuard makes a missing sentence say that it is missing.
//
// A blank detail is silence, and §4.3 says none of the three answers is silence. A subsystem that
// arrives with no account of itself gets one that says so, which is criterion 19: there is no
// input for which a line renders as nothing.
func detailOrSilenceGuard(detail string) string {
	if strings.TrimSpace(detail) == "" {
		return "no detail was recorded for this state, which is itself something this screen " +
			"could not establish"
	}
	return detail
}

// ControlJSON is the control API's form of this exact screen (§4.3, "the control API and the CLI
// report the same state"; criteria 9–12).
//
// IT SERIALISES THE SAME VALUE [Render] PRINTED. Not a projection of it, not a summary of it: the
// same Screen, with the same state words, produced by the same [State.Word]. A subsystem that is
// undetermined inside the daemon is undetermined here because it is the same field, which is
// criterion 11 with no coercion step in which to lose it.
func (s Screen) ControlJSON() (string, error) {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}

// UnmarshalControl reads a screen back off the control API.
//
// THE TYPED FIELDS ARE RESTORED FROM THE WORDS, so a Screen read over the boundary behaves like one
// produced locally. An unrecognised state word becomes Undetermined rather than a zero-valued
// negative — criterion 11's "not coerced to a negative, a null, or an omitted field", and
// criterion 10's unknown subsystem arriving with a state this build cannot name.
func UnmarshalControl(body []byte) (Screen, error) {
	var s Screen
	if err := json.Unmarshal(body, &s); err != nil {
		return Screen{}, err
	}
	for i := range s.Subsystems {
		s.Subsystems[i].State = stateFromWord(s.Subsystems[i].StateWord)
		for j := range s.Subsystems[i].Items {
			s.Subsystems[i].Items[j].State = stateFromWord(s.Subsystems[i].Items[j].StateWord)
		}
	}
	// The summary is RECOMPUTED rather than trusted, so a response whose summary field disagreed
	// with its own subsystems cannot make a reader more optimistic than the subsystems justify.
	s.wire()
	return s, nil
}

// renderedLine matches one subsystem line of [Render]'s output: `name: [word] sentence`.
// Members are indented and so never match — they begin with two spaces.
func parseLine(line string) (name, word string, ok bool) {
	if strings.HasPrefix(line, " ") {
		return "", "", false
	}
	colon := strings.Index(line, ": [")
	if colon < 0 {
		return "", "", false
	}
	rest := line[colon+3:]
	end := strings.Index(rest, "]")
	if end < 0 {
		return "", "", false
	}
	return line[:colon], rest[:end], true
}

// ParseRendered reads the state words back out of a rendered screen, as a map from subsystem name
// to state word.
//
// IT EXISTS SO THAT THE TWO SURFACES ARE COMPARED TO EACH OTHER (criterion 9). A test that asserted
// the CLI prints "undetermined" and separately that the control API reports "undetermined" would
// pass just as happily with two independent bugs that happen to be the same shape today — and that
// isolated shape is what Issue #41 was filed about. Comparing a parse of one surface against the
// other's own map has no literal in it to be wrong twice.
func ParseRendered(out string) map[string]string {
	states := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if name, word, ok := parseLine(line); ok {
			states[name] = word
		}
	}
	return states
}

// SortedNames is the subsystem names in a stable order, for a test that wants to compare two
// screens' membership without depending on rendering order.
func SortedNames(states map[string]string) []string {
	out := make([]string, 0, len(states))
	for k := range states {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
