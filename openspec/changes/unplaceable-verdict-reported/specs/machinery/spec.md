# machinery

## ADDED Requirements

### Requirement: A verdict the gate cannot place is reported, not discarded
The review gate SHALL match a verdict's declared sha against the head exactly, and SHALL distinguish
two ways a verdict can name something else. A sha naming a commit the repository knows is an ordinary
stale review and SHALL remain silent. A sha naming no object at all SHALL be reported, naming both the
sha that could not be placed and the head that was expected, and SHALL NOT allow the gate to publish a
pass.

The gate SHALL NOT report an unplaceable sha in a checkout that cannot answer the question. Where an
object may be absent because it was never fetched, the gate SHALL say the question could not be
determined rather than report that a verdict is unplaceable.

An unplaceable verdict and an absent review SHALL NOT share a published description. A landed refusal
takes precedence over an unplaceable verdict, and the unplaceable verdict SHALL still be reported.

#### Scenario: A verdict names a sha no object exists at
- **WHEN** a verdict for a pull request names a sha that is neither the head nor any object the
  repository holds, and another verdict approves the head
- **THEN** the gate does not pass, and reports the sha it could not place together with the head it
  expected

#### Scenario: A verdict names an earlier commit
- **WHEN** a verdict names a commit the repository holds that is not the head
- **THEN** the gate reports it as an ordinary stale review, naming who posted it, the sha it names
  and the current head, without changing the outcome or the exit code, and the head's own review
  decides whether the pull request passes

#### Scenario: A stale verdict and an absent review
- **WHEN** the only verdict for a pull request names a commit the repository holds that is not the
  head, and separately when no verdict exists at all
- **THEN** the two do not produce the same output, because one instructs a reviewer who has already
  looked to re-post and the other says nobody has looked

#### Scenario: A verdict names the current head
- **WHEN** a verdict names the head exactly
- **THEN** the gate reports nothing about its sha

#### Scenario: An unplaceable verdict is the only verdict
- **WHEN** the only verdict on a pull request names a sha no object exists at
- **THEN** the gate reports the unplaceable sha rather than reporting that no review exists

#### Scenario: A quoted verdict names an unknown sha
- **WHEN** a comment quotes, inside a code fence, a verdict naming a sha no object exists at
- **THEN** the gate does not report it, because a quotation is not a verdict

#### Scenario: The checkout cannot answer
- **WHEN** the repository is shallow, or its depth cannot be read, and a verdict names a sha that is
  not the head
- **THEN** the gate says whether any verdict is unplaceable could not be determined, and does not
  report any verdict as unplaceable

#### Scenario: A refusal has landed and another verdict is unplaceable
- **WHEN** a reviewer's refusal is in force on the head and a separate verdict names an unplaceable
  sha
- **THEN** the gate reports the refusal as its outcome and still reports the unplaceable verdict
