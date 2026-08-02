# Reports and subscriptions

## ADDED Requirements

### Requirement: A subscription is built from selectors naming a subject and a granularity
The system SHALL accept a subscription written as a comma-separated list of selectors, each naming a
subject and optionally a granularity, and SHALL read that subscription back in the form it was
written, including a dotted subject path, which SHALL NOT be collapsed to its parent.

#### Scenario: The PRD's own examples are written and read back
- **WHEN** a subscription is written as `git:full, token_usage:digest, *:summary, git.commit:event, !channel`
- **THEN** it is accepted, and reading it back shows the same five selectors in the same form, with
  `git.commit` still spelled as `git.commit`

### Requirement: A wildcard covers every subject known to the client at the time the report runs
A selector naming the wildcard subject SHALL cover every root subject the client knows when the
report is produced, and SHALL NOT be limited to a set fixed when the subscription was written.

#### Scenario: A wildcard subscription is run
- **WHEN** a subscription containing `*:summary` is run
- **THEN** every root subject the client knows appears in the report

### Requirement: An exclusion applies regardless of where it was written in the list
Where a selector list both matches a subject and excludes it, the exclusion SHALL take effect, and
the result SHALL NOT depend on the order the selectors were written in.

#### Scenario: The exclusion is written before and after the wildcard
- **WHEN** the reports produced by `*, !channel` and by `!channel, *` are compared
- **THEN** neither report contains the excluded subject at any granularity, and both contain every
  other root subject

### Requirement: The five granularities are ordered by detail and mean the same for every subject
The system SHALL accept exactly the granularities `full`, `event`, `digest`, `summary` and `count`
on every subject it knows. For one subject over one fixed set of activity, the report at each
granularity SHALL be no more detailed than the report at the granularity before it in that order,
`count` SHALL yield a quantity with no per-item content, and `full` SHALL include an item's text
where `event` SHALL NOT.

#### Scenario: Two subjects are given identical activity and reported at the same granularity
- **WHEN** two different subjects are given structurally identical activity and each is reported at
  the same granularity
- **THEN** the rendered body is identical for both subjects, at every one of the five granularities

#### Scenario: One subject is reported at each granularity in turn
- **WHEN** one subject with a fixed set of activity is reported at `full`, then `event`, then
  `digest`, then `summary`, then `count`
- **THEN** each report carries no more item texts, no more identified items, no more named kinds and
  no more lines than the one before it, and no two of the five are identical

### Requirement: A selector that cannot be read is refused and nothing is stored
Where any selector in a list is malformed — an unknown granularity, no subject before the colon, a
granularity on an exclusion, a subject path that is not one — the system SHALL refuse the whole
list, SHALL name the offending selector, and SHALL leave the store exactly as it was.

#### Scenario: A list containing an unknown granularity is written over an existing subscription
- **WHEN** an existing subscription is rewritten as `git:full, channel:enormous`
- **THEN** the write is refused, the refusal names `enormous`, the stored subscription is unchanged,
  and no part of the new list has been stored

### Requirement: A selector matching no known subject is reported, never rendered as silence
A well-formed selector naming a subject the client does not know SHALL be accepted, and every report
it appears in SHALL state that it matched no known subject and SHALL identify which selector. This
SHALL apply equally to an exclusion of a subject that does not exist. A refusal and a selector that
matched nothing SHALL NOT share an exit code.

#### Scenario: A subscription names an unknown subject alongside a known one
- **WHEN** a subscription containing both a known subject with activity and an unknown subject is run
- **THEN** the known subject reports its content, the unknown selector is named as unmatched, and
  neither suppresses the other

#### Scenario: An exclusion names a subject that does not exist
- **WHEN** a subscription containing `!nosuchsubject` is run
- **THEN** the exclusion is named as unmatched, and no known subject has been excluded

### Requirement: Activity, no activity, an unknown subject, an unreadable subject and a missing hub are five distinct outputs
The system SHALL render each of these five as distinguishable output, by the output alone. A subject
that could not be read SHALL be rendered as undetermined and SHALL NOT be rendered as empty or as a
count of zero. A subject only a hub can supply, with no hub configured, SHALL state that no hub is
configured and SHALL NOT be omitted from the report.

#### Scenario: A stored record is damaged and its subject is reported at count
- **WHEN** a subject whose stored activity has been damaged is reported at `count`
- **THEN** the subject is present, is rendered as undetermined, and the output does not read
  `count: 0`, while another subject in the same report still reports its own count

#### Scenario: An undetermined subject appears inside a wildcard report
- **WHEN** a report over `*:summary` includes one subject that could not be read
- **THEN** that subject is present and marked as undetermined, and every other subject still reports

### Requirement: Subscriptions need no daemon and no hub, and open no connection
No operation on a subscription or a report SHALL start the daemon, and each SHALL state whether the
daemon is running. With no hub configured, no such operation SHALL open a network connection, and a
subscription over purely local subjects SHALL be written, read back and produce a report with no
degradation and no warning about a missing hub.

#### Scenario: A local subscription is written, read and run with no daemon and no hub
- **WHEN** a subscription over local subjects is written, read back and run with no daemon running
  and no hub configured
- **THEN** each command reports that the daemon is not running, the daemon is still not running
  afterwards, the report is produced in full, and nothing in the output mentions a hub
