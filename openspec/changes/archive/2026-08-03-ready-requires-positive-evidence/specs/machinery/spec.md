# machinery

## ADDED Requirements

### Requirement: A mergeable signal SHALL rest on positive evidence
A watch SHALL announce a pull request as ready to merge only on evidence that the gates have spoken
and passed: at least one completed check run, no check run still running, and a review verdict
positively read as successful. The absence of a failure, of a pending run, and of a refusal SHALL NOT
together constitute a pass — on a head where nothing has reported, every one of those tests is
vacuously true.

A verdict that could not be read SHALL NOT be treated as a verdict that is absent, and neither SHALL
reach the ready signal.

#### Scenario: Nothing has reported on the head
- **WHEN** a pull request's head has no check runs at all, no commit statuses and no reviews
- **THEN** the watch does not announce it as ready

#### Scenario: The gates have passed and no verdict has been published
- **WHEN** every check run on the head has completed successfully and no review verdict has been
  published
- **THEN** the watch does not announce it as ready

#### Scenario: A check run is still running
- **WHEN** at least one check run on the head has not completed
- **THEN** the watch does not announce it as ready

#### Scenario: The verdict cannot be read
- **WHEN** the query for the head's commit statuses fails
- **THEN** the watch reports the failed lookup and does not announce the pull request as ready

#### Scenario: The evidence is there
- **WHEN** a check run has completed successfully, none is still running, and the review verdict is
  successful
- **THEN** the watch announces the pull request as ready

### Requirement: A head with no answer yet SHALL have its own event
A watch SHALL emit a distinct event for a pull request whose head has not been answered, saying that
nothing has reported and that this is not a pass. It SHALL NOT convey that state by emitting nothing:
silence would then mean both "nothing to report" and "no answer yet", which is the collapse the
watch's other events exist to prevent.

This SHALL hold on every entry point that evaluates a pull request's state, not only on the one-pass
sweep.

#### Scenario: Nothing has reported, on the continuous watch
- **WHEN** a pull request's head has nothing reported on it and the watch is running continuously
- **THEN** the watch emits its no-answer event for that pull request

#### Scenario: Nothing has reported, on a single pass
- **WHEN** a pull request's head has nothing reported on it and a single pass over the board is
  requested
- **THEN** that pass emits the no-answer event for that pull request

### Requirement: A single pass SHALL terminate whatever it finds
A one-pass sweep SHALL end, and SHALL exit non-zero when it could not answer, including when it
stood down for a rate limit. It is the fallback used when the continuous watch has died, so a sweep
that waits and retries hangs exactly like the watch it replaces.

#### Scenario: The sweep stands down for a budget limit
- **WHEN** a single pass is requested and the API budget guard reports that the caller should stand
  down
- **THEN** the pass ends rather than sleeping and retrying, and exits non-zero
