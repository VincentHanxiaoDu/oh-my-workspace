package status

import (
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// PAIRWISE, NOT AGAINST LITERALS. Asserting State(x).String() == "working" and so on passes just as
// happily after two of the four have been edited into the same wording — the test would be checking
// the constants against a copy of themselves. Criterion 1 and criterion 5 both ask that no two of
// these collapse, so the four are compared to each other and to the empty string, which is the
// silence §4.3 forbids.
func TestStateRendersFourWaysPairwiseDistinctly(t *testing.T) {
	for _, form := range []struct {
		what string
		of   func(State) string
	}{
		{"the sentence a person reads", State.String},
		{"the token a machine reads", State.Word},
	} {
		seen := map[string]State{}
		for _, s := range []State{Undetermined, Working, NotWorking, NotConfigured} {
			got := form.of(s)
			if got == "" {
				t.Errorf("%s for state %d is empty — silence is not one of the answers", form.what, s)
			}
			if other, dup := seen[got]; dup {
				t.Errorf("%s: state %d and state %d both render as %q — two answers collapsed into one",
					form.what, s, other, got)
			}
			seen[got] = s
		}
	}
	// The specific collapse the criterion is written about, named so a failure says which one.
	if Undetermined.String() == NotWorking.String() {
		t.Error("undetermined renders the same as NOT working — §4.3's exact prohibition")
	}
	if NotConfigured.String() == NotWorking.String() || NotConfigured.String() == Working.String() {
		t.Error("not-configured renders the same as running or as failing — criterion 1 asks for three distinct renderings")
	}
}

// The zero value must be Undetermined. A Subsystem an error path returned without setting State
// must not read as a confident negative; reversing the iota order is a one-character change that
// would do exactly that and nothing else in the package would notice.
func TestZeroStateIsUndetermined(t *testing.T) {
	var s Subsystem
	if s.State != Undetermined {
		t.Fatalf("the zero State is %v, want Undetermined", s.State)
	}
	if s.State.Determined() {
		t.Fatal("an unset subsystem state reports itself as determined")
	}
	var sum Summary
	if sum != SummaryUndetermined {
		t.Fatal("the zero Summary is not the undetermined one — a summary nobody computed would lead with good news")
	}
}

// CRITERION 8, AND IT IS THE ONE TO GET RIGHT. The summary must not lead with "everything is fine"
// when any subsystem is undetermined. Driven over every position an undetermined subsystem can
// occupy, because a fold that checks only the first or only the last is the plausible bug.
func TestSummaryNeverLeadsWithAllGoodWhenAnythingIsUndetermined(t *testing.T) {
	allGood := []Subsystem{
		{Name: Daemon, State: Working}, {Name: Store, State: Working},
		{Name: Channels, State: Working}, {Name: Projects, State: Working},
		{Name: Devices, State: Working}, {Name: Hub, State: Working},
	}
	if got := Summarise(allGood); got != SummaryAllWorking {
		t.Fatalf("six working subsystems summarise as %v, want SummaryAllWorking — the control for "+
			"everything below is broken", got)
	}
	good := Summarise(allGood).String()

	for i := range allGood {
		clouded := append([]Subsystem(nil), allGood...)
		clouded[i].State = Undetermined
		got := Summarise(clouded)
		if got == SummaryAllWorking {
			t.Errorf("with %q undetermined the screen still leads with everything working", clouded[i].Name)
		}
		// AT THE SUMMARY LINE, distinguishable — criterion 8's own wording. Compared against the
		// all-working sentence itself rather than against a literal.
		if got.String() == good {
			t.Errorf("with %q undetermined the summary sentence is identical to the all-working one: %q",
				clouded[i].Name, got.String())
		}
		if got != SummaryUndetermined {
			t.Errorf("with %q undetermined the summary is %v, want SummaryUndetermined", clouded[i].Name, got)
		}
	}

	// An undetermined MEMBER of an otherwise-working subsystem clouds the summary too: two channels
	// connected and a third unreachable is not a screen that may say everything is fine.
	withMember := append([]Subsystem(nil), allGood...)
	withMember[2].Items = []Item{{Name: "teams", State: Working}, {Name: "email", State: Undetermined}}
	if Summarise(withMember) == SummaryAllWorking {
		t.Error("a channel nobody could check left the summary saying everything is running")
	}

	// An undetermined summary must also be tellable from a failing one: "something is broken" and
	// "something could not be checked" are the two problems §4.3 exists to keep apart.
	failing := append([]Subsystem(nil), allGood...)
	failing[0].State = NotWorking
	if Summarise(failing).String() == Summarise(withMember).String() {
		t.Error("the not-working summary and the undetermined summary are the same sentence")
	}
	// An empty screen is not a clean bill of health.
	if Summarise(nil) == SummaryAllWorking {
		t.Error("a screen with no subsystems on it summarised as everything working")
	}
	// Not-configured is a determined answer, and it does not get the all-working sentence either.
	unconfigured := append([]Subsystem(nil), allGood...)
	unconfigured[5].State = NotConfigured
	if s := Summarise(unconfigured); s != SummaryAllConfiguredWorking {
		t.Errorf("a screen whose only oddity is an unconfigured hub summarises as %v", s)
	} else if s.String() == good {
		t.Error("the unconfigured-hub summary is word for word the all-working one")
	}
}

// CRITERION 7. One undetermined subsystem must not suppress, blank or abort the rest — driven at
// the renderer, which is where a `return` on the first unknown would live.
func TestOneUndeterminedSubsystemDoesNotSuppressTheOthers(t *testing.T) {
	subs := make([]Subsystem, 0, len(Required()))
	for _, name := range Required() {
		subs = append(subs, Subsystem{Name: name, State: Working, Detail: "fine", ObservedAt: time.Unix(1, 0)})
	}
	subs[1].State, subs[1].Detail = Undetermined, "the store could not be inspected"
	screen := Screen{Subsystems: subs}
	screen.wire()

	out := screen.Render()
	for _, name := range Required() {
		if !strings.Contains(out, name+": [") {
			t.Errorf("subsystem %q vanished from the screen when another one was undetermined:\n%s", name, out)
		}
	}
	states := ParseRendered(out)
	if len(states) != len(Required()) {
		t.Errorf("the screen rendered %d subsystem lines, want %d:\n%s", len(states), len(Required()), out)
	}
	if states[Store] != Undetermined.Word() {
		t.Errorf("the undetermined subsystem rendered as %q", states[Store])
	}
	for _, name := range []string{Daemon, Channels, Projects, Devices, Hub} {
		if states[name] != Working.Word() {
			t.Errorf("%q rendered as %q; the other five keep their own real states", name, states[name])
		}
	}
}

// CRITERION 5. The undetermined rendering is distinguishable from the not-working rendering BY
// INSPECTION OF THAT LINE ALONE — not by a missing line and not by an empty field. So: take the
// same subsystem twice, once undetermined and once not working, and compare the two lines to each
// other.
func TestUndeterminedAndNotWorkingLinesDifferOnTheirOwn(t *testing.T) {
	line := func(s State, detail string) string {
		screen := Screen{Subsystems: []Subsystem{{Name: Hub, State: s, Detail: detail, ObservedAt: time.Unix(1, 0)}}}
		screen.wire()
		for _, l := range strings.Split(screen.Render(), "\n") {
			if strings.HasPrefix(l, Hub+": ") {
				return l
			}
		}
		return ""
	}
	// THE DETAIL IS DELIBERATELY THE SAME on both, so that the distinction being tested is the
	// line's own state and not a difference in prose that a future refactor could drop.
	undet := line(Undetermined, "same words on both lines")
	notWorking := line(NotWorking, "same words on both lines")
	notConfigured := line(NotConfigured, "same words on both lines")
	working := line(Working, "same words on both lines")
	for name, got := range map[string]string{
		"undetermined": undet, "not working": notWorking, "not configured": notConfigured, "working": working,
	} {
		if got == "" {
			t.Fatalf("the %s line did not render at all", name)
		}
	}
	if undet == notWorking {
		t.Errorf("the undetermined line and the not-working line are identical: %q", undet)
	}
	if notConfigured == notWorking || notConfigured == working {
		t.Errorf("the not-configured line is identical to a running or failing one: %q", notConfigured)
	}
	if undet == working {
		t.Errorf("the undetermined line and the working line are identical: %q", undet)
	}
}

// CRITERION 5 again, at the field level: a line must not be told apart by an EMPTY field. A
// subsystem that arrives with no detail still says something.
func TestALineWithNoDetailStillSaysSomething(t *testing.T) {
	screen := Screen{Subsystems: []Subsystem{{Name: Hub, State: Undetermined}}}
	screen.wire()
	out := screen.Render()
	if strings.Contains(out, "\n  \n") || strings.HasSuffix(out, "\n  ") {
		t.Errorf("a subsystem with no detail rendered a blank line — silence is not an answer:\n%q", out)
	}
	if !strings.Contains(out, "no detail was recorded") {
		t.Errorf("a subsystem with no detail did not say so:\n%s", out)
	}
}

// CRITERION 3. A state with no observation time is shown as having none, rather than being
// rendered with a substituted or default time.
func TestAStateWithNoObservationTimeSaysSoRatherThanBorrowingOne(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	screen := Screen{Subsystems: []Subsystem{
		{Name: Daemon, State: Working, Detail: "d", ObservedAt: at},
		{Name: Store, State: Working, Detail: "s"},
	}}
	screen.wire()
	out := screen.Render()
	if !strings.Contains(out, at.Format(time.RFC3339)) {
		t.Errorf("the observed time was not rendered on the line that has one:\n%s", out)
	}
	if !strings.Contains(out, "no observation time was recorded") {
		t.Errorf("the line with no observation time did not say it has none:\n%s", out)
	}
	// The substitution this criterion forbids: the timeless line must not have picked up the other
	// line's stamp, nor a stamp of its own from anywhere.
	if strings.Count(out, at.Format(time.RFC3339)) != 1 {
		t.Errorf("an observation time appears more than once; a line with none was given one:\n%s", out)
	}
}

// CRITERION 9, 11 AND 12, AS A PROPERTY OF THE TYPES. The two surfaces are obtained from one screen
// and compared to EACH OTHER, subsystem by subsystem, including which are undetermined. Not one
// assertion per surface against a literal — that isolated shape is what Issue #41 exists about.
func TestTheTwoSurfacesAgreeSubsystemBySubsystem(t *testing.T) {
	screen := Screen{Subsystems: []Subsystem{
		{Name: Daemon, State: Working, Detail: "up", ObservedAt: time.Unix(2, 0)},
		{Name: Store, State: NotWorking, Detail: "gone", ObservedAt: time.Unix(2, 0)},
		{Name: Channels, State: Undetermined, Detail: "no idea", ObservedAt: time.Unix(2, 0)},
		{Name: Projects, State: NotConfigured, Detail: "none added", ObservedAt: time.Unix(2, 0)},
	}}
	screen.wire()

	body, err := screen.ControlJSON()
	if err != nil {
		t.Fatalf("the control API's form could not be produced: %v", err)
	}
	overTheWire, err := UnmarshalControl([]byte(body))
	if err != nil {
		t.Fatalf("the control API's form could not be read back: %v", err)
	}

	fromControl := overTheWire.States()
	fromCLI := ParseRendered(screen.Render())

	if len(fromCLI) == 0 || len(fromControl) == 0 {
		t.Fatal("one of the surfaces reported no subsystems at all, so the comparison establishes nothing")
	}
	if got, want := SortedNames(fromCLI), SortedNames(fromControl); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the CLI renders %v and the control API reports %v — criterion 10: neither surface "+
			"may carry a subsystem the other does not", got, want)
	}
	for name, cliState := range fromCLI {
		if fromControl[name] != cliState {
			t.Errorf("subsystem %q: the CLI says %q and the control API says %q", name, cliState, fromControl[name])
		}
	}
	// CRITERION 11, NAMED SEPARATELY: the undetermined one specifically survived the boundary, and
	// was not coerced to a negative, a null or an omitted field.
	if fromControl[Channels] != Undetermined.Word() {
		t.Errorf("the undetermined subsystem came back over the control API as %q", fromControl[Channels])
	}
	if overTheWire.Summary != screen.Summary {
		t.Errorf("the summary changed across the boundary: %v then %v", screen.Summary, overTheWire.Summary)
	}
	// CRITERION 12: no surface is more optimistic than another.
	if overTheWire.Summary == SummaryAllWorking {
		t.Error("the control API's summary says everything is working while a subsystem is undetermined")
	}
}

// CRITERION 10. A subsystem the control API reports and this build's renderer has never heard of is
// SURFACED, not dropped. Driven by feeding the renderer a response containing one.
func TestASubsystemTheRendererDoesNotKnowIsStillShown(t *testing.T) {
	body := `{
	  "summary": "all_working",
	  "summary_text": "everything is running.",
	  "subsystems": [
	    {"name": "daemon", "state": "working", "detail": "up"},
	    {"name": "quantum linkage", "state": "sideways", "detail": "a state this build has never heard of"}
	  ],
	  "taken_at": "2026-03-04T05:06:07Z"
	}`
	screen, err := UnmarshalControl([]byte(body))
	if err != nil {
		t.Fatalf("a response with an unknown subsystem could not be read: %v", err)
	}
	out := screen.Render()
	if !strings.Contains(out, "quantum linkage") {
		t.Errorf("a subsystem the renderer does not know was dropped from the screen:\n%s", out)
	}
	if !strings.Contains(out, "a state this build has never heard of") {
		t.Errorf("the unknown subsystem's detail was dropped:\n%s", out)
	}
	// A STATE WORD THIS BUILD CANNOT NAME IS UNDETERMINED, not a negative and not a blank
	// (criterion 11's last sentence, met at the decode).
	if got := screen.States()["quantum linkage"]; got != Undetermined.Word() {
		t.Errorf("an unrecognised state word decoded as %q, want undetermined", got)
	}
	// And it must cloud the summary rather than be waved through: the response claimed all_working.
	if screen.Summary == SummaryAllWorking {
		t.Error("a subsystem whose state this build cannot read left the summary at all-working — " +
			"the response's own optimistic summary was trusted over its subsystems")
	}
}

// The hub environment name is duplicated in two packages on purpose (see EnvHub). This is the test
// that keeps the duplication from drifting into two different variables.
func TestHubEnvNameMatchesTheOneDevicesReads(t *testing.T) {
	if EnvHub != devices.EnvHub {
		t.Fatalf("status reads %q and devices reads %q; one of them would silently see no hub", EnvHub, devices.EnvHub)
	}
}

// The conversion from the product's three-valued answer must have no branch that turns an
// undetermined into a negative. That is the whole reason it is one function.
func TestUndeterminedTriNeverBecomesNotWorking(t *testing.T) {
	if got := fromTri(tri.Undetermined); got != Undetermined {
		t.Errorf("fromTri(Undetermined) = %v — a state nobody established became an answer", got)
	}
	if fromTri(tri.Undetermined) == NotWorking {
		t.Error("'could not determine' became 'determined to be nothing'")
	}
	if fromTri(tri.Yes) != Working || fromTri(tri.No) != NotWorking {
		t.Error("the determined answers did not survive the conversion")
	}
	// A tri outside the three is undetermined, not a negative.
	if got := fromTri(tri.Value(99)); got != Undetermined {
		t.Errorf("an out-of-range tri became %v", got)
	}
}
