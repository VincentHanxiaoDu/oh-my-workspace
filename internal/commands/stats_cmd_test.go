package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// --- harness --------------------------------------------------------------------------------

// statsWorld is one run of `omw stats`.
//
// THE ENVIRONMENT IS PROBED, NOT NAMED. Whether the daemon is running comes from the product's ONE
// answer, `daemonLiveness`, which this harness drives through its three values; this file names no
// socket path and no socket variable, because `internal/daemon` owns that rule and a second copy of
// it is wrong on the runtime-directory fallback. The outbox is a directory created with the real
// `drafts.Create`. Nothing branches on runtime.GOOS: a test that named a path would pass or fail
// for reasons belonging to the machine rather than to the code.
type statsWorld struct {
	env map[string]string
	// live is what the ONE liveness answer says for this run. It is a tri.Value, not a bool: a
	// liveness that could not be established is a third state and this command must not render it
	// as a stopped daemon (Issue #41).
	live    tri.Value
	liveWhy string
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	code    int
	// sourceCalled records whether the command tried to reach a hub. Criterion 11 is asserted on
	// it: with no hub configured, the function that would open a connection is never entered.
	sourceCalled bool
}

func newStatsWorld(t *testing.T) *statsWorld {
	t.Helper()
	// tri.No is the default because it is the state a machine is in before anything is started,
	// and because the ZERO value of tri is Undetermined — which would silently make every run of
	// this harness the third case if it were left unset.
	return &statsWorld{env: map[string]string{}, live: tri.No}
}

// withDaemon says the one liveness answer reports a running daemon.
func (w *statsWorld) withDaemon(t *testing.T) *statsWorld {
	t.Helper()
	w.live = tri.Yes
	return w
}

// withUndeterminedLiveness says the one liveness answer could not be established — a lock that
// cannot be read, a store that cannot be resolved. It is NOT a stopped daemon.
func (w *statsWorld) withUndeterminedLiveness(why string) *statsWorld {
	w.live = tri.Undetermined
	w.liveWhy = why
	return w
}

func (w *statsWorld) withHub() *statsWorld        { w.env[statsEnvHub] = "hub.example"; return w }
func (w *statsWorld) as(p string) *statsWorld     { w.env[statsEnvIdentity] = p; return w }
func (w *statsWorld) scopes(s string) *statsWorld { w.env[statsEnvScopes] = s; return w }

// withOutbox creates a real local outbox holding the named drafts.
func (w *statsWorld) withOutbox(t *testing.T, draftNames ...string) *statsWorld {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "outbox")
	o, err := drafts.Create(dir)
	if err != nil {
		t.Fatalf("create outbox: %v", err)
	}
	for _, n := range draftNames {
		if _, err := o.Revise(hub.NoteID(n), "draft body for "+n); err != nil {
			t.Fatalf("revise %q: %v", n, err)
		}
	}
	w.env[statsEnvOutbox] = dir
	return w
}

func (w *statsWorld) run(t *testing.T, store *hub.Store, args ...string) *statsWorld {
	t.Helper()
	orig, origLive := statsSource, daemonLiveness
	t.Cleanup(func() { statsSource, daemonLiveness = orig, origLive })
	daemonLiveness = func(cli.Env) (tri.Value, string) { return w.live, w.liveWhy }
	statsSource = func(cli.Env) (*hub.Store, error) {
		w.sourceCalled = true
		if store == nil {
			return nil, hub.ErrHubUnreachable
		}
		return store, nil
	}
	w.code = cli.Run(append([]string{"stats"}, args...), &w.stdout, &w.stderr,
		func(k string) string { return w.env[k] })
	return w
}

func (w *statsWorld) out() string { return w.stdout.String() }
func (w *statsWorld) all() string { return w.stdout.String() + w.stderr.String() }

// statsHubCorpus is a hub holding one note the searcher may read and one they may not.
func statsHubCorpus(t *testing.T) *hub.Store {
	t.Helper()
	r := hub.NewRecord()
	s := hub.NewStore(r)
	n := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { n = n.Add(time.Hour); return n })
	r.AddPerson("searcher")
	r.AddPerson("ada")
	if _, err := s.Publish(hub.Publication{Author: "ada", Title: "readable", Body: "a body"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := s.Publish(hub.Publication{Author: "ada", Title: "hidden", Body: "b body", Visibility: hub.SelfOnly()}); err != nil {
		t.Fatalf("publish hidden: %v", err)
	}
	return s
}

// statsEmptyHub is a hub with nothing in it at all — the "determined to be nothing" control.
func statsEmptyHub(t *testing.T) *hub.Store {
	t.Helper()
	r := hub.NewRecord()
	r.AddPerson("searcher")
	return hub.NewStore(r)
}

// statLine returns the value of one `  name: value` line in a rendered report half.
func statLine(t *testing.T, out, half, name string) string {
	t.Helper()
	inHalf := false
	for _, line := range strings.Split(out, "\n") {
		if line == half+":" {
			inHalf = true
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") {
			inHalf = false
		}
		if inHalf && strings.HasPrefix(strings.TrimSpace(line), name+": ") {
			return strings.TrimPrefix(strings.TrimSpace(line), name+": ")
		}
	}
	t.Fatalf("no %q line in the %q half of:\n%s", name, half, out)
	return ""
}

// --- criterion 11: no hub means no network, and the local half still answers -------------------

func TestStatsWithNoHubReturnsLocalDeterminedAndHubUndetermined(t *testing.T) {
	w := newStatsWorld(t).withOutbox(t, "one", "two").as("searcher")
	w.run(t, statsHubCorpus(t))

	if w.sourceCalled {
		t.Fatalf("with no hub configured the command reached for a hub. PRD §4.2: no network connection without a hub.\n%s", w.all())
	}
	if got := statLine(t, w.out(), "local", "notes"); got != "2" {
		t.Fatalf("local notes = %q, want a determined 2 — PRD §4.4, the local half stands alone:\n%s", got, w.out())
	}
	hubNotes := statLine(t, w.out(), "hub", "notes")
	if !strings.Contains(hubNotes, hub.UndeterminedToken) {
		t.Fatalf("hub notes = %q, want undetermined with no hub configured:\n%s", hubNotes, w.out())
	}
	if !strings.Contains(hubNotes, hub.ErrNoHubConfigured.Code) {
		t.Fatalf("hub notes = %q, want the reason %q — criterion 11 asks for the reason to be that no hub is configured",
			hubNotes, hub.ErrNoHubConfigured.Code)
	}
	if hubNotes == "0" {
		t.Fatalf("hub statistics rendered as zero with no hub configured — that is the lie criterion 11 forbids")
	}
	if w.code != cli.ExitUndetermined {
		t.Fatalf("exit %d, want %d: a report containing an undetermined statistic is not a fully determined answer",
			w.code, cli.ExitUndetermined)
	}
}

// TestStatsNoLocalStoreIsNotZero. There being nowhere to look is a determined fact and still not a
// count of nothing.
func TestStatsNoLocalStoreIsNotZero(t *testing.T) {
	w := newStatsWorld(t).as("searcher")
	w.run(t, nil)
	got := statLine(t, w.out(), "local", "notes")
	if got == "0" {
		t.Fatalf("with no local outbox the local count printed as 0; want undetermined with reason %q", hub.ErrNoLocalStore.Code)
	}
	if !strings.Contains(got, hub.ErrNoLocalStore.Code) {
		t.Fatalf("local notes = %q, want the reason %q", got, hub.ErrNoLocalStore.Code)
	}
}

// --- criteria 6, 10, 12: the reasons are four different things, and none is a zero ---------------

// statsReasonCases is every way a statistic can come back undetermined, plus the determined-zero
// control. They are compared PAIRWISE below, because "each differs from the control" is easy to
// satisfy one at a time and easy to break between any two.
type statsReasonCase struct {
	name  string
	build func(t *testing.T) *statsWorld
	// wantCode is the reason code the hub half must carry, or "" for the determined control.
	wantCode string
}

func statsReasonCases() []statsReasonCase {
	return []statsReasonCase{
		{
			name: "determined zero — the hub answered and there is nothing readable",
			// The local outbox is real and empty, so BOTH halves are determined and the exit code
			// can be asserted as Success. Without it the local half would be undetermined for its
			// own reason and this case would stop being the fully determined control.
			build: func(t *testing.T) *statsWorld {
				return newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read").withOutbox(t)
			},
			wantCode: "",
		},
		{
			name:     "no hub configured",
			build:    func(t *testing.T) *statsWorld { return newStatsWorld(t).as("searcher").scopes("read") },
			wantCode: hub.ErrNoHubConfigured.Code,
		},
		{
			name:     "daemon not running",
			build:    func(t *testing.T) *statsWorld { return newStatsWorld(t).withHub().as("searcher").scopes("read") },
			wantCode: hub.ErrDaemonNotRunning.Code,
		},
		{
			name:     "not signed in",
			build:    func(t *testing.T) *statsWorld { return newStatsWorld(t).withHub().withDaemon(t).scopes("read") },
			wantCode: hub.ErrNotSignedIn.Code,
		},
		{
			name: "hub unreachable",
			build: func(t *testing.T) *statsWorld {
				return newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
			},
			wantCode: hub.ErrHubUnreachable.Code,
		},
	}
}

// TestStatsUndeterminedReasonsAreAllDistinctAndNoneIsZero drives criteria 6, 10 and 12 in one
// place, because what each of them asks for is that two outcomes do not look the same.
func TestStatsUndeterminedReasonsAreAllDistinctAndNoneIsZero(t *testing.T) {
	seen := map[string]string{}
	for _, tc := range statsReasonCases() {
		w := tc.build(t)
		var store *hub.Store
		if tc.wantCode == "" {
			store = statsEmptyHub(t) // the hub answered: determined, and the answer is nothing
		}
		w.run(t, store)

		got := statLine(t, w.out(), "hub", "notes")
		if tc.wantCode == "" {
			if got != "0" {
				t.Fatalf("%s: hub notes = %q, want a determined 0 — the hub answered and there is nothing readable:\n%s",
					tc.name, got, w.out())
			}
			if w.code != cli.Success {
				t.Fatalf("%s: exit %d, want %d — a fully determined answer succeeded at answering", tc.name, w.code, cli.Success)
			}
		} else {
			if got == "0" {
				t.Fatalf("%s: an undetermined statistic printed as a zero — criterion 6", tc.name)
			}
			if !strings.Contains(got, hub.UndeterminedToken) || !strings.Contains(got, tc.wantCode) {
				t.Fatalf("%s: hub notes = %q, want the undetermined marker and the reason %q:\n%s",
					tc.name, got, tc.wantCode, w.out())
			}
			if w.code != cli.ExitUndetermined {
				t.Fatalf("%s: exit %d, want %d", tc.name, w.code, cli.ExitUndetermined)
			}
		}
		if prev, dup := seen[got]; dup {
			t.Fatalf("two different outcomes rendered identically as %q: %q and %q", got, prev, tc.name)
		}
		seen[got] = tc.name
	}
	if len(seen) != len(statsReasonCases()) {
		t.Fatalf("expected %d distinct renderings, got %d: %v", len(statsReasonCases()), len(seen), seen)
	}
}

// TestStatsHubUnreachableIsNotTheHubReportingNothing is criterion 12 stated as its own assertion:
// "could not reach the hub" and "the hub reports nothing readable here" are different answers.
func TestStatsHubUnreachableIsNotTheHubReportingNothing(t *testing.T) {
	unreachable := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	unreachable.run(t, nil)

	nothing := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	nothing.run(t, statsEmptyHub(t))

	for _, name := range []string{"notes", "subjects", "recency"} {
		a := statLine(t, unreachable.out(), "hub", name)
		b := statLine(t, nothing.out(), "hub", name)
		if a == b {
			t.Fatalf("%s: an unreachable hub and a hub reporting nothing readable both rendered as %q", name, a)
		}
	}
	if !strings.Contains(statLine(t, unreachable.out(), "hub", "notes"), hub.ErrHubUnreachable.Code) {
		t.Fatalf("the unreachable hub did not name its reason:\n%s", unreachable.out())
	}
	if strings.Contains(statLine(t, nothing.out(), "hub", "recency"), hub.UndeterminedToken) {
		t.Fatalf("a hub that answered with nothing readable reported undetermined recency; criterion 13 wants a determined none:\n%s", nothing.out())
	}
}

// --- criterion 10: the daemon is said, never started ---------------------------------------------

func TestStatsStartsNoDaemon(t *testing.T) {
	w := newStatsWorld(t).withHub().as("searcher").scopes("read")
	w.run(t, statsHubCorpus(t))

	if w.sourceCalled {
		t.Fatalf("with no daemon running the command reached for a hub anyway:\n%s", w.all())
	}
	if !strings.Contains(w.out(), hub.ErrDaemonNotRunning.Code) {
		t.Fatalf("the daemon being down was not reported:\n%s", w.out())
	}

	// STRUCTURAL, and honestly labelled as such: this asserts the command has no code that could
	// start a process, and no code that reconstructs the daemon's socket path — the second is
	// Issue #41's rule, that whether a daemon is running has exactly one definition.
	src, err := os.ReadFile("stats_cmd.go")
	if err != nil {
		t.Fatalf("read own source: %v", err)
	}
	for _, banned := range []string{"exec.Command", "os/exec", "syscall.ForkExec", "cmd.Start(", "OMW_CONTROL_SOCKET", "control.sock"} {
		if strings.Contains(string(src), banned) {
			t.Fatalf("stats_cmd.go contains %q — a statistics request must neither start a daemon nor guess at whether one is running", banned)
		}
	}
	if !strings.Contains(string(src), "daemonLiveness(env)") {
		t.Fatalf("stats_cmd.go does not route its daemon question through daemonLiveness; Issue #41 says there is exactly one answer")
	}
}

// TestStatsUndeterminedLivenessIsNotAStoppedDaemon is Issue #41's criterion 4 read across to this
// Issue: a statistic computed while liveness is unknown is undetermined, and the reason is NOT that
// the daemon is stopped. A confident false negative is what #41 exists to remove.
func TestStatsUndeterminedLivenessIsNotAStoppedDaemon(t *testing.T) {
	w := newStatsWorld(t).withHub().as("searcher").scopes("read").
		withUndeterminedLiveness("the daemon lock could not be opened")
	w.run(t, statsHubCorpus(t))

	got := statLine(t, w.out(), "hub", "notes")
	if strings.Contains(got, hub.ErrDaemonNotRunning.Code) {
		t.Fatalf("hub notes = %q — nothing established that the daemon is stopped", got)
	}
	if !strings.Contains(got, hub.ErrDaemonLivenessUndetermined.Code) {
		t.Fatalf("hub notes = %q, want the reason %q", got, hub.ErrDaemonLivenessUndetermined.Code)
	}
	if got == "0" {
		t.Fatalf("a statistic computed while liveness was unknown printed as a zero")
	}
	if !strings.Contains(w.all(), "the daemon lock could not be opened") {
		t.Fatalf("the reason liveness could not be established was dropped:\n%s", w.all())
	}
	if !strings.Contains(w.all(), "this is not a report that the daemon is stopped") {
		t.Fatalf("the reader is not told that this is not a negative:\n%s", w.all())
	}
	if w.code != cli.ExitUndetermined {
		t.Fatalf("exit %d, want %d", w.code, cli.ExitUndetermined)
	}
	if w.sourceCalled {
		t.Fatalf("the command reached for a hub without establishing that a daemon was there")
	}
}

// TestStatsAndTheDaemonSurfacesSpellTheThirdAnswerTheSameWay stops the hub's error code and package
// commands' own constant for the third answer from drifting into two spellings of one state.
func TestStatsAndTheDaemonSurfacesSpellTheThirdAnswerTheSameWay(t *testing.T) {
	if hub.ErrDaemonLivenessUndetermined.Code != codeDaemonUndetermined {
		t.Fatalf("the statistics surface says %q and the daemon surfaces say %q for the same state",
			hub.ErrDaemonLivenessUndetermined.Code, codeDaemonUndetermined)
	}
}

// --- criterion 7: undetermined is present, not omitted ------------------------------------------

func TestStatsUndeterminedIsNeverSilence(t *testing.T) {
	w := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	w.run(t, nil, "--json")

	var decoded map[string]any
	if err := json.Unmarshal([]byte(w.out()), &decoded); err != nil {
		t.Fatalf("the agent API emitted unparseable JSON: %v\n%s", err, w.out())
	}
	half, ok := decoded["hub"].(map[string]any)
	if !ok {
		t.Fatalf("the hub half is missing entirely from the response:\n%s", w.out())
	}
	for _, field := range []string{"notes", "subjects", "recency", "undetermined_notes"} {
		f, present := half[field]
		if !present {
			t.Fatalf("field %q was omitted from the response; an undetermined statistic is present and labelled, not absent:\n%s",
				field, w.out())
		}
		m, ok := f.(map[string]any)
		if !ok {
			t.Fatalf("field %q is not a labelled statistic: %#v", field, f)
		}
		if m["state"] != hub.UndeterminedToken {
			t.Fatalf("field %q state = %v, want %q", field, m["state"], hub.UndeterminedToken)
		}
		if m["reason"] == "" || m["reason"] == nil {
			t.Fatalf("field %q carries no reason code:\n%s", field, w.out())
		}
	}
}

// --- criterion 9: the CLI and the agent API say the same thing -----------------------------------

// TestStatsCLIAndAgentAPIAgree drives criterion 9 across every reason a statistic can be
// undetermined, not just the happy path — "including which statistics are undetermined" is the
// part of the criterion that a second computation would break.
func TestStatsCLIAndAgentAPIAgree(t *testing.T) {
	for _, tc := range statsReasonCases() {
		var store *hub.Store
		if tc.wantCode == "" {
			store = statsEmptyHub(t)
		}
		text := tc.build(t)
		text.withOutbox(t, "one")
		text.run(t, store)

		api := tc.build(t)
		api.env[statsEnvOutbox] = text.env[statsEnvOutbox]
		api.run(t, store, "--json")

		var decoded struct {
			Scope string `json:"scope"`
			Local struct {
				Notes    struct{ State string } `json:"notes"`
				Recency  struct{ State string } `json:"recency"`
				Subjects struct{ State string } `json:"subjects"`
			} `json:"local"`
			Hub struct {
				Notes    struct{ State, Reason string } `json:"notes"`
				Recency  struct{ State string }         `json:"recency"`
				Subjects struct{ State string }         `json:"subjects"`
			} `json:"hub"`
		}
		if err := json.Unmarshal([]byte(api.out()), &decoded); err != nil {
			t.Fatalf("%s: unparseable JSON: %v\n%s", tc.name, err, api.out())
		}
		if api.code != text.code {
			t.Fatalf("%s: the two surfaces exited differently: text %d, json %d", tc.name, text.code, api.code)
		}
		for _, f := range []struct {
			half, name, state string
		}{
			{"local", "notes", decoded.Local.Notes.State},
			{"local", "recency", decoded.Local.Recency.State},
			{"local", "subjects", decoded.Local.Subjects.State},
			{"hub", "notes", decoded.Hub.Notes.State},
			{"hub", "recency", decoded.Hub.Recency.State},
			{"hub", "subjects", decoded.Hub.Subjects.State},
		} {
			rendered := statLine(t, text.out(), f.half, f.name)
			inText := strings.Contains(rendered, hub.UndeterminedToken)
			inAPI := f.state == hub.UndeterminedToken
			if inText != inAPI {
				t.Fatalf("%s: %s.%s — the CLI says undetermined=%v (%q) and the agent API says %q",
					tc.name, f.half, f.name, inText, rendered, f.state)
			}
		}
		if tc.wantCode != "" && decoded.Hub.Notes.Reason != tc.wantCode {
			t.Fatalf("%s: the agent API reported reason %q, want %q", tc.name, decoded.Hub.Notes.Reason, tc.wantCode)
		}
	}
}

// --- criterion 2: three scopes, and a fourth is refused -------------------------------------------

func TestStatsScopes(t *testing.T) {
	for _, tc := range []struct {
		scope string
		want  int
	}{
		{"", cli.Success},
		{"company", cli.Success},
		{"person:ada", cli.Success},
		{"group:platform", cli.Success},
		{"team:platform", cli.ExitUsage},
		{"everything", cli.ExitUsage},
		{"person:nobody", cli.ExitFailure},
		{"group:nosuch", cli.ExitFailure},
	} {
		s := hub.NewStore(statsScopeRecord())
		if _, err := s.Publish(hub.Publication{Author: "ada", Title: "note", Body: "body"}); err != nil {
			t.Fatalf("publish: %v", err)
		}
		w := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read").withOutbox(t)
		args := []string{}
		if tc.scope != "" {
			args = append(args, "--scope", tc.scope)
		}
		w.run(t, s, args...)
		if w.code != tc.want {
			t.Fatalf("scope %q: exit %d, want %d\n%s", tc.scope, w.code, tc.want, w.all())
		}
		if tc.want != cli.Success && strings.Contains(w.out(), "notes: 0") {
			t.Fatalf("scope %q was refused but still produced statistics — a refused scope must not be widened or narrowed to one that works:\n%s",
				tc.scope, w.out())
		}
		if tc.want == cli.ExitFailure && !strings.Contains(w.all(), hub.ErrUnknownSearchScope.Code) {
			t.Fatalf("scope %q: want the %q code:\n%s", tc.scope, hub.ErrUnknownSearchScope.Code, w.all())
		}
	}
}

func statsScopeRecord() *hub.Record {
	r := hub.NewRecord()
	r.AddPerson("searcher")
	r.AddPerson("ada")
	r.DefineGroup("platform", "ada", "searcher")
	return r
}

// --- the numbers are the reader's, at the CLI too -------------------------------------------------

// TestStatsCountIsPerReader is criterion 4 driven through the command, so that a defect introduced
// between hub.Corpus and the rendering is caught as well as one inside the corpus.
func TestStatsCountIsPerReader(t *testing.T) {
	s := statsHubCorpus(t)
	narrow := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	narrow.run(t, s)
	wide := newStatsWorld(t).withHub().withDaemon(t).as("ada").scopes("read")
	wide.run(t, s)

	if got := statLine(t, narrow.out(), "hub", "notes"); got != "1" {
		t.Fatalf("notes for the narrower reader = %q, want 1 — the hub holds %d notes and one of them is not theirs to know about:\n%s",
			got, s.Count(), narrow.out())
	}
	if got := statLine(t, wide.out(), "hub", "notes"); got != "2" {
		t.Fatalf("notes for the author = %q, want 2 — statistics that do not differ per reader are not per-reader:\n%s", got, wide.out())
	}
}

// TestStatsSaysWhatRecencyMeans is criterion 14 at the surface: the version semantics recency is
// defined against is stated in the output, not left for a reader to infer.
func TestStatsSaysWhatRecencyMeans(t *testing.T) {
	w := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	w.run(t, statsHubCorpus(t))
	if !strings.Contains(w.out(), hub.RecencySemantics) {
		t.Fatalf("the report does not state what recency means:\n%s", w.out())
	}
}

// --- --as is refused at the request, for BOTH halves ---------------------------------------------

// TestStatsAsIsRefusedNotAnsweredAboutSomebodyElse is the defect that got this pull request
// refused, driven at the surface a person uses.
//
// The local half took --as as the identity with no check, so `--as bob` counted the drafts of
// whoever is signed in and labelled them bob's, and `--as bob --scope person:<signed-in>` printed a
// DETERMINED `notes: 0` with `coverage: complete` about drafts sitting right there. A determined
// statistic that is false is worse than an undetermined one: an agent builds a plan on it.
//
// The control is the first case. Without it, "everything is refused" and "the fix works" look the
// same.
func TestStatsAsIsRefusedNotAnsweredAboutSomebodyElse(t *testing.T) {
	// CONTROL: signed in as alice, no --as, two drafts, counted and attributed to alice.
	control := newStatsWorld(t).as("alice").withOutbox(t, "one", "two")
	control.run(t, nil)
	if got := statLine(t, control.out(), "local", "notes"); got != "2" {
		t.Fatalf("control: local notes = %q, want 2 — without this the refusals below prove nothing:\n%s", got, control.out())
	}
	if got := statLine(t, control.out(), "local", "subjects"); got != "person:alice" {
		t.Fatalf("control: subjects = %q, want person:alice", got)
	}

	// --as yourself is not impersonation and must still answer.
	self := newStatsWorld(t).as("alice").withOutbox(t, "one", "two")
	self.run(t, nil, "--as", "alice")
	if got := statLine(t, self.out(), "local", "notes"); got != "2" {
		t.Fatalf("--as with your own name was not answered: local notes = %q\n%s", got, self.all())
	}

	for _, tc := range []struct {
		name string
		args []string
		code string
	}{
		{"reading as somebody else", []string{"--as", "bob"}, hub.ErrGrantWiderThanHolder.Code},
		{"and scoped to the real owner of the material", []string{"--as", "bob", "--scope", "person:alice"}, hub.ErrGrantWiderThanHolder.Code},
	} {
		w := newStatsWorld(t).as("alice").withOutbox(t, "one", "two")
		w.run(t, nil, tc.args...)
		if w.code != cli.ExitFailure {
			t.Fatalf("%s: exit %d, want %d — refused when requested, not answered narrowly\n%s", tc.name, w.code, cli.ExitFailure, w.all())
		}
		if !strings.Contains(w.all(), tc.code) {
			t.Fatalf("%s: want the code %q:\n%s", tc.name, tc.code, w.all())
		}
		if strings.Contains(w.out(), "notes: 2") {
			t.Fatalf("%s: another person's material was counted and reported:\n%s", tc.name, w.out())
		}
		if strings.Contains(w.out(), "notes: 0") {
			t.Fatalf("%s: a DETERMINED zero was printed about material that is there — the wrong-zero defect:\n%s", tc.name, w.out())
		}
		if strings.Contains(w.out(), "person:bob") {
			t.Fatalf("%s: one person's drafts were attributed to another:\n%s", tc.name, w.out())
		}
	}

	// --as with nobody signed in has no identity that could be allowed to.
	nobody := newStatsWorld(t).withOutbox(t, "one")
	nobody.run(t, nil, "--as", "bob")
	if nobody.code != cli.ExitFailure || !strings.Contains(nobody.all(), hub.ErrNotSignedIn.Code) {
		t.Fatalf("--as with no identity: exit %d, want %d with %q:\n%s", nobody.code, cli.ExitFailure, hub.ErrNotSignedIn.Code, nobody.all())
	}
}

// TestStatsBothHalvesNameTheSameReader. One request has one requesting identity, and criterion 4's
// "the number says who it belongs to" is weakened the moment the two halves disagree about who that
// is.
func TestStatsBothHalvesNameTheSameReader(t *testing.T) {
	w := newStatsWorld(t).as("alice").withOutbox(t, "one")
	w.run(t, nil)
	local := statLine(t, w.out(), "local", "reader")
	hubHalf := statLine(t, w.out(), "hub", "reader")
	if local != hubHalf {
		t.Fatalf("one request, two readers: local says %q and hub says %q\n%s", local, hubHalf, w.out())
	}
	if local != "alice" {
		t.Fatalf("reader = %q, want alice", local)
	}
}
