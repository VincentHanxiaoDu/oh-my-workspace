package hub

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
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
	// EVERY PACKAGE UNDER internal, DISCOVERED RATHER THAN LISTED. The first version named four
	// directories; merging main brought two more packages, and a hard-coded list would have gone on
	// passing while checking less than its name says. A new package that calls Publish is now
	// caught the day it appears.
	for _, dir := range internalPackageDirs(t) {
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

// THE SCOPE VOCABULARY AND THE VERSION CONSTRAINT ARE ASSERTED ON main, BY ISSUE #11.
//
// This branch was cut before #11 merged, and both Issues were told to honour Issue #12's two
// constraints, so both wrote a guard for each. #11's are merged and reviewed and they win:
// `TestTheScopeVocabularyIsStillExactlyThree` and `TestVersionStillCarriesNoVisibility` in
// versions_test.go cover everything my two did — and more, since they use reflection over the
// struct rather than reading its source text, and #11's also covers `VersionView`. Mine are gone.
//
// ONE THING MY VERSION CAUGHT THAT #11's DOES NOT, kept here under a name of its own rather than
// lost in the deduplication: #11 compares each field's type to `Visibility` exactly, so a field of
// type `*Visibility`, `[]Visibility` or `map[…]Visibility` would pass it. Mine was a text scan and
// happened to catch those. This is that assertion, made properly.
func TestVersionCarriesNoVisibilityThroughAnyIndirection(t *testing.T) {
	target := reflect.TypeOf(Visibility{})
	for _, container := range []reflect.Type{reflect.TypeOf(Version{}), reflect.TypeOf(VersionView{})} {
		for i := 0; i < container.NumField(); i++ {
			f := container.Field(i)
			ft := f.Type
			// Unwrap pointers, slices, arrays and maps until something concrete is reached.
			for {
				switch ft.Kind() {
				case reflect.Ptr, reflect.Slice, reflect.Array:
					ft = ft.Elem()
					continue
				case reflect.Map:
					if ft.Elem() == target || ft.Key() == target {
						t.Fatalf("%s.%s reaches a Visibility through a map; a note has ONE visibility\n"+
							"governing every version of it", container.Name(), f.Name)
					}
					ft = ft.Elem()
					continue
				}
				break
			}
			if ft == target {
				t.Fatalf("%s.%s reaches a Visibility (%s); per-version visibility is how the timeline\n"+
					"becomes a bypass", container.Name(), f.Name, f.Type)
			}
		}
	}
}

// internalPackageDirs returns every package directory under internal/, this one included.
func internalPackageDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("cannot read the internal directory: %v", err)
	}
	out := []string{"."}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "hub" {
			out = append(out, filepath.Join("..", e.Name()))
		}
	}
	if len(out) < 3 {
		t.Fatalf("only %d package directories found; this sweep would prove little: %v", len(out), out)
	}
	return out
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
