package drafts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func newOutbox(t *testing.T) *Outbox {
	t.Helper()
	o, err := Create(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return o
}

// CRITERION 11: the local half stands alone. This whole file runs with no hub configured, no
// daemon, and no network — and the assertion is not "it did not crash" but that successive local
// revisions are addressable and readable AS THEY STOOD.
func TestSuccessiveLocalRevisionsAreAddressableAndReadAsTheyStood(t *testing.T) {
	o := newOutbox(t)
	bodies := []string{"first draft\n", "  second draft with leading space", "third"}
	var refs []hub.VersionRef
	for _, b := range bodies {
		ref, err := o.Revise("plan", b)
		if err != nil {
			t.Fatalf("revise: %v", err)
		}
		refs = append(refs, ref)
	}
	for i, ref := range refs {
		v, err := hub.ReadView(o, nil, ref, "")
		if err != nil {
			t.Fatalf("read %v: %v", ref, err)
		}
		if v.Body != bodies[i] {
			t.Fatalf("%v reads %q, want %q byte-identical", ref, v.Body, bodies[i])
		}
		want := tri.No
		if i == len(refs)-1 {
			want = tri.Yes
		}
		if v.Standing != want {
			t.Fatalf("%v standing = %v, want %v", ref, v.Standing, want)
		}
	}
	tl, err := hub.ListTimeline(o, nil, "plan", "")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if !tl.Determined || len(tl.Entries) != 3 {
		t.Fatalf("timeline determined=%v entries=%d, want true/3", tl.Determined, len(tl.Entries))
	}
	if tl.Current != refs[2] {
		t.Fatalf("current = %v, want %v", tl.Current, refs[2])
	}
}

func TestARevisionSurvivesTheProcessThatWroteIt(t *testing.T) {
	// A local timeline that lives only in memory is not a timeline; a person who acted on revision
	// 1 last month must be able to read it after every process that wrote it has exited. Reopening
	// the directory is this test's stand-in for that.
	dir := filepath.Join(t.TempDir(), "outbox")
	o, err := Create(dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := o.Revise("plan", "as it stood"); err != nil {
		t.Fatalf("revise: %v", err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	v, err := reopened.VersionAt("plan", 1, "")
	if err != nil {
		t.Fatalf("version 1 after reopening: %v", err)
	}
	if v.Body != "as it stood" {
		t.Fatalf("read back %q", v.Body)
	}
}

func TestRevisingNeverOverwritesAnEarlierRevision(t *testing.T) {
	o := newOutbox(t)
	for i := 0; i < 50; i++ {
		if _, err := o.Revise("plan", strings.Repeat("x", i)); err != nil {
			t.Fatalf("revise %d: %v", i, err)
		}
	}
	for i := 0; i < 50; i++ {
		v, err := o.VersionAt("plan", i+1, "")
		if err != nil {
			t.Fatalf("revision %d: %v", i+1, err)
		}
		if v.Body != strings.Repeat("x", i) {
			t.Fatalf("revision %d reads %q", i+1, v.Body)
		}
	}
}

func TestNothingLocalExpiresEither(t *testing.T) {
	// PRD §5.4 applies to the local half too. Backdate every revision file by a century and read
	// them all again: no age-based sweep can have run, because there is nothing that sweeps.
	o := newOutbox(t)
	for i := 0; i < 20; i++ {
		if _, err := o.Revise("plan", "rev"); err != nil {
			t.Fatalf("revise: %v", err)
		}
	}
	dir := filepath.Join(o.Dir(), "plan")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		backdated := info.ModTime().AddDate(-100, 0, 0)
		if err := os.Chtimes(p, backdated, backdated); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	tl, err := hub.ListTimeline(o, nil, "plan", "")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(tl.Entries) != 20 {
		t.Fatalf("entries = %d after backdating a century, want 20", len(tl.Entries))
	}
}

func TestAMissingRevisionIsNotAnEmptyOne(t *testing.T) {
	o := newOutbox(t)
	if _, err := o.Revise("plan", ""); err != nil {
		t.Fatalf("revise: %v", err)
	}
	// An empty revision is a real, successful read.
	v, err := o.VersionAt("plan", 1, "")
	if err != nil {
		t.Fatalf("reading an empty revision: %v", err)
	}
	if v.Body != "" {
		t.Fatalf("body = %q", v.Body)
	}
	// A missing one is a refusal with its own code, tellable apart without looking at a body.
	if _, err := o.VersionAt("plan", 2, ""); hub.Code(err) != hub.ErrNoSuchVersion.Code {
		t.Fatalf("missing revision code = %q, want %q", hub.Code(err), hub.ErrNoSuchVersion.Code)
	}
	if _, err := o.VersionAt("no-such-draft", 1, ""); hub.Code(err) != ErrNoSuchDraft.Code {
		t.Fatalf("missing draft code = %q, want %q", hub.Code(err), ErrNoSuchDraft.Code)
	}
}

func TestARevisionThatCannotBeReadIsUndeterminedNotEmpty(t *testing.T) {
	o := newOutbox(t)
	if _, err := o.Revise("plan", "real content"); err != nil {
		t.Fatalf("revise: %v", err)
	}
	path := filepath.Join(o.Dir(), "plan", "000001.body")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	// PROBE, DO NOT NAME. Running as root, or on a filesystem that ignores modes, makes the file
	// readable anyway — and a test that assumed otherwise would be asserting nothing. So ask the
	// environment whether the mode took effect, and skip if it did not.
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("this environment can still read a mode-000 file; the unreadable-body path cannot be driven here")
	}

	v, err := hub.ReadView(o, nil, hub.VersionRef{Note: "plan", Number: 1}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.BodyKnown {
		t.Fatalf("an unreadable revision reported its body as known: %q", v.Body)
	}
	if v.Determined() {
		t.Fatalf("an unreadable revision reported the view as determined")
	}
	out := v.Render()
	if !strings.Contains(out, hub.BodyUnreadableLine) {
		t.Fatalf("output does not say the body could not be read:\n%s", out)
	}
	if strings.Contains(out, "body:\n\n") {
		t.Fatalf("an unreadable revision rendered as an empty body:\n%s", out)
	}
}

func TestNothingIsConjured(t *testing.T) {
	// PRD §4.2: the store is created explicitly. Opening a directory that is not an outbox refuses
	// and creates nothing — it does not quietly mkdir and then report an empty outbox.
	dir := filepath.Join(t.TempDir(), "not-an-outbox")
	if _, err := Open(dir); hub.Code(err) != ErrNoOutbox.Code {
		t.Fatalf("open code = %q, want %q", hub.Code(err), ErrNoOutbox.Code)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("Open created %s", dir)
	}
	// And an existing but unmarked directory is not an empty outbox either.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := Open(dir); hub.Code(err) != ErrNoOutbox.Code {
		t.Fatalf("an unmarked directory read as an outbox: %v", err)
	}
}

func TestADraftNameCannotEscapeTheOutbox(t *testing.T) {
	o := newOutbox(t)
	for _, bad := range []string{"..", ".", "", "a/b", "../escape", ".hidden"} {
		if _, err := o.Revise(hub.NoteID(bad), "x"); hub.Code(err) != ErrBadDraftID.Code {
			t.Fatalf("Revise(%q) code = %q, want %q", bad, hub.Code(err), ErrBadDraftID.Code)
		}
	}
}

func TestTheLocalHalfImportsNoNetwork(t *testing.T) {
	// Criterion 10: no network connection is opened unless a hub is configured. The strongest form
	// of that for this package is that it cannot: nothing here imports net.
	b, err := os.ReadFile("outbox.go")
	if err != nil {
		t.Fatalf("reading the package source: %v", err)
	}
	for _, banned := range []string{`"net"`, `"net/http"`, `"net/url"`} {
		if strings.Contains(string(b), banned) {
			t.Fatalf("the local draft half imports %s", banned)
		}
	}
}
