package agentapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
)

// ---------------------------------------------------------------------------
// The vocabulary is three, and this package invents nothing
// ---------------------------------------------------------------------------

// TestTheAgentAPIAddsNoFourthScope is Issue #16's `## Ruled` section, enforced.
//
// The scope vocabulary is Issue #19's and is exactly `read` / `write` / `publish`. The hub
// operator's ability to read everything is a DEPLOYMENT FACT (PRD §2.4), not a scope, and an
// "administer this machine" scope for issuing grants was the specific temptation here — it is not
// taken, and [ScopeFor] returns "needs no scope" for those two operations instead.
func TestTheAgentAPIAddsNoFourthScope(t *testing.T) {
	if got := len(hub.Vocabulary()); got != 3 {
		t.Fatalf("the scope vocabulary has %d entries; it is ruled at three", got)
	}
	for _, op := range Operations() {
		s, needs := ScopeFor(op)
		if !needs {
			continue
		}
		if !hub.KnownScope(s) {
			t.Errorf("operation %q needs scope %q, which is not in the ruled vocabulary %v", op, s, hub.Vocabulary())
		}
	}
	// AND NO STRING IN THIS PACKAGE NAMES A SCOPE THAT IS NOT ONE OF THE THREE.
	for _, forbidden := range []string{"admin", "operator", "read-all", "readall", "superuser", "root"} {
		if hub.KnownScope(hub.Scope(forbidden)) {
			t.Errorf("%q is a known scope; the vocabulary is ruled at three", forbidden)
		}
	}
}

// TestEveryEnumeratedAgentOperationIsAnswered makes adding an operation to the enumeration without
// wiring it a red test rather than a runtime surprise.
func TestEveryEnumeratedAgentOperationIsAnswered(t *testing.T) {
	f := newFixture(t, hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish)
	g := f.issue(hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish)
	for _, op := range Operations() {
		r := Answer(Request{Op: op, Grant: g.ID, Scopes: nil, NoteID: "d-1", Title: "t", Body: "b"}, f.src)
		if r.Code == ErrUnknownOperation.Code {
			t.Errorf("operation %q is enumerated and not wired up", op)
		}
		if r.Op != op {
			t.Errorf("operation %q answered as %q", op, r.Op)
		}
	}
	// AND THE CONTROL: an operation that is NOT enumerated is refused as unknown, so the check
	// above is capable of firing.
	if r := Answer(Request{Op: "read-everything", Grant: g.ID}, f.src); r.Code != ErrUnknownOperation.Code {
		t.Fatalf("an operation nobody defined answered %q; the unknown-operation check does not fire, "+
			"so the test above proves nothing", r.Code)
	}
}

// TestEveryAgentAPIRefusalIsDistinguishable is the property every "these two must not look the
// same" criterion rests on.
func TestEveryAgentAPIRefusalIsDistinguishable(t *testing.T) {
	all := append(append([]*hub.Error{}, agentAPIErrors...), hubErrorsThisSurfaceEmits...)
	codes := map[string]string{}
	msgs := map[string]string{}
	for _, e := range all {
		if e.Code == "" {
			t.Errorf("an error with the message %q has no code", e.Msg)
		}
		if other, dup := codes[e.Code]; dup {
			t.Errorf("code %q is shared by %q and %q", e.Code, other, e.Msg)
		}
		codes[e.Code] = e.Msg
		if other, dup := msgs[e.Msg]; dup {
			t.Errorf("message %q is shared with %q", e.Msg, other)
		}
		msgs[e.Msg] = e.Code
	}
}

// TestTheThreeOutcomesNeverShareAnExitCode is the project's standing rule, at this surface.
func TestTheThreeOutcomesNeverShareAnExitCode(t *testing.T) {
	seen := map[int]Outcome{}
	for _, o := range []Outcome{OutcomeOK, OutcomeRefused, OutcomeUndetermined} {
		if other, dup := seen[o.Exit()]; dup {
			t.Errorf("%q and %q both exit %d; `could not determine` and `determined to be nothing` "+
				"never share an exit code", o, other, o.Exit())
		}
		seen[o.Exit()] = o
	}
	if OutcomeUndetermined.Exit() != 3 {
		t.Errorf("the undetermined outcome exits %d; the product's code is 3 (cli.ExitUndetermined)",
			OutcomeUndetermined.Exit())
	}
	// AN OUTCOME NOBODY SET IS NOT A SUCCESS. The zero Outcome is "", which falls to the default
	// branch and is undetermined — the same reasoning tri uses for its zero value.
	var zero Outcome
	if zero.Exit() != 3 {
		t.Errorf("the zero Outcome exits %d; an answer nobody produced has not been determined", zero.Exit())
	}
}

// ---------------------------------------------------------------------------
// Criterion 13 — the credential is not on this surface
// ---------------------------------------------------------------------------

// TestNoAgentAPIResponseCarriesTheCredential drives EVERY operation, in its succeeding, refusing and
// undetermined forms, and scans the bytes that would reach the AI.
//
// THE FAILING PATHS ARE THE POINT. Criterion 13 names "not in an error message, and not in any
// diagnostic payload" specifically, and an error message built with %v over a configuration struct
// is the ordinary way a key ends up in a log.
func TestNoAgentAPIResponseCarriesTheCredential(t *testing.T) {
	const secret = "sk-THE-PERSONS-OWN-KEY-0123456789"

	f := newFixture(t, hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish)
	g := f.issue(hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish)
	readOnly := f.issue(hub.ScopeRead)
	f.putTicket("t-1", "a ticket")
	f.publish(me, "a note", hub.CompanyWide())
	if _, err := f.outbox.Revise("d-1", "a draft"); err != nil {
		t.Fatal(err)
	}

	// A model source that KNOWS the secret and reports only its presence — the shape a real
	// provider adapter has.
	configured := f.src
	configured.Model = func() model.Config {
		// A REAL model.Config holding the real secret, read the way the daemon reads it. A stub
		// that never held the credential could not leak it, and a sweep over a source that cannot
		// fail is not a sweep.
		return model.Read(withEnv(map[string]string{
			model.EnvProvider: "acme", model.EnvCredential: secret,
		}), nil)
	}
	// A source whose FAILURES mention the configuration, which is where a leak would actually
	// happen: an error path that formats what it was holding.
	leaky := f.src
	leaky.Hub = func() (*hub.Store, hub.Membership, error) {
		return nil, nil, hub.Refusedf(hub.ErrHubUnreachable, "dialling with credentials configured")
	}

	var checked int
	for _, src := range []Sources{configured, leaky, f.src} {
		for _, op := range Operations() {
			for _, grant := range []hub.GrantID{g.ID, readOnly.ID, "grant-not-issued", ""} {
				r := Answer(Request{
					Op: op, Grant: grant, NoteID: "d-1", Title: "t", Body: "b",
					Scopes: []hub.Scope{hub.ScopeRead},
				}, src)
				body, err := MarshalResponse(r)
				if err != nil {
					t.Fatalf("%s: %v", op, err)
				}
				checked++
				if strings.Contains(string(body), secret) {
					t.Errorf("criterion 13: the response to %q under grant %q contains the credential:\n%s",
						op, grant, body)
				}
			}
		}
	}
	// THE CONTROL. A scan that checked nothing passes vacuously.
	if checked < len(Operations()) {
		t.Fatalf("only %d response(s) were scanned; the sweep is not looking at anything", checked)
	}
	t.Logf("scanned %d responses across every operation for the credential", checked)
}

// TestTheModelViewServedHasNoFieldACredentialCouldBeAssignedTo is criterion 13 as a property of the
// type rather than of today's code.
//
// A `Credential string` field left empty is one careless assignment away from being populated. This
// fires when somebody adds one, before it is ever set.
//
// IT REFLECTS OVER THE TYPE THIS PACKAGE ACTUALLY SERVES, which is [model.View] and no longer a
// struct of this package's own. An earlier revision parsed api.go for a local `ModelView`; that
// version would have gone quietly green when the local type was deleted, asserting nothing about
// the type that replaced it. Reflection follows the field rather than the file.
func TestTheModelViewServedHasNoFieldACredentialCouldBeAssignedTo(t *testing.T) {
	field, ok := reflect.TypeOf(Response{}).FieldByName("Model")
	if !ok {
		t.Fatal("Response has no Model field, so this test proves nothing about what is served")
	}
	served := field.Type
	for served.Kind() == reflect.Ptr {
		served = served.Elem()
	}
	if served.Kind() != reflect.Struct {
		t.Fatalf("Response.Model is a %s, not a struct", served.Kind())
	}
	if served.NumField() == 0 {
		t.Fatalf("%s has no fields at all, so the scan below would pass vacuously", served)
	}
	for i := 0; i < served.NumField(); i++ {
		name := strings.ToLower(served.Field(i).Name)
		for _, banned := range []string{"key", "secret", "token", "password", "apikey", "credential"} {
			// `CredentialPresent` is WHETHER, not WHICH, and is the whole point of the type — so the
			// check is for a field that could hold the value, which means a string-shaped one whose
			// name is exactly the banned word or ends in it.
			if name == banned || (strings.HasSuffix(name, banned) && served.Field(i).Type.Kind() == reflect.String) {
				t.Errorf("criterion 13: %s has a field %q; the agent API must have nowhere to put a "+
					"credential (PRD §3.13)", served, served.Field(i).Name)
			}
		}
	}
	t.Logf("checked %d field(s) of %s", served.NumField(), served)
}

// ---------------------------------------------------------------------------
// No second implementation, and no transport
// ---------------------------------------------------------------------------

// TestTheAgentAPIDoesNotReimplementVisibility is Issue #12's package comment, enforced: "a second
// implementation which agrees today is exactly the hazard".
//
// It asserts two things about this package's product files:
//
//   - hub.CanRead / hub.CanReadNote are reached, through hub.Store.ListReadable and hub.Store.Read
//     (via hub.ReadThrough), which is where the predicate lives.
//   - nothing here inspects a Visibility's shape. A call to Kind(), People(), Group() or IsUnset()
//     in this package would be the beginning of a second rule.
func TestTheAgentAPIDoesNotReimplementVisibility(t *testing.T) {
	banned := map[string]string{
		"Kind":     "inspects a visibility's kind",
		"People":   "walks a visibility's named people",
		"Group":    "reads a visibility's group",
		"IsMember": "resolves group membership itself",
		"IsUnset":  "branches on an unset visibility",
	}
	fset := token.NewFileSet()
	scanned, delegations := 0, 0
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", name, perr)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "ListReadable", "Read", "ReadThrough", "PublishThrough", "EvaluateGrantRequest", "Permits":
				delegations++
			}
			if why, bad := banned[sel.Sel.Name]; bad {
				t.Errorf("%s:%d: this package %s. Visibility is hub.CanRead's and nothing else's",
					name, fset.Position(call.Pos()).Line, why)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("no product files were scanned, so this test proves nothing")
	}
	if delegations == 0 {
		t.Fatal("this package calls none of hub's visibility or authority entry points, so either it is " +
			"not deciding anything (and the surface is empty) or it is deciding for itself")
	}
	t.Logf("scanned %d product file(s); %d delegation(s) to package hub", scanned, delegations)
}

// TestTheAgentAPIHasNoTransport is PRD §3.12's "it is local", as a property of this package.
//
// The tree-wide rule — every net.Listen and net.Dial names "unix" — lives in package commands and
// in package daemon. This is the narrower statement that the agent API's own logic has no transport
// at all: no net, no http, no url, no tls, and no socket path.
func TestTheAgentAPIHasNoTransport(t *testing.T) {
	banned := map[string]bool{
		"net": true, "net/http": true, "net/url": true, "crypto/tls": true, "net/rpc": true,
		"os/exec": true,
	}
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return werr
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		scanned++
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if banned[p] {
				t.Errorf("%s imports %q. PRD §3.12: the agent API reaches the daemon over the control "+
					"API, not over the network, and this package is the logic rather than the wire", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("no files were scanned")
	}
}
