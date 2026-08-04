#!/usr/bin/env python3
"""What this role does next. Derived from repository state alone — never from being told.

PRD R3: an agent must be able to compute its own queue. There is no owner:* label, no assignment
message, no coordinator-maintained list. State is stored once.

THE OUTPUT OF A FAILED LOOKUP IS NOT AN EMPTY QUEUE. That is this project's unifying defect —
`could not see` rendering as `nothing to see` — and it is why every read here raises rather than
returning empty. An agent that gets a clean exit and no items may conclude it has no work, and that
conclusion must only ever be reachable from a query that actually ran.

Usage: queue.py <role>        # dev | qa | product | ops | flow | pm | owner
       queue.py --self-test
"""

from __future__ import annotations

import sys
from dataclasses import dataclass, field

import roles as roles_mod
import verdicts as vmod
from gh import Client, LookupFailure, resolve_repo

# THE POINT AT WHICH REVIEWING AGAIN IS NOT THE ANSWER. Three, because one pull request was still
# not converging at seven.
REVIEW_MAX_ROUNDS = 3

TYPE_TO_ROLE_BRANCH = {
    "qa": ("fix", "bug", "chore", "docs", "test", "ci", "build", "refactor", "perf"),
    "product": ("feat", "spec"),
}


@dataclass
class Board:
    """Everything read from GitHub for one run, read ONCE.

    ONE FETCH PER QUESTION PER RUN. Measured: three roles and four watches share a 5,000/hour
    budget, and 246 rate-limit refusals were counted in one day — with the queue reporting LOOKUP
    FAILED for polls its own polling had made impossible. The comments of the whole repository were
    being paginated three times in a single run of a single role's queue.
    """

    client: Client
    repo: str
    _issues: list[dict] | None = None
    _open_prs: list[dict] | None = None
    _all_prs: list[dict] | None = None
    _all_comments: list[dict] | None = None
    _pr_comments: dict[int, list[dict]] = field(default_factory=dict)

    def issues(self) -> list[dict]:
        """Open Issues. REST, not GraphQL: the shared GraphQL quota was measured exhausted
        (5000/5000) on a working day while REST still had headroom, and `gh issue list` returned an
        EMPTY FILE rather than an error when that happened."""
        if self._issues is None:
            raw = self.client.paginate(f"repos/{self.repo}/issues?state=open")
            self._issues = [i for i in raw if not i.get("pull_request")]
        return self._issues

    def open_prs(self) -> list[dict]:
        if self._open_prs is None:
            self._open_prs = self.client.paginate(f"repos/{self.repo}/pulls?state=open")
        return self._open_prs

    def merged_pr_refs(self) -> list[str]:
        """MERGED, NOT MERELY OPENED. `state=all` counted a pull request CLOSED without merging, so
        an abandoned attempt removed its Issue from dev's queue permanently — deleting the branch
        did not help, because the pull request record keeps the ref."""
        if self._all_prs is None:
            self._all_prs = self.client.paginate(f"repos/{self.repo}/pulls?state=all")
        return [p["head"]["ref"] for p in self._all_prs if p.get("merged_at")]

    def all_comments(self) -> list[dict]:
        """Every comment in the repository, once. Three questions are asked of this."""
        if self._all_comments is None:
            self._all_comments = self.client.paginate(f"repos/{self.repo}/issues/comments")
        return self._all_comments

    def pr_comments(self, number: int) -> list[dict]:
        if number not in self._pr_comments:
            self._pr_comments[number] = self.client.paginate(
                f"repos/{self.repo}/issues/{number}/comments"
            )
        return self._pr_comments[number]

    def verdicts(self, number: int) -> list[vmod.Verdict]:
        return vmod.parse(self.pr_comments(number))


def issue_number_of_branch(ref: str) -> int | None:
    """`<role>/<type>/<issue>-<slug>` carries the Issue number, so the branch IS the claim.

    There is no label to set, no comment to post and nothing to expire. Without this, two agents of
    the same role both took the same Issue: it stayed in "to resolve" while its own pull request was
    open two lines below.
    """
    parts = ref.split("/")
    if len(parts) != 3:
        return None
    tail = parts[2].split("-", 1)[0]
    return int(tail) if tail.isdigit() else None


def claimed_issues(board: Board) -> set[int]:
    """AN OPEN PULL REQUEST IS THE CLAIM, NOT A BRANCH THAT EXISTS.

    Branches outlive their merges — GitHub keeps them unless somebody deletes them — so a
    branch-based claim never expires, and an Issue whose work had shipped stayed marked "somebody is
    on it" forever. An open pull request ends exactly when the work does.
    """
    return {n for p in board.open_prs()
            if (n := issue_number_of_branch(p["head"]["ref"])) is not None}


def ever_built(board: Board) -> set[int]:
    return {n for ref in board.merged_pr_refs()
            if (n := issue_number_of_branch(ref)) is not None}


def _issue_numbers_with_comment_prefix(board: Board, prefixes: tuple[str, ...]) -> set[int]:
    out: set[int] = set()
    for c in board.all_comments():
        body = c.get("body") or ""
        if body.startswith(prefixes):
            url = c.get("issue_url", "")
            tail = url.rsplit("/", 1)[-1]
            if tail.isdigit():
                out.add(int(tail))
    return out


def ruled_issues(board: Board) -> set[int]:
    """AN ANSWERED DECISION IS NOT A WAITING ONE, AND THE ANSWER ARRIVES AS A COMMENT.

    The `## Blocked on a decision` section stays in the body forever — it is the record of what was
    asked — so a ruling posted underneath left the Issue sitting in "waiting on a decision" with the
    decision already made.
    """
    return _issue_numbers_with_comment_prefix(board, ("**[owner-ruling]", "[owner-ruling]"))


def verified_by(board: Board, role: str) -> set[int]:
    """Issues already carrying this role's own marked comment.

    A product agent UAT'd two Issues, found their criteria unreachable, deliberately left them open
    and recorded why — and the queue went on listing them under "UAT and CLOSE", telling the next
    agent to do work already done. A queue that repeats finished work is a queue people stop reading.
    """
    return _issue_numbers_with_comment_prefix(board, (f"[{role}]",))


# -- sections ------------------------------------------------------------------
def labels_of(issue: dict) -> set[str]:
    return {lb["name"] for lb in issue.get("labels", [])}


def _fmt(number: int, title: str, note: str = "", width: int = 46) -> str:
    if note:
        return f"  #{number:<4} {title[:width]:<{width}} {note}"
    return f"  #{number}  {title}"


def unreviewed_own_prs(board: Board, role: str, out: list[str]) -> None:
    """The author gets its own work reviewed, and one reviewer owns it across rounds.

    THIS REPLACED THE PING-PONG. Measured on one pull request: eleven verdicts in eighteen hours,
    seven changes-requested and four approve, alternating between two roles, 32 comments, still open
    and unmerged with every check green. A changes-requested costs a push, a push moves the head,
    and a moved head re-opened the review to EVERY independent role — so each round was judged by a
    different agent against a different standard, raising findings the previous one had passed.

    WHOSE PULL REQUEST THIS IS COMES FROM ITS BRANCH NAME, which the naming gate already enforces.
    Deriving it from the `Agent:` trailers cost one API call per open pull request per role per
    round to answer what the branch name already answers, and on a six-pull-request board that made
    dev's queue MORE expensive than the design it replaced. It is NOT the independence test —
    that is check_review, which re-derives from the trailers at verdict time.
    """
    out.append("\nYOUR PULL REQUESTS WITH NO VERDICT ON THE CURRENT HEAD — "
               "dispatch a reviewer before anything else:")
    any_ = False
    for pr in board.open_prs():
        ref, sha, num, title = (pr["head"]["ref"], pr["head"]["sha"],
                                pr["number"], pr.get("title", ""))
        if not ref.startswith(f"{role}/"):
            continue
        vs = board.verdicts(num)
        rounds = vmod.changes_rounds(vs)
        if rounds >= REVIEW_MAX_ROUNDS:
            any_ = True
            out.append(_fmt(num, title,
                            f"ESCALATED — {rounds} rounds of changes; do NOT push again"))
            out.append("        Say so on the pull request and hand it to product, which is the "
                       "only role that may put it to the owner.")
            continue
        owner = vmod.owner(vs)
        if owner and vmod.ruled_on(vs, owner, sha):
            continue
        any_ = True
        if owner:
            out.append(_fmt(num, title, f"re-review by {owner} (round {rounds + 1})"))
        else:
            out.append(_fmt(num, title, "NO REVIEW HAS HAPPENED — dispatch one now"))
    if not any_:
        out.append("  (none)")


def escalated_reviews(board: Board, out: list[str]) -> None:
    """What the review loop could not settle — product's section, and product's alone.

    A disagreement two competent agents cannot settle in three rounds is a question about what the
    project wants, and that is a decision, not a defect. Before this it had nowhere to go, so it
    went round again.
    """
    out.append("\nREVIEWS THAT DID NOT CONVERGE — yours to put to the owner, and nobody else may:")
    any_ = False
    for pr in board.open_prs():
        rounds = vmod.changes_rounds(board.verdicts(pr["number"]))
        if rounds >= REVIEW_MAX_ROUNDS:
            any_ = True
            out.append(_fmt(pr["number"], pr.get("title", ""),
                            f"{rounds} rounds of changes-requested"))
    if not any_:
        out.append("  (none)")


def decisions_waiting(board: Board, out: list[str], heading: str) -> None:
    ruled = ruled_issues(board)
    out.append(f"\n{heading}")
    any_ = False
    for i in board.issues():
        if "## Blocked on a decision" in (i.get("body") or "") and i["number"] not in ruled:
            any_ = True
            out.append(_fmt(i["number"], i.get("title", "")))
    if not any_:
        out.append("  (none)")


def my_prs(board: Board, patterns: tuple[str, ...], heading: str, out: list[str],
           state_of=None) -> None:
    """The pull requests this role acts on, routed by the TYPE in the branch name.

    WHICH PULL REQUESTS ARE MINE IS NOT ALWAYS "THE ONES I AUTHORED". dev owns the branches it
    wrote; product owns the FEATURE pull requests whoever wrote them, because UAT is done on
    somebody else's branch. Filtering product by a `product/*` prefix returned (none) for a round
    whose entire workload was two dev branches — a successful lookup that filtered the answer away,
    which is worse than a failed one because the exit code is 0.

    Matching every branch instead put one archive pull request in both queues at once, and two roles
    racing to merge the same thing is exactly the collision this queue exists to prevent.
    """
    out.append(f"\n{heading}:")
    any_ = False
    for pr in board.open_prs():
        ref = pr["head"]["ref"]
        parts = ref.split("/")
        if len(parts) != 3 or parts[1] not in patterns:
            continue
        any_ = True
        note = state_of(pr) if state_of else ""
        out.append(_fmt(pr["number"], pr.get("title", ""), note))
    if not any_:
        out.append("  (none)")


def emit(board: Board, heading: str, keep, out: list[str], *, drop_reasons=()) -> None:
    """One section. `keep(issue) -> bool`, and every DROP is reported rather than hidden.

    A COUNT THAT SILENTLY EXCLUDES THINGS IS A COUNT NOBODY CAN CHECK. Each `drop_reasons` entry is
    (predicate, why) and the Issues it removes are listed under it, so a role can see that work
    exists and why it is not being offered — the difference between "you have nothing" and "you have
    nothing right now, and here is what is behind it".
    """
    out.append(f"\n{heading}")
    kept, dropped = [], {}
    for i in board.issues():
        if not keep(i):
            continue
        for pred, why in drop_reasons:
            if pred(i):
                dropped.setdefault(why, []).append(i)
                break
        else:
            kept.append(i)
    for i in kept:
        out.append(_fmt(i["number"], i.get("title", "")))
    if not kept:
        out.append("  (none)")
    for why, items in dropped.items():
        out.append(f"\n  ({why})")
        for i in items:
            out.append(f"      #{i['number']}  {i.get('title', '')}")


def role_queue(board: Board, role: str, state_of=None) -> list[str]:
    if role not in roles_mod.ALL_ROLES:
        raise ValueError(
            f"'{role}' is not a role. One of: {' '.join(roles_mod.ALL_ROLES)}"
        )
    out: list[str] = []
    claimed = claimed_issues(board)
    built = ever_built(board)
    ruled = ruled_issues(board)
    verified = verified_by(board, role)

    typed = lambda i: any(n.startswith("type:") for n in labels_of(i))          # noqa: E731
    unbuilt = (lambda i: i["number"] in claimed,
               "a pull request has already been opened for this")
    landed = (lambda i: i["number"] in claimed,
              "still has an open pull request — somebody else's turn")
    notbuilt = (lambda i: i["number"] not in built,
                "not built yet — dev has not opened a pull request for it")
    done = (lambda i: i["number"] in verified,
            "you have already recorded a verdict on this")

    if role == "dev":
        emit(board, "ISSUES TO RESOLVE — open one branch and one PR per Issue:",
             lambda i: typed(i) and "blocked" not in labels_of(i), out,
             drop_reasons=(unbuilt,))
        my_prs(board, ("fix", "feat", "chore", "bug", "docs", "test", "ci", "build",
                       "refactor", "perf", "spec"),
               "YOUR PULL REQUESTS", out, state_of)
        unreviewed_own_prs(board, "dev", out)

    elif role == "qa":
        emit(board, "ISSUES WHOSE WORK HAS LANDED — verify on main and CLOSE:",
             lambda i: {"type:bug", "type:chore"} & labels_of(i), out,
             drop_reasons=(done, landed, notbuilt))
        my_prs(board, TYPE_TO_ROLE_BRANCH["qa"],
               "PULL REQUESTS TO VERIFY, MERGE AND CLOSE — whoever wrote them", out, state_of)
        unreviewed_own_prs(board, "qa", out)

    elif role == "product":
        emit(board, "FEATURES WHOSE WORK HAS LANDED — UAT on main and CLOSE:",
             lambda i: "type:feature" in labels_of(i), out,
             drop_reasons=(done, landed, notbuilt))
        my_prs(board, TYPE_TO_ROLE_BRANCH["product"],
               "PULL REQUESTS TO UAT, MERGE AND CLOSE — whoever wrote them", out, state_of)
        unreviewed_own_prs(board, "product", out)
        escalated_reviews(board, out)
        decisions_waiting(
            board, out,
            "DECISIONS RAISED BY OTHER ROLES — yours to put to the owner, and nobody else may:")

    elif role == "ops":
        emit(board, "OPEN PULL REQUESTS — CI and gate health:", lambda i: False, out)
        my_prs(board, ("ci", "build", "chore"), "YOUR PULL REQUESTS", out, state_of)
        unreviewed_own_prs(board, "ops", out)

    elif role == "flow":
        # THE PROCESS'S OWN MAINTAINER. `flow/` branches are what this framework's own changes are
        # built on, and until Issue #126 they passed the naming gate and reached no queue at all.
        emit(board, "MACHINERY ISSUES — the process fixing itself:",
             lambda i: "area:machinery" in labels_of(i), out, drop_reasons=(unbuilt,))
        my_prs(board, ("fix", "feat", "chore"), "YOUR PULL REQUESTS", out, state_of)
        unreviewed_own_prs(board, "flow", out)

    elif role == "pm":
        decisions_waiting(board, out,
                          "DECISIONS THE OWNER OWES — the work proceeds around them:")
        emit(board, "UNTYPED — cannot be routed until they carry a type: label:",
             lambda i: not typed(i), out)
        emit(board, "UNCLASSIFIED — no area: label, so the R7 ratio cannot see them:",
             lambda i: not any(n.startswith("area:") for n in labels_of(i)), out)
        p = sum(1 for i in board.issues() if "area:product" in labels_of(i))
        m = sum(1 for i in board.issues() if "area:machinery" in labels_of(i))
        out.append(f"\nRATIO (PRD R7) — product {p} : machinery {m}")
        if m > p:
            out.append("  OVER THE CAP. Dispatch no further machinery work until this is 1:1.")

    elif role == "owner":
        decisions_waiting(board, out,
                          "DECISIONS ONLY YOU CAN MAKE — the work proceeds around them:")
        blockers = [i for i in board.issues() if "blocks:release" in labels_of(i)]
        out.append("\nHOLDING THE RELEASE — these are why nothing can ship yet:")
        for i in blockers:
            out.append(_fmt(i["number"], i.get("title", "")))
        if not blockers:
            out.append("  (none)")
        # "NO BLOCKERS" AND "NOBODY HAS LOOKED" ARE DIFFERENT ANSWERS, and this is the page where
        # confusing them is most expensive: one means ship, the other means you have been told
        # nothing. Measured: a release verdict — "do not ship bbee48f, four blockers" — reached the
        # owner only because they happened to be reading that window at that moment.
        verdicts_recorded = sum(
            1 for c in board.all_comments()
            if (c.get("body") or "").startswith("[product]") and "RELEASE" in (c.get("body") or "")
        )
        out.append("\nRELEASE")
        if blockers:
            out.append(f"  BLOCKED — {len(blockers)} Issue(s) labelled blocks:release are open.")
        elif verdicts_recorded == 0:
            out.append("  UNDETERMINED — nothing is labelled blocks:release AND product has "
                       "recorded no release")
            out.append("  verdict. That is NOT 'ready to ship': it is nobody having said.")
        else:
            out.append(f"  No open blocker. product has recorded {verdicts_recorded} release "
                       f"verdict(s) — read the most recent")
            out.append("  before calling one; a verdict is about a specific sha.")

    return out


def main(argv: list[str]) -> int:
    if argv and argv[0].startswith("-"):
        if argv[0] != "--self-test":
            print(f"::error::unknown option '{argv[0]}'. This is a typo, not an argument — "
                  f"refusing.", file=sys.stderr)
            return 2
        return _self_test()
    if not argv:
        print("usage: queue.py <role> | --self-test", file=sys.stderr)
        return 2

    role = argv[0]
    try:
        repo = resolve_repo()
        client = Client()
        board = Board(client=client, repo=repo)

        # THE STATE COLUMN IS NOT DECORATION. Without it `YOUR PULL REQUESTS` lists three branches
        # that cannot merge and says nothing about it — which is #38 and #46 sitting conflicted for
        # a day and a half, reading as ordinary open work. It costs one read per pull request, which
        # is what it cost before, and it is the read that tells a role its branch is dead in the
        # water.
        import pr as pr_mod

        def state_of(p):
            try:
                return pr_mod.read_state(client, repo, p["number"]).brief
            except LookupFailure as e:
                # A STATE THAT COULD NOT BE READ IS NOT A GREEN, and it is not a conflict either.
                return f"COULD NOT READ — {e.reason[:60]}"

        lines = role_queue(board, role, state_of)
    except ValueError as e:
        print(f"::error::{e}", file=sys.stderr)
        return 1
    except LookupFailure as e:
        # THE WHOLE REASON THIS FILE RAISES RATHER THAN RETURNING EMPTY.
        print(f"::error::the queue could not be read: {e.reason}", file=sys.stderr)
        print("  This is a LOOKUP FAILURE and NOT a statement that you have no work. Do not "
              "proceed as", file=sys.stderr)
        print("  though the queue were empty. Retry, or report the outage.", file=sys.stderr)
        return 1
    print("\n".join(lines))
    return 0


def _self_test() -> int:
    import subprocess as sp
    from pathlib import Path
    here = Path(__file__).resolve().parent
    r = sp.run(["uv", "run", "--isolated", "--no-project", "--with", "pytest",
                "pytest", str(here / "tests"), "-q"],
               capture_output=True, text=True, cwd=here, check=False)
    if r.returncode == 0:
        print("self-test passed: unknown roles refuse, every role that may own a branch has a "
              "queue that shows it its own work, a failed lookup is not an empty queue, an "
              "unreviewed pull request is its author's work and nobody else's, a verdict on the "
              "current head settles it and one naming another head does not, a re-review goes back "
              "to the reviewer that already looked, three rounds escalate to product and two do "
              "not, and the owner is told UNDETERMINED rather than being handed silence")
        return 0
    print(r.stdout or r.stderr, file=sys.stderr)
    if r.returncode == 127 or "No such file" in (r.stderr or ""):
        print("::error::could not run the test suite (is `uv` installed?). This is NOT a pass.",
              file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
