# Projects: watching, listing, and saying which of the two happened

## Why

A person has three or four directories that actually matter and wants the client pointed at them.
PRD §3.6 promises the client keeps up with them — but the promise has a shape that is easy to build
wrong and impossible to notice from the outside once it is:

- **Watching is a poll, and with no daemon running nothing watches at all.** Those are two different
  situations. A listing produced by a daemon that polled two seconds ago and a listing produced by
  walking the directories during the command carry the same fields, and a build that prints only the
  fields hands a person a stale picture that looks identical to a live one. §3.6 says the listing
  must say **which of the two happened**, and that is the centre of this change.
- **§4.3, undetermined is a real answer.** A directory that is missing, one that exists and cannot be
  read, and one that exists and is empty are three facts about a person's work. The reference
  implementation the owner named (`hxd_underpants`) collapses all three — it swallows I/O errors and
  cannot tell them apart — and the owner ruled explicitly that this build must not.
- **§4.2, nothing implicit.** Listing projects must not start the daemon, and with no hub configured
  must not reach the network.

There is nothing in the build that holds a project, so the daemon (#2), the status line and the
reports have nothing to watch.

## What Changes

- **A new `internal/projects` package** holding the registry of directories a person has pointed the
  client at, and the ONE determination of what state each is in. `Snapshot` is that determination;
  the CLI renders it and Issue #2's control API is to serve the same struct, so the two surfaces
  cannot disagree (§4.3, "the control API and the CLI report the same state").
- **Provenance is recorded where state is produced, not derived where it is printed.** `Poll` stamps
  daemon-polled on what it writes; a listing stamps examined-during-this-command on what it walks
  itself. Every rendered entry carries its stamp unconditionally, and the unrecorded zero value
  renders as a defect marker rather than as either real answer — the same shape as `tri.Undetermined`
  being `tri`'s zero.
- **Nothing watches unless something calls `Poll`.** There is no init, no package-level goroutine and
  no lazy start anywhere in the package, so "with the daemon stopped, no state anywhere advances
  between commands" is a property of the code rather than a hope. `Run` is the entire contract the
  daemon has with this package.
- **A missing directory is marked, never dropped**, and missing / unreadable / partially-read / empty
  / a real count are five distinct renderings produced by one switch, so no two can converge without
  a reader seeing them on adjacent lines.
- **The walk follows the owner's ruling on the reference implementation**: recursive to 8 levels by
  default (deeper than that project's 4) and configurable; symlinks not followed; the prune list
  pruned DURING the walk rather than filtered after; every dot-directory pruned; and no `.gitignore`
  parser anywhere — inside a repository the ignore set is whatever `git ls-files` reports.
- **Reaching the depth limit and hitting an unreadable subdirectory are both visible.** A truncated
  walk and a partially-read project each render distinguishably from a complete scan.
- **A new `omw projects` command** with `add`, `list` and `remove`. It never creates a store, never
  starts the daemon, and imports nothing that can open a socket.

## What this change does NOT do

- **The control API half of §4.3 is not driven.** Issue #2 owns the control API and is under review on
  another branch, so it cannot be imported. This change makes agreement structural — one snapshot,
  one wire form — and tests that the CLI rendering and the wire form agree on provenance, the missing
  marking and the undetermined marking. A test that runs both REAL surfaces belongs where both exist.
- **No daemon is started or shipped by this change.** `Run` is a function Issue #2's daemon calls.
