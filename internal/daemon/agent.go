// The agent API, served over the control API (PRD §2.1, §3.12, §4.6; Issue #16).
//
// WHY IT IS SERVED FROM THIS PACKAGE AND NOWHERE ELSE. §3.12 says the agent API "reaches the daemon
// over the control API, not over the network". `internal/daemon` is the only package in the tree
// permitted to derive or name a control socket path — TestNoPackageOutsideDaemonDerivesAControlSocketPath
// enforces that over `internal/`, and it has already caught four branches. So the listener stays
// here, the dialler stays here, and everything else asks through [Ask].
//
// WHY THERE IS NO SECOND TRANSPORT. There is no second transport. An AF_UNIX socket has no address
// an off-machine packet can name, which is what makes criterion 11 a property of the transport
// rather than of an allow-list somebody has to keep correct; and criterion 12 falls out of
// openControl's existing order — a control API that declined to open is a control API nothing is
// listening on, so the agent API does not serve, and [Ask] reports that as its own distinguishable
// failure rather than as an absent daemon.
//
// WHAT THIS FILE DOES NOT DECIDE. It decides no visibility and no authority. It builds
// [agentapi.Sources] out of the SAME functions the CLI calls — inbox.List for tickets, the outbox
// for drafts — and hands them to agentapi.Answer, which calls hub.CanRead and
// hub.EvaluateGrantRequest. A second implementation that agrees today is the hazard Issue #12's
// package comment names, and the way not to write one is to have nothing here to write it in.

package daemon

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/agentapi"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// The environment this build reads for the agent API's identity and configuration.
//
// PERSON AND SCOPES ARE ENVIRONMENT TODAY BECAUSE SIGN-IN IS ISSUE #19'S, and this Issue must not
// decide it. What #16 owns is that whatever the person's authority turns out to be, the agent's
// cannot exceed it — which is [hub.EvaluateGrantRequest], called with whatever [Holder] this
// resolves to. When #19 lands, this function is what it replaces; the rule above it does not move.
const (
	// PersonEnv names the person the daemon serves.
	//
	// UNSET IS NOT A REFUSAL AND NOT AN ANONYMOUS EVERYONE. hub.CanRead answers Undetermined for an
	// unidentified reader, so with no person configured the hub half reports undetermined and the
	// local half still works — which is the honest answer and also §4.4's.
	PersonEnv = "OMW_PERSON"
	// ScopesEnv is what the PERSON holds. Default `read`: Issue #16's `## Ruled` says everyday use
	// is `read` alone, and §3.10 requires that `publish` was asked for on purpose.
	ScopesEnv = "OMW_SCOPES"
	// OutboxEnv overrides where the outbox lives. Default is `outbox` inside the store.
	OutboxEnv = "OMW_OUTBOX"
	// HubEnv configures a hub. Read only to report whether one is configured and to decide between
	// "no hub" and "unreachable"; this build has no hub transport and says so.
	HubEnv = "OMW_HUB"
)

// DefaultOutboxDir is where the outbox lives inside a store.
const DefaultOutboxDir = "outbox"

// agentHubSource is how the daemon reaches a hub.
//
// THERE IS STILL NO TRANSPORT IN THIS BUILD, AND THIS SAYS SO RATHER THAN PRETENDING — the same
// shape and the same honesty as `omw note`'s noteSource. A configured hub this build cannot talk to
// is UNREACHABLE, which is criterion 7's undetermined outcome and never an empty list of notes.
// Tests replace it to drive the hub paths against an in-memory store.
var agentHubSource = func(getenv func(string) string) (*hub.Store, hub.Membership, error) {
	if strings.TrimSpace(getenv(HubEnv)) == "" {
		return nil, nil, hub.ErrNoHubConfigured
	}
	return nil, nil, hub.ErrHubUnreachable
}

// agentSources builds everything agentapi.Answer is allowed to look at, for one store.
//
// It is rebuilt per request rather than captured once, so that a ticket written a second ago is in
// the answer and a grant revoked a second ago is not.
func agentSources(storeRoot string, getenv func(string) string) agentapi.Sources {
	if getenv == nil {
		getenv = os.Getenv
	}
	person := hub.PersonID(strings.TrimSpace(getenv(PersonEnv)))
	scopes := personScopes(getenv)

	src := agentapi.Sources{Person: person, PersonScopes: scopes}

	s, storeErr := store.Open(storeRoot)
	if storeErr == nil {
		src.Grants = agentapi.NewStoreGrants(s)
		// THE SAME FUNCTION `omw inbox list` CALLS. Not a copy of it, not a query that filters the
		// same way: inbox.List, over the same store. That is what criterion 1's equality is built
		// out of, rather than asserted about.
		src.Tickets = func() ([]inbox.Ticket, error) { return inbox.List(s) }
	}

	dir := strings.TrimSpace(getenv(OutboxEnv))
	if dir == "" {
		dir = filepath.Join(storeRoot, DefaultOutboxDir)
	}
	src.Drafts = func() ([]agentapi.DraftView, error) { return listDrafts(dir) }
	src.ReviseDraft = func(id, body string) (agentapi.DraftView, error) { return reviseDraft(dir, id, body) }
	// READ, NEVER DIALLED. Whether a hub is configured is a fact about this machine, so it is
	// answerable for a refused request too — which is what stops a refusal reporting the hub as
	// unknown on a machine that plainly has none (§4.4, criterion 17).
	src.HubConfigured = func() tri.Value {
		if strings.TrimSpace(getenv(HubEnv)) == "" {
			return tri.No
		}
		return tri.Yes
	}
	src.Hub = func() (*hub.Store, hub.Membership, error) { return agentHubSource(getenv) }
	// AN UNREADABLE STORE IS NOT A STORE WITH NO MODEL IN IT — the same sentence the CLI already
	// prints, because it is the same fact (criterion 18; §4.3).
	//
	// model.Read takes a nil store to mean "this caller has no store", which is a DETERMINED fact
	// and renders as "no provider is chosen". That reading is correct for a machine with no store,
	// and it is a lie for a machine whose store is there and would not open: passing nil in that
	// case turns a failure to determine into a confident negative, which is Issue #68's collapse
	// and exactly what §4.3 forbids. So the three outcomes of store.Open stay three here.
	//
	// THE THREE ARMS THEMSELVES LIVE IN modelConfigFrom, which the control API's Report also calls
	// (Issue #68 was that same collapse on that surface). The wording and the exit status are one
	// definition rather than two that agree today: this file used to hold its own copy, and a copy
	// is how the CLI and the control API drifted apart to begin with.
	src.Model = func() model.Config { return modelConfigFrom(storeRoot, s, storeErr, getenv) }
	return src
}

// personScopes reads what the person holds, defaulting to `read` alone.
//
// AN UNPARSEABLE LIST YIELDS NOTHING, NOT EVERYTHING. A configuration nobody can read is not a
// grant of authority, and the failure mode of the other choice is a person's agent quietly holding
// `publish`.
func personScopes(getenv func(string) string) []hub.Scope {
	raw := strings.TrimSpace(getenv(ScopesEnv))
	if raw == "" {
		return []hub.Scope{hub.ScopeRead}
	}
	scopes, err := agentapi.ParseScopes(raw)
	if err != nil {
		return nil
	}
	return scopes
}

func listDrafts(dir string) ([]agentapi.DraftView, error) {
	o, err := drafts.Open(dir)
	if err != nil {
		// NOT AN EMPTY OUTBOX. drafts.Open refuses a directory with no marker precisely so that
		// "you pointed me at your home directory" is not served as "you have no drafts".
		return nil, agentapi.ErrNoOutbox
	}
	ids, err := o.Drafts()
	if err != nil {
		return nil, err
	}
	out := make([]agentapi.DraftView, 0, len(ids))
	for _, id := range ids {
		out = append(out, draftView(o, id))
	}
	return out, nil
}

func reviseDraft(dir, id, body string) (agentapi.DraftView, error) {
	o, err := drafts.Open(dir)
	if err != nil {
		// §4.2: omw does not create an outbox behind a write any more than behind a read.
		return agentapi.DraftView{}, agentapi.ErrNoOutbox
	}
	if _, err := o.Revise(hub.NoteID(id), body); err != nil {
		return agentapi.DraftView{}, err
	}
	return draftView(o, hub.NoteID(id)), nil
}

// draftView renders one draft. State is ALWAYS "drafted" and Published is ALWAYS false, because the
// outbox holds exactly the unpublished (PRD §3.11, §2.3) — criterion 2.
func draftView(o *drafts.Outbox, id hub.NoteID) agentapi.DraftView {
	v := agentapi.DraftView{ID: string(id), State: agentapi.DraftedState, Published: false}
	if vs, err := o.Timeline(id, ""); err == nil && len(vs) > 0 {
		v.Revisions = len(vs)
		v.Latest = vs[len(vs)-1].Body
	}
	return v
}

// The model configuration is internal/model's answer, and this file does not have one.
//
// Issue #18 owns "is a model configured": model.Read reads the environment and the store record,
// answers both halves in three values, and opens no connection. This file used to read OMW_MODEL,
// OMW_MODEL_KEY and OMW_MODEL_KEY_FILE itself and build its own view — green on this branch and red
// the moment it met main, because internal/model has a structural test that permits exactly one
// file to name those variables. It was right to fail: two readings of the same configuration is how
// `omw model show` and the agent API come to disagree about whether a person has a model.

// ---------------------------------------------------------------------------
// The wire
// ---------------------------------------------------------------------------

// controlOp names which question a control connection is asking.
type controlOp string

const (
	// opStatus is the daemon's own state — the only question the control API answered before this
	// Issue, and still the default for a connection that says nothing.
	opStatus controlOp = "status"
	// opAgent is an agent API request, carried in Payload.
	opAgent controlOp = "agent"
)

// controlRequest is one line on the control socket.
//
// A LINE OF JSON, TERMINATED BY A NEWLINE, AND THE SERVER READS EXACTLY ONE. The framing is the
// smallest thing that can carry two questions; the alternative — a connection whose meaning depends
// on how long the server waits before giving up on a request — makes a slow machine look like a
// different question.
type controlRequest struct {
	Op      controlOp       `json:"op"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// serveAgent answers one agent API request against this daemon's store.
func (c *Control) serveAgent(payload []byte) []byte {
	var req agentapi.Request
	if err := json.Unmarshal(payload, &req); err != nil {
		body, _ := agentapi.MarshalResponse(agentapi.Undetermined("", agentapi.ErrLocalUndetermined))
		return body
	}
	resp := agentapi.Answer(req, agentSources(c.storeRoot, os.Getenv))
	body, err := agentapi.MarshalResponse(resp)
	if err != nil {
		body, _ = agentapi.MarshalResponse(agentapi.Undetermined(req.Op, agentapi.ErrLocalUndetermined))
	}
	return body
}

// Ask sends one agent API request to the daemon holding storeRoot and returns its answer.
//
// IT IS THE ONLY WAY IN, AND IT NAMES NO SOCKET TO ITS CALLER. The path comes from socketFor, which
// is this package's, so no surface outside `internal/daemon` reproduces the rule — the defect
// Issue #41 removed, restated for a new surface before it could happen a fifth time.
//
// THE THREE FAILURES IT CAN REPORT ARE THREE FAILURES (criteria 10, 12, and §4.3):
//
//	(nil, ErrDaemonNotRunning)      no daemon holds this store. Ask does NOT start one (§4.2).
//	(nil, ErrControlAPINotOpen)     a daemon holds it and its control API declined to open because
//	                                owner-only permissions could not be confirmed (§4.6). The agent
//	                                API therefore does not serve, and this is not an absent daemon.
//	(nil, ErrHubUnreachable-ish)    the socket is there and nothing answered — undetermined.
func Ask(storeRoot string, req agentapi.Request) (agentapi.Response, error) {
	rep := Inspect(storeRoot)
	switch rep.Running {
	case tri.Yes:
	case tri.No:
		// SAID, NEVER FIXED BY STARTING IT (§4.2, criterion 10).
		return agentapi.Response{}, hub.ErrDaemonNotRunning
	default:
		return agentapi.Response{}, hub.Refusedf(hub.ErrUndetermined,
			"whether a daemon holds %s could not be determined: %s", storeRoot, rep.HealthDetail)
	}
	if rep.Control != tri.Yes {
		// CRITERION 12. The daemon is running and its control API is not open, so the agent API
		// does not serve — a different fact from an absent daemon, with a different code.
		return agentapi.Response{}, hub.Refusedf(agentapi.ErrControlAPINotOpen, "%s", rep.ControlDetail)
	}

	_, socket := socketFor(storeRoot)
	payload, err := json.Marshal(req)
	if err != nil {
		return agentapi.Response{}, err
	}
	line, err := json.Marshal(controlRequest{Op: opAgent, Payload: payload})
	if err != nil {
		return agentapi.Response{}, err
	}
	conn, err := net.DialTimeout("unix", socket, controlDialTimeout)
	if err != nil {
		return agentapi.Response{}, hub.Refusedf(agentapi.ErrControlAPINotOpen,
			"the control socket for %s could not be reached: %v", storeRoot, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(controlDialTimeout))
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return agentapi.Response{}, err
	}
	body, err := io.ReadAll(conn)
	if err != nil || len(body) == 0 {
		// THE SOCKET WAS THERE AND NOTHING CAME BACK. Undetermined territory, never a refusal and
		// never an empty successful answer.
		return agentapi.Response{}, hub.Refusedf(hub.ErrUndetermined, "%v: %v", errControlSilent, err)
	}
	// UnmarshalResponse is what turns the wire's TEXT forms of the three-valued fields back into
	// values. Decoding straight into the struct would leave every tri.Value at its zero — which is
	// Undetermined, so it would fail safe, but it would also silently discard a determined answer.
	return agentapi.UnmarshalResponse(body)
}
