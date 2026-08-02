# Local store creation

## Why

The product has nowhere to put a ticket or a draft. PRD §2.1 and §3.14 say there is one store per
device, created by an explicit act, and that it is the sole home of unpublished data — but nothing
in the build creates one, and five other capabilities (the daemon, channels, tickets, the inbox and
the status line) cannot begin until it exists.

Three promises hang on getting it right rather than merely getting it built:

- **§4.1, the disk is the boundary.** A store inside Dropbox, iCloud Drive, OneDrive or a roaming
  profile means every promise about unpublished work staying on the machine is false, quietly. The
  store has to argue with the person before it is created, not after.
- **§3.14, it survives an interrupted write.** Losing the last thing typed is annoying; opening the
  store afterwards and finding it truncated ends the person's trust in the product.
- **§4.3, undetermined is a real answer.** Whether a location synchronises can genuinely fail to be
  determined, and that outcome must render as neither a yes nor a no.

## What Changes

- **A new `internal/store` package** — the foundation the other five Issues consume. It opens and
  creates stores, resolves the one-per-device path, and reads and writes records. A record is a
  kind, an id and an opaque payload: the package does not know what a ticket is, so tickets, draft
  notes, channel cursors and project state are all records owned by their own capabilities.
- **Creation is explicit and singular.** `Create` is the only function that brings a store into
  being; `Open` never creates, never repairs, never initialises on first use. A second creation is
  refused with the existing store left byte-identical.
- **A synchronising location is refused, by probing rather than by naming a platform.** The probe
  walks the target's ancestry looking for evidence a sync client left on disk — `.dropbox`, iCloud's
  `Mobile Documents` / `.icloud` placeholders, OneDrive's GUID file, a roaming profile's
  `ntuser.dat` — and consults the mount table for network filesystems where the system publishes
  one. The same code runs on macOS and on Linux.
- **Distinct, `errors.Is`-able failures.** Store missing, store already exists, path synchronising,
  sync undetermined, path missing, permission denied, store unreadable, record missing, invalid
  name. "This is Dropbox", "this path does not exist" and "I lack permission to write here" are
  three values and three sentences, not one shared failure.
- **Crash safety as a property of every write.** Payload streamed into a temporary file in the
  destination's own directory, fsynced, renamed over the destination, and the directory fsynced.
  Readers ignore anything that is not a completed record, so an interrupted write is invisible
  rather than half-readable. Each record carries a checksum, so damage beneath the product is
  reported as unreadable and never as an absence.
- **A new `omw store` command** with `create`, `path` and `status`, whose exit codes carry the
  distinctions: success, failure, and `ExitUndetermined` for what could not be determined.

### Not changed, and deliberately so

**Issue #3's blocked decision is left open.** When the probe cannot determine whether a location
synchronises, the PRD fixes neither proceeding nor halting nor an explicit override, and this change
picks none of the three. Creation stops on `ExitUndetermined` with a message saying the state could
not be determined, that the product has no ruling on whether to proceed, and naming Issue #3's open
decision. That is a refusal to decide made visible, not a decision.
