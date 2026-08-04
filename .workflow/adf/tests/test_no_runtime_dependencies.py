"""The runtime must import nothing but the standard library — asserted, not intended.

A consumer repository installs this framework and must be able to run it with a bare `python3`. The
moment one module imports something from PyPI, every consumer needs a virtualenv on every machine
and on CI before the process can start at all — and a process that cannot start is worse than one
that is more verbose to write. `uv` and pytest exist for THIS repository's tests and nowhere else.
"""

from __future__ import annotations

import ast
import sys
from pathlib import Path

PKG = Path(__file__).resolve().parents[1]
LOCAL = {p.stem for p in PKG.glob("*.py")}


def test_no_module_imports_anything_outside_the_standard_library():
    std = set(sys.stdlib_module_names)
    offenders = []
    for f in sorted(PKG.glob("*.py")):
        tree = ast.parse(f.read_text())
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                names = [a.name for a in node.names]
            elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
                names = [node.module]
            else:
                continue
            for name in names:
                root = name.split(".")[0]
                if root not in std and root not in LOCAL:
                    offenders.append(f"{f.name}: {name}")
    assert not offenders, (
        "these imports would make every consumer repository need a virtualenv before the process "
        f"could start: {offenders}"
    )
