package commands

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// env builds a getenv from a map. Nothing here touches the real process environment: PRD §4.2's
// "no network connection without a hub configured" is a state a test must be able to CREATE, and a
// test that mutated os.Environ could not run beside another one.
func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// run drives the real command through the real registry and returns what a person would see.
func run(t *testing.T, vars map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"visibility"}, args...), &out, &errb, env(vars))
	return out.String(), errb.String(), code
}

// withStore points the command at an in-memory hub for the duration of one test.
//
// The default source reports the hub unreachable, because this build has no client-to-hub
// transport — see visibilitySource. Tests that need the hub-backed paths supply one.
func withStore(t *testing.T, s *hub.Store) {
	t.Helper()
	prev := visibilitySource
	visibilitySource = func(cli.Env) (*hub.Store, error) { return s, nil }
	t.Cleanup(func() { visibilitySource = prev })
}

// withDaemon makes the daemon probe answer yes. The probe itself is tested separately.
func withDaemon(t *testing.T) {
	t.Helper()
	prev := daemonRunning
	daemonRunning = func(cli.Env) bool { return true }
	t.Cleanup(func() { daemonRunning = prev })
}

func hubConfigured() map[string]string { return map[string]string{envHub: "hub.example.internal"} }

func storeWithNotes(t *testing.T) (*hub.Store, map[string]hub.NoteID) {
	t.Helper()
	rec := hub.NewRecord()
	rec.DefineGroup("platform", "alice", "bo")
	rec.AddPerson("carol")
	s := hub.NewStore(rec)
	ids := map[string]hub.NoteID{}
	for name, v := range map[string]hub.Visibility{
		"company": hub.CompanyWide(),
		"self":    hub.SelfOnly(),
	} {
		n, err := s.Publish(hub.Publication{Author: "alice", Title: name, Body: "b", Visibility: v})
		if err != nil {
			t.Fatalf("Publish %s: %v", name, err)
		}
		ids[name] = n.ID
	}
	people, err := hub.ParseChoice("people:carol")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.Publish(hub.Publication{Author: "alice", Title: "people", Body: "b", Visibility: people})
	if err != nil {
		t.Fatal(err)
	}
	ids["people"] = n.ID
	group, err := hub.ParseChoice("group:platform")
	if err != nil {
		t.Fatal(err)
	}
	n, err = s.Publish(hub.Publication{Author: "carol", Title: "group", Body: "b", Visibility: group})
	if err != nil {
		t.Fatal(err)
	}
	ids["group"] = n.ID
	return s, ids
}

// ============================================================================================
// CRITERIA 7, 8 AND 9 — THE POINT OF THE ISSUE, GREPPED OFF THE REAL COMMAND'S REAL OUTPUT.
// ============================================================================================

// Every CLI surface that offers a visibility choice carries the §2.4 statement, and no surface uses
// wording implying privacy from the hub operator without it.
//
// This drives the actual command through the actual registry and greps what came out — not a table
// of strings the test and the code both read from, which would agree with itself forever.
func TestCLISurfacesStateWhatRestrictionIs(t *testing.T) {
	s, ids := storeWithNotes(t)
	withStore(t, s)
	withDaemon(t)

	cases := []struct {
		name         string
		args         []string
		vars         map[string]string
		offersChoice bool
	}{
		{"visibility (usage)", nil, nil, false},
		{"visibility choices", []string{"choices"}, nil, true},
		{"visibility plan company", []string{"plan", "company"}, nil, true},
		{"visibility plan self", []string{"plan", "self"}, nil, true},
		{"visibility plan group:platform", []string{"plan", "group:platform"}, nil, true},
		{"visibility plan people:carol", []string{"plan", "people:carol"}, nil, true},
		{"visibility plan (no argument)", []string{"plan"}, nil, true},
		{"visibility plan (nonsense)", []string{"plan", "everyone-ish"}, nil, true},
		{"visibility schema", []string{"schema"}, nil, true},
		{"visibility scopes", []string{"scopes"}, nil, false},
		{"visibility show self-only", []string{"show", string(ids["self"])}, hubConfigured(), false},
		{"visibility show group", []string{"show", string(ids["group"])}, hubConfigured(), false},
		{"visibility show named people", []string{"show", string(ids["people"])}, hubConfigured(), false},
		{"visibility show company-wide", []string{"show", string(ids["company"])}, hubConfigured(), false},
		{"visibility show, no hub", []string{"show", "note-1"}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, _ := run(t, c.vars, c.args...)
			// The person sees both streams. §2.4 must hold over the whole view, and a refusal
			// printed on stderr is as much a point of choice as a success on stdout.
			view := stdout + stderr
			if strings.TrimSpace(view) == "" {
				t.Fatalf("%s printed nothing at all — silence is not an answer", c.name)
			}
			if err := hub.CheckSurface(c.name, view, c.offersChoice); err != nil {
				t.Errorf("%v\n--- what the person sees ---\n%s", err, view)
			}
		})
	}
}

// CRITERION 8 SAID BLUNTLY: the four words the Issue names, grepped for by this test itself rather
// than delegated to hub.CheckSurface — so that a mistake in CheckSurface cannot make this pass.
func TestCLINeverLabelsANarrowedNotePrivateWithoutTheStatement(t *testing.T) {
	s, ids := storeWithNotes(t)
	withStore(t, s)
	withDaemon(t)

	forbidden := []string{"private", "encrypted", "secret", "only you can see this"}
	invocations := [][]string{
		{"choices"}, {"plan", "self"}, {"plan", "group:platform"}, {"plan", "people:carol"},
		{"plan", "company"}, {"schema"}, {"scopes"},
		{"show", string(ids["self"])}, {"show", string(ids["group"])}, {"show", string(ids["people"])},
	}
	for _, args := range invocations {
		name := "omw visibility " + strings.Join(args, " ")
		vars := hubConfigured()
		stdout, stderr, _ := run(t, vars, args...)
		view := stdout + stderr
		lower := strings.ToLower(view)
		for _, w := range forbidden {
			if strings.Contains(lower, w) && !strings.Contains(view, hub.RestrictionStatement) {
				t.Errorf("%s uses %q without the §2.4 statement in the same view:\n%s", name, w, view)
			}
		}
	}
}

// CRITERION 9: the statement is attached to the choice, not to a first run. The hundredth
// invocation says it too, and nothing in the command carries state that could stop it.
func TestTheStatementIsNotAOneTimeOnboardingStep(t *testing.T) {
	for i := 1; i <= 100; i++ {
		stdout, _, code := run(t, nil, "plan", "self")
		if code != cli.Success {
			t.Fatalf("invocation %d exited %d", i, code)
		}
		if !strings.Contains(stdout, hub.RestrictionStatement) {
			t.Fatalf("invocation %d dropped the §2.4 statement", i)
		}
	}
}

// ============================================================================================
// The four states, distinguishably — criterion 5, through the CLI.
// ============================================================================================

// PAIRWISE, not against literals. The four states plus the undetermined rendering must produce five
// different views of "what is this note's visibility".
func TestCLIRendersTheFourStatesAndUndeterminedPairwiseDistinct(t *testing.T) {
	s, ids := storeWithNotes(t)
	withDaemon(t)

	views := map[string]string{}
	withStore(t, s)
	for _, name := range []string{"company", "people", "group", "self"} {
		stdout, _, code := run(t, hubConfigured(), "show", string(ids[name]))
		if code != cli.Success {
			t.Fatalf("show %s exited %d", name, code)
		}
		views[name] = stdout
	}
	// The undetermined view, produced by the same command on an unreachable hub.
	prev := visibilitySource
	visibilitySource = func(cli.Env) (*hub.Store, error) { return nil, hub.ErrHubUnreachable }
	stdout, _, code := run(t, hubConfigured(), "show", "note-1")
	visibilitySource = prev
	if code != cli.ExitUndetermined {
		t.Fatalf("an unreachable hub exited %d, want %d", code, cli.ExitUndetermined)
	}
	views["undetermined"] = stdout

	names := make([]string, 0, len(views))
	for n := range views {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if views[names[i]] == views[names[j]] {
				t.Errorf("%s and %s produce the same output:\n%s", names[i], names[j], views[names[i]])
			}
		}
	}
}

// CRITERION 1 through the CLI: a note published with no choice reads back as company, not as an
// empty value the reader has to interpret.
func TestCLIReadsTheDefaultBackAsCompany(t *testing.T) {
	rec := hub.NewRecord()
	s := hub.NewStore(rec)
	n, err := s.Publish(hub.Publication{Author: "alice", Title: "t", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	withStore(t, s)
	withDaemon(t)

	stdout, _, code := run(t, hubConfigured(), "show", string(n.ID))
	if code != cli.Success {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "visibility: company\n") {
		t.Errorf("output does not read the default back as %q:\n%s", "company", stdout)
	}
	for _, bad := range []string{"visibility: \n", "visibility: unset", "visibility: <nil>", "visibility: none"} {
		if strings.Contains(stdout, bad) {
			t.Errorf("output contains %q — criterion 1 forbids an empty value, 'unset' or a null", bad)
		}
	}
}

// ============================================================================================
// Undetermined, refused, and missing — three answers, and the exit codes prove it.
// ============================================================================================

// THE PROJECT'S FIRST RULE, AT THE PROCESS BOUNDARY: `could not determine` and `determined to be
// nothing` never share an exit code.
func TestExitCodesKeepUndeterminedApartFromEverythingElse(t *testing.T) {
	s, ids := storeWithNotes(t)
	withDaemon(t)

	t.Run("a real answer succeeds", func(t *testing.T) {
		withStore(t, s)
		_, _, code := run(t, hubConfigured(), "show", string(ids["self"]))
		if code != cli.Success {
			t.Errorf("exit %d, want %d", code, cli.Success)
		}
	})
	t.Run("an unreachable hub is undetermined, not a failure and not a success", func(t *testing.T) {
		prev := visibilitySource
		visibilitySource = func(cli.Env) (*hub.Store, error) { return nil, hub.ErrHubUnreachable }
		defer func() { visibilitySource = prev }()

		stdout, stderr, code := run(t, hubConfigured(), "show", "note-1")
		if code != cli.ExitUndetermined {
			t.Errorf("exit %d, want %d (cli.ExitUndetermined)", code, cli.ExitUndetermined)
		}
		if code == cli.Success || code == cli.ExitFailure {
			t.Error("undetermined shares an exit code with a determined answer")
		}
		if !strings.Contains(stdout, hub.UndeterminedDescription) {
			t.Errorf("stdout does not render the undetermined description:\n%s", stdout)
		}
		// CRITERION 17: never displays as company-wide, never as self-only.
		if strings.Contains(stdout, hub.CompanyWide().Describe()) {
			t.Error("an undetermined visibility displayed as company-wide")
		}
		if strings.Contains(stdout, hub.SelfOnly().Describe()) {
			t.Error("an undetermined visibility displayed as self-only")
		}
		if !strings.Contains(stderr, hub.ErrHubUnreachable.Code) {
			t.Errorf("stderr does not carry a machine-readable code:\n%s", stderr)
		}
	})
	t.Run("no such note is a failure with its own code", func(t *testing.T) {
		withStore(t, s)
		_, stderr, code := run(t, hubConfigured(), "show", "note-nope")
		if code != cli.ExitFailure {
			t.Errorf("exit %d, want %d", code, cli.ExitFailure)
		}
		if !strings.Contains(stderr, hub.ErrNoSuchNote.Code) {
			t.Errorf("stderr lacks the %q code:\n%s", hub.ErrNoSuchNote.Code, stderr)
		}
	})
}

// CRITERION 12 at the CLI: refused and no-such-note are distinguishable without parsing prose.
func TestCLIDistinguishesRefusedFromNoSuchNote(t *testing.T) {
	s, ids := storeWithNotes(t)
	withStore(t, s)
	withDaemon(t)

	refusedOut, _, refusedCode := run(t, hubConfigured(), "show", string(ids["self"]), "--as", "bo")
	missingOut, missingErr, missingCode := run(t, hubConfigured(), "show", "note-nope", "--as", "bo")

	if !strings.Contains(refusedOut, "bo can read it: no") {
		t.Errorf("a refused reader is not reported as a determined no:\n%s", refusedOut)
	}
	if !strings.Contains(missingErr, hub.ErrNoSuchNote.Code) {
		t.Errorf("a missing note does not carry the %q code:\n%s", hub.ErrNoSuchNote.Code, missingErr)
	}
	if refusedOut == missingOut && refusedCode == missingCode {
		t.Error("refused and no-such-note are indistinguishable at the CLI")
	}
}

// ============================================================================================
// Nothing implicit, and the local half stands alone — criteria 18, 19, 20, 21.
// ============================================================================================

// CRITERION 21. With no hub configured, the part that genuinely needs the hub says precisely what is
// missing. It does not report an empty audience and it does not report success.
func TestNoHubConfiguredSaysExactlyWhatIsMissing(t *testing.T) {
	withDaemon(t)
	stdout, stderr, code := run(t, nil, "show", "note-1")
	if code == cli.Success {
		t.Error("reading a published note's visibility with no hub reported success")
	}
	if !strings.Contains(stderr, "no hub configured") {
		t.Errorf("stderr does not say 'no hub configured':\n%s", stderr)
	}
	if !strings.Contains(stderr, hub.ErrNoHubConfigured.Code) {
		t.Errorf("stderr carries no machine-readable code:\n%s", stderr)
	}
	view := stdout + stderr
	for _, bad := range []string{"visible to nobody", "no audience", "0 people", "empty audience"} {
		if strings.Contains(strings.ToLower(view), bad) {
			t.Errorf("with no hub the command reported %q — criterion 21 forbids reporting an empty audience", bad)
		}
	}
}

// CRITERION 20 / PRD §4.4: choosing a draft's intended visibility works fully with no hub, and the
// choice comes back as the person expressed it.
func TestPlanWorksFullyWithNoHubConfigured(t *testing.T) {
	cases := map[string]string{
		"company":          "visibility: company",
		"self":             "visibility: self",
		"people:carol,dan": "visibility: people",
		"group:platform":   "visibility: group",
	}
	for choice, want := range cases {
		stdout, stderr, code := run(t, nil, "plan", choice)
		if code != cli.Success {
			t.Errorf("plan %q exited %d: %s", choice, code, stderr)
			continue
		}
		if !strings.Contains(stdout, want) {
			t.Errorf("plan %q did not retain the choice (%q):\n%s", choice, want, stdout)
		}
		if !strings.Contains(stdout, hub.RestrictionStatement) {
			t.Errorf("plan %q omitted the §2.4 statement", choice)
		}
	}
	// A group name needs the hub to RESOLVE, and criterion 21 wants that said precisely rather than
	// half-worked. The choice is still retained.
	stdout, _, _ := run(t, nil, "plan", "group:platform")
	if !strings.Contains(stdout, hub.ErrNoHubConfigured.Code) {
		t.Errorf("planning a group narrowing with no hub does not say what is missing:\n%s", stdout)
	}
	if !strings.Contains(stdout, "platform") {
		t.Errorf("the group name was not retained on the draft:\n%s", stdout)
	}
}

// CRITERION 18. No command starts the daemon; a command that needs it says so.
func TestVisibilitySaysTheDaemonIsNotRunningRatherThanStartingIt(t *testing.T) {
	// The probe is the real one here — that is the point. With no socket named in the environment
	// there is no daemon to find.
	_, stderr, code := run(t, hubConfigured(), "show", "note-1")
	if code == cli.Success {
		t.Error("the command succeeded with no daemon running")
	}
	if !strings.Contains(stderr, "daemon is not running") {
		t.Errorf("stderr does not say the daemon is not running:\n%s", stderr)
	}
	if !strings.Contains(stderr, hub.ErrDaemonNotRunning.Code) {
		t.Errorf("stderr carries no machine-readable code:\n%s", stderr)
	}
}

// The daemon probe PROBES rather than assuming: it answers from the socket it is told about.
func TestDaemonProbeReadsTheSocketItIsGiven(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "omw.sock")

	e := cli.Env{Getenv: env(map[string]string{envSocket: sock})}
	if daemonRunning(e) {
		t.Error("the probe reports a daemon for a socket that does not exist")
	}
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !daemonRunning(e) {
		t.Error("the probe does not see a socket that exists")
	}
	if daemonRunning(cli.Env{Getenv: env(nil)}) {
		t.Error("the probe reports a daemon when the environment names no socket")
	}
}

// CRITERION 19: with no hub configured, no visibility surface opens a network connection.
//
// PROVED STRUCTURALLY, BY WHAT THE CODE CAN REACH, rather than by watching a socket — a test that
// observed zero connections during one run would also pass on a build that dials only sometimes.
//
// WHY "net" IS NOT SIMPLY BANNED, AND WHY THIS IS STRONGER THAN THE BAN IT REPLACES.
// The original form of this test banned the `net` package outright, as a proxy for "cannot open a
// network connection". That proxy was valid only while nothing in the product spoke to anything.
// PRD §4.6 REQUIRES a control API that is local and demonstrably so, and on Unix a local IPC socket
// is a `net.Listen("unix", ...)` — the same package. So the ban conflated "reaches the net package"
// with "can reach the network", and would have forced the control API to be implemented worse to
// keep a test green.
//
// The replacement is not a relaxation. Banning the package says only "cannot reach it". This asserts
// something the ban never did: that EVERY listen and dial in the product names the "unix" network.
// A TCP dial would now fail this test at the call site, which the package ban could only have caught
// by forbidding the local socket too.
func TestVisibilitySurfacesCannotOpenANetworkConnection(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH, so the import graph cannot be computed here: %v", err)
	}
	out, err := exec.Command(goBin, "list", "-deps",
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub",
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/commands",
		"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli",
	).CombinedOutput()
	if err != nil {
		t.Skipf("go list could not compute the import graph here: %v\n%s", err, out)
	}
	// These have no local-IPC use whatever. Reaching any of them is reaching outward.
	banned := map[string]bool{
		"net/http": true, "net/url": true, "crypto/tls": true, "net/rpc": true,
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		if banned[pkg] {
			t.Errorf("the visibility surfaces can reach %q — with no hub configured nothing may reach out (PRD §4.2, criterion 19)", pkg)
		}
	}
}

// CRITERION 19, the half the import graph cannot answer: every listen and dial in this product
// names the "unix" network, so the only socket anything opens is a local one (PRD §4.2, §4.6).
//
// A CONTROL IS ASSERTED FIRST. If the scan finds no call sites at all it proves nothing, and would
// go on proving nothing after somebody renamed the file it reads.
func TestEveryListenAndDialIsAUnixSocket(t *testing.T) {
	root := repoRoot(t)
	callSite := regexp.MustCompile(`net\.(Listen|Dial|DialTimeout)\s*\(\s*("[^"]*")?`)
	found := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, m := range callSite.FindAllStringSubmatch(string(b), -1) {
			found++
			if m[2] != `"unix"` {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s: net.%s opens %s, not \"unix\" — with no hub configured nothing may reach out (PRD §4.2), and the control API must be local (§4.6)", rel, m[1], m[2])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	// THE CONTROL. Zero call sites means the regex stopped matching, not that the product stopped
	// dialling — and those two look identical in a green run.
	if found == 0 {
		t.Fatal("found no net.Listen/net.Dial call sites at all; the scan is not looking at anything, so its pass proves nothing")
	}
	t.Logf("checked %d listen/dial call sites, all \"unix\"", found)
}

// ============================================================================================
// One vocabulary — criterion 13.
// ============================================================================================

// The CLI does not keep its own scope list; it prints the hub's. This asserts the SET, so a CLI that
// grew a name of its own, or dropped one, fails.
func TestScopesCommandPrintsExactlyTheHubVocabulary(t *testing.T) {
	stdout, _, code := run(t, nil, "scopes")
	if code != cli.Success {
		t.Fatalf("exit %d", code)
	}
	printed := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, " ") {
			continue // the trailing prose, not a scope name
		}
		printed[line] = true
	}
	want := map[string]bool{}
	for _, s := range hub.Vocabulary() {
		want[string(s)] = true
	}
	for name := range want {
		if !printed[name] {
			t.Errorf("the CLI does not print scope %q — the same name must mean the same thing on all three surfaces", name)
		}
	}
	for name := range printed {
		if !want[name] {
			t.Errorf("the CLI prints scope %q, which is not in the hub's vocabulary", name)
		}
	}
}

// The agent API schema the CLI serves is the hub's, byte for byte. A second copy is how the two
// points of choice drift apart.
func TestSchemaCommandServesTheHubsSchema(t *testing.T) {
	want, err := hub.AgentAPISchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	stdout, _, code := run(t, nil, "schema")
	if code != cli.Success {
		t.Fatalf("exit %d", code)
	}
	if stdout != want {
		t.Error("the CLI serves a different schema from the hub's")
	}
	if !strings.Contains(stdout, hub.RestrictionStatement) {
		t.Error("the served schema omits the §2.4 statement")
	}
}

// An unknown subcommand is named, not answered with a usage dump, and exits ExitUsage.
func TestUnknownSubcommandIsNamed(t *testing.T) {
	_, stderr, code := run(t, nil, "visble")
	if code != cli.ExitUsage {
		t.Errorf("exit %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(stderr, "visble") {
		t.Errorf("the typo is not echoed back:\n%s", stderr)
	}
}
