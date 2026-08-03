# Tasks

## The process

- [x] `cmd/omw-hub` — create, serve, describe, what-i-can-read; the directory is always an argument
- [x] `hubd.Create` / `hubd.Open` — an explicit act makes a hub; opening one that is not a hub creates nothing
- [x] `hubd.Run` — exit codes, with `could not determine` on its own code, apart from success and failure
- [x] `serve` runs until it is stopped, and says on the way up that this build has no transport
- [x] Nothing in the package imports `net`, `net/http`, `net/rpc` or `os/exec`, asserted on the source

## The durable corpus

- [x] Append-only, fsynced record of publications, amendments, re-scopings, people, groups and revocations
- [x] `hub.RestoreNote` — a note comes back under the id it was minted with, checked against the same visibility rules
- [x] `hub.RestoreVersion` — a replayed amendment keeps the time it was written, never the restart's clock
- [x] Replay is total: an entry this build cannot honour stops the hub rather than being skipped
- [x] A truncated final line is reported, not swallowed, and everything before it is held
- [x] A record that cannot be read stops the hub, and never becomes an empty corpus

## Visibility, search and statistics

- [x] Every question goes through the existing `hub.CanRead` / `Corpus`; no second rule
- [x] A note narrowed away from a reader leaves their rendered results byte-identical, with the control stated
- [x] Three search scopes and no fourth; an unresolvable subject is refused, not widened and not emptied
- [x] A refusal is a refusal — never an empty outcome and never a count of zero

## Authentication

- [x] Every operation takes token material; no operation takes a person
- [x] An unidentified caller is refused explicitly, before any readability is evaluated
- [x] Sign-in goes through the real device-code flow, approval included; nothing signs in silently
- [x] Sessions are listable by their person, and revocation is written down and replayed at start-up

## Honesty

- [x] `hubd.OperatorReach`, built from `hub.RestrictionStatement`, carried by every hub surface
- [x] Checked with the product's own `hub.CheckSurface` rule rather than by asserting a phrase
- [x] A halted hub answers nothing, reads included, and says it halted

## Out of scope, stated rather than listed as unfinished work

These are NOT tasks of this change and are deliberately not checkboxes — a box left unticked reads
as work this change dropped, and a box ticked to clear a gate is worse. They are named here so that
nobody has to infer them from what is missing.

**The client-to-hub transport** is Issue #104, a sibling of #103, and this change does not start it.
Nothing here imports `net` and a test asserts that on the source. The consequence is stated on every
surface that could mislead: `omw-hub serve` says on start-up that it is not reachable over a network,
and `omw` continues to answer that the hub could not be reached — which is UNDETERMINED, never "there
is nothing there".

**Durable session material** is not decided here. Sessions live in the hub process, so a restart ends
every one of them; the hub then answers a determined "no such token", which renders as "not signed
in" and never as undetermined. That is a real answer rather than a silence, and a test drives it.
Where a hub keeps token secrets is a decision about secrets at rest and deserves to be made on
purpose rather than as a side effect of adding durability to the corpus.
