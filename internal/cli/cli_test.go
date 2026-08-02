package cli

import (
	"bytes"
	"strings"
	"testing"
)

// run drives the registry the way main does and returns what a person would see.
func run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb, func(string) string { return "" })
	return code, out.String(), errb.String()
}

// Issue #24 criterion 3: two subcommands can be added in two separate files with no shared file
// edited between them. The two files are alpha_cmd_test.go and beta_cmd_test.go in this package —
// neither mentions the other and neither is listed anywhere. This test only checks the outcome.
func TestTwoCommandsRegisteredFromSeparateFiles(t *testing.T) {
	for _, name := range []string{"alpha-probe", "beta-probe"} {
		if _, ok := Lookup(name); !ok {
			t.Fatalf("%s is not registered — a command file's init did not reach the registry", name)
		}
	}
	code, stdout, _ := run("alpha-probe")
	if code != Success || !strings.Contains(stdout, "alpha ran") {
		t.Errorf("alpha-probe: code=%d stdout=%q", code, stdout)
	}
	code, stdout, _ = run("beta-probe", "x", "y")
	if code != Success {
		t.Errorf("beta-probe exited %d", code)
	}
	// Its arguments must arrive, minus its own name.
	if !strings.Contains(stdout, "beta ran with [x y]") {
		t.Errorf("beta-probe did not receive its arguments: %q", stdout)
	}
}

// An unknown command is named, on stderr, with a usage exit code — not a silent success and not a
// wall of help text that leaves a person hunting for their typo.
func TestUnknownCommandIsNamedOnStderr(t *testing.T) {
	code, stdout, stderr := run("staus")
	if code != ExitUsage {
		t.Errorf("unknown command exited %d, want ExitUsage (%d)", code, ExitUsage)
	}
	if !strings.Contains(stderr, `"staus"`) {
		t.Errorf("the unknown name was not echoed back: stderr=%q", stderr)
	}
	if stdout != "" {
		t.Errorf("an error was written to stdout: %q", stdout)
	}
}

// No arguments is a usage error, and it does not exit zero. A bare `omw` that exits 0 having done
// nothing is indistinguishable, to a script, from a command that succeeded.
func TestNoArgumentsIsUsage(t *testing.T) {
	if code, _, _ := run(); code != ExitUsage {
		t.Errorf("bare invocation exited %d, want %d", code, ExitUsage)
	}
	// Asking for help, however, IS a success — the person got what they asked for.
	if code, stdout, _ := run("help"); code != Success || stdout == "" {
		t.Errorf("help exited %d with stdout %q, want success and output", code, stdout)
	}
}

// The four exit codes must be four distinct numbers. "Could not determine" sharing an exit code
// with "determined to be nothing" is the defect this project names first; it would be a
// one-character edit here and nothing else in the package would fail.
func TestExitCodesAreDistinct(t *testing.T) {
	codes := map[string]int{
		"Success":          Success,
		"ExitFailure":      ExitFailure,
		"ExitUsage":        ExitUsage,
		"ExitUndetermined": ExitUndetermined,
	}
	seen := map[int]string{}
	for name, c := range codes {
		if other, dup := seen[c]; dup {
			t.Errorf("%s and %s are both %d — two different outcomes share an exit code", name, other, c)
		}
		seen[c] = name
	}
	if ExitUndetermined == ExitFailure {
		t.Error("undetermined and failure share an exit code")
	}
	if Success != 0 {
		t.Errorf("Success is %d, want 0", Success)
	}
}

// A command gets its streams from Env rather than reaching for os.Stdout, so a test can read what
// a person would see — and so two tests can run in parallel without a global swap.
func TestCommandWritesToTheStreamsItIsGiven(t *testing.T) {
	Register(&Command{
		Name:    "stream-probe",
		Summary: "writes to both streams",
		Run: func(env Env) int {
			env.Stdout.Write([]byte("answer\n"))
			env.Stderr.Write([]byte("reason\n"))
			return ExitFailure
		},
	})
	code, stdout, stderr := run("stream-probe")
	if code != ExitFailure {
		t.Errorf("code=%d", code)
	}
	if stdout != "answer\n" {
		t.Errorf("stdout=%q", stdout)
	}
	if stderr != "reason\n" {
		t.Errorf("stderr=%q", stderr)
	}
}

// Getenv is injected, so "no hub configured" is drivable without mutating the process environment.
// Several open Issues assert on exactly that condition.
func TestGetenvIsInjected(t *testing.T) {
	var got string
	Register(&Command{
		Name:    "env-probe",
		Summary: "reads configuration",
		Run: func(env Env) int {
			got = env.Getenv("OMW_HUB")
			return Success
		},
	})
	var out, errb bytes.Buffer
	Run([]string{"env-probe"}, &out, &errb, func(k string) string {
		if k == "OMW_HUB" {
			return "https://hub.example"
		}
		return ""
	})
	if got != "https://hub.example" {
		t.Errorf("the command read %q from the injected environment", got)
	}
}

// A nil Getenv must not crash a command that reads configuration; it reads as unset.
func TestNilGetenvReadsAsUnset(t *testing.T) {
	var got, ok = "sentinel", false
	Register(&Command{
		Name:    "nilenv-probe",
		Summary: "reads configuration",
		Run: func(env Env) int {
			got = env.Getenv("OMW_HUB")
			ok = true
			return Success
		},
	})
	var out, errb bytes.Buffer
	Run([]string{"nilenv-probe"}, &out, &errb, nil)
	if !ok {
		t.Fatal("the command did not run with a nil Getenv")
	}
	if got != "" {
		t.Errorf("a nil Getenv returned %q, want the empty string", got)
	}
}

// Registering the same name twice panics rather than silently keeping one of them. Two commands
// answering to `omw status` would dispatch to whichever init ran second — a coin flip nobody
// would ever see fail.
func TestDuplicateRegistrationPanics(t *testing.T) {
	Register(&Command{Name: "dup-probe", Summary: "first", Run: func(Env) int { return Success }})
	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate name did not panic — one of the two commands is unreachable")
		}
	}()
	Register(&Command{Name: "dup-probe", Summary: "second", Run: func(Env) int { return Success }})
}

// A command with no Run would be a nil dereference at dispatch — at the moment a person types it,
// rather than at startup.
func TestRegisterRejectsMalformedCommands(t *testing.T) {
	for _, c := range []*Command{
		nil,
		{Name: "", Run: func(Env) int { return Success }},
		{Name: "no-run"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%+v) did not panic", c)
				}
			}()
			Register(c)
		}()
	}
}

// The command list is ordered, so `omw help` does not shuffle between runs.
func TestCommandsAreOrderedByName(t *testing.T) {
	cmds := Commands()
	for i := 1; i < len(cmds); i++ {
		if cmds[i-1].Name > cmds[i].Name {
			t.Fatalf("Commands() is not sorted: %q before %q", cmds[i-1].Name, cmds[i].Name)
		}
	}
}
