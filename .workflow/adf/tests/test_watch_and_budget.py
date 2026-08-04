"""The watches and the budget guard. Every test is an outage somebody sat through."""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import budget  # noqa: E402
import watch  # noqa: E402
from gh import LookupFailure  # noqa: E402


# -- budget --------------------------------------------------------------------
def test_a_secondary_limit_is_not_a_primary_one():
    """Issue #81. The guard read the PRIMARY limit while every outage in that session was
    SECONDARY — a burst rule that clears with quiet, while x-ratelimit-remaining stays healthy
    throughout. Waiting on the primary reset is waiting on a signal that never described the
    problem."""
    assert budget.classify("You have exceeded a secondary rate limit") == "secondary"
    assert budget.classify("API rate limit exceeded for user") == "primary"
    assert budget.classify("dial tcp: connection refused") == "other"


def test_a_secondary_message_is_not_misread_as_primary():
    """Every secondary message also contains the words 'rate limit', so order matters."""
    assert budget.classify("secondary rate limit exceeded") == "secondary"


def test_an_unreadable_limit_is_not_a_healthy_budget():
    """Reporting a comfortable number when the check itself failed is this project's defect applied
    to the guard against it."""
    class Boom:
        def get(self, *_):
            raise LookupFailure("403")
    code, msg = budget.check(Boom())
    assert code == 2 and "NOT a statement that there is budget left" in msg


def test_below_the_reserve_holds_and_says_when_it_resumes():
    """A stop with no recovery time is indistinguishable from a dead watch."""
    import time as t
    class Low:
        def get(self, *_):
            return {"resources": {"core": {"remaining": 10, "limit": 5000,
                                           "reset": t.time() + 120}}}
    code, msg = budget.check(Low(), reserve=1500)
    assert code == 1 and "HOLDING" in msg and "resets in" in msg


def test_a_hold_uses_retry_after_for_a_secondary_limit_and_reset_for_a_primary_one():
    now = 1000.0
    assert budget.hold_for(60, secondary=True, retry_after=200, reset_at=now + 9999, now=now) == 200
    assert budget.hold_for(60, secondary=False, retry_after=200, reset_at=now + 300, now=now) == 300


def test_a_hold_never_parks_the_watch_indefinitely():
    """An unreadable or absurd reset must not park a watch forever — a watch that never polls again
    is indistinguishable from a dead one."""
    assert budget.hold_for(60, secondary=False, retry_after=None,
                           reset_at=1000 + 99999, now=1000) == 600


def test_a_hold_is_never_shorter_than_one_interval():
    assert budget.hold_for(300, secondary=False, retry_after=None, reset_at=1000 + 5,
                           now=1000) == 300


# -- the emitter ---------------------------------------------------------------
def test_an_item_is_announced_once(capsys):
    em = watch.Emitter()
    assert em.emit("READY", 9, "t", "d")
    assert not em.emit("READY", 9, "t", "d")
    assert capsys.readouterr().out.count("READY #9") == 1


def test_the_same_event_with_a_different_detail_is_a_different_fact(capsys):
    """This is the MERGED flood, recorded rather than hidden: the detail carries main's sha, so when
    main moves every recently-merged pull request re-emits. Triggered by MERGING — the one act the
    role is there to perform."""
    em = watch.Emitter()
    assert em.emit("MERGED", 9, "t", "main a1")
    assert em.emit("MERGED", 9, "t", "main b2")


# -- the loop ------------------------------------------------------------------
class FailingClient:
    def get(self, *_a, **_k):
        raise LookupFailure("dial tcp: operation timed out")

    def paginate(self, *_a, **_k):
        raise LookupFailure("dial tcp: operation timed out")


def test_a_failing_poll_emits_and_does_not_end_the_watch(capsys):
    """An expired token and a quiet queue look identical otherwise, and a role that cannot tell them
    apart sits idle believing it is finished. The first version of this arm grepped the source and
    passed with the emit deleted, because the string also appears in a comment."""
    calls = {"n": 0}

    def sleeper(_):
        calls["n"] += 1
        if calls["n"] >= 3:
            raise KeyboardInterrupt

    with pytest.raises(KeyboardInterrupt):
        watch.loop("prs", "dev", 1, client_factory=FailingClient, sleeper=sleeper)
    out = capsys.readouterr().out
    # TWO of them: one proves it emits, two prove it did not exit after the first.
    assert out.count("LOOKUP FAILED") >= 2


def test_a_sweep_exits_rather_than_looping(capsys):
    """It is a sweep, not a watch. The fallback that answers the question from scratch has to come
    back."""
    rc = watch.loop("prs", "dev", 1, once=True, client_factory=FailingClient,
                    sleeper=lambda _: pytest.fail("a sweep slept"))
    assert rc == 1
    assert "LOOKUP FAILED" in capsys.readouterr().out


def test_an_unknown_role_refuses(capsys):
    assert watch.main(["prs", "not-a-role"]) == 2


def test_an_unknown_watch_refuses(capsys):
    assert watch.main(["nonsense", "dev"]) == 2


def test_the_heartbeat_arrives_on_the_first_poll_and_every_tenth(capsys):
    """`WATCHING` is not noise. It is the ONLY evidence the watch is still standing — silence
    otherwise means 'no work' and 'I am dead' with the same number of characters. A watcher died
    three times in one session and the role it served sat idle believing its board was clear."""
    class Quiet:
        def get(self, path):
            import time as t
            return {"resources": {"core": {"remaining": 5000, "limit": 5000, "reset": t.time()+60}}}

        def paginate(self, *_a, **_k):
            return []

    n = {"i": 0}

    def sleeper(_):
        n["i"] += 1
        if n["i"] >= 11:
            raise KeyboardInterrupt

    import watch as w
    orig = w.resolve_repo
    w.resolve_repo = lambda *a, **k: "o/r"
    try:
        with pytest.raises(KeyboardInterrupt):
            w.loop("prs", "dev", 1, client_factory=Quiet, sleeper=sleeper)
    finally:
        w.resolve_repo = orig
    out = capsys.readouterr().out
    assert out.count("WATCHING") == 2, f"heartbeat on poll 1 and poll 10 only, got:\n{out}"


# -- main's colour -------------------------------------------------------------
class Runs:
    def __init__(self, runs, jobs=None, boom=False):
        self.runs, self.jobs, self.boom = runs, jobs or [], boom

    def get(self, path):
        if self.boom:
            raise LookupFailure("403")
        if "actions/runs?" in path:
            return {"workflow_runs": self.runs}
        if "/jobs" in path:
            return {"jobs": self.jobs}
        return {}


def test_mains_colour_comes_from_the_push_run_not_its_check_runs():
    """`issue_comment` fires from the default branch, so its jobs — conditioned out for anything but
    a pull request — file themselves as SKIPPED CHECK RUNS against main's head sha, timestamped
    after the real push run. Reading check runs returns 'skipped' for a build that passed and for
    one that failed, identically. The push run is the only place main's real colour survives."""
    st = watch.main_state(Runs([{"status": "completed", "conclusion": "success",
                                 "head_sha": "abcdef1234", "id": 1}]), "o/r")
    assert st.kind == watch.GREEN_MAIN and "GREEN" in st.line


def test_a_running_build_is_not_a_green_one():
    """Three answers, never two. A run still going spelled the same way as green is how a red main
    goes unmentioned."""
    st = watch.main_state(Runs([{"status": "in_progress", "conclusion": None,
                                 "head_sha": "abcdef1234", "id": 1}]), "o/r")
    assert st.kind == watch.RUNNING_MAIN and "still running" in st.line


def test_a_failed_lookup_is_not_a_green_main():
    st = watch.main_state(Runs([], boom=True), "o/r")
    assert st.kind == watch.UNKNOWN and "UNKNOWN" in st.line


def test_no_push_run_at_all_is_unknown_not_green():
    st = watch.main_state(Runs([]), "o/r")
    assert st.kind == watch.UNKNOWN


def test_a_red_main_names_the_failing_check():
    """A bare `(failure)` sends the reader to the Actions tab to find out what the watch had already
    been told."""
    st = watch.main_state(
        Runs([{"status": "completed", "conclusion": "failure", "head_sha": "19f05904aa", "id": 7}],
             jobs=[{"name": "Branch name and commit convention", "conclusion": "failure"},
                   {"name": "Build and tests", "conclusion": "success"}]), "o/r")
    assert st.kind == watch.RED_MAIN
    assert "Branch name and commit convention" in st.line and "Build and tests" not in st.line


def test_a_red_run_with_no_failing_job_returned_is_undetermined_not_empty():
    """A run that is red must have a red job in it; if none came back, the query did not answer and
    must not be rendered as 'nothing was failing'."""
    st = watch.main_state(
        Runs([{"status": "completed", "conclusion": "failure", "head_sha": "aa", "id": 7}],
             jobs=[]), "o/r")
    assert "NOT DETERMINED" in st.failing


def test_a_red_main_does_not_tell_the_last_merger_it_is_theirs():
    """Issue #64, and it was acted on twice. main was red and the alarm was right to fire — but
    19f05904 was a DIRECT PUSH by the framework, one parent, so the merge-commit exemption in the
    naming gate did not apply and its 113-character subject reddened the board. It was nobody's
    merge. Inferring the cause from who merged last stops measuring authorship the moment anything
    else can redden main, and something else can BY DESIGN."""
    st = watch.MainState(watch.RED_MAIN, sha="19f05904", red_sha="19f05904aabbcc")
    assert "NOT DETERMINED" in watch.attribute_red_main(st, "deadbeef0000")


def test_a_red_main_that_IS_your_merge_says_so():
    """The attribution stays possible — it is derived, not abandoned."""
    st = watch.MainState(watch.RED_MAIN, sha="19f05904", red_sha="19f05904aabbcc")
    assert "YOU merged into it" in watch.attribute_red_main(st, "19f05904aabbcc")


def test_no_merge_sha_means_undetermined_rather_than_yours():
    st = watch.MainState(watch.RED_MAIN, sha="19f05904", red_sha="19f05904aabbcc")
    assert "NOT DETERMINED" in watch.attribute_red_main(st, None)
