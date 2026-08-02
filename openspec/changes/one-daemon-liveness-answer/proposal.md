# One answer to whether the daemon is running

## Why

A person starts the daemon. `omw daemon status` says it is running and its control socket is open.
Another command tells them the daemon is not running. One of the two is lying and they have no way
to tell which, so they stop trusting either.

`internal/commands` carried a **placeholder** liveness probe that outlived the thing meant to
replace it. It read a path from `OMW_CONTROL_SOCKET` and stat'd it. **Nothing in the product ever
set that variable**, so the probe answered "not running" unconditionally, whatever the daemon was
doing. There were two independent copies of the same guess — one in `visibility_cmd.go`, one in
`note_cmd.go` — each with its own constant.

The real answer landed with the daemon itself: `daemon.Inspect(storeRoot)`. `internal/daemon`
derives the control socket from the store root via `socketFor`, which falls back to a per-user
runtime directory whenever the in-store path would exceed the kernel's `sun_path` limit — which is
exactly why no caller outside that package can reconstruct it and stay correct.

This has surfaced three times in three pull requests on one day. Two were refused on those grounds:
an inbox listing that printed "daemon: not running" and *"this listing is the store on disk, not a
live inbox"* with a daemon running, and a projects listing that printed "watching: no — nothing is
watching between commands". A third inherited it without adding to it. Each branch was doing the
locally reasonable thing; the defect is that the shared probe is wrong, so it will keep recurring —
it already has — until there is one definition. It is currently latent on the default branch only
because both surfaces reach the probe after a hub check that short-circuits with no hub configured,
and it becomes visible the moment a hub exists.

Three properties of the fix matter more than the deletion:

- **Three answers, not two.** A lock that cannot be read is not a daemon that is absent. A bool
  cannot represent that, and collapsing it is how a confident false negative gets printed in the
  one case where the person most needs to be told that nothing was established (PRD §4.3).
- **Agreement is asserted between surfaces, not within each one.** Every branch's own tests passed.
  Text asserted in isolation is what let this through three times.
- **One definition is a property of the tree.** Replacing two placeholders fixes today; a structural
  check is what stops the fourth guess.

## What Changes

- **A new `internal/commands/liveness.go`** holding the one definition every surface in the package
  uses: `daemonLiveness`, which resolves the store the way `omw daemon status` does and asks
  `daemon.Inspect`, returning a three-valued answer and, when undetermined, a reason.
- **One rendering of the two non-running answers**, in `reportDaemonNotLive`: the established
  negative keeps the product's existing sentence and exit code; the undetermined answer gets its
  own wording, its own machine-readable code and `ExitUndetermined`, and its sentences never contain
  the negative's.
- **`internal/commands/visibility_cmd.go`** (merged work of the visibility Issue) — its
  `OMW_CONTROL_SOCKET` constant and its private probe are deleted; `omw visibility show` calls the
  one definition. This is a merged file edited on purpose: the shared probe is the defect.
- **`internal/commands/note_cmd.go`** (merged work of the note-versions Issue) — the second,
  independent copy of the same constant and probe is deleted; `reachHub` calls the one definition,
  so every `omw note` subcommand that needs the hub is fixed at one call site.
- **A regression test that asserts the surfaces against each other** with a daemon started by the
  real binary, in both directions and across the stop transition, comparing every surface with what
  `omw daemon status` prints — plus a criterion-5 sweep over every registered command, so a surface
  added later is covered without its author knowing this test exists.
- **A structural test** that no package outside `internal/daemon` derives, names or stats a control
  socket path, with the search pointed at a known match first so it cannot pass vacuously.
- **No restructuring of either command.** The guess is replaced by one call; nothing else moves.
