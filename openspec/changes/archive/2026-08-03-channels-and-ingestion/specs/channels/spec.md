## ADDED Requirements

### Requirement: Teams and email are built-in channels a person connects by their own act
The system SHALL let a person connect a Teams channel and an email channel through the CLI with
nothing additional installed, SHALL require the person to supply the credential themselves, and
SHALL obtain no credential and connect no channel on their behalf.

#### Scenario: both kinds connect and are told apart in the listing
- **WHEN** a person connects an email channel and a Teams channel and then lists their channels
- **THEN** both are listed, each says which kind it is, and the two kind renderings differ from each
  other

#### Scenario: connecting without a credential is refused
- **WHEN** a person runs the connect command without naming a credential file
- **THEN** the command exits non-zero, says on the error stream that no credential will be obtained
  on their behalf, and no channel is connected

### Requirement: An empty channel set, an unreadable one and a failure to list are three answers
The system SHALL state an empty channel set as an empty channel set rather than printing blank
output, and SHALL distinguish it — in wording and in exit code — from a channel set whose state
could not be determined and from a failure to list at all.

#### Scenario: nothing connected
- **WHEN** a person lists their channels and none is connected
- **THEN** the output says no channels are connected and the command exits zero

#### Scenario: a damaged channel record
- **WHEN** a person lists their channels and one stored channel record cannot be read
- **THEN** the command says so on the error stream, does not print the set as empty, and exits with
  the undetermined code rather than the code an empty set exits with

### Requirement: The last successful ingestion renders three distinguishable ways
The system SHALL carry a last-successful-ingestion fact for every connected channel and SHALL render
it as a real timestamp, as "never ingested", or as undetermined — no two of them alike, and none of
them as silence.

#### Scenario: a channel that has never ingested
- **WHEN** a channel has just been connected and nothing has ingested from it
- **THEN** its last-ingestion fact renders as never ingested, which differs from the rendering of a
  channel whose last-ingestion fact could not be read

#### Scenario: a recorded time that cannot be read
- **WHEN** a channel record holds a last-ingestion time that cannot be parsed
- **THEN** the fact is reported as undetermined and never as "never ingested"

### Requirement: Ingestion happens because the daemon is running
The system SHALL ingest connected channels continuously for as long as the daemon runs, without any
ingest or refresh command, and SHALL NOT ingest while the daemon is stopped.

#### Scenario: a message arrives while the daemon is running
- **WHEN** a message arrives on a connected channel while the daemon is running and nobody types
  anything
- **THEN** a ticket for it becomes visible in the store

#### Scenario: traffic arrives while the daemon is stopped
- **WHEN** traffic is delivered to a connected channel while no daemon is running
- **THEN** no ticket appears and the store is unchanged

#### Scenario: a channel command does not ingest
- **WHEN** a person runs any channel command while the daemon is stopped
- **THEN** no record in the store is written or rewritten by that command

### Requirement: No channel command starts the daemon, and none presents stale facts as current
The system SHALL leave the daemon exactly as it found it, and SHALL state on every channel command
whether ingestion is running — never presenting a last-ingestion time as current when it is not.

#### Scenario: running a channel command with the daemon stopped
- **WHEN** a person runs any channel subcommand while the daemon is stopped
- **THEN** the daemon is still stopped immediately afterwards, the output says ingestion is not
  running, and any last-ingestion timestamp shown is marked as not current

### Requirement: Ingestion produces tickets, not a mirror of the traffic
The system SHALL group the messages of one matter into at most one ticket, SHALL give that ticket a
written title and summary rather than a copied subject line or message body, and SHALL report the
number of messages seen separately from the number of tickets written.

#### Scenario: several messages about one matter
- **WHEN** six messages about one broken login are ingested from one channel
- **THEN** one ticket exists, the run reports six messages and one ticket as two different numbers,
  and neither the ticket's title nor its summary is any message's subject line or body

#### Scenario: the same conversation is ingested again
- **WHEN** ingestion passes over the same conversation repeatedly
- **THEN** the inbox still holds one ticket for it

### Requirement: Acknowledgements and small talk produce nothing at all
The system SHALL create no ticket from traffic that contains no request, at any priority, tag or
state, and SHALL have no priority, rank or severity for such traffic to be placed at.

#### Scenario: eleven people say nothing
- **WHEN** only acknowledgements and small talk such as `ok`, `thanks` and `Hii` arrive on a
  connected channel and are ingested
- **THEN** the ticket count is unchanged at zero, and no ticket exists at any low or minimum
  priority because no priority exists

### Requirement: A channel that could not be reached is not a channel with nothing in it
The system SHALL render a channel whose ingestion attempt failed distinguishably from a channel that
was reached and found no new traffic, and SHALL NOT record a successful ingestion for an attempt
that failed.

#### Scenario: one channel is unreachable and another is empty
- **WHEN** one connected channel's ingestion attempt fails and another's succeeds with nothing new
- **THEN** both produce zero tickets, the two channels render differently, the unreachable one says
  it could not be reached, and only the reached one records a successful ingestion

#### Scenario: a credential that has expired
- **WHEN** a connected channel's credential has expired and ingestion runs
- **THEN** nothing reaches out for that channel, it is reported as unreachable naming the expiry,
  and its state differs from both disconnected and connected-and-healthy

### Requirement: Nothing reaches out without cause, and nothing ingested leaves the machine
The system SHALL construct nothing capable of opening a connection when no channel is connected,
SHALL NOT reach a hub as part of ingesting, and SHALL keep ingested material and message bodies
inside the local store.

#### Scenario: no hub configured and no channel connected
- **WHEN** ingestion runs with no channel connected and no hub configured
- **THEN** no adapter is constructed at all

#### Scenario: the local half stands alone
- **WHEN** channels are connected and listed with no hub configured
- **THEN** everything works in full, nothing degrades or no-ops, and no output asks for a hub

#### Scenario: the credential and the traffic stay put
- **WHEN** a connected channel is rendered in any of its forms
- **THEN** the credential does not appear in the output, and no stored record holds a message body

### Requirement: A failure is distinguishable from success by exit code alone
The system SHALL exit with a non-zero code and write the detail to the error stream whenever a
channel command cannot do what was asked, and SHALL NOT share an exit code between a determined
negative answer and an answer that could not be determined.

#### Scenario: the failing commands
- **WHEN** a person connects with no credential, names a kind this build does not have, names a
  credential file that is not there, or disconnects a channel that is not connected
- **THEN** each exits non-zero with the detail on the error stream, and every successful command
  exits zero
