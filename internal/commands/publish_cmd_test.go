package commands

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/publish"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Driving the command
// ---------------------------------------------------------------------------

type pubResult struct {
	code   int
	stdout string
	stderr string
}

func (r pubResult) all() string { return r.stdout + r.stderr }

func runPublishCmd(t *testing.T, env map[string]string, args ...string) pubResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"publish"}, args...), &out, &errb, func(k string) string { return env[k] })
	return pubResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// pubWorld is a machine with a store, an identity and the publish grant, and NO hub.
//
// No hub is the default on purpose: PRD §4.4 says the local half stands alone, so the environment a
// test has to build deliberately is the one WITH a hub.
func pubWorld(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	return map[string]string{
		store.PathEnv:  root,
		pubEnvIdentity: "ada",
		pubEnvScopes:   "read,publish",
	}
}

// pubSocket keeps a socket path short enough for the platform's limit.
func pubSocket(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "omwcmd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// pubHub starts a hub and points env at it.
func pubHub(t *testing.T, env map[string]string) *hub.Store {
	t.Helper()
	addr := pubSocket(t, "hub.sock")
	s := hub.NewStore(nil)
	ln, err := publish.Listen(addr)
	if err != nil {
		t.Fatalf("opening the hub endpoint: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go publish.Serve(ln, s, hub.NewOnce())
	env[pubEnvHub] = addr
	return s
}

func pubDraft(t *testing.T, env map[string]string, id, body string) {
	t.Helper()
	s, err := store.Open(env[store.PathEnv])
	if err != nil {
		t.Fatal(err)
	}
	o, err := drafts.InStore(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Revise(hub.NoteID(id), body); err != nil {
		t.Fatal(err)
	}
}

// pubStateLine extracts the machine-checkable state line.
func pubStateLine(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "state: ") {
			return line
		}
	}
	t.Fatalf("this output has no state line:\n%s", out)
	return ""
}

// ---------------------------------------------------------------------------
// The happy path through the CLI
// ---------------------------------------------------------------------------

func TestPublishingThroughTheCLIPutsOneNoteOnTheHubAndTakesItOutOfTheOutbox(t *testing.T) {
	env := pubWorld(t)
	h := pubHub(t, env)
	pubDraft(t, env, "quota", "the quota is four hundred")

	got := runPublishCmd(t, env, "note", "quota", "The quota")
	if got.code != cli.Success {
		t.Fatalf("omw publish note exited %d:\n%s", got.code, got.all())
	}
	if !strings.Contains(got.stdout, "state: "+string(publish.StatePublished)) {
		t.Errorf("the output does not report the note as published:\n%s", got.stdout)
	}
	if h.Count() != 1 {
		t.Fatalf("the hub holds %d notes; want 1", h.Count())
	}
	// The outbox no longer lists it, through Issue #9's own command.
	list := runOutboxCmd(t, env, "list")
	if strings.Contains(list.stdout, "quota") {
		t.Errorf("omw outbox still lists the published note:\n%s", list.stdout)
	}
	// And this client still knows where it went.
	state := runPublishCmd(t, env, "state", "quota")
	if state.code != cli.Success || !strings.Contains(state.stdout, "container: hub") {
		t.Errorf("omw publish state after publishing (exit %d):\n%s", state.code, state.all())
	}
}

// ---------------------------------------------------------------------------
// Criterion 6, through the surface a person actually uses
// ---------------------------------------------------------------------------

func TestTheCLIReportsFourStatesThatDifferInTheOutput(t *testing.T) {
	env := pubWorld(t)
	h := pubHub(t, env)
	_ = h
	pubDraft(t, env, "resting", "body")
	pubDraft(t, env, "gone", "body")
	pubDraft(t, env, "rejected", "body")

	if got := runPublishCmd(t, env, "note", "gone"); got.code != cli.Success {
		t.Fatalf("publishing 'gone' exited %d:\n%s", got.code, got.all())
	}
	// A refusal: the grant carries no publish scope.
	env[pubEnvScopes] = "read"
	if got := runPublishCmd(t, env, "note", "rejected"); got.code != cli.ExitFailure {
		t.Fatalf("a scope refusal exited %d, want %d:\n%s", got.code, cli.ExitFailure, got.all())
	}
	env[pubEnvScopes] = "read,publish"

	// In flight: a listener that reads and never answers.
	silent := pubSocket(t, "silent.sock")
	ln, err := publish.Listen(silent)
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
	pubDraft(t, env, "flying", "body")
	realHub := env[pubEnvHub]
	env[pubEnvHub] = silent
	if got := runPublishCmd(t, env, "note", "flying"); got.code != cli.ExitUndetermined {
		ln.Close()
		t.Fatalf("an unanswered attempt exited %d, want %d:\n%s", got.code, cli.ExitUndetermined, got.all())
	}
	ln.Close()
	env[pubEnvHub] = realHub

	lines := map[string]string{}
	for _, id := range []string{"resting", "flying", "gone", "rejected"} {
		lines[id] = pubStateLine(t, runPublishCmd(t, env, "state", id).stdout)
	}
	seen := map[string]string{}
	for id, line := range lines {
		if other, dup := seen[line]; dup {
			t.Errorf("%s and %s both report %q — two of the four states have collapsed", other, id, line)
		}
		seen[line] = id
	}
	if len(seen) != 4 {
		t.Fatalf("four notes in four states produced %d distinct state lines: %v", len(seen), lines)
	}

	// AND THEY ARE THE FOUR NAMED STATES, not four distinct accidents.
	want := map[string]publish.State{
		"resting": publish.StateDrafted, "flying": publish.StateInFlight,
		"gone": publish.StatePublished, "rejected": publish.StateRefused,
	}
	for id, st := range want {
		if !strings.Contains(lines[id], string(st)) {
			t.Errorf("%s reports %q, want state %q", id, lines[id], st)
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 8, 11 — three different failures, three different exit codes
// ---------------------------------------------------------------------------

func TestARefusalAnUnreachableHubAndNoHubHaveThreeDifferentExitCodesAndOutputs(t *testing.T) {
	env := pubWorld(t)
	pubHub(t, env)
	realHub := env[pubEnvHub]

	pubDraft(t, env, "a", "body")
	pubDraft(t, env, "b", "body")
	pubDraft(t, env, "c", "body")

	env[pubEnvScopes] = "read"
	refused := runPublishCmd(t, env, "note", "a")
	env[pubEnvScopes] = "read,publish"

	env[pubEnvHub] = pubSocket(t, "dead.sock")
	unreachable := runPublishCmd(t, env, "note", "b")

	env[pubEnvHub] = ""
	noHub := runPublishCmd(t, env, "note", "c")
	env[pubEnvHub] = realHub

	// A REFUSAL IS DETERMINED AND AN UNREACHABLE HUB IS NOT. They must not share an exit code.
	if refused.code != cli.ExitFailure {
		t.Errorf("a refusal exits %d, want %d", refused.code, cli.ExitFailure)
	}
	if unreachable.code != cli.ExitUndetermined {
		t.Errorf("an unreachable hub exits %d, want %d", unreachable.code, cli.ExitUndetermined)
	}
	if noHub.code != cli.ExitFailure {
		t.Errorf("no hub configured exits %d, want %d — it is a determined fact about this machine", noHub.code, cli.ExitFailure)
	}
	if refused.code == unreachable.code {
		t.Errorf("a refusal and an unreachable hub share exit code %d", refused.code)
	}

	// AND THE OUTPUTS DIFFER, not only the exit codes.
	outs := map[string]string{"refused": refused.all(), "unreachable": unreachable.all(), "no hub": noHub.all()}
	seen := map[string]string{}
	for name, out := range outs {
		if other, dup := seen[out]; dup {
			t.Errorf("%s and %s produce identical output", other, name)
		}
		seen[out] = name
	}
	if !strings.Contains(refused.all(), hub.ErrPublishScopeRequired.Code) {
		t.Errorf("the refusal does not carry a code a script can branch on:\n%s", refused.all())
	}
	if !strings.Contains(unreachable.all(), hub.ErrHubUnreachable.Code) {
		t.Errorf("the unreachable answer does not carry %q:\n%s", hub.ErrHubUnreachable.Code, unreachable.all())
	}
	if !strings.Contains(noHub.all(), hub.ErrNoHubConfigured.Code) {
		t.Errorf("the no-hub answer does not name what is missing:\n%s", noHub.all())
	}
	// An unreachable hub never renders as a refusal, and vice versa.
	if strings.Contains(pubStateLine(t, unreachable.stdout), string(publish.StateRefused)) {
		t.Errorf("an unreachable hub put the note into the refused state:\n%s", unreachable.stdout)
	}
	if strings.Contains(unreachable.all(), hub.ErrPublishScopeRequired.Code) {
		t.Errorf("an unreachable hub rendered a scope refusal:\n%s", unreachable.all())
	}
}

// ---------------------------------------------------------------------------
// Criterion 15 — publish never starts the daemon
// ---------------------------------------------------------------------------

func TestPublishSaysTheDaemonIsNotRunningAndStartsNothing(t *testing.T) {
	env := pubWorld(t)
	pubHub(t, env)
	pubDraft(t, env, "n", "body")

	// THE ONE LIVENESS ANSWER IS STUBBED, not a private probe of this command's own. Issue #41
	// consolidated four guesses into daemonLiveness; a test that stubbed something else would be
	// proving the rendering of a fifth.
	prev := daemonLiveness
	daemonLiveness = func(cli.Env) (tri.Value, string) { return tri.No, "" }
	t.Cleanup(func() { daemonLiveness = prev })

	got := runPublishCmd(t, env, "note", "n")
	if !strings.Contains(got.stderr, "daemon: not running") {
		t.Errorf("publish does not say the daemon is not running:\n%s", got.stderr)
	}
	if !strings.Contains(got.stderr, "nothing has been started") {
		t.Errorf("publish does not say it started nothing:\n%s", got.stderr)
	}

	// The three-valued answer reaches the surface intact.
	daemonLiveness = func(cli.Env) (tri.Value, string) { return tri.Undetermined, "the store's lock could not be read" }
	pubDraft(t, env, "m", "body")
	got = runPublishCmd(t, env, "state", "m")
	if !strings.Contains(got.stderr, tri.Undetermined.String()) {
		t.Errorf("an undetermined daemon state renders as something else:\n%s", got.stderr)
	}
	if strings.Contains(got.stderr, "daemon: not running") {
		t.Errorf("an undetermined daemon state renders as 'not running':\n%s", got.stderr)
	}
}

// THROUGH THE REAL BINARY, so "started nothing" is a fact about processes and not about a stub.
func TestPublishingThroughTheRealBinaryLeavesNoProcessBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("this test builds the binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	build := exec.Command(goTool, "build", "-o", bin, "./cmd/omw")
	build.Dir = repoRoot(t)
	if out, berr := build.CombinedOutput(); berr != nil {
		t.Fatalf("building omw: %v\n%s", berr, out)
	}

	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()
	hubAddr := pubSocket(t, "hub.sock")
	hs := hub.NewStore(nil)
	ln, lerr := publish.Listen(hubAddr)
	if lerr != nil {
		t.Fatal(lerr)
	}
	defer ln.Close()
	go publish.Serve(ln, hs, hub.NewOnce())

	run := func(args ...string) (int, string, *os.ProcessState) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// THE DEVICE POINTER IS SANDBOXED. Inheriting the developer's environment repoints their
		// real store at a t.TempDir() that is then deleted; both variables are set because the
		// pointer resolves from XDG_DATA_HOME and falls back to HOME.
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB="+hubAddr, "OMW_IDENTITY=ada", "OMW_TOKEN_SCOPES=read,publish",
			"OMW_MODEL=", "OMW_MODEL_KEY=", "OMW_CONTROL_SOCKET=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out), cmd.ProcessState
	}

	if code, out, _ := run("store", "create"); code != 0 {
		t.Fatalf("omw store create exited %d:\n%s", code, out)
	}
	if code, out, _ := run("outbox", "draft", "note-a", "worth publishing"); code != 0 {
		t.Fatalf("omw outbox draft exited %d:\n%s", code, out)
	}
	code, out, state := run("publish", "note", "note-a")
	if code != 0 {
		t.Fatalf("omw publish note exited %d:\n%s", code, out)
	}
	// CRITERION 15 THROUGH REAL PROCESSES. The child had its own process group; a daemon started on
	// the person's behalf would still be in it.
	if pgrep, lerr := exec.LookPath("pgrep"); lerr == nil {
		left, _ := exec.Command(pgrep, "-g", strconv.Itoa(state.Pid())).Output()
		for _, line := range strings.Fields(string(left)) {
			if line != "" && line != strconv.Itoa(state.Pid()) {
				t.Errorf("a process is still running in the publish command's process group: pid %s", line)
			}
		}
	} else {
		t.Log("pgrep is not on PATH; the leftover-process check was not run")
	}
	if hs.Count() != 1 {
		t.Fatalf("the hub holds %d notes after one publish through the real binary", hs.Count())
	}
	// A SECOND PROCESS KNOWS WHERE IT WENT. That is what durable means.
	code, out, _ = run("publish", "state", "note-a")
	if code != 0 || !strings.Contains(out, "state: "+string(publish.StatePublished)) {
		t.Fatalf("a new process does not report the note as published (exit %d):\n%s", code, out)
	}
	// Retrying from a third process makes no second copy.
	if code, out, _ = run("publish", "note", "note-a"); code != 0 {
		t.Fatalf("republishing exited %d:\n%s", code, out)
	}
	if hs.Count() != 1 {
		t.Fatalf("the hub holds %d notes after a retry; want 1", hs.Count())
	}
}

// ---------------------------------------------------------------------------
// list, and the usage surface
// ---------------------------------------------------------------------------

func TestAnEmptyListIsADeterminedAnswerAndNotSilence(t *testing.T) {
	env := pubWorld(t)
	got := runPublishCmd(t, env, "list")
	if got.code != cli.Success {
		t.Fatalf("exit %d:\n%s", got.code, got.all())
	}
	if !strings.Contains(got.stdout, "determined") {
		t.Errorf("an empty list does not say it is a determined answer:\n%s", got.stdout)
	}
}

func TestTheListNamesEveryNoteThisClientKnowsAboutIncludingPublishedOnes(t *testing.T) {
	env := pubWorld(t)
	pubHub(t, env)
	pubDraft(t, env, "resting", "body")
	pubDraft(t, env, "gone", "body")
	if got := runPublishCmd(t, env, "note", "gone"); got.code != cli.Success {
		t.Fatalf("exit %d:\n%s", got.code, got.all())
	}
	got := runPublishCmd(t, env, "list")
	for _, want := range []string{"resting", "gone"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("the listing omits %q:\n%s", want, got.stdout)
		}
	}
}

func TestPublishRefusesAnUnknownSubcommandByName(t *testing.T) {
	env := pubWorld(t)
	got := runPublishCmd(t, env, "sned", "n")
	if got.code != cli.ExitUsage {
		t.Errorf("exit %d, want %d", got.code, cli.ExitUsage)
	}
	if !strings.Contains(got.stderr, "sned") {
		t.Errorf("the typo is not echoed back:\n%s", got.stderr)
	}
}

// A caller that never asked for the publish grant does not get it by default (PRD §3.10).
func TestNoScopesConfiguredMeansNoPublishGrant(t *testing.T) {
	env := pubWorld(t)
	pubHub(t, env)
	delete(env, pubEnvScopes)
	pubDraft(t, env, "n", "body")
	got := runPublishCmd(t, env, "note", "n")
	if got.code != cli.ExitFailure {
		t.Fatalf("exit %d, want %d:\n%s", got.code, cli.ExitFailure, got.all())
	}
	if !strings.Contains(got.all(), hub.ErrPublishScopeRequired.Code) {
		t.Errorf("the refusal does not name the missing scope:\n%s", got.all())
	}
}

// ---------------------------------------------------------------------------
// A determined absence is not an undetermined answer (Issue #10, QA §4)
// ---------------------------------------------------------------------------

// pubUnreadableStore builds a REAL store — created by [store.Create], so it carries a real marker —
// and then takes the permission to read it away. A bare directory would not do: [store.Open]
// rejects it as [store.ErrNotFound], which compares the wrong two things.
func pubUnreadableStore(t *testing.T) map[string]string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 directory anyway, so this arm cannot be built here")
	}
	env := pubWorld(t)
	root := env[store.PathEnv]
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatalf("making the store unreadable: %v", err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })
	return env
}

// TestPublishDistinguishesAnAbsentStoreFromAnUnreadableOneByExitCode is the three-valued contract at
// the CLI seam: `could not determine` and `determined to be nothing` must never share an exit code.
//
// A store that is ABSENT is a DETERMINED fact — store.Open said ErrNotFound, which the store package
// documents as "the truth, and a caller that finds it must say so". Reporting it as
// ExitUndetermined tells a script "unknown, try again" about a machine that simply has no store, and
// it will retry or alert forever. A store that cannot be READ is the genuinely undetermined one.
//
// This is the mirror of Issues #68/#48, which report an undetermined answer as a determined
// negative. Same rule, opposite direction — so the fix is not to make everything undetermined.
//
// All three subcommands are driven. A fix verified on one and assumed for the other two is how the
// collapse shipped in the first place.
func TestPublishDistinguishesAnAbsentStoreFromAnUnreadableOneByExitCode(t *testing.T) {
	for _, sub := range [][]string{{"note", "n"}, {"state", "n"}, {"list"}} {
		name := sub[0]
		t.Run(name, func(t *testing.T) {
			// DETERMINED: there is no store here, and that is an answer.
			absent := runPublishCmd(t, obNoStore(t), sub...)
			if absent.code != cli.ExitFailure {
				t.Errorf("no store at all: exit %d, want %d (ExitFailure) — an absent store is a "+
					"DETERMINED negative, not something that could not be determined\n%s",
					absent.code, cli.ExitFailure, absent.all())
			}
			if !strings.Contains(absent.all(), "there is no store at") {
				t.Errorf("no store at all: the output does not name the absence:\n%s", absent.all())
			}

			// UNDETERMINED: a store is there and this user cannot read it.
			unreadable := runPublishCmd(t, pubUnreadableStore(t), sub...)
			if unreadable.code != cli.ExitUndetermined {
				t.Errorf("unreadable store: exit %d, want %d (ExitUndetermined)\n%s",
					unreadable.code, cli.ExitUndetermined, unreadable.all())
			}

			// THE CRITERION. Whatever the codes are, these two facts must not share one.
			if absent.code == unreadable.code {
				t.Errorf("an absent store and an unreadable store both exit %d — "+
					"`determined to be nothing` and `could not determine` have collapsed into one "+
					"answer, and no script can tell them apart", absent.code)
			}

			// A store that IS there and IS readable gets past the store entirely: whatever this
			// subcommand then exits, it is not one of the two failures above.
			present := runPublishCmd(t, pubWorld(t), sub...)
			if strings.Contains(present.all(), "there is no store at") ||
				strings.Contains(present.all(), "could not be read") {
				t.Errorf("a present, readable store was reported as absent or unreadable:\n%s", present.all())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The publication gate, at the command (product's ruling, 2026-08-03)
// ---------------------------------------------------------------------------

// pubMode records the person's chosen mode and rules in the store this env names.
func pubMode(t *testing.T, env map[string]string, mode drafts.Mode) {
	t.Helper()
	s, err := store.Open(env[store.PathEnv])
	if err != nil {
		t.Fatal(err)
	}
	if err := drafts.WriteMode(s, mode); err != nil {
		t.Fatal(err)
	}
	if err := drafts.WriteRules(s, "never mention customer names"); err != nil {
		t.Fatal(err)
	}
}

// TestOmwPublishNoteRunsThePersonsGateBeforeReachingTheHub is product's defect, at the surface it
// was found on.
//
// WHAT SHIPPED: `omw outbox publish` ran the review gate and `omw publish note` did not. A draft in
// `review` mode with no model — the exact draft the client calls "NOT checked and will not be
// published" — went to a REAL hub through `omw publish note` with exit 0 and left the outbox. The
// gate lived in the caller and a second caller appeared not knowing about it.
//
// Every arm below drives a real, REACHABLE hub, so nothing but the gate can stop a publication.
func TestOmwPublishNoteRunsThePersonsGateBeforeReachingTheHub(t *testing.T) {
	cases := []struct {
		name     string
		mode     drafts.Mode
		model    bool // is a model configured
		answer   string
		reviewer error
		wantCode int
		wantHub  int
		wantSays string
	}{{
		name: "review with no model is refused and names the mode", mode: drafts.ModeReview,
		wantCode: cli.ExitFailure, wantHub: 0, wantSays: "review",
	}, {
		name: "auto still publishes", mode: drafts.ModeAuto,
		wantCode: cli.Success, wantHub: 1, wantSays: "published",
	}, {
		name: "manual still publishes — running this command IS the person's act", mode: drafts.ModeManual,
		wantCode: cli.Success, wantHub: 1, wantSays: "published",
	}, {
		name: "a checked review draft still publishes", mode: drafts.ModeReview, model: true, answer: "pass",
		wantCode: cli.Success, wantHub: 1, wantSays: "published",
	}, {
		name: "rules that refuse are a determined refusal", mode: drafts.ModeReview, model: true,
		answer: "refuse: this names a customer", wantCode: cli.ExitFailure, wantHub: 0,
		wantSays: "refused by your own publication gate",
	}, {
		name: "a model that cannot be reached is UNDETERMINED and does not publish", mode: drafts.ModeReview,
		model: true, reviewer: errors.New("no route to host"),
		wantCode: cli.ExitUndetermined, wantHub: 0, wantSays: "not established",
	}, {
		name: "an answer that is not a verdict is UNDETERMINED and does not publish", mode: drafts.ModeReview,
		model: true, answer: "I'm afraid I can't help with that",
		wantCode: cli.ExitUndetermined, wantHub: 0, wantSays: "not established",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := pubWorld(t)
			h := pubHub(t, env) // REAL and REACHABLE
			pubMode(t, env, tc.mode)
			if tc.model {
				env[drafts.ModelEnv] = "a-model"
				env[drafts.ModelKeyEnv] = "a-key"
			}
			withReviewer(t, tc.answer, tc.reviewer)
			pubDraft(t, env, "n", "the body of this draft")

			got := runPublishCmd(t, env, "note", "n")

			if got.code != tc.wantCode {
				t.Errorf("exit %d, want %d\n%s", got.code, tc.wantCode, got.all())
			}
			if n := h.Count(); n != tc.wantHub {
				t.Errorf("the hub holds %d note(s), want %d\n%s", n, tc.wantHub, got.all())
			}
			if !strings.Contains(got.all(), tc.wantSays) {
				t.Errorf("the output does not contain %q:\n%s", tc.wantSays, got.all())
			}
		})
	}
}

// TestTheGatesThreeAnswersNeverShareAnExitCode is the project's rule applied to the gate: a draft
// determined not to be publishable, a draft that may publish, and a draft whose publishability could
// not be determined are three answers, and a script must be able to branch on them.
func TestTheGatesThreeAnswersNeverShareAnExitCode(t *testing.T) {
	drive := func(model bool, answer string, rerr error) int {
		env := pubWorld(t)
		pubHub(t, env)
		pubMode(t, env, drafts.ModeReview)
		if model {
			env[drafts.ModelEnv] = "a-model"
			env[drafts.ModelKeyEnv] = "a-key"
		}
		withReviewer(t, answer, rerr)
		pubDraft(t, env, "n", "the body")
		return runPublishCmd(t, env, "note", "n").code
	}
	granted := drive(true, "pass", nil)
	refused := drive(true, "refuse: no", nil)
	undetermined := drive(true, "", errors.New("unreachable"))

	if granted == refused || granted == undetermined || refused == undetermined {
		t.Fatalf("the gate's three answers do not have three exit codes: "+
			"granted=%d refused=%d undetermined=%d", granted, refused, undetermined)
	}
	if undetermined != cli.ExitUndetermined {
		t.Errorf("an undetermined gate exits %d, want %d", undetermined, cli.ExitUndetermined)
	}
}
