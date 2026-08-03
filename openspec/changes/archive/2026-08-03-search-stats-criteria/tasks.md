# Tasks

Only what shipped is listed. What is not here is in "Not done, deliberately" below, and in Issue #72,
which stays open.

## 1. The flake — establish where the timing sensitivity lives

- [x] 1.1 Identify the flake. It is `TestARunningDaemonPollsProjectsWithNoCommandRun`
      (`internal/commands`), named in #72's addendum. It is NOT the watch-gate self-test flake
      recorded on #32 — different test, different language, different failure text.
- [x] 1.2 Measure it idle: 2.7s across three consecutive runs, against a 10s per-wait deadline.
- [x] 1.3 Reproduce the pressure under sustained parallel load — 28 CPU burners and three concurrent
      full suites on 14 cores, load average 20→47. Slowest run 12.27s, past the deadline.
- [x] 1.4 Determine whether the sensitivity reaches the WATCH or is confined to the harness.
      Confined to the harness: `projects.Poll` re-scans every pass and records current truth.
- [x] 1.5 Assert 1.4 rather than reading it off the source —
      `internal/projects/poll_starvation_test.go`, driven under TOTAL tick starvation, which is
      strictly worse than any load: added files, a removed file, and a churn faster than the interval.

## 2. The flake — remove the timing dependence

- [x] 2.1 Replace the total-elapsed deadline with a STALL bound that every observed poll resets.
- [x] 2.2 Take the wall clock out of the assertion: wait for a poll stamped after the write, then
      assert the count once. A wrong count now reports a wrong count, not a timeout.
- [x] 2.3 Probe the daemon rather than assume it: `tri.No` fails (start returned, nothing running),
      `tri.Undetermined` SKIPS with a reason saying it determined nothing and has not passed.
- [x] 2.4 Watch it go red. Mutation M1 (the watcher announces once and stops) reddens both starvation
      tests. Mutation M2 (the poll records a stale count forever) reddens the commands test in 3.01s
      with a WRONG COUNT — where the old form would have timed out at 10s.
- [x] 2.5 Prove the fix is not a longer timeout. Mutation M3 sets the interval to 12s — longer than
      the old 10s deadline, which it fails by arithmetic — and the new test passes in 14.11s.
- [x] 2.6 Confirm each mutation by grep before running, and revert each after. `-count=1` throughout.

## 3. The statistics criteria — probes, and tests that refuse to pass

- [x] 3.1 Read what #42 merged first, and extend it. `TestStatsCLIAndAgentAPIAgree` and
      `TestStatsHubUnreachableIsNotTheHubReportingNothing` already exist and are not duplicated; the
      new tests say in their comments what each already drives and why it is not the criterion.
- [x] 3.2 `probeHubTransport` — calls the product's own hub seam, captured before any harness swaps
      it, and reports the reason code it gave. Not a build tag, not a version, not a machine.
- [x] 3.3 `probeStatisticsSurfaces` — counts the surfaces serving the capability by PARSING the tree.
      Parsing and not grepping: a grep counts a mention in a comment, and this file's own prose names
      the function, so a grep would turn writing a sentence into a false red on a gate.
- [x] 3.4 Criterion 1 as `TestCriterion1CLIAndTheAgentAPIAreTwoSurfaces`. Skips today, and FAILS the
      day a second surface appears while the comparison is unwritten.
- [x] 3.5 Criterion 2 as `TestCriterion2AReachableHubWithNothingReadableIsNotAnUnreachableOne`, with
      the full assertions ready to run behind the transport probe.
- [x] 3.6 Criterion 3 as `TestCriterion3PublishingAnInvisibleNoteLeavesTheReadersStatisticsByteIdentical`,
      with the CONTROL stated and asserted: a note the reader CAN see must move the count and the
      recency, or the probe beside it establishes nothing.
- [x] 3.7 Every skip message states the criterion, the reason the probe gave, the Issue that unblocks
      it, and that it determined nothing and did NOT pass.
- [x] 3.8 #72's third section — the local half's confident zero about another person's corpus — driven
      as `TestTheLocalHalfIsAboutTheLocalOutboxAndSaysSo`. It is not a defect and the test pins why:
      criterion 5 REQUIRES the local zero, and what must hold is that the hub half beside it is not
      quietly determined.
- [x] 3.9 Watch them go red. Mutation B (a second surface) reddens criterion 1. Mutation A (a working
      transport) makes criteria 2 and 3 RUN and pass; stacking A2 (the reader silently widened)
      reddens both — criterion 3 on its CONTROL, which is the guard working.
- [x] 3.10 Mutation C (the hub half rendered as a determined zero) reddens 3.8.

## 4. Specification

- [x] 4.1 `openspec/changes/search-stats-criteria/` with proposal, tasks and two spec deltas.
- [x] 4.2 Not archived.

## 5. Gates

- [x] 5.1 `make ci` green.
- [x] 5.2 `./.workflow/bin/run-gates.sh` green before pushing.
- [x] 5.3 Full suite re-run under sustained parallel load, to confirm the fix under the condition
      that produced the flake.

## Not done, deliberately

- **#72 criteria 1, 2 and 3.** Blocked on #16 and #10, which are open. They are recorded as undriven
  and refuse to report a pass. Issue #72 stays open.
- **A distinct reason code for "this build has no client→hub transport."** Six other commands return
  `ErrHubUnreachable` from the same stub seam; giving statistics its own would make one command
  disagree with six about one condition. It belongs to #10, once, for all of them.
- **Splitting the flake into its own Issue.** #72 offers the choice. Fixed here instead, because it
  arrived here and leaving it open buys nothing.

## Could not be determined

- **Whether the #32 watch-gate flake and this one share a cause.** They are different tests. The
  measurements here say nothing about `watch-prs.sh` or `watch-reviews.sh`, whose self-tests were not
  re-driven, and #32's cause remains unestablished. Nothing here closes it.
