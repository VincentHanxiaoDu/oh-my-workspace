package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// =================================================================================================
// ISSUE #112 CRITERION 4 — THE GUARD THAT COUNTS THE SITES, BECAUSE NOBODY WAS COUNTING.
//
// # WHY THIS IS SHAPED THE WAY IT IS
//
// Three times in one day the same defect: a check placed at a caller, and a second caller that did
// not know about it.
//
//	#10  publish.Transfer's review gate   bypassed by agentapi.answerPublish -> hub.PublishThrough
//	#21  outboxExtensionRefusal           bypassed by omw outbox draft's own review-mode gate
//	#21  ExtensionFactory                 bypassed by LoopFactory defaulting to Builtin
//
// And each time the guard written to prevent it could not see the bypass, for the SAME reason the
// bypass existed: `TestEveryPathToTheHubPassesTheGate` scanned `package publish` only, and the
// criterion-9 test drove the factory directly. **A guard whose reach is one package cannot catch
// the caller in another package — which is the only place the next bypass can be.** #108's table
// calls that a reach failure; this is the fifth.
//
// So the rule here is enforced over EVERY package under `internal/`, not over the package the
// decision lives in, and `TestTheReviewModeDecisionGuardFiresOnAPlantedBypass` proves it reaches by
// planting a bypass in a DIFFERENT package and requiring it to be named.
//
// # THE RULE
//
// `model.ErrNoModel` and `model.ErrProviderFailedToLoad` are the two answers whose ORDERING is
// criterion 10 — "the extension is consulted BEFORE the credential". Outside `internal/model`,
// which defines them, they may be referenced from EXACTLY ONE function:
// `outboxReviewModelRefusal`. Any other function that names one is, by construction, stating for
// itself which of the two situations a machine is in — a second copy of the ordering rule, free to
// drift from the first, which is precisely what #112 was.
//
// This is a rule about the machine-readable codes rather than about the prose, and deliberately:
// the prose "no model is configured" appears legitimately in several places that are DENYING it
// (`omw model show` says "This is NOT a report that no model is configured"), so a prose rule would
// have to carry an exception list, and an exception list is where the next bypass hides. The codes
// have no such honest second use — a capability that hands one to a person has decided.
// =================================================================================================

// theOrderingRule is the identifier pair whose ordering criterion 10 is about.
var theOrderingRule = map[string]bool{
	"ErrNoModel":              true,
	"ErrProviderFailedToLoad": true,
}

// theOneDecider is the single function permitted to state it.
const theOneDecider = "outboxReviewModelRefusal"

// reviewDecisionSites names every function under `root/internal` that states the ordering rule, as
// "<package path>.<function>". `internal/model` is skipped: it DEFINES these, and a package cannot
// bypass itself.
//
// It takes a root rather than reading the repository directly so that the guard can be pointed at a
// tree with a bypass planted in it. A guard nobody has watched fail is a guard nobody has tested.
func reviewDecisionSites(t *testing.T, root string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var found []string

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		// PRODUCT FILES ONLY, and not the package that defines the codes.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/model/") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		// PARSED, NOT GREPPED. `outbox_cmd.go` discusses both codes at length in its comments and
		// quotes the forbidden sentence; a regex would have to be taught to skip comments, and the
		// version of this rule that was taught to skip comments is the version that skips the
		// bypass somebody puts in a raw string.
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "model" || !theOrderingRule[sel.Sel.Name] {
					return true
				}
				site := filepath.ToSlash(filepath.Dir(rel)) + "." + fn.Name.Name
				for _, already := range found {
					if already == site {
						return true
					}
				}
				found = append(found, site)
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

// THE RULE, OVER THIS REPOSITORY.
func TestTheReviewModeModelRefusalIsMadeInExactlyOnePlace(t *testing.T) {
	sites := reviewDecisionSites(t, repoRoot(t))

	// CONTROL FIRST. An empty result and a broken walk are the same value, and a guard that found
	// nothing because it looked nowhere passes silently forever.
	if len(sites) == 0 {
		t.Fatalf("this guard found no site at all, including the decider it knows exists. It is not " +
			"measuring anything — check the walk, not the code")
	}

	want := "internal/commands." + theOneDecider
	for _, site := range sites {
		if site == want {
			continue
		}
		t.Errorf("%s decides for itself whether this machine has no model or a broken extension.\n"+
			"That ordering is criterion 10 and it belongs to %s alone: a second copy is free to drift "+
			"from the first, which is what #112 was — `omw outbox draft` told a person with a broken "+
			"extension \"no model is configured\", one gate over from where #21 had closed it.\n"+
			"Call %s instead of restating it.", site, want, want)
	}
	if len(sites) != 1 {
		t.Errorf("the ordering rule is stated in %d places: %v", len(sites), sites)
	}
}

// THE GUARD ITSELF, DRIVEN AGAINST A BYPASS PLANTED IN A DIFFERENT PACKAGE FROM THE DECIDER.
//
// #112 asks for this in as many words — "Assert it fires by planting a bypass in a different
// package from the gate" — because the three guards that failed today all passed against the tree
// they were written on and had never been shown a violation. A guard that has only ever been green
// is an untested function.
//
// The bypass planted is the shape the next one will really have: another capability, in another
// package, refusing with `model.ErrNoModel` on its own authority.
func TestTheReviewModeDecisionGuardFiresOnAPlantedBypass(t *testing.T) {
	root := t.TempDir()

	// The decider itself, so the planted tree is not merely a tree with one file in it: the guard
	// must pick the bypass OUT, not merely notice that something exists.
	decider := filepath.Join(root, "internal", "commands")
	if err := os.MkdirAll(decider, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(decider, "outbox_cmd.go"), `package commands

func `+theOneDecider+`() error { return model.ErrNoModel }
`)

	// THE BYPASS, IN ANOTHER PACKAGE — the agent API, which is exactly where #10's bypass of
	// publish.Transfer lives, so this is not a hypothetical shape.
	other := filepath.Join(root, "internal", "agentapi")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(other, "answer.go"), `package agentapi

func answerDraft(configured bool) error {
	if !configured {
		return model.ErrNoModel
	}
	return nil
}
`)

	sites := reviewDecisionSites(t, root)

	// CONTROL: the decider in the planted tree is seen. If it is not, the walk is broken and the
	// assertion below would be measuring the walk rather than the rule.
	if !contains(sites, "internal/commands."+theOneDecider) {
		t.Fatalf("control failed: the guard did not even see the decider in the planted tree, so its "+
			"verdict on the bypass means nothing. Found: %v", sites)
	}
	if !contains(sites, "internal/agentapi.answerDraft") {
		t.Fatalf("THE GUARD DOES NOT REACH. A bypass in internal/agentapi — another package, which is "+
			"the only place the next bypass can be — went unnamed. This is the failure mode of every "+
			"guard in #108's table. Found: %v", sites)
	}
}

// A COMMENT AND A STRING ARE NOT A DECISION, and the real `outbox_cmd.go` is full of both. If this
// were not so, the rule above would be unsatisfiable and somebody would relax it rather than the
// code.
func TestTheReviewModeDecisionGuardIgnoresCommentsAndStrings(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "prose.go"), `package docs

// This function must never say model.ErrNoModel, which is model.ErrProviderFailedToLoad's opposite.
func explain() string {
	return "not model.ErrNoModel, and not model.ErrProviderFailedToLoad either"
}
`)
	if sites := reviewDecisionSites(t, root); len(sites) != 0 {
		t.Errorf("prose about the rule was counted as a statement of it: %v", sites)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
