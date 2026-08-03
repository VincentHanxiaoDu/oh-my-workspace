package agentapi

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"

// The refusals this surface can produce that package hub does not already have a code for.
//
// THEY ARE *hub.Error VALUES ON PURPOSE. [hub.Code] walks wrapped errors with errors.As, so a
// caller reads one kind of code whether the refusal came from the hub's visibility rule or from
// this surface's grant handling, and no surface has to know which layer refused it.
//
// EVERY ONE OF THEM IS A CODE, NOT A SENTENCE. Issue #16's criteria 6, 7, 8, 12 and 15 all have the
// shape "these two outcomes must be distinguishable", and prose is what drifts. Codes are the
// contract; TestEveryAgentAPIRefusalIsDistinguishable asserts they are pairwise distinct from each
// other AND from the hub codes this surface also emits.
var (
	// ErrUnknownOperation — the request named an operation the agent API does not have.
	ErrUnknownOperation = &hub.Error{Code: "unknown-agent-operation", Msg: "refused: the agent API has no such operation"}

	// ErrNoGrant — the request presented no grant at all.
	//
	// Distinct from ErrUnknownGrant: "you did not say which authority you are acting under" is
	// fixed differently from "the authority you named is not one I know".
	ErrNoGrant = &hub.Error{Code: "no-grant-presented", Msg: "refused: the request presented no grant, and the agent API acts under a grant or not at all"}

	// ErrUnknownGrant — no grant by that id was ever issued.
	ErrUnknownGrant = &hub.Error{Code: "unknown-grant", Msg: "refused: no grant by that id was issued on this machine"}

	// ErrGrantRevoked — the grant was issued and has since been revoked (PRD §3.10, criterion 9).
	//
	// ITS OWN CODE, NOT ErrUnknownGrant'S. A person who revoked an agent's authority and then sees
	// "unknown grant" cannot tell whether the revocation took effect or whether they mistyped the
	// id. Criterion 9 is about the revocation being observable, so the observation has a name.
	ErrGrantRevoked = &hub.Error{Code: "grant-revoked", Msg: "refused: that grant has been revoked, and a request that succeeded before the revocation does not keep a later one alive"}

	// ErrGrantUndetermined — whether the grant is live could not be worked out. NEVER a refusal
	// dressed as a revocation, and never a success: §4.3, at the authority layer.
	ErrGrantUndetermined = &hub.Error{Code: "grant-undetermined", Msg: "whether that grant is live could not be determined, which is neither a live grant nor a revoked one"}

	// ErrScopeNotGranted — the person holds the scope, but this agent's grant does not carry it.
	//
	// DISTINCT FROM hub.ErrGrantWiderThanHolder, and criterion 6 names both halves separately: "a
	// scope that its person does not hold, OR that was not granted to the agent". The first is the
	// §4.5 rule and belongs to hub.EvaluateGrantRequest; the second is this one, and it is fixable
	// by asking for a wider grant while the first is not.
	ErrScopeNotGranted = &hub.Error{Code: "scope-not-granted", Msg: "refused: that scope was not granted to this agent"}

	// ErrWriteScopeRequired — the write path with no `write` scope. Criterion 8: `read` confers
	// neither `write` nor `publish`, and the refusal leaves the draft unchanged.
	//
	// Package hub has ErrReadScopeRequired and ErrPublishScopeRequired because those are the two
	// scopes its own surfaces gate on; the local write path is this Issue's, so its code is here.
	ErrWriteScopeRequired = &hub.Error{Code: "write-scope-required", Msg: "refused: changing a draft requires the write scope, and the read scope does not confer it"}

	// ErrNoOutbox — there is no outbox on this machine, which is a determined fact and not an empty
	// list of drafts (§4.2: omw does not create one behind a read).
	ErrNoOutbox = &hub.Error{Code: "no-outbox-here", Msg: "no draft outbox on this machine, which is not the same as having no drafts"}

	// ErrLocalUndetermined — a local read (tickets, drafts) could not be completed. Not an empty
	// inbox, not an empty outbox.
	ErrLocalUndetermined = &hub.Error{Code: "local-read-undetermined", Msg: "the local store could not be read, so this is not an empty answer — nothing was established"}

	// ErrControlAPINotOpen — §4.6 and the Platforms ruling, criterion 12. Owner-only socket
	// permissions could not be confirmed, so the control API did not open and the agent API does
	// not serve.
	//
	// IT IS NOT hub.ErrDaemonNotRunning. A daemon may be running perfectly and still have declined
	// to open its control API; telling the person to start the daemon in that case sends them to
	// fix the wrong thing. The two codes and the two exit codes both differ.
	ErrControlAPINotOpen = &hub.Error{Code: "control-api-not-open", Msg: "the control API did not open because owner-only socket permissions could not be confirmed, so the agent API does not serve"}
)

// agentAPIErrors is every error this package defines, for the distinguishability test.
var agentAPIErrors = []*hub.Error{
	ErrUnknownOperation, ErrNoGrant, ErrUnknownGrant, ErrGrantRevoked, ErrGrantUndetermined,
	ErrScopeNotGranted, ErrWriteScopeRequired, ErrNoOutbox, ErrLocalUndetermined,
	ErrControlAPINotOpen,
}

// hubErrorsThisSurfaceEmits are the package hub codes an agent API response can carry. They are
// listed so the distinguishability test can check across both sets: a code this package invented
// that collided with one of these would be invisible to a test that only looked at its own list.
var hubErrorsThisSurfaceEmits = []*hub.Error{
	hub.ErrNoSuchNote, hub.ErrRefused, hub.ErrUndetermined, hub.ErrHubUnreachable,
	hub.ErrNoHubConfigured, hub.ErrDaemonNotRunning, hub.ErrGrantWiderThanHolder,
	hub.ErrUnknownScope, hub.ErrReadScopeRequired, hub.ErrPublishScopeRequired,
}
