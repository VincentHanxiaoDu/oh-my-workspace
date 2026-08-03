# A hub process that holds published notes and answers questions about them

## Why

PRD §2.2 is a top-level architecture section — "The hub — one per company" — and until now nothing
in this repository was it. `internal/hub` held the hub's RULES (visibility, scopes, search,
statistics, references) as pure values over an in-memory store that vanished with the process that
built it. `cmd/` held one binary, the client. Issue #102 named that gap and split it; this change is
the server half of the split, and Issue #104 is the wire.

The owner's release ruling blocks on the hub, so this is the critical path — and the thing on the
critical path is not the transport. It is having somewhere for a company's knowledge to live that
survives the process restarting, and having that place answer honestly about what it can read.

§2.4 is the part worth getting exactly right. It is a promise a person is owed, not an
implementation note: the hub holds **everything** published to it, including notes narrowed to named
people, to a group, or to yourself, because it indexes them so they can be found. Restriction
controls which **colleagues** see a note; it is not a wall against whoever operates the server. A hub
built to imply otherwise would be worse than no hub.

## What Changes

- **A hub process.** `internal/hubd` and `cmd/omw-hub`: create a hub in a named directory, serve it,
  describe it, and print what it can read. The directory is always an argument. Nothing is conjured
  and nothing is defaulted.
- **A durable corpus.** An append-only, fsynced record of every publication, amendment,
  re-scoping, person, group and revocation, replayed at start-up. A note published by one process is
  readable by the next one, under **the same id** — so a note id a person holds keeps working, and
  two hubs given the same record answer identically.
- **Visibility settled server-side, before ranking.** Every question goes through the existing
  `hub.CanRead` and the existing `Corpus`; there is no second rule here.
- **Authentication where the token says what and the session says who.** Every operation takes token
  material. There is no argument by which a caller states who they are, so acting as somebody else
  is not expressible rather than checked-and-refused. An unidentified caller is refused explicitly,
  before it can reach a predicate that answers "undetermined" for the whole corpus.
- **Revocation that takes effect at the hub**, recorded durably, so ending a session is not the same
  as forgetting a credential.
- **A hub that stops when it cannot write.** A failed durable write halts the process, and every
  subsequent call — including every read — refuses. It does not keep serving a corpus that has
  silently stopped growing.
- **`could not determine` kept apart from `there is nothing`, at start-up and at every call.** A
  record that cannot be read stops the hub and exits on the third code; a refused search returns a
  refusal and never a zero.

## What this deliberately does NOT change

- **No transport.** Issue #104 is the client-to-hub wire and it is a sibling. Nothing here imports
  `net`, and a test asserts that on the source. A client with a hub configured still reports that the
  hub could not be reached, which is UNDETERMINED and not "there is nothing there" — and that
  remains the honest answer until #104 lands.
- **No client change.** `omw` is untouched.
- **No new authority model.** `internal/auth` and `internal/hub` already own scope, person and grant.
  This change calls them and adds no vocabulary.

## What was left open, and what was chosen

- **Sessions are held in the process, not on the disk.** A restart therefore ends every session. The
  hub then answers a DETERMINED "no such token", which a client renders as "not signed in" and never
  as undetermined — so it is a real answer rather than a silence, and re-signing-in is a deliberate
  act, which §3.10 wants anyway. Durable session material is a decision about where a hub keeps
  secrets and it is worth making on purpose rather than as a side effect of this change.
- **The scope vocabulary was not extended.** An operator's reach is a deployment fact and §4.5 says
  it is not expressed through scopes. There is no `admin` and no `operate` here.
