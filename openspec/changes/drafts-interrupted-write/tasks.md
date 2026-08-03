# Tasks

## Break it and watch it go red

- [x] Drive the real `omw` binary and SIGKILL it inside the draft write, aimed at the instant the
      draft's directory appears — 60 rounds, all 60 interrupted
- [x] Confirm the red: 60 of 60 destroyed drafts reported `state: drafted` and exited 0
- [x] Confirm the concurrency red without the fix, by stashing it: 222 raw-Go-error / empty-code
      findings
- [x] Confirm the stale-lock red through the real binary: a clean stop then start printed the
      crash-recovery sentence

## A draft becomes visible whole, or not at all

- [x] `createDraft` — assemble first revision and state record under a staging name, fsync, one
      rename into place
- [x] `appendRevision` — staging file, fsync, `os.Link` to publish, which refuses an existing
      destination as `O_EXCL` always did
- [x] Recount and retry on a lost race; refuse by name with `ErrDraftWriteRaced` when it cannot win
- [x] `writeFileSynced` / `linkFileSynced` / `replaceFileSynced` / `syncDir` — the durable writers
- [x] `Drafts` skips staging directories, so a half-built draft is never listed

## The reader stops believing an invariant that was false

- [x] `StateOf`: a missing state record is undetermined, never `drafted`
- [x] `SetState` writes the state record atomically
- [x] `Create` writes the outbox marker atomically
- [x] Assert the invariant directly — every visible draft directory has a state file, across 60
      interrupted writes

## The exit codes stay apart

- [x] Damaged draft → undetermined, non-zero; absent draft → a different non-zero code; intact draft
      → `drafted`, zero
- [x] Listing an outbox holding damaged drafts is non-zero

## The stale-lock notice says something

- [x] `release()` clears the lock body before unlocking, so a body means its writer never got there
- [x] Both directions driven through the real binary: clean restart is silent, restart after SIGKILL
      is not

## The structural guard

- [x] AST test in `internal/drafts` — no function may reach a destination path outside the durable
      writers; the allowlist is checked against the code so a renamed helper cannot leave a hole
- [x] Mutation-tested: an added `os.WriteFile` is named with its file and line

## Not done, and not claimed

These are not tasks of this change. They are written down so the list above is read as what it is —
the whole of what shipped — rather than as the whole of the problem.

Widening the structural guard beyond `internal/drafts` to every package that writes a person's
material is recorded on the rolling debt Issue #32. Power-loss durability beyond fsync ordering,
networked-filesystem locking, and builds where `lockingIsAvailable` is false are outside this Issue
and were not measured here; a SIGKILL does not lose the page cache, so nothing in this change is
evidence about a real machine crash.
