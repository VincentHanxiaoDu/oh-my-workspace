# Note references

## ADDED Requirements

### Requirement: A note carries inline references to people, groups and other notes
A note body SHALL be able to carry an inline reference to a person, to a group, and to another
note, and each SHALL be recoverable from the published note as a reference whose kind is part of
what is returned, distinguishable from ordinary body text that happens to contain the same
characters.

#### Scenario: All three kinds are retained on publication
- **WHEN** a note is published whose body references a person, a group and another note
- **THEN** all three are recoverable as references, each carrying its kind

#### Scenario: Prose containing the same characters is not a reference
- **WHEN** a body contains the reference characters as ordinary prose, or escaped
- **THEN** no reference is parsed from it, and it renders as the characters the author wrote

#### Scenario: Same name, different kind
- **WHEN** a person and a note have the same display name and both are referenced
- **THEN** the two references are distinguishable in output without the reader inspecting the target

### Requirement: A version's references are that version's
Retrieving an addressable earlier version of a note SHALL yield the references as they stood in
that version, not the current ones.

#### Scenario: A reference added and a reference removed
- **WHEN** version 2 adds one reference and removes another
- **THEN** version 1's reference set contains the removed one and not the added one

### Requirement: Publishing a reference the author cannot see is refused
Publishing a note whose body references a target its author cannot see SHALL be refused at
publication, and the refusal SHALL say why. The note SHALL NOT be published with the reference
stripped, narrowed or downgraded.

#### Scenario: An author references a note they may not read
- **WHEN** the author publishes a body referencing a note they are not permitted to read
- **THEN** the publication is refused with a code naming the reason, and nothing is stored

#### Scenario: The author's access could not be determined
- **WHEN** whether the author may read a referenced target could not be determined
- **THEN** the publication is refused with a different code, and neither answer is reported as the
  other

#### Scenario: An agent publishing on the author's behalf
- **WHEN** the same body is published through a grant carrying the publish scope
- **THEN** it is refused for the same reason, because an agent cannot point at what its person
  cannot read

### Requirement: A reference the reader may not see is not disclosed
Visibility SHALL be settled before any reference is rendered or counted, and a reference to a
target the reader may not read SHALL produce no title, identifier, slug, placeholder, marker,
per-reference error, count difference or gap in the reader's output.

#### Scenario: Two readers retrieve the same note
- **WHEN** a reader permitted to see a referenced note and a reader not permitted retrieve the same
  referencing note
- **THEN** the excluded reader's output is indistinguishable from the output for a note that
  references nothing restricted, including its count and the rendered prose

#### Scenario: Following a reference grants nothing
- **WHEN** a reader lists the references of a note that references a target they may not read
- **THEN** they still cannot read that target, directly or through any grant they hold

### Requirement: What else was written about this is answerable
Given a note, a person or a group, a reader SHALL be able to ask what references it and receive the
referencing notes they are permitted to read.

#### Scenario: The reverse question is answered
- **WHEN** two notes reference a subject and the reader may read one of them
- **THEN** the reader receives that one, and the other is absent rather than redacted, stubbed or
  represented by a gap

#### Scenario: An invisible target and a target that does not exist
- **WHEN** the reader asks about a target that exists but is invisible to them, and about a target
  that does not exist
- **THEN** the two produce the same observable outcome, including exit status

#### Scenario: Notes whose readability could not be determined
- **WHEN** the corpus contains notes whose readability for this reader could not be determined
- **THEN** they are reported as unexamined and the answer is reported as partial, rather than
  counted as matches or silently dropped

### Requirement: A reference that no longer resolves is shown, never dropped
A reference whose target the reader is permitted to see but which no longer resolves SHALL be
rendered as an unresolved reference: present, marked, and distinguishable from a resolved reference
and from plain body text. It SHALL NOT be interchanged with a reference the reader may not see.

#### Scenario: A dangling reference among others
- **WHEN** a note references a target that is gone and another that resolves
- **THEN** the dangling one is shown and marked, and the other still renders

#### Scenario: A note by a departed colleague
- **WHEN** a reference points at a note whose author has been deactivated and whose notes are
  archived
- **THEN** the reference resolves as a normal reference, because archiving is not unresolution

### Requirement: A reference that could not be resolved is undetermined
Where whether a reference resolves could not be determined, it SHALL be rendered as undetermined —
distinguishable from a resolved reference, from an unresolved one, and from silence — and SHALL NOT
be rendered as "does not exist" or omitted. Undetermined and determined-to-be-nothing SHALL NOT
share an exit code.

#### Scenario: The hub could not be reached
- **WHEN** a reference listing is asked for and the hub cannot be reached
- **THEN** the answer is undetermined, with its own exit status, and no reference is reported as
  absent

#### Scenario: A group that was dissolved
- **WHEN** a referenced note is narrowed to a group whose membership can no longer be resolved
- **THEN** the reference is undetermined rather than refused or missing, and its target is not named

### Requirement: Reference commands are implicit about nothing
No reference-reading or reference-listing command SHALL start the daemon, and with no hub
configured no reference operation SHALL open a network connection. Where the local half of the
capability is complete and the hub-dependent half is not, the answer SHALL be reported as partial
and SHALL be distinguishable from a complete one by exit status alone.

#### Scenario: No daemon running
- **WHEN** a reference command that needs the hub is run with no daemon running
- **THEN** it says so and does not start one

#### Scenario: A local draft with no hub configured
- **WHEN** a local draft's references are scanned with no hub configured
- **THEN** every reference is extracted, the hub is named as the piece missing for resolving them,
  and the exit status differs from that of a draft whose answer is complete
