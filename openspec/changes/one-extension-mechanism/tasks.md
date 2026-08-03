# Tasks

## The one mechanism

- [x] Add `internal/extension` as the shared half of §2.5's one mechanism: the act of registering,
      the one inventory, the four-state vocabulary, the wording, the exit-code answer
- [x] Give it four states — loaded, failed-to-load, not-registered, undetermined — with the zero
      value Undetermined, following `tri.Value` so a state nobody assigned cannot read as a negative
- [x] Render an entry through ONE function that takes no branch on the interface, so a failed
      channel adapter line and a failed model provider line cannot drift apart
- [x] Make registration a deliberate act recorded in the store, whose signature has no interface
      parameter — the interface comes from the extension, so there is nothing for a caller to vary
      between a channel and a model
- [x] Keep the shared half free of `internal/channels` and `internal/model`, so `internal/daemon`
      can import it without closing daemon → extension → channels → daemon

## Plugging in the two interfaces, without copying either

- [x] Add `channels.Extension`, offering the built-in Teams and email kinds into the one registry
      from an init, so §3.1's built-ins appear in the same listing as anything registered
- [x] Add `channels.ExtensionFactory`, so a channel whose adapter failed to load reaches `Ingest`
      as an error and is reported unreachable-with-a-reason rather than reached-and-empty
- [x] Add `model.Extension` and `model.Readiness`, so a capability that needs a model says which of
      "no model configured" and "the extension failed to load" it is in
- [x] Add one line to `model.Register` so a provider becomes known to both `omw model use` and
      `omw ext list` from a single registration, and one to `unregister` so removal empties both
- [x] Leave `internal/channels`' and `internal/model`'s own interfaces, registries and stored
      records exactly as Issues #6 and #18 built them

## The typed-refusal vocabulary

- [x] Move `Error`, `Code` and `Refusedf` out of `internal/hub` into `internal/refusal`, which
      imports nothing but `errors` and `fmt`
- [x] Alias them from `internal/hub` (`type Error = refusal.Error`), so nothing else in the tree
      changes shape and every existing `errors.As` keeps working
- [x] Point `internal/model` at `internal/refusal`, restoring Issue #6's transitive ban on
      `internal/channels` reaching the hub — which the #6 + #18 merge had broken

## The surfaces

- [x] Add `omw ext` (`internal/commands/extension_cmd.go`, a new file only) with `list`, `register`,
      `deregister`, `configure` and `show`
- [x] Report the daemon on every subcommand and start it on none; ask `daemonLiveness`, which is
      package `commands`' one answer, rather than adding a second
- [x] Exit 0 / 1 / 3 for every-registered-loaded / at-least-one-failed / at-least-one-undetermined,
      and say the reason in words on stderr so the terminal and `$?` agree
- [x] Evaluate a requested scope through `hub.EvaluateGrantRequest` rather than restating §4.5, and
      refuse the whole registration rather than issuing a narrower one
- [x] Carry the inventory on `daemon.Report` and render it with the same `extension.Render` the CLI
      calls, so the two surfaces cannot word one machine two ways
- [x] Wrap `outboxReviewer` so a provider whose extension failed to load is never resolved as an
      unconfigured model — a wrap because this branch may only ADD files to package commands

## Credentials

- [x] Give `Entry` no field a credential fits in, and assert the shape by reflection rather than
      grepping one instance
- [x] Refuse a credential-named setting at the point of RECORD, so nothing exists for a surface
      written later to leak
- [x] Allow location-named settings (`key_file`, `token-path`) — a first cut refused `key_file` by a
      message that told the person to record a path, and a test caught it

## Review round 1 — the summary named the wrong subsystem

- [x] Add the interface-neutral `extension.ErrFailedToLoad`, the twin `ErrLoadUndetermined` had been
      shipped without, so a summary over a mixed set has a correct code to reach for
- [x] Use it in `omw ext list`'s failure summary, which had been printing
      `model-provider-extension-failed-to-load` for a broken CHANNEL adapter — the Issue's own
      opening story reported as a model fault
- [x] Keep the per-entry codes interface-specific: `model.ErrProviderFailedToLoad`'s own reasoning
      about a caller with no English to inspect is right and is untouched
- [x] Drive it from both ends — channel-only, model-only and one of each — and assert the three
      summaries are the same sentence, so a build that swapped one interface's bias for the other's
      cannot pass

## Review round 2 — an incomplete read reported a determined answer

- [x] Add `store.IDs`, which enumerates record names WITHOUT decoding any of them, so no record's
      contents can fail the enumeration and a caller can degrade per record
- [x] Rebuild `Registered` on it: one damaged record is one `Undetermined` entry beside the intact
      ones, instead of `store.List` refusing the whole kind and erasing every registration
- [x] Add `extension.Listing` — the entries AND whether they are all of them, in one value — and
      `extension.Read` returning it, so the incompleteness travels inside the thing that gets
      rendered and there is no second return value for a surface to drop
- [x] Put the incompleteness in `Listing.Render` above the entries, carry it into `Listing.Summary`,
      and make `Summary.AllLoaded` unable to answer yes over a partial read
- [x] Point `omw ext list`, `omw ext show`, `omw ext register` and the control API's
      `extensionsFor` at `Read` rather than `Inventory`
- [x] Carry `extension.Listing` on `daemon.Report` in place of `[]extension.Entry`, so the
      undetermined warning crosses the wire with the entries (criterion 20)
- [x] Audit every path against "can this report a determined answer from an incomplete read?" and
      fix the one more it found: `omw ext show` answered "not registered" for a name absent from an
      inventory it had failed to ENUMERATE
- [x] Keep `omw ext show` DETERMINATE when only a record was damaged — the enumeration still
      established which names exist — with a control test so the hedge cannot spread
- [x] Drive criterion 22's bundle clause against a REAL bundle now that `internal/diagnostics` has
      landed on main, walking every file with `--include-bodies`

## Review round 3 — a channel adapter satisfied the model-provider check

- [x] Add `extension.FindAs`, which takes the interface as a parameter a caller cannot forget rather
      than leaving it a check a caller must remember
- [x] Report an extension of the right name and the WRONG interface as not registered — as the thing
      that was asked for, it is not — and SAY so, because "no such provider" sends somebody who has
      registered it hunting for a typo
- [x] Point `model.Readiness` at it, so `omw ext register slack` + `omw model use slack` no longer
      reports "the model provider slack is chosen and its extension loaded"
- [x] Point `channels.ExtensionFactory` at it too: the type assertion already caught the inverse,
      but only after resolving it to Loaded and with a sentence describing something else
- [x] Keep `Find`'s by-name behaviour for `omw ext show`, where describing whatever is registered
      under a typed name is right, and document which callers want which
- [x] Carry the entry's own detail into `Readiness`'s answer, so a person who registered a channel
      adapter is not told to run `omw ext register` on the thing they already registered

## Driven red on purpose

- [x] A failed-to-load adapter returning a working-looking empty adapter — red: "the broken channel
      is reported as REACHED with 0 message(s)"
- [x] A failed-to-load provider answering `SituationNoModelConfigured` — red: "broken and
      unconfigured say exactly the same thing"
- [x] `Entry.Render` branching on the interface — red: the criterion-3 diff prints both lines
- [x] The credential guard disabled — red, after the assertion was strengthened: the first version
      of that test passed under the mutation because it ran after a deregister and checked only a
      non-zero exit
- [x] An undetermined load collapsed into failed-to-load — red in four packages at once
- [x] Undetermined sharing an exit code with failed — red: "exit 1, want 3"
- [x] An unreadable registration skipped rather than listed — red: "the damaged registration was
      DROPPED from the listing"
- [x] The reviewer's exact defect reapplied — the summary hardcoding
      `model.ErrProviderFailedToLoad.Code` — red in all three cases: "the summary over a mixed set
      names one interface"
- [x] The same defect with the bias SWAPPED to `channels.ErrAdapterFailedToLoad.Code` — red in all
      three, so the repair is neutrality and not a change of side
- [x] `Registered` back on `store.List` — red with QA's exact symptom: "the failed-to-load extension
      is not registered … a broken extension reported as absent is this Issue's opening story", and
      the CLI's "the footer claims every registered extension loaded"
- [x] `Summary.AllLoaded` ignoring incompleteness — red: "the summary claims every registered
      extension loaded over an inventory it could not read"
- [x] The control API blanking the incompleteness again — red: "the control API does not carry
      \"may not be all of them\", which the CLI printed"
- [x] `omw ext show` answering not-registered from a failed enumeration — red: "exit 0 for a name
      looked up in an inventory that could not be enumerated"
- [x] `model.Readiness` back to `Find` (by name alone) — red with QA's own sentence: "a CHANNEL
      ADAPTER satisfied the model-provider check: the model provider slack is chosen, its extension
      loaded, and a credential is supplied"
- [x] `FindAs` matching any interface — red in both packages
- [x] `FindAs` never matching — red on the CONTROL arm: "a genuine model provider of the same name
      is 3, want SituationReady … the interface check has broken the case it was supposed to
      protect", so the repair is pinned in both directions rather than the lookup merely broken
