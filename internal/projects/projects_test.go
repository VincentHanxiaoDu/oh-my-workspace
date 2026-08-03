package projects_test

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// newStore makes a store of this test's own, under t.TempDir().
//
// It calls store.Create directly rather than spawning `omw store create`, so nothing in this suite
// can reach the developer's real device pointer: there is no process to inherit an environment. The
// store is NOT registered as the device's store (no store.AsDeviceStore), which is the other half of
// the same care.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(path, store.AcceptUndeterminedLocation())
	if err != nil {
		t.Fatalf("creating a store at %s: %v", path, err)
	}
	return s
}

// noEnv is an environment with nothing in it — notably no hub and no depth override.
func noEnv(string) string { return "" }

// The three liveness answers, as this package now receives them.
//
// THEY ARE FIXTURES AND NOT PROBES. Since Issue #41 this package does not establish liveness — it
// is told, by the one probe in internal/commands that wraps daemon.Inspect. So a test here supplies
// the answer directly, and the test that a REAL daemon produces the right answer lives beside that
// probe, where a real daemon can be started. Both halves exist; neither is in the wrong place.
var (
	nothingWatching = projects.Liveness{Running: tri.No}
	daemonWatching  = projects.Liveness{Running: tri.Yes}
	livenessUnknown = projects.Liveness{Running: tri.Undetermined, Detail: "the lock could not be opened"}
)

func pathsIn(snap projects.Snapshot) []string {
	out := make([]string, 0, len(snap.Entries))
	for _, e := range snap.Entries {
		out = append(out, e.Project.Path)
	}
	return out
}

func entryFor(t *testing.T, snap projects.Snapshot, path string) projects.Entry {
	t.Helper()
	for _, e := range snap.Entries {
		if e.Project.Path == path {
			return e
		}
	}
	t.Fatalf("no entry for %s in the listing; it has %v", path, pathsIn(snap))
	return projects.Entry{}
}

// CRITERION 1: a directory added appears in a subsequent listing, and adding it twice does not
// produce two entries.
func TestAddingADirectoryTwiceIsOneProject(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()

	first, err := projects.Add(s, dir)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// The same directory written three more ways. All four are one project, because the id is
	// derived from the cleaned absolute path and not from the characters the person typed.
	for _, variant := range []string{dir, dir + "/", filepath.Join(dir, ".")} {
		if _, err := projects.Add(s, variant); err != nil {
			t.Fatalf("re-adding as %q: %v", variant, err)
		}
	}

	list, err := projects.List(s)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("%d entries after adding the same directory four ways, want 1: %+v", len(list), list)
	}
	if list[0].Path != first.Path {
		t.Errorf("listed path %s, want %s", list[0].Path, first.Path)
	}
	if !list[0].AddedAt.Equal(first.AddedAt) {
		t.Errorf("re-adding reset AddedAt (%v -> %v); the second add rewrote a record it should "+
			"have left alone", first.AddedAt, list[0].AddedAt)
	}
}

// CRITERION 13: a path that is not an existing directory is refused, and the refusal reaches the
// caller as a value it can test — not as prose.
func TestAFileOrAnAbsentPathIsRefused(t *testing.T) {
	s := newStore(t)
	tmp := t.TempDir()
	file := filepath.Join(tmp, "a-file")
	mkfile(t, file, "x")

	for _, bad := range []string{file, filepath.Join(tmp, "not-there")} {
		if _, err := projects.Add(s, bad); !errors.Is(err, projects.ErrNotAProject) {
			t.Errorf("adding %s returned %v, want ErrNotAProject", bad, err)
		}
	}
	if list, _ := projects.List(s); len(list) != 0 {
		t.Errorf("a refused add still registered something: %+v", list)
	}
}

// CRITERION 3: after removal the project is gone from the listing and the directory is untouched.
func TestRemovingAProjectLeavesTheDirectoryAlone(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "important.txt"), "the person's work")
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatal(err)
	}

	removed, err := projects.Remove(s, dir)
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	if list, _ := projects.List(s); len(list) != 0 {
		t.Errorf("still listed after removal: %+v", list)
	}
	b, err := os.ReadFile(filepath.Join(dir, "important.txt"))
	if err != nil || string(b) != "the person's work" {
		t.Errorf("the directory on disk was affected by removing the project: %q, %v", b, err)
	}

	// Removing something that was never a project is not an error, and says so by returning false.
	again, err := projects.Remove(s, dir)
	if err != nil || again {
		t.Errorf("removing an unregistered project: removed=%v err=%v, want false, nil", again, err)
	}
}

// CRITERION 2 and 8: a project whose directory is missing is still listed, marked, and does not take
// the other projects down with it.
func TestAMissingDirectoryIsMarkedAndTheOthersSurvive(t *testing.T) {
	s := newStore(t)
	present := t.TempDir()
	mkfile(t, filepath.Join(present, "a.txt"), "x")
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{present, gone} {
		if _, err := projects.Add(s, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	snap, err := projects.Take(s, noEnv, time.Now().UTC(), nothingWatching)
	if err != nil {
		t.Fatalf("the listing failed because one directory was missing: %v", err)
	}
	if len(snap.Entries) != 2 {
		t.Fatalf("%d entries, want 2 — a missing project was DROPPED: %v", len(snap.Entries), pathsIn(snap))
	}
	if got := entryFor(t, snap, gone).State.Present; got != tri.No {
		t.Errorf("the missing project's Present is %s, want no", got)
	}
	if got := entryFor(t, snap, present).State.Files; got != 1 {
		t.Errorf("the surviving project reports %d files, want 1 — "+
			"the missing one affected it", got)
	}
}

// CRITERIA 9, 10 and 20: MISSING, EXISTS-BUT-CANNOT-BE-READ and EXISTS-AND-EMPTY are three distinct
// renderings, and a real value is a fourth.
//
// COMPARED PAIRWISE, NOT AGAINST LITERALS. Four assertions of the form
// `if got != "MISSING — ..."` all stay green after somebody edits two of the four phrases to say the
// same thing, because each still matches its own literal. Only comparing them to each other fails
// then, which is the failure the criteria are actually about ("no two of the three print the same
// thing"). Constructed all at once inside ONE listing, as criterion 20 asks.
func TestTheFourOutcomesAreFourDistinctRenderings(t *testing.T) {
	if !unreadableDirsWork(t) {
		t.Skip("this environment reads a 0000 directory anyway; the unreadable case cannot be built here")
	}
	s := newStore(t)
	base := t.TempDir()

	missing := filepath.Join(base, "missing")
	unreadable := filepath.Join(base, "unreadable")
	empty := filepath.Join(base, "empty")
	full := filepath.Join(base, "full")
	for _, d := range []string{missing, unreadable, empty, full} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := projects.Add(s, d); err != nil {
			t.Fatal(err)
		}
	}
	mkfile(t, filepath.Join(full, "a.txt"), "x")
	mkfile(t, filepath.Join(unreadable, "a.txt"), "x")
	if err := os.RemoveAll(missing); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o755) })

	snap, err := projects.Take(s, noEnv, time.Now().UTC(), nothingWatching)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if len(snap.Entries) != 4 {
		t.Fatalf("%d entries, want 4 — one of the four was omitted: %v", len(snap.Entries), pathsIn(snap))
	}

	rendered := map[string]string{}
	for _, name := range []string{"missing", "unreadable", "empty", "full"} {
		path := filepath.Join(base, name)
		r := projects.DescribeState(entryFor(t, snap, path).State)
		if strings.TrimSpace(r) == "" {
			t.Errorf("%s rendered as silence; none of the outcomes may be silence", name)
		}
		rendered[name] = r
	}

	names := []string{"missing", "unreadable", "empty", "full"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if rendered[a] == rendered[b] {
				t.Errorf("%s and %s render IDENTICALLY as %q — criteria 9, 10 and 20 require "+
					"no two of these to print the same thing", a, b, rendered[a])
			}
		}
	}

	// The specific collapse criterion 9 names by hand: a count beside a directory that is not there.
	if strings.Contains(rendered["missing"], "0 file") {
		t.Errorf("the missing directory renders with a file count (%q), which is criterion 9's "+
			"named failure", rendered["missing"])
	}
	// And criterion 10's: undetermined rendered as a negative.
	if !strings.Contains(rendered["unreadable"], "could not be determined") {
		t.Errorf("the unreadable directory does not carry the product's fixed undetermined "+
			"wording: %q", rendered["unreadable"])
	}
}

// CRITERION 6, the heart of the Issue: every entry states its provenance, IN THE OUTPUT.
//
// The assertion is on the rendered listing and not on the struct, because the criterion is about
// what a person can tell from the listing alone. A correct Provenance field that Render never prints
// fails criterion 6 while every struct-level test passes.
func TestEveryListedProjectStatesWhereItsStateCameFrom(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 3; i++ {
		if _, err := projects.Add(s, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := projects.Take(s, noEnv, time.Now().UTC(), nothingWatching)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := projects.Render(&sb, snap); err != nil {
		t.Fatal(err)
	}
	out := sb.String()

	stamps := strings.Count(out, "state from:")
	if stamps != len(snap.Entries) {
		t.Errorf("%d projects listed but %d provenance statements in the output.\n"+
			"  Criterion 6: EVERY listing states whether the state shown came from the daemon's "+
			"polling or from this command examining the directories.\n%s", len(snap.Entries), stamps, out)
	}
	if strings.Contains(out, "PROVENANCE NOT RECORDED") {
		t.Errorf("an entry reached the output with no provenance recorded:\n%s", out)
	}
}

// CRITERION 7: the same command over the same projects reports the two provenances in the two
// situations, and the outputs differ.
//
// The two situations are built by supplying the two liveness ANSWERS, because since Issue #41 this
// package is told the answer rather than working one out. That a real daemon produces the right
// answer is driven in internal/commands, against a daemon started by the real binary. Nothing here
// is inferred from timing: the assertion is that the two OUTPUTS differ and each names its own case.
func TestTheListingReportsBothProvenancesAndTheOutputsDiffer(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "a.txt"), "x")
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	// Daemon stopped.
	stoppedSnap, err := projects.Take(s, noEnv, now, nothingWatching)
	if err != nil {
		t.Fatal(err)
	}
	var stopped strings.Builder
	if err := projects.Render(&stopped, stoppedSnap); err != nil {
		t.Fatal(err)
	}

	// Daemon running, and it has polled.
	if err := projects.Poll(s, noEnv, now); err != nil {
		t.Fatal(err)
	}
	runningSnap, err := projects.Take(s, noEnv, now, daemonWatching)
	if err != nil {
		t.Fatal(err)
	}
	var running strings.Builder
	if err := projects.Render(&running, runningSnap); err != nil {
		t.Fatal(err)
	}

	if got := entryFor(t, stoppedSnap, dir).Provenance; got != projects.ExaminedNow {
		t.Errorf("with nothing watching, provenance is %v, want ExaminedNow", got)
	}
	if got := entryFor(t, runningSnap, dir).Provenance; got != projects.DaemonPolled {
		t.Errorf("with the daemon watching, provenance is %v, want DaemonPolled", got)
	}
	if stopped.String() == running.String() {
		t.Fatalf("the same command over the same project produced IDENTICAL output with the "+
			"daemon stopped and with it running. The two situations are indistinguishable from "+
			"the listing, which is criterion 6 failed.\n%s", stopped.String())
	}
	// And each names its own case, so the difference is the provenance rather than incidental.
	if !strings.Contains(stopped.String(), projects.ExaminedNow.String()) {
		t.Errorf("the stopped-daemon listing does not say the directories were examined now:\n%s", stopped.String())
	}
	if !strings.Contains(running.String(), projects.DaemonPolled.String()) {
		t.Errorf("the running-daemon listing does not say the state came from the daemon:\n%s", running.String())
	}
}

// CRITERION 4: with the daemon running, a change is reflected WITHOUT ANY COMMAND being run.
//
// The daemon here is projects.Run in a goroutine, which is precisely the contract Issue #2's daemon
// has with this package. The state is read from the STORE, not from a listing, so nothing in the
// assertion path could be what caused the state to advance — "reflecting a change only when a
// listing command is run is a failure of this criterion".
func TestWithTheDaemonRunningAChangeIsReflectedWithoutACommand(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runUntil(s, stop) }()
	defer func() { close(stop); <-done }()

	waitFor(t, 5*time.Second, func() bool { return storedFiles(t, s, dir) == 0 }, "the first poll")

	mkfile(t, filepath.Join(dir, "new.txt"), "x")

	// A read at t≈0 is ALLOWED to see the old state — the criterion says so — so this does not
	// assert on it. It waits past the poll interval, running nothing, and requires the new state.
	waitFor(t, 5*time.Second, func() bool { return storedFiles(t, s, dir) == 1 },
		"the change to be reflected without any command being run")
}

// CRITERION 5: with no daemon running, NOTHING watches between commands.
//
// Driven exactly as the Issue words it: stop the daemon, change a file, wait well beyond the poll
// interval, run nothing, and assert no state anywhere advanced. Then show that the state advances
// when — and only when — a listing is run.
func TestWithNoDaemonRunningNothingAdvancesBetweenCommands(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatal(err)
	}

	// A daemon that ran and stopped, so there IS a state to advance and the test is not passing
	// merely because nothing was ever there.
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runUntil(s, stop) }()
	waitFor(t, 5*time.Second, func() bool { return storedFiles(t, s, dir) == 0 }, "the first poll")
	close(stop)
	<-done

	before := wholeStore(t, s)
	mkfile(t, filepath.Join(dir, "new.txt"), "x")
	time.Sleep(4 * projects.PollInterval) // well beyond the interval

	after := wholeStore(t, s)
	if len(before) != len(after) {
		t.Fatalf("the store gained or lost records with nothing running: %d -> %d", len(before), len(after))
	}
	for name, was := range before {
		if after[name] != was {
			t.Errorf("%s changed with no daemon running and no command run.\n  was: %s\n  now: %s\n"+
				"  Something is watching between commands, which criterion 5 forbids.",
				name, was, after[name])
		}
	}

	// And the state DOES advance when a listing is run — otherwise the assertion above would pass
	// on a build where nothing works at all.
	snap, err := projects.Take(s, noEnv, time.Now().UTC(), nothingWatching)
	if err != nil {
		t.Fatal(err)
	}
	e := entryFor(t, snap, dir)
	if e.State.Files != 1 {
		t.Errorf("the listing reports %d files, want 1 — the state did not advance even when a "+
			"listing was run", e.State.Files)
	}
	if e.Provenance != projects.ExaminedNow {
		t.Errorf("provenance %v after the daemon stopped, want ExaminedNow", e.Provenance)
	}
}

// CRITERION 11 AS A PROPERTY OF THIS PACKAGE: no read path writes anything a daemon would write.
//
// It used to assert that no heartbeat appeared, which was circular — the heartbeat was this
// package's own invention. Since Issue #41 liveness lives in the store's daemon lock, so the honest
// package-level question is whether a listing leaves the store byte-for-byte as it found it. The
// behavioural half — that `omw daemon status` still says "not running" after project commands —
// is driven in internal/commands, where a real daemon and the real probe both exist.
func TestNoReadPathWritesAnythingToTheStore(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatal(err)
	}
	before := wholeStore(t, s)

	for i := 0; i < 3; i++ {
		if _, err := projects.Take(s, noEnv, time.Now().UTC(), nothingWatching); err != nil {
			t.Fatal(err)
		}
		if _, err := projects.List(s); err != nil {
			t.Fatal(err)
		}
	}

	after := wholeStore(t, s)
	if len(before) != len(after) {
		t.Fatalf("listing changed how many records the store holds: %d -> %d", len(before), len(after))
	}
	for name, was := range before {
		if after[name] != was {
			t.Errorf("%s was rewritten by a listing.\n  was: %s\n  now: %s\n"+
				"  A read path that writes is a read path that can start something.", name, was, after[name])
		}
	}

	// A CONTROL: a poll DOES write, so the comparison above is capable of noticing a write at all.
	if err := projects.Poll(s, noEnv, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if len(wholeStore(t, s)) == len(before) {
		t.Fatal("a poll changed no records, so the comparison above would not have noticed a " +
			"listing that wrote one either — its pass said nothing")
	}
}

// CRITERION 12, and the second half of criterion 11: no hub, no network.
//
// STRUCTURAL, because the honest behavioural test does not exist: "assert no connection was opened"
// requires either trusting that the test's own network was reachable or intercepting the syscall.
// Reading the imports asks the question directly — a package that imports nothing that can open a
// socket cannot open one — and it fails when the NEXT person adds net/http to reach a hub.
func TestNothingInThisPackageCanOpenANetworkConnection(t *testing.T) {
	banned := []string{"net", "net/http", "net/url", "crypto/tls", "net/rpc"}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	examined := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			examined++
			for _, imp := range file.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				for _, b := range banned {
					if p == b {
						t.Errorf("%s imports %q. PRD §4.4 and criterion 12: every local capability "+
							"works with no hub configured, and projects has no remote half at all.",
							filepath.Base(name), p)
					}
				}
			}
		}
	}
	// A CONTROL. With no files examined this passes vacuously and its green says nothing.
	if examined == 0 {
		t.Fatal("examined no non-test files — fix the walk, do not delete the test")
	}
	t.Logf("examined %d file(s)", examined)
}

// CRITERION 14, the half that lives on this side of the boundary.
//
// Issue #2's control API is on another branch and cannot be imported, so this does not drive two
// real surfaces. What it drives is the property that makes them agree when it does exist: the CLI's
// rendering and the wire form are two renderings of ONE snapshot, and they agree on the three
// markings the criterion names — provenance, missing, and undetermined.
func TestTheWireFormAndTheRenderedListingAgreeOnAllThreeMarkings(t *testing.T) {
	if !unreadableDirsWork(t) {
		t.Skip("this environment reads a 0000 directory anyway")
	}
	s := newStore(t)
	base := t.TempDir()
	missing := filepath.Join(base, "missing")
	unreadable := filepath.Join(base, "unreadable")
	ok := filepath.Join(base, "ok")
	for _, d := range []string{missing, unreadable, ok} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := projects.Add(s, d); err != nil {
			t.Fatal(err)
		}
	}
	mkfile(t, filepath.Join(ok, "a.txt"), "x")
	os.RemoveAll(missing)
	os.Chmod(unreadable, 0o000)
	t.Cleanup(func() { os.Chmod(unreadable, 0o755) })

	snap, err := projects.Take(s, noEnv, time.Now().UTC(), nothingWatching)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := projects.MarshalSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	// A control-API client's view: decode the wire form and render it the same way.
	remote, err := projects.UnmarshalSnapshot(wire)
	if err != nil {
		t.Fatal(err)
	}

	var local, viaWire strings.Builder
	if err := projects.Render(&local, snap); err != nil {
		t.Fatal(err)
	}
	if err := projects.Render(&viaWire, remote); err != nil {
		t.Fatal(err)
	}
	if local.String() != viaWire.String() {
		t.Errorf("the CLI's listing and the same snapshot round-tripped through the control API's "+
			"wire form disagree.\nCLI:\n%s\nwire:\n%s", local.String(), viaWire.String())
	}

	// And the three markings survive the round trip individually, so a whole-string comparison that
	// happened to match on a build where every entry rendered blank cannot be what passed.
	for _, path := range []string{missing, unreadable, ok} {
		a, b := entryFor(t, snap, path), entryFor(t, remote, path)
		if a.Provenance != b.Provenance {
			t.Errorf("%s: provenance %v locally, %v over the wire", path, a.Provenance, b.Provenance)
		}
		if a.State.Present != b.State.Present {
			t.Errorf("%s: Present %s locally, %s over the wire", path, a.State.Present, b.State.Present)
		}
		if a.State.Readable != b.State.Readable {
			t.Errorf("%s: Readable %s locally, %s over the wire", path, a.State.Readable, b.State.Readable)
		}
	}
	// The wire form names the provenance rather than numbering it, so a control API written against
	// it does not depend on this file's constant ordering.
	if !strings.Contains(string(wire), `"examined-now"`) {
		t.Errorf("the wire form does not name the provenance:\n%s", wire)
	}
}

// Provenance's zero value is not one of the two real answers, and renders as a defect rather than as
// either. Same reasoning as tri.Undetermined being tri's zero — see the constant's comment.
func TestUnrecordedProvenanceIsNotOneOfTheTwoAnswers(t *testing.T) {
	var zero projects.Provenance
	if zero == projects.DaemonPolled || zero == projects.ExaminedNow {
		t.Fatal("the zero Provenance IS one of the two real answers, so a struct built on an " +
			"error path silently claims a provenance nobody recorded")
	}
	if s := zero.String(); s == projects.DaemonPolled.String() || s == projects.ExaminedNow.String() || s == "" {
		t.Errorf("unrecorded provenance renders as %q", s)
	}
	b, err := json.Marshal(zero)
	if err != nil || string(b) != `"unrecorded"` {
		t.Errorf("unrecorded provenance on the wire: %s, %v", b, err)
	}
}

// THE FOUR PROVENANCES ARE FOUR DISTINCT RENDERINGS, COMPARED PAIRWISE.
//
// Criterion 10 requires a real value, missing and undetermined to be three distinct renderings of a
// project's STATE. Issue #41's criterion 4 applies the same rule to the liveness answer. This is the
// two of them meeting: the provenance is itself three-valued now, plus the unrecorded marker, and
// none of the four may print as another. Pairwise, because four assertions against four literals all
// stay green after two of the phrases are edited to match.
func TestTheFourProvenancesAllRenderDifferently(t *testing.T) {
	all := map[string]projects.Provenance{
		"daemon-polled": projects.DaemonPolled,
		"examined-now":  projects.ExaminedNow,
		"undetermined":  projects.ProvenanceUndetermined,
		"unrecorded":    projects.ProvenanceUnrecorded,
	}
	names := []string{"daemon-polled", "examined-now", "undetermined", "unrecorded"}
	for i := 0; i < len(names); i++ {
		if s := all[names[i]].String(); strings.TrimSpace(s) == "" {
			t.Errorf("%s renders as silence", names[i])
		}
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if all[a].String() == all[b].String() {
				t.Errorf("%s and %s render IDENTICALLY as %q", a, b, all[a].String())
			}
			ja, _ := json.Marshal(all[a])
			jb, _ := json.Marshal(all[b])
			if string(ja) == string(jb) {
				t.Errorf("%s and %s are the same value on the wire: %s", a, b, ja)
			}
		}
	}
	// The undetermined provenance carries the product's fixed third-answer wording, so it cannot be
	// read as a negative, and it does NOT claim the walk happened because nothing was watching.
	und := projects.ProvenanceUndetermined.String()
	if !strings.Contains(und, tri.Undetermined.String()) {
		t.Errorf("the undetermined provenance does not use the product's undetermined wording: %q", und)
	}
	if strings.Contains(und, projects.ExaminedNow.String()) {
		t.Errorf("the undetermined provenance contains the determined phrase %q, so a reader — or a "+
			"grep — matches one inside the other: %q", projects.ExaminedNow.String(), und)
	}
}

// An undetermined liveness produces an undetermined provenance on every row, and a listing that
// still shows real state. Silence would be a worse answer than an honest "I cannot tell you".
func TestAnUndeterminedLivenessMakesEveryRowsProvenanceUndetermined(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "a.txt"), "x")
	if _, err := projects.Add(s, dir); err != nil {
		t.Fatal(err)
	}

	snap, err := projects.Take(s, noEnv, time.Now().UTC(), livenessUnknown)
	if err != nil {
		t.Fatal(err)
	}
	e := entryFor(t, snap, dir)
	if e.Provenance != projects.ProvenanceUndetermined {
		t.Errorf("provenance is %v where liveness was not established; want undetermined. "+
			"Stamping a determined answer here resolves an unestablished fact in order to act, "+
			"and then reports the action as though the resolution had been a finding.", e.Provenance)
	}
	if e.State.Files != 1 {
		t.Errorf("the state is %d files, want 1 — the person was given silence instead of the "+
			"numbers, which is not what an undetermined LIVENESS justifies withholding", e.State.Files)
	}
	if snap.Watching != tri.Undetermined {
		t.Errorf("Watching is %s, want %s", snap.Watching, tri.Undetermined)
	}
	if snap.WatchingDetail == "" {
		t.Error("an undetermined watching answer carries no reason; a shrug is not the third answer")
	}

	// And it survives the wire, so a control API client is told the same thing.
	wire, err := projects.MarshalSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	back, err := projects.UnmarshalSnapshot(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryFor(t, back, dir).Provenance; got != projects.ProvenanceUndetermined {
		t.Errorf("the undetermined provenance came back over the wire as %v", got)
	}
	if back.Watching != tri.Undetermined || back.WatchingDetail != snap.WatchingDetail {
		t.Errorf("the watching answer did not survive the wire: %s / %q",
			back.Watching, back.WatchingDetail)
	}
}

// THE THREE WATCHING HEADERS ARE THREE DISTINCT RENDERINGS, PAIRWISE.
//
// Added after a mutation that deleted the "no" branch and let an established absence render in the
// undetermined wording was caught only by the CLI tests. The header is where a person reads the
// answer criterion 5 is about, and this package renders it, so this package should be the one that
// notices — a defect caught two packages away is caught by luck about who asserted what.
func TestTheThreeWatchingHeadersAllRenderDifferently(t *testing.T) {
	s := newStore(t)
	if _, err := projects.Add(s, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	rendered := map[string]string{}
	for name, live := range map[string]projects.Liveness{
		"yes":          daemonWatching,
		"no":           nothingWatching,
		"undetermined": livenessUnknown,
	} {
		snap, err := projects.Take(s, noEnv, time.Now().UTC(), live)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		if err := projects.Render(&b, snap); err != nil {
			t.Fatal(err)
		}
		// THE ANSWER LINE ITSELF, not the whole header block.
		//
		// The block version of this test STAYED GREEN under the mutation it was written for: with
		// the "no" branch deleted, an established absence rendered in the undetermined wording, but
		// the blocks still differed because the undetermined case appends a reason line and the
		// negative case has no reason to append. That is a difference in the EXPLANATION, not in
		// the answer — and a person reads the answer. Second time today a comparison of mine passed
		// on an incidental difference; both times only a mutation showed it.
		rendered[name] = strings.SplitN(b.String(), "\n", 2)[0]
		if strings.TrimSpace(rendered[name]) == "" {
			t.Errorf("the %s header is silence", name)
		}
	}
	names := []string{"yes", "no", "undetermined"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if rendered[a] == rendered[b] {
				t.Errorf("the %s and %s watching headers render IDENTICALLY:\n%s", a, b, rendered[a])
			}
		}
	}
	// The undetermined header must not be readable as the established absence.
	if strings.Contains(rendered["undetermined"], "nothing is watching between commands") {
		t.Errorf("the undetermined header contains the established-absence sentence:\n%s",
			rendered["undetermined"])
	}
}
