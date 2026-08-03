# Status and the agent API stop rendering unreadable material as a determined answer

## Why

Issue #101, found by driving `main` `ec8f79a` as a product across 210 invocations with 0 FAIL, 0
panics and 0 hangs. Two surfaces answered confidently about material they never opened, and both
pointed the reassuring way.

**Blocker 1 — `omw status` said everything was running over a store it could not read.** With one
ticket record at `chmod 000`, the control and the test were BYTE-IDENTICAL and both exited 0:

```
omw status         everything you have configured is running    local store: [working]
omw status --json  "summary": "all_configured_working"
```

At the same moment, on the same store, five other surfaces disagreed correctly: `omw store status`
exit 1, `omw inbox list` exit 3, `omw ticket list` exit 3, `omw agent tickets` exit 3, and
`omw diagnostics` reported `ticket-inventory undetermined`. `omw status --help` states that exit 0
means **every state on the screen was established**. It was not. Two runs with distinct
`observed_at` timestamps, so it was not a cache artefact.

The cause was not a missing concept. `internal/status` already had the whole machinery: `State`'s
zero value is `Undetermined`, `worse(current, member)` ranks an undetermined member above every
other state, and `Summarise` refuses to lead with "everything is fine" when anything is
undetermined. None of it was ever **reached** for this subsystem, because `storeSubsystem` decided
"working" from `store.Exists(root)` alone — a directory with a marker in it — and never asked what
the store held. This is the one screen whose entire promise is "whether everything runs", and it
was the only surface on the machine that answered yes about data nobody opened.

**Blocker 2 — the agent API rendered undetermined material as determined**, in the surface an AI
consumes, where PRD §3.12's promise is *an agent reads exactly what you can — no more, no less*.
Here it read **more confidently** than the person could:

- **(a)** `chmod 000 outbox/d2/.state` — `omw agent drafts` reported `d2 drafted`, exit 0, while
  `omw outbox list` exited 3 and said where the draft stood could not be read. Both read the same
  file; `daemon.draftView` set `State: agentapi.DraftedState` as a literal and never asked
  `drafts.Outbox.StateOf`.
- **(b)** `chmod 000 outbox/d2/000001.body` — `omw agent drafts` reported `(0 revision(s))`, a
  **determined zero about a revision nobody could read**. `draftView` guarded the timeline with
  `if vs, err := ...; err == nil`, so the error was dropped and the count stayed at its zero value.
  This is #67's exact shape, and it is the conventions note's own example: *a `(bool, error)` whose
  error is dropped has broken it*.
- **(c)** `agent_cmd.go:319/324/331` printed `len(resp.Tickets|Drafts|Notes)` unconditionally, so
  `tickets: 0` printed beside `outcome: undetermined` — the same line that means "3 tickets" on
  success, said about material nobody read.

## What Changes

Nothing here invents a vocabulary. Five surfaces already answer this correctly on the same data, and
each change below reaches for the answer that already exists: `tri.Undetermined`, whose zero value
and single wording are the point of the package; `cli.ExitUndetermined`'s 3, distinct from
`ExitFailure`'s 1; and `internal/status`' existing `worse`/`Summarise` precedence.

- **`internal/status/collect.go`** — `storeSubsystem` now asks the store what it holds, through a
  new `recordItems`. One member per record kind, each with its own state, using
  `store.Store.Kinds` and `store.Store.List` — **the same two functions `omw store status` asks**,
  so the two surfaces cannot come to disagree about one store. A kind that could not be listed is
  an `Undetermined` member and carries no count; the existing `worse` precedence lifts it to the
  store line and `Summarise` lifts it to the summary, which moves the exit code to 3 through the
  `AnyUndetermined` path that was already there. An empty store still says so, as a determined
  answer.
- **`internal/agentapi/api.go`** — `DraftView.Revisions` becomes a `*int`. `nil` means the count
  was not established and is omitted from the JSON, for the reason `Response.UndeterminedNotes` is
  already a pointer: a plain int is 0 on every failed read, and `"revisions":0` reads as "I counted
  them and there were none". `DraftView.State` may now be `UndeterminedState` — `tri`'s wording,
  not a new spelling. `Determined()` and `RenderRevisions()` are added so that the outcome, the
  exit code and the rendering cannot form three opinions about one draft.
- **`internal/daemon/agent.go`** — `draftView` asks `StateOf` and carries its three-valued `Known`,
  and keeps the timeline's error instead of discarding it. The **only** determined zero left is the
  one the outbox establishes: a listed draft directory holding no revision files.
- **`internal/agentapi/answer.go`** — `markUndeterminedDrafts` carries a draft nobody could read up
  to the response's `Outcome`, which is what the exit code derives from. It is the shape
  `answerHub` already uses for an undetermined note: what WAS read is still served, and the answer
  as a whole is not claimed to be established. Both draft paths call the one function.
- **`internal/commands/agent_cmd.go`** — `agentCountLine` prints a number only under
  `outcome: ok`; otherwise it prints `could not be determined`, which is what `omw inbox list`
  prints. The per-draft line renders its revisions through `DraftView.RenderRevisions`.

## Impact

- Surfaces: `omw status`, `omw status --json`, `omw agent drafts`, `omw agent draft.write`,
  `omw agent tickets`, `omw agent hub`.
- **Behaviour change a script may see:** `omw status` now exits 3, not 0, when a record it
  summarises cannot be read. That is precisely what `omw status --help` already promises, and it
  is acceptance criterion 1. A fully readable store still exits 0 and still says everything
  configured is running — driven as the inverse in every test here.
- **Wire change:** `revisions` is omitted from an agent API draft when the count was not
  established, rather than present as `0`.
- Capabilities touched: `status`, `agent`.
- **PR #87 (`dev/feat/66-status-model-provider`) also edits `internal/status/collect.go`** and adds
  a model-provider line to the same screen. Its work is not duplicated and not pre-empted here:
  that branch adds a new subsystem member; this adds record members inside `storeSubsystem`. The
  two are separate regions of one file and one may textually conflict; #87 lands on its own terms.
