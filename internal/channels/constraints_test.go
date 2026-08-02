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
	banned := map[string]string{
		"net/http":   "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"net/url":    "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"net/rpc":    "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"crypto/tls": "with no hub configured and no channel connected, nothing may open a connection (criterion 11)",
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub": "a connected channel never reaches the hub as part of ingesting (criterion 11), and ingested material never leaves the machine (criterion 14)",
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
