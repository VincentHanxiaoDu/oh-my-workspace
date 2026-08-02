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
