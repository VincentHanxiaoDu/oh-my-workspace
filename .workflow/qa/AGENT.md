# Project-specific instructions for the qa role

**This file is yours. The installer creates it once and never overwrites it.**

## Sign every comment `[qa]`, on the first line, alone

**Every comment on an Issue or a pull request begins with `[qa]` on its own first line** — before any
heading, bold or prose. `queue.sh` matches `startswith("[qa]")` to decide what this role has already
looked at, so a comment without it is not merely untidy: it does not count, and the queue re-offers
the work to the next agent.

**For a review verdict it is a hard refusal, not a formatting preference.** `check-review.sh` takes
the reviewer from the `[role]` marker on the comment and **deliberately will not fall back to the
`Reviewed-by:` name inside the block** — reading the name from the text is the hole #65 was about,
which let one role mint another's approval. A verdict with no marker is refused as
*"WHO POSTED IT COULD NOT BE DETERMINED, so it certifies nothing"* — an undetermined, explicitly not
a finding that the review is absent or forged.

**And a second comment cannot repair the first.** Every verdict for the current head is read, not
only the last (#82), so an unattributable block keeps refusing until it is **edited in place**.

Measured cost on this board: **three round-trips on #131 and #134 in one session**, because a
dispatched reviewer posted verdicts headed `Reviewed-by: reviewer` with no `[reviewer]` line. Both
times the gate was right and the dispatch instructions were wrong. **When you dispatch a reviewer,
put the marker rule and the edit-in-place rule in the brief.**

`make ci` is the suite. Verifying means running `omw`, not reading a test name.

**The negative guarantees are where the defects will be, and they are the hardest to drive:**

- **No network without a hub.** Observe outbound connections; do not take the code's word.
- **No implicit daemon start.** Run a command with no daemon and confirm it says so.
- **A missing value never renders as a real one.** `could not be determined` must differ from
  `not enabled` byte for byte.
- **The store refuses a synchronising directory.** Point it at one and watch it refuse.
- **An interrupted publish leaves the note in the outbox** — not published, not lost.

**A green `make ci` is not a verification.** It says the tests passed; whether they test what the
Issue asked is yours.

## Before you believe a result, check the instrument

**Everything below is a mistake made here, in one session.** None was a wrong conclusion from good
evidence; each was a confident answer to a question nobody asked.

1. **Confirm a mutation applied — by `git diff`, not by grep and not by "it still compiled".** Four
   mutations here matched nothing and reported a serene `ok`. One did not compile, which measures
   nothing at all. One edited the wrong file: `watch-prs.sh` when the defect was in `watch-queue.sh`.
   One changed only a message string, and a test that rightly does not pin prose survived it — a
   cosmetic mutation surviving proves nothing about the logic.
2. **Prove the test was selected.** Count `=== RUN` against `grep -c '^func Test'`. A `-run` pattern
   matched 2 of 6 tests here and the run reported `ok`; another matched a fraction three more times.
   Prefer the whole package to a name filter.
3. **`-count=1`, always.** The cache does not invalidate on a changed shell script.
4. **Run a control on every empty result.** A wrong pattern and a real absence are identical. A grep
   for `searchGrant` came back empty because the pattern was wrong; the symbol was there all along.
5. **Check the range is not empty.** `check-review.sh` derives its range from the worktree's `HEAD`,
   not from the sha you pass. Run from a worktree at `origin/main` and it fails on missing trailers
   *before reaching the verdict parser* — both arms of a comparison then "agree" for a reason that has
   nothing to do with what you are testing. Three attempts were lost to this.
6. **A diff against a `main` that has moved is not this branch's work.** It shows main's later commits
   in reverse. On an archive review — where deletions are what you are hunting — that reads as "this
   pull request deletes a feature". Check `git merge-base` first; if it equals `HEAD`, the branch is
   already contained in main and there is no delta to read that way.
7. **Isolate the environment.** Fresh `HOME`/`XDG_*`, every `OMW_*` unset, store parents pre-created,
   and the store made by `omw store create`. A leftover store in `/tmp` made `store create` refuse for
   an unrelated reason, which was nearly recorded as the behaviour under test.

**The detector that never failed: a result you cannot reconcile with what the code plainly does is a
broken measurement, not a finding.**

## What a green run does not tell you

`go test` prints `ok` for a package that skipped every interesting arm.

**The numbers below are method-sensitive, so the method is stated with them. A figure you cannot
re-derive is not a measurement** — and an earlier version of this file carried bare counts that
nobody could reproduce, which #116 found by trying.

- **Remove `jq` and the suite goes quiet, not red.** Driven by two PATHs that are identical
  3,112-entry symlink mirrors differing in **one link**, `go test ./... -count=1`, counting `--- SKIP`
  and `^FAIL`: **control 1 skip / 0 failures, without `jq` 27 skips / 3 failures — a delta of 26.**
  A cruder first attempt that stripped `PATH` instead gave 28/5. **The two methods disagree, which is
  the point**: quote the method or the number means nothing. (This measurement is #116's, not mine;
  the version I originally wrote said "21 more, plus one loud failure" with no method and does not
  reproduce.)
- **As root, the unreadable-file guards skip** — `chmod 000` cannot make anything unreadable for uid
  0, so every test of "unreadable is not the same as absent" declines to run. Those are the §4.3
  guards, the discipline most of this project's fixes are about. **The count is undetermined**: it
  needs a root run and I have not done one. An earlier version stated 12 as though driven. Treat the
  direction as established and the figure as unmeasured.
- Without `lsblk`/`cryptsetup`, `omw health` cannot determine full-disk encryption and exits 3.

**So count the skips, and state how you counted.** A run whose skip count you have not read is a run
you have not read; a count whose method you have not written down is one nobody can check.

## Closing an Issue is not the same act as verifying a pull request

**Four Issues were closed here on the pull request's fix rather than the Issue's criteria.** The four
carried forward on **#108** are **#64, #77, #95 and #101** — check the pointer before trusting this
list, because an earlier version of this file named `#82` (which #108 does not carry) and omitted
`#77` (which it does), so a reader following it landed on a different set.

Each closure carried real driven evidence: mutations killed, controls run, behaviour observed. The
evidence was sound and it was evidence for the wrong proposition — *that the fix works*, not *that
the Issue is finished*.

The criteria that get skipped are the ones the pull request did not touch, which is exactly where an
Issue's remaining work lives. On #95 the reason the Issue was unfinished was written **into the
comment that closed it**.

**So: read the Issue's criteria list before closing, and answer them one at a time.** The pull
request is the unit of verification. The Issue is the unit of closure. They are not the same size.

**A `Refs` trailer says *touches*, not *fixes*.** #120 was green, independently approved, and refs
#116 — and its diff was `.workflow/dev/AGENT.md` while every one of #116's criteria was about
`.workflow/qa/AGENT.md`. Read the diff, not the title.

**An Issue carrying criteria you have not met cannot just be closed** — closing destroys them. The
carry-forward has three parts and the two most often dropped are the last two:

1. **Carry them forward verbatim** into a new or existing Issue — not summarised, not paraphrased.
2. **Say what the build does in each open region**, so the next reader knows the current behaviour
   and not only that something is outstanding.
3. **Say on the closure where they went.** A closure that carries criteria to an Issue it does not
   name leaves them findable only by whoever already knew. #108 is the model for all three.
