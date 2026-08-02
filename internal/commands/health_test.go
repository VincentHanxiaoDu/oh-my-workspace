package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/health"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// forceHealth makes `omw health` report a given value, so the command — the thing a person
// actually runs — is driven through each of the three, on any machine.
func forceHealth(t *testing.T, v tri.Value, reason string) {
	t.Helper()
	real := healthRunner
	healthRunner = func(env cli.Env) health.Report {
		return health.Report{
			Platform: "testos",
			Assumptions: []health.Assumption{{
				Name:      health.EncryptionAssumption,
				Ref:       "PRD §4.1",
				Value:     v,
				Mechanism: "a forced probe",
				Reason:    reason,
			}},
			HubConfigured: strings.TrimSpace(env.Getenv(health.HubEnv)) != "",
		}
	}
	t.Cleanup(func() { healthRunner = real })
}

// invoke runs the command through the registry, exactly as `omw health` reaches it.
func invoke(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"health"}, args...), &out, &errb, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

func TestHealthIsRegistered(t *testing.T) {
	c, ok := cli.Lookup("health")
	if !ok {
		t.Fatal("`omw health` is not registered, so a person cannot ask the question at all")
	}
	if c.Summary == "" {
		t.Error("the command has no summary, so it appears in `omw help` as a bare name")
	}
}

// Criteria 2, 4, 12, 13: three values, three outputs, and the exit code each produces.
func TestHealthExitCodesAndOutputsForTheThreeValues(t *testing.T) {
	cases := []struct {
		name     string
		value    tri.Value
		reason   string
		wantCode int
		wantLine string
	}{
		{"enabled", tri.Yes, "", cli.Success, "full-disk encryption (PRD §4.1): enabled"},
		{"not enabled", tri.No, "", cli.Success, "full-disk encryption (PRD §4.1): not enabled"},
		{"undetermined", tri.Undetermined, "the query is unavailable", cli.ExitUndetermined,
			"full-disk encryption (PRD §4.1): could not be determined on this platform"},
	}

	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forceHealth(t, tc.value, tc.reason)
			code, stdout, stderr := invoke(t, nil)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(stdout, tc.wantLine) {
				t.Errorf("stdout does not contain %q:\n%s", tc.wantLine, stdout)
			}
			if stderr != "" {
				t.Errorf("health wrote to stderr on a run that answered: %q", stderr)
			}
			seen[tc.name] = stdout
		})
	}

	if seen["enabled"] == seen["not enabled"] || seen["enabled"] == seen["undetermined"] ||
		seen["not enabled"] == seen["undetermined"] {
		t.Error("two of the three values produced identical output from the command")
	}
	if strings.Contains(seen["undetermined"], "not enabled") {
		t.Error("the undetermined output contains `not enabled`")
	}
}

// Criterion 4, sharpened: `not enabled` succeeds, and the two failing-looking outcomes are told
// apart by TERMINATION ALONE.
func TestNotEnabledSucceedsAndIsDistinctByExitCodeFromUndetermined(t *testing.T) {
	forceHealth(t, tri.No, "")
	noCode, noOut, _ := invoke(t, nil)
	forceHealth(t, tri.Undetermined, "the check could not be completed")
	undetCode, _, _ := invoke(t, nil)

	if noCode != cli.Success {
		t.Errorf("`not enabled` exited %d; health is a report, never a blocker, and it ANSWERED", noCode)
	}
	if undetCode == noCode {
		t.Errorf("`not enabled` and `could not be determined` share exit code %d; "+
			"a script cannot tell a negative from a failure to look", noCode)
	}
	if undetCode == cli.ExitFailure {
		t.Error("an undetermined check exited with the generic failure code; it must be ExitUndetermined")
	}
	// The reported state and the run's success are independently observable: the output says the
	// state, the exit code says the run completed.
	if !strings.Contains(noOut, "not enabled") {
		t.Errorf("the reported state is not readable from the output:\n%s", noOut)
	}
}

// Criterion 8: with no hub configured, health reports in full and says so.
func TestHealthWithNoHubConfigured(t *testing.T) {
	forceHealth(t, tri.Yes, "")
	code, stdout, _ := invoke(t, nil)
	if code != cli.Success {
		t.Errorf("exit code = %d with no hub configured, want 0", code)
	}
	if !strings.Contains(stdout, "hub: not configured") {
		t.Errorf("health did not name the hub's absence:\n%s", stdout)
	}
	if !strings.Contains(stdout, "enabled") {
		t.Errorf("the encryption value was not reported with no hub:\n%s", stdout)
	}
	if strings.Contains(stdout, "not reported for lack of a configured hub") {
		t.Errorf("health named a missing part when nothing in this report needs a hub:\n%s", stdout)
	}
}

// Criteria 5, 6 and 14, at the command level: health answers with no store, no daemon and no
// control API, because it asks none of them anything. Driven by the real runner (not the forced
// one) with no environment at all — whatever this machine reports, the command must terminate with
// one of the three values and never with a failure code.
func TestHealthRunsWithNoStoreNoDaemonNoControlAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	code, stdout, stderr := invoke(t, nil)
	switch code {
	case cli.Success, cli.ExitUndetermined:
	default:
		t.Fatalf("exit code = %d (stderr %q); health must complete without a store, a daemon or a "+
			"control API, and neither ExitFailure nor ExitUsage is one of the three values", code, stderr)
	}
	if !strings.Contains(stdout, health.EncryptionAssumption) {
		t.Fatalf("health did not report the encryption assumption at all:\n%s", stdout)
	}
	if !strings.Contains(stdout, "needed no store and no running daemon") {
		t.Errorf("health does not state that it needed neither:\n%s", stdout)
	}
	if strings.Contains(stdout, ": \n") {
		t.Errorf("an assumption rendered with an empty value:\n%s", stdout)
	}
}

// Criterion 9: the answer is identifiable as a reported deployment assumption.
func TestHealthPresentsTheAnswerAsADeploymentAssumption(t *testing.T) {
	forceHealth(t, tri.Yes, "")
	_, stdout, _ := invoke(t, nil)
	if !strings.Contains(stdout, "reported deployment assumptions") {
		t.Errorf("output does not identify itself as the deployment assumptions:\n%s", stdout)
	}
}

func TestHealthRefusesArguments(t *testing.T) {
	forceHealth(t, tri.Yes, "")
	code, _, stderr := invoke(t, nil, "--verbose")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d for an unknown argument, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(stderr, "--verbose") {
		t.Errorf("the rejected argument is not named back: %q", stderr)
	}
}
