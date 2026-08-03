package commands

import (
	"bytes"
	"go/parser"
	"go/token"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Driving the command
// ---------------------------------------------------------------------------

type obResult struct {
	code   int
	stdout string
	stderr string
}

func (r obResult) all() string { return r.stdout + r.stderr }

func runOutboxCmd(t *testing.T, env map[string]string, args ...string) obResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"outbox"}, args...), &out, &errb, func(k string) string { return env[k] })
	return obResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// obWorld is a machine with a store and nothing else: no hub, no daemon, no model.
//
// THAT IS THE DEFAULT ON PURPOSE. PRD §4.4 says the local half stands alone, so the environment a
// test has to go out of its way to build is the one WITH a hub, not the one without.
func obWorld(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	return map[string]string{store.PathEnv: root}
}

// obNoStore is a machine where no store has ever been created.
func obNoStore(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{store.PathEnv: filepath.Join(t.TempDir(), "nothing-here")}
}

func obStorePath(t *testing.T, env map[string]string) string {
	t.Helper()
	p := env[store.PathEnv]
	if p == "" {
		t.Fatal("this test's environment names no store")
	}
	return p
}

func mustRun(t *testing.T, env map[string]string, args ...string) obResult {
	t.Helper()
	got := runOutboxCmd(t, env, args...)
	if got.code != cli.Success {
		t.Fatalf("omw outbox %s exited %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), got.code, got.stdout, got.stderr)
	}
	return got
}

// withReviewer swaps the model this build reaches for, and restores it.
func withReviewer(t *testing.T, answer string, err error) {
	t.Helper()
	prev := outboxReviewer
	outboxReviewer = func(cli.Env, drafts.ModelConfig) drafts.Reviewer {
		return scriptedReviewer{answer: answer, err: err}
	}
	t.Cleanup(func() { outboxReviewer = prev })
}

type scriptedReviewer struct {
	answer string
	err    error
}

func (r scriptedReviewer) Review(string, string) (string, error) { return r.answer, r.err }

// obModel adds a configured model, with a key nothing may ever print.
const obSecret = "sk-ZQXJ-the-persons-key-9f31"

func obWithModel(env map[string]string) map[string]string {
	env[drafts.ModelEnv] = "local-llama"
	env[drafts.ModelKeyEnv] = obSecret
	return env
}

// ---------------------------------------------------------------------------
// Criteria 1, 2, 3, 24 — drafting and the outbox
// ---------------------------------------------------------------------------

func TestADraftIsCreatedListedAndSaidToBeADraft(t *testing.T) {
	env := obWorld(t)
	got := mustRun(t, env, "draft", "note-a", "the quota is four hundred")
	if !strings.Contains(got.stdout, "note-a") {
		t.Errorf("creating a draft does not name it:\n%s", got.stdout)
	}
	list := mustRun(t, env, "list")
	if !strings.Contains(list.stdout, "note-a") {
		t.Errorf("the draft is not listed in the outbox:\n%s", list.stdout)
	}
	// A DRAFT IS SAID TO BE ONE. Criterion 1 asks for a state distinguishable from a note that has
	// left the outbox, and `omw outbox state` is where a person reads it.
	st := mustRun(t, env, "state", "note-a")
	if !strings.Contains(st.stdout, string(drafts.StateDrafted)) {
		t.Errorf("the draft's state does not say it is a draft:\n%s", st.stdout)
	}
}

// CRITERION 3. No store, so no draft — and NOT a draft written somewhere convenient.
func TestDraftingWithNoStoreNamesTheMissingStoreAndWritesNothing(t *testing.T) {
	// THE PLACES A FALLBACK WOULD LAND ARE REDIRECTED INTO ONE SANDBOX, so that the search below is
	// over a directory this test can walk in a moment rather than the whole machine's temporary
	// directory. TMPDIR is the one that matters: os.TempDir reads it, so a "just put it somewhere"
	// fallback lands inside the sandbox and is caught.
	sandbox := t.TempDir()
	for _, k := range []string{"TMPDIR", "HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, sandbox)
	}
	env := obNoStore(t)
	got := runOutboxCmd(t, env, "draft", "note-a", "something I would hate to lose")
	if got.code == cli.Success {
		t.Fatalf("drafting with no store exited 0:\n%s", got.all())
	}
	if !strings.Contains(got.stderr, "store") {
		t.Errorf("the failure does not name the missing store:\n%s", got.stderr)
	}
	// AND NOTHING WAS WRITTEN ANYWHERE THIS TEST CAN SEE. The phrase is distinctive, and the search
	// covers the sandbox the store would have been in and the process's temporary directory, which
	// is where a "just put it somewhere" fallback would land.
	for _, root := range []string{filepath.Dir(obStorePath(t, env)), sandbox} {
		if hit := grepFor(t, root, "something I would hate to lose"); hit != "" {
			t.Errorf("the draft was written to %s even though there is no store", hit)
		}
	}
	// A CONTROL. A search that cannot find a phrase that IS there proves nothing when it finds
	// nothing, and a walk that silently fails is indistinguishable from a clean result.
	control := filepath.Join(sandbox, "control")
	if err := os.WriteFile(control, []byte("something I would hate to lose"), 0o600); err != nil {
		t.Fatal(err)
	}
	if grepFor(t, sandbox, "something I would hate to lose") != control {
		t.Fatal("the search cannot find a phrase that is definitely there, so its empty result above says nothing")
	}
}

func grepFor(t *testing.T, root, phrase string) string {
	t.Helper()
	found := ""
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr == nil && bytes.Contains(b, []byte(phrase)) {
			found = p
		}
		return nil
	})
	return found
}

// CRITERION 4: an empty outbox and an outbox that could not be read are different answers with
// different exit codes.
func TestAnEmptyOutboxAndAnUnreadableOneAreDifferentAnswers(t *testing.T) {
	env := obWorld(t)
	empty := mustRun(t, env, "list")
	if !strings.Contains(empty.stdout, "drafts: 0") {
		t.Errorf("an empty outbox does not say so:\n%s", empty.stdout)
	}

	// PROBED, NOT NAMED: make the outbox directory unreadable and check that this environment
	// really cannot read it before asserting on what the command says about that.
	mustRun(t, env, "draft", "note-a", "hello")
	outboxDir := filepath.Join(obStorePath(t, env), drafts.OutboxDirName)
	if err := os.Chmod(outboxDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(outboxDir, 0o700) })
	if _, err := os.ReadDir(outboxDir); err == nil {
		t.Skip("this environment can read a 0o000 directory, so an unreadable outbox cannot be produced here")
	}

	unreadable := runOutboxCmd(t, env, "list")
	if unreadable.code == empty.code {
		t.Errorf("an unreadable outbox and an empty one share exit code %d", unreadable.code)
	}
	if unreadable.stdout == empty.stdout {
		t.Errorf("an unreadable outbox and an empty one print the same thing:\n%s", empty.stdout)
	}
	if strings.Contains(unreadable.all(), "drafts: 0") {
		t.Errorf("an unreadable outbox reports zero drafts:\n%s", unreadable.all())
	}
}

// CRITERION 24: nothing expires. A draft left untouched, with a write time long in the past, is
// still listed — and this build performs no age-based pass at all.
func TestAnOldDraftIsStillInTheOutbox(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "draft", "note-old", "written a long time ago")
	old := time.Now().Add(-5 * 365 * 24 * time.Hour)
	dir := filepath.Join(obStorePath(t, env), drafts.OutboxDirName, "note-old")
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			os.Chtimes(p, old, old)
		}
		return nil
	})
	for i := 0; i < 3; i++ {
		if got := mustRun(t, env, "list"); !strings.Contains(got.stdout, "note-old") {
			t.Fatalf("a five-year-old draft is gone after %d listings:\n%s", i+1, got.stdout)
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 5, 6, 7, 10 — the mode is the person's choice
// ---------------------------------------------------------------------------

func TestTheDefaultModeIsReportedAsManual(t *testing.T) {
	env := obWorld(t)
	got := mustRun(t, env, "mode")
	if !strings.Contains(got.stdout, string(drafts.ModeManual)) {
		t.Errorf("the default mode is not reported as a real value:\n%q", got.stdout)
	}
	line := strings.TrimSpace(strings.SplitN(got.stdout, "\n", 2)[0])
	if line == "mode:" || line == "" {
		t.Errorf("the default mode renders blank or absent: %q", line)
	}
}

func TestEachModeCanBeSetAndIsReportedBackByTheClient(t *testing.T) {
	env := obWorld(t)
	for _, m := range drafts.Modes() {
		set := mustRun(t, env, "mode", "set", string(m))
		if !strings.Contains(set.stdout, string(m)) {
			t.Errorf("setting %q does not report it back:\n%s", m, set.stdout)
		}
		read := mustRun(t, env, "mode")
		if !strings.Contains(read.stdout, string(m)) {
			t.Errorf("after setting %q, the mode reads back as:\n%s", m, read.stdout)
		}
	}
}

// CRITERION 7.
func TestAnUnknownModeIsRefusedAndLeavesTheEffectiveModeAlone(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "mode", "set", "review")
	for _, bad := range []string{"Review", "rev", "publish", "off"} {
		got := runOutboxCmd(t, env, "mode", "set", bad)
		if got.code == cli.Success {
			t.Errorf("mode set %q exited 0", bad)
		}
		back := mustRun(t, env, "mode")
		if !strings.Contains(back.stdout, "review") {
			t.Fatalf("after refusing %q the mode reads:\n%s", bad, back.stdout)
		}
	}
}

// CRITERION 10: changing the mode does not act on drafts already resting.
func TestChangingTheModeDoesNotActOnDraftsAlreadyInTheOutbox(t *testing.T) {
	env := obWorld(t)
	for _, id := range []string{"a", "b", "c"} {
		mustRun(t, env, "draft", id, "resting")
	}
	before := countDrafts(t, env)
	env[outboxEnvHub] = "https://hub.example"
	mustRun(t, env, "mode", "set", "auto")
	after := countDrafts(t, env)
	if before != after {
		t.Errorf("the outbox held %d drafts before the mode change and %d after", before, after)
	}
	for _, id := range []string{"a", "b", "c"} {
		st := mustRun(t, env, "state", id)
		if !strings.Contains(st.stdout, string(drafts.StateDrafted)) {
			t.Errorf("draft %q was acted on by the mode change:\n%s", id, st.stdout)
		}
	}
}

func countDrafts(t *testing.T, env map[string]string) int {
	t.Helper()
	got := mustRun(t, env, "list")
	for _, line := range strings.Split(got.stdout, "\n") {
		if strings.HasPrefix(line, "drafts: ") {
			n, err := strconv.Atoi(strings.Fields(strings.TrimPrefix(line, "drafts: "))[0])
			if err != nil {
				t.Fatalf("cannot read the draft count from %q", line)
			}
			return n
		}
	}
	t.Fatalf("the listing has no count line:\n%s", got.stdout)
	return -1
}

// ---------------------------------------------------------------------------
// Criteria 8, 9 — what each mode does when a draft is written
// ---------------------------------------------------------------------------

// CRITERION 8: manual publishes nothing and opens no connection.
func TestInManualModeADraftRestsAndNothingIsDialled(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "mode", "set", "manual")
	dials := watchForDials(t, env)
	got := mustRun(t, env, "draft", "note-a", "resting here")
	if !strings.Contains(got.stdout, string(drafts.StateDrafted)) {
		t.Errorf("a manual draft does not rest in the outbox:\n%s", got.stdout)
	}
	if n := dials(); n != 0 {
		t.Errorf("writing a draft in manual mode opened %d connection(s) to the configured hub", n)
	}
}

// CRITERION 9: auto has an effect — the draft leaves the resting state.
func TestInAutoModeWithAHubADraftDoesNotRemainInTheManualRestingState(t *testing.T) {
	env := obWorld(t)
	env[outboxEnvHub] = "https://hub.example"
	mustRun(t, env, "mode", "set", "auto")
	got := runOutboxCmd(t, env, "draft", "note-a", "straight out")
	st := runOutboxCmd(t, env, "state", "note-a")
	if strings.Contains(st.stdout, string(drafts.StateDrafted)) {
		t.Errorf("in auto mode the draft is still in the manual resting state:\n%s", st.stdout)
	}
	if !strings.Contains(st.stdout, string(drafts.StateLeaving)) {
		t.Errorf("in auto mode the draft's state did not move:\n%s\n(draft command said:\n%s)", st.stdout, got.all())
	}
}

// Criterion 22's other half: auto genuinely needs a hub, so with none it says precisely that and
// exits non-zero rather than half-working.
func TestAutoWithNoHubSaysWhatIsMissingAndExitsNonZero(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "mode", "set", "auto")
	got := runOutboxCmd(t, env, "draft", "note-a", "nowhere to go")
	if got.code == cli.Success {
		t.Fatalf("auto with no hub exited 0:\n%s", got.all())
	}
	if !strings.Contains(strings.ToLower(got.stderr), "hub") {
		t.Errorf("the failure does not name the missing hub:\n%s", got.stderr)
	}
	if !strings.Contains(mustRun(t, env, "list").stdout, "note-a") {
		t.Error("the draft is not in the outbox after auto could not send it")
	}
}

// ---------------------------------------------------------------------------
// Criteria 11, 12 — review runs here, on the person's words
// ---------------------------------------------------------------------------

// The wording is chosen to break a naive normaliser: leading spaces, a blank line, a tab, CRLF,
// mixed case and trailing spaces. The CRLF is here because a mutation that normalised line endings
// on read-back only was caught in internal/drafts and sailed through this package.
const cliAwkwardRules = "  NEVER mention customer names.\n\r\n\tno half-finished reasoning — Acme is a customer, acme is a package.  "

func TestTheRulesAreReadBackExactlyAsRecorded(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "rules", "set", cliAwkwardRules)
	got := mustRun(t, env, "rules")
	// STDOUT IS THE PERSON'S BYTES. A trailing newline is added when their text lacks one, because
	// a shell needs it; nothing else may differ.
	if strings.TrimSuffix(got.stdout, "\n") != strings.TrimSuffix(cliAwkwardRules, "\n") {
		t.Errorf("the rules came back changed.\n  recorded: %q\n  read back: %q", cliAwkwardRules, got.stdout)
	}
}

// CRITERION 12: the check runs with no hub anywhere near it.
func TestReviewRunsWithNoHubConfigured(t *testing.T) {
	env := obWithModel(obWorld(t))
	if env[outboxEnvHub] != "" {
		t.Fatal("this test means to drive a machine with no hub")
	}
	mustRun(t, env, "mode", "set", "review")
	mustRun(t, env, "rules", "set", "no customer names")
	mustRun(t, env, "draft", "note-a", "a fine draft")
	withReviewer(t, "pass", nil)
	got := runOutboxCmd(t, env, "review", "note-a")
	if got.code != cli.Success {
		t.Fatalf("review with no hub exited %d:\n%s", got.code, got.all())
	}
	if !strings.Contains(got.stdout, "passed") {
		t.Errorf("the check did not run with no hub configured:\n%s", got.stdout)
	}
}

// ---------------------------------------------------------------------------
// Criteria 13, 14, 15 — review with no model
// ---------------------------------------------------------------------------

func reviewWorldWithoutModel(t *testing.T) map[string]string {
	t.Helper()
	env := obWorld(t)
	mustRun(t, env, "mode", "set", "review")
	mustRun(t, env, "rules", "set", "no customer names")
	return env
}

// CRITERION 13.
func TestReviewWithNoModelNamesTheMissingModelAndExitsNonZero(t *testing.T) {
	env := reviewWorldWithoutModel(t)
	runOutboxCmd(t, env, "draft", "note-a", "a draft")
	got := runOutboxCmd(t, env, "publish", "note-a")
	if got.code == cli.Success {
		t.Fatalf("publishing under review with no model exited 0:\n%s", got.all())
	}
	if !strings.Contains(strings.ToLower(got.all()), "model") {
		t.Errorf("the output does not name the missing model configuration:\n%s", got.all())
	}
}

// CRITERION 14 — it must not behave like manual. The comparison is against the manual-mode output
// for the same draft, not against a string literal: what matters is that a person can TELL THEM
// APART, and only a comparison can show that.
func TestReviewWithNoModelIsDistinguishableFromADraftSimplyAwaitingThePerson(t *testing.T) {
	manualEnv := obWorld(t)
	mustRun(t, manualEnv, "mode", "set", "manual")
	mustRun(t, manualEnv, "draft", "note-a", "a draft")
	manualDraft := runOutboxCmd(t, manualEnv, "draft", "note-a", "a draft")
	manualState := runOutboxCmd(t, manualEnv, "state", "note-a")

	reviewEnv := reviewWorldWithoutModel(t)
	reviewDraft := runOutboxCmd(t, reviewEnv, "draft", "note-a", "a draft")
	reviewState := runOutboxCmd(t, reviewEnv, "state", "note-a")

	if reviewDraft.code == manualDraft.code {
		t.Errorf("drafting under review-with-no-model and under manual share exit code %d", manualDraft.code)
	}
	if reviewDraft.all() == manualDraft.all() {
		t.Errorf("drafting under review-with-no-model says exactly what manual says:\n%s", manualDraft.all())
	}
	if reviewState.stdout == manualState.stdout {
		t.Errorf("the draft's state under review-with-no-model reads identically to a resting manual draft:\n%s", manualState.stdout)
	}
	if strings.Contains(reviewState.stdout, string(drafts.StateDrafted)) {
		t.Errorf("a draft blocked on a missing model reports as merely drafted:\n%s", reviewState.stdout)
	}
	// SILENCE IS THE FAILURE. The review-mode path must name a prerequisite; the manual one must
	// not, because for a manual draft nothing is missing.
	if !mentionsAMissingPrerequisite(reviewDraft.all()) {
		t.Errorf("review-with-no-model names no missing prerequisite:\n%s", reviewDraft.all())
	}
	if mentionsAMissingPrerequisite(manualDraft.all()) {
		t.Errorf("a manual draft names a missing prerequisite, so the two cannot be told apart by it:\n%s", manualDraft.all())
	}
}

func mentionsAMissingPrerequisite(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "no model is configured") || strings.Contains(low, "missing")
}

// CRITERION 15 — and it must not behave like auto either.
func TestReviewWithNoModelPublishesNothing(t *testing.T) {
	env := reviewWorldWithoutModel(t)
	env[outboxEnvHub] = "https://hub.example" // a hub IS configured; still nothing goes out.
	runOutboxCmd(t, env, "draft", "note-a", "a draft")
	dials := watchForDials(t, env)
	got := runOutboxCmd(t, env, "publish", "note-a")
	if got.code == cli.Success {
		t.Fatalf("publish exited 0 with no model under review:\n%s", got.all())
	}
	if n := dials(); n != 0 {
		t.Errorf("publish opened %d connection(s) while the review could not be performed", n)
	}
	if !strings.Contains(mustRun(t, env, "list").stdout, "note-a") {
		t.Error("the draft has left the outbox")
	}
	st := runOutboxCmd(t, env, "state", "note-a")
	if strings.Contains(st.stdout, string(drafts.StateLeaving)) {
		t.Errorf("the draft was handed onward despite the review not running:\n%s", st.stdout)
	}
	// THE GATE ITSELF MUST HAVE HELD, not merely the transfer that does not exist yet. Asserting
	// only on the draft's state lets a build through in which the gate passed the draft and the
	// missing transport is the only thing that stopped it — which is a publication the day #10
	// lands. Found by mutating the gate to let a model-less review through WITHOUT recording a
	// state, and watching this test stay green.
	if !strings.Contains(got.stdout, "published: no") {
		t.Errorf("publish does not state that the draft was not published:\n%s", got.stdout)
	}
	if strings.Contains(got.stdout, "and passed") {
		t.Errorf("publish reports a pass with no model configured:\n%s", got.stdout)
	}
}

// ---------------------------------------------------------------------------
// Criteria 16, 17 — completed, refused, and not completed
// ---------------------------------------------------------------------------

func TestTheThreeReviewOutcomesAreDistinguishableInOutputAndInState(t *testing.T) {
	type run struct {
		out   string
		state string
		code  int
	}
	do := func(answer string, err error) run {
		env := obWithModel(reviewWorldWithoutModel(t))
		mustRun(t, env, "draft", "note-a", "a draft")
		withReviewer(t, answer, err)
		got := runOutboxCmd(t, env, "review", "note-a")
		return run{out: got.stdout, state: runOutboxCmd(t, env, "state", "note-a").stdout, code: got.code}
	}
	passed := do("pass", nil)
	refused := do("refuse: you named a customer", nil)
	unreachable := do("", os.ErrDeadlineExceeded)
	unusable := do("I am not sure, really", nil)
	// EACH KIND OF UNUSABLE ANSWER IS DRIVEN SEPARATELY. Driving only the rambling one let a
	// half-fix — "an empty answer passes, everything else is undetermined" — through this package
	// entirely; it was caught in internal/drafts and nowhere else, which is one test away from not
	// being caught at all.
	empty := do("", nil)
	whitespace := do("   \n\t", nil)

	outs := map[string]string{"passed": passed.out, "refused": refused.out, "unreachable": unreachable.out}
	seen := map[string]string{}
	for name, s := range outs {
		if other, dup := seen[s]; dup {
			t.Errorf("the %q and %q outcomes print the same thing:\n%s", other, name, s)
		}
		seen[s] = name
	}
	states := map[string]string{"passed": passed.state, "refused": refused.state, "unreachable": unreachable.state}
	seen = map[string]string{}
	for name, s := range states {
		if other, dup := seen[s]; dup {
			t.Errorf("the %q and %q outcomes leave the draft in the same reported state:\n%s", other, name, s)
		}
		seen[s] = name
	}

	// CRITERION 16: an incomplete review is not a pass, and it has its own exit code.
	for name, r := range map[string]run{"unreachable": unreachable, "unusable": unusable, "returned nothing": empty, "returned whitespace": whitespace} {
		if r.code == passed.code {
			t.Errorf("a review that %s shares an exit code with a pass (%d)", name, r.code)
		}
		if r.code != cli.ExitUndetermined {
			t.Errorf("a review that %s exits %d, want %d", name, r.code, cli.ExitUndetermined)
		}
		if strings.Contains(r.out, "and passed") {
			t.Errorf("a review that %s reports as a pass:\n%s", name, r.out)
		}
	}
	// `could not determine` and `determined to be nothing` never share an exit code.
	if unreachable.code == refused.code {
		t.Errorf("a review that could not be completed and a refusal share exit code %d", refused.code)
	}

	// CRITERION 17: a refused draft is still in the outbox and says it was refused.
	if !strings.Contains(refused.state, string(drafts.StateRefused)) {
		t.Errorf("a refused draft does not report as refused:\n%s", refused.state)
	}
	if !strings.Contains(refused.out, "customer") {
		t.Errorf("the refusal loses the reason:\n%s", refused.out)
	}
}

func TestARefusedDraftReadsDifferentlyFromOneNeverReviewed(t *testing.T) {
	env := obWithModel(reviewWorldWithoutModel(t))
	mustRun(t, env, "draft", "note-a", "a draft")
	never := runOutboxCmd(t, env, "state", "note-a").stdout
	withReviewer(t, "refuse: no", nil)
	runOutboxCmd(t, env, "review", "note-a")
	refused := runOutboxCmd(t, env, "state", "note-a").stdout
	if never == refused {
		t.Errorf("a refused draft and a never-reviewed one read identically:\n%s", never)
	}
	if !strings.Contains(mustRun(t, env, "list").stdout, "note-a") {
		t.Error("a refused draft has left the outbox")
	}
}

// ---------------------------------------------------------------------------
// Criterion 18 — the key
// ---------------------------------------------------------------------------

func TestThePersonsKeyAppearsInNoOutputOfThisCapability(t *testing.T) {
	env := obWithModel(obWorld(t))
	env[outboxEnvHub] = "https://hub.example"
	mustRun(t, env, "mode", "set", "review")
	mustRun(t, env, "rules", "set", "no customer names")
	mustRun(t, env, "draft", "note-a", "a draft")

	invocations := [][]string{
		{}, {"help"}, {"draft", "note-a", "more text"}, {"list"}, {"state", "note-a"},
		{"mode"}, {"mode", "set", "review"}, {"mode", "set", "nonsense"},
		{"rules"}, {"rules", "set", "no customer names"}, {"model"},
		{"review", "note-a"}, {"publish", "note-a"}, {"nonsense"},
	}
	for _, answer := range []struct {
		text string
		err  error
	}{{"pass", nil}, {"refuse: you named a customer", nil}, {"", os.ErrDeadlineExceeded}} {
		withReviewer(t, answer.text, answer.err)
		for _, args := range invocations {
			got := runOutboxCmd(t, env, args...)
			if strings.Contains(got.stdout, obSecret) {
				t.Errorf("omw outbox %s printed the key on stdout", strings.Join(args, " "))
			}
			if strings.Contains(got.stderr, obSecret) {
				t.Errorf("omw outbox %s printed the key on stderr", strings.Join(args, " "))
			}
		}
	}
	// A CONTROL: the key really is configured, so the sweep above was looking for something that
	// could have appeared.
	if drafts.ReadModel(func(k string) string { return env[k] }).Key() != obSecret {
		t.Fatal("the key was not configured, so this sweep proves nothing")
	}
}

// ---------------------------------------------------------------------------
// Criterion 19 — three answers, compared with each other
// ---------------------------------------------------------------------------

func TestTheModeHasThreeDistinctRenderingsThroughTheCommand(t *testing.T) {
	def := mustRun(t, obWorld(t), "mode").stdout

	setEnv := obWorld(t)
	mustRun(t, setEnv, "mode", "set", "auto")
	real := mustRun(t, setEnv, "mode").stdout

	damagedEnv := obWorld(t)
	mustRun(t, damagedEnv, "mode", "set", "auto")
	damageRecord(t, obStorePath(t, damagedEnv), "publication-mode")
	undet := runOutboxCmd(t, damagedEnv, "mode")

	if undet.code == cli.Success {
		t.Errorf("an unreadable mode exits 0:\n%s", undet.all())
	}
	assertThreeDistinct(t, "the publication mode", map[string]string{
		"a mode the person set":   real,
		"no mode ever set":        def,
		"could not be determined": undet.stdout,
	})
}

func TestWhetherAModelIsConfiguredHasThreeDistinctRenderingsThroughTheCommand(t *testing.T) {
	yes := mustRun(t, obWithModel(obWorld(t)), "model").stdout
	no := mustRun(t, obWorld(t), "model").stdout

	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(obSecret), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(keyFile); err == nil {
		t.Skip("this environment can read a 0o000 file, so an unreadable key file cannot be produced here")
	}
	env := obWorld(t)
	env[drafts.ModelEnv] = "local-llama"
	env[drafts.ModelKeyFileEnv] = keyFile
	undet := runOutboxCmd(t, env, "model")
	if undet.code != cli.ExitUndetermined {
		t.Errorf("an unreadable key file exits %d, want %d:\n%s", undet.code, cli.ExitUndetermined, undet.all())
	}
	assertThreeDistinct(t, "whether a model is configured", map[string]string{
		"configured":              yes,
		"none configured":         no,
		"could not be determined": undet.stdout,
	})
}

func TestADraftsStateHasThreeDistinctRenderingsThroughTheCommand(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "draft", "note-a", "a draft")
	real := mustRun(t, env, "state", "note-a").stdout
	absent := runOutboxCmd(t, env, "state", "note-nope")
	if absent.code == cli.Success {
		t.Errorf("asking about a draft that is not there exits 0:\n%s", absent.all())
	}

	statePath := filepath.Join(obStorePath(t, env), drafts.OutboxDirName, "note-a", ".state")
	if err := os.WriteFile(statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	undet := runOutboxCmd(t, env, "state", "note-a")
	if undet.code != cli.ExitUndetermined {
		t.Errorf("an unreadable draft state exits %d, want %d", undet.code, cli.ExitUndetermined)
	}
	if undet.code == absent.code {
		t.Errorf("'no such draft' and 'could not be determined' share exit code %d", undet.code)
	}
	assertThreeDistinct(t, "a draft's state", map[string]string{
		"drafted":                 real,
		"no such draft":           absent.stdout,
		"could not be determined": undet.stdout,
	})
}

// assertThreeDistinct compares renderings PAIRWISE. See the same argument in the drafts package:
// three assertions against literals go green after two of the three are edited to match.
func assertThreeDistinct(t *testing.T, what string, renderings map[string]string) {
	t.Helper()
	seen := map[string]string{}
	for name, s := range renderings {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s: the %q rendering is blank", what, name)
		}
		if other, dup := seen[s]; dup {
			t.Errorf("%s: the %q and %q renderings are identical:\n%s", what, other, name, s)
		}
		seen[s] = name
	}
}

func damageRecord(t *testing.T, storeRoot, id string) {
	t.Helper()
	p := filepath.Join(storeRoot, "records", "outbox", id+".rec")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the record this test means to damage is not at %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Criteria 20, 21, 22, 23 — the universal ones
// ---------------------------------------------------------------------------

// everySubcommand is the list the universal criteria are asserted across. It is here rather than
// inline so that a subcommand added later without being added here is a visible omission.
func everySubcommand() [][]string {
	return [][]string{
		{"draft", "note-a", "some text"}, {"list"}, {"state", "note-a"},
		{"mode"}, {"mode", "set", "manual"}, {"rules"}, {"rules", "set", "my rules"},
		{"model"}, {"review", "note-a"}, {"publish", "note-a"},
	}
}

// CRITERION 20: no command starts the daemon, each says it is not running, and it is still not
// running afterwards.
func TestNoCommandStartsTheDaemonAndEachSaysItIsNotRunning(t *testing.T) {
	env := obWorld(t)
	root := obStorePath(t, env)
	// ASKED THROUGH THE DAEMON'S OWN INSPECTION, not by looking for a socket path this package has
	// no business deriving (Issue #41). Before, after, and for every subcommand in between.
	if rep := daemon.Inspect(root); rep.Running != tri.No {
		t.Fatalf("a freshly created store reports Running = %v; this test cannot tell whether a command started anything", rep.Running)
	}
	for _, args := range everySubcommand() {
		got := runOutboxCmd(t, env, args...)
		if !strings.Contains(got.stderr, "daemon: not running") {
			t.Errorf("omw outbox %s does not say the daemon is not running:\n%s", strings.Join(args, " "), got.all())
		}
		if rep := daemon.Inspect(root); rep.Running != tri.No {
			t.Fatalf("after omw outbox %s the daemon reports Running = %v — something was started", strings.Join(args, " "), rep.Running)
		}
	}
}

// The daemon's state is Issue #2's three-valued answer, and this capability passes all three
// through. "Not running" and "I could not tell" are different sentences here for the same reason
// they are different values there.
func TestTheDaemonsThreeStatesAreSaidDistinctly(t *testing.T) {
	env := obWorld(t)
	said := map[string]string{}
	for name, v := range map[string]tri.Value{"running": tri.Yes, "not running": tri.No, "could not be determined": tri.Undetermined} {
		prev := daemonLiveness
		daemonLiveness = func(cli.Env) (tri.Value, string) { return v, "the lock could not be opened" }
		got := runOutboxCmd(t, env, "list")
		daemonLiveness = prev
		line := ""
		for _, l := range strings.Split(got.stderr, "\n") {
			if strings.HasPrefix(l, "daemon:") {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("with the daemon %s, no daemon line was printed:\n%s", name, got.stderr)
		}
		said[name] = line
	}
	assertThreeDistinct(t, "the daemon's state", said)
	if strings.Contains(said["could not be determined"], "not running —") {
		t.Errorf("an undetermined daemon is reported as not running: %q", said["could not be determined"])
	}
}

// CRITERION 21 and 8, at runtime: with a hub address that is a REAL LISTENER this test controls,
// nothing in this capability connects to it.
func watchForDials(t *testing.T, env map[string]string) func() int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("this environment cannot open a loopback listener to watch for dials: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	count := make(chan int, 1)
	count <- 0
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			n := <-count
			count <- n + 1
			c.Close()
		}
	}()
	// The hub is CONFIGURED and points at the listener, which is the strong form: the command knows
	// where a hub is and still must not dial it for local work.
	env[outboxEnvHub] = "http://" + l.Addr().String()
	return func() int {
		time.Sleep(20 * time.Millisecond) // give a stray dial time to land
		n := <-count
		count <- n
		return n
	}
}

func TestTheLocalHalfOpensNoConnectionEvenWithAHubConfigured(t *testing.T) {
	env := obWorld(t)
	dials := watchForDials(t, env)
	for _, args := range [][]string{{"draft", "note-a", "text"}, {"list"}, {"mode"}, {"mode", "set", "manual"}, {"rules"}, {"rules", "set", "mine"}, {"state", "note-a"}} {
		runOutboxCmd(t, env, args...)
	}
	if n := dials(); n != 0 {
		t.Errorf("the local half opened %d connection(s) to the configured hub", n)
	}
}

// CRITERION 21, the structural half: this capability's own files reach for no network package.
//
// STATED PRECISELY, because it is easy to overclaim. It covers the files this Issue wrote — the
// command and package drafts — and not their transitive imports: `daemon.Inspect` is called from
// the preflight and package daemon does use `net`, for a UNIX DOMAIN SOCKET on this machine.
// Whole-repository coverage of that is `TestEveryListenAndDialIsAUnixSocket` above, which requires
// every listen and dial under internal/ to name "unix" as a literal. This check is the narrower
// one: the capability itself has no business importing net at all, and if it ever does, the reason
// should have to be argued in a diff.
func TestThisCapabilityImportsNoNetworkPackage(t *testing.T) {
	files := []string{"outbox_cmd.go"}
	matches, err := filepath.Glob(filepath.Join("..", "drafts", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if !strings.HasSuffix(m, "_test.go") {
			files = append(files, m)
		}
	}
	if len(files) < 5 {
		t.Fatalf("this check found only %d files to examine; it is not looking where the capability lives", len(files))
	}
	fset := token.NewFileSet()
	for _, f := range files {
		parsed, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == "net" || strings.HasPrefix(path, "net/") {
				t.Errorf("%s imports %q; with no hub configured this capability must be unable to open a connection at all", f, path)
			}
		}
	}
}

// CRITERION 22: the local half works fully with no hub, and says nothing about a missing one.
func TestWithNoHubTheLocalHalfWorksFullyAndDoesNotMentionAMissingHub(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "mode", "set", "manual")
	for _, args := range [][]string{{"draft", "note-a", "text"}, {"draft", "note-b", "more"}, {"list"}, {"state", "note-a"}, {"mode"}, {"rules", "set", "mine"}, {"rules"}} {
		got := mustRun(t, env, args...)
		// THE STORE'S PATH IS REMOVED BEFORE THE SEARCH. A t.TempDir() is built from the test's own
		// name, so a test called …NoHub… puts the word "hub" into every path the command prints,
		// and the check would fail on its own name rather than on the product's wording. Caught by
		// this test failing before the product had said anything about a hub at all.
		said := strings.ToLower(strings.ReplaceAll(got.all(), obStorePath(t, env), "<store>"))
		if strings.Contains(said, "hub") {
			t.Errorf("omw outbox %s warns about a hub while doing purely local work:\n%s", strings.Join(args, " "), got.all())
		}
	}
	if n := countDrafts(t, env); n != 2 {
		t.Errorf("the outbox holds %d drafts, want 2", n)
	}
}

// CRITERION 23, §4.6: where the control API is not open on a socket only its owner can reach, or
// where that could not be confirmed, the commands say so and stop.
//
// WHAT THIS TEST DOES AND DOES NOT PROVE, stated rather than glossed. The daemon is the thing that
// proves its socket is owner-only and refuses to open otherwise, and `internal/daemon` owns the
// tests for that. What this drives is what THIS capability does with each of the daemon's three
// answers — which is the part Issue #9 is responsible for. Deriving the socket path out here to
// look at it myself is exactly what Issue #41 forbids, and it would be wrong on the runtime-
// directory fallback path.
func TestCommandsStopWhenTheControlAPIIsNotOpenOrCannotBeConfirmed(t *testing.T) {
	env := obWorld(t)
	withDaemonSaying := func(state tri.Value, detail string) func() {
		prevLive, prevCtl := daemonLiveness, outboxControlState
		daemonLiveness = func(cli.Env) (tri.Value, string) { return tri.Yes, "" }
		outboxControlState = func(cli.Env) (tri.Value, string) { return state, detail }
		return func() { daemonLiveness, outboxControlState = prevLive, prevCtl }
	}

	cases := map[string]struct {
		state tri.Value
		want  int
	}{
		"the control API is not open": {tri.No, cli.ExitFailure},
		"it could not be established": {tri.Undetermined, cli.ExitUndetermined},
	}
	said := map[string]string{}
	for name, c := range cases {
		restore := withDaemonSaying(c.state, "the socket's permissions could not be confirmed as owner-only")
		for _, args := range everySubcommand() {
			got := runOutboxCmd(t, env, args...)
			if got.code == cli.Success {
				t.Errorf("omw outbox %s proceeded when %s:\n%s", strings.Join(args, " "), name, got.all())
			}
			if got.code != c.want {
				t.Errorf("omw outbox %s exits %d when %s, want %d", strings.Join(args, " "), got.code, name, c.want)
			}
			if !strings.Contains(got.stderr, "control API") {
				t.Errorf("omw outbox %s does not say what is wrong when %s:\n%s", strings.Join(args, " "), name, got.stderr)
			}
			if args[0] == "list" {
				said[name] = got.stderr
			}
		}
		restore()
	}
	// A DETERMINED "not open" AND "could not be confirmed" ARE NOT THE SAME SENTENCE, and they do
	// not share an exit code — asserted above by value and here by comparing the two renderings.
	assertThreeDistinct(t, "the control API's state", said)

	// AND THE CONTROL: with the daemon reporting an open control API, the same commands work.
	// Without this, a build that refused everything always would pass the assertions above.
	restore := withDaemonSaying(tri.Yes, "")
	defer restore()
	if got := runOutboxCmd(t, env, "list"); got.code != cli.Success {
		t.Errorf("with the control API open, list exits %d:\n%s", got.code, got.all())
	}
}

// A daemon that is NOT running has no control API, and there is nothing to confirm. Purely local
// drafting must not be blocked by the absence of a thing it does not use.
func TestWithNoDaemonRunningTheLocalHalfIsNotBlockedByTheControlAPI(t *testing.T) {
	env := obWorld(t)
	prev := outboxControlState
	outboxControlState = func(cli.Env) (tri.Value, string) {
		t.Error("the control API's state was consulted with no daemon running")
		return tri.Undetermined, ""
	}
	t.Cleanup(func() { outboxControlState = prev })
	mustRun(t, env, "draft", "note-a", "local work")
	mustRun(t, env, "list")
}

// ---------------------------------------------------------------------------
// Criterion 2 — the drafts are on disk, and survive the process
// ---------------------------------------------------------------------------

func TestADraftSurvivesTheProcessThatWroteIt(t *testing.T) {
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
	run := func(args ...string) (int, string, *os.ProcessState) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// THE DEVICE POINTER IS SANDBOXED. Inheriting the developer's environment repoints their
		// real store at a t.TempDir() that is then deleted; both variables are set because the
		// pointer resolves from XDG_DATA_HOME and falls back to HOME.
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=", "OMW_MODEL=", "OMW_MODEL_KEY=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out), cmd.ProcessState
	}

	if code, out, _ := run("store", "create"); code != 0 {
		t.Fatalf("omw store create exited %d:\n%s", code, out)
	}
	code, out, state := run("outbox", "draft", "note-a", "worth keeping")
	if code != 0 {
		t.Fatalf("omw outbox draft exited %d:\n%s", code, out)
	}
	// Nothing was left running (criterion 20 through the real binary).
	if pgrep, lerr := exec.LookPath("pgrep"); lerr == nil {
		left, _ := exec.Command(pgrep, "-g", strconv.Itoa(state.Pid())).Output()
		for _, line := range strings.Fields(string(left)) {
			if line != "" && line != strconv.Itoa(state.Pid()) {
				t.Errorf("a process is still running in the draft command's process group: pid %s", line)
			}
		}
	}
	// A SECOND PROCESS FINDS IT. That is what "not held only in memory" means, and it is the same
	// thing a daemon restart or a machine restart does.
	code, out, _ = run("outbox", "list")
	if code != 0 || !strings.Contains(out, "note-a") {
		t.Fatalf("a new process does not find the draft (exit %d):\n%s", code, out)
	}
	code, out, _ = run("outbox", "state", "note-a")
	if code != 0 || !strings.Contains(out, string(drafts.StateDrafted)) {
		t.Fatalf("a new process does not find the draft's state (exit %d):\n%s", code, out)
	}
}
