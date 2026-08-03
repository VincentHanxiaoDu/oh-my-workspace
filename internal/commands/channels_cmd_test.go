package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// runChannelsCmd drives `omw channels ...` the way a person does, through the real registry.
func runChannelsCmd(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"channels"}, args...), &out, &errb, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

// chanEnv is a sandboxed environment with a store created in it and NO HUB CONFIGURED, which is
// criterion 12's premise and the default state of every test in this file.
func chanEnv(t *testing.T) (map[string]string, string, *store.Store) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating a store to test against: %v", err)
	}
	// HOME is redirected as well as OMW_STORE: the device's store pointer lives under HOME and no
	// test may touch the pointer belonging to the machine it runs on.
	return map[string]string{store.PathEnv: root, "HOME": t.TempDir(), "OMW_HUB": ""}, root, s
}

// credFile writes a sign-in artifact of the shape a person hands over.
func credFile(t *testing.T, token string, expires time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "credential.json")
	body := `{"token":"` + token + `","expires_at":"` + expires.UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func connectBoth(t *testing.T, env map[string]string) {
	t.Helper()
	good := time.Now().Add(24 * time.Hour)
	for _, kind := range []string{"email", "teams"} {
		code, _, stderr := runChannelsCmd(t, env, "connect", kind,
			"--account", "ana@example.com", "--credential-file", credFile(t, "tok-"+kind, good))
		if code != cli.Success {
			t.Fatalf("connecting %s exited %d: %s", kind, code, stderr)
		}
	}
}

// chanBlock returns the listing block for one channel, so that comparisons are between renderings
// of the same field rather than against literals.
func chanBlock(stdout, id string) string {
	for _, part := range strings.Split(stdout, "\n\n") {
		if strings.HasPrefix(strings.TrimSpace(part), id+"\n") {
			return part
		}
	}
	return ""
}

// =================================================================================================
// CRITERION 1 — both built in, connectable through the CLI, and distinguishable in the listing.
// =================================================================================================

func TestConnectingTeamsAndEmailNeedsNothingInstalledAndTheListingTellsThemApart(t *testing.T) {
	env, _, _ := chanEnv(t)
	connectBoth(t, env)

	code, stdout, stderr := runChannelsCmd(t, env, "list")
	if code != cli.Success {
		t.Fatalf("list exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	email := chanBlock(stdout, "email-ana-example-com")
	teams := chanBlock(stdout, "teams-ana-example-com")
	if email == "" || teams == "" {
		t.Fatalf("both connected channels are not both listed:\n%s", stdout)
	}
	// PAIRWISE. The two kind lines must differ from each other; asserting each against a literal
	// passes just as happily after both have been edited into the same wording.
	ek, tk := lineWith(email, "kind:"), lineWith(teams, "kind:")
	if ek == "" || tk == "" {
		t.Fatalf("a listed channel does not say what kind it is:\n%s", stdout)
	}
	if ek == tk {
		t.Errorf("the email channel and the Teams channel both render their kind as %q", strings.TrimSpace(ek))
	}
}

// =================================================================================================
// CRITERION 2 — an empty channel set, an unreadable one, and a failure to list are three outcomes.
// =================================================================================================

func TestAnEmptyChannelSetIsStatedAndIsNotBlankNorAFailure(t *testing.T) {
	env, _, _ := chanEnv(t)
	emptyCode, emptyOut, _ := runChannelsCmd(t, env, "list")
	if emptyCode != cli.Success {
		t.Fatalf("listing an empty channel set exited %d; an empty set is an ANSWER", emptyCode)
	}
	if strings.TrimSpace(emptyOut) == "" {
		t.Fatal("an empty channel set printed as blank output; §4.3 says none of the answers is silence")
	}
	if !strings.Contains(emptyOut, "no channels are connected") {
		t.Errorf("the empty set is not stated as one:\n%s", emptyOut)
	}

	// UNREADABLE. A damaged channel record is not an absent one.
	env2, _, s2 := chanEnv(t)
	if err := s2.Put(store.Record{Kind: channels.RecordKind, ID: "email-a", Data: []byte("not json at all")}); err != nil {
		t.Fatal(err)
	}
	badCode, badOut, badErr := runChannelsCmd(t, env2, "list")
	if badCode == emptyCode {
		t.Errorf("a channel list that could not be read exits %d, the same as an empty one; "+
			"`could not determine` and `determined to be nothing` must never share an exit code", badCode)
	}
	if badCode != cli.ExitUndetermined {
		t.Errorf("an unreadable channel list exits %d; want %d", badCode, cli.ExitUndetermined)
	}
	if strings.Contains(badOut, "no channels are connected") {
		t.Errorf("an unreadable channel list was printed as an empty one:\n%s", badOut)
	}
	if strings.TrimSpace(badErr) == "" {
		t.Error("an unreadable channel list said nothing on stderr")
	}

	// A FAILURE TO LIST AT ALL — there is no store. Its own code, its own sentence.
	failCode, _, failErr := runChannelsCmd(t, map[string]string{store.PathEnv: filepath.Join(t.TempDir(), "nope"), "HOME": t.TempDir()}, "list")
	if failCode == emptyCode || failCode == badCode {
		t.Errorf("failing to list exits %d, which is also what %d or %d is", failCode, emptyCode, badCode)
	}
	if strings.TrimSpace(failErr) == "" {
		t.Error("failing to list said nothing on stderr")
	}
}

// =================================================================================================
// CRITERIA 5, 6 AND 7 — the daemon is stopped: nothing ingests, the facts are said not to be
// current, and nothing here starts anything.
// =================================================================================================

func TestWithTheDaemonStoppedEveryChannelCommandSaysItsFactsAreNotCurrent(t *testing.T) {
	env, root, s := chanEnv(t)
	connectBoth(t, env)

	// A channel that HAS ingested, so there is a real timestamp available to be misrepresented.
	c, err := channels.Get(s, "email-ana-example-com")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c.Last.State, c.Last.At, c.Last.Outcome = tri.Yes, when, channels.OutcomeReached
	if err := channels.Save(s, c); err != nil {
		t.Fatal(err)
	}
	if daemon.Inspect(root).Running == tri.Yes {
		t.Fatal("a daemon is running against this brand-new store; the test cannot be staged")
	}

	for _, args := range [][]string{{"list"}, {"status", "email-ana-example-com"}} {
		code, stdout, stderr := runChannelsCmd(t, env, args...)
		if code != cli.Success {
			t.Fatalf("%v exited %d: %s", args, code, stderr)
		}
		if !strings.Contains(stdout, "NOT RUNNING") {
			t.Errorf("%v does not say ingestion is not running:\n%s", args, stdout)
		}
		line := lineWith(stdout, "last successful ingestion:")
		if line == "" {
			t.Fatalf("%v does not report a last successful ingestion at all:\n%s", args, stdout)
		}
		// THE TIMESTAMP IS THERE — the fact is not hidden — AND IT IS MARKED. Criterion 6 forbids
		// presenting it as if it were current, not reporting it.
		if !strings.Contains(line, when.Format(time.RFC3339)) {
			t.Errorf("%v does not report the last-ingestion time at all: %q", args, line)
		}
		if !strings.Contains(line, "NOT CURRENT") {
			t.Errorf("%v presents a stale last-ingestion time as if it were current: %q", args, line)
		}
	}
}

// CRITERION 7 — no channel command starts the daemon, confirmed by checking immediately after.
func TestNoChannelCommandStartsTheDaemon(t *testing.T) {
	env, root, _ := chanEnv(t)
	connectBoth(t, env)
	for _, args := range [][]string{
		{"list"}, {"status", "email-ana-example-com"}, {"status", "nothing-connected-here"},
		{"disconnect", "teams-ana-example-com"}, {"help"},
	} {
		runChannelsCmd(t, env, args...)
		if got := daemon.Inspect(root).Running; got == tri.Yes {
			t.Fatalf("after 'omw channels %s' a daemon is running; nothing starts the daemon on a "+
				"person's behalf (PRD §4.2, criterion 7)", strings.Join(args, " "))
		}
	}
}

// CRITERION 5, THROUGH THE CLI — a channel command with the daemon stopped ingests nothing.
//
// This is the mutation the Issue names: a `list` that "helpfully" refreshes first.
//
// WHY THIS COMPARES THE WHOLE STORE AND NOT THE TICKET COUNT. The first version of this test
// counted tickets, and the mutation — inserting a real `channels.Ingest` call into `channels list`
// — LEFT IT GREEN. This build's built-in adapters have no transport, so an ingestion run from the
// wrong place still produces zero tickets, and a count cannot tell "it did not ingest" from "it
// ingested and found nothing". Every byte of the store is compared instead: an ingestion pass
// records what it attempted against every channel, so the mutation is caught by the write it makes
// whether or not it produced a ticket. Found by driving the mutation and watching the count-based
// version pass.
func TestChannelCommandsWithTheDaemonStoppedDoNotIngest(t *testing.T) {
	env, root, s := chanEnv(t)
	connectBoth(t, env)
	beforeTickets := len(mustList(t, s))
	before := treeSnapshot(t, root)
	for _, args := range [][]string{{"list"}, {"status", "email-ana-example-com"}, {"status", "nope"}} {
		runChannelsCmd(t, env, args...)
	}
	if after := len(mustList(t, s)); after != beforeTickets {
		t.Errorf("running channel commands with the daemon stopped changed the inbox from %d to %d "+
			"tickets; ingestion is a property of the daemon running (criterion 5)", beforeTickets, after)
	}
	after := treeSnapshot(t, root)
	for name, was := range before {
		if now, ok := after[name]; !ok || now != was {
			t.Errorf("a read-only channel command rewrote %s with the daemon stopped. Ingestion "+
				"happens because the daemon is running, not because somebody typed something "+
				"(criteria 4 and 5).", name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("a read-only channel command created %s with the daemon stopped (criterion 5)", name)
		}
	}
}

func mustList(t *testing.T, s *store.Store) []inbox.Ticket {
	t.Helper()
	ts, err := inbox.List(s)
	if err != nil {
		t.Fatalf("listing the inbox: %v", err)
	}
	return ts
}

// =================================================================================================
// CRITERION 12 — the local half stands alone.
// =================================================================================================

func TestConnectingAndListingWorkFullyWithNoHubConfigured(t *testing.T) {
	env, _, s := chanEnv(t)
	if env["OMW_HUB"] != "" {
		t.Fatal("this test's premise is no hub configured")
	}
	connectBoth(t, env)
	conns, err := channels.List(s)
	if err != nil || len(conns) != 2 {
		t.Fatalf("with no hub configured, connecting did not fully work: %d channels, err %v", len(conns), err)
	}
	code, stdout, stderr := runChannelsCmd(t, env, "list")
	if code != cli.Success {
		t.Fatalf("with no hub configured, listing exited %d: %s", code, stderr)
	}
	for _, hubWord := range []string{"hub", "OMW_HUB"} {
		if strings.Contains(strings.ToLower(stdout), strings.ToLower(hubWord)) {
			t.Errorf("the channel listing mentions %q; nothing in this capability wants a hub "+
				"(criterion 12):\n%s", hubWord, stdout)
		}
	}
}

// =================================================================================================
// CRITERION 13 — an explicit act, including the sign-in. Three states, three sentences.
// =================================================================================================

func TestConnectingWithoutSigningInIsRefusedAndConnectsNothing(t *testing.T) {
	env, _, s := chanEnv(t)
	code, stdout, stderr := runChannelsCmd(t, env, "connect", "email", "--account", "ana@example.com")
	if code == cli.Success {
		t.Fatalf("connecting with no credential succeeded:\n%s", stdout)
	}
	if !strings.Contains(stderr, "on your behalf") {
		t.Errorf("the refusal does not say that no credential is obtained for you:\n%s", stderr)
	}
	conns, err := channels.List(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 0 {
		t.Errorf("a refused connect left %d channel(s) connected", len(conns))
	}
}

func TestTheThreeConnectionStatesAreThreeDifferentSentences(t *testing.T) {
	env, _, s := chanEnv(t)
	connectBoth(t, env)
	// One healthy, one whose credential has expired, one that was never connected.
	c, err := channels.Get(s, "teams-ana-example-com")
	if err != nil {
		t.Fatal(err)
	}
	c.CredentialExpiresAt = time.Now().Add(-time.Hour)
	if err := channels.Save(s, c); err != nil {
		t.Fatal(err)
	}

	sentences := map[string]string{}
	for name, id := range map[string]string{
		"connected and healthy": "email-ana-example-com",
		"credential expired":    "teams-ana-example-com",
		"disconnected":          "never-connected-at-all",
	} {
		_, stdout, stderr := runChannelsCmd(t, env, "status", id)
		line := lineWith(stdout, "connection:")
		if line == "" {
			t.Fatalf("%s: status printed no connection line\nstdout: %s\nstderr: %s", name, stdout, stderr)
		}
		sentences[name] = strings.TrimSpace(line)
	}
	for a, sa := range sentences {
		for b, sb := range sentences {
			if a < b && sa == sb {
				t.Errorf("%q and %q both render as %q — criterion 13 requires an expired credential "+
					"be distinct from disconnected AND from connected-and-healthy", a, b, sa)
			}
		}
	}
}

// =================================================================================================
// CRITERION 15 — failure is distinguishable from success by exit code alone.
// =================================================================================================

func TestFailureAndSuccessNeverShareAnExitCode(t *testing.T) {
	env, _, _ := chanEnv(t)
	connectBoth(t, env)

	successes := [][]string{{"list"}, {"status", "email-ana-example-com"}, {"help"}}
	failures := [][]string{
		{"connect", "email", "--account", "a@example.com"},          // no credential
		{"connect", "carrier-pigeon", "--account", "a@example.com"}, // no such kind
		{"disconnect", "never-connected-at-all"},                    // no such channel
		{"connect", "email", "--account", "a@example.com", "--credential-file", "/nope/nope"},
	}
	for _, args := range successes {
		code, _, stderr := runChannelsCmd(t, env, args...)
		if code != cli.Success {
			t.Errorf("'omw channels %s' exited %d; it succeeded: %s", strings.Join(args, " "), code, stderr)
		}
	}
	for _, args := range failures {
		code, stdout, stderr := runChannelsCmd(t, env, args...)
		if code == cli.Success {
			t.Errorf("'omw channels %s' failed and exited 0:\n%s", strings.Join(args, " "), stdout)
		}
		// THE DETAIL IS ON THE ERROR STREAM, which is the other half of criterion 15.
		if strings.TrimSpace(stderr) == "" {
			t.Errorf("'omw channels %s' failed with nothing on stderr", strings.Join(args, " "))
		}
	}
}

// =================================================================================================
// THROUGH THE REAL BINARY — criteria 1, 4, 7, 10 and 11 as a person meets them.
// =================================================================================================

// This is the honest end-to-end: the real `omw`, the real daemon, a real store, and this build's
// REAL built-in adapters — which have no transport and say so. It therefore drives criterion 10's
// unreachable rendering against the product rather than against a fake, and confirms that an
// unreachable channel produces no tickets and does not read as "nothing arrived".
func TestTheRealBinaryConnectsIngestsAndReportsAnUnreachableChannelHonestly(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	build := exec.Command(goTool, "build", "-o", bin, "./cmd/omw")
	build.Dir = repoRoot(t)
	if out, berr := build.CombinedOutput(); berr != nil {
		t.Fatalf("building omw: %v\n%s", berr, out)
	}

	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()
	run := func(args ...string) (int, string, *os.ProcessState) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// THE DEVICE POINTER MUST BE SANDBOXED TOO, NOT JUST THE STORE. `store.productDir` resolves
		// the per-user pointer from XDG_DATA_HOME, else HOME; a spawn inheriting the developer's
		// environment repoints their real store at a t.TempDir() that is then deleted. BOTH
		// variables, because setting one leaves the other live on the platform that uses it.
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=", "XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out), cmd.ProcessState
	}

	if code, out, _ := run("store", "create"); code != 0 {
		t.Fatalf("store create exited %d:\n%s", code, out)
	}

	cred := filepath.Join(sandbox, "cred.json")
	if werr := os.WriteFile(cred, []byte(`{"token":"t","expires_at":"2099-01-01T00:00:00Z"}`), 0o600); werr != nil {
		t.Fatal(werr)
	}
	for _, kind := range []string{"email", "teams"} {
		code, out, state := run("channels", "connect", kind, "--account", "ana@example.com", "--credential-file", cred)
		if code != 0 {
			t.Fatalf("connect %s exited %d:\n%s", kind, code, out)
		}
		// CRITERION 7, THROUGH THE BINARY. Connecting starts nothing: if it had, the child would
		// still be in its process group.
		assertNothingLeftRunning(t, state)
	}

	// CRITERION 11 AS FAR AS THIS CAN BE OBSERVED: nothing was written outside the store, and no
	// socket appeared in it as a result of connecting.
	filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr == nil && d.Type()&os.ModeSocket != 0 {
			t.Errorf("connecting a channel left a socket at %s", p)
		}
		return nil
	})

	if code, out, _ := run("daemon", "start"); code != 0 {
		t.Fatalf("daemon start exited %d:\n%s", code, out)
	}
	t.Cleanup(func() { run("daemon", "stop") })

	// Give the daemon's ingestion a couple of passes.
	deadline := time.Now().Add(10 * time.Second)
	var listOut string
	for time.Now().Before(deadline) {
		_, listOut, _ = run("channels", "list")
		if strings.Contains(listOut, "COULD NOT BE REACHED") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !strings.Contains(listOut, "COULD NOT BE REACHED") {
		t.Fatalf("this build has no Teams or mail transport, so both channels must report as "+
			"unreachable rather than as empty. The listing says:\n%s", listOut)
	}
	if strings.Contains(listOut, "reached; it saw") {
		t.Errorf("a channel with no transport reported as reached:\n%s", listOut)
	}
	// AND ZERO TICKETS. An unreachable channel produces none — and must not read as "nothing
	// arrived", which is what the sentence above is for.
	if code, out, _ := run("inbox", "list"); code == 0 && strings.Contains(out, "ingested-") {
		t.Errorf("an unreachable channel produced tickets:\n%s", out)
	}
}

func assertNothingLeftRunning(t *testing.T, state *os.ProcessState) {
	t.Helper()
	pgrep, err := exec.LookPath("pgrep")
	if err != nil {
		t.Log("pgrep is not on PATH; the leftover-process check was not run")
		return
	}
	left, _ := exec.Command(pgrep, "-g", strconv.Itoa(state.Pid())).Output()
	for _, line := range strings.Fields(string(left)) {
		if line != "" && line != strconv.Itoa(state.Pid()) {
			t.Errorf("a process is still running in the command's process group after it exited: pid %s", line)
		}
	}
}

// CRITERION 7, STRUCTURALLY. The behavioural check above can only observe the commands it happens
// to run; this one fails on the next subcommand somebody adds that spawns something.
func TestNoChannelCommandCanStartAProcess(t *testing.T) {
	body, err := os.ReadFile("channels_cmd.go")
	if err != nil {
		t.Fatalf("reading this Issue's command file: %v", err)
	}
	src := string(body)
	for _, bad := range []string{"exec.Command", "daemon.Start(", "os.StartProcess"} {
		if strings.Contains(src, bad) {
			t.Errorf("channels_cmd.go contains %s — no channel command starts the daemon or any "+
				"other process on a person's behalf (PRD §4.2, criterion 7)", bad)
		}
	}
	// AND NO CHANNEL COMMAND INGESTS (criteria 4 and 5). Ingestion is what the daemon does; a
	// command that "refreshes first" has moved it back to being a thing a person types.
	for _, bad := range []string{"channels.Ingest(", "channels.IngestPass(", "IngestPass("} {
		if strings.Contains(src, bad) {
			t.Errorf("channels_cmd.go contains %s — ingestion is a property of the daemon running, "+
				"not of a command being typed (PRD §3.1, criteria 4 and 5)", bad)
		}
	}
	// A CONTROL. If the file stops being read this proves nothing.
	if !strings.Contains(src, "func runChannels(") {
		t.Fatal("channels_cmd.go does not contain runChannels; this scan is reading the wrong file")
	}
}
