# Tasks

## Writing and holding references

- [x] A reference syntax for the three kinds, with a backslash escape for prose about the syntax
- [x] `ParseReferences` — occurrences in order; prose containing the same characters is not a reference
- [x] `Reference` carries its kind, so a person and a note with the same name are different targets
- [x] References derived from a version's body, so a version's reference set is that version's
- [x] `PublishWithReferences` — a reference the author cannot see is refused at publication
- [x] A separate refusal, with its own code, when the author's access could not be determined
- [x] `PublishThrough` routed through the check, so the agent API is not a way around it

## Reading references forward

- [x] `ResolveReference` — one reference, one reader, four states, all through `CanRead`
- [x] `OutboundReferences` — a note's references as one reader sees them, plus that reader's body
- [x] `RenderBody` — each reference rendered in its state, escaped tokens unescaped
- [x] The whitespace behind a removed reference is closed up, leaving no gap in the prose

## The reverse question

- [x] `ReferencesTo` — what else was written about this, for a note, a person or a group
- [x] It never looks the target up, so existence cannot be inferred from the answer
- [x] Notes whose readability could not be determined are counted and reported, never folded in

## Never disclosing what the reader may not see

- [x] Hidden references are absent from the listing, the count and the rendered body
- [x] Counts are computed over the reader's visible set only; there is no global count to print
- [x] Unresolved and hidden render differently, and neither renders as a resolved reference
- [x] Undetermined renders as itself, naming neither its kind nor its target

## The CLI

- [x] `omw references syntax` — local, reaches nothing, shows the three states
- [x] `omw references scan <file>` — the local half; complete or explicitly partial, by exit status
- [x] `omw references of <note> [--as p] [--version n]`
- [x] `omw references to <kind:target> [--as p]`
- [x] No command starts the daemon; with none running it says so

## Tests

- [x] Two readers, the same note: the excluded reader's output is byte-identical to the control
- [x] Resolved, unresolved and hidden compared pairwise over ONE target
- [x] A dangling reference is shown, marked, and does not take the rest of the listing with it
- [x] An invisible target and a nonexistent one answer identically, through the API and the CLI
- [x] A version's references are that version's, both halves of the criterion
- [x] Following a reference grants no read, directly or through a grant
- [x] A departed colleague's note still resolves
- [x] The reference code imports no network-capable package
- [x] The only direct caller of `Store.Publish` is the wrapper that checks references
- [x] The vocabulary is still three scopes and `Version` still carries no visibility field

## After merging main (Issue #11 landed)

- [x] Merge `origin/main` and make the merged tree compile and pass, not just this branch
- [x] Drop my duplicate `TestVersionStillCarriesNoVisibility` in favour of #11's merged one
- [x] Keep the one thing mine caught that #11's does not, under its own name: a `Visibility`
      reached through a pointer, slice, array or map
- [x] Drop my duplicate scope-vocabulary guard; #11's merged one covers it and more
- [x] Adopt #11's `VersionRef` so a listing says which version it means in the shared spelling
- [x] Drive criterion 13 against #11's real `Archive` instead of structurally
- [x] Widen the `Store.Publish` caller sweep to every package under `internal`, discovered not listed

## After PR #43 (one daemon liveness answer) and the owner's identifier ruling

- [x] Merge `origin/main` and verify the merged result, not just this branch
- [x] Route liveness through the shared three-valued `daemonLiveness`; hold no socket-path knowledge
- [x] Render all three liveness answers distinguishably, with distinct exit codes
- [x] Criterion 19: references derive no identifier — structural check over this Issue's source
- [x] Criterion 19: rendering depends on no other note's identifier, publication order or count
- [x] Criterion 19: control/test differential over a corpus differing by one unreadable target

## Taking #46's unguessable identifiers

- [x] Merge `origin/main`, which now carries `internal/hub/noteid.go`; change no reference code
- [x] Rework the criterion-19 differential for random ids: the hidden note sits BETWEEN two
      readable ones, and the comparison abstracts identifiers only where that is licensed
- [x] Assert the identifiers bo sees carry no ordinal content
- [x] Assert the identifier difference is unattributable to the hidden note, by showing two
      identically-built corpora differ the same way — the clause that licenses the abstraction
