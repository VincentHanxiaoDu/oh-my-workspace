# Device registry and listing

## Why

PRD §3.8: "Every device is listed, including one never started — a device that has not checked in is
a fact worth seeing, not an absence."

That sentence is §4.3's rule pointed at devices, and it is the whole reason this change exists. A
person who set a machine up last month and never turned it on is the person most in need of seeing
it: if the entry quietly is not there, they read the silence as "fine", and it is not. So a machine
that was registered and never started is **not** missing data and **not** an error. It is a distinct,
renderable state, and it must be told apart from a machine that checked in long ago, from a machine
that is checked in now, from a machine whose state could not be worked out, and from a label nobody
ever registered.

Two more things hang off the same sentence:

- **§3.8 again.** Each machine is registered under a label unique to the person, and devices are
  separate and are shown as separate. A person who types the same label twice by accident must be
  told, not have one machine silently take over the other's registration.
- **§4.4, the local half stands alone.** A device list is per-person, and anything beyond this
  machine needs the hub. With no hub configured, a one-device list and a genuine one-device list must
  not read alike — a silently-truncated list that reads as complete is the failure mode this
  principle exists to prevent.

## What Changes

- **A new `internal/devices` package** holding the person's inventory of the machines carrying their
  store. It is the representation, and the representation is where §3.8 is enforced: a check-in is a
  `tri.Value` with an instant beside it, not a `*time.Time` — a nil pointer holds "never" and
  "unknown" in one field, which is exactly the collapse §4.3 forbids.
- **"Never checked in" is a value written to disk, not an absence inferred from one.** Registering a
  machine records `"state":"never"` there and then, so the never-started box is a fact in the
  inventory from the moment it is registered. A check-in field that is missing, unrecognised or
  carries an instant that will not parse reads back as **undetermined**, never as "never".
- **One rendering, so two states cannot be made to read alike.** `CheckIn.Describe` is the only
  rendering of a check-in in the product, and both the CLI and the control API's JSON form call it.
  The two surfaces agree by construction rather than by two implementations that happen to agree.
- **A registration is an explicit act, and the only one.** `Registry.Register` is the only function
  that adds a device; recording a check-in for a label nobody registered is refused rather than
  quietly registering it. Nothing here starts the daemon, creates a store, or opens a connection.
- **A duplicate label is refused, and so is a duplicate machine.** The second machine takes nothing:
  the file is left byte-identical and the label still resolves to the first machine. Registering one
  machine twice under two labels is refused too, because §3.8 registers a machine under *a* label.
- **A listing carries how much of itself it could establish.** `Snapshot.Complete` is a `tri.Value`
  with the precise reasons beside it. With **no hub configured** it is a determined `No` — the
  product knows the list is only this machine's half — and the reason is stated. With a hub
  configured that could not be reached it is `Undetermined`. Those two never share an exit code:
  1 for "known to be partial", 3 for "could not tell", 0 only for a listing that is whole and every
  state determined.
- **A new `omw devices` command** — `list`, `show`, `register`, `check-in`, with `--json` for the
  control API's form of the same answer. An empty listing for a person with no devices, an empty
  listing that could not be completed, and a one-device listing with no hub each render differently.
- **The daemon is probed, never started, and never named.** The command looks for the control socket
  the environment names; nothing there is "not running", a path it cannot examine is undetermined.
  Where owner-only access to that socket cannot be confirmed, §4.6's refusal is carried through to
  the listing, which says so instead of presenting itself as complete.

## What Issue #17 did NOT settle, and what this build does about it

Reported here rather than decided quietly:

- **The label's format.** The Issue fixes the uniqueness *scope* ("unique to the person") and says
  nothing about the format. So this build folds no case, normalises no Unicode, trims no interior
  whitespace and caps no length: `Laptop` and `laptop` are two machines. It refuses exactly three
  labels, and each refusal is a case where accepting would break something already settled — the
  blank label (nothing to be unique against, and a blank entry is the silence §4.3 forbids), a label
  containing a line break (the listing is one device per line, so such a label can forge an entry for
  a machine that does not exist), and a label containing a NUL.
- **Machine identity.** The Issue's own "Related" note settles it — "each device has exactly one
  store, so device identity and store identity are the same question asked twice" — so a
  registration records the device store's id and this build mints no second scheme. A machine with no
  store yet has no identity this build will invent: registering there is refused as **undetermined**,
  naming the two things a person can do about it.
- **Registering a machine that is not this one.** The journey names a box set up elsewhere, and with
  no hub there is no other way for its registration to reach this machine's inventory. `--machine
  <store id>` is the explicit way to enter one; it is this change's own decision and not the Issue's.
- **The hub transport.** There is none in this build. A configured hub is reported unreachable, which
  makes the listing undetermined — never a short list presented as whole.
