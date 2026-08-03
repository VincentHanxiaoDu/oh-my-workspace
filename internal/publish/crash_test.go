package publish

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// WHY THIS FILE SPAWNS A PROCESS AND KILLS IT.
//
// Criterion 4 is about a machine that dies mid-sentence, and PRD §3.11's "never both, never
// neither" is an invariant that has to survive that. A test that calls Transfer and then pokes at
// the files has not tested it: it has tested the code's intentions, in a process that was never
// interrupted, with every buffer flushed by the runtime on the way out.
//
// So this re-invokes the TEST BINARY ITSELF as a child. The child opens the same outbox and ledger,
// announces itself, and begins a publish against a hub the parent controls. The parent waits until
// the hub has STORED the note and is holding the answer, then SIGKILLs the child — which no
// deferred function, no flush and no cleanup handler survives — and asserts, from its own process,
// that the note is in exactly one container and reported as not published.
//
// It then retries from the parent and asserts the hub holds ONE note, which is criterion 5 driven
// through a real interruption rather than a simulated one.
//
// THE TEST ALSO ASSERTS THAT THE KILL LANDED WHERE IT MEANT TO. Without that, a build with no
// atomicity at all could pass by being killed before it opened anything, and the whole exercise
// would be theatre. The parent checks the hub actually stored the note before it pulled the plug.

const (
	crashChildEnv = "OMW_PUBLISH_CRASH_CHILD"
	crashRootEnv  = "OMW_PUBLISH_CRASH_ROOT"
	crashHubEnv   = "OMW_PUBLISH_CRASH_HUB"
	crashNoteEnv  = "OMW_PUBLISH_CRASH_NOTE"
	crashBody     = "the whole body and nothing missing"
)

func TestMain(m *testing.M) {
	if os.Getenv(crashChildEnv) != "" {
		crashChild()
		return
	}
	os.Exit(m.Run())
}

// crashChild runs inside the doomed process. It never returns: the parent kills it.
//
// Every exit below is a failure the parent reports, because a child that exits by itself has not
// been interrupted and the run proved nothing.
func crashChild() {
	root := os.Getenv(crashRootEnv)
	id := hub.NoteID(os.Getenv(crashNoteEnv))
	o, err := drafts.Open(filepath.Join(root, "outbox"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "child: opening the outbox: %v\n", err)
		os.Exit(11)
	}
	l, err := OpenLedger(filepath.Join(root, LedgerDirName))
	if err != nil {
		fmt.Fprintf(os.Stderr, "child: opening the ledger: %v\n", err)
		os.Exit(12)
	}
	fmt.Fprintln(os.Stdout, "READY")
	os.Stdout.Sync()

	res := Transfer(l, o, id, Config{
		HubAddr: os.Getenv(crashHubEnv),
		Author:  author,
		Scopes:  publisher,
		Title:   "a title",
		Gate:    grantingGate{},
	})
	fmt.Fprintf(os.Stderr, "child: the publish RETURNED (%v: %s) — it was supposed to be killed\n", res.Attempt, res.Detail)
	os.Exit(13)
}

// crashRun spawns the child and returns once it has announced itself.
func crashRun(t *testing.T, root, hubAddr string, id hub.NoteID) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	// THE DEVICE POINTER IS SANDBOXED. This child inherits the real environment so that the test
	// binary can find its own runtime; inheriting HOME and XDG_DATA_HOME unchanged is how a suite
	// repoints the developer's real store at a t.TempDir() that is then deleted. Both are set
	// because the pointer resolves from XDG_DATA_HOME first and falls back to HOME.
	sandbox := t.TempDir()
	cmd.Env = append(os.Environ(),
		crashChildEnv+"=1", crashRootEnv+"="+root, crashHubEnv+"="+hubAddr, crashNoteEnv+"="+string(id),
		"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "READY" {
		t.Fatalf("the child never announced itself (%q, %v)", line, err)
	}
	return cmd
}

// crashClient reopens the outbox and ledger from a directory the child was using, from THIS
// process — no caches, no benefit of the doubt.
func crashClient(t *testing.T, root string) *client {
	t.Helper()
	o, err := drafts.Open(filepath.Join(root, "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := OpenLedger(filepath.Join(root, LedgerDirName))
	if err != nil {
		t.Fatal(err)
	}
	return &client{l: l, o: o}
}

func crashRoot(t *testing.T, id hub.NoteID) string {
	t.Helper()
	// NOT t.TempDir(). The child's own unix socket work and the parent's assertions both use this,
	// and a t.TempDir() under a long test name pushes the socket path over the platform limit.
	root, err := os.MkdirTemp("", "omwcrash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	c := &client{}
	o, err := drafts.Create(filepath.Join(root, "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := OpenLedger(filepath.Join(root, LedgerDirName))
	if err != nil {
		t.Fatal(err)
	}
	c.l, c.o = l, o
	draft(t, c, id, crashBody)
	return root
}

// ---------------------------------------------------------------------------
// Criterion 4 and criterion 5, through a real kill
// ---------------------------------------------------------------------------

func TestKillingTheClientAfterTheHubStoredTheNoteLeavesItInExactlyOnePlace(t *testing.T) {
	if testing.Short() {
		t.Skip("this test spawns a process")
	}
	const id = hub.NoteID("interrupted")
	root := crashRoot(t, id)

	stored := make(chan struct{}, 1)
	var hold atomic.Bool
	hold.Store(true)
	h := &testHub{addr: socketPath(t, "hub.sock"), store: hub.NewStore(nil), once: hub.NewOnce()}
	ln, err := Listen(h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go Serve(ln, h.store, h.once, AfterPublish(func(hub.NoteID) bool {
		if hold.Load() {
			// STORED, AND THE ANSWER IS HELD. This is the window criterion 4 is about, propped open
			// so the kill can land inside it.
			select {
			case stored <- struct{}{}:
			default:
			}
			time.Sleep(30 * time.Second)
			return false
		}
		return true
	}))

	cmd := crashRun(t, root, h.addr, id)
	select {
	case <-stored:
	case <-time.After(20 * time.Second):
		t.Fatal("the hub never stored the note, so the kill below would not land mid-transfer and this test would prove nothing")
	}
	// THE KILL LANDED WHERE IT MEANT TO, asserted before pulling the plug.
	if got := h.store.Count(); got != 1 {
		t.Fatalf("the hub holds %d notes at the moment of the kill; this test is not exercising the window it claims", got)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("killing the child: %v", err)
	}
	state, err := cmd.Process.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if state.Exited() {
		t.Fatalf("the child exited by itself (%d) instead of being killed; it was not interrupted", state.ExitCode())
	}

	// FROM A FRESH VIEW OF THE DISK.
	c := crashClient(t, root)
	got := c.state(id)
	if got.Known != tri.Yes {
		t.Fatalf("after the kill the note's state could not be read: %+v", got)
	}
	// CRITERION 4: reported as not published.
	if got.State == StatePublished {
		t.Errorf("an interrupted publish reports the note as published")
	}
	if got.State != StateInFlight {
		t.Errorf("state = %q, want %q — an attempt was sent and its outcome was never learned", got.State, StateInFlight)
	}
	if !got.InOutbox() {
		t.Errorf("the interrupted note is not in the outbox")
	}
	// NEVER NEITHER: the draft is still there, with its content.
	vs, err := c.o.Timeline(id, "")
	if err != nil {
		t.Fatalf("the draft is gone after the kill: %v", err)
	}
	if vs[len(vs)-1].Body != crashBody {
		t.Errorf("the draft's content changed: %q", vs[len(vs)-1].Body)
	}
	// The hub does have a copy. That is the one state an interruption can produce and nothing on
	// this machine can prevent — see the note in assertExactlyOneContainer. What the client owes is
	// to say it does not know, which the assertions above check, and to resolve to ONE copy on the
	// retry, which is next.
	assertExactlyOneContainer(t, c, h, id)

	// CRITERION 5: retry, and count.
	hold.Store(false)
	res := transfer(c, id, h.addr, publisher)
	// THE COUNT FIRST: it is the criterion, and an assertion about the retry's own wording checked
	// before it would hide a second copy behind a naming failure.
	if n := h.store.Count(); n != 1 {
		t.Fatalf("after an interrupted publish and a retry the hub holds %d notes matching that draft; criterion 5 says 1", n)
	}
	if res.Attempt != AttemptAlreadyPublished {
		t.Fatalf("the retry reports %v, want already-published (detail: %s)", res.Attempt, res.Detail)
	}
	assertExactlyOneContainer(t, c, h, id)
	if c.state(id).State != StatePublished {
		t.Errorf("after the retry the note is %q", c.state(id).State)
	}
}

// The other side of the same window: the hub never got it. The note must still be in the outbox
// with its content, and the retry must publish it exactly once.
func TestKillingTheClientBeforeTheHubStoredTheNoteLeavesItInTheOutbox(t *testing.T) {
	if testing.Short() {
		t.Skip("this test spawns a process")
	}
	const id = hub.NoteID("interrupted-early")
	root := crashRoot(t, id)

	arrived := make(chan struct{}, 1)
	addr := socketPath(t, "slow.sock")
	ln, err := Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = conn.Read(make([]byte, 4096))
				select {
				case arrived <- struct{}{}:
				default:
				}
				time.Sleep(30 * time.Second) // never answers, never stores
			}()
		}
	}()

	cmd := crashRun(t, root, addr, id)
	select {
	case <-arrived:
	case <-time.After(20 * time.Second):
		t.Fatal("the request never reached the listener, so the kill would not land mid-transfer")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	state, err := cmd.Process.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if state.Exited() {
		t.Fatalf("the child exited by itself (%d) instead of being killed", state.ExitCode())
	}

	c := crashClient(t, root)
	got := c.state(id)
	if got.State != StateInFlight {
		t.Fatalf("state = %q, want %q", got.State, StateInFlight)
	}
	if got.Published() != tri.Undetermined {
		t.Errorf("published = %v; the client cannot know and must not claim to", got.Published())
	}
	ids, err := c.o.Drafts()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("the outbox lists %v; criterion 3 says the note must still be listed", ids)
	}

	// Resolving it against a hub that never saw it publishes it, exactly once.
	h := newHub(t)
	if res := transfer(c, id, h.addr, publisher); res.Attempt != AttemptPublished {
		t.Fatalf("resolving gives %v (%s)", res.Attempt, res.Detail)
	}
	if n := h.store.Count(); n != 1 {
		t.Fatalf("the hub holds %d notes; want 1", n)
	}
	assertExactlyOneContainer(t, c, h, id)
}
