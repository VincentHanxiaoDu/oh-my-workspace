# agent

## ADDED Requirements

### Requirement: A draft whose state could not be read is not served as a resting draft
The agent API SHALL report a draft whose recorded state could not be read as undetermined, in the
same wording every other surface uses for a state that could not be established, and the invocation
SHALL exit with the undetermined code. It SHALL NOT report such a draft as drafted, which is a
determined claim that the draft is resting in the outbox and that nothing is outstanding.

The answer the agent API gives about a draft SHALL agree with the answer the outbox listing gives
about the same draft at the same moment, because an agent reads exactly what its person can — no
more and no less.

#### Scenario: The draft's state record cannot be read
- **WHEN** the agent API is asked for the drafts in an outbox holding a draft whose state record is
  present and cannot be read
- **THEN** that draft's state is reported as undetermined, the answer differs from the answer for
  the same outbox with the state record readable, and the invocation exits with the undetermined
  code rather than the success code

#### Scenario: The outbox listing is asked about the same draft
- **WHEN** both the agent API and the outbox listing are asked about one draft whose state record
  cannot be read
- **THEN** the two exit with the same code and neither reports the draft as resting

#### Scenario: Every draft is readable
- **WHEN** the agent API is asked for the drafts in an outbox whose records can all be read
- **THEN** each draft's recorded state is reported and the invocation exits with the success code

### Requirement: A revision that could not be read is never counted as zero
The agent API SHALL NOT serve a revision count that was not established. A count SHALL be present
only when it was established; an outbox read that failed SHALL leave the count absent rather than
zero, in every rendering the surface offers, so that a reader cannot take a determined zero from a
read nobody completed.

A count of zero SHALL remain reachable when it was genuinely established, which is a draft the
outbox listed and that holds no revisions.

#### Scenario: The draft's only revision cannot be read
- **WHEN** the agent API is asked for a draft whose single revision is present and cannot be read
- **THEN** no revision count is served for that draft, the human rendering does not print a count
  of zero revisions, and the invocation exits with the undetermined code

#### Scenario: The draft genuinely has no revisions
- **WHEN** the agent API is asked for a listed draft that holds no revisions
- **THEN** the count is served as an established zero

### Requirement: No count is printed for material that was not read
A count line SHALL be printed as a number only when the outcome of the operation established one. On
an outcome of refused or undetermined the surface SHALL print that the count could not be
determined, or SHALL NOT print the line at all. It SHALL NOT print a number in the position that
carries a real count on success.

No surface SHALL render a count derived from a read that failed. A count field on this surface SHALL
be shaped so that "not established" and "established as none" are different values.

#### Scenario: The local material could not be read
- **WHEN** the agent API answers an operation whose outcome is undetermined
- **THEN** the rendering carries no number on the count line, and it differs from the rendering of
  the same operation when it succeeded

#### Scenario: A count field is added to the surface
- **WHEN** a field carrying how many of something there are is served by the agent API
- **THEN** that field distinguishes a count that was not established from a count established as
  none, rather than reporting both as zero
