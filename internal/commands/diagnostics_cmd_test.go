package commands

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/diagnostics"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// diagSecret is the recognisable ticket body the command-level tests search for.
const diagSecret = "ZZQ-CMD-TICKET-BODY-6d20a7f4-DO-NOT-DISCLOSE"

// diagEnv is a machine with a seeded store, no hub, and HOME and XDG_DATA_HOME inside the test's
// own directory — so nothing here can read or rewrite the developer's real device pointer.
func diagEnv(t *testing.T) (func(string) string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating a store: %v", err)
	}
	if err := s.Put(store.Record{Kind: store.Kind("ticket"), ID: "t1", Data: []byte(diagSecret)}); err != nil {
		t.Fatalf("seeding a ticket: %v", err)
	}
	sandbox := t.TempDir()
	vars := map[string]string{
		store.PathEnv:   root,
		"HOME":          sandbox,
		"XDG_DATA_HOME": sandbox,
	}
	return func(k string) string { return vars[k] }, root
}

func runDiag(t *testing.T, getenv func(string) string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(append([]string{"diagnostics"}, args...), &out, &errOut, getenv)
	return code, out.String(), errOut.String()
}

func diagFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the bundle: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("nothing was searched: the bundle at %s is empty", root)
	}
	return files
}

// The end-to-end story of Issue #20: a person runs the command, gets one artifact, and can read
// what it holds and what it does not before deciding to send it.
func TestDiagnosticsWritesABundleThatSaysWhatItHolds(t *testing.T) {
	getenv, _ := diagEnv(t)
	dest := filepath.Join(t.TempDir(), "bundle")
	code, out, errOut := runDiag(t, getenv, dest)
	if code != cli.Success {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, cli.Success, out, errOut)
	}
	if !strings.Contains(out, dest) {
		t.Errorf("the command does not say where the bundle is:\n%s", out)
	}
	if !strings.Contains(out, "nothing has been sent anywhere") {
		t.Errorf("the command does not tell the person nothing was transmitted:\n%s", out)
	}
	if !strings.Contains(out, "NOT in this bundle") {
		t.Errorf("the command does not say what it withheld:\n%s", out)
	}
	m, err := diagnostics.ReadManifest(dest)
	if err != nil {
		t.Fatalf("reading the manifest a person would read: %v", err)
	}
	if m.BodiesIncluded {
		t.Errorf("a bundle nobody asked bodies for says it includes them")
	}
	for _, body := range diagFiles(t, dest) {
		if strings.Contains(body, diagSecret) {
			t.Errorf("the default bundle written by the command disclosed a ticket body")
		}
	}
}

// Criterion 6: the opt-in is one spelled-out flag, and nothing else turns it on.
func TestBodiesAreOnlyIncludedOnTheExplicitFlag(t *testing.T) {
	getenv, _ := diagEnv(t)
	dir := t.TempDir()

	on := filepath.Join(dir, "with")
	if code, out, errOut := runDiag(t, getenv, on, includeBodiesFlag); code != cli.Success {
		t.Fatalf("exit %d with the flag\n%s\n%s", code, out, errOut)
	}
	if !containsIn(diagFiles(t, on), diagSecret) {
		t.Errorf("bodies were asked for and the ticket body is not in the bundle")
	}

	// Every other flag-shaped thing a person might type is REFUSED, not treated as a broader
	// request. Guessing here means disclosing something nobody asked for.
	for _, guess := range []string{"--all", "--full", "--verbose", "-b", "--bodies"} {
		code, _, errOut := runDiag(t, getenv, filepath.Join(dir, "guess"+guess), guess)
		if code != cli.ExitUsage {
			t.Errorf("%q exited %d, want %d — an option this command does not have must be refused", guess, code, cli.ExitUsage)
		}
		if !strings.Contains(errOut, guess) {
			t.Errorf("%q was refused without naming it:\n%s", guess, errOut)
		}
	}
}

// Criterion 9: producing a bundle starts no daemon, and says the daemon was not running rather than
// reporting something indistinguishable from a running one.
func TestProducingABundleStartsNoDaemon(t *testing.T) {
	getenv, root := diagEnv(t)
	before := daemon.Inspect(root)
	if before.Running == tri.Yes {
		t.Fatalf("a daemon is already running against a store this test just created; the fixture is wrong")
	}
	dest := filepath.Join(t.TempDir(), "bundle")
	if code, out, errOut := runDiag(t, getenv, dest); code != cli.Success {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	after := daemon.Inspect(root)
	if after.Running == tri.Yes {
		t.Errorf("a daemon is running after the bundle was produced; PRD §4.2 says no command starts it")
	}
	if after.PID != before.PID {
		t.Errorf("the daemon's pid changed from %d to %d across a bundle run", before.PID, after.PID)
	}
	// The bundle's own answer is the product's one liveness answer, three-valued, and it does not
	// read as a running daemon.
	body := readDiagFile(t, filepath.Join(dest, "daemon.json"))
	if strings.Contains(body, `"running": "yes"`) {
		t.Errorf("the bundle reports a running daemon where none was started:\n%s", body)
	}
	if !strings.Contains(body, `"running": "no"`) && !strings.Contains(body, tri.Undetermined.String()) {
		t.Errorf("the bundle does not record the daemon's liveness at all:\n%s", body)
	}
}

// Criterion 15: a failure exits non-zero and leaves nothing that could be mistaken for a bundle.
func TestAFailureExitsNonZeroAndLeavesNothing(t *testing.T) {
	getenv, _ := diagEnv(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "bundle")
	if err := os.WriteFile(dest, []byte("something else"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runDiag(t, getenv, dest)
	if code == cli.Success {
		t.Fatalf("writing over an existing path succeeded")
	}
	if code != cli.ExitFailure {
		t.Errorf("exit %d, want %d", code, cli.ExitFailure)
	}
	if !strings.Contains(errOut, "no bundle was produced") {
		t.Errorf("the failure does not say no bundle exists:\n%s", errOut)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the failed run left %d entries behind, want just the file that was already there", len(entries))
	}
}

func TestDiagnosticsWithNoPathIsAUsageError(t *testing.T) {
	getenv, _ := diagEnv(t)
	code, _, errOut := runDiag(t, getenv)
	if code != cli.ExitUsage {
		t.Errorf("exit %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "needs a path") {
		t.Errorf("the usage error does not say what is missing:\n%s", errOut)
	}
}

// Criterion 13, structurally: internal/diagnostics holds no transport at all.
//
// The tree-wide TestEveryListenAndDialIsAUnixSocket already forbids a non-unix listen or dial
// anywhere under internal/. This is narrower and stricter for the one package that must never reach
// out at all: it may not import a transport package, so there is nothing there to dial WITH.
// STRUCTURAL, and marked as such: it proves the package cannot open a connection, not that a
// particular run did not.
func TestTheDiagnosticsPackageImportsNoTransport(t *testing.T) {
	banned := map[string]bool{
		"net": true, "net/http": true, "net/url": true, "net/rpc": true,
		"crypto/tls": true, "os/exec": true,
	}
	dir := filepath.Join(repoRoot(t), "internal", "diagnostics")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		checked++
		file, perr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parsing %s: %v", e.Name(), perr)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if banned[path] {
				t.Errorf("%s imports %q; producing a bundle opens no connection and starts no process", e.Name(), path)
			}
		}
	}
	if checked == 0 {
		// A CHECK THAT EXAMINED NOTHING IS NOT A PASS.
		t.Fatalf("no product files were examined in %s", dir)
	}
}

func containsIn(files map[string]string, s string) bool {
	for _, body := range files {
		if strings.Contains(body, s) {
			return true
		}
	}
	return false
}

func readDiagFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// A bundle is never written to a path relative to wherever the person is standing.
//
// THIS TEST EXISTS BECAUSE THE SUITE FOUND THE BUG. The criterion-5 sweep in liveness_test.go runs
// every registered command with `{name}`, `{name} list` and `{name} status`, and the first version
// of this command took those bare words as destinations and wrote three real bundles — facts about
// the machine, in the source tree, on every `go test`. A command that can materialise an artifact
// somewhere unexpected is a command that can materialise it somewhere a person did not mean to put
// it, which for this artifact is the whole risk.
func TestARelativeDestinationIsRefusedAndWritesNothing(t *testing.T) {
	getenv, _ := diagEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"list", "status", "bundle", "./here", "../up"} {
		code, out, errOut := runDiag(t, getenv, rel)
		if code != cli.ExitUsage {
			t.Errorf("`omw diagnostics %s` exited %d, want %d", rel, code, cli.ExitUsage)
		}
		if !strings.Contains(errOut, "absolute") {
			t.Errorf("the refusal of %q does not say why:\n%s%s", rel, out, errOut)
		}
		if _, err := os.Lstat(filepath.Join(cwd, rel)); err == nil {
			t.Errorf("`omw diagnostics %s` created %s in the working directory", rel, rel)
		}
	}
	// And the sweep's own argument shapes leave nothing behind, checked the way the sweep runs them.
	for _, args := range [][]string{{"diagnostics"}, {"diagnostics", "list"}, {"diagnostics", "status"}} {
		var out, errOut bytes.Buffer
		cli.Run(args, &out, &errOut, getenv)
	}
	for _, name := range []string{"list", "status", "diagnostics"} {
		if _, err := os.Lstat(filepath.Join(cwd, name)); err == nil {
			t.Errorf("the command sweep left %s in %s", name, cwd)
		}
	}
}
