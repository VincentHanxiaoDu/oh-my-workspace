package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// runDeparted drives the real command through the real registry and returns what a person would see.
func runDep(t *testing.T, vars map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"departed"}, args...), &out, &errb, env(vars))
	return out.String(), errb.String(), code
}

// withDepartedStore points the command at an in-memory hub for one test.
func withDepartedStore(t *testing.T, s *hub.Store) {
	t.Helper()
	prev := departedSource
	departedSource = func(cli.Env) (*hub.Store, error) { return s, nil }
	t.Cleanup(func() { departedSource = prev })
}

// departedFixture is the CLI's corpus: priya publishes company-wide and self-only, ravi publishes
// company-wide and refers to priya's note.
type departedFixture struct {
	store *hub.Store
	arch  *hub.Archive
	wide  hub.NoteID
	self  hub.NoteID
	ravi  hub.NoteID
	refs  *hub.RefIndex
}

func newDepartedFixture(t *testing.T) *departedFixture {
	t.Helper()
	rec := hub.NewRecord()
	rec.AddPerson("priya")
	rec.AddPerson("ravi")
	s := hub.NewStore(rec)
	arch := hub.NewArchive()
	s.SetPeopleStatus(arch)
	f := &departedFixture{store: s, arch: arch, refs: hub.NewRefIndex()}

	pub := func(author hub.PersonID, title string, v hub.Visibility) hub.NoteID {
		t.Helper()
		n, err := s.Publish(hub.Publication{Author: author, Title: title,
			Body: "the billing reconciliation job runs twice: " + title, Visibility: v})
		if err != nil {
			t.Fatalf("Publish %s: %v", title, err)
		}
		return n.ID
	}
	f.wide = pub("priya", "reconciliation", hub.CompanyWide())
	f.self = pub("priya", "her own notes", hub.SelfOnly())
	f.ravi = pub("ravi", "ravi's note", hub.CompanyWide())
	f.refs.Link(f.ravi, f.wide)

	withDepartedStore(t, s)
	prev := departedRefsIndex
	departedRefsIndex = func(cli.Env, *hub.Store) *hub.RefIndex { return f.refs }
	t.Cleanup(func() { departedRefsIndex = prev })
	return f
}

// -------------------------------------------------------------------------------------------
// Criterion 21 and criterion 20 — the local half stands alone, and it reaches for nothing
// -------------------------------------------------------------------------------------------

// TestDepartedWithNoHubConfiguredReachesForNothing is criterion 20, asserted LIVE rather than by an
// import ban: the one function that would talk to a hub fails the test if it is called at all.
//
// The AST guard in network_guard_test.go asserts that every listen and dial in the tree names
// "unix". This asserts the complementary thing that guard cannot: that with no hub configured this
// capability does not even reach the code that would connect.
func TestDepartedWithNoHubConfiguredReachesForNothing(t *testing.T) {
	prev := departedSource
	departedSource = func(cli.Env) (*hub.Store, error) {
		t.Errorf("criterion 20: with no hub configured, this capability reached for one anyway")
		return nil, hub.ErrHubUnreachable
	}
	t.Cleanup(func() { departedSource = prev })

	prevLive := daemonLiveness
	daemonLiveness = func(cli.Env) (tri.Value, string) {
		t.Errorf("criterion 20/21: with no hub configured, the daemon was probed before the machine's " +
			"own configuration had been reported")
		return tri.Yes, ""
	}
	t.Cleanup(func() { daemonLiveness = prevLive })

	for _, args := range [][]string{
		{"notes", "--by", "priya"},
		{"show", "note-1"},
		{"versions", "note-1"},
		{"refs", "note-1"},
		{"corpus"},
	} {
		if _, _, code := runDep(t, map[string]string{}, args...); code != cli.ExitFailure {
			t.Errorf("omw departed %v with no hub exited %d, want %d", args, code, cli.ExitFailure)
		}
	}
}

// TestNoHubAndAGenuineZeroAreDifferentAnswers is criterion 21's sharpest clause: "A no-hub answer
// and a genuine zero-results answer must not be the same output."
func TestNoHubAndAGenuineZeroAreDifferentAnswers(t *testing.T) {
	f := newDepartedFixture(t)
	_ = f
	withDaemon(t)

	noHubOut, noHubErr, noHubCode := runDep(t, map[string]string{}, "notes", "--by", "nobody")
	zeroOut, zeroErr, zeroCode := runDep(t, hubConfigured(t), "notes", "--by", "nobody", "--as", "ravi")

	if noHubOut == zeroOut && noHubErr == zeroErr {
		t.Fatalf("criterion 21: the no-hub answer and the genuine-zero answer are the same output:\n%s%s", noHubOut, noHubErr)
	}
	if noHubCode == zeroCode {
		t.Errorf("criterion 21: both answers exit %d; a no-hub answer must exit distinguishably from success", noHubCode)
	}
	if zeroCode != cli.Success {
		t.Errorf("a genuine zero is a determined answer and must succeed; exited %d\nstdout:\n%s\nstderr:\n%s", zeroCode, zeroOut, zeroErr)
	}
	// The no-hub answer says what is missing, and says it is not a zero.
	if !strings.Contains(noHubErr, hub.ErrNoHubConfigured.Code) {
		t.Errorf("criterion 21: the no-hub answer does not carry the code %q:\n%s", hub.ErrNoHubConfigured.Code, noHubErr)
	}
	if !strings.Contains(noHubErr, "hub capability") {
		t.Errorf("criterion 21: the no-hub answer does not say precisely what is missing:\n%s", noHubErr)
	}
	if !strings.Contains(strings.ToLower(noHubErr), "not a report that there are no such notes") {
		t.Errorf("criterion 21: the no-hub answer does not disclaim being an empty result:\n%s", noHubErr)
	}
	// The genuine zero says the hub was asked.
	if !strings.Contains(zeroOut, "the hub was asked") {
		t.Errorf("criterion 21: a genuine zero does not say the hub was asked:\n%s", zeroOut)
	}
}

// TestNoHubUnreachableHubAndSuccessAreThreeDifferentExits is criterion 21's "exits distinguishably
// from both success and from hub-configured-but-unreachable".
func TestNoHubUnreachableHubAndSuccessAreThreeDifferentExits(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)

	_, _, noHub := runDep(t, map[string]string{}, "show", string(f.wide))

	prev := departedSource
	departedSource = func(cli.Env) (*hub.Store, error) { return nil, hub.ErrHubUnreachable }
	unreachOut, unreachErr, unreach := runDep(t, hubConfigured(t), "show", string(f.wide))
	departedSource = prev

	okOut, _, success := runDep(t, hubConfigured(t), "show", string(f.wide), "--as", "ravi")

	codes := map[int]string{noHub: "no hub", unreach: "unreachable", success: "success"}
	if len(codes) != 3 {
		t.Errorf("criterion 21: no-hub=%d unreachable=%d success=%d — three answers, three exit codes",
			noHub, unreach, success)
	}
	if unreach != cli.ExitUndetermined {
		t.Errorf("an unreachable hub is undetermined and must exit %d; exited %d", cli.ExitUndetermined, unreach)
	}
	if !strings.Contains(unreachOut+unreachErr, hub.ErrHubUnreachable.Code) {
		t.Errorf("the unreachable answer does not carry its code:\n%s%s", unreachOut, unreachErr)
	}
	if okOut == unreachOut {
		t.Errorf("criterion 21: success and unreachable print the same thing:\n%s", okOut)
	}
}

// TestTheDaemonIsSaidNotStarted is criterion 19.
func TestTheDaemonIsSaidNotStarted(t *testing.T) {
	f := newDepartedFixture(t)
	vars := hubConfigured(t)

	// hubConfigured names a store whose daemon has never run, so liveness is a determined "no".
	notRunningOut, notRunningErr, notRunningCode := runDep(t, vars, "show", string(f.wide), "--as", "ravi")
	if notRunningCode != cli.ExitFailure {
		t.Fatalf("criterion 19: with the daemon not running the command exited %d, want %d\nstdout:%s\nstderr:%s",
			notRunningCode, cli.ExitFailure, notRunningOut, notRunningErr)
	}
	if !strings.Contains(notRunningErr, hub.ErrDaemonNotRunning.Code) {
		t.Errorf("criterion 19: the answer does not carry %q:\n%s", hub.ErrDaemonNotRunning.Code, notRunningErr)
	}

	withDaemon(t)
	runningOut, runningErr, runningCode := runDep(t, vars, "show", string(f.wide), "--as", "ravi")
	if runningCode != cli.Success {
		t.Fatalf("with the daemon running the command exited %d\nstdout:%s\nstderr:%s", runningCode, runningOut, runningErr)
	}
	if runningOut == notRunningOut && runningErr == notRunningErr {
		t.Error("criterion 19: the daemon-running and daemon-not-running cases are indistinguishable in output")
	}
	// And nothing started it: the answer for the not-running case never claims to have.
	if strings.Contains(strings.ToLower(notRunningErr), "starting") {
		t.Errorf("criterion 19: the command offered to start the daemon:\n%s", notRunningErr)
	}
}

// -------------------------------------------------------------------------------------------
// The journey, through the CLI
// -------------------------------------------------------------------------------------------

// TestTheSameQueryFindsTheSameNotesAfterTheAuthorLeaves is criteria 4 and 6 at the surface.
func TestTheSameQueryFindsTheSameNotesAfterTheAuthorLeaves(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)

	beforeOut, _, beforeCode := runDep(t, vars, "notes", "--by", "priya", "--as", "ravi")
	f.arch.Deactivate("priya")
	afterOut, _, afterCode := runDep(t, vars, "notes", "--by", "priya", "--as", "ravi")

	if beforeCode != cli.Success || afterCode != cli.Success {
		t.Fatalf("exit codes: before=%d after=%d", beforeCode, afterCode)
	}
	if !strings.Contains(beforeOut, string(f.wide)) {
		t.Fatalf("fixture: ravi should see priya's company-wide note before the departure:\n%s", beforeOut)
	}
	if !strings.Contains(afterOut, string(f.wide)) {
		t.Errorf("criteria 4 and 6: ravi could see %s before the deactivation and cannot after:\n%s",
			f.wide, afterOut)
	}
	if strings.Contains(afterOut, string(f.self)) {
		t.Errorf("criterion 5: ravi can now see priya's self-only note:\n%s", afterOut)
	}
	if !strings.Contains(afterOut, "priya") {
		t.Errorf("criterion 9: the listing no longer names the author:\n%s", afterOut)
	}
	if !strings.Contains(afterOut, hub.RetentionLine) {
		t.Errorf("§5.4: the departed listing does not say nothing expires:\n%s", afterOut)
	}
}

// TestTheThreeAuthorStatesAreThreeDifferentOutputs is criteria 12 and 18 driven through the real
// command, compared PAIRWISE against each other rather than against string literals.
func TestTheThreeAuthorStatesAreThreeDifferentOutputs(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)

	active, _, activeCode := runDep(t, vars, "show", string(f.wide), "--as", "ravi")

	f.arch.Deactivate("priya")
	departed, _, departedCode := runDep(t, vars, "show", string(f.wide), "--as", "ravi")

	// A SECOND, INDEPENDENT FIXTURE for the undetermined case, so that the three are three states
	// of the same note rather than three stages of one mutation.
	g := newDepartedFixture(t)
	g.arch.MarkUnreadable("priya")
	undet, undetErr, undetCode := runDep(t, vars, "show", string(g.wide), "--as", "ravi")

	outs := map[string]string{"active": active, "deactivated": departed, "undetermined": undet}
	for name, o := range outs {
		if strings.TrimSpace(o) == "" {
			t.Errorf("criterion 12: the %q rendering is blank", name)
		}
		if !strings.Contains(o, "priya") {
			t.Errorf("criteria 10 and 18: the %q rendering does not name the author:\n%s", name, o)
		}
	}
	for a, oa := range outs {
		for b, ob := range outs {
			if a < b && oa == ob {
				t.Errorf("criteria 12 and 18: the %q and %q outputs are identical:\n%s", a, b, oa)
			}
		}
	}
	// `could not determine` and `determined to be nothing` never share an exit code.
	if undetCode == activeCode || undetCode == departedCode {
		t.Errorf("criterion 18: undetermined exits %d, the same as active (%d) or deactivated (%d)",
			undetCode, activeCode, departedCode)
	}
	if undetCode != cli.ExitUndetermined {
		t.Errorf("an undetermined author state must exit %d; exited %d", cli.ExitUndetermined, undetCode)
	}
	if !strings.Contains(undetErr, hub.ErrPersonStateUndetermined.Code) {
		t.Errorf("criterion 18: the undetermined answer does not carry its code:\n%s", undetErr)
	}
	// And it is distinct from silence: it does not merely omit the state.
	if !strings.Contains(undet, "could not be determined") {
		t.Errorf("criterion 18: the undetermined author state rendered as an absence:\n%s", undet)
	}
}

// TestAnArchivedNoteIsNeverAnErrorAtTheSurface is criterion 11 at the CLI, and it also checks that
// a refusal for visibility remains a different, distinguishable answer.
func TestAnArchivedNoteIsNeverAnErrorAtTheSurface(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)
	f.arch.Deactivate("priya")

	out, errOut, code := runDep(t, vars, "show", string(f.wide), "--as", "ravi")
	if code != cli.Success {
		t.Errorf("criterion 11: an archived note exited %d\nstdout:%s\nstderr:%s", code, out, errOut)
	}
	if !strings.Contains(out, "body:") || strings.Contains(out, "body:\n\n") {
		t.Errorf("criterion 11: an archived note came back with no body:\n%s", out)
	}

	refusedOut, refusedErr, refusedCode := runDep(t, vars, "show", string(f.self), "--as", "ravi")
	if refusedCode == code {
		t.Errorf("criterion 11: a refused note and an archived note share an exit code (%d)", code)
	}
	if !strings.Contains(refusedErr, hub.ErrRefused.Code) {
		t.Errorf("criterion 11: the refusal does not carry %q:\n%s", hub.ErrRefused.Code, refusedErr)
	}
	if refusedOut == out {
		t.Error("criterion 11: a refusal and an archived note print the same thing")
	}
}

// TestEveryVersionShowsTheSameDepartedAuthorAtTheSurface is criterion 13 at the CLI.
func TestEveryVersionShowsTheSameDepartedAuthorAtTheSurface(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)
	if _, err := f.store.Amend(f.wide, "second version"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Amend(f.wide, "third version"); err != nil {
		t.Fatal(err)
	}
	f.arch.Deactivate("priya")

	out, _, code := runDep(t, vars, "versions", string(f.wide), "--as", "ravi")
	if code != cli.Success {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	// Every "author:" line in the output must be the same line. Compared to each other, so an
	// edited wording cannot pass by matching a literal that was edited with it.
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "author:") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("criterion 13: expected an attribution on each of the three versions, found %d:\n%s", len(lines), out)
	}
	for i, l := range lines {
		if l != lines[0] {
			t.Errorf("criterion 13: version 1 says %q and version %d says %q", lines[0], i+1, l)
		}
	}
}

// TestReferencesToAnArchivedNoteStillResolveAtTheSurface is criterion 3 at the CLI.
func TestReferencesToAnArchivedNoteStillResolveAtTheSurface(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)

	beforeFwd, _, _ := runDep(t, vars, "refs", string(f.ravi), "--as", "ravi")
	beforeBack, _, _ := runDep(t, vars, "refs", string(f.wide), "--as", "ravi")
	if !strings.Contains(beforeFwd, "refers to: 1") {
		t.Fatalf("fixture: ravi's note should refer to one note:\n%s", beforeFwd)
	}
	if !strings.Contains(beforeBack, "referred to by: 1") {
		t.Fatalf("fixture: priya's note should be referred to by one note:\n%s", beforeBack)
	}

	f.arch.Deactivate("priya")

	fwd, _, fwdCode := runDep(t, vars, "refs", string(f.ravi), "--as", "ravi")
	back, _, backCode := runDep(t, vars, "refs", string(f.wide), "--as", "ravi")
	if fwdCode != cli.Success || backCode != cli.Success {
		t.Errorf("criterion 3: refs exited %d/%d after the departure", fwdCode, backCode)
	}
	if !strings.Contains(fwd, "refers to: 1") {
		t.Errorf("criterion 3: the reference to the archived note stopped resolving:\n%s", fwd)
	}
	if !strings.Contains(back, "referred to by: 1") {
		t.Errorf("criterion 3: the archived note stopped listing what referenced it:\n%s", back)
	}
	if !strings.Contains(fwd, "priya") {
		t.Errorf("criterion 3: the resolved archived note is no longer attributed:\n%s", fwd)
	}
}

// TestCorpusStatisticsAtTheSurfaceCountArchivedNotesTheReaderMayRead is criterion 8 at the CLI.
func TestCorpusStatisticsAtTheSurfaceCountArchivedNotesTheReaderMayRead(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)
	f.arch.Deactivate("priya")

	ravi, _, code := runDep(t, vars, "corpus", "--as", "ravi")
	if code != cli.Success {
		t.Fatalf("exited %d:\n%s", code, ravi)
	}
	// ravi can read two notes: priya's company-wide (archived) and his own.
	if !strings.Contains(ravi, "notes you can read: 2") {
		t.Errorf("criterion 8: ravi's corpus is not 2:\n%s", ravi)
	}
	if !strings.Contains(ravi, "written by someone who has left: 1") {
		t.Errorf("criterion 8: ravi's archived count is not 1 — priya's self-only note must not be counted:\n%s", ravi)
	}
	priya, _, _ := runDep(t, vars, "corpus", "--as", "priya")
	if !strings.Contains(priya, "notes you can read: 3") {
		t.Errorf("criterion 8: priya can read all three notes:\n%s", priya)
	}
}
