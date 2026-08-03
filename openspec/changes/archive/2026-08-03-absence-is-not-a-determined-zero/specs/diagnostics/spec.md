# diagnostics

## ADDED Requirements

### Requirement: The bundle counts the records the product actually writes
A support bundle's inventory of a kind of record SHALL be produced by enumerating the subsystem that
writes those records. It SHALL NOT be produced by counting a storage location that nothing writes to,
because such a location is readable and yields a count of zero.

The count of drafts in a bundle SHALL equal the count of drafts the outbox reports for the same store
at the same moment.

#### Scenario: A bundle is produced on a machine with drafts
- **WHEN** a person has written two drafts and produces a support bundle
- **THEN** the bundle's draft inventory reports the same number of drafts as the outbox listing does

#### Scenario: Bodies are asked for on a machine with drafts
- **WHEN** a person produces a bundle and affirmatively asks for bodies
- **THEN** the collected draft bodies contain the drafts that exist, and the manifest reports the
  same number of them as the outbox listing does

### Requirement: The bundle's summary does not overstate what it holds
The summary a person reads before handing a bundle over SHALL be derived from the manifest rather
than from a fixed sentence, so that it cannot name as included a category the bundle did not collect.

#### Scenario: Bodies are asked for and one category could not be established
- **WHEN** a person asks for bodies and one of the body categories is undetermined
- **THEN** the summary names only the categories the manifest marks collected

### Requirement: Records that could not be enumerated are said, never counted as none
Where a bundle cannot enumerate a kind of record, the manifest SHALL report that category as
undetermined, with a machine-readable reason and a sentence stating that this is not a report that
there are none. It SHALL NOT report it as collected with a count of zero.

An enumeration that fails for one record SHALL make the whole category undetermined rather than
reporting the records that did enumerate as though they were all of them.

Where nothing in the build writes a kind of record at all, the reason given SHALL distinguish that
from records that exist and would not be read.

#### Scenario: Drafts exist and cannot be read
- **WHEN** a bundle is produced on a machine whose outbox cannot be read
- **THEN** the draft inventory is undetermined, carries a reason and a sentence a person can read,
  and differs in both state and reason from the entry produced for a store that genuinely holds no
  drafts

#### Scenario: A kind of record nothing in this build writes
- **WHEN** a bundle is produced, with or without an explicit request for bodies, for a kind of record
  that nothing in the build writes
- **THEN** that category is undetermined and names this build's lack of a producer as the reason,
  rather than being collected with a count of zero

#### Scenario: An outbox that holds nothing
- **WHEN** a bundle is produced on a machine with a store and no drafts in it
- **THEN** the draft inventory is collected with a count of zero, which is a determined answer
