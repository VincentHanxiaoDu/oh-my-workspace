# search Specification

## Purpose

The product exists to keep one list at three. When somebody's AI hits something it does not know and
searches the hub, a miss must mean exactly one of: nobody knows how to search, it was not written up
clearly enough, or it was deliberately kept private. **A fourth answer — *it is in there somewhere and
nobody can find it* — is the failure this capability exists to prevent** (PRD §1).

So search is scoped the way people actually think about a corpus: to **a person**, to **a group**, or
**company-wide**. Naming a scope that does not exist is reported as an unknown scope, never as an
empty result — those are different facts and they send a person to different places.

**Visibility is a precondition of ranking, not a filter applied after it** (PRD §3.5). What a searcher
may see is settled before anything is ordered, counted or summarised. The consequence is stated as a
negative, because that is where it is testable: **the presence of a note the searcher cannot read
changes nothing they can observe.** Not the result count, not a snippet, not a suggestion, not the
ordering, not a facet or author list. A corpus containing it and a corpus without it are
indistinguishable to that searcher.

And failure is never silence. Every way a search can fail to run — no hub configured, the hub
unreachable, not signed in, a token without the `read` scope, or a corpus only partly reachable — says
so and is distinguishable from **a search that ran and found nothing**. An empty result is a real
answer about the corpus; the others are the absence of an answer, and a product that renders them the
same has told the person something untrue about what is written down.
## Requirements
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

### Requirement: Corpus statistics report what exists, how much, and how recent
The product SHALL report, for a requested scope, which subjects have material, how many notes there
are, and how recent the most recent one is, each independently readable and each independently
capable of being undetermined without the others being dragged down with it.

#### Scenario: The three statistics are reported together
- **WHEN** an agent requests corpus statistics for a scope
- **THEN** the response carries a subjects figure, a count and a recency figure, each labelled, and
  each either a determined value or an explicit undetermined marker

#### Scenario: Some determined and some not, in one response
- **WHEN** one statistic can be computed and another cannot for the same request
- **THEN** both are returned in the same response, each labelled with what it is, and the request
  neither fails whole nor succeeds whole

### Requirement: Every statistic is computed over exactly what the requester may read
Counts, subjects and recency SHALL be derived from the visibility-settled corpus for the requesting
identity, and SHALL NOT be derived from any wider set of notes.

#### Scenario: The count is the readable subset
- **WHEN** the corpus contains notes the requester may not read
- **THEN** the returned count equals the count of the readable subset — not the total, and not the
  total with a redaction note

#### Scenario: Two identities differ in what they may see
- **WHEN** the same statistics are requested for the same scope by two identities whose readable
  sets differ
- **THEN** the numbers returned to each reflect only their own readable set

#### Scenario: Recency is never drawn from an unreadable note
- **WHEN** the most recently written note in the whole corpus is one the requester may not read
- **THEN** the reported recency is that of the most recent note the requester MAY read

### Requirement: Statistics never surface the existence of unreadable material
For two identities differing only in what they may see, the statistics returned to the narrower
identity SHALL be consistent with a corpus in which the unreadable notes do not exist.

#### Scenario: An unreadable note changes nothing
- **WHEN** the same request is made over a corpus of readable notes, and again over that corpus plus
  a note the requester may not read
- **THEN** the two responses are identical in every statistic, with no total, no count of hidden
  material, and no error that fires only when unreadable material is present

#### Scenario: A scope with nothing visible in it
- **WHEN** statistics are requested for a scope in which every note is unreadable to the requester
- **THEN** the response is indistinguishable from statistics for a scope that is genuinely empty of
  readable material

#### Scenario: A statistic is not a search result
- **WHEN** a statistic reports that material exists in a scope
- **THEN** it carries no note title, identifier or excerpt that the requester could not obtain by
  searching that same scope as themselves

### Requirement: A statistic that could not be computed is undetermined, never zero
A statistic that could not be computed SHALL render distinguishably from a statistic computed as
zero, by inspecting the output alone, with no reference to logs, exit code, or a second command, and
SHALL be present in the output rather than omitted.

#### Scenario: Zero and undetermined do not share a rendering
- **WHEN** one statistic is determined to be zero and another could not be computed
- **THEN** the two render differently in every rendering the product offers, and the undetermined
  one carries an explicit undetermined marker and a stable reason code

#### Scenario: Undetermined is not silence
- **WHEN** a statistic could not be computed
- **THEN** the field is present in the response carrying its undetermined marker; it is not omitted,
  not absent-in-a-way-that-parses-as-nothing, and does not suppress the rest of the response

#### Scenario: An unevaluable note is not counted as absent
- **WHEN** the corpus contains a note, WITHIN THE REQUESTED SCOPE, whose readability could not be
  determined at all
- **THEN** it is neither counted among the notes the requester may read nor silently dropped: it is
  reported separately and the affected statistics are undetermined

#### Scenario: The separate report of unevaluable notes is itself scoped
- **WHEN** the only notes whose readability could not be determined lie outside the requested scope
- **THEN** the count of unevaluable notes for that scope is a determined zero, and the response is
  indistinguishable from one over a scope in which no such note exists anywhere

### Requirement: Statistics are computed for a named identity, and a request naming none is refused
Corpus statistics SHALL be computed over one identity's readable set, and a request that names no
identity SHALL be refused before any statistic is computed.

#### Scenario: A request that names nobody
- **WHEN** corpus statistics are requested without an identity, whether directly or through a grant
  whose holder is unset
- **THEN** the request is refused with the not-signed-in code, and no statistic — including any
  count of notes that could not be evaluated — is returned

#### Scenario: The refusal discloses nothing about corpus size
- **WHEN** the same identity-less request is made against two corpora of different sizes
- **THEN** the two responses are identical, so that nothing about how much exists is learned by
  asking as nobody

### Requirement: Statistics are requestable at each of the three search scopes and no other
Statistics SHALL be requestable at person, group and company scope, and a scope that is not one of
these three SHALL be refused rather than silently widened or narrowed to one that is.

#### Scenario: Each of the three scopes
- **WHEN** statistics are requested at person, group and company scope over the same corpus
- **THEN** each returns statistics over that scope's readable material

#### Scenario: A scope that is not one of the three
- **WHEN** the request names a scope of another kind, or a person or group the hub has no record of
- **THEN** the request is refused with its own machine-readable code and a non-zero exit, and no
  statistics for any other scope are returned in its place

#### Scenario: Statistics add no capability scope
- **WHEN** the capability vocabulary is inspected after this capability exists
- **THEN** it is exactly read, write and publish, and reading statistics requires read and nothing
  else

### Requirement: Recency is defined against the latest version and does not vary by scope
Recency SHALL be the timestamp of the latest version of the most recently written readable note in
scope, that definition SHALL be stated in the output, and it SHALL be the same definition at every
scope.

#### Scenario: An amendment moves recency
- **WHEN** the oldest note in the corpus is amended, making its latest version the most recent
  writing in the corpus
- **THEN** the reported recency is that amendment's timestamp, at every scope that contains the note

#### Scenario: No readable note in scope
- **WHEN** there is no note in scope that the requester may read
- **THEN** recency is a determined "none", rendered distinguishably from undetermined recency

### Requirement: Statistics start no daemon and open no network without a hub
Requesting statistics SHALL NOT start the daemon and SHALL NOT open a network connection when no hub
is configured.

#### Scenario: No daemon running
- **WHEN** statistics are requested with no daemon running
- **THEN** no daemon is started, the response reports that the daemon is not running with its own
  reason code, and that is distinguishable from a run in which the daemon was running and a
  statistic was undetermined for another reason

#### Scenario: No hub configured
- **WHEN** statistics are requested on a machine with no hub configured
- **THEN** no connection is attempted, local statistics that can be computed locally are returned as
  determined values, and hub statistics are returned as undetermined with the reason being that no
  hub is configured — not as zero and not as an unexplained failure

#### Scenario: A hub that is configured but unreachable
- **WHEN** statistics are requested with a hub configured that cannot be reached
- **THEN** hub statistics are undetermined and the response distinguishes "could not reach the hub"
  from "the hub reports nothing readable here"

### Requirement: The CLI and the agent API report the same statistics
For the same scope and the same identity, the statistics reported through the CLI and through the
agent API SHALL be the same, including which statistics are undetermined.

#### Scenario: The two surfaces agree
- **WHEN** the same request is made through the CLI and through the agent API, for each way a
  statistic can come back undetermined
- **THEN** both report the same value or the same undetermined marker for every statistic, the same
  reason codes, and the same exit status

### Requirement: A statistics criterion that cannot be driven is recorded as undriven, never as met
A criterion that needs a surface this build does not have SHALL NOT be reported as satisfied on the
strength of shared code. Identity that holds because there is only one implementation is
construction, not observation, and the two SHALL NOT share a value.

The suite SHALL carry each such criterion as an executable test which PROBES the product's own seam
for the capability it needs — never a build tag, a version, or a named machine — and which, when the
capability is absent, reports that it determined nothing and did not pass. It SHALL NOT report a
skip in a way that a reader or a gate can take for a pass.

When the capability appears, the test SHALL begin asserting without an edit; and where the criterion
cannot be written in advance against a surface that does not yet exist, the test SHALL FAIL once that
surface appears, so that an undriven criterion cannot survive the arrival of the means to drive it.

#### Scenario: The build serves statistics from one surface
- **WHEN** exactly one surface of the build serves the corpus statistics capability
- **THEN** the criterion requiring two surfaces to return identical statistics reports that it
  determined nothing and did not pass, and names the surface it found and the work it is waiting on

#### Scenario: A second surface appears while the comparison is unwritten
- **WHEN** a second surface of the build serves the corpus statistics capability and no test drives
  the two against each other
- **THEN** the suite fails, naming both surfaces and what must now be driven

#### Scenario: No client-to-hub transport exists
- **WHEN** the one seam through which a statistics request reaches a hub cannot return a hub
- **THEN** the criteria requiring a hub that ANSWERS report that they determined nothing and did not
  pass, name the reason the seam gave, and state that a rendering distinction driven against an
  injected store is not the criterion

#### Scenario: A transport exists
- **WHEN** the one seam through which a statistics request reaches a hub returns a hub
- **THEN** the criteria requiring a hub that answers are driven through the command rather than
  skipped, and a hub that answers with nothing readable renders a determined zero that does not print
  the same as a hub that could not be reached

### Requirement: A statistic that must not move is driven against a control that must
An assertion that publishing material the reader cannot see left their statistics unchanged SHALL be
driven together with a control in the same test: material the reader CAN see, published through the
same path, which must move both the count and the recency.

Without the control the assertion is satisfied by a build that publishes nothing, by one whose
statistics never move, and by one whose hub half is undetermined throughout — so it establishes
nothing on its own. A control that does not move SHALL fail the test naming the control, not the
probe.

#### Scenario: The control does not move
- **WHEN** a note the reader may read is published and their statistics are byte-identical afterwards
- **THEN** the test fails, saying that the control did not move and that the probe beside it therefore
  establishes nothing

