# Daemon

## ADDED Requirements

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
