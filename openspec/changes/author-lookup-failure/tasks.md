# Tasks

## 1. The durable half — a Go test that executes the installed machinery

- [x] 1.1 Add `internal/machinery/authorlookup_test.go`, driving the installed
      `.workflow/bin/queue.sh` against a stub `gh`. It does not reimplement the script's logic.
- [x] 1.2 Narrow the stub's failure to the AUTHOR lookup alone, so every earlier call succeeds. A
      stub that fails everything cannot reach this code: the queue dies on the first failed call.
- [x] 1.3 Cover the intermittent path a secondary rate limit actually produces — the first commit-list
      call fails, the `--all-trailers` follow-up succeeds — and assert the queue exits non-zero and
      offers the pull request to nobody, including its author.
- [x] 1.4 Cover the per-commit file-list query, where an unreadable diff was read as a spec-only commit.
- [x] 1.5 Assert the failure is NAMED in the output, not just exited on.
- [x] 1.6 Add a well-formedness arm asserting the healthy stub offers to product and withholds from
      dev, so a red from a broken fixture cannot be mistaken for a red from the queue.
- [x] 1.7 Add the arm for the property this must NOT break: a DETERMINED empty author set (every
      commit spec-only) still reaches every role, which is #32's unblocking exemption.
- [x] 1.8 Probe every dependency rather than naming it; skip with a reason that says nothing was
      determined and it is not passing. Reused `queueScript`, which already does this.
- [x] 1.9 Watch all four defect arms go RED against the unmodified `queue.sh`, with `-count=1`.

## 2. The fix (framework-owned; the real one belongs upstream)

- [x] 2.1 `pr-authors.sh`: `--pr` exits 3 when the commit list cannot be read.
- [x] 2.2 `pr-authors.sh`: `--pr` exits 3 when a commit's file list cannot be read.
- [x] 2.3 `pr-authors.sh`: propagate that out of the command substitution in the dispatch — the
      subshell is how the previous version of this defect survived a correct function.
- [x] 2.4 `queue.sh`: stop swallowing both author-lookup call sites; exit non-zero and say what failed.
- [x] 2.5 Matching `--self-test` arms in both scripts, so the upstream patch arrives with its own proof.
- [x] 2.6 Drive `check-review.sh --self-test`, the other `pr-authors.sh` caller, and confirm the
      `--range` exit conventions it depends on are unchanged.
- [x] 2.7 Declare both files in `internal/machinery/framework-local-commits.txt` with the reason.

## 3. Specification

- [x] 3.1 `openspec/changes/author-lookup-failure/` with proposal, tasks and the spec delta.
- [x] 3.2 The new requirement is the sibling of the one #76 shipped for the verdict lookup, and keeps
      the determined empty author set intact.

## 4. Gates

- [x] 4.1 `make ci` green.
- [x] 4.2 `./.workflow/bin/run-gates.sh` green before pushing.

## Not done, deliberately

- The archive-only case (#32). A determined empty author set is an answer and the gate treats it as
  "every role is independent" on purpose. Folding it in would break the unblocking property that
  exemption exists for. Task 1.7 pins it as a property to KEEP.
- Upstreaming to agent-dev-flow. Out of this repository; the debt is recorded in
  `framework-local-commits.txt`, which is not a silencer.
