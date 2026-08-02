# Tasks

## The outbox, inside the store

- [x] `drafts.InStore` — one outbox at a fixed name inside the store, creating no store and nothing outside one
- [x] `drafts.ErrNoStore` — drafting with no store is a refusal that names the store, never a temporary location
- [x] Consume Issue #11's `Outbox`, `Revise`, `Drafts` and `Timeline` unchanged; add no second outbox

## Publication mode

- [x] `Mode` with exactly three values, `ParseMode` that accepts exactly those names, and `DefaultMode`
- [x] `ReadMode` / `WriteMode` against the store, with the record separate from the rules record
- [x] `ModeSetting` — effective mode, whether the person chose it, and an undetermined third answer
- [x] `ModeSetting.Render` — three pairwise-distinct renderings, none of them blank

## Draft state

- [x] `State` values covering resting, blocked, review-incomplete, refused, cleared and handed onward
- [x] `SetState` / `StateOf`, with a state file that is invisible to the revision timeline
- [x] A damaged state record reads as undetermined, never as `drafted`
- [x] `StateReport.Render` — distinct renderings for present, absent and undetermined

## Rules, in the person's own words

- [x] `WriteRules` / `ReadRules` storing one string, byte for byte
- [x] Read-back path that puts the person's bytes on stdout and every word of the client's on stderr
- [x] A test whose rule text would be mangled by a naive normaliser

## Review

- [x] `Reviewer` returning the model's raw answer, so the text-to-verdict seam is testable in one place
- [x] `Interpret` — anything that is not a verdict is undetermined, never a pass
- [x] `Check` — an error is never read for a verdict, even when the answer says "pass"
- [x] `Outcome.Render` and `Outcome.StateFor` — three outcomes, three states, no branch that looks untouched
- [x] `ModelConfig` with three answers, an unexported key, and a `String` method so `%v` cannot leak it

## The command

- [x] `omw outbox` with draft, list, state, mode, mode set, rules, rules set, model, review, publish
- [x] Preflight on every subcommand: platform, the control socket's owner-only permissions, the daemon's state
- [x] The review gate in one function, so there is exactly one path by which a draft may leave
- [x] The gate runs before the hub is considered, so review never depends on a hub
- [x] The boundary with Issue #10 stated in the output: the gate passed, and nothing has left this machine

## Tests

- [x] Criterion-by-criterion tests at the command level, driving the registry the way `main` does
- [x] Three-way renderings compared pairwise rather than against string literals
- [x] The key swept for across stdout and stderr of every subcommand, with a control
- [x] A spawn test proving a draft outlives the process, with `XDG_DATA_HOME` and `HOME` sandboxed
- [x] A structural test that nothing this capability is built from imports a network package
- [x] Environment-dependent checks probe (unreadable file, real unix socket, loopback listener) and skip loudly
