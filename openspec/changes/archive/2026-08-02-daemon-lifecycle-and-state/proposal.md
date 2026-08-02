# Daemon lifecycle and state

## Why

The store exists and nothing uses it. PRD §2.1 says the product is a long-running process on one
person's machine that watches projects, ingests from channels and owns the store's write lock — and
this build has no such process, so five other capabilities have nothing to attach to.

Building it is not the hard part. Four promises are, and each of them is a place where a daemon that
"works" is worse than no daemon at all:

- **§4.3, it says how its last run ended.** A daemon that died overnight and comes back up smiling
  has told the person their machine has been working when it has not. Ended on purpose, ended
  because it could not write, ended without recording an ending, never run, and could not be
  determined are FIVE answers, and the fifth is not silence.
- **§4.3, it stops when it cannot write.** A daemon running while every write fails is a person
  believing their tickets are being captured while they are being dropped. It has to stop, and there
  must be no moment at which it reports itself fine after knowing otherwise.
- **§4.2, nothing implicit.** If a person did not start it, it is not running, and whatever they
  typed says so. No command anywhere brings it up as a side effect.
- **§4.6, the control API is local and demonstrably so.** It confirms its socket is owner-only before
  opening and does not open if it cannot confirm it. The refusal is the feature: a control API that
  opens anyway on a platform where it cannot prove who can reach it has quietly published a person's
  private material to whoever is on the machine.

## What Changes

- **A new `internal/daemon` package** — the long-running process, the store's write lock, the run
  record, and the local control API. It consumes `internal/store` and adds nothing to it.
- **`omw daemon start | run | stop | status`**, a new file in `internal/commands`. `start` launches
  the daemon and returns only once it is genuinely running, so its exit code is a fact about the
  daemon rather than about the launch; `run` is the foreground process a service manager wants;
  `stop` returns once the lock is actually released; `status` reads and starts nothing.
- **One daemon per store, enforced by an advisory whole-file lock inside the store.** The lock is per
  STORE, so two stores on one machine each get a daemon. Because the kernel releases an advisory lock
  when its holder dies, a lock left by a dead process is never a live conflicting daemon — that is a
  property of the mechanism rather than a guess about whether a recorded pid is still alive. The pid
  in the file is used only to SAY that what was found was stale.
- **Five distinct renderings for how the last run ended**, spelled in exactly one place, with the
  crash rendering INFERRED rather than stored: a run record that still says "running" while nothing
  holds the lock describes a process that did not get to write its ending. Storing "crashed" would
  require the crashed process to have written it.
- **Health is a separate answer from running, and it is three-valued.** "Something holds the lock"
  and "that something can still write" are two facts, and only the daemon knows the second. A reader
  with no control API is told the daemon is running and that its health is UNDETERMINED — never that
  it is fine. The daemon's own answer flips to "not healthy" inside the same critical section that
  observes a failed write, so there is no interleaving in which it says fine after knowing otherwise.
- **The write probe is a real write through the store**, not a capability check, because the only
  thing that establishes that the daemon can write is having written. It is injectable so that the
  stopping behaviour is drivable without filling a disk.
- **The control API is a unix domain socket, confirmed owner-only before it opens.** The containing
  directory is confirmed BEFORE anything listens, then the socket is created, tightened and confirmed
  in its own right; anything other than a confirmed yes closes the listener and removes the socket.
  An undetermined confirmation refuses exactly as a determined negative does, and the two are still
  reported differently. There is no other transport in the package at all, and a test reads the
  package's own syntax tree to keep it that way.
- **The control API and the CLI cannot disagree, because the CLI asks the control API.** One report
  type, one renderer; when the control API is open its answer IS the CLI's answer.
- **A refused control API does not stop the daemon.** It runs, watches and records how its run ended
  regardless — which is Issue #1's carried-forward criterion 14 made drivable for the first time.

### Decided here, and not settled by the Issue

- **`start` detaches; `run` is the foreground process.** The Issue requires that the daemon be
  running after the start command returns, which a foreground process cannot satisfy. `run` is
  documented rather than hidden, because a person under a service manager needs it.
- **Starting includes proving it can write.** A daemon that cannot write does not start at all,
  rather than starting and stopping an interval later. Without this, `omw daemon status` run
  immediately after a successful start answered "could not determine" about a healthy daemon.
- **Where the store's path makes an in-store socket path too long for the kernel's socket-address
  field, the socket moves to a short per-user runtime directory.** Still a unix socket, still
  confirmed owner-only, one per store. The lock and the run record — which do say something about the
  person's store — stay inside the store.
