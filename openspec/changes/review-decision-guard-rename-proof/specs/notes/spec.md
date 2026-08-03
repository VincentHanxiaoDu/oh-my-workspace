# Drafting notes into the outbox

## ADDED Requirements

### Requirement: The check on where the model decision is made survives a rename of what it checks
The check that the review-mode model decision is stated in only one place SHALL identify the
refusals it guards by the machine-readable codes they carry rather than by the names they are given
in code, so that renaming a refusal does not disarm it. Where a guarded code no longer exists, the
build SHALL fail and say so rather than checking less than the rule requires.

#### Scenario: A refusal is renamed and a bypass remains
- **WHEN** a refusal carrying one of the guarded codes is renamed throughout the source, and a
  second place still decides for itself which of the two situations a machine is in
- **THEN** the check fails and names that place, as it would have before the rename

#### Scenario: A refusal is renamed and there is no bypass
- **WHEN** a refusal carrying one of the guarded codes is renamed throughout the source and the
  decision is still made in one place
- **THEN** the check passes, because a rename alone is not a violation

#### Scenario: A guarded code stops existing
- **WHEN** no value declares one of the codes the check is anchored to
- **THEN** the build fails and names the missing code, rather than passing while checking only the
  codes that remain

### Requirement: The check identifies a reference by what it refers to, not by how it is spelled
The check SHALL resolve references to the refusals through the import path of the package declaring
them, and SHALL examine every top-level declaration rather than only functions. A value belonging to
a different package SHALL NOT be reported merely because it shares a name.

#### Scenario: The declaring package is imported under another name
- **WHEN** a place states the rule for itself while importing the declaring package under an alias,
  or importing it so its names need no qualifier
- **THEN** the check fails and names that place

#### Scenario: The reference is held in a variable rather than used inside a function
- **WHEN** a place holds one of the refusals in a declaration outside any function
- **THEN** the check fails and names that declaration

#### Scenario: An unrelated value shares the name
- **WHEN** a place refers to a value of the same name belonging to a different package
- **THEN** the check passes, because that value is not the one the rule is about
