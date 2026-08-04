#!/usr/bin/env python3
"""Open a pull request, read its real state, arm auto-merge. One command per act.

WHY THIS EXISTS. Everything here was prose in a role prompt, and prose is read once, before it is
needed. Each of these was a measured failure:

  - `gh pr create` and `gh pr merge --auto` are GraphQL calls. That quota runs out separately from
    REST and, when it did, they failed while REST kept working.
  - CHECK RUNS AND COMMIT STATUSES ARE DIFFERENT ENDPOINTS, and the review verdict lives only in the
    status. An agent read the check runs, saw green, and took an unreviewed pull request as
    reviewed. Another fixed the one red the check runs showed and never saw the one blocking it.
  - `gh pr merge --auto` exits 0 while refusing, and some repositories disallow auto-merge entirely.
    An agent that does not read back reports an armed pull request that is not armed.

Usage: pr.py open <branch> <title> <body-file>
       pr.py state <number> [--brief]   1 red, 2 no answer yet, 3 conflict
       pr.py arm <number>
       pr.py rereview <number>
       pr.py --self-test
"""

from __future__ import annotations

import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

from gh import Client, LookupFailure, resolve_repo

GREEN, RED, NO_ANSWER, CONFLICT = 0, 1, 2, 3


@dataclass
class State:
    code: int
    sha: str = ""
    brief: str = ""
    failing_runs: list[str] = field(default_factory=list)
    failing_statuses: list[str] = field(default_factory=list)
    pending: int = 0
    lines: list[str] = field(default_factory=list)


def read_state(client: Client, repo: str, number: int, local_head: str | None = None) -> State:
    """Both endpoints, always, in one place. Reading one is how an unreviewed pull request looks
    reviewed, and how a blocking red stays invisible."""
    pr = client.get(f"repos/{repo}/pulls/{number}")
    sha = pr["head"]["sha"]
    mergeable = pr.get("mergeable")

    lines: list[str] = []

    # A HEAD THAT IS NOT YOUR HEAD MUST SAY SO. For about a minute after a force-push this reported
    # the PREVIOUS commit and its verdicts, with nothing marking them as another commit's.
    if local_head and local_head != sha and _is_ancestor(sha, local_head):
        lines.append(f"STALE HEAD — the API still has {sha[:8]}, not your {local_head[:8]}")

    # A CONFLICT IS A STATE, AND UNTIL RECENTLY IT WAS SILENCE. Measured: #38 and #46 sat at
    # mergeable=false for a day and a half. GitHub cannot build a merge ref for such a pull request,
    # so `gates.yml` — which runs `on: pull_request` — NEVER SCHEDULES. No check ever reports, so
    # this said `NO ANSWER YET`, the string for "CI has not got there yet", which reads as "wait".
    # Nobody was waiting. Both branches also held their Issues claimed, and both were release
    # blockers.
    #
    # `mergeable` IS COMPUTED ASYNCHRONOUSLY AND None MEANS NOT YET COMPUTED. Rendering None as
    # False would be this project's own defect: `could not determine` becoming `determined to be no`.
    if mergeable is False:
        return State(code=CONFLICT, sha=sha, lines=lines,
                     brief="CONFLICT — will not merge into the base; "
                           "no gate can run until it is rebased")

    runs = client.get(f"repos/{repo}/commits/{sha}/check-runs") or {}
    statuses = client.get(f"repos/{repo}/commits/{sha}/status") or {}

    check_runs = runs.get("check_runs") or []
    st = statuses.get("statuses") or []

    bad_runs = [r["name"] for r in check_runs
                if r.get("conclusion") in ("failure", "timed_out", "cancelled")]
    bad_st = [s["context"] for s in st if s.get("state") in ("failure", "error")]
    pending = sum(1 for r in check_runs if r.get("status") != "completed")

    # NO CHECKS AT ALL IS NOT A GREEN. It once printed "(none yet — CI may not have started)" and
    # "all green." on the same run — two lines contradicting each other, and `all green` is the
    # string an agent greps for.
    if not check_runs and not st:
        return State(code=NO_ANSWER, sha=sha, lines=lines,
                     brief="NO ANSWER YET — nothing has reported on this head")

    if bad_runs or bad_st:
        return State(code=RED, sha=sha, lines=lines, failing_runs=bad_runs,
                     failing_statuses=bad_st,
                     brief="RED " + " ".join(bad_runs + bad_st))
    if pending:
        return State(code=NO_ANSWER, sha=sha, lines=lines, pending=pending,
                     brief=f"RUNNING {pending} check(s)")
    return State(code=GREEN, sha=sha, lines=lines, brief="GREEN")


def _is_ancestor(a: str, b: str) -> bool:
    r = subprocess.run(["git", "merge-base", "--is-ancestor", a, b],
                       capture_output=True, check=False)
    return r.returncode == 0


def _local_head() -> str | None:
    r = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True, check=False)
    return r.stdout.strip() or None


def do_open(client: Client, repo: str, branch: str, title: str, body_file: str) -> int:
    p = Path(body_file)
    if not p.is_file():
        print(f"::error::body file '{body_file}' does not exist. Refusing to open a pull request "
              f"with an empty body.", file=sys.stderr)
        return 1
    base = (client.get(f"repos/{repo}") or {}).get("default_branch", "main")
    try:
        out = client.post(f"repos/{repo}/pulls",
                          {"title": title, "head": branch, "base": base, "body": p.read_text()})
    except LookupFailure as e:
        print(f"::error::could not open the pull request: {e.reason}", file=sys.stderr)
        if "No commits between" in e.reason:
            print("  Nothing to merge — the branch has no commits the base lacks.", file=sys.stderr)
        elif "already exist" in e.reason:
            print("  A pull request for this branch is already open.", file=sys.stderr)
        elif "not found" in e.reason:
            print(f"  Push the branch first: git push -u origin {branch}", file=sys.stderr)
        return 1
    print(f"opened #{out['number']}  {branch} -> {base}")
    return 0


def do_arm(client: Client, repo: str, number: int) -> int:
    """`gh pr merge --auto` is GraphQL and exits 0 while refusing, so the READ-BACK is the check."""
    repo_info = client.get(f"repos/{repo}") or {}
    if repo_info.get("allow_auto_merge") is False:
        print(f"NOT APPLICABLE: this repository has auto-merge disabled, so no pull request can be "
              f"armed.\n  A verifier merges by hand. This is a repository setting, not something "
              f"about #{number}.")
        return 0
    subprocess.run(["gh", "pr", "merge", str(number), "--auto", "--squash"],
                   capture_output=True, check=False)
    pr = client.get(f"repos/{repo}/pulls/{number}") or {}
    armed = pr.get("auto_merge") is not None
    if armed:
        print(f"ARMED — #{number} merges itself when the gates go green.")
        return 0
    print("NOT ARMED. The call did not fail loudly; the read-back is how you know.", file=sys.stderr)
    return 1


def do_rereview(client: Client, repo: str, number: int) -> int:
    """A re-review request is a state change on the pull request, not a note.

    A REQUEST NAMING THE WRONG SHA SENDS A REVIEWER TO THE WRONG COMMIT. Called straight after a
    push, this raced it and asked for a re-review of the commit that had just been replaced.
    """
    sha = (client.get(f"repos/{repo}/pulls/{number}") or {})["head"]["sha"]
    local = _local_head()
    if local and local != sha and _is_ancestor(sha, local):
        print(f"::error::the API still has {sha[:8]}; your HEAD is {local[:8]}. Nothing was sent.",
              file=sys.stderr)
        return 1
    body = (f"**Re-review requested — the head has moved to `{sha[:8]}`.**\n\n"
            f"Any earlier verdict was posted against a different commit and no longer applies.\n")
    client.post(f"repos/{repo}/issues/{number}/comments", {"body": body})
    print(f"re-review requested on #{number} for {sha[:8]}")
    return 0


def main(argv: list[str]) -> int:
    if argv and argv[0].startswith("-"):
        if argv[0] != "--self-test":
            print(f"::error::unknown option '{argv[0]}'. This is a typo, not an argument — "
                  f"refusing.", file=sys.stderr)
            return 2
        return _self_test()
    if not argv:
        print("usage: pr.py open|state|arm|rereview ... | --self-test", file=sys.stderr)
        return 2

    cmd, rest = argv[0], argv[1:]
    try:
        repo = resolve_repo()
        client = Client()
        if cmd == "open":
            if len(rest) != 3:
                print("usage: pr.py open <branch> <title> <body-file>", file=sys.stderr)
                return 2
            return do_open(client, repo, *rest)
        if cmd == "state":
            if not rest:
                print("usage: pr.py state <number> [--brief]", file=sys.stderr)
                return 2
            brief = "--brief" in rest
            s = read_state(client, repo, int(rest[0]), _local_head())
            if brief:
                for line in s.lines:
                    print(line)
                print(s.brief)
            else:
                print(f"PR #{rest[0]}  head {s.sha[:8]}")
                for line in s.lines:
                    print(f"  {line}")
                print(f"\n  {s.brief}")
            return s.code
        if cmd == "arm":
            return do_arm(client, repo, int(rest[0]))
        if cmd == "rereview":
            return do_rereview(client, repo, int(rest[0]))
    except LookupFailure as e:
        print(f"::error::{e.reason}", file=sys.stderr)
        print("  This is a LOOKUP FAILURE and NOT a statement about its state.", file=sys.stderr)
        return 1
    print(f"::error::'{cmd}' is not a subcommand. One of: open state arm rereview", file=sys.stderr)
    return 2


def _self_test() -> int:
    import subprocess as sp
    here = Path(__file__).resolve().parent
    r = sp.run(["uv", "run", "--isolated", "--no-project", "--with", "pytest",
                "pytest", str(here / "tests"), "-q"],
               capture_output=True, text=True, cwd=here, check=False)
    if r.returncode == 0:
        print("self-test passed: unknown input refuses, state reads both endpoints, arm reads back, "
              "an unreadable repository never reports green, a conflict is reported as a conflict, "
              "and an uncomputed mergeable is not")
        return 0
    print(r.stdout or r.stderr, file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
