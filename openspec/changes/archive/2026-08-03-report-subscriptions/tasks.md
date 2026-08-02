# Tasks

Every box below is ticked because the work behind it genuinely happened. Lines for work that did not
happen were deleted rather than left unticked; the pull request says which and why.

## Selectors and granularities, `internal/reports`

- [x] `Granularity` with the ordering written once, and a zero value that is not `full`
- [x] `ParseGranularity` — refuses an unknown token by name, never coerces to a neighbour
- [x] `ParseSelector` / `ParseSelectors` — wildcard, dotted paths, negation, comma-separated lists
- [x] All or nothing: one malformed selector refuses the whole list
- [x] `Selector.String` — the canonical form a subscription is stored and read back through
- [x] A negation carries no granularity, and `!channel:full` is refused rather than ignored
- [x] `DefaultGranularity`, written down as one constant because the PRD does not settle it

## Subjects

- [x] A small catalogue: `git`, `git.commit`, `token_usage`, `channel`, `published_notes`
- [x] Root subjects, which the wildcard enumerates, distinguished from narrower dotted paths
- [x] `under` — a subject's activity is its own plus everything beneath it, segment-wise

## The report

- [x] `resolve` — inclusion as a set rule, so exclusion cannot depend on written order
- [x] Most-detailed-wins when two positive selectors match the same subject
- [x] Four per-subject states, pairwise distinct in the rendered bytes
- [x] `renderBody` — one function, switching on the granularity, with no subject in scope
- [x] An unmatched selector is named in the report, and never suppresses the selectors that matched
- [x] A wildcard is never reported as unmatched
- [x] An excluded subject appears at no granularity, including inside its parent's items

## Subscriptions on the local store

- [x] `Save` — parses the whole list before the store is touched; a refusal stores nothing
- [x] `Load` / `List` — re-parsed on read, so a stored selector this build cannot read is said
- [x] `StoreSource` — activity from `activity.<subject>` records, one subject undetermined alone
- [x] `WriteActivity`, so reader and writer agree on the store kind by construction

## The command

- [x] `omw report subscribe | show | list | run | subjects`
- [x] Four exit codes: refusal, unmatched, undetermined, success
- [x] Every operation says whether the daemon is running, and none of them starts it
- [x] The daemon question routes through `daemonLiveness` (Issue #41) — three-valued, no socket
      path derived here, and an undetermined daemon does not change a determined report's exit code
- [x] No hub is read for anything local, and the flow cannot open a connection at all

## Tests, each driven red before being trusted

- [x] The PRD's five examples, each independently
- [x] The ordering as a containment PROPERTY over consecutive pairs, not five literals
- [x] A granularity means the same for every subject: identical activity, identical rendered body
- [x] `*:summary` equals naming every root subject at `summary`, on the rendered bytes
- [x] Refusal, unmatched, quiet day, undetermined and no-hub, pairwise distinct
- [x] A record damaged on disk makes its subject undetermined and never `count: 0`
- [x] A refused list leaves the previously stored subscription byte-identical
- [x] All three liveness answers rendered and driven, the third one distinct in words and reason
- [x] The report flow imports no `net`, `net/http`, `net/url` or `os/exec`, with a walk control
