# Tasks

## The projects package

- [x] Write `internal/projects/doc.go` stating the provenance rule, the nothing-watches-without-the-
      daemon property, the three-way distinction and the declared boundary with Issue #2
- [x] Define `Project`, and derive its id from the cleaned absolute path so the same directory typed
      three ways is one project
- [x] Implement `Add` refusing anything that is not an existing directory, and returning the existing
      record untouched when the directory is already registered
- [x] Implement `Remove` so it touches nothing outside the store, and removes the polled state with
      the project so a re-add cannot be served a poll from a daemon run that ended weeks ago
- [x] Implement `List`, ordered by path rather than by the hashed id
- [x] Define `Provenance` with the unrecorded zero value, a distinct rendering for each branch, and a
      named form on the wire so a control API does not depend on constant ordering
- [x] Define `State` with every failable determination as a `tri.Value`, and `Empty()` returning
      undetermined rather than "nothing" for a directory that could not be fully read

## The walk

- [x] Recursive walk with a depth cap defaulting to 8, overridable through `$OMW_PROJECT_DEPTH`, with
      an unparseable value falling back to the default rather than scanning nothing
- [x] Record that the depth limit was reached, so a truncated walk never renders as a complete one
- [x] Do not follow symlinks, so a link pointing at its own ancestor terminates instead of looping
- [x] Prune the hardcoded name list and every dot-directory DURING the walk, and count directories
      entered so "not traversed" is observable rather than merely claimed
- [x] Inside a git repository take the ignore set from `git ls-files --cached --others
      --exclude-standard`; parse no `.gitignore` anywhere
- [x] Apply the prune list and dot rule on top of git's answer, since criterion 18 is not scoped to
      outside a repository
- [x] Report an unreadable subdirectory, continue the walk, and keep the result distinguishable from
      a complete scan
- [x] Apply that same rule on the GIT path: capture `git ls-files` stderr and treat any output as
      evidence the read was not clean, because git warns about an unreadable directory and still
      exits 0 — a zero exit is evidence of no fatal error, not of a complete read
- [x] Count an unrecognised warning too, so a future git wording or an unanticipated locale cannot
      silently become a complete scan
- [x] Keep an unreadable ROOT distinct from an unreadable subdirectory — the first is criterion 10's
      undetermined marking, the second is criterion 21's partial read

## Watching

- [x] Take the liveness answer as an argument (`projects.Liveness`) rather than establishing one
      here, so this package cannot become a second answer to "is the daemon running" (Issue #41)
- [x] Fill it in at the CLI from `daemonLiveness`, the one probe wrapping `daemon.Inspect` — the
      same call `omw daemon status` renders
- [x] Make the provenance three-valued to match: where liveness was not established, walk the
      directories but stamp the provenance undetermined rather than "examined during this command"
- [x] Render the three watching answers distinguishably, with the undetermined one carrying a reason
      and never the established-absence sentence
- [x] `Poll` writes only the polled state and announces nothing about its own liveness
- [x] `Run` is the whole contract the daemon has with this package, and nothing else starts a poll
- [x] Make `Run` survive a failing poll instead of exiting: the daemon's own write probe already
      decides when to stop, and a second exit decision on less evidence would stop watching silently
- [x] WIRE IT IN, so that a running daemon actually polls — `omw daemon run` starts the polling for
      the length of the run, which is what makes criterion 4 true rather than merely available
- [x] Assert structurally that only the daemon's run command can start the polling

## The command

- [x] Register `omw projects` with `add`, `list` and `remove`, refusing flag-shaped arguments rather
      than treating them as directory names
- [x] Open the store without ever creating one, with "there is no store" and "where the store lives
      could not be determined" on different exit codes
- [x] Render every entry's provenance unconditionally, and state the watching answer in the header
- [x] Exit `ExitUndetermined` when whether anything is watching could not be determined

## Driving it

- [x] Drive criterion 4 by running the poller and reading the STORE, not a listing, so the
      observation cannot be what caused the state to advance
- [x] Drive criterion 4 END TO END against a real daemon started by the real binary: change a file,
      run nothing, and require the polled record to advance
- [x] Drive criterion 21 on the git path with a real permission bit, pinning BOTH arms on one
      repository — readable, then chmod 000, then restored — so the marking tracks the world
- [x] Drive criterion 5 by stopping the poller, changing a file, waiting past the interval and
      comparing every file in the store byte for byte
- [x] Compare the missing, unreadable, empty and real-count renderings PAIRWISE inside one listing
- [x] Probe whether unreadable directories can be built in this environment rather than naming an
      operating system or a uid
- [x] Assert structurally that the package imports nothing that can open a socket, with a control
      that fails if the walk examined no files
- [x] Mutate provenance out of the listing, make missing and empty identical, drop a missing project,
      make a listing start the daemon, half-fix the prune to walk-then-filter, silence truncation,
      follow symlinks, and disable the git path — confirm each goes red naming the defect, and revert
- [x] Mutate the liveness wiring too: bypass the one probe, collapse an undetermined provenance to
      "examined during this command", and delete the negative header branch — confirm each goes red
- [x] Drive criterion 7 against a REAL daemon started by the real binary, not a fixture this package
      invented, and assert agreement with `omw daemon status` rather than with a local record

## Not done, and why — read with the pull request, which says the same thing

Two things named in the proposal are deliberately absent rather than ticked. Both are stated on the
pull request as well, because a tasks file nobody re-reads is not where a gap should live.

- A test running Issue #2's real control API against the CLI. It is on another branch, under review,
  and cannot be imported. What is here instead: one determination, one wire form, and a test that
  both renderings agree on all three markings.
- The polling is started from `internal/commands/projects_daemon.go` rather than registered with
  `daemon.RegisterBackground`, because that registry is on Issue #6's branch (PR #40) and not on
  `main`. It is one function called from one line, and the file says how to migrate it. It MUST be
  migrated when #40 lands: two ways to run daemon background work is a second mechanism, and this
  branch has already been refused once for shipping a second answer to a settled question.
