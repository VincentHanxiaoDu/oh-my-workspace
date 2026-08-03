// Issue #67, BLOCKER 2: `omw diagnostics` reported zero drafts while drafts existed.
//
// The bundle read `store.Kind("draft")`, which nothing in this build writes. `omw outbox draft`
// writes revision files under `<store>/outbox/<id>/`. The reader and the writer never met, and the
// result rendered as a confident `collected (0)` — a support engineer opens that bundle and
// concludes the person has no drafts.
//
// These tests drive BOTH commands, on one store, and compare the two surfaces to EACH OTHER rather
// than to a fixed number. A test that asserted "draft-inventory says 2" would have been written
// green against a mock; a test that asserted the bundle merely mentions drafts passes against the
// build this Issue was filed on.
package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/diagnostics"
)

// runBundle produces a bundle into a fresh path and returns the manifest it wrote.
func runBundle(t *testing.T, env map[string]string, extra ...string) (diagnostics.Manifest, int, string) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "bundle")
	args := append([]string{"diagnostics", dest}, extra...)
	var out, errb bytes.Buffer
	code := cli.Run(args, &out, &errb, func(k string) string { return env[k] })
	if code != cli.Success {
		t.Fatalf("omw diagnostics exited %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	body, err := os.ReadFile(filepath.Join(dest, "manifest.json"))
	if err != nil {
		t.Fatalf("reading the manifest the bundle says it wrote: %v", err)
	}
	var man diagnostics.Manifest
	if err := json.Unmarshal(body, &man); err != nil {
		t.Fatalf("the manifest is not readable JSON: %v", err)
	}
	return man, code, dest
}

func categoryNamed(t *testing.T, man diagnostics.Manifest, name string) diagnostics.Category {
	t.Helper()
	for _, c := range man.Categories {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("the manifest names no category %q", name)
	return diagnostics.Category{}
}

// draftsReportedByOutboxList parses the count `omw outbox list` printed. It parses rather than
// assumes, because criterion 3 is an assertion about the two surfaces agreeing and a hard-coded
// number on either side would let them agree with the test and not with each other.
func draftsReportedByOutboxList(t *testing.T, env map[string]string) int {
	t.Helper()
	got := runOutboxCmd(t, env, "list")
	if got.code != cli.Success {
		t.Fatalf("omw outbox list exited %d:\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, line := range strings.Split(got.stdout, "\n") {
		rest, ok := strings.CutPrefix(line, "drafts: ")
		if !ok {
			continue
		}
		// "drafts: 0 — your outbox is empty…" and "drafts: 2" both start with the number.
		n, err := strconv.Atoi(strings.Fields(rest)[0])
		if err != nil {
			t.Fatalf("omw outbox list did not report a countable number of drafts: %q", line)
		}
		return n
	}
	t.Fatalf("omw outbox list printed no drafts line:\n%s", got.stdout)
	return -1
}

// ISSUE #67 CRITERION 3: the two surfaces report the SAME draft count, with drafts present.
func TestDiagnosticsReportsTheSameDraftCountAsTheOutbox(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "draft", "n1", "the first draft")
	mustRun(t, env, "draft", "n2", "the second draft")

	fromOutbox := draftsReportedByOutboxList(t, env)
	if fromOutbox != 2 {
		t.Fatalf("this test wrote two drafts and omw outbox list reports %d; the premise is wrong", fromOutbox)
	}

	man, _, _ := runBundle(t, env)
	inv := categoryNamed(t, man, diagnostics.CatDraftInventory)
	if inv.State != diagnostics.StateCollected {
		t.Fatalf("with two drafts present the bundle's draft inventory is %q (%s): %s",
			inv.State, inv.Reason, inv.Detail)
	}
	if inv.Items != fromOutbox {
		t.Errorf("omw outbox list reports %d drafts and the bundle's draft inventory reports %d — "+
			"the reader and the writer are looking in two different places:\n%+v", fromOutbox, inv.Items, inv)
	}
}

// The bodies half of the same defect: `--include-bodies` collected nothing to withhold or disclose.
func TestDiagnosticsDraftBodiesReachTheDraftsThatExist(t *testing.T) {
	env := obWorld(t)
	mustRun(t, env, "draft", "n1", "the first draft")
	mustRun(t, env, "draft", "n2", "the second draft")
	fromOutbox := draftsReportedByOutboxList(t, env)

	man, _, dest := runBundle(t, env, "--include-bodies")
	bodies := categoryNamed(t, man, diagnostics.CatDraftBodies)
	if bodies.State != diagnostics.StateCollected {
		t.Fatalf("with bodies asked for and two drafts present, draft-note-bodies is %q (%s): %s",
			bodies.State, bodies.Reason, bodies.Detail)
	}
	if bodies.Items != fromOutbox {
		t.Errorf("omw outbox list reports %d drafts and the bundle collected %d bodies", fromOutbox, bodies.Items)
	}
	// And the words the person actually wrote are in there, so "collected" is not a count with
	// nothing behind it.
	if len(bodies.Files) == 0 {
		t.Fatalf("draft-note-bodies is collected and names no file: %+v", bodies)
	}
	body, err := os.ReadFile(filepath.Join(dest, bodies.Files[0]))
	if err != nil {
		t.Fatalf("reading the file the manifest names: %v", err)
	}
	if !strings.Contains(string(body), "the second draft") {
		t.Errorf("the collected draft bodies do not contain the draft that was written:\n%s", body)
	}
}

// THE SAME DEFECT, THE SAME BUNDLE, ONE CATEGORY OVER (found in review of PR #92).
//
// Nothing in this build writes a raw ingested message: channel ingestion turns a message into a
// TICKET and stores no raw message. The bundle counted the kind anyway and reported
// `message-inventory  collected (0)` — a support engineer reads that and concludes the person has
// no ingested messages, on exactly the reasoning #67 was filed to refuse. A count that can only
// ever be zero is not a count.
func TestTheBundleDoesNotAssertZeroMessagesItCannotCount(t *testing.T) {
	env := obWorld(t)
	man, _, _ := runBundle(t, env, "--include-bodies")

	for _, name := range []string{diagnostics.CatMessageInventory, diagnostics.CatMessageBodies} {
		c := categoryNamed(t, man, name)
		if c.State == diagnostics.StateCollected {
			t.Errorf("%s is reported as collected (%d) although nothing in this build writes those "+
				"records; a zero nobody could have counted is an assertion, not a measurement: %+v",
				name, c.Items, c)
		}
		if c.State != diagnostics.StateUndetermined {
			t.Errorf("%s is %q, want undetermined: %+v", name, c.State, c)
		}
		if c.Reason != diagnostics.ReasonNotInThisBuild {
			t.Errorf("%s gives reason %q, which does not say this build has nothing that writes them", name, c.Reason)
		}
		if !strings.Contains(c.Detail, "NOT a report that there are none") {
			t.Errorf("%s does not say it is not a report that there are none: %q", name, c.Detail)
		}
	}

	// AND IT IS STILL DISTINGUISHABLE FROM A REAL EMPTINESS. Tickets are written by the inbox, so a
	// store with no tickets is a determined zero and must not have been swept into the same answer.
	tickets := categoryNamed(t, man, diagnostics.CatTicketInventory)
	if tickets.State != diagnostics.StateCollected || tickets.Items != 0 {
		t.Errorf("a store with no tickets is no longer a determined zero: %+v", tickets)
	}
}

// ISSUE #67 CRITERION 4, AND THE ASSERTION THAT MAKES THIS SUITE WORTH RUNNING.
//
// "There are no drafts" and "the drafts could not be enumerated" must produce DIFFERENT manifest
// entries — different state, and a machine-readable reason on the one that could not be read. A
// test that only asserted the unreadable case produces *something* passes against a build that
// renders both as `collected (0)`, which is precisely how this shipped.
func TestAnEmptyOutboxAndAnUnreadableOneAreDifferentAnswersInTheBundle(t *testing.T) {
	// Determined empty: a store whose outbox holds no drafts.
	emptyEnv := obWorld(t)
	if n := draftsReportedByOutboxList(t, emptyEnv); n != 0 {
		t.Fatalf("a fresh store reports %d drafts; the premise is wrong", n)
	}
	emptyMan, _, _ := runBundle(t, emptyEnv)
	empty := categoryNamed(t, emptyMan, diagnostics.CatDraftInventory)

	// Undetermined: an outbox that is there and will not be read.
	brokenEnv := obWorld(t)
	mustRun(t, brokenEnv, "draft", "n1", "a draft that will become unreadable")
	outbox := filepath.Join(obStorePath(t, brokenEnv), "outbox")
	if err := os.Chmod(outbox, 0o000); err != nil {
		t.Skipf("this filesystem will not make the outbox unreadable: %v", err)
	}
	t.Cleanup(func() { os.Chmod(outbox, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("running as root: an unreadable directory is still readable, so this case cannot be driven")
	}
	brokenMan, _, _ := runBundle(t, brokenEnv)
	broken := categoryNamed(t, brokenMan, diagnostics.CatDraftInventory)

	// THE TWO MUST DIFFER. This is the whole test.
	if empty.State == broken.State && empty.Reason == broken.Reason {
		t.Fatalf("an empty outbox and an unreadable one produce the same manifest entry (%q/%q):\nempty:  %+v\nbroken: %+v",
			empty.State, empty.Reason, empty, broken)
	}
	if empty.State != diagnostics.StateCollected {
		t.Errorf("a determined-empty outbox is not reported as collected: %+v", empty)
	}
	if empty.Items != 0 {
		t.Errorf("a determined-empty outbox reports %d drafts", empty.Items)
	}
	if broken.State != diagnostics.StateUndetermined {
		t.Errorf("an outbox that could not be read is reported as %q, want undetermined: %+v", broken.State, broken)
	}
	if broken.Reason == diagnostics.ReasonNone {
		t.Errorf("the undetermined draft inventory carries no machine-readable reason: %+v", broken)
	}
	if broken.Detail == "" {
		t.Errorf("the undetermined draft inventory carries no sentence a person can read: %+v", broken)
	}

	// AND THE SAME SPLIT IS VISIBLE IN THE EXIT CODES A SCRIPT READS — not from `omw diagnostics`,
	// which reports an ACT and exits on whether a bundle was produced, but from the surface that
	// answers the question: `omw outbox list` says 0 and succeeds, or says nothing was established
	// and exits 3.
	emptyList := runOutboxCmd(t, emptyEnv, "list")
	brokenList := runOutboxCmd(t, brokenEnv, "list")
	if emptyList.code == brokenList.code {
		t.Errorf("an empty outbox and an unreadable one both exit %d from omw outbox list", emptyList.code)
	}
	if emptyList.code != cli.Success {
		t.Errorf("a determined-empty outbox exits %d from omw outbox list", emptyList.code)
	}
	if brokenList.code != cli.ExitUndetermined {
		t.Errorf("an unreadable outbox exits %d from omw outbox list, want ExitUndetermined (%d)",
			brokenList.code, cli.ExitUndetermined)
	}
	if emptyList.stdout == brokenList.stdout {
		t.Errorf("an empty outbox and an unreadable one print the same listing:\n%s", emptyList.stdout)
	}
}
