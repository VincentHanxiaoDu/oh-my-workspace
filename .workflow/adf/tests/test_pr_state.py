"""Reading a pull request's real state: both endpoints, and the conflict that was silence."""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import pr as prmod  # noqa: E402


class C:
    def __init__(self, mergeable=True, runs=(), statuses=()):
        self.mergeable, self.runs, self.statuses = mergeable, list(runs), list(statuses)

    def get(self, path):
        if "check-runs" in path:
            return {"check_runs": self.runs}
        if path.endswith("/status"):
            return {"statuses": self.statuses}
        if "/pulls/" in path:
            return {"head": {"sha": "cafe1234", "ref": "dev/fix/1-x"}, "mergeable": self.mergeable}
        return {}


def test_a_conflict_is_reported_as_a_conflict_not_as_no_answer_yet():
    """#38 and #46 sat at mergeable=false for a day and a half. GitHub builds no merge ref, so the
    gates NEVER SCHEDULE — nothing will ever report on that head. `NO ANSWER YET` is the string for
    'CI has not got there yet' and it reads as 'wait'. Nobody was waiting, and both branches held
    release-blocking Issues claimed the whole time."""
    s = prmod.read_state(C(mergeable=False), "o/r", 1)
    assert s.code == prmod.CONFLICT and "CONFLICT" in s.brief


def test_an_uncomputed_mergeable_is_not_a_conflict():
    """`mergeable` is computed asynchronously and None means NOT YET COMPUTED. Rendering None as
    False would be this project's own defect: could-not-determine becoming determined-to-be-no."""
    s = prmod.read_state(C(mergeable=None), "o/r", 1)
    assert s.code != prmod.CONFLICT


def test_no_checks_at_all_is_not_a_green():
    """It once printed '(none yet — CI may not have started)' and 'all green.' on the same run —
    two lines contradicting each other, and `all green` is the string an agent greps for."""
    assert prmod.read_state(C(), "o/r", 1).code == prmod.NO_ANSWER


def test_a_red_commit_status_is_seen_even_when_every_check_run_is_green():
    """CHECK RUNS AND COMMIT STATUSES ARE DIFFERENT ENDPOINTS and the review verdict lives only in
    the status. An agent read the check runs, saw green, and took an UNREVIEWED pull request as
    reviewed."""
    c = C(runs=[{"name": "build", "status": "completed", "conclusion": "success"}],
          statuses=[{"context": "Reviewed by an agent", "state": "failure"}])
    s = prmod.read_state(c, "o/r", 1)
    assert s.code == prmod.RED and "Reviewed by an agent" in s.brief


def test_a_pending_check_is_not_an_answer():
    c = C(runs=[{"name": "build", "status": "in_progress"}])
    assert prmod.read_state(c, "o/r", 1).code == prmod.NO_ANSWER


def test_green_requires_something_to_have_reported():
    c = C(runs=[{"name": "build", "status": "completed", "conclusion": "success"}],
          statuses=[{"context": "review", "state": "success"}])
    assert prmod.read_state(c, "o/r", 1).code == prmod.GREEN
