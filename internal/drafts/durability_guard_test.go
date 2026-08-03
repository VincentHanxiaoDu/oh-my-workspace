package drafts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Issue #69 criterion 7: the STRUCTURAL guard.
//
// # WHY A TEST THAT READS THE SOURCE, AND NOT A BETTER BEHAVIOURAL TEST
//
// This defect did not ship because anyone thought an interrupted draft write was acceptable. It
// shipped because two writers in one tree disagreed and nothing compared them: `internal/store`
// wrote records through a temporary, an fsync and a rename, and this package — the one holding
// people's unsent words — wrote straight into the destination. Each half was correct on its own
// terms and each had passing tests. The asymmetry was the defect, and asymmetry is not something a
// behavioural test can see: you can only find it by asking every writer the same question.
//
// So this asks it. A file in this package may reach a destination path only through the three
// helpers in outbox.go that fsync and then rename or link. Anything else — os.WriteFile, os.Create,
// an os.OpenFile with O_CREATE — creates a destination that exists before its contents do, which is
// precisely the window a SIGKILL landed in sixty times out of sixty.
//
// # WHAT IT DOES NOT COVER, STATED RATHER THAN IMPLIED
//
// It guards THIS package. It does not walk the tree: `internal/inbox`, `internal/projects` and
// anything else that writes a person's material are not examined here, and a guard that claimed to
// cover them while only parsing this directory would be worse than one that says where it stops.
// Widening it is recorded on the rolling debt Issue #32 rather than smuggled into this branch.

// durableWriters are the only functions permitted to open a file for writing in this package.
// Each writes to a staging name, fsyncs it, and only then gives it the name a reader will look up.
var durableWriters = map[string]bool{
	"writeFileSynced":   true,
	"linkFileSynced":    true,
	"replaceFileSynced": true,
}

// Create is the one other function allowed to make a directory, and the reason is the marker file.
//
// A half-created OUTBOX is not the shape #69 is about, because a directory without `.omw-outbox`
// is not an outbox: [Open] refuses it by name rather than reading it as an empty one, and the
// marker itself is written durably. The invalid state is unrepresentable there already, by the
// same argument this guard exists to enforce everywhere else — so it is named here, with its
// reason, rather than left to look like an oversight.
const outboxRootMaker = "Create"

// nonDurableCalls are the ways a Go program creates a file that is visible before it is written.
var nonDurableCalls = map[string]string{
	"WriteFile": "os.WriteFile truncates the destination first, so a reader between the truncate and the write sees an empty file where a person's words were",
	"Create":    "os.Create truncates the destination first, with the same window",
	"OpenFile":  "os.OpenFile with O_CREATE makes the destination exist before anything is in it",
	"Mkdir":     "os.Mkdir makes a draft's directory visible before its contents; a draft is renamed into place whole or not at all",
	"MkdirAll":  "os.MkdirAll makes a draft's directory visible before its contents; a draft is renamed into place whole or not at all",
}

func TestEveryWriteInThisPackageGoesThroughADurableWriter(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no package was parsed, so this guard has examined NOTHING and is not a pass")
	}

	examined, guarded := 0, 0
	declared := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			examined++
			// The enclosing function, tracked so a finding can name it.
			var fn string
			ast.Inspect(file, func(n ast.Node) bool {
				if decl, ok := n.(*ast.FuncDecl); ok {
					fn = decl.Name.Name
					declared[fn] = true
					return true
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "os" {
					return true
				}
				why, bad := nonDurableCalls[sel.Sel.Name]
				if !bad {
					return true
				}
				if durableWriters[fn] {
					guarded++
					return true
				}
				if fn == outboxRootMaker && sel.Sel.Name == "MkdirAll" {
					return true
				}
				// MkdirTemp and CreateTemp are how the staging name is made, and a staging name is
				// not a destination — nothing reads it. They are distinct identifiers, so they are
				// simply not in the map above; this branch is the real finding.
				t.Errorf("%s: %s calls os.%s directly.\n  %s\n  Use one of %v, which fsync and then "+
					"rename or link — see Issue #69, where a draft directory that existed before its "+
					"state file made the product report a destroyed draft as a healthy one.",
					fset.Position(call.Pos()), fn, sel.Sel.Name, why, keysOf(durableWriters))
				return true
			})
		}
	}
	if examined == 0 {
		t.Fatal("no file was examined")
	}
	// AN ALLOWLIST ENTRY THAT NAMES NOTHING IS A HOLE. A renamed or deleted helper would otherwise
	// leave this guard permitting a function that no longer exists while enforcing nothing.
	for name := range durableWriters {
		if !declared[name] {
			t.Errorf("this guard permits writes inside %q and no such function exists in the package, "+
				"so the allowlist has drifted away from the code it is meant to describe", name)
		}
	}
	if !declared[outboxRootMaker] {
		t.Errorf("this guard exempts %q and no such function exists in the package", outboxRootMaker)
	}
	if guarded == 0 {
		t.Errorf("%d files were parsed and NOT ONE durable write was found in them, which means this "+
			"guard is matching nothing and would stay green if every write in the package were direct",
			examined)
	}
	t.Logf("%d files examined, %d writes inside the durable writers", examined, guarded)
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
