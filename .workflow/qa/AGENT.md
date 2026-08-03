# Project-specific instructions for the qa role

**This file is yours. The installer creates it once and never overwrites it.**

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

`go test` prints `ok` for a package that skipped every interesting arm. Measured here against this
machine (0 skips, all green) as the control:

- **as root: 12 guards skip, silently** — `chmod 000` cannot make anything unreadable, so every test
  of "unreadable is not the same as absent" declines to run. Those are the guards for §4.3, the
  discipline most of this project's fixes are about.
- **without `jq`: 21 more skip silently**, plus one loud failure. The loud one is not representative.
- without `lsblk`/`cryptsetup`, `omw health` cannot determine full-disk encryption and exits 3.

**So count the skips.** A run whose skip count you have not read is a run you have not read.

## Closing an Issue is not the same act as verifying a pull request

**Four Issues were closed here on the pull request's fix rather than the Issue's criteria** — #64,
#82, #95 and #101. Each closure carried real driven evidence: mutations killed, controls run,
behaviour observed. The evidence was sound and it was evidence for the wrong proposition — *that the
fix works*, not *that the Issue is finished*.

The criteria that get skipped are the ones the pull request did not touch, which is exactly where an
Issue's remaining work lives. On #95 the reason the Issue was unfinished was written **into the
comment that closed it**.

**So: read the Issue's criteria list before closing, and answer them one at a time.** The pull
request is the unit of verification. The Issue is the unit of closure. They are not the same size.

**An Issue carrying criteria you have not met cannot just be closed** — closing destroys them. Carry
them forward verbatim, as #108 does.
