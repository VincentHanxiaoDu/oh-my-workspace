package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
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
func openControl(p runPaths, snapshot func() Report, confirm func(string) (tri.Value, string)) (*Control, tri.Value, string, error) {
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

	c := &Control{listener: ln, path: p.socket, snapshot: snapshot, done: make(chan struct{})}
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

// answer writes one report and closes.
//
// ONE REQUEST, ONE ANSWER, NO PROTOCOL. The only question this interface answers today is "what is
// your state" (§4.3), and a request format is a thing to get wrong before there is a second
// question to ask. Issue #16's agent API is where a real protocol belongs.
func (c *Control) answer(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(controlDialTimeout))
	rep := c.snapshot()
	body, err := json.Marshal(rep)
	if err != nil {
		return
	}
	_, _ = conn.Write(body)
}

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
	_ = conn.SetReadDeadline(time.Now().Add(controlDialTimeout))
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
