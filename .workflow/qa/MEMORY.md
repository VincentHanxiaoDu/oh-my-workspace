# qa memory

**Mine. Newest first. Delete what stops being true.**

Verification *discipline* lives in `AGENT.md`. This file is for facts about **this project** that
cost time — especially dead ends, so the next round does not walk into them again.

---

## 2026-08-03

### A monitor event names a *state*, not a *head* — re-read the checks before diagnosing
A `RED <gate>` event is true at the moment it is emitted and stale immediately after. #125 was
announced `RED Branch name and commit convention`; by the time it was looked at, that gate was
`SUCCESS` on the current head and a later run was still in flight. Diagnosing from the event alone
would have produced a defect report for a failure that no longer existed.

The first check was also *the wrong instrument*: `gh run view --log-failed` on the newest run
returned **empty**, which reads as "no naming failure found" and is indistinguishable from a broken
query. The control — listing the runs at all — showed five runs, the newest still in progress with
`conclusion: ""`. **Read `gh pr view <n> --json statusCheckRollup` at the head; do not infer a
gate's colour from a log grep.** An empty `conclusion` is *in progress*, which is a third state and
not a pass.

### `check-review.sh` ignores the sha you pass for its range
It derives the commit range from the **worktree's `HEAD`**, not from argument 1. Run it from a
worktree at `origin/main` and the range is empty, so it fails on *"no commit carries an `Agent:`
trailer"* **before reaching the verdict parser** — and every arm of a comparison then "agrees" for a
reason unrelated to the test. **Set up: a detached worktree at the branch head, and copy in whichever
`check-review.sh` you mean to test.** Three attempts lost to this, then a fourth on a different day.

### A verdict can be aged out by a commit that touches no code
A push retires an outstanding refusal — correct when the push changes the code, wrong when it does
not. On #115 an **archive commit** (three `R100` renames, one spec file, zero code) cleared a
substantive `changes-requested`, and the defect it named is live on `main`. Carried on #108. When a
refusal matters, re-check the head before assuming it still stands.

### `omw outbox mode set review`, not `omw outbox mode review`
The short form is accepted-looking and leaves the draft in **manual** mode, so review never runs and
the output says *"review: not run — the mode in effect does not review drafts"* — which reads as a
product behaviour and is not. **Set it, then confirm with `omw outbox mode` before testing.** Two
reproduction attempts wasted; I nearly reported "could not reproduce" on a real defect.

### A leftover store makes `store create` refuse for the wrong reason
`/tmp` state from an earlier run gives *"this device already has a store"* — a refusal, but not the
sync-directory refusal you were testing. Fresh `HOME`/`XDG_DATA_HOME`/`XDG_CONFIG_HOME` per run, all
`OMW_*` unset, parents pre-created, store made by `omw store create`.

### The suite is not container-portable, and most of the loss is silent
Measured against this Mac (0 skips, all green) as control:
`root` → **12 guards skip silently**; no `jq` → **21 more skip silently** plus one loud failure;
no `lsblk`/`cryptsetup` → `omw health` exits 3 and one `internal/commands` test fails.
Only the last announces itself. **Count skips, never trust `ok` alone.**
*(The image recipe itself belongs in `PROJECT.md` when that exists — it is every role's, not mine.)*

### What did NOT work
- **Raising the watch interval to fix output volume.** 60→300→600→1800s, and the monitor still got
  stopped for output. `watch-prs.sh` re-emits `MERGED` for up to 20 closed PRs **every poll** with no
  dedupe, and a role's own PRs emit twice. The volume is per-poll, not per-minute — raising the
  interval cannot fix it. On #32.
- **`gh api rate_limit` as an outage diagnosis.** It read 4896/5000 while every call 403'd, because
  the secondary (burst) limit is not reported there at all. The endpoint that would tell you is the
  one exempt from the thing you are diagnosing.
