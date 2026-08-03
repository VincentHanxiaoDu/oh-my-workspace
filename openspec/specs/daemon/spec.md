# daemon Specification

## Purpose

The daemon is the long-running half of the client: it watches projects, ingests from channels, and
owns the store's write lock. **One daemon per store**, enforced by an exclusive lock — a second
cannot start against the same store, and the refusal names the process that holds it rather than
failing vaguely.

**Nothing starts it on a person's behalf.** If it is not running, every command that would have
needed it says so instead of quietly launching one (PRD §4.2). A background process a person did not
start is a background process they cannot reason about.

The rest of this capability is the daemon being honest about itself, which is harder than running:

- **It reports how its last run ended**, and the endings are distinct values rather than a
  best guess: ended by an explicit stop; ended because it could not write to the store; ended without
  recording an ending at all (a crash or power loss). **Never having run is a fourth answer**, and an
  ending that *could not be determined* — the record is present but unreadable — is a fifth. None of
  them is silence and none of them is rounded to another.
- **It stops when it cannot write**, rather than continuing in a state a person would read as healthy
  (PRD §4.3). Losing the ability to write is not a degraded mode to soldier on in; it is the end of
  the run, and the reason is recorded so it outlives the process that observed it.
- **The control API is local and demonstrably so.** It confirms its socket is owner-only before
  opening and does not open if it cannot confirm it (PRD §4.6). Where that cannot be confirmed, the
  CLI says so — the product does not fall back to something weaker and call it success.
- **The CLI and the control API report the same state.** Two surfaces onto one daemon must not be
  able to disagree about whether it is running.

A store that cannot be read is not an absent daemon, and a stale lock left by a dead process is not a
live one. Both are stated as what they are.
## Requirements
### Requirement: The daemon starts and stops only when a person says so
The product SHALL start the daemon only in response to an explicit command, SHALL report the daemon
as running once that command has returned successfully, and SHALL start it from no other command.

#### Scenario: An explicit start against an existing store
- **WHEN** a person runs the start command against a store that exists and has no daemon
- **THEN** the command exits zero, and from that moment the daemon is running and reports itself as
  running

#### Scenario: An explicit stop
- **WHEN** a person runs the stop command against a running daemon
- **THEN** the command returns only once the daemon has stopped and the store's write lock is
  released, and a subsequent start against the same store succeeds

#### Scenario: A stop with nothing to stop
- **WHEN** a person runs the stop command against a store with no daemon running
- **THEN** the output says the daemon is not running, and it is not the output a stop that
  terminated a running daemon produces

#### Scenario: A start against a store that does not exist
- **WHEN** a person runs the start command against a path where no store has been created
- **THEN** no store is created, the store is named as the missing thing in the wording the store
  itself uses, and the command exits non-zero

#### Scenario: Any other command, with no daemon running
- **WHEN** a person runs any command in the product while no daemon is running
- **THEN** no daemon process exists after that command returns

### Requirement: One daemon per store, and the lock is per store
The product SHALL enforce that at most one daemon holds a store at a time, SHALL allow daemons
against different stores on one machine to run concurrently, and SHALL never report a lock left by a
process that is no longer alive as a live conflicting daemon.

#### Scenario: A second start against a store that already has a daemon
- **WHEN** a person starts a daemon against a store another daemon already holds
- **THEN** the second start exits non-zero naming the lock conflict, the first daemon is unaffected
  and still answers on its control API, and the refusal reads differently from a missing store and
  from a write failure

#### Scenario: Two stores on one machine
- **WHEN** daemons are started against two different stores on the same machine
- **THEN** both start and both run concurrently

#### Scenario: A lock file left behind by a daemon that is gone
- **WHEN** a person starts a daemon against a store whose lock file was left by a process that is no
  longer alive
- **THEN** the start does not fail permanently: the store is acquired, what was found is reported
  precisely as stale, and no live conflicting daemon is claimed

### Requirement: The daemon reports its own state, including how its last run ended
The product SHALL report the daemon's state on request without starting it, and that report SHALL
include how the previous run ended, as distinct renderings none of which is silence.

#### Scenario: Each way a run can end
- **WHEN** the state is asked for after a run that was stopped on purpose, after a run that stopped
  because it could not write, and after a run that ended without recording an ending
- **THEN** the three are reported as three distinct renderings, and none of them reads as any of the
  others

#### Scenario: A store whose daemon has never run
- **WHEN** the state is asked for against a store whose daemon has never run
- **THEN** the report says so, distinguishably from every recorded ending and from an empty value

#### Scenario: A run record that is present and cannot be read
- **WHEN** the state is asked for and the record of the previous run exists but is unreadable or
  incomplete
- **THEN** how the last run ended is reported as undetermined — distinguishable from a clean ending,
  from a crash and from never having run, and never rendered as silence

#### Scenario: Asking a daemon that is not running
- **WHEN** the state is asked for while no daemon is running
- **THEN** nothing is started, and how the last run ended is still reported

#### Scenario: Two ways of asking, one state
- **WHEN** the state is read over the control API and through the command line for the same daemon at
  the same moment
- **THEN** both report the same state

### Requirement: The daemon stops when it cannot write, and never reads as healthy afterwards
The product SHALL stop the daemon when it can no longer write to the store, SHALL record that as the
reason the run ended, and SHALL NOT present a state a person would read as healthy at any point after
a failed write has been observed.

#### Scenario: Writing stops working while the daemon runs
- **WHEN** the daemon can no longer write to the store
- **THEN** it stops, it does not report itself as running afterwards, and it does not continue
  watching or ingesting

#### Scenario: The reason survives the run
- **WHEN** the state is asked for after a daemon stopped because it could not write
- **THEN** the last run is reported as having ended because it could not write, distinguishably from
  an explicit stop

#### Scenario: Between the failure and the stop
- **WHEN** the daemon's state is read repeatedly while its writes begin to fail
- **THEN** no reading taken after the daemon has observed a failed write reports it as running and
  healthy

#### Scenario: A reader that cannot reach the control API
- **WHEN** the daemon's state is read by something that cannot reach its control API
- **THEN** the daemon's health is reported as undetermined, and never as healthy, because nothing
  such a reader can see establishes it

### Requirement: The control API is local, and does not open unless it can prove it
The product SHALL confirm, as an observable step before opening the control API, that its socket is
reachable by its owner alone; SHALL NOT open the control API otherwise; and SHALL NOT substitute any
other transport when it does not open.

#### Scenario: Owner-only access is confirmed
- **WHEN** the daemon starts and owner-only access to its socket can be confirmed
- **THEN** the control API opens, and the confirmation happened before anything was listening

#### Scenario: Owner-only access cannot be confirmed
- **WHEN** owner-only access to the socket cannot be confirmed, on any platform and for any reason
- **THEN** the control API does not open, nothing is left listening, and the command line says so in
  wording distinguishable from "the daemon is not running" and from "the daemon is running normally"

#### Scenario: No fallback transport
- **WHEN** the control API does not open
- **THEN** no network-reachable listener is opened in its place, and no other transport is used

#### Scenario: A control API whose state could not be determined
- **WHEN** whether the control API is open could not be determined
- **THEN** it is reported as undetermined, never as closed and never as silence

#### Scenario: The daemon with no control API
- **WHEN** the daemon runs having declined to open its control API
- **THEN** it still runs, still records how its run ended, and every other capability's output and
  exit code are unaffected

### Requirement: The local half works with no hub configured
The product SHALL allow the daemon to be started, run, stopped and queried with no hub configured,
SHALL open no network connection while doing so, and SHALL NOT let any of those capabilities fail
silently or half-work.

#### Scenario: The whole lifecycle with no hub
- **WHEN** the daemon is started, queried and stopped on a machine with no hub configured
- **THEN** each command completes or names precisely what is missing, none of them is silent, and no
  outbound network connection is made for the daemon's lifetime

### Requirement: Whether the daemon is running has one definition
The product SHALL answer "is the daemon running against this store" in exactly one place, and that
place SHALL be the package that owns the daemon. No other package SHALL derive, name or stat a
control socket path, and no command SHALL determine liveness from an environment variable naming a
socket.

#### Scenario: A surface asks whether the daemon is running
- **WHEN** any command needs to know whether the daemon is running against a store
- **THEN** it obtains the answer from the daemon package's inspection of that store, and it does not
  reconstruct the socket path or read a socket-naming environment variable

#### Scenario: A second definition is introduced
- **WHEN** a package outside the daemon package names a control socket path, names a
  socket-naming environment variable, or stats a socket path
- **THEN** the build fails and names the one definition that should have been called instead

#### Scenario: The structural search stops matching
- **WHEN** the search that enforces the single definition examines no files, or fails to match code
  known to violate the rule
- **THEN** it refuses rather than passing, because a search that matches nothing is
  indistinguishable from a codebase that no longer offends

### Requirement: Every surface that reports daemon state agrees with the daemon status command
The product SHALL report the same liveness from every command that reports it as the daemon status
command reports for the same store at the same moment, in all three answers.

#### Scenario: A daemon is genuinely running
- **WHEN** a person starts the daemon against a store and then runs any command that reports daemon
  state
- **THEN** that command reports the daemon as running, and the daemon status command agrees

#### Scenario: No daemon is running
- **WHEN** no daemon is running against the store and a person runs any command that reports daemon
  state
- **THEN** that command reports the daemon as not running, and the daemon status command agrees

#### Scenario: The daemon is stopped while the person is working
- **WHEN** a running daemon is stopped and the same commands are run again
- **THEN** every one of them reports the daemon as not running, so no surface retains an answer it
  established earlier

#### Scenario: Agreement is asserted between surfaces
- **WHEN** the product's tests establish that these surfaces are correct
- **THEN** they compare the surfaces with each other and with the daemon status command against one
  genuinely running daemon, and not each surface's own text in isolation

### Requirement: A liveness that cannot be established is undetermined, never a negative
The product SHALL report liveness as undetermined, with a reason, wherever it genuinely cannot be
established — the store's location cannot be worked out, or the evidence of a running daemon cannot
be read — and SHALL NOT report such a case as a daemon that is not running.

#### Scenario: The evidence of liveness cannot be read
- **WHEN** a command needs the daemon and whether one holds the store cannot be established
- **THEN** the command says that whether the daemon is running could not be determined, gives the
  reason, states that this is not a report that the daemon is stopped, and exits with the
  undetermined exit code

#### Scenario: The store's location cannot be worked out
- **WHEN** a command needs the daemon and the store this invocation is about cannot be resolved
- **THEN** liveness is reported as undetermined with that as the reason, and not as a daemon that is
  not running

#### Scenario: The two negatives are told apart
- **WHEN** the undetermined report and the established "not running" report are compared
- **THEN** they share neither their wording nor their exit code, and the undetermined report does
  not contain the sentence the established negative uses

### Requirement: No surface justifies stale output by an absence that is not there
The product SHALL NOT state that it is showing on-disk state because nothing is watching, or
otherwise explain its output by an absent daemon, while a daemon is running against that store.

#### Scenario: A listing runs with a daemon running
- **WHEN** a daemon is running against the store and a person runs any command in the product
- **THEN** its output contains no claim that the daemon is not running and no claim that nothing is
  watching

