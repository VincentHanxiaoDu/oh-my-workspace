"""The queue. Every test is a round somebody actually lost."""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import queue as q  # noqa: E402
from gh import LookupFailure  # noqa: E402


class FakeClient:
    """Answers exactly what was asked and COUNTS IT, because the call count is a reported number."""

    def __init__(self, data, fail=()):
        self.data, self.fail, self.calls = data, fail, []

    def paginate(self, path, per_page=100):
        self.calls.append(path)
        for frag in self.fail:
            if frag in path:
                raise LookupFailure(f"HTTP 403: secondary rate limit ({path})")
        for key, val in self.data.items():
            if key in path:
                return val
        return []


def issue(n, title="t", labels=(), body=""):
    return {"number": n, "title": title, "body": body,
            "labels": [{"name": x} for x in labels]}


def pr(n, ref, sha="cafe", title="t"):
    return {"number": n, "head": {"ref": ref, "sha": sha}, "title": title, "merged_at": None}


def comment(role, sha, kind="approve"):
    return {"body": f"[{role}]\nReviewed-by: {role}\nReviewed-sha: {sha}\nVerdict: {kind}"}


def board(**data):
    return q.Board(client=FakeClient(data), repo="o/r")


def text(role, b):
    return "\n".join(q.role_queue(b, role))


def test_an_unknown_role_is_refused_not_given_an_empty_queue():
    """An empty queue is indistinguishable from 'you have no work', which is the defect this
    project is about."""
    with pytest.raises(ValueError):
        q.role_queue(board(), "not-a-role")


def test_every_role_that_may_own_a_branch_has_a_queue():
    """Issue #126: `flow/` passed the naming gate and reached NO role's queue — green, zero exit,
    invisible. Both lists were individually correct and nothing compared them."""
    import roles
    for r in roles.BUILD_ROLES:
        out = text(r, board())
        assert "YOUR PULL REQUESTS" in out or "PULL REQUESTS TO" in out, r
        assert "NO VERDICT ON THE CURRENT HEAD" in out, f"{r} is never shown its own unreviewed work"


def test_a_failed_lookup_is_not_an_empty_queue():
    """The whole reason this module raises rather than returning empty."""
    b = q.Board(client=FakeClient({}, fail=("issues?state=open",)), repo="o/r")
    with pytest.raises(LookupFailure):
        q.role_queue(b, "dev")


def test_an_unreviewed_pull_request_is_its_authors_work_and_nobodys_else():
    b = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x")]})
    assert "NO REVIEW HAS HAPPENED" in text("dev", b)
    b2 = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x")]})
    out = text("qa", b2)
    section = out.split("NO VERDICT ON THE CURRENT HEAD")[1]
    assert "NO REVIEW HAS HAPPENED" not in section


def test_a_verdict_on_the_current_head_settles_it():
    b = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x", "cafe")],
                 "issues/9/comments": [comment("qa", "cafe")]})
    out = text("dev", b)
    assert "NO REVIEW HAS HAPPENED" not in out and "re-review by" not in out


def test_a_verdict_naming_another_head_does_not():
    """A push makes every prior verdict stale and the work reappears — which is what stops a review
    stranding the head it does not describe."""
    b = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x", "cafe")],
                 "issues/9/comments": [comment("qa", "0000")]})
    assert "re-review by qa" in text("dev", b)


def test_three_rounds_escalate_and_two_do_not():
    """One pull request took eleven verdicts and never converged. A disagreement that survives three
    reviews is a question about what the project wants, and no further round can answer it."""
    three = [comment("qa", f"a{i}", "changes-requested") for i in range(3)]
    b = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x")], "issues/9/comments": three})
    out = text("dev", b)
    assert "ESCALATED" in out and "do NOT push again" in out

    two = three[:2]
    b2 = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x")], "issues/9/comments": two})
    assert "ESCALATED" not in text("dev", b2)


def test_an_escalation_reaches_product_and_nobody_else():
    """An escalation nobody is holding is the orphan defect one level up."""
    three = [comment("qa", f"a{i}", "changes-requested") for i in range(3)]
    b = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x")], "issues/9/comments": three})
    assert "DID NOT CONVERGE" in text("product", b)
    b2 = board(**{"pulls?state=open": [pr(9, "dev/feat/9-x")], "issues/9/comments": three})
    assert "DID NOT CONVERGE" not in text("qa", b2)


def test_an_open_pull_request_is_the_claim_and_a_branch_is_not():
    """Branches outlive their merges, so a branch-based claim never expired and an Issue whose work
    had shipped stayed marked 'somebody is on it' forever."""
    b = board(**{"issues?state=open": [issue(9, labels=("type:feature",))],
                 "pulls?state=open": [pr(1, "dev/feat/9-x")]})
    claimed = q.claimed_issues(b)
    assert 9 in claimed


def test_the_owner_is_told_undetermined_rather_than_handed_silence():
    """'No blockers' and 'nobody has looked' are different answers, and this is the page where
    confusing them is most expensive."""
    out = text("owner", board(**{"issues?state=open": [issue(5, labels=("type:bug",))]}))
    assert "UNDETERMINED" in out


def test_the_owner_is_told_when_something_blocks_the_release():
    b = board(**{"issues?state=open": [issue(5, "blocker", ("blocks:release",))]})
    out = text("owner", b)
    assert "BLOCKED" in out and "#5" in out


def test_a_decision_with_a_ruling_is_no_longer_waiting():
    """The `## Blocked on a decision` heading stays in the body forever — it is the record of what
    was asked — so a ruling posted underneath left the Issue sitting in 'waiting'."""
    body = "## Blocked on a decision\nwhich way?"
    data = {"issues?state=open": [issue(7, "q", ("type:feature",), body)],
            "issues/comments": [{"body": "[owner-ruling] do it this way",
                                 "issue_url": "https://api.github.com/repos/o/r/issues/7"}]}
    assert "#7" not in text("product", board(**data)).split("DECISIONS RAISED")[1]


def test_the_repository_comments_are_paginated_once_not_three_times():
    """Three roles and four watches share 5,000 an hour; 246 rate-limit refusals were counted in one
    day, with the queue reporting LOOKUP FAILED for polls its own polling had made impossible."""
    c = FakeClient({"issues?state=open": [issue(5, labels=("type:feature",))]})
    b = q.Board(client=c, repo="o/r")
    q.role_queue(b, "product")
    assert sum(1 for p in c.calls if p.endswith("issues/comments")) <= 1


def test_every_section_a_role_prompt_promises_is_a_section_the_queue_produces():
    """A PROMPT THAT DESCRIBES MACHINERY THAT IS NOT THERE IS A RULE NOTHING ENFORCES.

    Found by differential-testing the port against the shell it replaced: product-workflow.md said
    "Your queue has two sections that are exactly this, and neither is optional — DECISIONS ONLY YOU
    CAN MAKE …", and `queue.sh product` HAD NO SUCH SECTION. Only the owner arm did. product was
    told, every round, to read something that did not exist, and to route the owner's decisions from
    a list it was never shown.

    check-prompts.sh could not catch it: it asserts what the prompts SAY, and both halves were
    internally consistent — the prompt promised a section and the queue produced a different set.
    Only asking both the same question finds it.

    SCOPED TO THE SENTENCE THAT MAKES THE PROMISE. A prompt mentions plenty of shouted strings that
    are watch events rather than queue headings (`SUPERVISOR DIED`, `LOOKUP FAILED`), and asserting
    over all of them would fail for a reason that has nothing to do with this defect — the shape of
    wrong check this project keeps finding in itself.
    """
    import re
    from pathlib import Path

    cmds = Path(__file__).resolve().parents[3] / ".claude" / "commands"
    if not cmds.is_dir():
        return
    for role, prompt in (("product", "product-workflow.md"), ("dev", "dev-workflow.md"),
                         ("qa", "qa-workflow.md")):
        f = cmds / prompt
        if not f.is_file():
            continue
        text = f.read_text()
        m = re.search(r"[Yy]our queue has .{0,40}sections?.{0,120}?:\n(.*?)\n\n", text, re.S)
        if not m:
            continue
        produced = "\n".join(q.role_queue(board(), role))
        for heading in re.findall(r"`([A-Z][A-Z ,\'\u2019\u2014-]{10,})`", m.group(1)):
            assert heading.strip() in produced, (
                f"{prompt} tells {role} its queue has a section '{heading.strip()}', and "
                f"`queue.py {role}` does not produce it. The role is being sent, every round, to "
                f"read something that is not there."
            )
