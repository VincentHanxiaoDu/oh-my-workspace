# status

## ADDED Requirements

### Requirement: The one screen reports the model provider
`omw status` SHALL report a model-provider subsystem on both of its surfaces — the screen a person
reads and `--json` — with its own named line, its own state and its own sentence. The state SHALL be
sourced from the same `model.View` that `omw model show` and `omw daemon status` render, and SHALL
NOT be a second derivation of the person's configuration.

A model state that could not be determined SHALL make `omw status` say so and exit 3, matching the
other two surfaces. It SHALL NOT be reported as a negative, and SHALL NOT be omitted from the screen.

#### Scenario: A provider is configured with a credential
- **WHEN** a person has chosen a provider and supplied a credential for it, and `omw status` is run
- **THEN** the screen and `--json` both name a model-provider subsystem, report it as working, carry
  the same sentence `omw model show` prints, and the invocation exits 0

#### Scenario: No provider is chosen
- **WHEN** nobody has chosen a model provider, and `omw status` is run
- **THEN** the model-provider subsystem is reported as not configured rather than as not working,
  its sentence differs from every other model rendering, and the invocation exits 0

#### Scenario: A provider is chosen and no credential has been supplied
- **WHEN** a person has chosen a provider and has supplied no credential for it, and `omw status` is
  run
- **THEN** the model-provider subsystem is reported as not configured rather than as not working, its
  sentence is neither the one for a fully configured model nor the one for no provider chosen, and
  the invocation exits 0

#### Scenario: A provider this build has no adapter for is chosen
- **WHEN** a person has chosen a provider that this build registers no adapter for
- **THEN** the model-provider subsystem does not report as not working, and the screen says the
  adapter is missing rather than saying the person's configuration is absent

#### Scenario: Whether a credential is supplied could not be determined
- **WHEN** a credential file is named, exists, and cannot be read, and `omw status` is run
- **THEN** the model-provider subsystem is reported as undetermined, the screen says so in words that
  are not the words for an absent credential, and the invocation exits 3

#### Scenario: Which provider is configured could not be determined
- **WHEN** a recorded model choice is present in the store and cannot be read, and no provider is
  named in the environment, and `omw status` is run
- **THEN** the model-provider subsystem is reported as undetermined, its sentence is not the one for
  "no provider is chosen", and the invocation exits 3

#### Scenario: The three surfaces are asked about one machine
- **WHEN** `omw status`, `omw model show` and `omw daemon status` are run against the same machine in
  the same environment, for each of: no provider; a provider with no credential; a provider with a
  credential; a credential that could not be read; and a recorded choice that could not be read
- **THEN** all three report the same model state in the same words for each configuration, and the
  renderings of the five configurations are pairwise distinct

#### Scenario: The credential never appears on the one screen
- **WHEN** a person's credential is held in a file or in the environment and `omw status` is run in
  any of its forms
- **THEN** the credential value appears in no part of the output — not on stdout, not on stderr and
  not in `--json` — while the model-provider line is present and says whether a credential is
  supplied

### Requirement: A capability the daemon report renders has a line on the one screen
A capability that `omw daemon status` and the control API report about SHALL also have its own line
on `omw status`. This SHALL be enforced structurally rather than by a list of subsystem names, so
that a capability added to the daemon's report and not to the screen fails by name rather than going
unnoticed until somebody reads the screen and finds it silent.

#### Scenario: A capability is added to the daemon's report and not to the screen
- **WHEN** the daemon's report carries a capability that renders itself, and `omw status` carries no
  subsystem reporting that capability
- **THEN** the suite fails, naming the capability and the surface it is missing from

#### Scenario: The guard has nothing to examine
- **WHEN** the daemon's report carries no self-rendering capability at all
- **THEN** the guard fails rather than passing, because a guard that examined nothing establishes
  nothing
