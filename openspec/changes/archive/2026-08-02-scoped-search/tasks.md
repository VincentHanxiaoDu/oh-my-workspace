# Tasks

## Visibility before ranking

- [x] `Corpus` — the settled set of readable notes, with unexported fields and one constructor
- [x] `Settle` / `SettleWith` — filter through `CanRead` by way of the merged `Store.ListReadable`
- [x] Corpus-relative ranking, so that filtering-before and filtering-after are distinguishable from outside
- [x] Every observable (count, snippet, suggestion, order, score, author facet) derived from the corpus and nothing wider

## Scoping

- [x] `SearchScope` — person, group, company; a search SUBJECT, not a capability
- [x] `ParseSearchScope` — one parser for `company`, `person:<id>`, `group:<id>`
- [x] Unknown person or group refused as `unknown-search-scope`, distinct from a scope that matched nothing
- [x] A directory that cannot answer makes scope existence undetermined, never "no such group"
- [x] Only the latest version of a note is indexed

## The three-valued answer

- [x] An undetermined readability excluded from results AND from corpus statistics, counted separately
- [x] `Outcome.Coverage` — a complete result is distinguishable from an incomplete one of the same size
- [x] `Roster` — three-valued activity, with a nil roster answering undetermined
- [x] `Record.Dissolve` — a real sequence of events that produces an unresolvable readability
- [x] `Corpus.Size` pinned: only the readable are counted, with the unreadable AND the undetermined both present in the fixture
- [x] `Corpus.UndeterminedIDs` pinned: the ids are reported, and the accessor hands out a copy
- [x] `Corpus.Reader` pinned: a corpus reports the reader its exclusions were computed against

## Not narrowed at the edge

- [x] `SearchThrough` requires `read`; a grant without it is refused, not run
- [x] Searching as somebody the grant does not act as is refused, not narrowed to the holder
- [x] The refusal names the scopes the grant DOES hold
- [x] The capability vocabulary is still exactly three names

## The CLI

- [x] `omw search <terms> [--scope] [--as]`
- [x] `omw search scopes` — the search subjects, and the unchanged capability vocabulary
- [x] Found nothing exits 0 and says so; could not run exits 1; could not determine exits 3
- [x] No hub configured is stated precisely and reaches nothing
- [x] The daemon question routed through Issue #41's single answer; no socket knowledge in this package
- [x] Liveness rendered three-valued: undetermined carries its own wording, code and exit status
- [x] The daemon is said to be not running, never started

## Unguessable note ids (the owner's ruling, amended into Issue #15)

- [x] `mintNoteID` — 128 bits from `crypto/rand`, hex, never a counter and never a fallback
- [x] Collision retried then REFUSED, never overwriting a published note
- [x] `ErrIDUnavailable` — its own code, distinguishable from every other refusal
- [x] Issue #12's criterion 12 kept intact: `refused` and `no such note` still differ
- [x] The id space walked in a test and required to yield nothing — no reads and no refusals
- [x] A held id's arithmetic neighbours required not to locate a note hidden from the reader
- [x] Every literal `note-N` in this branch's tests replaced with the id the store actually minted

## Tests

- [x] Control/test corpus pairs over four hidden-visibility cases, compared on the whole rendered output
- [x] Ordering asserted against a literal as well as against the control run
- [x] A guard test proving ranking really is corpus-sensitive, so the leak test cannot pass vacuously
- [x] A guard test proving suggestions work at all when the term is readable
- [x] Every CLI failure mode compared PAIRWISE against every other and against the empty result
- [x] Six mutations driven to red, each naming the defect, recorded in the pull request body
- [x] Merged current `main` and dropped this branch's duplicate `mustPublish` in favour of Issue #11's merged copy
- [x] Every mutation re-driven on the merged tree, because a result from a tree that does not compile is not a result
- [x] The counter restored as a mutation and both id tests watched go red, reproducing the owner's own demonstration
