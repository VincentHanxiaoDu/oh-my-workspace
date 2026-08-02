# Project-specific instructions for the product role

**This file is yours. The installer creates it once and never overwrites it.**

## The specification

**[`PRD.md`](../../PRD.md) is authoritative.** Cite the section when you file or judge an Issue.

## What makes an acceptance criterion good here

- **Bad:** "the daemon reports honestly"
- **Good:** "`omw status` with no daemon running prints `daemon: not running` and exits 0"
- **Good:** "`omw doctor` where FDE cannot be read prints `could not be determined on this platform`,
  distinct from `not enabled`, and exits 0"

This product's sharpest guarantees are negative — nothing implicit, no network without a hub, a
missing thing never rendered as an empty one. **Write criteria for those**, and assert
distinguishability rather than inventing an exit code the PRD never fixed.

## The one thing not to compromise

**The client is useful to one person before it is useful to a company** (PRD §4.4). A capability
that only works once a hub exists is not the first slice.
