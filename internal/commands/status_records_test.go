// Issue #101, BLOCKER 1: `omw status` said "everything you have configured is running" over a store
// holding a record it could not read, byte-identical to the all-readable control and exit 0 — while
// `omw store status`, `omw inbox list`, `omw ticket list`, `omw agent tickets` and `omw diagnostics`
// all reported the same store as undetermined at the same moment.
//
// THE TESTS HERE DRIVE THE UNREADABLE CASE AGAINST THE READABLE ONE AND REQUIRE THEM TO DIFFER, in
// output AND in exit code. A test that only asserted the unreadable case renders *something* passes
// against the build this Issue was filed on: that build rendered the reassuring answer, in full, on
// both.
//
// The unreadability is a REAL `chmod 000` on a real record file, never a missing file. Absent and
// unreadable are two different states — that conflation is the whole family of defects this Issue
// belongs to (#67, #69), so a fixture that removed the file would be testing the wrong one.
package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/status"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// aTicketRecordIn writes one real ticket and returns the path of the file on disk that holds it.
func aTicketRecordIn(t *testing.T, root string) string {
	t.Helper()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := inbox.Put(s, inbox.Ticket{
		ID: "t-one", Title: inbox.Text("about the one ticket"), Summary: inbox.Text("s"),
		Channel: inbox.Text("teams"), Arrived: time.Unix(1700000000, 0).UTC(),
	}); err != nil {
		t.Fatalf("could not write the ticket this test is about: %v", err)
	}
	path := filepath.Join(root, "records", string(inbox.Kind), "t-one.rec")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the record this test makes unreadable is not where it was expected (%s): %v", path, err)
	}
	return path
}

// makeUnreadable is product's exact condition: the file stays, and nobody can open it.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root, where chmod 000 does not deny a read — this condition cannot be driven here")
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("could not make %s unreadable: %v", path, err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skipf("%s is still readable at mode 000, so the condition under test does not exist on this filesystem", path)
	}
}

// storeLineOf reads the status screen's own word for the local store, from the screen a person sees.
func storeLineOf(t *testing.T, rendered string) string {
	t.Helper()
	word, ok := status.ParseRendered(rendered)[status.Store]
	if !ok {
		t.Fatalf("the status screen has no %q line at all:\n%s", status.Store, rendered)
	}
	return word
}

// TestStatusIsUndeterminedWhenARecordItSummarisesCannotBeRead is acceptance criterion 1.
//
// BOTH DIRECTIONS ARE DRIVEN IN ONE TEST, over one store, so the two runs cannot differ in anything
// except the record's mode. The inverse half is not decoration: a fix that reported every store as
// undetermined would satisfy the unreadable half and destroy the screen.
func TestStatusIsUndeterminedWhenARecordItSummarisesCannotBeRead(t *testing.T) {
	_, root, getenv := statusSandbox(t)
	rec := aTicketRecordIn(t, root)

	// ---- Control: every record readable. -----------------------------------
	okCode, okOut, okErr := runOMW(t, getenv, "status")
	if okCode != cli.Success {
		t.Fatalf("the control run over a fully readable store exited %d, not 0; the rest of this test would be measuring the wrong thing.\n%s%s",
			okCode, okOut, okErr)
	}
	if got := storeLineOf(t, okOut); got != "working" {
		t.Fatalf("the control run reports the local store as %q rather than working:\n%s", got, okOut)
	}

	// ---- Test: one record, chmod 000. --------------------------------------
	makeUnreadable(t, rec)
	badCode, badOut, badErr := runOMW(t, getenv, "status")

	if badCode == okCode {
		t.Errorf("criterion 1: `omw status` exited %d over a store holding an UNREADABLE record — the same code as the all-readable control.\n"+
			"  `omw status --help` states that exit 0 means every state on the screen was established. It was not.\n"+
			"  expected %d (cli.ExitUndetermined).\n%s%s", badCode, cli.ExitUndetermined, badOut, badErr)
	}
	if badCode != cli.ExitUndetermined {
		t.Errorf("criterion 1: `omw status` over an unreadable record exited %d; a state that could not be established is %d and never %d.\n%s%s",
			badCode, cli.ExitUndetermined, cli.ExitFailure, badOut, badErr)
	}
	if badOut == okOut {
		t.Errorf("criterion 1: the screen over an unreadable record is BYTE-IDENTICAL to the all-readable control.\n"+
			"  The one screen whose promise is 'whether everything runs' answered yes about data it could not open:\n%s", badOut)
	}
	if got := storeLineOf(t, badOut); got != "undetermined" {
		t.Errorf("criterion 1: the local store line says %q while one of its records could not be read; it must be undetermined.\n%s",
			got, badOut)
	}
	if strings.Contains(badOut, status.SummaryAllConfiguredWorking.String()) ||
		strings.Contains(badOut, status.SummaryAllWorking.String()) {
		t.Errorf("criterion 1: the summary still leads with everything running over a store that could not be read:\n%s", badOut)
	}
}

// TestStatusAndStoreStatusDoNotDisagreeAboutTheSameStore is acceptance criterion 2.
//
// THE TWO SURFACES ARE COMPARED TO EACH OTHER AND NOT TO A LITERAL. Two surfaces each asserted
// against their own expected string pass together while disagreeing about a third state — the
// isolated shape Issue #41 was filed about. Here the question asked of both is the same one:
// "did you establish this store's records?", and both must give the same answer in both directions.
func TestStatusAndStoreStatusDoNotDisagreeAboutTheSameStore(t *testing.T) {
	_, root, getenv := statusSandbox(t)
	rec := aTicketRecordIn(t, root)

	establishedByStatus := func() bool {
		code, out, _ := runOMW(t, getenv, "status")
		return code == cli.Success && storeLineOf(t, out) != "undetermined"
	}
	establishedByStoreStatus := func() bool {
		code, out, errOut := runOMW(t, getenv, "store", "status")
		return code == cli.Success && !strings.Contains(out+errOut, "could not be determined")
	}

	if !establishedByStatus() || !establishedByStoreStatus() {
		t.Fatalf("the control direction already disagrees or already reports undetermined over a fully readable store; "+
			"status established=%t, store status established=%t", establishedByStatus(), establishedByStoreStatus())
	}

	makeUnreadable(t, rec)
	byStatus, byStoreStatus := establishedByStatus(), establishedByStoreStatus()
	if byStatus != byStoreStatus {
		code, out, errOut := runOMW(t, getenv, "status")
		scode, sout, serr := runOMW(t, getenv, "store", "status")
		t.Errorf("criterion 2: the two surfaces disagree about ONE store with one unreadable record.\n"+
			"  `omw status` established it: %t (exit %d)\n%s%s\n"+
			"  `omw store status` established it: %t (exit %d)\n%s%s",
			byStatus, code, out, errOut, byStoreStatus, scode, sout, serr)
	}
	if byStatus {
		t.Errorf("criterion 2: both surfaces agree, and both are wrong — a record that could not be read was reported as established.")
	}
}
