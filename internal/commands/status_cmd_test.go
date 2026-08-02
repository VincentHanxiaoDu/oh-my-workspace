package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/status"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// statusSandbox is a whole machine in a temporary directory.
//
// BOTH XDG_DATA_HOME AND HOME ARE SET, and that is not belt-and-braces. Anything that falls back to
// the home directory when the XDG root is unset would otherwise reach the developer's real one, and
// this suite would rewrite their actual device pointer to a directory the framework deletes on the
// way out. It has happened on this repository.
func statusSandbox(t *testing.T) (dir, root string, getenv func(string) string) {
	t.Helper()
	dir = t.TempDir()
	root = filepath.Join(dir, "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("could not create a store to report on: %v", err)
	}
	env := map[string]string{
		store.PathEnv:   root,
		"XDG_DATA_HOME": dir,
		"HOME":          dir,
	}
	return dir, root, func(k string) string { return env[k] }
}

// CRITERIA 9, 10, 11 AND 12, DRIVEN AT THE TWO REAL SURFACES.
//
// BOTH ARE OBTAINED AND COMPARED TO EACH OTHER, subsystem by subsystem, including which are
// undetermined. Neither is checked against a string somebody wrote in this file: an isolated
// per-surface assertion passes just as happily when both surfaces are wrong in the same way, which
// is exactly how a defect got through four times on this repository and why Issue #41 exists.
func TestTheCLIScreenAndTheControlAPIAgreeSubsystemBySubsystem(t *testing.T) {
	dir, root, getenv := statusSandbox(t)
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// A machine with a bit of everything, so the comparison is over states that differ from each
	// other rather than over six identical lines.
	if err := channels.Connect(st, channels.Connection{
		ID: "work-email", Kind: channels.KindEmail, Account: "me@example.com",
		Credential: "token", CredentialExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("could not connect a channel: %v", err)
	}
	if err := channels.Connect(st, channels.Connection{
		ID: "stale-teams", Kind: channels.KindTeams, Account: "me@example.com",
		Credential: "token", CredentialExpiresAt: time.Now().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("could not connect a second channel: %v", err)
	}
	gone := filepath.Join(dir, "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Add(st, gone); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	// A CONFIGURED HUB THIS BUILD CANNOT REACH, so that at least one subsystem on this screen is
	// genuinely undetermined. Without it the criterion-11 comparison below would be vacuous: two
	// surfaces agreeing about nothing undetermined prove nothing about whether undetermined
	// survives the boundary.
	base := getenv
	getenv = func(k string) string {
		if k == "OMW_HUB" {
			return "hub.example.internal"
		}
		return base(k)
	}

	textCode, textOut, textErr := runOMW(t, getenv, "status")
	jsonCode, jsonOut, jsonErr := runOMW(t, getenv, "status", "--json")

	if strings.TrimSpace(textOut) == "" || strings.TrimSpace(jsonOut) == "" {
		t.Fatalf("one of the two surfaces printed nothing, so the comparison establishes nothing.\n"+
			"text: %q %s\njson: %q %s", textOut, textErr, jsonOut, jsonErr)
	}
	fromControl, err := status.UnmarshalControl([]byte(jsonOut))
	if err != nil {
		t.Fatalf("the control API's form could not be read back: %v\n%s", err, jsonOut)
	}
	control := fromControl.States()
	screen := status.ParseRendered(textOut)

	if len(control) == 0 || len(screen) == 0 {
		t.Fatal("one of the two surfaces reported no subsystems at all")
	}
	// CRITERION 10, BOTH DIRECTIONS: neither surface may carry a subsystem the other does not.
	if a, b := status.SortedNames(screen), status.SortedNames(fromControl.States()); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("the CLI renders %v and the control API reports %v", a, b)
	}
	for name, want := range control {
		if screen[name] != want {
			t.Errorf("subsystem %q: the control API says %q and the CLI screen says %q", name, want, screen[name])
		}
	}
	// CRITERION 11, NAMED: at least one subsystem here is undetermined, and it is undetermined on
	// BOTH surfaces rather than coerced to a negative on either.
	undetermined := 0
	for name, word := range control {
		if word == "undetermined" {
			undetermined++
			if screen[name] != "undetermined" {
				t.Errorf("subsystem %q is undetermined over the control API and %q on the screen", name, screen[name])
			}
		}
	}
	if undetermined == 0 {
		t.Fatal("no subsystem on this screen is undetermined, so the loop above compared nothing " +
			"and criterion 11 is not being driven")
	}
	// CRITERION 12: no surface gets a more optimistic view. The summary word is the same one.
	if !strings.Contains(textOut, fromControl.Summary.String()) {
		t.Errorf("the CLI's summary is not the control API's summary %q:\n%s", fromControl.Summary, textOut)
	}
	if textCode != jsonCode {
		t.Errorf("the same state exits %d through the screen and %d through the control API's form", textCode, jsonCode)
	}
}

// CRITERION 13. "The daemon is not running" is a successfully delivered answer, distinguishable BY
// THE INVOCATION'S OWN OUTCOME from the tool failing to produce an answer at all — and no daemon
// exists afterwards that did not exist before.
func TestStatusWithNoDaemonIsADeliveredAnswerAndStartsNothing(t *testing.T) {
	_, root, getenv := statusSandbox(t)

	before := daemon.Inspect(root)
	if before.Running == tri.Yes {
		t.Fatal("a daemon is already running against a store this test just created")
	}

	code, out, errOut := runOMW(t, getenv, "status")

	if code != cli.Success {
		t.Errorf("status with no daemon exited %d; delivering the answer 'the daemon is not running' "+
			"is a success, not a failure of the tool.\n%s%s", code, out, errOut)
	}
	if code == cli.ExitFailure || code == cli.ExitUsage {
		t.Error("status reported the tool's own failure for a state it answered perfectly well")
	}
	if !strings.Contains(out, "daemon: [not_working]") {
		t.Errorf("the daemon line does not say the daemon is not running:\n%s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("status printed nothing, so its exit code above is a success with no answer behind it")
	}
	// NO DAEMON EXISTS AFTERWARDS. This is the §4.2 half of the criterion.
	if after := daemon.Inspect(root); after.Running == tri.Yes {
		t.Error("a daemon is running after `omw status`; status started one")
	}
	// The distinction the criterion asks for, at the outcome: a tool that could NOT answer exits
	// differently. An unknown argument is the reachable example of that.
	badCode, _, _ := runOMW(t, getenv, "status", "--nonsense")
	if badCode == code {
		t.Errorf("an invocation that could not be answered exits %d, the same as one that was answered", badCode)
	}
}

// CRITERION 4, AT THE CLI. Status is a report: twice in a row gives the same states and changes
// nothing on the machine.
func TestStatusRunTwiceChangesNothing(t *testing.T) {
	dir, root, getenv := statusSandbox(t)

	before := statusTree(t, dir)
	_, first, _ := runOMW(t, getenv, "status", "--json")
	_, second, _ := runOMW(t, getenv, "status", "--json")
	after := statusTree(t, dir)

	a, err := status.UnmarshalControl([]byte(first))
	if err != nil {
		t.Fatalf("the first screen could not be read: %v", err)
	}
	b, err := status.UnmarshalControl([]byte(second))
	if err != nil {
		t.Fatalf("the second screen could not be read: %v", err)
	}
	for name, want := range a.States() {
		if got := b.States()[name]; got != want {
			t.Errorf("subsystem %q was %q on the first run and %q on the second", name, want, got)
		}
	}
	if before != after {
		t.Errorf("running status twice changed the machine.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if daemon.Inspect(root).Running == tri.Yes {
		t.Error("a daemon is running after two status screens")
	}
}

func statusTree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		b.WriteString(p)
		if !info.IsDir() {
			b.WriteString("\t" + info.Mode().String())
		}
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk the sandbox: %v", err)
	}
	return b.String()
}

// CRITERION 17, AGAINST A REAL DAEMON THAT REALLY DECLINED.
//
// The refusal is produced through the injected confirmation seam rather than by naming a platform
// on which owner-only permissions happen to be unconfirmable — a test that skips unless it is on
// such a platform never runs on either platform this product ships for.
func TestStatusSaysTheControlAPIDeclinedRatherThanNoDaemon(t *testing.T) {
	_, refusedRoot, refusedEnv := statusSandbox(t)
	refused, err := daemon.Start(daemon.Options{
		StorePath: refusedRoot, Interval: 5 * time.Millisecond,
		Write:            func() error { return nil },
		ConfirmOwnerOnly: func(string) (tri.Value, string) { return tri.Undetermined, "the owner of the socket could not be read" },
	})
	if err != nil {
		t.Fatalf("a daemon whose control API declines should still start: %v", err)
	}
	defer refused.Close()
	if refused.Control() != nil {
		t.Fatal("the control API opened despite an unconfirmable socket, so this test is not exercising criterion 17")
	}
	_, declinedOut, _ := runOMW(t, refusedEnv, "status")

	// The same command against a machine where NO daemon was ever started.
	_, _, neverEnv := statusSandbox(t)
	_, neverOut, _ := runOMW(t, neverEnv, "status")

	declinedLine := statusLineFor(t, declinedOut, "daemon")
	neverLine := statusLineFor(t, neverOut, "daemon")
	if declinedLine == neverLine {
		t.Fatalf("a daemon whose control API declined reads exactly like a machine where none was "+
			"started:\n%s", declinedLine)
	}
	if !strings.Contains(declinedOut, "owner-only") {
		t.Errorf("status does not say why the control API did not open:\n%s", declinedOut)
	}
	if !strings.Contains(declinedOut, "control API") {
		t.Errorf("status does not say that the control API is not open:\n%s", declinedOut)
	}
	if strings.Contains(declinedLine, "[not_working]") {
		t.Errorf("a running daemon whose control API declined is reported as not working: %s", declinedLine)
	}
}

// statusLineFor returns a subsystem's line and everything indented under it.
func statusLineFor(t *testing.T, out, name string) string {
	t.Helper()
	var b strings.Builder
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, name+": ["):
			in = true
		case in && !strings.HasPrefix(l, " "):
			in = false
		}
		if in {
			b.WriteString(l + "\n")
		}
	}
	if b.Len() == 0 {
		t.Fatalf("there is no %q line on this screen:\n%s", name, out)
	}
	return b.String()
}

// CRITERION 7 AT THE CLI, and criterion 19 with it: something undetermined moves the exit code away
// from success without costing the screen a single line.
func TestAnUndeterminedSubsystemChangesTheCodeAndNotTheScreensCompleteness(t *testing.T) {
	dir, _, base := statusSandbox(t)
	// A configured hub this build has no transport for: the one undetermined thing a test can
	// arrange without breaking the machine underneath it.
	env := map[string]string{
		store.PathEnv:   filepath.Join(dir, "store"),
		"XDG_DATA_HOME": dir,
		"HOME":          dir,
		"OMW_HUB":       "hub.example.internal",
	}
	getenv := func(k string) string { return env[k] }

	plainCode, plainOut, _ := runOMW(t, base, "status")
	code, out, errOut := runOMW(t, getenv, "status")

	if code != cli.ExitUndetermined {
		t.Errorf("a screen with an undetermined subsystem exited %d, want ExitUndetermined (%d).\n%s%s",
			code, cli.ExitUndetermined, out, errOut)
	}
	if code == cli.ExitFailure {
		t.Error("'I could not check' exited as 'the answer is no'")
	}
	if plainCode == code {
		t.Errorf("a screen with everything established and one with an undetermined subsystem both exit %d", code)
	}
	// EVERY LINE IS STILL THERE (criterion 7), and there are as many as on the untroubled screen.
	before, after := status.ParseRendered(plainOut), status.ParseRendered(out)
	if len(after) != len(before) {
		t.Errorf("the undetermined screen has %d subsystem lines and the untroubled one has %d:\n%s", len(after), len(before), out)
	}
	for _, name := range status.Required() {
		if _, ok := after[name]; !ok {
			t.Errorf("subsystem %q vanished when the hub went undetermined:\n%s", name, out)
		}
	}
	// CRITERION 8 at the surface a person actually reads.
	if strings.Contains(out, "everything is running.") {
		t.Errorf("the screen leads with everything running while a subsystem is undetermined:\n%s", out)
	}
}

// The daemon line's state must be the product's ONE liveness answer and not a second opinion
// (Issue #41). Driven against a REAL running daemon, not a stub: a stub proves the rendering, and
// only a started daemon proves the answer.
func TestTheDaemonLineIsTheOneLivenessAnswer(t *testing.T) {
	_, root, getenv := statusSandbox(t)
	d, err := daemon.Start(daemon.Options{
		StorePath: root, Interval: 5 * time.Millisecond, Write: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("the daemon did not start: %v", err)
	}
	defer d.Close()

	live, why := daemonLiveness(cli.Env{Getenv: getenv})
	if live != tri.Yes {
		t.Fatalf("the product's liveness answer for a started daemon is %v (%s), so this test is "+
			"not comparing against a running daemon", live, why)
	}
	_, out, _ := runOMW(t, getenv, "status")
	if got := status.ParseRendered(out)["daemon"]; got != "working" {
		t.Errorf("the daemon is running by the product's one answer and the status screen says %q:\n%s", got, out)
	}
}
