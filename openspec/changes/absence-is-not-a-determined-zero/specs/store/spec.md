# store

## ADDED Requirements

### Requirement: A record kind read by one package and written by none fails the build
There SHALL be a check over the product's own source that reports every store record kind which some
package reads and no package writes, and the build SHALL fail on any such kind that has not been
declared.

A storage location nobody writes to is readable and yields no records, so a reader of such a kind
does not fail, return nothing, or raise anything: it renders a confident zero. Case-by-case tests
cannot find this, because each half is correct on its own and no test compares the two halves.

Removing a record from a kind SHALL NOT count as writing it, so that a kind nothing produces cannot
be excused by a cleanup path.

A read whose kind the check cannot resolve SHALL be reported as unresolved rather than treated as
checked, because a check that quietly stopped looking is the failure it exists to catch.

A declared exception SHALL name where its decision lives, SHALL remain visible as a finding, and
SHALL fail the build once it no longer corresponds to a real finding.

#### Scenario: A kind is read and never written
- **WHEN** the check runs over source in which a record kind is read and no package writes it
- **THEN** the check reports that kind, names where it is read, and the build fails unless the kind
  has been declared

#### Scenario: A kind is read and written
- **WHEN** the check runs over source in which a record kind is both read and written
- **THEN** the check reports nothing for that kind

#### Scenario: A kind that can only be deleted
- **WHEN** the check runs over source in which a record kind is read and deleted and never written
- **THEN** the check reports that kind

#### Scenario: The check examined nothing
- **WHEN** the check runs over a tree containing no source
- **THEN** it reports an error rather than success, because a negative conclusion over an empty set
  establishes nothing
