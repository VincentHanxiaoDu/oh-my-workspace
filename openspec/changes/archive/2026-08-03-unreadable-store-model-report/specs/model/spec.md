# model

## ADDED Requirements

### Requirement: A store that could not be opened is not a caller with no store
Every surface that resolves a model configuration from a store SHALL distinguish three outcomes of
opening that store: it opened, there is genuinely no store there, and it could not be read. A store
that could not be read SHALL report which provider is configured and whether a credential is supplied
as undetermined, and SHALL say why.

`No store exists here` and `the store could not be read` SHALL NOT share a value and SHALL NOT share
a rendering. A surface SHALL NOT resolve an unreadable store as a caller that has no store, because
that resolution is a determined negative and renders as `no provider is chosen`.

The reason such a surface gives SHALL be the same wording the command line gives for the same store
at the same moment, produced by the same renderer, so that the two cannot drift.

#### Scenario: The control interface reports a store it could not read
- **WHEN** a report is produced for a store that exists and cannot be opened
- **THEN** the model state in that report is undetermined, names the store and the reason it could
  not be read, and states that an unreadable store is not one with no model recorded in it

#### Scenario: The two answers are compared byte for byte
- **WHEN** a report is produced for a readable store with no model configured, and a report is
  produced for a store that could not be read
- **THEN** the rendered model state of the two reports differs

#### Scenario: There is genuinely no store
- **WHEN** a report is produced for a path where the filesystem establishes that no store exists
- **THEN** the environment alone is the configuration, and the answer is determined rather than
  undetermined

#### Scenario: The command line and the report are asked about the same unreadable store
- **WHEN** the command line and the control interface are asked about the model configuration of the
  same unreadable store
- **THEN** both report it as undetermined, both give the same reason, and neither reports that no
  provider is chosen
