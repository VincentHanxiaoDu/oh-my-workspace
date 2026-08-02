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

## The ruling on an undetermined location (criteria 23-25)

- [x] Add `AcceptUndeterminedLocation`, the explicit override, so an undetermined location can be
      created in on purpose
- [x] Keep the refusal for a location KNOWN to synchronise unoverridable — the option is not
      consulted in that branch at all
- [x] Record the override in the store's marker, and expose it as
      `Store.CreatedAtUndeterminedLocation`
- [x] Re-probe the location on every report so an override is never rendered as a confirmation
- [x] Name the override in the halt message, and delete the claim that the product has not ruled

## Arguments (review finding 2)

- [x] Parse `omw store` arguments instead of treating every argument as a path
- [x] Reject unknown arguments beginning with a dash, non-zero, creating nothing
- [x] Give `create` a real `--help` that documents the override and what it will not do
- [x] Name the real flag when the person types `--force`, `--yes`, `-f` or `-y`
- [x] Support `--` so a path that legitimately begins with a dash is still reachable

## One store per device (review finding 3, criterion 4)

- [x] Record which store is this device's store in a pointer file holding a path and nothing else
- [x] Refuse creation at a second path while a registered store is still there
- [x] Treat a pointer to a store that is gone as stale, so a machine is never left unable to create
- [x] Refuse rather than proceed when the pointer exists and cannot be read
- [x] Make `Resolve` consult the pointer, so later commands find the store that was created

## The default location on a fresh machine (review finding 4)

- [x] Create the product's own containing directory when the location is the product's default, and
      say so in the output
- [x] Leave a location the person named alone: a missing parent there is still "this path does not
      exist"

## Verification

- [x] Break the atomic write and watch the crash test go red, then revert
- [x] Make an undetermined sync probe render as "not synchronising" and watch three tests go red,
      then revert
- [x] Make a second creation overwrite the existing store and watch two tests go red, then revert
- [x] Collapse two of the three creation failures into one message and watch the criterion-6 test go
      red, then revert
- [x] Make the override defeat the known-synchronising refusal and watch two tests go red, then
      revert
- [x] Make an undetermined location render as a confirmation and watch three tests go red, then
      revert
- [x] Stop rejecting unknown flags and watch the flag test go red, then revert
- [x] Remove the one-store-per-device check and watch three tests go red, then revert
- [x] Stop recording the created store as this device's store and watch five tests go red, then
      revert
- [x] Conjure the parent of a path the person typed and watch three tests go red, then revert
- [x] `make ci` green with every mutation reverted
