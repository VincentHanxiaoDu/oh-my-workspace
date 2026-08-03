package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
// # THE RULE
//
// Two refusals carry the ordering criterion 10 is about — "the extension is consulted BEFORE the
// credential". Outside `internal/model`, which declares them, they may be referenced from EXACTLY
// ONE function: `outboxReviewModelRefusal`. Any other declaration naming one is, by construction,
// stating for itself which of the two situations a machine is in — a second copy of the ordering
// rule, free to drift, which is precisely what #112 was.
//
// # WHY THE RULE IS NOT WRITTEN AS A LIST OF IDENTIFIER NAMES
//
// The first version of this guard held `{"ErrNoModel", "ErrProviderFailedToLoad"}` and compared
// them against `sel.Sel.Name`. **QA renamed `ErrNoModel` to `ErrModelMissing` across the six files
// that reference it — leaving that map stale, the ordinary shape of a refactor that forgets a
// guard — and the guard went GREEN on a tree that still contained the cross-package bypass.** The
// `len(sites) == 0` control did not save it: the other name still matched, so `sites` had length
// one and the guard reported success while tracking half a rule. **That is #108's documented
// failure mode word for word** — "fires on `AttachmentCount`, goes green when the identical
// violation is renamed `Attachments`" — and a guard a rename silently disarms is worse than no
// guard, because it manufactures assurance.
//
// A second name-matcher with more names in it is the same defect with a longer list. So the rule
// anchors on the thing about these two values a rename CANNOT change: **their codes**. `no-model`
// and `model-provider-extension-failed-to-load` are the product's machine-readable contract — the
// whole reason `ErrProviderFailedToLoad` exists as a value distinct from `ErrNoModel` is that
// "sharing a code would make the two situations indistinguishable to exactly the caller that has no
// English to inspect". Renaming the Go identifier does not touch them; changing one is a breaking
// product change that arrives through the front door.
//
// So [orderingRuleNames] READS `internal/model` and asks which identifiers are declared to carry
// those codes. The Go names are discovered, never assumed. Rename the identifier and the guard
// follows it. Change the code and [TestTheOrderingRuleIsAnchoredToTheProductCodes] fails loudly
// rather than tracking nothing.
//
// # AND THE REFERENCE IS RESOLVED, NOT STRING-MATCHED EITHER
//
// QA also planted three evasions the first version missed, each of which compiles and each of which
// is ordinary Go:
//
//	import m ".../internal/model";  return m.ErrNoModel      — the base ident is not "model"
//	var noModel = model.ErrNoModel                           — outside any FuncDecl
//	import . ".../internal/model";  return ErrNoModel        — there is no SelectorExpr at all
//
// The second is the one to expect by accident. So the local binding for the import PATH is resolved
// per file — alias, dot-import and default name alike — and every top-level declaration is walked,
// `GenDecl` as well as `FuncDecl`.
//
// # WHAT THIS STILL CANNOT SEE, STATED RATHER THAN LEFT TO BE FOUND
//
// This is an AST walk, not a type-check. A bypass that reaches the value through a THIRD package —
// `commands.SomeExportedAlias` re-exported and then referenced — is not resolved, because that
// needs `go/types` and a full import graph. Every evasion known today is closed; that one is named
// here so the next person does not have to rediscover it.
// =================================================================================================

// theOrderingCodes is the product contract this rule is anchored to: the machine-readable codes for
// "no model is configured" and "the chosen provider's extension failed to load". These are the
// strings a script, an agent or the control API reads, and they do not move when Go names do.
var theOrderingCodes = map[string]bool{
	"no-model": true,
	"model-provider-extension-failed-to-load": true,
}

// theOneDecider is the single declaration permitted to state the rule.
//
// This IS a name, and deliberately: it is the guard's own subject, and renaming it makes the guard
// report the new name as an offender — red, and loudly. A name whose staleness fails safe is not
// the defect above, which was a name whose staleness passed.
const theOneDecider = "outboxReviewModelRefusal"

// modelPkgSuffix identifies the package that declares the two refusals, by import path rather than
// by the identifier a file happens to bind it to.
const modelPkgSuffix = "internal/model"

// orderingRuleNames discovers, from `root/internal/model`, the identifiers declared to carry the
// codes in [theOrderingCodes].
//
// THE GO NAMES ARE READ OUT OF THE SOURCE, NOT WRITTEN DOWN HERE. That is the whole repair: the
// rename that defeated the first version of this guard now simply changes which names it looks for.
func orderingRuleNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	dir := filepath.Join(root, modelPkgSuffix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s, which declares the refusals this rule is about: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", e.Name(), perr)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) && declaresOneOfTheCodes(vs.Values[i]) {
						names[name.Name] = true
					}
				}
			}
		}
	}
	return names
}

// declaresOneOfTheCodes reports whether a composite literal sets `Code:` to one of the contract
// codes. It matches the field by name and the value by content — nothing about the TYPE is
// asserted, so moving the refusal to another type does not blind it.
func declaresOneOfTheCodes(v ast.Expr) bool {
	if u, ok := v.(*ast.UnaryExpr); ok {
		v = u.X
	}
	lit, ok := v.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Code" {
			continue
		}
		bl, ok := kv.Value.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			continue
		}
		if s, err := strconv.Unquote(bl.Value); err == nil && theOrderingCodes[s] {
			return true
		}
	}
	return false
}

// modelBindings returns the local identifiers this file binds to the model package, and whether it
// was dot-imported. Alias, default name and dot-import are all resolved from the import PATH.
func modelBindings(file *ast.File) (names map[string]bool, dot bool) {
	names = map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasSuffix(path, modelPkgSuffix) {
			continue
		}
		switch {
		case imp.Name == nil:
			names[filepath.Base(path)] = true
		case imp.Name.Name == ".":
			dot = true
		case imp.Name.Name == "_":
			// imported for effect; nothing is bound to reference
		default:
			names[imp.Name.Name] = true
		}
	}
	return names, dot
}

// reviewDecisionSites names every top-level declaration in the module that states the ordering rule,
// as "<package path>.<declaration>". `internal/model` ITSELF is skipped — it DECLARES these, and a
// package cannot bypass itself — and the comparison is for EXACT DIRECTORY EQUALITY, not a prefix.
//
// A prefix skip was the sixth reach failure: `internal/model/plantedsub` declares nothing, is a
// different package, imports `internal/model` like any other caller, and is therefore exactly the
// caller this rule is about — yet a `HasPrefix(slash, "internal/model/")` skip stepped over the whole
// subtree, and a bypass planted there built clean and went green. The skip's own stated reason did
// not cover it.
//
// AND THE WALK STARTS AT THE MODULE ROOT, not at `internal`. `cmd/omw/main.go` is 21 lines of
// dispatch today, but "the only place the next bypass can be" is wherever the guard is not looking,
// and a walk rooted at `internal` was not looking at any of `cmd/`. Excluded from the walk, each for
// a reason that is not a scope judgement:
//
//	*_test.go        — tests plant these deliberately, including this file's own fixtures
//	dot-directories  — `.git`, `.github`, `.workflow`: not module source
//	testdata/        — the go tool ignores it, and so does the compiler; it is not built
//
// It takes a root rather than reading the repository directly so the guard can be pointed at a tree
// with a bypass planted in it — including a tree where the identifiers are spelled differently. A
// guard nobody has watched fail is a guard nobody has tested.
func reviewDecisionSites(t *testing.T, root string) []string {
	t.Helper()
	guarded := orderingRuleNames(t, root)
	if len(guarded) == 0 {
		t.Fatalf("no identifier in %s/%s is declared with any of the codes %v. This guard is tracking "+
			"nothing — anchor it, do not relax it", root, modelPkgSuffix, sortedRuleNames(theOrderingCodes))
	}
	fset := token.NewFileSet()
	var found []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Not module source: skip whole subtrees rather than filtering their files.
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// PRODUCT FILES ONLY, and not the package that declares the codes.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		slash := filepath.ToSlash(rel)
		// EXACT DIRECTORY, NOT PREFIX. `internal/model/anything` is a caller like any other.
		if filepath.ToSlash(filepath.Dir(slash)) == modelPkgSuffix {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		local, dot := modelBindings(file)
		if len(local) == 0 && !dot {
			return nil
		}
		pkg := filepath.ToSlash(filepath.Dir(slash))

		// EVERY TOP-LEVEL DECLARATION, not only functions. `var noModel = model.ErrNoModel` at
		// package level is idiomatic Go and lives outside any FuncDecl; the version of this guard
		// that walked only FuncDecls missed it, and QA expected that one to happen by accident.
		for _, decl := range file.Decls {
			where := declName(decl)
			if where == "" {
				continue
			}
			if statesTheRule(decl, local, dot, guarded) {
				site := pkg + "." + where
				if !contains(found, site) {
					found = append(found, site)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

// statesTheRule reports whether a declaration references one of the guarded values.
//
// PARSED, NOT GREPPED. `outbox_cmd.go` discusses both refusals at length in its comments and quotes
// the forbidden sentence; a regex would have to be taught to skip comments, and the version taught
// to skip comments is the version that skips the bypass somebody puts in a raw string.
func statesTheRule(decl ast.Decl, local map[string]bool, dot bool, guarded map[string]bool) bool {
	hit := false
	var look func(n ast.Node) bool
	look = func(n ast.Node) bool {
		if hit || n == nil {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if base, ok := sel.X.(*ast.Ident); ok && local[base.Name] && guarded[sel.Sel.Name] {
				hit = true
				return false
			}
			// Descend into the BASE only. Walking `Sel` would let `other.ErrNoModel` — a different
			// package's value that happens to share the name — read as a hit under a dot-import.
			ast.Inspect(sel.X, look)
			return false
		}
		if id, ok := n.(*ast.Ident); ok && dot && guarded[id.Name] {
			hit = true
			return false
		}
		return true
	}
	ast.Inspect(decl, look)
	return hit
}

// declName is how a site is reported: a function by its name, a var or const block by its first
// name, so an offender can be found rather than merely counted.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
				return "var " + vs.Names[0].Name
			}
		}
	}
	return ""
}

// =================================================================================================
// The rule, over this repository
// =================================================================================================

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

// THE ANCHOR ITSELF. If the codes this rule is pinned to stop existing under those names, the guard
// is tracking nothing and must say so rather than reporting a serene pass over an empty set.
//
// This is also the sentence a person gets if they change a code: these two are the product's
// machine-readable contract for telling an unconfigured model from a broken extension, and changing
// one is a breaking change for every script, agent and control-API caller that reads them.
func TestTheOrderingRuleIsAnchoredToTheProductCodes(t *testing.T) {
	names := orderingRuleNames(t, repoRoot(t))
	if len(names) != len(theOrderingCodes) {
		t.Fatalf("internal/model declares %d value(s) carrying the codes %v — %v — and there must be "+
			"exactly one per code. If a code was renamed, that is a breaking change for every caller "+
			"that reads it without English; if a value was deleted, this guard now tracks less than "+
			"the rule it exists for", len(names), sortedRuleNames(theOrderingCodes), sortedRuleNames(names))
	}
}

// =================================================================================================
// The guard, driven — including against the rename that defeated its first version
// =================================================================================================

// plantedTree writes a minimal tree: `internal/model` declaring the two refusals under the names
// given, plus the decider. Callers add whatever bypass they are testing.
//
// The refusal identifiers are PARAMETERS, which is the point: a test can spell them however it
// likes and the guard has to find them anyway.
func plantedTree(t *testing.T, noModel, failedToLoad string) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, modelPkgSuffix, "config.go"), `package model

type Error struct {
	Code string
	Msg  string
}

var `+noModel+` = &Error{
	Code: "no-model",
	Msg:  "no model is configured",
}

var `+failedToLoad+` = &Error{
	Code: "model-provider-extension-failed-to-load",
	Msg:  "the chosen model provider's extension failed to load",
}
`)
	write(t, filepath.Join(root, "internal", "commands", "outbox_cmd.go"), `package commands

import "example.test/internal/model"

func `+theOneDecider+`() error { return model.`+noModel+` }
`)
	return root
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE ACCEPTANCE TEST FOR CRITERION 4, WRITTEN FROM QA'S OWN RECIPE.
//
// The refusal is spelled `ErrModelMissing` here — not the name this repository uses today, and not
// a name written down anywhere in this file. The first version of this guard went GREEN on exactly
// this tree while the cross-package bypass sat in it. Anything that reintroduces a hard-coded name
// list fails here.
func TestTheGuardSurvivesARenameOfTheRefusalItGuards(t *testing.T) {
	const renamed = "ErrModelMissing"
	root := plantedTree(t, renamed, "ErrExtensionBroken")

	// CONTROL: the decider is seen under the new spelling. If it is not, the assertion below would
	// be measuring a broken walk rather than the rule.
	if sites := reviewDecisionSites(t, root); !contains(sites, "internal/commands."+theOneDecider) {
		t.Fatalf("control failed: with the refusals renamed the guard cannot even see the decider, so "+
			"its verdict on a bypass means nothing. Found: %v", sites)
	}

	// THE BYPASS, under the renamed value, in another package.
	write(t, filepath.Join(root, "internal", "agentapi", "answer.go"), `package agentapi

import "example.test/internal/model"

func answerDraft(configured bool) error {
	if !configured {
		return model.`+renamed+`
	}
	return nil
}
`)

	if sites := reviewDecisionSites(t, root); !contains(sites, "internal/agentapi.answerDraft") {
		t.Fatalf("A RENAME DISARMED THE GUARD. The refusal was renamed to %q — the ordinary shape of "+
			"a refactor that forgets a guard — and a cross-package bypass returning it went unnamed. "+
			"This is #108's failure mode: fires on one spelling, green on another. Found: %v",
			renamed, sites)
	}
}

// THE GUARD FIRES ON A BYPASS IN A DIFFERENT PACKAGE FROM THE DECIDER.
//
// #112 asks for this in as many words, because the three guards that failed today all passed
// against the tree they were written on and had never been shown a violation.
func TestTheReviewModeDecisionGuardFiresOnAPlantedBypass(t *testing.T) {
	root := plantedTree(t, "ErrNoModel", "ErrProviderFailedToLoad")
	write(t, filepath.Join(root, "internal", "agentapi", "answer.go"), `package agentapi

import "example.test/internal/model"

func answerDraft(configured bool) error {
	if !configured {
		return model.ErrNoModel
	}
	return nil
}
`)
	sites := reviewDecisionSites(t, root)
	if !contains(sites, "internal/commands."+theOneDecider) {
		t.Fatalf("control failed: the guard did not see the decider in the planted tree. Found: %v", sites)
	}
	if !contains(sites, "internal/agentapi.answerDraft") {
		t.Fatalf("THE GUARD DOES NOT REACH. A bypass in internal/agentapi — another package, which is "+
			"the only place the next bypass can be — went unnamed. This is the failure mode of every "+
			"guard in #108's table. Found: %v", sites)
	}
}

// THE THREE EVASIONS QA PLANTED, EACH OF WHICH COMPILES AND EACH OF WHICH IS ORDINARY GO.
//
// The middle one is the one to expect by accident: `var noModel = model.ErrNoModel` at package
// level is how anybody would shorten a long reference, and it lives outside every FuncDecl.
func TestTheGuardResolvesTheImportRatherThanMatchingTheWordModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"an aliased import", `package agentapi

import m "example.test/internal/model"

func answerDraft() error { return m.ErrNoModel }
`},
		{"a package-level var outside any function", `package agentapi

import "example.test/internal/model"

var noModel = model.ErrNoModel

func answerDraft() error { return noModel }
`},
		{"a dot import", `package agentapi

import . "example.test/internal/model"

func answerDraft() error { return ErrNoModel }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := plantedTree(t, "ErrNoModel", "ErrProviderFailedToLoad")
			write(t, filepath.Join(root, "internal", "agentapi", "answer.go"), tc.body)
			sites := reviewDecisionSites(t, root)
			named := false
			for _, s := range sites {
				if strings.HasPrefix(s, "internal/agentapi.") {
					named = true
				}
			}
			if !named {
				t.Errorf("%s evades the guard. It compiles, it is ordinary Go, and it states the "+
					"ordering rule in another package. Found: %v", tc.name, sites)
			}
		})
	}
}

// THE TWO REACH EVASIONS THE PREVIOUS ROUND WENT GREEN ON. Both compiled, both built clean in this
// repository, and both stated the rule in a package that is not the decider.
//
//	internal/model/plantedsub  — a package UNDER internal/model, skipped by a prefix that was one
//	                             character too broad. It declares nothing; it is a caller.
//	cmd/omw                    — outside `internal` entirely, where the walk did not start.
//
// These are separate cases and not a table entry apiece by accident: the first is about WHICH
// directories are skipped, the second about WHERE the walk begins, and a fix to either alone leaves
// the other open.
func TestTheGuardReachesPackagesUnderModelAndOutsideInternal(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		pkg  string
		site string
	}{
		{
			name: "a package under internal/model, which declares nothing and is a caller like any other",
			path: filepath.Join("internal", "model", "plantedsub", "bypass.go"),
			pkg:  "plantedsub",
			site: "internal/model/plantedsub.PlantedReviewDecision",
		},
		{
			name: "package main under cmd, outside internal entirely",
			path: filepath.Join("cmd", "omw", "bypass.go"),
			pkg:  "main",
			site: "cmd/omw.PlantedReviewDecision",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := plantedTree(t, "ErrNoModel", "ErrProviderFailedToLoad")
			write(t, filepath.Join(root, tc.path), `package `+tc.pkg+`

import "example.test/internal/model"

func PlantedReviewDecision(configured bool) error {
	if !configured {
		return model.ErrNoModel
	}
	return model.ErrProviderFailedToLoad
}
`)
			sites := reviewDecisionSites(t, root)
			if !contains(sites, "internal/commands."+theOneDecider) {
				t.Fatalf("control failed: the guard did not see the decider, so its verdict on the "+
					"bypass means nothing. Found: %v", sites)
			}
			if !contains(sites, tc.site) {
				t.Errorf("THE GUARD DOES NOT REACH %s. It compiles, it builds clean, and it states the "+
					"ordering rule outside the one decider. Found: %v", tc.site, sites)
			}
		})
	}
}

// AND internal/model ITSELF IS STILL NOT BLAMED. The fix above narrows a skip; this is the thing the
// skip is for, and it must survive the narrowing — a guard that blames the declaring package for
// declaring is one somebody switches off.
func TestTheGuardStillDoesNotBlameTheDeclaringPackageItself(t *testing.T) {
	root := plantedTree(t, "ErrNoModel", "ErrProviderFailedToLoad")
	write(t, filepath.Join(root, modelPkgSuffix, "helpers.go"), `package model

func WhichRefusal(configured bool) error {
	if !configured {
		return ErrNoModel
	}
	return ErrProviderFailedToLoad
}
`)
	for _, s := range reviewDecisionSites(t, root) {
		if strings.HasPrefix(s, modelPkgSuffix+".") {
			t.Errorf("the package that DECLARES the refusals was blamed for referencing them: %v", s)
		}
	}
}

// A COMMENT AND A STRING ARE NOT A DECISION, and the real `outbox_cmd.go` is full of both. If this
// were not so, the rule above would be unsatisfiable and somebody would relax it rather than the
// code.
func TestTheReviewModeDecisionGuardIgnoresCommentsAndStrings(t *testing.T) {
	root := plantedTree(t, "ErrNoModel", "ErrProviderFailedToLoad")
	write(t, filepath.Join(root, "internal", "docs", "prose.go"), `package docs

import "example.test/internal/model"

var _ = model.Error{}

// This function must never say model.ErrNoModel, which is model.ErrProviderFailedToLoad's opposite.
func explain() string {
	return "not model.ErrNoModel, and not model.ErrProviderFailedToLoad either"
}
`)
	for _, s := range reviewDecisionSites(t, root) {
		if strings.HasPrefix(s, "internal/docs.") {
			t.Errorf("prose about the rule was counted as a statement of it: %v", s)
		}
	}
}

// A DIFFERENT PACKAGE'S VALUE THAT HAPPENS TO SHARE THE NAME IS NOT THIS RULE. Without this the
// guard would blame `other.ErrNoModel` for the model package's rule, and a guard that cries wolf is
// one somebody switches off.
func TestTheGuardDoesNotBlameAnUnrelatedPackagesSameNamedValue(t *testing.T) {
	root := plantedTree(t, "ErrNoModel", "ErrProviderFailedToLoad")
	write(t, filepath.Join(root, "internal", "other", "other.go"), `package other

var ErrNoModel = &struct{ Code string }{Code: "something-else-entirely"}
`)
	write(t, filepath.Join(root, "internal", "agentapi", "answer.go"), `package agentapi

import "example.test/internal/other"

func answerDraft() error { _ = other.ErrNoModel; return nil }
`)
	for _, s := range reviewDecisionSites(t, root) {
		if strings.HasPrefix(s, "internal/agentapi.") {
			t.Errorf("an unrelated package's same-named value was blamed for the model rule: %v", s)
		}
	}
}

func sortedRuleNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
