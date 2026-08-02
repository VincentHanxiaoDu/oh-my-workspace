# Tasks

Every box below is ticked because the work behind it genuinely happened. Lines for work that did
not happen were deleted rather than left unticked; the pull request says which and why.

## The version surface in `internal/hub`

- [x] `VersionRef` with `String` / `ParseVersionRef` — one format, round-tripped by test
- [x] `VersionSource` interface, so the undetermined branches can be driven by a failing double
- [x] `VersionView` and `TimelineView`, each carrying whether what they claim was established
- [x] Standing as a `tri.Value`, with three pairwise-distinct sentences in the output itself
- [x] `ListTimeline` — the enumerable timeline; a one-version note is one entry, never empty
- [x] `ReadView` — standing worked out from the timeline, not from the ref the caller passed
- [x] `CurrentView` — an unqualified request, which never falls back to an older version
- [x] `BodyUnreadableLine` and the `BodyKnown` flag, so an unreadable body is not an empty one
- [x] `ErrNoSuchVersion`, `ErrVersionUnreadable`, `ErrBadVersionRef`, pairwise distinct from #12's
- [x] `Archive` — deactivation as a label, never consulted when deciding readability
- [x] `Store.Timeline`, `Store.VersionAt`, `Store.AuthorOf`, all routing through `Store.Read`
- [x] `SearchLatest` — the current version only, each hit naming its version

## The control API

- [x] `TimelineAnswer` / `VersionAnswer`, with `body_known` so an absent body cannot decode as blank
- [x] `TimelineJSON`, `VersionJSON`, `CurrentJSON`, built from the same three functions the CLI uses
- [x] `VersionAPISchema` — three operations, all under the existing `read` scope

## The local half, `internal/drafts`

- [x] An outbox created by an explicit act, at a directory the person names; nothing is conjured
- [x] Append-only revisions on disk, surviving the process that wrote them
- [x] Implements `hub.VersionSource`, so drafts and published notes read alike
- [x] A missing revision, an empty one and an unreadable one are three outcomes

## The command

- [x] `omw note show / versions / read / search / draft / schema`
- [x] No hub configured is said precisely, and nothing is dialled
- [x] The daemon is probed and never started
- [x] An unreachable hub is undetermined, and is not an empty history

## Driving it

- [x] Every acceptance criterion has at least one test that drives it
- [x] The three standings, and the four failure reports, compared pairwise rather than to literals
- [x] `TestNoRetentionMechanism` walks the package's declarations for anything that removes
- [x] Nine mutations applied, each confirmed red naming the defect, each reverted
