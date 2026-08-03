package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// mdSecret is the sentinel credential these tests configure.
//
// IT IS A STRING NOTHING IN THE PRODUCT COULD PRODUCE BY ACCIDENT, so finding it in an output
// stream is a finding and never a coincidence. Criterion 7 is driven by grepping for exactly this.
const mdSecret = "sk-ZQXJ-the-persons-own-key-7c21"

type mdResult struct {
	code   int
	stdout string
	stderr string
}

func (r mdResult) all() string { return r.stdout + r.stderr }

func runModelCmd(t *testing.T, env map[string]string, args ...string) mdResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"model"}, args...), &out, &errb, func(k string) string { return env[k] })
	return mdResult{code: code, stdout: out.String(), stderr: errb.String()}
}

// mdWorld is a machine with a store and nothing else: no hub, no daemon, no model.
//
// THAT IS THE DEFAULT ON PURPOSE (PRD §4.4, criterion 17). The environment a test has to go out of
// its way to build is the one WITH a hub.
func mdWorld(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	return map[string]string{store.PathEnv: root}
}

func mdMustRun(t *testing.T, env map[string]string, args ...string) mdResult {
	t.Helper()
	got := runModelCmd(t, env, args...)
	if got.code != cli.Success {
		t.Fatalf("omw model %s exited %d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), got.code, got.stdout, got.stderr)
	}
	return got
}

func mdDistinct(t *testing.T, what string, renders map[string]string) {
	t.Helper()
	for aName, a := range renders {
		if strings.TrimSpace(a) == "" {
			t.Errorf("%s: the %q output is empty; silence is not one of the answers", what, aName)
		}
		for bName, b := range renders {
			if aName < bName && a == b {
				t.Errorf("%s: %q and %q produce identical output:\n%s", what, aName, bName, a)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 1, 2, 3 — choosing a provider and supplying a key, through the CLI
// ---------------------------------------------------------------------------

func TestAPersonChoosesAProviderAndReadsItBack(t *testing.T) {
	env := mdWorld(t)

	before := mdMustRun(t, env, "show")
	if !strings.Contains(before.stdout, "no provider is chosen") {
		t.Errorf("a fresh machine does not say no provider is chosen:\n%s", before.stdout)
	}

	mdMustRun(t, env, "use", "acme")

	after := mdMustRun(t, env, "show")
	if !strings.Contains(after.stdout, "acme") {
		t.Errorf("after choosing acme, the read-out does not name it (criterion 2):\n%s", after.stdout)
	}
	// CRITERION 2: the two never render identically.
	if before.stdout == after.stdout {
		t.Error("naming a provider and naming none render identically")
	}
}

// CRITERION 3, at the command: the three configurations produce three different read-outs, and the
// middle one is neither of the others.
func TestTheCommandDistinguishesChosenChosenWithoutAKeyAndConfigured(t *testing.T) {
	none := mdWorld(t)

	half := mdWorld(t)
	mdMustRun(t, half, "use", "acme")

	whole := mdWorld(t)
	mdMustRun(t, whole, "use", "acme")
	whole[model.EnvCredential] = mdSecret

	noneOut := mdMustRun(t, none, "show").stdout
	halfOut := mdMustRun(t, half, "show").stdout
	wholeOut := mdMustRun(t, whole, "show").stdout

	if !strings.Contains(halfOut, "NO credential") {
		t.Errorf("a provider with no credential does not say so:\n%s", halfOut)
	}
	mdDistinct(t, "the model read-out", map[string]string{
		"no provider":           noneOut,
		"chosen, no credential": halfOut,
		"chosen and configured": wholeOut,
	})
}

// CRITERION 1, the other half: nothing configures a model as a side effect of another command.
//
// It is driven at the RECORD, because a side effect through any function would still have to write
// it. Every command this build has is run, and the record must still not exist.
func TestNoCommandConfiguresAModelAsASideEffect(t *testing.T) {
	env := mdWorld(t)
	root := env[store.PathEnv]
	recordPath := filepath.Join(root, "records", "model")

	for _, c := range cli.Commands() {
		if c.Name == "model" {
			continue // `omw model use` is the explicit act; that is the point.
		}
		var out, errb bytes.Buffer
		_ = cli.Run([]string{c.Name}, &out, &errb, func(k string) string { return env[k] })
		if _, err := os.Stat(recordPath); err == nil {
			t.Fatalf("omw %s created a model configuration as a side effect (§4.2, criterion 1)", c.Name)
		}
	}

	// THE CONTROL. The explicit act must actually create it, or the sweep above proves only that
	// the path is never written by anything.
	mdMustRun(t, env, "use", "acme")
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("the explicit act did not create the record either, so this test is watching the wrong path: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Criteria 4, 7 — the credential appears in no output stream, and in no export
// ---------------------------------------------------------------------------

// EVERY COMMAND IN THE BINARY, NOT THE ONES SOMEBODY THOUGHT OF.
//
// The list comes from the registry, so an Issue that adds a subcommand next week is swept by this
// test without anybody remembering to add it. Both streams are checked, because a credential on
// stderr is as published as one on stdout.
func TestTheCredentialAppearsInNoOutputOfAnyCommand(t *testing.T) {
	env := mdWorld(t)
	mdMustRun(t, env, "use", "acme")
	env[model.EnvCredential] = mdSecret

	// THE CONTROL, FIRST. If the credential is not actually configured, every assertion below is
	// vacuous — the sweep would be looking for something that was never in play.
	s, err := store.Open(env[store.PathEnv])
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Read(func(k string) string { return env[k] }, s).Secret(); got != mdSecret {
		t.Fatalf("the credential is not configured, so this sweep proves nothing (got %q)", got)
	}

	invocations := [][]string{
		{"model"}, {"model", "show"}, {"model", "use", "acme"}, {"model", "providers"},
		{"model", "key", "file", "/tmp/nowhere"}, {"model", "clear"}, {"model", "help"},
	}
	for _, c := range cli.Commands() {
		invocations = append(invocations, []string{c.Name}, []string{c.Name, "help"})
	}
	// The outbox path that actually reaches for the credential.
	invocations = append(invocations,
		[]string{"outbox", "mode", "set", "review"},
		[]string{"outbox", "model"},
		[]string{"outbox", "draft", "a-draft", "some writing"},
		[]string{"outbox", "review", "a-draft"},
		[]string{"outbox", "publish", "a-draft"},
		[]string{"outbox", "list"},
		[]string{"daemon", "status"},
		[]string{"health"},
	)

	for _, args := range invocations {
		var out, errb bytes.Buffer
		_ = cli.Run(args, &out, &errb, func(k string) string { return env[k] })
		if strings.Contains(out.String(), mdSecret) {
			t.Errorf("omw %s printed the credential on stdout:\n%s", strings.Join(args, " "), out.String())
		}
		if strings.Contains(errb.String(), mdSecret) {
			t.Errorf("omw %s printed the credential on stderr:\n%s", strings.Join(args, " "), errb.String())
		}
	}

	// CRITERION 7's "a full export of the local store". Every byte under the store root, including
	// the drafts written above and the daemon's run directory.
	assertNoSecretUnder(t, env[store.PathEnv])
}

// A recorded credential FILE puts nothing in the store either — the path is recorded, the bytes are
// not (criterion 7, and the reason omw takes no custody of keys).
func TestRecordingACredentialFileLeavesNoCredentialInTheStore(t *testing.T) {
	env := mdWorld(t)
	keyPath := filepath.Join(t.TempDir(), "my-key")
	if err := os.WriteFile(keyPath, []byte(mdSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	mdMustRun(t, env, "use", "acme")
	mdMustRun(t, env, "key", "file", keyPath)

	show := mdMustRun(t, env, "show")
	if strings.Contains(show.all(), mdSecret) {
		t.Errorf("the read-out printed the credential:\n%s", show.all())
	}
	if !strings.Contains(show.stdout, "is configured, with a credential") {
		t.Errorf("the recorded credential file did not resolve, so the sweep below proves nothing:\n%s", show.stdout)
	}
	assertNoSecretUnder(t, env[store.PathEnv])
}

func assertNoSecretUnder(t *testing.T, root string) {
	t.Helper()
	read := 0
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		read++
		if strings.Contains(string(b), mdSecret) {
			t.Errorf("%s contains the person's credential", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if read == 0 {
		t.Fatalf("the walk of %s read no files at all, so its pass proves nothing", root)
	}
}

// ---------------------------------------------------------------------------
// Criteria 8, 9, 17 — no model configured is not a broken client
// ---------------------------------------------------------------------------

// CRITERION 8: with no model AND no hub, everything that does not need a model reports success,
// with nothing about a missing model in its output.
//
// The word "model" must not appear in a run of a command that has nothing to do with models — that
// is what "no warning or degradation attributable to the absent model" means when read as something
// a test can check.
func TestWithNoModelEverythingThatDoesNotNeedOneWorksFully(t *testing.T) {
	env := mdWorld(t)

	for _, tc := range []struct {
		args []string
		// mayMentionAModel is true only for `omw daemon status`, and only because criterion 18
		// requires it: the control API reports the model configuration so that the CLI and the
		// control API can be shown to agree, and `daemon status` renders what the control API
		// serves. That is a STATE REPORT, not a warning and not a degradation — the two things
		// criterion 8 actually forbids. Every other command here must not mention a model at all.
		mayMentionAModel bool
	}{
		{args: []string{"store", "status"}},
		{args: []string{"health"}},
		{args: []string{"daemon", "status"}, mayMentionAModel: true},
		{args: []string{"outbox", "list"}},
		{args: []string{"outbox", "draft", "note-one", "something I wrote"}},
		{args: []string{"outbox", "state", "note-one"}},
		{args: []string{"outbox", "mode"}},
	} {
		var out, errb bytes.Buffer
		code := cli.Run(tc.args, &out, &errb, func(k string) string { return env[k] })
		if code != cli.Success {
			t.Errorf("omw %s exited %d with no model configured; it does not need one (criterion 8)\nstdout:\n%s\nstderr:\n%s",
				strings.Join(tc.args, " "), code, out.String(), errb.String())
		}
		if tc.mayMentionAModel {
			continue
		}
		// THE STORE PATH IS STRIPPED FIRST. t.TempDir() embeds the test's own name, and this test
		// is called TestWithNoModel… — so a naive search matched the temporary directory in every
		// output that prints a path, and reported three failures that were the test reading its
		// own name back. Found by driving it; recorded so nobody re-introduces it.
		body := strings.ToLower(strings.ReplaceAll(out.String(), env[store.PathEnv], "<store>"))
		if strings.Contains(body, "model") {
			t.Errorf("omw %s mentions a model on stdout, and it does not need one:\n%s",
				strings.Join(tc.args, " "), out.String())
		}
	}
}

// CRITERION 9 AND 8 TOGETHER: `omw model` on a fresh machine SUCCEEDS. Being told "no model is
// configured" is an answer, and a non-zero exit here would make a fresh machine read as broken —
// which §3.13 forbids in those words.
func TestNoModelConfiguredIsAnAnswerAndNotAnError(t *testing.T) {
	env := mdWorld(t)
	got := runModelCmd(t, env, "show")
	if got.code != cli.Success {
		t.Errorf("omw model on a machine with no model exited %d, want %d — 'no model configured' is not a broken client",
			got.code, cli.Success)
	}
	if !strings.Contains(got.stdout, "no provider is chosen") {
		t.Errorf("it did not say what is missing:\n%s", got.stdout)
	}
	// It must not be silent about it either: the answer is on stdout, not implied by exit 0.
	if strings.TrimSpace(got.stdout) == "" {
		t.Error("it answered with silence")
	}
}

// CRITERION 17: with no hub configured, the whole capability works. This asserts on what is NOT
// said as much as what is: nothing in this path may mention a hub, because nothing in it needs one.
func TestTheWholeCapabilityWorksWithNoHubConfigured(t *testing.T) {
	env := mdWorld(t)
	if env["OMW_HUB"] != "" {
		t.Fatal("this test's environment has a hub, so it is not driving what it names")
	}
	for _, args := range [][]string{
		{"use", "acme"}, {"key", "file", "/tmp/a-key"}, {"show"}, {"providers"}, {"clear"},
	} {
		got := runModelCmd(t, env, args...)
		if got.code != cli.Success {
			t.Errorf("omw model %s exited %d with no hub configured (criterion 17, §4.4)\n%s",
				strings.Join(args, " "), got.code, got.all())
		}
		if strings.Contains(strings.ToLower(got.all()), "hub") {
			t.Errorf("omw model %s mentions a hub, and nothing in this capability needs one:\n%s",
				strings.Join(args, " "), got.all())
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 11, 14 — three states, three outputs
// ---------------------------------------------------------------------------

// CRITERION 11: "A configured-but-failing model is distinguishable from no model configured. Both
// are distinguishable from a successful run. Three states, three distinguishable outputs."
//
// It is driven through `omw outbox review`, which is the surface that needs a model, with the
// provider seam replaced so that "the provider rejected the credential" is a state this build can
// reach without a network (§4.2).
func TestAFailingModelANoModelAndASuccessAreThreeOutputs(t *testing.T) {
	success := mdReviewRun(t, "pass", nil)
	failing := mdReviewRun(t, "", errCredentialRejected)
	noModel := mdReviewRunWithNoModel(t)

	if success.code != cli.Success {
		t.Errorf("a passing review exited %d:\n%s", success.code, success.all())
	}
	// A MODEL THAT FAILED IS NOT A REFUSAL AND NOT A PASS: the rules were not checked, so it is the
	// third value and gets the third exit code.
	if failing.code != cli.ExitUndetermined {
		t.Errorf("a model that rejected the credential exited %d, want %d — nothing was checked",
			failing.code, cli.ExitUndetermined)
	}
	// NO MODEL AT ALL IS A DETERMINED FACT and exits differently from "we could not check".
	if noModel.code == failing.code {
		t.Errorf("no model configured and a failing model share exit code %d; criterion 11 wants them apart",
			noModel.code)
	}
	if noModel.code != cli.ExitFailure {
		t.Errorf("no model configured exited %d, want %d", noModel.code, cli.ExitFailure)
	}

	mdDistinct(t, "the three model states through review", map[string]string{
		"review succeeded": success.all(),
		"model failed":     failing.all(),
		"no model":         noModel.all(),
	})
}

var errCredentialRejected = &mdError{"the provider rejected this credential"}

type mdError struct{ s string }

func (e *mdError) Error() string { return e.s }

// mdReviewRun drives `omw outbox review` with a model configured and a scripted provider answer.
func mdReviewRun(t *testing.T, answer string, err error) mdResult {
	t.Helper()
	env := mdWorld(t)
	env[model.EnvProvider] = "acme"
	env[model.EnvCredential] = mdSecret

	prev := outboxReviewer
	outboxReviewer = func(cli.Env, model.Config) drafts.Reviewer {
		return mdScripted{answer: answer, err: err}
	}
	t.Cleanup(func() { outboxReviewer = prev })

	mdSetUpReviewDraft(t, env)
	return mdRunAny(t, env, "outbox", "review", "a-draft")
}

func mdReviewRunWithNoModel(t *testing.T) mdResult {
	t.Helper()
	env := mdWorld(t)
	mdSetUpReviewDraft(t, env)
	return mdRunAny(t, env, "outbox", "review", "a-draft")
}

func mdSetUpReviewDraft(t *testing.T, env map[string]string) {
	t.Helper()
	// The rules and the draft are written in `manual` mode so that writing them is not itself
	// gated on the model, and the mode is switched to review afterwards.
	mdRunAny(t, env, "outbox", "rules", "set", "never mention a customer by name")
	mdRunAny(t, env, "outbox", "draft", "a-draft", "some writing about nobody in particular")
	mdRunAny(t, env, "outbox", "mode", "set", "review")
}

func mdRunAny(t *testing.T, env map[string]string, args ...string) mdResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(args, &out, &errb, func(k string) string { return env[k] })
	return mdResult{code: code, stdout: out.String(), stderr: errb.String()}
}

type mdScripted struct {
	answer string
	err    error
}

func (r mdScripted) Review(string, string) (string, error) { return r.answer, r.err }

// ---------------------------------------------------------------------------
// Criterion 15 — undetermined is not "no", at the command
// ---------------------------------------------------------------------------

// The third value gets its OWN EXIT CODE, and that is the assertion that matters: a script must be
// able to tell "no model" from "I could not check" without reading English.
func TestAnUndeterminedConfigurationExitsThreeAndSaysItIsNotANo(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(mdSecret), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(keyFile); err == nil {
		t.Skip("this environment can read a 0o000 file, so an unreadable credential file cannot be produced here")
	}

	env := mdWorld(t)
	env[model.EnvProvider] = "acme"
	env[model.EnvCredentialFile] = keyFile

	undet := runModelCmd(t, env, "show")
	if undet.code != cli.ExitUndetermined {
		t.Errorf("an unreadable credential file exits %d, want %d\n%s", undet.code, cli.ExitUndetermined, undet.all())
	}
	if !strings.Contains(undet.stdout, tri.Undetermined.String()) {
		t.Errorf("it does not use the product's wording for the third answer:\n%s", undet.stdout)
	}
	if strings.Contains(undet.stdout, "no provider is chosen") ||
		strings.Contains(undet.stdout, "NO credential has been supplied") {
		t.Errorf("an undetermined configuration renders as a negative:\n%s", undet.stdout)
	}

	// And it is a different exit code from every determined answer.
	none := runModelCmd(t, mdWorld(t), "show")
	if none.code == undet.code {
		t.Errorf("'no model configured' and 'could not be determined' share exit code %d", none.code)
	}
}

// AN UNREADABLE STORE IS NOT A STORE WITH NO MODEL IN IT. This is the second source of the third
// value, at the command: the recorded choice is there and cannot be read.
func TestAnUnreadableRecordedChoiceIsUndeterminedAndNotNoModel(t *testing.T) {
	env := mdWorld(t)
	mdMustRun(t, env, "use", "acme")

	rec := filepath.Join(env[store.PathEnv], "records", "model", "provider.rec")
	if _, err := os.Stat(rec); err != nil {
		t.Fatalf("the record this test means to corrupt is not at %s: %v", rec, err)
	}
	if err := os.WriteFile(rec, []byte("not a record"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := runModelCmd(t, env, "show")
	if got.code != cli.ExitUndetermined {
		t.Errorf("an unreadable recorded choice exits %d, want %d\n%s", got.code, cli.ExitUndetermined, got.all())
	}
	if strings.Contains(got.stdout, "no provider is chosen") {
		t.Errorf("an unreadable recorded choice renders as 'no provider is chosen':\n%s", got.stdout)
	}
}

// ---------------------------------------------------------------------------
// Criterion 16 — nothing implicit
// ---------------------------------------------------------------------------

// NO MODEL COMMAND STARTS THE DAEMON, AND EVERY ONE SAYS WHETHER IT IS RUNNING.
//
// The liveness answer comes from `liveness.go` (Issue #41), which is the one definition in this
// package, so this cannot disagree with `omw daemon status` about the same daemon.
func TestNoModelCommandStartsTheDaemonAndEveryOneSaysSo(t *testing.T) {
	env := mdWorld(t)
	for _, args := range [][]string{{"show"}, {"use", "acme"}, {"providers"}, {"clear"}} {
		got := runModelCmd(t, env, args...)
		if !strings.Contains(got.stderr, "daemon:") {
			t.Errorf("omw model %s says nothing about the daemon (criterion 16, §4.2):\n%s",
				strings.Join(args, " "), got.all())
		}
		if !strings.Contains(got.stderr, "nothing has been started on your behalf") &&
			!strings.Contains(got.stderr, "did not start it") {
			t.Errorf("omw model %s does not say that it started nothing:\n%s", strings.Join(args, " "), got.stderr)
		}
	}
	// Nothing is running afterwards, asked the one way this package is allowed to ask.
	if live, _ := daemonLiveness(cli.Env{Getenv: func(k string) string { return env[k] }}); live == tri.Yes {
		t.Error("a model command left a daemon running")
	}
}
