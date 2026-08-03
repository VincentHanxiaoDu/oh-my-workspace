// Package kindguard fails the build when a store kind is READ by some package and WRITTEN by none.
//
// # WHY THIS EXISTS (Issue #67, criterion 6)
//
// Three defects shipped in one cycle with the same shape: a reader that was correct, a producer
// that was never built, and an absence rendered as a determined zero.
//
//   - `omw report run` reported "no activity in this period" for `git`, forever, on every machine.
//   - `omw diagnostics` reported `draft-inventory  collected (0)` while two drafts existed.
//   - A third, `message`, is still here and is declared below.
//
// Every one of them passed every branch gate and three UAT sweeps, because each HALF is right on
// its own. The reader has a test proving it reads correctly. The writer, where there is one, has a
// test proving it writes correctly. Nothing anywhere compares the two halves, and a case test for
// any single instance would not have found the other two. That is the defect class this package is
// aimed at, not any one of its instances.
//
// # WHAT IT CHECKS
//
// A store kind is a directory name (see internal/store's Kind doc). A directory nobody has ever
// written to reads perfectly well — as zero records — so an unwritten kind is not a crash, a nil,
// or an error. It is a confident, cheerful, wrong zero. This package parses the product's own
// source, resolves each store kind flowing into a read call and into a write call, and reports the
// kinds that are read and never written.
//
// # WHAT IT DELIBERATELY DOES NOT DO
//
// It does not type-check. It matches on the store API's method names and on kind expressions it can
// resolve statically, which means it can be defeated by an indirection — a kind passed through a
// function parameter, or built at runtime from a string. Those are reported SEPARATELY as
// [Report.Unresolved] rather than silently passing, because a check that quietly stopped looking is
// the exact failure it was written to catch. The test over this repository asserts the unresolved
// set as well, so a new indirection has to be looked at rather than absorbed.
package kindguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// storeMethod is one method of *store.Store, by the shape of its call.
//
// ARITY IS PART OF THE MATCH, and it is what keeps the analysis off methods that merely share a
// name. internal/diagnostics has its own `List(st, wantBodies)` on a record source; store's is
// `List(kind)`. Matching on the name alone put every one of those in the unresolved list, which is
// noise that makes the list unreadable and therefore unenforceable.
type storeMethod struct {
	// KindArg is the argument position the kind is in.
	KindArg int
	// Args is exactly how many arguments the store's method takes.
	Args int
}

// readMethods are the store methods that ANSWER a question about a kind. A read of a kind nobody
// writes is what this package exists to find.
var readMethods = map[string]storeMethod{
	"Get":     {KindArg: 0, Args: 2},
	"GetJSON": {KindArg: 0, Args: 3},
	"List":    {KindArg: 0, Args: 1},
}

// writeMethods are the store methods that PUT records under a kind, with the argument position the
// kind is in.
//
// Delete IS NOT HERE, ON PURPOSE. A kind that is only ever deleted from is still a kind nothing
// writes, and counting a delete as a writer would let exactly the defect this package looks for
// hide behind a cleanup path.
var writeMethods = map[string]storeMethod{
	"PutJSON":   {KindArg: 0, Args: 3},
	"PutStream": {KindArg: 0, Args: 3},
}

// writeStructs are the composite literals that carry a kind INTO a write — `store.Record{Kind: …}`
// passed to Put, and `store.Op{Kind: …}` appended to a batch.
var writeStructs = map[string]bool{"Record": true, "Op": true}

// Use is one place a kind is read or written.
type Use struct {
	// Kind is the store kind, as the string that names its directory.
	Kind string
	// Pkg is the package directory the use is in.
	Pkg string
	// Pos is file:line, so a finding can be acted on rather than merely counted.
	Pos string
}

// Finding is a kind read by at least one package and written by none.
type Finding struct {
	Kind string
	// Reads is every place the kind is read. Never empty — a kind nobody reads and nobody writes is
	// not a defect, it is a spare constant.
	Reads []Use
	// Declared is the reason this finding is tolerated, from [Declared]. Empty means it is not.
	Declared string
}

func (f Finding) String() string {
	var where []string
	for _, r := range f.Reads {
		where = append(where, r.Pos)
	}
	return fmt.Sprintf("store kind %q is read at %s and written nowhere", f.Kind, strings.Join(where, ", "))
}

// Report is one analysis.
type Report struct {
	// Violations are kinds read and never written, DECLARED ONES INCLUDED. A caller decides what to
	// do about a declared one; this package does not hide it.
	Violations []Finding
	// Unresolved is every read whose kind could not be pinned to a literal — an indirection this
	// package cannot see through. Reported, never assumed harmless.
	Unresolved []Use
	// Reads and Writes are every resolved use, so a test can assert the analysis examined something
	// rather than passing because it found nothing at all.
	Reads, Writes []Use
}

// Declared are the read-with-no-writer findings this repository knowingly carries, each with the
// Issue that will settle it.
//
// A DECLARATION IS NOT A SUPPRESSION. The finding is still in [Report.Violations]; the declaration
// only says a human has seen it and where the decision lives. The test over this repository fails
// on any UNDECLARED violation and also on any declaration that no longer corresponds to a real
// finding, so an entry cannot outlive the thing it excuses.
var Declared = map[string]string{
	"message": "Issue #32 (debt): `omw diagnostics` counts raw ingested messages under this kind, and " +
		"channel ingestion stores TICKETS rather than raw messages, so nothing writes it. Issue #67 " +
		"named three surfaces and this is a fourth; recorded rather than widened into that change.",
}

// Analyze parses every non-test Go file under fsys and reports the kinds read with no writer.
//
// fsys is a tree of Go source — os.DirFS over the repository root in the check that matters.
func Analyze(fsys fs.FS) (Report, error) {
	files, err := goFiles(fsys)
	if err != nil {
		return Report{}, err
	}
	if len(files) == 0 {
		// A SCAN THAT EXAMINED NOTHING IS NOT A PASS. Every conclusion here is a negative one, and a
		// negative over an empty set is the "green that means nothing" this package is about.
		return Report{}, fmt.Errorf("kindguard: no non-test Go files were found, so nothing was analysed")
	}

	fset := token.NewFileSet()
	parsed := make(map[string]*ast.File, len(files))
	for _, name := range files {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return Report{}, fmt.Errorf("kindguard: reading %s: %w", name, err)
		}
		f, err := parser.ParseFile(fset, name, body, parser.SkipObjectResolution)
		if err != nil {
			return Report{}, fmt.Errorf("kindguard: parsing %s: %w", name, err)
		}
		parsed[name] = f
	}

	g := &globals{kinds: map[string][]string{}}
	// Constants first: a collection literal may name a constant declared in another file.
	for _, name := range files {
		g.collectConstants(parsed[name])
	}
	for _, name := range files {
		g.collectCollections(parsed[name])
	}

	var rep Report
	for _, name := range files {
		scanFile(fset, parsed[name], name, g, &rep)
	}

	written := map[string]bool{}
	for _, w := range rep.Writes {
		written[w.Kind] = true
	}
	byKind := map[string][]Use{}
	for _, r := range rep.Reads {
		if written[r.Kind] {
			continue
		}
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}
	for kind, reads := range byKind {
		rep.Violations = append(rep.Violations, Finding{Kind: kind, Reads: reads, Declared: Declared[kind]})
	}
	sort.Slice(rep.Violations, func(i, j int) bool { return rep.Violations[i].Kind < rep.Violations[j].Kind })
	return rep, nil
}

// Undeclared returns the violations nobody has accounted for. This is what a test fails on.
func (r Report) Undeclared() []Finding {
	var out []Finding
	for _, v := range r.Violations {
		if v.Declared == "" {
			out = append(out, v)
		}
	}
	return out
}

// StaleDeclarations names declarations that match no finding — a kind that has since acquired a
// writer, or been removed. They are failures too: a declaration nobody removes is how the next
// instance of this defect gets waved through.
func (r Report) StaleDeclarations() []string {
	found := map[string]bool{}
	for _, v := range r.Violations {
		found[v.Kind] = true
	}
	var out []string
	for kind := range Declared {
		if !found[kind] {
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}

func goFiles(fsys fs.FS) ([]string, error) {
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "openspec", ".workflow", ".github":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		out = append(out, p)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// globals holds every identifier this analysis can resolve to a set of kinds: the kind constants,
// and the package-level collections built out of them.
type globals struct{ kinds map[string][]string }

func (g *globals) add(name string, kinds ...string) {
	for _, k := range kinds {
		for _, have := range g.kinds[name] {
			if have == k {
				return
			}
		}
		g.kinds[name] = append(g.kinds[name], k)
	}
}

// collectConstants finds a kind declared at package level, in either spelling:
//
//	const KindTicket = store.Kind("ticket")     a conversion
//	const GrantKind store.Kind = "agent-grant"  a typed declaration
//
// BOTH, BECAUSE THE PRODUCT USES BOTH. Understanding only the first left internal/agentapi entirely
// unresolved — a whole package's kinds unchecked, and reported as such by [Report.Unresolved],
// which is the only reason it was noticed.
func (g *globals) collectConstants(f *ast.File) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := kindConversion(vs.Values[i]); ok {
					g.add(name.Name, lit)
					continue
				}
				if isKindType(vs.Type) {
					if lit, ok := stringLiteral(vs.Values[i]); ok {
						g.add(name.Name, lit)
					}
				}
			}
		}
	}
}

// collectCollections finds package-level maps and slices whose values are kinds, so that a read
// driven from `m[name]` or from a range over one is resolvable rather than unresolved.
func (g *globals) collectCollections(f *ast.File) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				cl, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range cl.Elts {
					value := elt
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						value = kv.Value
					}
					if kinds, ok := g.resolve(value, nil); ok {
						g.add(name.Name, kinds...)
					}
				}
			}
		}
	}
}

// resolve turns an expression into the kinds it can be, or reports that it cannot be pinned.
func (g *globals) resolve(e ast.Expr, locals map[string][]string) ([]string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return nil, false
		}
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			return nil, false
		}
		return []string{s}, true
	case *ast.CallExpr:
		if lit, ok := kindConversion(x); ok {
			return []string{lit}, true
		}
		// A conversion of something that is not a literal — `store.Kind(prefix + subject)`. That is
		// a kind computed at runtime and this package says so rather than guessing.
		return nil, false
	case *ast.Ident:
		if k, ok := locals[x.Name]; ok {
			return k, true
		}
		if k, ok := g.kinds[x.Name]; ok {
			return k, true
		}
		return nil, false
	case *ast.SelectorExpr:
		if k, ok := g.kinds[x.Sel.Name]; ok {
			return k, true
		}
		return nil, false
	case *ast.IndexExpr:
		// m[whatever] is one of m's values.
		return g.resolve(x.X, locals)
	case *ast.ParenExpr:
		return g.resolve(x.X, locals)
	}
	return nil, false
}

// isKindType reports whether a declaration is typed `store.Kind` or `Kind`.
func isKindType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "Kind"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Kind"
	}
	return false
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// kindConversion recognises `store.Kind("lit")` and `Kind("lit")`.
func kindConversion(e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	named := ""
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		named = fn.Name
	case *ast.SelectorExpr:
		named = fn.Sel.Name
	}
	if named != "Kind" {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func scanFile(fset *token.FileSet, f *ast.File, name string, g *globals, rep *Report) {
	pkg := path.Dir(name)
	at := func(p token.Pos) string { return fset.Position(p).String() }
	imported := importedNames(f)

	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		locals := localKinds(fn.Body, g)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				// A CALL ON A PACKAGE IS NOT A CALL ON A STORE. `inbox.Get(s, id)` and
				// `model.List(s)` are package functions that happen to share a name with a store
				// method; counting them would fill the unresolved list with noise and, worse, could
				// invent a writer out of a function called PutJSON somewhere else.
				if id, ok := sel.X.(*ast.Ident); ok && imported[id.Name] {
					return true
				}
				if m, ok := readMethods[sel.Sel.Name]; ok && len(x.Args) == m.Args {
					record(g, locals, x.Args[m.KindArg], Use{Pkg: pkg, Pos: at(x.Pos())}, &rep.Reads, &rep.Unresolved)
				}
				if m, ok := writeMethods[sel.Sel.Name]; ok && len(x.Args) == m.Args {
					// An unresolvable WRITE is not recorded as unresolved: an unresolved write
					// cannot excuse anything, and listing it would invite somebody to treat it as
					// if it could.
					record(g, locals, x.Args[m.KindArg], Use{Pkg: pkg, Pos: at(x.Pos())}, &rep.Writes, nil)
				}
			case *ast.CompositeLit:
				elts := x.Elts
				if arr, ok := x.Type.(*ast.ArrayType); ok && isWriteStruct(arr.Elt) {
					// `[]store.Op{{Kind: …}}` — the element literals elide their type, so they are
					// reached from the slice rather than on their own. internal/inbox writes its
					// batches exactly like this.
					var inner []ast.Expr
					for _, e := range elts {
						if cl, ok := e.(*ast.CompositeLit); ok && cl.Type == nil {
							inner = append(inner, cl.Elts...)
						}
					}
					elts = inner
				} else if !isWriteStruct(x.Type) {
					return true
				}
				for _, elt := range elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Kind" {
						continue
					}
					record(g, locals, kv.Value, Use{Pkg: pkg, Pos: at(x.Pos())}, &rep.Writes, nil)
				}
			}
			return true
		})
		return true
	})
}

func record(g *globals, locals map[string][]string, e ast.Expr, at Use, into *[]Use, unresolved *[]Use) {
	kinds, ok := g.resolve(e, locals)
	if !ok {
		if unresolved != nil {
			*unresolved = append(*unresolved, at)
		}
		return
	}
	for _, k := range kinds {
		u := at
		u.Kind = k
		*into = append(*into, u)
	}
}

// localKinds tracks the variables inside one function that hold a kind: `k := SomeKind`,
// `kind := someMap[name]`, `for _, k := range someKinds`.
func localKinds(body *ast.BlockStmt, g *globals) map[string][]string {
	locals := map[string][]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) != 1 || len(x.Rhs) != 1 {
				return true
			}
			id, ok := x.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if kinds, ok := g.resolve(x.Rhs[0], locals); ok {
				locals[id.Name] = kinds
			}
		case *ast.RangeStmt:
			id, ok := x.Value.(*ast.Ident)
			if !ok {
				return true
			}
			if kinds, ok := g.resolve(x.X, locals); ok {
				locals[id.Name] = kinds
			}
		}
		return true
	})
	return locals
}

// importedNames is every package name this file can call through, so a call on one of them is not
// mistaken for a call on a store.
func importedNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = true
	}
	return out
}

func isWriteStruct(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return writeStructs[t.Name]
	case *ast.SelectorExpr:
		return writeStructs[t.Sel.Name]
	}
	return false
}
