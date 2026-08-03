# A support bundle that says what it holds and what it withheld

## Why

Something is wrong with a person's client and they need help from whoever supports the product.
Everything on the machine is their material: their tickets, their unpublished drafts, the messages a
channel ingested for them. They want to hand over enough to be diagnosed, and they want to know,
**before they press send**, exactly what they are handing over and what they are not.

PRD §3.9 states both halves in one sentence: **"The bundle states what it contains"**, and it
**"withholds identifying data by default — raw message bodies are not in it unless asked for."**

Those two halves pull against each other. A bundle useful to a supporter and a bundle that does not
hand over a person's material are in tension, and the resolution is not a middle amount of data —
it is that the bundle is **explicit about the trade**. It says what it holds and what it left out,
so the person can decide whether to send it and the supporter knows what they are missing.

Two failure modes follow directly, and they are symmetric:

- **A bundle that silently omits something is a defect.** A supporter reading a bundle with no
  daemon section cannot tell "the daemon was fine" from "we could not look". PRD §4.3 already says a
  state that could not be determined is shown as undetermined, never as a "no" and never as silence.
- **A bundle that silently includes a body is a defect**, and a worse one, because it is
  irreversible the moment it is sent. PRD §2.3's three containers do not leave the machine except by
  publication, and a support bundle is not a publication.

There is a third thing, which is neither: **a credential.** A model key is not a body with a higher
disclosure level; PRD §3.13 puts it outside what is published, synchronised or readable through the
agent API, and a supporter's need to diagnose a client is not a reason to hold it. So the opt-in
that includes bodies does not reach it, at any level.

Finally, the bundle has to work **when the daemon is dead**, because the daemon being dead is the
usual reason to need it — and producing it must start nothing (§4.2) and reach out nowhere (§4.4).

## What Changes

- **A new `internal/diagnostics` package** that produces a bundle: a directory carrying a
  `manifest.json` plus the payload files the manifest names. It imports no transport package at all,
  so it cannot open a connection or start a process.
- **A manifest that is machine-checkable rather than prose.** Every category carries a state
  (`collected` / `withheld` / `undetermined`), a machine-readable reason code, a sentence a person
  can read, an item count, and the bundle-relative files it produced. The category list is fixed and
  exhaustive, seeded before any gathering happens, and a manifest still carrying a seeded entry is
  refused rather than shipped — so a category cannot go silently missing.
- **Withholding as the default.** The default gather never reads a record payload: inventories carry
  ids and sizes. Ticket, draft-note and ingested-message bodies are `withheld` with the reason
  `withheld-by-default`, and are collected only under one explicitly named flag.
- **A credential that no flag reaches.** The model key is `withheld` with the reason
  `never-collected-credential` in every bundle, including the opt-in one, and the opt-in switch is
  wired to exactly three body kinds.
- **Three-valued reporting throughout**, taken from the packages that own each answer rather than
  re-derived: full-disk encryption from `internal/health` in its three values, the store's location
  state from `internal/store` with `undetermined` reported as itself, and daemon liveness from the
  product's single answer in `internal/commands/liveness.go`. The control API's owner-only
  confirmation is its own manifest entry, separate from encryption.
- **Gaps named rather than dropped.** A subsystem that would not answer is `undetermined` with a
  reason. A machine with no store, a machine with no hub, and a capability not in this build each
  get their own reason code, and none of them renders as a negative finding or as an empty
  collection.
- **A new `omw diagnostics <path> [--include-bodies]` command**, added as new files in
  `internal/commands`. One flag, defaulting to off, with no short form and no broader option that
  implies it; every other flag-shaped argument is refused rather than guessed at.
- **An absolute destination, refused otherwise.** A bundle is an artifact the person hands to
  somebody; where it lands must not depend on which directory they were standing in. This is not a
  hypothesis: the suite's sweep over every registered command ran `omw diagnostics list` and the
  first version of the command wrote a real bundle of machine facts into the source tree. PRD §3.9
  does not settle this and the decision is flagged in the pull request.
- **All-or-nothing placement.** The bundle is assembled in a staging directory beside the
  destination and renamed into place as the last act, so a failed run leaves nothing a person could
  mistake for a complete bundle.
- **The negative is driven, not asserted.** A store is seeded with recognisable strings in a ticket
  body, a draft body, an ingested message body and a model key; every file of the default bundle is
  searched for all four and must contain none — and the same search is first pointed at the store
  itself and required to find all four there, so a clean bundle is never a broken search.
