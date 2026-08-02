# Follow a note's references, and ask what else was written about this

## Why

PRD §3.4 is one paragraph and it is the product's reason for existing: notes carry inline
references to people, groups and other notes, and that "is what makes the corpus a graph rather
than a pile, and what makes *what else was written about this* answerable". PRD §1 says the product
exists so that "it is in there somewhere and nobody can find it" stops being the answer. Until now
a note that named a colleague, a team and an earlier note named them as words on a page: the reader
had to go hunting, usually did not, and the next person solved the same problem again.

Two things make this more than a link type.

The first is PRD §3.5. "Visibility is a precondition of ranking… ranking never surfaces the
existence of something the searcher cannot read." A reference is a **second doorway into the same
corpus** and it is bound by the same rule. A note visible company-wide may reference a note
restricted to one group, and the reader must not learn from a title, an identifier, a count, a
placeholder, an error, or a gap in the prose that the restricted note exists. So nothing here
decides readability for itself: every decision goes through Issue #12's `CanRead`.

The second is PRD §4.3. A reference whose target was archived, whose group was dissolved, or whose
resolution could not be attempted at all is not the same fact as no reference, and is not the same
fact as a reference the reader may not see. There are four states, not two, and the two that are
easiest to confuse are the two that matter: a target that is **gone** is shown and marked; a target
that **exists and this reader may not read it** is shown as nothing whatsoever.

## What Changes

- **A reference syntax and a parser** — `[[person:<id>]]`, `[[group:<name>]]`, `[[note:<id>]]`,
  with a backslash escape so a note can be written *about* the syntax. Prose that happens to
  contain the same characters is not a reference.
- **References are derived from a version's body, not stored beside it** — so a version's
  reference set is what its author actually wrote, and PRD §3.3's addressable earlier versions
  yield the references as they stood then, with nothing to keep in step.
- **`hub.ResolveReference`** — one reference, one reader, four states: resolved, unresolved,
  undetermined, and hidden. Hidden references never reach a caller: they are removed from the
  listing, from the count, and from the rendered body, whitespace closed up behind them.
- **`hub.OutboundReferences`** — a note's references as one reader sees them, with the reader's own
  rendering of the body. The count on it is the reader's count; there is no global one to print.
- **`hub.ReferencesTo`** — *what else was written about this*, for a note, a person or a group. It
  never looks the target up, so a target that does not exist and a target the reader may not see
  produce the same answer, with no timing, ordering or error difference to tell them apart.
- **`hub.PublishWithReferences`** — publication is refused when the body references something the
  **author** cannot see, with a separate refusal for the case where that could not be determined.
  `PublishThrough`, the agent API path, was changed to call it, so an agent cannot point at what
  its person cannot read.
- **New command `omw references`** — `syntax`, `scan`, `of`, `to`. `scan` is the local half: it
  extracts a draft's references with no hub, names the hub as the missing piece for resolving them,
  and reports the partial answer with an exit status a script can tell from a complete one.

Not in this change, because other Issues own them: versions themselves (#11), search ranking (#15),
archival (#22), the outbox (#9), the client-to-hub transport (unassigned). No fourth scope was
added and `Version` still carries no visibility field.
