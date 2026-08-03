package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Criterion 18: "The model configuration state reported through the CLI and through the control API
// is the same state, with the same three-way distinction between configured, not configured, and
// undetermined — with the credential value itself absent from both."
//
// # WHY THIS SPAWNS THE REAL BINARY
//
// The two surfaces read the environment through different doors: the CLI takes cli.Env.Getenv, and
// the daemon's report — which is what the control API serves — reads the process environment,
// because a daemon has no cli.Env. An in-process test would have to inject one of those and would
// then be comparing two renderings of a value it supplied, which is a test of the formatter. Two
// child processes with ONE identical environment is the only arrangement where "they agree about
// this machine" is the thing being asserted.
//
// It also gets §4.2 for free: two real invocations, watched for anything left running.
func TestTheCLIAndTheControlAPIReportTheSameModelState(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	build := exec.Command(goTool, "build", "-o", bin, "./cmd/omw")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building omw: %v\n%s", err, out)
	}

	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()

	// BOTH XDG_DATA_HOME AND HOME ARE SANDBOXED. `store create` records which store is this
	// device's store in a per-user file resolved from XDG_DATA_HOME, else HOME. Inheriting the
	// developer's environment points that file at a t.TempDir() that is then deleted, leaving the
	// product reporting NO STORE while a real person's drafts sit on disk unreferenced. Setting
	// only one leaves the other live on the platform that uses it.
	run := func(extra []string, args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = append(append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=",
			model.EnvProvider+"=", model.EnvCredential+"=", model.EnvCredentialFile+"=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		), extra...)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}

	if code, out := run(nil, "store", "create"); code != 0 {
		t.Fatalf("omw store create exited %d:\n%s", code, out)
	}

	unreadable := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(unreadable, []byte(mdSecret), 0o000); err != nil {
		t.Fatal(err)
	}
	skipUndetermined := false
	if _, err := os.ReadFile(unreadable); err == nil {
		skipUndetermined = true
	}

	cases := []struct {
		name string
		env  []string
	}{
		{"nothing configured", nil},
		{"provider chosen only", []string{model.EnvProvider + "=acme"}},
		{"provider and credential", []string{model.EnvProvider + "=acme", model.EnvCredential + "=" + mdSecret}},
		{"credential file missing", []string{model.EnvProvider + "=acme", model.EnvCredentialFile + "=/no/such/file"}},
		{"undetermined", []string{model.EnvProvider + "=acme", model.EnvCredentialFile + "=" + unreadable}},
	}

	seen := map[string]string{}
	for _, tc := range cases {
		if tc.name == "undetermined" && skipUndetermined {
			t.Log("this environment can read a 0o000 file; the undetermined case is not driven here")
			continue
		}

		cliCode, cliOut := run(tc.env, "model", "show")
		_, daemonOut := run(tc.env, "daemon", "status")

		cliLine := modelLineOf(t, tc.name+" (omw model show)", cliOut)
		daemonLine := modelLineOf(t, tc.name+" (omw daemon status)", daemonOut)

		if cliLine != daemonLine {
			t.Errorf("%s: the CLI and the control API's report word the same state differently (criterion 18)\n"+
				"  omw model show:    %q\n  omw daemon status: %q", tc.name, cliLine, daemonLine)
		}

		// THE CREDENTIAL IS ABSENT FROM BOTH, which is the second half of the criterion.
		if strings.Contains(cliOut, mdSecret) {
			t.Errorf("%s: omw model show printed the credential:\n%s", tc.name, cliOut)
		}
		if strings.Contains(daemonOut, mdSecret) {
			t.Errorf("%s: omw daemon status printed the credential:\n%s", tc.name, daemonOut)
		}

		// THE THREE-WAY DISTINCTION SURVIVES THE TRIP. `omw model show` exits 3 exactly on the
		// state that could not be determined, and 0 on both determined answers.
		wantUndetermined := tc.name == "undetermined"
		if got := cliCode == 3; got != wantUndetermined {
			t.Errorf("%s: omw model show exited %d; exit 3 means undetermined and this state %v that",
				tc.name, cliCode, map[bool]string{true: "is", false: "is not"}[wantUndetermined])
		}

		seen[tc.name] = cliLine
	}

	// AND THE STATES ARE PAIRWISE DISTINCT ACROSS THE WHOLE TRIP, not merely internally consistent.
	// Two surfaces agreeing on one indistinguishable sentence would satisfy the check above.
	mdDistinct(t, "the model state through the real binary", seen)
}

// modelLineOf pulls the model paragraph out of a command's output, and FAILS IF THERE IS NONE —
// two surfaces that both say nothing about the model agree perfectly and prove nothing.
func modelLineOf(t *testing.T, what, out string) string {
	t.Helper()
	var lines []string
	collecting := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "model:"):
			collecting = true
			lines = append(lines, line)
		case collecting && strings.HasPrefix(line, "  "):
			lines = append(lines, strings.TrimSpace(line))
		case collecting:
			collecting = false
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s: there is no model line in the output at all, so an agreement between two of them "+
			"would be an agreement about nothing:\n%s", what, out)
	}
	return strings.Join(lines, "\n")
}
