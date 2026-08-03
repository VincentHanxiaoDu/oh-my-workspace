# Tasks

## The one definition

- [x] Add `internal/commands/liveness.go` with `daemonLiveness`, the single answer to whether the
      daemon is running against this invocation's store: resolve the store exactly as
      `omw daemon status` does, then ask `daemon.Inspect`, so the two cannot disagree
- [x] Return a three-valued answer rather than a bool, with a reason string that is empty for a
      determined answer and never empty for an undetermined one
- [x] Write the file's comment to say why the socket path is not reconstructed here — `socketFor`
      falls back to a per-user runtime directory above the `sun_path` limit, so a caller
      reproducing the rule will disagree with the daemon about a daemon that is running
- [x] Add `reportDaemonNotLive` so the two non-running answers are rendered in exactly one place,
      with different sentences, different machine-readable codes and different exit codes

## Removing the guesses

- [x] Delete `visibility_cmd.go`'s `OMW_CONTROL_SOCKET` constant and its `daemonRunning` probe, and
      call the one definition from `omw visibility show`
- [x] Delete `note_cmd.go`'s independent copy of the same constant and its `noteDaemonRunning`
      probe, and call the one definition from `reachHub`, which every hub-facing `omw note`
      subcommand already goes through
- [x] Leave both commands otherwise unrestructured — the change is one call in place of one guess
- [x] Record in both files why a merged file was edited, so a reader meets the reason where the
      deletion is rather than only in the pull request

## Driving it

- [x] Add `TestEveryDaemonReportingSurfaceAgreesWithDaemonStatus`: start a real daemon with the real
      binary, then run every reporting surface and compare each with what `omw daemon status`
      prints — both directions, and again after a real stop
- [x] Read each surface's claim from the machine-readable codes rather than from prose, and read
      the status command's claim from its rendering rather than from `daemon.Inspect`, so the
      comparison is between two things a person reads
- [x] Keep the surfaces in one slice so a surface added later joins the agreement test by one line
- [x] Assert criterion 5 over every registered command, not only the listed surfaces
- [x] Add `TestALivenessThatCannotBeEstablishedIsUndeterminedAndNotANo`, staging a store whose lock
      genuinely cannot be opened rather than stubbing the answer
- [x] Add `TestTheThirdAnswerAndTheNegativeShareNeitherWordingNorExitCode`
- [x] Add `TestNoPackageOutsideDaemonDerivesAControlSocketPath`, with the search pointed at the
      removed placeholder first and required to fire before its verdict on the tree is believed,
      and a second control requiring the walk to have examined something
- [x] Rewrite the fixtures that bound a real unix socket to make the old probe say yes — the
      fixture was shaped by the defect — and state in the helper why it no longer names a socket

## Watching them fail

- [x] Mutate the liveness answer to a hardcoded "not running" (the shipped bug) and confirm the
      agreement test goes red naming Issue #41
- [x] Mutate it to a hardcoded "running" and confirm both the no-daemon direction and the
      after-stop direction go red
- [x] Mutate the undetermined rendering into the "not running" one and confirm the undetermined
      test goes red on the answer, the exit code, the wording and the missing reason
- [x] Point the structural search at a directory with no Go files and confirm it refuses rather
      than passing
- [x] Reintroduce the placeholder constant and `os.Stat` in a product file and confirm the
      structural search finds it
- [x] Revert every mutation and confirm `make ci` is green
