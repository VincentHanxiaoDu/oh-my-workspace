# Tasks

## 1. See it red first, against the unmodified gate

- [x] 1.1 Write `TestQuotedVerdictIsNotAVerdict` in `internal/machinery/reviewgate_test.go` BEFORE
      touching `check-review.sh`, and run it with `-count=1` — go test caches and does not
      invalidate on a changed shell script.
- [x] 1.2 Choose the quoted block's declared reviewer so the old gate exits **0**. Naming an author
      of the branch would have made the case fail for the independence rule instead, and it would
      have gone green for the wrong reason.
- [x] 1.3 Confirm the red is the Issue's mechanism: the old gate answered
      `review ok: … reviewed by 'product', which authored none of its commits` for a comment posted
      by product that only QUOTED a verdict.

## 2. The durable half — a Go test that executes the installed script

- [x] 2.1 Drive `.workflow/bin/check-review.sh` itself. Reimplementing its logic would leave the test
      green while the real gate stayed broken.
- [x] 2.2 Add `checkOut`, returning the gate's output as well as its exit code: three of the new
      refusals share exit 1 with "no review exists" because all four mean this head is not certified,
      so the distinction lives in the message and the message is asserted.
- [x] 2.3 Cover the #63 near-miss, a quote at the top of the comment, a `~~~` fence, and a
      `>`-blockquoted verdict.
- [x] 2.4 Cover both orderings — a quotation before and after a genuine verdict — so the `| last`
      hazard that decided #63 by luck cannot decide anything.
- [x] 2.5 Cover the other direction (Issue criterion 4): a genuine verdict passes, including one that
      itself contains a fenced block of command output. A fix that refuses everything passes every
      case above.
- [x] 2.6 Probe the environment rather than naming it: reuse `needTool`/`repoRoot`, which skip with a
      reason. Nothing new here walks `git log`.

## 3. The fix in `check-review.sh`

- [x] 3.1 Strip ` ``` ` and `~~~` blocks from each comment body before selecting.
- [x] 3.2 Anchor the selection to a line start, so a `>`-quoted verdict is not selected either.
- [x] 3.3 Keep the comment object rather than projecting `.body` out of it, and derive the reviewer
      from the `[role]` marker on its first line.
- [x] 3.4 Refuse a declared `Reviewed-by:` that disagrees with the poster, saying they disagree, and
      do not act on its `Verdict:` line.
- [x] 3.5 Refuse an unattributable verdict as UNDETERMINED, with no fallback to the declared name.
- [x] 3.6 Add matching arms to the script's own `--self-test`, so the patch arrives upstream with its
      own proof, and update its fixtures to carry the `[role]` marker.
- [x] 3.7 Declare the local commit in `internal/machinery/framework-local-commits.txt` with the
      reason and the debt.

## 4. Prove the tests can fail, and that nothing else broke

- [x] 4.1 Mutate `strip_fences` to a no-op; confirm the four fence arms and the self-test go red.
- [x] 4.2 Mutate the disagreement refusal to never fire; confirm the two attribution arms and the
      self-test go red.
- [x] 4.3 Mutate the undetermined path to fall back to the declared name; confirm that arm and the
      self-test go red. Restore after each, and `grep` the mutation in place before believing it.
- [x] 4.4 Update the fixtures in `TestRefusedReviewDoesNotReadAsNoReview` and `prauthors_test.go` to
      carry the marker — they predate it and would otherwise be testing the new refusal.
- [x] 4.5 Confirm the archive-only exemption still holds: `TestArchiveOnlyPullRequestCanBeReviewed`
      passes and a DETERMINED empty author set still means every role is independent.
- [x] 4.6 Drive `check-review.sh --self-test` and `pr-authors.sh --self-test`. `pr-authors.sh` is
      untouched; it is driven because `check-review.sh` calls it.
- [x] 4.7 Measure the assumption against reality before shipping it: confirm on a real comment thread
      that genuine verdicts already carry the `[role]` marker and that it agrees with `Reviewed-by:`.
- [x] 4.8 `make ci` and `./.workflow/bin/run-gates.sh` green.

## 5. Not done, and deliberately

- [ ] 5.1 **Give the roles distinct posting identities.** Until they have them, the `[role]` marker is
      a convention and a determined forger can type another role's. This is the half of Issue #65
      that is not fixed, it is a repository-and-credentials decision rather than a change to this
      script, and it is why the Issue stays open.
- [ ] 5.2 **Decide what an unmarked verdict means.** The gate refuses and says it could not determine
      the poster, which is a refusal to guess and not a ruling. Whoever owns the convention should
      settle whether the marker is now mandatory on verdicts and say so in the role prompts.
- [ ] 5.3 **`queue.sh` attributes comments the same way, one level up.** `startswith("[role]")` decides
      what a role has already looked at, on the same unauthenticated marker. Out of scope for this
      Issue and worth its own.
