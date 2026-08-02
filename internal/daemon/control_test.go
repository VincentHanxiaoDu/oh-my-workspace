package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// refusing builds an Options override whose owner-only confirmation gives the answer named.
//
// THE PLATFORM IS NOT NAMED ANYWHERE IN THESE TESTS. Criterion 25 is about a platform where
// owner-only permissions cannot be confirmed; `if runtime.GOOS == "windows" { skip }` would mean
// the refusal is never exercised on either platform this product ships for. Driving the seam runs
// the refusal path on every machine the suite runs on.
func refusing(state tri.Value, why string) func(*Options) {
	return func(o *Options) {
		o.ConfirmOwnerOnly = func(path string) (tri.Value, string) { return state, why }
	}
}

// TestTheControlAPIDoesNotOpenWhenOwnerOnlyCannotBeConfirmed is criteria 22, 23 and 24.
func TestTheControlAPIDoesNotOpenWhenOwnerOnlyCannotBeConfirmed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state tri.Value
		why   string
	}{
		{"could not be confirmed", tri.Undetermined, "this system does not report an owner for the socket"},
		{"confirmed as not owner-only", tri.No, "the socket is reachable by other users"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newTestStore(t)
			d := startTestDaemon(t, root, refusing(tc.state, tc.why))

			if d.Control() != nil {
				t.Fatal("the control API opened without a confirmation; §4.6 says it opens only when it can prove its socket is owner-only")
			}
			state, detail := d.ControlState()
			if state == tri.Yes {
				t.Errorf("the control API reports itself open after declining to open")
			}
			if state != tc.state {
				t.Errorf("a %v confirmation was reported as %v; the two negatives are different answers (§4.3)", tc.state, state)
			}
			if !strings.Contains(detail, tc.why) {
				t.Errorf("the refusal does not carry what was found: %q", detail)
			}

			// CRITERION 24: nothing was opened as a substitute. No socket exists, and no other
			// listener was created — see TestNoNetworkTransportExistsInThisPackage for the second
			// half of that, which no runtime assertion can cover.
			if _, err := os.Stat(pathsFor(root).socket); !os.IsNotExist(err) {
				t.Errorf("a socket was left at %s after the control API declined to open", pathsFor(root).socket)
			}
		})
	}
}

// TestTheDirectoryIsConfirmedBeforeAnythingListens is criterion 22's "before".
//
// ALSO FOUND BY MUTATION: deleting the directory confirmation left every test green, because the
// tests refused for every path and the socket's own confirmation caught it a moment later. That
// moment is the whole of criterion 22 — it is the difference between confirming before opening and
// confirming after. Here the directory fails its confirmation and the socket would pass, so code
// that checks the directory first refuses and code that checks it late opens the control API.
func TestTheDirectoryIsConfirmedBeforeAnythingListens(t *testing.T) {
	root := newTestStore(t)
	p := pathsFor(root)
	d := startTestDaemon(t, root, func(o *Options) {
		o.ConfirmOwnerOnly = func(path string) (tri.Value, string) {
			if path == p.socketDir {
				return tri.No, "the run directory is traversable by other users"
			}
			return tri.Yes, ""
		}
	})

	if d.Control() != nil {
		t.Fatal("the control API opened inside a directory that failed its confirmation; §4.6 confirms BEFORE opening, not after")
	}
	if _, err := os.Stat(p.socket); !os.IsNotExist(err) {
		t.Errorf("a socket was created at %s despite its directory failing confirmation; nothing may listen before the confirmation passes", p.socket)
	}
	state, detail := d.ControlState()
	if state != tri.No {
		t.Errorf("a directory confirmed as NOT owner-only was reported as %v", state)
	}
	if !strings.Contains(detail, "the directory the socket would live in") {
		t.Errorf("the refusal does not say it was the directory that failed: %q", detail)
	}
}

// TestTheSocketItselfIsConfirmedAndNotOnlyItsDirectory closes a gap this test file had.
//
// FOUND BY MUTATION. Making the socket's own confirmation pass unconditionally left every test
// green, because the directory is confirmed first and the tests above refuse there — so the second
// confirmation was never reached and the assertion "the socket is confirmed" was resting on
// nothing. This drives the case the mutation exposed: a directory that confirms and a socket that
// does not.
func TestTheSocketItselfIsConfirmedAndNotOnlyItsDirectory(t *testing.T) {
	root := newTestStore(t)
	p := pathsFor(root)
	d := startTestDaemon(t, root, func(o *Options) {
		o.ConfirmOwnerOnly = func(path string) (tri.Value, string) {
			if path == p.socket {
				return tri.No, "the socket is reachable by other users"
			}
			return tri.Yes, ""
		}
	})

	if d.Control() != nil {
		t.Fatal("the control API opened with a socket that failed its own confirmation; confirming the directory is not confirming the socket")
	}
	state, detail := d.ControlState()
	if state != tri.No {
		t.Errorf("a socket confirmed as NOT owner-only was reported as %v", state)
	}
	if !strings.Contains(detail, "the control socket") {
		t.Errorf("the refusal does not say it was the socket that failed: %q", detail)
	}
	if _, err := os.Stat(p.socket); !os.IsNotExist(err) {
		t.Errorf("a socket that failed its confirmation was left in place at %s, where a later reader could take it for an open control API", p.socket)
	}
}

// TestTheRefusalIsWordedApartFromNotRunningAndFromRunningNormally is criterion 23 stated exactly as
// the Issue states it: three renderings a person can tell apart.
func TestTheRefusalIsWordedApartFromNotRunningAndFromRunningNormally(t *testing.T) {
	render := func(root string) string {
		var b strings.Builder
		rep := Inspect(root)
		if _, err := rep.WriteTo(&b); err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(b.String(), root, "<store>")
	}

	notRunning := render(newTestStore(t))

	normalRoot := newTestStore(t)
	normal := startTestDaemon(t, normalRoot)
	serveInBackground(t, normal)
	if normal.Control() == nil {
		t.Skipf("the control API did not open in this environment (%s), so 'running normally' cannot be rendered here", func() string {
			_, d := normal.ControlState()
			return d
		}())
	}
	runningNormally := render(normalRoot)

	refusedRoot := newTestStore(t)
	refused := startTestDaemon(t, refusedRoot, refusing(tri.Undetermined, "owner could not be read"))
	serveInBackground(t, refused)
	refusedText := render(refusedRoot)

	cases := map[string]string{
		"not running":      notRunning,
		"running normally": runningNormally,
		"control refused":  refusedText,
	}
	names := []string{"not running", "running normally", "control refused"}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if cases[a] == cases[b] {
				t.Errorf("%q and %q render identically:\n%s", a, b, cases[a])
			}
			if lineOf(cases[a], "control:") == lineOf(cases[b], "control:") {
				t.Errorf("%q and %q give the same control line %q", a, b, lineOf(cases[a], "control:"))
			}
		}
	}
	// AND THE REFUSAL SAYS WHY. A control line that reads "not open" with no reason is
	// indistinguishable to a person from a daemon that is simply not there.
	if !strings.Contains(refusedText, tri.Undetermined.String()) {
		t.Errorf("the refusal does not report the confirmation as undetermined:\n%s", refusedText)
	}
}

// TestTheDaemonRunsWithNoControlAPI is Issue #1's criterion 14, carried forward and made drivable:
// the control API declining removes an interface, not the product.
func TestTheDaemonRunsWithNoControlAPI(t *testing.T) {
	root := newTestStore(t)
	d := startTestDaemon(t, root, refusing(tri.Undetermined, "cannot read the owner"))
	done := serveInBackground(t, d)

	if rep := d.Report(); rep.Running != tri.Yes || rep.Healthy != tri.Yes {
		t.Errorf("with no control API the daemon reports running=%v healthy=%v; it should be running normally otherwise",
			rep.Running, rep.Healthy)
	}
	d.Stop()
	<-done
	d.Close()
	if rep := Inspect(root); rep.LastRun != EndingStopped {
		t.Errorf("a daemon that ran without a control API recorded its ending as %q; expected %q", rep.LastRun, EndingStopped)
	}
}

// TestAnUndeterminedControlStateIsNeverReportedAsClosed is criterion 26.
func TestAnUndeterminedControlStateIsNeverReportedAsClosed(t *testing.T) {
	root := newTestStore(t)
	p := pathsFor(root)
	if err := ensureRunDir(p); err != nil {
		t.Fatal(err)
	}
	// A record from a daemon that says its control API opened, with nothing answering. Neither
	// "open" nor "closed" was established.
	rec := runRecord{Format: runFormat, PID: 1, StartedAt: time.Now().UTC(), Phase: phaseRun, Control: "open"}
	state, detail := controlFromDisk(p, tri.Yes, rec, nil)
	if state != tri.Undetermined {
		t.Errorf("a control API that did not answer was reported as %v; criterion 26 says undetermined, never closed", state)
	}
	if detail == "" {
		t.Error("an undetermined control state was reported as silence")
	}

	// And a record that cannot be read at all is likewise undetermined, not closed.
	state, detail = controlFromDisk(p, tri.Yes, runRecord{}, errRunRecordUnreadable)
	if state != tri.Undetermined || detail == "" {
		t.Errorf("an unreadable run record gave control state %v (%q); expected undetermined, said out loud", state, detail)
	}
}

// TestTheControlAPIAndTheCLIReportTheSameState is criterion 14.
//
// Both are taken at the same moment against the same daemon, and both are RENDERED, because the
// criterion is about what a person is told and not only about what a struct holds.
func TestTheControlAPIAndTheCLIReportTheSameState(t *testing.T) {
	root := newTestStore(t)
	d := startTestDaemon(t, root)
	serveInBackground(t, d)
	if d.Control() == nil {
		_, why := d.ControlState()
		t.Skipf("the control API did not open in this environment, so the two cannot be compared: %s", why)
	}

	overAPI, err := queryControl(d.Control().Path())
	if err != nil {
		t.Fatalf("the control API did not answer: %v", err)
	}
	viaCLI := Inspect(root) // what `omw daemon status` renders

	var a, b strings.Builder
	if _, err := overAPI.WriteTo(&a); err != nil {
		t.Fatal(err)
	}
	if _, err := viaCLI.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Errorf("the control API and the CLI report different states for the same daemon.\nover the control API:\n%s\nvia the CLI:\n%s", a.String(), b.String())
	}
}

// TestTheControlAPIIsAUnixSocket is criterion 21 as far as a unit test can reach it: the transport
// has no address an off-machine connection can name.
//
// HONESTLY LABELLED. This does not send a packet from another machine. It asserts the property
// that makes such a packet impossible — the listener is AF_UNIX and its address is a path inside
// an owner-only directory — plus the source-level assertion below that no other listener exists.
func TestTheControlAPIIsAUnixSocket(t *testing.T) {
	root := newTestStore(t)
	d := startTestDaemon(t, root)
	if d.Control() == nil {
		_, why := d.ControlState()
		t.Skipf("the control API did not open in this environment: %s", why)
	}
	ln := d.Control().listener
	if got := ln.Addr().Network(); got != "unix" {
		t.Errorf("the control API listens on a %q address; criterion 21 requires a transport with no off-machine address", got)
	}
	p := pathsFor(root)
	if !strings.HasPrefix(ln.Addr().String(), p.socketDir) {
		t.Errorf("the control socket is at %s, outside the directory whose owner-only permissions were confirmed (%s)", ln.Addr(), p.socketDir)
	}
	// AND THE CONFIRMATION WAS ABOUT THE PLACE THE SOCKET ACTUALLY IS. A confirmation of some
	// other directory would pass every assertion above while proving nothing about reachability.
	if state, why := confirmOwnerOnly(p.socketDir); state != tri.Yes {
		t.Errorf("the directory the socket lives in is not confirmed owner-only (%v): %s", state, why)
	}
	if state, why := confirmOwnerOnly(p.socket); state != tri.Yes {
		t.Errorf("the socket itself is not confirmed owner-only (%v): %s", state, why)
	}
}

// TestTheOwnerOnlyConfirmationIsARealProbe drives the real confirmation rather than the injected
// one, so that the seam the other tests use is known to stand for something.
//
// IT PROBES ITS ENVIRONMENT RATHER THAN NAMING IT. If the filesystem under the test does not keep
// the mode it is given — some do not — the test says so and skips, instead of asserting something
// about a platform it guessed at.
func TestTheOwnerOnlyConfirmationIsARealProbe(t *testing.T) {
	dir := t.TempDir()
	tight := filepath.Join(dir, "tight")
	if err := os.WriteFile(tight, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state, why := confirmOwnerOnly(tight); state != tri.Yes {
		t.Skipf("this filesystem does not report owner-only permissions for a 0600 file (%v: %s), so the probe cannot be exercised here", state, why)
	}

	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(loose, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o666); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(loose)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 == 0 {
		t.Skip("this filesystem does not keep group and other permission bits, so a widened file cannot be created to probe against")
	}
	state, why := confirmOwnerOnly(loose)
	if state != tri.No {
		t.Errorf("a world-writable file was reported as %v; a DETERMINED negative is required here (%s)", state, why)
	}
	if why == "" {
		t.Error("the negative came with no reason")
	}

	// A path that is not there at all: undetermined, never a determined "no". Nothing was
	// inspected, so nothing was established.
	if state, why := confirmOwnerOnly(filepath.Join(dir, "absent")); state != tri.Undetermined || why == "" {
		t.Errorf("an absent path gave %v (%q); expected undetermined, said out loud", state, why)
	}
}

// TestNoNetworkTransportExistsInThisPackage is criterion 24, and criterion 19's local half.
//
// IT READS THIS PACKAGE'S OWN SYNTAX TREE. A runtime test cannot prove the absence of a fallback:
// it can only fail to trigger one. This asserts the property directly — every net.Listen and every
// net.Dial in this package names "unix" as a literal — so a fallback added later, or an outbound
// connection added with no hub configured, fails here rather than in somebody's packet capture.
func TestNoNetworkTransportExistsInThisPackage(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("could not read this package's own source: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no package was parsed, so this test asserted nothing")
	}
	checked := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "net" {
					return true
				}
				if !strings.HasPrefix(sel.Sel.Name, "Listen") && !strings.HasPrefix(sel.Sel.Name, "Dial") {
					return true
				}
				checked++
				if len(call.Args) == 0 {
					t.Errorf("%s: net.%s with no network argument; this package must name \"unix\" explicitly", name, sel.Sel.Name)
					return true
				}
				network := call.Args[0]
				lit, ok := network.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || lit.Value != `"unix"` {
					t.Errorf("%s: net.%s uses a network that is not the literal \"unix\"; criterion 24 forbids any other transport here",
						name, sel.Sel.Name)
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Error("no net.Listen or net.Dial call was found at all, so this test did not exercise the rule it states")
	}
}
