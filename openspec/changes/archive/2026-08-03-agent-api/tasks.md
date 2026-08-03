# Tasks

Every box below is ticked because it happened. Nothing is ticked for being intended.

## The surface

- [x] `internal/agentapi` — request, response, outcome and the closed list of operations, with the
      scope each one needs mapped onto the ruled three and nothing else.
- [x] `Answer` — authority established before anything is read or written, then the operation.
- [x] Tickets, drafts, hub material, one hub note, draft revision, publication, model state, grant
      issuance and grant revocation.
- [x] A grant ledger in the local store, with revocation as a field, and grant ids from
      `crypto/rand`.
- [x] `internal/daemon/agent.go` — the sources wired to the same functions the CLI calls, and the
      one exported way in.
- [x] A request line on the control socket, with a connection that says nothing still served the
      status report.
- [x] `omw agent` — renders the answer and returns the outcome's exit code; decides nothing.

## Called rather than re-derived

- [x] `hub.CanRead`, through `hub.Store.ListReadable` and `hub.Store.Read`, is the only visibility
      decision. Nothing in `internal/agentapi` inspects a visibility's shape.
- [x] `hub.EvaluateGrantRequest` decides every grant request; `hub.Permits` decides every scope
      check; `hub.ReadThrough`, `hub.PublishThrough` carry the grant.
- [x] `daemonLiveness` is the only daemon question the command asks. No probe, no socket path
      outside `internal/daemon`.
- [x] No fourth scope. Issuing and revoking a grant need none.

## Driven, not asserted in isolation

- [x] Tickets and drafts obtained from BOTH the agent API (over a real socket, from a real binary,
      against a real daemon) and the CLI, and compared.
- [x] Hub notes obtained from BOTH the agent API and `omw note search` over one hub store, and
      compared — two different code paths onto the same predicate.
- [x] Two hubs differing only by a restricted-away note compared BYTE-FOR-BYTE.
- [x] Every operation swept for the credential in its succeeding, refusing and undetermined forms,
      with a control that fails if the sweep examined nothing.
- [x] The three outcomes checked never to share an exit code, including the zero value.

## Mutations, each confirmed red naming the defect, each reverted

- [x] The hub read returns every note id instead of the readable ones.
- [x] A visibility refusal rendered as "no such note".
- [x] The daemon's ticket source drops a ticket, so the agent and the CLI diverge.
- [x] The control API opens without confirming owner-only.
- [x] Revocation stops taking effect.
- [x] The credential written into a diagnostic field.
- [x] A second visibility rule planted in `internal/agentapi`, to prove the structural search fires.

## Not done, and stated rather than left to be inferred

- [x] Recorded in the pull request: there is still no hub transport in this build, so the hub-backed
      comparison is in-process; criterion 11's off-host unreachability is structural (AF_UNIX plus
      the AST guard) and is not driven by an actual off-host connection attempt.
- [x] Recorded in the pull request: sign-in is Issue #19's, so who the person is and what they hold
      comes from the environment today.

## After qa's refusal of #48 — every field says what is actually known

- [x] Every response leaves `Answer` through one funnel that stamps the facts true of the request
      whatever happened in it. Refusals carry the person; they used to claim none was configured.
- [x] `HubState` split into `HubConfigured` (a fact about the machine, knowable for a refused
      request, read without dialling) and `HubContacted` (a fact about this request).
- [x] `hubContactedSet`, because tri's zero is Undetermined and that is the wrong default for "was
      a hub contacted" — nothing contacted is a determined no. A test corrected a comment of mine
      that said the opposite.
- [x] The undetermined-note count is absent when nothing was examined, rather than a literal zero
      claiming work that was not done.
- [x] The discriminator driven: the same refusal with the person set and unset must differ. Driven
      against a build that hardcodes a name, which a weaker assertion would have passed.
- [x] Every refusing path swept for the same property, with a control that fails if the sweep
      exercised nothing.

## After the merged tree went red — one answer for the model

- [x] `agentapi.ModelView` deleted. The agent API serves `model.View`, takes the combined answer
      from `model.Config.Configured`, and renders through `model.View.Render`.
- [x] `internal/daemon/agent.go` no longer names `OMW_MODEL`, `OMW_MODEL_KEY` or
      `OMW_MODEL_KEY_FILE`; it calls `model.Read`.
- [x] The criterion 13 structural test reflects over the type `Response.Model` actually serves,
      rather than parsing this package for a struct that no longer exists.
- [x] A bug of mine fixed by consuming the one answer: a missing credential file is a determined
      no, not undetermined. The undetermined fixture now stages a file that is present and
      unreadable.

## After qa's second refusal of #48 — the store a person could not read

- [x] `agentSources` keeps `store.Open`'s three outcomes three. A store that opened, a store that
      is not there (determined: the environment is the whole configuration), and a store that would
      not open (undetermined) are three branches, where the third used to pass a nil store to
      `model.Read` — which documents nil as "this caller has no store", a determined fact.
- [x] The undetermined branch carries the CLI's own two sentences, not a fourth vocabulary: the
      store's path, the error, and "An unreadable store is not one with no model recorded in it."
      Both surfaces render through `model.View.Render` and both land on exit 3.
- [x] Driven through the agent API surface, not through `internal/model` in isolation, with real
      stores from `store.Create` in every arm: readable-with-nothing-configured and unreadable no
      longer produce the same bytes, and `agentapi.Answer` no longer answers `OutcomeOK` for the
      second.
- [x] A third arm — a store with a provider recorded in it — because two arms alone do not notice a
      seam that ignores the store entirely. It is what kills qa's surviving mutation M3.
- [x] `internal/agentapi`'s fixture reads its own real store instead of passing nil, so no test in
      that package leaves the store off the model path either.
- [ ] The binary-level CLI/control-API agreement test still has no unreadable-store configuration.
      `omw daemon status` answers that condition through `modelViewFor`, which is Issue #68's
      collapse and not this branch's seam; adding the case here would go red on #68's defect. Left
      for #68, deliberately, rather than widened into this branch.
