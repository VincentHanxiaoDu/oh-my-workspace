# Tasks

## The status package

- [x] Write `internal/status/doc.go` stating why four states exist, why the zero value is
      undetermined, and why this package determines nothing another package already owns
- [x] Define `State` with `Undetermined` as the zero value, and `Word`, `String` and `Determined`
      as its only renderings
- [x] Make `State.Word` the single machine token both surfaces publish, and `stateFromWord` decode
      by asking it rather than by repeating the tokens
- [x] Name the six §2.1 subsystems as constants with a `Required()` list, so "none is silently
      omitted" is a property a test can iterate
- [x] Define `Item` so a channel, a project and a device each carry their own state and their own
      sentence inside a subsystem line
- [x] Give `Subsystem` an `ObservedAt` whose zero value means there is no observation time, and an
      `ObservedText` that says so rather than substituting one
- [x] Write `Summarise` as the only place the leading line is decided, checking for anything
      undetermined — subsystem or member — before anything else
- [x] Make `Screen.Render` data-driven over the subsystem slice, with no switch on names and no
      early return
- [x] Guard a blank detail so no line renders as silence
- [x] Write `Screen.ControlJSON` serialising the same value `Render` prints, and `UnmarshalControl`
      restoring the typed fields from the words and recomputing the summary
- [x] Write `ParseRendered` so a test can compare the two surfaces to each other rather than each to
      a literal

## Collecting the six lines

- [x] Take every outside input through one `Query` struct, with daemon liveness as an INPUT from the
      product's one answer and `Report.Running` documented as deliberately unread
- [x] Give `Collect` no early return: six lines on every path, an unanswerable one becoming an
      undetermined line
- [x] Report the daemon's liveness, how its last run ended, and what its control API did as three
      separate sentences on the daemon line
- [x] Report the store's existence and its location, with a store that is not there as NOT
      CONFIGURED and nothing creating one
- [x] Read channels from the store, so the line is fully answerable with no daemon
- [x] Mark an adapter the last attempt could not reach as undetermined, with its own wording
- [x] Take the projects listing from `projects.Take` with the one liveness answer, and stamp the
      line with the POLL's time — or with none at all when the poll recorded none
- [x] Mark a project directory that has gone missing with its own sentence, distinct from a plain
      failure
- [x] Take the device listing from `devices.Load`, and mark a machine that has never checked in with
      its own sentence
- [x] Make the hub line not-configured with no hub, undetermined when configured and unreachable,
      and working when it answers — with `Query.Dial` unreachable from the unconfigured branch
- [x] Write the member-to-subsystem precedence once, in `worse`, with undetermined outranking
      not-working

## The command

- [x] Register `omw status` in a NEW file in `internal/commands`, with `--json` and `--help`
- [x] Take liveness from `daemonLiveness` and the daemon's own report from `daemon.Inspect`, naming
      why the two readings cannot disagree
- [x] Exit 0 for a screen that was produced with every state established, 3 for anything
      undetermined, 1 only when no screen exists — and print no partial screen on that last path
- [x] Point at `omw health` and `omw diagnostics` without implementing either

## Driving the criteria

- [x] Compare the four state renderings pairwise, in both the human and the machine form
- [x] Drive criterion 8 over every position an undetermined subsystem can occupy, and over an
      undetermined member of a working subsystem
- [x] Drive criterion 7 at the renderer and again end-to-end at the CLI, asserting the other lines
      keep their own real states
- [x] Compare an undetermined line against a not-working line carrying IDENTICAL detail, so the
      distinction under test is the line's state and not its prose
- [x] Drive criteria 9–12 by obtaining BOTH surfaces from one real invocation and comparing them
      subsystem by subsystem, with a configured hub ensuring at least one is undetermined
- [x] Drive criterion 10 by feeding the renderer a control response containing a subsystem and a
      state word this build has never heard of
- [x] Drive criterion 13 against a real machine with no daemon: exit 0, a rendered daemon line, and
      no daemon afterwards
- [x] Drive criterion 17 against a REAL daemon whose control API declined through the injected
      confirmation seam, compared against a machine where none was started
- [x] Drive criterion 4 by hashing the whole sandbox tree before and after two runs
- [x] Drive criterion 16 against a path with no store, asserting none exists afterwards
- [x] Drive criterion 15 with a dial function that counts its own invocations
- [x] Set BOTH `XDG_DATA_HOME` and `HOME` in every sandbox, so the suite cannot reach the
      developer's real device pointer
- [x] Pin the member-to-subsystem precedence in both directions — a table over `worse` for the
      function, and a real two-member channels line for the fact that a subsystem calls it
- [x] Read a device's check-in through `CheckIn.State()`, PR #44's method, and drive the value it
      made representable — a check-in recorded with no instant — through the member fold
- [x] Mutate each of the defects the criteria are written about, confirm RED naming the defect,
      and revert
