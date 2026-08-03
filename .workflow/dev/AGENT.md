# Project-specific instructions for the dev role

**This file is yours. The installer creates it once and never overwrites it.**

## Sign every comment `[dev]`, on the first line, alone

**Every comment on an Issue or a pull request begins with `[dev]` on its own first line** — before any
heading, bold or prose. `queue.sh` matches `startswith("[dev]")` to work out what this role has
already looked at. A trailing `Agent: dev`, a `[dev-agent]`, or a name in prose is **invisible to it**,
and the queue then offers the same work to the next agent as though nobody had touched it.

This is recorded here, and not only in the role prompt, because the prompt lives in `.claude/commands/`
— framework-owned, replaced wholesale on every refresh, and the rule was historically present in only
some of the role prompts. Measured on this board: **100 comments, `[dev]` appeared zero times.** Every
comment this role had posted was unattributable and unseen by the queue, and the work was re-offered.

**A review verdict carries both** — `[dev]` on the first line, and the `Reviewed-by:` /
`Reviewed-sha:` / `Verdict:` block the gate parses. `Reviewed-sha:` is the **full 40-character** sha;
a short one does not parse. Neither substitutes for the other: one says who is speaking, the other
says what the verdict is.

## This project

**Go**, standard library first. A dependency needs a reason in the pull request body.

The product is `omw`: a local daemon and CLI on each person's machine, and a shared hub. Read
[`PRD.md`](../../PRD.md) — it is authoritative and it is short.

```bash
make ci          # build, vet, test — the gate runs this
go test ./...
```

## Which paths cannot hold your fix

**Read this before you build a machinery Issue.** These paths are installed by agent-dev-flow and
are **replaced wholesale by the next `install.sh` run**. A fix committed here is undone silently, as
a normal part of refreshing the framework — not by a bug, but by the installer doing what it
documents:

| Path | Owner |
| --- | --- |
| `.workflow/bin/` | framework — replaced |
| `.github/` | framework — replaced |
| `.claude/commands/` | framework — replaced (and gitignored here) |
| `.workflow/<role>/AGENT.md` | **yours** — created once, never overwritten |
| `internal/`, `openspec/`, `Makefile`, `PRD.md` | **yours** |

This is not hypothetical: #52 was found here, fixed here, reviewed here and merged here as #54, and
the next install deleted it — 2 insertions, 24 deletions, no warning (#58).

**So a fix that must survive an upgrade goes in a project-owned file.** In practice:

- **An assertion about the machinery** goes in `internal/machinery/` as a Go test. It runs under
  `make ci`, which the `Build and tests` gate invokes, so a refresh that reintroduces the defect
  turns this repository's suite red instead of quietly restoring it. Execute the installed script —
  do not reimplement it, or the test goes green while the real gate is broken.
- **An instruction to a role** goes in that role's `.workflow/<role>/AGENT.md`.
- **A change to the machinery itself** belongs upstream in agent-dev-flow. Send it there, reinstall,
  and then declare the local commit in `internal/machinery/framework-local-commits.txt` with the
  reason it is reconciled.

`TestNoUndeclaredLocalCommitsOnFrameworkOwnedPaths` enforces this: it names any framework-owned file
whose most recent commit is one of ours rather than a refresh. It fails on a name, not a count, so
you can act on it.

**It cannot answer on CI, and it says so rather than passing.** The job that runs `make ci` checks
out at depth 1, so the history it needs is not there and it *skips with a reason*. **Run `make ci`
locally in a full clone** — a green CI is not evidence this check looked. Anything else you add here
that walks `git log` has the same problem; probe `git rev-parse --is-shallow-repository` and skip,
do not assume.

## Conventions a newcomer gets wrong here

- **`could not determine` and `determined to be nothing` are different values** — a product rule,
  not a style one. `omw doctor` answers `enabled` / `not enabled` / `could not be determined on this
  platform`, and the third is a real answer. A `(bool, error)` whose error is dropped has broken it.
- **Nothing is implicit.** No command starts the daemon. No network call without a hub configured.
  The store is created by an explicit act and never conjured.
- **The daemon stops when it cannot write** rather than continuing in a state a person reads as healthy.
- **The control API refuses to open if it cannot prove its socket is owner-only.** That refusal is
  the feature.
- **A note is in the outbox or published, never both and never neither** (PRD §3.11).
- **`openspec/specs/**` is generated.** Change the spec inside the change; archiving regenerates it.
