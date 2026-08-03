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

