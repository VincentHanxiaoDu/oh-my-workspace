# machinery Specification

## Purpose
Whoever opens a pull request here can rely on the question "who wrote this?" having one answer —
the same one on a laptop, on a colleague's machine, and on the runner that decides whether the work
may merge. A review that was independent stays independent when the runner's git is upgraded, and a
red gate always means something a person did, never something the machine's version happened to be.

When nobody can be named as an author, the machinery says which kind of nobody it means: a branch
that only moves specs genuinely has no product judgement to be independent of, and a branch whose
commits forgot to say who wrote them is a defect the author is told to fix, in those words, at the
gate that can explain it. Neither is ever silently reported as the other.

## Requirements
### Requirement: Authorship does not depend on which git was asked
The derivation of a pull request's authors SHALL give the same answer regardless of the version or
output formatting of the git that runs it. It SHALL read file lists from git's plumbing rather than
from output formatted for people, and SHALL NOT treat a blank or whitespace-only line as a changed
path.

#### Scenario: A branch of one code commit and one archive commit
- **WHEN** the derivation is run over a range holding a commit by `dev` that changes `internal/` and
  a commit by `product` that changes nothing outside `openspec/`
- **THEN** the authors are exactly `dev`, and `product` is not among them

#### Scenario: The same branch under a git that formats its output differently
- **WHEN** that same derivation is run against a git whose `show --name-only` output carries leading
  and trailing blank lines, as the runner's does and a laptop's does not
- **THEN** the authors are exactly `dev`, identical to the answer on the other git

#### Scenario: A blank line is offered as a changed path
- **WHEN** the spec-only predicate is given a file list whose lines are blank or whitespace apart
  from paths under `openspec/`
- **THEN** it answers that the commit is spec-only and confers no authorship

#### Scenario: A commit named like an archive that carries code
- **WHEN** the predicate is given a file list holding both a path under `openspec/` and a path
  outside it
- **THEN** it answers that the commit is not spec-only, whatever its subject line says

### Requirement: An undetermined author set is never reported as an empty one
A caller SHALL be able to tell "no commit carries an `Agent:` trailer" from "every commit was
spec-only, so nobody authored product judgement here". The first is a defect in the commits; the
second is a determined answer under which every role is independent.

#### Scenario: A pull request whose every commit is an archive
- **WHEN** a branch holds only commits that change nothing outside `openspec/`, each carrying an
  `Agent:` trailer
- **THEN** the author set is empty, the trailers are still reported when asked for, and the review
  gate accepts an approval from any reviewer

#### Scenario: A pull request whose commits carry no trailer
- **WHEN** a branch holds commits with no `Agent:` trailer at all
- **THEN** no trailer is reported, the review gate refuses, and the refusal names the missing
  `Agent:` trailer rather than describing the review

### Requirement: The check executes the installed machinery and probes its environment
The assertion SHALL run the installed `.workflow/bin/` scripts rather than reimplement them, and
SHALL live on a project-owned path so that reinstalling the framework cannot remove it. It SHALL
determine what it needs from its environment by asking, and SHALL skip with a stated reason rather
than pass when it cannot get an answer.

#### Scenario: The framework is refreshed and reintroduces the defect
- **WHEN** an install replaces `.workflow/bin/pr-authors.sh` with a version that derives file lists
  from porcelain, or that loses the distinction between the two empty author sets
- **THEN** `make ci` fails in this repository, naming the defect and the remedy

#### Scenario: The environment cannot answer
- **WHEN** git is absent, or `git --version` or `git rev-parse --is-shallow-repository` cannot be
  answered
- **THEN** the check skips, stating that it determined nothing and is not passing, and reports which
  question went unanswered

#### Scenario: The checkout is shallow
- **WHEN** the check runs in a repository checked out at depth 1
- **THEN** it reports that the checkout is shallow, and either skips or asserts only over history it
  created itself — it never assumes this repository's history is present

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
