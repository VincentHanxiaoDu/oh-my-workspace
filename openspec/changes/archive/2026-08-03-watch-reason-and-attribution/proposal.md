# The watches detect the event, then print the part that is not the reason

## Why

Two defects of one shape: the signal fires correctly, and the text attached to it is about something
else — so a correct alarm reads as noise, and noise is what it got treated as.

### 1. The reason is truncated to its head, where the reason never is

```sh
if ! out=$("$here/queue.sh" "$role" 2>&1); then
  echo "LOOKUP FAILED: $(printf '%s' "$out" | tr '\n' ' ' | cut -c1-200)"
```

`2>&1` merges the streams, so `$out` is everything the queue printed before it died and the failure
is the **last** line. Keeping the first 200 characters keeps the headings and the work items and
discards the only part that says what went wrong. Driven, same bytes, same budget:

```
head   LOOKUP FAILED: FEATURES WHOSE WORK HAS LANDED — UAT on main and CLOSE   #9  feat(notes):
       draft notes into the outbox and publish them   #10 feat(publish): three publication modes…
tail   LOOKUP FAILED: …#38 feat(outbox): drafts and modes gh: dial tcp 140.82.116.6:443: operation
       timed out
```

One of them names the outage. The real `dial tcp … operation timed out` sat past character 200, was
cut off, was read as a misfire, and a round of a release day went on proving the watch right.

### 2. A true state attached to a cause that was never measured

> `MERGED #51 … — you merged this — MAIN IS RED at 19f05904 (failure) — YOU merged into it, so this
> is yours to fix before merging anything else`

**`main` was red and the alarm was right to fire.** The merges did not cause it:

```
Build and tests                        success
Generated files not hand-authored      success
Tasks complete                         success
Branch name and commit convention      failure   ← 19f0590's subject is 113 characters, limit 72
```

`19f05904` is a direct push to `main` by the framework, one parent, so the merge-commit exemption in
`check-naming.sh` does not apply to it. Inferring the cause from who merged last is a proxy for
authorship that stops measuring it the moment anything else can redden `main` — and something else
can, **by design**, because the framework pushes to `main`. Sending the merger to fix a commit they
did not write is the same error `pr-authors.sh` exists to end.

## What Changes

- **The reason is the tail**, with the ellipsis inside the same budget so an elided reason does not
  read as a complete one. Applied to every `LOOKUP FAILED` in both watches.
- **A reason that fits is still rendered byte-identically.** Asserted, because a fix that always
  printed a tail would mangle the common case.
- **`MAIN IS RED` names the failing check**, read from the run's jobs, instead of `(failure)`.
- **The attribution is derived or not claimed.** Main's failing commit is compared with the merge
  commit the caller actually produced; where they match it may still say the red is theirs, and
  where they do not it says `CAUSE NOT DETERMINED` and says why it will not guess.
- **A check name that could not be read is not an absent one.** A red run with no failing job
  returned says the check was not determined rather than rendering an empty parenthesis.

**The real fix belongs upstream in agent-dev-flow.** Both watches are framework-owned and replaced
wholesale by the next `install.sh` run, so they are declared in
`internal/machinery/framework-local-commits.txt`. What survives is
`internal/machinery/watchreason_test.go`.

## Out of scope

- **The `head -3` on CI annotations in `watch-prs.sh`.** That list is already the diagnosis rather
  than a log, and its first entries are its most relevant ones — the defect here is keeping the head
  of a stream whose *end* is the reason, which is not that.
- **The owner's standing ruling on this particular red** — leave it, the next merge clears it — is a
  decision about that commit, not about what the watch should print.
