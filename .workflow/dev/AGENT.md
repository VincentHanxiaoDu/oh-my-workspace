# Project-specific instructions for the dev role

**This file is yours. The installer creates it once and never overwrites it.**

## This project

**Go**, standard library first. A dependency needs a reason in the pull request body.

The product is `omw`: a local daemon and CLI on each person's machine, and a shared hub. Read
[`PRD.md`](../../PRD.md) — it is authoritative and it is short.

```bash
make ci          # build, vet, test — the gate runs this
go test ./...
```

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
