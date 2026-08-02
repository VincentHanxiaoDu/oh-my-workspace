package health

import (
	"context"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sourceFilesUnderTest are the files this slice adds. The constraints below are asserted against
// their source, not against prose.
var sourceFilesUnderTest = []string{
	".",                     // internal/health
	"../commands/health.go", // the command
}

// goFilesIn returns the non-test Go files for a path that is either a directory or a single file.
func goFilesIn(t *testing.T, path string) []string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cannot stat %s: %v", path, err)
	}
	if !info.IsDir() {
		return []string{path}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(path, n))
	}
	if len(out) == 0 {
		t.Fatalf("no non-test Go files found under %s; this assertion would pass vacuously", path)
	}
	return out
}

// Criterion 7: with no hub configured, a health run makes no outbound network connection.
//
// WHAT THIS DRIVES AND WHAT IT DOES NOT. It parses the source this slice adds and fails if any of
// it imports a network-capable standard-library package. That is a STRUCTURAL guarantee: code that
// cannot reach a socket API cannot open a connection. It is NOT a packet-level observation — no
// network observer is attached, and the transitive imports of os/exec and friends are not walked.
// Said plainly here and in the pull request body rather than left for a reader to assume.
func TestHealthImportsNoNetworkPackage(t *testing.T) {
	forbidden := []string{"net", "net/http", "net/url", "crypto/tls", "net/rpc", "net/smtp"}
	fset := token.NewFileSet()

	checkedAny := false
	for _, target := range sourceFilesUnderTest {
		for _, file := range goFilesIn(t, target) {
			f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("cannot parse %s: %v", file, err)
			}
			checkedAny = true
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbidden {
					if path == bad || strings.HasPrefix(path, bad+"/") {
						t.Errorf("%s imports %q; health opens no network connection, and the way that is "+
							"guaranteed is by not being able to", file, path)
					}
				}
			}
		}
	}
	if !checkedAny {
		t.Fatal("no files were parsed, so this assertion proved nothing")
	}
}

// The same for the daemon: health never starts it (PRD §4.2, criterion 6). There is no daemon
// package yet, so what is asserted is that health's only process start goes through runCommand —
// i.e. no other os/exec call site exists in this slice's source.
func TestTheOnlyProcessStartIsTheProbe(t *testing.T) {
	var starts []string
	for _, target := range sourceFilesUnderTest {
		for _, file := range goFilesIn(t, target) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("cannot read %s: %v", file, err)
			}
			for i, line := range strings.Split(string(src), "\n") {
				if strings.Contains(line, "exec.Command") || strings.Contains(line, "exec.Start") {
					starts = append(starts, filepath.ToSlash(file)+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
		}
	}
	if len(starts) != 1 {
		t.Errorf("this slice has %d process-start sites, want exactly 1 (the encryption probe):\n  %s",
			len(starts), strings.Join(starts, "\n  "))
	}
	if len(starts) == 1 && !strings.Contains(starts[0], "probe.go") {
		t.Errorf("the one process start is not the platform probe: %s", starts[0])
	}
}

// Criteria 5 and 6, driven: a health run with an injected probe starts NO process at all, and
// creates NO file anywhere it could reach.
func TestHealthRunStartsNothingAndCreatesNothing(t *testing.T) {
	// Count every process the package would start.
	realRun := runCommand
	started := 0
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		started++
		return nil, errNoUsableOutput
	}
	t.Cleanup(func() { runCommand = realRun })

	// Point everything a program conventionally writes to at a directory we can watch.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
	if err := os.MkdirAll(filepath.Join(home, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, home)

	rep := Runner{GOOS: "testos", Checker: &fakeChecker{enabled: false}}.Run(context.Background())
	if err := Write(io.Discard, rep); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if started != 0 {
		t.Errorf("a health run with an injected probe started %d process(es); it must start none — "+
			"in particular it never starts the daemon", started)
	}
	after := snapshot(t, home)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("a health run changed the filesystem; health needs no store and creates none.\nbefore:\n  %s\nafter:\n  %s",
			strings.Join(before, "\n  "), strings.Join(after, "\n  "))
	}

	// And it still reported. Needing no store did not cost it the answer.
	if a, _ := rep.Encryption(); a.Rendered() != "not enabled" {
		t.Errorf("the run reported %q, want \"not enabled\"", a.Rendered())
	}
}

// A real-probe run starts the encryption query and nothing else.
func TestRealProbeStartsOnlyTheEncryptionQuery(t *testing.T) {
	realRun := runCommand
	var started []string
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		started = append(started, name)
		if name == "fdesetup" {
			return []byte("FileVault is On.\n"), nil
		}
		return []byte("crypto_LUKS\n"), nil
	}
	t.Cleanup(func() { runCommand = realRun })

	for _, goos := range []string{"darwin", "linux"} {
		started = nil
		Runner{GOOS: goos}.Run(context.Background())
		if len(started) != 1 {
			t.Errorf("%s: started %v, want exactly the encryption query", goos, started)
		}
	}
}

func snapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}
