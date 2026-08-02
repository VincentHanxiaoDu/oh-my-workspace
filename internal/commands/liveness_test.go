package commands

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// storeThatExists makes a real store and returns its root. Liveness is derived from a store now, so
// a fixture that names no store is asking a different question than the one under test.
func storeThatExists(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("could not create a store to test against: %v", err)
	}
	return root
}

// ---------------------------------------------------------------------------
// The surfaces, in one list
// ---------------------------------------------------------------------------

// daemonReportingSurface is one command that reports daemon state as part of doing its job.
//
// THE LIST IS THE POINT. Issue #41 happened three times because each surface asserted its own text
// in isolation; the tests below run every entry here against the SAME daemon and compare them with
// `omw daemon status`, so a new surface joins the agreement test by being added to one slice.
type daemonReportingSurface struct {
	name string
	args []string
}

var daemonReportingSurfaces = []daemonReportingSurface{
	{"omw visibility show", []string{"visibility", "show", "note-1"}},
	{"omw note versions", []string{"note", "versions", "note-1"}},
	{"omw note show", []string{"note", "show", "note-1"}},
	{"omw note read", []string{"note", "read", "note-1@v1"}},
	{"omw note search", []string{"note", "search", "quota"}},
}

// liveEnv is a machine with a hub configured — so the daemon is the next thing each surface needs —
// pointed at one particular store.
func liveEnv(root string) func(string) string {
	vars := map[string]string{
		envHub:        "hub.example.internal",
		store.PathEnv: root,
	}
	return func(k string) string { return vars[k] }
}

// claimOf reads back what a surface CLAIMED about the daemon, as one of the three answers.
//
// It reads the machine-readable codes rather than prose, because prose is what drifted. A surface
// that got past its liveness check without saying anything about the daemon has claimed the daemon
// is running: that is what "past the check" means, and it is the claim a person acts on.
func claimOf(out, errOut string) tri.Value {
	all := out + errOut
	switch {
	case strings.Contains(all, codeDaemonUndetermined):
		return tri.Undetermined
	case strings.Contains(all, hub.ErrDaemonNotRunning.Code):
		return tri.No
	default:
		return tri.Yes
	}
}

func runSurface(t *testing.T, getenv func(string) string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = cli.Run(args, &o, &e, getenv)
	return code, o.String(), e.String()
}

// statusClaim runs `omw daemon status` the way a person does and reads its answer back out of the
// rendering they see, rather than calling daemon.Inspect directly.
//
// THAT DISTINCTION MATTERS. Criterion 2 is that the SURFACES agree with `omw daemon status`, and a
// comparison against Inspect would still pass if `daemon status` rendered the opposite of what it
// was told. Both sides of this comparison are things a person reads.
func statusClaim(t *testing.T, getenv func(string) string) tri.Value {
	t.Helper()
	_, out, errOut := runSurface(t, getenv, "daemon", "status")
	all := out + errOut
	switch {
	case strings.Contains(all, "daemon:   "+tri.Undetermined.String()):
		return tri.Undetermined
	case strings.Contains(all, "daemon:   not running"):
		return tri.No
	case strings.Contains(all, "daemon:   running"):
		return tri.Yes
	default:
		t.Fatalf("`omw daemon status` said nothing this test can read as an answer:\n%s%s", out, errOut)
		return tri.Undetermined
	}
}

// ---------------------------------------------------------------------------
// Criteria 2, 3, 5 and 6 — the surfaces agree, with a daemon actually running
// ---------------------------------------------------------------------------

// TestEveryDaemonReportingSurfaceAgreesWithDaemonStatus is criterion 6, and criteria 2, 3 and 5
// inside it.
//
// IT DRIVES BOTH DIRECTIONS AGAINST THE SAME STORE, IN ONE TEST. A test that only started a daemon
// would pass against a probe hardcoded to `true`, and the one that only checked the stopped case is
// what shipped: the placeholder answered "not running" unconditionally and every branch's own tests
// were green. Neither half is a test on its own, so they are not separable here.
//
// The daemon is a REAL separate process started by the real binary — not a double, and not this
// process taking its own lock, which would prove nothing about what a person's `omw daemon start`
// leaves behind.
func TestEveryDaemonReportingSurfaceAgreesWithDaemonStatus(t *testing.T) {
	bin := buildOMW(t)
	root := storeThatExists(t)
	getenv := liveEnv(root)

	// --- DIRECTION ONE: nothing running. ------------------------------------
	if got := statusClaim(t, getenv); got != tri.No {
		t.Fatalf("before anything was started `omw daemon status` says %v; the fixture is wrong, not the product", got)
	}
	for _, s := range daemonReportingSurfaces {
		code, out, errOut := runSurface(t, getenv, s.args...)
		if claim := claimOf(out, errOut); claim != tri.No {
			t.Errorf("with no daemon running, `%s` claims the daemon is %v; `omw daemon status` says not running\n%s%s",
				s.name, claim, out, errOut)
		}
		if code == cli.Success {
			t.Errorf("`%s` succeeded with no daemon running:\n%s%s", s.name, out, errOut)
		}
	}

	// --- DIRECTION TWO: a real daemon, started the way a person starts it. ---
	start := runBinary(t, bin, root, "daemon", "start")
	if start.code != 0 {
		t.Fatalf("`omw daemon start` exited %d\nstdout: %s\nstderr: %s", start.code, start.stdout, start.stderr)
	}
	stopped := false
	stop := func() {
		if !stopped {
			runBinary(t, bin, root, "daemon", "stop")
			stopped = true
		}
	}
	t.Cleanup(stop)

	if got := statusClaim(t, getenv); got != tri.Yes {
		t.Fatalf("`omw daemon start` returned successfully but `omw daemon status` says %v", got)
	}
	for _, s := range daemonReportingSurfaces {
		code, out, errOut := runSurface(t, getenv, s.args...)
		if claim := claimOf(out, errOut); claim != tri.Yes {
			t.Errorf("WITH A DAEMON RUNNING, `%s` claims the daemon is %v while `omw daemon status` says running.\n"+
				"  This is Issue #41 exactly: one of the two is lying and the person cannot tell which.\n%s%s",
				s.name, claim, out, errOut)
		}
		// CRITERION 5. No surface may explain itself by an absence that is not there.
		assertNoStaleBecauseNothingIsWatchingClaim(t, s.name, out+errOut)
		// The surface still has no hub transport in this build, so it reports the HUB as
		// unreachable — an undetermined answer about the hub, which is a different sentence and a
		// different subject from an absent daemon. What must not happen is a daemon claim.
		if code == cli.ExitFailure && strings.Contains(out+errOut, hub.ErrDaemonNotRunning.Code) {
			t.Errorf("`%s` failed on the daemon while one was running:\n%s%s", s.name, out, errOut)
		}
	}

	// CRITERION 5, over EVERY registered command rather than the ones this file listed — the shape
	// that catches the next surface before its author has to know this test exists.
	for _, c := range cli.Commands() {
		if c.Name == "daemon" {
			continue // `omw daemon` is the reference answer, and its usage text discusses the state.
		}
		for _, args := range [][]string{{c.Name}, {c.Name, "list"}, {c.Name, "status"}} {
			_, out, errOut := runSurface(t, getenv, args...)
			assertNoStaleBecauseNothingIsWatchingClaim(t, "omw "+strings.Join(args, " "), out+errOut)
		}
	}

	// --- BACK TO DIRECTION ONE, after a real stop. --------------------------
	// Not a repetition of the first block: this is the transition the person actually performs, and
	// a probe that latched to "running" once it had seen a daemon would pass everything above.
	stop()
	if got := statusClaim(t, getenv); got != tri.No {
		t.Fatalf("after `omw daemon stop` returned, `omw daemon status` says %v", got)
	}
	for _, s := range daemonReportingSurfaces {
		_, out, errOut := runSurface(t, getenv, s.args...)
		if claim := claimOf(out, errOut); claim != tri.No {
			t.Errorf("after the daemon was stopped, `%s` still claims the daemon is %v\n%s%s",
				s.name, claim, out, errOut)
		}
	}
}

// assertNoStaleBecauseNothingIsWatchingClaim is criterion 5's assertion, in one place.
//
// The wordings are the ones two refused pull requests actually printed (#29's "this listing is the
// store on disk, not a live inbox" and #35's "nothing is watching between commands"), plus the
// machine-readable code that carried them. A surface may say it is showing the store on disk; what
// it may not do is justify that by an absence contradicted by `omw daemon status`.
func assertNoStaleBecauseNothingIsWatchingClaim(t *testing.T, what, output string) {
	t.Helper()
	for _, forbidden := range []string{
		hub.ErrDaemonNotRunning.Code,
		"daemon: not running",
		"nothing is watching",
		"not a live",
	} {
		if strings.Contains(output, forbidden) {
			t.Errorf("with a daemon running, `%s` printed %q — a claim that depends on the wrong answer (criterion 5):\n%s",
				what, forbidden, output)
		}
	}
}

// ---------------------------------------------------------------------------
// Criterion 4 — undetermined is not "no"
// ---------------------------------------------------------------------------

// TestALivenessThatCannotBeEstablishedIsUndeterminedAndNotANo drives the case the placeholder could
// not represent at all.
//
// THE STORE IS BROKEN IN A REAL WAY rather than by stubbing the probe: the lock the daemon package
// reads is replaced by a directory, so opening it fails and `internal/daemon` genuinely cannot
// establish whether anything holds the store. That is one of the two cases criterion 4 names, and
// it reaches every surface through the same call `omw daemon status` uses.
func TestALivenessThatCannotBeEstablishedIsUndeterminedAndNotANo(t *testing.T) {
	root := storeThatExists(t)
	lock := filepath.Join(root, daemon.RunDir, "daemon.lock")
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatalf("could not stage an unreadable lock: %v", err)
	}
	getenv := liveEnv(root)

	if got := statusClaim(t, getenv); got != tri.Undetermined {
		t.Fatalf("with an unreadable lock `omw daemon status` says %v, so this test is not staging what it claims", got)
	}

	for _, s := range daemonReportingSurfaces {
		code, out, errOut := runSurface(t, getenv, s.args...)
		all := out + errOut
		if claim := claimOf(out, errOut); claim != tri.Undetermined {
			t.Errorf("`%s` reports %v where liveness could not be established; a confident negative is the defect #41 removes\n%s",
				s.name, claim, all)
		}
		if code != cli.ExitUndetermined {
			t.Errorf("`%s` exited %d where liveness could not be established; want %d, which no determined answer uses\n%s",
				s.name, code, cli.ExitUndetermined, all)
		}
		// DISTINGUISHABLE IN THE OUTPUT, not only in the exit code — a person reading the terminal
		// must not be able to mistake it for a stopped daemon.
		if !strings.Contains(all, tri.Undetermined.String()) {
			t.Errorf("`%s` does not render the third answer in words:\n%s", s.name, all)
		}
		if strings.Contains(all, hub.ErrDaemonNotRunning.Error()) {
			t.Errorf("`%s` says the daemon is not running when nothing established that:\n%s", s.name, all)
		}
		// A REASON, not just a shrug (criterion 4).
		if !strings.Contains(all, "lock") && !strings.Contains(all, "could not be determined") {
			t.Errorf("`%s` reports undetermined without a reason:\n%s", s.name, all)
		}
		if !strings.Contains(all, "this is not a report that the daemon is stopped") {
			t.Errorf("`%s` does not tell the reader that this is not a negative:\n%s", s.name, all)
		}
	}
}

// TestTheThirdAnswerAndTheNegativeShareNeitherWordingNorExitCode is the twin of the test above,
// stated as a property of the two renderings rather than of one store.
func TestTheThirdAnswerAndTheNegativeShareNeitherWordingNorExitCode(t *testing.T) {
	var noOut, undOut bytes.Buffer
	noEnv := cli.Env{Stdout: &noOut, Stderr: &noOut}
	undEnv := cli.Env{Stdout: &undOut, Stderr: &undOut}

	noCode := reportDaemonNotLive(noEnv, "omw x", tri.No, "")
	undCode := reportDaemonNotLive(undEnv, "omw x", tri.Undetermined, "the lock could not be opened")

	if noCode == undCode {
		t.Errorf("a determined negative and an undetermined answer both exit %d; the project's standing rule is that they never share a code", noCode)
	}
	if noCode != cli.ExitFailure || undCode != cli.ExitUndetermined {
		t.Errorf("exit codes are %d and %d; want %d and %d", noCode, undCode, cli.ExitFailure, cli.ExitUndetermined)
	}
	if strings.Contains(undOut.String(), hub.ErrDaemonNotRunning.Error()) {
		t.Errorf("the undetermined rendering contains the negative's sentence:\n%s", undOut.String())
	}
	if !strings.Contains(undOut.String(), "the lock could not be opened") {
		t.Errorf("the undetermined rendering dropped the reason it was given:\n%s", undOut.String())
	}
	if undOut.String() == noOut.String() {
		t.Error("the two answers render identically")
	}
}

// ---------------------------------------------------------------------------
// Criterion 1 — one definition
// ---------------------------------------------------------------------------

// controlSocketFindings reports every place in one parsed file that derives, names or stats a
// control-socket path.
//
// It is a function rather than an inline loop so that the test can point it at a KNOWN MATCH and
// confirm it fires. A structural search that has stopped matching is indistinguishable from a
// codebase that has stopped offending, and this project has shipped both.
func controlSocketFindings(fset *token.FileSet, file *ast.File, rel string) []string {
	var found []string
	note := func(pos token.Pos, why string) {
		found = append(found, rel+":"+strconv.Itoa(fset.Position(pos).Line)+": "+why)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return true
			}
			lit := strings.ToLower(v.Value)
			switch {
			case strings.Contains(lit, "control_socket"):
				note(v.Pos(), "names an OMW_CONTROL_SOCKET-style environment variable "+v.Value)
			case strings.Contains(lit, "control.sock"), strings.Contains(lit, "control-sock"):
				note(v.Pos(), "names the control socket's file name "+v.Value)
			case strings.HasSuffix(strings.TrimSuffix(lit, `"`), ".sock"):
				note(v.Pos(), "names a socket path literal "+v.Value)
			}
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			// Stat'ing a thing whose name says "socket" is the placeholder's exact shape, and it
			// is a guess wherever it appears outside the package that owns the path.
			if ident.Name == "os" && (sel.Sel.Name == "Stat" || sel.Sel.Name == "Lstat") {
				for _, a := range v.Args {
					if mentionsSocket(a) {
						note(v.Pos(), "stats a socket path")
					}
				}
			}
		}
		return true
	})
	return found
}

func mentionsSocket(e ast.Expr) bool {
	hit := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(strings.ToLower(id.Name), "sock") {
			hit = true
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && strings.Contains(strings.ToLower(sel.Sel.Name), "sock") {
			hit = true
		}
		return true
	})
	return hit
}

// TestNoPackageOutsideDaemonDerivesAControlSocketPath is criterion 1.
//
// ONE DEFINITION IS A PROPERTY OF THE TREE, NOT OF THESE TWO FILES. Replacing the two placeholders
// fixes today; this is what stops the fourth guess, because the next surface that reaches for
// `OMW_CONTROL_SOCKET` or joins a `.sock` name onto a store path fails here before its author has
// read this Issue.
func TestNoPackageOutsideDaemonDerivesAControlSocketPath(t *testing.T) {
	// THE CONTROL, AND IT COMES FIRST. A grep-style test that matches nothing passes vacuously, and
	// that is how a search gets quietly broken: point it at a known match and require it to fire
	// before believing anything it says about the tree.
	//
	// The fixture is the placeholder this Issue removed, verbatim in shape.
	const knownMatch = `package p

import "os"

const envSocket = "OMW_CONTROL_SOCKET"

func daemonRunning(getenv func(string) string) bool {
	sock := getenv(envSocket)
	_, err := os.Stat(sock)
	return err == nil
}
`
	probeFset := token.NewFileSet()
	probeFile, err := parser.ParseFile(probeFset, "known_match.go", knownMatch, 0)
	if err != nil {
		t.Fatalf("the control fixture does not parse, so this test proves nothing: %v", err)
	}
	if hits := controlSocketFindings(probeFset, probeFile, "known_match.go"); len(hits) < 2 {
		t.Fatalf("the search does not fire on the very code this Issue removed (%d finding(s)); "+
			"a green run below would mean nothing. Fix the search, do not delete the test.\n%v", len(hits), hits)
	}

	root := repoRoot(t)
	scanned := 0
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return werr
		}
		rel, _ := filepath.Rel(root, path)
		// `internal/daemon` OWNS the path and is the one place allowed to derive it. Test files are
		// allowed too: a test may legitimately bind a socket it created itself.
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/daemon/") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		scanned++
		for _, f := range controlSocketFindings(fset, file, rel) {
			t.Errorf("%s\n"+
				"  Whether the daemon is running against a store has ONE definition, and it is\n"+
				"  daemon.Inspect (Issue #41, PRD §4.3). The socket path is chosen by socketFor,\n"+
				"  which falls back to a per-user runtime directory above the sun_path limit — a\n"+
				"  caller reconstructing it will disagree with the daemon about a daemon that is\n"+
				"  running. Call daemonLiveness instead.", f)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	// The second control: the walk itself must have looked at something.
	if scanned == 0 {
		t.Fatal("the walk examined no product files outside internal/daemon; its pass proves nothing")
	}
	t.Logf("examined %d product file(s) outside internal/daemon", scanned)
}
