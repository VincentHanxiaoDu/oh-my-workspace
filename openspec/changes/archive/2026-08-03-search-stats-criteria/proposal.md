# Statistics criteria that need a hub transport, and a load-sensitive test that goes red for the machine

## Why

Issue #72 carries two unrelated things forward from #13's closure.

### The criteria

Three of #72's criteria need surfaces this build does not have, and #13 recorded that honestly at
closure rather than rounding it to a pass:

| # | criterion | blocked on |
| --- | --- | --- |
| 1 | the CLI and the agent API return identical statistics | #16 — there is no second surface |
| 2 | a reachable hub with nothing readable renders a DETERMINED zero | #10 — no client→hub transport |
| 3 | publishing an invisible note leaves statistics byte-identical, end to end | #10 — nothing can be published |

**None of the three is driven by this change, and none is claimed.** What the absence costs is not
the missing coverage — it is that the criteria get argued instead. `--json` and the rendered report
come from one in-process `hub.Report`, so criterion 1 holds **by construction**; construction is not
observation, and #13 said so in as many words. An argument survives a refactor that an assertion
would catch, and a criterion nobody drives is one nobody notices has stopped being true.

So each criterion is written here as the test that will drive it, gated on a **probe of the
product's own seam**, and skipping with a reason that says outright it has determined nothing and has
not passed. `Could not determine` and `determined to be nothing` do not share a value here (PRD
§4.3), and a skip that read like a pass would be the second wearing the first's clothes.

**The skips cannot rot into permanent silence.** The criterion-1 test fails outright the moment a
second surface appears while the comparison is still unwritten — which is how a recorded
`unobservable` otherwise becomes a permanent one.

### The flake

`TestARunningDaemonPollsProjectsWithNoCommandRun` waited **up to a fixed 10s** for the daemon's poll
to reach an expected count. Measured on this machine: 2.7s idle, **12.27s** at load average 47 under
28 CPU burners and three concurrent full suites — 4.5× and past the deadline. A test about whether
the daemon polls at all was failing because the box was busy. That is a **false red**, and a false
red on a gate is expensive: roles learn to re-run until green and stop reading failures.

The Issue is explicit that lengthening the timeout is not a fix — it only moves the same cliff. So
the wall clock comes out of the assertion entirely.

**And the dangerous reading had to be excluded, not assumed.** If the timing sensitivity lived in the
WATCH rather than in its harness, a loaded machine would make the daemon **miss a change**, which is
invisible and looks exactly like a directory that did not change. It does not: `projects.Poll` calls
`Scan` afresh every pass and records what is there now, accumulating nothing and subscribing to no
change events. A tick Go's ticker drops under load — and it drops them, it does not queue them —
costs **latency and nothing else**. That is now asserted rather than read off the source.

## What Changes

- **`internal/commands/stats_criteria_test.go`** — the three #72 criteria as gated tests, plus two
  probes: one that asks the product's own hub seam whether it can reach a hub, one that counts the
  surfaces serving the statistics capability by parsing the tree. Neither names a build or a machine.
- **`internal/commands/projects_test.go`** — the wait becomes progress-based. It waits for a poll
  stamped after the file was written; such a poll's scan saw the file, so the count is then asserted
  exactly, first time. The bound is on a **stall** — a span in which the daemon recorded nothing —
  and every poll observed resets it.
- **`internal/projects/poll_starvation_test.go`** — the watcher is level-triggered, asserted under
  total tick starvation, which is strictly worse than any load.
- Two requirements: statistics criteria that cannot be driven are recorded as undriven; the watcher
  loses nothing to a pass it misses.

**No product code changes.** Every defect these tests describe is already absent from this build; the
change is that its absence is now driven.

### What was deliberately NOT built

- **A distinct reason code for "this build has no transport"**, separate from `hub-unreachable`.
  Considered and rejected. `statsSource` is not alone: `search`, `visibility`, `departed`, `note`,
  `references` and `auth` all return `ErrHubUnreachable` from the same stub seam. Giving statistics
  its own code would make one command disagree with six others about one condition — the exact
  two-answers-to-one-question defect #41 removed from daemon liveness. It belongs to #10, once, for
  all of them.
- **Criteria 1, 2 and 3 themselves.** They are blocked, not deferred for convenience. Issue #72
  stays open.
