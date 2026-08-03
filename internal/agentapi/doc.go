// Package agentapi is what a person's own AI reads their private material through (PRD §2.1,
// §3.12; Issue #16).
//
// # It is not a new authority model
//
// PRD §3.12: "It is scoped to that person and nothing wider. The same authority model as everything
// else (§4.5) — an agent cannot read what its person cannot."
//
// So this package DECIDES NOTHING ABOUT VISIBILITY. It calls [hub.CanRead] — through
// [hub.Store.ListReadable] and [hub.Store.Read], which are the two places that call it — and it
// calls [hub.EvaluateGrantRequest] for the §4.5 rule that a grant wider than its holder is refused
// when it is requested. Issue #12's package comment names the hazard exactly: "a second
// implementation which agrees today". There is no second implementation here, and
// TestTheAgentAPIDoesNotReimplementVisibility asserts that structurally.
//
// The scope vocabulary is [hub.Vocabulary] and is exactly three — `read`, `write`, `publish`. This
// package adds no fourth. The hub operator's read-everything is a deployment fact (§2.4), not a
// scope, and nothing here expresses it.
//
// # It is local, and there is no transport in this package
//
// PRD §3.12: "It reaches the daemon over the control API (§4.6), not over the network." This
// package contains no listener, no dialler and no socket path. [Answer] is a pure function from a
// [Request] and a set of [Sources] to a [Response]; `internal/daemon` serves it over the control
// API's unix socket, which is the one place in the tree that owns a socket path, and
// `internal/commands` asks through `internal/daemon`. An off-host connection cannot reach an
// AF_UNIX socket because such a socket has no address an off-host packet can name — criterion 11 is
// a property of the transport, not of an allow-list.
//
// # Three answers, never two
//
// Every [Response] carries an [Outcome] that is one of ok / refused / undetermined, and a stable
// [hub.Error] code. PRD §4.3 and this project's standing rule: `could not determine` and
// `determined to be nothing` share neither a rendering nor an exit code. The four outcomes a hub
// read can have are four distinct codes:
//
//	ok, zero notes                  the hub was read and holds nothing you may read
//	refused, no-hub-configured      there is no hub — a determined fact about this machine (§4.4)
//	undetermined, hub-unreachable   a hub is configured and did not answer (§3.11)
//	refused, read-scope-required    your grant may not read — nothing was looked at
//
// # The credential is not on this surface
//
// PRD §3.13: a key "is not published, not synchronised, and not readable through the agent API".
// [ModelView] carries whether a model is configured as a [tri.Value] and never the value of the
// credential; the model source is handed a reader that reports presence and cannot return the
// secret, so there is no field for a credential to be assigned to.
// TestNoAgentAPIResponseCarriesTheCredential drives every op, including the failing ones, and scans
// the serialised bytes.
package agentapi
