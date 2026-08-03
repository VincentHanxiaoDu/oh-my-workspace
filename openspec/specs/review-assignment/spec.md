# review-assignment Specification

## Purpose
A reviewer can trust that the queue only shows them work they can actually act on. Once they have
ruled on a pull request — approving it or refusing it — that same unchanged pull request stops
coming back to them, so a refusal is a decision that sticks rather than a job that reappears. When
the author pushes, it returns, because there is now something new to judge.

Everybody else's queue is unaffected by that: one reviewer's verdict never quietly removes the work
from another independent role, so a pull request cannot go unlooked-at because somebody already
looked. Nothing is claimed or locked up front, so a reviewer who is interrupted strands nothing and
nobody waits for a marker to expire. And if the queue cannot find out whether a verdict exists, it
says so and stops, rather than presenting an unanswered question as an unreviewed pull request.

## Requirements
### Requirement: A role is not offered a head it has already ruled on
The queue SHALL NOT offer a pull request to a role that has already recorded a verdict against that
pull request's current head, whatever that verdict was. A refusal and an approval are both reviews
that happened, and only the head sha decides whether the record is current.

#### Scenario: A refusal does not come back to its author
- **WHEN** a role has posted a `changes-requested` verdict against the current head, leaving the
  review status red
- **THEN** the queue does not offer that pull request to that role

#### Scenario: A push makes the record stale
- **WHEN** a role's only recorded verdict names a head other than the current one
- **THEN** the queue offers that pull request to that role again

### Requirement: Suppression is per role and never narrows the eligible set
The queue SHALL suppress a pull request only for the role whose verdict it read, and SHALL continue
to offer it to every other role that authored none of its commits. One role's verdict SHALL NOT
decide the work for any other role.

#### Scenario: Another independent role still sees it
- **WHEN** one role has recorded a verdict against the current head and a second role authored none
  of the pull request's commits and has recorded no verdict
- **THEN** the queue offers that pull request to the second role

#### Scenario: An unreviewed head reaches every independent role
- **WHEN** no verdict has been recorded against the current head
- **THEN** the queue offers the pull request to every role that authored none of its commits, and to
  no role that authored any of them

### Requirement: No claim is taken before a review, so none can be stranded
The queue SHALL derive suppression solely from a record written after a review is given, and SHALL
NOT write, hold, or require the release of any marker before or during one. A role that stops
mid-review SHALL leave the pull request available to every role, including itself.

#### Scenario: A role stops before posting a verdict
- **WHEN** a role begins a review and stops without recording a verdict
- **THEN** the queue continues to offer that pull request to that role and to every other
  independent role, and nothing has to expire for that to be true

### Requirement: A verdict record that cannot be read is not an absent verdict
The queue SHALL exit non-zero when the verdict record for a pull request cannot be read, and SHALL
NOT report that pull request as awaiting a review. `Could not determine whether a verdict exists`
and `determined that none exists` SHALL NOT share an exit code.

#### Scenario: The record cannot be read
- **WHEN** the query for a pull request's verdict record fails
- **THEN** the queue exits non-zero, says the lookup failed, and offers no review on the strength of
  that query

