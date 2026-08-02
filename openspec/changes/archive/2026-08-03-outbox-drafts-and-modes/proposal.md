# Draft notes into the outbox, and choose how they leave it

## Why

PRD §2.3 puts an outbox on the person's machine and says nothing leaves it unless they publish.
§3.3 makes a note the unit of published knowledge. §3.14 makes the local store the sole home of
unpublished data. Issue #11 built the local half of the outbox — successive revisions of a draft,
addressable and readable as they stood — and deliberately left two things open: where the outbox
lives, and what a person can choose to have happen to a draft.

Three habits are all legitimate. One person drafts all week and publishes on Friday: `manual`. One
does not trust himself to catch what he should not have written, so he writes his rules down in his
own words and has an AI check each draft against them, on his machine, with his model and his key:
`review`. One drafts into a team log where everything is visible anyway and wants no gate: `auto`.

The failure this change exists to prevent is narrow and specific. A person picks `review`, has no
model configured, and the client behaves like `manual` — drafts pile up, nothing said — or like
`auto` — drafts publish unchecked. Both are the client silently doing something other than what the
person chose, and on the screen both look like nothing happening at all. The same shape appears one
level down: a review that could not be completed, counted as a pass, is `auto` wearing `review`'s
name.

## What Changes

- **The outbox moves into the store.** `drafts.InStore` puts one outbox at a fixed name inside the
  store Issue #3 creates, so §3.14's "sole home" is true rather than aspirational. No second outbox
  is introduced; Issue #11's `Outbox`, its revisions and its three-valued reads are consumed as they
  stand.
- **Publication mode**, recorded in the store: exactly `manual`, `review` and `auto`. With nothing
  ever set the effective mode is `manual` — a real value, reported as one. A name outside the three
  is refused and changes nothing. A recorded choice that cannot be read is undetermined, and is
  neither a mode nor the default.
- **Draft state**, recorded beside the draft's revisions: `drafted`, blocked on a missing
  prerequisite, review-could-not-be-completed, refused-by-review, cleared-by-review, and
  handed-to-publication. A draft blocked because `review` has no model does not read like a draft
  its author simply has not published.
- **Review rules in the person's own words**, stored as one string and read back byte for byte. No
  trimming, no line-splitting, no case folding — the read-back path prints the person's bytes on
  stdout and everything the command has to say on stderr.
- **A three-valued review outcome.** A model that cannot be reached, that errors, or that answers
  something that is not a verdict leaves the draft unpublished and the outcome undetermined, with
  its own exit code, distinguishable in output from both a pass and a refusal.
- **`review` with no model names the missing model** and exits non-zero — at the moment the draft is
  written as well as at the attempt to publish, so drafts cannot accumulate in silence.
- **New command `omw outbox`** — `draft`, `list`, `state`, `mode`, `mode set`, `rules`,
  `rules set`, `model`, `review`, `publish`. Every subcommand reports the daemon's state and starts
  it for nobody, and refuses to proceed beside a control socket whose owner-only permissions cannot
  be confirmed.

Not in this change, because other Issues own them: the publication transfer itself (#10 — this
change decides whether a draft may leave and says plainly that nothing has left), visibility (#12),
versions (#11), and model configuration (#18 — a placeholder is read from the environment and is
called out as such).
