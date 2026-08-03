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

## Deliberately not done

- [ ] Prevent two roles reviewing the same head concurrently — Issue #59 leaves this decision open
      and criterion 2 forbids narrowing to one reviewer. The current behaviour is now pinned by a
      test instead, so removing it must be deliberate.
- [ ] Upstream the change into agent-dev-flow — outside this repository, and the reason the local
      edit is declared rather than relied on.
