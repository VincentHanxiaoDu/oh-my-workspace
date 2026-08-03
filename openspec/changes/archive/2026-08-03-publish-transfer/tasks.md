# Tasks

Every box below is ticked because it happened. Anything that is only structurally guaranteed says so
in its own line rather than being ticked as though it were driven.

## The durable record

- [x] `internal/publish/journal.go` — a ledger inside the store, one record per note, written with a
      temp file, an `fsync`, an atomic rename and an `fsync` of the directory
- [x] Phases (`in-flight`, `published`, `refused`) kept as a storage vocabulary separate from the
      state names a person reads, so rewording one cannot invalidate records on disk
- [x] A record that is present and damaged reads as undetermined, never as `drafted`, and stops the
      transfer — driven by damaging one on disk
- [x] A phase this build does not know is undetermined, not a fifth state
- [x] `Reconcile` finishes the draft deletion a killed process left, and leaves in-flight and resting
      drafts alone
- [x] The ledger lives beside the outbox rather than inside each draft, because a published note's
      draft is deleted and the record must outlive it — found by the first version forgetting every
      note it published, caught by the test suite

## The four states

- [x] `internal/publish/state.go` — `drafted` / `in flight` / `published` / `refused`, a closed set
- [x] `Report` is the one computation; `Render` for a person, `Wire` for a program
- [x] Every rendering line a caller may branch on is `key: value` with a closed vocabulary
- [x] The four are compared PAIRWISE in the tests, and the four notes compared are put into their
      states by real attempts rather than by hand-built structs
- [x] `Container` is a total function with two outcomes, checked over every combination of the
      report's fields — never both, never neither, by construction
- [x] `Published()` is three-valued; an in-flight note answers undetermined, never "no"
- [x] A refusal with no reason renders as a named defect, not as a blank

## The transfer

- [x] `internal/publish/transfer.go` — no-hub check first, record before the dial, and the ledger
      written before the draft is removed
- [x] The attempt key is reused from the record, which is what makes a retry a retry
- [x] A dial that never connected clears the record; a request that was sent does not
- [x] `internal/publish/wire.go` — newline-delimited JSON over a unix domain socket, `"unix"` as a
      literal in both the listen and the dial
- [x] `internal/hub/idempotent.go` — key → note, recorded on success only, under the same lock as the
      publication
- [x] An attempt key is bound to its holder, so replaying somebody else's key is refused rather than
      returning their note — found by writing the test for it and watching it fail

## The command

- [x] `internal/commands/publish_cmd.go` — a NEW file; nothing that already existed in the package
      was edited
- [x] `omw publish note | state | list`
- [x] Liveness through `daemon.Inspect`, three-valued, starting nothing
- [x] Scopes are not defaulted: nothing configured means no `publish` grant and a refusal that names
      the missing scope
- [x] Exit codes: a refusal is `ExitFailure`, an unreachable hub is `ExitUndetermined`, and no hub
      configured is `ExitFailure` — three answers, and the undetermined one is not shared

## Identifiers (the owner's ruling)

- [x] `internal/hub/noteid.go` taken byte-for-byte from Issue #15's branch, and the one hunk in
      `internal/hub/store.go` taken byte-for-byte too, so the two branches cannot land two schemes
- [x] `internal/hub/publish_ids_test.go` — this Issue's own tests, named so both files survive the
      merge with #15
- [x] Criterion 19 driven three ways: neighbours, order, and constant gaps
- [x] Criterion 20 driven as a differential on ONE corpus, plus the assertion the differential as
      worded does not make — that the hidden note is not one step from anything observable
- [x] Criteria 21, 22, 23 driven: stability across amendments and visibility changes; refused vs no
      such note; one path segment, printable, no metacharacters, no control runes
- [x] No fallback when there is no randomness, and a collision refused rather than overwritten

## Tests that had to interrupt something

- [x] `internal/publish/crash_test.go` — the test binary re-invoked as a child and SIGKILLed, twice:
      once after the hub stored the note and once before it did
- [x] Both spawns sandbox `XDG_DATA_HOME` and `HOME`
- [x] Each asserts the kill landed in the window it claims before pulling the plug
- [x] Each asserts the child was killed rather than having exited by itself
- [x] The retry after each asserts the hub holds ONE note, and asserts the count BEFORE asserting
      what the retry called itself

## Mutation testing

- [x] Ten mutations driven, each confirmed RED naming the defect, each reverted and the tree
      re-verified green. The table is in the pull request body.
- [x] Recorded — in the code, in this list and in the pull request — the ONE thing here that is only
      structurally guaranteed: the ordering of "write the published record, then delete the draft".
      Swapping it changes no observable outcome on any path a test can reach, because the failure it
      opens needs a kill inside a window a test cannot aim at without a hook in product code. It is
      not claimed as driven anywhere. That disclosure is the task, and it is done.

## Review round 2 — qa's blocking finding on `7da9cb1` (§4)

- [x] `publishOpen` distinguishes the three facts about the store instead of collapsing them:
      `store.ErrNotFound` is a DETERMINED negative and exits `ExitFailure` (1);
      `ErrUnreadable`/`ErrPermissionDenied` and anything unrecognised stay `ExitUndetermined` (3)
- [x] The wording and the distinction are `outboxOpenStore`'s, not a second vocabulary for the same
      three facts
- [x] `TestPublishDistinguishesAnAbsentStoreFromAnUnreadableOneByExitCode` drives all three
      subcommands (`note`, `state`, `list`) and compares the two exit codes TO EACH OTHER, so it
      cannot pass by the two collapsing onto a shared value again
- [x] The unreadable arm is a REAL store from `store.Create` with its permissions removed — a bare
      directory is rejected as `ErrNotFound` and would compare the wrong two things
- [x] Confirmed RED before the fix on all three subcommands, and confirmed the mutation that restores
      the collapse is killed while leaving the message text correct — the test discriminates on the
      exit code, not on prose

## Review round 2 — qa's non-blocking finding 2 (M3, §3)

- [x] `TestARefusalAndAnUnreachableHubDifferMachineCheckably` now asserts the never-sent path leaves
      the note in EXACTLY `drafted` with no ledger record. "Neither published nor refused" was
      satisfied by `in flight` too, which is why M3 survived the whole repository suite.
- [x] M3 re-driven and now KILLED on both assertions

## Review round 3 — product's scope ruling of 2026-08-03 (the gate moves into the transfer)

- [x] Reproduced product's defect first, and worse than reported: a `review`-mode draft with no model
      handed to `omw publish note` against a REAL, REACHABLE hub published with **exit 0** and left
      the outbox. Product's run only reached an outbound dial against an unreachable hub.
- [x] Confirmed by grep that `internal/publish` contained **zero** references to the mode or the gate
- [x] `internal/publish/gate.go` — the gate lives beside `Transfer`, not in a caller. `Permission`'s
      zero value is `PermissionUndetermined`, so every path that fails to establish an answer lands
      on "does not publish" structurally rather than by being remembered.
- [x] `publish.Transfer` consults the gate through `gateDecision`, the single call site, before the
      body is read, the key is minted, the record is written or anything is dialled
- [x] A `Config` with **no gate** is undetermined and does not publish — the fail-safe at the seam
      where Issue #16's agent API will arrive
- [x] `AttemptGateRefused` and `AttemptGateUndetermined` are distinct from each other and from
      `AttemptRefused` (which means the HUB said no, and the hub is never asked here)
- [x] All four directions driven at the package seam AND through the CLI against a real hub:
      `review` unchecked → refused naming the mode; `auto` → publishes; `manual` → publishes;
      checked `review` → publishes; unreachable model / non-verdict answer / undetermined model
      configuration → `could not be determined`, and **not published**
- [x] The gate's three answers never share an exit code (compared to each other, not to literals)
- [x] Structural guard: exactly one function in `internal/publish` reaches a hub, it is `Transfer`,
      and it consults the gate
- [x] **The guard was watched go red.** A second, ungated `send` caller is planted into the package
      and the same analysis is required to see two paths and name the planted one.
- [x] Mutations, each grep-confirmed applied before the result was believed: undetermined falls
      through to permitted → KILLED (4 tests); a nil gate treated as permitted → KILLED; `Transfer`
      does not consult the gate at all → KILLED (7 tests **including the structural guard**)

### Not done, and declared rather than decided

- [ ] `omw outbox publish` is NOT removed — that is #38's branch and another agent has it. This
      branch does the transfer side only. The two are paired and neither is complete alone.
