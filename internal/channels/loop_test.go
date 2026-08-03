package channels

import (
	"sync"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// mailbox is a channel somebody is delivering to, one message at a time, while the test runs.
//
// IT IS THE FAKE, AND IT IS THE ONLY FAKE. Everything between it and the store — grouping,
// refusing acknowledgements, writing tickets, recording what happened — is the real code, driven by
// the real daemon's real Serve loop. What it stands in for is a Teams or IMAP client, which this
// build does not have (see [Builtin]) and which a test may not have either: a test that reached a
// real service would be a test about somebody's account.
type mailbox struct {
	mu   sync.Mutex
	msgs []Message
}

func (m *mailbox) deliver(ms ...Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, ms...)
}

func (m *mailbox) factory(Connection) (Adapter, error) {
	return AdapterFunc(func(Connection) ([]Message, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return append([]Message(nil), m.msgs...), nil
	}), nil
}

// withLoopFactory points the DAEMON'S ingestion at a mailbox for the duration of one test.
func withLoopFactory(t *testing.T, f Factory) {
	t.Helper()
	prev := LoopFactory
	LoopFactory = f
	t.Cleanup(func() { LoopFactory = prev })
}

// runningDaemon starts a real daemon against root and stops it when the test ends.
func runningDaemon(t *testing.T, root string) *daemon.Daemon {
	t.Helper()
	d, err := daemon.Start(daemon.Options{StorePath: root, Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("starting a daemon against %s: %v", root, err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Serve() }()
	t.Cleanup(func() {
		d.Close()
		<-done
	})
	return d
}

func waitForTickets(t *testing.T, s *store.Store, want int) []inbox.Ticket {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last []inbox.Ticket
	for time.Now().Before(deadline) {
		ts, err := inbox.List(s)
		if err == nil {
			last = ts
			if len(ts) >= want {
				return ts
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited 10s for %d ticket(s); the store holds %d", want, len(last))
	return nil
}

// =================================================================================================
// CRITERION 4 — with the daemon running, ingestion is continuous and nobody types anything.
// =================================================================================================

func TestWithTheDaemonRunningANewMessageBecomesATicketWithNoCommandTyped(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	var box mailbox
	withLoopFactory(t, box.factory)

	// The daemon starts against an EMPTY mailbox, so the ticket below cannot be a leftover of the
	// first pass — it can only come from a pass that happened after the message arrived.
	runningDaemon(t, s.Path())
	time.Sleep(100 * time.Millisecond)
	if got, err := inbox.List(s); err != nil || len(got) != 0 {
		t.Fatalf("the inbox was not empty before the message was delivered: %v %d", err, len(got))
	}

	// A message arrives. NOTHING IS TYPED: no ingest command exists, and this test calls none.
	box.deliver(Message{
		ID: "m1", From: "ana@example.com", Subject: "Login broken",
		Body: "I cannot sign in since this morning, can you reset it?", At: day, Thread: "T1",
	})

	tickets := waitForTickets(t, s, 1)
	if len(tickets) != 1 {
		t.Fatalf("want 1 ticket, got %d", len(tickets))
	}
	title, ok := tickets[0].Title.Value()
	if !ok || title == "" {
		t.Errorf("the ticket ingestion produced has no written title")
	}
}

// The registration itself, so that a build in which the loop was never handed to the daemon fails
// here with a sentence rather than by mysteriously ingesting nothing.
func TestIngestionIsRegisteredAsWorkTheDaemonDoes(t *testing.T) {
	for _, b := range daemon.Backgrounds() {
		if b.Name == BackgroundName {
			if b.Run == nil {
				t.Fatal("ingestion is registered with nothing to run")
			}
			return
		}
	}
	t.Fatalf("ingestion is not registered as the daemon's work, so it is not a property of the "+
		"daemon running (criterion 4). Registered: %v", daemon.Backgrounds())
}

// =================================================================================================
// CRITERION 5 — with the daemon not running, ingestion does not happen.
// =================================================================================================

func TestWithTheDaemonStoppedNoTicketAppearsHoweverMuchArrives(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	var box mailbox
	withLoopFactory(t, box.factory)

	// Traffic is delivered with no daemon anywhere. Nothing is watching, so nothing happens.
	box.deliver(oneMatter()...)
	before := treeOf(t, s)
	time.Sleep(3 * IngestInterval)
	if got, err := inbox.List(s); err != nil || len(got) != 0 {
		t.Fatalf("with no daemon running, %d ticket(s) appeared (err %v) — ingestion is a property "+
			"of the daemon running (criterion 5)", len(got), err)
	}
	if after := treeOf(t, s); !sameTree(before, after) {
		t.Errorf("with no daemon running the store changed:\nbefore %v\nafter  %v", before, after)
	}
}

// AND THE OTHER DIRECTION: a daemon that is stopped stops ingesting. Without this, criterion 5
// passes on a build that never ingests at all.
func TestStoppingTheDaemonStopsIngestion(t *testing.T) {
	s := newStore(t)
	connected(t, s, "email-a", KindEmail)
	var box mailbox
	withLoopFactory(t, box.factory)

	d, err := daemon.Start(daemon.Options{StorePath: s.Path(), Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("starting a daemon: %v", err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Serve() }()

	box.deliver(oneMatter()...)
	waitForTickets(t, s, 1)

	d.Close()
	<-done

	// A SECOND, UNRELATED MATTER arrives after the daemon has gone. It must produce nothing.
	box.deliver(Message{
		ID: "x1", From: "kim@example.com", Subject: "Printer jammed",
		Body: "The third-floor printer is jammed again, can you look?", At: day, Thread: "T9",
	})
	time.Sleep(3 * IngestInterval)
	got, err := inbox.List(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("after the daemon stopped, the inbox holds %d tickets; it held 1 when the daemon "+
			"was stopped, and nothing may be ingested while it is down (criterion 5)", len(got))
	}
}

// treeOf snapshots every record in the store, so "the store is unchanged" is an assertion about the
// store and not about one listing of it.
func treeOf(t *testing.T, s *store.Store) map[string]int {
	t.Helper()
	out := map[string]int{}
	kinds, err := s.Kinds()
	if err != nil {
		t.Fatalf("listing kinds: %v", err)
	}
	for _, k := range kinds {
		recs, lerr := s.List(k)
		if lerr != nil {
			t.Fatalf("listing %s: %v", k, lerr)
		}
		out[string(k)] = len(recs)
	}
	return out
}

func sameTree(a, b map[string]int) bool {
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
