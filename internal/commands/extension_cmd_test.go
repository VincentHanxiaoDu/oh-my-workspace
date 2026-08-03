package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// xtSecret is the sentinel credential these tests configure.
//
// IT IS A STRING NOTHING IN THE PRODUCT COULD PRODUCE BY ACCIDENT, so finding it in an output
// stream is a finding and never a coincidence. Criterion 22 is driven by grepping for exactly this.
const xtSecret = "sk-ZQXJ-the-persons-own-key-7c21"

type xtResult struct {
	code   int
	stdout string
	stderr string
}

func (r xtResult) all() string { return r.stdout + r.stderr }

func runExtCmd(t *testing.T, env map[string]string, args ...string) xtResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"ext"}, args...), &out, &errb, func(k string) string { return env[k] })
	return xtResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// xtWorld is a machine with a store and nothing else: NO HUB, no daemon, no model.
//
// That is the default on purpose (PRD §4.4, criterion 18). The environment a test has to go out of
// its way to build is the one WITH a hub.
func xtWorld(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	return map[string]string{store.PathEnv: root}
}

// xtFake is an extension of whichever interface a test needs.
type xtFake struct {
	name  string
	iface extension.Interface
	err   error
}

func (f xtFake) Name() string                   { return f.name }
func (f xtFake) Interface() extension.Interface { return f.iface }
func (f xtFake) Load() error                    { return f.err }

// xtRegistry swaps the command's registry for one this test controls, and puts it back.
//
// The product's registry is what this build ships; a test that mutated it would make "what does
// this machine offer" a statement about whichever test ran last, which is the reason
// `extension.Registry` is a value and not a package global.
func xtRegistry(t *testing.T, exts ...extension.Extension) *extension.Registry {
	t.Helper()
	r := extension.NewRegistry()
	for _, e := range exts {
		r.Offer(e)
	}
	prev := extensionRegistry
	extensionRegistry = r
	t.Cleanup(func() { extensionRegistry = prev })
	return r
}

// ---------------------------------------------------------------------------
// CRITERIA 1, 2, 4, 5 — the sameness, at the surface a person touches
// ---------------------------------------------------------------------------

// CRITERION 1. "Registering a channel adapter and registering a model provider are the same act:
// the same command, invoked with the same arguments in the same order, differing only in the
// extension being registered. A test that registers one and then the other, changing only the
// extension identifier, passes both times."
//
// # THE ARGUMENT VECTORS ARE COMPARED, NOT JUST THE OUTCOMES
//
// Two invocations that both succeed do not prove they were the same act: `omw ext register --model
// acme` and `omw ext register --channel slack` would both succeed and would be two systems wearing
// one command name. So the test builds both argument lists and asserts they differ in exactly one
// position before running either.
func TestRegisteringAChannelAndAModelAreTheSameCommand(t *testing.T) {
	xtRegistry(t,
		xtFake{name: "slack", iface: extension.Channel},
		xtFake{name: "acme", iface: extension.Model},
	)

	channelArgs := []string{"register", "slack"}
	modelArgs := []string{"register", "acme"}

	if len(channelArgs) != len(modelArgs) {
		t.Fatalf("the two invocations have different shapes: %v vs %v", channelArgs, modelArgs)
	}
	diffs := 0
	for i := range channelArgs {
		if channelArgs[i] != modelArgs[i] {
			diffs++
		}
	}
	if diffs != 1 {
		t.Fatalf("the two invocations differ in %d positions, want exactly 1 (the extension "+
			"identifier): %v vs %v", diffs, channelArgs, modelArgs)
	}

	for _, args := range [][]string{channelArgs, modelArgs} {
		env := xtWorld(t)
		got := runExtCmd(t, env, args...)
		if got.code != cli.Success {
			t.Errorf("omw ext %s exited %d\nstdout:\n%s\nstderr:\n%s",
				strings.Join(args, " "), got.code, got.stdout, got.stderr)
		}
		if !strings.Contains(got.stdout, "registered: "+args[1]) {
			t.Errorf("omw ext %s did not say it registered anything:\n%s", strings.Join(args, " "), got.stdout)
		}
	}
}

// CRITERION 2. "Listing extensions is one listing … There is no channel-only listing command and no
// model-only listing command that shows something the shared one does not."
func TestOneListingShowsBothInterfaces(t *testing.T) {
	xtRegistry(t,
		xtFake{name: "slack", iface: extension.Channel},
		xtFake{name: "acme", iface: extension.Model},
	)
	env := xtWorld(t)
	mustExt(t, env, "register", "slack")
	mustExt(t, env, "register", "acme")

	got := runExtCmd(t, env, "list")
	if got.code != cli.Success {
		t.Fatalf("omw ext list exited %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"slack", "acme", string(extension.Channel), string(extension.Model)} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("the one listing does not mention %q:\n%s", want, got.stdout)
		}
	}
	// EACH ENTRY STATES WHICH INTERFACE IT IMPLEMENTS.
	if strings.Count(got.stdout, "interface:") < 2 {
		t.Errorf("entries do not each state their interface:\n%s", got.stdout)
	}

	// And there is no per-interface listing command hiding somewhere.
	for _, forbidden := range []string{"channels", "models", "providers", "adapters"} {
		sub := runExtCmd(t, env, forbidden)
		if sub.code != cli.ExitUsage {
			t.Errorf("`omw ext %s` exists (exit %d). Criterion 2: there is no channel-only and no "+
				"model-only listing.\n%s%s", forbidden, sub.code, sub.stdout, sub.stderr)
		}
	}
}

// CRITERION 4. "Configuration is the same shape for both. Whatever a person types to supply an
// adapter's settings is what they type to supply a provider's settings … a test that configures one
// and then the other using the same command form succeeds for both."
func TestConfiguringIsTheSameShapeForBoth(t *testing.T) {
	xtRegistry(t,
		xtFake{name: "slack", iface: extension.Channel},
		xtFake{name: "acme", iface: extension.Model},
	)
	for _, name := range []string{"slack", "acme"} {
		env := xtWorld(t)
		mustExt(t, env, "register", name)
		// THE SAME COMMAND FORM, changing only the identifier.
		got := runExtCmd(t, env, "configure", name, "endpoint=https://inside.example.invalid", "key_file=/home/me/.omw/key")
		if got.code != cli.Success {
			t.Errorf("configuring %s exited %d\n%s%s", name, got.code, got.stdout, got.stderr)
		}
		if !strings.Contains(got.stdout, "endpoint = https://inside.example.invalid") {
			t.Errorf("configuring %s did not record the setting:\n%s", name, got.stdout)
		}
	}
}

// CRITERION 5. "Failure reporting is the same mechanism: an extension of either interface that
// fails to load is reported through the same command and the same field as the other, not through a
// channel-specific error path and a separate model-specific one."
func TestFailureIsReportedThroughOneCommandAndOneField(t *testing.T) {
	const boom = "the shared object could not be opened"
	xtRegistry(t,
		xtFake{name: "slack", iface: extension.Channel, err: errors.New(boom)},
		xtFake{name: "acme", iface: extension.Model, err: errors.New(boom)},
	)
	env := xtWorld(t)
	// NOT mustExt: registering an extension that fails to load exits non-zero by criterion 12, and
	// that is the correct answer. What is asserted is that the registration happened.
	for _, n := range []string{"slack", "acme"} {
		if got := runExtCmd(t, env, "register", n); !strings.Contains(got.stdout, "registered: "+n) {
			t.Fatalf("%s was not registered, so the listing below shows nothing:\n%s", n, got.all())
		}
	}

	got := runExtCmd(t, env, "list")
	// Both failures arrive through `omw ext list`, in the `detail:` field, and there is exactly one
	// `detail:` per failure — not a second channel-shaped error path elsewhere in the output.
	detailLines := 0
	for _, line := range strings.Split(got.stdout, "\n") {
		if !strings.Contains(line, "detail:") {
			continue
		}
		detailLines++
		if !strings.Contains(line, boom) {
			t.Errorf("a detail line does not carry the failure: %q", line)
		}
	}
	if detailLines != 2 {
		t.Errorf("the two failures are not both reported in the same field: found %d detail "+
			"line(s), want 2. Criterion 5: one command, one field, not a channel-specific error "+
			"path and a separate model-specific one.\n%s", detailLines, got.stdout)
	}
	if got.code != cli.ExitFailure {
		t.Errorf("exit %d with two failed extensions, want %d", got.code, cli.ExitFailure)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 12 — exit status, by number alone
// ---------------------------------------------------------------------------

// CRITERION 12. "Command exit status distinguishes 'every registered extension loaded' from 'at
// least one failed to load' — distinguishable by exit code alone, with no output parsing."
//
// Plus the project's standing rule: `could not determine` and `determined to be nothing` must never
// share an exit code.
func TestExitStatusDistinguishesLoadedFailedAndUndetermined(t *testing.T) {
	cases := []struct {
		name string
		ext  extension.Extension
		want int
	}{
		{"every registered extension loaded", xtFake{name: "e", iface: extension.Channel}, cli.Success},
		{"one failed to load", xtFake{name: "e", iface: extension.Channel, err: errors.New("no")}, cli.ExitFailure},
		{"one could not be determined", xtFake{name: "e", iface: extension.Channel, err: extension.ErrLoadUndetermined}, cli.ExitUndetermined},
	}
	seen := map[int]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			xtRegistry(t, c.ext)
			env := xtWorld(t)
			// NOT mustExt: `register` reports the state of what it just registered and carries the
			// same exit code as `list` does, so a broken extension exits non-zero here too — which
			// is the point of criterion 12, not a failure of the fixture.
			if reg := runExtCmd(t, env, "register", "e"); !strings.Contains(reg.stdout, "registered: e") {
				t.Fatalf("e was not registered, so the exit code below is about an empty machine:\n%s", reg.all())
			}
			got := runExtCmd(t, env, "list")
			if got.code != c.want {
				t.Errorf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, c.want, got.stdout, got.stderr)
			}
		})
		if other, dup := seen[c.want]; dup {
			t.Errorf("%q and %q share exit code %d", c.name, other, c.want)
		}
		seen[c.want] = c.name
	}
	if len(seen) != 3 {
		t.Errorf("the three situations produced %d distinct exit codes, want 3: %v", len(seen), seen)
	}
}

// An extension merely PRESENT and unregistered is not a failure, and does not make the command
// exit non-zero. Criterion 17 says that is a normal state, not a fault.
func TestSomethingMerelyOfferedDoesNotFailTheCommand(t *testing.T) {
	xtRegistry(t, xtFake{name: "lying-around", iface: extension.Model})
	env := xtWorld(t)
	got := runExtCmd(t, env, "list")
	if got.code != cli.Success {
		t.Errorf("exit %d for a machine whose only extension is unregistered, want 0\n%s%s",
			got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "not registered") {
		t.Errorf("the unregistered extension is not reported as not registered:\n%s", got.stdout)
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 15, 16, 18 — nothing implicit, and the local half stands alone
// ---------------------------------------------------------------------------

// CRITERION 15. "Registering, listing, or configuring an extension never starts the daemon on the
// person's behalf. With the daemon not running, these commands say the daemon is not running rather
// than launching it, and a test that runs each of them with no daemon finds no daemon process
// afterwards."
//
// AND CRITERION 18. "With no hub configured, registering, listing, configuring and diagnosing
// extensions all work fully. A test performing the entire register-then-list-then-configure
// sequence with no hub completes without a hub-related error."
//
// # WHY THIS SPAWNS THE REAL BINARY
//
// "Finds no daemon process afterwards" is not assertable in-process: an in-process test cannot tell
// a daemon it did not start from one it did. Child processes in their own process group can be
// counted.
func TestNoExtensionCommandStartsTheDaemonOrNeedsAHub(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := xtBuild(t)
	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		// A PROCESS GROUP OF ITS OWN, so anything the command leaves behind is attributable.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// BOTH XDG_DATA_HOME AND HOME ARE SANDBOXED. `store create` records which store is this
		// device's store in a per-user file resolved from XDG_DATA_HOME, else HOME. Inheriting the
		// developer's environment points that file at a deleted t.TempDir(), leaving the product
		// reporting NO STORE while a real person's data sits on disk unreferenced. Setting only one
		// leaves the other live on the platform that uses it.
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root,
			// NO HUB. This is criterion 18's whole arrangement.
			"OMW_HUB=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}

	if code, out := run("store", "create"); code != 0 {
		t.Fatalf("store create exited %d: %s", code, out)
	}

	// THE ENTIRE SEQUENCE, with no hub (criterion 18).
	sequence := [][]string{
		{"ext", "list"},
		{"ext", "register", "email"},
		{"ext", "configure", "email", "folder=INBOX"},
		{"ext", "show", "email"},
	}
	for _, args := range sequence {
		code, out := run(args...)
		if code != 0 && code != cli.ExitFailure && code != cli.ExitUndetermined {
			t.Errorf("omw %s exited %d (a usage or crash code)\n%s", strings.Join(args, " "), code, out)
		}
		// CRITERION 18: no hub-related error anywhere in the sequence.
		low := strings.ToLower(out)
		for _, hubWord := range []string{"no hub configured", "hub could not be reached", "hub-unreachable"} {
			if strings.Contains(low, hubWord) {
				t.Errorf("omw %s failed for want of a hub (%q). §4.4: the local half stands alone.\n%s",
					strings.Join(args, " "), hubWord, out)
			}
		}
		// CRITERION 15: it says the daemon is not running.
		if !strings.Contains(low, "daemon") {
			t.Errorf("omw %s says nothing about the daemon; §4.2 asks that it be said rather than "+
				"started\n%s", strings.Join(args, " "), out)
		}
		if !strings.Contains(low, "not running") && !strings.Contains(low, "could not be determined") {
			t.Errorf("omw %s does not report the daemon as not running\n%s", strings.Join(args, " "), out)
		}
	}

	// AND NO DAEMON IS RUNNING AFTERWARDS.
	if out, err := exec.Command("pgrep", "-f", bin).CombinedOutput(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Errorf("a process from this build survived the sequence: %s", out)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 20 — the CLI and the control API report the same state
// ---------------------------------------------------------------------------

// CRITERION 20. "Extension state is reported identically by the CLI and by the control API. A test
// reading a failed-to-load extension through both surfaces sees the same state and the same failure
// reason."
//
// # WHY THIS SPAWNS THE REAL BINARY, TWICE
//
// The two surfaces read the environment through different doors: the CLI takes cli.Env.Getenv and
// the daemon's report reads the process environment, because a daemon has no cli.Env. An in-process
// test would inject one of those and would then be comparing two renderings of a value it supplied
// — a test of the formatter. Two child processes with ONE identical environment is the only
// arrangement in which "they agree about this machine" is the thing being asserted.
//
// The extension driven here is a BUILT-IN one, because a test-only fake cannot be compiled into the
// spawned binary. What is compared is therefore the state and the wording of a real entry through
// both doors, which is what the criterion asks.
func TestTheCLIAndTheControlAPIReportTheSameExtensionState(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := xtBuild(t)
	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=",
			model.EnvProvider+"=", model.EnvCredential+"=", model.EnvCredentialFile+"=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}
	if code, out := run("store", "create"); code != 0 {
		t.Fatalf("store create exited %d: %s", code, out)
	}

	_, cliOut := run("ext", "show", "email")
	_, apiOut := run("daemon", "status")

	// The state sentence the CLI printed must appear verbatim in the control API's report. They
	// agree because there is ONE renderer, not because two were written carefully.
	state := extension.Loaded.String()
	if !strings.Contains(cliOut, state) {
		t.Fatalf("`omw ext show email` does not report the built-in email adapter as loaded; this "+
			"test is comparing the wrong thing.\n%s", cliOut)
	}
	if !strings.Contains(apiOut, "email") {
		t.Errorf("`omw daemon status` does not mention the email extension at all, so the control "+
			"API is not reporting extension state:\n%s", apiOut)
	}
	if !strings.Contains(apiOut, state) {
		t.Errorf("the control API does not report the same state as the CLI.\nCLI said: %q\n"+
			"control API:\n%s", state, apiOut)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 22 — no credential, anywhere
// ---------------------------------------------------------------------------

// CRITERION 22. "A model provider's credentials (§3.13) never appear in the extension listing, in
// failure reasons, or in a diagnostics bundle (§3.9 withholds identifying data by default). A test
// that configures a provider with a recognisable secret and then greps every extension-related
// output and a generated bundle finds no occurrence of it."
//
// # ABOUT THE BUNDLE
//
// §3.9's diagnostics bundle is Issue #20's and is not on this branch — there is no `omw doctor
// bundle` to generate. What IS asserted is the property the bundle would have to preserve, and it
// is asserted structurally rather than by grepping one command's output: the credential is refused
// at the point of RECORD, so it never enters the store, so there is nothing for a bundle written
// later to collect. `extension.TestNoCredentialCanReachAnEntryOrTheStore` holds that; this holds
// the surface half.
func TestNoExtensionOutputEverContainsACredential(t *testing.T) {
	// The extension FAILS TO LOAD, because a failure reason is one of the three places criterion 22
	// names and is the one most likely to carry a credential: a real provider's load error is the
	// natural place for "could not authenticate with sk-…".
	xtRegistry(t, xtFake{name: "acme", iface: extension.Model, err: errors.New("the acme endpoint refused this build")})
	env := xtWorld(t)
	// The person's credential lives in THEIR environment, which is where §3.13 says it lives.
	env[model.EnvCredential] = xtSecret

	// NOT mustExt: registering an extension that fails to load exits non-zero by criterion 12, and
	// that is correct. What matters here is that it was recorded.
	if got := runExtCmd(t, env, "register", "acme"); !strings.Contains(got.stdout, "registered: acme") {
		t.Fatalf("acme was not registered, so the greps below examine nothing:\n%s", got.all())
	}

	// IT IS REFUSED AS A SETTING rather than recorded and redacted later — and this is asserted
	// FIRST, while acme is still registered, ON ITS CODE.
	//
	// # THIS ASSERTION WAS WEAK AND A MUTATION CAUGHT IT
	//
	// It used to sit at the END of this test, after the deregister below, and it used to check only
	// that the exit code was non-zero. Both were wrong in the same direction: by then acme was gone,
	// so `configure` failed with "not registered" — a refusal that has nothing to do with
	// credentials — and the test passed with the credential guard entirely disabled. Driving the
	// mutation is how that was found; the repair is to assert the CODE, which no unrelated refusal
	// can produce.
	refused := runExtCmd(t, env, "configure", "acme", "api_key="+xtSecret)
	if refused.code == cli.Success {
		t.Error("a setting called api_key holding a credential was accepted")
	}
	if !strings.Contains(refused.stderr, "extension-setting-looks-like-a-secret") {
		t.Errorf("the credential setting was not refused as a credential (it may have been refused "+
			"for some unrelated reason, which would let the guard rot unnoticed):\n%s", refused.stderr)
	}
	if strings.Contains(refused.all(), xtSecret) {
		t.Errorf("the refusal echoes the credential back:\n%s", refused.all())
	}
	// And the store did not take it: a later listing cannot leak what was never recorded.
	if after := runExtCmd(t, env, "show", "acme"); strings.Contains(after.all(), xtSecret) {
		t.Errorf("the credential survived into the listing:\n%s", after.all())
	}

	// Every extension-related output, including the failure reasons.
	outputs := map[string]string{
		"list":            runExtCmd(t, env, "list").all(),
		"show":            runExtCmd(t, env, "show", "acme").all(),
		"configure":       runExtCmd(t, env, "configure", "acme", "endpoint=https://acme.invalid").all(),
		"register again":  runExtCmd(t, env, "register", "acme").all(),
		"help":            runExtCmd(t, env, "help").all(),
		"deregister":      runExtCmd(t, env, "deregister", "acme").all(),
		"show after gone": runExtCmd(t, env, "show", "acme").all(),
	}
	for label, out := range outputs {
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s produced no output at all; this grep examined nothing", label)
		}
		if strings.Contains(out, xtSecret) {
			t.Errorf("the credential appears in %s output:\n%s", label, out)
		}
	}

}

// ---------------------------------------------------------------------------
// CRITERION 23 — nothing wider than the person it runs for
// ---------------------------------------------------------------------------

// CRITERION 23. "Adding a channel adapter grants it nothing wider than the person it runs for
// (§4.5). An extension cannot read what its person cannot; a scope that would let it is refused
// when requested, not narrowed at the edge."
func TestAScopeWiderThanThePersonIsRefusedAndNothingIsRegistered(t *testing.T) {
	xtRegistry(t, xtFake{name: "slack", iface: extension.Channel})
	env := xtWorld(t)
	// A person who holds only `read`.
	env[extensionScopesEnv] = "read"

	got := runExtCmd(t, env, "register", "slack", "scope=read,publish")
	if got.code == cli.Success {
		t.Fatalf("an extension asking for `publish` on behalf of a person who holds only `read` "+
			"was registered:\n%s", got.all())
	}
	if !strings.Contains(got.stderr, "grant-wider-than-holder") {
		t.Errorf("the refusal does not carry §4.5's code:\n%s", got.stderr)
	}

	// NOT NARROWED AT THE EDGE — it was not registered with `read` instead.
	after := runExtCmd(t, env, "show", "slack")
	if !strings.Contains(after.stdout, "not registered") {
		t.Errorf("slack is not reported as unregistered after a refused registration; a narrower "+
			"grant was issued instead of a refusal (§4.5):\n%s", after.stdout)
	}

	// A word outside the three-scope vocabulary is refused too, and there is no fourth.
	fourth := runExtCmd(t, env, "register", "slack", "scope=admin")
	if fourth.code == cli.Success {
		t.Error("a scope outside the vocabulary was accepted")
	}
	if !strings.Contains(fourth.stderr, "unknown-scope") {
		t.Errorf("a fourth scope word was refused for the wrong reason:\n%s", fourth.stderr)
	}

	// And a scope the person DOES hold goes through, so the refusals above are about width and not
	// about scopes being broken altogether.
	ok := runExtCmd(t, env, "register", "slack", "scope=read")
	if ok.code != cli.Success {
		t.Errorf("a scope the person holds was refused:\n%s", ok.all())
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustExt(t *testing.T, env map[string]string, args ...string) xtResult {
	t.Helper()
	got := runExtCmd(t, env, args...)
	if got.code != cli.Success {
		t.Fatalf("omw ext %s exited %d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), got.code, got.stdout, got.stderr)
	}
	return got
}

func xtBuild(t *testing.T) string {
	t.Helper()
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
	return bin
}

// The failure summary names NEITHER INTERFACE, whichever interface actually failed.
//
// # WHAT THIS CAUGHT, AND WHY IT IS DRIVEN BOTH WAYS
//
// `omw ext list`'s failure summary printed `model.ErrProviderFailedToLoad.Code`
// unconditionally, so a machine whose only broken extension was a CHANNEL ADAPTER told every
// machine reader `code: model-provider-extension-failed-to-load`. A reviewer drove it and refused
// the pull request. It is the Issue's own opening story — the Slack adapter that will not load —
// reported as a model fault, and it sends the reader to the wrong subsystem entirely.
//
// The obvious repair is to pick the code from whichever interface failed, and that is a trap: with
// one broken extension of each, it has to pick one anyway, and it would break §2.5's symmetry in
// exactly the place a script looks. So the summary is NEUTRAL, and this asserts it from both ends —
// the channel-only case, the model-only case, and the mixed case — and asserts the three produce
// the SAME code. Driving only the channel case would pass just as happily on a build that had
// swapped one interface's bias for the other's.
//
// Per-ENTRY codes are a different question and stay interface-specific; `model.ErrProviderFailedToLoad`
// argues why, and nothing here disturbs it.
func TestTheFailureSummaryNamesNeitherInterface(t *testing.T) {
	cases := map[string][]extension.Extension{
		"only a channel adapter is broken": {
			xtFake{name: "brokechan", iface: extension.Channel, err: errors.New("libslack.so is missing")},
			xtFake{name: "finemodel", iface: extension.Model},
		},
		"only a model provider is broken": {
			xtFake{name: "brokemodel", iface: extension.Model, err: errors.New("the acme extension needs a newer omw")},
			xtFake{name: "finechan", iface: extension.Channel},
		},
		"one of each is broken": {
			xtFake{name: "brokechan", iface: extension.Channel, err: errors.New("libslack.so is missing")},
			xtFake{name: "brokemodel", iface: extension.Model, err: errors.New("the acme extension needs a newer omw")},
		},
	}

	summaries := map[string]string{}
	for label, exts := range cases {
		t.Run(label, func(t *testing.T) {
			xtRegistry(t, exts...)
			env := xtWorld(t)
			for _, e := range exts {
				// NOT mustExt: registering a broken extension exits non-zero by criterion 12.
				if got := runExtCmd(t, env, "register", e.Name()); !strings.Contains(got.stdout, "registered: "+e.Name()) {
					t.Fatalf("%s was not registered, so this case examines nothing:\n%s", e.Name(), got.all())
				}
			}

			got := runExtCmd(t, env, "list")
			if got.code != cli.ExitFailure {
				t.Fatalf("exit %d, want %d — this case is meant to have a failure in it\n%s",
					got.code, cli.ExitFailure, got.all())
			}

			// The summary line, isolated. Asserting on the whole output would also match the
			// per-entry lines, which are allowed to be interface-specific.
			summary := ""
			for _, line := range strings.Split(got.stderr, "\n") {
				if strings.Contains(line, "FAILED TO LOAD (code:") {
					summary = line
				}
			}
			if summary == "" {
				t.Fatalf("no failure summary line was printed at all, so the assertions below "+
					"examine nothing:\n%s", got.stderr)
			}
			summaries[label] = summary

			if !strings.Contains(summary, extension.ErrFailedToLoad.Code) {
				t.Errorf("the summary does not carry the interface-neutral code %q:\n%s",
					extension.ErrFailedToLoad.Code, summary)
			}
			for _, biased := range []string{
				model.ErrProviderFailedToLoad.Code,
				channels.ErrAdapterFailedToLoad.Code,
				"model-provider", "channel-adapter",
			} {
				if strings.Contains(summary, biased) {
					t.Errorf("the summary over a mixed set names one interface (%q). A machine whose "+
						"broken extension is of the OTHER interface is told the wrong subsystem is "+
						"at fault:\n%s", biased, summary)
				}
			}
		})
	}

	// AND THE THREE ARE THE SAME SENTENCE apart from the count. Each case above could pass alone on
	// a build that picked the code from whichever interface happened to fail.
	if len(summaries) != len(cases) {
		t.Fatalf("only %d of %d cases produced a summary; the comparison below is incomplete", len(summaries), len(cases))
	}
	normalised := map[string]string{}
	for label, s := range summaries {
		// The count differs between the one-broken and two-broken cases, and that is the only thing
		// permitted to differ.
		normalised[label] = strings.NewReplacer("1 extension(s)", "N extension(s)", "2 extension(s)", "N extension(s)").Replace(s)
	}
	var first, firstLabel string
	for label, s := range normalised {
		if first == "" {
			first, firstLabel = s, label
			continue
		}
		if s != first {
			t.Errorf("the failure summary differs by which interface failed.\n%s: %s\n%s: %s\n"+
				"§2.5: the two interfaces are one mechanism, and this is the one line a script reads.",
				firstLabel, first, label, s)
		}
	}
}

// xtCorrupt damages one record's checksum in place — a bad disk, a half-synced file. The record is
// still valid JSON and still parses; its content no longer matches what it says its content is.
func xtCorrupt(t *testing.T, root string, kind, id string) {
	t.Helper()
	dir := filepath.Join(root, "records", kind)
	matches, err := filepath.Glob(filepath.Join(dir, id+".*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one record file for %q in %s, found %v (err %v)", id, dir, matches, err)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("reading %s: %v", matches[0], err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("the record envelope is not JSON; this test would damage the wrong thing: %v", err)
	}
	if _, ok := envelope["sha256"]; !ok {
		t.Fatalf("the record envelope has no sha256 field; this test would damage nothing")
	}
	envelope["sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	out, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}
	if err := os.WriteFile(matches[0], out, 0o600); err != nil {
		t.Fatalf("writing the damaged record: %v", err)
	}
}

// QA'S EXACT REPRODUCTION. One damaged record, and the footer must not claim completeness.
//
// # WHAT WAS DRIVEN AND REFUSED
//
//	omw ext list: which extensions are registered could not be determined: ...
//	extensions:
//	  email  registered, and it loaded
//	  teams  registered, and it loaded
//	omw ext list: every registered extension loaded.          <-- FALSE
//	CLI-EXIT=3
//
// Two registered, FAILED-TO-LOAD extensions had vanished, and the last line a person reads said
// everything was fine. The exit code and the header were honest; the prose was not.
//
// The header and the footer are both asserted here, because fixing only the footer would leave the
// entries themselves still missing — and that was the real defect, in `Registered`.
func TestADamagedRecordNeitherErasesTheInventoryNorLetsTheFooterClaimAllIsWell(t *testing.T) {
	xtRegistry(t,
		xtFake{name: "brokeext", iface: extension.Channel, err: errors.New("libslack.so is missing")},
		xtFake{name: "fineext", iface: extension.Model},
		xtFake{name: "damagedext", iface: extension.Channel},
	)
	env := xtWorld(t)
	for _, n := range []string{"brokeext", "fineext", "damagedext"} {
		if got := runExtCmd(t, env, "register", n); !strings.Contains(got.stdout, "registered: "+n) {
			t.Fatalf("%s was not registered, so this test examines nothing:\n%s", n, got.all())
		}
	}
	xtCorrupt(t, env[store.PathEnv], "extension", "damagedext")

	got := runExtCmd(t, env, "list")

	// THE FAILED-TO-LOAD EXTENSION IS STILL THERE. This is the half that matters most: a broken
	// extension reported as absent is this Issue's opening story.
	if !strings.Contains(got.stdout, "brokeext") {
		t.Errorf("the failed-to-load extension vanished because a DIFFERENT record was damaged:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "IT FAILED TO LOAD") {
		t.Errorf("no extension is reported as failed to load:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "fineext") || !strings.Contains(got.stdout, "damagedext") {
		t.Errorf("an intact or a damaged registration is missing from the listing:\n%s", got.stdout)
	}

	// THE FOOTER DOES NOT CLAIM COMPLETENESS.
	if strings.Contains(got.stderr, "every registered extension loaded") {
		t.Errorf("the footer claims every registered extension loaded, over an inventory holding a "+
			"failure and a record that would not read:\nstdout:\n%s\nstderr:\n%s", got.stdout, got.stderr)
	}
	// There IS a failure present, so the failure summary is the honest one and the exit is 1.
	if got.code != cli.ExitFailure {
		t.Errorf("exit %d, want %d — a failed-to-load extension is a determined negative\n%s",
			got.code, cli.ExitFailure, got.all())
	}
}

// The same state with NO failure in it: the footer must still not say all is well, and the exit
// must be undetermined rather than success.
//
// Driven separately because the case above has a real failure in it, and a build that only ever
// reported failures would pass it while still claiming completeness on a machine whose sole problem
// is a record it cannot read.
func TestADamagedRecordAloneIsUndeterminedAndNotSuccess(t *testing.T) {
	xtRegistry(t,
		xtFake{name: "fineext", iface: extension.Model},
		xtFake{name: "damagedext", iface: extension.Channel},
	)
	env := xtWorld(t)
	for _, n := range []string{"fineext", "damagedext"} {
		mustExt(t, env, "register", n)
	}
	xtCorrupt(t, env[store.PathEnv], "extension", "damagedext")

	got := runExtCmd(t, env, "list")
	if strings.Contains(got.stderr, "every registered extension loaded") {
		t.Errorf("the footer claims completeness over a record it could not read:\n%s", got.stderr)
	}
	if got.code != cli.ExitUndetermined {
		t.Errorf("exit %d, want %d — 'I could not read part of the inventory' is not success\n%s",
			got.code, cli.ExitUndetermined, got.all())
	}
	if !strings.Contains(got.stdout+got.stderr, "could not be determined") {
		t.Errorf("nothing in the output says anything was undetermined:\n%s", got.all())
	}
}

// `omw ext show` must not answer "not registered" from an inventory it could not ENUMERATE.
//
// `Find` answers not-registered for a name it did not see. Over a complete inventory that is a
// determined fact and stays one — including when some individual RECORD was damaged, because the
// enumeration still told us which names exist. It is a lie only when the enumeration itself failed,
// since the record naming this extension may be one we never saw.
//
// I had this wrong first: the test drove a damaged record and expected `show` to go undetermined,
// which would have been the product hedging on a question it can actually answer. Per-record
// degradation is exactly what makes the damaged-record case determinate. The residual is here.
func TestShowDoesNotClaimNotRegisteredWhenTheInventoryCouldNotBeEnumerated(t *testing.T) {
	xtRegistry(t, xtFake{name: "someext", iface: extension.Channel})
	env := xtWorld(t)
	mustExt(t, env, "register", "someext")

	dir := filepath.Join(env[store.PathEnv], "records", "extension")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Skipf("cannot make the directory unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	// The fixture is checked: if the directory is still readable, everything below passes for the
	// wrong reason.
	if probe := runExtCmd(t, env, "list"); !strings.Contains(probe.all(), "may not be all of them") {
		t.Skipf("the registrations directory is still readable (running as root?), so this test "+
			"would prove nothing:\n%s", probe.all())
	}

	got := runExtCmd(t, env, "show", "somethingelse")
	if got.code == cli.Success {
		t.Errorf("exit 0 for a name looked up in an inventory that could not be enumerated:\n%s", got.all())
	}
	if !strings.Contains(got.all(), "could not be determined") {
		t.Errorf("it answered 'not registered' without saying the inventory was unreadable:\n%s", got.all())
	}
}

// A damaged RECORD leaves `show` determinate about a different name — the control for the test
// above, so that the hedge it requires cannot spread to questions the product can answer.
func TestShowStaysDeterminateWhenOnlyOneRecordIsDamaged(t *testing.T) {
	xtRegistry(t, xtFake{name: "damagedext", iface: extension.Channel})
	env := xtWorld(t)
	mustExt(t, env, "register", "damagedext")
	xtCorrupt(t, env[store.PathEnv], "extension", "damagedext")

	got := runExtCmd(t, env, "show", "somethingelse")
	if got.code != cli.Success {
		t.Errorf("exit %d — the enumeration succeeded, so whether %q is registered IS determined; "+
			"hedging here would make the product vague about a question it can answer\n%s",
			got.code, "somethingelse", got.all())
	}
	if !strings.Contains(got.stdout, "not registered") {
		t.Errorf("it did not answer:\n%s", got.stdout)
	}
}

// CRITERION 20 IN THE STATE THAT BROKE IT (B2).
//
// `extensionsFor` discarded `extension.Inventory`'s error twice — `entries, _ :=` — so the CLI
// printed "which extensions are registered could not be determined" and the control API said
// nothing at all. Same machine, two surfaces, two different reports. PRD §4.3: "the control API and
// the CLI report the same state."
//
// The repair is structural rather than a third careful format string: both surfaces render an
// `extension.Listing`, which carries the incompleteness inside the value, so there is no separate
// return value for either of them to drop.
func TestTheCLIAndTheControlAPIAgreeWhenTheInventoryCannotBeRead(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := xtBuild(t)
	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=",
			model.EnvProvider+"=", model.EnvCredential+"=", model.EnvCredentialFile+"=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}
	if code, out := run("store", "create"); code != 0 {
		t.Fatalf("store create exited %d: %s", code, out)
	}
	if code, out := run("ext", "register", "email"); code != 0 {
		t.Fatalf("registering email exited %d: %s", code, out)
	}

	dir := filepath.Join(root, "records", "extension")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Skipf("cannot make the directory unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, cliOut := run("ext", "list")
	if !strings.Contains(cliOut, "may not be all of them") {
		t.Skipf("the registrations directory is still readable (running as root?), so this test "+
			"would prove nothing:\n%s", cliOut)
	}
	_, apiOut := run("daemon", "status")

	// The warning the CLI prints must appear in the control API's report too.
	for _, want := range []string{"may not be all of them", "NOT a report that none is registered"} {
		if !strings.Contains(apiOut, want) {
			t.Errorf("the control API does not carry %q, which the CLI printed. Same machine, two "+
				"surfaces, two different reports (criterion 20, §4.3).\n\nCLI:\n%s\ncontrol API:\n%s",
				want, cliOut, apiOut)
		}
	}
}

// CRITERION 22'S THIRD PLACE, NOW THAT IT EXISTS: a generated diagnostics bundle.
//
// "A model provider's credentials never appear in the extension listing, in failure reasons, or in
// a diagnostics bundle (§3.9 withholds identifying data by default). A test that configures a
// provider with a recognisable secret and then greps every extension-related output AND A GENERATED
// BUNDLE finds no occurrence of it."
//
// When this branch opened, §3.9's bundle was Issue #20's and was not in the tree, so the property
// was asserted structurally — the credential is refused at the point of RECORD, so nothing exists
// for a bundle to collect. `omw diagnostics` has since landed on main. The structural argument still
// holds and is still the reason this passes, but an argument is not a measurement: this generates a
// real bundle with a real key in the environment and greps every byte of it, including
// `--include-bodies`, which is the affirmative act that widens what is collected.
func TestNoGeneratedDiagnosticsBundleContainsAnExtensionCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := xtBuild(t)
	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()
	bundleDir := t.TempDir()

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=",
			// THE PERSON'S KEY, WHERE §3.13 SAYS IT LIVES: their own environment.
			model.EnvProvider+"=acme", model.EnvCredential+"="+xtSecret, model.EnvCredentialFile+"=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}
	if code, out := run("store", "create"); code != 0 {
		t.Fatalf("store create exited %d: %s", code, out)
	}
	// A registered extension with settings, so the bundle has extension material to collect if it
	// collects any.
	run("ext", "register", "email")
	run("ext", "configure", "email", "endpoint=https://inside.example.invalid", "key_file="+filepath.Join(sandbox, "k"))
	run("model", "use", "acme")

	dest := filepath.Join(bundleDir, "b")
	if code, out := run("diagnostics", dest, "--include-bodies"); code != 0 && code != 3 {
		t.Fatalf("omw diagnostics exited %d: %s", code, out)
	}

	// EVERY BYTE OF THE BUNDLE. Walked rather than sampled: a grep of the manifest alone would miss
	// a credential in a collected file, which is where one would actually end up.
	files := 0
	err := filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files++
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Errorf("reading %s from the bundle: %v", p, rerr)
			return nil
		}
		if bytes.Contains(body, []byte(xtSecret)) {
			t.Errorf("the credential appears in the bundle at %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the bundle: %v", err)
	}
	// THE CONTROL. A walk that found nothing would pass loudest of all.
	if files == 0 {
		t.Fatalf("the bundle at %s contains no files, so this grep examined nothing", dest)
	}
	t.Logf("grepped %d file(s) in the bundle", files)

	// And the control on the sentinel itself: it really is in the environment these runs saw, so a
	// clean grep is a finding rather than an artefact of the key never being present.
	if _, out := run("model", "show"); strings.Contains(out, xtSecret) {
		t.Fatalf("`omw model show` printed the credential, which means the sentinel is live but the "+
			"product leaks it elsewhere:\n%s", out)
	}
}

// QA'S REPRODUCTION AT THE COMMAND LINE: register a channel adapter, choose it as a model.
//
//	omw ext register slack
//	omw model use slack
//
// Both documented, both exit 0, and `omw model show` then said the provider's extension had loaded.
// Driven here through the real command path, because the defect is reachable by a person typing two
// supported commands and no test-only seam should stand between this assertion and that.
//
// Both arms, so the interface check is pinned rather than the lookup merely broken.
func TestChoosingAChannelAdapterAsAModelIsNotReportedAsLoaded(t *testing.T) {
	const name = "slackish"

	t.Run("a channel adapter chosen as a model", func(t *testing.T) {
		xtRegistry(t, xtFake{name: name, iface: extension.Channel})
		env := xtWorld(t)
		mustExt(t, env, "register", name)

		// The fixture: it really is registered and really does load.
		if shown := mustExt(t, env, "show", name); !strings.Contains(shown.stdout, "registered, and it loaded") {
			t.Fatalf("the channel adapter did not load, so this test is not reproducing the "+
				"situation it is about:\n%s", shown.stdout)
		}

		var out, errb bytes.Buffer
		if code := cli.Run([]string{"model", "use", name}, &out, &errb,
			func(k string) string { return env[k] }); code != cli.Success {
			t.Fatalf("`omw model use %s` exited %d; qa's reproduction relies on it being accepted\n%s%s",
				name, code, out.String(), errb.String())
		}

		var sout, serr bytes.Buffer
		cli.Run([]string{"model", "show"}, &sout, &serr, func(k string) string { return env[k] })
		all := sout.String() + serr.String()
		if strings.Contains(all, "is configured, with a credential") {
			t.Errorf("`omw model show` reports a working model configuration when what is "+
				"registered under that name is a CHANNEL ADAPTER:\n%s", all)
		}
	})

	t.Run("a real model provider chosen as a model", func(t *testing.T) {
		// THE CONTROL. Returning "not registered" for everything would pass the arm above.
		//
		// IT USES A GENUINELY REGISTERED PROVIDER — `model.Register`, the real door, which offers
		// into the one extension registry as well. An `xtFake` with interface Model is not enough
		// and an earlier version of this test used one: it never entered `model`'s own registry, so
		// Issue #18's `View.Adapter` correctly reported "this build has no adapter", and the
		// control failed for a reason that had nothing to do with the interface check. The fixture
		// has to be a real provider because the thing being controlled for is the real path.
		provider := xtStubProvider{name: name}
		model.Register(provider)
		t.Cleanup(func() { extension.Default.Withdraw(name) })
		prev := extensionRegistry
		extensionRegistry = extension.Default
		t.Cleanup(func() { extensionRegistry = prev })

		env := xtWorld(t)
		mustExt(t, env, "register", name)

		var out, errb bytes.Buffer
		if code := cli.Run([]string{"model", "use", name}, &out, &errb,
			func(k string) string { return env[k] }); code != cli.Success {
			t.Fatalf("`omw model use %s` exited %d\n%s%s", name, code, out.String(), errb.String())
		}
		// Asserted through the command, against the store this invocation actually uses. An
		// earlier version of this passed a nil store to extension.Read and failed because the
		// registration was invisible — a test bug that looked exactly like the product bug it was
		// meant to rule out.
		var sout, serr bytes.Buffer
		cli.Run([]string{"model", "show"}, &sout, &serr, func(k string) string { return env[k] })
		all := sout.String() + serr.String()
		if strings.Contains(all, "no extension for it is registered") ||
			strings.Contains(all, "no model-provider extension for it is registered") {
			t.Fatalf("a genuine model provider is reported as having no registered extension; the "+
				"interface check has broken the case it was supposed to protect:\n%s", all)
		}
		if strings.Contains(all, "has no adapter for") {
			t.Fatalf("a registered model provider is reported as having no adapter:\n%s", all)
		}
	})
}

// xtStubProvider is a real `model.Provider`, registered through the real door, for the control arm
// above. It opens nothing — the control is about the interface check, not about talking to a model.
type xtStubProvider struct{ name string }

func (p xtStubProvider) Name() string { return p.name }
func (p xtStubProvider) Open(string) (model.Session, error) {
	return nil, errors.New("this provider is not opened in tests")
}
