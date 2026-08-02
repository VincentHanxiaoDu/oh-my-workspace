# Tasks

## Three answers about a person, not two

- [x] Add `hub.PeopleStatus`, one read-only question (`HasLeft`) about one person, narrow enough
      that no directory client could hide behind it and no consumer could deactivate through it
- [x] Implement it on `*hub.Archive`, keeping Issue #11's `IsDeactivated` and `Deactivated()`
      untouched so its criterion 7 tests keep passing unchanged
- [x] Add `Archive.MarkUnreadable` and an `unreadable` set alongside `deactivated`, so "could not
      be determined" is a real state reachable through the production lookup path
- [x] Add `hub.AuthorActive`, the one conversion to `tri.Value`, with nil-`PeopleStatus` and an
      unnamed person both answering Undetermined rather than active

## Attribution that is never blank and never authorless

- [x] Add `hub.Attribution` carrying the author as itself and the standing beside it, with the
      zero value undetermined so a half-built struct does not render as a colleague at their desk
- [x] Write four renderings — active, departed, undetermined, and NOT RECORDED — with the departed
      one saying archived-and-kept in as many words
- [x] Add `AllAttributionLines` for the pairwise test, built for the SAME author so the test
      compares the state clauses rather than passing because the names differ
- [x] Add `hub.RetentionLine` for §5.4, said where a departure is reported

## Archived, not deleted

- [x] Add `hub.AttributedRead` and `hub.AttributedVersion`, both reading attribution from the NOTE
      so every version shows the same author
- [x] Confirm no read path consults a person's standing: `AttributedRead` is `Store.Read` plus a
      lookup, so the archived path IS the ordinary path

## Findability, statistics and references across a departure

- [x] Add `hub.NotesBy`, filtering `Store.ListReadable`'s output by author — gate first, filter
      second, never the other way round
- [x] Add `hub.CorpusSummary` / `hub.Summarise`, counting archived and author-undetermined notes as
      SUBSETS of what the reader may read
- [x] Add `hub.RefIndex` with `Link`, `Resolve` and `Backlinks`, resolving through `Store.Read` and
      branching on no person's standing
- [x] Report a reference the reader may not follow as refused, not as absent

## Sessions ended, publications untouched

- [x] Add `hub.CheckActive`, the one gate for acts that create new authority, with three outcomes
      and two distinct error codes
- [x] Add `hub.AcceptGrant`, checking the holder BEFORE the scope, so "for any scope" is a property
      of the shape
- [x] Add `ReadThroughLive`, `PublishThroughLive`, `SetVisibilityThroughLive`,
      `EvaluateGrantRequestLive` and `Ledger.RequestLive`
- [x] Gate `Store.Publish` and `Store.Amend` on the author's standing, in the store rather than in
      a wrapper, and before the id counter is touched so a refusal stores nothing
- [x] Add `Store.SetPeopleStatus` / `Store.PeopleStatusOf` so the record the write gate enforces is
      the record the attribution is read from

## The command

- [x] Add `internal/commands/departed_cmd.go` — a NEW file, touching no existing command file
- [x] `omw departed notes --by <person> [--as <reader>]`, stating the person's standing even when
      they have no notes
- [x] `omw departed show`, `versions`, `refs`, `corpus`
- [x] Order the preconditions: no hub configured (determined, `ExitFailure`, reaches for nothing),
      then daemon liveness through Issue #41's one definition, then unreachable hub
      (`ExitUndetermined`)
- [x] Call `daemonLiveness` and `reportDaemonNotLive`; write no probe and name no control socket
- [x] Give the no-hub answer wording that shares nothing with a genuine zero, and give the genuine
      zero wording that says the hub was asked
- [x] Return `Store.ListReadable`'s undetermined ids to the person rather than dropping them, and
      exit `ExitUndetermined` when there are any

## Driving it

- [x] Build one fixture used by every criterion, so before-and-after are two observations of one
      corpus rather than two corpora that resemble each other
- [x] Criterion 1: fetch a note before and after the deactivation, compare id, body and author
- [x] Criterion 2: snapshot every version's body and timestamp before, compare after
- [x] Criterion 3: resolve a reference and its backlink before and after
- [x] Criteria 4 and 6: run the identical query as the identical searcher, for four readers across
      company-wide, group, named-people and self scopes, and compare the result sets
- [x] Criterion 5: assert three narrowings stay refused, at the store AND in search results
- [x] Criterion 7: compare a searcher's count against a corpus with and without archived notes they
      cannot read
- [x] Criterion 8: assert the statistics equal what `ListReadable` returns, for every reader
- [x] Criteria 9–11: assert an archived note is never an error, never an empty body, and never a
      placeholder author; check for `unknown`, `deleted user`, `anonymous`, `nobody`
- [x] Criteria 12 and 18: compare the renderings PAIRWISE, and check the undetermined one does not
      CONTAIN either determined clause
- [x] Criterion 13: compare each version's attribution to the first version's, not to a literal
- [x] Criteria 14–16: one run asserting both "token refused" and "note still findable and
      attributed"; assert the refused grant request left the ledger unchanged
- [x] Criterion 17: assert `PeopleStatus` has exactly one, read-only method and `Deactivate` takes
      a `PersonID` and nothing else
- [x] Criterion 18: drive the third value through `MarkUnreadable`, not a hand-built struct
- [x] Criterion 19: compare the daemon-running and daemon-not-running outputs to each other
- [x] Criterion 20: replace the hub source with one that fails the test if it is called, and assert
      no subcommand reaches it with no hub configured
- [x] Criterion 21: assert no-hub, unreachable and success are three exit codes and three outputs,
      and that a genuine zero is none of them
- [x] §5.4: advance the clock a month, a year, seven years and a hundred years past the departure
      and assert every version is still there and still attributed
- [x] Assert `CanRead`'s answer is identical for every note and every reader before and after two
      deactivations and one unreadable record
- [x] Assert the scope vocabulary is still exactly three
- [x] Assert this Issue's errors are pairwise distinct against Issue #12's and Issue #11's lists

## Breaking the tests on purpose

- [x] Mutate an archived note out of search — RED, naming criteria 4, 6, 7 and 8
- [x] Mutate an archived note's author to empty — RED, naming criteria 3, 9, 10 and 13
- [x] Mutate deactivation to widen a narrowed note — RED, naming criteria 4, 5, 6 and 8
- [x] Mutate a reference to an archived note into an undetermined one — RED, naming criterion 3
- [x] Mutate `AcceptGrant` to accept everything — RED, naming criteria 14 and 15
- [x] Mutate the undetermined author state into a departure — RED, naming criteria 12 and 18
- [x] Mutate the store's publish gate into a dropped error — RED, naming criterion 16
- [x] Verify each mutation was present in the file and semantically live before believing its result
