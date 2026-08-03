# machinery

## ADDED Requirements

### Requirement: A budget guard SHALL NOT report a healthy budget while calls are being refused
The API budget guard SHALL treat a refusal carrying a rate-limit body as evidence that calls are
being refused, and SHALL stand the watches down on it. It SHALL NOT answer with a counter that is
true about a different limit: the primary hourly quota reads nearly full throughout a secondary
(burst) throttle, and reporting it is answering a question nobody asked.

Where the secondary state cannot be determined without spending a call, the guard SHALL say that it
could not be determined rather than presenting the primary counter as though it settled the matter.
`Could not determine` and `determined to be nothing` SHALL NOT share a rendering.

#### Scenario: A secondary rate limit is in force and the primary quota reads healthy
- **WHEN** a call has been refused with a body naming a rate limit, and the primary quota reports
  nearly its full allowance remaining
- **THEN** the guard stands the caller down rather than reporting budget, names the throttle as a
  secondary burst limit rather than as primary exhaustion, and says when it will retry

#### Scenario: The primary quota is healthy and no refusal has been seen
- **WHEN** the primary quota is above the reserve and no call has been refused
- **THEN** the guard permits polling and states that its answer is about the primary quota only and
  that the secondary limit could not be determined, rather than reporting a bare number

#### Scenario: The primary quota is exhausted
- **WHEN** the primary quota is at or below the reserve reserved for the role's own work
- **THEN** the guard stands the caller down and says when the quota resets

#### Scenario: The rate limit cannot be read at all
- **WHEN** the query for the rate limit fails
- **THEN** the guard answers with neither "budget" nor "exhausted", and names itself a lookup failure

### Requirement: A watch SHALL distinguish a throttle from an outage from a quiet board
A watch whose poll is refused by a rate limit SHALL report that it is holding, and SHALL NOT report
it as a failed lookup or poll again on its ordinary interval — a secondary limit clears with quiet,
so retrying extends it. A poll that failed for any other reason SHALL remain a reported lookup
failure. Neither SHALL be rendered as a board with nothing on it.

A hold SHALL NOT end the watch: the throttle is transient, and a watch that stops is
indistinguishable from a quiet queue.

#### Scenario: The poll is refused by a secondary rate limit
- **WHEN** a watch's poll is refused with a body naming a secondary rate limit
- **THEN** the watch reports that it is holding, names the burst throttle, says when it will retry,
  and resumes polling afterwards rather than exiting

#### Scenario: The poll fails for a reason quiet does not fix
- **WHEN** a watch's poll fails with a network error such as a connection timeout
- **THEN** the watch reports a lookup failure and does not report that it is holding

### Requirement: A stand-down SHALL wait on the clock belonging to its cause
The wait after a hold SHALL be derived from the limit that caused it. A secondary limit clears with
quiet and SHALL be waited out on its own cooldown, or on the `Retry-After` the refusal carried when
it carried one. A primary exhaustion SHALL be waited out on the quota reset.

#### Scenario: The refusal carried a Retry-After
- **WHEN** a refusal names the number of seconds to wait
- **THEN** the stand-down uses that number rather than a default cooldown

#### Scenario: A secondary limit is live and the primary quota resets much later
- **WHEN** a secondary limit is in force and the primary quota's reset is far in the future
- **THEN** the stand-down is the secondary limit's cooldown and not the primary reset interval
