// Issue #66: `omw status` — the one screen — did not mention the model provider at all, so a model
// state nobody could determine was SILENT there while two other surfaces exited 3 over it.
//
// # WHY THIS SPAWNS THE REAL BINARY
//
// The same reason model_agreement_test.go does, and it is not a preference. The three surfaces read
// the environment through different doors: `omw model show` takes cli.Env.Getenv, and the daemon's
// report — which `omw daemon status` prints and which `omw status` now carries — reads the process
// environment, because a daemon has no cli.Env. An in-process test would have to inject one of
// those and would then be comparing renderings of a value it supplied, which tests a formatter.
// Child processes sharing ONE environment is the only arrangement in which "these three agree about
// this machine" is the thing asserted.
//
// # AND WHY IT DRIVES FOUR CONFIGURATIONS
//
// Criterion 4 says so outright: "a test covering only the configured case is the one that would
// have stayed green through this". A test that asserts the model merely APPEARS on the screen
// passes against a build that renders "no provider is chosen" for a provider whose name could not
// be established — which is the §4.3 collapse the whole Issue is about. So the assertion is that
// the four renderings are PAIRWISE DISTINCT, and that the undetermined one moves the exit code.
package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/status"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// statusModelDetail pulls the model subsystem's own sentence out of `omw status --json`, and FAILS
// IF THE SCREEN HAS NO SUCH SUBSYSTEM — which is the defect this Issue was filed about, so it is
// the failure this helper must produce rather than an empty string that later comparisons would
// happily find equal to another empty string.
func statusModelDetail(t *testing.T, what, jsonOut string) string {
	t.Helper()
	screen, err := status.UnmarshalControl([]byte(jsonOut))
	if err != nil {
		t.Fatalf("%s: the control API's form of the status screen could not be read back: %v\n%s", what, err, jsonOut)
	}
	for _, sub := range screen.Subsystems {
		if sub.Name == status.Model {
			if strings.TrimSpace(sub.Detail) == "" || sub.StateWord == "" {
				t.Fatalf("%s: the status screen carries a %q subsystem with no sentence at all; "+
					"silence is not one of the three answers (PRD §4.3)", what, status.Model)
			}
			return sub.Detail
		}
	}
	t.Fatalf("%s: `omw status --json` reports no %q subsystem at all. A person scanning the one "+
		"screen for problems cannot see a model state that could not be determined (Issue #66):\n%s",
		what, status.Model, jsonOut)
	return ""
}

// modelParagraph normalises a model paragraph to one comparable form: every line trimmed of its
// surrounding space.
//
// IT REMOVES NOTHING BUT INDENTATION. The three surfaces nest the same sentences differently —
// `omw status` prints the paragraph under a subsystem line, `omw model show` prints it at the left
// margin — and an indentation difference is not a disagreement about state. Every word, and the
// line structure, is preserved, so a surface that actually said something different still fails.
func modelParagraph(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}

// TestStatusModelProviderAgreesWithTheOtherTwoSurfacesOnFourConfigurations is criteria 1, 2, 3, 4
// and 5.
func TestStatusModelProviderAgreesWithTheOtherTwoSurfacesOnFourConfigurations(t *testing.T) {
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

	// BOTH XDG_DATA_HOME AND HOME ARE SANDBOXED, for the reason model_agreement_test.go records:
	// the per-user device pointer is resolved from one of them, and inheriting the developer's
	// environment rewrites their real one.
	run := func(extra []string, args ...string) (int, string, string) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = append(append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=",
			model.EnvProvider+"=", model.EnvCredential+"=", model.EnvCredentialFile+"=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		), extra...)
		var outb, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &outb, &errb
		_ = cmd.Run()
		return cmd.ProcessState.ExitCode(), outb.String(), errb.String()
	}

	if code, out, errOut := run(nil, "store", "create"); code != 0 {
		t.Fatalf("omw store create exited %d:\n%s\n%s", code, out, errOut)
	}

	// THE UNDETERMINED CASE IS A REAL ONE ON DISK: a credential file that exists and cannot be
	// read. Issue #66 was found by driving exactly this.
	unreadable := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(unreadable, []byte(mdSecret), 0o000); err != nil {
		t.Fatal(err)
	}
	// CRITERION 5's CONTROL, STATED BEFORE THE SENTINEL ASSERTION BELOW MEANS ANYTHING. The
	// sentinel must be findable at its source, or "it did not appear in the output" is a fact
	// about a string nothing ever held. This reads it back as root would — the test process may or
	// may not be able to, and either way the file is the source.
	if raw, err := os.ReadFile(unreadable); err == nil && !strings.Contains(string(raw), mdSecret) {
		t.Fatalf("the control failed: the credential file does not contain the sentinel, so finding "+
			"no sentinel in any output would establish nothing:\n%q", string(raw))
	}
	skipUndetermined := false
	if _, err := os.ReadFile(unreadable); err == nil {
		skipUndetermined = true
	}

	cases := []struct {
		name string
		env  []string
		// corruptRecord makes the RECORDED provider choice unreadable, which is how "WHICH
		// provider is configured could not be determined" is reached — a different fact from "a
		// provider is chosen and whether it has a credential could not be determined", and the two
		// must not render alike. It is driven by damaging the record rather than by chmod, so this
		// case runs everywhere, including as root.
		corruptRecord bool
		wantExitThree bool
		// skippable marks the case that depends on a 0o000 file being unreadable to this process.
		skippable bool
	}{
		{name: "no provider chosen"},
		{name: "chosen without a credential", env: []string{model.EnvProvider + "=acme"}},
		{name: "chosen with a credential", env: []string{model.EnvProvider + "=acme", model.EnvCredential + "=" + mdSecret}},
		{
			name: "chosen, and the credential could not be determined", wantExitThree: true, skippable: true,
			env: []string{model.EnvProvider + "=acme", model.EnvCredentialFile + "=" + unreadable},
		},
		{name: "which provider is configured could not be determined", corruptRecord: true, wantExitThree: true},
	}

	seen := map[string]string{}
	for _, tc := range cases {
		if tc.skippable && skipUndetermined {
			t.Log("this environment can read a 0o000 file; the unreadable-credential case is not driven here")
			continue
		}
		if tc.corruptRecord {
			// The choice is recorded by the product itself and then damaged on disk, so that the
			// store still OPENS and only the record will not read. An unopenable store is a
			// different fact, it is Issue #68's, and it is deliberately not what this drives.
			if code, out, errOut := run(nil, "model", "use", "acme"); code != 0 {
				t.Fatalf("omw model use exited %d:\n%s\n%s", code, out, errOut)
			}
			recs, err := filepath.Glob(filepath.Join(root, "records", "model", "*"))
			if err != nil || len(recs) == 0 {
				t.Fatalf("the recorded model choice is not where this test expected it (%v): %v", recs, err)
			}
			for _, r := range recs {
				if err := os.WriteFile(r, []byte("this is not a model record"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}

		statusCode, statusOut, statusErr := run(tc.env, "status")
		_, statusJSON, _ := run(tc.env, "status", "--json")
		_, showOut, _ := run(tc.env, "model", "show")
		_, daemonOut, _ := run(tc.env, "daemon", "status")

		detail := statusModelDetail(t, tc.name, statusJSON)

		// CRITERION 1, THE TEXT HALF. The same sentence a machine reads is on the screen a person
		// reads — not a second wording of it.
		if !strings.Contains(statusOut, strings.SplitN(detail, "\n", 2)[0]) {
			t.Errorf("%s: `omw status` prints no model line matching its own --json detail.\ndetail: %q\nscreen:\n%s",
				tc.name, detail, statusOut)
		}

		// CRITERION 4: the three surfaces do not disagree. Each is compared to the OTHERS rather
		// than to a literal in this file — two surfaces wrong the same way is how this class of
		// defect has got through here before (Issue #41).
		showLine := modelLineOf(t, tc.name+" (omw model show)", showOut)
		daemonLine := modelLineOf(t, tc.name+" (omw daemon status)", daemonOut)
		if showLine != daemonLine {
			t.Errorf("%s: omw model show and omw daemon status disagree:\n  show:   %q\n  daemon: %q",
				tc.name, showLine, daemonLine)
		}
		if modelParagraph(detail) != showLine {
			t.Errorf("%s: `omw status` words the model state differently from `omw model show`\n"+
				"  status: %q\n  show:   %q", tc.name, modelParagraph(detail), showLine)
		}

		// CRITERION 2: an undetermined model makes the ONE SCREEN say so and exit 3, matching the
		// other two surfaces. And a determined one — including "no provider chosen" and "chosen
		// with no credential" — must NOT move the code: neither is a failure (criterion 3).
		if got := statusCode == 3; got != tc.wantExitThree {
			t.Errorf("%s: `omw status` exited %d; exit 3 means something could not be determined and "+
				"this state %v that.\nscreen:\n%s\nstderr:\n%s", tc.name, statusCode,
				map[bool]string{true: "is", false: "is not"}[tc.wantExitThree], statusOut, statusErr)
		}

		// CRITERION 3: none of the determined renderings reads as a failure. The subsystem's state
		// word is the machine-readable half of that, so it is the half asserted.
		if !tc.wantExitThree && strings.Contains(statusJSON, `"name": "`+status.Model+`"`) {
			screen, err := status.UnmarshalControl([]byte(statusJSON))
			if err != nil {
				t.Fatal(err)
			}
			for _, sub := range screen.Subsystems {
				if sub.Name == status.Model && sub.StateWord == "not_working" {
					t.Errorf("%s: the model subsystem reports not_working. A determined, non-negative "+
						"fact about a person's configuration is not a broken subsystem (criterion 3):\n%s",
						tc.name, sub.Detail)
				}
			}
		}

		// CRITERION 5, RE-DRIVEN NOW THAT THE LINE EXISTS. The zero-hit result was close to vacuous
		// while `omw status` said nothing about the model at all; it means something here.
		for what, out := range map[string]string{"stdout": statusOut, "stderr": statusErr, "--json": statusJSON} {
			if strings.Contains(out, mdSecret) {
				t.Errorf("%s: `omw status` %s printed the person's credential (PRD §3.13):\n%s", tc.name, what, out)
			}
		}

		seen[tc.name] = detail
	}

	// THE FOUR ARE PAIRWISE DISTINCT, which is the assertion a build that collapses two of them
	// fails. Every check above passes against a screen that renders "no provider is chosen" for an
	// undetermined provider, because such a screen agrees with itself perfectly.
	mdDistinct(t, "the model state on `omw status`", seen)
}
