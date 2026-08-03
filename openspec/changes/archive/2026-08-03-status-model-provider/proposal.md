# The one screen reports the model provider, so an undetermined model is not silent

## Why

`omw status` rendered six subsystems — daemon, local store, configured channels, watched projects,
devices registration, hub connection — and **zero occurrences of "model"**, in text and in `--json`.
`grep -rni model internal/status/` found nothing at all.

PRD §2.1's component table lists model providers as part of the client. The six were a closed list
written before Issue #18's model existed, and nothing anywhere noticed when it fell behind.

The cost is a **false silence**, which §4.3 names as its own failure. Driven with a credential file
that exists and cannot be read — genuinely undetermined, not absent:

```
omw model show      → "whether a credential is supplied for it could not be determined —
                       this is NOT 'no credential'"        exit 3
omw daemon status   → the same sentence, verbatim           exit 3
omw status          → no undetermined subsystem, model never mentioned, exit 0
```

The screen that exists to surface problems exited 0 in silence over a subsystem two other surfaces
exited 3 about. `internal/tri`'s own package comment settles what that is: "each is a distinct
answer, and none of them is silence." A person scanning the one screen for problems could miss an
undetermined model entirely.

## What Changes

- **A seventh subsystem, `model provider`**, on both surfaces — the rendered screen and `--json`.
- **It derives nothing.** The sentence is `model.View.Render()`, and the View arrives on the
  `daemon.Report` this screen is already given. That is the same value `omw daemon status` renders
  and the same rendering `omw model show` prints, which is #66 criterion 1 and #41's standing rule.
  No second resolution of the environment was added.
- **Four states, and none of them is `not_working`.** No provider chosen, and a provider chosen with
  no credential yet supplied, are both `not_configured` — determined facts about a person's
  configuration, not failures (§3.13: "No model configured is not a broken client"). Choosing a
  provider this build has no adapter for stays a determined, non-negative fact, said in the View's
  own words. Only a state that could not be ESTABLISHED is undetermined, and it reaches the summary
  and the exit code by the existing route, so the one screen now exits 3 exactly as the other two do.
- **A structural guard**, `TestEveryCapabilityTheDaemonReportRendersHasALineOnTheScreen`. It walks
  `daemon.Report` by shape: a field whose type renders ITSELF — a capability's own projection with a
  no-argument `Render() string` — is a subsystem a person is told about on that surface, and must
  have a line on the one screen. A closed list of seven would have this same defect one Issue later;
  this fails by name on the eighth. It refuses to pass vacuously if it examines no capability.
- **PRD §3.13 re-driven against the new line.** The old zero-hit result was close to vacuous —
  `omw status` printed nothing about the model, so nothing could leak. The sentinel is now shown
  findable at its source file first, and then asserted absent from stdout, stderr and `--json` across
  every configuration.

## The red that was watched

1. The command test failed at `omw status --json reports no "model provider" subsystem at all`,
   against the unmodified tree — the Issue's exact complaint.
2. With the line built, the collapse was **injected**: the undetermined branch was made to render
   `not_configured` / "model: no provider is chosen". Confirmed present by grep on the exercised
   path, and the test went red on both the wording and the exit code (`exited 0; exit 3 means
   something could not be determined and this state is that`). Restored, green.
3. The structural guard was driven the same way: `modelSubsystem` was removed from `Collect` and the
   guard failed naming `daemon.Report.Model`. Restored, green.

All runs used `-count=1`.

## Out of scope

**Issue #68's defect, deliberately.** `daemon.modelViewFor` falls back to an environment-only read
when the store will not OPEN, which reports a recorded choice as absent rather than as undetermined.
That value is #68's and another agent holds it. This screen shows whatever that resolution produces,
faithfully and without a second opinion — so when #68 lands, this line is correct with no change
here. The undetermined cases driven in this change are reached WITHOUT an unopenable store: an
unreadable credential file, and a store that opens with a damaged model record.

Nothing else on the screen changed. No new dependency.
