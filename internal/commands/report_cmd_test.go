package commands

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/reports"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// reportRunner drives `omw report ...` IN THIS PROCESS, with a fully constructed environment.
//
// In-process on purpose: every criterion in Issue #23 is an assertion about what a person sees and
// what the exit code is, and both are reachable here without spawning anything. It also means this
// file cannot repeat the defect the package's structural check guards against — there is no
// os.Environ() to inherit because there is no child process at all.
type reportRunner struct {
	t     *testing.T
	env   map[string]string
	store string
}

func newReportRunner(t *testing.T) *reportRunner {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(dir, store.AcceptUndeterminedLocation()); err != nil {
		t.Skipf("this environment cannot create a store to test against: %v", err)
	}
	// The store is named in the ENVIRONMENT rather than by a flag, because the command resolves it
	// the same way daemonLiveness does and the two must be about the same store. It is set to a
	// t.TempDir(), so nothing here can reach the developer's own store or their device pointer.
	return &reportRunner{t: t, env: map[string]string{store.PathEnv: dir}, store: dir}
}

func (r *reportRunner) getenv(k string) string { return r.env[k] }

func (r *reportRunner) run(args ...string) (int, string, string) {
	r.t.Helper()
	var out, errb bytes.Buffer
	full := append([]string{"report"}, args...)
	code := cli.Run(full, &out, &errb, r.getenv)
	return code, out.String(), errb.String()
}

// openStore reaches the same store the command writes to, for staging activity.
func (r *reportRunner) openStore() *store.Store {
	r.t.Helper()
	s, err := store.Open(r.store)
	if err != nil {
		r.t.Fatalf("opening the test store: %v", err)
	}
	return s
}

func (r *reportRunner) stage(items ...reports.Item) {
	r.t.Helper()
	s := r.openStore()
	for _, it := range items {
		if err := reports.WriteActivity(s, it); err != nil {
			r.t.Fatalf("staging %v: %v", it, err)
		}
	}
}

// A MALFORMED SELECTOR AND A SELECTOR THAT MATCHES NOTHING GET DIFFERENT EXIT CODES.
//
// This is the Issue's central distinction expressed the way a script sees it. The two must never
// be confused, and prose alone would not stop a caller confusing them.
func TestRefusalAndUnmatchedHaveDifferentExitCodes(t *testing.T) {
	r := newReportRunner(t)

	code, _, errOut := r.run("subscribe", "bad", "git:enormous")
	if code != cli.ExitUsage {
		t.Errorf("a malformed selector exited %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "enormous") {
		t.Errorf("the refusal does not name the offending token: %q", errOut)
	}
	if !strings.Contains(errOut, "nothing has been stored") {
		t.Errorf("the refusal does not say nothing was stored: %q", errOut)
	}
	// AND NOTHING WAS STORED. The refusal's own words are checked above; this checks the disk.
	if code, out, _ := r.run("show", "bad"); code == cli.Success {
		t.Errorf("the refused subscription is on disk: %q", out)
	}
	if code, out, _ := r.run("list"); !strings.Contains(out, "subscriptions: 0") {
		t.Errorf("list after a refusal says %q (exit %d), want no subscriptions", out, code)
	}

	code, _, _ = r.run("subscribe", "typo", "nosuchsubject:full")
	if code == cli.ExitUsage {
		t.Fatalf("a well-formed selector for an unknown subject was refused as a usage error — "+
			"refusing loudly and matching nothing quietly are different outcomes and must not share a code (exit %d)", code)
	}
	code, out, _ := r.run("run", "typo")
	if code != cli.ExitFailure {
		t.Errorf("an unmatched selector exited %d, want ExitFailure (%d)", code, cli.ExitFailure)
	}
	if code == cli.ExitUsage {
		t.Error("an unmatched selector shares its exit code with a refusal")
	}
	if !strings.Contains(out, "unmatched selector") {
		t.Errorf("the report does not report the unmatched selector:\n%s", out)
	}
}

// THREE FACTS, THREE EXIT CODES AND THREE OUTPUTS, THROUGH THE CLI.
func TestReportRunExitCodesForTheThreeFacts(t *testing.T) {
	r := newReportRunner(t)
	r.stage(reports.Item{ID: "c1", Subject: "git", Kind: "commit", Text: "a real commit"})

	if _, err := reports.Save(r.openStore(), "active", "git:full"); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Save(r.openStore(), "quiet", "token_usage:count"); err != nil {
		t.Fatal(err)
	}

	code, out, _ := r.run("run", "active")
	if code != cli.Success {
		t.Errorf("a subject with activity exited %d, want Success", code)
	}
	if !strings.Contains(out, "a real commit") {
		t.Errorf("full did not carry the commit message:\n%s", out)
	}

	code, quietOut, _ := r.run("run", "quiet")
	if code != cli.Success {
		t.Errorf("a subject with no activity exited %d, want Success — an established emptiness is an answer", code)
	}
	if !strings.Contains(quietOut, "no activity") {
		t.Errorf("a quiet subject did not say so:\n%s", quietOut)
	}
	if quietOut == out {
		t.Error("a quiet subject and an active one produced the same output")
	}
}

// CRITERION 20: no command here starts the daemon, and every subscription operation SAYS whether it
// is running rather than appearing to succeed silently.
//
// The daemon's state is PROBED, not named: the socket that stands for "running" is a real file this
// test creates, so the assertion does not depend on any daemon actually existing here.
func TestSubscriptionOperationsSayTheDaemonIsNotRunningAndDoNotStartIt(t *testing.T) {
	r := newReportRunner(t)
	for _, args := range [][]string{
		{"subscribe", "daily", "git:full"},
		{"show", "daily"},
		{"list"},
		{"run", "daily"},
	} {
		code, out, errOut := r.run(args...)
		if code != cli.Success {
			t.Fatalf("omw report %v exited %d: %s", args, code, errOut)
		}
		if !strings.Contains(out, "daemon: not running") {
			t.Errorf("omw report %v does not say the daemon is not running:\n%s", args, out)
		}
	}

	// AND IT IS STILL NOT RUNNING AFTERWARDS, asked through the SAME function the command uses —
	// which is the whole point of Issue #41: a second opinion here would be a second guess.
	if live, why := daemonLiveness(cli.Env{Getenv: r.getenv}); live != tri.No {
		t.Errorf("after four report operations, liveness is %v (%s) — want a determined no", live, why)
	}
}

// THE OTHER TWO ANSWERS, DRIVEN. Without these the test above would pass for a command that printed
// "not running" unconditionally — which is precisely the defect Issue #41 removed from four
// surfaces, this one among them.
//
// daemonLiveness is stubbed here and ONLY here: this asserts the RENDERING of each of the three
// answers. That the answer itself is right is asserted by `liveness_test.go`, against a real daemon
// started by the real binary, and stubbing would prove nothing about it.
func TestTheReportCommandRendersAllThreeLivenessAnswers(t *testing.T) {
	r := newReportRunner(t)
	real := daemonLiveness
	t.Cleanup(func() { daemonLiveness = real })

	daemonLiveness = func(cli.Env) (tri.Value, string) { return tri.Yes, "" }
	_, running, _ := r.run("list")
	if !strings.Contains(running, "daemon: running") {
		t.Errorf("with the daemon running the command does not say so:\n%s", running)
	}
	if strings.Contains(running, "not running") {
		t.Errorf("the running answer contains the negative's wording:\n%s", running)
	}

	daemonLiveness = func(cli.Env) (tri.Value, string) { return tri.No, "" }
	_, stopped, _ := r.run("list")
	if !strings.Contains(stopped, "daemon: not running") {
		t.Errorf("with the daemon stopped the command does not say so:\n%s", stopped)
	}

	daemonLiveness = func(cli.Env) (tri.Value, string) {
		return tri.Undetermined, "the daemon lock could not be opened"
	}
	code, undetermined, _ := r.run("list")
	if undetermined == stopped || undetermined == running {
		t.Errorf("the third answer renders identically to a determined one:\n%s", undetermined)
	}
	if !strings.Contains(undetermined, tri.Undetermined.String()) {
		t.Errorf("the third answer is not rendered in words:\n%s", undetermined)
	}
	if strings.Contains(undetermined, "daemon: not running") {
		t.Errorf("liveness that could not be established was rendered as a stopped daemon:\n%s", undetermined)
	}
	if !strings.Contains(undetermined, "the daemon lock could not be opened") {
		t.Errorf("the third answer dropped its reason:\n%s", undetermined)
	}
	if !strings.Contains(undetermined, "this is not a report that the daemon is stopped") {
		t.Errorf("the third answer does not tell the reader it is not a negative:\n%s", undetermined)
	}
	// AND IT DOES NOT CHANGE THE ANSWER TO THE QUESTION THE PERSON ASKED. These operations are
	// local and need no daemon (§4.4); an undetermined daemon must not turn a determined, complete
	// subscription listing into an undetermined one.
	if code != cli.Success {
		t.Errorf("`omw report list` exited %d because the DAEMON's state was undetermined; the listing itself was determined", code)
	}
}

// CRITERION 22: with no hub configured, a purely local subscription is written, read back and run
// end to end, with no degradation and NO WARNING ABOUT A MISSING HUB.
func TestLocalOnlyFlowNeedsNoHubAndSaysNothingAboutOne(t *testing.T) {
	r := newReportRunner(t)
	if r.env["OMW_HUB"] != "" {
		t.Fatal("this test requires no hub configured")
	}
	r.stage(
		reports.Item{ID: "c1", Subject: "git", Kind: "commit", Text: "a local commit"},
		reports.Item{ID: "s1", Subject: "token_usage", Kind: "spend", Text: "4210"},
	)
	written := "git:full, token_usage:digest"
	if code, _, errOut := r.run("subscribe", "local", written); code != cli.Success {
		t.Fatalf("subscribe exited %d: %s", code, errOut)
	}
	code, shown, _ := r.run("show", "local")
	if code != cli.Success {
		t.Fatalf("show exited %d", code)
	}
	if !strings.Contains(shown, "git:full") || !strings.Contains(shown, "token_usage:digest") {
		t.Errorf("the subscription did not read back as written:\n%s", shown)
	}
	code, out, errOut := r.run("run", "local")
	if code != cli.Success {
		t.Errorf("a local-only report exited %d, want Success:\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(strings.ToLower(out+errOut), "hub") {
		t.Errorf("a local-only flow warned about the hub:\n%s\n%s", out, errOut)
	}
	if !strings.Contains(out, "a local commit") || !strings.Contains(out, "- spend: 1") {
		t.Errorf("the local report did not produce its content:\n%s", out)
	}
}

// CRITERION 23 THROUGH THE CLI: a hub-supplied subject with no hub says what is missing and exits
// distinguishably from a clean run.
func TestHubSuppliedSubjectWithNoHubSaysSo(t *testing.T) {
	r := newReportRunner(t)
	if code, _, errOut := r.run("subscribe", "notes", "published_notes:summary"); code != cli.Success {
		t.Fatalf("subscribe exited %d: %s", code, errOut)
	}
	code, out, _ := r.run("run", "notes")
	if code == cli.Success {
		t.Errorf("a report that could not answer a subject exited Success:\n%s", out)
	}
	if !strings.Contains(out, "no hub is configured") {
		t.Errorf("the report does not say a hub is what is missing:\n%s", out)
	}
	if strings.Contains(out, "no activity") || strings.Contains(out, "unmatched") {
		t.Errorf("the missing hub was rendered as an emptiness or an unknown subject:\n%s", out)
	}
	if !strings.Contains(out, "published_notes:summary") {
		t.Errorf("the subject was omitted from the report:\n%s", out)
	}
}

// CRITERION 21, STRUCTURALLY: no operation on subscriptions or reports opens any connection.
//
// WHY STRUCTURAL. "Zero outbound connections were attempted" is only observable at run time by
// something that can see the syscalls; a test that dialled a listener and found nobody there would
// be asserting that this build has no transport, which is true today for a reason unrelated to this
// Issue. So this reads the source of everything this flow can reach and requires that it cannot
// open a connection at all: no net, no net/http, no exec. The repository's existing check that
// every listen and dial names "unix" is the complement — this one says there are none.
func TestTheReportFlowCannotOpenAConnection(t *testing.T) {
	files := []string{"report_cmd.go"}
	dir, err := os.ReadDir("../reports")
	if err != nil {
		t.Fatalf("reading the reports package: %v", err)
	}
	paths := append([]string{}, files...)
	for _, e := range dir {
		if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			paths = append(paths, filepath.Join("../reports", e.Name()))
		}
	}
	banned := map[string]bool{"net": true, "net/http": true, "net/url": true, "os/exec": true}

	fset := token.NewFileSet()
	checked := 0
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		checked++
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquoting %s: %v", p, imp.Path.Value, err)
			}
			if banned[path] {
				t.Errorf("%s imports %q. With no hub configured nothing on this path may reach out "+
					"(PRD §4.2), and a subscription is a local standing instruction.", p, path)
			}
		}
	}
	// A CONTROL. Fewer files than the package has means the walk examined the wrong place, and a
	// pass would say nothing at all.
	if checked < 6 {
		t.Fatalf("examined only %d file(s) — the walk found nothing to check, so its pass is empty", checked)
	}
	t.Logf("examined %d file(s) on the report flow", checked)
}

// The report command reaches nothing outside its own package and cli/reports/store. Asserted so a
// later change cannot route a report through something that does dial.
func TestReportCommandImportsAreNarrow(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "report_cmd.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing report_cmd.go: %v", err)
	}
	allowed := map[string]bool{
		"errors": true, "fmt": true, "io": true, "strings": true,
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri":     true,
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli":     true,
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/reports": true,
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store":   true,
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if !allowed[path] {
			t.Errorf("report_cmd.go imports %q, which is outside what this command needs", path)
		}
	}
	// This file must not reference another command file's helpers — package commands is one file
	// per command by design (see doc.go).
	src, err := os.ReadFile("report_cmd.go")
	if err != nil {
		t.Fatalf("reading report_cmd.go: %v", err)
	}
	// daemonLiveness is the ONE exception and is deliberately not in this list: Issue #41 made it
	// shared on purpose, and a command that avoided it would be the fifth guess.
	for _, foreign := range []string{"reachHub(", "noteSource", "noteDaemonRunning", "parseNoteFlags"} {
		if bytes.Contains(src, []byte(foreign)) {
			t.Errorf("report_cmd.go uses %s from another Issue's file", foreign)
		}
	}
}
