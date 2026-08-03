# Tasks

## 1. Blocker 1 — a subject nothing writes is not a quiet day

- [x] 1.1 Read `internal/tri` and `internal/cli` before writing anything, and use the established
      answer rather than inventing one.
- [x] 1.2 Confirm by grep that `reports.WriteActivity` has no production caller, rather than taking
      the Issue's word for it.
- [x] 1.3 Red first, through `omw report run`: a subject nothing writes and the same subject with
      real activity must differ in OUTPUT and in EXIT CODE. Watched it fail with the Issue's exact
      bytes — `no activity in this period`, exit 0.
- [x] 1.4 `Subject.Producer`, empty for every local subject, with the reason it is empty recorded in
      the catalog so a later change cannot fill it in casually.
- [x] 1.5 `ErrNoProducer` from `StoreSource.Activity`, checked only when there are no records, so
      activity that IS present still wins.
- [x] 1.6 `StateNoProducer` with its own line, not `Determined()`, so the command exits 3.
- [x] 1.7 `omw report subjects` says which subjects nothing writes (criterion 2).
- [x] 1.8 Confirm the three answers — quiet day, nothing writes it, could not be read — are three
      renderings, driven at the level where all three are reachable.

## 2. Blocker 2 — diagnostics and the outbox report the same drafts

- [x] 2.1 Red first, through BOTH real commands on one store: `omw outbox draft` twice, then
      `omw outbox list` and `omw diagnostics`, comparing the two surfaces to each other rather than
      to a fixed number. Watched it report `collected (0)` against 2.
- [x] 2.2 Delete `KindDraft`. A category names the subsystem that produces its records.
- [x] 2.3 `listDrafts` reads the outbox and creates nothing — an absent outbox is a determined zero,
      anything else that fails is undetermined.
- [x] 2.4 An unreadable outbox renders undetermined WITH a machine-readable reason (criterion 4),
      asserted to differ from the empty case in state and reason, not merely to be non-empty.
- [x] 2.5 Re-seed the package's own tests through the real writer, so the draft assertions stop
      being assertions about a fixture.
- [x] 2.6 Bodies too: `--include-bodies` reaches the drafts that exist, checked by reading the file
      the manifest names.

## 3. Blocker 3 — a partial device listing is not a failure

- [x] 3.1 Red first, through `omw devices list`, driving BOTH endings: a complete inventory via a
      hub that answers and a partial one with no hub. Watched the partial one exit 1.
- [x] 3.2 `devicesCode` returns `ExitUndetermined` for a listing that is not whole.
- [x] 3.3 Assert a refused registration still exits `ExitFailure`, so the failure code is not
      vacated along with the change.
- [x] 3.4 Rewrite `TestKnownPartialAndCouldNotTellDoNotShareAnExitCode` to assert the distinction
      where it now lives, and record the supersession in the proposal and in `devicesCode`.

## 4. Criterion 6 — the structural guard

- [x] 4.1 `internal/kindguard`: parse the product's source, resolve kinds into read and write calls,
      report kinds read with no writer.
- [x] 4.2 Assert it FIRES: a read of a never-written kind, a read through a package-level map (the
      indirection Blocker 2 actually used), a delete-only kind, and a typed kind declaration.
- [x] 4.3 Assert it does NOT fire on a kind that is both read and written, so the check above cannot
      pass against an analysis that flags everything.
- [x] 4.4 A scan that examined nothing is an error, and the repository check requires the analysis
      to have found real reads and writes before believing its negative.
- [x] 4.5 Report unresolved reads separately and assert that set, so an indirection is looked at
      rather than absorbed. This found a real gap: typed declarations left `internal/agentapi`
      entirely unchecked.
- [x] 4.6 Run it against unmodified `main` and confirm it names Blocker 2 by kind and by file:line.
- [x] 4.7 Declare `message` with the Issue that will settle it, and fail on a declaration that
      outlives its finding.

## 5. Gates

- [x] 5.1 Every mutation confirmed by grep to be on the exercised path before believing red or green.
- [x] 5.2 `-count=1` throughout.
- [x] 5.3 `make ci` green.
- [x] 5.4 `./.workflow/bin/run-gates.sh` green before pushing.

## Not done, deliberately

- **The `message` inventory.** A fourth surface with the same defect, found while building the
  guard. Recorded on Issue #32 and declared in `kindguard.Declared` rather than widened into this
  change.
- **A producer for `git` or `token_usage`.** Criterion 2 accepts either half.
- **The Issue's smaller non-blocking findings.** Named there, not in its acceptance criteria.
- **Archiving this change.** Not this role's act.
