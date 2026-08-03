# Tasks

## 1. The durable half — Go tests that execute the installed watches

- [x] 1.1 Add `internal/machinery/watchreason_test.go`, driving the installed `watch-queue.sh` and
      `watch-prs.sh`. It does not reimplement their logic.
- [x] 1.2 Drive the long-output failure path specifically: a queue stub that prints more than the
      budget of normal output and THEN fails. A stub that fails immediately cannot observe this,
      which is why the shipped self-test never did.
- [x] 1.3 Assert a short reason is rendered byte-identically, ellipsis and all absent.
- [x] 1.4 Assert a red `main` names the failing check.
- [x] 1.5 Assert a red `main` on a non-merge commit does not tell the last merger it is theirs, and
      says the cause was not determined.
- [x] 1.6 Assert an attributable red may still say so, derived from the merge commit sha.
- [x] 1.7 Assert an unreadable jobs list is not rendered as a red run with nothing failing.
- [x] 1.8 Add a well-formedness arm, since most arms assert an ABSENT string and an absent string
      proves nothing against empty output.
- [x] 1.9 Probe every dependency and skip with a reason that says nothing was determined.
- [x] 1.10 Watch every arm go RED against a mutated script with `-count=1`. Six mutations, each
      grep-verified to have applied.

## 2. The fix (framework-owned; the real one belongs upstream)

- [x] 2.1 `watch-queue.sh`: a `reason` helper that keeps the tail and marks the elision, used by the
      `LOOKUP FAILED` emit.
- [x] 2.2 `watch-prs.sh`: the same helper, applied to both of its `LOOKUP FAILED` emits.
- [x] 2.3 `watch-prs.sh`: `main_state` names the failing check, read from the run's jobs.
- [x] 2.4 `watch-prs.sh`: `attributed_main_state` derives the attribution from the merge commit sha
      and says `CAUSE NOT DETERMINED` when it cannot.
- [x] 2.5 `watch-prs.sh`: the merged-pull-request loop passes each merge commit sha, and `main_state`
      caches its lookups so a poll still asks about main once.
- [x] 2.6 Matching `--self-test` arms in both scripts, including the long-output one and the
      byte-identical short one.
- [x] 2.7 Declare both files in `internal/machinery/framework-local-commits.txt` with the reason.

## 3. Specification

- [x] 3.1 `openspec/changes/watch-reason-and-attribution/` with proposal, tasks and the spec delta.

## 4. Gates

- [x] 4.1 `make ci` green, run locally in a full clone.
- [x] 4.2 `./.workflow/bin/run-gates.sh` before pushing.

## Not done, deliberately

- **Upstreaming to agent-dev-flow.** Out of this repository; the debt is declared.
- **The `head -3` on CI annotations.** That list is the diagnosis, not a log whose end is the reason.
