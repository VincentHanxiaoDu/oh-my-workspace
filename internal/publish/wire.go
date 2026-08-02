// The transfer itself: what goes over the wire, and what carries it.
//
// # The transport is a unix domain socket, and that was not free choice
//
// PRD does not say what the client talks to the hub over. This repository does: every `net.Listen`
// and every `net.Dial` under `internal/` must name "unix" as a literal, checked by an AST walk in
// `internal/commands/network_guard_test.go`, because §4.2 and §4.6 together mean nothing in this
// tree may open a connection that could reach another machine. So `$OMW_HUB` names a unix socket
// path, and a hub on another host is reached through something that terminates at one — which is
// the shape §4.6 already forces on the control API. This is called out in the pull request because
// it is a decision Issue #10 did not make and somebody will need to revisit it.
//
// # One request, one reply, newline-delimited JSON
//
// Not because JSON is good, but because the alternative on the table — reuse Issue #2's control
// protocol, which is "connect and read a blob with no request at all" — cannot carry a request, and
// inventing a framing more clever than a newline would be a thing to get wrong before there is a
// second message to send.
//
// # What the request carries, and what it deliberately does not
//
// It carries the ATTEMPT KEY, the author, the note, and the scopes the caller holds. It does not
// carry a secret: tokens, signatures and expiry are Issue #19's, and a placeholder secret here
// would be a security property this build does not have, written down as if it did. The scopes are
// read from the environment as a stand-in for #19 and are named as such at their declaration.
package publish

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// dialTimeout and replyTimeout bound an attempt.
//
// A HUB THAT NEVER ANSWERS MUST NOT HANG THE COMMAND. Silence is not one of the three answers
// (§4.3), and a publish that never returns is silence with a spinner. When these expire the outcome
// is undetermined and the note is in flight, which is the honest thing rather than the convenient
// one.
const (
	dialTimeout  = 5 * time.Second
	replyTimeout = 30 * time.Second
)

// The outcomes the hub can report. Closed vocabulary; anything else is a hub this build cannot
// understand, which is undetermined and never a refusal.
const (
	outcomePublished = "published"
	outcomeRefused   = "refused"
)

// Request is one publication attempt.
type Request struct {
	// Attempt is the idempotency key. The same value on every retry of the same publication.
	Attempt string `json:"attempt"`
	Author  string `json:"author"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	// Scopes are what the caller holds. Publishing needs `publish`; a caller holding only `read` or
	// only `write` is refused by the hub, distinguishably (PRD §3.10, §4.5).
	Scopes []string `json:"scopes"`
}

// Response is the hub's answer.
type Response struct {
	Outcome string `json:"outcome"`
	NoteID  string `json:"note_id,omitempty"`
	// Fresh reports whether this call created the note. False on a retry the hub had already
	// completed — which is a SUCCESS, and the fact that makes "one copy, not two" observable.
	Fresh  bool   `json:"fresh,omitempty"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Listen opens the hub's endpoint.
//
// The "unix" is a literal and must stay one — see the file comment and the AST guard.
func Listen(path string) (net.Listener, error) { return net.Listen("unix", path) }

// dialHub is how the client reaches the hub. It is a var so that a test can COUNT the attempts:
// criterion 10 asks for zero outbound connection attempts with no hub configured, and "zero
// attempts" is a claim about calls made, which only a counted seam can settle.
var dialHub = func(addr string) (net.Conn, error) { return net.DialTimeout("unix", addr, dialTimeout) }

// send performs one attempt and returns the hub's answer.
//
// The two error returns are DIFFERENT FACTS and the caller must not merge them:
//
//	(resp, false, err)  nothing was sent — the connection was never established
//	(resp, true,  err)  something was sent and the answer was not read
//
// The first leaves the note `drafted`; the second leaves it `in flight`. Collapsing them would
// either lose a note the hub has (reporting drafted when it may be published) or leave a note
// permanently in flight that never left the machine.
func send(addr string, req Request) (resp Response, sent bool, err error) {
	conn, err := dialHub(addr)
	if err != nil {
		return resp, false, hub.Refusedf(hub.ErrHubUnreachable, "%s: %v", addr, err)
	}
	defer conn.Close()

	body, err := json.Marshal(req)
	if err != nil {
		return resp, false, err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
	if _, err := conn.Write(append(body, '\n')); err != nil {
		// A WRITE THAT FAILED PART-WAY IS STILL A SEND. We cannot know how many bytes reached the
		// hub or what it did with them, and assuming none did is the assumption that produces a
		// second copy.
		return resp, true, hub.Refusedf(hub.ErrUndetermined, "the request was partly written and %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(replyTimeout))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		if errors.Is(err, io.EOF) {
			return resp, true, hub.Refusedf(hub.ErrUndetermined, "the hub closed the connection without answering")
		}
		return resp, true, hub.Refusedf(hub.ErrUndetermined, "no answer was read from the hub: %v", err)
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return resp, true, hub.Refusedf(hub.ErrUndetermined, "the hub's answer could not be read: %v", err)
	}
	switch resp.Outcome {
	case outcomePublished:
		if resp.NoteID == "" {
			return resp, true, hub.Refusedf(hub.ErrUndetermined, "the hub reported a publication and named no note")
		}
	case outcomeRefused:
		if resp.Reason == "" {
			// CRITERION 7 DEFENDED AT THE SEAM. A refusal with no reason is a defect, and the place
			// to catch it is where the reason would have come from.
			resp.Reason = missingReason
		}
	default:
		return resp, true, hub.Refusedf(hub.ErrUndetermined, "the hub answered %q, which this build does not understand", resp.Outcome)
	}
	return resp, true, nil
}

// ServeOption configures [Serve].
type ServeOption func(*server)

// AfterPublish runs on the hub after a note has been stored and BEFORE the answer is written.
// Returning false drops the connection without answering.
//
// It exists for one test and says so: the interruption that matters is the one where the hub acted
// and the client never found out, and that window is a microsecond wide unless something holds it
// open. Nothing in the product sets it.
func AfterPublish(f func(hub.NoteID) bool) ServeOption {
	return func(s *server) { s.afterPublish = f }
}

type server struct {
	store        *hub.Store
	once         *hub.Once
	afterPublish func(hub.NoteID) bool
}

// Serve answers publication attempts on ln until it is closed.
//
// It is the hub side, and it is in this package rather than in `internal/hub` because it is
// transport and `internal/hub` deliberately contains none: five Issues build on that package and
// none of them should acquire a dependency on a socket.
func Serve(ln net.Listener, s *hub.Store, once *hub.Once, opts ...ServeOption) {
	srv := &server{store: s, once: once}
	for _, o := range opts {
		o(srv)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go srv.answer(conn)
	}
}

func (s *server) answer(conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(conn, Response{Outcome: outcomeRefused, Code: "unreadable-request", Reason: fmt.Sprintf("the request could not be read: %v", err)})
		return
	}
	scopes := make([]hub.Scope, 0, len(req.Scopes))
	for _, sc := range req.Scopes {
		if !hub.KnownScope(hub.Scope(sc)) {
			s.reply(conn, Response{Outcome: outcomeRefused, Code: hub.ErrUnknownScope.Code,
				Reason: fmt.Sprintf("%q is not in the scope vocabulary", sc)})
			return
		}
		scopes = append(scopes, hub.Scope(sc))
	}
	g := hub.Grant{ID: hub.GrantID(req.Attempt), Holder: hub.PersonID(req.Author), Scopes: scopes}
	note, fresh, perr := hub.PublishOnce(s.store, s.once, g, hub.IdempotencyKey(req.Attempt), hub.Publication{
		Author: hub.PersonID(req.Author),
		Title:  req.Title,
		Body:   req.Body,
	})
	if perr != nil {
		s.reply(conn, Response{Outcome: outcomeRefused, Code: hub.Code(perr), Reason: perr.Error()})
		return
	}
	if s.afterPublish != nil && !s.afterPublish(note.ID) {
		return
	}
	s.reply(conn, Response{Outcome: outcomePublished, NoteID: string(note.ID), Fresh: fresh})
}

func (s *server) reply(conn net.Conn, r Response) {
	body, err := json.Marshal(r)
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
	_, _ = conn.Write(append(body, '\n'))
}
