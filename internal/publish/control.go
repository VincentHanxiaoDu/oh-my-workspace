// The control surface, and criterion 16: "the state a note is in is reported identically through
// the CLI and through the control API".
//
// # How that is guaranteed rather than checked
//
// There is ONE computation — [StateOf] — and two renderings of its result: [Report.Render] for a
// person and [Report.Wire] for a program. The CLI does not compute a state and the control endpoint
// does not compute a state; both are handed the same [Report]. Two surfaces that agree because they
// were both tested against the same expectation today are two surfaces that will disagree the first
// time one of them is edited; two surfaces reading one value cannot.
//
// The test still drives it over a real socket, because "there is one computation" is a claim about
// this file and criterion 16 is a claim about what a person and a script actually receive.
//
// # Why this is not a new verb on Issue #2's daemon control socket
//
// That socket answers exactly one question and has no request format at all — deliberately, and its
// own comment says a real protocol belongs to Issue #16's agent API. Adding a second question to it
// would mean inventing that protocol here, in a branch about publication, and every other Issue
// that wants to ask the daemon something would then inherit it. So this is a small endpoint of its
// own, owner-only like every other local endpoint in this product, and Issue #16 is where the two
// become one. That is recorded in the pull request as a thing this Issue did not settle.
package publish

import (
	"bufio"
	"encoding/json"
	"net"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// StateRequest asks where one note stands.
type StateRequest struct {
	Note string `json:"note"`
}

// ServeState answers state queries on ln until it is closed, from the same outbox the CLI reads.
func ServeState(ln net.Listener, l *Ledger, o *drafts.Outbox) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go answerState(conn, l, o)
	}
}

func answerState(conn net.Conn, l *Ledger, o *drafts.Outbox) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req StateRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
	// THE SAME CALL THE CLI MAKES. If this line ever computes something instead of calling StateOf,
	// criterion 16 has been broken by the edit that did it.
	body, err := json.Marshal(StateOf(l, o, hub.NoteID(req.Note)).Wire())
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
	_, _ = conn.Write(append(body, '\n'))
}

// QueryState asks the control endpoint where a note stands.
func QueryState(addr string, id hub.NoteID) (Wire, error) {
	var w Wire
	conn, err := dialHub(addr)
	if err != nil {
		return w, hub.Refusedf(hub.ErrHubUnreachable, "%s: %v", addr, err)
	}
	defer conn.Close()
	body, err := json.Marshal(StateRequest{Note: string(id)})
	if err != nil {
		return w, err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return w, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(replyTimeout))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return w, hub.Refusedf(hub.ErrUndetermined, "the control endpoint did not answer: %v", err)
	}
	if err := json.Unmarshal(line, &w); err != nil {
		return w, hub.Refusedf(hub.ErrUndetermined, "the control endpoint's answer could not be read: %v", err)
	}
	return w, nil
}
