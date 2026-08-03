# A person's own AI, reading their own material

## Why

The product exists because writing things up is unpaid work nobody does, so the client does it
(PRD §1). It cannot write anything up until it can read. The agent API (§2.1, §3.12) is that read
surface — what a person's own AI reads their tickets, their drafts, and the hub through.

Two things the person is counting on, and both are the point rather than a detail:

- **It cannot see more than they can.** Same authority model as everything else (§4.5). A
  colleague's note restricted away from them is not visible to their AI either — not a narrowed
  view of it, not its title, not the fact it exists (§3.5: visibility is a precondition of ranking).
- **It is local.** It reaches the daemon over the control API (§4.6), not over the network.

And one thing they are counting on *not* happening: the credential they supplied (§3.13) is theirs.
An agent that can enumerate a person's material must not be a path to exfiltrating the key that
pays for the model.

## What this change is not

**It is not a new authority model, and it is not a new transport.** Both of those were available as
the natural thing to build and both would have been wrong:

- The visibility predicate is `hub.CanRead`, reached through `hub.Store.ListReadable` and
  `hub.Store.Read`. Issue #12's package comment names the hazard precisely — "a second
  implementation which agrees today" — and the agent API is exactly where a second one would be
  written, because filtering "just the notes this person can see" looks like a small local loop.
  There is no such loop. A structural test bans this package from inspecting a visibility's shape at
  all, and requires it to be demonstrably calling the hub's entry points.
- The §4.5 rule that a grant wider than its holder is refused *when it is requested* is
  `hub.EvaluateGrantRequest`, called. The scope vocabulary is `read` / `write` / `publish` and
  **there is no fourth** — issuing and revoking a grant on your own machine needs no scope, because
  that is the person acting over a socket that did not open until it was confirmed owner-only. An
  "administer" scope was the specific temptation and it is not taken, on the same reasoning §2.4
  uses for the hub operator: a deployment fact, not a scope.
- The transport is the control API's existing unix socket. `internal/daemon` is the only package
  permitted to name a socket path, so the listener and the dialler both live there and everything
  else asks through one exported call. An AF_UNIX socket has no address an off-host packet can name,
  which makes "not reachable from another machine" a property of the transport rather than of an
  allow-list somebody has to keep correct.

## What changes

- **New `internal/agentapi`** — the whole of the agent API's behaviour as one pure function from a
  request and a set of sources to a response. No net, no socket, no visibility rule of its own.
- **The control API grows a request line.** It answered exactly one question before this change, and
  the note in its own source said a request format belonged with the agent API. A connection that
  says nothing still gets the status report, so an older binary's dial cannot hang.
- **New `omw agent`** — the command a person points their AI at. It renders; it decides nothing.
- **A grant ledger in the local store**, with revocation as a field rather than a deletion, because
  "revoked" and "never issued" are two facts a person needs to tell apart.

## The distinctions this change refuses to collapse

Every one of these is two outcomes that would be one outcome in the obvious implementation:

| | and not |
|---|---|
| no hub configured (determined, §4.4) | the hub could not be reached (undetermined, §3.11) |
| the hub holds nothing you may read | there is no hub |
| this note is not visible to you | there is no such note |
| your grant may not read this | the hub answered and it was empty |
| the person does not hold that scope | this agent was not granted it |
| that grant was revoked | that grant was never issued |
| the daemon is not running | the control API did not open (§4.6) |
| a model is not configured | whether one is configured could not be determined |

## Impact

- `internal/agentapi` (new), `internal/commands/agent_cmd.go` (new).
- `internal/daemon`: a new file for the agent API's wiring and its client, and a request line on the
  control socket. `openControl` takes the store root so a connection cannot answer about a different
  store than the socket belongs to.
- No dependency added. Standard library only.
