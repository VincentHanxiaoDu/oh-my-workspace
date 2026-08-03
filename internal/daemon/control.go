package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// controlDialTimeout bounds a control API query. A daemon that has stopped answering must produce
// an UNDETERMINED control state promptly rather than hanging the CLI: a status command that never
// returns is silence, and silence is not one of the three answers (§4.3).
const controlDialTimeout = 2 * time.Second

// Control is the daemon's local-only control interface (PRD §2.1, §4.6).
//
// IT IS A UNIX DOMAIN SOCKET, AND THAT IS NOT AN IMPLEMENTATION DETAIL. Criterion 21 requires that
// a connection originating off the machine cannot reach the daemon; an AF_UNIX socket has no
// address an off-machine packet can name, so unreachability is a property of the transport rather
// than of an allow-list somebody has to keep correct. Criterion 24 requires that a refusal to open
// does not fall back to another transport, and the way that is guaranteed is that this package
// contains no other transport at all.
type Control struct {
	mu       sync.Mutex
	listener net.Listener
	path     string
	snapshot func() Report
	done     chan struct{}
	// storeRoot is the store this control API is about. The agent API (Issue #16) is served
	// against it, and it is held here rather than re-resolved per request so that a connection
	// cannot end up answering about a different store than the one the socket belongs to.
	storeRoot string
}

// openControl brings the control API up, or declines and says why.
//
// THE ORDER OF THE CONFIRMATIONS IS THE FEATURE (criterion 22, §4.6):
//
//  1. The containing directory is confirmed owner-only BEFORE anything is listening. A socket
//     inside a directory nobody else may traverse cannot be connected to by anybody else, whatever
//     the socket's own mode turns out to be — so doing this first means there is no instant at
//     which a reachable endpoint exists while the confirmation is still pending.
//  2. Only then is the socket created, and immediately tightened.
//  3. The socket itself is then confirmed. On anything other than a confirmed yes the listener is
//     closed and the socket removed, so a declined control API leaves nothing behind that a later
//     run or a later reader could mistake for an open one.
//
// A tri.Undetermined confirmation refuses exactly as a tri.No does, because §4.6 says the API
// opens on a confirmation, and "could not confirm" is not one. The two are still reported
// differently: the returned error's message carries which of them happened.
func openControl(p runPaths, storeRoot string, snapshot func() Report, confirm func(string) (tri.Value, string)) (*Control, tri.Value, string, error) {
	if confirm == nil {
		confirm = confirmOwnerOnly
	}
	if err := ensureOwnerOnlyDir(p.socketDir); err != nil {
		return nil, tri.Undetermined, err.Error(), fmt.Errorf("%w: %v", ErrControlNotOwnerOnly, err)
	}
	if state, why := confirm(p.socketDir); state != tri.Yes {
		detail := controlRefusalDetail(state, why, "the directory the socket would live in")
		return nil, state, detail, fmt.Errorf("%w: %s", ErrControlNotOwnerOnly, detail)
	}

	// A socket left behind by a process that is gone would make Listen fail with "address already
	// in use". We hold the store's lock by the time this runs, so nothing else can be listening
	// here, and removing it is safe rather than a race with a live daemon.
	_ = os.Remove(p.socket)

	ln, err := net.Listen("unix", p.socket)
	if err != nil {
		return nil, tri.Undetermined, err.Error(),
			fmt.Errorf("%w: the socket could not be created: %v", ErrControlNotOwnerOnly, err)
	}
	if err := os.Chmod(p.socket, ownerOnlyFile); err != nil {
		ln.Close()
		_ = os.Remove(p.socket)
		return nil, tri.Undetermined, err.Error(),
			fmt.Errorf("%w: the socket's permissions could not be set: %v", ErrControlNotOwnerOnly, err)
	}
	if state, why := confirm(p.socket); state != tri.Yes {
		ln.Close()
		_ = os.Remove(p.socket)
		detail := controlRefusalDetail(state, why, "the control socket")
		return nil, state, detail, fmt.Errorf("%w: %s", ErrControlNotOwnerOnly, detail)
	}

	c := &Control{listener: ln, path: p.socket, storeRoot: storeRoot, snapshot: snapshot, done: make(chan struct{})}
	go c.serve()
	return c, tri.Yes, "", nil
}

// controlRefusalDetail words a refusal so that a person can tell WHICH of the two negatives
// happened. A determined "other users can reach this" is a finding; an undetermined one is the
// absence of a finding, and §4.3 will not let them share a sentence.
func controlRefusalDetail(state tri.Value, why, what string) string {
	switch state {
	case tri.No:
		return fmt.Sprintf("owner-only access to %s was checked and is NOT in place, so the control API did not open: %s", what, why)
	default:
		return fmt.Sprintf("owner-only access to %s %s, so the control API did not open: %s", what, tri.Undetermined, why)
	}
}

func (c *Control) serve() {
	defer close(c.done)
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			return
		}
		go c.answer(conn)
	}
}

// answer reads one request line, writes one answer, and closes.
//
// ONE REQUEST, ONE ANSWER. The framing arrived with Issue #16, which is the second question this
// interface has to answer and therefore the first point at which a request format earns its keep —
// the note this function used to carry said exactly that. It is a line of JSON so that the server
// can read a bounded amount and know it is done, rather than deciding what a connection means by
// how long it waited.
//
// A CONNECTION THAT SAYS NOTHING STILL GETS THE STATUS REPORT. That is not politeness: `Inspect`
// dials this socket to find out whose store a running daemon holds, and a framing change that made
// an older binary's dial hang would turn a version skew into silence, which is not one of the three
// answers.
func (c *Control) answer(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(controlDialTimeout))

	req := controlRequest{Op: opStatus}
	line, err := bufio.NewReader(io.LimitReader(conn, maxControlRequest)).ReadBytes('\n')
	if err == nil || len(line) > 0 {
		var parsed controlRequest
		if json.Unmarshal(line, &parsed) == nil && parsed.Op != "" {
			req = parsed
		}
	}

	switch req.Op {
	case opAgent:
		_, _ = conn.Write(c.serveAgent(req.Payload))
	default:
		rep := c.snapshot()
		body, merr := json.Marshal(rep)
		if merr != nil {
			return
		}
		_, _ = conn.Write(body)
	}
}

// maxControlRequest bounds a request line. A peer that can reach this socket is the owner (§4.6),
// so this is not a defence against them — it is a bound so that a malformed write cannot make the
// daemon allocate without limit while it is holding the store's write lock.
const maxControlRequest = 1 << 20

// Path is where the control API listens. Empty when it is not open.
func (c *Control) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Close stops the control API and removes its socket.
func (c *Control) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listener == nil {
		return
	}
	_ = c.listener.Close()
	<-c.done
	_ = os.Remove(c.path)
	c.listener = nil
}

// queryControl asks a running daemon for its state over the control API.
//
// It is how the CLI gets criterion 14 right: the bytes the daemon serialised are the bytes the CLI
// renders, so there is no second computation of the state to disagree with the first.
func queryControl(socket string) (Report, error) {
	var rep Report
	conn, err := net.DialTimeout("unix", socket, controlDialTimeout)
	if err != nil {
		return rep, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(controlDialTimeout))
	// ASKED FOR EXPLICITLY. The server reads one request line; a client that wrote nothing would
	// wait out the read deadline before being served the status it wanted, turning every `omw
	// daemon status` into a two-second pause.
	if _, err := conn.Write([]byte(`{"op":"status"}` + "\n")); err != nil {
		return rep, err
	}
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&rep); err != nil {
		return rep, err
	}
	rep.unwire()
	return rep, nil
}

// errControlSilent means the socket is there and the daemon behind it did not answer. Undetermined
// territory, never "closed" (criterion 26).
var errControlSilent = errors.New("the control socket is present and the daemon did not answer on it")
