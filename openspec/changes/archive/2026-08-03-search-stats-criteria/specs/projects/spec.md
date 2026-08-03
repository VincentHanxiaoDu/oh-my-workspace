# projects

## ADDED Requirements

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
