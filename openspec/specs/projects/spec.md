# projects Specification

## Purpose

A person points the client at the directories that matter to them, and the client watches them
(PRD §3.6). That is the easy half.

The hard half is that **watching is a poll, not an instant notification** — and that whether anything
is polling depends on whether the daemon is running. So a project listing has two quite different
things it can be telling you:

- the daemon is running and re-examining these directories every couple of seconds, so what you are
  reading is at most that stale; or
- **nothing is watching at all**, and this listing looked at each directory during this very command.

**The listing says which of the two happened.** Not as a footnote — per project, in the output, so
the two cases are distinguishable from the listing alone with no other command run and nothing
inferred from timing. A listing that showed correct fields with no indication of where they came from
would fail this capability even if every field were right, because the person's real question is *"is
anything keeping up with these?"* and correct-looking numbers do not answer it.

**A missing project directory is marked, never dropped.** And marked is not the same as empty, which
is not the same as unreadable: a directory that is gone, a directory that is there and holds nothing,
and a directory that is there and could not be read are **three different answers**, rendered three
different ways, none of them silence (PRD §4.3). Dropping the first would quietly shrink the list a
person is relying on to be complete.

The walk is bounded and says so. It descends a fixed depth, prunes the directories nobody means
(`node_modules`, `.venv`, `dist`, `target`, and everything dot-prefixed), does not follow symbolic
links, and **never parses `.gitignore` itself** — inside a repository it asks git, which is the only
answer that cannot drift from what git actually ignores. Where the walk stops early because of the
depth limit, the listing says that too: a truncated scan must not read as a complete one.

Nothing here starts the daemon on a person's behalf (PRD §4.2), and whether the daemon is running is
asked of the daemon rather than guessed at locally, so this surface and `omw daemon status` cannot
disagree about the same machine (PRD §4.3).
## Requirements
### Requirement: A person points the client at directories and they stay pointed at
The product SHALL let a person add a directory as a project, list every project that was added and
not removed, and remove one without touching the directory on disk. Adding the same directory twice
SHALL leave one project.

#### Scenario: The same directory added twice
- **WHEN** a person adds a directory and then adds the same directory again
- **THEN** the listing contains one entry for it, and the second add is not an error

#### Scenario: A project removed
- **WHEN** a person removes a project
- **THEN** it no longer appears in the listing, and every file in the directory is untouched

### Requirement: Watching is a poll, and only a running daemon does it
While a daemon is running the product SHALL re-examine each watched directory every couple of
seconds, so a change is reflected within about that long with no command run. With no daemon
running the product SHALL NOT observe any watched directory between commands.

#### Scenario: A change while the daemon is running
- **WHEN** a file changes inside a watched directory while the daemon is running, and more than the
  poll interval passes with no command run
- **THEN** the state the product holds for that project reflects the change, and a listing run
  afterwards attributes that state to the daemon rather than to its own examination

#### Scenario: A daemon started the way a person starts it
- **WHEN** a person starts the daemon and adds a file to a watched directory, running nothing else
- **THEN** the polled state recorded for that project advances without any command being run

#### Scenario: A change while nothing is running
- **WHEN** the daemon is stopped, a file changes inside a watched directory, and well beyond the poll
  interval passes with no command run
- **THEN** no state anywhere has advanced, and the state advances only when a listing is run

### Requirement: Every listing says where the state it shows came from
The product SHALL state, in the listing output for each project, whether the state shown was
produced by the daemon's polling or by the directories being examined during that command. The two
cases SHALL be distinguishable from the listing output alone, with nothing inferred from timing.

#### Scenario: The same command in the two situations
- **WHEN** the same listing command is run over the same projects, once with the daemon running and
  once with it stopped
- **THEN** the two outputs differ, the first states that the state came from the daemon's polling and
  the second states that the directories were examined during that command

#### Scenario: A state with no provenance recorded
- **WHEN** an entry reaches the output without a provenance having been recorded for it
- **THEN** it renders as a defect marker and as neither of the two real answers

#### Scenario: Whether a daemon is running could not be established
- **WHEN** a listing is run and the product cannot establish whether a daemon is watching
- **THEN** the listing still reports each project's state, states that whether anything is watching
  could not be determined together with the reason, marks each entry's provenance as undetermined
  rather than as examined-during-this-command, does not report that nothing is watching, and exits
  with the code reserved for an undetermined answer

### Requirement: Missing, unreadable and empty are three distinct answers
The product SHALL mark a project whose directory is missing rather than omitting it, SHALL mark a
project whose state could not be determined as undetermined rather than as a negative or as silence,
and SHALL render a missing directory, a directory that exists and cannot be read, and a directory
that exists and is empty as three different things.

#### Scenario: All three in one listing
- **WHEN** a listing contains a project whose directory is missing, one whose directory exists and
  cannot be read, and one whose directory exists and is empty
- **THEN** all three appear, none of them is silence, and no two of them print the same thing

#### Scenario: One project's directory is missing
- **WHEN** a project's directory does not exist at the moment its state is determined
- **THEN** that project appears marked as missing, the listing does not fail, and the other projects
  are reported as they would have been anyway

### Requirement: No project command starts the daemon or reaches the network
Adding, listing and removing projects SHALL leave a stopped daemon stopped, SHALL create no store,
and SHALL make no outbound connection when no hub is configured. All three SHALL work fully with no
hub configured.

#### Scenario: Listing with the daemon stopped
- **WHEN** a person adds, lists and removes projects with the daemon stopped
- **THEN** whatever the product uses to report daemon state still reports that nothing is running,
  and the listing says so

#### Scenario: Listing with a daemon running
- **WHEN** a person lists projects while a daemon is running
- **THEN** the listing agrees with the product's own daemon-state surface, and reports no absence

#### Scenario: No hub configured
- **WHEN** adding, listing and removing are run with no hub configured
- **THEN** each completes and exits zero

### Requirement: A path that is not a healthy directory is not accepted as one
The product SHALL NOT accept a path that is not a directory, or that does not exist when it is added,
and render it as an ordinary present project. It SHALL either refuse it distinguishably by exit
status alone, or accept it carrying the missing marking.

#### Scenario: A file offered as a project
- **WHEN** a person points the client at a regular file, or at a path that is not there
- **THEN** the command exits with a status different from the one a healthy directory produces, and
  the path does not appear in the listing as an ordinary project

### Requirement: The walk is bounded, configurable, and says when it was cut short
The product SHALL descend at least eight directory levels below a watched directory by default, SHALL
allow that limit to be set to another value, and SHALL state in the listing when the limit was
actually reached so that a truncated walk never renders as a complete one.

#### Scenario: A file eight levels down
- **WHEN** a file sits eight directory levels below a watched directory and a default scan runs
- **THEN** that file is reflected in the project's state and the walk is not reported as truncated

#### Scenario: The limit lowered over the same tree
- **WHEN** the same tree is listed at the default limit and at a smaller one
- **THEN** the two listings differ, and the smaller one states that the walk was truncated

### Requirement: Links are not followed and pruned names are not entered
The product SHALL NOT descend into a symbolic link, including one pointing at an ancestor of itself,
and the walk SHALL terminate. It SHALL skip `node_modules`, `.venv`, `venv`, `__pycache__`, `.git`,
`dist`, `build`, `.next`, `target`, `.cache`, `vendor` and every directory whose name begins with a
dot DURING the walk rather than walking them and filtering afterwards.

#### Scenario: A link pointing at its own ancestor
- **WHEN** a watched directory contains a symlink to one of its own ancestors
- **THEN** the scan terminates and nothing reached through the link is counted as the project's

#### Scenario: A deep tree under a pruned name
- **WHEN** a large or deep tree is placed inside a directory on the prune list
- **THEN** none of it appears in the project's state and the scan does not enter that directory

### Requirement: The client parses no ignore file
Inside a git repository the excluded set SHALL be whatever git itself reports, including nested
ignore files, negation patterns and repository-level excludes that live in no `.gitignore`. Outside a
repository the prune list and the dot rule SHALL be the entire exclusion policy.

#### Scenario: A repository with awkward ignore rules
- **WHEN** a watched directory is a git repository with a nested ignore file, a negation pattern and
  a repository-level exclude
- **THEN** a file git ignores is absent from the project's state and a file git does not ignore is
  present

#### Scenario: An ignore file outside a repository
- **WHEN** a watched directory that is not a repository contains a `.gitignore` naming a file
- **THEN** that file is still present in the project's state

### Requirement: A partially-read project never renders as a complete scan
When part of a project cannot be read, the product SHALL still list the project, SHALL report the
unreadable portion as unreadable, SHALL continue the walk, SHALL leave the other projects unaffected,
and SHALL render the result distinguishably from a walk that read everything.

#### Scenario: An unreadable subdirectory mid-walk
- **WHEN** a watched directory contains a subdirectory that refuses to be read
- **THEN** the project appears, the unreadable portion is named, the readable remainder is counted,
  the entry differs from one produced by a complete scan, and no other project is affected

### Requirement: The control API and the CLI report the same project state
For the same projects at the same moment, the provenance, the missing marking and the undetermined
marking SHALL agree between the control API and the CLI.

#### Scenario: One determination rendered twice
- **WHEN** the same project state is rendered for a person and served over the control API's wire
  form
- **THEN** the provenance, the missing marking and the undetermined marking are the same in both

### Requirement: A poll the watcher misses costs latency and nothing else
Each pass of the watcher SHALL record what is in a watched directory at the moment it looks, derived
from a fresh examination and from nothing carried over from an earlier pass. It SHALL NOT accumulate
changes, and SHALL NOT depend on having separately observed any change.

A scheduled pass that does not run — the ticker drops ticks under load rather than queueing them —
SHALL therefore cost only the delay until the next pass. It SHALL NOT be possible for a change to go
unreported because the pass that would have seen it did not run.

#### Scenario: Several changes appear with no pass running
- **WHEN** files are added to a watched directory while no pass of the watcher runs, and then one
  pass runs
- **THEN** that pass records every file present, not only those it separately observed appearing

#### Scenario: A file is removed with no pass running
- **WHEN** a file is removed from a watched directory while no pass runs, and then one pass runs
- **THEN** that pass records the reduced count

#### Scenario: A pass runs over an unchanged directory
- **WHEN** two consecutive passes run over a directory that did not change between them
- **THEN** both record the same state

#### Scenario: Changes arrive faster than the poll interval
- **WHEN** more changes are made to a watched directory than there are passes to observe them
  separately, and a pass then runs after the last of them
- **THEN** that pass records the final state exactly

### Requirement: A test of the watcher is bounded by a stall, never by elapsed time
A test asserting that the watcher advances state SHALL bound its wait by a span in which the watcher
recorded NOTHING, and SHALL restart that bound on every poll it observes. It SHALL NOT fail on total
elapsed time.

A machine under load runs a correct watcher slowly, and a bound on elapsed time turns that into a
report that the watcher does not work — a false red, on a gate, about the machine rather than the
product. Raising such a bound is not a remedy: it moves the same cliff.

Where the test asserts a particular recorded value, it SHALL wait for a poll stamped after the change
was made — such a poll necessarily examined the directory after it — and SHALL then assert the value
once, so that a watcher recording the wrong value reports a wrong value rather than a timeout.

#### Scenario: The watcher is slower than any fixed deadline the test might have used
- **WHEN** the watcher's interval is longer than a deadline a total-elapsed bound would have imposed,
  and the watcher is otherwise working
- **THEN** the test passes

#### Scenario: The watcher records the wrong value
- **WHEN** the watcher polls without pause but records a value other than the one the change requires
- **THEN** the test fails naming the recorded value and the expected one, rather than timing out

#### Scenario: The watcher records nothing at all
- **WHEN** no poll is ever recorded
- **THEN** the test fails saying that nothing is advancing project state in the background

