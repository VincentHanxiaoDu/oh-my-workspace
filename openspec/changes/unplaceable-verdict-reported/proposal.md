# A verdict the gate cannot place is reported, not discarded in silence

## Why

This is the **third** defect in `check-review.sh` and the quietest. The gate correctly declines to
apply a verdict whose `Reviewed-sha:` is not the head — and then tells nobody.

On #38, a UAT refusal named `e7e1368a7fbdd0c6ee7c07eebc0e5b6cf9d0e1b1`; the head was
`e7e1368a36734b898b95803e95c23348e3718245`. **Eight shared hex characters**, which is not chance, and
the posted sha names no git object at all. The gate ran 28 seconds later and published `success`. A
role believed it had blocked that pull request; the board said green and it was merge-eligible.

**The exact-sha matching is correct and is not touched.** It is what makes a verdict stale the moment
somebody pushes, and loosening it would let a review of old code certify new code. The silence on one
particular miss was the defect: *could not determine* rendered as *nothing to see*, which is the one
thing this project says a check must never do — the same shape as #79 one surface over.

## What Changes

Two ways of naming a sha that is not the head, separated:

1. **A sha the repository knows** — an ordinary stale review. A push is expected to produce these.
   **Unchanged: silent.** Announcing them would bury the case that matters under noise on every
   branch that was ever pushed to.
2. **A sha naming no object at all** — nothing was ever reviewed there, so this cannot be a stale
   review. Reported, naming the sha and the head, and it **does not pass**.

- New exit code **4**, with its own sentence in the publish step. A fourth fact needs a fourth code,
  for the reason a refusal needed one: two facts sharing a code is how #52 published a landed refusal
  as an absent review.
- **Precedence, stated rather than left to line order:** 4 beats 0 and 1; it loses to 2. A landed
  refusal is concrete and already actionable, and the unplaceable notice still prints alongside it.
- **The scan probes its checkout.** In a shallow clone an object can be missing because it was never
  fetched, so `cat-file` failing would not mean unplaceable — the gate says the question could not be
  determined rather than accusing. The review job uses `fetch-depth: 0` today, but that is a
  framework-owned workflow file the installer replaces, so it is probed and not trusted.
- Fence-stripping is now defined once and shared by both jq programs, so the selector and this scan
  cannot disagree about what a quotation is.

## Stacked on #93, which is stacked on #88

Cut from **#93's final commit (`7d7bb04`)**, not from `origin/main`. All three Issues edit
`check-review.sh` and #84 reuses #65's fence handling directly. Merge order: **#88, then #93, then
this**.

## What this makes non-passing that used to pass, said plainly

The Issue's remedy asks for the miss to be **reported**. Reporting it into the log while still
publishing `success` would leave #38 exactly as it is — green and merge-eligible with a role
believing it had blocked it — and the harm the Issue names is the merge-eligibility. So exit 4 is a
failure. **The cost is that a push does not clear it**: an unplaceable sha names no object after a
push either, so the remedy is for the poster to re-post the verdict with the correct sha, which is
the action that should happen anyway. This is a judgement about how strict to be, it goes beyond
"report it", and it is flagged here rather than buried.

## Impact

- Affected specs: `machinery`
- Affected code: `.workflow/bin/check-review.sh` and `.github/workflows/gates.yml` — both
  framework-owned, both declared in `internal/machinery/framework-local-commits.txt`; the real fix
  belongs upstream in agent-dev-flow. `internal/machinery/reviewgate_test.go`.
- **Two fixtures changed meaning.** Arms that spelled "some other head" as forty zeros were naming no
  object, which is now case 2 rather than case 1. They name a real earlier commit instead, which is
  what a stale review actually looks like, and the all-zeros case has an arm of its own.
- Not done: sweeping the board for other dropped verdicts of this shape. The Issue asks for it and
  says nobody has noticed any of them by construction. It is an audit of the past, not a change to
  the gate.
