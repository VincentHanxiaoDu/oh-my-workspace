# store Specification

## Purpose

The local store is the sole home of everything a person has not published — every ticket, every
unfinished draft (PRD §3.14, §2.3). Nothing else on the machine is a second copy, which is what makes
"nothing leaves the machine unless the person publishes it" a statement about one place rather than a
hope about many.

It is created by an explicit act, never as a side effect of some other command (PRD §4.2). One store
per device.

**The disk is the boundary, so where the file lives is not a detail.** The client does not encrypt
its own store; it assumes full-disk encryption (PRD §4.1). That assumption survives only while the
file stays on this disk, so the store refuses to live anywhere that synchronises off the machine —
Dropbox, iCloud Drive, OneDrive, a roaming profile, a network filesystem. **That refusal is not
overridable.** A location that copies itself to another company's servers is not your disk, and an
override there would turn the product's central guarantee into a preference.

The harder case is the one in between, and the product ruling settles it: **where the check cannot
conclude, creation halts and says so — and the person may proceed by an explicit act they type.**
Three things follow, and each is a distinct answer rather than a rounding of the other two:

- Undetermined is rendered as undetermined. It is never a "no", never a silent pass, and never
  confused with the refusal given for a location known to synchronise (PRD §4.3).
- The override reaches the undetermined case only, never the determined one.
- A store created through the override **still reports its location as undetermined**. The override
  records that a person accepted the risk; it does not manufacture a confirmation nobody made.

Finally, the store survives interruption. A process killed mid-write leaves a store that still opens
and still lists every record complete before the interruption — a half-written record is absent, not
readable as a record with missing fields. A missing value and a real value must never render
identically, and that rule reaches all the way down to the bytes on disk.
## Requirements
### Requirement: The store is created by an explicit act, and only once per device
The product SHALL create a store only when a person runs the creation command, SHALL print the
absolute path it created, and SHALL refuse a second creation at an existing store without altering
a byte of it.

#### Scenario: A first creation on a machine with no store
- **WHEN** a person runs the store-creation command on a machine that has no store
- **THEN** exactly one store is created, the absolute path it was created at is printed, and the
  command exits zero

#### Scenario: The default location on a machine that has never run the product
- **WHEN** the creation command is run with no location named, on a machine where the product's own
  directory does not exist yet
- **THEN** the product creates its own directory, says that it did, and creates the store — the
  default location is not unreachable on the machine the default exists for

#### Scenario: A location the person named whose parent does not exist
- **WHEN** the creation command is given a location, by argument or environment, whose containing
  directory does not exist
- **THEN** creation is refused and says the path does not exist, because a location the person typed
  is theirs and a missing parent there is a mistyped path

#### Scenario: A second creation against an existing store
- **WHEN** the creation command is run again against a path that already holds a store
- **THEN** no second store is created, the existing store is left byte-identical, and the command
  exits non-zero with a code different from the first run's

#### Scenario: A command that is not the creation command
- **WHEN** any other command runs with no store present
- **THEN** no store is created, and the command names the absence of a store as the reason it could
  do no more — worded differently from a store that is present and empty

#### Scenario: The store is resolved from any working directory
- **WHEN** the product resolves the store's location from different working directories
- **THEN** it resolves to the same single path every time, and never searches upwards for a nearby
  store

#### Scenario: Creation records which store is this device's store
- **WHEN** a store is created at a location of the person's choosing
- **THEN** later commands run with no location named resolve to that store rather than to the
  default location

#### Scenario: A second store at a different path
- **WHEN** creation is attempted at a path other than the one this device has already registered,
  and a store is still present there
- **THEN** creation is refused, the output names where the one store already is, and nothing is
  created at the new path

#### Scenario: The registered store no longer exists
- **WHEN** the store this device registered has been deleted and creation is attempted elsewhere
- **THEN** creation succeeds, because a pointer to something that is gone does not leave the machine
  permanently unable to hold a store

#### Scenario: Whether this device already has a store cannot be determined
- **WHEN** the record of this device's store exists and cannot be read
- **THEN** creation is refused rather than proceeding, because proceeding is how a second store gets
  made

### Requirement: The store refuses a location that synchronises off the machine
The product SHALL probe the target location's ancestry for evidence that it is copied off this
machine, SHALL refuse creation there, SHALL name which synchronising location was detected and at
which path, and SHALL leave nothing behind at the target.

#### Scenario: Creation targeting a synchronising location
- **WHEN** creation targets a path under Dropbox, iCloud Drive, OneDrive, a roaming profile or a
  network filesystem
- **THEN** creation is refused, no directory or file is left at the target path, the output names
  the provider and the path the evidence was found at, and the command exits non-zero

#### Scenario: The same evidence on any supported platform
- **WHEN** a synchronising root's on-disk markers are present on macOS or on Linux
- **THEN** the same probe detects them and refuses on either platform, because detection reads
  evidence from disk and never branches on which operating system is running

#### Scenario: An argument that is not a path
- **WHEN** an argument beginning with a dash that the command does not recognise is given
- **THEN** the command exits non-zero, creates nothing, echoes the argument back, and never treats
  it as a location for the store

#### Scenario: Asking what the command does
- **WHEN** the person asks the creation command for help
- **THEN** the usage is printed, including the override and what it will not do, the command exits
  zero, and no store is created

#### Scenario: Three failures that must not read alike
- **WHEN** creation fails because the location synchronises, because the path does not exist, or
  because the person cannot write there
- **THEN** each produces its own message and its own distinguishable error, so "this is Dropbox",
  "this path does not exist" and "I lack permission to write here" are never confused

#### Scenario: A store whose location becomes synchronising after creation
- **WHEN** an existing store's location is later placed under a synchronising root
- **THEN** the product reports the location as synchronising rather than accepting it silently, and
  distinguishes that from "confirmed off the sync path" and from "could not be determined"

### Requirement: An undetermined sync state halts creation, with an explicit override
The product SHALL render a sync probe that could not conclude as undetermined, distinguishable in
output and in exit code from both "confirmed local, store created" and "confirmed synchronising,
refused", SHALL never render it as "not synchronising" or as silence, and SHALL create the store
there only when the person says so with an explicit act they type.

#### Scenario: The probe cannot conclude
- **WHEN** the product cannot determine whether the target location synchronises off the machine
- **THEN** the outcome is rendered as undetermined with the reason attached, on an exit code shared
  with neither settled outcome, and nothing is created

#### Scenario: The halt says what the person can do about it
- **WHEN** the undetermined outcome is reported
- **THEN** the output names the explicit override that would create the store there, and says the
  location will still report as undetermined afterwards

#### Scenario: The person overrides an undetermined location
- **WHEN** the person types the explicit override for a location whose sync status could not be
  determined
- **THEN** exactly one store is created, its absolute path is printed, and the command exits zero

#### Scenario: The override is offered a location known to synchronise
- **WHEN** the person types the explicit override for a location determined to synchronise off the
  machine
- **THEN** creation is refused with the same non-zero outcome and the same wording as without the
  override, and nothing is created — the refusal for a known synchronising location is not
  overridable

#### Scenario: Reporting on a store created under the override
- **WHEN** the product reports on a store that was created with the override
- **THEN** its location state is still undetermined, never confirmed non-synchronising, and the
  report is distinguishable from the report for a store at a confirmed non-synchronising location

#### Scenario: A word the person guesses instead of the override
- **WHEN** the person types a flag the command does not have, such as a general-purpose force or yes
- **THEN** the command exits non-zero, creates nothing, and names the one flag that does exist and
  what it will not do

### Requirement: A record is either absent or complete, never partial
The product SHALL write every record to a temporary file in the destination's own directory, fsync
it, rename it over the destination, and fsync the directory, and SHALL ignore anything that is not a
completed record when reading.

#### Scenario: A process killed while writing a record
- **WHEN** the process is killed part-way through writing a record
- **THEN** reopening the store succeeds and lists every record that was complete before the
  interruption, with its content unchanged

#### Scenario: Reading after an interrupted write
- **WHEN** a record whose write was interrupted is asked for
- **THEN** it is reported as absent, and is never returned as a record with missing or empty fields

#### Scenario: Repeated interruption
- **WHEN** the store is interrupted mid-write more than once in succession
- **THEN** it still opens after every interruption and still lists every record completed before the
  first one

#### Scenario: A store that cannot be read at all
- **WHEN** a store's marker or a record is damaged so that it cannot be read
- **THEN** the product says the store cannot be read and exits non-zero, and never presents it as an
  empty store or as an absent one

### Requirement: The store is the sole home of unpublished data, and says where it is
The product SHALL write ticket and draft content nowhere on the machine except inside the store
path, and SHALL expose that path.

#### Scenario: Searching the machine for unpublished content
- **WHEN** a ticket and a draft note are written and the filesystem is searched for their content
- **THEN** every occurrence found is inside the store path, including the temporary files the store
  writes while saving

#### Scenario: Asking where the store is
- **WHEN** a person asks the product where the store lives
- **THEN** the absolute path is printed, whether or not a store is present there, alongside a
  three-valued answer for whether one is present

### Requirement: Creating a store implies nothing else
The product SHALL create a store without starting the daemon, without opening a network connection
when no hub is configured, and without requiring the control API.

#### Scenario: After creation completes
- **WHEN** the store-creation command finishes
- **THEN** no daemon process is running, no process is left behind by the command, and the store is
  usable by the next command

#### Scenario: No hub and no model configured
- **WHEN** store creation runs with no hub configured and no model configured
- **THEN** it completes fully, opens no outbound connection, and the created store holds both
  tickets and draft notes — it never reports success while leaving a store a later command reports
  as absent or unusable

