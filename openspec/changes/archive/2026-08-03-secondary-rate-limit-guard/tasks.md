# Tasks

## 1. The durable half — Go tests that execute the installed machinery

- [x] 1.1 Add `internal/machinery/budgetguard_test.go`, driving the installed
      `.workflow/bin/gh-budget.sh` and `.workflow/bin/watch-queue.sh` against stubs. It does not
      reimplement their logic.
- [x] 1.2 Make the stub reproduce the measured outage: `/rate_limit` answers 4896/5000 while the
      calls the watch makes come back 403. A stub with an exhausted primary quota cannot test this —
      it is the case that already worked.
- [x] 1.3 Assert a live secondary limit exits 1, names itself a secondary burst throttle, and says
      when it retries.
- [x] 1.4 Assert a healthy primary quota says its answer is about the primary quota and that the
      secondary limit could not be determined.
- [x] 1.5 Assert a primary exhaustion still holds and still says when it recovers — the property
      today's refresh (`dadec1a`) built, which this change must not undo.
- [x] 1.6 Assert an unreadable rate limit is still exit 2, distinct from both other answers.
- [x] 1.7 Drive the installed watch end to end: a refused poll emits `HOLDING`, and holds more than
      once so a hold is not a death.
- [x] 1.8 Drive the opposite: a dial timeout stays `LOOKUP FAILED` and does not emit `HOLDING`.
- [x] 1.9 Assert a `Retry-After` is obeyed, and that the stand-down is not the primary reset clock.
- [x] 1.10 Add a well-formedness arm, so a red from a broken fixture cannot be mistaken for a red
      from the guard.
- [x] 1.11 Probe every dependency rather than naming it; skip with a reason saying nothing was
      determined and it is NOT passing. Reused `needTool`/`repoRoot`, which already do this.
- [x] 1.12 Watch every defect arm go RED against a mutated script, with `-count=1` — the Go cache
      does not invalidate on a changed shell script. Seven mutations, each grep-verified to have
      applied before its test was run.

## 2. The fix (framework-owned; the real one belongs upstream)

- [x] 2.1 `gh-budget.sh`: `note-failure` classifies a refusal and records a secondary rate limit,
      with the `Retry-After` when one was sent.
- [x] 2.2 `gh-budget.sh`: `check` consults the recorded refusal BEFORE the primary counter, and
      holds on it.
- [x] 2.3 `gh-budget.sh`: a healthy answer states it is about the primary quota only and that the
      secondary limit could not be determined.
- [x] 2.4 `gh-budget.sh`: `hold-for` answers with the cooldown belonging to the cause, so a secondary
      limit is not waited out on the primary reset clock.
- [x] 2.5 `watch-queue.sh` and `watch-prs.sh`: hand a refused poll to `note-failure`, emit `HOLDING`
      naming the burst throttle when it is one, and keep `LOOKUP FAILED` for everything else.
- [x] 2.6 `watch-prs.sh`: a `--sweep` that was refused still exits non-zero — `HOLDING` is not an
      answer to a question asked once.
- [x] 2.7 The self-test gains the secondary arm and the not-a-throttle arm; its three existing arms
      were primary-only, which is why this shipped.
- [x] 2.8 Declare all three files in `internal/machinery/framework-local-commits.txt` with the reason.

## 3. Specification

- [x] 3.1 `openspec/changes/secondary-rate-limit-guard/` with proposal, tasks and the spec delta.

## 4. Gates

- [x] 4.1 `make ci` green, run locally in a full clone so the shallow-repository check can answer.
- [x] 4.2 `./.workflow/bin/run-gates.sh` green before pushing.

## Not done, deliberately

- **Upstreaming to agent-dev-flow.** Out of this repository; the debt is recorded in
  `framework-local-commits.txt`, which is not a silencer.
- **Probing the secondary limit directly.** There is no free read of it; a probe call would be the
  instrument consuming the thing it measures.
- **The truncated `LOOKUP FAILED` reason** — Issue #64, on the branch stacked on this one.
