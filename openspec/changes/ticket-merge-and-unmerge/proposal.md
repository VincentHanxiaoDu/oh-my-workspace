# Merge the traffic about one problem into one ticket, and take it apart again

## Why

"There's one broken login. I got five emails about it, there's a Teams thread, and then someone
pinged me again this morning. My inbox shows that as seven separate things to act on. It's one
thing." Issue #8 landed the inbox that lists those seven; nothing yet lets a person say *these are
all the same problem*. Until they can, the inbox is honest and useless in the same breath — it shows
them exactly the fragmentation they wanted to be rid of.

Four sentences decide the shape of this, and each is a way the obvious implementation goes wrong:

- **§3.2 — every merge is reversible and shows its working.** The obvious merge writes a new ticket
  and deletes the old ones, which is a one-way door: "I will get this wrong sometimes, and I want
  back exactly what I had, not a best-effort reconstruction." A reconstruction from a decoded struct
  is only as exact as the last person to add a field to `Ticket` remembered to make it. And a merge
  nobody can inspect is indistinguishable from the client losing traffic.
- **§3.14 — a record is either absent or complete.** That promise is about ONE record. A merge is
  N+2 records changing together, and no ordering of individual writes keeps a killed process from
  leaving the inbox with the merged ticket beside its own inputs, or with the inputs gone and
  nothing in their place. Both are the half-merged state the Issue forbids.
- **§4.3 — undetermined is never a "no" and never silence.** A source whose origin channel could not
  be resolved, and a merge whose "why" nobody recorded, are answers. They are not blanks, and they
  are not the same answer as "this input has no source".
- **§3.2 again — acknowledgements are not low-priority tickets.** A merge is exactly where a
  priority field would arrive: somebody folds in an "ok", needs somewhere to put it, and puts it at
  the bottom of the list. There is no bottom of the list.

## What Changes

- **A new atomic-batch primitive in `internal/store`** — `Store.Apply(name, ops)`: a set of puts and
  deletes that is either all applied or none. A journal is written by one atomic rename, which IS
  the commit point; the ops are then applied and the journal removed. `Open` replays any journal it
  finds before handing the store to anybody, so a process that died mid-batch cannot leave a half
  state for a later reader. This widens invariant 4 from one record to a set of them.
  **It is in the store, and not in the inbox, because the reader who must never see a half-merged
  inbox is `omw inbox list` — a command that knows nothing about merging and should not have to.**
  The store still does not know what a ticket is: an op is a kind, an id and opaque bytes.
- **A new `internal/inbox/merge.go`**, added beside the ticket type rather than replacing any of it.
  `Merge` folds two or more stored tickets into one; `Unmerge` takes it apart.
- **A merge keeps each source's stored payload VERBATIM**, and an unmerge writes those bytes back.
  Not `encode(decode(x))` — a round trip through this build's struct would silently drop whatever a
  later build put in the file, and criterion 5 is "the content it had", not "the content this build
  knows how to hold".
- **The trace of a merge lives outside the restored ticket.** Criterion 5 wants the ticket back
  byte-identical; criterion 6 wants a restored ticket to be distinguishable from one never merged.
  Both are satisfiable at once only if the second fact is a separate record — `ticket-unmerged`,
  keyed by the restored ticket's own identifier so that inspecting THAT ticket answers it.
- **Every input carries what it was, which channel and which source it came from, and why.** All
  four are `Field`s, so an unresolved origin and an unrecorded reason render as undetermined rather
  than as blanks. A key that is missing from the record entirely is a different thing again and is
  reported as `ErrIncompleteWorking` — criterion 4 calls that a failure, not a field to print empty.
  A wrapper object is needed on disk for this: a `Field` marshals a recorded absence as `null`, and
  `null` decodes into a `*Field` as nil, which would have made "recorded as none" and "not recorded"
  the same bytes.
- **No priority, still.** Nothing here adds a rank, and no parameter of a merge can carry raw
  traffic: inputs are identifiers of stored tickets, resolved through the store, and the merged
  ticket goes through `Put` and faces the acknowledgement refusal like anything else.
- **A written title and a written summary are required**, and a summary that is the source titles
  run together — in any order, with any punctuation — is refused. One item titled "yes ok Hii" is
  the five items §3.2 forbids, on one line.
- **A new `omw ticket` command** with `merge`, `unmerge`, `show` and `list`. Its exit codes carry the
  distinctions: a merge that could not happen, a ticket that was never merged, and a store that
  could not be read are three answers on three codes, and `could not determine` never shares one
  with `determined to be nothing`.
- **Nothing consults the clock to decide what exists** (§5.4). A merge from 2009 is listed and is
  still reversible; asserted by backdating and by a scan over the source.
