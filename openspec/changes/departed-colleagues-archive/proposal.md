# A departed colleague's notes: archived, not deleted, and still theirs

## Why

"Priya left in March. She was the only person who ever understood why the billing reconciliation job
runs twice. I know she wrote it up — I read it once. Now I search for it and I need to actually find
it, and I need to know it was her, so I know how much to trust it and roughly when she knew it."

A person leaving is the most predictable way for knowledge to fall into the fourth bucket PRD §1
names — *it is in there somewhere and nobody can find it*. §3.3 closes it flatly: **"Notes outlive
employment. A deactivated person's notes are archived, not deleted — the knowledge was the point."**

So deactivating a person is two things that must not be confused with each other. It **ends what is
signed in as them** (§3.10) — every token, every delegated client, their own AI, any script. It
**does not remove what they published.**

The failure is the quiet one, and it has three faces that are all the same loss:

- a note that becomes **unfindable** because its author left;
- a note that becomes **unattributed** — blank, `unknown`, `deleted user`, a placeholder
  indistinguishable from "nobody wrote this";
- a note that becomes **attributed to nobody** while still being served, so a reader cannot weigh
  how much to trust it.

And there is a fourth failure that is subtler than all three: deactivation quietly **changing who
can read something**. A note narrowed to a group must stay narrowed when its author leaves, and a
company-wide note must stay company-wide. This is exactly the case where a plausible implementation
— one that starts consulting an archive flag inside the visibility predicate, for a good local
reason — moves notes in and out of people's reach without anybody noticing.

Issue #11 landed `Archive` with a two-valued `IsDeactivated`, which was the minimum its own
criterion 7 needed. This change makes the answer three-valued, because §4.3 applies to a person's
standing exactly as it applies to a disk's encryption: **an author state that could not be
determined is not a departure, and it is not a colleague at their desk.**

## What Changes

- **`hub.PeopleStatus`** — one narrow, read-only question about one person (`HasLeft`), and
  `*hub.Archive` implements it. Read-only is the point: criterion 17 says deactivation is an act
  performed against the hub, never a side effect, and an interface with no writer gives a
  client-side signal nowhere to arrive.
- **`hub.AuthorActive`** — the one conversion to a `tri.Value`. Yes for still-here, No for
  departed, Undetermined for a record that could not be read, a person nobody named, or no record
  at all. `Archive.MarkUnreadable` makes the third value a real, drivable state rather than only a
  test double.
- **`hub.Attribution`** — author plus standing, with **four** renderings compared pairwise: active,
  departed, undetermined, and a loud "NOT RECORDED" for a note that reached a surface with no
  author. The author is carried as itself and never folded into the state, because folding is how a
  placeholder ends up in the field a name belongs in.
- **Write gates on the store** — `Store.Publish` and `Store.Amend` refuse a departed author, in the
  one function that stores a note rather than in a wrapper a caller must remember.
  `AcceptGrant` / `ReadThroughLive` / `PublishThroughLive` / `SetVisibilityThroughLive` /
  `EvaluateGrantRequestLive` / `Ledger.RequestLive` end sessions without touching a note.
- **`hub.NotesBy`, `hub.Summarise`, `hub.RefIndex`** — findability, corpus statistics and
  references across a departure. All three filter with `Store.ListReadable` / `Store.Read`, which
  call `CanRead`. None of them branches on a person's standing when deciding what comes back.
- **`omw departed`** — `notes`, `show`, `versions`, `refs`, `corpus`. No hub configured is said
  precisely and reaches for nothing; the daemon is said, never started; an unreachable hub is
  undetermined. Three different exit codes for three different answers, and a genuine zero shares
  no wording with any of them.

## What deliberately does NOT change

- **`CanRead` is untouched and uncalled-into.** Deactivation is not an input to visibility, in
  either direction. Every read path here goes through it; there is no second predicate.
- **No fourth scope.** The vocabulary stays `read` / `write` / `publish`. Reading an archived note
  is reading; publishing as a departed person is refused, not granted a scope of its own.
- **No visibility on `Version`.** One visibility governs a note and all of its versions, which is
  why attribution being stable across versions is cheap: there is one note, one author, one state.
- **Nothing expires (§5.4).** No prune, no window, no retention count. A test advances the clock a
  hundred years past the departure and asserts every version is still there and still attributed.

## Tensions recorded rather than smoothed over

- **Undetermined refuses writes but never refuses reads.** For a READ, an unestablished author state
  changes nothing at all. For a WRITE, proceeding is how a departed person's script keeps publishing
  through a flaky people record. This is the one asymmetry, and it is deliberate.
- **`RefIndex` is a placeholder for Issue #14.** #14 owns references and has not landed. What must
  survive its replacement is the property — archival does not break a reference in either direction
  — not this type.
- **`CorpusSummary` is a placeholder for Issue #13** on the same terms.
- **`hub.Store` still mints sequential note ids (`note-N`).** Unguessable ids are Issue #15's, and
  nothing added here depends on ids being dense or sequential.
