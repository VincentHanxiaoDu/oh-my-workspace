# Corpus statistics

## ADDED Requirements

### Requirement: Corpus statistics report what exists, how much, and how recent
The product SHALL report, for a requested scope, which subjects have material, how many notes there
are, and how recent the most recent one is, each independently readable and each independently
capable of being undetermined without the others being dragged down with it.

#### Scenario: The three statistics are reported together
- **WHEN** an agent requests corpus statistics for a scope
- **THEN** the response carries a subjects figure, a count and a recency figure, each labelled, and
  each either a determined value or an explicit undetermined marker

#### Scenario: Some determined and some not, in one response
- **WHEN** one statistic can be computed and another cannot for the same request
- **THEN** both are returned in the same response, each labelled with what it is, and the request
  neither fails whole nor succeeds whole

### Requirement: Every statistic is computed over exactly what the requester may read
Counts, subjects and recency SHALL be derived from the visibility-settled corpus for the requesting
identity, and SHALL NOT be derived from any wider set of notes.

#### Scenario: The count is the readable subset
- **WHEN** the corpus contains notes the requester may not read
- **THEN** the returned count equals the count of the readable subset — not the total, and not the
  total with a redaction note

#### Scenario: Two identities differ in what they may see
- **WHEN** the same statistics are requested for the same scope by two identities whose readable
  sets differ
- **THEN** the numbers returned to each reflect only their own readable set

#### Scenario: Recency is never drawn from an unreadable note
- **WHEN** the most recently written note in the whole corpus is one the requester may not read
- **THEN** the reported recency is that of the most recent note the requester MAY read

### Requirement: Statistics never surface the existence of unreadable material
For two identities differing only in what they may see, the statistics returned to the narrower
identity SHALL be consistent with a corpus in which the unreadable notes do not exist.

#### Scenario: An unreadable note changes nothing
- **WHEN** the same request is made over a corpus of readable notes, and again over that corpus plus
  a note the requester may not read
- **THEN** the two responses are identical in every statistic, with no total, no count of hidden
  material, and no error that fires only when unreadable material is present

#### Scenario: A scope with nothing visible in it
- **WHEN** statistics are requested for a scope in which every note is unreadable to the requester
- **THEN** the response is indistinguishable from statistics for a scope that is genuinely empty of
  readable material

#### Scenario: A statistic is not a search result
- **WHEN** a statistic reports that material exists in a scope
- **THEN** it carries no note title, identifier or excerpt that the requester could not obtain by
  searching that same scope as themselves

### Requirement: A statistic that could not be computed is undetermined, never zero
A statistic that could not be computed SHALL render distinguishably from a statistic computed as
zero, by inspecting the output alone, with no reference to logs, exit code, or a second command, and
SHALL be present in the output rather than omitted.

#### Scenario: Zero and undetermined do not share a rendering
- **WHEN** one statistic is determined to be zero and another could not be computed
- **THEN** the two render differently in every rendering the product offers, and the undetermined
  one carries an explicit undetermined marker and a stable reason code

#### Scenario: Undetermined is not silence
- **WHEN** a statistic could not be computed
- **THEN** the field is present in the response carrying its undetermined marker; it is not omitted,
  not absent-in-a-way-that-parses-as-nothing, and does not suppress the rest of the response

#### Scenario: An unevaluable note is not counted as absent
- **WHEN** the corpus contains a note whose readability could not be determined at all
- **THEN** it is neither counted among the notes the requester may read nor silently dropped: it is
  reported separately and the affected statistics are undetermined

### Requirement: Statistics are requestable at each of the three search scopes and no other
Statistics SHALL be requestable at person, group and company scope, and a scope that is not one of
these three SHALL be refused rather than silently widened or narrowed to one that is.

#### Scenario: Each of the three scopes
- **WHEN** statistics are requested at person, group and company scope over the same corpus
- **THEN** each returns statistics over that scope's readable material

#### Scenario: A scope that is not one of the three
- **WHEN** the request names a scope of another kind, or a person or group the hub has no record of
- **THEN** the request is refused with its own machine-readable code and a non-zero exit, and no
  statistics for any other scope are returned in its place

#### Scenario: Statistics add no capability scope
- **WHEN** the capability vocabulary is inspected after this capability exists
- **THEN** it is exactly read, write and publish, and reading statistics requires read and nothing
  else

### Requirement: Recency is defined against the latest version and does not vary by scope
Recency SHALL be the timestamp of the latest version of the most recently written readable note in
scope, that definition SHALL be stated in the output, and it SHALL be the same definition at every
scope.

#### Scenario: An amendment moves recency
- **WHEN** the oldest note in the corpus is amended, making its latest version the most recent
  writing in the corpus
- **THEN** the reported recency is that amendment's timestamp, at every scope that contains the note

#### Scenario: No readable note in scope
- **WHEN** there is no note in scope that the requester may read
- **THEN** recency is a determined "none", rendered distinguishably from undetermined recency

### Requirement: Statistics start no daemon and open no network without a hub
Requesting statistics SHALL NOT start the daemon and SHALL NOT open a network connection when no hub
is configured.

#### Scenario: No daemon running
- **WHEN** statistics are requested with no daemon running
- **THEN** no daemon is started, the response reports that the daemon is not running with its own
  reason code, and that is distinguishable from a run in which the daemon was running and a
  statistic was undetermined for another reason

#### Scenario: No hub configured
- **WHEN** statistics are requested on a machine with no hub configured
- **THEN** no connection is attempted, local statistics that can be computed locally are returned as
  determined values, and hub statistics are returned as undetermined with the reason being that no
  hub is configured — not as zero and not as an unexplained failure

#### Scenario: A hub that is configured but unreachable
- **WHEN** statistics are requested with a hub configured that cannot be reached
- **THEN** hub statistics are undetermined and the response distinguishes "could not reach the hub"
  from "the hub reports nothing readable here"

### Requirement: The CLI and the agent API report the same statistics
For the same scope and the same identity, the statistics reported through the CLI and through the
agent API SHALL be the same, including which statistics are undetermined.

#### Scenario: The two surfaces agree
- **WHEN** the same request is made through the CLI and through the agent API, for each way a
  statistic can come back undetermined
- **THEN** both report the same value or the same undetermined marker for every statistic, the same
  reason codes, and the same exit status
