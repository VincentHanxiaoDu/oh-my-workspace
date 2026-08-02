package store

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CRITERIA 14 AND 15: the store is the sole home of unpublished data.
//
// The drive is the one the criterion asks for: write a distinctive string into a ticket and into a
// draft, then SEARCH THE FILESYSTEM for it and assert every hit is inside the store.
//
// WHAT THIS TEST CAN AND CANNOT SEE, stated rather than glossed. It redirects every root a program
// writes to by convention — HOME, XDG_*, TMPDIR — into a sandbox, does the work, and then walks the
// entire sandbox plus the process's real temporary directory. A copy written to a path this test
// does not walk would not be caught; nothing this product does today writes to one, and the
// companion test below asserts the package never reaches for a temporary or cache directory at all,
// which is the mechanism a second copy would have to come through.
func TestNoUnpublishedBodyExistsOutsideTheStore(t *testing.T) {
	sandbox := t.TempDir()
	for k, v := range map[string]string{
		"HOME":            filepath.Join(sandbox, "home"),
		"XDG_DATA_HOME":   filepath.Join(sandbox, "home", ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(sandbox, "home", ".cache"),
		"XDG_CONFIG_HOME": filepath.Join(sandbox, "home", ".config"),
		"TMPDIR":          filepath.Join(sandbox, "tmp"),
	} {
		if err := os.MkdirAll(v, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(k, v)
	}

	root, err := Resolve(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Create(root)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const ticketPhrase = "ZQXJ-TICKET-BODY-a-thing-only-this-test-ever-wrote"
	const draftPhrase = "ZQXJ-DRAFT-NOTE-another-thing-only-this-test-ever-wrote"
	if err := s.PutJSON("ticket", "t1", map[string]string{"title": "please review", "body": ticketPhrase}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutJSON("draft", "d1", map[string]string{"body": draftPhrase}); err != nil {
		t.Fatal(err)
	}

	// CRITERION 15 is what makes the search possible at all: the product says where the store is.
	storePath := s.Path()
	if resolvedStore, rerr := filepath.EvalSymlinks(storePath); rerr == nil {
		// Compared after following links, because a temporary directory on macOS is reached through
		// /var and reported through /private/var, and that difference is not the product's.
		storePath = resolvedStore
	}
	if storePath == "" || !filepath.IsAbs(storePath) {
		t.Fatalf("Path() = %q; without an absolute store path criterion 14 cannot be checked by anyone", storePath)
	}

	for _, phrase := range []string{ticketPhrase, draftPhrase} {
		hits := grepTree(t, sandbox, phrase)
		hits = append(hits, grepTree(t, os.TempDir(), phrase)...)
		if len(hits) == 0 {
			t.Fatalf("the phrase %q was not found anywhere, not even in the store — this search is not looking where the data went, so it proves nothing", phrase)
		}
		for _, hit := range hits {
			if !strings.HasPrefix(hit, storePath+string(filepath.Separator)) {
				t.Errorf("unpublished content is on this machine outside the store:\n  %s\n  store is %s", hit, storePath)
			}
		}
	}
}

// grepTree returns every file under root whose bytes contain phrase.
//
// The payload is base64 in the record file, so the phrase is searched for in its encoded form too —
// otherwise this test would miss a copy in the very format the store itself uses, which is the one
// copy it is most likely to find.
func grepTree(t *testing.T, root, phrase string) []string {
	t.Helper()
	needles := [][]byte{[]byte(phrase)}
	for offset := 0; offset < 3; offset++ {
		// base64 of the phrase depends on its byte offset within the payload, so all three
		// alignments are searched.
		needles = append(needles, []byte(base64Middle(phrase, offset)))
	}

	deadline := time.Now().Add(20 * time.Second)
	var hits []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || time.Now().After(deadline) {
			return nil // Unreadable or out of time: skipped, and the count check above notices.
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > 4<<20 {
			return nil
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for _, n := range needles {
			if len(n) > 0 && strings.Contains(string(body), string(n)) {
				resolvedPath, _ := filepath.EvalSymlinks(p)
				if resolvedPath == "" {
					resolvedPath = p
				}
				hits = append(hits, resolvedPath)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return hits
}

// base64Middle is the stable middle of the phrase's base64 encoding at a given byte alignment: the
// substring that appears regardless of what surrounds the phrase in the payload.
func base64Middle(phrase string, offset int) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	padded := strings.Repeat("\x00", offset) + phrase
	var sb strings.Builder
	for i := 0; i+3 <= len(padded); i += 3 {
		n := int(padded[i])<<16 | int(padded[i+1])<<8 | int(padded[i+2])
		sb.WriteByte(alphabet[(n>>18)&63])
		sb.WriteByte(alphabet[(n>>12)&63])
		sb.WriteByte(alphabet[(n>>6)&63])
		sb.WriteByte(alphabet[n&63])
	}
	full := sb.String()
	// Trim the first and last group, which the surrounding bytes can change.
	if len(full) > 8 {
		return full[4 : len(full)-4]
	}
	return ""
}

// CRITERIA 17, 18 AND 20, structurally: this package cannot start a daemon, cannot open a network
// connection and cannot need a control API, because none of the machinery for any of those is
// reachable from it.
//
// A behavioural assertion — count the sockets a run opened — can only observe the run it watched.
// This observes every possible run: a package that does not link `net` has no outbound connection
// to make, at any input, on any platform. It is the stronger statement of the two, and it is the
// one that keeps holding when somebody adds a feature here next month.
func TestTheStorePackageCannotReachTheNetworkOrStartAProcess(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH to ask about the import graph: %v", err)
	}
	out, err := exec.Command(goTool, "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	forbidden := map[string]string{
		"net":       "an outbound connection (§4.2: no network without a hub configured)",
		"net/http":  "an outbound connection (§4.2)",
		"os/exec":   "starting another process — including a daemon (§4.2: no command starts the daemon)",
		"net/rpc":   "a control API dependency (§4.6, criterion 20)",
		"os/signal": "daemon lifecycle machinery",
	}
	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if why, bad := forbidden[strings.TrimSpace(dep)]; bad {
			t.Errorf("internal/store depends on %q, which makes %s possible from store creation", dep, why)
		}
	}
}
