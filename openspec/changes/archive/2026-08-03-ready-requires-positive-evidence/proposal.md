# READY fires when no gate has reported, so absence of red reads as green

## Why

`READY` is the signal a verifier acts on to **merge**. It fired when the gates had not run.

```sh
watch-prs.sh   [ "${pending:-0}" -eq 0 ] && emit READY "$num" "$title"
watch-prs.sh   pending=$(... '[.check_runs[]? | select(.status!="completed")] | length' ...)
```

`pending` counts check runs that are **not yet completed**. With **no check runs at all** the array
is empty, its length is `0`, and `READY` fires. The condition is vacuously true.

**The two reds above it cannot save it**, and that is what makes this a class of defect rather than a
typo: both are existence-dependent in the same way. `FAILING` needs a `failure`/`error` status to
EXIST; `CHANGES` needs a review to EXIST. On a head where nothing has reported, all three fall
through to the permissive answer.

### Measured, not argued

One sweep of one board emitted six `READY` lines: **three genuine and three for heads with nothing
reported on them at all** — a 50% false-positive rate on the signal that decides what gets merged,
with two of the false ones on the release's blocking branches and one of those carrying an
unaddressed `changes-requested` and a scope ruling.

Reproduced deterministically on #46 head `4e9ca30c`, twice seconds apart:

```
$ watch-prs.sh dev --sweep | grep ' #46 '
READY #46  feat(publish): the outbox or the hub, never both

$ pr.sh state 46
  NOTHING HAS REPORTED on this head yet. That is not a pass — it is no answer.
```

**`pr.sh state` is correct.** Two derivations disagreed; the wrong one was the automated signal and
the right one was the command a person has to think to run. It fires most readily exactly when a
branch is most in flux — in the seconds after a push, which is when a fix agent hands off and a
verifier looks.

This is the same shape as #79 (a failed author lookup read as an empty author set) and #84 (an
unplaceable verdict discarded in silence): an absence resolved to the permissive answer. The others
cost a round; this one costs the gate.

## What Changes

- **`READY` requires positive evidence**: at least one **completed** check run, nothing still
  running, and a review verdict positively read as `success`. The absence of a refusal is not the
  presence of an approval.
- **`NO-ANSWER` is a new event**, naming which of the three no-answer cases it is. Suppressing the
  line instead would make silence mean both "nothing to report" and "no answer yet" — the same
  collapse one level up, in the file that exists because a dead watch and a quiet queue look
  identical. The wording is carried across from `pr.sh state` rather than reinvented.
- **A failed commit-status lookup is reported.** It was `if st=$(...); then ... fi`, so a failed
  verdict lookup fell through to `READY` with nothing said — #79's rule, in the branch that decides
  what gets merged.
- **The monitor is covered, and that is measured rather than assumed.** The Issue drove only
  `--sweep` and declined to claim the monitor was affected without seeing it. It is the same loop
  with an `exit 0` after one pass, and both entry points are now driven in the Go test and in the
  self-test. **Restoring the original defect turns both red.**
- **A `--sweep` that stands down for a budget hold now ends.** Found while building the fixture: it
  slept and continued forever, so the one-pass fallback for a dead watch hung exactly like the watch
  it replaces.

**The real fix belongs upstream in agent-dev-flow.** `.workflow/bin/` is replaced wholesale by the
next `install.sh` run, so the file is declared in `internal/machinery/framework-local-commits.txt`.
What survives a refresh is `internal/machinery/readyevidence_test.go`.

## Out of scope

- **`queue.sh`'s merge-queue derivation.** It already has the right vocabulary and was not the signal
  that misfired.
- **Auto-merge arming in `pr.sh`.** A different decision point, not measured here.
