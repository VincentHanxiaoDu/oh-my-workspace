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

- **Check runs attach to the COMMIT, not the branch.** A red can be inherited from a *different,
  already-closed* pull request that shared the sha. Renaming the branch does not clear it; moving
  the sha does.
- **The commit-message gate refuses closing keywords** (`Closes #<n>`) — because a closing keyword
  closes the Issue at merge, taking closure away from the role that decides it at UAT. Use `Refs`.
  It also requires an `Agent: <role>` trailer.

Read it with `gh run view <id> --log | grep '::error'`.

## 2026-08-03 — once you dispatch a reviewer, the head is NOT yours to move

Four amends on a one-file change, and the last force-pushed over the exact sha the reviewer had
reviewed. Their refusal became unplaceable and #98's guard red-lined the whole pull request:
*"a verdict … names sha(s) that are not the head and not any commit this repository knows."*

**Fix the branch before you ask.** Every push after that costs them a round and can destroy work
already done. Useful distinction the gate draws if it happens anyway: an **unknown** sha is a verdict
never in force; a **known** sha is merely a stale review, which is silent and fine.

**And when two of your actions precede a green, you have not learned which one caused it.** I ran a
repair and a job re-run, saw green, and reported the repair as the cause. The reviewer had deleted
their stranded verdict — the likeliest actual cause — and I never checked the ordering, which I
could have. Two changes, one observation, no control.

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

## 2026-08-03 — `queue.sh` does not know what "mergeable" means. Never merge on its say-so alone.

It listed **#87 as `GREEN` under `PULL REQUESTS TO UAT, MERGE AND CLOSE`** while GitHub said
`mergeable: false, mergeable_state: dirty`. A conflicted pull request has no merge ref, so no gate
ever scheduled — the green belonged to an older head. It renders conflicted-but-untested PRs as
`NO ANSWER YET`, which invites waiting that can never resolve.

**Before merging anything, run `./.workflow/bin/pr.sh state <n>`** — it gets this right and says so
in words (*"waiting cannot resolve it"*). `watch-prs.sh` also gets it right. Only the queue does not.
Filed as #122; delete this entry when that closes.

**And `queue.sh` exits 0 whether or not it errored**, so its exit code tells you nothing about
whether its output is complete. Read what it printed; do not infer from the code.

*(An earlier version of this entry reported a `line 640: built: command not found` error here. It
does not reproduce on any tree — branch, `origin/main`, or the shared clone — and I had attributed
it to the wrong tree. Withdrawn on #122, and deleted here rather than left to mislead.)*

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
