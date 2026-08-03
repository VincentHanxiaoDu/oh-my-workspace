# Devices

## ADDED Requirements

### Requirement: Every device is listed, including one never started
The product SHALL list every machine registered under the person's name, SHALL carry each machine's
own label on its own entry, and SHALL include a machine that has been registered and has never
checked in. A never-checked-in entry SHALL carry a check-in state and SHALL NOT be omitted, blank or
collapsed into any other entry.

#### Scenario: One registered machine
- **WHEN** a person who has registered exactly one machine lists their devices
- **THEN** that machine appears, under the label it was registered with

#### Scenario: Two machines under two labels
- **WHEN** two machines have been registered under two different labels and the person lists devices
- **THEN** two separate entries appear, each carrying its own label, and neither is merged,
  deduplicated or made to stand for the other

#### Scenario: A machine registered and never started
- **WHEN** a device is registered and has never checked in, and the person lists their devices
- **THEN** an entry for that device is present and its check-in state says the machine is registered
  and has not reported in, rather than being absent or blank

#### Scenario: The listing before and after that machine's first check-in
- **WHEN** a listing is taken before a device has ever checked in and another after it checks in
- **THEN** both listings contain an entry for that device, and the two listings differ only in that
  device's check-in state

### Requirement: Never-checked-in is neither a no, nor an unknown, nor a silence
The product SHALL render a device's check-in state as one of exactly three answers — checked in at a
named instant, registered and never checked in, or could not be determined — and no two of the three
SHALL render alike for the same device. None SHALL render as an empty or absent field, and a
determined answer SHALL NOT carry the wording reserved for the undetermined one.

#### Scenario: Three states, compared with each other
- **WHEN** the same device label is rendered while never checked in, while last checked in far in the
  past, and while checked in now
- **THEN** the three renderings are pairwise distinct, with no fixed wording assumed by the comparison

#### Scenario: A check-in state that cannot be worked out
- **WHEN** a registered device's recorded check-in cannot be read at all
- **THEN** the entry remains in the listing and renders a third thing, distinct from never-checked-in
  and from any real check-in value, and distinct from silence

#### Scenario: A determined answer is not dressed as an undetermined one
- **WHEN** a device is known to have never checked in, or is known to have checked in
- **THEN** its rendering does not carry the product's wording for "could not be determined"

### Requirement: A label that was never registered is not a device
The product SHALL answer differently for a label that was registered and never checked in and for a
label that was never registered, and SHALL NOT report the second as a device that exists.

#### Scenario: Asking about a registered but never-started label
- **WHEN** a person asks about a label that is registered and has never checked in
- **THEN** the product answers with that device and its check-in state, and exits zero

#### Scenario: Asking about a label nobody registered
- **WHEN** a person asks about a label that was never registered
- **THEN** the product says nothing is registered under it, does not present it as a device, and
  exits with a code different from the registered case

### Requirement: A label is unique to the person and a duplicate is refused
The product SHALL refuse to register a second machine under a label already registered to that
person. After the refusal the number of registered devices SHALL be unchanged, the first machine's
registration SHALL still resolve to the first machine, and the second machine SHALL NOT inherit or
reuse it.

#### Scenario: Registering under a taken label
- **WHEN** a second machine is registered under a label already registered to that person
- **THEN** the command exits non-zero, the inventory is unchanged, and the label still resolves to
  the first machine

#### Scenario: Telling a refusal from a success without reading prose
- **WHEN** a successful registration and a refused duplicate registration are compared
- **THEN** they are distinguishable by exit code alone

#### Scenario: A label whose format the product will not decide
- **WHEN** a label is blank, is only whitespace, or contains a line break or a NUL
- **THEN** registration is refused and says why, and nothing is added to the inventory

### Requirement: Nothing is registered or started implicitly
The product SHALL register a device only when a person runs the registration command, and SHALL NOT
start the daemon on their behalf.

#### Scenario: Recording a check-in for an unregistered label
- **WHEN** a check-in is recorded for a label nobody registered
- **THEN** it is refused, nothing is registered, and the person is pointed at the registration command

#### Scenario: Listing devices with the daemon not running
- **WHEN** a person lists their devices while no daemon is running
- **THEN** the daemon is not running before the command and is not running after it, the command
  does not hang, and it either produces the listing or says the daemon is not running

#### Scenario: Listing and registering with no hub configured
- **WHEN** devices are listed and a device is registered with no hub configured
- **THEN** no network connection is opened, and both commands behave exactly as they do with
  outbound networking available

### Requirement: A listing states how much of itself it could establish
With no hub configured the product SHALL either list the person's devices fully or state precisely
what is missing and exit non-zero. It SHALL NOT return a partial list presented as complete, and
SHALL NOT return an empty list where a missing hub, rather than an absence of devices, is the reason.
A listing that is known to be partial and a listing whose completeness could not be determined SHALL
NOT share an exit code.

#### Scenario: A one-device listing with no hub
- **WHEN** a person with one registered machine lists devices with no hub configured
- **THEN** the listing states that machines registered on their other devices are not in it, exits
  non-zero, and does not render identically to a genuinely complete one-device listing

#### Scenario: An empty listing that is real, and one that failed
- **WHEN** a person who has registered no devices lists them against a hub that answered, and a
  person lists them with no hub configured or with a hub that could not be reached
- **THEN** the three renderings are distinct, and the genuinely empty one is the only one that exits
  zero

#### Scenario: A hub that is configured and cannot be reached
- **WHEN** a hub is configured and cannot be reached
- **THEN** the listing reports that whether it is complete could not be determined, and exits on the
  undetermined code rather than the code used for a listing known to be partial

### Requirement: The control API and the CLI report the same devices
For the same person and the same moment the product SHALL report the same set of device labels and
the same check-in state per device on the control API's form and on the CLI.

#### Scenario: The two surfaces compared
- **WHEN** the same listing is rendered for a person and for the control API
- **THEN** both carry the same labels, neither invents or omits a device, and each device's check-in
  state is the same on both

#### Scenario: Owner-only access to the control socket cannot be confirmed
- **WHEN** the control API's socket cannot be confirmed owner-only
- **THEN** the CLI says so and the listing is not presented as complete
