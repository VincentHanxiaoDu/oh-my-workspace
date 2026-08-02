# Tasks

## The devices package

- [x] Write `internal/devices/doc.go` stating why a check-in is a three-valued answer with an
      instant beside it, and not a `*time.Time` that conflates "never" with "unknown"
- [x] Define `Label`, `Machine`, `CheckIn` and `Device`, with `NeverCheckedIn`, `CheckedInAt` and
      `UndeterminedCheckIn` as the only ways to build a check-in state
- [x] Make `CheckIn.Describe` the single rendering of a check-in, with no branch returning a blank
      and no determined branch carrying the undetermined wording
- [x] Define the distinct, `errors.Is`-able failures: duplicate label, machine already registered,
      no such device, label refused, inventory unreadable, machine undetermined
- [x] Write `CheckLabel` refusing exactly the three labels that would break something already
      settled, and inventing no format beyond them
- [x] Persist the inventory beside the store package's device pointer, in the product's own per-user
      directory and not in the store
- [x] Write `"never"` explicitly on registration, so a never-started machine is a recorded fact and
      not an absence read back as one
- [x] Decode a missing, unrecognised or unparseable check-in as undetermined, never as "never"
- [x] Keep a device with an unreadable check-in IN the listing, and fail only when the inventory as
      a whole cannot be read
- [x] Refuse a duplicate label and a machine already registered, writing nothing in either case
- [x] Refuse to rewrite an inventory that could not be read, so a registration cannot delete devices
- [x] Make the inventory write atomic — temporary in the same directory, fsync, rename, fsync the
      directory

## The snapshot, and what the product does not know

- [x] Give `Snapshot` a `Complete` `tri.Value` with the precise reasons beside it
- [x] Take every outside input through one `Query` struct
- [x] Report no hub configured as a DETERMINED incompleteness, and an unreachable hub as undetermined
- [x] Take the hub as a `Dial` parameter rather than a package-level variable, so no connection can
      happen without a caller deciding to allow one
- [x] Merge the hub's devices by label only, never folding two labels together
- [x] Take the daemon's state as a three-valued INPUT (`Query.Daemon`) rather than deriving it —
      this package names, derives and stats no control socket (Issue #41)
- [x] Carry an UNDETERMINED liveness into the listing's completeness, and leave a determined
      "not running" alone: the inventory is a file that needs no daemon
- [x] Render an empty-and-complete listing, an empty-because-partial one and an empty-because-
      undetermined one differently
- [x] Serve the control API's JSON form from the same snapshot and the same `Describe`

## The command

- [x] Add `internal/commands/devices_cmd.go` as a new file referencing nothing else in the package
- [x] Implement `list`, `show`, `register`, `check-in` and `--json`
- [x] Take the machine identity from the device store's id, refusing as undetermined where there is
      no store rather than inventing one; allow `--machine` for a machine that is not this one
- [x] Give the three endings three exit codes: 0 whole and determined, 1 known to be partial or a
      refused registration, 3 anything that could not be determined
- [x] Refuse an unknown flag rather than treating it as a label
- [x] Ask the product's ONE liveness answer through `daemonLiveness`, and pin in a test that it is
      the real one and answers a determined negative in a sandbox

## Driving it, and breaking it

- [x] Compare the three check-in renderings PAIRWISE, not against literals, in the package and
      through the real command
- [x] Assert additionally that a determined answer does not wear the undetermined wording — added
      after a driven mutation kept all four strings distinct while telling a reader the product could
      not work out something it knows for certain
- [x] Drive criterion 3 by taking a listing before and after a first check-in and asserting exactly
      one line differs
- [x] Drive criterion 7 by comparing the inventory file byte-for-byte across a refused registration
- [x] Drive criteria 10 and 11 on the real binary: own process group, no leftover process, output
      byte-identical with every proxy variable poisoned
- [x] Sandbox BOTH `XDG_DATA_HOME` and `HOME` in every process spawn, and confirm the structural
      guard on `main` fails when one is removed
- [x] Probe the environment rather than naming it: `pgrep` presence, whether the filesystem honours
      permission bits, whether a unix socket can be created at all
- [x] Mutate and watch each test go red: a never-started device omitted; never-started rendered as
      undetermined; a no-hub listing presented as complete; a check-in registering implicitly; a
      duplicate label accepted; an unreadable check-in read as "never"; the two non-zero exit codes
      collapsed; the control form dropping a device; an unregistered label answered as a device;
      an unestablishable daemon ignored; the command guessing instead of asking `daemonLiveness`;
      the deleted socket constant reinstated
