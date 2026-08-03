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
	// ModelEnv names the model provider (PRD §3.13).
	ModelEnv = "OMW_MODEL"
	// ModelKeyEnv holds the credential. NOTHING READS ITS VALUE ONTO A RESPONSE — the agent API's
	// model view has no field for one. This constant exists so that the test which scans every
	// response for the credential knows what to look for.
	ModelKeyEnv = "OMW_MODEL_KEY"
	// ModelKeyFileEnv points at a file holding the credential. It is supported because it is the
	// case that produces criterion 15's third answer honestly: a file that cannot be read means
	// whether a credential is configured could not be determined, which is neither yes nor no.
	ModelKeyFileEnv = "OMW_MODEL_KEY_FILE"
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
	src.Model = func() agentapi.ModelView { return modelView(getenv) }
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

// modelView answers whether a model is configured, in three values, and never with the credential.
//
// CRITERION 13 IS A PROPERTY OF THE TYPE, NOT OF THIS FUNCTION'S CARE. agentapi.ModelView has no
// field a credential could be assigned to. What this function decides is only which of the three
// answers is true, and criterion 15's third one is real here: a key FILE that cannot be read means
// we did not determine whether a credential is configured.
func modelView(getenv func(string) string) agentapi.ModelView {
	provider := strings.TrimSpace(getenv(ModelEnv))
	keyFile := strings.TrimSpace(getenv(ModelKeyFileEnv))
	hasInline := strings.TrimSpace(getenv(ModelKeyEnv)) != ""

	switch {
	case keyFile != "":
		if _, err := os.ReadFile(keyFile); err != nil {
			// UNDETERMINED, AND THE REASON NAMES THE FILE AND NOT ITS CONTENTS.
			return agentapi.ModelView{
				Configured: tri.Undetermined, Provider: provider,
				Detail: "the credential file named by " + ModelKeyFileEnv + " could not be read, so whether a credential is configured was not determined",
			}
		}
		return agentapi.ModelView{Configured: tri.Yes, Provider: provider}
	case hasInline:
		return agentapi.ModelView{Configured: tri.Yes, Provider: provider}
	case provider != "":
		// A provider with no credential is a DETERMINED no on the credential, and the provider name
		// is still worth reporting: §3.13's "everything that does need one says what is missing".
		return agentapi.ModelView{
			Configured: tri.No, Provider: provider,
			Detail: "a provider is named and no credential is configured for it",
		}
	default:
		// CRITERION 14's OTHER HALF: no model configured is a determined answer, not a broken
		// client, and tickets and drafts keep working around it.
		return agentapi.ModelView{Configured: tri.No, Detail: "no model provider is configured on this machine"}
	}
}

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
