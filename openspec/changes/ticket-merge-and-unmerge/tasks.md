# Tasks

## A set of writes that is either all applied or none

- [x] Add `store.Op` and `Store.Apply` in a new `internal/store/batch.go`, with the journal's atomic
      rename as the single commit point
- [x] Validate every op before the commit point, so a batch with one unusable id writes nothing
- [x] Replay committed batches from `Open`, so a reader that knows nothing about batches cannot
      observe a half-applied one
- [x] Refuse an unreadable journal rather than stepping over it
- [x] Keep the journal out of `Kinds`, so a batch in flight is never reported as records the person
      has

## Merging

- [x] Add `internal/inbox/merge.go` without touching a line of the existing inbox files
- [x] `Merge` folding two or more stored tickets into one, as a single batch
- [x] Keep each source's stored payload verbatim as the snapshot an unmerge restores
- [x] Record what each input was, which channel and which source it came from, and why
- [x] Require a written title and a written summary, and refuse a summary that is the source titles
      run together in any order
- [x] Refuse fewer than two inputs, a repeated input, a ticket that does not exist, and an
      identifier a bystander holds — each before anything is written
- [x] Allow the merged ticket to take the identifier of one of its own inputs
- [x] Give the merged ticket the earliest determined arrival of its inputs, or undetermined
- [x] Put the merged ticket through the acknowledgement refusal, so no merge mints one

## Unmerging, and the trace

- [x] `Unmerge` restoring every source from its snapshot bytes, as a single batch
- [x] Write a `ticket-unmerged` record per restored ticket, so criterion 6 is answerable by
      inspecting that ticket
- [x] Refuse to unmerge a ticket that was never merged, changing nothing
- [x] `IsMerged` and `LoadUnmerged` answering in three values, so an unreadable record is never a
      confident "no"

## Undetermined, on disk and on the page

- [x] Wrap each working field on disk so that a recorded absence and a key that was never written
      are different bytes
- [x] Report a merge record that does not carry all four fields for an input as a failure
- [x] Default an unrecorded "why" and an unrecorded source identifier to undetermined, never to
      empty

## The command

- [x] Add `internal/commands/ticket.go` as a new file, registering from an init
- [x] `merge`, `unmerge`, `show` and `list`, with every field rendered through `Field.Render`
- [x] Ask `daemonLiveness` whether the daemon is running, never a socket path this file derives,
      and start nothing
- [x] Render the daemon answer in three values, and say the "store on disk" sentence only where the
      negative was established
- [x] Refuse merge and unmerge on an undetermined liveness, on `ExitUndetermined`, changing nothing
- [x] Say the hub was not contacted and that no operation here would
- [x] Take the control API's state from the same `daemon.Inspect` report, and refuse when it is not
      open, distinguishably from "there is nothing to merge"
- [x] Keep the third answer about the control API off the negative's exit code
- [x] Turn each distinct failure into its own sentence and its own exit code
- [x] Refuse an unknown flag rather than taking it as a ticket identifier

## Driving it

- [x] Drive criterion 1 through `omw inbox list`, not through this command's own listing
- [x] Compare a cross-channel merge against a same-channel one and assert nothing is warned about
- [x] Compare restoration against a byte snapshot taken before the merge
- [x] Compare the three renderings of an origin pairwise, never against a literal
- [x] Kill a real subprocess partway through a real merge, at kill times measured from the machine
      rather than written down, and assert both endpoints are reached and neither is half
- [x] Assert the header's stale-state sentence directly rather than relying on the shared guard's
      list of phrasings — a rewording walked out of that net, proved by mutation
- [x] Break each guarantee in turn, watch the tests go red naming the defect, and revert
