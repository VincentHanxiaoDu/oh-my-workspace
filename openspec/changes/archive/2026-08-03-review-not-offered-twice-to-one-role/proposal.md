# A review already given does not come back to the role that gave it

## Why

Issue #59 filed a consequence of `1889abd`: `queue.sh qa` and `queue.sh product` now offer the same
pull requests, because both roles are genuinely independent of them. Two roles working their queues
in parallel review the same head twice.

**Driving the queue found a second mode the Issue does not name, and the two have different causes.**

Measured on this board, `queue.sh qa` and `queue.sh product` offered an identical list — #70, #53,
#48 — so the reported mode is real and needs no reconstruction. But `reviews_waiting` suppressed a
pull request on exactly one condition: the `Reviewed by an agent` commit status being `success`. A
`changes-requested` verdict publishes `failure`. So a role that had just refused a pull request was
offered that same head again the next round, and every round after, with nothing recording that it
had ever looked. Driven against a stub: qa with a landed `changes-requested` verdict on head `cafe`
was offered `cafe` again.

**The `[dev]` comment marker is not the cause of either, and that had to be checked rather than
assumed.** `reviews_waiting` reads no comments at all. The `[<role>]` marker feeds `VERIFIED`, which
is consumed only by the Issue arms under `--landed`. Driven directly: with a correctly signed `[qa]`
comment on the Issue *and* a red status, the pull request was still offered to qa. The marker
explains a repeating **Issue** queue; it explains neither review mode.

## What Changes

- **`queue.sh` reads the verdict attestation it already relies on.** `Reviewed-by: <role>` and
  `Reviewed-sha: <head>`, in a comment on the pull request — the same record `check-review.sh`
  parses to decide the gate. No new state, no label, no lock: the record exists because a review
  happened, and the queue stops inventing a second answer beside the gate's.
- **It is scoped to one role and one head.** Suppressing for the role that ruled, never for the
  others, so the eligible set is never narrowed to one — the property whose absence stopped the
  board in #32.
- **It cannot strand anything.** The attestation is posted *after* a review, never before, so a role
  that dies mid-review holds nothing. And the sha is part of it, so a push makes every prior verdict
  stale and the work reappears in every queue with no expiry to run and nothing to release.
- **A failed comments lookup exits non-zero.** `could not read the verdict record` must not render
  as `no verdict exists`, which is the answer that hands out work already done.
- Assertions in `internal/machinery/reviewassignment_test.go`, executing the installed script, plus
  matching arms in the script's own `--self-test` so the patch carries its proof upstream.

## What this deliberately does NOT change

**Two roles being offered the same unreviewed head stays exactly as it is.** Issue #59 states the
redundancy was chosen on purpose and that doing nothing may be correct; criterion 2 forbids
narrowing to a single eligible reviewer. A post-hoc record cannot close a true concurrency window
anyway — only a claim taken *before* the review could, and that is the thing criterion 3 rules out.
So this change narrows the duplication to the genuine race and leaves the decision open. The
existing behaviour is now pinned by a test, so removing it has to be deliberate.

## Impact

- `.workflow/bin/queue.sh` — framework-owned. Declared in
  `internal/machinery/framework-local-commits.txt`; the real fix belongs upstream in agent-dev-flow.
- One additional API call per open pull request per queue run, alongside the status call already made.
- `.workflow/bin/pr-authors.sh` is **not** touched — #61 is editing it.
