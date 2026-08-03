# machinery

## ADDED Requirements

### Requirement: A landed refusal is retired only by the reviewer that made it
The review gate SHALL consider every verdict posted for the current head, not only the most recent
one, and SHALL refuse while any reviewer's most recent verdict for that head requests changes. A
verdict posted by one reviewer SHALL NOT retire a refusal made by another, whoever posted it and
whenever. Widening who may certify SHALL NOT widen what counts as certified.

The refusal SHALL name the reviewers whose objections are outstanding.

A verdict is bound to the head it names, so a push SHALL leave every earlier verdict inapplicable and
a refused branch SHALL always be clearable by changing the code.

#### Scenario: An author self-approves over an independent refusal
- **WHEN** an independent reviewer requests changes on a head and an agent that authored commits in
  the pull request then approves the same head under a policy permitting self-review
- **THEN** the gate refuses with the changes-requested outcome, and does not report that no
  independent agent has looked

#### Scenario: A second independent reviewer approves over the first one's refusal
- **WHEN** one independent reviewer requests changes on a head and a different independent reviewer
  then approves the same head
- **THEN** the gate refuses, because the reviewer that objected has not withdrawn

#### Scenario: A reviewer withdraws its own refusal
- **WHEN** a reviewer requests changes on a head and later approves the same head
- **THEN** the gate acts on that reviewer's later verdict, and the refusal no longer stands

#### Scenario: A self-approve with nothing before it
- **WHEN** an agent that authored commits in the pull request approves the head, no other verdict for
  that head exists, and the policy permits a self-review
- **THEN** the gate passes and says the review was a self-review rather than an independent one

#### Scenario: The code is changed in answer to a refusal
- **WHEN** a head carrying a refusal is superseded by a new head and that new head is approved
- **THEN** the gate passes, because a verdict applies only to the head it names

#### Scenario: A verdict for the head cannot be attributed
- **WHEN** any verdict naming the current head cannot be attributed to a poster, whether or not a
  later verdict for the same head can be
- **THEN** the gate refuses rather than passing over it to reach the later verdict
