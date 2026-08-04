"""The gate. Every test is an incident, and the exit codes are five facts rather than pass/fail."""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import check_review as cr  # noqa: E402

HEAD = "cafe1234"
KNOWN = {"cafe1234", "beef0000"}


def knows(sha, cwd=None):
    return sha in KNOWN


def c(role, sha, kind="approve", declared=None):
    return {"body": f"[{role}]\nReviewed-by: {declared or role}\nReviewed-sha: {sha}\n"
                    f"Verdict: {kind}"}


def ev(comments, author_set=frozenset(), policy="independent"):
    return cr.evaluate(HEAD, comments, set(author_set), policy=policy, knows_sha=knows)


def test_an_independent_approve_passes():
    assert ev([c("qa", HEAD)], {"dev"}).code == cr.OK


def test_an_author_cannot_certify_its_own_work():
    assert ev([c("dev", HEAD)], {"dev"}).code == cr.NO_REVIEW


def test_a_review_of_another_head_does_not_certify_this_one():
    """A push invalidates any earlier review; without this a review of old code certifies new code."""
    assert ev([c("qa", "beef0000")], {"dev"}).code == cr.NO_REVIEW


def test_changes_requested_has_its_own_exit_code():
    """A refused review and an absent one are different facts and they SHARED an exit code, so the
    workflow could publish only one description for both. A reviewer that had just refused read
    'No current review by an independent agent' and could not tell its verdict had landed from its
    comment never having been parsed."""
    assert ev([c("qa", HEAD, "changes-requested")], {"dev"}).code == cr.CHANGES


def test_a_self_approve_does_not_erase_an_independent_refusal():
    """Issue #82, live on main. The author pushed nothing, posted an approve after somebody else's
    refusal, and the outcome was byte-identical to there never having been a refusal."""
    r = ev([c("qa", HEAD, "changes-requested"), c("dev", HEAD, "approve")], {"dev"})
    assert r.code == cr.CHANGES
    assert r.refusers == ["qa"]


def test_a_second_reviewers_approve_does_not_clear_the_first_ones_refusal():
    """The sibling of #82: overriding somebody else's judgement by posting after them is the same
    act whoever does it. Only the refuser retires its own refusal — or a push does."""
    r = ev([c("qa", HEAD, "changes-requested"), c("product", HEAD, "approve")], {"dev"})
    assert r.code == cr.CHANGES and r.refusers == ["qa"]


def test_the_refuser_can_withdraw_its_own_refusal():
    """It must stay possible, or a refused branch could never be cleared by the reviewer that
    refused it."""
    r = ev([c("qa", HEAD, "changes-requested"), c("qa", HEAD, "approve")], {"dev"})
    assert r.code == cr.OK


def test_a_quoted_verdict_certifies_nothing():
    """Issue #65. Quoting the template while asking somebody to re-attest read as a real verdict."""
    quoted = {"body": f"[product]\nfor reference:\n```\nReviewed-by: product\n"
                      f"Reviewed-sha: {HEAD}\nVerdict: approve\n```"}
    assert ev([quoted], {"dev"}).code == cr.NO_REVIEW


def test_a_verdict_whose_poster_and_declared_name_disagree_is_refused_not_reattributed():
    """Quietly correcting the name would let an attempt to certify one's own work under another
    name pass unremarked, and the attempt is the thing worth seeing."""
    r = ev([c("qa", HEAD, declared="product")], {"dev"})
    assert r.code == cr.NO_REVIEW
    assert any("THE TWO DISAGREE" in m for m in r.messages)


def test_a_verdict_naming_a_sha_this_repository_does_not_know_is_reported():
    """Issue #84, live on #38: a refusal named e7e1368a7fbd… while the head was e7e1368a3673… —
    eight shared hex characters, which everyone who read them side by side read as equal. The gate
    published success 28 seconds later."""
    r = ev([{"body": f"[qa]\nReviewed-by: qa\nReviewed-sha: 0ddba11\nVerdict: changes-requested"}],
           {"dev"})
    assert r.code == cr.UNPLACEABLE
    assert r.unplaceable == [("qa", "0ddba11")]


def test_an_unplaceable_verdict_does_not_beat_a_landed_refusal():
    """A landed refusal is concrete, already red, and already tells the author what to do. The
    unplaceable notice still prints above it, so nothing is lost by not owning the code."""
    r = ev([
        {"body": f"[qa]\nReviewed-by: qa\nReviewed-sha: 0ddba11\nVerdict: approve"},
        c("product", HEAD, "changes-requested"),
    ], {"dev"})
    assert r.code == cr.CHANGES
    assert r.unplaceable  # still reported


def test_an_unplaceable_verdict_beats_a_pass():
    """A pass while somebody's verdict lies unplaced is the whole of #84 — the harm is the
    merge-eligibility, not the quietness on its own."""
    r = ev([
        {"body": f"[qa]\nReviewed-by: qa\nReviewed-sha: 0ddba11\nVerdict: changes-requested"},
        c("product", HEAD, "approve"),
    ], {"dev"})
    assert r.code == cr.UNPLACEABLE


def test_an_ordinary_stale_review_is_silent():
    """A sha this repository KNOWS is an ordinary stale review. Announcing those would bury the one
    that matters under noise on every branch that was ever pushed to."""
    r = ev([c("qa", "beef0000"), c("product", HEAD)], {"dev"})
    assert r.code == cr.OK and not r.unplaceable


def test_self_review_passes_only_where_the_policy_allows_it_and_says_what_it_was():
    r = ev([c("dev", HEAD)], {"dev"}, policy="self-allowed")
    assert r.code == cr.SELF_REVIEWED
    assert any("SELF-review, never as an independent one" in m for m in r.messages)


def test_widening_who_may_certify_never_widens_what_counts_as_certified():
    """A self-allowed repository must still not merge over an outstanding refusal."""
    r = ev([c("qa", HEAD, "changes-requested"), c("dev", HEAD, "approve")], {"dev"},
           policy="self-allowed")
    assert r.code == cr.CHANGES


def test_a_missing_policy_file_is_the_strict_rule(tmp_path):
    assert cr.review_policy(tmp_path / "nope") == "independent"


def test_a_misspelt_policy_is_the_strict_rule(tmp_path):
    """`self_allowed` is a typo somebody will make, and reading it permissively would relax the one
    gate in this process that is not mechanical."""
    p = tmp_path / "policy"
    p.write_text("self_allowed\n")
    assert cr.review_policy(p) == "independent"
    p.write_text("  Self-Allowed \n")
    assert cr.review_policy(p) == "self-allowed"


def test_a_missing_comments_file_is_a_lookup_failure_not_an_absent_review(tmp_path, capsys):
    code = cr.main([HEAD, str(tmp_path / "nope.json")])
    assert code == cr.NO_REVIEW
    assert "LOOKUP FAILURE" in capsys.readouterr().err


def test_unreadable_json_is_a_lookup_failure_too(tmp_path, capsys):
    """`gh issue list` was observed returning an EMPTY FILE rather than an error when a quota ran
    out, and the caller read it as 'no comments'."""
    f = tmp_path / "c.json"
    f.write_text("")
    assert cr.main([HEAD, str(f)]) == cr.NO_REVIEW
    assert "LOOKUP FAILURE" in capsys.readouterr().err


def test_a_mistyped_flag_refuses_rather_than_doing_something_adjacent(capsys):
    assert cr.main(["--self-tests"]) == cr.USAGE
    assert "typo" in capsys.readouterr().err
