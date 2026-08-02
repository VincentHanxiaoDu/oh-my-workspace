# Note versions and the addressable timeline

## ADDED Requirements

### Requirement: Amending a note adds a version and never overwrites one
Publishing a changed body for an existing note SHALL append a new version, and every earlier
version SHALL remain retrievable with its body byte-identical to what was published.

#### Scenario: A note is amended and the earlier text is asked for
- **WHEN** a note is published and then a changed body is published for the same note, and the
  earlier version is retrieved
- **THEN** the earlier body is returned exactly as it was published, and it is not the later body

### Requirement: The timeline is enumerable and never renders as empty
For any note a person may read, the system SHALL list that note's versions in order, each carrying
an identifier that can be passed straight back in to read that version, and a note with one version
SHALL list exactly one entry.

#### Scenario: A note that has never been amended is listed
- **WHEN** the timeline of a note with a single version is listed
- **THEN** exactly one entry is shown, its identifier reads that version back, and the output is
  not an empty timeline

### Requirement: A version identifier is addressable and stable
An identifier obtained at any earlier point SHALL return the same content on every later read, and
SHALL NOT change when newer versions are published to the same note.

#### Scenario: An identifier kept before many later publications
- **WHEN** a version identifier is kept, two hundred further versions are published to that note,
  and the kept identifier is read again
- **THEN** it returns the same content it returned the first time, and it is reported as superseded

### Requirement: Search returns the latest version and identifies it
A search SHALL match only the current version of a note, and every result SHALL name the version it
refers to; for a note that has never been amended that identifier SHALL be the one its sole timeline
entry carries.

#### Scenario: A term that survives only in a superseded version
- **WHEN** a note is amended so that a term appears only in a version that is no longer current, and
  that term is searched for
- **THEN** the note is not presented as though its current version contained the term

### Requirement: Every read states whether it is current or superseded
Reading any version SHALL produce output that states its standing, distinguishable by content rather
than by the identifier the caller passed, and a reader who named no version SHALL still be told
which they are holding.

#### Scenario: An old version is read by someone who did not choose it
- **WHEN** a version that is no longer current is read
- **THEN** the output states that it is superseded, and with the identifier removed it is still
  different from the output of reading the current version

### Requirement: An unqualified request never yields superseded content
A request for a note that names no version SHALL return the current version, and where the current
version cannot be identified it SHALL report that the state could not be determined rather than
returning any older version.

#### Scenario: The current version cannot be identified
- **WHEN** a note is requested without naming a version and the timeline cannot be established
- **THEN** no body is returned, the output says the state could not be determined, and no earlier
  version is served in its place

### Requirement: Nothing expires
No version SHALL be removed by age, by count, by the publication of newer versions, or by any
retention setting, and the system SHALL expose no mechanism that deletes or truncates a note's
history; a deactivated person's notes and their full history SHALL remain readable to those
permitted to read them, marked archived rather than absent.

#### Scenario: An arbitrary amount of time and many publications pass
- **WHEN** hundreds of versions are published to one note and the clock is advanced far past any
  plausible retention window
- **THEN** every version, including the first, is still addressable and returns its original body

#### Scenario: The author of a note leaves the company
- **WHEN** a note's author is deactivated and a colleague reads the note's timeline
- **THEN** every version is still listed and readable, and the output marks the note archived

### Requirement: An undetermined version state is rendered as undetermined
Where it cannot be established which version is current, or whether the version in hand is
superseded, or what a version's content is, the output SHALL say the state could not be determined,
distinguishably from both "this is the current version" and "this is a superseded version", and
SHALL be neither silence nor an empty timeline.

#### Scenario: A version's content cannot be retrieved
- **WHEN** a version exists and is permitted but its content cannot be read
- **THEN** the output says the content could not be read, no body is shown, and the result is
  distinguishable from a successful read of a version whose body is empty

### Requirement: A version that does not exist differs from one that is empty
Requesting an unknown version identifier, or a version of an unknown note, SHALL fail
distinguishably from a successful read of a version whose body is empty, by exit status alone and
without parsing the body.

#### Scenario: An unknown version number is requested
- **WHEN** a version number the note does not have is requested
- **THEN** the request fails with an identifier-specific code, and no blank content is returned
  under a success result

### Requirement: Reading versions starts nothing and reaches nowhere implicitly
Reading a note's timeline or a specific version SHALL NOT start the daemon, and SHALL NOT open a
network connection unless a hub is configured.

#### Scenario: No daemon is running
- **WHEN** a timeline is requested on a machine with a hub configured and no daemon running
- **THEN** the command says the daemon is not running, does not start it, and does not reach the hub

### Requirement: The local half of versioning stands alone
With no hub configured, versioning of local drafts SHALL work fully — successive revisions
addressable and readable as they stood — and any part that genuinely requires the hub SHALL state
precisely what is missing rather than returning an empty timeline, a partial one, or a silent
success.

#### Scenario: A person with no hub revises a draft three times
- **WHEN** three revisions of a local draft are written and the draft's timeline is listed
- **THEN** all three are listed and each is readable as it stood, and asking for a published note's
  timeline instead says that a published timeline lives on the hub and there is none configured

### Requirement: An unreachable hub is not an empty history
A hub that cannot be reached while listing versions or reading one SHALL produce a report distinct
from a note that legitimately has the versions shown and from a refusal.

#### Scenario: The hub is configured and does not answer
- **WHEN** a timeline is requested and the hub cannot be reached
- **THEN** the report says the timeline could not be determined, shows no versions, and carries an
  exit status that no refusal and no successful listing uses

### Requirement: The CLI and the control API report the same version state
For the same note, the current-version identifier, the timeline and the current/superseded labelling
SHALL agree between the two surfaces, and neither SHALL show a version the other does not.

#### Scenario: The same note is read on both surfaces
- **WHEN** a note's timeline is requested through the CLI and through the control API
- **THEN** both name the same current version, list the same version identifiers, give each the same
  standing, and exit with the same status

### Requirement: Visibility precedes version access
A person who may not read a note SHALL NOT read any of its versions, and the existence of a version
SHALL NOT be surfaced to them through the timeline, through search, or through an identifier they
obtained some other way; their result SHALL be indistinguishable from that for a note that does not
exist.

#### Scenario: A note is narrowed away from a colleague who had read it
- **WHEN** a colleague who could read a note is excluded by a later narrowing and then asks for the
  timeline, an earlier version, the current version, or searches for its text
- **THEN** every one of those is refused, no body is returned, and the refusal reads the same as it
  would for a note that does not exist
