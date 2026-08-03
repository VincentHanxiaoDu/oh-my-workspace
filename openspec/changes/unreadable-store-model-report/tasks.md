# Tasks

## 1. Establish whether PR #48 already fixes this

- [x] 1.1 Fetch `dev/feat/16-agent-api` and diff it against `main` rather than reasoning about it.
- [x] 1.2 Confirm the file this defect lives in, `internal/daemon/model_report.go`, is not in that
      diff, and that `modelViewFor`'s two callers (`daemon.go:247`, `state.go:568`) are unchanged
      by it.
- [x] 1.3 Read #48's `agentSources` and record its three-arm shape and its exact wording, so this
      change reuses them instead of inventing a second vocabulary.

## 1a. Re-establish it against MERGED `main` once #48 landed

- [x] 1a.1 Fetch `origin/main` and confirm #48 merged at `9b17eaa`.
- [x] 1a.2 Confirm `model_report.go`'s most recent commit on merged `main` is still `3d7a69c`
      (Issue #18's original), so the merge did not reach it.
- [x] 1a.3 Do NOT conclude from the diff. Build `omw` from `9b17eaa` and drive both arms: the
      `model:` blocks are `cmp`-identical, so the defect is on `main`.
- [x] 1a.4 Run this change's test against `9b17eaa` and watch it go RED there, with `-count=1`.
- [x] 1a.5 MUTATE, as the positive control: remove #48's three-arm switch on a scratch copy of
      `9b17eaa`, confirm the removal by grep, and watch #48's OWN two tests go PASS -> FAIL. This is
      what rules out a broken harness behind 1a.3.
- [x] 1a.6 Rebase onto merged `main` and confirm the predicted absence of conflict.

## 2. The assertion, red first

- [x] 2.1 Add `internal/daemon/model_report_unreadable_test.go` with both arms: the CONTROL is a
      readable store with no model configured, where a determined negative is CORRECT.
- [x] 2.2 Use a REAL store via `store.Create` in BOTH arms. A bare directory is rejected by
      `store.Open` as `ErrNotFound`, which takes the same branch as the control and compares two
      spellings of "no store".
- [x] 2.3 Drive `daemon.Inspect` — the surface behind `omw daemon status` — not `internal/model`
      in isolation.
- [x] 2.4 Assert on the RENDERED `model:` block as well as the `View`, since that block is the thing
      a reader quotes.
- [x] 2.5 Empty the model environment in both arms, so neither answer can come from `$OMW_MODEL`.
- [x] 2.6 Confirm the arrangement rather than assuming it: skip with a reason on Windows, as root, or
      if `store.Open` still succeeds or returns `ErrNotFound` after `chmod 000`.
- [x] 2.7 Watch it go RED against the unmodified `model_report.go`, with `-count=1`, and confirm the
      red reproduces the Issue's exact bytes.

## 3. The fix

- [x] 3.1 `modelConfigFor` keeps the three outcomes of `store.Open` as three.
- [x] 3.2 `store.ErrNotFound` maps to `model.Read(getenv, nil)` — the filesystem answered.
- [x] 3.3 Any other error maps to `tri.Undetermined` for provider AND credential.
- [x] 3.4 The undetermined arm carries `internal/commands`' existing two sentences in `Config.Why`,
      so it renders through the single `model.View.Render` both surfaces call.
- [x] 3.5 The environment is NOT consulted on the undetermined path, matching `omw model show`.
- [x] 3.6 Confirm the mutation applied by grep before believing the green.
- [x] 3.7 Drive the real `omw` binary in both arms and confirm the `model:` blocks differ under
      `cmp`, and that the control API's reason line is byte-identical to `omw model show`'s.
- [x] 3.8 De-duplicate against merged #48: the three arms live once, in `modelConfigFrom`, and
      `agentSources` calls it instead of keeping its own copy of the same sentences.
- [x] 3.9 `modelConfigFrom` takes the store `agentSources` has already opened, so sharing the rule
      does not cost a second `store.Open`.
- [x] 3.10 Confirm #48's own two tests — which this change did not write — still pass against the
      shared helper.

## 4. Specification

- [x] 4.1 `openspec/changes/unreadable-store-model-report/` with proposal, tasks and the spec delta.
- [x] 4.2 Record the #48 relationship and the absence of a file conflict in the proposal.

## 5. Gates

- [x] 5.1 `make ci` green.
- [x] 5.2 `./.workflow/bin/run-gates.sh` green before pushing.

## Not done, deliberately

- **`internal/commands/publish_cmd.go`**, which QA refused PR #46 over: the MIRROR of this defect,
  collapsing a DETERMINED absence (`ErrNotFound`) into `ExitUndetermined`. Same rule, opposite
  direction, another agent's branch. Touching it here would collide with that work.
- **Archiving this change.** Not this role's act.
