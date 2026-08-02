# Read a note as it stood when someone acted on it

## Why

PRD §3.3: "Notes are versioned. Search finds the latest; the timeline is addressable, so a claim
someone acted on last month can still be read as it stood." Issue #12 built the note, its single
visibility and the read gate, and left `Version` and `Store.Amend` as a stub with no surface: there
was no way to enumerate a timeline, no identifier for a version, and nothing that told a reader
which version they were holding.

Three things make this more than a list of revisions.

The first is that a person reading an old version has to be TOLD they are reading an old version.
The Issue's journey paragraph is precise about the failure: "I look at an old link someone sent me,
and it silently shows me today's text as though that were what they sent." The inverse is just as
bad — superseded text served under a heading that reads as current. So the standing is in the
output, on every read, worked out from the timeline and never from the argument the caller typed.

The second is PRD §5.4, which the owner has ruled: nothing expires. Notes and their versions are
kept forever. A version somebody acted on that can be aged out makes the whole timeline a
decoration, so this change adds no retention window, no maximum count and no prune — and a test
walks the package's own declarations to keep it that way rather than trusting a paragraph.

The third is PRD §4.3 applied to versions. "This is the current version", "this is a superseded
version" and "which of the two this is could not be established" are three answers, and so are
"this version's body is empty", "this version's body could not be read" and "there is no such
version". Each pair must be tellable apart by a person reading the output and by a script reading
the exit code.

Issue #12 left two constraints for this change and both are respected: no visibility field on a
version, and no fourth scope. Reading a version is reading.

## What Changes

- **`hub.VersionRef`** — `note-1@v3`, printed by every surface, parsed back by one parser. Criterion
  2's "an identifier that can be fed straight back in", and stable because nothing is ever removed.
- **`hub.ListTimeline` / `hub.ReadView` / `hub.CurrentView`** — free functions over a
  `hub.VersionSource`, in the same shape as `hub.CanRead` and for the same reason: #15's search must
  be able to reach them without holding a store, and "which version is current" must be decided in
  exactly one place.
- **Standing is a `tri.Value`** — current / superseded / could-not-be-determined, with three
  pairwise-distinct sentences. Not a new enum: the product has one three-valued answer already, and
  a second is where the third value acquires a softer second wording.
- **`Store.Timeline`, `Store.VersionAt`, `Store.AuthorOf`** — new methods in a new file, each
  routing through `Store.Read` so the visibility gate cannot be skipped by reaching for a version.
- **`hub.ErrNoSuchVersion` and `hub.ErrVersionUnreadable`** — a missing version and an unreadable
  one each get their own code, distinct from each other, from "no such note", and from a successful
  read of an empty body.
- **`hub.Archive`** — a deactivated person's notes and full history stay readable, marked archived.
  A label, never consulted when deciding readability.
- **`hub.SearchLatest`** — matches the current version only, and names the version each hit refers
  to. The version-facing contract #15 must build on, not search itself.
- **The control API's version answers** (`TimelineJSON`, `VersionJSON`, `CurrentJSON`) built from
  the same three functions the CLI renders, so criterion 13's agreement is structural.
- **New package `internal/drafts`** — the local half. A draft outbox in a directory the person named
  on purpose, with successive revisions addressable and readable as they stood, no hub and no
  network. It implements `hub.VersionSource`, so a draft timeline and a published one read alike.
- **New command `omw note`** — `show`, `versions`, `read`, `search`, `draft`, `schema`.

Not in this change, because other Issues own them: search ranking and corpus statistics (#15, #13),
the client store's location (#3), the outbox proper (#9), departed colleagues' notes as a capability
(#22), the client-to-hub transport (unassigned — so a configured hub reports as unreachable, which
is the honest answer and the same path a real outage takes).
