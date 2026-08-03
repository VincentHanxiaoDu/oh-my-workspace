# Tasks

## 1. Establish what is already there, and keep PR #87 out of scope

- [x] 1.1 Read `internal/tri/tri.go` and `internal/cli/cli.go` before writing anything: the zero
      value is `Undetermined`, `FromError` never returns `No` on an error, and `ExitUndetermined`
      is 3 and is distinct from `ExitFailure`'s 1.
- [x] 1.2 Read `internal/status/collect.go`'s `worse` precedence and `screen.go`'s `Summarise`, and
      confirm blocker 1 is that precedence never being REACHED rather than a missing concept —
      `storeSubsystem` decided from `store.Exists` alone and produced no members.
- [x] 1.3 Fetch `dev/feat/66-status-model-provider` (PR #87) and diff it against `main` rather than
      reasoning about it: it touches `status_cmd.go`, `collect.go`, `doc.go`, `screen.go` and adds
      a model line to the screen. Nothing of its work is reproduced here.
- [x] 1.4 Record the overlap in the pull request body, naming #87, so the conflict is expected
      rather than discovered.

## 2. Blocker 1, red first

- [x] 2.1 Add `internal/commands/status_records_test.go` driving a REAL store with a real ticket
      record at `chmod 000` — never a missing file, because absent and unreadable are different
      states and conflating them is the defect.
- [x] 2.2 Assert the readable and the unreadable cases DIFFER, in output AND in exit code, and
      drive the inverse: a fully readable store still exits 0 and still says everything configured
      is running.
- [x] 2.3 Assert `omw status` and `omw store status` agree about one store, comparing the two
      surfaces to EACH OTHER rather than to a literal in the test file.
- [x] 2.4 Watch both go red on the unfixed build. They did: exit 0 against the expected 3, and the
      screen byte-identical to the control.

## 3. Blocker 1, the fix

- [x] 3.1 Add `recordItems` to `internal/status/collect.go`, asking `store.Store.Kinds` and
      `store.Store.List` — the same two functions `omw store status` asks.
- [x] 3.2 Attach the members inside `storeSubsystem` and fold each through the existing `worse`,
      so the exit code moves through `AnyUndetermined`, which was already there.
- [x] 3.3 Add `internal/status/records_test.go` so a regression goes red in the package that owns
      the judgement, and confirm it goes red with the fix removed.

## 4. Blocker 2(a), an unreadable draft reported as drafted

- [x] 4.1 Add `internal/commands/agent_undetermined_test.go`, driving the real binary against a
      real daemon with `chmod 000` on `outbox/d2/.state`.
- [x] 4.2 Assert both directions and assert against `omw outbox list`'s own exit code rather than
      against a literal.
- [x] 4.3 Watch it go red: `drafted`, exit 0, byte-identical to the control, while `omw outbox
      list` exited 3 on the same draft.
- [x] 4.4 Fix `draftView` in `internal/daemon/agent.go` to ask `drafts.Outbox.StateOf` and carry
      its three-valued `Known`.

## 5. Blocker 2(b), an unreadable revision counted as zero

- [x] 5.1 Drive `chmod 000` on `outbox/d2/000001.body` and assert the served count is not a
      determined zero, in `--json` and in what a person reads.
- [x] 5.2 Watch it go red: `(0 revision(s))`, exit 0.
- [x] 5.3 Make `DraftView.Revisions` a `*int` — absent means not established — following
      `Response.UndeterminedNotes`' own reasoning, and stop `draftView` discarding the timeline's
      error.
- [x] 5.4 Keep the ONE genuinely determined zero: a listed draft directory with no revisions in it.
- [x] 5.5 Carry an undetermined draft up to the response's `Outcome` in `markUndeterminedDrafts`,
      from both draft paths, so the exit code follows.

## 6. Blocker 2(c), the count line printed on every outcome

- [x] 6.1 Drive `omw agent tickets` against a `chmod 000` ticket record and assert no NUMBER is
      printed on the `tickets:` line beside `outcome: undetermined`.
- [x] 6.2 Watch it go red.
- [x] 6.3 Add `agentCountLine` and route the tickets, drafts and notes lines through it, printing
      `omw inbox list`'s wording rather than a sixth vocabulary.

## 7. Criterion 6, the structural guard

- [x] 7.1 Add `internal/agentapi/count_structure_test.go`: every count-shaped field on every type
      this surface serves must distinguish "not established" from "established as none". It fails
      on the field NAME, and it fails if its own matcher matches nothing.
- [x] 7.2 Assert at the rendering that an unestablished count never renders as a number, and that
      an established zero still does.

## 8. Verification

- [x] 8.1 Confirm every mutation by `git diff`, not by grep.
- [x] 8.2 Run the FULL packages with `-count=1`, not a name-filtered `-run`.
- [x] 8.3 `make ci` green, then `./.workflow/bin/run-gates.sh` green, before pushing.
