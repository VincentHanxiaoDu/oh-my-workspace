# Local store

## ADDED Requirements

### Requirement: The store is created by an explicit act, and only once per device
The product SHALL create a store only when a person runs the creation command, SHALL print the
absolute path it created, and SHALL refuse a second creation at an existing store without altering
a byte of it.

#### Scenario: A first creation on a machine with no store
- **WHEN** a person runs the store-creation command on a machine that has no store
- **THEN** exactly one store is created, the absolute path it was created at is printed, and the
  command exits zero

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

#### Scenario: Three failures that must not read alike
- **WHEN** creation fails because the location synchronises, because the path does not exist, or
  because the person cannot write there
- **THEN** each produces its own message and its own distinguishable error, so "this is Dropbox",
  "this path does not exist" and "I lack permission to write here" are never confused

#### Scenario: A store whose location becomes synchronising after creation
- **WHEN** an existing store's location is later placed under a synchronising root
- **THEN** the product reports the location as synchronising rather than accepting it silently, and
  distinguishes that from "confirmed off the sync path" and from "could not be determined"

### Requirement: An undetermined sync state is neither a pass nor a refusal
The product SHALL render a sync probe that could not conclude as undetermined, distinguishable in
output and in exit code from both "confirmed local, store created" and "confirmed synchronising,
refused", and SHALL never render it as "not synchronising" or as silence.

#### Scenario: The probe cannot conclude
- **WHEN** the product cannot determine whether the target location synchronises off the machine
- **THEN** the outcome is rendered as undetermined with the reason attached, on an exit code shared
  with neither settled outcome, and nothing is created

#### Scenario: The product has not ruled on whether to proceed
- **WHEN** the undetermined outcome is reported
- **THEN** the output states that the product has no ruling on whether creation should proceed here
  and names the open decision, so the halt cannot be mistaken for a settled refusal

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
