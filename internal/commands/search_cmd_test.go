package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// --- harness ---------------------------------------------------------------------------------

// searchWorld is one run of `omw search`: the environment it sees and what it wrote.
//
// LIVENESS IS STUBBED HERE, AND THAT IS ALL THESE TESTS CLAIM. Issue #41 gave the product one
// answer to "is the daemon running" (daemonLiveness), and this file drives a surface's RENDERING of
// that answer — including the third one. It does not claim the answer itself is right: only a real
// started daemon proves that, and TestEveryDaemonReportingSurfaceAgreesWithDaemonStatus in
// liveness_test.go does it for every surface at once, deliberately without a stub.
//
// Nothing here names a control socket. The old harness created one and set an OMW_CONTROL_SOCKET
// variable the product never read, which is how this command shipped a confident false negative.
type searchWorld struct {
	env    map[string]string
	live   tri.Value
	why    string
	stdout bytes.Buffer
	stderr bytes.Buffer
	code   int
	// sourceCalled records whether the command tried to reach a hub. Criterion 21 is asserted on
	// this: with no hub configured, the function that would open a connection is never entered.
	sourceCalled bool
}

func newSearchWorld(t *testing.T) *searchWorld {
	t.Helper()
	// The default is a DETERMINED negative: no daemon. Undetermined is opted into explicitly by
	// withUndeterminedDaemon, so a test never gets the third answer by accident.
	return &searchWorld{env: map[string]string{}, live: tri.No}
}

// withDaemon says a daemon is running against this store.
func (w *searchWorld) withDaemon(t *testing.T) *searchWorld {
	t.Helper()
	w.live, w.why = tri.Yes, ""
	return w
}

// withUndeterminedDaemon says liveness could not be established — not that it is absent.
func (w *searchWorld) withUndeterminedDaemon(why string) *searchWorld {
	w.live, w.why = tri.Undetermined, why
	return w
}

func (w *searchWorld) withHub() *searchWorld        { w.env[searchEnvHub] = "hub.example"; return w }
func (w *searchWorld) as(p string) *searchWorld     { w.env[searchEnvIdentity] = p; return w }
func (w *searchWorld) scopes(s string) *searchWorld { w.env[searchEnvScopes] = s; return w }

// run drives the real registry, so the command is reached exactly as `omw` reaches it.
func (w *searchWorld) run(t *testing.T, store *hub.Store, roster *hub.Roster, args ...string) *searchWorld {
	t.Helper()
	origSource, origRoster, origLive := searchSource, searchRoster, daemonLiveness
	t.Cleanup(func() { searchSource, searchRoster, daemonLiveness = origSource, origRoster, origLive })
	daemonLiveness = func(cli.Env) (tri.Value, string) { return w.live, w.why }
	searchSource = func(cli.Env) (*hub.Store, error) {
		w.sourceCalled = true
		if store == nil {
			return nil, hub.ErrHubUnreachable
		}
		return store, nil
	}
	searchRoster = func(cli.Env) *hub.Roster { return roster }

	w.code = cli.Run(append([]string{"search"}, args...), &w.stdout, &w.stderr, func(k string) string { return w.env[k] })
	return w
}

func (w *searchWorld) out() string    { return w.stdout.String() }
func (w *searchWorld) errOut() string { return w.stderr.String() }
func (w *searchWorld) all() string    { return w.stdout.String() + w.stderr.String() }

func searchCorpus(t *testing.T) (*hub.Store, *hub.Record, hub.NoteID) {
	t.Helper()
	r := hub.NewRecord()
	s := hub.NewStore(r)
	n := time.Unix(0, 0).UTC()
	s.SetClock(func() time.Time { n = n.Add(time.Second); return n })
	r.AddPerson("searcher")
	r.AddPerson("ada")
	r.AddPerson("dana")
	note, err := s.Publish(hub.Publication{Author: "ada", Title: "why sessions drop", Body: "the staging cluster sessiondrop"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return s, r, note.ID
}

// --- criterion 14: found nothing is an answer --------------------------------------------------

func TestSearchFoundNothingSucceeds(t *testing.T) {
	s, _, _ := searchCorpus(t)
	w := newSearchWorld(t).withHub().as("searcher").scopes("read")
	w.withDaemon(t).run(t, s, nil, "nosuchterm")

	if w.code != cli.Success {
		t.Fatalf("exit %d, want %d — a search that ran and matched nothing SUCCEEDED at answering.\nstderr: %s",
			w.code, cli.Success, w.errOut())
	}
	if !strings.Contains(w.out(), "found nothing") {
		t.Fatalf("an empty result must say so on stdout:\n%s", w.out())
	}
	if !strings.Contains(w.out(), "coverage: complete") {
		t.Fatalf("an empty COMPLETE result must say it is complete:\n%s", w.out())
	}
}

// --- criterion 15, 16, 20, 22: the failure modes are four different things ---------------------

// searchFailureModes is every way a search can fail to run, and what each must look like. Driven
// as a TABLE, and then compared PAIRWISE below, because "each differs from criterion 14" is easy
// to satisfy one at a time and easy to break between any two.
type searchFailureMode struct {
	name     string
	world    func(t *testing.T) *searchWorld
	store    func(t *testing.T) *hub.Store
	wantCode int
	wantHint string
}

var searchFailureModes = []searchFailureMode{
	{
		// Criterion 22 / 21: no hub configured. A determined fact about this machine.
		name:     "no-hub-configured",
		world:    func(t *testing.T) *searchWorld { return newSearchWorld(t).as("searcher").scopes("read").withDaemon(t) },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrNoHubConfigured.Code,
	},
	{
		// Criterion 20: the daemon is not running. Said, never started.
		name:     "daemon-not-running",
		world:    func(t *testing.T) *searchWorld { return newSearchWorld(t).withHub().as("searcher").scopes("read") },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrDaemonNotRunning.Code,
	},
	{
		// Criterion 16: not signed in.
		name:     "not-signed-in",
		world:    func(t *testing.T) *searchWorld { return newSearchWorld(t).withHub().withDaemon(t) },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrNotSignedIn.Code,
	},
	{
		// Criterion 16: signed in, but the token does not carry `read` — only `write`, or only
		// `publish`. PRD §3.10.
		name: "token-carries-only-write",
		world: func(t *testing.T) *searchWorld {
			return newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("write")
		},
		store:    func(t *testing.T) *hub.Store { s, _, _ := searchCorpus(t); return s },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrReadScopeRequired.Code,
	},
	{
		name: "token-carries-only-publish",
		world: func(t *testing.T) *searchWorld {
			return newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("publish")
		},
		store:    func(t *testing.T) *hub.Store { s, _, _ := searchCorpus(t); return s },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrReadScopeRequired.Code,
	},
	{
		// Issue #41's third answer: liveness could not be established. Distinct from both a stopped
		// daemon and every other failure.
		name: "daemon-liveness-undetermined",
		world: func(t *testing.T) *searchWorld {
			return newSearchWorld(t).withHub().as("searcher").scopes("read").
				withUndeterminedDaemon("the lock file could not be read")
		},
		wantCode: cli.ExitUndetermined,
		wantHint: codeDaemonUndetermined,
	},
	{
		// Criterion 15: the hub is unreachable. UNDETERMINED, and its own exit code.
		name: "hub-unreachable",
		world: func(t *testing.T) *searchWorld {
			return newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
		},
		wantCode: cli.ExitUndetermined,
		wantHint: hub.ErrHubUnreachable.Code,
	},
	{
		// Criterion 4: an unknown scope. Zero-result-shaped, but not a zero result.
		name: "unknown-scope",
		world: func(t *testing.T) *searchWorld {
			return newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
		},
		store:    func(t *testing.T) *hub.Store { s, _, _ := searchCorpus(t); return s },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrUnknownSearchScope.Code,
	},
	{
		// Criterion 12: a search issued as somebody the grant does not act as. Refused when
		// requested, never narrowed to what the holder may see.
		name: "as-somebody-else",
		world: func(t *testing.T) *searchWorld {
			return newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
		},
		store:    func(t *testing.T) *hub.Store { s, _, _ := searchCorpus(t); return s },
		wantCode: cli.ExitFailure,
		wantHint: hub.ErrGrantWiderThanHolder.Code,
	},
}

func (m searchFailureMode) drive(t *testing.T) *searchWorld {
	t.Helper()
	w := m.world(t)
	var s *hub.Store
	if m.store != nil {
		s = m.store(t)
	}
	args := []string{"sessiondrop"}
	switch m.name {
	case "unknown-scope":
		args = append(args, "--scope=person:nobody")
	case "as-somebody-else":
		args = append(args, "--as=ada")
	}
	return w.run(t, s, nil, args...)
}

func TestSearchFailureModesAreEachSaidAndNoneIsSilence(t *testing.T) {
	// CRITERION 18: no failure path is silence, and none exits zero with empty output.
	for _, m := range searchFailureModes {
		t.Run(m.name, func(t *testing.T) {
			w := m.drive(t)
			if w.code != m.wantCode {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", w.code, m.wantCode, w.out(), w.errOut())
			}
			if strings.TrimSpace(w.all()) == "" {
				t.Fatalf("this failure produced NO OUTPUT ON ANY STREAM; silence is not one of the answers")
			}
			if !strings.Contains(w.all(), m.wantHint) {
				t.Fatalf("output does not carry the code %q, so a caller must parse prose to tell it\n"+
					"from another failure:\n%s", m.wantHint, w.all())
			}
			if strings.Contains(w.out(), "found nothing") {
				t.Fatalf("a search that COULD NOT RUN rendered as one that found nothing:\n%s", w.out())
			}
		})
	}
}

func TestEveryFailureModeIsDistinguishableFromEveryOtherAndFromFoundNothing(t *testing.T) {
	// CRITERIA 14, 15, 16, 22 TOGETHER, AND PAIRWISE. Each mode is compared with every other mode
	// and with the successful empty search, on the pair (exit code, whole output). Asserting each
	// against a literal would pass just as happily after two of them were edited into the same
	// sentence — the same reasoning Issue #12 used for its visibility renderings.
	type observed struct {
		name string
		code int
		text string
	}
	var seen []observed

	s, _, _ := searchCorpus(t)
	empty := newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("read").run(t, s, nil, "nosuchterm")
	seen = append(seen, observed{"found-nothing (criterion 14)", empty.code, empty.all()})

	for _, m := range searchFailureModes {
		w := m.drive(t)
		seen = append(seen, observed{m.name, w.code, w.all()})
	}

	for i := range seen {
		for j := i + 1; j < len(seen); j++ {
			if seen[i].code == seen[j].code && seen[i].text == seen[j].text {
				t.Fatalf("%q and %q produce identical output AND identical exit codes (%d).\n"+
					"They are different facts and a person cannot tell them apart:\n%s",
					seen[i].name, seen[j].name, seen[i].code, seen[i].text)
			}
		}
	}
	// The specific pairing criterion 15 names: the empty result exits ZERO and the hub failure does
	// not, so exit status ALONE separates them.
	for _, o := range seen[1:] {
		if o.code == cli.Success {
			t.Fatalf("failure mode %q exited 0, which is the code a successful empty search uses", o.name)
		}
	}
}

// --- criterion 20, 21: nothing implicit --------------------------------------------------------

func TestSearchNeverStartsTheDaemon(t *testing.T) {
	// CRITERION 20, PROBED AGAINST A REAL STORE AND THE PRODUCT'S OWN ANSWER.
	//
	// The old version of this test stat'd a socket path of its own invention, which is the very
	// habit Issue #41 removed: it would have gone on passing while the product's real answer said
	// something else. Here the store is real, liveness is asked through daemon.Inspect — the one
	// definition — before and after, and no stub is installed for that question.
	root := storeThatExists(t)
	if before := daemon.Inspect(root).Running; before != tri.No {
		t.Fatalf("the fixture is broken: a fresh store already reports the daemon as %v", before)
	}

	w := newSearchWorld(t).withHub().as("searcher").scopes("read") // live defaults to tri.No
	w.env[store.PathEnv] = root
	w.run(t, nil, nil, "sessiondrop")

	if after := daemon.Inspect(root).Running; after != tri.No {
		t.Fatalf("after the search the daemon reports as %v — something was started on the person's behalf", after)
	}
	if w.code != cli.ExitFailure || !strings.Contains(w.errOut(), hub.ErrDaemonNotRunning.Code) {
		t.Fatalf("exit %d / %s — the command must SAY the daemon is not running", w.code, w.errOut())
	}
}

func TestUndeterminedLivenessIsNotReportedAsAStoppedDaemon(t *testing.T) {
	// ISSUE #41's THIRD ANSWER, at this surface. A liveness that could not be established is not a
	// daemon that is absent, and the two must not share wording or an exit code — otherwise the
	// person is told something was established when nothing was.
	stopped := newSearchWorld(t).withHub().as("searcher").scopes("read")
	stopped.run(t, nil, nil, "sessiondrop")

	unknown := newSearchWorld(t).withHub().as("searcher").scopes("read").
		withUndeterminedDaemon("the lock file could not be read")
	unknown.run(t, nil, nil, "sessiondrop")

	if unknown.code == stopped.code {
		t.Fatalf("an undetermined liveness exits %d, the same as a determined 'not running' — a caller\n"+
			"cannot tell 'I could not check' from 'it is stopped'", unknown.code)
	}
	if unknown.code != cli.ExitUndetermined {
		t.Fatalf("exit %d, want %d", unknown.code, cli.ExitUndetermined)
	}
	if strings.Contains(unknown.errOut(), hub.ErrDaemonNotRunning.Code) {
		t.Fatalf("the undetermined report carries the code for a STOPPED daemon:\n%s", unknown.errOut())
	}
	if !strings.Contains(unknown.errOut(), "the lock file could not be read") {
		t.Fatalf("the undetermined report does not say why:\n%s", unknown.errOut())
	}
	if unknown.errOut() == stopped.errOut() {
		t.Fatalf("the two reports are word for word identical:\n%s", unknown.errOut())
	}
}

func TestSearchOpensNoNetworkWithoutAHub(t *testing.T) {
	// CRITERION 21. Asserted by PROBING whether the only function in this command that could reach
	// a hub was entered at all. A source that is never called cannot have dialled.
	w := newSearchWorld(t).as("searcher").scopes("read").withDaemon(t)
	w.run(t, nil, nil, "sessiondrop")

	if w.sourceCalled {
		t.Fatalf("with no hub configured the command still tried to reach one")
	}
	if w.code != cli.ExitFailure {
		t.Fatalf("exit %d, want %d", w.code, cli.ExitFailure)
	}
	// CRITERION 22: it names what is missing, and claims nothing about the corpus.
	for _, want := range []string{hub.ErrNoHubConfigured.Code, "No connection was attempted", "NOT an empty result"} {
		if !strings.Contains(w.errOut(), want) {
			t.Fatalf("output does not contain %q:\n%s", want, w.errOut())
		}
	}
}

func TestSearchCommandFilesOpenNoNetwork(t *testing.T) {
	// The structural half of criterion 21: this command has no network client to open a connection
	// with, and no way to spawn a process.
	b, err := os.ReadFile("search_cmd.go")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, pkg := range []string{`"net"`, `"net/http"`, `"os/exec"`} {
		if strings.Contains(string(b), pkg) {
			t.Fatalf("search_cmd.go imports %s", pkg)
		}
	}
}

// --- criterion 17, 19: incomplete and undetermined ---------------------------------------------

func TestAnIncompleteSearchIsNotPresentedAsAnAnswer(t *testing.T) {
	// CRITERION 17 and 19 through the CLI: a partially-covered corpus reports INCOMPLETE and exits
	// with the undetermined code, not the success code an equally-sized complete answer uses.
	r := hub.NewRecord()
	s := hub.NewStore(r)
	n := time.Unix(0, 0).UTC()
	s.SetClock(func() time.Time { n = n.Add(time.Second); return n })
	r.AddPerson("searcher")
	r.DefineGroup("platform", "ada")
	v, err := hub.ToGroup("platform")
	if err != nil {
		t.Fatalf("to group: %v", err)
	}
	if _, err := s.Publish(hub.Publication{Author: "ada", Title: "t", Body: "sessiondrop"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := s.Publish(hub.Publication{Author: "dana", Title: "hidden", Body: "sessiondrop", Visibility: v}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	r.Dissolve("platform")

	w := newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("read").run(t, s, nil, "sessiondrop")
	if w.code != cli.ExitUndetermined {
		t.Fatalf("exit %d, want %d — an incomplete result must not exit as a complete one.\n%s",
			w.code, cli.ExitUndetermined, w.all())
	}
	if !strings.Contains(w.out(), "INCOMPLETE") {
		t.Fatalf("the result does not say it is incomplete:\n%s", w.out())
	}
	if strings.Contains(w.out(), "hidden") {
		t.Fatalf("the unevaluable note's text reached the output:\n%s", w.out())
	}
}

// --- criterion 23 and the scoping criteria, through the real CLI --------------------------------

func TestSearchFindsAPermittedNoteUnderEveryScope(t *testing.T) {
	s, _, id := searchCorpus(t)
	for _, scope := range []string{"company", "person:ada"} {
		w := newSearchWorld(t).withHub().withDaemon(t).as("searcher").scopes("read").run(t, s, nil, "sessiondrop", "--scope="+scope)
		if w.code != cli.Success {
			t.Fatalf("scope %q: exit %d\n%s", scope, w.code, w.all())
		}
		if !strings.Contains(w.out(), string(id)) {
			t.Fatalf("scope %q did not return the note:\n%s", scope, w.out())
		}
	}
}

func TestSearchScopesSurfaceDoesNotInventACapability(t *testing.T) {
	w := newSearchWorld(t)
	w.run(t, nil, nil, "scopes")
	if w.code != cli.Success {
		t.Fatalf("exit %d", w.code)
	}
	for _, want := range []string{"read", "write", "publish", "person:", "group:", "company"} {
		if !strings.Contains(w.out(), want) {
			t.Fatalf("scopes output is missing %q:\n%s", want, w.out())
		}
	}
	for _, forbidden := range []string{"search-admin", "read-all", "search-scope capability"} {
		if strings.Contains(w.out(), forbidden) {
			t.Fatalf("the scopes surface offers %q; the vocabulary is ruled at three", forbidden)
		}
	}
}

func TestSearchWithNoTermsIsAUsageError(t *testing.T) {
	w := newSearchWorld(t)
	w.run(t, nil, nil)
	if w.code != cli.ExitUsage {
		t.Fatalf("exit %d, want %d", w.code, cli.ExitUsage)
	}
	if strings.TrimSpace(w.all()) == "" {
		t.Fatalf("a usage error must not be silent")
	}
}
