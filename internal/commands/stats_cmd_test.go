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
)

// --- harness --------------------------------------------------------------------------------

// statsWorld is one run of `omw stats`.
//
// THE ENVIRONMENT IS PROBED, NOT NAMED. The control-socket path is a file inside t.TempDir() whose
// existence this test controls, and the outbox is a directory this test creates with the real
// constructor. Nothing here assumes a platform's convention for where a socket or a store lives,
// and nothing branches on runtime.GOOS: a test that named a path would pass or fail for reasons
// belonging to the machine rather than to the code.
type statsWorld struct {
	env    map[string]string
	socket string
	stdout bytes.Buffer
	stderr bytes.Buffer
	code   int
	// sourceCalled records whether the command tried to reach a hub. Criterion 11 is asserted on
	// it: with no hub configured, the function that would open a connection is never entered.
	sourceCalled bool
}

func newStatsWorld(t *testing.T) *statsWorld {
	t.Helper()
	return &statsWorld{env: map[string]string{}, socket: filepath.Join(t.TempDir(), "omw.sock")}
}

// withDaemon creates the socket file so the command's PROBE finds one.
func (w *statsWorld) withDaemon(t *testing.T) *statsWorld {
	t.Helper()
	if err := os.WriteFile(w.socket, nil, 0o600); err != nil {
		t.Fatalf("create socket stand-in: %v", err)
	}
	w.env[statsEnvSocket] = w.socket
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
	orig := statsSource
	t.Cleanup(func() { statsSource = orig })
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

	if _, err := os.Stat(w.socket); err == nil {
		t.Fatalf("omw stats created the daemon's socket — PRD §4.2: no command starts the daemon on a person's behalf")
	}
	if w.sourceCalled {
		t.Fatalf("with no daemon running the command reached for a hub anyway:\n%s", w.all())
	}
	if !strings.Contains(w.out(), hub.ErrDaemonNotRunning.Code) {
		t.Fatalf("the daemon being down was not reported:\n%s", w.out())
	}

	// STRUCTURAL, and honestly labelled as such: this asserts the command has no code that could
	// start a process, which is stronger than observing that this one run did not.
	src, err := os.ReadFile("stats_cmd.go")
	if err != nil {
		t.Fatalf("read own source: %v", err)
	}
	for _, banned := range []string{"exec.Command", "os/exec", "syscall.ForkExec", "cmd.Start("} {
		if strings.Contains(string(src), banned) {
			t.Fatalf("stats_cmd.go contains %q — a statistics request must have no way to start a daemon", banned)
		}
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
