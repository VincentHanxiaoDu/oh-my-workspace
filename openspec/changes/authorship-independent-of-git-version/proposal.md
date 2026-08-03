# Who built a pull request is one answer, on every machine and after every upgrade

## Why

`.workflow/bin/pr-authors.sh` exists to end one failure: the routing and the review gate deriving
authorship separately and disagreeing, so a reviewer is cleared to review work the gate will then
refuse it for. One derivation, one answer, three callers.

It reintroduced that failure through itself. Authorship turns on a predicate — a commit that changes
nothing outside `openspec/` confers none — and the file list feeding that predicate came from
`git show --name-only --format=""`. That is **porcelain**: output formatted for people, whose shape
is not a stable interface. Some git versions prefix it with a blank line and some do not. A blank
line does not match `^openspec/`, so the predicate concluded the archive commit had touched
something else, and its author became an author — **on the runner only**. Observed on
`dev/feat/7-ticket-merge`:

    git 2.50.1 (a laptop)  ->  dev
    git 2.54.0 (the runner)->  dev, product

A reviewer was cleared locally, did the review, and had the gate refuse the verdict it had just
posted. Demonstrated here by driving the pre-fix script under a git whose `show --name-only` output
carries a leading blank line: it answers `dev` without and `dev, product` with, and the shipped
script answers `dev` under both.

**A second, unrelated defect lives in the same script and is not this one.** It reproduces with the
predicate working perfectly and on a single git version. A pull request whose commits are all under
`openspec/` correctly has every author stripped, and the review gate read that empty set as "no
commit carries an `Agent:` trailer" — about commits that plainly carry one. **An empty author set
has two meanings**: *who built this cannot be determined*, and *it is determined that nobody
authored product judgement here, so every role is independent*. Collapsing them made #63 unmergeable
by any reviewer except with `--admin`. That is this project's own rule — `could not be determined` is
not the same value as `determined to be nothing` — broken inside the gate that enforces it.

Both are fixed, upstream, in the refresh already on `main`. **That is exactly the problem this change
addresses.** `.workflow/bin/` is framework-owned and is replaced wholesale by the next `install.sh`
run; a fix living only there has a half-life of one install, which is how the #52 fix was deleted
(#58). Nothing in this repository currently notices if a refresh takes either fix back out.

## What Changes

- **A project-owned regression test in `internal/machinery/`** that EXECUTES the installed
  `.workflow/bin/pr-authors.sh` rather than restating it, so it cannot go green over a broken script.
  It runs under `make ci`, which the `Build and tests` gate invokes, so a refresh that reintroduces
  either defect turns this repository red instead of quietly restoring it.
- **The shape of git's output is supplied, not borrowed.** Every fixture in the script's own
  self-test is built by the same git whose output shape is the variable, so it agrees with itself on
  every machine — which is why the outage was invisible. The test puts a stand-in `git` ahead of the
  real one on `PATH` that perturbs only the porcelain command, leaves plumbing untouched, and demands
  the same answer from both.
- **The predicate is driven against literal file lists**, so shapes this machine's git does not
  produce are still asserted.
- **The archive-only pull request is asserted end to end**, through the real `check-review.sh`: an
  independent reviewer's approval is accepted, while a branch whose commits carry no trailer at all
  is still refused, and the refusal still names the trailer.
- **The test probes its environment and never names it.** It asks `git --version` and
  `git rev-parse --is-shallow-repository`, and when it cannot get an answer it **skips saying it
  determined nothing and is not passing** — never a silent pass. CI checks out at depth 1, so
  assuming history is present is the same bug class.

No change is made to `.workflow/bin/pr-authors.sh` here. The fix already landed there upstream; the
real fix belongs upstream in agent-dev-flow, and this change is what makes its removal visible.

## Impact

- Added: `internal/machinery/prauthors_test.go`, and a `machinery` capability requirement covering
  authorship.
- Unchanged: `.workflow/bin/`, `.github/`, and every product package. Nothing declares a local
  commit over a framework-owned path, so `TestNoUndeclaredLocalCommitsOnFrameworkOwnedPaths` has
  nothing new to name.
- `make ci` grows roughly fifteen seconds of shell-driven git fixtures.
