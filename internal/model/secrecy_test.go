package model

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/refusal"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// PRD §3.13: "A key belongs to the person who supplied it. It is not published, not synchronised,
// and not readable through the agent API." Issue #18 criteria 4–7.
//
// The three negatives are driven separately, because each fails for its own reason and a single
// test that swept them all would go green when one of them stopped being exercised.

// ---------------------------------------------------------------------------
// Criterion 7 — the credential is in no rendering, of anything, ever
// ---------------------------------------------------------------------------

// EVERY FORMATTING VERB, NOT JUST THE ONE SOMEBODY REMEMBERED.
//
// `%v` is the one people think of and `%#v` is the one that bites: it ignores String entirely and
// prints the struct literal, unexported fields and all. It was measured before GoString was added,
// and it printed the credential. `%+v` and the error verbs are here for the same reason — the
// question is not "does Render leak" but "can fmt be made to".
func TestNoFormattingOfAConfigCanProduceTheCredential(t *testing.T) {
	cfg := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), nil)
	if cfg.Secret() != theSecret {
		t.Fatalf("Secret() = %q; this test is not holding the credential it means to check for", cfg.Secret())
	}

	// A struct that merely CONTAINS a Config, because that is how a Config usually reaches fmt in
	// real code — as a field of something being logged.
	type wrapper struct {
		Note string
		Cfg  Config
	}
	w := wrapper{Note: "a diagnostic line somebody added", Cfg: cfg}

	renderings := map[string]string{
		"Render()":      cfg.Render(),
		"String()":      cfg.String(),
		"GoString()":    cfg.GoString(),
		"View().Render": cfg.View().Render(),
		"%v":            fmt.Sprintf("%v", cfg),
		"%s":            fmt.Sprintf("%s", cfg),
		"%+v":           fmt.Sprintf("%+v", cfg),
		"%#v":           fmt.Sprintf("%#v", cfg),
		"%q":            fmt.Sprintf("%q", cfg),
		"wrapper %v":    fmt.Sprintf("%v", w),
		"wrapper %+v":   fmt.Sprintf("%+v", w),
		"wrapper %#v":   fmt.Sprintf("%#v", w),
	}
	for name, s := range renderings {
		if strings.Contains(s, theSecret) {
			t.Errorf("%s contains the person's credential:\n  %s", name, s)
		}
	}
}

// The credential is not in the JSON of anything that gets serialised, and the View has NOWHERE TO
// PUT ONE. The second half is structural: reflection over View's fields, so the guarantee survives
// somebody adding a field rather than depending on this one instance.
func TestTheViewHasNowhereToPutACredential(t *testing.T) {
	cfg := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), nil)
	view := cfg.View()

	b, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), theSecret) {
		t.Errorf("the serialised View contains the credential: %s", b)
	}
	// The Config itself must survive being handed to a serialiser too, since a future surface may
	// do it by mistake.
	b2, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), theSecret) {
		t.Errorf("the serialised Config contains the credential: %s", b2)
	}

	// STRUCTURAL: every exported field of View must be one of the four known, non-secret ones. A
	// new field is not forbidden — it just has to be added here on purpose, which is the point.
	// Each of these was added deliberately, and each is a fact ABOUT the configuration rather than
	// any part of the credential: the provider's name, the two three-valued answers, the reason
	// behind a negative, and whether this build has an adapter for the chosen provider. "Adapter"
	// arrived when criterion 18's agreement test caught two surfaces wording one state differently;
	// this test refused it until it was listed here, which is the whole point of the check.
	allowed := map[string]bool{
		"Provider": true, "ProviderChosen": true, "CredentialPresent": true,
		"Detail": true, "Adapter": true,
	}
	vt := reflect.TypeOf(View{})
	for i := 0; i < vt.NumField(); i++ {
		f := vt.Field(i)
		if !allowed[f.Name] {
			t.Errorf("View has a field %q that this test does not know about. A View crosses the control "+
				"API and, when Issue #16 lands, the agent API. If it is not a secret, add it to the "+
				"allowed set on purpose; if it could hold a credential, it does not belong here.", f.Name)
		}
	}
	if vt.NumField() != len(allowed) {
		t.Errorf("View has %d fields and %d are allowed; a field was removed without this test being told", vt.NumField(), len(allowed))
	}

	// The record written to disk has no credential field either, for the same reason and checked
	// the same way.
	rt := reflect.TypeOf(record{})
	for i := 0; i < rt.NumField(); i++ {
		if n := rt.Field(i).Name; n != "Provider" && n != "CredentialFile" {
			t.Errorf("the on-disk record has a field %q; the credential VALUE is never stored (criterion 7)", n)
		}
	}
}

// ---------------------------------------------------------------------------
// Criterion 6 — refusal and "no credential configured" are different answers
// ---------------------------------------------------------------------------

// AN API SURFACE IS REFUSED, NOT GIVEN AN EMPTY STRING.
//
// The criterion is precise: "refused rather than answered with a redacted or empty value that a
// caller could mistake for 'no credential configured' — refusal and 'no credential configured' are
// distinguishable in the agent API response." So the two are compared with each other: the refusal
// carries an error with its own code, and "no credential" is a successful answer in a field.
func TestAskingAnAPIForTheCredentialIsRefusedAndNotAnsweredEmpty(t *testing.T) {
	configured := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), nil).View()
	none := Read(envOf(map[string]string{EnvProvider: "acme"}), nil).View()

	val, err := CredentialThrough(configured)
	if err == nil {
		t.Fatal("an API surface was given the credential")
	}
	if val != "" {
		t.Errorf("the refusal also returned a value %q", val)
	}
	if refusal.Code(err) != ErrCredentialNotReadable.Code {
		t.Errorf("the refusal's code is %q, want %q — a caller must tell it apart without parsing prose",
			refusal.Code(err), ErrCredentialNotReadable.Code)
	}

	// THE DISTINCTION. A fully configured credential refused, and a genuinely absent one reported:
	// the first is an error, the second is a successful answer of "no". They must not arrive the
	// same way, and the refusal must be IDENTICAL whether or not there is a credential — otherwise
	// the refusal itself tells the caller whether one exists.
	if none.Present() != tri.No {
		t.Errorf("a provider with no credential reports present=%v, want no", none.Present())
	}
	if configured.Present() != tri.Yes {
		t.Errorf("a provider with a credential reports present=%v, want yes", configured.Present())
	}
	_, err2 := CredentialThrough(none)
	if refusal.Code(err2) != refusal.Code(err) {
		t.Errorf("the refusal differs depending on whether a credential exists (%q vs %q); the refusal itself "+
			"must not be an oracle for the answer it refuses", refusal.Code(err2), refusal.Code(err))
	}
}

// The three states a caller can be in arrive three different ways, and none of them is silence.
func TestTheApiViewsThreeStatesArePairwiseDistinct(t *testing.T) {
	keyFile := unreadableFile(t, theSecret)
	pairwiseDistinct(t, "the API view", map[string]string{
		"nothing chosen":        Read(envOf(nil), nil).View().Render(),
		"chosen, no credential": Read(envOf(map[string]string{EnvProvider: "acme"}), nil).View().Render(),
		"chosen, credential":    Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), nil).View().Render(),
		"undetermined":          Read(envOf(map[string]string{EnvProvider: "acme", EnvCredentialFile: keyFile}), nil).View().Render(),
	})
}

// The View and the Config describe the same state in the same words. Criterion 18 asks that the CLI
// and the control API agree; they agree because one Render is called from both, and this is the
// assertion that keeps it one.
func TestTheViewAndTheConfigRenderIdentically(t *testing.T) {
	keyFile := unreadableFile(t, theSecret)
	for name, env := range map[string]map[string]string{
		"nothing":               nil,
		"chosen only":           {EnvProvider: "acme"},
		"chosen and credential": {EnvProvider: "acme", EnvCredential: theSecret},
		"undetermined":          {EnvProvider: "acme", EnvCredentialFile: keyFile},
	} {
		cfg := Read(envOf(env), nil)
		if cfg.Render() != cfg.View().Render() {
			t.Errorf("%s: the Config and its View word the same state differently:\n  config: %s\n  view:   %s",
				name, cfg.Render(), cfg.View().Render())
		}
	}
}

// ---------------------------------------------------------------------------
// Structural: one reader of the environment, one caller of Secret
// ---------------------------------------------------------------------------

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find the repository root")
		}
		dir = parent
	}
}

func walkProductFiles(t *testing.T, fn func(rel string, src string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	root := repoRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		// PRODUCT FILES ONLY. A test may legitimately name an environment variable or hold a
		// credential it created itself; the rules below are about what the shipped product does.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, body, parser.ParseComments)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fn(rel, string(body), f, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
}

// THE RECONCILIATION, ENFORCED (Issue #18's whole reason for touching #9's code).
//
// #9's `internal/drafts/model.go` read $OMW_MODEL because #18 had not landed. "Two implementations
// of 'is a model configured' that disagree is worse than either alone" — the same class as the two
// outboxes §3.14 forbids. The resolution now lives here, and this test is what stops a second one
// appearing: exactly one file in the product may name these variables, and it is this package's.
func TestExactlyOneFileReadsTheModelEnvironment(t *testing.T) {
	names := []string{EnvProvider, EnvCredential, EnvCredentialFile}
	readers := map[string][]string{}
	walkProductFiles(t, func(rel, src string, f *ast.File, fset *token.FileSet) {
		// The literal is looked for in the SYNTAX TREE's string literals, not with a substring
		// search over the bytes: a comment mentioning $OMW_MODEL is documentation, not a second
		// reader, and a rule that could not tell them apart would be one people route around.
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			for _, name := range names {
				if lit.Value == `"`+name+`"` {
					readers[name] = append(readers[name], rel)
				}
			}
			return true
		})
	})
	for _, name := range names {
		files := readers[name]
		if len(files) == 0 {
			t.Errorf("no file in the product names %s at all; this scan is not looking at anything, "+
				"so its pass would prove nothing", name)
			continue
		}
		for _, f := range files {
			if f != filepath.Join("internal", "model", "config.go") {
				t.Errorf("%s names %q. There is one answer to 'is a model configured' and it is "+
					"internal/model (Issue #18, PRD §4.3). Consume model.Read; do not read the "+
					"environment a second time.", f, name)
			}
		}
	}
}

// THE CREDENTIAL ENTERS THE PRODUCT AT ONE SEAM.
//
// Config.Secret is the only route to the credential value, and the only thing entitled to call it
// is the code that authenticates to the person's provider. A second caller is not necessarily a
// leak — but it is a place a leak becomes possible, and it should be met deliberately rather than
// discovered. The count is asserted so that a new one fails here and gets read.
func TestNothingButTheProviderSeamCallsSecret(t *testing.T) {
	callers := map[string]int{}
	walkProductFiles(t, func(rel, src string, f *ast.File, fset *token.FileSet) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Secret" {
				return true
			}
			callers[rel]++
			return true
		})
	})
	want := map[string]int{
		// The seam: `review` opening the person's provider with their credential.
		filepath.Join("internal", "commands", "outbox_cmd.go"): 1,
	}
	for file, n := range callers {
		if want[file] != n {
			t.Errorf("%s calls Secret() %d time(s), expected %d. The credential enters the product at "+
				"one seam (PRD §3.13). If this call is right, add it here on purpose and say why in "+
				"the pull request.", file, n, want[file])
		}
	}
	for file, n := range want {
		if callers[file] != n {
			t.Errorf("%s calls Secret() %d time(s), expected %d — the seam this test is guarding has "+
				"moved, so the guard is not guarding it", file, callers[file], n)
		}
	}
}

// No two of this package's errors share a code or a message. Every criterion here is "these two
// outcomes must not look the same", and a shared code makes two of them one.
func TestTheErrorsArePairwiseDistinguishable(t *testing.T) {
	codes := map[string]string{}
	msgs := map[string]string{}
	for _, e := range allErrors {
		if e.Code == "" || e.Msg == "" {
			t.Errorf("an error has an empty code or message: %+v", e)
		}
		if prev, dup := codes[e.Code]; dup {
			t.Errorf("code %q is shared by %q and %q", e.Code, prev, e.Msg)
		}
		codes[e.Code] = e.Msg
		if prev, dup := msgs[e.Msg]; dup {
			t.Errorf("message %q is shared by codes %q and %q", e.Msg, prev, e.Code)
		}
		msgs[e.Msg] = e.Code
	}
	// The two that matter most: "there is no model" and "we could not tell" must never share an
	// identity, because they must never share an exit code (cli.ExitUndetermined = 3).
	if ErrNoModel.Code == ErrUndetermined.Code {
		t.Error("no-model and undetermined share a code; a script cannot tell 'the answer is no' from 'I could not check'")
	}
}
