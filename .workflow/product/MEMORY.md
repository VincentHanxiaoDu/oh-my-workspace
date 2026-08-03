# What the product role has learned about this project

**This file is yours, and the installer never overwrites it.** Write to it the moment you learn
something that cost you time and would cost the next round the same time — how this project actually
behaves, where the traps are, and what you tried that did not work.

Not here: work state (that is Issues and pull requests), decisions (those are `[owner-ruling]` on
the Issue), project configuration (that is `.workflow/PROJECT.md`), or how the process works.

Newest first. Date every entry. **Delete what has stopped being true** — a role acting confidently on
a stale note is worse off than one that knew nothing.

## 2026-08-03 — a verdict posted while the gates run is in flight is SILENTLY DROPPED

The `issue_comment` recheck tries to re-run the review job, GitHub answers **`HTTP 403 — already
running`**, the recheck exits 1, and nothing retries or announces it. The verdict sits unread while
the pull request reports `No current review by an independent agent` — *nobody has looked*.

**It bites hardest when a reviewer is prompt**, since the sooner they answer after a push, the
likelier the gates run is still going. So: **after a reviewer reports, confirm their verdict is in
force** — `./.workflow/bin/pr.sh state <n>` — and if the board says no review exists while a verdict
sits in the comments, re-run the review job rather than asking them to repost.

## 2026-08-03 — before believing a red gate, READ THE LOG. It answers in one line.

On one single-file pull request I diagnosed the red wrong **three times running** and the log had it
each time. Two traps behind that:

**Check runs attach to the COMMIT, not the branch.** So a red can be inherited from a *different,
already-closed* pull request that shared the sha, and renaming the branch does not clear it — only
moving the sha does. That one is invisible from the pull request you are looking at, which is why it
cost three wrong guesses.

Read it with `gh run view <id> --log | grep '::error'`. The gates here state their remedy in the
failure text, so the log is not a hint — it is the answer.

## 2026-08-03 — once you dispatch a reviewer, the head is NOT yours to move

Four amends on a one-file change, and the last force-pushed over the exact sha the reviewer had
reviewed. Their refusal became unplaceable and #98's guard red-lined the whole pull request:
*"a verdict … names sha(s) that are not the head and not any commit this repository knows."*

**Fix the branch before you ask.** Every push after that costs them a round and can destroy work
already done. Useful distinction the gate draws if it happens anyway: an **unknown** sha is a verdict
never in force; a **known** sha is merely a stale review, which is silent and fine.

## 2026-08-03 — the working clone is SHARED. `git stash` there sweeps up other roles' work.

`/Users/hanxiao.du/Desktop/vincent/projects/oh-my-workspace` is not yours alone. It routinely
carries **uncommitted edits by dev/flow** to `.workflow/bin/*` and untracked `MEMORY.md` files for
other roles, and it can sit on someone else's branch with an unpushed commit on it.

I ran `git stash` there to move one file to a branch. It took **everything** — and the pull request
I opened contained two files I had never touched. Nothing was lost, but the PR asserted authorship
of another role's in-flight work, and only checking `gh pr diff --name-only` caught it.

**So: never `git stash` or `git checkout -b` in the shared clone. Use a worktree.**

    git worktree add -q /tmp/wt-<thing> origin/main
    # copy in ONLY the file you mean to change, commit there

**And verify before you believe it:** `gh pr diff <n> -R <repo> --name-only` must list exactly the
files you intended. A pull request that opened successfully is not a pull request containing what
you think.

**And branch from `origin/main`, never from the clone's local `main`.** That local `main` can be a
stale merge whose tree genuinely differs from origin's — it has been, on `.workflow/bin/*`, which is
the worst place for it because those are the scripts every role then runs. Fetch and branch from the
remote ref, and the question never arises.

## 2026-08-03 — a green in a listing is not a green on this head

`queue.sh` offered **#87 for merge showing `GREEN`** while GitHub reported `mergeable: false,
mergeable_state: dirty`. Filed as #122 — delete this entry when that closes.

**Check the head before you merge: `./.workflow/bin/pr.sh state <n>`.** What makes it trustworthy is
that it anchors every read to the **current head sha** — `commits/$sha/check-runs` and
`commits/$sha/status` — so a stale green sitting on an older head simply is not on this one, and it
reports `NOTHING HAS REPORTED on this head yet. That is not a pass — it is no answer.`
(`pr.sh:114-116`). `watch-prs.sh` takes the same `completed == 0` → `NO-ANSWER` path (`:720-722`).

**Neither tool reads mergeability at all** — `pr.sh state` never asks for the field, and
`watch-prs.sh` fetches the `pulls?state=open` *list* endpoint, which does not return it. Head-sha
anchoring is the whole mechanism. Do not credit them with a check they do not perform.

## 2026-08-03 — measurement traps that have each cost a round

- **`$?` after a pipeline is the LAST command's code, not the one you care about.** `cmd | tail`
  then `echo $?` reported `tail`. And `${PIPESTATUS[0]}` is already gone if anything ran in between.
  For a sweep: `./.workflow/bin/watch-prs.sh product --sweep >/tmp/o 2>&1; echo $?`.
- **Grep `'^FAIL'`, never count `'^ok'`.** A count of passing packages is not a test result; a round
  reported "9 packages ok" on an already-red tree.
- **zsh does not word-split unquoted variables.** A sweep believed to run 110 invocations ran 5.
  Print and assert the counter, always.
- **`timeout` does not exist on macOS.** Use `go test -timeout`, and split long test+push sequences
  into separate calls — a command timeout once landed a commit whose push never ran.
- **The naive panic pattern `goroutine [0-9]+ \[` scores 0 on a real Go 1.26 dump** (it emits
  `goroutine 0 gp=0x… m=0 mp=0x… [idle]:`). Validate any panic grep against a real `kill -QUIT` dump
  before believing a zero.
- **A `-i` grep can match JSON key names rather than findings** — `grep -i undetermined` "found"
  undetermined-ness in a diagnostics bundle purely via keys like `synchronising_undetermined_why`.
- **Two of your actions preceding a good result do not tell you which one caused it.** I made a
  repair and re-ran a job, saw a gate go green, and reported the repair as the cause; a third party
  had meanwhile deleted the thing that was actually blocking it. The ordering was there to check and
  I did not check it. Change one thing, or establish the order.
- **A wait loop keyed on prose can stop waiting early and look like a result.** Mine polled
  `pr.sh state` until the string `still running` disappeared. **A red makes it disappear while checks
  are still live** — `pr.sh` tests the failure branch *before* the pending one, so one failing check
  suppresses the running line and the loop returns mid-flight. My own captured output showed
  `in_progress Build and tests` and a `RED:` section together, with no running line.
  **Key on `pr.sh`'s exit code — `0` green, `1` red, `2` no answer — never on its wording.**

**The general rule this project keeps teaching: a negative result means nothing until the probe is
shown to fire on something known-present.** State the control, every time.

## 2026-08-03 — a finding without its tree is not a finding

I passed `state.go:81` to dev without saying which tree. True of `main`, already fixed on the
branch. Two reviewers then reported opposite facts and **both were correct**. Always name the build:
branch alone, or branch+main.

Related: gates certify one head against `main`, and reviewers read one branch, so **branch-green and
merged-red is normal** (#46 was). Reviewers must `git merge origin/main` in the worktree *before*
judging, or they are not adding the only thing they can add.

## 2026-08-03 — local fixes to `.workflow/bin/` are deleted by the next framework refresh

`.workflow/<role>/AGENT.md` and `MEMORY.md` survive a refresh. **`.workflow/bin/` and `.github/` are
replaced wholesale** — authoritative list at `internal/machinery/frameworkpaths_test.go`
(`frameworkOwned`). A refresh did exactly this once: it overwrote `pr-authors.sh`, deleted merged fix
#80, and left `main` red with a live fail-open.

**The pattern that survives:** put the alarm in project-owned `internal/machinery/`, and make it
*execute* the installed script rather than restate it. Then a refresh that removes the fix goes red
immediately. `framework-local-commits.txt` lists what is currently unupstreamed — read it before
approving any refresh, and treat a red in `./internal/machinery/` as *the refresh removed a fix*,
not as a broken test.

## 2026-08-03 — what a strong acceptance criterion looks like here

    weak:    "state the method that produced this number"
    strong:  "a second person must reach the same figure from this text alone"

The weak form was met by a correction that reproduced the very defect it was correcting (a reviewer
derived 12 where the file claimed 11). Criteria must be drivable by someone who was not there.
