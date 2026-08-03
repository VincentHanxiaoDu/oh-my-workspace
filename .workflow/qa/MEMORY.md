# qa memory

**Mine. Newest first. Delete what stops being true.**

Verification *discipline* lives in `AGENT.md`. This file is for facts about **this project** that
cost time — especially dead ends, so the next round does not walk into them again.

---

## 2026-08-03

### This file can be shadowed by a stale untracked copy — check it against `origin/main` first
The shared working tree runs **behind** `origin/main` (8 commits, once). A memory file committed
after that tree's `main` is still **untracked** there, so the old copy sits on disk and **that is the
one the role prompt loads**. It happened with this file: the prompt served the pre-correction 54-line
version, including the entry already proven wrong, while `origin/main` had the corrected 59.

**The obvious check misreports it.** `git diff origin/main -- <path>` shows the file as *wholly
deleted* — diff against a commit ignores untracked files — so the command that looks like it would
catch this produces a phantom difference on a file that is byte-identical. Use `cmp` against
`git show origin/main:<path>`, not `git diff`.

**This presents identically to `AGENT.md` item 6, and item 6's remedy will not catch it.** Both show
a phantom deletion; there the cause is a moved `main` and `git merge-base` finds it, here the cause
is an untracked path and `merge-base` *can* return a clean answer while you are still wrong — it does
whenever the tree has **diverged** rather than merely fallen behind, which is the case that happened.
In a tree that is a pure ancestor, item 6's test does fire — but for the wrong reason, saying nothing
about a file that is on disk and byte-identical. **Tell them apart by whether the path is tracked at
`HEAD`, not by the diff**; that works in both cases.

The mirror case exists too: a file committed on `main` can be **absent** from the tree entirely.

**So at the start of a round, reconcile this file with `origin/main` before trusting it**, and treat
`??` in `git status` on a `.workflow/` path as a shadow until proven otherwise.

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

### Testing `check-review.sh`: the set-up, not the trap
The trap itself — it ranges over the worktree's `HEAD`, not the sha you pass — is
`AGENT.md` item 5, and lives there. What that item does not give you is the recipe:
**a detached worktree at the branch head, with whichever `check-review.sh` you mean to test copied
in.** Testing the gate is not the same as running it.

### A verdict can be aged out by a commit that touches no code
A push retires an outstanding refusal — correct when the push changes the code, wrong when it does
not. On #115 an **archive commit** (three `R100` renames, one spec file, zero code) cleared a
substantive `changes-requested`. Carried on #108. When a refusal matters, re-check the head before
assuming it still stands.

### `omw outbox mode set review`, not `omw outbox mode review`
The short form is accepted-looking and leaves the draft in **manual** mode, so review never runs and
the output says *"review: not run — the mode in effect does not review drafts"* — which reads as a
product behaviour and is not. **Set it, then confirm with `omw outbox mode` before testing.** Two
reproduction attempts wasted; I nearly reported "could not reproduce" on a real defect.

### What did NOT work
- **Twice-wrong on why the watch floods. The live answer: merging causes it.** `emit()` keys on
  `state|number|detail` (`:404`), and the MERGED detail carries main's state line (`:757`), which
  carries **main's sha** (`:482`). So `MERGED|131|… main is GREEN at <sha-a>` and the same event at
  `<sha-b>` are different keys: **when main moves, every recently-merged PR re-emits.** Observed —
  one merge replayed twelve already-reported merges and the monitor was stopped for volume.
  The flood is triggered by the one act the role is here to perform, so a quiet board stays quiet
  and a working board silences its own watch. Reported on #32.
  *Two earlier answers I believed and should not have: "no dedupe, re-emits every poll" (false —
  `seen` at `:399` is outside the loop at `:527`), then "first-poll burst, then near-silence" (true
  of a still board, and it does not explain a stoppage an hour in). Both times I stopped at the
  first explanation that fit what I had already seen.* **Do not drop `watch-prs`** — I ran
  queue-only for a session on the first wrong answer, which is the half-blind state the supervisor
  exists to prevent.
- **`gh api rate_limit` as an outage diagnosis.** It read 4896/5000 while every call 403'd, because
  the secondary (burst) limit is not reported there at all. The endpoint that would tell you is the
  one exempt from the thing you are diagnosing.
