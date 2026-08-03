# A failed author lookup is not an empty author set

## Why

`reviews_waiting` in `.workflow/bin/queue.sh` decided independence from

```sh
authors=$(.../pr-authors.sh --pr "$num" 2>/dev/null || echo "")
```

which converts a **failed** lookup into an **empty author set** — stderr discarded, non-zero exit
swallowed. An empty set already carried two meanings and the script handles both: no `Agent:` trailer
anywhere is a commit defect the naming gate reports, and trailers that are all spec-only means nobody
authored product judgement so every role is independent. There is a third, and it was not handled:
**the question could not be answered.** That is this project's own rule — `could not determine` and
`determined to be nothing` must never collapse — broken inside the routing that enforces independence.

The path is reachable. The existing guard only fires when the `--all-trailers` follow-up ALSO comes
back empty, and a **secondary** rate limit fails intermittently: `--pr N` fails and yields an empty
set, `--pr N --all-trailers` succeeds and yields a trailer, the guard does not fire, and
`grep -qx "$role"` against an empty string matches nothing — so the pull request is offered to every
role including the ones that wrote it. Observed live: `queue.sh dev` printed
`#46 ... run /review-pr 46   (built by )` for a branch carrying nine `Agent: dev` trailers.

The cost is not a cosmetic blank. `queue.sh` promises that if it offers you a pull request the gate
will accept your verdict on it. The gate re-derives authorship from git at verdict time, where the
lookup does not fail, so it sees `dev` and refuses. **The role does the whole review and has it
rejected** — #61's complaint in a different lookup. And in the other direction a role told it is
independent may believe it, and self-review is the one thing this machinery exists to prevent.

This is the sibling of the requirement #76 shipped for the *verdict* lookup. That one exists; the
author lookup had none.

## What Changes

- A new requirement under `review-assignment`: the queue exits non-zero and offers nothing when who
  built a pull request cannot be determined.
- `internal/machinery/authorlookup_test.go` — the durable half. It **executes** the installed
  `.workflow/bin/queue.sh` against a stub `gh` that fails the author lookup while every other call
  succeeds, and asserts the queue exits non-zero and offers nothing. It does not restate the script's
  logic; a refresh that removes the fix turns this suite red.
- `.workflow/bin/pr-authors.sh` — `--pr` exits **3** when the API cannot be reached, for both the
  commit-list query and the per-commit file-list query. An unreadable file list is not a diff that
  touched nothing.
- `.workflow/bin/queue.sh` — both call sites stop swallowing it, and say what failed.

**The real fix belongs upstream in agent-dev-flow.** `.workflow/bin/` is replaced wholesale by the
next `install.sh` run, so the two shell edits are declared in
`internal/machinery/framework-local-commits.txt` as the debt they are. What survives a refresh is the
Go test, and the two scripts carry matching `--self-test` arms so the upstream patch arrives with its
own proof.

## Out of scope

**The archive-only case, deliberately.** An archive-only pull request yields a *determined* empty
author set, and `check-review.sh:72-91` treats that as "every role is independent" on purpose — that
exemption is what unblocked a board of eleven, and folding it in would break the property it exists
for. It is a separate question, recorded on #32. Only the could-not-determine case is here.

`--range`, which reads this clone rather than the API, has no such failure mode and is unchanged.
`check-review.sh` is its only caller and needs no change to go with this; its self-test was driven to
confirm that.
