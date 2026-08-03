# Tasks

## 1. Watch it go red first

- [x] 1.1 Write the command-level test before the line exists, and watch it fail at "`omw status
      --json` reports no `model provider` subsystem at all" — the Issue's own complaint, reproduced.
- [x] 1.2 After building the line, inject the §4.3 collapse — render the undetermined branch as
      `not_configured` / "no provider is chosen" — confirm by grep that the mutation is on the
      exercised path, and watch the test go red on BOTH the wording and the exit code.
- [x] 1.3 Inject the structural defect — remove `modelSubsystem` from `Collect` — and watch the
      guard fail naming `daemon.Report.Model`. Restore, green.
- [x] 1.4 Every run with `-count=1`.

## 2. The line (criteria 1, 2, 3)

- [x] 2.1 `status.Model` constant and its place in `Required()`, in the reading order.
- [x] 2.2 `modelSubsystem` in `internal/status/collect.go`, sourced from the `model.View` already
      carried on `Query.Report` — no second derivation.
- [x] 2.3 The detail is `model.View.Render()` verbatim, the same rendering the other two surfaces
      print.
- [x] 2.4 Four states, mapped so that no configuration produces `not_working`; only a state that
      could not be established is `undetermined`.
- [x] 2.5 Both surfaces, by construction: the state word comes from `State.Word()` and the JSON is
      the same `Screen` value, so no new code was needed for `--json`.
- [x] 2.6 The package and command comments say the list is not closed and why it fell behind.

## 3. The three surfaces do not disagree (criterion 4)

- [x] 3.1 `internal/commands/status_model_test.go`, driving the REAL binary — `omw status`,
      `omw status --json`, `omw model show`, `omw daemon status` — under one identical environment.
- [x] 3.2 Five configurations, not one: no provider; chosen without a credential; chosen with a
      credential; chosen with an unreadable credential file; and a recorded choice that will not read.
- [x] 3.3 Each surface compared to the OTHERS rather than to a literal in the test file.
- [x] 3.4 The renderings asserted PAIRWISE DISTINCT, which is the assertion a build that collapses
      two of them fails.
- [x] 3.5 Exit codes: 3 on exactly the two undetermined configurations, 0 on the three determined
      ones, including the two that are not configured.
- [x] 3.6 Unit-level counterpart in `internal/status/model_test.go`, comparing the four renderings to
      each other.

## 4. PRD §3.13 re-driven (criterion 5)

- [x] 4.1 State the control: the sentinel is shown findable at its source file before any absence is
      asserted.
- [x] 4.2 Assert it absent from `omw status` stdout, stderr and `--json` on every configuration,
      including the one where the credential file holds it.

## 5. The structural guard (criterion 6)

- [x] 5.1 `TestEveryCapabilityTheDaemonReportRendersHasALineOnTheScreen` — walks `daemon.Report` by
      SHAPE, not by a list of names, so the eighth capability cannot walk past it as the seventh did.
- [x] 5.2 It fails if it examined no capability at all, so it cannot pass vacuously.

## 6. Gates

- [x] 6.1 `make ci` green — the whole suite, with no existing test needing an edit.
- [x] 6.2 `./.workflow/bin/run-gates.sh` green before pushing.

## Not done, deliberately

- **Issue #68's collapse in `daemon.modelViewFor`.** An unopenable store is read as environment-only
  there, which reports a recorded choice as absent rather than undetermined. It is #68's value and
  another agent holds it; this screen shows it faithfully and adds no second opinion. The two
  undetermined cases driven here are reached without an unopenable store.
- Archiving this change. `openspec/specs/**` is generated and archiving is not this role's act.
