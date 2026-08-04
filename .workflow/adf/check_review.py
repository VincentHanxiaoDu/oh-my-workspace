#!/usr/bin/env python3
"""Gate 5 of 5: this head carries a review by an agent that authored none of its commits.

Two questions survive the mechanical gates and no test answers either — does this do what the Issue
asked, and did it go wider than it should. That is what a review is for and the whole of it.

THE EXIT CODES ARE FIVE FACTS, NOT A PASS AND A FAIL, and every split was paid for:

    0  an independent agent approved this head
    1  no review, or one that cannot be attributed, or a non-independent one under strict policy
    2  an outstanding changes-requested. BEATS EVERYTHING.
    3  SELF-REVIEWED, permitted by this repository's policy and never spelled like an independent one
    4  a verdict names a sha this repository does not know (#84)

**Two facts sharing a code is how a landed refusal came to be published as an absent review.** A
reviewer that had just refused a pull request read "No current review by an independent agent" and
could not tell whether its verdict had landed or its comment had never been parsed. The publishing
step picks its wording from this number, so each fact needs its own.

PRECEDENCE, STATED RATHER THAN LEFT TO THE ORDER OF LINES. 2 beats 4 beats 1 and 0:
  - 4 over 0: a pass while somebody's verdict lies unplaced is the whole of #84.
  - 4 over 1: "no review exists" is true but sends the reader hunting for a comment that is sitting
    right there naming a sha nobody can find. The unplaceable one is actionable.
  - 4 under 2: a landed refusal is concrete, already red, and already tells the author what to do.
    The unplaceable notice still prints above it, so nothing is lost.

Usage: check_review.py <head-sha> <comments-json> [base-sha]
       check_review.py --self-test
"""

from __future__ import annotations

import json
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

import authors as authors_mod
import verdicts as vmod
from gh import LookupFailure

OK = 0
NO_REVIEW = 1
CHANGES = 2
SELF_REVIEWED = 3
UNPLACEABLE = 4
USAGE = 2  # only for argv misuse, before any gate logic runs


@dataclass
class Result:
    code: int
    messages: list[str] = field(default_factory=list)
    reviewer: str = ""
    refusers: list[str] = field(default_factory=list)
    unplaceable: list[tuple[str, str]] = field(default_factory=list)  # (role, sha)

    def report(self, out=sys.stderr) -> None:
        for m in self.messages:
            print(m, file=out)


def review_policy(path: str | Path = ".workflow/review-policy") -> str:
    """`self-allowed` lets an author certify its own work. Anything else — including no file, an
    unreadable one, or a misspelling — is the STRICT rule.

    IT SITS OUTSIDE THE FRAMEWORK'S HALF deliberately: `bin/` is replaced wholesale on every
    install, and a policy a refresh silently reverts is not a policy.

    A TYPO MUST NOT WIDEN WHAT MAY MERGE. `self_allowed` is a typo somebody will make, and reading
    it permissively would relax the one gate in this process that is not mechanical.
    """
    p = Path(path)
    try:
        raw = p.read_text()
    except OSError:
        return "independent"
    return "self-allowed" if "".join(raw.split()).lower() == "self-allowed" else "independent"


def _git_knows(sha: str, cwd: str | None = None) -> bool:
    """Does this repository know an object by this name?"""
    if not sha:
        return False
    r = subprocess.run(
        ["git", "cat-file", "-e", f"{sha}^{{commit}}"],
        capture_output=True, cwd=cwd, check=False,
    )
    return r.returncode == 0


def evaluate(
    head: str,
    comments: list[dict],
    author_set: set[str],
    *,
    policy: str = "independent",
    knows_sha=_git_knows,
) -> Result:
    """The whole gate, as a pure function of what was read. No I/O, so every branch is drivable."""
    msgs: list[str] = []
    parsed = vmod.parse(comments)

    # ISSUE #84: A VERDICT THE GATE CANNOT PLACE IS REPORTED, NOT DISCARDED IN SILENCE.
    #
    # Matching `Reviewed-sha:` against the head exactly is correct and is not touched here — it is
    # what makes a verdict stale the moment somebody pushes. The defect was the silence on one
    # particular miss, and the two misses are DIFFERENT FACTS:
    #   1. the sha names a commit this repository KNOWS — an ordinary stale review. Expected on any
    #      branch that was ever pushed to; announcing them would bury the one that matters.
    #   2. the sha names NO OBJECT AT ALL — nothing was ever reviewed there, so this cannot be a
    #      stale review. Somebody posted a verdict that is not in force and nobody was told.
    #
    # Live on #38: a UAT refusal named e7e1368a7fbd… while the head was e7e1368a3673… — EIGHT
    # SHARED HEX CHARACTERS, which is not chance, and which everyone who read them side by side read
    # as equal. The gate ran 28 seconds later and published `success`.
    unplaceable = [
        (v.role, v.sha) for v in parsed
        if v.sha and v.sha != head and not knows_sha(v.sha)
    ]
    for role, sha in unplaceable:
        msgs.append(
            f"::error::a verdict by '[{role}]' names sha {sha}, which is NOT AN OBJECT IN THIS "
            f"REPOSITORY. It cannot be a stale review of an older commit, because no such commit "
            f"exists. It certifies nothing and it was nearly discarded in silence (#84)."
        )

    for_head = [v for v in parsed if v.sha == head]
    if not for_head:
        msgs.append(
            f"::error::no review found for head {head}. A push invalidates any earlier review — "
            f"this head needs its own."
        )
        msgs.append(
            "  A verdict QUOTED inside a code fence or a '>' block is not a verdict and is not "
            "counted (#65)."
        )
        code = UNPLACEABLE if unplaceable else NO_REVIEW
        return Result(code=code, messages=msgs, unplaceable=unplaceable)

    # WHO POSTED IT IS ESTABLISHED BEFORE WHAT IT SAYS IS READ, FOR EVERY BLOCK. An unattributable
    # verdict is not a weak verdict, it is not a verdict. It sweeps EVERY block rather than the
    # certifying one: a block that cannot be attributed might be a refusal, and skipping over it to
    # reach a later approve is #82 wearing a different hat.
    #
    # (`verdicts.parse` drops unmarked blocks, so an unattributable one arrives here as a verdict
    # that is simply absent from `for_head`. The disagreement case below is the one that survives
    # parsing and must be refused loudly rather than re-attributed.)
    for v in vmod.disagreements(for_head):
        msgs.append(
            f"::error::this verdict was posted by '[{v.role}]' but declares "
            f"'Reviewed-by: {v.declared}'. THE TWO DISAGREE, so it is REFUSED — not re-attributed "
            f"to either of them."
        )
        msgs.append(
            "  Its Verdict: line was not acted on at all, because a verdict whose author is in "
            "doubt is not a verdict."
        )
        return Result(code=NO_REVIEW, messages=msgs, unplaceable=unplaceable)

    # A REFUSAL IS CLEARED ONLY BY A LATER VERDICT FROM THE SAME REVIEWER (#82).
    #
    # "A reviewer changed its mind" is the only thing that should retire that reviewer's refusal,
    # and it must stay possible or a refused branch could never be cleared by the reviewer that
    # refused it. Nobody else gets a vote: not the author — which is #82 itself — and not a second
    # independent reviewer, which is the same act of overriding somebody else's judgement by posting
    # after them. THE ESCAPE IS THE PUSH: a verdict is bound to a head, so fixing the code makes a
    # new head and every verdict here stops applying. A refused branch is never trapped; it is fixed.
    latest: dict[str, vmod.Verdict] = {}
    for v in for_head:
        latest[v.role] = v
    refusers = sorted(r for r, v in latest.items() if v.is_changes)

    code = OK
    if refusers:
        msgs.append(
            f"::error::the current review requests changes — outstanding refusal(s) by: "
            f"{' '.join(refusers)}"
        )
        msgs.append(
            "  A LATER APPROVE DOES NOT CLEAR THIS (#82). Only a new verdict from the same reviewer "
            "retires its refusal, or a push, which makes a new head that these verdicts do not name."
        )

    # The certifying verdict is the most recent one. It decides WHO is checked for independence; it
    # does not decide whether anything was refused.
    reviewer = for_head[-1].role

    if reviewer in author_set:
        if policy == "self-allowed" and not refusers:
            # EXIT 3: SELF-REVIEWED. Not 0, which would say an independent agent looked, and not 1,
            # which would say nobody did. **Widening WHO may certify must never widen WHAT counts as
            # certified** — so this is unreachable while anything is refused.
            msgs.append(
                f"::notice::'{reviewer}' authored commits in this PR. This repository permits a "
                f"self-review, so this PASSES — published as a SELF-review, never as an independent one."
            )
            code = SELF_REVIEWED
        else:
            msgs.append(
                f"::error::'{reviewer}' authored commits in this PR, so its review does not "
                f"establish independence"
            )
            code = NO_REVIEW

    # A REFUSAL SURVIVES EVERY OTHER COMPLAINT ABOUT THE SAME REVIEW. `rc` used to be one scalar
    # each check overwrote in turn, so a changes-requested set 2 and the independence check three
    # lines later set 1 — and the workflow published "No current review by an independent agent"
    # over a verdict that had landed and refused.
    if refusers:
        code = CHANGES
    elif unplaceable and code in (OK, NO_REVIEW):
        code = UNPLACEABLE

    if code == OK:
        msgs.append(
            f"review ok: {head} reviewed by '{reviewer}', which authored none of its commits"
        )
    elif code == SELF_REVIEWED:
        msgs.append(
            f"review ok (SELF-REVIEWED): {head} certified by '{reviewer}', which also built it. "
            f"NO INDEPENDENT AGENT HAS LOOKED AT THIS."
        )

    return Result(code=code, messages=msgs, reviewer=reviewer,
                  refusers=refusers, unplaceable=unplaceable)


def main(argv: list[str]) -> int:
    if argv and argv[0].startswith("-"):
        if argv[0] != "--self-test":
            print(
                f"::error::unknown option '{argv[0]}'. This is a typo, not an argument — refusing.",
                file=sys.stderr,
            )
            return USAGE
        return _self_test()

    if len(argv) < 2:
        print("usage: check_review.py <head-sha> <comments-json> [base-sha]", file=sys.stderr)
        return USAGE

    head, comments_path = argv[0], argv[1]
    base = argv[2] if len(argv) > 2 else ""

    # A MISSING COMMENTS FILE IS A LOOKUP FAILURE, NOT A FINDING THAT NO REVIEW EXISTS.
    try:
        comments = json.loads(Path(comments_path).read_text())
    except OSError:
        print(
            f"::error::'{comments_path}' does not exist, so no review was examined. This is a "
            f"LOOKUP FAILURE and NOT a statement that no review exists.",
            file=sys.stderr,
        )
        return NO_REVIEW
    except json.JSONDecodeError as e:
        print(
            f"::error::'{comments_path}' is not readable JSON ({e}), so no review was examined. "
            f"This is a LOOKUP FAILURE and NOT a statement that no review exists.",
            file=sys.stderr,
        )
        return NO_REVIEW

    try:
        commits = authors_mod.from_range(base, head) if base else []
        author_set = authors_mod.authors_of(commits) if commits else set()
    except authors_mod.NoTrailers:
        # NO TRAILER MEANS INDEPENDENCE CANNOT BE ESTABLISHED, which the naming gate reports with
        # its remedy. Here it means nobody is excluded — the review still has to exist and be
        # attributable, which is what the rest of this gate checks.
        author_set = set()
    except LookupFailure as e:
        print(f"::error::{e.reason}", file=sys.stderr)
        print(
            "  Independence could not be established in EITHER direction, so nothing is certified.",
            file=sys.stderr,
        )
        return NO_REVIEW

    result = evaluate(head, comments, author_set, policy=review_policy())
    result.report()
    return result.code


def _self_test() -> int:
    """The suite lives in tests/; this stays so `run-gates.sh` and the Makefile keep working."""
    import subprocess as sp
    here = Path(__file__).resolve().parent
    r = sp.run(
        ["uv", "run", "--isolated", "--no-project", "--with", "pytest",
         "pytest", str(here / "tests"), "-q"],
        capture_output=True, text=True, cwd=here, check=False,
    )
    if r.returncode == 0:
        print("self-test passed: " + _PROPERTIES)
        return 0
    print(r.stdout or r.stderr, file=sys.stderr)
    # A MISSING TEST RUNNER IS NOT A PASSING SUITE. If `uv` is absent this says so rather than
    # reporting success, because "could not check" and "checked and fine" is the one confusion this
    # project does not permit.
    if "No such file" in (r.stderr or "") or r.returncode == 127:
        print("::error::could not run the test suite (is `uv` installed?). This is NOT a pass.",
              file=sys.stderr)
    return 1


_PROPERTIES = (
    "a missing comments file refuses, an author cannot certify its own work, a stale sha does not "
    "carry over, a QUOTED verdict is not a verdict, a verdict naming somebody other than its poster "
    "is refused, a landed refusal survives every later verdict except its own reviewer's, and a "
    "verdict naming a sha this repository does not know is reported rather than dropped"
)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
