# Tasks

## 1. Reproduce the Issue's table, with BOTH controls, before changing anything

- [x] 1.1 Add `TestASelfApproveDoesNotEraseARefusal` to `internal/machinery/reviewgate_test.go` and
      run it with `-count=1` against the unmodified gate.
- [x] 1.2 Drive both controls, not just the test row. The defect's signature is that the test row is
      byte-identical to the no-refusal control, so a probe that cannot tell the controls apart proves
      nothing by agreeing with either.
- [x] 1.3 Teach the fixture which review policy it is under. `.workflow/review-policy` is read
      relative to the working directory, so a temp-directory fixture silently ran under the strict
      default while #82 lives entirely in `self-allowed`.
- [x] 1.4 Confirm the red matches the Issue: `rc=3`, `SELF-REVIEWED`, `NO INDEPENDENT AGENT HAS
      LOOKED AT THIS` — with an independent refusal sitting unread above it.

## 2. The fix

- [x] 2.1 Read every verdict block naming the current head, not `| last` alone.
- [x] 2.2 Keep each reviewer's most recent verdict; refuse while any of them is `changes-requested`.
- [x] 2.3 Name the reviewers whose refusals are outstanding, and say that a later approve does not
      clear them and what does.
- [x] 2.4 Keep the most recent verdict as the one that decides who is checked for independence.
- [x] 2.5 Check attribution for every block naming the head, not only the certifying one — stepping
      over an unattributable block to reach a later approve is this defect wearing a different hat.
- [x] 2.6 Add matching arms to the script's own `--self-test`, both controls included, so the patch
      arrives upstream with its own proof.
- [x] 2.7 Declare the local commit in `internal/machinery/framework-local-commits.txt`, carrying #65's
      debt on the same file forward rather than leaving an inert line behind it.

## 3. Both directions, and the trap

- [x] 3.1 A reviewer may withdraw its own refusal (criterion 4).
- [x] 3.2 A self-approve with no prior refusal still passes as `SELF-REVIEWED` (criterion 2).
- [x] 3.3 A refusal of a PREVIOUS head does not follow the code that answered it (criterion 3), on a
      fixture that actually advances its head — without this, the fix traps every refused branch
      permanently, which is worse than the defect.
- [x] 3.4 A refusal posted AFTER an approve still refuses. It passed under the old gate for the wrong
      reason, because `last` happened to point at it.
- [x] 3.5 A second independent reviewer cannot vote away the first one's refusal. Stricter than the
      Issue required; stated in the proposal and the pull request body rather than slipped in.

## 4. Prove the tests can fail

- [x] 4.1 Mutate the refusal scan back to last-wins; confirm three Go arms go red.
- [x] 4.2 Mutate it to "a refusal is forever"; confirm the withdraw arm goes red — so the rule is
      pinned from both sides and not just the safe one.
- [x] 4.3 Re-run mutation 4.1 after adding the self-test arms and confirm the SCRIPT's own self-test
      goes red too. It stayed green the first time, which is exactly the gap those arms close.
- [x] 4.4 `grep` each mutation in place before believing any red, and restore after each. `-count=1`
      throughout.
- [x] 4.5 Drive `check-review.sh --self-test` and `pr-authors.sh --self-test`.
- [x] 4.6 Full `internal/machinery` suite green, including every #65 test this branch is stacked on.
- [x] 4.7 `make ci` and `./.workflow/bin/run-gates.sh` green.

## 5. Found while building this, and worth recording

- [x] 5.1 The first cut of the fix passed records through `@tsv` and read them with `IFS=$'\t'`. A tab
      is IFS whitespace, so `read` collapses a LEADING EMPTY field — an unattributable block, whose
      role is empty, shifted every field left and was reported as a disagreement instead of as
      undetermined. Switched to US (`0x1f`), which is not whitespace. **Caught by #65's own self-test
      arm**, which is the argument for those arms existing.

## 6. Not part of this change

Auditing the pull requests currently sitting on erased refusals — #38, #46 and #53 are named in the
Issue. Once this lands they go red on their own, which is the correct state; whether anything already
merged on an erased refusal cannot be seen from `main` and is being run down separately. Not carried
as an unticked box here because it is not this change's work.
