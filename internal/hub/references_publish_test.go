package hub

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// referenceSourceFiles is the non-test source this Issue adds, named so that a rename is a red
// build rather than a check that quietly stops checking anything.
var referenceSourceFiles = []string{
	"references.go",
	"references_read.go",
	"references_publish.go",
}

// CRITERION 4: publishing a note whose body references a target the author cannot see is REFUSED at
// publication — not silently narrowed, stripped or downgraded — and the refusal says why.
func TestPublishingAReferenceTheAuthorCannotSeeIsRefused(t *testing.T) {
	c := newCorpus(t)
	before := c.store.Count()

	_, err := PublishWithReferences(c.store, Publication{
		Author: "bo", Title: "bo cites something bo cannot see",
		Body: "as [[note:" + string(c.secret) + "]] explains",
	})
	if Code(err) != ErrReferenceNotVisibleToAuthor.Code {
		t.Fatalf("got %v (code %q), want %q", err, Code(err), ErrReferenceNotVisibleToAuthor.Code)
	}
	// SAYS WHY, per PRD §3.11. The author is told which reference, which discloses nothing to them
	// that their own draft did not already say.
	if !strings.Contains(err.Error(), string(c.secret)) {
		t.Errorf("the refusal %q does not say which reference was refused", err)
	}
	// NOT NARROWED AT THE EDGE. Nothing was stored — not the note with the reference stripped, not
	// the note narrowed to hide it.
	if got := c.store.Count(); got != before {
		t.Fatalf("the store holds %d notes after a refused publication, want %d — a refusal that has\n"+
			"already written is a refusal in name only", got, before)
	}
	for _, id := range c.store.IDs() {
		n, err := c.store.Read(id, "alice")
		if err != nil {
			continue
		}
		if n.Title == "bo cites something bo cannot see" {
			t.Fatalf("the refused note was stored anyway as %+v", n)
		}
	}
}

// The author CAN publish a reference to something they can see, or this refusal would be a wall
// rather than a rule.
func TestPublishingAReferenceTheAuthorCanSeeIsAllowed(t *testing.T) {
	c := newCorpus(t)
	n, err := PublishWithReferences(c.store, Publication{
		Author: "alice", Title: "alice cites her own restricted note",
		Body: "as [[note:" + string(c.secret) + "]] explains",
	})
	if err != nil {
		t.Fatalf("alice may read the target, so this must be allowed: %v", err)
	}
	if n == nil {
		t.Fatal("no note came back")
	}
}

// A dangling reference is NOT refused at publication. Criterion 11 exists because targets go away
// after publication, and a publication path that refused a target that does not exist would make
// that state reachable only by deleting things.
func TestPublishingADanglingReferenceIsAllowed(t *testing.T) {
	c := newCorpus(t)
	if _, err := PublishWithReferences(c.store, Publication{
		Author: "bo", Title: "points at nothing", Body: "see [[note:note-404]]",
	}); err != nil {
		t.Fatalf("a reference to a target that does not exist discloses nothing: %v", err)
	}
}

// Undetermined access is refused with ITS OWN CODE. Publishing anyway would treat undetermined as
// permitted; refusing with the not-visible code would claim we established something we did not.
func TestPublishingWhenTheAuthorsAccessCannotBeDeterminedIsItsOwnRefusal(t *testing.T) {
	c := newCorpus(t)
	delete(c.rec.groups, "platform")
	_, err := PublishWithReferences(c.store, Publication{
		Author: "bo", Title: "cites it", Body: "see [[note:" + string(c.secret) + "]]",
	})
	if Code(err) != ErrReferenceUndetermined.Code {
		t.Fatalf("got %v (code %q), want %q — could not determine and determined to be refused are\n"+
			"different answers", err, Code(err), ErrReferenceUndetermined.Code)
	}
	if ErrReferenceUndetermined.Code == ErrReferenceNotVisibleToAuthor.Code {
		t.Error("the two refusals share a code")
	}
}

// The agent API path is bound by criterion 4 too. PublishThrough must not be a way around it.
func TestPublishingThroughAGrantIsBoundByTheSameRule(t *testing.T) {
	c := newCorpus(t)
	g, err := NewLedger().Request(Holder{Person: "bo", Scopes: []Scope{ScopePublish}}, []Scope{ScopePublish})
	if err != nil {
		t.Fatal(err)
	}
	before := c.store.Count()
	_, err = PublishThrough(c.store, g, Publication{
		Author: "bo", Title: "via an agent", Body: "see [[note:" + string(c.secret) + "]]",
	})
	if Code(err) != ErrReferenceNotVisibleToAuthor.Code {
		t.Fatalf("an agent published %v (code %q); PRD §3.12 — an agent cannot read what its person\n"+
			"cannot, and it cannot point at it either", err, Code(err))
	}
	if c.store.Count() != before {
		t.Error("the agent's refused publication stored something")
	}
}

// THE CHECK MUST BE UNAVOIDABLE, not merely available. Every non-test caller of Store.Publish in
// this repository has to be the one wrapper that checks references, or criterion 4 holds only for
// callers who remembered.
func TestTheOnlyDirectCallerOfStorePublishIsTheCheckedWrapper(t *testing.T) {
	const allowed = "references_publish.go"
	found := map[string]int{}
	for _, dir := range []string{".", "../commands", "../cli", "../health"} {
		for _, file := range referenceGoFilesIn(t, dir) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(src), ".Publish("); n > 0 {
				found[filepath.Base(file)] += n
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("no call to .Publish( was found anywhere; this assertion would pass vacuously")
	}
	for name, n := range found {
		if name != allowed {
			t.Errorf("%s calls Store.Publish directly %d time(s); it must call PublishWithReferences,\n"+
				"or criterion 4 is a rule that callers may forget", name, n)
		}
	}
}

// CRITERION 15, structurally: resolving or querying references opens no network connection. Code
// that cannot reach a socket API cannot open one.
//
// WHAT THIS DRIVES AND WHAT IT DOES NOT, said here rather than left to be assumed: it parses the
// source this Issue adds and fails on a network-capable standard-library import. It is not a
// packet-level observation, and it does not walk transitive imports.
func TestReferenceCodeImportsNoNetworkPackage(t *testing.T) {
	forbidden := []string{"net", "net/http", "net/url", "crypto/tls", "net/rpc", "net/smtp", "os/exec"}
	fset := token.NewFileSet()
	files := append([]string{}, referenceSourceFiles...)
	files = append(files, "../commands/references_cmd.go")
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("cannot parse %s: %v — a renamed file makes this check silently stop checking", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q; references open no network connection and start no process,\n"+
						"and the way that is guaranteed is by not being able to", name, path)
				}
			}
		}
	}
}

// This Issue's errors collide with nothing Issue #12 defined. errors.go is merged work and this
// Issue does not edit it, so its own distinctness test cannot see these.
func TestReferenceErrorsAreDistinguishableFromEveryOther(t *testing.T) {
	all := append(append([]*Error{}, allErrors...), referenceErrors...)
	if len(referenceErrors) == 0 {
		t.Fatal("no reference errors are listed, so this comparison is vacuous")
	}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].Code == all[j].Code {
				t.Errorf("two errors share the code %q", all[i].Code)
			}
			if all[i].Msg == all[j].Msg {
				t.Errorf("two errors share the message %q", all[i].Msg)
			}
		}
	}
}

// THE THREE SCOPES ARE STILL THREE. Issue #12 ruled the vocabulary and this Issue adds nothing to
// it — reading references is `read`, and the hub operator's ability to read everything is a
// deployment fact, not a scope.
func TestThisIssueAddsNoFourthScope(t *testing.T) {
	if got := len(Vocabulary()); got != 3 {
		t.Fatalf("the vocabulary has %d scopes: %v", got, Vocabulary())
	}
	for _, s := range Vocabulary() {
		switch s {
		case ScopeRead, ScopeWrite, ScopePublish:
		default:
			t.Errorf("unexpected scope %q", s)
		}
	}
}

// And no visibility field crept onto Version. One visibility governs a note and all of its
// versions; per-version visibility is how the timeline becomes a bypass.
func TestVersionStillCarriesNoVisibility(t *testing.T) {
	src, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	decl := string(src)
	start := strings.Index(decl, "type Version struct {")
	if start < 0 {
		t.Fatal("Version's declaration was not found, so this check proves nothing")
	}
	end := strings.Index(decl[start:], "\n}")
	if end < 0 {
		t.Fatal("Version's declaration does not end")
	}
	if strings.Contains(strings.ToLower(decl[start:start+end]), "visibility") {
		t.Error("Version has gained a visibility field; a note's visibility governs every version of it")
	}
}

func referenceGoFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	if len(out) == 0 {
		t.Fatalf("no non-test Go files under %s; an assertion over them would be vacuous", dir)
	}
	return out
}
