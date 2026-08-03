package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnlyTheSignInCommandCanCreateACredential is criterion 2 — "nothing signs in silently" —
// enforced STRUCTURALLY over the whole tree, because the driven version can only exercise the
// surfaces that exist today.
//
// WHY BOTH. The driven test (TestSignInIsTheOnlyThingThatSignsAnybodyIn) runs every current `auth`
// surface and confirms no credential appears. It is the better test, and it goes stale the moment
// somebody adds a twenty-fourth command. This one cannot: it walks every product file under
// `internal/` and fails if anything other than `auth`'s own package and the sign-in command's
// completion path calls the one function that writes a credential.
//
// It is an AST walk rather than a grep for the same reason network_guard_test.go is: a regex
// matches inside comments and strings, and this file's own prose contains the words it looks for.
func TestOnlyTheSignInCommandCanCreateACredential(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	// The one function that brings a credential into existence, and the files permitted to call it.
	const writer = "Save"
	permitted := map[string]bool{
		// The sign-in command's completion path. Nothing else in package commands.
		"internal/commands/auth_cmd.go": true,
	}

	found := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		// `internal/auth` owns the function; a call inside its own package is its own business.
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/auth/") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parsing %s: %v", rel, perr)
			return nil
		}
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
			if !ok || ident.Name != "auth" || sel.Sel.Name != writer {
				return true
			}
			found++
			slash := filepath.ToSlash(rel)
			if !permitted[slash] {
				t.Errorf("%s:%d calls auth.%s. Criterion 2: a credential comes into existence only "+
					"through the sign-in command a person runs. If this is a legitimate new sign-in "+
					"surface, the Issue has to be re-read before it is added to this list.",
					slash, fset.Position(call.Pos()).Line, writer)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	// THE CHECK MUST HAVE FOUND SOMETHING. A rename of auth.Save would make this test pass by
	// looking at nothing at all, which is the failure mode of every structural test.
	if found == 0 {
		t.Fatalf("no call to auth.%s was found anywhere outside internal/auth. Either the sign-in "+
			"command no longer writes a credential, or this check is now looking for the wrong name "+
			"and is proving nothing.", writer)
	}
}

// TestSignInNeedsNoBrowserAndBindsNoPort is criterion 4's "no graphical session and no callback
// listener", enforced over the command's own source.
//
// THIS IS A STRUCTURAL GUARANTEE AND IT IS MARKED AS ONE. The driven half — that the flow runs to
// completion with nothing but a printed code and an out-of-band approval — is
// TestADeviceCodeSignInCompletesWithoutABrowserOrAPort. What this adds is that the completion
// cannot QUIETLY start depending on a browser later: the file may not reach for os/exec, may not
// listen, and may not name the commands that open a browser.
//
// Note what is NOT claimed: this test has not run on a headless machine over SSH. It establishes
// that the code has no way to want one. The whole-tree rule that every listen and dial names
// "unix" (network_guard_test.go) covers the port half for real.
func TestSignInNeedsNoBrowserAndBindsNoPort(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal", "commands", "auth_cmd.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing auth_cmd.go: %v", err)
	}

	banned := map[string]string{
		"os/exec":  "spawning a process is how a browser gets opened on the person's behalf",
		"net":      "a callback listener is exactly what a device-code flow exists to avoid",
		"net/http": "a local callback server is a listener with extra steps",
	}
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if why, bad := banned[p]; bad {
			t.Errorf("auth_cmd.go imports %q: %s (criterion 4)", p, why)
		}
	}

	// And the browser-opening incantations, by name, in case one arrives through some other package.
	var seenCode bool
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v := strings.ToLower(lit.Value)
		for _, incantation := range []string{"xdg-open", "open -a", "rundll32", "start chrome"} {
			if strings.Contains(v, incantation) {
				t.Errorf("auth_cmd.go contains %q; the sign-in prints a code, it does not open a browser", incantation)
			}
		}
		if strings.Contains(v, "code: ") {
			seenCode = true
		}
		return true
	})
	// A CONTROL. If the file stopped printing a code, every assertion above would still pass while
	// there was no device-code flow left to have this property.
	if !seenCode {
		t.Fatal("auth_cmd.go no longer prints a code for the person to enter; this test is now " +
			"asserting properties of something that is not a device-code flow")
	}
}
