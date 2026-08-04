"""The role names, in one place, because two places disagreed and the disagreement was silent.

Measured (Issue #126, found by driving the refreshed queue against a live board):

    check-naming accepted   ^(dev|qa|product|ops|flow)/<type>/<issue>-<slug>$
    the queue routed with   case "$branch" in "$role"/*)

`flow` was in the gap. A `flow/` branch PASSED the naming gate — green, `Branch name and commit
convention` — and there is no role called `flow`, so the routing skipped it for every role and the
pull request appeared in NOBODY's queue. No error, zero exit code, invisible. It was live on the
pull request that introduced it.

Both lists were individually correct. Nothing compared them.
"""

from __future__ import annotations

# ROLES THAT BUILD: may own a branch, and therefore MUST have a queue that shows them their own
# pull requests. The naming gate's branch pattern is built from this list.
BUILD_ROLES: tuple[str, ...] = ("dev", "qa", "product", "ops", "flow")

# ROLES THAT MAY BE ASKED FOR A QUEUE. The two that are not build roles never appear in a branch
# name because they do not build: `pm` routes and `owner` decides.
ALL_ROLES: tuple[str, ...] = BUILD_ROLES + ("pm", "owner")


def build_roles_alt() -> str:
    """`dev|qa|product|ops|flow` — for the naming gate's regex."""
    return "|".join(BUILD_ROLES)


def is_build_role(role: str) -> bool:
    return role in BUILD_ROLES


def check_lists_agree() -> list[str]:
    """Every build role must be a role a queue answers for. Returns complaints, empty if sound."""
    return [
        f"{r!r} may own a branch but is not a role any queue answers for — a branch named after "
        f"it passes the naming gate and reaches nobody"
        for r in BUILD_ROLES
        if r not in ALL_ROLES
    ]


if __name__ == "__main__":
    import sys
    # PRINTED FOR THE ONE BASH CALLER LEFT. check-naming.sh builds its branch pattern from this, so
    # the two lists cannot drift — which is Issue #126 and the reason this module exists.
    if len(sys.argv) > 1 and sys.argv[1] == "--alt":
        print(build_roles_alt())
    elif len(sys.argv) > 1 and sys.argv[1] == "--self-test":
        problems = check_lists_agree()
        for p in problems:
            print(f"SELF-TEST FAIL: {p}", file=sys.stderr)
        if not problems:
            print("self-test passed: every build role is a role a queue answers for")
        sys.exit(1 if problems else 0)
    else:
        print(" ".join(BUILD_ROLES))
