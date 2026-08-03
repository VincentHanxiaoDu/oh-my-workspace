# Connect Teams and email once, and have them ingested continuously into tickets

## Why

People reach one person in two places, and that person loses things in both. A Teams ping on
Tuesday and a mail on Thursday turn out on Friday to have been the same broken login. What they
asked for is not a second copy of their inbox — they have one of those and it does not help — but a
client that is plugged into the places work arrives and is keeping up for as long as it is running.

Four sentences decide the shape of this, and each names a way the obvious implementation goes wrong:

- **§3.1 — connect once, and it keeps up while the client runs.** The obvious build is an
  `omw ingest` command, and that is a synchroniser somebody has to remember. Ingestion has to be a
  property of the daemon RUNNING, which means it needs somewhere in the daemon to live; there was no
  such place, because the daemon's only periodic work was its own write probe.
- **§3.2 — a ticket is a thing you have to act on, and acknowledgements are not low-priority
  tickets.** The obvious build writes one ticket per message and, when told that `ok` and `Hii` are
  not tickets, files them at the bottom of the list. There is no bottom of the list. Eleven
  acknowledgements are nothing, not eleven small things.
- **§4.3 — undetermined is not "no", and neither is unreachable.** A channel whose credential was
  rejected and a channel that was reached and had nothing both produce zero tickets, and a build
  that renders them the same has told a person nobody wants anything from them when in fact nobody
  was listening.
- **§4.2 — nothing is implicit.** No command starts the daemon; no channel is connected and no
  credential obtained without the person doing it; and with nothing connected there is nothing to
  reach out to, so nothing reaches out.

## What Changes

- **A new `internal/channels` package** holding what a connected channel is, what ingestion does to
  the traffic on it, and the adapter seam through which a channel is reached.
- **Teams and email are built in as channel kinds** — enumerated by `Kinds()`, connectable with
  nothing installed, stored, listed, health-checked and ingested through the same code, and neither
  needing a hub. **What is not built in is a transport for either**, and the built-in adapter says
  so: it reaches nothing and reports `ErrUnreachable` naming precisely what is missing, which is
  criterion 10's unreachable rendering produced honestly rather than a stub that pretends to work.
  The transports belong to Issue #21, which owns the one extension mechanism these are instances of.
- **Ingestion produces tickets, not a mirror of the traffic.** Messages are grouped into the matters
  they are about — by the channel's own thread identifier where there is one, by a normalised
  subject otherwise — and each matter produces at most one ticket, with a WRITTEN title and summary
  and no verbatim subject line or message body. The message count and the ticket count are two
  separate numbers, reported per channel and recorded on the channel.
- **A matter whose every message is an acknowledgement produces no ticket at all.** Not at a low
  priority: there is no priority, and a structural test forbids the word appearing in this package's
  code, alongside a reflection test on the ticket type it produces.
- **A raw message is never stored.** `Message` exists for one ingestion pass and is then gone; the
  stored channel record has no field that could hold one, asserted on the record type's syntax tree.
- **Ingestion runs because the daemon runs.** A new `daemon.Background` registry — a name, an
  interval, and a function given the store path — is the narrowest place for work that is a property
  of the daemon running, and `internal/channels` registers into it from an `init`. There is no
  ingest command and no refresh command, and a structural test keeps one from appearing.
- **Ticket identifiers are stable for a channel and a matter**, so ingestion every second updates one
  ticket rather than accumulating one per pass.
- **A new `omw channels` command** with `connect`, `list`, `status` and `disconnect`. Connecting
  requires the person's own credential file: nothing here opens a browser, polls a token endpoint or
  reads a keychain. Every read says whether ingestion is running, and attaches that standing to the
  last-ingestion timestamp itself rather than to a banner three lines above it.
- **The exit codes carry the distinctions.** An empty channel set is an answer and exits zero; a
  channel list that could not be read exits `ExitUndetermined`; a missing store exits `ExitFailure`.
  `could not determine` and `determined to be nothing` never share a code.
