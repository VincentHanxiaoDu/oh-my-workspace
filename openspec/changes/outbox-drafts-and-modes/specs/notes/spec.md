# Drafting notes into the outbox

## ADDED Requirements

### Requirement: A draft lives in the local store and nowhere else
Creating a draft SHALL write it into this device's local store, and SHALL refuse — naming the
missing store and exiting non-zero — when no store has been created.

#### Scenario: Drafting with a store
- **WHEN** a person creates a draft on a device that has a store
- **THEN** the draft is written inside that store, is listed in the outbox, and is reported as a
  draft

#### Scenario: Drafting with no store
- **WHEN** a person creates a draft on a device where no store has ever been created
- **THEN** the command exits non-zero, names the missing store, and the draft's text exists nowhere
  on the machine — not in a temporary directory and not in the person's home directory

#### Scenario: A draft outlives the process that wrote it
- **WHEN** the process that wrote a draft has exited and another process reads the outbox
- **THEN** the draft and its state are still there

### Requirement: An empty outbox and an unreadable one are different answers
Listing the outbox SHALL distinguish "there are no drafts" from "the outbox could not be read", in
both its output and its exit code.

#### Scenario: The outbox is empty
- **WHEN** a person lists an outbox holding no drafts
- **THEN** the command reports zero drafts as a determined answer and exits zero

#### Scenario: The outbox cannot be read
- **WHEN** a person lists an outbox that cannot be read
- **THEN** the command says nothing has been established about their drafts, does not report zero
  drafts, and exits with a code that success does not use

### Requirement: The publication mode is the person's choice and is always a real value
The client SHALL report an effective publication mode of `manual` when none has ever been set, SHALL
accept exactly `manual`, `review` and `auto`, and SHALL report undetermined when a recorded choice
cannot be read.

#### Scenario: No mode has ever been set
- **WHEN** a person asks which publication mode is in effect on a device where none was ever set
- **THEN** the client answers `manual`, states it as a real value rather than as blank or absent,
  and says that this is the default rather than a choice they made

#### Scenario: A mode outside the vocabulary
- **WHEN** a person sets a mode name that is not one of the three
- **THEN** the command exits non-zero, and reading the mode back shows the previously effective mode
  unchanged

#### Scenario: The recorded choice cannot be read
- **WHEN** the record of the person's chosen mode exists and cannot be read
- **THEN** the client reports undetermined, in wording distinct from both a mode it could report and
  from the default, and exits with the undetermined code

#### Scenario: Changing the mode does not act on drafts already written
- **WHEN** a person switches from `manual` to `auto`
- **THEN** the drafts already resting in the outbox are still there, in the state they were in

### Requirement: Review checks the person's own words, on their own machine
`review` mode SHALL check a draft against rules recorded in the person's own wording, read back
byte for byte, using the person's own model, and SHALL perform that check without a hub.

#### Scenario: Rules are read back as they were written
- **WHEN** a person records rules containing leading spaces, blank lines, tabs, mixed case and
  trailing whitespace, and reads them back
- **THEN** the text read back is byte-for-byte the text recorded

#### Scenario: Review with no hub configured
- **WHEN** a person with a configured model reviews a draft on a device with no hub configured
- **THEN** the check runs and reports its verdict

### Requirement: Review with no model says what is missing and publishes nothing
Attempting to draft or publish under `review` with no model configured SHALL name the missing model
configuration, SHALL exit non-zero, SHALL leave the draft in the outbox, and SHALL report the draft
in a state distinguishable from a draft its author has simply not published.

#### Scenario: The person picks review and has no model
- **WHEN** a person under `review` writes a draft or attempts to publish one, with no model
  configured
- **THEN** the output names the missing model, the command exits non-zero, the draft is still in the
  outbox, and neither the command's output nor the draft's reported state matches what `manual`
  produces for the same draft

#### Scenario: Nothing is published while the check cannot run
- **WHEN** a person under `review` with no model attempts to publish, on a device with a hub
  configured
- **THEN** no connection is opened, the draft is not published, and its state does not say it has
  been handed onward

### Requirement: A review that could not be completed is not a pass
Where the configured model is unreachable, errors, or returns an answer that is not a verdict, the
draft SHALL NOT be published and the outcome SHALL be reported as undetermined, distinguishable in
output and in exit code from both a pass and a refusal.

#### Scenario: The model cannot be reached
- **WHEN** a review is attempted and the model errors or cannot be reached
- **THEN** the outcome is undetermined, the draft stays in the outbox, and the exit code is the
  undetermined one, which neither a pass nor a refusal uses

#### Scenario: The model answers something that is not a verdict
- **WHEN** the model returns nothing, whitespace, or prose with no conclusion
- **THEN** the outcome is undetermined rather than a pass

#### Scenario: A refused draft
- **WHEN** a review completes and refuses a draft
- **THEN** the draft is still in the outbox, is reported as refused by review with the model's
  reason, and reads differently from a draft that has never been reviewed

### Requirement: The person's key is never printed
No command in this capability SHALL write the person's model key to any output stream.

#### Scenario: A configured key across every command
- **WHEN** a model is configured with a key and every subcommand of this capability is run, under a
  passing, a refusing and an unreachable model
- **THEN** the key's value appears on neither stdout nor stderr of any of them

### Requirement: Nothing implicit, and no network without a hub
No command in this capability SHALL start the daemon, and with no hub configured none SHALL open a
network connection; with a hub configured, purely local work SHALL still open none.

#### Scenario: The daemon is not running
- **WHEN** any command in this capability is run with the daemon stopped
- **THEN** it says the daemon is not running, and the daemon is still not running afterwards

#### Scenario: Local work beside a configured hub
- **WHEN** drafting in `manual` mode, listing the outbox, setting the mode and reading it back are
  run on a device whose configured hub address is a live listener
- **THEN** that listener receives no connection

#### Scenario: The local half with no hub at all
- **WHEN** a person with no hub configured drafts, lists, and reads their drafts in `manual` mode
- **THEN** every command succeeds, and none of them warns about a missing hub

### Requirement: Where a hub or an owner-only socket is required, the command says so
Where the transfer of a draft genuinely requires a hub, or where the daemon reports that its
control API is not open on an owner-only socket — or that this could not be confirmed — the command
SHALL say precisely what is missing and exit non-zero rather than half-working.

#### Scenario: Auto mode with no hub
- **WHEN** a person under `auto` writes a draft on a device with no hub configured
- **THEN** the command names the missing hub, exits non-zero, and the draft rests in the outbox

#### Scenario: A control API that is not open, or cannot be confirmed
- **WHEN** any command in this capability is run while a daemon is running and reports that its
  control API is not open, or that whether it is open could not be established
- **THEN** the command says which of the two it is and exits non-zero rather than proceeding, and
  the two do not share an exit code

### Requirement: Nothing in the outbox expires
No draft SHALL be removed from the outbox by age.

#### Scenario: An old draft
- **WHEN** a draft written years ago and never touched is listed, repeatedly
- **THEN** it is still listed every time
