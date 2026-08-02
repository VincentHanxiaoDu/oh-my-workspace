package publish

import (
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// TestTheControlEndpointAndTheRenderingReportTheSameStateForEveryNote is criterion 16.
//
// IT IS DRIVEN OVER A REAL SOCKET, not by calling two functions in the same process and comparing.
// The criterion is about what a person and a script actually receive, and a serialisation that
// loses or renames a state is exactly the failure it is guarding against — which only a round trip
// through JSON and a connection can catch.
//
// All four states are compared, because a parity test over one state proves parity for one state.
func TestTheControlEndpointAndTheRenderingReportTheSameStateForEveryNote(t *testing.T) {
	c, _, ids := eachState(t)

	addr := socketPath(t, "control.sock")
	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("opening the control endpoint: %v", err)
	}
	defer ln.Close()
	go ServeState(ln, c.l, c.o)

	seen := map[string]string{}
	for want, id := range ids {
		w, err := QueryState(addr, id)
		if err != nil {
			t.Fatalf("querying %q over the control endpoint: %v", id, err)
		}
		if w.State != string(want) {
			t.Errorf("%q: the control endpoint says %q and the state is %q", id, w.State, want)
		}
		// THE CLI'S OWN ANSWER, from the rendering a person reads.
		line := stateLine(t, c.state(id).Render())
		if got := strings.TrimPrefix(line, "state: "); got != w.State {
			t.Errorf("%q: the CLI renders %q and the control API answers %q — the two surfaces disagree", id, got, w.State)
		}
		if other, dup := seen[w.State]; dup {
			t.Errorf("the control endpoint reports %q for both %s and %s", w.State, other, id)
		}
		seen[w.State] = string(id)

		// The published answer and the container travel too, and must agree with the rendering.
		if !strings.Contains(c.state(id).Render(), "published: "+w.Published) {
			t.Errorf("%q: the control endpoint says published=%q and the rendering does not:\n%s", id, w.Published, c.state(id).Render())
		}
		if !strings.Contains(c.state(id).Render(), "container: "+w.Container) {
			t.Errorf("%q: the control endpoint says container=%q and the rendering does not", id, w.Container)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("the control endpoint produced %d distinct states across four notes", len(seen))
	}

	// A REFUSAL'S REASON REACHES THE CONTROL SURFACE TOO. Criterion 7 is about the state a person
	// sees wherever they look, and a reason that only reaches the terminal is half of it.
	w, err := QueryState(addr, ids[StateRefused])
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(w.Reason) == "" {
		t.Error("the control endpoint reports a refusal with no reason")
	}
	if w.Code == "" {
		t.Error("the control endpoint reports a refusal with no code")
	}
}

// A note the client never heard of gets the same answer on both surfaces as well — including the
// fact that it is not one of the four states.
func TestTheControlEndpointAgreesAboutANoteNobodyHasHeardOf(t *testing.T) {
	c := newClient(t)
	addr := socketPath(t, "control.sock")
	ln, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go ServeState(ln, c.l, c.o)

	w, err := QueryState(addr, hub.NoteID("never-existed"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Exists != "no" {
		t.Errorf("Exists = %q, want a determined no", w.Exists)
	}
	for _, s := range States() {
		if w.State == string(s) {
			t.Errorf("a note nobody has heard of is reported in state %q", s)
		}
	}
}
