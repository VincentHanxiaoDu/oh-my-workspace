# Tasks

## Statistics over the settled corpus, never wider

- [x] `Corpus.Statistics` — a method on the visibility-settled corpus, with no `*Store` parameter anywhere in the new code
- [x] Counts derived per-reader, driven by two readers whose readable sets differ
- [x] Recency drawn only from notes the reader may read, with the unreadable note made the NEWEST in the fixture so a store-wide recency fails
- [x] A scope the reader has no visibility into renders identically to a scope that is genuinely empty of readable material
- [x] No note identifiers, titles or excerpts in any statistic, asserted on both renderings

## Undetermined is not zero, and not silence

- [x] `Count`, `Recency`, `Subjects` — undetermined as a state of the type, with the ZERO VALUE undetermined
- [x] `Recency` three-valued: an instant, a determined "none", and undetermined
- [x] Every undetermined statistic carries a stable reason code, not prose
- [x] Determined-nothing and undetermined never share a rendering or a token, asserted pairwise over every statistic
- [x] The undetermined field is PRESENT in both renderings — never omitted, never null-as-absent
- [x] `UndeterminedStatistics` — a whole half that could not be computed, with one reason on every field

## The identity door, and the scope of what could not be evaluated

- [x] A request naming no identity is refused before anything is counted, in `Corpus.Statistics` and again in `StatisticsThrough`
- [x] Driven with the SIZE CONTROL: a 5-note store and a 2-note store are indistinguishable to a caller who names nobody
- [x] A grant whose holder is unset — the exported-struct zero value — refused rather than reaching the corpus
- [x] The count of unevaluable notes restricted to the requested scope, so a scope holding none of the requester's material is indistinguishable from a genuinely empty one
- [x] `--as` refused at the request for BOTH halves, before either is computed, matching what `--help` promises
- [x] Both halves of one report name the same reader

## Partial determination

- [x] Three independent determinacy rules, so some statistics are determined while others are not in one response
- [x] Driven both ways: an unplaceable note older than the newest placed one leaves recency determined; a newer one does not
- [x] Notes whose READABILITY could not be determined counted separately and never folded into the count

## The three scopes, and only three

- [x] Statistics at person, group and company scope, through the one parser search uses
- [x] A scope that is not one of the three is refused, not widened to the company and not narrowed to an empty answer
- [x] The capability vocabulary is still exactly `read` / `write` / `publish` — no fourth scope for statistics or administration

## Version semantics

- [x] Recency defined against the LATEST version (PRD §3.3), stated in the output as a constant
- [x] The definition asserted identical at all three scopes, with an amendment to an older note making it the most recent

## The command

- [x] `omw stats`, with `--scope`, `--as`, `--outbox`, `--json` and `schema`
- [x] No hub configured: local statistics determined, hub statistics undetermined with `no-hub-configured`, and no connection attempted
- [x] No daemon running: said with `daemon-not-running`, never started; asserted structurally as well as observationally
- [x] Hub unreachable told apart from a hub that reports nothing readable, field by field
- [x] The CLI and the agent API agree on every statistic's determinacy, driven across all five reasons
- [x] Exit 3 when any statistic was undetermined, 0 when all were determined

## Driven red

- [x] Sixteen mutations applied, each confirmed RED naming the defect, each reverted; recorded in the pull request body
