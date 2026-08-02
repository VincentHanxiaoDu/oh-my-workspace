# Notes

## ADDED Requirements

### Requirement: A deactivated person's notes are archived, not deleted
The hub SHALL keep every note a deactivated person published, and every version of every such note,
retrievable on exactly the terms they were retrievable before the deactivation. Deactivation SHALL
remove nothing.

#### Scenario: A direct reference resolves across a departure
- **WHEN** a note reference that resolved before a person was deactivated is resolved after it
- **THEN** it resolves to the same note, with the same body content and the same author

#### Scenario: A version addressable before is addressable after
- **WHEN** a version of a note published by a person who has since been deactivated is requested by
  the identifier that addressed it before the deactivation
- **THEN** the same version is returned, with the same body and the same time of writing

#### Scenario: References survive in both directions
- **WHEN** a note by a still-active person referenced a note whose author has since been deactivated
- **THEN** that reference still resolves, and the archived note still lists what referenced it

#### Scenario: A retention window is applied
- **WHEN** any amount of time passes after a person is deactivated
- **THEN** their notes and every version of those notes are still present and still attributed,
  because nothing expires

### Requirement: Deactivation is not a visibility change
The hub SHALL settle who may read a note without consulting whether the note's author is still with
the company. Deactivating a person SHALL neither widen nor narrow the reach of anything they
published.

#### Scenario: A searcher who could read it before can read it after
- **WHEN** the identical query is run by the identical searcher before and after the author is
  deactivated, under a company-wide, group, named-people or self narrowing
- **THEN** the same notes are returned in both result sets

#### Scenario: A searcher outside the narrowing is still outside it
- **WHEN** a searcher who could not read a narrowed note before the deactivation asks for it after
- **THEN** they are refused exactly as before, and the note appears in none of their results

#### Scenario: A company-wide note stays company-wide
- **WHEN** a company-wide note's author is deactivated and any colleague searches for it
- **THEN** it is still returned to them

#### Scenario: Ranking meets an archived note the searcher cannot read
- **WHEN** a searcher's results are produced from a corpus containing archived notes they may not
  read
- **THEN** neither their results nor their result count differ from the same query against a corpus
  without those notes, and no title, redacted row or count reveals that the notes exist

#### Scenario: Corpus statistics are served to an agent
- **WHEN** an agent asks how much of the corpus its person may read
- **THEN** the count includes the archived notes that person is permitted to read and excludes the
  archived notes they are not, so a search that follows never finds the corpus smaller than promised

### Requirement: An archived note is shown as written by someone deactivated
The hub SHALL render an author for every archived note, identified the same way that person was
identified while active, together with an indication that they are deactivated. It SHALL never
render such a note as authorless, as missing, or as an error.

#### Scenario: An archived note is read
- **WHEN** a colleague reads a note whose author has been deactivated
- **THEN** the output names that author, is the same author as before the deactivation, and carries
  a deactivated indication

#### Scenario: A rendering would be authorless
- **WHEN** an archived note is rendered
- **THEN** the author is not an empty string, not absent, and not a placeholder such as `unknown` or
  `deleted user` that a reader could mistake for "this note has no author"

#### Scenario: An archived note is requested by reference, by version or from a search result
- **WHEN** any of those three requests is made for a note whose author has been deactivated
- **THEN** the note is returned with its content, and not as a not-found, an error or an empty body

#### Scenario: A refusal and a departure are told apart
- **WHEN** one note is refused because the reader may not see it and another is served with a
  deactivated author
- **THEN** the two are reported differently, with different machine-readable codes

#### Scenario: Attribution is read across a timeline
- **WHEN** every version of a deactivated person's note is rendered
- **THEN** each version shows the same author and the same deactivated indication as every other

### Requirement: An author state that could not be determined is a third answer
The hub SHALL render an author state it could not establish as neither active nor deactivated nor
absent. The three states SHALL be distinguishable from one another in output and by exit code.

#### Scenario: The three states are rendered side by side
- **WHEN** a note by an active author, a note by a deactivated author, and a note whose author state
  could not be determined are each rendered
- **THEN** the three outputs differ pairwise, none of them is blank, and each names its author

#### Scenario: The author-state lookup fails
- **WHEN** the hub cannot establish whether a note's author is active or deactivated
- **THEN** the output states that it could not be determined, still names the author, and exits with
  the undetermined exit code rather than the code an answered question uses

#### Scenario: An unestablished author state meets a read
- **WHEN** a note's author state could not be determined and a permitted colleague reads the note
- **THEN** the note is returned exactly as it would have been, because author state is not an input
  to who may read a note

### Requirement: Deactivation ends sessions without removing publications
The hub SHALL refuse every token issued to a deactivated person, for every scope, and SHALL create
no new authority for them. It SHALL do so without making any note less findable or less attributed.

#### Scenario: A token is replayed after deactivation
- **WHEN** a request is made with a token issued to a person before they were deactivated, carrying
  any scope in the vocabulary
- **THEN** it is refused, with a code distinct from a visibility refusal

#### Scenario: The same deactivation is examined from both sides in one run
- **WHEN** a person is deactivated, their token is replayed, and a colleague then looks for their
  notes
- **THEN** the token is refused and the notes are still found and still attributed to them

#### Scenario: A grant is requested for a deactivated person
- **WHEN** a grant that would let something act as a deactivated person is requested
- **THEN** it is refused at the point of request, no grant is issued, and the set of grants
  attributable to that person is unchanged

#### Scenario: Something tries to write as a deactivated person
- **WHEN** a new note is published as a deactivated person, or a version is added to one of their
  existing notes
- **THEN** it is refused and nothing is stored, because the archive is readable and not writable

#### Scenario: A signal arrives from outside the hub
- **WHEN** any client-side event, channel message or directory record would deactivate a person
- **THEN** nothing happens, because deactivation is an act performed against the hub and the record
  consulted everywhere else exposes no way to write to it

### Requirement: Archived-note lookup states what is missing when there is no hub
The capability SHALL open no network connection when no hub is configured, SHALL say precisely that
archived-note lookup is a hub capability and that no hub is configured, and SHALL exit
distinguishably from success, from a configured-but-unreachable hub, and from a genuine zero result.

#### Scenario: No hub is configured
- **WHEN** any archived-note subcommand runs on a machine with no hub configured
- **THEN** it names what is missing, states that this is not a report that no such notes exist,
  reaches for no hub at all, and exits with the failure code

#### Scenario: A hub is configured but does not answer
- **WHEN** a hub is configured and cannot be reached
- **THEN** the answer is undetermined, exits with the undetermined code, and differs in wording and
  exit code from the no-hub answer and from success

#### Scenario: The hub answers and holds nothing
- **WHEN** the hub is asked for a person's notes and holds none the reader may read
- **THEN** the answer says the hub was asked and holds none, succeeds, and shares neither its
  wording nor its exit code with the no-hub answer

#### Scenario: The daemon is not running
- **WHEN** an archived-note subcommand runs with a hub configured and the daemon not running
- **THEN** it says the daemon is not running, does not start it, and its output differs from the
  daemon-running case
