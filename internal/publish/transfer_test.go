package publish

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

const author = hub.PersonID("ada")

// publishScopes is a caller who asked for the publish grant on purpose (PRD §3.10).
var publisher = []hub.Scope{hub.ScopeRead, hub.ScopePublish}

// client is one person's machine: an outbox and the ledger beside it.
type client struct {
	l *Ledger
	o *drafts.Outbox
}

func newClient(t *testing.T) *client {
	t.Helper()
	root := t.TempDir()
	o, err := drafts.Create(filepath.Join(root, "outbox"))
	if err != nil {
		t.Fatalf("creating the outbox this test drives against: %v", err)
	}
	l, err := OpenLedger(filepath.Join(root, LedgerDirName))
	if err != nil {
		t.Fatalf("creating the ledger this test drives against: %v", err)
	}
	return &client{l: l, o: o}
}

func (c *client) state(id hub.NoteID) Report { return StateOf(c.l, c.o, id) }

func draft(t *testing.T, c *client, id hub.NoteID, body string) {
	t.Helper()
	if _, err := c.o.Revise(id, body); err != nil {
		t.Fatalf("writing draft %q: %v", id, err)
	}
}

type testHub struct {
	addr  string
	store *hub.Store
	once  *hub.Once
}

// socketPath keeps the path short. A unix socket path has a hard length limit (104 bytes on macOS)
// and a t.TempDir() under a long test name overruns it — which fails as "invalid argument" and
// looks like a bug in the product rather than in the test.
func socketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "omwpub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func newHub(t *testing.T, opts ...ServeOption) *testHub {
	t.Helper()
	h := &testHub{addr: socketPath(t, "hub.sock"), store: hub.NewStore(nil), once: hub.NewOnce()}
	ln, err := Listen(h.addr)
	if err != nil {
		t.Fatalf("opening the hub endpoint: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go Serve(ln, h.store, h.once, opts...)
	return h
}

func transfer(c *client, id hub.NoteID, addr string, scopes []hub.Scope) Result {
	return Transfer(c.l, c.o, id, Config{HubAddr: addr, Author: author, Scopes: scopes, Title: "a title"})
}

// assertExactlyOneContainer is PRD §3.11's invariant, checked against the DISK and the HUB rather
// than against what Transfer said it did.
//
// It asserts both halves separately so that a failure names which one broke: a note in both places
// and a note in neither are different defects with different causes, and a single "want 1" would
// report them identically.
func assertExactlyOneContainer(t *testing.T, c *client, h *testHub, id hub.NoteID) {
	t.Helper()
	r := c.state(id)
	if r.Known != tri.Yes {
		t.Fatalf("%s: where the note stands could not be read (%s); the invariant cannot be checked and that is itself a failure", id, r.Why)
	}

	dir, err := c.o.DraftDir(id)
	if err != nil {
		t.Fatal(err)
	}
	_, derr := os.Stat(dir)
	draftOnDisk := derr == nil

	// IN FLIGHT IS EXEMPT FROM THE HUB HALF, AND THE EXEMPTION IS THE HONEST READING.
	//
	// Criterion 2 says "at no point after a COMPLETED publish attempt". An interrupted attempt is
	// not a completed one, and there is a physically unavoidable state after it: the hub stored the
	// note and the client was killed before it learned so. No amount of care on this machine can
	// remove that state — the two halves are on different sides of a link that went down.
	//
	// What the client owes in that state is not a false certainty; it is to SAY SO. So the note is
	// in the outbox, its published answer is undetermined rather than "no", and a retry resolves it
	// to exactly one copy — which is criterion 5 and is asserted by the tests that produce this
	// state. Asserting "exactly one" here instead would require the client to guess, and the guess
	// is what makes the second copy.
	if r.State == StateInFlight {
		if !draftOnDisk {
			t.Errorf("%s: an in-flight note has no draft in the outbox", id)
		}
		if r.Published() != tri.Undetermined {
			t.Errorf("%s: an in-flight note answers published=%v; it must not claim to know", id, r.Published())
		}
		if r.Container() != ContainerOutbox {
			t.Errorf("%s: an in-flight note is reported as being on the hub", id)
		}
		return
	}

	onHub := false
	if r.State == StatePublished {
		if _, rerr := h.store.Read(r.HubID, author); rerr != nil {
			t.Errorf("%s: this client reports it published as %q and the hub cannot read it back: %v", id, r.HubID, rerr)
		} else {
			onHub = true
		}
	} else if h.store.Count() != 0 {
		// The client says it is not published. The hub had better agree, or the note is in both.
		for _, hid := range h.store.IDs() {
			n, rerr := h.store.Read(hid, author)
			if rerr == nil && n.Author == author {
				t.Errorf("%s: this client reports state %q while the hub holds note %q by the same author — the note is in BOTH containers",
					id, r.State, hid)
				onHub = true
			}
		}
	}

	inOutbox := r.Container() == ContainerOutbox
	switch {
	case inOutbox && onHub:
		t.Errorf("%s: the note is in the outbox AND on the hub. PRD §3.11: never both.", id)
	case !inOutbox && !onHub:
		t.Errorf("%s: the note is in neither container. PRD §3.11: never neither.", id)
	}
	if inOutbox && !draftOnDisk {
		t.Errorf("%s: this client says the note is in the outbox and there is no draft at %s — the outbox listing would be empty for it, which criterion 3 calls a defect", id, dir)
	}
	if !inOutbox && draftOnDisk {
		t.Errorf("%s: this client says the note is on the hub and the draft is still at %s", id, dir)
	}
}

// ---------------------------------------------------------------------------
// Criteria 1, 2 — the happy path, and exactly one container
// ---------------------------------------------------------------------------

func TestAPublishedNoteIsOnTheHubAndGoneFromTheOutbox(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "quota", "the quota is four hundred")

	res := transfer(c, "quota", h.addr, publisher)
	if res.Attempt != AttemptPublished {
		t.Fatalf("attempt = %v, want published (detail: %s, code: %s)", res.Attempt, res.Detail, res.Code)
	}
	if !res.Fresh {
		t.Errorf("the first publication of a note reports itself as not fresh")
	}
	assertExactlyOneContainer(t, c, h, "quota")

	// CRITERION 1's TWO HALVES, ASSERTED SEPARATELY. Retrievable from the hub…
	n, err := h.store.Read(res.Report.HubID, author)
	if err != nil {
		t.Fatalf("the published note is not retrievable from the hub: %v", err)
	}
	if n.Latest().Body != "the quota is four hundred" {
		t.Errorf("the hub holds %q", n.Latest().Body)
	}
	// …and absent from an outbox listing.
	ids, err := c.o.Drafts()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == "quota" {
			t.Errorf("the outbox still lists %q after a successful publish", id)
		}
	}
}

// CRITERION 2 driven across all three completed outcomes in one test, because the criterion is
// about the three of them and a per-outcome test cannot state "at no point".
func TestEveryCompletedOutcomeLeavesTheNoteInExactlyOnePlace(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		c, h := newClient(t), newHub(t)
		draft(t, c, "n", "body")
		transfer(c, "n", h.addr, publisher)
		assertExactlyOneContainer(t, c, h, "n")
	})
	t.Run("refused", func(t *testing.T) {
		c, h := newClient(t), newHub(t)
		draft(t, c, "n", "body")
		// Refused for want of the publish scope — a real refusal from the hub, not a simulated one.
		res := transfer(c, "n", h.addr, []hub.Scope{hub.ScopeRead})
		if res.Attempt != AttemptRefused {
			t.Fatalf("attempt = %v, want refused", res.Attempt)
		}
		assertExactlyOneContainer(t, c, h, "n")
	})
	t.Run("unreachable", func(t *testing.T) {
		c, h := newClient(t), newHub(t)
		draft(t, c, "n", "body")
		res := transfer(c, "n", socketPath(t, "nothing-listens-here.sock"), publisher)
		if res.Attempt != AttemptUnreachable {
			t.Fatalf("attempt = %v, want unreachable", res.Attempt)
		}
		assertExactlyOneContainer(t, c, h, "n")
	})
}

// CRITERION 3. Each unsuccessful outcome leaves the note listed, intact, and re-publishable — the
// last of which is proved by publishing it afterwards and finding it on the hub.
func TestAFailedPublishLeavesTheNoteListedIntactAndRepublishable(t *testing.T) {
	const body = "  content with leading space and a trailing newline\n"
	for name, addr := range map[string]func(t *testing.T, h *testHub) string{
		"unreachable hub": func(t *testing.T, h *testHub) string { return socketPath(t, "dead.sock") },
		"no hub":          func(t *testing.T, h *testHub) string { return "" },
	} {
		t.Run(name, func(t *testing.T) {
			c, h := newClient(t), newHub(t)
			draft(t, c, "n", body)
			transfer(c, "n", addr(t, h), publisher)

			ids, err := c.o.Drafts()
			if err != nil {
				t.Fatal(err)
			}
			if len(ids) != 1 || ids[0] != "n" {
				t.Fatalf("the outbox lists %v after a failed publish; the note must still be listed", ids)
			}
			vs, err := c.o.Timeline("n", "")
			if err != nil {
				t.Fatalf("the draft's content is gone: %v", err)
			}
			if got := vs[len(vs)-1].Body; got != body {
				t.Errorf("the draft's content changed: %q", got)
			}
			if res := transfer(c, "n", h.addr, publisher); res.Attempt != AttemptPublished {
				t.Errorf("the note is not re-publishable after a failed attempt: %v (%s)", res.Attempt, res.Detail)
			}
		})
	}
}

// A refusal has to be re-publishable too, and it is the interesting one: the journal is left
// behind, and a retry must reuse its key rather than being blocked by it.
func TestARefusedNoteIsRepublishableOnceTheReasonNoLongerHolds(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "n", "body")
	if res := transfer(c, "n", h.addr, []hub.Scope{hub.ScopeRead}); res.Attempt != AttemptRefused {
		t.Fatalf("attempt = %v, want refused", res.Attempt)
	}
	if res := transfer(c, "n", h.addr, publisher); res.Attempt != AttemptPublished {
		t.Fatalf("after being granted the publish scope the note is still not publishable: %v (%s)", res.Attempt, res.Detail)
	}
	if h.store.Count() != 1 {
		t.Errorf("the hub holds %d notes after a refusal and a successful retry; want 1", h.store.Count())
	}
	assertExactlyOneContainer(t, c, h, "n")
}

// ---------------------------------------------------------------------------
// Criterion 5 — a retry does not make a second copy
// ---------------------------------------------------------------------------

// TestARetryOfAnAttemptTheHubReceivedProducesOneNote drives the exact interruption criterion 5
// names: the hub DID receive it, and the client never learned so.
//
// The interruption is produced by closing the connection from the hub after the note is stored and
// before the answer is written — which is what a dropped wifi link looks like from here — rather
// than by faking the client's belief.
func TestARetryOfAnAttemptTheHubReceivedProducesOneNote(t *testing.T) {
	var swallow atomic.Bool
	swallow.Store(true)
	h := &testHub{addr: socketPath(t, "hub.sock"), store: hub.NewStore(nil), once: hub.NewOnce()}
	ln, err := Listen(h.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go Serve(ln, h.store, h.once, AfterPublish(func(hub.NoteID) bool {
		// The note is stored; on the first attempt the answer never leaves. Returning false drops
		// the connection, which is exactly what the client sees when a link goes down mid-reply.
		return !swallow.Swap(false)
	}))

	c := newClient(t)
	draft(t, c, "n", "body")

	first := transfer(c, "n", h.addr, publisher)
	if first.Attempt != AttemptUndetermined {
		t.Fatalf("attempt = %v, want undetermined — the hub acted and never answered (detail: %s)", first.Attempt, first.Detail)
	}
	if got := c.state("n"); got.State != StateInFlight {
		t.Fatalf("state after the drop = %q, want %q", got.State, StateInFlight)
	}
	if h.store.Count() != 1 {
		t.Fatalf("this test is not exercising what it claims: the hub holds %d notes and should hold the one it stored before dropping", h.store.Count())
	}

	second := transfer(c, "n", h.addr, publisher)
	// THE COUNT IS THE CRITERION, so it is asserted FIRST. An assertion about what the retry called
	// itself, checked first, hides the count behind a wording failure.
	if got := h.store.Count(); got != 1 {
		t.Fatalf("after an interrupted publish and a retry the hub holds %d notes matching this draft; criterion 5 says 1", got)
	}
	if second.Attempt != AttemptAlreadyPublished {
		t.Fatalf("the retry reports %v, want already-published (detail: %s)", second.Attempt, second.Detail)
	}
	assertExactlyOneContainer(t, c, h, "n")
}

// CRITERION 14: the undetermined state is RESOLVABLE, and resolving it does not need the hub to
// have received the first attempt. Here it did not.
func TestAnUndeterminedNoteResolvesToPublishedWithoutASecondCopy(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "n", "body")

	// An attempt that was sent to a listener that closes without answering: nothing was stored.
	silent := socketPath(t, "silent.sock")
	sln, err := Listen(silent)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := sln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			_, _ = c.Read(buf) // read the request, answer nothing
			c.Close()
		}
	}()
	res := transfer(c, "n", silent, publisher)
	sln.Close()
	if res.Attempt != AttemptUndetermined {
		t.Fatalf("attempt = %v, want undetermined (detail: %s)", res.Attempt, res.Detail)
	}
	if got := c.state("n"); got.State != StateInFlight {
		t.Fatalf("state = %q, want %q", got.State, StateInFlight)
	}

	if res := transfer(c, "n", h.addr, publisher); res.Attempt != AttemptPublished {
		t.Fatalf("resolving an undetermined note gives %v (%s)", res.Attempt, res.Detail)
	}
	if h.store.Count() != 1 {
		t.Fatalf("the hub holds %d notes; want 1", h.store.Count())
	}
	assertExactlyOneContainer(t, c, h, "n")
}

// ---------------------------------------------------------------------------
// Criteria 8, 9, 18 — a refusal and an unreachable hub are different facts
// ---------------------------------------------------------------------------

// TestARefusalAndAnUnreachableHubDifferMachineCheckably drives BOTH against the same draft and
// compares the two answers to EACH OTHER, which is what criterion 8 asks for. Asserting each
// against a literal passes just as happily after the two have been edited to the same wording.
func TestARefusalAndAnUnreachableHubDifferMachineCheckably(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "n", "body")

	unreachable := transfer(c, "n", socketPath(t, "dead.sock"), publisher)
	refused := transfer(c, "n", h.addr, []hub.Scope{hub.ScopeRead})

	if unreachable.Attempt == refused.Attempt {
		t.Fatalf("an unreachable hub and a refusal both report attempt %v", refused.Attempt)
	}
	if unreachable.Code == refused.Code {
		t.Errorf("both answers carry the code %q; a caller cannot branch on it", refused.Code)
	}
	if unreachable.Report.State == StateRefused {
		t.Errorf("an unreachable hub put the note into state %q — criterion 8 forbids exactly this", StateRefused)
	}
	if refused.Report.State != StateRefused {
		t.Errorf("a hub refusal left the note in state %q", refused.Report.State)
	}
	// CRITERION 9: after the unreachable attempt the note is neither published nor refused, and it
	// is still in the outbox.
	if s := unreachable.Report.State; s == StatePublished || s == StateRefused {
		t.Errorf("after an unreachable-hub attempt the state is %q", s)
	}
	if !unreachable.Report.InOutbox() {
		t.Errorf("after an unreachable-hub attempt the note is not in the outbox")
	}
	// And the renderings differ as strings, which is what a person sees.
	if unreachable.Report.Render() == refused.Report.Render() {
		t.Errorf("the two renderings are identical:\n%s", refused.Report.Render())
	}
}

// CRITERION 7: refused says why, and the reason survives to the listing.
func TestARefusalCarriesTheHubsReasonAndItSurvivesToTheListing(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "n", "body")
	res := transfer(c, "n", h.addr, []hub.Scope{hub.ScopeRead})
	if res.Attempt != AttemptRefused {
		t.Fatalf("attempt = %v", res.Attempt)
	}
	if strings.TrimSpace(res.Report.Reason) == "" {
		t.Fatal("the refusal carries no reason; criterion 7 calls that a defect")
	}
	// SURVIVES THE PROCESS. Read it back from the disk, not from the value we were just handed.
	back := c.state("n")
	if back.State != StateRefused {
		t.Fatalf("re-read state = %q", back.State)
	}
	if back.Reason != res.Report.Reason {
		t.Errorf("the reason did not survive: wrote %q, read %q", res.Report.Reason, back.Reason)
	}
	if !strings.Contains(back.Render(), back.Reason) {
		t.Errorf("the rendering does not carry the reason:\n%s", back.Render())
	}
	if back.Code != hub.ErrPublishScopeRequired.Code {
		t.Errorf("code = %q, want %q", back.Code, hub.ErrPublishScopeRequired.Code)
	}
}

// CRITERION 17 AND 18. Both under-scoped tokens are refused, distinguishably from success and from
// an unreachable hub, and the note stays put.
func TestNeitherReadNorWriteAloneCanPublish(t *testing.T) {
	c, h := newClient(t), newHub(t)
	unreachable := func() Result {
		draft(t, c, "u", "body")
		return transfer(c, "u", socketPath(t, "dead.sock"), publisher)
	}()

	for _, held := range [][]hub.Scope{{hub.ScopeRead}, {hub.ScopeWrite}, {hub.ScopeRead, hub.ScopeWrite}} {
		name := "held:" + strings.Join(scopeNames(held), "+")
		t.Run(name, func(t *testing.T) {
			id := hub.NoteID("n-" + strings.Join(scopeNames(held), "-"))
			draft(t, c, id, "body")
			res := Transfer(c.l, c.o, id, Config{HubAddr: h.addr, Author: author, Scopes: held, Title: "t"})
			if res.Attempt != AttemptRefused {
				t.Fatalf("attempt = %v, want refused (detail: %s)", res.Attempt, res.Detail)
			}
			if res.Report.State == StatePublished {
				t.Fatalf("a token without the publish scope published")
			}
			if !res.Report.InOutbox() {
				t.Errorf("the note left the outbox on a refusal")
			}
			// DISTINGUISHABLE FROM AN UNREACHABLE HUB by the same machine-checkable means.
			if res.Code == unreachable.Code {
				t.Errorf("a scope refusal and an unreachable hub share the code %q", res.Code)
			}
			if res.Report.State == unreachable.Report.State {
				t.Errorf("a scope refusal and an unreachable hub leave the same state %q", res.Report.State)
			}
			if res.Code != hub.ErrPublishScopeRequired.Code {
				t.Errorf("code = %q, want %q — the refusal must name the missing scope", res.Code, hub.ErrPublishScopeRequired.Code)
			}
		})
	}
	if h.store.Count() != 0 {
		t.Errorf("the hub holds %d notes after only under-scoped attempts", h.store.Count())
	}
}

func scopeNames(s []hub.Scope) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = string(v)
	}
	return out
}

// A FOURTH SCOPE IS NOT INVENTED HERE. The vocabulary is ruled at three, and the guard against
// quietly growing it is that anything outside it is refused rather than accepted.
func TestAScopeOutsideTheVocabularyIsRefusedAndPublishesNothing(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "n", "body")
	res := Transfer(c.l, c.o, "n", Config{HubAddr: h.addr, Author: author, Scopes: []hub.Scope{"publish-notes"}, Title: "t"})
	if res.Attempt != AttemptRefused {
		t.Fatalf("attempt = %v, want refused", res.Attempt)
	}
	if res.Code != hub.ErrUnknownScope.Code {
		t.Errorf("code = %q, want %q", res.Code, hub.ErrUnknownScope.Code)
	}
	if h.store.Count() != 0 {
		t.Errorf("a request naming an unknown scope published something")
	}
}

// ---------------------------------------------------------------------------
// Criteria 10, 11, 12 — with no hub configured
// ---------------------------------------------------------------------------

// TestWithNoHubConfiguredNoConnectionIsAttemptedAtAll is criterion 10 as stated: ZERO attempts, not
// an attempt that fails fast.
//
// It is driven two ways because either alone is weak. The counter proves no call was made through
// the client's only dialling seam; the listener proves nothing arrived at a real socket. A counter
// alone would miss a dial written somewhere else, and a listener alone would miss a dial to an
// address the test did not think of.
func TestWithNoHubConfiguredNoConnectionIsAttemptedAtAll(t *testing.T) {
	var dials atomic.Int64
	prev := dialHub
	dialHub = func(addr string) (net.Conn, error) {
		dials.Add(1)
		return prev(addr)
	}
	t.Cleanup(func() { dialHub = prev })

	// A REAL LISTENER THAT NOTHING SHOULD REACH. It counts accepts.
	var accepts atomic.Int64
	addr := socketPath(t, "watch.sock")
	ln, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			c.Close()
		}
	}()

	c := newClient(t)
	draft(t, c, "n", "body")
	res := Transfer(c.l, c.o, "n", Config{HubAddr: "", Author: author, Scopes: publisher, Title: "t"})

	if res.Attempt != AttemptNoHub {
		t.Fatalf("attempt = %v, want no-hub (detail: %s)", res.Attempt, res.Detail)
	}
	if got := dials.Load(); got != 0 {
		t.Errorf("with no hub configured the client made %d outbound connection attempt(s); criterion 10 says zero", got)
	}
	if got := accepts.Load(); got != 0 {
		t.Errorf("with no hub configured %d connection(s) arrived at a listening socket", got)
	}
	// A CONTROL. If the seam is not the seam, this test proves nothing — so prove the counter counts.
	if _, _, _ = send(addr, Request{Attempt: "probe"}); dials.Load() != 1 {
		t.Fatalf("the dial counter did not observe a dial through the client's own seam; this test's zero above means nothing")
	}
}

// CRITERION 11: named, and distinguishable from BOTH of the other two.
func TestNoHubConfiguredIsDistinguishableFromARefusalAndFromAnUnreachableHub(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "a", "body")
	draft(t, c, "b", "body")
	draft(t, c, "c", "body")

	noHub := Transfer(c.l, c.o, "a", Config{HubAddr: "", Author: author, Scopes: publisher, Title: "t"})
	unreachable := transfer(c, "b", socketPath(t, "dead.sock"), publisher)
	refused := transfer(c, "c", h.addr, []hub.Scope{hub.ScopeRead})

	codes := map[string]string{"no hub": noHub.Code, "unreachable": unreachable.Code, "refused": refused.Code}
	seen := map[string]string{}
	for name, code := range codes {
		if code == "" {
			t.Errorf("%s carries no code, so nothing can branch on it", name)
		}
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share the code %q", other, name, code)
		}
		seen[code] = name
	}
	if noHub.Code != hub.ErrNoHubConfigured.Code {
		t.Errorf("no-hub code = %q, want %q — it must NAME what is missing", noHub.Code, hub.ErrNoHubConfigured.Code)
	}
	if noHub.Report.State == StateRefused {
		t.Errorf("no hub configured reported the note as refused")
	}
}

// CRITERION 12: unchanged, drafted, and no partial state left behind (PRD §4.4).
func TestWithNoHubTheNoteIsUntouched(t *testing.T) {
	c := newClient(t)
	draft(t, c, "n", "body")
	before := treeOf(t, c, "n")

	res := Transfer(c.l, c.o, "n", Config{HubAddr: "  ", Author: author, Scopes: publisher, Title: "t"})
	if res.Attempt != AttemptNoHub {
		t.Fatalf("attempt = %v", res.Attempt)
	}
	if res.Report.State != StateDrafted {
		t.Errorf("state = %q, want %q", res.Report.State, StateDrafted)
	}
	after := treeOf(t, c, "n")
	if len(before) != len(after) {
		t.Errorf("the draft directory changed:\n  before %v\n  after  %v", keysOf(before), keysOf(after))
	}
	for name, body := range before {
		if after[name] != body {
			t.Errorf("%s changed", name)
		}
	}
	// No journal was written: a publication that never began must leave no record that one did.
	if _, present, err := c.l.read("n"); err != nil || present {
		t.Errorf("a publication record exists after a no-hub attempt (present=%v, err=%v)", present, err)
	}
}

func treeOf(t *testing.T, c *client, id hub.NoteID) map[string]string {
	t.Helper()
	dir, err := c.o.DraftDir(id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		out[e.Name()] = string(b)
	}
	return out
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Local refusals
// ---------------------------------------------------------------------------

func TestPublishingADraftThatIsNotThereSendsNothing(t *testing.T) {
	c, h := newClient(t), newHub(t)
	res := transfer(c, "nope", h.addr, publisher)
	if res.Attempt != AttemptLocalFailure {
		t.Fatalf("attempt = %v, want a local failure", res.Attempt)
	}
	if res.Report.Exists != tri.No {
		t.Errorf("Exists = %v; that there is no such draft is a determined answer", res.Report.Exists)
	}
	if h.store.Count() != 0 {
		t.Errorf("something was published for a draft that does not exist")
	}
}

// A DAMAGED RECORD IS NOT `drafted`. This is the lie the journal exists to prevent: publishing
// again on the strength of a record we could not read is how the second copy gets made.
func TestADamagedPublicationRecordIsUndeterminedAndStopsTheTransfer(t *testing.T) {
	c, h := newClient(t), newHub(t)
	draft(t, c, "n", "body")
	p, err := c.l.pathFor("n")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := c.state("n")
	if got.Known != tri.Undetermined {
		t.Fatalf("StateOf = %+v; a damaged record must not resolve to a state", got)
	}
	if got.State == StateDrafted {
		t.Errorf("a damaged record reads as %q", StateDrafted)
	}
	res := transfer(c, "n", h.addr, publisher)
	if res.Attempt != AttemptLocalFailure {
		t.Errorf("attempt = %v; a note whose record cannot be read must not be sent", res.Attempt)
	}
	if h.store.Count() != 0 {
		t.Errorf("a note whose publication record could not be read was published anyway")
	}
}

// An unknown phase is a record we cannot read, not a fifth state.
func TestAnUnknownPhaseIsUndeterminedAndNotAFifthState(t *testing.T) {
	c := newClient(t)
	draft(t, c, "n", "body")
	if err := c.l.write("n", record{Attempt: "attempt-x", Phase: "halfway"}); err != nil {
		t.Fatal(err)
	}
	got := c.state("n")
	if got.Known != tri.Undetermined {
		t.Fatalf("StateOf = %+v", got)
	}
	for _, s := range States() {
		if got.State == s {
			t.Errorf("an unknown phase resolved to the known state %q", s)
		}
	}
}

// Reconcile finishes the deletion a killed process left, and leaves everything else alone.
func TestReconcileFinishesAPublishedDraftAndTouchesNothingElse(t *testing.T) {
	c := newClient(t)
	draft(t, c, "done", "body")
	draft(t, c, "resting", "body")
	draft(t, c, "flying", "body")
	if err := c.l.write("done", record{Attempt: "a", Phase: phasePublished, HubID: "note-abc"}); err != nil {
		t.Fatal(err)
	}
	if err := c.l.write("flying", record{Attempt: "b", Phase: phaseInFlight}); err != nil {
		t.Fatal(err)
	}
	finished, err := Reconcile(c.l, c.o)
	if err != nil {
		t.Fatal(err)
	}
	if len(finished) != 1 || finished[0] != "done" {
		t.Fatalf("Reconcile finished %v, want [done]", finished)
	}
	ids, err := c.o.Drafts()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("the outbox holds %v after reconciling; want resting and flying", ids)
	}
	if StateOf(c.l, c.o, "flying").State != StateInFlight {
		t.Errorf("reconcile disturbed an in-flight note")
	}
	if StateOf(c.l, c.o, "resting").State != StateDrafted {
		t.Errorf("reconcile disturbed a resting draft")
	}
}
