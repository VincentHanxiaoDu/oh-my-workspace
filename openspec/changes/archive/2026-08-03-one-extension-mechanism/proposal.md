# Add a channel or choose a model through one extension mechanism

## Why

Two people at the same company say two sentences on the same afternoon. "We run on Slack, not
Teams." "Our security people will not let us send anything to a model we don't have a contract
with — we have our own endpoint."

Today they would be handed two guides. PRD §2.5 says no: *"Channel adapters and model providers are
the same mechanism with two interfaces. A company adding a channel and a company choosing a model do
the same kind of thing, and should not learn two systems."*

So it is one journey. They register the same way, list what is registered the same way, configure it
the same way, and — the part they are really buying — when an extension is broken they are told it
is broken in the same words.

**The failure they fear is the silent one.** They install the Slack adapter, nothing appears in
their tickets, and the product tells them there is no Slack traffic. Or their model extension fails
to load and `review` behaves as if they simply never chose a model — which §3.13 explicitly says is
*not* a broken client, a sentence that becomes a lie the moment a failed load is dressed up as an
unconfigured one.

**An extension that failed to load is reported as failed to load — never as absent, never as
present-but-idle.** That is §4.3 applied to extensions: a distinct answer, and none of them is
silence.

## What already existed, and how it was unified rather than replaced

Two implementations of §2.5's "one mechanism" were already in the tree, each right about its own
half:

- **`internal/channels` (Issue #6)** — the channel adapter interface, Teams and email built in, plus
  connection state and last-ingestion facts.
- **`internal/model` (Issue #18)** — the provider interface, its registry, and a `View` that reports
  *whether* a credential is present and never *which*. Its own source says why it stayed small:
  *"the wrong thing to do here is to build the general mechanism … because then #21 arrives to find
  a second extension system already load-bearing and its job becomes a migration instead of a
  design."* That judgement held.

Neither was copied and neither was rewritten. What was added is the mechanism they are both
instances of:

- `internal/extension` owns the **shared half**: the act of registering, the one inventory, the four
  states, the wording, the exit code. It imports neither `internal/channels` nor `internal/model`.
- Each interface **plugs itself in** from its own package — `channels.Extension` and
  `model.Extension` — exactly as `channels/loop.go` already registers its background work with the
  daemon. That is not the mechanism splitting in two: it is a six-line adapter living next to the
  interface it adapts, and it is where it is because `internal/channels` imports `internal/daemon`,
  and `internal/daemon` must import `internal/extension` to serve extension state over the control
  API. An `internal/extension` that imported `internal/channels` would close
  daemon → extension → channels → daemon.
- **A provider is registered once.** `model.Register` gained one line offering the provider to the
  extension registry. Asking each provider's init to register twice is two registrations of one
  thing that can disagree — a provider choosable by `omw model use` and invisible to `omw ext list` —
  which is the "two systems" §2.5 forbids, rebuilt inside the fix for it.

## The typed-refusal vocabulary had to move, and that is the interesting part

Merging Issue #6 and Issue #18 into one tree turns a green pair of branches red, and it is right to.

Issue #6 asserts structurally that `internal/channels` cannot reach `internal/hub`, transitively —
"a connected channel never reaches the hub as part of ingesting". Issue #18's `internal/model` needed
typed refusals, and the only typed-refusal type in the tree was `hub.Error`, so it imported
`internal/hub` for a struct with two string fields. `internal/channels` imports `internal/daemon`;
#18 made `internal/daemon` import `internal/model`. channels → daemon → model → hub.

The repair a person reaches for first is to relax #6's ban to a direct-import check, which keeps the
merge green and throws the guarantee away. The repair that holds is for the shared vocabulary not to
be the hub's property, because it never was one — `drafts`, `model` and `commands` all use it and
none of them is about a hub. So `Error`, `Code` and `Refusedf` moved to `internal/refusal`, and
`internal/hub` **aliases** them (`type Error = refusal.Error`). An alias and not a defined type:
every `&hub.Error{…}` and every `errors.As` against a `*hub.Error` in the tree keeps working
unchanged, and #6's transitive ban is untouched.

## What changes for a person

```
omw ext register slack        # a channel adapter
omw ext register acme         # a model provider
```

One command, one argument, one order — the interface is not an argument, it comes from the
extension. `omw ext list` is one inventory: both interfaces, Teams and email sorted in among
everything registered through the extension point, every registered extension present whatever its
state. Exit 0 means every registered extension loaded, 1 means at least one failed, 3 means at least
one could not be determined, and those three are never the same number.

`omw` takes no custody of credentials. A setting whose name says it is a secret is refused at the
point of record rather than redacted at the point of display, so there is nothing for a surface
written later — #20's diagnostics bundle, #16's agent API — to leak. A setting whose name is a
*location* (`key_file`, `token-path`) is recorded, because that is precisely what §3.13 asks people
to do.
