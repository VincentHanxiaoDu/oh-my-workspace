# Subscribe to my own work, with selectors that name a subject and a granularity

## Why

PRD §3.7: "Separately from the knowledge loop, the client reports on a person's own work. A
subscription is a standing instruction built from selectors, each naming a subject and a
granularity." The section then lists five examples and one sentence that constrains everything
else: the five granularities "mean the same thing for every subject, which is what makes `*:summary`
a sensible thing to ask for."

That sentence is the design, not a nicety. If `summary` meant one thing for `git` and another for
`token_usage`, then `*:summary` — a person asking for one paragraph per subject without knowing the
subject list — would be asking for a bag of unrelated things. So this change has exactly one
function that turns activity into a rendering, it takes a granularity and a list of items, and it
never sees a subject. A per-subject branch cannot be added to it without deleting it.

The failure Issue #23 actually fears is the other half:

> "I typo a subject, or I subscribe to something that isn't a subject in this product, and the
> report comes back empty — and an empty report looks *exactly* like a quiet day."

That is §4.3 arriving in a new capability, and it arrives twice over. A selector that cannot be
read is REFUSED, by name, at the point the subscription is written, and nothing is stored. A
well-formed selector naming a subject this build does not have is STORED and reported as unmatched,
by name, every time the report runs. A real subject with nothing to report is a determined,
successful, ordinary answer. Three facts, three outputs, three exit codes — and beside them two
more the same rule produces: a subject whose source could not be read is undetermined and never
`count: 0`, and a subject only a hub can supply, with no hub configured, says exactly that rather
than reporting as empty, unmatched, or vanishing from the report.

§4.2 and §4.4 constrain the surface: nothing here starts the daemon (every operation says whether it
is running), and nothing here can open a connection at all — asserted by reading the source of
everything the flow reaches, because with no hub configured the correct number of dials is zero and
the honest way to guarantee zero is to have no `net` on the path.

## What Changes

- **`internal/reports`, a new package.** Selectors, the five granularities, the subject catalogue,
  the report, and subscriptions on the local store.
- **`Granularity`** — `full`, `event`, `digest`, `summary`, `count`, with the ordering written once
  in `byDetail` and rendering deliberately NOT driven by it, so a swap in the ordering is caught by
  the containment property rather than moving the output along with it. The zero value is
  `GranularityUnspecified`, not `Full`: an unset granularity must not be a request for everything.
- **`ParseSelectors`** — the PRD's whole grammar: wildcards, dotted subject paths, negation, and
  comma-separated lists. All or nothing: one bad selector refuses the list and stores nothing.
- **Precedence stated once.** A subject is included when some positive selector matches it and no
  negation matches it. It is a set rule with no fold over the written list, so `*, !channel` and
  `!channel, *` cannot differ — the person is not required to know an evaluation order.
- **`Build` / `Report.Render`** — four per-subject states (reported, no activity, undetermined, no
  hub) plus the unmatched-selector line, all pairwise distinct in the bytes a person reads.
- **`omw report`** — `subscribe`, `show`, `list`, `run`, `subjects`. Exit codes: `ExitUsage` for a
  refusal, `ExitFailure` for an unmatched selector or a subject only a hub could answer,
  `ExitUndetermined` for a subject that could not be read, `Success` otherwise.
- **The subject catalogue is small on purpose:** `git`, `git.commit`, `token_usage`, `channel` from
  the PRD, plus `published_notes` as the one hub-supplied subject criterion 23 needs to exist before
  it can be reported on. Nothing else was invented; adding one is a line in the catalogue.

## What this change does NOT do

- **It does not ingest.** Activity is read from the local store under `activity.<subject>` via
  `reports.WriteActivity`; nothing in this change populates `git` from a repository or `token_usage`
  from a model provider. Those belong to the capabilities that own those sources. The mechanism, the
  granularities and every distinction above are complete and driven by tests either way.
- **It does not add a transport.** A hub-supplied subject with a hub configured reads the local
  store like any other and will report nothing until there is one. With no hub configured — the case
  criterion 23 is about — it says precisely that.
