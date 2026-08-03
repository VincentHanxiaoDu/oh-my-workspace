# Tasks

## The ticket type — the contract #6 and #7 consume

- [x] Write `internal/inbox/doc.go` stating what a ticket is, what the type deliberately does not
      have, and what Issues #6 and #7 may add to it
- [x] Define `Field` with four states — written, written and empty, never recorded, could not be
      determined — with the zero value undetermined, following `tri`
- [x] Render all four distinguishably, with the undetermined wording taken from `tri` rather than
      chosen here
- [x] Encode the four states as distinct bytes on disk, so a written empty value and an absence do
      not become one value in storage
- [x] Define `Ticket` with an identifier, a title, a summary, a channel and an arrival time — and
      with no priority, rank, severity, score, order or raw message body
- [x] Define `Kind` so #6 and #7 reference the store kind rather than the string
- [x] Define the distinct, `errors.Is`-able failures: no such ticket, not an obligation, invalid
      ticket, unreadable ticket
- [x] Refuse an acknowledgement at `Put` with `ErrNotAnObligation`, and document the part of that
      decision the Issue did not settle

## Reading, and deleting

- [x] `List` returning every ticket ordered by identifier, filtering nothing and consulting no clock
- [x] `List` failing on a damaged ticket rather than skipping it
- [x] `Get` distinguishing a ticket that is not there from one that cannot be read
- [x] `Delete` refusing an identifier that is not in the inbox, over the store's idempotent delete
- [x] `Operations` as the closed, enumerable set of what can be done to a ticket

## Saying what was and was not read

- [x] Probe the daemon and the control API from the control socket's own file, opening nothing and
      starting nothing, answering both in three values
- [x] Refuse to call the control API open when owner-only permissions cannot be confirmed, and say
      which of the reasons applies

## The command

- [x] `omw inbox list`, rendering every ticket with its title and its summary
- [x] `omw inbox read <id>`, rendering title, summary and that it is an inbox ticket
- [x] `omw inbox delete <id>`, the only way a ticket leaves the inbox
- [x] Build the command's help from `Operations` so the two cannot drift
- [x] An empty inbox stated explicitly and succeeding; an unreadable one on `ExitUndetermined`; no
      store at all on `ExitFailure`
- [x] Every ticket field rendered through `Field.Render`, never with a `fmt` verb
- [x] A header stating the daemon's state, the control API's state, and that no hub was contacted

## Tests, including the ones that assert an absence

- [x] Four renderings compared pairwise, never against string literals
- [x] A driver seeding the traffic about one broken login and asserting no listed title is a message
      body, and that two obligations are two tickets
- [x] Reflection over `Ticket` and over every exported identifier asserting no priority exists
- [x] `Operations` enumerated, with none publishing, sharing or sending, and no exported identifier
      named for one
- [x] A real, reachable hub configured under four plausible names, with zero requests observed
      across list, read and delete
- [x] Tickets backdated a century, every operation exercised, and the ticket set asserted identical
- [x] Delete of an unknown identifier refused, by exit code, with the listing unchanged
- [x] The control API's refusal staged on a real unix socket, in a directory FOUND by binding one
      rather than named by platform
- [x] Every mutation in the pull request's table applied, run, confirmed red by name, and reverted

## Review round 1 — PR #29, changes-requested by product and QA

- [x] Take #27's fix for the spawned binary rewriting the developer's real device pointer, by
      cherry-pick rather than by writing our own, so the two branches do not diverge on one file
- [x] Re-drive that fix's own mutations here: the half-fix that sandboxes one variable, the
      quote-less match that lets the half-fix pass, and the control where the walk matches no spawn
- [x] Take the follow-up that narrows the spawn check to `_test.go` files, so it cannot flag
      production code that must inherit the real environment, and re-drive the mutations after it
- [x] Drive the narrowing itself: a production file that spawns with `os.Environ()` is not flagged
      with the filter, and IS flagged with the filter removed
- [x] Pin the written-empty rendering of a `Field` specifically, not merely that it differs from the
      other three — the mutant product found surviving
- [x] Fix `TestArrivedRendersTheUnknownTimeAsUndeterminedAndNotAsTheEpoch`, which asserted on 1970
      when Go's zero time is year 0001, so it could not fail for the reason it was named for
- [x] Make every declared acknowledgement reachable — `"+1"` was declared and never matched — and
      add a test over the whole list so a dead entry fails on the commit that adds it

## Review round 2 — `main` moved: Issue #41's one liveness answer

- [x] Merge `origin/main`, which now carries Issue #41 (PR #43): one answer to whether the daemon is
      running, and one place that derives a control-socket path
- [x] Delete `internal/inbox/presence.go` and the socket knowledge in it, rather than updating it —
      the path is `internal/daemon`'s, and a second copy is wrong on the runtime-directory fallback
- [x] Ask `daemonLiveness` for the daemon's state, and render all three of its values
- [x] Print the "read from the store on disk" sentence only where an absence has been ESTABLISHED,
      never where liveness could not be determined
- [x] Stop answering the control API's state from the inbox and point at the surface that owns it,
      recording on the pull request that this is a criterion of #8 reassigned by #41
- [x] Drive the merged tree — `main` + this branch — rather than only this branch, since CI tests
      each head against its own base and cannot see this class of break
