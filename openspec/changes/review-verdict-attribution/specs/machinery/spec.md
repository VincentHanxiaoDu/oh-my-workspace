# machinery

## ADDED Requirements

### Requirement: A verdict is attributed to the agent that posted it, never to a name in its text
The review gate SHALL derive the reviewer's identity from the comment that carries the verdict, and
SHALL NOT accept a `Reviewed-by:` line as evidence of who reviewed. A verdict whose declared reviewer
disagrees with its poster SHALL be refused and SHALL say that the two disagree; it SHALL NOT be
silently re-attributed to either of them, and its verdict SHALL NOT be acted on.

A verdict whose poster cannot be determined SHALL be refused as undetermined, SHALL say that this is
an inability to attribute the review rather than a finding that no review exists, and SHALL NOT fall
back to the name the verdict declares about itself.

This requirement bounds what it establishes. Where every role posts through one account, the poster's
role is derived from a marker the roles write themselves, which is a convention and not an
authenticated fact. It SHALL close the case where text that is not a verdict is counted as one and
the case where a verdict names an agent other than its poster; it does not make a verdict unforgeable
by an agent that sets out to forge one.

#### Scenario: A verdict names a role other than the one that posted it
- **WHEN** a comment posted by one role carries a verdict block declaring a different role as
  `Reviewed-by:`
- **THEN** the gate refuses, says the poster and the declared reviewer disagree, and does not
  certify the head for either of them

#### Scenario: An author certifies its own work under another role's name
- **WHEN** a role that authored a commit in the pull request posts a verdict declaring a role that
  authored none of them
- **THEN** the gate refuses, rather than reading the declared name and finding it independent

#### Scenario: A verdict carries no marker saying who posted it
- **WHEN** a comment carries a verdict block for the head but nothing establishing which role posted
  it
- **THEN** the gate refuses, says who posted it could not be determined, and distinguishes that from
  a head for which no review exists

### Requirement: Quoted text is not a verdict
The review gate SHALL discard fenced and quoted text from a comment before searching it for a
verdict, so that a comment which quotes a verdict — to request one, to give an example, or to
discuss one — is not counted as giving one. Discarding SHALL apply to the quoted text alone: a
genuine verdict that also quotes command output SHALL still be accepted.

#### Scenario: A comment quotes the verdict template to ask for a verdict
- **WHEN** a comment's only `Reviewed-by:`, `Reviewed-sha:` and `Verdict:` lines sit inside a fenced
  code block
- **THEN** the gate finds no review for that head, whatever role the quoted block names and wherever
  in the comment the block sits

#### Scenario: A genuine verdict quotes what the reviewer ran
- **WHEN** a comment carries a verdict block for the head and also a fenced block of command output
- **THEN** the gate accepts the verdict

#### Scenario: A quotation is posted after a genuine verdict
- **WHEN** a genuine verdict for the head is posted and a comment quoting a verdict for the same head
  is posted afterwards
- **THEN** the gate acts on the genuine verdict, and the order in which the two were posted does not
  change the outcome
