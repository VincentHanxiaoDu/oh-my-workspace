# Tasks

## 1. Establish the defect is real and unguarded

- [x] 1.1 Confirm `main` carries two unarchived change directories and that nothing in
      `.workflow/bin/` or `.github/workflows/gates.yml` reads the relationship in this direction.
- [x] 1.2 Work out what separates the two real fixtures. Recorded in the proposal's non-goals: on
      every repository-local signal they are alike, and the only difference is the merge state of
      their owning pull requests.

## 2. The rule

- [x] 2.1 Define "shipped" as the post-condition of `openspec archive`: every `### Requirement:`
      heading of the delta present in the generated capability spec.
- [x] 2.2 Reject ticked tasks as the signal — measured, both fixtures have every box ticked.
- [x] 2.3 Reject pull-request merge state — a GitHub fact, unavailable to a gate reading a diff and
      unavailable at all during an API outage.

## 3. The gate

- [x] 3.1 Add `archive_gate` to `check-generated.sh` rather than a new script, so no new required
      status context has to be created and every existing pull request keeps merging.
- [x] 3.2 Scope it to the capabilities the pull request regenerates.
- [x] 3.3 Name the change directory and the `openspec archive <slug>` command in the refusal.
- [x] 3.4 Say "in flight, not blocked" on the passing path, so a pass is not an absence of checking.
- [x] 3.5 Report a delta with no requirements as UNDETERMINED rather than judging it.
- [x] 3.6 Hoist the base-commit check into `run_gate` so a lookup failure cannot reach either arm.
- [x] 3.7 Run both arms unconditionally — short-circuiting would hide the second finding.
- [x] 3.8 Five self-test arms: the defect, the correct archive, the in-flight case, the empty delta,
      and the pull request that regenerates nothing.

## 4. Break it and watch it go red

- [x] 4.1 Four mutations of the script, each confirmed **by `git diff`** and restored: the shipped
      comparison forced false; forced true; the empty-delta guard removed; the regeneration scoping
      removed. Each reddened the arms it should and only those.
- [x] 4.2 A fifth mutation removing `archive_gate` from `run_gate` entirely — the shape a framework
      refresh takes — and the FULL `internal/machinery` package run, five tests red.

## 5. Both real fixtures, driven against the real repository

- [x] 5.1 PR #38's head, which archives `outbox-drafts-and-modes` correctly: **passes, rc=0.**
- [x] 5.2 The same head with the change directory restored from `main`: **fails, rc=1**, naming the
      directory and `openspec archive outbox-drafts-and-modes`, on all 10 of its requirements.
- [x] 5.3 `main`'s `openspec/specs/notes/spec.md` with the directory in place beside somebody else's
      regeneration: **passes, rc=0**, `0 of 10 requirements` — in flight, not blocked.

## 6. The durable half

- [x] 6.1 `internal/machinery/archivegate_test.go`, executing the installed script, never restating
      its rule.
- [x] 6.2 Driven with the REAL `outbox-drafts-and-modes` delta and all ten of its requirements, not
      a one-line toy.
- [x] 6.3 Skip with a reason when the fixture is finally archived, rather than passing on nothing.
- [x] 6.4 Full package with `-count=1` and `-v`, selection proven by `=== RUN`.
- [x] 6.5 Declare `.workflow/bin/check-generated.sh` in `framework-local-commits.txt`.

## 7. #109's review: the gate broke the rule it enforces

- [x] 7.1 Accept the finding. Run against real `main` the arm printed `in flight, not blocked …
      so its work has not landed` about `unplaceable-verdict-reported`, which shipped in #98. A
      determination, in the same sentence shape as the genuinely in-flight directory, and false.
      PRD §4.3 does not stop applying because the thing rendering the state is a gate.
- [x] 7.2 Three-way rendering: a full match is the defect; `hit == 0` and `0 < hit < n` are both
      UNDETERMINED, named with the count measured, and neither blocks.
- [x] 7.3 Fix the closing summary too — `either gone or still in flight` restated the same claim.
- [x] 7.4 Fix the header comment: the gate does not "stay silent", it answers, and now it answers
      the right thing.
- [x] 7.5 Two self-test arms — the `0 of N` case and a separate `1 of 2` partial case, which reaches
      the branch by different arithmetic.
- [x] 7.6 `TestAPartialMatchIsUndeterminedRatherThanAnswered`, plus an assertion pinning the exact
      false sentence out of existence in `TestAnInFlightChangeIsNotAccused`.
- [x] 7.7 Mutation: restore the confident sentence, confirmed **by `git diff`**. Four self-test arms
      and three Go tests red. Restored.
- [x] 7.8 Re-run the reviewer's decisive drive against real `main` at `42becb7b`: both directories
      now report UNDETERMINED, neither blocks, and the false sentence is gone.

## 8. The reviewer's stated gaps, closed

- [x] 8.1 A REAL `openspec archive outbox-drafts-and-modes -y` on `main`, not a hand-moved
      directory. The CLI reports `+ 10 added` to `openspec/specs/notes/spec.md`, and the gate passes
      on the result — confirming the rule matches the tool's actual output and not an assumption
      about it.
- [x] 8.2 The same tree with the change directory restored: rc=1, all 10 requirements.
- [x] 8.3 `make ci` and `./.workflow/bin/run-gates.sh`, which the reviewer did not run.

## 9. Not part of this change

Issue #108 carries far more than this: #101's holder binding, #64's four remaining cases, #95's
upstreaming debt, and the `[role]` marker in the four `AGENT.md` files that lack it. None of it is
touched here and none of it is carried as an unticked box.
