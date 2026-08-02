# Tasks

## The store package

- [x] Write `internal/store/doc.go` stating the five invariants the package guarantees
- [x] Define the distinct, `errors.Is`-able failures in `internal/store/errors.go`, with a
      `PathError` carrying the operation, the path and the specific finding
- [x] Resolve the one-per-device store path from the environment only, never from the working
      directory, with a missing home reported as undetermined rather than fallen back on
- [x] Define `Record`, `Kind` and the on-disk envelope, general enough for tickets and draft notes
      with nothing ticket-shaped in the package
- [x] Validate kinds and ids as single path segments so a record cannot be written outside the store
- [x] Implement `Create`, refusing an existing store, a missing path, a synchronising location, an
      undetermined sync probe and an unwritable location — in that order, leaving nothing behind
- [x] Implement `Open` so that it never creates, and `Exists` so that it answers in three values
- [x] Implement `Put`, `PutStream`, `Get`, `GetJSON`, `PutJSON`, `List`, `Delete` and `Kinds`
- [x] Make every write atomic: stream into a temporary in the destination's directory, fsync,
      rename, fsync the directory
- [x] Checksum every record's payload so damage beneath the product reads as unreadable, not absent
- [x] Make `List` fail on a damaged record rather than skipping it

## Sync detection

- [x] Detect Dropbox, iCloud Drive, OneDrive and roaming-profile markers by reading the target's
      ancestry, with no branch on which operating system is running
- [x] Detect network filesystems from the mount table where the system publishes one, and treat its
      absence as silence rather than as a negative answer
- [x] Follow symlinks so a link into a sync root is judged by where it lands
- [x] Judge a path that does not exist yet from its nearest existing ancestor
- [x] Return `Undetermined` with a reason when a level cannot be listed, and never `No`
- [x] Prefer determined evidence over an unreadable level
- [x] Render the three states distinguishably, with none of them blank

## The command

- [x] Add `internal/commands/store.go` registering `omw store` with `create`, `path` and `status`
- [x] Map each store failure to its own sentence and its own exit code
- [x] Exit `ExitUndetermined` on an undetermined sync probe, saying the state could not be
      determined, that the product has no ruling on whether to proceed, and naming Issue #3
- [x] Report an existing store's location every time it is asked, so the sync refusal is not a
      one-time gate
- [x] Keep "no store here", "the store is here and empty" and "the store cannot be read" three
      different outputs on three different exit codes where the rule requires it

## Tests

- [x] Drive the synchronising refusal from synthetic sync roots built on the running platform, so
      the same assertions run on macOS and on Linux
- [x] Drive the three creation failures and assert they are distinguishable by value and by text
- [x] Drive the undetermined case and assert it shares an exit code with neither settled outcome
- [x] Kill a subprocess mid-write three times over and assert every complete record survives, the
      interrupted one is absent, and the kill genuinely landed on a partial write
- [x] Drive the sole-home criterion by searching a sandboxed filesystem for content written into a
      ticket and a draft
- [x] Assert the store package links neither the network nor process-spawning machinery
- [x] Build the real binary, create a store, and assert nothing is left running and the next command
      finds the store

## Verification

- [x] Break the atomic write and watch the crash test go red, then revert
- [x] Make an undetermined sync probe render as "not synchronising" and watch three tests go red,
      then revert
- [x] Make a second creation overwrite the existing store and watch two tests go red, then revert
- [x] Collapse two of the three creation failures into one message and watch the criterion-6 test go
      red, then revert
- [x] `make ci` green with every mutation reverted
