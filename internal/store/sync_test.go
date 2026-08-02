package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// THE POINT OF THIS FILE, AND WHY IT NAMES NO OPERATING SYSTEM.
//
// Criterion 7 forbids a check that works on macOS and silently passes everything on Linux. A test
// that says `if runtime.GOOS == "darwin" { ...expect refusal... }` would pass on a build where the
// probe does nothing on Linux, which is the exact defect. So every case here BUILDS a synthetic
// synchronising root out of the markers a real client writes — a `.dropbox` file, an `iCloud
// Drive` placeholder, OneDrive's GUID file, a roaming profile's `ntuser.dat` — inside a temporary
// directory on whatever platform the test is running on, and asserts refusal there. The assertions
// are identical on macOS and on Linux, and `go test ./...` on either one is the whole of criterion 7.

// syncRoot builds a directory that looks, on disk, like the given provider's synced root, and
// returns a path to a would-be store inside it.
func syncRoot(t *testing.T, rootName string, markers ...string) (root, inside string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), rootName)
	inside = filepath.Join(root, "projects", "omw-store")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("building the synthetic sync root: %v", err)
	}
	for _, m := range markers {
		if err := os.WriteFile(filepath.Join(root, m), []byte("synthetic marker\n"), 0o644); err != nil {
			t.Fatalf("writing marker %s: %v", m, err)
		}
	}
	return root, inside
}

// resolved is the path with its symlinks followed, which is how the probe reports evidence — on
// macOS a temporary directory lives under /var, which is a link to /private/var, and comparing the
// unresolved spelling would fail for a reason that has nothing to do with the product.
func resolved(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return p
}

func TestDetectSyncNamesTheProviderAndThePath(t *testing.T) {
	cases := []struct {
		name     string
		rootName string
		markers  []string
		provider string
	}{
		{"dropbox", "Dropbox", []string{".dropbox"}, "Dropbox"},
		{"dropbox cache", "Dropbox", []string{".dropbox.cache"}, "Dropbox"},
		{"onedrive guid", "OneDrive", []string{".849C9593-D756-4E56-8D6E-42412F2A707B"}, "OneDrive"},
		{"roaming profile", "profile", []string{"ntuser.dat"}, "a roaming profile"},
		{"icloud placeholder", "somewhere", []string{".Report.pages.icloud"}, "iCloud Drive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, inside := syncRoot(t, c.rootName, c.markers...)
			got := DetectSync(inside)
			if got.State != tri.Yes {
				t.Fatalf("DetectSync(%s) = %v (%s); want Yes — a %s root must be detected on EVERY platform, not just the one its client is famous on",
					inside, got.State, got.Describe(), c.provider)
			}
			if got.Provider != c.provider {
				t.Errorf("provider = %q, want %q — criterion 6 requires the refusal to name WHICH location was detected", got.Provider, c.provider)
			}
			if !strings.HasPrefix(got.Evidence, resolved(t, root)) {
				t.Errorf("evidence = %q, want a path under %q — the refusal must name where the evidence was found", got.Evidence, root)
			}
		})
	}
}

// iCloud's roots carry no marker file of their own; the directory name IS the evidence.
func TestDetectSyncNamedSyncRootDirectories(t *testing.T) {
	for _, dirName := range []string{"Mobile Documents", "com~apple~CloudDocs", "CloudStorage"} {
		t.Run(dirName, func(t *testing.T) {
			_, inside := syncRoot(t, dirName)
			got := DetectSync(inside)
			if got.State != tri.Yes || got.Provider != "iCloud Drive" {
				t.Fatalf("DetectSync under %q = %v/%q; want Yes/iCloud Drive", dirName, got.State, got.Provider)
			}
		})
	}
}

// A store may be created before its directory exists, so the probe must judge the nearest ancestor
// that does. Without this, every refusal could be dodged by naming a path one level deeper.
func TestDetectSyncJudgesAPathThatDoesNotExistYet(t *testing.T) {
	_, inside := syncRoot(t, "Dropbox", ".dropbox")
	deep := filepath.Join(inside, "not", "created", "yet")
	if got := DetectSync(deep); got.State != tri.Yes {
		t.Fatalf("DetectSync(%s) = %v; want Yes — a path that does not exist yet is still inside Dropbox", deep, got.State)
	}
}

// A symlink out of a clean directory into a sync root is the quiet version of the same mistake.
func TestDetectSyncFollowsASymlinkIntoASyncRoot(t *testing.T) {
	_, inside := syncRoot(t, "Dropbox", ".dropbox")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "looks-local")
	if err := os.Symlink(inside, link); err != nil {
		t.Skipf("this filesystem will not make symlinks: %v", err)
	}
	if got := DetectSync(link); got.State != tri.Yes {
		t.Fatalf("DetectSync(%s) = %v (%s); want Yes — the store is judged by where the path LANDS", link, got.State, got.Describe())
	}
}

func TestDetectSyncConfirmsAPlainDirectory(t *testing.T) {
	dir := t.TempDir()
	got := DetectSync(dir)
	if got.State != tri.No {
		t.Fatalf("DetectSync(%s) = %v (%s); want No — a plain temporary directory was inspected and holds no evidence", dir, got.State, got.Describe())
	}
	if got.Describe() == "" {
		t.Error("Describe() is empty; none of the three answers may render as silence (§4.3)")
	}
}

// THE FAILURE THIS PACKAGE EXISTS TO PREVENT: answering "no" because it could not look.
func TestDetectSyncCouldNotLookIsNotANo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read a directory whose permissions forbid it")
	}
	parent := filepath.Join(t.TempDir(), "opaque")
	inside := filepath.Join(parent, "store")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	// Traversable but not listable: the probe can reach the store, and cannot see what is beside it.
	if err := os.Chmod(parent, 0o111); err != nil {
		t.Skipf("this filesystem will not take the permission bits: %v", err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })

	got := DetectSync(inside)
	if got.State == tri.No {
		t.Fatalf("DetectSync answered No for a directory it could not list — that is 'could not determine' rendered as 'determined to be nothing', the one thing §4.3 forbids")
	}
	if got.State != tri.Undetermined {
		t.Fatalf("DetectSync = %v; want Undetermined", got.State)
	}
	if got.Reason == "" {
		t.Error("an undetermined finding must say why; a bare 'could not be determined' is nearly silence")
	}
	if !strings.Contains(got.Describe(), tri.Undetermined.String()) {
		t.Errorf("Describe() = %q; the undetermined wording must appear so a reader cannot mistake it for a negative", got.Describe())
	}
}

// A determined Yes beneath an unreadable level still has to come back as Yes: an unreadable
// directory must not downgrade evidence that was actually found.
func TestDetectSyncPrefersFoundEvidenceOverAnUnreadableLevel(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	root, inside := syncRoot(t, "Dropbox", ".dropbox")
	opaque := filepath.Join(root, "projects")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(opaque, 0o111); err != nil {
		t.Skipf("permissions not honoured here: %v", err)
	}
	t.Cleanup(func() { os.Chmod(opaque, 0o755) })

	if got := DetectSync(inside); got.State != tri.Yes {
		t.Fatalf("DetectSync = %v (%s); want Yes — the Dropbox marker above is determined evidence", got.State, got.Describe())
	}
}

func TestSyncFindingRendersThreeDistinctAnswers(t *testing.T) {
	yes := SyncFinding{State: tri.Yes, Provider: "Dropbox", Evidence: "/x"}.Describe()
	no := SyncFinding{State: tri.No}.Describe()
	und := SyncFinding{Reason: "the probe was blocked"}.Describe()
	for name, s := range map[string]string{"yes": yes, "no": no, "undetermined": und} {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s renders as silence", name)
		}
	}
	if yes == no || no == und || yes == und {
		t.Fatalf("two of the three renderings are identical:\n  yes: %s\n  no:  %s\n  und: %s", yes, no, und)
	}
	if strings.Contains(und, "confirmed") {
		t.Errorf("the undetermined rendering %q reads like a confirmation", und)
	}
}
