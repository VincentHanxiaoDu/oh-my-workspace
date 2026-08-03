# A landed refusal is retired only by the reviewer that made it

## Why

`11605b5` enabled self-review and stated its own invariant: *this widens who may certify, never what
counts as certified.* **The invariant did not hold.** An author erased an independent refusal by
posting a self-approve after it — same head, no code change, no new commit.

Driven against the shipped gate, with both controls:

```
CONTROL  independent refusal only            rc=2   ::error::the current review requests changes
CONTROL  author self-approve only            rc=3   SELF-REVIEWED — no independent agent has looked
TEST     refusal THEN author self-approve    rc=3   SELF-REVIEWED — no independent agent has looked
```

The test row is byte-identical in outcome to the no-refusal control. The published sentence is also
false in a second way: it says no independent agent has looked, and one had — and had said no.

The cause is one line. The gate selected exactly **one** verdict block, `| last`, and computed
`refused` from it alone, so an earlier `changes-requested` was never read. The defence-in-depth check
at the independence branch is sound and simply never saw it.

This is the same selector that made #65 exploitable: #65's accidental forgery failed to take effect
*only* because a genuine verdict came later and `last` won. **One takes the wrong identity from the
block; the other takes the wrong block.**

## What Changes

- **Every verdict for the current head is read**, not only the most recent.
- **Each reviewer's most recent verdict is kept, and the gate refuses while any of them requests
  changes.** The refusal names the reviewers whose objections are outstanding, so the author does not
  have to read every comment to find out whose.
- The most recent verdict still decides *who* is checked for independence. It no longer decides
  *whether anything was refused*.
- Attribution is now checked for **every** block naming the head, not just the certifying one — an
  unattributable block might be a refusal, and stepping over it to reach a later approve is this
  same defect wearing a different hat.

## The rule chosen, and why

**A refusal is cleared only by a later verdict from the same reviewer.**

- *A reviewer changing its mind* is the only thing that should retire that reviewer's refusal, and it
  must stay possible — otherwise a refusal could never be withdrawn by the person who made it.
  (Criterion 4.)
- **Nobody else gets a vote on it.** Not the author, which is this Issue. And not a second
  independent reviewer, which the Issue records as the pre-existing half of the same defect and calls
  less alarming — it is the same act, overriding somebody else's judgement by posting after them.
  Two reviewers who disagree resolve it the way people do: the one who refused withdraws. This is
  **stricter than the Issue strictly required**, and it is called out here because it changes
  behaviour that predates the self-review policy.
- **The escape is the push.** A verdict is bound to a head sha, so changing the code makes a new head
  and every earlier verdict stops applying. A refused branch is never trapped; it is fixed.
  (Criterion 3, driven on a fixture that actually advances its head.)

## Stacked on #65

This branch is cut from **#65's final commit**, not from `origin/main`, because both edit
`check-review.sh` and criterion 4 of this Issue depends on #65's work: distinguishing *a reviewer
withdrawing its own refusal* from *the author overriding somebody else* requires knowing who posted
each verdict, which before #65 the gate took from the comment text. Merge #65 first.

They are kept as two changes because they are two defects with two different remedies, and either
fix is worth having without the other.

## Impact

- Affected specs: `machinery`
- Affected code: `.workflow/bin/check-review.sh` (framework-owned — declared in
  `internal/machinery/framework-local-commits.txt`; the real fix belongs upstream in agent-dev-flow),
  `internal/machinery/reviewgate_test.go`
- `pr-authors.sh` is untouched; both self-tests were driven anyway because the gate calls it.
- The archive-only exemption is untouched.
- **Operational consequence, not fixed here:** any pull request currently sitting on an erased
  refusal will go red once this lands. That is the correct state and it is the point, but it means
  heads that read green today will not afterwards. The Issue names #38, #46 and #53; auditing them is
  not part of this change.
