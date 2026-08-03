package commands

import (
	"bytes"
	"go/ast"
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

	// THE THIRD FACT CHANGED WITH ISSUE #67, AND THE TEST SAYS WHICH FACT IT NOW IS.
	//
	// This used to read `token_usage` as an established emptiness — "a subject with no activity
	// exited 0, an established emptiness is an answer". It was neither: nothing in this build has
	// ever written token_usage activity, so the emptiness was a fact about the client and the exit
	// 0 was a confident negative. `omw report subjects` now says so, and the report says so.
	// TestASubjectWithNoProducerIsUndeterminedAndNotAQuietDay drives the distinction in full.
	code, quietOut, _ := r.run("run", "quiet")
	if code != cli.ExitUndetermined {
		t.Errorf("a subject nothing in this build writes exited %d, want ExitUndetermined (%d)", code, cli.ExitUndetermined)
	}
	if strings.Contains(quietOut, "no activity") {
		t.Errorf("a subject nobody observes is reported as a quiet period:\n%s", quietOut)
	}
	if !strings.Contains(quietOut, "could not be determined") {
		t.Errorf("an unobservable subject does not say so:\n%s", quietOut)
	}
	if quietOut == out {
		t.Error("an unobservable subject and an active one produced the same output")
	}
}

// CRITERION 20: no command here starts the daemon, and every subscription operation SAYS whether it
// is running rather than appearing to succeed silently.
//
// The daemon's state is PROBED, not named: the socket that stands for "running" is a real file this
// test creates, so the assertion does not depend on any daemon actually existing here.
func TestSubscriptionOperationsSayTheDaemonIsNotRunningAndDoNotStartIt(t *testing.T) {
	r := newReportRunner(t)
	// `run` is staged with real activity first, so it reaches a DETERMINED report: since Issue #67
	// a report on a subject nothing writes exits 3, and this test is about what every operation
	// SAYS, not about that exit code.
	r.stage(reports.Item{ID: "c1", Subject: "git", Kind: "commit", Text: "a real commit"})
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

// CRITERION 21: NO OPERATION ON SUBSCRIPTIONS OR REPORTS OPENS A NETWORK CONNECTION.
//
// # WHAT KIND OF CLAIM THIS IS, STATED BEFORE THE CODE
//
// The criterion's literal wording is "zero outbound connections are attempted across the whole
// flow". A run-time observation of that is either a syscall trace or a sandbox, and a SANDBOX PROVES
// THE WRONG THING: running under a network-denying sandbox and succeeding shows the flow WORKS
// without network, not that it never TRIES. A dial that is attempted and denied is indistinguishable
// from one never made when the caller ignores the error. That is a deny-and-succeed argument, not a
// count of attempts.
//
// So this counts attempts, statically, over everything the flow can reach. It walks the TRANSITIVE
// import closure of the report flow inside this module and requires:
//
//   - no package in the closure imports a network stack at all (net/http, net/url, net/rpc,
//     crypto/tls, os/exec) — none of these has a local-IPC use, so an import is the attempt;
//   - every net.Dial* / net.Listen* CALL SITE reachable from the flow names "unix" as a literal.
//
// That second rule is why this is not an import ban: `net` cannot be banned outright any more,
// because the control API is a unix socket and Go puts unix sockets in `net`. The rule about the
// USE is strictly stronger than a ban on the import — a ban cannot tell a local socket from a
// connection to another machine, and this can. It is the repository-wide rule from
// `network_guard_test.go`, scoped to this flow and paired with a count.
//
// # THE ONE THING THE REPORT FLOW DOES DIAL, NAMED HONESTLY
//
// Since Issue #41 this command asks `daemonLiveness`, which reaches `daemon.Inspect`, which calls
// `net.DialTimeout("unix", socket, …)` when the lock says a daemon is holding the store. So the
// flow is NOT "cannot open a socket at all" — an earlier version of this test asserted that, and it
// was true only before the liveness rewiring. It is: the only socket anything on this path can open
// is a unix-domain socket to a process on this machine, and there is no code path from here to a
// network one. That is what the assertions below establish, and it is what the PR body claims.
func TestTheReportFlowOpensNoNetworkConnection(t *testing.T) {
	const modulePrefix = "github.com/VincentHanxiaoDu/oh-my-workspace/"

	// The roots the report flow actually reaches: this package's report command imports cli,
	// reports, store and tri, and daemonLiveness takes it into daemon and hub.
	roots := []string{
		"internal/reports", "internal/store", "internal/cli", "internal/tri",
		"internal/daemon", "internal/hub",
	}
	// Banned outright: none of these has a local-IPC use, so reaching one IS the attempt.
	banned := map[string]string{
		"net/http":   "an HTTP client or server",
		"net/url":    "a URL, which is a thing addressed on another machine",
		"net/rpc":    "an RPC transport",
		"crypto/tls": "a TLS connection",
		"os/exec":    "a child process, which could dial on this flow's behalf",
	}

	fset := token.NewFileSet()
	seen := map[string]bool{}
	var files []string     // every file in the closure, for the call-site walk
	var order []string     // packages visited, for the report
	var unixDials []string // every reachable dial/listen, with the network it names

	var visit func(pkg string)
	visit = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		order = append(order, pkg)
		entries, err := os.ReadDir(filepath.Join("..", "..", pkg))
		if err != nil {
			t.Fatalf("reading %s: %v", pkg, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel := filepath.Join("..", "..", pkg, name)
			f, err := parser.ParseFile(fset, rel, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing %s: %v", rel, err)
			}
			files = append(files, rel)
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s: unquoting %s: %v", rel, imp.Path.Value, err)
				}
				if why, bad := banned[path]; bad {
					t.Errorf("%s imports %q — %s. With no hub configured nothing on the report flow "+
						"may reach out (PRD §4.2), and a subscription is a local standing instruction.",
						rel, path, why)
				}
				if strings.HasPrefix(path, modulePrefix) {
					visit(strings.TrimPrefix(path, modulePrefix))
				}
			}
		}
	}
	// report_cmd.go's own imports are walked first, so this test fails if the command ever grows a
	// dependency on something the roots above do not already cover.
	cmd, err := parser.ParseFile(fset, "report_cmd.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing report_cmd.go: %v", err)
	}
	for _, imp := range cmd.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if why, bad := banned[path]; bad {
			t.Errorf("report_cmd.go imports %q — %s", path, why)
		}
		if strings.HasPrefix(path, modulePrefix) {
			visit(strings.TrimPrefix(path, modulePrefix))
		}
	}
	for _, r := range roots {
		visit(r)
	}

	// EVERY REACHABLE DIAL AND LISTEN NAMES "unix". This is the count of attempts: each one is
	// found, and each one is checked, rather than the absence of any being inferred from a run.
	dialOrListen := func(name string) bool {
		return strings.HasPrefix(name, "Dial") || strings.HasPrefix(name, "Listen")
	}
	for _, rel := range files {
		f, err := parser.ParseFile(fset, rel, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("re-parsing %s: %v", rel, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "net" || !dialOrListen(sel.Sel.Name) {
				return true
			}
			where := rel + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
			if len(call.Args) == 0 {
				t.Errorf("%s: net.%s with no network argument — this test cannot tell what it opens", where, sel.Sel.Name)
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: net.%s takes its network from a variable, so what it opens cannot be "+
					"read here. Name \"unix\" as a literal.", where, sel.Sel.Name)
				return true
			}
			if lit.Value != `"unix"` {
				t.Errorf("%s: net.%s opens %s, not \"unix\". The report flow may reach a process on "+
					"this machine and nothing else (PRD §4.2, §4.6).", where, sel.Sel.Name, lit.Value)
				return true
			}
			unixDials = append(unixDials, where)
			return true
		})
	}

	// TWO CONTROLS, because a static walk that examined nothing passes vacuously and a green would
	// then mean "I found no network" when it means "I looked nowhere".
	for _, want := range []string{"internal/reports", "internal/daemon"} {
		if !seen[want] {
			t.Fatalf("the closure never reached %s — the walk is wrong, so its pass says nothing", want)
		}
	}
	if len(files) < 20 {
		t.Fatalf("the closure held only %d file(s); the walk examined too little to mean anything", len(files))
	}
	t.Logf("walked %d package(s), %d file(s); %d reachable dial/listen call site(s), all \"unix\": %v",
		len(order), len(files), len(unixDials), unixDials)
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
