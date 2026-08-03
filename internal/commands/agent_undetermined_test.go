// Issue #101, BLOCKER 2: the agent API rendered material it could not read as a determined answer.
//
// Three parts, driven separately here because a fix verified on one and assumed for the other two
// is how this shipped:
//
//	(a) an unreadable draft (`chmod 000 outbox/<id>/.state`) was served as `drafted`, exit 0, while
//	    `omw outbox list` correctly exited 3 and said it could not be read
//	(b) an unreadable revision (`chmod 000 outbox/<id>/000001.body`) became `(0 revision(s))` — a
//	    DETERMINED ZERO about a revision nobody could read, #67's exact shape in the surface an AI
//	    consumes
//	(c) the `tickets:` / `drafts:` / `notes:` count line printed unconditionally, so a count sat
//	    beside `outcome: undetermined`
//
// EVERY TEST BELOW DRIVES BOTH DIRECTIONS over one store, one daemon and one outbox, differing in
// nothing but a file's mode. The unreadability is a real chmod on a real file that stays where it
// is: absent and unreadable are different states, and conflating them is the defect itself.
//
// THEY GO THROUGH THE REAL BINARY AND THE REAL CONTROL API, as the rest of this Issue's surface
// does — an in-process call to agentapi.Answer would prove the logic and nothing about what a
// person's AI actually receives.
package commands

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/agentapi"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// agentDraftFixture is a live machine: a store, a daemon, an outbox with one draft that has one
// revision and a recorded state, and a read grant.
//
// The outbox is at <store>/outbox — the daemon's default and the one `omw outbox list` reads — so
// the two surfaces this test compares are looking at the SAME directory. (The three-outboxes
// finding recorded on Issue #101 is about `omw stats` seeding a different one; it is not this
// blocker and is not fixed here.)
func agentDraftFixture(t *testing.T) (bin, root, outboxDir, grant string) {
	t.Helper()
	bin = buildOMW(t)
	root = storeThatExists(t)
	outboxDir = filepath.Join(root, daemon.DefaultOutboxDir)
	o, err := drafts.Create(outboxDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Revise(hub.NoteID("d2"), "the one revision of d2"); err != nil {
		t.Fatal(err)
	}
	if err := o.SetState(hub.NoteID("d2"), drafts.StateDrafted, ""); err != nil {
		t.Fatal(err)
	}
	start := runBinary(t, bin, root, "daemon", "start")
	if start.code != 0 {
		t.Fatalf("`omw daemon start` exited %d\n%s%s", start.code, start.stdout, start.stderr)
	}
	t.Cleanup(func() { runBinary(t, bin, root, "daemon", "stop") })
	return bin, root, outboxDir, issueGrantViaBinary(t, bin, root, "read")
}

// TestAnUnreadableDraftIsNotServedAsDrafted is acceptance criterion 3, part (a).
func TestAnUnreadableDraftIsNotServedAsDrafted(t *testing.T) {
	bin, root, outboxDir, grant := agentDraftFixture(t)
	stateFile := filepath.Join(outboxDir, "d2", ".state")

	// ---- Control: the state file is readable. ------------------------------
	okRun := runBinary(t, bin, root, "agent", "drafts", "--grant", grant)
	if okRun.code != cli.Success {
		t.Fatalf("the control run over a readable draft exited %d, not 0\n%s%s", okRun.code, okRun.stdout, okRun.stderr)
	}
	if !strings.Contains(okRun.stdout, string(drafts.StateDrafted)) {
		t.Fatalf("the control run does not report the draft as drafted, so the comparison below would be vacuous:\n%s", okRun.stdout)
	}

	// ---- Test: chmod 000 on the draft's state record. ----------------------
	makeUnreadable(t, stateFile)
	badRun := runBinary(t, bin, root, "agent", "drafts", "--grant", grant)

	if badRun.code == okRun.code {
		t.Errorf("criterion 3: `omw agent drafts` exited %d over a draft whose state record CANNOT BE READ — the same code as the readable control.\n"+
			"  `omw outbox list` exits %d on this same draft and says it could not be read.\n%s%s",
			badRun.code, cli.ExitUndetermined, badRun.stdout, badRun.stderr)
	}
	if badRun.code != cli.ExitUndetermined {
		t.Errorf("criterion 3: `omw agent drafts` exited %d over an unreadable draft; it must be %d.\n%s%s",
			badRun.code, cli.ExitUndetermined, badRun.stdout, badRun.stderr)
	}
	if badRun.stdout == okRun.stdout {
		t.Errorf("criterion 3: the answer over an unreadable draft is identical to the readable control:\n%s", badRun.stdout)
	}
	if strings.Contains(badRun.stdout, string(drafts.StateDrafted)) {
		t.Errorf("criterion 3: a draft whose state record could not be read is still served as %q.\n"+
			"  An agent grounding itself on this acts on a draft whose state nobody established (#16: no more, no less).\n%s",
			drafts.StateDrafted, badRun.stdout)
	}

	// AND THE TWO SURFACES AGREE. `omw outbox list` is the surface that already got this right;
	// the comparison is against what IT says rather than against a literal in this file.
	list := runBinary(t, bin, root, "outbox", "list")
	if list.code != badRun.code {
		t.Errorf("criterion 3: `omw outbox list` exits %d and `omw agent drafts` exits %d about the SAME unreadable draft.\n"+
			"  outbox list:\n%s%s\n  agent drafts:\n%s%s",
			list.code, badRun.code, list.stdout, list.stderr, badRun.stdout, badRun.stderr)
	}
}

// TestAnUnreadableRevisionIsNeverCountedAsZero is acceptance criterion 4, part (b).
//
// THE ASSERTION IS ABOUT A DETERMINED ZERO, not about the wording. `(0 revision(s))` is a claim
// that the count was established and came to nothing; the draft has one revision that nobody could
// read. `--json` is checked too, because that is the form an agent parses.
func TestAnUnreadableRevisionIsNeverCountedAsZero(t *testing.T) {
	bin, root, outboxDir, grant := agentDraftFixture(t)
	revision := filepath.Join(outboxDir, "d2", "000001.body")

	okRun := runBinary(t, bin, root, "agent", "drafts", "--grant", grant, "--json")
	if okRun.code != cli.Success {
		t.Fatalf("the control run exited %d, not 0\n%s%s", okRun.code, okRun.stdout, okRun.stderr)
	}
	okResp := decodeAgentJSON(t, okRun.stdout)
	if len(okResp.Drafts) != 1 || okResp.Drafts[0].Revisions == nil || *okResp.Drafts[0].Revisions != 1 {
		t.Fatalf("the control run does not report the one revision this draft has, so the comparison would be vacuous:\n%s", okRun.stdout)
	}

	makeUnreadable(t, revision)
	badRun := runBinary(t, bin, root, "agent", "drafts", "--grant", grant, "--json")
	badResp := decodeAgentJSON(t, badRun.stdout)

	if len(badResp.Drafts) == 1 && badResp.Drafts[0].Revisions != nil && *badResp.Drafts[0].Revisions == 0 {
		t.Errorf("criterion 4: a revision that COULD NOT BE READ is served as a determined count of 0.\n"+
			"  `(0 revision(s))` must be reachable only when the count is genuinely established.\n%s", badRun.stdout)
	}
	if badRun.code == okRun.code {
		t.Errorf("criterion 4: `omw agent drafts` exited %d over an unreadable revision — the same code as the readable control.\n%s%s",
			badRun.code, badRun.stdout, badRun.stderr)
	}
	if badRun.code != cli.ExitUndetermined {
		t.Errorf("criterion 4: `omw agent drafts` exited %d over an unreadable revision; it must be %d.\n%s%s",
			badRun.code, cli.ExitUndetermined, badRun.stdout, badRun.stderr)
	}

	// And what a person reads must not carry the determined zero either.
	human := runBinary(t, bin, root, "agent", "drafts", "--grant", grant)
	if strings.Contains(human.stdout, "(0 revision(s))") {
		t.Errorf("criterion 4: the human rendering prints `(0 revision(s))` for a draft with one unreadable revision:\n%s", human.stdout)
	}
}

// TestNoCountLineIsPrintedForMaterialNobodyRead is acceptance criterion 5, part (c), and the
// structural half of criterion 6 for this surface.
//
// The undetermined outcome is produced the way product produced it: one ticket record at chmod 000,
// so `omw agent tickets` cannot establish the inventory. The count line then either must not be
// printed at all or must say it could not be determined — what it must never be is a NUMBER beside
// `outcome: undetermined`, which is the same line that means "3 tickets" on success.
func TestNoCountLineIsPrintedForMaterialNobodyRead(t *testing.T) {
	bin := buildOMW(t)
	root := storeThatExists(t)
	rec := aTicketRecordIn(t, root)

	start := runBinary(t, bin, root, "daemon", "start")
	if start.code != 0 {
		t.Fatalf("`omw daemon start` exited %d\n%s%s", start.code, start.stdout, start.stderr)
	}
	t.Cleanup(func() { runBinary(t, bin, root, "daemon", "stop") })
	grant := issueGrantViaBinary(t, bin, root, "read")

	okRun := runBinary(t, bin, root, "agent", "tickets", "--grant", grant)
	if okRun.code != cli.Success || !strings.Contains(okRun.stdout, "tickets:") {
		t.Fatalf("the control run exited %d and did not print a tickets count line\n%s%s", okRun.code, okRun.stdout, okRun.stderr)
	}

	makeUnreadable(t, rec)
	badRun := runBinary(t, bin, root, "agent", "tickets", "--grant", grant)

	if badRun.code != cli.ExitUndetermined {
		t.Fatalf("the unreadable run exited %d rather than %d, so the rendering under test was not reached\n%s%s",
			badRun.code, cli.ExitUndetermined, badRun.stdout, badRun.stderr)
	}
	if !strings.Contains(badRun.stdout, string(agentapi.OutcomeUndetermined)) {
		t.Fatalf("the unreadable run did not report an undetermined outcome, so this test is measuring the wrong branch:\n%s", badRun.stdout)
	}
	for _, line := range strings.Split(badRun.stdout, "\n") {
		if !strings.HasPrefix(line, "tickets:") {
			continue
		}
		if strings.ContainsAny(line, "0123456789") {
			t.Errorf("criterion 5: a COUNT is printed beside `outcome: undetermined`:\n  %s\n"+
				"  That is the same line that means \"3 tickets\" on success, said about material nobody read.\n%s",
				strings.TrimSpace(line), badRun.stdout)
		}
	}
	if badRun.stdout == okRun.stdout {
		t.Errorf("criterion 5: the undetermined rendering is identical to the successful control:\n%s", badRun.stdout)
	}
}
