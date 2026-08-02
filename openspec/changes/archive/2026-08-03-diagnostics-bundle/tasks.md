# Tasks

## The bundle and its manifest

- [x] Add `internal/diagnostics` with `Produce(Options) (Result, error)`, writing a bundle directory
      holding `manifest.json` and the payload files that manifest names
- [x] Define the three category states — `collected`, `withheld`, `undetermined` — and the
      machine-readable reason codes that tell "withheld by default" from "could not be determined"
      from "not applicable on this machine"
- [x] Make the category list fixed and exhaustive, seeded as undetermined before any gathering, and
      refuse a manifest that still carries a seeded entry, so a category cannot go silently missing
- [x] Produce each manifest entry and the file it names from the same call, so the two cannot be
      written independently
- [x] Refuse a manifest whose collected category names no file, whose non-collected category names a
      file, or that carries a category name the manifest does not know

## Withholding by default

- [x] Read no record payload in the default gather: inventories carry ids and sizes only
- [x] Render ticket, draft-note and ingested-message bodies as `withheld` /
      `withheld-by-default` unless bodies were explicitly asked for
- [x] Wire the opt-in to exactly three body kinds, and to nothing else
- [x] Render the model key as `withheld` / `never-collected-credential` in every bundle, with no
      branch on the opt-in — PRD §3.13
- [x] Carry environment configuration as variable NAMES only, never values

## Three-valued reporting, taken from the packages that own each answer

- [x] Take daemon liveness from `internal/commands`' single `daemonLiveness`, injected into the
      diagnostics package, rather than writing a second probe or naming a control socket path
- [x] Take the rest of the daemon's state from `daemon.Inspect`, which starts nothing
- [x] Give the control API's owner-only confirmation its own manifest entry, separate from
      full-disk encryption
- [x] Take full-disk encryption from `internal/health` and record it in its three values
- [x] Record the store's location state from `internal/store`, reporting `undetermined` as itself
      and not resolving it to a yes or a no
- [x] Give a machine with no store, a machine with no hub, an unreadable subsystem and a capability
      absent from this build four distinct reason codes, none of them a negative finding

## The command

- [x] Add `internal/commands/diagnostics_cmd.go` registering `omw diagnostics <path>` — a new file,
      changing nothing already there
- [x] Give it one spelled-out `--include-bodies` flag, defaulting to off, with no short form
- [x] Refuse every other flag-shaped argument rather than guessing at a broader request
- [x] Refuse a relative destination, so a bundle never appears in whatever directory the person
      happened to be standing in — found by the suite's sweep over every registered command, which
      the first version of this command answered by writing three real bundles into the source tree
- [x] Print the summary from the manifest the bundle carries, not from a second accounting
- [x] Say out loud that nothing has been sent, and which of the two disclosure levels was used
- [x] Exit non-zero on a failure to produce a bundle, and zero when a complete bundle exists —
      including when subsystems inside it are undetermined

## All-or-nothing placement

- [x] Assemble the bundle in a staging directory beside the destination and rename it into place as
      the last act
- [x] Refuse a destination that already exists, leaving what is there byte-identical
- [x] Remove the staging directory on every failure path

## Driving it

- [x] Seed a store with recognisable strings in a ticket body, a draft body, an ingested message
      body and a model key, and search every file of the default bundle for all four
- [x] Point the same search at the store itself first and require it to find all four there, so a
      clean bundle is never a broken search
- [x] Fail the walk when it finds no files at all, so no negative assertion can pass vacuously
- [x] Assert the opt-in bundle carries the three bodies and still does not carry the key
- [x] Assert manifest/contents agreement in both directions, for both disclosure levels
- [x] Assert the manifest is readable with every other file in the bundle deleted
- [x] Compare the three daemon renderings and the three encryption renderings pairwise, not against
      string literals
- [x] Drive an unreadable subsystem with a real unreadable directory and assert it renders as
      undetermined with a reason, distinguishable from an empty collection
- [x] Drive a machine with no store and assert unavailable-because-no-store is distinguishable from
      present-but-empty
- [x] Assert no daemon is running before or after a bundle run, and that the pid did not change
- [x] Drive a failure AFTER the staging directory is full and assert nothing is left at or beside
      the destination
- [x] Assert a relative destination is refused, names why, and creates nothing in the working
      directory, including under the sweep's own argument shapes
- [x] Add a structural test that `internal/diagnostics` imports no transport and no process-spawning
      package
