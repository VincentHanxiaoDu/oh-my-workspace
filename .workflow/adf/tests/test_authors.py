"""Independence, and the three empty answers that are not the same answer."""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import authors  # noqa: E402
from gh import LookupFailure  # noqa: E402


def commit(agent: str | None, *files: str, sha: str = "c1", subject: str = "chore: x"):
    msg = subject + ("\n\nAgent: " + agent if agent else "")
    return authors.Commit(sha=sha, message=msg, files=files)


def test_a_spec_only_commit_confers_no_authorship():
    """product must archive onto the branch before merging, so it authors every feature pull
    request. Measured: eleven open, eight red, exactly one role left eligible to review any of
    them — a single point of failure by construction, and it failed."""
    cs = [commit("dev", "internal/a.go"), commit("product", "openspec/specs/x/spec.md")]
    assert authors.authors_of(cs) == {"dev"}


def test_a_commit_NAMED_archive_that_carries_code_still_confers_authorship():
    """The exemption is earned by the diff, never by the subject line. Exempting on the message
    would carry code through the one gate in this process that is not mechanical, by naming a commit
    after a thing it is not."""
    cs = [commit("product", "openspec/specs/x/spec.md", "internal/a.go",
                 subject="chore(openspec): archive the thing")]
    assert authors.authors_of(cs) == {"product"}


def test_no_trailers_at_all_is_not_nobody_authored_it():
    """Two empty answers that are different facts: a commit defect the naming gate reports, versus
    a determined 'nobody authored product judgement, so every role is independent'."""
    with pytest.raises(authors.NoTrailers):
        authors.authors_of([commit(None, "internal/a.go")])


def test_all_spec_only_is_a_DETERMINED_empty_author_set():
    cs = [commit("product", "openspec/specs/x/spec.md")]
    assert authors.authors_of(cs) == set()
    assert authors.all_trailers(cs) == {"product"}


def test_a_commit_with_no_files_is_not_spec_only():
    """An empty file list is what an UNREADABLE one looks like, and `all()` over an empty sequence
    is True — so this would have turned a failed lookup into an exemption."""
    assert not commit("product").spec_only
    assert not authors.is_spec_only([])


def test_a_failed_api_lookup_cannot_be_received_as_an_author_set(monkeypatch):
    """Issue #79, and the reason for the port. `--pr` printed nothing and exited 0 under a secondary
    rate limit; the queue read that as 'nobody built it' and offered a pull request carrying nine
    `Agent: dev` trailers to dev. Here the failure is an exception — there is no value to mistake."""
    class Boom:
        def paginate(self, *_a, **_k):
            raise LookupFailure("HTTP 403: You have exceeded a secondary rate limit")

    with pytest.raises(LookupFailure):
        authors.from_pr(Boom(), "o/r", 9)


def test_an_empty_commit_list_from_the_api_is_a_failed_lookup():
    """GitHub does not create a pull request with no commits, so this is a truncated read."""
    class Empty:
        def paginate(self, *_a, **_k):
            return []

    with pytest.raises(LookupFailure):
        authors.from_pr(Empty(), "o/r", 9)


def test_an_unreachable_base_is_a_failed_lookup_not_an_empty_range(tmp_path):
    """`git rev-list bad..head` exits non-zero; reading its empty stdout as 'no commits' would
    certify a pull request whose commits were never examined."""
    import subprocess
    subprocess.run(["git", "init", "-q"], cwd=tmp_path, check=True)
    with pytest.raises(LookupFailure):
        authors.from_range("nosuchref", "HEAD", cwd=str(tmp_path))


def test_a_blank_line_cannot_change_the_predicate():
    """A leading blank line once flipped an exemption, so the gate and the routing gave different
    answers on different git versions and a reviewer withdrew a verdict it had posted."""
    c = authors.Commit(sha="c1", message="\n\nchore: x\n\nAgent: dev\n\n", files=("internal/a.go",))
    assert c.agent == "dev"


def test_carriage_returns_do_not_hide_a_trailer():
    c = authors.Commit(sha="c1", message="chore: x\r\n\r\nAgent: qa\r\n", files=("a.go",))
    assert c.agent == "qa"
