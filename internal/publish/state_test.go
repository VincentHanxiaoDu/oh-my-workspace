package publish

import (
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// pairwiseDistinct is how every closed set of answers in this package is checked.
//
// WHY NOT ASSERT EACH AGAINST A LITERAL. Four assertions of the form `got == "drafted"` all pass
// just as happily after two of the four renderings have been edited to the same wording — which is
// the collapse §4.3 forbids, passing a suite written to catch it. Comparing the renderings against
// EACH OTHER catches it by construction and does not need updating when somebody rewords one.
func pairwiseDistinct(t *testing.T, what string, renderings map[string]string) {
	t.Helper()
	for name, s := range renderings {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s: the %q rendering is blank; silence is not an answer", what, name)
		}
	}
	seen := map[string]string{}
	for name, s := range renderings {
		if other, dup := seen[s]; dup {
			t.Errorf("%s: the %q and %q renderings are the same string:\n  %q\n"+
				"  Two answers have collapsed into one.", what, other, name, s)
		}
		seen[s] = name
	}
}

// eachState builds one client holding four notes, one in each state, by DRIVING the transitions
// rather than by hand-writing four Report values.
//
// A test that constructs the four structs itself proves the renderer distinguishes four inputs. It
// does not prove that the product can produce four distinguishable states, which is what criterion 6
// asks. So each of these is the state a real attempt left behind.
func eachState(t *testing.T) (*client, *testHub, map[State]hub.NoteID) {
	t.Helper()
	c, h := newClient(t), newHub(t)
	ids := map[State]hub.NoteID{
		StateDrafted:   "resting",
		StateInFlight:  "flying",
		StatePublished: "gone",
		StateRefused:   "rejected",
	}
	for _, id := range ids {
		draft(t, c, id, "body for "+string(id))
	}
	// drafted: nothing done to it.
	// published: a real accepted transfer.
	if res := transfer(c, ids[StatePublished], h.addr, publisher); res.Attempt != AttemptPublished {
		t.Fatalf("setting up the published note: %v (%s)", res.Attempt, res.Detail)
	}
	// refused: a real refusal from the hub, for want of the publish scope.
	if res := transfer(c, ids[StateRefused], h.addr, []hub.Scope{hub.ScopeRead}); res.Attempt != AttemptRefused {
		t.Fatalf("setting up the refused note: %v (%s)", res.Attempt, res.Detail)
	}
	// in flight: a real attempt to a listener that reads and never answers.
	silent := socketPath(t, "silent.sock")
	ln, err := Listen(silent)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_, _ = conn.Read(make([]byte, 4096))
			conn.Close()
		}
	}()
	if res := transfer(c, ids[StateInFlight], silent, publisher); res.Attempt != AttemptUndetermined {
		ln.Close()
		t.Fatalf("setting up the in-flight note: %v (%s)", res.Attempt, res.Detail)
	}
	ln.Close()

	for want, id := range ids {
		if got := c.state(id); got.State != want {
			t.Fatalf("setting up %q left it in state %q, not %q; this test would then not be comparing four states", id, got.State, want)
		}
	}
	return c, h, ids
}

// ---------------------------------------------------------------------------
// Criterion 6 — four states, distinguishable by inspecting the output
// ---------------------------------------------------------------------------

func TestTheFourStatesAreDistinguishableFromEachOtherInTheOutput(t *testing.T) {
	c, _, ids := eachState(t)
	renderings := map[string]string{}
	for st, id := range ids {
		renderings[string(st)] = c.state(id).Render()
	}
	if len(renderings) != 4 {
		t.Fatalf("this test is comparing %d renderings, not four", len(renderings))
	}
	pairwiseDistinct(t, "a note's publication state", renderings)

	// NOT BY EXIT CODE COINCIDENCE. The criterion says the four must be told apart by the OUTPUT,
	// so the `state:` line is checked to be four different lines on its own.
	lines := map[string]string{}
	for st, id := range ids {
		lines[string(st)] = stateLine(t, c.state(id).Render())
	}
	pairwiseDistinct(t, "the machine-checkable state line", lines)
}

// The vocabulary itself has no duplicates. A rename that collides two of the four would otherwise
// only show up as a confusing pairwise failure above.
func TestTheFourStateNamesAreFourNames(t *testing.T) {
	seen := map[State]bool{}
	for _, s := range States() {
		if s == "" {
			t.Error("a state has no name")
		}
		if seen[s] {
			t.Errorf("the state %q appears twice in the vocabulary", s)
		}
		seen[s] = true
	}
	if len(seen) != 4 {
		t.Fatalf("there are %d states; the four are drafted, in flight, published and refused", len(seen))
	}
}

func stateLine(t *testing.T, render string) string {
	t.Helper()
	for _, line := range strings.Split(render, "\n") {
		if strings.HasPrefix(line, "state: ") {
			return line
		}
	}
	t.Fatalf("this rendering has no machine-checkable state line:\n%s", render)
	return ""
}

// ---------------------------------------------------------------------------
// Criterion 2, as a property of the type
// ---------------------------------------------------------------------------

// TestExactlyOneContainerHoldsEveryReport is the invariant checked over EVERY combination of the
// report's fields rather than over the states a test happened to produce.
//
// Container returns one value and there is no combination that returns none, so "never both, never
// neither" is a property of the function's shape. This test is what stops somebody making it return
// a slice, or adding a branch that returns "".
func TestExactlyOneContainerHoldsEveryReport(t *testing.T) {
	var checked int
	for _, known := range []tri.Value{tri.Yes, tri.No, tri.Undetermined} {
		for _, exists := range []tri.Value{tri.Yes, tri.No, tri.Undetermined} {
			for _, st := range append(States(), State("something else"), State("")) {
				r := Report{Note: "n", Known: known, Exists: exists, State: st}
				checked++
				switch r.Container() {
				case ContainerOutbox, ContainerHub:
				default:
					t.Fatalf("%+v: the note is in neither container (%q)", r, r.Container())
				}
				if r.InOutbox() != (r.Container() == ContainerOutbox) {
					t.Fatalf("%+v: InOutbox and Container disagree", r)
				}
				// A note is on the hub only when this client has been TOLD it is. Anything less
				// than a determined `published` leaves it in the outbox, which is the direction
				// that cannot lose somebody's writing.
				if r.Container() == ContainerHub && !(known == tri.Yes && st == StatePublished) {
					t.Fatalf("%+v: reported as on the hub without a determined published state", r)
				}
			}
		}
	}
	if checked < 50 {
		t.Fatalf("the loop examined %d combinations; it is not examining what it claims", checked)
	}
}

// ---------------------------------------------------------------------------
// Criterion 13 — undetermined is its own answer, and is not "not published"
// ---------------------------------------------------------------------------

func TestAnInFlightNoteIsNeitherPublishedNorRefusedNorSilent(t *testing.T) {
	c, _, ids := eachState(t)
	flying := c.state(ids[StateInFlight])

	if got := flying.Published(); got != tri.Undetermined {
		t.Errorf("an in-flight note answers published=%v; the client does not know, so it must not say", got)
	}
	// The three-valued answer differs from all three of the others', which is what makes it
	// distinguishable from `published`, from `refused` and from silence.
	answers := map[string]string{}
	for st, id := range ids {
		answers[string(st)] = c.state(id).Published().String()
	}
	if answers[string(StateInFlight)] == answers[string(StatePublished)] {
		t.Error("in flight and published give the same published answer")
	}
	if answers[string(StateInFlight)] == answers[string(StateRefused)] {
		t.Error("in flight and refused give the same published answer")
	}
	out := flying.Render()
	if strings.TrimSpace(out) == "" {
		t.Fatal("an in-flight note renders as silence")
	}
	// It must say the outcome was not established. A rendering that merely says "not published"
	// claims knowledge this client does not have.
	if !strings.Contains(out, tri.Undetermined.String()) {
		t.Errorf("the in-flight rendering never says the outcome could not be determined:\n%s", out)
	}
	// CRITERION 4's half: it is still in the outbox and it is not published.
	if !flying.InOutbox() {
		t.Error("an in-flight note is not reported as being in the outbox")
	}
	if flying.State == StatePublished {
		t.Error("an interrupted publish reports the note as published")
	}
}

// ---------------------------------------------------------------------------
// Criterion 7 — a refusal with no reason is a defect, and says so
// ---------------------------------------------------------------------------

func TestARefusalWithNoReasonRendersAsADefectAndNotAsABlank(t *testing.T) {
	r := Report{Note: "n", Known: tri.Yes, Exists: tri.Yes, State: StateRefused}
	out := r.Render()
	if strings.Contains(out, "reason: \n") || strings.Contains(out, "reason:\n") {
		t.Errorf("a reasonless refusal renders a blank reason:\n%s", out)
	}
	if !strings.Contains(out, missingReason) {
		t.Errorf("a reasonless refusal does not name itself as a defect:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// A note nobody has heard of
// ---------------------------------------------------------------------------

func TestANoteThisClientNeverHeardOfIsADeterminedAbsence(t *testing.T) {
	c := newClient(t)
	got := c.state("never-existed")
	if got.Known != tri.Yes || got.Exists != tri.No {
		t.Fatalf("StateOf = %+v; that there is no such note is a determined answer", got)
	}
	if got.Render() == "" {
		t.Error("an absent note renders as silence")
	}
	for _, s := range States() {
		if strings.Contains(stateLineOrEmpty(got.Render()), string(s)) {
			t.Errorf("an absent note renders as the state %q", s)
		}
	}
}

func stateLineOrEmpty(render string) string {
	for _, line := range strings.Split(render, "\n") {
		if strings.HasPrefix(line, "state: ") {
			return line
		}
	}
	return ""
}
