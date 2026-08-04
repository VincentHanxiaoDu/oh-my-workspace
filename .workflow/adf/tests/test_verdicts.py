"""Every test here is a defect that reached production in the bash implementation.

The names say what went wrong, not what the function does, so a failure reads as the incident
returning rather than as an assertion being unhappy.
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import verdicts  # noqa: E402


def c(body: str) -> dict:
    return {"body": body}


def block(role: str, sha: str, kind: str = "approve", declared: str | None = None) -> dict:
    return c(f"[{role}]\nReviewed-by: {declared or role}\nReviewed-sha: {sha}\nVerdict: {kind}")


def test_a_quoted_verdict_is_not_a_verdict():
    """A comment QUOTING the template read as a real verdict, under whoever the quote named.

    It happened while asking somebody to re-attest, and it lost only because a genuine verdict was
    posted afterwards. A reviewer pasting the template into an explanation must not certify
    anything.
    """
    quoted = c("[product]\nwhat went wrong was:\n```\nReviewed-by: product\n"
               "Reviewed-sha: cafe\nVerdict: approve\n```")
    assert verdicts.parse([quoted]) == []


def test_a_verdict_that_pastes_command_output_is_still_a_verdict():
    """The inverse, and the reason the fix is 'strip fences' rather than 'reject any comment with
    a fence in it'. A real review that quotes what it drove is a real review."""
    v = verdicts.parse([c("[qa]\nReviewed-by: qa\nReviewed-sha: cafe\nVerdict: approve\n\n"
                          "I ran it:\n```\n$ make ci\nok\n```")])
    assert [x.kind for x in v] == [verdicts.APPROVE]


def test_the_poster_is_the_marker_not_the_name_in_the_text():
    """Every role posts through ONE GitHub account, so `.user.login` would make them all one
    reviewer and switch independence off. The marker is what distinguishes them — and when the two
    disagree that is refused rather than re-attributed to either."""
    v = verdicts.parse([block("qa", "cafe", declared="product")])
    assert v[0].role == "qa" and v[0].declared == "product"
    assert verdicts.disagreements(v) == v


def test_an_unsigned_verdict_is_not_attributed_to_the_name_it_claims():
    """Attributing an unmarked verdict to the name inside it is precisely the forgery the marker
    exists to stop."""
    assert verdicts.parse([c("Reviewed-by: qa\nReviewed-sha: cafe\nVerdict: approve")]) == []


def test_a_self_approve_does_not_erase_an_earlier_refusal():
    """Issue #82. Taking `last` let an author erase an independent refusal by posting a self-approve
    after it — no code change, no new commit, byte-identical to there never having been a refusal."""
    v = verdicts.parse([block("qa", "cafe", "changes-requested"), block("dev", "cafe", "approve")])
    assert verdicts.changes_rounds(v) == 1
    assert any(x.is_changes for x in v)


def test_a_verdict_naming_another_head_does_not_settle_this_one():
    """The sha is what makes a verdict release itself on a push. Without it a review of old code
    certifies new code."""
    v = verdicts.parse([block("qa", "0000")])
    assert verdicts.ruled_on(v, "qa", "0000")
    assert not verdicts.ruled_on(v, "qa", "cafe")


def test_the_review_owner_is_whoever_looked_first():
    """Eleven verdicts in eighteen hours, alternating between two roles, because each push re-opened
    the review to everybody. The first reviewer owns it."""
    v = verdicts.parse([block("qa", "a1", "changes-requested"), block("product", "a2", "approve")])
    assert verdicts.owner(v) == "qa"


def test_rounds_count_every_refusal_not_the_last_one():
    v = verdicts.parse([
        block("qa", "a1", "changes-requested"),
        block("qa", "a2", "changes-requested"),
        block("qa", "a3", "changes-requested"),
    ])
    assert verdicts.changes_rounds(v) == 3


def test_carriage_returns_do_not_hide_a_verdict():
    """Comments arrive over an API that may carry CRLF, and a `$`-anchored match against a line
    ending in \\r matches nothing — which renders every verdict absent."""
    v = verdicts.parse([c("[qa]\r\nReviewed-by: qa\r\nReviewed-sha: cafe\r\nVerdict: approve\r\n")])
    assert v and v[0].sha == "cafe"


def test_an_empty_comment_list_is_no_verdicts_and_says_nothing_else():
    assert verdicts.parse([]) == []
    assert verdicts.owner([]) == ""
    assert verdicts.changes_rounds([]) == 0
