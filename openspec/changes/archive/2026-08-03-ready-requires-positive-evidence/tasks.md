# Tasks

## 1. The durable half — a Go test that executes the installed watch

- [x] 1.1 Add `internal/machinery/readyevidence_test.go`, driving the installed `watch-prs.sh`
      against a stub `gh` returning an EMPTY `check_runs` array. It does not reimplement its logic.
- [x] 1.2 Assert no `READY` for a head on which nothing has reported.
- [x] 1.3 Assert the no-answer case produces its OWN event, carrying `pr.sh state`'s wording.
- [x] 1.4 Drive the MONITOR as well as `--sweep`, so the shared branch is measured and not assumed.
- [x] 1.5 Assert a completed green build with no published verdict is not `READY`.
- [x] 1.6 Assert a still-running check run is not `READY` and is not silence — with a fixture that
      also has a COMPLETED run, so the assertion reaches the branch it names.
- [x] 1.7 Assert an unreadable commit-status lookup is a reported failure, not an absent verdict.
- [x] 1.8 Add a well-formedness arm: full evidence must still produce `READY`.
- [x] 1.9 Probe every dependency and skip with a reason that says nothing was determined.
- [x] 1.10 Watch every arm go RED against a mutated script with `-count=1`. Seven mutations, each
      verified to have applied before its test was run.

## 2. The fix (framework-owned; the real one belongs upstream)

- [x] 2.1 Count COMPLETED check runs, the number `pending` never was.
- [x] 2.2 Read the review verdict positively from the commit status.
- [x] 2.3 `READY` requires a completed run, nothing pending, and a successful verdict.
- [x] 2.4 `NO-ANSWER` emitted for each of the three no-answer cases, naming which one it is.
- [x] 2.5 A failed commit-status lookup becomes a reported `LOOKUP FAILED` instead of falling
      through to the permissive answer.
- [x] 2.6 `NO-ANSWER` added to the emitted-states list in the header and in the self-test.
- [x] 2.7 Self-test arms driving both entry points, plus the two properties this must not break.
- [x] 2.8 A `--sweep` that stands down for a budget hold exits instead of looping forever.
- [x] 2.9 Declare the file in `internal/machinery/framework-local-commits.txt` with the reason.

## 3. Specification

- [x] 3.1 `openspec/changes/ready-requires-positive-evidence/` with proposal, tasks and spec delta.

## 4. Gates

- [x] 4.1 `make ci` green, run locally in a full clone.
- [x] 4.2 `./.workflow/bin/run-gates.sh` before pushing.

## Not done, deliberately

- **Upstreaming to agent-dev-flow.** Out of this repository; the debt is declared.
- **`queue.sh`'s own merge-queue derivation.** It has the right wording already and was not the
  signal that misfired; changing it was not asked for and would widen this beyond the measurement.
