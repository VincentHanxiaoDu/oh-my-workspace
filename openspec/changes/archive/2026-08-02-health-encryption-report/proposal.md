# Health reports full-disk encryption, in three honest values

## Why

Everything the product promises about data staying on a person's machine rests on one sentence:
the client does not encrypt its own store, it assumes the disk is already encrypted (PRD §4.1).
A person putting an unpublished ticket or an unfinished draft on a machine has no way to ask
whether that assumption holds there.

The answer has to be three-valued, and the third value has to be real. "Full-disk encryption is
not enabled" sends a person to turn FileVault or LUKS on. "I could not read the disk's encryption
state on this platform" sends them somewhere else entirely. Telling them the second as though it
were the first is the failure PRD §4.3 exists to forbid — and it is the failure a `(bool, error)`
with a dropped error produces by default.

Health is also the one capability that must work on a machine where nothing has been set up: no
store, no daemon, no hub. It is a question about the machine, not about the product's own state,
and it must never stand in a person's way.

## What Changes

- **A new `internal/health` package** that reports the deployment assumptions (PRD §3.9), of
  which full-disk encryption is the first. It answers with `internal/tri`'s three-valued type —
  it does not reinvent one — and renders `enabled`, `not enabled`, or `could not be determined on
  this platform`.
- **A real macOS probe**: FileVault via `fdesetup status`, parsed from the output because that
  command exits 0 whether FileVault is on or off.
- **A real Linux probe**: LUKS via `lsblk -rno FSTYPE`, falling back to `/dev/mapper` plus
  `cryptsetup status` where the block tree cannot be read.
- **A new `omw health` command**. `enabled` and `not enabled` both exit 0 — health answered, and
  it is a report, never a blocker. Only a check that could not be completed exits 3
  (`ExitUndetermined`), which is distinct from the generic failure code so that "I could not look"
  is never scripted as "the answer is no".
- **No platform with no probe is guessed at.** Windows has no probe in this slice (PRD §5.1 ships
  macOS and Linux) and reports the third value rather than a negative.
- **No store, no daemon, no network.** Health creates no file, starts no process other than the
  platform probe, and this slice's source imports no network-capable package.

Standard library only. No new dependency.
