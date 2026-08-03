# An interrupted write leaves a destroyed draft reporting itself as a healthy one

## Why

PRD §3.14 says the store survives an interrupted write. For drafts it did not, and the failure was
silent — the worst shape of this project's central defect. Not "could not determine rendered as no",
but **corrupt data rendered as good data**. A person was told their draft was fine when the content
was gone.

`omw outbox draft` SIGKILLed mid-write left 15 damaged drafts out of 60 on the Issue's measurement,
**13 of which reported themselves as healthy**:

```
$ ls -la outbox/d008/          # entirely empty directory: no .state, no body
$ omw outbox state d008
state: drafted — in your outbox, awaiting you; nothing is outstanding and nothing is in flight
EXIT=0
```

Two correct halves made one lie:

- `drafts.Revise` did `os.MkdirAll(dir)` and then wrote into it — no temporary, no rename, no
  fsync. The directory, and sometimes a partial body, became visible before the state file existed.
- `drafts.StateOf` mapped a missing `.state` to `StateDrafted` and documented the mapping as "a REAL
  VALUE, not an absence". Sound reasoning, and load-bearing on an invariant — a draft directory can
  never exist before its state file — that `Revise` broke. The comment asserting it was false and
  nothing in the tree checked it.

**The asymmetry is the argument.** `internal/store` already wrote records through a temporary, an
fsync and a rename. The outbox did not go through it. One writer in this codebase was durable and
the other was not, and the durable one was not the one holding people's unsent words.

Two further findings from the same measurement are fixed here because they are the same sentence
about trust:

- The **stale-lock notice was a 100% false positive**. It fired on 10/10 crash recoveries and on
  5/5 *clean* stop-then-start cycles, so a person could not distinguish crash recovery from a normal
  restart — the entire purpose of saying it. `release()` gave the lock back and left the previous
  holder's pid inside the file. A guard that cries wolf is how a guard stops being read.
- A **refused concurrent write leaked a raw Go error under an empty code**:
  `omw outbox draft: open /tmp/.../000001.body: file exists (code: )`. Every other refusal in this
  product names a code and explains itself, and a blank code is what a caller reads to mean "no
  refusal".

## What Changes

- **A draft becomes visible whole, or it does not become visible.** `Revise` assembles a new draft —
  first revision and state file both — under a staging name no reader looks at, fsyncs it, and moves
  it into place with one rename. There is no instant at which a partial draft has a name. This is the
  `internal/devices` technique applied to a directory: the invalid state is made unrepresentable
  rather than detected afterwards.
- **A revision is added durably and still never overwritten.** The bytes reach disk under a staging
  name and are published with `os.Link`, which is atomic and, unlike rename, refuses an existing
  destination. A writer that miscounted the revision number recounts and retries; one that cannot win
  is refused by name, `draft-write-raced`, with no Go error text.
- **`StateOf`'s missing-`.state` mapping is removed.** A draft directory with no state file is now
  something that went wrong, and the honest answer is that it could not be determined — never
  `drafted`. The invariant the old mapping rested on is asserted directly by a test.
- **`SetState` and the outbox marker are written atomically**, so a killed process cannot leave a
  truncated record of where a draft stands.
- **The lock body is cleared on clean release**, so a body in the lock file now says exactly one
  thing: its writer never reached that line.
- **A structural guard** — an AST test in `internal/drafts` — fails when any function in the package
  reaches a destination path outside the durable writers. This shipped because two writers in one
  tree disagreed and nothing compared them; behavioural tests cannot see an asymmetry, only a test
  that asks every writer the same question can.

## Impact

- `internal/drafts/outbox.go`, `internal/drafts/state.go`, `internal/daemon/lock.go`
- Specs: `notes`
- Not covered, and deliberately not claimed: power-loss durability beyond fsync ordering, networked
  filesystems, and builds where `lockingIsAvailable` is false. The structural guard covers package
  `drafts` only; widening it to every package that writes a person's material is recorded on #32.
