#!/usr/bin/env python3
"""Who built this pull request — the command-line face of authors.py.

THE EXIT CODES ARE THE CONTRACT and they are not a pass and a fail:

    0  the answer follows on stdout (possibly empty, and that emptiness is DETERMINED)
    1  role query only: that role authored nothing here
    2  usage
    3  COULD NOT DETERMINE. Not an author set. Independence cannot be established in either
       direction, so a caller must stop rather than route on it.

Issue #79 is the reason 3 exists. `--pr` printed nothing and exited 0 under a secondary rate limit,
the queue read that as "nobody built it, so every role is independent", and offered a pull request
carrying nine `Agent: dev` trailers to dev. The gate re-derives from git at verdict time, where the
lookup does not fail, so it saw dev and refused the review dev had just done.

Usage: authors_cli.py --pr <n> [role] [--all-trailers]
       authors_cli.py --range <base> <head> [role] [--all-trailers]
       authors_cli.py --is-spec-only        (file list on stdin)
       authors_cli.py --self-test
"""

from __future__ import annotations

import sys
from pathlib import Path

import authors
from gh import Client, LookupFailure, resolve_repo

UNDETERMINED = 3


def main(argv: list[str]) -> int:
    if not argv:
        print(__doc__.split("Usage:")[-1].strip(), file=sys.stderr)
        return 2
    if argv[0] == "--self-test":
        return _self_test()

    all_trailers = "--all-trailers" in argv
    argv = [a for a in argv if a != "--all-trailers"]

    if argv[0] == "--is-spec-only":
        files = [ln.strip() for ln in sys.stdin.read().split("\n") if ln.strip()]
        return 0 if authors.is_spec_only(files) else 1

    try:
        if argv[0] == "--pr":
            if len(argv) < 2:
                return 2
            commits = authors.from_pr(Client(), resolve_repo(), int(argv[1]))
            want = argv[2] if len(argv) > 2 else ""
        elif argv[0] == "--range":
            if len(argv) < 3:
                return 2
            commits = authors.from_range(argv[1], argv[2])
            want = argv[3] if len(argv) > 3 else ""
        else:
            print(f"::error::unknown option '{argv[0]}'. This is a typo, not an argument — "
                  f"refusing.", file=sys.stderr)
            return 2
    except LookupFailure as e:
        # EXIT 3, NEVER AN EMPTY LIST ON EXIT 0. This is the whole of #79.
        print(f"::error::who built this could not be determined: {e.reason}", file=sys.stderr)
        print("  This is a LOOKUP FAILURE and NOT a statement that nobody authored it.",
              file=sys.stderr)
        return UNDETERMINED

    if all_trailers:
        names = authors.all_trailers(commits)
    else:
        try:
            names = authors.authors_of(commits)
        except authors.NoTrailers:
            # NO TRAILER AT ALL is an answer — a commit defect the naming gate reports with its
            # remedy — and it is NOT the same answer as "all spec-only". The caller tells them apart
            # by asking again with --all-trailers, which is why that flag exists.
            names = set()

    if want:
        return 0 if want in names else 1
    for n in sorted(names):
        print(n)
    return 0


def _self_test() -> int:
    import subprocess as sp
    here = Path(__file__).resolve().parent
    r = sp.run(["uv", "run", "--isolated", "--no-project", "--with", "pytest",
                "pytest", str(here / "tests"), "-q"],
               capture_output=True, text=True, cwd=here, check=False)
    if r.returncode == 0:
        print("self-test passed: a spec-only commit confers no authorship, a code commit does, a "
              "commit NAMED archive that carries code still does, blank lines cannot change the "
              "predicate, and a lookup that could not answer exits 3 rather than reporting an "
              "empty author set")
        return 0
    print(r.stdout or r.stderr, file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
