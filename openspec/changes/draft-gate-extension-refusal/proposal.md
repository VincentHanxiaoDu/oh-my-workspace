# The draft gate names a broken extension, and the ordering rule is stated once

## Why

PRD §3.13 says "no model configured is not a broken client". Issue #21 observed that this sentence
"becomes a lie the moment a failed load is dressed up as an unconfigured one": a person told to
configure a model has already configured one, and the real fault — an extension that will not load —
is never looked at.

Issue #21 closed that at the review gate. **`omw outbox draft` was never behind that gate.** Its
`case drafts.ModeReview:` held its own copy of the three-way branch on whether a model is
configured, so with a broken extension and no credential a person writing a draft saw:

```
omw outbox draft: no model is configured, and review mode checks your drafts with your own model
                  (code: no-model)
state reason: you chose review mode and no model is configured
```

That is the exact sentence §3.13 and Issue #21's criterion 10 forbid — the code, the headline and
the **persisted state reason**, which outlives the terminal. The `model:` line printed above it did
name the extension failure, so a reader was not blind; nothing a machine reads said so.

**This is the third instance in one day of one shape: a check placed at a caller, and a second
caller that does not know about it** — #10's agent API bypassing `publish.Transfer`, #21's
`LoopFactory` bypassing `ExtensionFactory`, and this. Each time the guard written to prevent it
could not see the bypass, and for the same reason the bypass existed: the guard's reach was one
package or one code path, and the next caller is by definition somewhere else. #108's table calls
this a reach failure; this is the fifth.

So adding a third call at a third site would fix the symptom and leave the shape intact. **The
ordering rule moves into one function, and a guard over every package fails any package that
restates it.**

## What Changes

- **One decision, for every review-mode gate.** `outboxReviewModelRefusal` answers "is the model
  ready, and if not what is the true reason?" — all three situations, the exit code, the headline
  and the persisted state reason. `outboxReviewGate` and `omw outbox draft` now hold a call to it
  and no branch of their own. It supersedes `outboxExtensionRefusal`, which answered only one of the
  three and left its callers to answer the rest.
- **`omw outbox draft` names a broken extension** — `model-provider-extension-failed-to-load` in
  the exit code, the headline and the persisted state reason.
- **A working extension with no credential still says `no-model`**, at both gates, because on that
  machine it is true. The ordering — extension before credential — is `model.Readiness`'s and is
  not restated.
- **A structural guard over every package under `internal/`.** Outside `internal/model`, which
  defines them, `model.ErrNoModel` and `model.ErrProviderFailedToLoad` may be referenced from
  exactly one function. A second reference anywhere is a second copy of the ordering rule, free to
  drift. The guard is driven against a bypass planted in a different package and required to name
  it, because the three guards that failed today had only ever been green.

The one sentence a caller may vary is what that command has and has not done ("this draft has NOT
been checked" versus "nothing has been published"). The code and the persisted reason are the
decision function's, for every caller.

## Impact

- `internal/commands/outbox_cmd.go` — the two branches replaced by one function and two calls.
- No new dependency. No behaviour change where a credential IS recorded: the decision function
  returns "not refused" on `Configured() == tri.Yes`, so a broken extension with a credential still
  reaches `outboxReviewer` and is named there, as before.
