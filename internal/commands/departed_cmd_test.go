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
	store  *hub.Store
	roster *hub.Roster
	wide   hub.NoteID
	self   hub.NoteID
	ravi   hub.NoteID
}

func newDepartedFixture(t *testing.T) *departedFixture {
	t.Helper()
	return newDepartedFixtureKnowing(t, "priya", "ravi")
}

// newDepartedFixtureKnowing builds the same corpus with a roster that has heard of only the named
// people. A person the roster never heard of is undetermined — the real path, not a stub.
func newDepartedFixtureKnowing(t *testing.T, known ...hub.PersonID) *departedFixture {
	t.Helper()
	rec := hub.NewRecord()
	rec.AddPerson("priya")
	rec.AddPerson("ravi")
	s := hub.NewStore(rec)
	roster := hub.NewRoster()
	for _, p := range known {
		roster.Register(p)
	}
	f := &departedFixture{store: s, roster: roster}

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

	// Attached last: everything above was published while its author was employed.
	s.SetRoster(roster)
	withDepartedStore(t, s)
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

	noHubOut, noHubErr, noHub := runDep(t, map[string]string{}, "show", string(f.wide), "--as", "ravi")

	prev := departedSource
	departedSource = func(cli.Env) (*hub.Store, error) { return nil, hub.ErrHubUnreachable }
	unreachOut, unreachErr, unreach := runDep(t, hubConfigured(t), "show", string(f.wide), "--as", "ravi")
	departedSource = prev

	okOut, _, success := runDep(t, hubConfigured(t), "show", string(f.wide), "--as", "ravi")

	// The fourth answer, which is the one this branch was refused for missing: nobody is asking.
	unidOut, unidErr, unid := runDep(t, hubConfigured(t), "show", string(f.wide))

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

	// FOUR ANSWERS, FOUR OUTPUTS, compared pairwise. No-hub and not-signed-in share an exit code —
	// both are determined failures of the request, and the project's rule is about `could not
	// determine` never sharing a code with a negative, which holds — but they must not read alike,
	// because the person's next action differs: configure a hub, or say who you are.
	answers := map[string]string{
		"no hub":        noHubOut + noHubErr,
		"unreachable":   unreachOut + unreachErr,
		"success":       okOut,
		"not signed in": unidOut + unidErr,
	}
	for a, oa := range answers {
		for b, ob := range answers {
			if a < b && oa == ob {
				t.Errorf("criterion 21: the %q and %q answers are the same output:\n%s", a, b, oa)
			}
		}
	}
	if unid == cli.Success || unid == unreach {
		t.Errorf("an unidentified request exited %d, which is success (%d) or undetermined (%d)",
			unid, success, unreach)
	}
	// It must not borrow the no-hub sentence: a hub IS configured here.
	if strings.Contains(unidErr, "hub capability") {
		t.Errorf("the not-signed-in answer reuses the no-hub wording, which is a different fact:\n%s", unidErr)
	}
	if strings.Contains(unidOut+unidErr, "the hub was asked") {
		t.Errorf("the not-signed-in answer claims the hub was asked:\n%s%s", unidOut, unidErr)
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
	f.roster.Deactivate("priya")
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

	f.roster.Deactivate("priya")
	departed, _, departedCode := runDep(t, vars, "show", string(f.wide), "--as", "ravi")

	// A SECOND, INDEPENDENT FIXTURE for the undetermined case, so that the three are three states
	// of the same note rather than three stages of one mutation.
	g := newDepartedFixtureKnowing(t, "ravi")
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
	f.roster.Deactivate("priya")

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
	f.roster.Deactivate("priya")

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

// CRITERION 3 IS NOT DRIVEN AT THIS COMMAND ANY MORE, and that is the fix rather than a gap.
// `omw departed refs` was this branch's own reference surface and it did not gate the note whose
// edges it served. Issue #14's `omw references of` / `omw references to` already do, so the
// duplicate is gone. References-survive-a-departure is driven in
// internal/hub/departed_test.go against #14's own OutboundReferences and ReferencesTo.

// TestCorpusStatisticsAtTheSurfaceCountArchivedNotesTheReaderMayRead is criterion 8 at the CLI.
func TestCorpusStatisticsAtTheSurfaceCountArchivedNotesTheReaderMayRead(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)
	f.roster.Deactivate("priya")

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

// -------------------------------------------------------------------------------------------
// The enumeration oracle stays closed
// -------------------------------------------------------------------------------------------

// TestAnUnidentifiedRequestDiscloses NoNoteIds is the defect that refused the first revision of this
// branch, kept as a standing test with the control that makes it load-bearing.
//
// THE DEFECT. `--as` was optional. An empty reader makes CanRead answer Undetermined for every note
// — correctly, because "you did not say who you are" is not a determined refusal — so
// Store.ListReadable put every note in the hub into its undetermined return and this command
// printed those ids one per line. `omw departed notes --by anybody` therefore dumped the whole id
// space, including notes narrowed to one person, needing no identity and not even a real person.
//
// THE CONTROL IS WHY THIS TEST MEANS ANYTHING. The same scan runs against `--as ravi`, a real
// reader, which must also emit no ids — and against the old behaviour the scan is capable of
// firing, which is what the mutation table records. A test that only checked the happy `--as` path
// would have passed throughout the defect's life.
func TestAnUnidentifiedRequestDisclosesNoNoteIds(t *testing.T) {
	f := newDepartedFixture(t)
	withDaemon(t)
	vars := hubConfigured(t)
	f.roster.Deactivate("priya")

	// Every note id in the hub, so the scan looks for the real things rather than for a prefix.
	var ids []string
	for _, id := range f.store.IDs() {
		ids = append(ids, string(id))
	}
	if len(ids) != 3 {
		t.Fatalf("fixture: expected three notes, got %d", len(ids))
	}
	leaked := func(streams ...string) []string {
		var found []string
		for _, s := range streams {
			for _, id := range ids {
				if strings.Contains(s, id) {
					found = append(found, id)
				}
			}
		}
		return found
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"a real person, no identity", []string{"notes", "--by", "priya"}},
		{"a person who does not exist", []string{"notes", "--by", "nobody-at-all"}},
		{"corpus, no identity", []string{"corpus"}},
	} {
		out, errOut, code := runDep(t, vars, tc.args...)
		if got := leaked(out, errOut); len(got) != 0 {
			t.Errorf("%s: an unidentified request disclosed %d note id(s): %v\n"+
				"  Note ids are unguessable so that the id space cannot be walked; handing them to a\n"+
				"  caller who never said who they are hands the space over directly.\n"+
				"stdout:\n%s\nstderr:\n%s", tc.name, len(got), got, out, errOut)
		}
		if code != cli.ExitFailure {
			t.Errorf("%s: exited %d, want %d — an unidentified request is refused, not answered",
				tc.name, code, cli.ExitFailure)
		}
		if !strings.Contains(errOut, hub.ErrNotSignedIn.Code) {
			t.Errorf("%s: the refusal does not carry %q:\n%s", tc.name, hub.ErrNotSignedIn.Code, errOut)
		}
		// It must not read as an empty result either.
		if strings.Contains(errOut, "no such notes") && !strings.Contains(errOut, "this is not a report") {
			t.Errorf("%s: the refusal reads as a determined absence:\n%s", tc.name, errOut)
		}
	}

	// THE CONTROL: a real reader, a successful answer, and still no ids the reader was not shown.
	out, errOut, code := runDep(t, vars, "notes", "--by", "priya", "--as", "ravi")
	if code != cli.Success {
		t.Fatalf("control: a signed-in reader exited %d\nstdout:%s\nstderr:%s", code, out, errOut)
	}
	// ravi may read exactly one of priya's notes, so that id legitimately appears on stdout.
	if !strings.Contains(out, string(f.wide)) {
		t.Errorf("control: ravi should be shown the note he may read:\n%s", out)
	}
	// The two he may NOT read must appear nowhere, on either stream.
	for _, hidden := range []hub.NoteID{f.self} {
		if strings.Contains(out+errOut, string(hidden)) {
			t.Errorf("a signed-in reader was shown the id of a note they may not read (%s):\n%s%s",
				hidden, out, errOut)
		}
	}
}

// TestAnUndeterminedReadabilityIsReportedAsACountAndNotAsIds is the same rule one level down: even
// for a reader who IS identified, the ids of notes they have not been shown are not theirs.
func TestAnUndeterminedReadabilityIsReportedAsACountAndNotAsIds(t *testing.T) {
	rec := hub.NewRecord()
	rec.DefineGroup("billing", "priya")
	rec.AddPerson("ravi")
	s := hub.NewStore(rec)
	g, err := hub.ToGroup("billing")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.Publish(hub.Publication{Author: "priya", Title: "grouped", Body: "b", Visibility: g})
	if err != nil {
		t.Fatal(err)
	}
	// Dissolving the group makes this note's readability genuinely undetermined for ravi — a real
	// sequence of events, which is the only kind worth asserting against.
	rec.Dissolve("billing")
	if _, rerr := s.Read(n.ID, "ravi"); hub.Code(rerr) != hub.ErrUndetermined.Code {
		t.Fatalf("fixture: expected an undetermined read, got %v", rerr)
	}

	withDepartedStore(t, s)
	withDaemon(t)
	out, errOut, code := runDep(t, hubConfigured(t), "corpus", "--as", "ravi")

	if code != cli.ExitUndetermined {
		t.Errorf("an undetermined note must exit %d; exited %d", cli.ExitUndetermined, code)
	}
	if !strings.Contains(out+errOut, "1") {
		t.Errorf("the undetermined note is not counted anywhere:\n%s%s", out, errOut)
	}
	if strings.Contains(out+errOut, string(n.ID)) {
		t.Errorf("the id of a note whose readability could not be determined was printed:\n%s%s\n"+
			"  A note you have not been shown is a note whose id is not yours either.", out, errOut)
	}
}
