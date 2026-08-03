# Tasks

## Establish the mechanism before changing anything

- [x] Drive `queue.sh dev|qa|product` against the live board and record what each is offered
- [x] Confirm the reported mode — two roles, one head — reproduces without construction (#70, #53, #48)
- [x] Construct the unreported mode against a stub: one role offered the same head across two rounds
- [x] Test the `[<role>]` marker hypothesis directly, and record that it explains neither review mode

## The queue reads the record the gate already reads

- [x] `reviewed_by` — `Reviewed-by: <role>` and `Reviewed-sha: <head>` on the pull request's comments
- [x] Suppress in `reviews_waiting` only for that role and only for that head
- [x] Propagate the lookup failure out of the command substitution rather than reading empty as "none"
- [x] Anchor the role and sha matches so a longer name or sha cannot satisfy them

## Assertions that execute the installed script

- [x] `internal/machinery/reviewassignment_test.go`, driving `.workflow/bin/queue.sh` with a stub `gh`
- [x] Probe `bash`, `jq` and the script's presence; skip with a reason that says it did not determine anything
- [x] An arm that fails only the comments query, because a fail-everything stub never reaches this code
- [x] Matching arms in `queue.sh --self-test`, so the upstream patch carries its own proof
- [x] Watch it go red: suppressor removed, role ignored, sha ignored, lookup failure swallowed

## Declare the framework-owned edit

- [x] Record `.workflow/bin/queue.sh` in `internal/machinery/framework-local-commits.txt` with its reason

## Out of scope, stated rather than listed

These are not unfinished tasks and they are not ticked. They are work this change deliberately does
not contain, recorded so the omission is visible.

**Preventing two roles from reviewing the same head concurrently.** Issue #59 says the redundancy
was chosen on purpose, that doing nothing may be correct, and that the cost should be measured
before a mechanism is spent on it; criterion 2 forbids narrowing to a single eligible reviewer, and
criterion 3 forbids the pre-review claim that is the only thing which could close a true race. The
decision stays open on #59, which stays open. The current behaviour is now pinned by
`TestAnUnreviewedHeadReachesEveryIndependentRoleAndNoAuthor`, so removing it has to be deliberate.

**Upstreaming the `queue.sh` change into agent-dev-flow.** That is another repository. The local
edit is declared in `internal/machinery/framework-local-commits.txt` as a debt, and the Go test is
what survives the refresh that will delete it.
