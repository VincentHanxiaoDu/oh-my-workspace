package status

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// viewFor builds the View a given configuration produces, in the wire form the daemon's report
// carries it in. The tri values are spelled through [tri.Value.String] rather than as literals, so
// a reworded third answer cannot make this file and the product disagree silently.
func viewFor(provider, chosen, credential string) model.View {
	return model.View{Provider: provider, ProviderChosen: chosen, CredentialPresent: credential}
}

// TestTheModelLineIsFourDistinctAnswersAndNoneOfThemIsAFailure is Issue #66 criteria 2 and 3 at the
// unit the screen is made of.
//
// IT COMPARES THE FOUR TO EACH OTHER, not to sentences written here. A per-case assertion against
// its own literal keeps passing after two branches have been edited into the same wording, and two
// branches with the same wording is precisely the defect §4.3 forbids and this Issue was filed
// about.
func TestTheModelLineIsFourDistinctAnswersAndNoneOfThemIsAFailure(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	yes, no, und := tri.Yes.String(), tri.No.String(), tri.Undetermined.String()

	cases := []struct {
		name  string
		view  model.View
		want  State
		exits bool // whether this state must make the one screen exit 3
	}{
		{"no provider chosen", viewFor("", no, no), NotConfigured, false},
		{"chosen without a credential", viewFor("acme", yes, no), NotConfigured, false},
		{"chosen with a credential", viewFor("acme", yes, yes), Working, false},
		{"which provider is configured could not be determined", viewFor("", und, no), Undetermined, true},
		{"chosen, and the credential could not be determined", viewFor("acme", yes, und), Undetermined, true},
	}

	rendered := map[string]string{}
	for _, tc := range cases {
		screen := Collect(Query{Now: now, Report: daemon.Report{Model: tc.view}})
		var line Subsystem
		found := false
		for _, sub := range screen.Subsystems {
			if sub.Name == Model {
				line, found = sub, true
			}
		}
		if !found {
			t.Fatalf("%s: the screen carries no %q subsystem at all — the whole of Issue #66", tc.name, Model)
		}
		if line.State != tc.want {
			t.Errorf("%s: the model line is %v; want %v", tc.name, line.State, tc.want)
		}
		// CRITERION 3, STATED AS ITS OWN ASSERTION. Choosing a provider this build has no adapter
		// for, or choosing none at all, is a determined fact about a person's configuration and
		// never a broken subsystem.
		if line.State == NotWorking {
			t.Errorf("%s: the model line reports NOT working. No model configuration is a failing "+
				"subsystem (PRD §3.13):\n%s", tc.name, line.Detail)
		}
		// CRITERION 2, THIS LINE'S HALF OF IT. The exit code is [Screen.AnyUndetermined] over every
		// subsystem, so what this line contributes is whether its own state is determined — and
		// that is what is asserted here. The whole route, from an unreadable credential file to
		// `omw status` exiting 3, is driven at the real binary in package commands, because a
		// screen assembled in this file cannot show that the command wires it up.
		if got := !line.State.Determined(); got != tc.exits {
			t.Errorf("%s: the model line's state is %v, which %v feed an undetermined screen; want it to %v",
				tc.name, line.State,
				map[bool]string{true: "does", false: "does not"}[got],
				map[bool]string{true: "do so", false: "not"}[tc.exits])
		}
		// AND THE SENTENCE IS THE VIEW'S OWN, not a second wording invented here (criterion 1).
		if line.Detail != tc.view.Render() {
			t.Errorf("%s: the line does not carry the View's own rendering:\n  line: %q\n  view: %q",
				tc.name, line.Detail, tc.view.Render())
		}
		rendered[tc.name] = line.StateWord + "\n" + line.Detail
	}

	for a, ra := range rendered {
		if strings.TrimSpace(ra) == "" {
			t.Errorf("%q renders as nothing; silence is not one of the answers (§4.3)", a)
		}
		for b, rb := range rendered {
			if a < b && ra == rb {
				t.Errorf("%q and %q render identically on the one screen:\n%s", a, b, ra)
			}
		}
	}
}

// TestEveryCapabilityTheDaemonReportRendersHasALineOnTheScreen is criterion 6, and it is the
// STRUCTURAL kind the Issue asked for rather than a list somebody has to remember to extend.
//
// # WHY IT IS SHAPED THIS WAY
//
// Issue #66 opened because §3.9's six subsystems were a CLOSED LIST written before Issue #18's
// model existed, and nothing anywhere noticed when the list fell behind. A guard that names the
// seven has the same defect one Issue later: it is a second closed list, and the eighth capability
// walks past it exactly as the seventh walked past the first.
//
// So the guard asks a question about SHAPE. [daemon.Report] is what `omw daemon status` renders and
// what the control API serves; a field on it whose type renders ITSELF — a capability's own
// projection, with its own `Render() string` — is a subsystem a person is being told about on that
// surface. Criterion 6: anything in that position must also have a line on the one screen. A future
// capability that adds such a field to the Report and stops there turns this red, by name.
func TestEveryCapabilityTheDaemonReportRendersHasALineOnTheScreen(t *testing.T) {
	// A REPORT WHOSE RENDERED CAPABILITIES SAY SOMETHING DISTINCTIVE. A zero View renders the
	// undetermined sentence, which several lines could carry; this one names a provider, so
	// finding it on the screen is a finding about this field and not a coincidence.
	rep := daemon.Report{
		Model: viewFor("acme-guard-sentinel", tri.Yes.String(), tri.Yes.String()),
	}
	screen := Collect(Query{Now: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC), Report: rep})

	rt, rv := reflect.TypeOf(rep), reflect.ValueOf(rep)
	checked := 0
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		m, ok := f.Type.MethodByName("Render")
		// The signature is the test, not the name: a no-argument Render returning one string is a
		// value that renders itself. tri.Value's Render takes the caller's wording and is not one
		// of these — it is a three-valued answer INSIDE a capability, not a capability.
		if !ok || m.Type.NumIn() != 1 || m.Type.NumOut() != 1 || m.Type.Out(0).Kind() != reflect.String {
			continue
		}
		checked++
		want := rv.Field(i).MethodByName("Render").Call(nil)[0].String()
		if strings.TrimSpace(want) == "" {
			t.Errorf("daemon.Report.%s renders as nothing; silence is not one of the answers", f.Name)
			continue
		}
		on := false
		for _, sub := range screen.Subsystems {
			if strings.Contains(sub.Detail, want) {
				on = true
			}
		}
		if !on {
			t.Errorf("daemon.Report.%s is a capability `omw daemon status` renders and `omw status` "+
				"does not carry. A subsystem that exists on one surface and not on the one screen is "+
				"Issue #66 happening again — give it a line in Collect.\nits rendering:\n%s\nthe screen:\n%s",
				f.Name, want, screen.Render())
		}
	}
	// THE GUARD MUST NOT BE VACUOUS. A Report that grew no self-rendering field, or a reflection
	// walk that matched none, would pass this test in silence — which is the failure mode of every
	// structural guard and the reason this line is here.
	if checked == 0 {
		t.Fatal("this guard examined no capability at all, so it establishes nothing. If daemon.Report " +
			"no longer carries a self-rendering capability, this test needs rewriting rather than deleting")
	}
}
