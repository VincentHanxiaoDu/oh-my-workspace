# Find an answer in the corpus, scoped to a person, a group, or the company

## Why

PRD §1 says the product exists so that *"it is in there somewhere and nobody can find it"* is never
the fourth diagnosis. Nothing could search anything: Issue #12 landed the hub, its notes and its
visibility predicate, and stopped there deliberately.

Three things make search more than a substring scan.

The first is PRD §3.5: **"Visibility is a precondition of ranking. What a searcher may see is
settled before how results are ordered; ranking never surfaces the existence of something the
searcher cannot read."** That is not satisfied by filtering unreadable notes out of a result list.
A ranker that has seen a note has already been changed by it — the note's terms are in the
document-frequency table, so the *relative order of two readable notes* now depends on a note the
searcher may not read. The same channel runs through result counts, "did you mean" suggestions,
author facets and the wording of an empty-result message. Every one of them is a way to learn that
something exists without being shown it.

The second is PRD §1's own promise about an empty result: when a search comes back with nothing, the
searcher must be able to conclude one of exactly three things — they searched badly, it was not
written up clearly, or somebody kept it private. That conclusion is only safe if "the search could
not run" never looks like "the search found nothing". A hub that did not answer, a person who is
not signed in, a token with no `read` scope and a machine with no hub configured are four different
facts, and none of them is an empty result set.

The third is PRD §4.3. Where readability cannot be worked out at all — an unresolvable group, a
membership record that cannot be read — search must not quietly count that as "there is nothing
there". It is a third answer and it makes the result *incomplete*.

## What Changes

- **`hub.Corpus`, and visibility settled as a TYPE.** `Settle`/`SettleWith` filter a store's notes
  through `CanRead` (via the merged `Store.ListReadable`) and return the only value the ranker can
  consume. Corpus fields are unexported and there is no other constructor, so "rank first, filter
  after" is not reachable by reordering statements.
- **Corpus-relative ranking, chosen because it makes the order of work OBSERVABLE.** A term's weight
  is its inverse document frequency over the settled corpus. An unreadable note in the ranker would
  reorder two readable notes, so asserting the order is an assertion about the order of work and not
  merely about absence.
- **Three search scopes — person, group, company.** These are search SUBJECTS. The capability
  vocabulary is untouched and remains exactly `read` / `write` / `publish`.
- **An unknown scope is not an empty result.** `unknown-search-scope` has its own code and its own
  exit status; a valid scope that matched nothing succeeds and exits zero.
- **The undetermined note is neither included nor silent.** It is excluded from results and from the
  corpus statistics, counted separately, and it makes the whole result report INCOMPLETE.
- **`SearchThrough` — refused at request, never narrowed at the edge.** No `read` scope, or a
  request to search as somebody the grant does not act as, is refused outright; the caller does not
  receive a smaller result set instead.
- **`hub.Roster`** — deactivation, kept deliberately out of `Record` so no readability branch can
  ever read it. §5.4: archived, not deleted.
- **`Record.Dissolve`** — so that a genuinely unresolvable readability can be produced by a real
  sequence of events rather than by a mock.
- **New command `omw search`** with four distinct outcomes: found nothing (exit 0), could not run
  (exit 1), could not determine (exit 3), and usage (exit 2).

Not in this change, because other Issues own them: the client-to-hub transport (unassigned, so a
configured hub reports as unreachable), sign-in and token material (#19), corpus statistics for
agents (#13, and excluded by this Issue in as many words), searching a note's history.
