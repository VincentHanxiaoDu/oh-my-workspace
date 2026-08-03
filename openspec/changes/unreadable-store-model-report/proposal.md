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

## Relationship to PR #48 (`dev/feat/16-agent-api`), which is open and approved

**#48 does not fix this.** It fixes the same class of defect one surface out: it adds a new file,
`internal/daemon/agent.go`, whose `agentSources` keeps the three outcomes of `store.Open` as three
for the agent API. It does not touch `internal/daemon/model_report.go`, and `modelViewFor` — called
from `daemon.go:247` and `state.go:568` — was left as it was. Verified by diffing the fetched branch
against `main`: `model_report.go` is not in it.

**There is no file conflict.** #48 touches `internal/daemon/{agent.go,agent_model_test.go,control.go,
daemon.go}`, `internal/agentapi/`, `internal/commands/agent_cmd*.go` and `openspec/`. This change
touches `internal/daemon/model_report.go` and adds one test file. The two sets are disjoint, so
either may land first.

**One follow-up is left deliberately undone**, because doing it here would create the conflict this
avoids: after #48 merges, `agentSources`' inline undetermined arm and `modelConfigFor` are the same
three sentences written twice. `agentSources` should call `modelConfigFor`. That is a one-line
change against a file this branch does not have, and it belongs to whichever pull request lands
second.

## Out of scope

`internal/model` itself, which is already correct and three-valued — the defect was only ever in this
wrapper, and testing `model.Read` again would prove nothing about it. `internal/commands`' CLI half,
which already behaves. The report's other fields, which were already undetermined-correct.
