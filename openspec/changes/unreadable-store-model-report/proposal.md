# A store that could not be read is not a store with no model in it

## Why

`modelViewFor` in `internal/daemon/model_report.go` — the resolution behind the control API's
`Report`, and so behind `omw daemon status` and `daemon.Inspect` — collapsed every failure of
`store.Open` into `model.Read(getenv, nil)`:

```go
s, err := store.Open(storeRoot)
if err != nil {
	return model.Read(os.Getenv, nil).View()   // every error becomes "no store"
}
```

`nil` is `model.Read`'s way of saying **"this caller has no store"**, which is a determined fact and
renders as `model: no provider is chosen`. That reading is right for a machine with no store. It is a
lie for a machine whose store is there and would not open, and `model.Read`'s own contract says so:

> *s MAY BE NIL, and nil means 'this caller has no store', NOT 'the store could not be read'.*

Driven on this branch before the fix, with a real store made with `store.Create` in both arms, the
`model:` block for a readable store with no model and the `model:` block for the same store at
`chmod 000` were **byte for byte identical** — `cmp` clean. Every other field in that same report
correctly said `could not be determined`; `model` alone asserted a fact about a store nobody could
read. That is this project's central rule broken (§4.3): `could not determine` and `determined to be
nothing` do not share a rendering and do not share an exit code.

The two surfaces also disagreed, which is the criterion-18 failure. `internal/commands`' `modelStore`
already separates `store.ErrNotFound` from every other error and exits 3, so at the same moment on
the same store `omw model show` told the truth and `omw daemon status` did not. A caller filtering
for the model answer — exactly what an agent API consumer does — got the determined negative alone.
"Another field mentions it" is not "this field does not lie".

## What Changes

- `internal/daemon/model_report.go` — a new `modelConfigFor` keeps the **three** outcomes of
  `store.Open` as three: opened, `store.ErrNotFound` (the filesystem answered; the environment alone
  is the configuration, as `omw model show` also treats it), and any other error (`tri.Undetermined`
  for provider AND credential, carrying the CLI's own two sentences in `Config.Why`).
- `internal/daemon/model_report_unreadable_test.go` — the assertion, with the **control** in it.
- The wording is not new. The undetermined arm carries `internal/commands`' existing sentences and
  goes through `model.View.Render`, the single renderer both surfaces call, so the CLI and the
  control API cannot word this state differently.

## Relationship to PR #48, now MERGED to `main` at `9b17eaa`

**#48 did not fix this, and its merge did not fix it.** #48 fixed the same class of defect one
surface out: it added a new file, `internal/daemon/agent.go`, whose `agentSources` keeps the three
outcomes of `store.Open` as three for the AGENT API. It never touched
`internal/daemon/model_report.go`. On merged `main`, `git log -- internal/daemon/model_report.go`
still shows `3d7a69c` — Issue #18's original commit — as its most recent change.

Established by driving merged `main`, not by reading the diff:

- An `omw` built from `9b17eaa`: readable-store-with-no-model and the same store at `chmod 000`
  produce `model:` blocks that are `cmp`-identical. The defect is on `main`.
- The test in this change is RED against `9b17eaa`.
- **Mutation, as the positive control.** On a scratch copy of `9b17eaa`, #48's three-arm switch in
  `agentSources` was replaced by the collapsing form and the removal confirmed by grep. #48's own
  two tests went from PASS to FAIL, naming the collapse. So those tests do guard their surface, the
  harness can see a fixed surface, and the identical bytes above are the defect rather than a broken
  rig. Two independent surfaces.

**The duplication #48 left is now closed here, since it can be.** While #48 was open, `agentSources`
holding its own copy of the three arms and this change holding another was unavoidable without a
conflict. With #48 merged, this branch rebases cleanly onto it (no conflict, as predicted) and the
arms move into one function, `modelConfigFrom`, which both surfaces call:

- `modelConfigFor(storeRoot, getenv)` opens the store — the control API's Report path.
- `modelConfigFrom(storeRoot, s, err, getenv)` takes an already-open store — `agentSources`, which
  needs that handle for its other sources anyway, so nothing is opened twice.

The wording and the exit status are now one definition rather than two that agree today. #48's own
two tests, which this change did not write, still pass against the shared helper — that is the
evidence the de-duplication preserved its behaviour.

## Out of scope

`internal/model` itself, which is already correct and three-valued — the defect was only ever in this
wrapper, and testing `model.Read` again would prove nothing about it. `internal/commands`' CLI half,
which already behaves. The report's other fields, which were already undetermined-correct.
