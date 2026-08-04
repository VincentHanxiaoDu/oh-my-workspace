"""Reading review verdicts out of comments — ONE implementation, for the gate and the queue both.

WHY ONE. The gate decides whether a pull request may merge; the queue decides who is asked to look
at it. When those two derive the same fact separately they drift, and the drift is silent: a verdict
visible to one and not the other means the queue offers work the gate will refuse, or the gate
refuses work the queue says is done. Both were observed.

EVERY RULE BELOW IS A MEASURED FAILURE.

  - FENCED AND QUOTED TEXT IS DISCARDED BEFORE ANYTHING IS PARSED. A comment QUOTING the verdict
    template read as a real verdict, attributed to whoever the quote happened to name. It was done
    by accident while asking somebody to re-attest, and it lost only because a genuine verdict came
    afterwards.
  - THE REVIEWER IS THE POSTER, NOT THE NAME IN THE TEXT. `.user.login` is deliberately NOT used:
    every role posts through the SAME GitHub account, so it would make all of them one reviewer and
    switch independence off entirely. The `[role]` marker on the first line is what distinguishes
    them, and it is A CONVENTION, NOT AN AUTHENTICATED FACT. This closes the accident; it does not
    make a verdict unforgeable by a role that sets out to forge one. Anything stronger needs
    distinct posting identities, which is not this file's to decide.
  - EVERY VERDICT FOR A HEAD IS READ, NOT ONLY THE LAST (Issue #82). Taking `last` let an author
    erase an independent refusal by posting a self-approve after it, with no code change and no new
    commit — byte-identical to there never having been a refusal.
  - THE SHA IS PART OF THE VERDICT, which is what makes it release itself: a push moves the head,
    every prior verdict stops matching, and the work reappears. It errs toward "not reviewed",
    which is the safe direction.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

_FENCE = re.compile(r"^[ \t]{0,3}(```|~~~)")
_MARKER = re.compile(r"^\[([A-Za-z][A-Za-z0-9_-]*)\][ \t]*$")
_FIELD = {
    "by": re.compile(r"^Reviewed-by:[ \t]*(.*?)[ \t]*$"),
    "sha": re.compile(r"^Reviewed-sha:[ \t]*(.*?)[ \t]*$"),
    "verdict": re.compile(r"^Verdict:[ \t]*(.*?)[ \t]*$"),
}

APPROVE = "approve"
CHANGES = "changes"


@dataclass(frozen=True)
class Verdict:
    role: str      # who posted it — the [role] marker, not the name in the text
    declared: str  # the name the text claims, which must agree with `role`
    sha: str
    kind: str      # APPROVE | CHANGES

    @property
    def is_changes(self) -> bool:
        return self.kind == CHANGES


def strip_fences(body: str) -> str:
    """Remove fenced blocks. A `> `-quoted verdict is not one either, because the field patterns
    below are anchored to the start of a line."""
    out, inside = [], False
    for line in body.split("\n"):
        if _FENCE.match(line):
            inside = not inside
            continue
        if not inside:
            out.append(line)
    return "\n".join(out)


def parse(comments: list[dict]) -> list[Verdict]:
    """Every verdict in these comments, oldest first. Malformed ones are skipped, not guessed at."""
    found: list[Verdict] = []
    for c in comments:
        raw = (c.get("body") or "").replace("\r", "")
        if not raw:
            continue
        first = raw.split("\n", 1)[0]
        m = _MARKER.match(first.strip())
        role = m.group(1) if m else ""
        lines = strip_fences(raw).split("\n")
        fields: dict[str, str] = {}
        for key, pat in _FIELD.items():
            for line in lines:
                if (fm := pat.match(line)) is not None:
                    fields[key] = fm.group(1)
                    break
        if "by" not in fields or "verdict" not in fields:
            continue
        # A VERDICT WITH NO `[role]` MARKER CANNOT BE ATTRIBUTED, and attributing it to the name in
        # its own text is exactly the forgery the marker exists to stop. Skipped, and the gate
        # reports it separately — silence here would make an unsigned verdict look absent.
        if not role:
            continue
        kind = CHANGES if fields["verdict"].lower().startswith("changes") else APPROVE
        found.append(Verdict(role=role, declared=fields["by"], sha=fields.get("sha", ""), kind=kind))
    return found


def owner(verdicts: list[Verdict]) -> str:
    """Whose review this is: the role that took it first, and it keeps it.

    THIS IS THE ANTI-PING-PONG RULE. Measured on one pull request: eleven verdicts in eighteen
    hours — seven changes-requested and four approve, alternating between two roles, 32 comments,
    still open and unmerged with every check green. A changes-requested costs a push, a push moves
    the head, and a moved head re-opened the review to EVERY independent role, so each round was
    judged by a different agent against a different standard, raising findings the previous one had
    considered and passed. Nobody was wrong and it did not converge.
    """
    return verdicts[0].role if verdicts else ""


def changes_rounds(verdicts: list[Verdict]) -> int:
    """How many times this was sent back. Three is not a review any more, it is a decision."""
    return sum(1 for v in verdicts if v.is_changes)


def ruled_on(verdicts: list[Verdict], role: str, sha: str) -> bool:
    """Has this role already ruled on THIS EXACT head?"""
    return any(v.role == role and v.sha == sha for v in verdicts)


def disagreements(verdicts: list[Verdict]) -> list[Verdict]:
    """Verdicts whose `[role]` marker and `Reviewed-by:` name each other differently.

    REFUSED, NOT RE-ATTRIBUTED TO EITHER. The two disagreeing is either a copied template or a
    mistake, and both readings are guesses.
    """
    return [v for v in verdicts if v.declared and v.declared != v.role]
