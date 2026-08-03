# The budget guard reads the primary rate limit, and the outages are secondary

## Why

`.workflow/bin/gh-budget.sh` answers "is there API budget?" from `.resources.core.remaining`. That is
the **primary** hourly quota. Every 403 in the outage this was built for was a **secondary** rate
limit — GitHub's burst/concurrency throttle — and `GET /rate_limit` does not report it at all.

Measured live, while `queue.sh` and every sweep were returning HTTP 403 `API rate limit exceeded`:

```
core     4896/5000  resets in 25m
graphql  4964/5000  resets in 41m
```

Both essentially untouched, and the guard itself, run while the API was refusing every call:

```
$ ./.workflow/bin/gh-budget.sh check
4841
$ echo $?
0                      ← "plenty of budget, keep polling"
```

So the reserve was never reached, `HOLDING` never fired, and the watches polled straight through the
event `HOLDING` was added to prevent — **each retry renewing the burst that caused it**. The watch
reported `LOOKUP FAILED` over and over while insisting it had budget, and a role reading that could
not tell an outage from a throttle its own polling was sustaining.

**It fails in the trusting direction**, which is the dangerous one. It is also *proxy decay in
machinery written to fix proxy decay*: a guard that correlates with the thing it protects against in
one failure mode and is blind in the one that actually occurs.

And the recovery signal was wrong too. A secondary limit clears with **quiet**, not when
`.resources.core.reset` arrives, so a hold that waits on `reset-in` is waiting on a clock that never
described the problem.

## What Changes

- **The 403 is the signal**, because it is the only place a secondary limit is ever visible. A watch
  whose poll is refused hands the output to `gh-budget.sh note-failure`; the guard records it, and
  every later `check` holds until the throttle is expected to have lifted.
- **`HOLDING`, `LOOKUP FAILED` and a quiet board stay three separate renderings.** A dial timeout is
  not a throttle — quiet does not fix it — so it stays a `LOOKUP FAILED`, and nothing routes every
  failed poll into a hold.
- **A primary exhaustion still holds, unchanged.** The reserve that landed in today's refresh
  (`dadec1a`) is kept and asserted, not replaced.
- **`check` stops answering a question it did not ask.** A healthy primary quota is reported as being
  about the primary quota, saying in as many words that the secondary limit could not be determined
  without spending a call. An undetermined answer must not wear a determined face (PRD §4.3).
- **`hold-for`** answers the stand-down with the right clock per cause: the secondary's own cooldown,
  or a `Retry-After` when GitHub supplied one, and the reset clock only for a primary exhaustion.
- **The durable half**: `internal/machinery/budgetguard_test.go` **executes** the installed
  `gh-budget.sh` and the installed `watch-queue.sh` against stubs in which `/rate_limit` reads
  4896/5000 — the measured number — while the calls being made come back 403. It does not restate
  their logic.
- The shipped self-test gains the secondary arm. Its three existing arms are 4900-vs-1500,
  900-vs-1500 and an unreadable limit — **all primary-only, which is exactly why a guard blind to
  secondary limits passed every one of them and shipped.**

**The real fix belongs upstream in agent-dev-flow.** `.workflow/bin/` is replaced wholesale by the
next `install.sh` run — #52 was fixed there and #58 deleted it — so the three shell edits are
declared in `internal/machinery/framework-local-commits.txt` as the debt they are. What survives a
refresh is the Go test.

## Out of scope

- **The 300-second interval and the reserve are not disputed and not touched.** The arithmetic behind
  them is right, and `TestAPrimaryExhaustionStillHolds` exists so this change cannot quietly undo
  them.
- **Nothing here spends a call to probe the throttle.** Reading `/rate_limit` is free; there is no
  free read of the secondary limit, and inventing one by making a test call would be the instrument
  consuming the thing it measures — the defect this file's header already records.
- **`watch-queue.sh`'s truncated `LOOKUP FAILED` reason is Issue #64**, on the branch stacked on this
  one. It is deliberately untouched here.
