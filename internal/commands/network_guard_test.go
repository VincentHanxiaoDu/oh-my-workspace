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

// TestEveryListenAndDialIsAUnixSocket is PRD §4.2 and §4.4's "no network connection without a hub
// configured" — Issue #12's criterion 19 — and §4.6's "the control API does not fall back to
// another transport" — Issue #2's criterion 24 — enforced by what the code DOES with the net
// package rather than by whether it may mention it.
//
// # WHY AN IMPORT BAN WAS REPLACED, AND WHY THIS IS STRICTER RATHER THAN LOOSER
//
// Criterion 19's original test banned the `net` package outright from everything the visibility
// surfaces can reach. That was right until PRD §2.1's control API existed. The control API is a
// UNIX DOMAIN SOCKET, and Go puts unix sockets in `net` — so once `omw daemon` landed, the ban and
// §2.1 could not both be satisfied: obeying it meant deleting the local control API, which is the
// opposite of what §4.6 asks for. Two pull requests, each green alone, red together.
//
// The naive repair — delete `"net"` from the banned map — WAS MEASURED AND IT OPENS A REAL HOLE.
// With `net` merely unbanned, this compiles and the whole suite passes:
//
//	func leakProbe() (net.Conn, error) { return net.Dial("tcp", "example.com:80") }
//
// A raw outbound TCP dial, green. So the ban on the IMPORT is replaced by a rule about the USE:
// every listen and every dial must name "unix". That is strictly stronger than the ban, because an
// import ban cannot tell a local socket from a connection to another machine, and this can.
// `net/http`, `net/url`, `crypto/tls` and `net/rpc` remain banned outright by the test above —
// unlike `net`, none of them has any local-IPC use.
//
// # THIS IS ONE CHECK BECAUSE TWO WERE WRITTEN, EACH MISSING WHAT THE OTHER CAUGHT
//
// Two versions of this rule were written independently and briefly both existed here. Neither was
// redundant, which is why this is a merge of them rather than a choice between them:
//
//   - One walked every file under `internal/`. Broad package coverage, but it matched with a
//     REGEX over `net.(Listen|Dial|DialTimeout)`, so `net.ListenPacket("udp", …)` — a real,
//     outbound-capable call — slipped straight through. Driven and confirmed: it passed with that
//     probe planted.
//   - The other parsed the syntax tree and matched any selector beginning `Listen` or `Dial`, so it
//     caught `ListenPacket`. But it took its package list from `go list -deps` of the command tree,
//     so a package outside that graph was unguarded.
//
// This keeps the AST matching and the whole-`internal/` walk. A regex also matches inside comments
// and string literals, which an AST walk cannot; that is a second reason the parser wins.
//
// `internal/daemon` keeps its own copy of this rule as well. That is deliberate duplication: the
// package that owns the control API must hold the line whether or not anything else imports it.
func TestEveryListenAndDialIsAUnixSocket(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	found := 0

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		// PRODUCT FILES ONLY. A test may legitimately dial a loopback listener it started itself;
		// §4.2's guarantee is about what the shipped product reaches out to.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		rel, _ := filepath.Rel(root, path)
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
			if !ok || ident.Name != "net" {
				return true
			}
			// MATCHED BY PREFIX, not by an alternation of the three names somebody thought of.
			// This is what catches ListenPacket, DialTimeout, ListenUnix and anything Go adds
			// later; the enumeration that preceded it missed ListenPacket, and a udp listener is
			// exactly the reaching-out this rule exists to prevent.
			if !strings.HasPrefix(sel.Sel.Name, "Listen") && !strings.HasPrefix(sel.Sel.Name, "Dial") {
				return true
			}
			found++
			line := fset.Position(call.Pos()).Line
			if len(call.Args) == 0 {
				t.Errorf("%s:%d: net.%s is called with no network argument", rel, line, sel.Sel.Name)
				return true
			}
			// MATCHED AS A QUOTED LITERAL, INCLUDING THE QUOTES. A substring test for `unix` is
			// satisfied by "unixgram" and by a variable named unixNet, and a half-fix that passes
			// is worse than no check — this project has been bitten by exactly that twice.
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || lit.Value != `"unix"` {
				t.Errorf("%s:%d: net.%s does not name \"unix\" as a literal network.\n"+
					"  With no hub configured nothing may reach out (PRD §4.2, §4.4; criterion 19),\n"+
					"  and the control API must not fall back to a network-reachable listener\n"+
					"  (PRD §4.6; Issue #2 criterion 24). A unix domain socket is the only\n"+
					"  transport permitted in this tree.", rel, line, sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	// THE CONTROL. Zero call sites means the scan stopped finding things, not that the product
	// stopped dialling, and those two are indistinguishable in a green run.
	if found == 0 {
		t.Fatal("found no net.Listen/net.Dial call sites at all; the scan is not looking at " +
			"anything, so its pass proves nothing. Fix the walk, do not delete the test.")
	}
	t.Logf("checked %d listen/dial call site(s) under internal/, all \"unix\"", found)
}
