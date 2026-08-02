# Tasks

## The daemon package

- [x] Write `internal/daemon/doc.go` stating why the lock is advisory and per store, why the crash
      rendering is inferred rather than stored, why the control API refuses, and why the write
      probe's failure and the phase change are one critical section
- [x] Define `Ending` with five values whose zero value is undetermined, spelled in exactly one
      place, with an on-disk code that is separate from the rendering so rewording a sentence does
      not reinterpret a record written yesterday
- [x] Define `Report` as the one state type the control API serves and the CLI renders, carrying its
      three-valued answers over the wire as their own renderings rather than as integers
- [x] Give the report a `Healthy` answer separate from `Running`, three-valued, so that a reader who
      cannot reach the control API is never told the daemon is fine
- [x] Write and read the run record atomically — temporary in the destination's directory, fsync,
      rename, fsync the directory — carrying forward how the PREVIOUS run ended
- [x] Take the store's lock with a non-blocking advisory whole-file lock, in build-tagged files, and
      report a build without one as undetermined rather than as free or held
- [x] Report a lock left by a dead process precisely as stale, and never as a live conflicting daemon
- [x] Infer a crash from a run record that says "running" while nothing holds the lock, and refuse to
      infer one when liveness itself could not be determined
- [x] Confirm owner-only permissions by a real stat of the mode and the owner, in build-tagged files,
      returning undetermined where the system publishes no owner
- [x] Open the control API only after confirming its directory, then its socket, closing and removing
      what was opened on anything other than a confirmed yes
- [x] Make the write probe a real write through the store, injectable so the stopping behaviour is
      drivable
- [x] Stop the daemon when it cannot write, recording the reason before anything else, and change the
      phase inside the same critical section that observes the failure
- [x] Prove the daemon can write as part of starting, so that a start that returns has established it
- [x] Make `Inspect` ask the control API when one is open, so the CLI and the control API cannot
      report different states
- [x] Place the control socket in a short per-user runtime directory when the in-store path exceeds
      the kernel's socket-address limit, still owner-only and still confirmed

## The command

- [x] Add `internal/commands/daemon.go` with `start`, `run`, `stop` and `status`, as a new file that
      touches nothing else in the package
- [x] Make `start` launch the daemon, wait for it to report that it has taken the lock, and return an
      exit code that is a fact about the daemon
- [x] Classify a start failure into a token the child sends and the parent words, so the sentence
      telling a lock conflict apart from a missing store is written in the process a person reads
- [x] Say a missing store in the store package's own wording, so `omw daemon` and `omw store` report
      one fact rather than two different-sounding failures
- [x] Give the lock conflict, the missing store and an undetermined lock distinct sentences AND
      distinct exit codes, with `ExitUndetermined` reserved for what could not be determined
- [x] Make `stop` return only once the lock is actually released, and say something different when
      there was nothing to stop
- [x] Make `status` read and start nothing, and report the refused control API in wording that is
      neither "not running" nor "running normally"
- [x] Treat SIGTERM and SIGINT as an explicit stop, and detach the child into its own session so a
      closing terminal does not look like one

## Tests, and watching them fail

- [x] Compare the five endings PAIRWISE, both as renderings and end-to-end through the real
      lifecycle, rather than each against its own literal
- [x] Drive each ending by doing the thing: never running, stopping, losing the ability to write,
      being killed, and a record that cannot be read
- [x] Cover a run record that will not OPEN as well as one that will not PARSE — added after a
      mutation of the first branch left every test green
- [x] Drive the socket's own confirmation separately from its directory's — added after a mutation of
      each left every test green, because the other one caught it
- [x] Assert criterion 17 as a monotonicity property under a concurrent poller, plus a decisive
      sample taken after the daemon has observed the failure
- [x] Read the package's own syntax tree to assert no transport other than `unix` exists
- [x] Drive start, stop, the second-start refusal and a real kill through the built binary, in
      separate processes
- [x] Assert over EVERY registered command that none of them starts the daemon
- [x] Probe the environment rather than naming a platform: skip on a filesystem that does not keep
      permission bits, skip where no Go toolchain exists to build the binary, and drive the
      owner-only refusal through an injected seam so it runs everywhere
- [x] Record each mutation and its exact failure message in the pull request body

## The device pointer, carried in from #3

- [x] Cherry-pick `8d095f5` from `dev/feat/3-store-create` rather than writing a second fix, so the
      two branches do not diverge on one file
- [x] Sandbox both `XDG_DATA_HOME` and `HOME` in this package's own binary spawn, which the
      cherry-picked structural check flags — a latent hazard here rather than damage that happened,
      since nothing in the daemon tests writes the pointer
- [x] Report that the check also flagged `omw daemon start` launching its own child, where
      inheriting the person's environment is the correct behaviour and a blanked `$HOME` would be
      the defect rather than the guard — rather than contorting the product to go green
- [x] Take `60fb700`, the narrowing made on #3's branch where the check lives, and revert the
      equivalent local edit so the two branches do not diverge on one file
- [x] Re-drive both spawn mutations after the narrowing, and confirm the vacuous-pass control still
      reports the spawns it examined

## What a merge simulation found that this branch cannot see

- [x] Test-merge `main` + #27 + this branch in a scratch clone and run the whole suite there, rather
      than trusting that a branch green on its own base is green on the base it lands on
- [x] Report that the merged tree fails #12's `TestVisibilitySurfacesCannotOpenANetworkConnection`,
      because that guard bans the `net` package outright and this daemon's control API is a unix
      socket — attributed by bisecting the merge, and left for its owner to narrow rather than
      worked around here
- [x] Record that `omw health` has since landed on `main`, so Issue #1's carried-forward criterion 14
      is drivable after all — and drive it in the merged tree, where it passes
- [x] Measure the real pointer before and after `go test ./internal/commands/` and record the
      result, rather than asserting from the code that it is safe
- [x] Re-verify the merge against `main` after #27 landed (`669efda`), rather than leaving a finding
      measured against a base that has since moved
- [x] State the four findings and the two unruled decisions at the TOP of the pull request body,
      where a reviewer meets them, rather than only inside a mutation table
