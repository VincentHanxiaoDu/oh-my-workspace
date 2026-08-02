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
