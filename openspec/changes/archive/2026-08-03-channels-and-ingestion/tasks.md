# Tasks

## What a connected channel is

- [x] Write `internal/channels/doc.go` stating that ingestion produces tickets rather than a mirror
      of the traffic, that there is nowhere for an acknowledgement to go, and that unreached is not
      empty
- [x] Define `Kind` with `KindTeams` and `KindEmail`, enumerated by `Kinds()`, rendering
      distinguishably from each other
- [x] Define `Health` with four states — connected and healthy, credential expired, disconnected,
      undetermined — with the zero value undetermined, following `tri`
- [x] Define `Ingestion` with a three-valued last-success fact and a separate last-attempt
      `Outcome`, so a channel that succeeded yesterday and failed a minute ago reports both
- [x] Render the last-success fact three ways — a timestamp, "never ingested", undetermined — and
      compare them pairwise rather than against literals
- [x] Render the last-success fact differently again when ingestion is not running, so a stale time
      is never presented as current
- [x] Define the distinct, `errors.Is`-able failures: no such channel, already connected, unknown
      kind, invalid connection, no credential, unreadable record, unreachable
- [x] Store, read, list and disconnect channels through `internal/store`, failing on a damaged
      record rather than skipping it
- [x] Refuse a connect with no credential, and refuse to silently replace an existing channel

## Reaching a channel

- [x] Define `Message`, `Adapter` and `Factory` as the seam, with the factory a parameter of
      `Ingest` rather than a package-level variable
- [x] Ship a built-in adapter for both kinds that is honest that this build has no transport, and
      document it where a reviewer will read it before believing otherwise

## Turning traffic into tickets

- [x] Group messages into matters by thread identifier, else normalised subject, else sender
- [x] Decide "is there anything to act on" on the message body, reusing `inbox.IsAcknowledgement`,
      and document the subject-only email the rule gives up
- [x] Produce at most one ticket per matter, with a written title and summary that copy no subject
      line and no body
- [x] Make ticket identifiers stable for a channel and a matter, so repeated passes update one ticket
- [x] Report message count and ticket count separately, per channel and in total, and record both on
      the channel
- [x] Leave the last-success time alone when an attempt fails, and record the failure as unreachable
- [x] Skip a channel whose credential has expired without reaching out, reporting it as unreachable
      and naming the expiry
- [x] Attempt every channel even after an earlier one failed

## Ingestion as a property of the daemon running

- [x] Add `daemon.Background`, `daemon.RegisterBackground` and `daemon.Backgrounds`
- [x] Start registered work in `Serve`, first pass immediately, stopped and waited for before
      `Serve` returns
- [x] Register ingestion from `internal/channels`'s `init`, with no ingest command anywhere

## The command

- [x] `omw channels connect <kind> --account --credential-file [--id]`, refusing without a credential
- [x] `omw channels list`, stating an empty channel set as one and distinguishing it from an
      unreadable one and from a failure to list
- [x] `omw channels status <id>`, including a channel that is not connected
- [x] `omw channels disconnect <id>`, refusing an identifier that is not connected
- [x] Say whether ingestion is running before every set of ingestion facts, and inline on the
      timestamp itself
- [x] Exit codes: empty is success, undetermined is `ExitUndetermined`, failure is `ExitFailure`,
      detail on stderr

## Driving it

- [x] Criterion 1 — both kinds connect through the CLI and the listing tells them apart, compared
      pairwise
- [x] Criterion 2 — empty, unreadable and failed-to-list are three outcomes with three exit codes
- [x] Criterion 3 — the three last-ingestion renderings, compared pairwise, and a recorded time that
      will not parse reported as undetermined rather than as "never"
- [x] Criterion 4 — a real daemon, a message delivered after it started, and a ticket with nothing
      typed
- [x] Criterion 5 — traffic delivered with no daemon changes nothing; and stopping a running daemon
      stops ingestion
- [x] Criterion 6 — with the daemon stopped, the timestamp is reported AND marked not current
- [x] Criterion 7 — every subcommand leaves the daemon stopped, plus a structural check that this
      Issue's command file can spawn nothing and ingest nothing
- [x] Criterion 8 — six messages about one matter make one ticket; the two counts differ; no subject
      line or body appears verbatim
- [x] Criterion 9 — eleven acknowledgements produce zero tickets, plus the structural and reflection
      checks that there is no priority to put one at
- [x] Criterion 10 — reached-and-empty and could-not-be-reached render differently, compared against
      each other
- [x] Criterion 11 — with nothing connected, zero adapters are CONSTRUCTED; plus the import-graph
      and no-listen-or-dial checks
- [x] Criterion 12 — connecting and listing work fully with no hub configured, and mention no hub
- [x] Criterion 13 — a connect without a credential is refused and connects nothing; the three
      connection states are three sentences, compared pairwise
- [x] Criterion 14 — no rendering contains the credential; no stored record can hold a message
- [x] Criterion 15 — success and failure never share an exit code, with detail on stderr
- [x] The real binary end to end: create, connect both kinds, start the daemon, and see this build's
      real adapters reported as unreachable rather than as empty
- [x] Mutation-drive each of: acknowledgements becoming tickets; one ticket per message; unreachable
      rendering as empty; a stale time presented as current; a channel command ingesting; the daemon
      not running its background work; a copied subject line; a failed attempt recording a success
