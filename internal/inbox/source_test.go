package inbox

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Helpers for the assertions that are about the SHAPE OF THE CODE rather than about its behaviour:
// that nothing here can reach a hub, that nothing here consults the clock, that nothing here
// exports a priority. Those are properties no amount of driving the command can establish — a
// driver observing zero connections observes this run, not every configuration — so they are
// asserted over the source, and every test that uses them says so in its own comment.
//
// THEY REFUSE AN EMPTY RESULT. A scan that silently finds no files passes every assertion built on
// it, which is the failure mode that makes structural tests worthless. Each helper fails the test
// rather than returning nothing.

func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	if len(out) == 0 {
		t.Fatalf("no non-test Go files found under %s — this scan would pass vacuously", dir)
	}
	return out
}

// sourcesOf returns the text of every non-test Go file in dir, keyed by path.
func sourcesOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, path := range goFiles(t, dir) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		out[path] = string(b)
	}
	return out
}

// importsOf returns the import paths of every non-test Go file in dir, keyed by path.
func importsOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	out := map[string][]string{}
	for _, path := range goFiles(t, dir) {
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, spec := range f.Imports {
			p, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("parsing %s: import %s: %v", path, spec.Path.Value, err)
			}
			out[path] = append(out[path], p)
		}
	}
	return out
}

// exportedNames returns every exported top-level identifier declared in dir: types, functions,
// methods, constants, variables, and the exported fields of exported structs.
func exportedNames(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var out []string
	add := func(name string) {
		if name != "" && ast.IsExported(name) {
			out = append(out, name)
		}
	}
	for _, path := range goFiles(t, dir) {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				add(d.Name.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, n := range s.Names {
							add(n.Name)
						}
					case *ast.TypeSpec:
						add(s.Name.Name)
						st, ok := s.Type.(*ast.StructType)
						if !ok || st.Fields == nil {
							continue
						}
						for _, field := range st.Fields.List {
							for _, n := range field.Names {
								add(n.Name)
							}
						}
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no exported identifiers found under %s — this scan would pass vacuously", dir)
	}
	return out
}
