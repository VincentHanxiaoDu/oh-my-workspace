# Publish a draft to the hub, and always know which state it is in

## Why

PRD §3.11 is one sentence and it is an invariant, not a behaviour:

> A note that did not arrive is still in the outbox — never both, never neither.

An invariant has to hold across an interruption. Publication is a transfer over a link that can go
down, and the two halves of the product end up on opposite sides of it: the hub can have acted while
the client never learned so, and from the client's side that world is indistinguishable from one
where nothing arrived. Everything in this change follows from taking that seriously.

Three failures are available, and each is worse than a visible error:

- **Two copies.** A person's wifi drops, they press publish again, and the company hub now holds the
  same note twice with no way to tell which is which. This is the failure PRD §3.11 names directly:
  "a person retries and does not get two copies."
- **A lost note.** A publish that fails and takes the draft with it. §3.11's "never neither", and
  §3.14's "the sole home of unpublished data" — there is nowhere else it could have gone.
- **A refusal and an unreachable hub reported as the same thing.** They are different facts and a
  person does different things about them: one is fixed by asking for a different grant or changing
  the note, the other by getting on a network. §3.11 says so, §4.3 says why, and this project's
  standing rule — `could not determine` and `determined to be nothing` never share a rendering and
  never share an exit code — makes it a repository-wide requirement rather than a preference.

Issue #9 built the outbox, the revisions and the local `review` gate, and stopped exactly at the
transfer, reporting it as undetermined and saying it belonged here. This change is the transfer.

## What Changes

- **A publication ledger inside the store**, one small durable record per note, written with one
  atomic rename and one `fsync`. It is the SINGLE authority on which of PRD §2.3's two containers
  holds a note, which is what makes "never both, never neither" a property of one write rather than
  a race between two.
- **Four states, and they are a closed set**: `drafted`, `in flight`, `published`, `refused`. They
  are compared pairwise in the tests, never each against a literal.
- **An attempt key, minted before the request leaves.** The client mints one idempotency key per
  publication, records it durably BEFORE dialling, and resends it unchanged on every retry. The hub
  records key → note on success and answers a repeat with the note it already made. That ordering —
  record, then send — is the whole of "interrupted means not published".
- **A transport, and it is a unix domain socket.** `$OMW_HUB` names a socket path. PRD does not say
  what the client talks to the hub over; this repository does, through the AST guard that requires
  every listen and dial under `internal/` to name `"unix"` as a literal (§4.2, §4.6).
- **`omw publish`**, a new command in a new file: `note` to send one draft, `state` to ask where one
  note is, `list` for every note this client knows about. It reports the daemon's state and starts
  nothing (§4.2).
- **A local control endpoint** answering the same [Report] the CLI renders, so §4.3's "the control
  API and the CLI report the same state" is a property of there being one computation.
- **Unguessable note ids**, adopted from Issue #15's branch byte-for-byte. Ids are minted at
  publication, which is this Issue, and the owner's ruling binds #10, #12, #14 and #15 together.
- **A refusal is not remembered by the hub.** The person fixes the reason and retries with the same
  key; remembering the refusal would make the fix take effect only after they invented a new attempt.

## What is deliberately NOT changed

- **Issue #9's `omw outbox` is untouched.** Its `publish` subcommand still reports the transfer as
  undetermined, which remains honest; `omw publish` is the transfer. Whether the two state screens
  should later become one is not settled here.
- **`internal/hub`'s store, visibility and version surfaces** are untouched except for the id hunk
  taken verbatim from #15. The idempotency ledger is a new file and a new type.
- **No fourth scope.** The vocabulary is ruled at three; publishing requires `publish`, and this
  client does not filter its own requests — the hub decides, so a refusal is one the hub made.
