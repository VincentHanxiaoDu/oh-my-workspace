package health

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// fakeChecker forces any of the three outcomes. It is the reason criteria 12 and 13 are driven by
// a test rather than asserted in prose: an injected probe reaches the undetermined path and the
// error path on any machine, on every run.
type fakeChecker struct {
	enabled   bool
	err       error
	mechanism string
	calls     int
}

func (f *fakeChecker) Mechanism() string {
	if f.mechanism == "" {
		return "a fake probe"
	}
	return f.mechanism
}

func (f *fakeChecker) Enabled(context.Context) (bool, error) {
	f.calls++
	return f.enabled, f.err
}

func run(t *testing.T, c EncryptionChecker) Report {
	t.Helper()
	return Runner{GOOS: "testos", Checker: c}.Run(context.Background())
}

func render(t *testing.T, r Report) string {
	t.Helper()
	var b strings.Builder
	if err := Write(&b, r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return b.String()
}

// Criterion 1: exactly three values, and one of them is always emitted.
func TestRunAlwaysEmitsOneOfThreeValues(t *testing.T) {
	cases := []struct {
		name    string
		checker EncryptionChecker
		want    tri.Value
	}{
		{"filevault on", &fakeChecker{enabled: true}, tri.Yes},
		{"filevault off", &fakeChecker{enabled: false}, tri.No},
		{"probe errored", &fakeChecker{err: errors.New("boom")}, tri.Undetermined},
		{"no probe for platform", nil, tri.Undetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := run(t, tc.checker)
			a, ok := rep.Encryption()
			if !ok {
				t.Fatalf("health emitted no full-disk encryption assumption at all; report: %+v", rep)
			}
			if a.Value != tc.want {
				t.Errorf("value = %v (rendered %q), want %v", a.Value, a.Rendered(), tc.want)
			}
			if a.Rendered() == "" {
				t.Error("the assumption rendered as the empty string; silence is not one of the three answers")
			}
			if a.Mechanism == "" {
				t.Error("no mechanism reported; a person told the state must also be told what was tried")
			}
		})
	}
}

// Criterion 2, 12, 13: the three render distinguishably, and neither collapses into the other.
func TestThreeValuesRenderDistinctly(t *testing.T) {
	yes := render(t, run(t, &fakeChecker{enabled: true}))
	no := render(t, run(t, &fakeChecker{enabled: false}))
	undet := render(t, run(t, &fakeChecker{err: errors.New("the query is unavailable")}))

	if yes == no || yes == undet || no == undet {
		t.Fatalf("the three outputs are not distinct:\nenabled:\n%s\nnot enabled:\n%s\nundetermined:\n%s", yes, no, undet)
	}

	if !strings.Contains(yes, EncryptionAssumption+" (PRD §4.1): enabled\n") {
		t.Errorf("enabled did not render as `enabled`:\n%s", yes)
	}
	if !strings.Contains(no, EncryptionAssumption+" (PRD §4.1): not enabled\n") {
		t.Errorf("not enabled did not render as `not enabled`:\n%s", no)
	}
	want := EncryptionAssumption + " (PRD §4.1): could not be determined on this platform\n"
	if !strings.Contains(undet, want) {
		t.Errorf("undetermined did not render as %q:\n%s", want, undet)
	}

	// THE COLLAPSE, BOTH WAYS ROUND. `not enabled` is a substring of nothing in the undetermined
	// output, and the undetermined wording appears in neither of the determined outputs.
	if strings.Contains(undet, "not enabled") {
		t.Errorf("the undetermined output contains the words `not enabled`; the two must not be confusable:\n%s", undet)
	}
	if strings.Contains(no, "could not be determined") || strings.Contains(yes, "could not be determined") {
		t.Error("a determined value rendered the undetermined wording")
	}
}

// Criterion 3: undetermined is never silence — not an omitted line, not an empty value, not a
// default.
func TestUndeterminedIsNotSilence(t *testing.T) {
	rep := run(t, &fakeChecker{err: errors.New("fdesetup is not on this machine")})
	out := render(t, rep)

	if !strings.Contains(out, EncryptionAssumption) {
		t.Fatalf("the encryption line was omitted entirely:\n%s", out)
	}
	if !strings.Contains(out, "could not be determined on this platform") {
		t.Errorf("the undetermined value was not stated:\n%s", out)
	}
	if !strings.Contains(out, "why not determined: fdesetup is not on this machine") {
		t.Errorf("the reason the state could not be read was not reported:\n%s", out)
	}
	if rep.Determined() {
		t.Error("Report.Determined() reported a real answer for a check that could not be completed")
	}
}

// Criterion 13, sharpened: the error path must not be reachable as No under any error.
func TestAnErrorIsNeverNo(t *testing.T) {
	for _, err := range []error{
		errors.New("exit status 1"),
		errors.New("executable file not found in $PATH"),
		errNoUsableOutput,
	} {
		rep := run(t, &fakeChecker{enabled: false, err: err})
		a, _ := rep.Encryption()
		if a.Value == tri.No {
			t.Errorf("check errored with %v and the reported value was `no`; an error is never a negative", err)
		}
		if a.Value != tri.Undetermined {
			t.Errorf("check errored with %v and the value was %v, want undetermined", err, a.Value)
		}
	}

	// And the same when the probe reports enabled=true alongside an error: the error wins.
	rep := run(t, &fakeChecker{enabled: true, err: errors.New("partial read")})
	if a, _ := rep.Encryption(); a.Value != tri.Undetermined {
		t.Errorf("value = %v with a non-nil error, want undetermined", a.Value)
	}
}

// Criterion 15: Windows has no probe in this slice, and the answer there is the third value —
// never a guess and never `not enabled`.
func TestUnsupportedPlatformIsUndeterminedNotNo(t *testing.T) {
	rep := Runner{GOOS: "windows"}.Run(context.Background())
	a, ok := rep.Encryption()
	if !ok {
		t.Fatal("no encryption assumption reported for windows")
	}
	if a.Value != tri.Undetermined {
		t.Fatalf("windows reported %v, want undetermined (BitLocker is out of scope for this slice)", a.Value)
	}
	if CheckerFor("windows") != nil {
		t.Error("CheckerFor(\"windows\") returned a probe; this slice ships macOS and Linux only")
	}
}

// Criterion 8: with no hub configured, health reports in full and names nothing as missing.
func TestNoHubConfiguredReportsInFull(t *testing.T) {
	rep := Runner{GOOS: "testos", Checker: &fakeChecker{enabled: true}}.Run(context.Background())
	if rep.HubConfigured {
		t.Error("no hub was configured and the report says one was")
	}
	if len(rep.MissingForLackOfHub) != 0 {
		t.Errorf("parts were reported missing for lack of a hub: %v", rep.MissingForLackOfHub)
	}
	out := render(t, rep)
	if !strings.Contains(out, EncryptionAssumption+" (PRD §4.1): enabled") {
		t.Errorf("the encryption value was not reported in full with no hub:\n%s", out)
	}
	if !strings.Contains(out, "hub: not configured") {
		t.Errorf("health did not say that no hub is configured:\n%s", out)
	}

	withHub := Runner{
		GOOS:    "testos",
		Checker: &fakeChecker{enabled: true},
		Getenv:  func(k string) string { return map[string]string{HubEnv: "https://hub.example"}[k] },
	}.Run(context.Background())
	if !withHub.HubConfigured {
		t.Error("a hub was configured and the report says it was not")
	}
}

// Criterion 9: the encryption answer is identifiable as a reported deployment assumption.
func TestEncryptionIsReportedAsADeploymentAssumption(t *testing.T) {
	out := render(t, run(t, &fakeChecker{enabled: false}))
	if !strings.Contains(out, "reported deployment assumptions") {
		t.Errorf("the report does not present itself as the deployment assumptions:\n%s", out)
	}
	if !strings.Contains(out, "(PRD §4.1)") {
		t.Errorf("the encryption assumption is not traceable to the principle it comes from:\n%s", out)
	}
}
