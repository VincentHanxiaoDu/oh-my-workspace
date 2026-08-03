# devices

## ADDED Requirements

### Requirement: A partial inventory is an undetermined answer, not a failure
Where a device listing is produced and is not the person's whole inventory, the command SHALL exit
with the undetermined code. It SHALL NOT exit with the failure code, because the listing was produced
and every device on it is real, and a caller treating a non-zero exit as failure would report a
healthy inventory as broken.

A listing that is the person's whole inventory, with every check-in state determined, SHALL exit
successfully.

The failure code SHALL remain reserved for the cases where the command could not do what was asked —
a refused registration, or an inventory that could not be read at all and for which no listing was
printed.

Where the listing is not whole, it SHALL still state that it is not whole and state precisely what is
missing, and the wording SHALL distinguish an incompleteness this machine established from one it
could not establish.

#### Scenario: No hub is configured
- **WHEN** a person lists their devices on a machine with no hub configured
- **THEN** the listing states that it is only part of the inventory, states what is missing, and the
  command exits with the undetermined code

#### Scenario: A hub answers with the whole inventory
- **WHEN** a person lists their devices on a machine whose hub answers
- **THEN** the listing claims completeness and the command exits successfully, and it differs from
  the partial listing in what it prints as well as in the code it exits with

#### Scenario: A registration is refused
- **WHEN** a person registers a device under a label that is already registered
- **THEN** the command exits with the failure code
