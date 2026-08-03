# reports Specification

## Purpose
A person works across several subjects in a day — commits land, model spend accrues, channel traffic
arrives — and does not want to go asking after each one. Separately from the knowledge loop, the
client reports on that person's **own** work (§3.7). They leave a standing instruction once, and it
reports back at the level of detail they asked for: not a firehose, and not a shrug.

The instruction is a **subscription**, built from **selectors** that each name a **subject** and a
**granularity**. The five granularities are ordered by detail — `full`, `event`, `digest`, `summary`,
`count` — and they mean the same thing for every subject. That consistency is not decoration. It is
precisely what makes `*:summary` a sensible thing to type without first knowing the full list of
subjects; if `summary` meant one thing for `git` and another for `token_usage`, the wildcard would be
a lie. A dotted subject stays dotted: `git.commit:event` asks about commits, not about everything
under `git`.

The failure this capability exists to prevent is silence. A person who typos a subject, or subscribes
to something this product does not have, must never receive an empty report — because an empty report
looks exactly like a quiet day. A selector that names no known subject is reported as unmatched, by
name, every time the report runs, and an exclusion of a subject that does not exist is a typo rather
than a no-op, so it is reported on the same terms. A wildcard, in contrast, is never unmatched merely
because nothing happened. A subscription with a bad granularity is refused at the moment it is
written, naming the offending token; it is never nudged to a neighbouring granularity, and nothing is
stored half-applied.

Where the client could not look — a source it failed to read, as opposed to one it read and found
empty — the subject is rendered as undetermined, never as a "no" (§4.3). An undetermined subject
inside a wildcard report neither disappears from the report nor takes the rest of the report down
with it. Where a selector names a subject only a hub can supply and no hub is configured, the report
says exactly that. Real activity, no activity, undetermined, no hub, and unknown subject are five
distinct facts, and they are five distinct outputs.

Nothing here is implicit (§4.2). Writing, reading, listing, or running a subscription does not start
the daemon; if the daemon is not running, these commands say so, in the one answer the rest of the
product gives. With no hub configured, nothing reaches out — and the local subjects still write, read
back, and report end to end, with no degradation and no complaint about the hub that is not there
(§4.4).
## Requirements
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

### Requirement: A subject nothing produces is undetermined, not a quiet period
A subject the client advertises SHALL report as undetermined when nothing in the build writes its
activity and no activity is stored for it. It SHALL NOT report as a period with no activity, and the
command SHALL NOT exit as though it had answered.

An emptiness is a determined answer only when something would have written to the subject had there
been anything to write. Where there is no such producer, the emptiness is a fact about the client and
not about the period, and reporting it as a quiet period is a confident negative derived from never
having looked.

Activity that is present SHALL take precedence over what the catalogue believes about a producer, so
that a report is about what is there.

#### Scenario: A report is run on a subject nothing writes
- **WHEN** a report is run for a subject whose activity nothing in this build writes, on a machine
  where no activity is stored for it
- **THEN** the report says the subject could not be determined, does not use the wording it uses for
  a period with no activity, and the command exits with the undetermined code

#### Scenario: The same subject once activity exists
- **WHEN** a report is run for that same subject on a machine where activity for it is stored
- **THEN** the report carries that activity and the command exits successfully, and the two reports
  differ both in what they print and in the code they exit with

#### Scenario: A subject something writes, with nothing to report
- **WHEN** a report is run for a subject something writes, and there is no activity for it
- **THEN** the report says there was no activity in the period, and that is a determined, successful
  answer

### Requirement: The catalogue of subjects does not promise silently
The list of subjects the client can report on SHALL state, for each subject, whether anything in the
build writes its activity. A subject that can only ever report nothing SHALL NOT be advertised
without that qualification.

The qualification SHALL be produced by the subject itself rather than by each surface that displays
it, so that two surfaces cannot word it differently.

#### Scenario: A person asks which subjects the client knows
- **WHEN** the subjects this build knows are listed
- **THEN** each subject nothing in this build writes is marked as such, on its own line

