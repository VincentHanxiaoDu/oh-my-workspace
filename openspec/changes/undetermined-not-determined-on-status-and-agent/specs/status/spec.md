# status

## ADDED Requirements

### Requirement: The status screen establishes what it summarises before it reports it as working
The status screen SHALL NOT report a subsystem as working on the strength of facts it did not
establish. A subsystem whose material could not be read SHALL be reported as undetermined, the
summary SHALL NOT lead with everything running, and the invocation SHALL exit with the code
reserved for an answer that could not be established — never the success code, which the screen's
own help states means every state on it was established.

The local store SHALL be summarised from what it holds and not from its presence alone. A record
that exists and cannot be read SHALL make the store's state undetermined and SHALL NOT be counted.

A store whose every record is readable SHALL still report as working and SHALL still exit with the
success code, including when it holds no records at all: an empty store is an established answer.

#### Scenario: One record on the store cannot be read
- **WHEN** the screen is produced for a store holding a record that is present and cannot be read
- **THEN** the local store's state is undetermined, the summary does not say everything configured
  is running, the rendered screen differs from the screen for the same store with every record
  readable, and the invocation exits with the undetermined code rather than the success code

#### Scenario: Every record is readable
- **WHEN** the screen is produced for a store whose records can all be read
- **THEN** the local store's state is working and the invocation exits with the success code

#### Scenario: The store holds no records
- **WHEN** the screen is produced for a store to which nothing has been written
- **THEN** the store reports an established answer of no records rather than an undetermined one

### Requirement: The status screen and the store report do not disagree about one store
The screen SHALL derive the store's record inventory through the same functions the store report
uses, so that the two surfaces cannot establish different things about one store at one moment. For
any store, the two SHALL agree on whether that store's records were established.

#### Scenario: Both surfaces are asked about the same unreadable store
- **WHEN** both the status screen and the store report are produced for one store holding an
  unreadable record
- **THEN** neither surface reports the store's records as established, and neither exits with the
  success code

#### Scenario: Both surfaces are asked about the same readable store
- **WHEN** both are produced for one store whose records can all be read
- **THEN** both report the records as established and both exit with the success code
