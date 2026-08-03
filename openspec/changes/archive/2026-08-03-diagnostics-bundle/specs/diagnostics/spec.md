# Diagnostics

## ADDED Requirements

### Requirement: A bundle carries a manifest that states what the bundle contains
The product SHALL produce a diagnostics bundle as one artifact containing a manifest, and the
manifest SHALL be readable by a person who has only the bundle and not the machine it came from,
without opening any collected data.

#### Scenario: A person reads the manifest without the machine
- **WHEN** a person holds only a bundle
- **THEN** the manifest can be read from the bundle alone, and it describes every category of data
  the bundle could hold

#### Scenario: The manifest names a file the bundle does not contain
- **WHEN** the manifest names a payload file that is not present in the bundle
- **THEN** the discrepancy is a defect and the product's tests fail naming the manifest entry and
  the missing file

#### Scenario: The bundle contains a file the manifest does not name
- **WHEN** a bundle carries a payload file no manifest category claims
- **THEN** the discrepancy is a defect and the product's tests fail naming the unclaimed file

### Requirement: Every category is named, and no category is silently missing
The manifest SHALL enumerate every category of data the bundle could hold, on every machine and in
every disclosure level. A category that was collected SHALL be named. A category with nothing
collected SHALL be rendered as such rather than omitted.

#### Scenario: A category no branch of the run spoke for
- **WHEN** a run reaches its end without deciding what happened to one category
- **THEN** no bundle is produced, and the failure names the category that was not spoken for

#### Scenario: A category that is present but empty
- **WHEN** a category was read successfully and holds nothing
- **THEN** it is rendered as collected with a count of zero, which is distinguishable from every
  other state the manifest can carry

### Requirement: Every category carries one of three states, and undetermined is a real answer
Each manifest category SHALL carry exactly one of `collected`, `withheld` or `undetermined`.
`undetermined` SHALL be distinguishable from a collected value, from a withheld one, and from
absence, and SHALL never be rendered as a negative finding.

#### Scenario: A subsystem that could not be read
- **WHEN** a subsystem the bundle gathers from cannot be read
- **THEN** its category is `undetermined` and carries a reason a person can read
- **AND** it is distinguishable from a category that was read and found empty

#### Scenario: A subsystem that could not be read is omitted instead
- **WHEN** a gather path drops a category it could not read
- **THEN** no bundle is produced rather than one with an unexplained gap

### Requirement: The manifest states what was omitted and why
For every category that is not collected, the manifest SHALL carry a machine-readable reason that
distinguishes withheld-by-default from could-not-be-determined from not-applicable-on-this-machine,
so a person reading only the manifest can state what they handed over and what they did not.

#### Scenario: A machine with no store
- **WHEN** a bundle is produced on a machine that has no store
- **THEN** the bundle is still produced, and each store-derived category is unavailable-because-no-store
- **AND** that rendering is distinguishable from a category that is present and empty

#### Scenario: A machine with no hub configured
- **WHEN** a bundle is produced with no hub configured
- **THEN** the bundle is produced fully, every local category is gathered, and each inherently
  hub-derived category is named as unavailable-because-no-hub rather than dropped or reported as a
  negative finding

#### Scenario: A capability that is not in this build
- **WHEN** a category depends on a capability this build does not have
- **THEN** the category is `undetermined` with a reason naming the absent capability, and is not
  reported as though the answer were no

### Requirement: A default bundle contains no ticket body, no draft body and no ingested message body
A bundle produced without an explicit request for bodies SHALL contain no ticket body, no draft note
body, and no raw message body ingested from any channel, in any file.

#### Scenario: An exhaustive search of a default bundle
- **WHEN** a store holds a ticket, a draft note and an ingested message each carrying a known string,
  and a default bundle is produced from it
- **THEN** an exhaustive search of every file in the bundle for those strings returns zero matches

#### Scenario: The search itself is verified before it is trusted
- **WHEN** the exhaustive search is run against the store the bundle was produced from
- **THEN** it finds every one of the known strings, because a search that can find nothing proves
  nothing about a bundle it reports as clean

### Requirement: Bodies are included only on an affirmative request, and the manifest says which
Including bodies SHALL require an explicit act by the person. No default and no other option SHALL
imply it. A bundle produced with the request and one produced without it SHALL be distinguishable
from the manifest alone, and the manifest SHALL never understate what the bundle contains.

#### Scenario: Bodies are asked for
- **WHEN** a person explicitly asks for bodies
- **THEN** the ticket, draft and ingested message bodies are in the bundle, and the same manifest
  field that reads withheld by default reads as included

#### Scenario: Another option is offered instead
- **WHEN** an argument other than the body request is given to the command
- **THEN** it is refused by name, and no bodies are included

### Requirement: A credential is never in a bundle at any disclosure level
The bundle SHALL never contain the person's model key, whether or not bodies were asked for, and
SHALL state in the manifest that the key was withheld rather than omitting the category.

#### Scenario: An opt-in bundle from a store holding a key
- **WHEN** a store holds a model key with a known string and a bundle is produced with bodies
  explicitly requested
- **THEN** an exhaustive search of every file in the bundle for that string returns zero matches
- **AND** the manifest carries the key's category as withheld, with a reason saying it is never
  collected

#### Scenario: Configuration is reported
- **WHEN** the bundle reports which of the product's configuration variables are set
- **THEN** it carries their names and no value of any of them

### Requirement: Producing a bundle starts no daemon and opens no network connection
Producing a bundle SHALL start no daemon, SHALL open no network connection when no hub is
configured, and SHALL transmit the bundle nowhere. Handing the bundle over is the person's act.

#### Scenario: No daemon is running before the command
- **WHEN** a bundle is produced against a store with no daemon running
- **THEN** the command completes and emits a bundle, no daemon is running afterwards, and the
  manifest records the daemon as not running rather than omitting daemon state

#### Scenario: The daemon's liveness could not be established
- **WHEN** whether a daemon holds the store cannot be determined
- **THEN** the bundle records that as undetermined with a reason, rendered distinguishably from the
  determined negative

#### Scenario: The bundle is produced
- **WHEN** a bundle has been produced
- **THEN** the command's last act is to name a path on disk, and it has sent nothing anywhere

### Requirement: The bundle reports the health facts in their full range of answers
The bundle SHALL record full-disk encryption as one of three values without collapsing any two of
them, SHALL record whether owner-only permissions on the control API could be confirmed as a
separate entry, and SHALL record the store's location state including undetermined without resolving
it to a yes or a no.

#### Scenario: Each of the three encryption answers
- **WHEN** a bundle is produced once for each of the three encryption answers
- **THEN** the three bundles render the answer differently from one another

#### Scenario: The control API did not open on this platform
- **WHEN** the product refuses to open the control API because owner-only permissions could not be
  confirmed
- **THEN** that refusal is a recorded fact in the bundle, in its own manifest entry, rather than an
  omission

#### Scenario: The store's location could not be determined
- **WHEN** whether the store's location synchronises off the machine could not be determined
- **THEN** the bundle reports that state as undetermined and does not resolve it to either answer

### Requirement: The bundle records where it came from
The bundle SHALL record the platform it was produced on, and SHALL record the device's label where
that is available, naming it as undetermined with a reason where it is not.

#### Scenario: A platform is recorded
- **WHEN** a bundle is produced
- **THEN** the operating system it was produced on is in the bundle and named in the manifest

#### Scenario: The device label cannot be read
- **WHEN** the device label cannot be read on this machine
- **THEN** the manifest names the label as undetermined with a reason, and does not report the
  device as having no label

### Requirement: A bundle that exists is a complete bundle
The command SHALL distinguish success from failure by exit status alone. On a failure to produce a
bundle it SHALL exit non-zero and SHALL leave no partial artifact that could be mistaken for a
complete bundle.

#### Scenario: The run fails after data has been gathered
- **WHEN** a run fails after gathering has begun
- **THEN** the command exits non-zero, nothing exists at the destination, and no staging remnant is
  left beside it

#### Scenario: The destination is relative
- **WHEN** the destination given to the command is not an absolute path
- **THEN** the command refuses with a usage error naming why, and creates nothing in the working
  directory

#### Scenario: The destination already exists
- **WHEN** the destination already holds something
- **THEN** the run is refused, what was there is left byte-identical, and no bundle is produced

#### Scenario: A category inside the bundle is undetermined
- **WHEN** a complete bundle is produced in which some category could not be determined
- **THEN** the command exits zero, because a complete bundle exists and the undetermined answers are
  inside it
