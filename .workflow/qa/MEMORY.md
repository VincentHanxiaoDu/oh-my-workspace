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
