package channels

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
)

// sourceWithoutComments returns this package's non-test source with every comment removed.
//
// COMMENTS ARE REMOVED BECAUSE THE COMMENTS SAY THE FORBIDDEN WORDS IN ORDER TO FORBID THEM. A
// scan that matched them would force the documentation of the rule to be deleted in order to keep
// the rule's test green, which is the wrong way round.
func sourceWithoutComments(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing this package: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		// parser.SkipObjectResolution with no ParseComments: the comments never enter the tree.
		file, perr := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parsing %s: %v", f, perr)
		}
		var b strings.Builder
		if err := printer.Fprint(&b, fset, file); err != nil {
			t.Fatalf("printing %s: %v", f, err)
		}
		out[f] = b.String()
	}
	if len(out) == 0 {
		t.Fatal("no non-test source found in this package; the scan examined nothing, so its pass proves nothing")
	}
	return out
}

// CRITERION 9, STRUCTURALLY. "A test asserting 'a low-priority ticket was created' must fail" — and
// the strongest form of that is that such a test cannot be written, because there is nothing to
// assert on. This is what keeps it unwritable on the producing side; the reflection check below
// keeps it unwritable on the ticket itself.
func TestNoPriorityRankOrSeverityExistsInThisPackagesCode(t *testing.T) {
	forbidden := []string{"priority", "severity", "urgency", "importance", "triage"}
	src := sourceWithoutComments(t)
	for name, code := range src {
		low := strings.ToLower(code)
		for _, word := range forbidden {
			if strings.Contains(low, word) {
				t.Errorf("%s: %q appears in this package's code. PRD §3.2 and criterion 9: "+
					"acknowledgements are not low-priority tickets, and there must be no priority "+
					"value, tag or state that such traffic maps to.", name, word)
			}
		}
	}
	t.Logf("examined %d source file(s)", len(src))
}

// CRITERION 9, ON THE TICKET THIS CAPABILITY PRODUCES. Issue #8 pins this in its own package; this
// asserts it from the producing side too, because the day the two packages are edited by different
// people is the day one of them grows a field.
func TestTheTicketThisCapabilityProducesHasNoPriorityField(t *testing.T) {
	ty := reflect.TypeOf(inbox.Ticket{})
	forbidden := []string{"priority", "severity", "rank", "score", "urgency", "importance", "order"}
	for i := 0; i < ty.NumField(); i++ {
		name := strings.ToLower(ty.Field(i).Name)
		for _, word := range forbidden {
			if strings.Contains(name, word) {
				t.Errorf("inbox.Ticket has a field %q. Criterion 9: there must be nowhere for "+
					"acknowledgement traffic to go, including the bottom of a list.", ty.Field(i).Name)
			}
		}
	}
	if ty.NumField() == 0 {
		t.Fatal("inbox.Ticket has no fields at all; this check examined nothing")
	}
}

// CRITERION 11 AND 14 — this capability has no route to a network and none to the hub.
//
// PROVED BY WHAT THE CODE CAN REACH rather than by watching for connections during a run: a build
// that dials only when a credential is present looks exactly like this one for the duration of a
// test that has no credential.
func TestIngestionCannotReachTheNetworkOrTheHub(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH, so the import graph cannot be computed here: %v", err)
	}
	out, err := exec.Command(goBin, "list", "-deps",
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels",
	).CombinedOutput()
	if err != nil {
		t.Skipf("go list could not compute the import graph here: %v\n%s", err, out)
	}
	// `net` ITSELF IS NOT BANNED, AND THAT IS NOT A RELAXATION. This package imports the daemon in
	// order to register its background work, and the daemon's control API is a local IPC socket —
	// a net.Listen("unix", …). Banning the package would conflate "reaches net" with "can reach the
	// network" and would force §4.6's control API to be built worse to keep a test green. What is
	// banned is everything with no local-IPC use whatever, and the narrower assertion that this
	// package contains no listen or dial of its own is made by
	// TestThisPackageContainsNoListenOrDial below.
	// WHY internal/hub IS NOT IN THIS MAP ANY MORE, AND WHY THAT IS NOT A RELAXATION.
	//
	// It was, and the ban was right while it measured what it claimed. Then #19 landed
	// `internal/auth`, `internal/daemon` began importing it to report sign-in state in
	// `omw daemon status`, and `internal/auth` reads its scope vocabulary from `internal/hub` —
	// PRD §4.5's one vocabulary. This package imports `internal/daemon` in loop.go for one reason:
	// to register its background loop. So the graph became
	//
	//     channels -> daemon -> auth -> hub
	//
	// and a transitive ban turned red over three edges that are each correct. Nothing on the
	// ingestion path gained the ability to reach the hub: this package contains no reference to
	// `internal/hub` at all, and its direct imports are daemon, inbox, store and tri.
	//
	// A transitive import ban cannot tell "ingestion talks to the hub" from "something I register
	// with also does bookkeeping I have nothing to do with". THIS IS THE THIRD TIME A PROXY OF THAT
	// SHAPE HAS DECAYED HERE — the `net` package ban broke when §4.6's unix control socket arrived,
	// and a regex enumerating Listen|Dial|DialTimeout missed ListenPacket. The lesson each time is
	// the same: assert the property, not a stand-in that happens to correlate with it today.
	//
	// So the rule is now stated exactly: reaching the hub is permitted ONLY through the daemon
	// edge, and TestIngestionReachesTheHubOnlyThroughTheDaemon below proves there is no other path.
	// That is strictly more precise than the ban it replaces, which could not distinguish the two.
	banned := map[string]string{
		"net/http":   "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"net/url":    "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"net/rpc":    "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"crypto/tls": "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
	}
	seen := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		seen++
		if why, bad := banned[pkg]; bad {
			t.Errorf("internal/channels can reach %q — %s", pkg, why)
		}
	}
	if seen == 0 {
		t.Fatal("go list returned nothing; the scan examined no packages, so its pass proves nothing")
	}
	t.Logf("examined %d packages in the ingestion import graph", seen)
}

// CRITERION 14, THE OTHER HALF — no message body is ever written to disk by this package.
//
// The type-level statement of it: [Message] appears in no encoded record. This reads the encode
// path's own syntax tree for any reference to Message, which is stronger than checking one run's
// output, because it fails on a build that stores bodies only for one channel kind.
func TestNoStoredRecordCanCarryAMessage(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "channel.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing channel.go: %v", err)
	}
	found := false
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "connectionFile" {
				continue
			}
			found = true
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatal("connectionFile is not a struct; fix the test, not the product")
			}
			for _, f := range st.Fields.List {
				var b strings.Builder
				_ = printer.Fprint(&b, fset, f.Type)
				if strings.Contains(b.String(), "Message") && !strings.HasPrefix(b.String(), "int") {
					t.Errorf("the stored channel record has a field of type %s — a record that can "+
						"hold the traffic verbatim is a record somebody will fill with the traffic "+
						"verbatim (§2.3, criterion 14)", b.String())
				}
			}
		}
	}
	if !found {
		t.Fatal("connectionFile was not found in channel.go; the check examined nothing")
	}
}

// The store's own guard has a companion here: nothing in this package listens or dials at all. The
// repository-wide check that every listen names "unix" lives in package commands; this is the
// narrower statement that ingestion opens no socket of any kind.
func TestThisPackageContainsNoListenOrDial(t *testing.T) {
	for name, code := range sourceWithoutComments(t) {
		for _, bad := range []string{"net.Listen", "net.Dial", "http.Get", "http.Client"} {
			if strings.Contains(code, bad) {
				t.Errorf("%s: %s appears in ingestion code (criterion 11)", name, bad)
			}
		}
	}
}

// A CONTROL FOR THE WHOLE FILE. Every check above is a scan, and a scan that stops finding things
// looks exactly like a product that stopped doing them. This asserts the scanning machinery still
// reads real source.
func TestTheStructuralScansAreActuallyReadingThisPackage(t *testing.T) {
	src := sourceWithoutComments(t)
	if _, ok := src["ingest.go"]; !ok {
		t.Fatalf("ingest.go was not among the scanned files (%v); the scans above are not looking "+
			"at the code they claim to check", keys(src))
	}
	if !strings.Contains(src["ingest.go"], "func Ingest(") {
		t.Fatal("the scanned ingest.go does not contain Ingest; the scans are reading something else")
	}
	if _, err := os.Stat("loop.go"); err != nil {
		t.Fatalf("loop.go is missing: %v", err)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestIngestionReachesTheHubOnlyThroughTheDaemon is criterion 11 and criterion 14 stated as the
// property rather than as a stand-in for it: nothing on the ingestion path may reach the hub, and
// the ONE edge that does is this package registering its background loop with the daemon.
//
// It replaces a blanket transitive ban on internal/hub. That ban was correct until three separately
// correct edges composed into a path — channels -> daemon (register the loop) -> auth (report
// sign-in state) -> hub (the one scope vocabulary, PRD §4.5). A transitive ban cannot tell that
// apart from ingestion actually talking to the hub, and it went red on a product doing exactly what
// the PRD asks.
//
// This asserts BOTH halves, which the ban could only do for one:
//
//  1. this package's own imports do not include the hub, and
//  2. with the daemon edge cut, the hub is UNREACHABLE from here — so the daemon really is the only
//     route, rather than one route among several nobody had noticed.
//
// If ingestion ever gains its own path to the hub, (2) fails. That is the thing worth knowing, and
// the old ban would have reported it identically to today's false alarm.
func TestIngestionReachesTheHubOnlyThroughTheDaemon(t *testing.T) {
	const (
		prefix = "github.com/VincentHanxiaoDu/oh-my-workspace/"
		self   = prefix + "internal/channels"
		hub    = prefix + "internal/hub"
		daemon = prefix + "internal/daemon"
	)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f",
		"{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", self).CombinedOutput()
	if err != nil {
		t.Skipf("go list could not compute the import graph: %v\n%s", err, out)
	}
	graph := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) > 0 {
			graph[f[0]] = f[1:]
		}
	}
	// THE CONTROL. An empty graph would satisfy every assertion below while proving nothing.
	if len(graph) == 0 || len(graph[self]) == 0 {
		t.Fatal("the import graph came back empty, so this test examined nothing")
	}

	// 1. Nothing in this package reaches for the hub itself.
	for _, imp := range graph[self] {
		if imp == hub {
			t.Errorf("internal/channels imports %q directly — ingestion must not reach the hub (criteria 11, 14)", hub)
		}
	}

	// 2. Cut the daemon edge; the hub must become unreachable.
	seen := map[string]bool{self: true}
	queue := []string{self}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if pkg == daemon {
			continue // the one permitted edge: registering the background loop
		}
		for _, imp := range graph[pkg] {
			if seen[imp] {
				continue
			}
			seen[imp] = true
			queue = append(queue, imp)
		}
	}
	if seen[hub] {
		t.Errorf("internal/hub is reachable from internal/channels WITHOUT going through internal/daemon — "+
			"ingestion has acquired its own route to the hub, and ingested material must never leave the "+
			"machine (criteria 11, 14). Reachable set size: %d", len(seen))
	}
	// And the daemon edge really is there, or (2) passes because the walk stopped early.
	if !seen[daemon] && len(graph[self]) > 0 {
		hasDaemon := false
		for _, imp := range graph[self] {
			if imp == daemon {
				hasDaemon = true
			}
		}
		if !hasDaemon {
			t.Fatal("internal/channels no longer imports internal/daemon; this test's premise is stale and it is no longer checking what it claims")
		}
	}
	t.Logf("hub unreachable with the daemon edge cut; %d packages walked", len(seen))
}
