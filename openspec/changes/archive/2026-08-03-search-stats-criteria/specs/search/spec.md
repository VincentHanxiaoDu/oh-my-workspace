# search

## ADDED Requirements

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
