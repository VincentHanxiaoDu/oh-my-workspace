# The hub process

## ADDED Requirements

### Requirement: A hub process holds published notes and they survive it restarting
The hub SHALL store every published note in a durable record in a named directory, and SHALL make
each note readable, under the same identifier, by a process started later against that directory.

#### Scenario: A note published by one process is read by another
- **WHEN** one process publishes a note into a hub directory and exits, and a second process is
  started against the same directory
- **THEN** the second process holds the note under the identifier the first one gave it, with the
  same title, the same body and the same visibility

#### Scenario: A note's timeline keeps the times it was written with
- **WHEN** a note is published, amended, and then read after the hub has been restarted at a much
  later moment
- **THEN** every version on its timeline carries the time it was originally written, and none of
  them carries the moment the hub restarted

#### Scenario: The hub is pointed at a directory that is not a hub
- **WHEN** a hub process is started against a directory nothing has ever created a hub in
- **THEN** it refuses, names the directory, creates nothing inside it, and does not serve an empty
  corpus

### Requirement: A hub that cannot read its record determines nothing rather than reporting nothing
The hub SHALL refuse to start when its durable record cannot be read or cannot be replayed in full,
SHALL say so, and SHALL exit with a code that neither success nor ordinary failure uses.

#### Scenario: The record cannot be parsed
- **WHEN** a hub is started against a directory whose durable record is not readable
- **THEN** it does not start, it does not report a corpus of any size, and it exits on the code
  reserved for "could not be determined"

#### Scenario: The record contains something this build does not understand
- **WHEN** a hub is started against a record containing an operation, a visibility, or a note it
  cannot honour
- **THEN** it does not start, rather than starting with the entries it did understand

#### Scenario: The last write did not finish
- **WHEN** a hub is started against a record whose final line was cut short by a crash
- **THEN** it starts, holds everything written before that line, and states that the last entry did
  not finish

### Requirement: A hub stops when it cannot write, and answers nothing thereafter
The hub SHALL halt when a durable write fails, and SHALL refuse every subsequent request, including
reads, rather than continuing to serve from memory.

#### Scenario: A publication cannot be written down
- **WHEN** the hub cannot write its durable record
- **THEN** the publication is refused, the hub records that it halted, and it does not return a
  note identifier for something that is not on the disk

#### Scenario: Reading from a halted hub
- **WHEN** a person reads a note, searches, or asks for corpus statistics from a halted hub
- **THEN** each is refused with the halt's own answer, and none of them returns a result or a count

### Requirement: Visibility is settled by the hub before anything is ranked
The hub SHALL decide what a reader may see before it orders any result, and a note a reader may not
see SHALL leave that reader's results byte-identical to the note not existing — counts, facets,
recency, ordering and coverage included.

#### Scenario: A note narrowed away from a reader
- **WHEN** two hubs hold the same corpus and one of them additionally holds a note the reader may
  not see
- **THEN** the reader's rendered results from the two hubs are byte-identical

#### Scenario: The control
- **WHEN** a note the reader **can** see is added to one of those hubs
- **THEN** the reader's rendered results change, so the byte-identity above is evidence rather than
  an insensitive comparison

### Requirement: Search is scoped to a person, a group, or the company, and to nothing else
The hub SHALL answer searches scoped to a person, to a group, or company-wide, and SHALL refuse any
other scope rather than widening it or answering it with nothing.

#### Scenario: The three scopes
- **WHEN** a person searches scoped to themselves, to a group the hub knows, or company-wide
- **THEN** the hub answers each

#### Scenario: A subject the hub cannot resolve
- **WHEN** a person searches scoped to a person or group the hub has no record of
- **THEN** the hub refuses, and does not report zero results and does not fall back to a
  company-wide search

### Requirement: The hub says what it can read, in the product's own words
The hub SHALL state that it holds every published note including those narrowed to named people, to
a group, or to yourself, that restriction governs which colleagues see a note rather than whoever
operates the hub, and that the genuinely private note is the one never published. It SHALL say this
in the same words the client says it in.

#### Scenario: An operator asks what the hub can read
- **WHEN** a person runs the hub's own command for it, creates a hub, or describes one
- **THEN** each of those surfaces carries the statement in full

#### Scenario: The statement is not a second, softer copy
- **WHEN** the hub's statement is checked against the product's own rule for surfaces that offer or
  display a narrowing
- **THEN** it passes that rule, and it contains the product's own sentence rather than a paraphrase

### Requirement: An unidentified caller gets no determined answer
The hub SHALL refuse, with its own distinct refusal, any request that presents no token, and SHALL
NOT evaluate any note's readability for an unidentified caller.

#### Scenario: A request with no token
- **WHEN** a caller reads, searches, publishes, lists sessions, or asks for corpus statistics
  without presenting a token
- **THEN** each is refused with the refusal reserved for an unidentified caller, and none of them
  returns a corpus, a result, or a count of zero

### Requirement: A token carries a scope and the session carries the identity
The hub SHALL take the acting person's identity from the session a token resolves to, and SHALL use
the token's scopes only to limit what may be done within that identity.

#### Scenario: The same person with two tokens
- **WHEN** a person holds one token carrying only read and another carrying publish
- **THEN** both act as that person, the first can read their notes and cannot publish, and the
  refused publication leaves the corpus the size it was

#### Scenario: Nothing signs in silently
- **WHEN** a sign-in is started and nobody approves it
- **THEN** no token is ever issued, retrying does not eventually produce one, and the person has no
  sessions

### Requirement: A session is visible to its person and can be ended at the hub
The hub SHALL list every session signed in as a person, SHALL let that person end any of them, and
SHALL keep an ended session ended across a restart of the hub process.

#### Scenario: Ending a session
- **WHEN** a person lists their sessions and ends one of them
- **THEN** the ended session stops working at the hub immediately, and the other one keeps working

#### Scenario: The hub restarts after a revocation
- **WHEN** a hub is restarted after a person ended a session
- **THEN** that session is still ended, because ending a session is not the same act as forgetting a
  credential

### Requirement: Two hubs holding the same corpus answer the same
The hub SHALL give answers that depend only on the corpus it holds and on who is asking, so that a
reader cannot tell which of two hubs holding the same published corpus they are talking to.

#### Scenario: The same queries against two hubs
- **WHEN** two hubs are started from the same durable record and the same person searches both,
  reads the same note from both, and asks both for corpus statistics
- **THEN** every answer is identical, including note identifiers, counts, recency and ordering
