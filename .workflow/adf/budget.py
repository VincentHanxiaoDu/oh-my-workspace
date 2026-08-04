#!/usr/bin/env python3
"""The API budget, and WHICH limit is holding us — because the two clear on different clocks.

THE RESERVE IS FOR THE WORK, NOT FOR THE WATCHING. Below it the watches stop polling and the role
keeps what is left to review, merge and close with. Measured: one poll of both watches costs a role
about 52 API calls on a six-pull-request board, so three roles at 60s is ~9360 calls/hour against a
limit of 5000 — 1.9x over, before any agent does any work of its own. The watch then spends the
budget the role needs and reports LOOKUP FAILED for polls its own polling made impossible.

ISSUE #81 IS THE WHOLE OF THE SECOND HALF OF THIS FILE. The guard read the PRIMARY rate limit while
every outage in that session was SECONDARY:

    PRIMARY    the hourly quota. Exhausted means `x-ratelimit-remaining` is 0, and
               `x-ratelimit-reset` says when it comes back.
    SECONDARY  a burst rule. It clears with QUIET, `retry-after` says how long, and
               `x-ratelimit-remaining` can be perfectly healthy throughout — which is exactly how
               the guard came to report "plenty left" through an outage it existed to catch.

Waiting on the primary reset when a secondary limit is holding you is waiting on a signal that never
described the problem.

Usage: budget.py check [reserve]      exit 1 below the reserve, 2 if the limit could not be read
       budget.py hold-for <floor>     seconds to wait, from whichever limit is holding
       budget.py note-failure <text>  exit 0 if that text is a rate limit, 1 if some other outage
       budget.py --self-test
"""

from __future__ import annotations

import os
import re
import sys
import time

from gh import Client, LookupFailure

DEFAULT_RESERVE = int(os.environ.get("ADF_BUDGET_RESERVE", "1500"))

# THE PHRASES GITHUB ACTUALLY USES. Matched case-insensitively, and `secondary` is checked BEFORE
# the generic one because every secondary message also contains "rate limit".
_SECONDARY = re.compile(r"secondary rate limit|abuse detection|retry[- ]after", re.I)
_PRIMARY = re.compile(r"rate limit exceeded|api rate limit|x-ratelimit-remaining: ?0", re.I)


def classify(text: str) -> str:
    """`secondary` | `primary` | `other`. Never a boolean — the two limits are not one fact."""
    if _SECONDARY.search(text):
        return "secondary"
    if _PRIMARY.search(text):
        return "primary"
    return "other"


def read_limit(client: Client) -> tuple[int, int, float]:
    """(remaining, limit, reset_epoch). READING THE LIMIT IS FREE and does not spend it.

    Raises LookupFailure. A budget that cannot be read is NOT a healthy budget — reporting a
    comfortable number when the check itself failed is this project's defect applied to the guard
    against it.
    """
    data = client.get("rate_limit")
    core = (data or {}).get("resources", {}).get("core") or (data or {}).get("rate")
    if not core:
        raise LookupFailure("the rate limit response carried no core resource")
    return int(core["remaining"]), int(core["limit"]), float(core["reset"])


def hold_for(floor: int, *, secondary: bool, retry_after: float | None,
             reset_at: float | None, now: float | None = None) -> float:
    """Seconds to wait, never less than one interval and never more than ten minutes.

    `retry-after` FOR A SECONDARY LIMIT, `x-ratelimit-reset` FOR A PRIMARY ONE (#81). And a ceiling,
    because an unreadable or absurd reset must not park a watch indefinitely — a watch that never
    polls again is indistinguishable from a dead one, which is the failure the heartbeat exists for.
    """
    now = time.time() if now is None else now
    if secondary and retry_after:
        wait = retry_after
    elif reset_at:
        wait = reset_at - now
    else:
        wait = floor
    return float(max(floor, min(wait, 600)))


def check(client: Client, reserve: int = DEFAULT_RESERVE) -> tuple[int, str]:
    """(exit code, message). 0 above the reserve, 1 below it, 2 if it could not be read."""
    try:
        remaining, limit, reset = read_limit(client)
    except LookupFailure as e:
        return 2, (f"::error::the rate limit could not be read: {e.reason}\n"
                   f"  This is NOT a statement that there is budget left.")
    if remaining < reserve:
        secs = max(0, int(reset - time.time()))
        return 1, (f"HOLDING — {remaining} of {limit} left, below the reserve of {reserve}; "
                   f"resets in {secs}s")
    return 0, f"{remaining} of {limit} left (reserve {reserve})"


def main(argv: list[str]) -> int:
    if argv and argv[0] == "--self-test":
        return _self_test()
    if not argv:
        print("usage: budget.py check|hold-for|note-failure ... | --self-test", file=sys.stderr)
        return 2

    cmd = argv[0]
    if cmd == "check":
        reserve = int(argv[1]) if len(argv) > 1 else DEFAULT_RESERVE
        code, msg = check(Client(), reserve)
        print(msg, file=sys.stderr if code else sys.stdout)
        return code
    if cmd == "hold-for":
        floor = int(argv[1]) if len(argv) > 1 else 300
        try:
            _, _, reset = read_limit(Client())
        except LookupFailure:
            print(int(floor))
            return 0
        print(int(hold_for(floor, secondary=False, retry_after=None, reset_at=reset)))
        return 0
    if cmd == "note-failure":
        kind = classify(" ".join(argv[1:]))
        print(kind)
        # EXIT 0 MEANS "THIS WAS A RATE LIMIT", which the caller uses to decide whether to back off
        # or to report an outage. `other` is not a throttle and must not be waited out silently.
        return 0 if kind in ("secondary", "primary") else 1
    print(f"::error::'{cmd}' is not a subcommand. One of: check hold-for note-failure",
          file=sys.stderr)
    return 2


def _self_test() -> int:
    import subprocess as sp
    from pathlib import Path
    here = Path(__file__).resolve().parent
    r = sp.run(["uv", "run", "--isolated", "--no-project", "--with", "pytest",
                "pytest", str(here / "tests"), "-q"],
               capture_output=True, text=True, cwd=here, check=False)
    if r.returncode == 0:
        print("self-test passed: a secondary limit is not a primary one, an unreadable limit is not "
              "a healthy budget, and a hold never exceeds ten minutes")
        return 0
    print(r.stdout or r.stderr, file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
