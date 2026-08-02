# Project-specific instructions for the ops role

**This file is yours. The installer creates it once and never overwrites it.**

**Platforms are an open question** (PRD §5.1): the control API must prove its socket is owner-only,
which is straightforward on Unix domain sockets and unresolved on Windows. Until the owner rules,
say on every release which platforms were actually built and tested.

Product decides a release; you execute. The notes name what CI actually saw on the tagged sha — a
merge commit is not the commit that was reviewed.
