# Scoped search

## ADDED Requirements

### Requirement: Visibility is settled before results are ordered
Search SHALL settle what the searcher may read, using the one visibility predicate, before any
result is scored or ordered, and ranking SHALL be computed over only the notes the searcher may
read.

#### Scenario: An unreadable note does not change the order of readable ones
- **WHEN** the same search is run over a corpus of readable notes, and again over that corpus plus
  one note the searcher may not read
- **THEN** the sequence of returned results and every relevance score is identical between the two
  runs

#### Scenario: A readable note does change the order
- **WHEN** the additional note is published company-wide instead, so the searcher may read it
- **THEN** the ordering of the original results changes, showing that ranking is genuinely
  sensitive to the corpus and that the previous scenario is not vacuous

### Requirement: Ranking never surfaces the existence of an unreadable note
Every observable a search emits — result count, snippet, suggestion, ordering, relevance score,
author facet and empty-result message — SHALL be identical whether or not a note the searcher may
not read exists and matches the query.

#### Scenario: A term appearing only in an unreadable note is never suggested
- **WHEN** the searcher mistypes a term that occurs only in a note they may not read
- **THEN** no correction, completion or "did you mean" is offered for it, and the output is
  identical to the run in which that note does not exist

#### Scenario: The query matches only the unreadable note
- **WHEN** the only note matching the query is one the searcher may not read
- **THEN** the output is byte-identical to a search over a corpus in which that note does not
  exist, including the count, the facets and the words of the empty-result message

#### Scenario: The unreadable note is restricted in each of the three ways
- **WHEN** the withheld note is restricted to its author alone, to a group the searcher is not in,
  to named people not including the searcher, or is authored by a deactivated colleague
- **THEN** the preceding scenarios hold unchanged in every case

### Requirement: A search can be scoped to a person, a group, or the company
Search SHALL accept a scope naming one author, one group defined by the hub, or the whole company,
and SHALL return only notes within that scope that the searcher may read.

#### Scenario: A person-scoped search
- **WHEN** two colleagues each publish a company-wide note containing the same term and the search
  names one of them
- **THEN** exactly that colleague's note is returned

#### Scenario: A group-scoped search
- **WHEN** one note is published to group A and one to group B with the same term, and the search
  names group A
- **THEN** the group A note is returned and the group B note is not

#### Scenario: A scope the hub does not know
- **WHEN** the search names a person or a group the hub has no record of
- **THEN** the search is refused as an unknown scope with its own machine-readable code and a
  non-zero exit, distinguishable from a valid scope that matched nothing

### Requirement: Search reflects the latest version of a note
Search SHALL match against the current version of a note, and a term removed by an amendment SHALL
NOT return that note as a current result.

#### Scenario: A term replaced by a later version
- **WHEN** a note containing term X is amended to contain term Y instead
- **THEN** a search for Y returns the note and names version 2, and a search for X returns nothing

### Requirement: Searching requires the read scope and is refused, not narrowed
Search SHALL require the `read` capability, and a request the searcher's grant does not cover SHALL
be refused when it is made rather than silently narrowed to what the grant permits.

#### Scenario: A token carrying only write or only publish
- **WHEN** a search is issued under a grant that does not carry `read`
- **THEN** it is refused with its own code, it returns no results at all, and the refusal names the
  capabilities the grant does hold

#### Scenario: A search issued as somebody else
- **WHEN** a search asks to run as a person the grant does not act as
- **THEN** it is refused, and it is not re-run as the grant's own holder

#### Scenario: Holding read is not permission to see
- **WHEN** a grant carrying `read` searches a corpus containing notes its holder may not read
- **THEN** only the notes its holder may read are returned

### Requirement: A search that could not run is never a search that found nothing
A search that ran and matched nothing SHALL report that it found nothing and exit zero. A search
that could not run SHALL say why, on some stream, and exit non-zero, and SHALL NOT render as an
empty result set.

#### Scenario: Each failure mode is its own fact
- **WHEN** the hub is unreachable, the daemon is not running, no hub is configured, the person is
  not signed in, or their token carries no `read` scope
- **THEN** each produces output carrying its own machine-readable code, none produces silence, none
  exits zero, and no two produce both the same output and the same exit status

#### Scenario: No hub is configured
- **WHEN** a search is issued on a machine with no hub configured
- **THEN** the output names the missing hub, states that no connection was attempted and that
  nothing has been established about the corpus, and no attempt is made to reach a hub

#### Scenario: Nothing is started implicitly
- **WHEN** a search is issued with no daemon running
- **THEN** the command says the daemon is not running, and no daemon exists afterwards

### Requirement: An undetermined readability is neither included nor silent
Where search cannot determine whether the searcher may read a note, or whether a named scope
exists, that state SHALL be reported as undetermined, SHALL NOT be rendered as "no results", and
SHALL NOT be treated as readable.

#### Scenario: A note whose readability cannot be worked out
- **WHEN** a note is narrowed to a group and the group is later dissolved, so nobody can say who was
  in it
- **THEN** the note is not returned, its text does not reach the output, it is counted separately
  from the results, and the whole search reports as incomplete and exits with the undetermined
  status

#### Scenario: An incomplete result of the same size as a complete one
- **WHEN** a complete search and a partially-covered search both return the same number of results
- **THEN** their output differs and the incomplete one is never presented as a complete answer

#### Scenario: The corpus size an agent grounds itself on
- **WHEN** the settled corpus is asked how large it is, over a store holding one readable note, one
  the searcher may not read, and one whose readability could not be determined
- **THEN** it answers one, so that a count published as a corpus statistic can never disclose the
  existence of a note the searcher may not see

#### Scenario: A scope whose existence cannot be checked
- **WHEN** the hub's record cannot be read to decide whether a named person or group exists
- **THEN** the answer is undetermined and does not collapse into "there is no such person or group"

### Requirement: A permitted note is reachable by search
For any note the searcher is permitted to read, there SHALL exist a search over that corpus that
returns it.

#### Scenario: A note is reachable under every scope it belongs to
- **WHEN** a note is published to a group the searcher belongs to and a distinctive term from its
  body is searched under the company, person and group scopes
- **THEN** the note is returned under each of them

#### Scenario: A deactivated colleague's notes are archived, not deleted
- **WHEN** a colleague is deactivated and a search is scoped to them
- **THEN** their notes remain findable exactly as their visibility allows, and the output says the
  colleague has been deactivated so that a thin result set is not read as a broken search
