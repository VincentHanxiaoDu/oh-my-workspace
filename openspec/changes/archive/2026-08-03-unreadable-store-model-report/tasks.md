# Tasks

## 1. Establish whether PR #48 already fixes this

- [x] 1.1 Fetch `dev/feat/16-agent-api` and diff it against `main` rather than reasoning about it.
- [x] 1.2 Confirm the file this defect lives in, `internal/daemon/model_report.go`, is not in that
      diff, and that `modelViewFor`'s two callers (`daemon.go:247`, `state.go:568`) are unchanged
      by it.
- [x] 1.3 Read #48's `agentSources` and record its three-arm shape and its exact wording, so this
      change reuses them instead of inventing a second vocabulary.

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

## 4. Specification

- [x] 4.1 `openspec/changes/unreadable-store-model-report/` with proposal, tasks and the spec delta.
- [x] 4.2 Record the #48 relationship and the absence of a file conflict in the proposal.

## 5. Gates

- [x] 5.1 `make ci` green.
- [x] 5.2 `./.workflow/bin/run-gates.sh` green before pushing.

## Not done, deliberately

- **Making #48's `agentSources` call `modelConfigFor`.** #48 is open and approved; editing
  `internal/daemon/agent.go` here would conflict with it for no gain, since that surface is already
  correct. The duplication is named in the proposal and belongs to whichever pull request lands
  second.
- **Archiving this change.** Not this role's act.
