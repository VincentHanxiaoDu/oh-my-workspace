package commands

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// ---------------------------------------------------------------------------
// Driving the command
// ---------------------------------------------------------------------------

type result struct {
	code   int
	stdout string
	stderr string
}

// runNoteCmd drives `omw note ...` through the registry, exactly as main does.
func runNoteCmd(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"note"}, args...), &out, &errb, func(k string) string { return env[k] })
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

// liveSocket PROBES rather than names. It creates a real unix socket in a temp directory and
// returns its path, so the daemon probe is answered by something that genuinely exists on this
// machine instead of by a constant this test happened to agree with.
//
// If this platform cannot make a unix socket, the test that needs one skips: an unrunnable
// assertion must not pass silently.
func liveSocket(t *testing.T) string {
	t.Helper()
	// NOT t.TempDir(): its path is built from the test's name, and a unix socket path has a hard
	// length limit (104 bytes on darwin, 108 on Linux) that a long test name silently exceeds. That
	// is exactly the environment assumption this helper exists to avoid — a socket that could not be
	// bound would skip every test here and the suite would go green having asserted nothing.
	dir, err := os.MkdirTemp("", "omw")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := filepath.Join(dir, "s")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Skipf("this environment cannot create a unix socket to probe: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if _, err := os.Stat(p); err != nil {
		t.Skipf("the socket was created but cannot be stat'd here: %v", err)
	}
	return p
}

// withSource swaps the hub source for the duration of a test and restores it.
func withSource(t *testing.T, src hub.VersionSource, arch *hub.Archive, err error) {
	t.Helper()
	prev := noteSource
	noteSource = func(cli.Env) (hub.VersionSource, *hub.Archive, error) { return src, arch, err }
	t.Cleanup(func() { noteSource = prev })
}

func seededStore(t *testing.T) (*hub.Store, hub.NoteID) {
	t.Helper()
	s := hub.NewStore(hub.NewRecord())
	n, err := s.Publish(hub.Publication{Author: "ada", Title: "quota", Body: "the limit is fourhundred"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := s.Amend(n.ID, "the limit is ninehundred"); err != nil {
		t.Fatalf("amend: %v", err)
	}
	return s, n.ID
}

// hubEnv is a machine with a hub configured and a daemon running.
func hubEnv(t *testing.T) map[string]string {
	return map[string]string{noteEnvHub: "https://hub.example", noteEnvSocket: liveSocket(t)}
}

// ---------------------------------------------------------------------------
// Criterion 10 — nothing implicit
// ---------------------------------------------------------------------------

func TestReadingATimelineNeverStartsTheDaemonAndSaysItIsNotRunning(t *testing.T) {
	// A hub IS configured, so the daemon is the relevant missing thing. The socket path names a
	// file that does not exist — and the command must not create it, connect to it, or start
	// anything.
	sock := filepath.Join(t.TempDir(), "not-there.sock")
	dialled := false
	prev := noteSource
	noteSource = func(cli.Env) (hub.VersionSource, *hub.Archive, error) {
		dialled = true
		return nil, nil, nil
	}
	t.Cleanup(func() { noteSource = prev })

	got := runNoteCmd(t, map[string]string{noteEnvHub: "https://hub.example", noteEnvSocket: sock}, "versions", "note-1")
	if got.code != cli.ExitFailure {
		t.Fatalf("exit = %d, want %d", got.code, cli.ExitFailure)
	}
	if !strings.Contains(got.stderr, hub.ErrDaemonNotRunning.Code) {
		t.Fatalf("stderr does not say the daemon is not running:\n%s", got.stderr)
	}
	if dialled {
		t.Fatalf("the command reached for the hub before establishing the daemon was running")
	}
	if _, err := os.Stat(sock); err == nil {
		t.Fatalf("the command created %s — it started something on the person's behalf", sock)
	}
}

func TestWithNoHubConfiguredNothingIsReachedFor(t *testing.T) {
	dialled := false
	prev := noteSource
	noteSource = func(cli.Env) (hub.VersionSource, *hub.Archive, error) {
		dialled = true
		return nil, nil, nil
	}
	t.Cleanup(func() { noteSource = prev })

	for _, args := range [][]string{
		{"versions", "note-1"}, {"show", "note-1"}, {"read", "note-1@v1"}, {"search", "quota"},
	} {
		got := runNoteCmd(t, map[string]string{}, args...)
		if dialled {
			t.Fatalf("%v opened a connection with no hub configured", args)
		}
		if got.code != cli.ExitFailure {
			t.Fatalf("%v exit = %d, want %d", args, got.code, cli.ExitFailure)
		}
		if !strings.Contains(got.stderr, hub.ErrNoHubConfigured.Code) {
			t.Fatalf("%v does not name what is missing:\n%s", args, got.stderr)
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 11 and 12 — precise about what is missing, never an empty history
// ---------------------------------------------------------------------------

func TestTheFourReportsAreComparedPairwiseAndNoneIsAnEmptyTimeline(t *testing.T) {
	s, id := seededStore(t)

	// 1. A real timeline.
	withSource(t, s, nil, nil)
	real := runNoteCmd(t, hubEnv(t), "versions", string(id), "--as", "ada")
	if real.code != cli.Success {
		t.Fatalf("a real timeline exited %d: %s", real.code, real.stderr)
	}

	// 2. A hub that is configured and cannot be reached.
	withSource(t, nil, nil, hub.ErrHubUnreachable)
	unreachable := runNoteCmd(t, hubEnv(t), "versions", string(id), "--as", "ada")
	if unreachable.code != cli.ExitUndetermined {
		t.Fatalf("an unreachable hub exited %d, want %d", unreachable.code, cli.ExitUndetermined)
	}

	// 3. No hub at all.
	noHub := runNoteCmd(t, map[string]string{}, "versions", string(id), "--as", "ada")

	// 4. A refusal — a note that is not there.
	withSource(t, s, nil, nil)
	refusal := runNoteCmd(t, hubEnv(t), "versions", "note-does-not-exist", "--as", "ada")
	if refusal.code != cli.ExitFailure {
		t.Fatalf("a refusal exited %d, want %d", refusal.code, cli.ExitFailure)
	}

	reports := map[string]result{"real": real, "unreachable": unreachable, "no-hub": noHub, "refusal": refusal}
	names := []string{"real", "unreachable", "no-hub", "refusal"}
	for i := range names {
		a := reports[names[i]]
		if strings.TrimSpace(a.stdout+a.stderr) == "" {
			t.Fatalf("%s is silence", names[i])
		}
		for j := i + 1; j < len(names); j++ {
			b := reports[names[j]]
			// PAIRWISE, on the whole report — output and exit code together, because a script
			// reads the code and a person reads the text.
			if a.stdout == b.stdout && a.stderr == b.stderr && a.code == b.code {
				t.Fatalf("%s and %s are the same report:\n%s%s", names[i], names[j], a.stdout, a.stderr)
			}
		}
	}
	// AND THE SPECIFIC CONFUSION THE CRITERION NAMES: neither failure may look like a note with a
	// short or absent history.
	for _, name := range []string{"unreachable", "no-hub"} {
		if strings.Contains(reports[name].stdout, "versions: 0") || strings.Contains(reports[name].stdout, "versions: 1") {
			t.Fatalf("%s renders as a note with a history:\n%s", name, reports[name].stdout)
		}
	}
	if !strings.Contains(unreachable.stdout, hub.UndeterminedTimelineLine) {
		t.Fatalf("the unreachable report does not say the timeline could not be determined:\n%s", unreachable.stdout)
	}
	if !strings.Contains(noHub.stderr, "no hub to ask") {
		t.Fatalf("the no-hub report is not precise about what is missing:\n%s", noHub.stderr)
	}
	// The three exit codes never collide: determined-nothing, could-not-determine, and worked.
	if real.code == unreachable.code || unreachable.code == refusal.code || real.code == refusal.code {
		t.Fatalf("exit codes collide: real=%d unreachable=%d refusal=%d", real.code, unreachable.code, refusal.code)
	}
}

func TestTheLocalDraftHalfWorksWithNoHubConfiguredAtAll(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	env := map[string]string{} // no OMW_HUB, no OMW_CONTROL_SOCKET

	if got := runNoteCmd(t, env, "draft", "create", "--dir", dir); got.code != cli.Success {
		t.Fatalf("create exited %d: %s", got.code, got.stderr)
	}
	for _, body := range []string{"first", "second", "third"} {
		if got := runNoteCmd(t, env, "draft", "revise", "--dir", dir, "plan", body); got.code != cli.Success {
			t.Fatalf("revise %q exited %d: %s", body, got.code, got.stderr)
		}
	}
	list := runNoteCmd(t, env, "draft", "versions", "--dir", dir, "plan")
	if list.code != cli.Success {
		t.Fatalf("draft versions exited %d: %s", list.code, list.stderr)
	}
	if !strings.Contains(list.stdout, "versions: 3") {
		t.Fatalf("the local timeline is not complete:\n%s", list.stdout)
	}
	// Addressable: take the ref the listing printed and read it back.
	read := runNoteCmd(t, env, "draft", "read", "--dir", dir, "plan@v1")
	if read.code != cli.Success {
		t.Fatalf("draft read exited %d: %s", read.code, read.stderr)
	}
	if !strings.Contains(read.stdout, "first") {
		t.Fatalf("revision 1 does not read as it stood:\n%s", read.stdout)
	}
	if !strings.Contains(read.stdout, hub.StandingSupersededLine) {
		t.Fatalf("an old local revision is not labelled superseded:\n%s", read.stdout)
	}

	// AND THE DISTINCTION THE CRITERION IS ABOUT: "this has one version" vs "I could not see them".
	oneVersion := runNoteCmd(t, env, "draft", "revise", "--dir", dir, "solo", "only")
	if oneVersion.code != cli.Success {
		t.Fatalf("revise: %s", oneVersion.stderr)
	}
	solo := runNoteCmd(t, env, "draft", "versions", "--dir", dir, "solo")
	published := runNoteCmd(t, env, "versions", "note-1")
	if solo.stdout == published.stdout || solo.code == published.code {
		t.Fatalf("a one-revision draft and an unavailable published timeline are not distinguishable:\n%q %d\n%q %d",
			solo.stdout, solo.code, published.stdout, published.code)
	}
	if !strings.Contains(published.stderr, "local draft revisions do not need a hub") {
		t.Fatalf("the hub-needing half does not point at the half that works:\n%s", published.stderr)
	}
}

func TestNoDraftOutboxIsConjuredByAReadOrARevision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never-created")
	for _, args := range [][]string{
		{"draft", "versions", "--dir", dir, "plan"},
		{"draft", "revise", "--dir", dir, "plan", "text"},
		{"draft", "read", "--dir", dir, "plan@v1"},
		{"draft", "list", "--dir", dir},
	} {
		got := runNoteCmd(t, map[string]string{}, args...)
		if got.code == cli.Success {
			t.Fatalf("%v succeeded against a directory nobody created", args)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Fatalf("%v created %s", args, dir)
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 5, 6, 8 and 9 — what the person reads, and what a script reads
// ---------------------------------------------------------------------------

func TestReadingAnOldVersionSaysSoAndReadingTheCurrentOneSaysThat(t *testing.T) {
	s, id := seededStore(t)
	withSource(t, s, nil, nil)
	env := hubEnv(t)

	old := runNoteCmd(t, env, "read", string(id)+"@v1", "--as", "ada")
	if old.code != cli.Success {
		t.Fatalf("read v1 exited %d: %s", old.code, old.stderr)
	}
	if !strings.Contains(old.stdout, hub.StandingSupersededLine) {
		t.Fatalf("reading v1 does not say it is superseded:\n%s", old.stdout)
	}
	if !strings.Contains(old.stdout, "fourhundred") {
		t.Fatalf("reading v1 does not return the earlier text:\n%s", old.stdout)
	}

	cur := runNoteCmd(t, env, "show", string(id), "--as", "ada")
	if !strings.Contains(cur.stdout, hub.StandingCurrentLine) {
		t.Fatalf("reading the note without naming a version does not say it is current:\n%s", cur.stdout)
	}
	if strings.Contains(cur.stdout, "fourhundred") {
		t.Fatalf("an unqualified read served superseded text:\n%s", cur.stdout)
	}
	if old.stdout == cur.stdout {
		t.Fatalf("a superseded read and a current read are the same output")
	}
}

func TestAMissingVersionAndAnEmptyOneDifferByExitStatusAlone(t *testing.T) {
	s := hub.NewStore(hub.NewRecord())
	n, err := s.Publish(hub.Publication{Author: "ada", Title: "t", Body: ""})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	withSource(t, s, nil, nil)
	env := hubEnv(t)

	empty := runNoteCmd(t, env, "read", string(n.ID)+"@v1", "--as", "ada")
	missing := runNoteCmd(t, env, "read", string(n.ID)+"@v9", "--as", "ada")
	if empty.code != cli.Success {
		t.Fatalf("a version whose body is empty exited %d, want %d", empty.code, cli.Success)
	}
	if missing.code == empty.code {
		t.Fatalf("a missing version and an empty one share exit code %d", missing.code)
	}
	if missing.code != cli.ExitFailure {
		t.Fatalf("a missing version exited %d, want %d", missing.code, cli.ExitFailure)
	}
	if !strings.Contains(missing.stderr, hub.ErrNoSuchVersion.Code) {
		t.Fatalf("stderr does not carry the no-such-version code:\n%s", missing.stderr)
	}
	if strings.Contains(missing.stdout, "body:") {
		t.Fatalf("a missing version rendered blank content on stdout:\n%q", missing.stdout)
	}
}

// unreadableSource has a timeline but cannot fetch a body — a persistent store whose blob is gone.
type unreadableSource struct{ versions []hub.Version }

func (u unreadableSource) Timeline(hub.NoteID, hub.PersonID) ([]hub.Version, error) {
	return u.versions, nil
}
func (u unreadableSource) VersionAt(hub.NoteID, int, hub.PersonID) (hub.Version, error) {
	return hub.Version{}, hub.Refusedf(hub.ErrVersionUnreadable, "the blob store did not answer")
}

func TestAnUnreadableVersionExitsUndeterminedAndNeverPrintsAnEmptyBody(t *testing.T) {
	withSource(t, unreadableSource{versions: []hub.Version{{Number: 1, Body: "x"}}}, nil, nil)
	env := hubEnv(t)

	got := runNoteCmd(t, env, "read", "note-1@v1", "--as", "ada")
	if got.code != cli.ExitUndetermined {
		t.Fatalf("exit = %d, want %d", got.code, cli.ExitUndetermined)
	}
	if !strings.Contains(got.stdout, hub.BodyUnreadableLine) {
		t.Fatalf("output does not say the body could not be read:\n%s", got.stdout)
	}

	// Against a genuinely empty body, pairwise: different text AND a different exit code.
	s := hub.NewStore(hub.NewRecord())
	n, _ := s.Publish(hub.Publication{Author: "ada", Title: "t", Body: ""})
	withSource(t, s, nil, nil)
	empty := runNoteCmd(t, env, "read", string(n.ID)+"@v1", "--as", "ada")
	if empty.code == got.code {
		t.Fatalf("an unreadable body and an empty body share exit code %d", got.code)
	}
	if empty.stdout == got.stdout {
		t.Fatalf("an unreadable body and an empty body print the same thing:\n%s", got.stdout)
	}
}

// ---------------------------------------------------------------------------
// Criterion 13 — the CLI and the control API agree
// ---------------------------------------------------------------------------

func TestTheCLIAndTheControlAPIAgreeAboutVersionState(t *testing.T) {
	s, id := seededStore(t)
	withSource(t, s, nil, nil)
	env := hubEnv(t)

	text := runNoteCmd(t, env, "versions", string(id), "--as", "ada")
	jsonOut := runNoteCmd(t, env, "versions", string(id), "--json", "--as", "ada")
	if text.code != jsonOut.code {
		t.Fatalf("the two surfaces exit differently: text %d, json %d", text.code, jsonOut.code)
	}
	var answer hub.TimelineAnswer
	if err := json.Unmarshal([]byte(jsonOut.stdout), &answer); err != nil {
		t.Fatalf("the control API surface is not decodable: %v\n%s", err, jsonOut.stdout)
	}
	if len(answer.Versions) == 0 {
		t.Fatalf("the control API showed no versions:\n%s", jsonOut.stdout)
	}
	// NEITHER IS COMPARED TO A FIXTURE. Every version one shows, the other must show.
	for _, v := range answer.Versions {
		if !strings.Contains(text.stdout, v.Ref) {
			t.Fatalf("the control API shows %q and the CLI does not:\n%s", v.Ref, text.stdout)
		}
	}
	if !strings.Contains(text.stdout, "current: "+answer.Current) {
		t.Fatalf("the two disagree about which version is current: json %q\n%s", answer.Current, text.stdout)
	}
	// And the count they each state.
	if !strings.Contains(text.stdout, "versions: "+strconv.Itoa(len(answer.Versions))) {
		t.Fatalf("the CLI's count and the control API's differ:\n%s\n%s", text.stdout, jsonOut.stdout)
	}

	// One version, both ways.
	vtext := runNoteCmd(t, env, "read", string(id)+"@v1", "--as", "ada")
	vjson := runNoteCmd(t, env, "read", string(id)+"@v1", "--json", "--as", "ada")
	var va hub.VersionAnswer
	if err := json.Unmarshal([]byte(vjson.stdout), &va); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if va.Standing != "superseded" {
		t.Fatalf("the control API says standing %q for v1 of a two-version note", va.Standing)
	}
	if !strings.Contains(vtext.stdout, hub.StandingSupersededLine) {
		t.Fatalf("the CLI does not agree that v1 is superseded:\n%s", vtext.stdout)
	}
	if !strings.Contains(vtext.stdout, va.Body) {
		t.Fatalf("the two surfaces disagree about the body")
	}
	if vtext.code != vjson.code {
		t.Fatalf("exit codes differ: %d vs %d", vtext.code, vjson.code)
	}
}

// ---------------------------------------------------------------------------
// Criteria 4 and 14
// ---------------------------------------------------------------------------

func TestSearchOnTheCLIFindsTheLatestAndNamesTheVersion(t *testing.T) {
	s, id := seededStore(t)
	withSource(t, s, nil, nil)
	env := hubEnv(t)

	stale := runNoteCmd(t, env, "search", "fourhundred", "--as", "bo")
	if !strings.Contains(stale.stdout, "results: 0") {
		t.Fatalf("a term that exists only in a superseded version produced results:\n%s", stale.stdout)
	}
	current := runNoteCmd(t, env, "search", "ninehundred", "--as", "bo")
	if !strings.Contains(current.stdout, "results: 1") {
		t.Fatalf("search did not find the current version:\n%s", current.stdout)
	}
	if !strings.Contains(current.stdout, string(id)+"@v2") {
		t.Fatalf("the result does not name the version it refers to:\n%s", current.stdout)
	}
	if !strings.Contains(current.stdout, hub.StandingCurrentLine) {
		t.Fatalf("the result does not say it is the current version:\n%s", current.stdout)
	}
}

func TestAReaderNarrowedOutOfANoteReachesNoneOfItsVersionsThroughTheCLI(t *testing.T) {
	s := hub.NewStore(hub.NewRecord())
	n, err := s.Publish(hub.Publication{Author: "ada", Title: "t", Body: "v1 secret quota"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := s.Amend(n.ID, "v2 secret quota"); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if _, err := s.SetVisibility(n.ID, "ada", hub.SelfOnly()); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	withSource(t, s, nil, nil)
	env := hubEnv(t)

	for _, args := range [][]string{
		{"versions", string(n.ID), "--as", "bo"},
		{"read", string(n.ID) + "@v1", "--as", "bo"},
		{"read", string(n.ID) + "@v2", "--as", "bo"},
		{"show", string(n.ID), "--as", "bo"},
		{"versions", string(n.ID), "--as", "bo", "--json"},
	} {
		got := runNoteCmd(t, env, args...)
		if got.code == cli.Success {
			t.Fatalf("%v succeeded for a reader narrowed out of the note:\n%s", args, got.stdout)
		}
		if strings.Contains(got.stdout+got.stderr, "quota") {
			t.Fatalf("%v leaked note content:\n%s%s", args, got.stdout, got.stderr)
		}
	}
	// Criterion 14's second half: the answer must not differ from a note that does not exist.
	refused := runNoteCmd(t, env, "versions", string(n.ID), "--as", "bo")
	missing := runNoteCmd(t, env, "versions", "note-9999", "--as", "bo")
	norm := func(r result, id string) string {
		return strings.ReplaceAll(r.stdout+"\n"+r.stderr, id, "<id>")
	}
	if refused.code != missing.code || norm(refused, string(n.ID)) != norm(missing, "note-9999") {
		t.Fatalf("a refused reader can tell the note exists:\n%q\n%q", norm(refused, string(n.ID)), norm(missing, "note-9999"))
	}
	// And the author is unaffected.
	if got := runNoteCmd(t, env, "read", string(n.ID)+"@v1", "--as", "ada"); got.code != cli.Success {
		t.Fatalf("the author lost their own history: %s", got.stderr)
	}
}

// ---------------------------------------------------------------------------
// The command itself
// ---------------------------------------------------------------------------

func TestUnknownSubcommandsAndRefsAreNamedNotDumped(t *testing.T) {
	got := runNoteCmd(t, map[string]string{}, "verisons", "note-1")
	if got.code != cli.ExitUsage {
		t.Fatalf("exit = %d, want %d", got.code, cli.ExitUsage)
	}
	if !strings.Contains(got.stderr, `"verisons"`) {
		t.Fatalf("the typo is not echoed back:\n%s", got.stderr)
	}
	bad := runNoteCmd(t, map[string]string{}, "read", "note-1")
	if bad.code != cli.ExitUsage || !strings.Contains(bad.stderr, hub.ErrBadVersionRef.Code) {
		t.Fatalf("a malformed ref exited %d:\n%s", bad.code, bad.stderr)
	}
	// A malformed ref is rejected BEFORE any hub is considered — it is a usage error about what
	// was typed, not a report about a machine's configuration.
	if strings.Contains(bad.stderr, hub.ErrNoHubConfigured.Code) {
		t.Fatalf("a malformed ref was reported as a missing hub:\n%s", bad.stderr)
	}
}

func TestTheNoteCommandIsRegisteredAndListed(t *testing.T) {
	c, ok := cli.Lookup("note")
	if !ok {
		t.Fatalf("omw note is not registered")
	}
	if c.Summary == "" {
		t.Fatalf("omw note has no summary and would print as a blank line in the command list")
	}
}

func TestTheVersionSchemaIsServedAndCarriesTheDistinctions(t *testing.T) {
	got := runNoteCmd(t, map[string]string{}, "schema")
	if got.code != cli.Success {
		t.Fatalf("schema exited %d: %s", got.code, got.stderr)
	}
	var tools []hub.ToolSchema
	if err := json.Unmarshal([]byte(got.stdout), &tools); err != nil {
		t.Fatalf("the schema is not decodable: %v", err)
	}
	if len(tools) == 0 {
		t.Fatalf("the schema is empty")
	}
	for _, want := range []string{hub.ErrNoSuchVersion.Code, hub.ErrVersionUnreadable.Code} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("the schema does not tell a caller about %q:\n%s", want, got.stdout)
		}
	}
	// No fourth scope leaked into the agent API.
	for _, tool := range tools {
		for _, s := range tool.Scopes {
			if s != string(hub.ScopeRead) {
				t.Fatalf("tool %q declares scope %q", tool.Tool, s)
			}
		}
	}
}

// An unidentified reader is not a refusal and not a success: the command was not told who is
// asking, so it did not work out whether they may read it. Criterion 8's shape, at the door.
func TestAReadWithNoIdentityIsUndeterminedRatherThanRefusedOrServed(t *testing.T) {
	s, id := seededStore(t)
	withSource(t, s, nil, nil)
	env := hubEnv(t)

	anon := runNoteCmd(t, env, "versions", string(id))
	if anon.code != cli.ExitUndetermined {
		t.Fatalf("exit = %d, want %d", anon.code, cli.ExitUndetermined)
	}
	if strings.Contains(anon.stdout, "ninehundred") || strings.Contains(anon.stdout, "fourhundred") {
		t.Fatalf("an unidentified reader was served content:\n%s", anon.stdout)
	}
	named := runNoteCmd(t, env, "versions", string(id), "--as", "ada")
	if named.code != cli.Success {
		t.Fatalf("a named reader exited %d: %s", named.code, named.stderr)
	}
	if anon.stdout == named.stdout {
		t.Fatalf("an unidentified read and a named read are the same output")
	}
}
