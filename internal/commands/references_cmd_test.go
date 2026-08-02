package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// runRefs drives the real command through the real registry and returns what a person would see.
// Named apart from visibility_cmd_test.go's helpers because both files are this one package.
func runRefs(t *testing.T, vars map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"references"}, args...), &out, &errb, func(k string) string { return vars[k] })
	return out.String(), errb.String(), code
}

func withRefStore(t *testing.T, s *hub.Store) {
	t.Helper()
	prev := referencesSource
	referencesSource = func(cli.Env) (*hub.Store, error) { return s, nil }
	t.Cleanup(func() { referencesSource = prev })
}

func withRefDaemon(t *testing.T) {
	t.Helper()
	prev := daemonRunning
	daemonRunning = func(cli.Env) bool { return true }
	t.Cleanup(func() { daemonRunning = prev })
}

func refHubConfigured() map[string]string { return map[string]string{envHub: "hub.example.internal"} }

// refCorpus is the same shape as the hub package's: alice is in group platform, bo is not, and
// there is a note narrowed to platform that bo must never learn about.
type refCorpus struct {
	store  *hub.Store
	secret hub.NoteID
}

func newRefCorpus(t *testing.T) *refCorpus {
	t.Helper()
	rec := hub.NewRecord()
	rec.DefineGroup("platform", "alice")
	rec.AddPerson("bo")
	s := hub.NewStore(rec)
	group, err := hub.ParseChoice("group:platform")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := hub.PublishWithReferences(s, hub.Publication{
		Author: "alice", Title: "the auth migration", Body: "how it went", Visibility: group,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &refCorpus{store: s, secret: secret.ID}
}

func (c *refCorpus) publish(t *testing.T, title, body string) hub.NoteID {
	t.Helper()
	n, err := hub.PublishWithReferences(c.store, hub.Publication{Author: "alice", Title: title, Body: body})
	if err != nil {
		t.Fatalf("publishing %q: %v", title, err)
	}
	return n.ID
}

// CRITERION 7, THROUGH THE CLI: two readers, the same note, and bo's output must be
// indistinguishable from the output for a note that references nothing restricted.
func TestCLIReferenceToAnUnreadableNoteIsInvisible(t *testing.T) {
	c := newRefCorpus(t)
	withRefStore(t, c.store)
	withRefDaemon(t)
	withRef := c.publish(t, "the rewrite", "the background is in [[note:"+string(c.secret)+"]] and in the wiki.")
	control := c.publish(t, "the rewrite, again", "the background is in and in the wiki.")

	bosOut, bosErr, bosCode := runRefs(t, refHubConfigured(), "of", string(withRef), "--as", "bo")
	ctlOut, ctlErr, ctlCode := runRefs(t, refHubConfigured(), "of", string(control), "--as", "bo")

	// The note ids differ, so compare everything after the first line, which is the only place
	// they legitimately differ.
	if after(bosOut) != after(ctlOut) {
		t.Errorf("bo's output for the referencing note is\n%s\nand for the control note is\n%s\n"+
			"nothing may distinguish them", after(bosOut), after(ctlOut))
	}
	if bosErr != ctlErr {
		t.Errorf("stderr differs: %q vs %q — a per-reference error is a disclosure too", bosErr, ctlErr)
	}
	if bosCode != ctlCode {
		t.Errorf("exit codes differ: %d vs %d", bosCode, ctlCode)
	}
	if !strings.Contains(bosOut, "references: 0") {
		t.Errorf("bo's count is not zero:\n%s", bosOut)
	}
	for _, leak := range []string{string(c.secret), "the auth migration", "restricted", "unresolved", "undetermined"} {
		if strings.Contains(bosOut+bosErr, leak) {
			t.Errorf("bo's output mentions %q:\n%s\n%s", leak, bosOut, bosErr)
		}
	}

	// alice sees it, so the reference really is there.
	alicesOut, _, alicesCode := runRefs(t, refHubConfigured(), "of", string(withRef), "--as", "alice")
	if alicesCode != cli.Success || !strings.Contains(alicesOut, string(c.secret)) {
		t.Errorf("alice's output does not carry the reference she may follow (code %d):\n%s", alicesCode, alicesOut)
	}
	if !strings.Contains(alicesOut, "references: 1") {
		t.Errorf("alice's count is not one:\n%s", alicesOut)
	}
}

func after(s string) string {
	_, rest, _ := strings.Cut(s, "\n")
	return rest
}

// CRITERION 9, THROUGH THE CLI: an invisible target and a nonexistent one answer identically —
// same stdout, same stderr, same exit code.
func TestCLIInvisibleAndMissingTargetsAnswerIdentically(t *testing.T) {
	c := newRefCorpus(t)
	withRefStore(t, c.store)
	withRefDaemon(t)
	c.publish(t, "some note", "unrelated prose")

	invOut, invErr, invCode := runRefs(t, refHubConfigured(), "to", "note:"+string(c.secret), "--as", "bo")
	misOut, misErr, misCode := runRefs(t, refHubConfigured(), "to", "note:note-does-not-exist", "--as", "bo")

	// Only the echoed target may differ; everything else must not.
	if after(invOut) != after(misOut) {
		t.Errorf("the two answers differ after the target line:\n%q\n%q", after(invOut), after(misOut))
	}
	if invErr != misErr || invCode != misCode {
		t.Errorf("stderr or exit code tells the two apart: %q/%d vs %q/%d", invErr, invCode, misErr, misCode)
	}
	if !strings.HasPrefix(invOut, "notes referencing note "+string(c.secret)+": 0") {
		t.Errorf("the invisible target's answer is not an empty count:\n%s", invOut)
	}
}

// CRITERION 6 through the CLI: what else was written about this.
func TestCLIWhatElseWasWrittenAboutThis(t *testing.T) {
	c := newRefCorpus(t)
	withRefStore(t, c.store)
	withRefDaemon(t)
	subject := c.publish(t, "the subject", "the original")
	c.publish(t, "the follow-up", "builds on [[note:"+string(subject)+"]]")

	out, _, code := runRefs(t, refHubConfigured(), "to", "note:"+string(subject), "--as", "bo")
	if code != cli.Success {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "the follow-up") || !strings.Contains(out, ": 1") {
		t.Errorf("the reverse question was not answered:\n%s", out)
	}
}

// CRITERION 14 through the CLI: a hub that could not be reached renders undetermined, with its own
// exit code — never "there are no references".
func TestCLIUnreachableHubIsUndetermined(t *testing.T) {
	c := newRefCorpus(t)
	withRefDaemon(t)
	n := c.publish(t, "a note", "see [[note:note-1]]")
	// referencesSource is NOT replaced: the build's default reports the hub unreachable.
	out, errOut, code := runRefs(t, refHubConfigured(), "of", string(n), "--as", "bo")
	if code != cli.ExitUndetermined {
		t.Errorf("exit %d, want %d — could not determine and determined to be nothing must never share\n"+
			"an exit code", code, cli.ExitUndetermined)
	}
	if !strings.Contains(out, "could not be determined") {
		t.Errorf("stdout does not say the answer is undetermined:\n%s", out)
	}
	if strings.Contains(out, "references: 0") {
		t.Errorf("an unreachable hub was rendered as an absence of references:\n%s", out)
	}
	if !strings.Contains(errOut, hub.ErrHubUnreachable.Code) {
		t.Errorf("stderr does not carry the code:\n%s", errOut)
	}
}

// CRITERION 15: no command starts the daemon, and with no daemon the command says so.
func TestCLISaysTheDaemonIsNotRunningAndDoesNotStartIt(t *testing.T) {
	c := newRefCorpus(t)
	withRefStore(t, c.store)
	// daemonRunning is NOT replaced. It PROBES the socket path the environment names — here, none.
	_, errOut, code := runRefs(t, refHubConfigured(), "of", "note-1", "--as", "bo")
	if code != cli.ExitFailure {
		t.Errorf("exit %d, want %d", code, cli.ExitFailure)
	}
	if !strings.Contains(errOut, hub.ErrDaemonNotRunning.Code) {
		t.Errorf("stderr does not say the daemon is not running:\n%s", errOut)
	}
}

// The daemon probe PROBES rather than naming a platform's convention: a socket path that exists
// answers yes and one that does not answers no, whatever this machine is.
func TestTheDaemonProbeIsAProbe(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "omw.sock")
	e := cli.Env{Getenv: func(k string) string {
		if k == envSocket {
			return sock
		}
		return ""
	}}
	if daemonRunning(e) {
		t.Fatal("the probe answered yes for a path that does not exist")
	}
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !daemonRunning(e) {
		t.Error("the probe answered no for a path that exists; it is naming something rather than probing it")
	}
}

// CRITERION 15 and 16: with no hub configured, the command says so, names the hub as the missing
// piece, and reaches nothing.
func TestCLINoHubConfiguredSaysSoPrecisely(t *testing.T) {
	_, errOut, code := runRefs(t, map[string]string{}, "of", "note-1", "--as", "bo")
	if code != cli.ExitFailure {
		t.Errorf("exit %d, want %d", code, cli.ExitFailure)
	}
	if !strings.Contains(errOut, hub.ErrNoHubConfigured.Code) {
		t.Errorf("stderr does not carry the code:\n%s", errOut)
	}
	if !strings.Contains(errOut, "hub") {
		t.Errorf("stderr does not name the hub as the missing piece:\n%s", errOut)
	}
}

// CRITERION 16: the local half stands alone, and a PARTIAL answer is distinguishable from a
// complete one BY EXIT STATUS ALONE.
func TestCLIScanIsCompleteOrSaysItIsPartial(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.md")
	withRefs := filepath.Join(dir, "refs.md")
	if err := os.WriteFile(plain, []byte("a draft that references nothing at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(withRefs, []byte("ask [[person:alice]] about [[note:note-3]]"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, code := runRefs(t, map[string]string{}, "scan", plain)
	if code != cli.Success {
		t.Errorf("a draft with no references is a COMPLETE answer; exit %d:\n%s", code, out)
	}

	out2, err2, code2 := runRefs(t, map[string]string{}, "scan", withRefs)
	if code2 != cli.ExitUndetermined {
		t.Errorf("a draft whose references need the hub is PARTIAL; exit %d, want %d", code2, cli.ExitUndetermined)
	}
	if code2 == code {
		t.Error("a partial answer and a complete one share an exit code, so they are not distinguishable\n" +
			"by exit status alone")
	}
	// The local half worked FULLY: both references were extracted, with their kinds.
	for _, want := range []string{"person", "alice", "note", "note-3", "references: 2"} {
		if !strings.Contains(out2, want) {
			t.Errorf("the local extraction is missing %q:\n%s", want, out2)
		}
	}
	if !strings.Contains(err2, "hub") {
		t.Errorf("the partial answer does not name the hub as the missing piece:\n%s", err2)
	}
	// It claims nothing about the targets — neither "no such note" nor "unresolved".
	for _, forbidden := range []string{"unresolved", "no such note"} {
		if strings.Contains(strings.ToLower(out2+err2), forbidden) {
			t.Errorf("with no hub, the command claimed %q about a target it never asked about", forbidden)
		}
	}
}

// The syntax help is local and reaches nothing, and it shows three states that read differently.
func TestCLISyntaxIsLocalAndShowsThreeDistinctStates(t *testing.T) {
	out, errOut, code := runRefs(t, map[string]string{}, "syntax")
	if code != cli.Success {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	lines := map[string]bool{}
	for _, st := range []hub.RefState{hub.StateResolved, hub.StateUnresolved, hub.StateUndetermined} {
		r := hub.RenderReference(hub.Reference{Kind: hub.RefNote, Target: "note-9"}, st)
		if !strings.Contains(out, r) {
			t.Errorf("the syntax help does not show the %s rendering %q:\n%s", st, r, out)
		}
		if lines[r] {
			t.Errorf("two states render identically as %q", r)
		}
		lines[r] = true
	}
}

// CRITERION 11 through the CLI: a dangling reference is shown, marked, and does not take the rest
// of the listing with it.
func TestCLIDanglingReferenceIsShownAndDoesNotBreakTheListing(t *testing.T) {
	c := newRefCorpus(t)
	withRefStore(t, c.store)
	withRefDaemon(t)
	other := c.publish(t, "a readable note", "prose")
	n := c.publish(t, "with a dangling reference", "gone [[note:note-404]] and here [[note:"+string(other)+"]]")

	out, _, code := runRefs(t, refHubConfigured(), "of", string(n), "--as", "bo")
	if code != cli.Success {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "references: 2") {
		t.Errorf("the dangling reference broke the listing:\n%s", out)
	}
	if !strings.Contains(out, "unresolved") {
		t.Errorf("the dangling reference is not marked:\n%s", out)
	}
	if !strings.Contains(out, string(other)) {
		t.Errorf("the other reference vanished with it:\n%s", out)
	}
}

// A version can be asked for, and the answer is that version's references (criterion 3).
func TestCLIAsksForAVersion(t *testing.T) {
	c := newRefCorpus(t)
	withRefStore(t, c.store)
	withRefDaemon(t)
	a := c.publish(t, "a", "a")
	b := c.publish(t, "b", "b")
	n := c.publish(t, "history", "v1 has [[note:"+string(a)+"]]")
	if _, err := hub.AmendWithReferences(c.store, n, "alice", "v2 has [[note:"+string(b)+"]]"); err != nil {
		t.Fatal(err)
	}
	out, _, code := runRefs(t, refHubConfigured(), "of", string(n), "--as", "bo", "--version", "1")
	if code != cli.Success {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, string(a)) || strings.Contains(out, string(b)) {
		t.Errorf("version 1's listing is not version 1's references:\n%s", out)
	}
}
