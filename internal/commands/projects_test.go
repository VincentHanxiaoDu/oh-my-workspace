package commands

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// projectsSandbox gives a test a store of its own and an environment that names it.
//
// $OMW_STORE points at it, so nothing resolves through the device registry and nothing in this file
// can touch the developer's real device pointer. Every command below is run IN PROCESS through
// cli.Run rather than by spawning the binary — there is no environment to inherit and therefore no
// way to reach the real pointer at all, which is the same defect the structural check in
// store_test.go guards the spawning tests against.
func projectsSandbox(t *testing.T) (storePath string, getenv func(string) string) {
	t.Helper()
	root := t.TempDir()
	storePath = filepath.Join(root, "store")
	if _, err := store.Create(storePath, store.AcceptUndeterminedLocation()); err != nil {
		t.Fatalf("creating the sandbox store: %v", err)
	}
	env := map[string]string{
		store.PathEnv:     storePath,
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"HOME":            root,
		"OMW_HUB":         "", // no hub configured: PRD §4.4, criterion 12
		projects.DepthEnv: "",
	}
	return storePath, func(k string) string { return env[k] }
}

// runProjectsCmd drives `omw projects ...` and returns exit code, stdout and stderr.
func runProjectsCmd(t *testing.T, getenv func(string) string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"projects"}, args...), &out, &errb, getenv)
	return code, out.String(), errb.String()
}

func openSandbox(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("opening the sandbox store: %v", err)
	}
	return s
}

// CRITERIA 1, 2, 3 through the surface a person actually uses.
func TestProjectsAddListRemoveThroughTheCLI(t *testing.T) {
	storePath, getenv := projectsSandbox(t)
	dir := t.TempDir()

	if code, _, errOut := runProjectsCmd(t, getenv, "add", dir); code != cli.Success {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	// Adding twice is one project (criterion 1), and the second add is not an error.
	if code, _, errOut := runProjectsCmd(t, getenv, "add", dir); code != cli.Success {
		t.Fatalf("the second add exited %d: %s", code, errOut)
	}
	code, out, errOut := runProjectsCmd(t, getenv, "list")
	if code != cli.Success {
		t.Fatalf("list exited %d: %s", code, errOut)
	}
	if n := strings.Count(out, dir); n != 1 {
		t.Errorf("the directory appears %d times in the listing, want 1:\n%s", n, out)
	}

	if code, _, errOut := runProjectsCmd(t, getenv, "remove", dir); code != cli.Success {
		t.Fatalf("remove exited %d: %s", code, errOut)
	}
	_, out, _ = runProjectsCmd(t, getenv, "list")
	if strings.Contains(out, dir) {
		t.Errorf("still listed after remove:\n%s", out)
	}
	_ = storePath
}

// CRITERION 13: refused by exit status alone, with no prose parsed.
func TestPointingAtSomethingThatIsNotADirectoryIsRefusedByExitCode(t *testing.T) {
	_, getenv := projectsSandbox(t)
	tmp := t.TempDir()
	file := filepath.Join(tmp, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := t.TempDir()

	okCode, _, _ := runProjectsCmd(t, getenv, "add", good)
	for _, bad := range []string{file, filepath.Join(tmp, "not-there")} {
		code, _, _ := runProjectsCmd(t, getenv, "add", bad)
		if code == okCode {
			t.Errorf("adding %s exited %d, the same as adding a healthy directory. "+
				"Criterion 13 requires the two be distinguishable without parsing prose.", bad, code)
		}
		if code == cli.Success {
			t.Errorf("adding %s succeeded", bad)
		}
	}
	// And nothing was accepted-and-rendered-as-ordinary: the listing has only the good one.
	_, out, _ := runProjectsCmd(t, getenv, "list")
	if strings.Contains(out, "a-file") || strings.Contains(out, "not-there") {
		t.Errorf("a refused path is in the listing:\n%s", out)
	}
}

// CRITERION 6 AND 7 AT THE SURFACE THE CRITERION IS ABOUT: the two situations produce different
// listings, from the same command over the same project, and each states its own provenance.
//
// THE DAEMON IS A REAL ONE, started by the real binary the way a person starts it. It used to be
// simulated by writing a heartbeat record this package invented, and that heartbeat WAS the defect
// Issue #41 removed: a second answer to "is the daemon running", which agreed with nothing. A test
// against a fixture only this package understands would still have been green with `omw daemon
// status` saying the opposite — which is exactly what shipped.
func TestTheListingsWithAndWithoutADaemonAreDifferentOutputs(t *testing.T) {
	bin := buildOMW(t)
	storePath, getenv := projectsSandbox(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := runProjectsCmd(t, getenv, "add", dir); code != cli.Success {
		t.Fatalf("add: %s", e)
	}

	// --- Nothing running. The fixture is checked against the product's own answer first, so a
	// broken fixture reads as a broken fixture and not as a failed criterion.
	if got := statusClaim(t, getenv); got != tri.No {
		t.Fatalf("before anything was started `omw daemon status` says %v; the fixture is wrong", got)
	}
	codeStopped, stopped, _ := runProjectsCmd(t, getenv, "list")

	// --- A real daemon, plus the poll a projects-aware daemon performs.
	if start := runBinary(t, bin, storePath, "daemon", "start"); start.code != 0 {
		t.Fatalf("`omw daemon start` exited %d\n%s%s", start.code, start.stdout, start.stderr)
	}
	stoppedOnce := false
	stop := func() {
		if !stoppedOnce {
			runBinary(t, bin, storePath, "daemon", "stop")
			stoppedOnce = true
		}
	}
	t.Cleanup(stop)
	if got := statusClaim(t, getenv); got != tri.Yes {
		t.Fatalf("`omw daemon start` returned but `omw daemon status` says %v", got)
	}
	// Issue #2's daemon does not yet poll projects, so this stands in for the one call it will
	// make. It writes only the polled STATE — never any claim about liveness, which now has exactly
	// one source.
	if err := projects.Poll(openSandbox(t, storePath), getenv, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	codeRunning, running, _ := runProjectsCmd(t, getenv, "list")

	if stopped == running {
		t.Fatalf("the same command over the same project printed IDENTICAL output with nothing "+
			"watching and with a daemon watching. A person cannot tell the two apart from the "+
			"listing, which is criterion 6:\n%s", stopped)
	}
	if !strings.Contains(stopped, "examined during this command") {
		t.Errorf("with nothing watching, the listing does not say it examined the directories:\n%s", stopped)
	}
	if !strings.Contains(stopped, "watching: no") {
		t.Errorf("with nothing watching, the listing does not say so:\n%s", stopped)
	}
	if !strings.Contains(running, "watched by the daemon") {
		t.Errorf("with a daemon watching, the listing does not say the state came from it:\n%s", running)
	}
	if !strings.Contains(running, "watching: yes") {
		t.Errorf("with a daemon running, the listing does not say anything is watching:\n%s", running)
	}
	// CRITERION 5 OF ISSUE #41, at this surface: with a daemon running, nothing here may claim an
	// absence. This is the assertion whose absence let #35 through.
	assertNoStaleBecauseNothingIsWatchingClaim(t, "omw projects list", running)
	if codeStopped != cli.Success || codeRunning != cli.Success {
		t.Errorf("determined answers exited %d and %d; both are answers and both should be %d",
			codeStopped, codeRunning, cli.Success)
	}

	// --- And back, after a real stop: a probe that latched to "running" passes everything above.
	stop()
	if got := statusClaim(t, getenv); got != tri.No {
		t.Fatalf("after `omw daemon stop` returned, `omw daemon status` says %v", got)
	}
	_, afterStop, _ := runProjectsCmd(t, getenv, "list")
	if strings.Contains(afterStop, "watched by the daemon") {
		t.Errorf("after the daemon stopped, the listing still claims daemon provenance over a "+
			"state record the dead daemon left behind:\n%s", afterStop)
	}
	if !strings.Contains(afterStop, "watching: no") {
		t.Errorf("after the daemon stopped, the listing does not say nothing is watching:\n%s", afterStop)
	}
}

// ISSUE #41 CRITERION 4, APPLIED ONE LEVEL UP TO THE PROVENANCE ITSELF.
//
// Where liveness cannot be established, the listing must not say "examined during this command" —
// that phrase carries the claim that this command walked the directories BECAUSE nothing was
// watching, and nothing established that. It must not say "nothing is watching" either. The store
// is broken in a real way (the lock the daemon package reads is replaced by a directory), exactly
// as liveness_test.go stages it, rather than by stubbing the probe.
func TestWhereLivenessCannotBeEstablishedTheProvenanceIsUndeterminedToo(t *testing.T) {
	storePath, getenv := projectsSandbox(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := runProjectsCmd(t, getenv, "add", dir); code != cli.Success {
		t.Fatalf("add: %s", e)
	}
	// Determined first, so the two are compared against each other and not against literals.
	_, determined, _ := runProjectsCmd(t, getenv, "list")

	lock := filepath.Join(storePath, daemon.RunDir, "daemon.lock")
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatalf("could not stage an unreadable lock: %v", err)
	}
	if got := statusClaim(t, getenv); got != tri.Undetermined {
		t.Fatalf("with an unreadable lock `omw daemon status` says %v, so this test is not "+
			"staging what it claims", got)
	}

	code, out, errOut := runProjectsCmd(t, getenv, "list")
	all := out + errOut

	if code != cli.ExitUndetermined {
		t.Errorf("exited %d where liveness could not be established; want %d, which no determined "+
			"answer uses\n%s", code, cli.ExitUndetermined, all)
	}
	if strings.Contains(all, "examined during this command") {
		t.Errorf("the listing claims it examined the directories BECAUSE nothing was watching, "+
			"which nothing established:\n%s", all)
	}
	// The phrase a listing prints only when it has ESTABLISHED an absence. Asserted through the
	// shared helper so this surface is held to the same list as every other one, rather than to a
	// copy of it that can drift.
	assertNoStaleBecauseNothingIsWatchingClaim(t, "omw projects list (liveness undetermined)", all)
	if !strings.Contains(all, tri.Undetermined.String()) {
		t.Errorf("the third answer is not rendered in words:\n%s", all)
	}
	if !strings.Contains(all, "this is not a report that the daemon is stopped") {
		t.Errorf("the listing does not tell the reader this is not a negative:\n%s", all)
	}
	// The state itself is still there: a person is owed real numbers, not silence.
	if !strings.Contains(all, dir) {
		t.Errorf("the project vanished from the listing because liveness was undetermined:\n%s", all)
	}
	if all == determined {
		t.Errorf("an undetermined liveness and a determined one produce the same listing:\n%s", all)
	}
}

// CRITERION 8 and 9 at the surface, PAIRWISE. A missing directory and an empty one in one listing.
func TestAMissingAndAnEmptyDirectoryDoNotPrintTheSameThing(t *testing.T) {
	_, getenv := projectsSandbox(t)
	base := t.TempDir()
	missing := filepath.Join(base, "missing")
	empty := filepath.Join(base, "empty")
	for _, d := range []string{missing, empty} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if code, _, e := runProjectsCmd(t, getenv, "add", d); code != cli.Success {
			t.Fatalf("add %s: %s", d, e)
		}
	}
	if err := os.RemoveAll(missing); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runProjectsCmd(t, getenv, "list")
	if code != cli.Success {
		t.Fatalf("the listing exited %d because one directory was missing: %s", code, errOut)
	}
	if !strings.Contains(out, missing) {
		t.Fatalf("the missing project was DROPPED from the listing:\n%s", out)
	}

	missingLines := linesFor(out, missing)
	emptyLines := linesFor(out, empty)
	if missingLines == "" || emptyLines == "" {
		t.Fatalf("one of the two projects rendered as nothing:\nmissing=%q empty=%q\n%s",
			missingLines, emptyLines, out)
	}
	if missingLines == emptyLines {
		t.Errorf("a missing directory and an empty one print the same thing:\n%s\n"+
			"Criterion 9: they are different facts about the person's work.", missingLines)
	}

	// AND THE STATE LINE SPECIFICALLY, NOT ONLY THE BLOCK.
	//
	// Found by mutation, not by reading: with the two state phrases edited to be identical, the
	// comparison above STAYED GREEN, because a missing directory prints no `scan:` line and an empty
	// one does — so the blocks still differed on something that is not the distinction criterion 9 is
	// about. A test that passes on an incidental difference is a test that will pass on the day the
	// real one is gone.
	missState, emptyState := fieldLine(missingLines, "state:"), fieldLine(emptyLines, "state:")
	if missState == "" || emptyState == "" {
		t.Fatalf("a project printed no state line at all: missing=%q empty=%q", missState, emptyState)
	}
	if missState == emptyState {
		t.Errorf("the STATE of a missing directory and of an empty one is the same sentence: %q\n"+
			"  Rendering both as \"0 files\", or both as blank, is criterion 9's named failure.", missState)
	}
}

// fieldLine picks one labelled line out of a project's rendered block.
func fieldLine(block, label string) string {
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(l, label) {
			return strings.TrimSpace(strings.TrimPrefix(l, label))
		}
	}
	return ""
}

// linesFor returns the indented block that follows the line naming path, with the path itself
// removed — so two projects at different paths are compared on what was SAID about them and not on
// their names, which of course differ.
func linesFor(out, path string) string {
	lines := strings.Split(out, "\n")
	var b strings.Builder
	for i, l := range lines {
		if l != path {
			continue
		}
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, "    ") {
				break
			}
			b.WriteString(strings.TrimSpace(next) + "\n")
		}
		return b.String()
	}
	return ""
}

// CRITERION 11 at the surface: no project command starts the daemon, and the listing says the
// daemon is not running rather than quietly behaving as if it were.
func TestNoProjectCommandStartsTheDaemonThroughTheCLI(t *testing.T) {
	storePath, getenv := projectsSandbox(t)
	dir := t.TempDir()

	runProjectsCmd(t, getenv, "add", dir)
	runProjectsCmd(t, getenv, "list")
	runProjectsCmd(t, getenv, "remove", dir)
	runProjectsCmd(t, getenv, "add", dir)

	// VERIFIED BY WHAT THE PRODUCT ITSELF USES TO REPORT DAEMON STATE — criterion 11 says exactly
	// that — which since Issue #41 is `omw daemon status` and nothing else. An earlier version
	// asked a heartbeat record this package invented; that fixture would have stayed green while
	// the product's own surface said the opposite, which is how #41 happened three times.
	if got := statusClaim(t, getenv); got != tri.No {
		t.Errorf("after add, list, remove and add, `omw daemon status` says %v, want not running. "+
			"A project command started something on the person's behalf (PRD §4.2).", got)
	}
	_ = storePath
	_, out, _ := runProjectsCmd(t, getenv, "list")
	if !strings.Contains(out, "watching: no") {
		t.Errorf("the listing does not tell the person nothing is watching:\n%s", out)
	}
}

// CRITERION 12 and the second half of 11: with no hub configured, add, list and remove work fully.
//
// The environment here has NO hub value at all. Everything below must complete and exit zero; a
// build that needed a hub for any of it would have to fail or say what is missing, and this asserts
// the first branch of that — it works.
func TestEverythingWorksWithNoHubConfigured(t *testing.T) {
	_, getenv := projectsSandbox(t)
	if getenv("OMW_HUB") != "" {
		t.Fatal("this test's environment has a hub in it, so it is not driving the no-hub case")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"add", dir}, {"list"}, {"remove", dir}, {"list"}} {
		code, _, errOut := runProjectsCmd(t, getenv, args...)
		if code != cli.Success {
			t.Errorf("with no hub configured, 'omw projects %s' exited %d: %s",
				strings.Join(args, " "), code, errOut)
		}
	}
}

// CRITERION 16 through the CLI: a truncated walk and a complete one over the same tree print
// differently, and the depth limit is settable from outside the build.
func TestTheDepthLimitIsSettableAndTruncationShowsInTheListing(t *testing.T) {
	_, getenv := projectsSandbox(t)
	dir := t.TempDir()
	deep := dir
	for i := 0; i < 4; i++ {
		deep = filepath.Join(deep, "level")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "buried.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, e := runProjectsCmd(t, getenv, "add", dir); code != cli.Success {
		t.Fatalf("add: %s", e)
	}

	withDepth := func(n string) func(string) string {
		return func(k string) string {
			if k == projects.DepthEnv {
				return n
			}
			return getenv(k)
		}
	}
	_, full, _ := runProjectsCmd(t, withDepth("8"), "list")
	_, shallow, _ := runProjectsCmd(t, withDepth("2"), "list")

	if full == shallow {
		t.Fatalf("the same tree listed identically at depth 8 and depth 2 — $%s does nothing:\n%s",
			projects.DepthEnv, full)
	}
	if !strings.Contains(shallow, "TRUNCATED") {
		t.Errorf("the truncated listing does not say the walk was truncated:\n%s", shallow)
	}
	if strings.Contains(full, "TRUNCATED") {
		t.Errorf("a walk that completed inside its limit claims truncation:\n%s", full)
	}
}

// `could not determine` and `determined to be nothing` never share an exit code, at this surface.
//
// A store that resolves but is not there is a determined answer (ExitFailure); a store whose
// location cannot be worked out is not (ExitUndetermined). A script must be able to tell them apart.
func TestTheUndeterminedAndTheNegativeAnswersUseDifferentExitCodes(t *testing.T) {
	// A resolvable location with no store at it: determined, and the answer is "there is none".
	absent := filepath.Join(t.TempDir(), "no-store-here")
	noStore := func(k string) string {
		if k == store.PathEnv {
			return absent
		}
		return ""
	}
	determined, _, _ := runProjectsCmd(t, noStore, "list")
	if determined != cli.ExitFailure {
		t.Errorf("listing with no store exited %d, want ExitFailure (%d): "+
			"the answer was determined and it is 'there is no store'", determined, cli.ExitFailure)
	}
	if determined == cli.ExitUndetermined {
		t.Error("'there is no store' and 'I could not determine where your store is' share an exit code")
	}
	if determined == cli.Success {
		t.Error("listing against no store succeeded")
	}
}

// A listing with no projects says so rather than printing nothing, and still states the watching
// answer — the question "is anything keeping up" has an answer even with nothing to keep up with.
func TestAnEmptyListingIsSaidAndNotBlank(t *testing.T) {
	_, getenv := projectsSandbox(t)
	code, out, errOut := runProjectsCmd(t, getenv, "list")
	if code != cli.Success {
		t.Fatalf("exited %d: %s", code, errOut)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("a listing with no projects printed nothing at all")
	}
	if !strings.Contains(out, "no projects") {
		t.Errorf("the empty listing does not say there are no projects:\n%s", out)
	}
	if !strings.Contains(out, "watching:") {
		t.Errorf("the empty listing does not say whether anything is watching:\n%s", out)
	}
}

// The command surface does not create a store, matching the store package's first invariant and
// PRD §4.2. Driven for every subcommand, because it is the LISTING that is most tempting to make
// self-initialising.
func TestNoProjectCommandCreatesAStore(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"add", "."}, {"remove", "."}} {
		root := t.TempDir()
		target := filepath.Join(root, "store")
		getenv := func(k string) string {
			switch k {
			case store.PathEnv:
				return target
			case "XDG_DATA_HOME":
				return filepath.Join(root, "data")
			case "HOME":
				return root
			}
			return ""
		}
		code, _, _ := runProjectsCmd(t, getenv, args...)
		if code == cli.Success {
			t.Errorf("'omw projects %s' succeeded with no store", strings.Join(args, " "))
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("'omw projects %s' created a store at %s. Nothing but 'omw store create' may.",
				strings.Join(args, " "), target)
		}
	}
}

// THE SUITE IN THIS FILE MUST NOT SPAWN THE BINARY WITHOUT SANDBOXING THE DEVICE POINTER.
//
// store_test.go already enforces that structurally across this whole package, and this file adds no
// spawns — every command above runs in process through cli.Run. This test states that as a property
// rather than as a comment, so a future spawn added HERE is caught by a message naming this file's
// own reason, not only by the package-wide check.
func TestTheProjectsTestsSpawnNoProcesses(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "projects_test.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing this file: %v", err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "exec" {
			return true
		}
		found = true
		var b strings.Builder
		printer.Fprint(&b, fset, call)
		t.Errorf("%d: this file spawns a process (%s).\n"+
			"  Every command here runs in process through cli.Run so that no test can inherit the\n"+
			"  developer's environment and repoint their real store at a t.TempDir(). If a spawn is\n"+
			"  genuinely needed, set BOTH \"XDG_DATA_HOME=\" and \"HOME=\" in its env — see\n"+
			"  TestEveryProcessSpawnSandboxesTheDevicePointer in store_test.go.",
			fset.Position(call.Pos()).Line, b.String())
		return true
	})
	if found {
		return
	}
	t.Log("no process spawns in this file; every command runs in process")
}
