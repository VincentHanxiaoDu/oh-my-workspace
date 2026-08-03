# Three readers with no writer render absence as a determined zero

## Why

Issue #67, found by driving `main` itself as a product rather than by reviewing a branch. Three
surfaces, one defect: **a reader that is correct, a producer that was never built, and the resulting
absence rendered as a determined zero.** Each half is right on its own, every branch gate passed, and
three UAT sweeps passed, because nothing anywhere compared the two halves.

**Blocker 1 — `omw report run` reports "no activity" forever.** `git` and `token_usage` are
advertised by `omw report subjects` and nothing in the build has ever written activity for them.
Taken with the daemon running, the project watched, and a commit eleven minutes old:

```
git:full
  no activity in this period
EXIT=0
```

The feature's own help text names the failure — *"it never comes back as an empty report, because an
empty report looks exactly like a quiet day"* — and it comes back as a quiet day, exit 0.

**Blocker 2 — `omw diagnostics` reports zero drafts while drafts exist.** The bundle read
`store.Kind("draft")`; `omw outbox draft` writes revision files under `<store>/outbox/<id>/`. With
two drafts on disk and `omw outbox list` reporting `drafts: 2`, the bundle said
`draft-inventory  collected (0)`. A support engineer reading that bundle concludes the person has no
drafts. This is worse than withholding, because it is asserted.

**Blocker 3 — `omw devices list` exits 1 on a successful listing.** A partial inventory is a screen
that was produced with something on it this machine cannot establish, which is what exit 3 is for
(`omw status --help` spells it out; `references scan` already uses it). Exit 1 means the command
could not do what was asked, so every script treating non-zero as failure reported a healthy device
list as broken.

## What Changes

**Reports (Blocker 1).** `reports.Subject` gains `Producer`, naming what writes a subject's
activity; empty means nothing in this build does. `StoreSource.Activity` returns the new
`ErrNoProducer` when a subject has no producer *and* no records — records present still win, so an
ingester landing tomorrow needs no change here. A new `StateNoProducer` renders its own line, does
not contain the words "no activity", and is not `Determined()`, so `omw report run` exits 3.
`omw report subjects` marks every such subject, which is criterion 2's second half: not silently
promising. Every `Producer` in the catalog is empty except `published_notes`, whose producer is the
hub — writing a plausible name for the others would restore the exact defect the field ends.

**Diagnostics (Blocker 2).** `KindDraft` is deleted. A bundle category now names the SUBSYSTEM that
produces its records rather than a store kind, and drafts are enumerated through the outbox that
writes them. An enumeration that fails renders undetermined with a reason and the sentence saying it
is not a report that there are none.

**Devices (Blocker 3).** A partial listing exits `ExitUndetermined` instead of `ExitFailure`.

**The structural guard (criterion 6).** `internal/kindguard` parses the product's own source,
resolves the store kinds flowing into read calls and write calls, and fails when a kind is read and
written nowhere. Run against unmodified `main` it names Blocker 2 by kind and by file and line.

## Two decisions taken here that the Issue did not settle

**`omw devices list` no longer distinguishes "known partial" from "could not tell" by exit code.**
Issue #17 split them 1 and 3, and criterion 5 requires 3 for a partial inventory, so both are now 3.
The split was between two answers a script acts on identically — in both cases there are machines
this run cannot account for — and it bought that distinction by spending the failure code on a
command that succeeded. The distinction is kept where it is read: the `listing complete:` line and
the `missing:` lines, asserted to differ. `TestKnownPartialAndCouldNotTellDoNotShareAnExitCode` is
renamed and rewritten to assert that.

**`omw diagnostics` keeps a two-valued exit code.** Criterion 4 asks the bundle to SAY it could not
enumerate drafts, and that is where the fix is. This command performs an act rather than answering a
question — it produced a bundle or it did not — and exiting 3 would tell a script no bundle exists
when one does. The three-valued answers live inside the manifest, and the exit-code split for this
fact is driven on `omw outbox list`, which does answer the question (0 with a count, 3 when nothing
was established).

## Changed in review (PR #92)

- **The guard was blind to function literals.** It inspected declared function bodies only, so a
  store read inside a `func` literal was neither reported nor listed as unresolved — it produced
  nothing at all, which is the one outcome the design refuses. This mattered immediately: the fix
  for Blocker 2 is a package-level map of `recordSource{List: …}`, and writing those closures inline
  is an ordinary tidy-up that would have hidden the very code the guard protects. The scan now walks
  every body, and a second pass records any read-shaped call it did not reach.
- **`message-inventory` no longer asserts a zero it cannot count.** It was left reading a kind
  nothing writes, declared as known debt. The review was right that a declaration must not also be
  what keeps a wrong answer on a person's screen: the mechanism for saying so was already in this
  diff. Both message categories now render undetermined, and the declaration is gone — the staleness
  test required its removal once the kind stopped being read.

## Out of scope, recorded rather than widened

- **Ingestion of raw channel messages.** The bundle now says it cannot determine the count; nothing
  yet produces one. Issue #32.
- **`omw daemon status` and `omw model show` disagree on the exit code** for an unreadable
  credential. Both print the same undetermined sentence, byte for byte — product drove this and
  corrected the review's stronger reading of it — but one exits 3 and the other 0, so a script
  learns two things about one machine. Recorded on #66 and #67. **#67 does not close until it is
  settled**, and the tension is architectural: something has to rule on whether the daemon's report
  and a caller's command may differ in their codes.
- The Issue's other smaller findings (`channels status <unknown-id>`, inconsistent usage exit codes,
  `--help` not universal, no `omw version`, two `draft` verbs, tense after a clean stop). Named in
  the Issue, not in its acceptance criteria.
- Building an actual producer for `git` or `token_usage`. Criterion 2 accepts either half; this
  change takes the half that stops the client claiming a quiet day it cannot observe.

## What could not be determined

Whether `omw report run` renders correctly for a subject that HAS a producer and is genuinely empty
cannot be driven through the CLI, because no subject in this build has a producer. That state is
driven at the package level, where a subject with a producer can be added for the duration of one
test — a distinction with one unreachable side is a distinction no test can hold.
