package status

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/health"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// sandbox is a whole machine's worth of state in a temporary directory: a store, a device registry
// under an XDG root, and nothing else.
//
// BOTH XDG_DATA_HOME AND HOME ARE SET. Setting only the first leaves anything that falls back to
// the home directory pointed at the developer's real one, and this suite would then rewrite their
// actual device pointer to a directory the test framework deletes on the way out.
type sandbox struct {
	root   string
	dir    string
	env    map[string]string
	getenv func(string) string
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("could not create a store to report on: %v", err)
	}
	env := map[string]string{
		store.PathEnv:    root,
		"XDG_DATA_HOME":  dir,
		"HOME":           dir,
		"XDG_CACHE_HOME": dir,
	}
	return &sandbox{root: root, dir: dir, env: env, getenv: func(k string) string { return env[k] }}
}

func (s *sandbox) open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(s.root)
	if err != nil {
		t.Fatalf("could not open the sandbox store: %v", err)
	}
	return st
}

// collect runs a screen against the sandbox with the daemon answer the caller names. The daemon
// liveness is an INPUT here exactly as it is in production (Issue #41).
func (s *sandbox) collect(live tri.Value, why string) Screen {
	return Collect(Query{
		Getenv: s.getenv, Now: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		Daemon: live, DaemonWhy: why, Report: daemon.Inspect(s.root),
	})
}

func find(t *testing.T, screen Screen, name string) Subsystem {
	t.Helper()
	for _, sub := range screen.Subsystems {
		if sub.Name == name {
			return sub
		}
	}
	t.Fatalf("subsystem %q is not on the screen at all; it has %v", name, SortedNames(screen.States()))
	return Subsystem{}
}

// CRITERION 1. Every subsystem §2.1 names appears, each as its own named line, none silently
// omitted — driven against the six rather than against whatever Collect happens to return.
func TestEverySubsystemNamedBySection21Appears(t *testing.T) {
	sb := newSandbox(t)
	screen := sb.collect(tri.No, "")
	states := screen.States()
	for _, name := range Required() {
		if _, ok := states[name]; !ok {
			t.Errorf("subsystem %q is missing from the screen; it has %v", name, SortedNames(states))
		}
	}
	if len(screen.Subsystems) != len(Required()) {
		t.Errorf("the screen has %d subsystems, want the %d §2.1 names", len(screen.Subsystems), len(Required()))
	}
	// The rendered screen must carry them too — a value that has them and a renderer that drops one
	// is the same failure to the person reading it.
	rendered := ParseRendered(screen.Render())
	for _, name := range Required() {
		if _, ok := rendered[name]; !ok {
			t.Errorf("subsystem %q is in the value and not on the rendered screen:\n%s", name, screen.Render())
		}
	}
}

// CRITERION 13 AND 14, TOGETHER, WHICH IS HOW THEY HAPPEN. With no daemon running: the daemon line
// says so, the invocation is a delivered answer rather than a tool failure, and every fact that
// does not need a daemon is still reported.
func TestWithNoDaemonEveryFactThatDoesNotNeedOneIsStillReported(t *testing.T) {
	sb := newSandbox(t)
	st := sb.open(t)
	connectChannel(t, st, "work-email", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	projectDir := filepath.Join(sb.dir, "a-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Add(st, projectDir); err != nil {
		t.Fatalf("could not add a project: %v", err)
	}

	screen := sb.collect(tri.No, "")

	if got := find(t, screen, Daemon); got.State != NotWorking {
		t.Errorf("with no daemon the daemon line is %v, want NotWorking", got.State)
	}
	// The facts that need no daemon: the store's existence and location, the channels, the
	// projects, the hub configuration. None of them may be undetermined merely because the daemon
	// is absent.
	for _, name := range []string{Store, Channels, Projects, Hub} {
		sub := find(t, screen, name)
		if !sub.State.Determined() {
			t.Errorf("with no daemon, %q is %v — that fact does not need a daemon.\n%s",
				name, sub.State, sub.Detail)
		}
	}
	if !strings.Contains(find(t, screen, Store).Detail, sb.root) {
		t.Errorf("the store line does not say where the store is:\n%s", find(t, screen, Store).Detail)
	}
	// Criterion 13's own wording: this is a successfully delivered answer, and the screen is not
	// blank. Nothing on it is undetermined, so a caller keying off that gets a success.
	if screen.AnyUndetermined() {
		t.Errorf("a fully-configured machine with no daemon produced undetermined states:\n%s", screen.Render())
	}
	if screen.Summary != SummaryNotAllWorking {
		t.Errorf("summary is %v; a stopped daemon on an otherwise-working machine is 'not everything is running'", screen.Summary)
	}
	// AND NO DAEMON EXISTS AFTERWARDS. Looking is not starting (§4.2, criterion 13).
	if rep := daemon.Inspect(sb.root); rep.Running == tri.Yes {
		t.Error("a daemon is running after a status screen was taken; status started one")
	}
}

// CRITERION 16. Status does not create the store: invoked where none exists it says none exists,
// and none exists afterwards.
func TestStatusDoesNotCreateTheStore(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "no-store-here")
	env := map[string]string{store.PathEnv: root, "XDG_DATA_HOME": dir, "HOME": dir}
	getenv := func(k string) string { return env[k] }

	screen := Collect(Query{Getenv: getenv, Now: time.Now().UTC(), Daemon: tri.No})

	sub := find(t, screen, Store)
	if sub.State != NotConfigured {
		t.Errorf("with no store the store line is %v, want NotConfigured", sub.State)
	}
	if !strings.Contains(sub.Detail, "no store exists") {
		t.Errorf("the store line does not say that no store exists:\n%s", sub.Detail)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("something exists at %s after a status screen; status created a store", root)
	}
	if store.Exists(root) == tri.Yes {
		t.Error("a store exists after status ran")
	}
	// CRITERION 18/19: the rest of the screen is still there and says what it could not establish.
	if len(screen.Subsystems) != len(Required()) {
		t.Errorf("a missing store cost the screen its other lines: %v", SortedNames(screen.States()))
	}
}

// CRITERION 15. With no hub configured, no network connection is opened; the hub line reads as not
// configured; and that is distinguishable from configured-and-unreachable and from
// configured-and-reachable. Every other line still renders its real state (§4.4, criterion 18).
func TestWithNoHubNothingIsDialledAndTheOtherLinesStillRender(t *testing.T) {
	sb := newSandbox(t)
	dialled := 0
	// A DIAL THAT COUNTS ITSELF. The assertion is that this function is never entered, which is a
	// stronger statement than "the hub line said not configured" — a line can say the right thing
	// after having reached out anyway.
	counting := func(getenv func(string) string) (devices.Source, error) {
		dialled++
		return nil, errors.New("this dial should never have happened")
	}
	q := Query{Getenv: sb.getenv, Now: time.Now().UTC(), Daemon: tri.No, Dial: counting, Report: daemon.Inspect(sb.root)}

	unconfigured := Collect(q)
	if dialled != 0 {
		t.Errorf("with no hub configured, the hub was dialled %d time(s) — §4.2 says nothing reaches out", dialled)
	}
	hubLine := find(t, unconfigured, Hub)
	if hubLine.State != NotConfigured {
		t.Errorf("with no hub the hub line is %v, want NotConfigured", hubLine.State)
	}
	for _, name := range []string{Store, Channels, Projects, Devices} {
		if sub := find(t, unconfigured, name); !sub.State.Determined() {
			t.Errorf("with no hub, the local line %q is %v — the local half stands alone (§4.4)", name, sub.State)
		}
	}

	// Configured and unreachable.
	sb.env[EnvHub] = "hub.example.internal"
	unreachable := Collect(q)
	if dialled == 0 {
		t.Fatal("with a hub configured nothing was dialled, so the comparison below is between two no-ops")
	}
	unreachableLine := find(t, unreachable, Hub)
	if unreachableLine.State != Undetermined {
		t.Errorf("a configured hub this build cannot reach is %v; it has NOT been established to be down", unreachableLine.State)
	}

	// Configured and reachable.
	q.Dial = func(func(string) string) (devices.Source, error) { return stubHub{}, nil }
	reachable := find(t, Collect(q), Hub)
	if reachable.State != Working {
		t.Errorf("a hub that answered is %v, want Working", reachable.State)
	}

	// THE THREE COMPARED PAIRWISE, which is criterion 15's actual requirement.
	lines := map[string]string{
		"not configured": hubLine.StateWord + " " + hubLine.Detail,
		"unreachable":    unreachableLine.StateWord + " " + unreachableLine.Detail,
		"reachable":      reachable.StateWord + " " + reachable.Detail,
	}
	seen := map[string]string{}
	for name, l := range lines {
		if other, dup := seen[l]; dup {
			t.Errorf("the %s hub line and the %s hub line are identical: %q", name, other, l)
		}
		seen[l] = name
	}
}

type stubHub struct{}

func (stubHub) Devices() ([]devices.Device, error) { return nil, nil }

// CRITERION 6. An unreachable channel adapter, a project directory that has gone missing and a
// device that has never checked in each appear with their own state, and none of the three renders
// identically to something confirmed not working.
//
// COMPARED PAIRWISE against a genuine confirmed-not-working member — an expired credential — rather
// than against string literals.
func TestTheThreeNamedCasesEachGetTheirOwnRenderingAndNoneReadsAsAPlainFailure(t *testing.T) {
	sb := newSandbox(t)
	st := sb.open(t)

	// An adapter the last attempt could not reach.
	unreachable := channels.Connection{
		ID: "unreachable-chan", Kind: channels.KindTeams, Account: "someone@example.com",
		Credential: "x", CredentialExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := channels.Connect(st, unreachable); err != nil {
		t.Fatalf("could not connect a channel: %v", err)
	}
	unreachable.Last = channels.Ingestion{
		State: tri.No, Outcome: channels.OutcomeUnreachable, OutcomeDetail: "the endpoint did not answer",
	}
	if err := channels.Save(st, unreachable); err != nil {
		t.Fatalf("could not record an unreachable attempt: %v", err)
	}
	// A CONFIRMED FAILURE to compare the three against: a credential that has demonstrably expired.
	expired := channels.Connection{
		ID: "expired-chan", Kind: channels.KindEmail, Account: "old@example.com",
		Credential: "y", CredentialExpiresAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := channels.Connect(st, expired); err != nil {
		t.Fatalf("could not connect the expired channel: %v", err)
	}

	// A project directory that has gone missing: added while it existed, then removed.
	gone := filepath.Join(sb.dir, "gone-project")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Add(st, gone); err != nil {
		t.Fatalf("could not add a project: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	// A device registered and never started.
	reg, err := devices.Open(sb.getenv)
	if err != nil {
		t.Fatalf("could not open the device registry: %v", err)
	}
	if _, err := reg.Register("laptop", "machine-1", time.Now().UTC()); err != nil {
		t.Fatalf("could not register a device: %v", err)
	}

	screen := sb.collect(tri.No, "")
	item := func(sub string, name string) Item {
		t.Helper()
		for _, it := range find(t, screen, sub).Items {
			if strings.Contains(it.Name, name) {
				return it
			}
		}
		t.Fatalf("no member named %q on the %q line; it has %d members:\n%s",
			name, sub, len(find(t, screen, sub).Items), screen.Render())
		return Item{}
	}

	adapter := item(Channels, "unreachable-chan")
	missing := item(Projects, "gone-project")
	neverStarted := item(Devices, "laptop")
	confirmedFailure := item(Channels, "expired-chan")

	// Each appears with its own state. The adapter and the directory and the device are all on the
	// screen — that is the first half of the criterion.
	if adapter.State != Undetermined {
		t.Errorf("an unreachable adapter is %v; it has not been established to be broken", adapter.State)
	}
	if missing.State == Undetermined {
		t.Errorf("a directory established to be missing is %v; missing is a determined finding", missing.State)
	}
	if confirmedFailure.State != NotWorking {
		t.Fatalf("the comparator is %v, not a confirmed failure, so the comparisons below prove nothing", confirmedFailure.State)
	}

	// PAIRWISE. Every one of the four lines differs from every other.
	lines := map[string]string{
		"unreachable adapter":   render(adapter),
		"missing directory":     render(missing),
		"never-checked-in":      render(neverStarted),
		"confirmed not working": render(confirmedFailure),
	}
	seen := map[string]string{}
	for name, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Errorf("the %s member rendered as nothing", name)
		}
		if other, dup := seen[l]; dup {
			t.Errorf("the %s member and the %s member render identically: %q", name, other, l)
		}
		seen[l] = name
	}
	// And all four are actually on the screen a person reads, not just in the value.
	out := screen.Render()
	for name := range lines {
		_ = name
	}
	for _, needle := range []string{"unreachable-chan", "gone-project", "laptop", "expired-chan"} {
		if !strings.Contains(out, needle) {
			t.Errorf("%q never reached the rendered screen:\n%s", needle, out)
		}
	}
}

func render(it Item) string { return "[" + it.StateWord + "] " + it.Detail }

// CRITERION 7, DRIVEN. A subsystem that cannot be determined must not suppress, blank or abort the
// rest of the screen — here the hub and the device listing go undetermined together, because a
// configured hub this build cannot reach clouds both.
func TestAnUndeterminedSubsystemLeavesTheRestOfTheScreenIntact(t *testing.T) {
	sb := newSandbox(t)
	st := sb.open(t)
	connectChannel(t, st, "work-email", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	sb.env[EnvHub] = "hub.example.internal"

	screen := Collect(Query{
		Getenv: sb.getenv, Now: time.Now().UTC(), Daemon: tri.No, Report: daemon.Inspect(sb.root),
		Dial: func(func(string) string) (devices.Source, error) { return nil, errors.New("the hub did not answer") },
	})

	if got := find(t, screen, Hub); got.State != Undetermined {
		t.Fatalf("the hub is %v, so this test is not exercising an undetermined subsystem", got.State)
	}
	// THE REST OF THE SCREEN. Every line present, and the ones that do not depend on the hub still
	// carrying their real, determined answers rather than being blanked in sympathy.
	for _, name := range Required() {
		sub := find(t, screen, name)
		if strings.TrimSpace(sub.Detail) == "" {
			t.Errorf("%q was blanked when the hub went undetermined", name)
		}
	}
	for _, name := range []string{Daemon, Store, Channels, Projects} {
		if sub := find(t, screen, name); !sub.State.Determined() {
			t.Errorf("%q became %v because the HUB could not be reached; it does not depend on the hub", name, sub.State)
		}
	}
	if find(t, screen, Channels).State != Working {
		t.Errorf("the channels line lost its answer: %s", find(t, screen, Channels).Detail)
	}
	// And the summary is the undetermined one, not the cheerful one (criterion 8 again, end to end).
	if screen.Summary == SummaryAllWorking {
		t.Error("the summary leads with everything running while the hub is undetermined")
	}
}

// CRITERION 4. Status is a report, never a mutation: twice in a row against an unchanged client
// gives the same states, and nothing on disk changed.
func TestStatusTwiceChangesNothingAndSaysTheSame(t *testing.T) {
	sb := newSandbox(t)
	st := sb.open(t)
	connectChannel(t, st, "work-email", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))

	before := treeOf(t, sb.dir)
	first := sb.collect(tri.No, "")
	between := treeOf(t, sb.dir)
	second := sb.collect(tri.No, "")
	after := treeOf(t, sb.dir)

	if a, b := first.States(), second.States(); !sameStates(a, b) {
		t.Errorf("two consecutive status screens disagree:\n%v\n%v", a, b)
	}
	if first.Summary != second.Summary {
		t.Errorf("the summary changed between two consecutive runs: %v then %v", first.Summary, second.Summary)
	}
	if before != between {
		t.Errorf("the first status screen changed the machine:\n%s\n%s", before, between)
	}
	if before != after {
		t.Errorf("two status screens changed the machine:\n%s\n%s", before, after)
	}
	// AND NO DAEMON WAS STARTED by either of them.
	if daemon.Inspect(sb.root).Running == tri.Yes {
		t.Error("a daemon is running after two status screens")
	}
}

// treeOf is every path under dir with its size and mode, as one comparable string. It is how
// "changed no store content and no configuration" is asserted rather than asserted about.
func treeOf(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		b.WriteString(p)
		if !info.IsDir() {
			b.WriteString("\t" + info.Mode().String() + "\t" + itoa(info.Size()))
		}
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk the sandbox: %v", err)
	}
	return b.String()
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func sameStates(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// CRITERION 2. How the daemon's last run ended reaches the screen, and the three endings the
// criterion names produce three different outputs. Compared pairwise.
func TestTheLastRunEndingReachesTheScreenInThreeDistinctForms(t *testing.T) {
	sb := newSandbox(t)
	lines := map[string]string{}
	for name, ending := range map[string]daemon.Ending{
		"clean stop":      daemon.EndingStopped,
		"could not write": daemon.EndingCannotWrite,
		"unrecorded":      daemon.EndingUndetermined,
	} {
		screen := Collect(Query{
			Getenv: sb.getenv, Now: time.Now().UTC(), Daemon: tri.No,
			Report: daemon.Report{StorePath: sb.root, LastRun: ending},
		})
		lines[name] = find(t, screen, Daemon).Detail
	}
	seen := map[string]string{}
	for name, l := range lines {
		if !strings.Contains(l, "last run:") {
			t.Errorf("the %s daemon line does not report how the last run ended:\n%s", name, l)
		}
		if other, dup := seen[l]; dup {
			t.Errorf("the %s daemon line and the %s daemon line are identical:\n%s", name, other, l)
		}
		seen[l] = name
	}
}

// CRITERION 17. Where §4.6's owner-only check could not be confirmed and the control API therefore
// did not open, the screen says exactly that — and it is distinguishable from "no daemon was
// started".
func TestAControlAPIThatDeclinedIsNotReportedAsNoDaemon(t *testing.T) {
	sb := newSandbox(t)

	declined := Collect(Query{
		Getenv: sb.getenv, Now: time.Now().UTC(), Daemon: tri.Yes,
		Report: daemon.Report{
			StorePath: sb.root, Running: tri.Yes, LastRun: daemon.EndingNeverRun,
			Control:       tri.No,
			ControlDetail: daemon.ErrControlNotOwnerOnly.Error() + ": the owner of the socket could not be read",
		},
	})
	neverStarted := Collect(Query{
		Getenv: sb.getenv, Now: time.Now().UTC(), Daemon: tri.No,
		Report: daemon.Report{StorePath: sb.root, LastRun: daemon.EndingNeverRun, Control: tri.No,
			ControlDetail: "the daemon is not running, so nothing is listening on its control API"},
	})

	d, n := find(t, declined, Daemon), find(t, neverStarted, Daemon)
	if d.Detail == n.Detail {
		t.Fatalf("a daemon whose control API declined reads exactly like one that was never started:\n%s", d.Detail)
	}
	if d.State == NotWorking {
		t.Error("a running daemon whose control API declined is reported as not working — criterion 17 " +
			"says it is neither 'not running' nor 'failing'")
	}
	if !strings.Contains(d.Detail, "control API") || !strings.Contains(d.Detail, "owner-only") {
		t.Errorf("the declined case does not say the control API is not open and why:\n%s", d.Detail)
	}
	// It must also be visible on the screen a person reads, not only in the value.
	if !strings.Contains(declined.Render(), "owner-only") {
		t.Errorf("the reason the control API did not open never reached the screen:\n%s", declined.Render())
	}
}

// CRITERION 3, DRIVEN THROUGH COLLECT. A project state a daemon poll produced carries the POLL's
// time, and one whose poll time was never recorded leaves the line with no observation time rather
// than with a substituted one.
func TestAPolledStateWithNoRecordedTimeLeavesTheLineWithoutOne(t *testing.T) {
	sb := newSandbox(t)
	st := sb.open(t)
	dir := filepath.Join(sb.dir, "watched")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := projects.Add(st, dir)
	if err != nil {
		t.Fatalf("could not add a project: %v", err)
	}

	// A poll record with a real time. The daemon is reported as running, which is what makes
	// projects.Take read the polled record rather than walking the directory.
	polled := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	if err := st.PutJSON(projects.KindState, p.ID, map[string]any{
		"state":     map[string]any{"present": 1, "readable": 1},
		"polled_at": polled,
	}); err != nil {
		t.Fatalf("could not write a polled state: %v", err)
	}
	withTime := find(t, sb.collect(tri.Yes, ""), Projects)
	if !withTime.ObservedAt.Equal(polled) {
		t.Errorf("the projects line is stamped %v, want the poll's own time %v", withTime.ObservedAt, polled)
	}
	if !strings.Contains(withTime.ObservedText(), polled.Format(time.RFC3339)) {
		t.Errorf("the poll's time did not reach the line:\n%s", withTime.ObservedText())
	}

	// The same record with NO time recorded.
	if err := st.PutJSON(projects.KindState, p.ID, map[string]any{
		"state": map[string]any{"present": 1, "readable": 1},
	}); err != nil {
		t.Fatalf("could not write a polled state with no time: %v", err)
	}
	without := find(t, sb.collect(tri.Yes, ""), Projects)
	if !without.ObservedAt.IsZero() {
		t.Errorf("a polled state with no recorded time was stamped %v — criterion 3 forbids a "+
			"substituted or default time", without.ObservedAt)
	}
	if !strings.Contains(without.ObservedText(), "no observation time") {
		t.Errorf("the line does not say it has no observation time:\n%s", without.ObservedText())
	}
	if withTime.ObservedText() == without.ObservedText() {
		t.Error("a state read just now and a state with no observation time read the same")
	}
}

func connectChannel(t *testing.T, st *store.Store, id string, expires time.Time) {
	t.Helper()
	c := channels.Connection{
		ID: id, Kind: channels.KindEmail, Account: id + "@example.com",
		Credential: "token", CredentialExpiresAt: expires,
	}
	if err := channels.Connect(st, c); err != nil {
		t.Fatalf("could not connect channel %q: %v", id, err)
	}
}

// ISSUE #5's RELATED NOTE ON ISSUE #1: "status renders FDE's three values and must show them
// without collapsing any two of them into one."
//
// The three are compared PAIRWISE against each other rather than against literals, and the last
// assertion is the one the Issue's scope note demands: rendering the value is not implementing the
// health report, so an encryption answer nobody could read does not turn "is everything running?"
// into a question status failed to answer.
func TestFullDiskEncryptionsThreeValuesReachTheScreenWithoutCollapsing(t *testing.T) {
	sb := newSandbox(t)
	lines := map[string]string{}
	summaries := map[string]Summary{}
	values := map[string]string{}
	undetermined := map[string]bool{}
	for name, checker := range map[string]health.EncryptionChecker{
		"enabled":      stubChecker{on: true},
		"not enabled":  stubChecker{on: false},
		"undetermined": stubChecker{err: errors.New("the platform tool refused to answer")},
	} {
		screen := Collect(Query{
			Getenv: sb.getenv, Now: time.Now().UTC(), Daemon: tri.No, Report: daemon.Inspect(sb.root),
			Health: health.Runner{Checker: checker},
		})
		found := false
		for _, it := range find(t, screen, Store).Items {
			if it.Name != "full-disk encryption" {
				continue
			}
			found = true
			lines[name] = render(it)
			// THE RENDERED VALUE ON ITS OWN, cut before the mechanism and before the reason.
			// Comparing whole lines is not enough: an undetermined answer worded "not enabled"
			// still differs from the real not-enabled line by its reason text, and would pass a
			// whole-line comparison while telling the person their disk is unprotected on the
			// strength of the product not having looked.
			values[name] = strings.SplitN(it.Detail, " (", 2)[0]
		}
		if !found {
			t.Fatalf("the %s encryption answer never reached the store line:\n%s", name, screen.Render())
		}
		summaries[name] = screen.Summary
		undetermined[name] = screen.AnyUndetermined()
		if !strings.Contains(screen.Render(), "full-disk encryption") {
			t.Errorf("the %s encryption answer is in the value and not on the rendered screen", name)
		}
	}
	seen := map[string]string{}
	for name, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Errorf("the %s encryption answer rendered as nothing", name)
		}
		if other, dup := seen[l]; dup {
			t.Errorf("the %s encryption answer and the %s one render identically: %q", name, other, l)
		}
		seen[l] = name
	}
	seenValue := map[string]string{}
	for name, v := range values {
		if strings.TrimSpace(v) == "" {
			t.Errorf("the %s encryption answer rendered no value at all", name)
		}
		if other, dup := seenValue[v]; dup {
			t.Errorf("the %s encryption answer and the %s one render the same VALUE %q — two of "+
				"§4.1's three collapsed into one", name, other, v)
		}
		seenValue[v] = name
	}
	// AN UNREADABLE DISK STATE IS NOT A FAILED STATUS SCREEN. §4.1: a report, never a blocker; and
	// §3.9 keeps the health report a separate capability that status only points at.
	if summaries["undetermined"] != summaries["enabled"] {
		t.Errorf("an unreadable encryption answer changed the screen's summary from %v to %v — "+
			"status answered 'is everything running?' perfectly well in both cases",
			summaries["enabled"], summaries["undetermined"])
	}
	if undetermined["undetermined"] != undetermined["enabled"] {
		t.Error("an unreadable encryption answer changed whether the screen counts as having " +
			"something undetermined, and with it the invocation's outcome")
	}
}

type stubChecker struct {
	on  bool
	err error
}

func (stubChecker) Mechanism() string                       { return "a probe this test supplied" }
func (c stubChecker) Enabled(context.Context) (bool, error) { return c.on, c.err }
