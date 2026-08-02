# health Specification

## Purpose

Everything this product promises about data staying on a person's machine rests on one assumption it
does not itself enforce: that the disk is already encrypted. The client does not encrypt its own
store (PRD §4.1) — anyone who can read an unencrypted disk can read unpublished tickets and drafts.

Health exists so that assumption is checkable rather than merely stated. It answers, for the machine
it is run on, whether full-disk encryption is on — and it answers in three values, not two, because
`could not be determined on this platform` and `not enabled` send a person to completely different
places. Collapsing them would be the product quietly guessing about the one thing its privacy story
depends on.

Two constraints follow, and both are load-bearing rather than incidental:

- **It is a report, never a blocker.** Encryption being off is the person's problem to fix, not a
  reason for the tool to refuse them. A determined `not enabled` terminates successfully; only a
  check that could not be *completed* terminates differently, so the two are distinguishable by
  termination alone.
- **It needs nothing set up.** No store, no running daemon, no control API, no hub. It is a question
  about the machine, not about the product's own state, and a person must be able to ask it before
  they have committed anything to this machine — which is precisely when the answer matters most.
## Requirements
### Requirement: Health answers full-disk encryption in exactly three values
Health SHALL report full-disk encryption as exactly one of `enabled`, `not enabled`, or `could not be determined on this platform`, and SHALL always emit one of the three — never a fourth value, never an empty value, and never no value at all.

#### Scenario: The disk is encrypted
- **WHEN** the platform probe reports that full-disk encryption is on
- **THEN** health reports the encryption assumption as `enabled`

#### Scenario: The disk is not encrypted
- **WHEN** the platform probe completes and reports that full-disk encryption is off
- **THEN** health reports the encryption assumption as `not enabled`

#### Scenario: The check could not be completed
- **WHEN** the platform probe is unavailable, returns nothing usable, or errors
- **THEN** health reports the encryption assumption as `could not be determined on this platform`

### Requirement: The three values are mutually distinguishable and never collapse
Health SHALL render each of the three values differently from the other two. `could not be determined on this platform` SHALL NOT be rendered as, nor collapsed into, `not enabled`, and `not enabled` SHALL NOT be rendered as, nor collapsed into, `could not be determined on this platform`.

#### Scenario: Three states produce three outputs
- **WHEN** health is run once for each of the three states and its output is captured each time
- **THEN** the three captured outputs are distinct from one another
- **AND** the undetermined output does not contain the words `not enabled`
- **AND** neither determined output contains the undetermined wording

### Requirement: An undetermined state is stated, never implied by silence
Health SHALL emit the undetermined value explicitly when the encryption state cannot be read. It SHALL NOT omit the encryption line, SHALL NOT emit an empty value, and SHALL NOT fall back to a default.

#### Scenario: The encryption line is present when nothing could be determined
- **WHEN** the encryption check cannot be completed
- **THEN** the output still contains an encryption line
- **AND** that line states `could not be determined on this platform`
- **AND** the output states why the state could not be read

### Requirement: An error in the check is never a negative
Where the encryption check itself fails or errors, health SHALL report `could not be determined on this platform` and SHALL NOT report `not enabled`.

#### Scenario: The platform tool is absent
- **WHEN** the platform's encryption tool is not installed on the machine
- **THEN** health reports `could not be determined on this platform`

#### Scenario: The platform tool returns output that cannot be read
- **WHEN** the platform's encryption tool returns empty or unrecognised output
- **THEN** health reports `could not be determined on this platform`, not `not enabled`

### Requirement: Health is a report and never a blocker
Health SHALL complete and report its findings whichever of the three values it determines. A determined `not enabled` SHALL terminate successfully, and a check that could not be completed SHALL terminate with an exit code distinct from both success and the generic failure code.

#### Scenario: Not enabled succeeds
- **WHEN** health determines that full-disk encryption is not enabled
- **THEN** the command writes the report and exits 0

#### Scenario: Undetermined is distinguishable by termination alone
- **WHEN** health cannot complete the encryption check
- **THEN** the command writes the report and exits 3
- **AND** that exit code differs from the exit code of a determined `not enabled` run
- **AND** that exit code differs from the generic failure exit code

### Requirement: Health checks the real platform on macOS and on Linux
Health SHALL determine FileVault state on macOS and LUKS state on Linux, and `could not be determined on this platform` SHALL NOT be the standing answer on either. Where this slice ships no probe for a platform, health SHALL report the undetermined value rather than a negative.

#### Scenario: macOS reads FileVault
- **WHEN** health runs on macOS and `fdesetup status` reports FileVault is On
- **THEN** health reports `enabled`
- **AND WHEN** it reports FileVault is Off
- **THEN** health reports `not enabled`

#### Scenario: Linux reads LUKS
- **WHEN** health runs on Linux and a `crypto_LUKS` filesystem is present in the block tree
- **THEN** health reports `enabled`
- **AND WHEN** the block tree is readable and contains no LUKS device
- **THEN** health reports `not enabled`

#### Scenario: A platform with no probe
- **WHEN** health runs on a platform this slice ships no encryption probe for, such as Windows
- **THEN** health reports `could not be determined on this platform`

### Requirement: Health needs no store, no daemon and no control API
Health SHALL run to completion and report the encryption value on a machine with no store, with no daemon running, and where the control API has not opened. It SHALL NOT create a store, SHALL NOT start the daemon, and SHALL NOT start any process other than the platform encryption probe.

#### Scenario: A machine with nothing set up
- **WHEN** health runs where no store has been created and no daemon is running
- **THEN** it reports the encryption value
- **AND** no store exists afterwards
- **AND** no daemon is running afterwards

#### Scenario: The only process health starts is the probe
- **WHEN** health runs with the encryption probe supplied in-process
- **THEN** it starts no process at all
- **AND WHEN** it runs with the real platform probe
- **THEN** it starts exactly the encryption query and no second process

### Requirement: Health works fully with no hub configured and opens no network connection
With no hub configured, health SHALL report the encryption value in full, SHALL NOT degrade or half-report, and SHALL make no outbound network connection. Any part of the report unavailable for lack of a hub SHALL be named rather than omitted.

#### Scenario: No hub configured
- **WHEN** health runs with no hub configured
- **THEN** it reports the encryption value in full
- **AND** it states that no hub is configured and that it made no outbound connection

#### Scenario: No network capability
- **WHEN** the source of health and its command is inspected
- **THEN** it imports no network-capable package

### Requirement: The encryption answer is presented as a reported deployment assumption
Health SHALL present the encryption answer as one of the deployment assumptions the product rests on, identifiable as such, so a reader can tell which assumptions were checked and which principle each comes from.

#### Scenario: A reader can tell what was checked
- **WHEN** health writes its report
- **THEN** the report identifies itself as the reported deployment assumptions
- **AND** the encryption line names the PRD section the assumption comes from

