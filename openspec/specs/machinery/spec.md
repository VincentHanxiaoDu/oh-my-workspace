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

### Requirement: A budget guard SHALL NOT report a healthy budget while calls are being refused
The API budget guard SHALL treat a refusal carrying a rate-limit body as evidence that calls are
being refused, and SHALL stand the watches down on it. It SHALL NOT answer with a counter that is
true about a different limit: the primary hourly quota reads nearly full throughout a secondary
(burst) throttle, and reporting it is answering a question nobody asked.

Where the secondary state cannot be determined without spending a call, the guard SHALL say that it
could not be determined rather than presenting the primary counter as though it settled the matter.
`Could not determine` and `determined to be nothing` SHALL NOT share a rendering.

#### Scenario: A secondary rate limit is in force and the primary quota reads healthy
- **WHEN** a call has been refused with a body naming a rate limit, and the primary quota reports
  nearly its full allowance remaining
- **THEN** the guard stands the caller down rather than reporting budget, names the throttle as a
  secondary burst limit rather than as primary exhaustion, and says when it will retry

#### Scenario: The primary quota is healthy and no refusal has been seen
- **WHEN** the primary quota is above the reserve and no call has been refused
- **THEN** the guard permits polling and states that its answer is about the primary quota only and
  that the secondary limit could not be determined, rather than reporting a bare number

#### Scenario: The primary quota is exhausted
- **WHEN** the primary quota is at or below the reserve reserved for the role's own work
- **THEN** the guard stands the caller down and says when the quota resets

#### Scenario: The rate limit cannot be read at all
- **WHEN** the query for the rate limit fails
- **THEN** the guard answers with neither "budget" nor "exhausted", and names itself a lookup failure

### Requirement: A watch SHALL distinguish a throttle from an outage from a quiet board
A watch whose poll is refused by a rate limit SHALL report that it is holding, and SHALL NOT report
it as a failed lookup or poll again on its ordinary interval — a secondary limit clears with quiet,
so retrying extends it. A poll that failed for any other reason SHALL remain a reported lookup
failure. Neither SHALL be rendered as a board with nothing on it.

A hold SHALL NOT end the watch: the throttle is transient, and a watch that stops is
indistinguishable from a quiet queue.

#### Scenario: The poll is refused by a secondary rate limit
- **WHEN** a watch's poll is refused with a body naming a secondary rate limit
- **THEN** the watch reports that it is holding, names the burst throttle, says when it will retry,
  and resumes polling afterwards rather than exiting

#### Scenario: The poll fails for a reason quiet does not fix
- **WHEN** a watch's poll fails with a network error such as a connection timeout
- **THEN** the watch reports a lookup failure and does not report that it is holding

### Requirement: A stand-down SHALL wait on the clock belonging to its cause
The wait after a hold SHALL be derived from the limit that caused it. A secondary limit clears with
quiet and SHALL be waited out on its own cooldown, or on the `Retry-After` the refusal carried when
it carried one. A primary exhaustion SHALL be waited out on the quota reset.

#### Scenario: The refusal carried a Retry-After
- **WHEN** a refusal names the number of seconds to wait
- **THEN** the stand-down uses that number rather than a default cooldown

#### Scenario: A secondary limit is live and the primary quota resets much later
- **WHEN** a secondary limit is in force and the primary quota's reset is far in the future
- **THEN** the stand-down is the secondary limit's cooldown and not the primary reset interval

### Requirement: A mergeable signal SHALL rest on positive evidence
A watch SHALL announce a pull request as ready to merge only on evidence that the gates have spoken
and passed: at least one completed check run, no check run still running, and a review verdict
positively read as successful. The absence of a failure, of a pending run, and of a refusal SHALL NOT
together constitute a pass — on a head where nothing has reported, every one of those tests is
vacuously true.

A verdict that could not be read SHALL NOT be treated as a verdict that is absent, and neither SHALL
reach the ready signal.

#### Scenario: Nothing has reported on the head
- **WHEN** a pull request's head has no check runs at all, no commit statuses and no reviews
- **THEN** the watch does not announce it as ready

#### Scenario: The gates have passed and no verdict has been published
- **WHEN** every check run on the head has completed successfully and no review verdict has been
  published
- **THEN** the watch does not announce it as ready

#### Scenario: A check run is still running
- **WHEN** at least one check run on the head has not completed
- **THEN** the watch does not announce it as ready

#### Scenario: The verdict cannot be read
- **WHEN** the query for the head's commit statuses fails
- **THEN** the watch reports the failed lookup and does not announce the pull request as ready

#### Scenario: The evidence is there
- **WHEN** a check run has completed successfully, none is still running, and the review verdict is
  successful
- **THEN** the watch announces the pull request as ready

### Requirement: A head with no answer yet SHALL have its own event
A watch SHALL emit a distinct event for a pull request whose head has not been answered, saying that
nothing has reported and that this is not a pass. It SHALL NOT convey that state by emitting nothing:
silence would then mean both "nothing to report" and "no answer yet", which is the collapse the
watch's other events exist to prevent.

This SHALL hold on every entry point that evaluates a pull request's state, not only on the one-pass
sweep.

#### Scenario: Nothing has reported, on the continuous watch
- **WHEN** a pull request's head has nothing reported on it and the watch is running continuously
- **THEN** the watch emits its no-answer event for that pull request

#### Scenario: Nothing has reported, on a single pass
- **WHEN** a pull request's head has nothing reported on it and a single pass over the board is
  requested
- **THEN** that pass emits the no-answer event for that pull request

### Requirement: A single pass SHALL terminate whatever it finds
A one-pass sweep SHALL end, and SHALL exit non-zero when it could not answer, including when it
stood down for a rate limit. It is the fallback used when the continuous watch has died, so a sweep
that waits and retries hangs exactly like the watch it replaces.

#### Scenario: The sweep stands down for a budget limit
- **WHEN** a single pass is requested and the API budget guard reports that the caller should stand
  down
- **THEN** the pass ends rather than sleeping and retrying, and exits non-zero

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

### Requirement: A failed lookup SHALL show the part of its output that says what failed
A watch that reports a failed lookup SHALL show the END of the captured output when that output is
longer than the space available for it, because the captured stream is everything the command
printed before it died and the failure is its last line. Output that fits SHALL be shown whole and
unaltered.

Where output was elided, the rendering SHALL say so, so that an abbreviated reason does not read as
a complete one.

#### Scenario: The command answered at length and then failed
- **WHEN** a watch's lookup prints more normal output than the reason budget allows and then fails
  with an error on its last line
- **THEN** the reported reason contains that error

#### Scenario: The whole output fits
- **WHEN** a watch's lookup fails with an output shorter than the reason budget
- **THEN** the reported reason is that output unchanged, with nothing trimmed and nothing added

### Requirement: A red default branch SHALL name the failing check and SHALL NOT assert an unmeasured cause
A watch reporting that the default branch is red SHALL name the check that is failing. It SHALL NOT
attribute the failure to whoever merged last: the default branch can be reddened by commits nobody
here merged, including direct pushes by the framework, so who merged last is a proxy for authorship
that stops measuring it exactly when it matters.

Where the failure IS attributable to the caller's own merge, derived by comparing the failing commit
with the merge commit that caller produced, the watch MAY say so. Where it cannot be derived, the
watch SHALL say the cause was not determined rather than guess.

A check name that could not be read SHALL NOT be rendered as a red run with nothing failing in it.

#### Scenario: The default branch is red on a commit no merge here produced
- **WHEN** the default branch's failing commit is a direct push rather than the caller's merge
- **THEN** the event names the failing check, says the cause was not determined, and does not tell
  the caller the failure is theirs to fix

#### Scenario: The failing commit is the caller's own merge commit
- **WHEN** the default branch's failing commit is the merge commit the caller produced
- **THEN** the event may say the failure is theirs to fix, on the strength of that comparison

#### Scenario: The failing check cannot be read
- **WHEN** the default branch is red and the query naming which check failed cannot be answered
- **THEN** the event still reports the branch as red and says the failing check could not be
  determined

