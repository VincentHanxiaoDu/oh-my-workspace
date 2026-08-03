# status Specification

## Purpose

**One screen that says whether everything runs** (PRD §3.9). A person should not have to know which
of a dozen commands owns which fact in order to find out whether their client is working.

That makes this the most dangerous surface in the product, because it is the only one that speaks for
things it does not own. Every line here belongs to another capability — the daemon, the store, the
hub, projects, devices, channels — and a summary that disagrees with the thing it summarises is worse
than no summary at all: it teaches a person to distrust both. So the rule is not "status is usually
right", it is that **status and the command it reports on cannot disagree**, because status asks that
command rather than working the answer out again.

This is not hypothetical. Three separate capabilities shipped a private guess at whether the daemon
was running, each answering "no" while a daemon was running, before the answer was made singular
(§4.3: *the control API and the CLI report the same state*).

**A state that could not be determined is shown as undetermined, never as a "no"** (PRD §4.3). This
screen exists to be read quickly, which is exactly when a false negative does its damage: a person
scanning for problems and finding none, because something could not be checked and said so as
silence.

**And it starts nothing.** Asking whether the daemon is running never starts it; asking about the
store never creates one (PRD §4.2). A diagnostic that changes what it is diagnosing is not a
diagnostic. With no daemon running, the screen still reports everything readable from disk — and says
which parts came from a live daemon and which were examined during this command, so a person can tell
current state from last-known state.
## Requirements
### Requirement: One screen names every subsystem the client is made of
The product SHALL report, in one status invocation, the state of every subsystem PRD §2.1 names as
part of a running client: the daemon, the local store, the configured channels, the watched
projects, the devices registration and the hub connection. Each SHALL appear as its own named line
carrying its own state. No subsystem SHALL be omitted. A subsystem that is not configured SHALL be
shown as not configured, and that rendering SHALL differ from both the running rendering and the
failing rendering.

#### Scenario: A freshly installed client
- **WHEN** a person runs status against a machine with a store and nothing else configured
- **THEN** all six subsystems appear, each on its own named line, and the unconfigured ones say they
  are not configured rather than reading as failures

#### Scenario: The three renderings compared with each other
- **WHEN** the same subsystem is rendered as working, as not working and as not configured
- **THEN** the three lines are pairwise distinct, with no fixed wording assumed by the comparison

### Requirement: The screen reports how the daemon's last run ended
The product SHALL report on the status screen how the daemon's last run ended, taking the answer
from the daemon's own state model. A run that ended cleanly, a run that ended because the daemon
could not write, and a run whose outcome is unrecorded SHALL produce three outputs that differ from
each other.

#### Scenario: Three endings
- **WHEN** status is taken against a store whose last run ended cleanly, once whose last run ended
  because it could not write, and once whose ending is unrecorded
- **THEN** the daemon line differs in all three cases, compared pairwise

### Requirement: Every state carries when it was observed, or says it has none
The product SHALL carry on each subsystem line the time the state was observed. A state with no
observation time SHALL be shown as having none and SHALL NOT be rendered with a substituted or
default time.

#### Scenario: A state produced by an earlier poll
- **WHEN** a subsystem's state was produced by a daemon poll that recorded its time
- **THEN** the line carries that poll's time, not the time of this invocation

#### Scenario: A state whose observation time was never recorded
- **WHEN** a subsystem's state was produced by a poll that recorded no time
- **THEN** the line says it has no observation time, and carries no time from anywhere else

### Requirement: Status is a report and never a mutation
The product SHALL NOT change any store content, any configuration or any daemon lifecycle when
status is invoked. Two consecutive invocations against an unchanged client SHALL produce the same
subsystem states.

#### Scenario: Twice in a row
- **WHEN** status is run twice against an unchanged client
- **THEN** the two screens report the same state for every subsystem, and every file on the machine
  is unchanged

#### Scenario: Status where no store exists
- **WHEN** status is invoked against a location holding no store
- **THEN** it reports that no store exists, and no store exists afterwards

### Requirement: Undetermined is its own answer on every line
The product SHALL represent and render three outcomes separately for every subsystem: working, not
working, and could not be determined. The undetermined rendering SHALL be distinguishable from the
not-working rendering by inspection of that line alone, and SHALL NOT be signalled by the absence of
a line or by an empty field.

#### Scenario: The same subsystem, two states, identical prose
- **WHEN** one subsystem is rendered as undetermined and as not working with the same detail text
- **THEN** the two lines still differ from each other

#### Scenario: A subsystem that arrived with no detail
- **WHEN** a subsystem carries a state and no account of it
- **THEN** the line says that no detail was recorded rather than rendering a blank

### Requirement: A state that could not be determined is never a negative and never silence
The product SHALL show an unreachable channel adapter, a project directory that has gone missing and
a device that has never checked in, each with its own state, and SHALL NOT render any of the three
identically to a subsystem confirmed not working.

#### Scenario: The three named cases beside a confirmed failure
- **WHEN** a screen contains an unreachable adapter, a missing project directory, a never-started
  device and a channel whose credential has demonstrably expired
- **THEN** all four appear on the screen and their four renderings are pairwise distinct

#### Scenario: An adapter that could not be reached
- **WHEN** the last ingestion attempt on a channel could not reach its adapter
- **THEN** that channel is undetermined, and is not reported as not working

### Requirement: One undetermined subsystem does not suppress the rest
The product SHALL yield undetermined for a probe that times out, errors or returns something it
cannot interpret, and SHALL NOT yield working or not-working in those cases. One undetermined
subsystem SHALL NOT suppress, blank or abort the remaining subsystem lines.

#### Scenario: A subsystem with one member nobody could check
- **WHEN** a subsystem has several members and exactly one of them could not be determined
- **THEN** the subsystem's own line reads as undetermined rather than as working, and differs from
  the line of a subsystem containing a confirmed failure

#### Scenario: An unreachable hub with everything else fine
- **WHEN** a configured hub cannot be reached and every local subsystem is readable
- **THEN** the hub line is undetermined, every other line is present with its own determined state,
  and no line is blank

### Requirement: The summary never leads with good news over an unchecked subsystem
The product SHALL NOT lead the status screen with a summary saying everything is fine when any
subsystem is undetermined. A screen with at least one undetermined subsystem SHALL be
distinguishable at the summary line from a screen on which every subsystem is confirmed working.

#### Scenario: Each subsystem undetermined in turn
- **WHEN** an otherwise fully-working screen has exactly one subsystem undetermined, in each position
  in turn
- **THEN** the summary in every case differs from the all-working summary

#### Scenario: An undetermined member of a working subsystem
- **WHEN** every subsystem line is working and one channel inside one of them is undetermined
- **THEN** the summary does not say everything is running

### Requirement: The control API and the CLI report the same state
The product SHALL report the same subsystem states through the control API's form of the answer and
through the CLI's rendered screen, for a given client at a given moment, including which subsystems
are undetermined. Neither surface SHALL carry a subsystem the other does not, and no surface SHALL
present a summarised, softened or more optimistic view than another.

#### Scenario: Both surfaces obtained and compared
- **WHEN** both surfaces are obtained from one client whose hub is configured and unreachable
- **THEN** they name the same subsystems and report the same state for every one of them, at least
  one of which is undetermined on both

#### Scenario: A subsystem the renderer does not know
- **WHEN** the control API's form carries a subsystem name and a state word this build has never
  heard of
- **THEN** the subsystem is still rendered, its unknown state reads as undetermined rather than as a
  negative, and the summary does not claim everything is working

#### Scenario: Undetermined across the boundary
- **WHEN** a subsystem is undetermined and the answer is serialised and read back
- **THEN** it is still undetermined, and is not a negative, a null or an omitted field

### Requirement: A stopped daemon is an answer, not a tool failure
The product SHALL report that the daemon is not running when it is not, SHALL NOT start a daemon
that did not exist before the invocation, and SHALL treat the delivered answer as a success —
distinguishable by the invocation's own outcome from the tool failing to produce an answer at all.

#### Scenario: Status with nothing running
- **WHEN** status is invoked against a store whose daemon is not running
- **THEN** the daemon line says so, the invocation's outcome is a success, and no daemon exists
  afterwards

#### Scenario: An invocation that could not be answered
- **WHEN** status is invoked in a way it cannot answer
- **THEN** its outcome differs from the outcome of an invocation that was answered

### Requirement: With no daemon the screen still reports everything establishable without one
The product SHALL report, with no daemon running, every fact it can establish without one — the
store's existence and location, the configured channels, the configured projects and the hub
configuration — and SHALL mark as undetermined only those facts that genuinely require a running
daemon.

#### Scenario: A configured machine with the daemon stopped
- **WHEN** status runs against a machine with a store, channels and projects, and no daemon
- **THEN** the store, channels, projects and hub lines all carry determined states, and the screen is
  not blanked

### Requirement: With no hub configured nothing reaches off the machine
The product SHALL open no network connection when no hub is configured. The hub line SHALL read as
not configured, distinguishably from hub-configured-and-unreachable and from
hub-configured-and-reachable, and every other subsystem line SHALL still render its real state.

#### Scenario: No hub configured
- **WHEN** status runs with no hub configured
- **THEN** nothing is dialled, the hub line says not configured, and every local line carries a
  determined state

#### Scenario: The three hub states compared
- **WHEN** the hub line is rendered unconfigured, configured-and-unreachable, and
  configured-and-answering
- **THEN** the three renderings are pairwise distinct, and the unreachable one is undetermined rather
  than a negative

### Requirement: A control API that declined to open says so
The product SHALL report, where owner-only permissions could not be confirmed and the control API
therefore did not open, that the control API is not open and why — rather than reporting the daemon
as simply not running or as failing. This case SHALL be distinguishable in output from a machine on
which no daemon was started.

#### Scenario: A running daemon whose control API declined
- **WHEN** a daemon is running and its control API declined to open because owner-only permissions
  could not be confirmed
- **THEN** the screen says the control API is not open and gives the reason, the daemon is not
  reported as not working, and the output differs from a machine where no daemon was started

### Requirement: A screen is never partial and never silently truncated
The product SHALL make explicit in the output any subsystem it cannot render, and SHALL NOT produce
an empty or truncated screen accompanied by an outcome indistinguishable from success. With no hub
configured it SHALL either report every local subsystem fully or name precisely which subsystem state
it could not establish and why.

#### Scenario: A screen that could not be produced at all
- **WHEN** the answer cannot be produced
- **THEN** nothing is printed as a screen and the outcome differs from that of a screen that was
  produced

#### Scenario: An undetermined subsystem changes the outcome and not the completeness
- **WHEN** one subsystem is undetermined
- **THEN** the screen still carries every line, and the invocation's outcome differs from one where
  everything was established

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
