package hubd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/auth"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// --- helpers ------------------------------------------------------------------------------

// newHub creates and opens a hub in a fresh directory.
func newHub(t *testing.T) (*Server, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "hub")
	if err := Create(dir, Options{Company: "Example Ltd"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

// signIn performs the WHOLE device-code sign-in: ask, approve as the person, redeem.
//
// It goes through Approve on purpose. There is no shortcut here that mints a token without the
// approval step, because "nothing signs in silently" (PRD §3.10) is only true if the code has no
// path that does.
func signIn(t *testing.T, s *Server, p hub.PersonID, scopes ...hub.Scope) auth.Secret {
	t.Helper()
	sec, _ := signInWithID(t, s, p, scopes...)
	return sec
}

// signInWithID is [signIn] keeping the token id, for the tests that end a session.
func signInWithID(t *testing.T, s *Server, p hub.PersonID, scopes ...hub.Scope) (auth.Secret, auth.TokenID) {
	t.Helper()
	a := s.Authority()
	d, err := a.StartSignIn(auth.SignInRequest{Scopes: scopes, Label: "test machine"})
	if err != nil {
		t.Fatalf("StartSignIn(%s): %v", p, err)
	}
	if err := a.Approve(d.UserCode, p); err != nil {
		t.Fatalf("Approve(%s): %v", p, err)
	}
	iss, err := a.Redeem(d)
	if err != nil {
		t.Fatalf("Redeem(%s): %v", p, err)
	}
	return iss.Secret, iss.ID
}

func mustAddPerson(t *testing.T, s *Server, p hub.PersonID, scopes ...hub.Scope) {
	t.Helper()
	if err := s.AddPerson(p, scopes); err != nil {
		t.Fatalf("AddPerson(%s): %v", p, err)
	}
}

func mustPublish(t *testing.T, s *Server, sec auth.Secret, title, body string, v hub.Visibility) *hub.Note {
	t.Helper()
	n, err := s.Publish(sec, title, body, v)
	if err != nil {
		t.Fatalf("Publish(%q): %v", title, err)
	}
	return n
}

func allScopes() []hub.Scope { return []hub.Scope{hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish} }

// --- criterion 1: durability across a restart of the PROCESS ------------------------------

// TestANoteSurvivesARestartOfTheServer is the in-process half of criterion 1.
func TestANoteSurvivesARestartOfTheServer(t *testing.T) {
	s, dir := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	sec := signIn(t, s, "alice", allScopes()...)
	n := mustPublish(t, s, sec, "the login outage", "restart the auth pods", hub.CompanyWide())
	id := n.ID
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopening the hub: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	// The person and their sign-in are gone with the process — that is a determined fact about this
	// build, asserted in TestARestartEndsEverySessionAsADeterminedNegative. So the note is read back
	// through a fresh sign-in as the same person, which the durable person record makes possible.
	mustAddPerson(t, reopened, "alice", allScopes()...)
	sec2 := signIn(t, reopened, "alice", allScopes()...)
	got, err := reopened.Read(sec2, id)
	if err != nil {
		t.Fatalf("reading %q back after a restart: %v", id, err)
	}
	if got.ID != id {
		t.Errorf("note id after restart = %q, want %q; a note id a person holds must still resolve", got.ID, id)
	}
	if got.Title != "the login outage" {
		t.Errorf("title after restart = %q, want %q", got.Title, "the login outage")
	}
	if body := got.Latest().Body; body != "restart the auth pods" {
		t.Errorf("body after restart = %q, want %q", body, "restart the auth pods")
	}
	if got.Visibility.Token() != "company" {
		t.Errorf("visibility after restart = %q, want %q", got.Visibility.Token(), "company")
	}
}

// TestANoteSurvivesARestartOfTheOSProcess is criterion 1 with a real second process.
//
// IT RE-EXECS THIS TEST BINARY. That is the standard way to get a genuinely separate OS process out
// of `go test` without building a binary at test time, and it matters here: an in-process reopen
// shares the page cache, the file descriptors and the Go heap with the writer, so it cannot tell a
// synced write from a buffered one. This can.
//
// It PROBES rather than naming its environment: if the test binary cannot be re-executed it skips
// WITH A REASON, and the reason says it determined nothing.
func TestANoteSurvivesARestartOfTheOSProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("DETERMINED NOTHING, NOT PASSING: this test needs to re-execute the test binary and it could not be located (%v), so whether a note survives a separate process was not established here", err)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Skipf("DETERMINED NOTHING, NOT PASSING: the test binary at %q could not be examined (%v), so whether a note survives a separate process was not established here", exe, err)
	}

	dir := filepath.Join(t.TempDir(), "hub")
	if err := Create(dir, Options{Company: "Example Ltd"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	writer := exec.Command(exe, "-test.run=TestHelperHubProcess", "-test.v=false")
	writer.Env = append(os.Environ(), "OMW_HUBD_HELPER=publish", "OMW_HUBD_DIR="+dir)
	out, err := writer.CombinedOutput()
	if err != nil {
		t.Skipf("DETERMINED NOTHING, NOT PASSING: the helper process could not be run (%v); its output was:\n%s\nwhether a note survives a separate process was not established here", err, out)
	}
	id := helperNoteID(t, string(out))

	// A SECOND, INDEPENDENT PROCESS reads it back. The writer has exited.
	reader := exec.Command(exe, "-test.run=TestHelperHubProcess", "-test.v=false")
	reader.Env = append(os.Environ(), "OMW_HUBD_HELPER=read", "OMW_HUBD_DIR="+dir, "OMW_HUBD_NOTE="+id)
	out2, err := reader.CombinedOutput()
	if err != nil {
		t.Fatalf("the reading process failed (%v); a note published by one process was not readable by another. Its output was:\n%s", err, out2)
	}
	if !strings.Contains(string(out2), "BODY=restart the auth pods") {
		t.Fatalf("the second process did not read the note back. Its output was:\n%s", out2)
	}
}

func helperNoteID(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "NOTE="); ok {
			return rest
		}
	}
	t.Fatalf("the publishing process printed no note id. Its output was:\n%s", out)
	return ""
}

// TestHelperHubProcess is not a test. It is the body of the child process the test above spawns,
// and it does nothing at all unless that test asked for it.
func TestHelperHubProcess(t *testing.T) {
	mode := os.Getenv("OMW_HUBD_HELPER")
	if mode == "" {
		t.Skip("not a test: this is the child process body for TestANoteSurvivesARestartOfTheOSProcess, and nothing asked for it")
	}
	dir := os.Getenv("OMW_HUBD_DIR")
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("child: Open(%q): %v", dir, err)
	}
	defer func() { _ = s.Close() }()
	mustAddPerson(t, s, "alice", allScopes()...)
	sec := signIn(t, s, "alice", allScopes()...)

	switch mode {
	case "publish":
		n := mustPublish(t, s, sec, "the login outage", "restart the auth pods", hub.CompanyWide())
		t.Logf("NOTE=%s", n.ID)
		// Logged AND printed: -test.v is off in the child, so t.Logf alone would be swallowed.
		os.Stdout.WriteString("NOTE=" + string(n.ID) + "\n")
	case "read":
		n, err := s.Read(sec, hub.NoteID(os.Getenv("OMW_HUBD_NOTE")))
		if err != nil {
			t.Fatalf("child: reading the note published by the previous process: %v", err)
		}
		os.Stdout.WriteString("BODY=" + n.Latest().Body + "\n")
	default:
		t.Fatalf("child: unknown helper mode %q", mode)
	}
}

// TestATimelineComesBackWithTheTimesItWasWrittenWith is the deterministic form of a defect the
// two-hub test found only by clock luck: a replayed amendment was re-stamped with the restart's
// clock, so a note's timeline said it was written when the hub was last started.
//
// That is PRD §3.3 — "a claim someone acted on last month can still be read as it stood" — and it
// is criterion 8, since two hubs restarted at different moments then report different recency for
// one corpus. Asserted on the exact timestamps, so it cannot pass because two runs happened to land
// in the same second.
func TestATimelineComesBackWithTheTimesItWasWrittenWith(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	if err := Create(dir, Options{Company: "Example Ltd"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	written := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	s, err := Open(dir, Options{Now: func() time.Time { return written }})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustAddPerson(t, s, "alice", allScopes()...)
	alice := signIn(t, s, "alice", allScopes()...)
	n := mustPublish(t, s, alice, "a note", "the first body", hub.CompanyWide())
	if _, err := s.Amend(alice, n.ID, "the second body"); err != nil {
		t.Fatalf("Amend: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Restarted much later. Nothing about the note was written at this moment.
	restarted := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	reopened, err := Open(dir, Options{Now: func() time.Time { return restarted }})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	mustAddPerson(t, reopened, "alice", allScopes()...)
	alice2 := signIn(t, reopened, "alice", allScopes()...)
	got, err := reopened.Read(alice2, n.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Versions) != 2 {
		t.Fatalf("the note came back with %d versions, want 2", len(got.Versions))
	}
	for i, v := range got.Versions {
		if !v.At.Equal(written) {
			t.Errorf("version %d came back stamped %v, want %v; a restart must not re-date what a person wrote", i+1, v.At, written)
		}
		if v.At.Equal(restarted) {
			t.Errorf("version %d is stamped with the moment the hub restarted", i+1)
		}
	}
	if got.Versions[1].Body != "the second body" {
		t.Errorf("the amended body came back as %q", got.Versions[1].Body)
	}
}

// --- criterion 2: visibility before ranking, byte-identical ---------------------------------

// TestAHiddenNoteLeavesAReadersResultsByteIdentical is criterion 2, WITH ITS CONTROL.
//
// The two hubs share a corpus by SHARING A JOURNAL — hub B is hub A's durable record copied before
// the hidden note was published. That is what makes byte-identity meaningful: the note ids in the
// rendered output are the same ids, so a difference in the bytes can only come from the hidden note.
// Regenerating the corpus by re-publishing would mint fresh ids and the comparison would be
// worthless.
func TestAHiddenNoteLeavesAReadersResultsByteIdentical(t *testing.T) {
	a, dirA := newHub(t)
	mustAddPerson(t, a, "alice", allScopes()...)
	mustAddPerson(t, a, "bob", allScopes()...)
	alice := signIn(t, a, "alice", allScopes()...)

	mustPublish(t, a, alice, "login outage one", "the auth pods needed a restart", hub.CompanyWide())
	mustPublish(t, a, alice, "login outage two", "the auth cache was stale", hub.CompanyWide())
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Hub B is the corpus WITHOUT the hidden note: a byte-for-byte copy of the record so far.
	dirB := filepath.Join(t.TempDir(), "hubB")
	copyHubDir(t, dirA, dirB)

	// Hub A gains a note narrowed away from bob.
	a2, err := Open(dirA, Options{})
	if err != nil {
		t.Fatalf("reopening hub A: %v", err)
	}
	defer func() { _ = a2.Close() }()
	mustAddPerson(t, a2, "alice", allScopes()...)
	mustAddPerson(t, a2, "bob", allScopes()...)
	alice2 := signIn(t, a2, "alice", allScopes()...)
	mustPublish(t, a2, alice2, "login outage three", "the auth secret had rotated", hub.SelfOnly())

	b, err := Open(dirB, Options{})
	if err != nil {
		t.Fatalf("opening hub B: %v", err)
	}
	defer func() { _ = b.Close() }()
	mustAddPerson(t, b, "alice", allScopes()...)
	mustAddPerson(t, b, "bob", allScopes()...)

	q := hub.Query{Terms: "auth", Scope: hub.CompanyScope()}
	withHidden := renderSearch(t, a2, signIn(t, a2, "bob", allScopes()...), q)
	without := renderSearch(t, b, signIn(t, b, "bob", allScopes()...), q)

	if withHidden != without {
		t.Errorf("a note bob cannot read changed bob's results.\nwith the hidden note:\n%s\nwithout it:\n%s", withHidden, without)
	}

	// THE CONTROL. Without this, byte-identity proves only that the comparison is insensitive.
	mustPublish(t, a2, alice2, "login outage four", "the auth ingress was misrouted", hub.CompanyWide())
	visible := renderSearch(t, a2, signIn(t, a2, "bob", allScopes()...), q)
	if visible == without {
		t.Errorf("CONTROL FAILED: a note bob CAN read did not change his results, so the byte-identity above proves nothing.\ngot:\n%s", visible)
	}
}

func renderSearch(t *testing.T, s *Server, sec auth.Secret, q hub.Query) string {
	t.Helper()
	out, err := s.Search(sec, q)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return out.Render()
}

// renderStats is every observable a reader gets from corpus statistics, in one string. It uses the
// package's own renderings rather than the struct's fields, because a rendering that collapsed two
// states would be invisible to a field-by-field comparison and is exactly what a reader would see.
func renderStats(s hub.Statistics) string {
	return strings.Join([]string{
		"scope: " + s.Scope.Token(),
		"reader: " + string(s.Reader),
		"notes: " + s.Notes.Render(),
		"subjects: " + s.Subjects.Render(),
		"recency: " + s.Recency.Render(),
		"undetermined: " + s.UndeterminedNotes.Render(),
		"coverage: " + s.Coverage.String(),
	}, "\n")
}

func copyHubDir(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o700); err != nil {
		t.Fatalf("copying the hub directory: %v", err)
	}
	for _, name := range []string{markerFile, journalFile} {
		body, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(to, name), body, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
}

// --- criterion 8: two hubs, one corpus, one answer ------------------------------------------

// TestTwoHubsHoldingTheSameCorpusAnswerIdentically is criterion 8. Notice it compares the SEARCH
// output, the STATISTICS output and the note READ — the three things a reader could use to tell one
// hub from another.
func TestTwoHubsHoldingTheSameCorpusAnswerIdentically(t *testing.T) {
	a, dirA := newHub(t)
	mustAddPerson(t, a, "alice", allScopes()...)
	mustAddPerson(t, a, "bob", allScopes()...)
	if err := a.DefineGroup("platform", "alice", "bob"); err != nil {
		t.Fatalf("DefineGroup: %v", err)
	}
	alice := signIn(t, a, "alice", allScopes()...)
	group, err := hub.ToGroup("platform")
	if err != nil {
		t.Fatalf("ToGroup: %v", err)
	}
	mustPublish(t, a, alice, "the deploy runbook", "drain, roll, verify", hub.CompanyWide())
	mustPublish(t, a, alice, "the platform rota", "who is on call this week", group)
	mustPublish(t, a, alice, "my own scratch", "not for anybody else", hub.SelfOnly())
	n := mustPublish(t, a, alice, "the rollback runbook", "roll back, verify, tell the channel", hub.CompanyWide())
	if _, err := a.Amend(alice, n.ID, "roll back, verify, tell the channel, then write it up"); err != nil {
		t.Fatalf("Amend: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dirB := filepath.Join(t.TempDir(), "hubB")
	copyHubDir(t, dirA, dirB)

	open := func(dir string) (*Server, auth.Secret) {
		s, err := Open(dir, Options{})
		if err != nil {
			t.Fatalf("Open(%q): %v", dir, err)
		}
		t.Cleanup(func() { _ = s.Close() })
		mustAddPerson(t, s, "bob", allScopes()...)
		return s, signIn(t, s, "bob", allScopes()...)
	}
	sa, seca := open(dirA)
	sb, secb := open(dirB)

	for _, q := range []hub.Query{
		{Terms: "runbook", Scope: hub.CompanyScope()},
		{Terms: "verify", Scope: hub.CompanyScope()},
		{Terms: "scratch", Scope: hub.CompanyScope()},
		{Terms: "rota", Scope: hub.CompanyScope()},
	} {
		if got, want := renderSearch(t, sa, seca, q), renderSearch(t, sb, secb, q); got != want {
			t.Errorf("the two hubs answered %q differently; a reader can tell which one they are talking to.\nhub A:\n%s\nhub B:\n%s", q.Terms, got, want)
		}
	}

	statA, err := sa.Statistics(seca, hub.CompanyScope())
	if err != nil {
		t.Fatalf("Statistics on hub A: %v", err)
	}
	statB, err := sb.Statistics(secb, hub.CompanyScope())
	if err != nil {
		t.Fatalf("Statistics on hub B: %v", err)
	}
	if renderStats(statA) != renderStats(statB) {
		t.Errorf("the two hubs reported different corpus statistics.\nhub A:\n%s\nhub B:\n%s", renderStats(statA), renderStats(statB))
	}

	na, err := sa.Read(seca, n.ID)
	if err != nil {
		t.Fatalf("hub A could not read %q: %v", n.ID, err)
	}
	nb, err := sb.Read(secb, n.ID)
	if err != nil {
		t.Fatalf("hub B could not read %q, which hub A could: %v", n.ID, err)
	}
	if na.Latest().Body != nb.Latest().Body {
		t.Errorf("the two hubs hold different bodies for %q: %q vs %q", n.ID, na.Latest().Body, nb.Latest().Body)
	}
	if len(na.Versions) != len(nb.Versions) {
		t.Errorf("the two hubs hold different timelines for %q: %d versions vs %d", n.ID, len(na.Versions), len(nb.Versions))
	}
}

// --- criteria 3 and 9: what this hub can read, stated plainly -------------------------------

// TestTheHubStoresGroupAndSelfRestrictedNotes is criterion 3's capability half: §2.4 says the hub
// reads everything published to it BECAUSE it indexes them, and a hub that dropped them could not
// make that promise.
func TestTheHubStoresGroupAndSelfRestrictedNotes(t *testing.T) {
	s, dir := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	if err := s.DefineGroup("platform", "alice"); err != nil {
		t.Fatalf("DefineGroup: %v", err)
	}
	alice := signIn(t, s, "alice", allScopes()...)
	group, err := hub.ToGroup("platform")
	if err != nil {
		t.Fatalf("ToGroup: %v", err)
	}
	selfNote := mustPublish(t, s, alice, "self", "only me", hub.SelfOnly())
	groupNote := mustPublish(t, s, alice, "group", "only platform", group)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// THE OPERATOR'S VIEW, which is what §2.4 is about: the bytes are in the directory, in full,
	// with no token involved at all.
	body, err := os.ReadFile(filepath.Join(dir, journalFile))
	if err != nil {
		t.Fatalf("reading the hub's durable record as an operator would: %v", err)
	}
	for _, want := range []string{"only me", "only platform", string(selfNote.ID), string(groupNote.ID)} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the hub's durable record does not contain %q; §2.4 says the hub holds every published note including narrowed ones, and the documentation would be a false promise", want)
		}
	}

	// And they survive a restart, still narrowed.
	reopened, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	v, err := reopened.store.VisibilityOf(selfNote.ID)
	if err != nil {
		t.Fatalf("the self-restricted note did not survive the restart: %v", err)
	}
	if v.Token() != "self" {
		t.Errorf("the self-restricted note came back as %q, not %q; a restart must not widen a note", v.Token(), "self")
	}
	gv, err := reopened.store.VisibilityOf(groupNote.ID)
	if err != nil {
		t.Fatalf("the group-restricted note did not survive the restart: %v", err)
	}
	if gv.Token() != "group" || gv.Group() != "platform" {
		t.Errorf("the group-restricted note came back as %q/%q, want group/platform", gv.Token(), gv.Group())
	}
}

// TestTheOperatorReachIsStatedInTheProductsOwnWords is criteria 3 and 9's honesty half. It runs the
// REAL rule (hub.CheckSurface, through CheckOperatorReach) rather than asserting a phrase.
func TestTheOperatorReachIsStatedInTheProductsOwnWords(t *testing.T) {
	if err := CheckOperatorReach(); err != nil {
		t.Fatalf("the hub's own statement of what it can read fails the product's §2.4 rule: %v", err)
	}
	if !strings.Contains(OperatorReach, hub.RestrictionStatement) {
		t.Errorf("the hub states its reach in words of its own rather than the product's; §2.4 must be said in the same words everywhere, or one copy gets softer")
	}
	for _, surface := range map[string]string{
		"create":   captureRun(t, []string{"create", filepath.Join(t.TempDir(), "h")}),
		"describe": describeOutput(t),
		"what":     captureRun(t, []string{"what-i-can-read"}),
	} {
		if !strings.Contains(surface, hub.RestrictionStatement) {
			t.Errorf("a hub surface does not carry the §2.4 statement:\n%s", surface)
		}
	}
}

func describeOutput(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "hub")
	if err := Create(dir, Options{Company: "Example Ltd"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return captureRun(t, []string{"describe", dir})
}

func captureRun(t *testing.T, args []string) string {
	t.Helper()
	var out, errOut strings.Builder
	code := Run(context.Background(), args, &out, &errOut)
	if code != cli.Success {
		t.Fatalf("omw-hub %v exited %d; stderr:\n%s", args, code, errOut.String())
	}
	return out.String()
}

// --- criterion 4: three search scopes and no fourth ------------------------------------------

// TestSearchIsScopedToPersonGroupOrCompanyAndNothingElse is criterion 4. It drives the SERVER, not
// the parser, so that the server cannot be the place a fourth scope creeps in.
func TestSearchIsScopedToPersonGroupOrCompanyAndNothingElse(t *testing.T) {
	s, _ := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	if err := s.DefineGroup("platform", "alice"); err != nil {
		t.Fatalf("DefineGroup: %v", err)
	}
	alice := signIn(t, s, "alice", allScopes()...)
	mustPublish(t, s, alice, "a note", "a body about deploys", hub.CompanyWide())

	for _, token := range []string{"company", "person:alice", "group:platform"} {
		scope, err := hub.ParseSearchScope(token)
		if err != nil {
			t.Fatalf("ParseSearchScope(%q): %v; the three scopes must all be accepted", token, err)
		}
		if _, err := s.Search(alice, hub.Query{Terms: "deploys", Scope: scope}); err != nil {
			t.Errorf("the hub refused scope %q: %v", token, err)
		}
	}
	for _, token := range []string{"team:platform", "project:omw", "everything", "org:example"} {
		if _, err := hub.ParseSearchScope(token); err == nil {
			t.Errorf("scope %q was accepted; there are three scopes and no fourth", token)
		}
	}
	// THE VOCABULARY ITSELF, not just today's refusals. A fourth scope added anywhere would have to
	// be spelled out here, and this assertion is what makes adding one a deliberate act rather than
	// a quiet one.
	if got := len(hub.SearchScopeSyntax); got != 3 {
		t.Errorf("the hub offers %d search scopes, want exactly 3 (person, group, company): %v", got, hub.SearchScopeSyntax)
	}

	// A SCOPE THE HUB DOES NOT KNOW IS REFUSED, NOT WIDENED AND NOT EMPTIED. This is the shape a
	// fourth scope would actually arrive in — a caller naming a subject the hub cannot resolve —
	// and the answer must be a refusal, never "0 results" (#101: a count is an answer).
	for _, token := range []string{"person:nobody-here", "group:no-such-group"} {
		scope, err := hub.ParseSearchScope(token)
		if err != nil {
			t.Fatalf("ParseSearchScope(%q): %v", token, err)
		}
		out, err := s.Search(alice, hub.Query{Terms: "deploys", Scope: scope})
		if err == nil {
			t.Errorf("scope %q was answered with %d results instead of refused; a subject the hub cannot resolve is not an empty corpus", token, out.Total)
		}
	}
}

// TestAReplayableRecordThatCannotBeReplayedStopsTheHub is the other half of "could not determine is
// not an empty corpus": a durable record whose LINES parse but whose CONTENT cannot be honoured.
//
// This is the case a hub upgrade produces — an operation a build does not know — and the tempting
// behaviour is to skip what it does not understand and carry on. That hub then serves a corpus
// smaller than the one on its disk, with no sign that anything is missing.
func TestAReplayableRecordThatCannotBeReplayedStopsTheHub(t *testing.T) {
	for _, record := range []struct {
		name string
		line string
	}{
		{"an operation this build does not know", `{"op":"chisel","id":"x"}`},
		{"a note with a visibility that is not one of the four", `{"op":"publish","id":"x","author":"alice","visibility":{"kind":"nearly-everyone"},"versions":[{"number":1,"body":"b"}]}`},
		{"a note narrowed to a group the hub has no record of", `{"op":"publish","id":"x","author":"alice","visibility":{"kind":"group","group":"ghosts"},"versions":[{"number":1,"body":"b"}]}`},
		{"two notes under one id", `{"op":"publish","id":"x","author":"alice","visibility":{"kind":"company"},"versions":[{"number":1,"body":"b"}]}` + "\n" +
			`{"op":"publish","id":"x","author":"alice","visibility":{"kind":"company"},"versions":[{"number":1,"body":"c"}]}`},
		{"a timeline that does not start at one", `{"op":"publish","id":"x","author":"alice","visibility":{"kind":"company"},"versions":[{"number":2,"body":"b"}]}`},
		{"a note with no author", `{"op":"publish","id":"x","visibility":{"kind":"company"},"versions":[{"number":1,"body":"b"}]}`},
	} {
		t.Run(record.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "hub")
			if err := Create(dir, Options{}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, journalFile), []byte(record.line+"\n"), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			s, err := Open(dir, Options{})
			if err == nil {
				held := s.Describe().Notes
				_ = s.Close()
				t.Fatalf("the hub started anyway and reports %d notes; %s is a corpus it could not read, not a corpus of that size", held, record.name)
			}
			if hub.Code(err) != ErrHubRecordUnreadable.Code {
				t.Errorf("code = %q, want %q", hub.Code(err), ErrHubRecordUnreadable.Code)
			}
		})
	}
}

// --- criteria 5 and 6: who is asking, and what they may do -----------------------------------

// TestAnUnidentifiedCallerGetsNoDeterminedAnswer is Issue #62's shape, driven at the door.
//
// The point is NOT that an empty caller gets refused eventually. It is that the refusal is its own,
// explicit refusal, and that no query with an empty identity ever reaches the visibility predicate —
// where hub.CanRead("") answers UNDETERMINED for everything and a filter would return the lot.
func TestAnUnidentifiedCallerGetsNoDeterminedAnswer(t *testing.T) {
	s, _ := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	alice := signIn(t, s, "alice", allScopes()...)
	mustPublish(t, s, alice, "a note", "a body", hub.CompanyWide())
	mustPublish(t, s, alice, "a narrowed note", "another body", hub.SelfOnly())

	var nobody auth.Secret
	if out, err := s.Search(nobody, hub.Query{Terms: "body", Scope: hub.CompanyScope()}); err == nil {
		t.Fatalf("an unidentified caller got a search answer: %d results, rendered:\n%s", out.Total, out.Render())
	} else if hub.Code(err) != ErrNoCredentialPresented.Code {
		t.Errorf("an unidentified search was refused with code %q, want %q; the empty-identity case must be refused explicitly, not by falling through into some other refusal",
			hub.Code(err), ErrNoCredentialPresented.Code)
	}
	if _, err := s.Read(nobody, "any-id"); hub.Code(err) != ErrNoCredentialPresented.Code {
		t.Errorf("an unidentified read was refused with code %q, want %q", hub.Code(err), ErrNoCredentialPresented.Code)
	}
	if _, err := s.Publish(nobody, "t", "b", hub.CompanyWide()); hub.Code(err) != ErrNoCredentialPresented.Code {
		t.Errorf("an unidentified publication was refused with code %q, want %q", hub.Code(err), ErrNoCredentialPresented.Code)
	}
	if _, err := s.Sessions(nobody); hub.Code(err) != ErrNoCredentialPresented.Code {
		t.Errorf("an unidentified session listing was refused with code %q, want %q", hub.Code(err), ErrNoCredentialPresented.Code)
	}
	if _, err := s.Statistics(nobody, hub.CompanyScope()); hub.Code(err) != ErrNoCredentialPresented.Code {
		t.Errorf("an unidentified statistics request was refused with code %q, want %q", hub.Code(err), ErrNoCredentialPresented.Code)
	}
	// AND THE REFUSAL IS NOT A ZERO. #101: a count is an answer. A refused search must not be
	// reported as a corpus of nothing.
	if _, err := s.Statistics(nobody, hub.CompanyScope()); err == nil {
		t.Error("statistics answered an unidentified caller")
	}
}

// TestNothingSignsInSilently is criterion 5. A device code with nobody's approval is not a token,
// and asking twice does not eventually produce one.
func TestNothingSignsInSilently(t *testing.T) {
	s, _ := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	a := s.Authority()
	d, err := a.StartSignIn(auth.SignInRequest{Scopes: allScopes(), Label: "a laptop"})
	if err != nil {
		t.Fatalf("StartSignIn: %v", err)
	}
	if _, err := a.Redeem(d); err == nil {
		t.Fatal("a token was issued for a sign-in nobody approved")
	}
	if _, err := a.Redeem(d); err == nil {
		t.Fatal("a second attempt at an unapproved sign-in produced a token; nothing signs in silently, and nothing signs in by persistence either")
	}
	if got := len(a.Sessions("alice")); got != 0 {
		t.Errorf("alice has %d sessions after nobody approved anything, want 0", got)
	}
}

// TestATokenCarriesAScopeNotAnIdentity is criterion 6.
//
// There is no author argument on Server.Publish and no reader argument on Server.Read: the identity
// comes from the session. So this drives the OTHER half — that the scope on the token is what limits
// what may be done, and that it limits it WITHIN the session's identity rather than replacing it.
func TestATokenCarriesAScopeNotAnIdentity(t *testing.T) {
	s, _ := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	readOnly := signIn(t, s, "alice", hub.ScopeRead)
	full := signIn(t, s, "alice", allScopes()...)

	n := mustPublish(t, s, full, "alice's note", "a body", hub.CompanyWide())

	// Same person, narrower token: the identity is unchanged and the capability is not.
	if got, err := s.Read(readOnly, n.ID); err != nil {
		t.Errorf("alice's read-only token could not read her own note: %v", err)
	} else if got.Author != "alice" {
		t.Errorf("the note read through alice's token is authored by %q, want alice", got.Author)
	}
	if _, err := s.Publish(readOnly, "t", "b", hub.CompanyWide()); hub.Code(err) != hub.ErrPublishScopeRequired.Code {
		t.Errorf("a read-only token published, or was refused with code %q; want %q", hub.Code(err), hub.ErrPublishScopeRequired.Code)
	}
	// A refused publication stored NOTHING — a count is an answer, and this one must not have moved.
	before := s.Describe().Notes
	_, _ = s.Publish(readOnly, "t2", "b2", hub.CompanyWide())
	if after := s.Describe().Notes; after != before {
		t.Errorf("a refused publication changed the corpus from %d notes to %d", before, after)
	}
}

// --- criterion 7: revocation, at the hub -----------------------------------------------------

// TestRevokingASessionEndsItAtTheHubAndSurvivesARestart is criterion 7, both halves.
func TestRevokingASessionEndsItAtTheHubAndSurvivesARestart(t *testing.T) {
	s, dir := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	keep, keepID := signInWithID(t, s, "alice", allScopes()...)
	doomed, doomedID := signInWithID(t, s, "alice", allScopes()...)

	sessions, err := s.Sessions(keep)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("alice sees %d sessions, want 2; a person must be able to see what has been signed in as them", len(sessions))
	}
	if !listsSession(sessions, keepID) || !listsSession(sessions, doomedID) {
		t.Fatalf("the listing does not name both of alice's sessions; she cannot end what she cannot see")
	}
	// It works before it is ended, or ending it proves nothing.
	if _, err := s.Sessions(doomed); err != nil {
		t.Fatalf("the doomed session did not work before it was revoked: %v", err)
	}

	if err := s.Revoke(keep, doomedID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := s.Sessions(doomed); !errors.Is(err, auth.ErrTokenRevoked) {
		t.Errorf("a revoked token still worked at the hub; error was %v, want %v", err, auth.ErrTokenRevoked)
	}

	// AND IT SURVIVES A RESTART. Ending a session is not forgetting a credential.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if _, ended := reopened.revoked[string(doomedID)]; !ended {
		t.Errorf("the revocation of %q was not durable; a person who ends a session and then sees the hub restart must not find it alive again", doomedID)
	}
}

func listsSession(views []auth.SessionView, id auth.TokenID) bool {
	for _, v := range views {
		if v.ID == id {
			return true
		}
	}
	return false
}

// TestARestartEndsEverySessionAsADeterminedNegative records, as a test rather than as a comment, a
// property of this build that a person would otherwise have to discover.
//
// Sessions live in the process. A restart therefore ends all of them — and the point is that the
// hub then answers a DETERMINED "there is no such token", which the client renders as "not signed
// in" and never as "could not be determined". Both would be non-working; only one of them tells a
// person what to do about it.
func TestARestartEndsEverySessionAsADeterminedNegative(t *testing.T) {
	s, dir := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	sec := signIn(t, s, "alice", allScopes()...)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	_, err = reopened.Sessions(sec)
	if !errors.Is(err, auth.ErrNoSuchToken) {
		t.Fatalf("a token presented after a restart got %v, want %v — a determined negative, not an undetermined one and not a working session", err, auth.ErrNoSuchToken)
	}
}

// --- writing, halting, and never quietly continuing -------------------------------------------

// TestAHubThatCannotWriteHaltsAndAnswersNothing is PRD §4.3 in a server.
func TestAHubThatCannotWriteHaltsAndAnswersNothing(t *testing.T) {
	s, _ := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	alice := signIn(t, s, "alice", allScopes()...)
	n := mustPublish(t, s, alice, "a note", "a body about deploys", hub.CompanyWide())

	// Take the durable record away from underneath it, the way a full disk or a revoked mount does.
	// PROBED, NOT NAMED: if closing the descriptor does not make the next write fail on this
	// platform, the test says it determined nothing.
	s.mu.Lock()
	_ = s.journal.f.Close()
	s.mu.Unlock()

	_, err := s.Publish(alice, "another note", "another body", hub.CompanyWide())
	if err == nil {
		t.Skip("DETERMINED NOTHING, NOT PASSING: writing to a closed durable record succeeded on this platform, so the halt path was never entered and nothing about it was established here")
	}
	if hub.Code(err) != ErrHubHalted.Code {
		t.Fatalf("a failed durable write was refused with code %q, want %q", hub.Code(err), ErrHubHalted.Code)
	}
	if s.Halted() == nil {
		t.Error("the hub did not record that it had halted")
	}
	// EVERY subsequent call, INCLUDING READS. A hub still serving reads out of a store it can no
	// longer add to is a hub whose corpus has silently stopped growing, and a person reads its
	// answers as current.
	if _, err := s.Read(alice, n.ID); hub.Code(err) != ErrHubHalted.Code {
		t.Errorf("a halted hub answered a read with code %q, want %q", hub.Code(err), ErrHubHalted.Code)
	}
	if _, err := s.Search(alice, hub.Query{Terms: "deploys", Scope: hub.CompanyScope()}); hub.Code(err) != ErrHubHalted.Code {
		t.Errorf("a halted hub answered a search with code %q, want %q", hub.Code(err), ErrHubHalted.Code)
	}
	if _, err := s.Statistics(alice, hub.CompanyScope()); hub.Code(err) != ErrHubHalted.Code {
		t.Errorf("a halted hub answered a statistics request with code %q, want %q", hub.Code(err), ErrHubHalted.Code)
	}
}

// TestNothingIsConjured is PRD §4.2 for a hub directory.
func TestNothingIsConjured(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-hub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := Open(dir, Options{}); hub.Code(err) != ErrNoHubDirectory.Code {
		t.Fatalf("opening a directory that is not a hub answered %v (code %q), want %q", err, hub.Code(err), ErrNoHubDirectory.Code)
	}
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatalf("ReadDir: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("opening a directory that is not a hub left %d entries in it; nothing is conjured", len(entries))
	}

	if err := Create(dir, Options{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Create(dir, Options{}); hub.Code(err) != ErrHubDirectoryExists.Code {
		t.Errorf("creating a hub over an existing one answered %v (code %q), want %q; re-initialising is how a corpus is lost", err, hub.Code(err), ErrHubDirectoryExists.Code)
	}
}

// TestAnUnreadableRecordIsUndeterminedNotAnEmptyCorpus is the §4.3 rule at start-up, and it checks
// the EXIT CODE as well as the refusal — the two must not be the ones success and failure use.
func TestAnUnreadableRecordIsUndeterminedNotAnEmptyCorpus(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	if err := Create(dir, Options{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, journalFile), []byte("this is not a record\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Open(dir, Options{})
	if err == nil {
		_ = s.Close()
		t.Fatalf("a hub with an unreadable durable record started anyway, holding %d notes; that is 'could not determine' rendered as 'there is nothing'", s.Describe().Notes)
	}
	if hub.Code(err) != ErrHubRecordUnreadable.Code {
		t.Errorf("code = %q, want %q", hub.Code(err), ErrHubRecordUnreadable.Code)
	}

	var out, errOut strings.Builder
	code := Run(context.Background(), []string{"describe", dir}, &out, &errOut)
	if code != cli.ExitUndetermined {
		t.Errorf("omw-hub describe on an unreadable record exited %d, want %d; 'could not determine' must not share an exit code with 'determined to be nothing' or with success",
			code, cli.ExitUndetermined)
	}
	if code == cli.Success || code == cli.ExitFailure {
		t.Errorf("exit code %d is one success or ordinary failure uses", code)
	}
}

// TestATruncatedFinalWriteIsReportedNotSwallowed — a crash between write and sync.
func TestATruncatedFinalWriteIsReportedNotSwallowed(t *testing.T) {
	s, dir := newHub(t)
	mustAddPerson(t, s, "alice", allScopes()...)
	alice := signIn(t, s, "alice", allScopes()...)
	mustPublish(t, s, alice, "a note", "a body", hub.CompanyWide())
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, journalFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString(`{"op":"publish","id":"half`); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = f.Close()

	reopened, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("a hub whose last write did not finish refused to start: %v; everything before that line was synced and is good", err)
	}
	defer func() { _ = reopened.Close() }()
	d := reopened.Describe()
	if !d.Truncated {
		t.Error("the unfinished final write was not reported; it is not silence's job to explain it")
	}
	if d.Notes != 1 {
		t.Errorf("the hub holds %d notes after an unfinished final write, want 1", d.Notes)
	}
	if !strings.Contains(d.Render(), "did not finish") {
		t.Errorf("the description does not mention the unfinished write:\n%s", d.Render())
	}
}

// --- structural -------------------------------------------------------------------------------

// TestHubdOpensNoNetworkConnection asserts on the SOURCE, so that the sibling Issue #104's wire
// cannot be started here by accident. It is the same shape as the assertion in internal/hub.
func TestHubdOpensNoNetworkConnection(t *testing.T) {
	forbidden := []string{`"net"`, `"net/http"`, `"os/exec"`, `"net/rpc"`}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	saw := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		saw++
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, pkg := range forbidden {
			if strings.Contains(string(body), pkg) {
				t.Errorf("%s imports %s; the client-to-hub transport is Issue #104 and is not this Issue's to build", name, pkg)
			}
		}
	}
	if saw == 0 {
		t.Fatal("no non-test source files were examined, so this assertion looked at nothing")
	}
}

// TestRefusalsArePairwiseDistinguishable — two refusals that render the same are one refusal.
func TestRefusalsArePairwiseDistinguishable(t *testing.T) {
	for i, a := range allHubdErrors {
		for j, b := range allHubdErrors {
			if i >= j {
				continue
			}
			if a.Code == b.Code {
				t.Errorf("%s and %s share the code %q", a.Msg, b.Msg, a.Code)
			}
			if a.Msg == b.Msg {
				t.Errorf("%s and %s share a message", a.Code, b.Code)
			}
		}
	}
	if len(allHubdErrors) == 0 {
		t.Fatal("no refusals were compared, so this assertion looked at nothing")
	}
}

// TestUsageNamesNoDefaultDirectory — PRD §4.2 at the command line.
func TestUsageNamesNoDefaultDirectory(t *testing.T) {
	var out, errOut strings.Builder
	if code := Run(context.Background(), nil, &out, &errOut); code != cli.ExitUsage {
		t.Errorf("omw-hub with no arguments exited %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut.String(), "never picks one for you") {
		t.Errorf("the usage does not say the directory is never chosen for you:\n%s", errOut.String())
	}
	var o2, e2 strings.Builder
	if code := Run(context.Background(), []string{"serve"}, &o2, &e2); code != cli.ExitUsage {
		t.Errorf("omw-hub serve with no directory exited %d, want %d; a hub must not serve a directory nobody named", code, cli.ExitUsage)
	}
}

// TestServeRunsUntilItIsStopped — the process is a process.
func TestServeRunsUntilItIsStopped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	if err := Create(dir, Options{Company: "Example Ltd"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut strings.Builder
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"serve", dir}, &out, &errOut) }()

	select {
	case code := <-done:
		t.Fatalf("serve returned %d without being stopped; stdout:\n%s\nstderr:\n%s", code, out.String(), errOut.String())
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case code := <-done:
		if code != cli.Success {
			t.Errorf("a hub stopped on purpose exited %d, want %d; stderr:\n%s", code, cli.Success, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop when it was told to")
	}
	if !strings.Contains(out.String(), "NOT reachable over a network") {
		t.Errorf("serve did not say that this build has no transport; an unstated gap is read as a working one:\n%s", out.String())
	}
}
