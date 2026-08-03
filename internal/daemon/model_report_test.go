package daemon

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// dmSecret is the sentinel credential these tests configure. Nothing in the product could produce
// it by accident, so finding it in a served report is a finding and never a coincidence.
const dmSecret = "sk-ZQXJ-a-key-that-must-not-travel-4b70"

// THE CREDENTIAL DOES NOT CROSS THE CONTROL API — driven over the ACTUAL SOCKET.
//
// Issue #18 criterion 6 is about an API returning the credential, and criterion 18 requires the
// control API to carry the model state at all. Those two together are the risk: the moment a
// surface serialises model configuration, a credential can travel. So this starts a real daemon
// with a real credential configured in the process environment, connects to its control socket, and
// searches the BYTES THAT CAME BACK — not a struct field, the bytes.
func TestTheCredentialNeverCrossesTheControlAPI(t *testing.T) {
	t.Setenv(model.EnvProvider, "acme")
	t.Setenv(model.EnvCredential, dmSecret)
	t.Setenv(model.EnvCredentialFile, "")

	root := newTestStore(t)
	d := startTestDaemon(t, root)
	if state, why := d.ControlState(); state != tri.Yes {
		t.Skipf("the control API did not open here (%v: %s), so there is no socket to read from", state, why)
	}

	// THE CONTROL, FIRST. If the credential is not actually configured in this process, everything
	// below is a search for something that was never in play.
	if got := model.Read(os.Getenv, nil).Secret(); got != dmSecret {
		t.Fatalf("the credential is not configured in this process (%q), so this test proves nothing", got)
	}

	rep, err := queryControl(d.Control().Path())
	if err != nil {
		t.Fatalf("reading the daemon's own control API: %v", err)
	}

	// The report says something about the model — otherwise the searches below pass by saying
	// nothing at all, which is the vacuous-green shape criterion 18 is meant to close.
	if rep.Model.ProviderChosen == "" {
		t.Fatal("the served report carries no model state, so an absent credential in it proves nothing")
	}
	if rep.Model.Provider != "acme" || rep.Model.Present() != tri.Yes {
		t.Fatalf("the served report does not describe the configured model: %+v", rep.Model)
	}

	// The serialised report, as the wire carries it.
	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), dmSecret) {
		t.Errorf("the control API's report carries the person's credential:\n%s", body)
	}
	// And the rendering a person reads.
	var out strings.Builder
	if _, err := rep.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), dmSecret) {
		t.Errorf("the rendered report carries the person's credential:\n%s", out.String())
	}
}

// The model state survives the round trip through the control API unchanged, in all three values.
//
// A tri.Value is an int whose zero value is meaningful, so a state that decoded wrong would arrive
// as UNDETERMINED — the failure that looks like a correct answer. This asserts the trip preserves
// each of the three rather than that decoding produced something.
func TestTheModelStateSurvivesTheControlAPIRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name          string
		chosen, creds tri.Value
		provider      string
	}{
		{"nothing chosen", tri.No, tri.No, ""},
		{"chosen, no credential", tri.Yes, tri.No, "acme"},
		{"chosen and configured", tri.Yes, tri.Yes, "acme"},
		{"undetermined", tri.Undetermined, tri.Undetermined, ""},
	} {
		want := model.View{
			Provider:          tc.provider,
			ProviderChosen:    tc.chosen.String(),
			CredentialPresent: tc.creds.String(),
			Detail:            "a reason nobody may lose",
		}
		rep := Report{StorePath: "/somewhere", Model: want}
		rep.wire()

		body, err := json.Marshal(rep)
		if err != nil {
			t.Fatal(err)
		}
		var back Report
		if err := json.Unmarshal(body, &back); err != nil {
			t.Fatal(err)
		}
		back.unwire()

		if back.Model != want {
			t.Errorf("%s: the model state changed in transit:\n  sent: %+v\n  got:  %+v", tc.name, want, back.Model)
		}
		if back.Model.Chosen() != tc.chosen || back.Model.Present() != tc.creds {
			t.Errorf("%s: the three-way distinction did not survive: chosen=%v credential=%v",
				tc.name, back.Model.Chosen(), back.Model.Present())
		}
		if back.Model.Render() != want.Render() {
			t.Errorf("%s: the rendering changed in transit:\n  %s\n  %s", tc.name, want.Render(), back.Model.Render())
		}
	}
}

// The Report a running daemon serves and the one Inspect builds from disk describe the model the
// same way. They are two code paths — Daemon.Report and Inspect — and criterion 18's agreement
// depends on both filling this field.
func TestTheRunningAndTheOnDiskReportAgreeAboutTheModel(t *testing.T) {
	t.Setenv(model.EnvProvider, "acme")
	t.Setenv(model.EnvCredential, dmSecret)
	t.Setenv(model.EnvCredentialFile, "")

	root := newTestStore(t)
	d := startTestDaemon(t, root)

	own := d.Report()
	if own.Model.ProviderChosen == "" {
		t.Fatal("a running daemon's own report carries no model state")
	}
	// Inspect asks the control API when one is open, which is the correct behaviour and would make
	// this comparison tautological — so the on-disk path is asked directly.
	fromDisk := modelViewFor(root)
	if own.Model != fromDisk {
		t.Errorf("the running daemon and the on-disk resolution disagree about the model:\n  daemon: %+v\n  disk:   %+v",
			own.Model, fromDisk)
	}
}
