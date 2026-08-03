# Drafts survive an interrupted write

## ADDED Requirements

### Requirement: A draft directory is never visible before its state file
Writing a draft SHALL make it visible only once its first revision and its state record are both on
disk. A process interrupted at any point during a draft's creation SHALL leave either no draft by
that name or a whole one, and SHALL NOT leave a draft directory that a reader can find.

#### Scenario: A draft write is killed part-way through
- **WHEN** a person's machine kills `omw outbox draft` at any point during the write, repeatedly
- **THEN** every draft directory that is visible in the outbox afterwards contains its state record,
  and no draft is visible holding an empty or partial body

#### Scenario: A concurrent writer does not destroy an existing draft
- **WHEN** two writers create the same new draft at the same time
- **THEN** one draft exists, both writers' text is preserved as separate revisions, and no revision
  is overwritten

### Requirement: A damaged draft is undetermined and is never reported as resting
Reporting where a draft stands SHALL answer `could not be determined` with a non-zero exit code for a
draft whose state record is missing, empty or unreadable, and SHALL NOT answer `drafted`. That exit
code SHALL differ from the one used for a draft that does not exist.

#### Scenario: A draft left behind by an unfinished write
- **WHEN** a person asks where a draft stands whose directory holds no state record
- **THEN** the command says the state could not be determined, does not say the draft is resting in
  the outbox awaiting them, and exits with a code that success does not use

#### Scenario: A draft with a truncated state record
- **WHEN** a person asks where a draft stands whose state record is empty or cannot be parsed
- **THEN** the command says the state could not be determined and exits non-zero

#### Scenario: An absent draft is a determined answer
- **WHEN** a person asks where a draft stands that was never written
- **THEN** the command reports that there is no such draft, and exits with a code that differs both
  from success and from the code used for a draft that could not be determined

#### Scenario: An intact draft is unaffected
- **WHEN** a person asks where an intact draft stands
- **THEN** the command reports it as `drafted` and exits zero

### Requirement: A refused draft write names a code
A draft write that cannot be completed because another writer holds the revision number it claimed
SHALL be refused with this product's own refusal code and wording, SHALL NOT expose the text of the
underlying system call, and SHALL NOT report an empty code.

#### Scenario: Concurrent writers against one draft
- **WHEN** several writers add revisions to the same draft at the same time
- **THEN** no refusal contains a raw system-call error, every refusal names a non-empty code, and
  every revision that reported success is on disk with its text intact

### Requirement: The stale-lock notice appears only after an unclean exit
Starting the daemon SHALL report that it took over a lock left behind only when the previous holder
did not release it. A daemon that is stopped on purpose SHALL leave nothing that a later start
reports as a lock left behind.

#### Scenario: A clean stop, then a start
- **WHEN** a person stops the daemon and starts it again
- **THEN** the start says nothing about a lock left behind by another process

#### Scenario: A start after the daemon was killed
- **WHEN** the daemon is killed without releasing its lock and a person starts it again
- **THEN** the start reports that a lock left behind was found and taken over, and names the process
  that left it
