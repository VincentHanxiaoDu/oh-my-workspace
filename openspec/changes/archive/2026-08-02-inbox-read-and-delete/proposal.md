# Read the inbox as the list of things you have to act on

## Why

A person is contacted all day across Teams, email and a follow-up ping, and by the afternoon does
not know what is still owed by them. They do not want to re-read the traffic; they want the list of
obligations. The store can hold records and nothing yet says what a ticket is, so there is nothing
to read.

Three sentences in the PRD decide the shape of this, and each one is a way the obvious
implementation goes wrong:

- **§3.2 — a ticket is a thing you have to act on, it is not a message.** The obvious inbox is a row
  per message, and the person ends up reading past `yes`, `ok` and `Hii` to find the one broken
  login all of it was about. The sentence continues: "Acknowledgements and small talk are not
  low-priority tickets. They are not tickets." A priority field is exactly how that promise breaks —
  somebody has an acknowledgement, needs somewhere to put it, and puts it at the bottom.
- **§2.3 — tickets live on the machine and are never published.** The inbox needs no route to the
  hub, so it must have none, under any name.
- **§4.3 — undetermined is not "no".** A summary that has not been written, a field that was never
  recorded, and a field written as the empty string are three different facts about a ticket, and a
  `string` cannot hold the difference between the last two.

## What Changes

- **A new `internal/inbox` package** defining what a ticket is, for this Issue and for #6
  (ingestion) and #7 (merging), which consume it and cannot ask.
- **`Ticket` has no priority, rank, severity, score, order or position, and no raw message body.**
  That absence is the product decision, so it is asserted rather than merely documented: a test
  reflects over the type and over every exported identifier in the package and fails if one appears.
  A test asserting "a low-priority ticket was created" does not compile.
- **`Field`, a ticket's text in the four states text can be in** — written with a value, written and
  empty, never recorded, and could not be determined. All four render distinguishably, all four are
  distinct bytes on disk, and the zero value is undetermined, following `tri`.
- **`Put` refuses an acknowledgement** with `ErrNotAnObligation` rather than storing it at a low
  priority. There is no low priority. This is enforcement the Issue did not ask for by name and
  could have been left to #6 — see the note on `IsAcknowledgement`, and the pull request body.
- **`Operations()` is the closed, enumerable set of what can be done to a ticket** — list, read,
  delete. It exists as a value so criterion 6's driver can enumerate it; the CLI's own help is built
  from it, so the two cannot drift.
- **Nothing consults the clock.** No filtering by age anywhere, asserted both by backdating tickets
  a century and by a scan establishing that no code in the package calls `time.Now`.
- **`Delete` refuses an identifier that is not in the inbox**, though the store's own `Delete` is
  idempotent by design. A person who mistypes must not be told the ticket they meant is gone.
- **A new `omw inbox` command** with `list`, `read` and `delete`, whose exit codes carry the
  distinctions: an empty inbox succeeds, an inbox that could not be read exits `ExitUndetermined`,
  and a store that is not there exits `ExitFailure`. `could not determine` and `determined to be
  nothing` never share a code.
- **The listing states what it read and what it did not**: that the daemon is not running and that
  this is the store on disk rather than a live inbox (§4.2), whether the control API is open and why
  it is not (§4.6, §5.1), and that the hub was not contacted and there is no operation that would
  (§2.3).
- **A daemon and control-API probe living in `internal/inbox` as an acknowledged placeholder.**
  Issue #2 owns the daemon and has not landed; the two facts criteria 13 and 15 require are
  answerable from the control socket's own file, so they are answered from it and the file says so.
