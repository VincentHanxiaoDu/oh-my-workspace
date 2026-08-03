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

- **`hub.Roster` is adopted, not duplicated.** Issue #15's roster is already three-valued, its nil
  and unknown-person cases already answer Undetermined, and search already consults it. An earlier
  revision of this change defined its own `PeopleStatus` interface over Issue #11's `Archive`; that
  was a second record of the same fact and it is gone. Nothing here wraps, converts or caches it.
  The third value is reached by a roster that has never heard of somebody — an ordinary state of an
  incomplete people record, not a test double.
- **`hub.Attribution`** — author plus standing, with **four** renderings compared pairwise: active,
  departed, undetermined, and a loud "NOT RECORDED" for a note that reached a surface with no
  author. The author is carried as itself and never folded into the state, because folding is how a
  placeholder ends up in the field a name belongs in.
- **Write gates on the store** — `Store.Publish` and `Store.Amend` refuse a departed author, in the
  one function that stores a note rather than in a wrapper a caller must remember.
  `AcceptGrant` / `ReadThroughLive` / `PublishThroughLive` / `SetVisibilityThroughLive` /
  `EvaluateGrantRequestLive` / `Ledger.RequestLive` end sessions without touching a note.
- **`hub.NotesBy`, `hub.Summarise`** — findability and corpus statistics across a departure. Both
  filter with `Store.ListReadable`, which calls `CanRead`. Neither branches on a person's standing
  when deciding what comes back. **References are Issue #14's** — see below.
- **`omw departed`** — `notes`, `show`, `versions`, `corpus`. An identity is required before the
  store is touched; no hub configured is said precisely and reaches for nothing; the daemon is
  said, never started; an unreachable hub is undetermined. Four answers, and a genuine zero shares
  wording with none of them.

## Two defects this change was refused for, and what they had in common

Both were **an undetermined readability being rendered as an answer rather than withheld as a
non-answer**, and both are worth stating because neither was a careless omission.

**The enumeration oracle was reopened.** `--as` was optional. An empty reader makes `CanRead`
answer `Undetermined` for every note — correctly, since "you did not say who you are" is not a
determined refusal — so `Store.ListReadable` returned every id in the hub in its undetermined slice
and the command printed them one per line. `omw departed notes --by anybody`, with no identity and
not even a real person named, dumped the whole id space including notes narrowed to one person.
`internal/hub/noteid.go` made ids unguessable *specifically so* that "refused" and "no such note"
could stay distinguishable without the space being walkable; this handed the space over directly.

The repair is not to render the undetermined answer more carefully. **`Undetermined` for an
unidentified reader is a different fact from `Undetermined` for a reader whose group membership
could not be resolved.** The second is an answer about notes; the first is an answer about the
request, and a request that never said who is asking is refused at the argument with
`hub.ErrNotSignedIn` — the position `omw search` already takes. Two consequences, and the second
matters as much as the first: no identity means no store access, and **even with an identity the
undetermined count is reported without its ids**, because a reader who may not see a note may not
see its id either.

**A second, ungated reference surface.** This change shipped a `RefIndex` of its own, written while
Issue #14 had not landed. #14 has since landed and its `OutboundReferences` reads the referencing
note through `Store.Read` *first*, so a reader refused a note learns nothing about its edges. The
`RefIndex` did not: a former reader who legitimately learned a note's id, and was then narrowed
away from it, kept its reference graph forever. The type and the `omw departed refs` subcommand are
gone, and criterion 3 is now driven against #14's functions.

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
- **`CorpusSummary` is a placeholder for Issue #13**, which has not landed. What must survive its
  replacement is the property — archived notes are counted for exactly the people who may read them
  — not this type. Its `Undetermined` is an `int` and not a list of ids, the same shape
  `hub.Backlinks.Undetermined` takes, for the same reason.
- **Note ids are unguessable and nothing here depends on them being dense or sequential.** An
  earlier revision of this document said the store still minted `note-N`; that was stale, and the
  same staleness is what produced the reference defect above.
