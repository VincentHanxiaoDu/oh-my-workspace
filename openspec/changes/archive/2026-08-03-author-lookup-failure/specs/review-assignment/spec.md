# review-assignment

## ADDED Requirements

### Requirement: An author set that could not be determined is not an empty author set
The queue SHALL exit non-zero when who built a pull request cannot be determined, and SHALL NOT offer
that pull request to any role on the strength of that query. `Could not determine who built this` and
`determined that nobody authored it` SHALL NOT share an exit code, and SHALL NOT share a value.

A determined empty author set — every commit spec-only — SHALL keep its existing meaning: nobody
authored product judgement, so every role is independent and the pull request is offered to all of
them.

#### Scenario: The author lookup fails while the rest of the queue answers
- **WHEN** the query for a pull request's authors fails and every other query the queue makes succeeds
- **THEN** the queue exits non-zero, says the lookup failed and that this is not a statement that
  nobody authored the pull request, and offers that pull request to no role

#### Scenario: The failure is intermittent, so only the first query fails
- **WHEN** the query for a pull request's authors fails and a follow-up query for the same pull
  request's commit trailers succeeds
- **THEN** the queue exits non-zero and offers that pull request to no role, including the role that
  authored its commits

#### Scenario: A commit's file list cannot be read
- **WHEN** a pull request's commits carry an `Agent:` trailer and the file list that decides whether
  a commit changed anything outside the generated specification directory cannot be read
- **THEN** the queue exits non-zero and offers that pull request to no role, rather than treating the
  unreadable file list as a commit that conferred no authorship

#### Scenario: Every commit is genuinely spec-only
- **WHEN** every commit of a pull request carries an `Agent:` trailer and changes nothing outside the
  generated specification directory, and every query succeeds
- **THEN** the queue offers that pull request to every role, because nobody authored product
  judgement in it
