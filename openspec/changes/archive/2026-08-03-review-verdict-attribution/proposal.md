# A verdict is a comment somebody posted, not a string found inside one

## Why

`.workflow/bin/check-review.sh` selected the review block from the comment **body** and took the
reviewer's name from the **text**:

```sh
block=$(jq -r --arg h "$head" '
  [ .[] | .body
    | select(test("Reviewed-by:"))
    | select(test("Reviewed-sha:[[:space:]]*" + $h)) ] | last // ""' "$comments")

reviewer=$(printf '%s' "$block" | sed -n 's/^Reviewed-by:[[:space:]]*//p' | head -1)
```

`.body` is projected out of the comment object, so **who posted it is discarded before any check
runs**, and the identity the independence rule is applied to is a string the reviewer typed about
themselves. An author that wanted to certify its own work only had to type a different name.

It is worse than a latent hole, because it does not need intent. **product tripped it by accident on
#63** while asking dev to re-attest: quoting the verdict template inside a fenced code block produced
a valid, independent-looking approval from dev. `jq`'s `test()` has no notion of a fence and `sed`'s
`^` matches inside one. It failed to take effect only because a genuine verdict was posted afterwards
and the selector takes `| last` — so every re-review request, every Issue quoting an example verdict,
every postmortem discussing what a verdict looked like was a live grenade whose timing decided the
outcome.

The gate this defeats is the only thing between an unreviewed change and `main`; branch protection
reads its commit status. Everything else — the queue routing, the author exemption, `pr-authors.sh` —
is machinery for deciding *who may attest*, and none of it matters while the attestation is
self-declared. It also fails in the trusting direction: #52 and #64 are a gate saying the wrong
thing, this is a gate saying `success` about something that did not happen.

The audit on Issue #65 established that nothing has in fact shipped on a quoted verdict. That is the
past; this is the mechanism.

## What Changes

- **Fenced and quoted text is discarded before anything is parsed.** ` ``` ` and `~~~` blocks are
  stripped from each comment, and the selection is anchored to a line start so a `>`-quoted verdict
  is not one either. Only the fenced text goes — a real verdict that pastes command output is still
  a real verdict.
- **The reviewer is the poster.** The identity comes from the `[role]` marker that every role already
  signs every comment with on its own first line and that `queue.sh` already routes on, kept from the
  comment object rather than thrown away with it.
- **A declared `Reviewed-by:` that disagrees with the poster is REFUSED, not re-attributed**, and the
  refusal says the two disagree. Silent correction would hide the attempt, and the attempt is the
  thing worth seeing. The verdict's `Verdict:` line is not acted on at all.
- **A verdict whose poster cannot be determined refuses as UNDETERMINED** — a different value from
  "no review exists" and from "this review is forged", and not spelled like either.
- `internal/machinery/reviewgate_test.go` gains `TestQuotedVerdictIsNotAVerdict`, which **executes**
  the installed `check-review.sh` over each of these shapes. It is the durable half: `.workflow/bin/`
  is framework-owned and the next installer refresh replaces it wholesale (#58), so the assertion has
  to live in project-owned code or it leaves with the fix it guards.

## What this does NOT establish, stated rather than rounded up

**`.user.login` is deliberately not used, and criterion 1 of the Issue names it.** Every role on this
repository posts through the **same GitHub account**, so the login identifies the human and would
collapse all five roles into one reviewer — turning the independence rule off entirely rather than
enforcing it. What is used instead is the `[role]` marker, which criterion 1 also allows as "the
equivalent the project already trusts elsewhere". **That marker is a convention, not an authenticated
fact.** A role that sets out to forge a verdict can still type another role's marker.

So this change closes the accident and does not close the attack. It removes the case where a comment
*that is not a verdict at all* is counted as one, and the case where a verdict names somebody other
than the agent that posted it. **Making the attestation unforgeable requires the roles to post under
distinct identities**, which is a repository-and-credentials decision, not a change to this script.
Issue #65 stays open on it.

## The undecided path, refused rather than answered

Nothing in the Issue's criteria says what a verdict carrying **no `[role]` marker** means. There is no
honest default available: reading its `Reviewed-by:` as a fallback is exactly the hole, and inventing
a poster is worse. So the gate **refuses and says it could not determine who posted it**, naming the
remedy. This is a real answer in this project's terms and it is not a decision that unmarked verdicts
are invalid forever — it is a refusal to guess.

Measured before shipping it: on PR #80's real comment thread, both verdicts carry the marker on line
one and both agree with their `Reviewed-by:`. This does not break how verdicts are actually posted
here. It does make the marker load-bearing where it was previously only a routing convenience, and
whether that is the right place to put the weight is the decision left open.

## Impact

- Affected specs: `machinery`
- Affected code: `.workflow/bin/check-review.sh` (framework-owned — declared in
  `internal/machinery/framework-local-commits.txt`; the real fix belongs upstream in agent-dev-flow),
  `internal/machinery/reviewgate_test.go`, `internal/machinery/prauthors_test.go`
- `pr-authors.sh` is untouched. Both `check-review.sh --self-test` and `pr-authors.sh --self-test`
  were driven anyway, because `check-review.sh` calls it.
- The archive-only exemption is untouched: a DETERMINED empty author set still means every role is
  independent, and `TestArchiveOnlyPullRequestCanBeReviewed` still passes.
