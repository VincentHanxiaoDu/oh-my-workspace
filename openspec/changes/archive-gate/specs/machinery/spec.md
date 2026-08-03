# machinery

## ADDED Requirements

### Requirement: A change whose content has landed in the generated specification SHALL NOT remain in flight
The gate that guards generated specifications SHALL refuse a pull request that regenerates
`openspec/specs/<x>/spec.md` while leaving in place an unarchived `openspec/changes/<slug>/` whose
delta for `<x>` has already landed in that specification. The refusal SHALL name the change
directory and SHALL name the `openspec archive <slug>` command that resolves it.

A change SHALL be judged to have landed when every `### Requirement:` heading in its delta
`openspec/changes/<slug>/specs/<x>/spec.md` is present in `openspec/specs/<x>/spec.md`. That is the
post-condition of archiving, and no other signal SHALL be substituted for it — in particular the
completeness of a change's task list SHALL NOT be read as evidence that its work has landed.

Only a complete match SHALL be a determination. Where some or none of a delta's requirements are
present, the gate SHALL NOT block and SHALL NOT report that the change's work has not landed; it
SHALL report the count it measured and state that whether the change has shipped is undetermined
from this repository. Not blocking and answering `no` are different acts, and the gate SHALL make
them distinguishable, so that a pass is also distinguishable from an absence of checking.

The closing summary SHALL NOT restate as settled anything the gate reported as undetermined.

The judgement SHALL be confined to the capabilities the pull request regenerates. A pull request
that regenerates no capability specification SHALL pass and SHALL say why it had nothing to judge.

A delta declaring no requirement SHALL be reported as undetermined rather than judged, because every
one of no requirements is vacuously present and would otherwise be read as having landed.

A base commit that cannot be resolved SHALL fail and SHALL NOT render as a completed archive check.

#### Scenario: The specification is regenerated and the change directory is left standing
- **WHEN** a pull request regenerates a capability specification and an unarchived change directory
  whose every requirement for that capability is now present in it remains in `openspec/changes/`
- **THEN** the gate refuses, naming that change directory and the `openspec archive` command that
  resolves it

#### Scenario: The change is archived correctly
- **WHEN** a pull request regenerates a capability specification and moves the change directory that
  produced it under `openspec/changes/archive/`
- **THEN** the gate passes, unchanged

#### Scenario: Work whose delta is absent from the specification is not accused
- **WHEN** a pull request regenerates a capability specification and an unarchived change directory
  declares a delta for that capability none of whose requirements are present in it
- **THEN** the gate passes, names the change and the count it measured, and reports whether that
  change has shipped as undetermined rather than as a finding that its work has not landed

#### Scenario: Only some of a change's requirements are present
- **WHEN** an unarchived change directory declares a delta for a regenerated capability some but not
  all of whose requirements are present in it
- **THEN** the gate passes and reports the same undetermined answer, because a partial match
  establishes neither that the change has shipped nor that it is still in flight

#### Scenario: A pull request that regenerates nothing
- **WHEN** a pull request changes no `openspec/specs/<x>/spec.md`
- **THEN** the gate passes and states that no archive is owed by it, rather than judging change
  directories it did not touch

#### Scenario: A delta that declares no requirement
- **WHEN** an unarchived change's delta for a regenerated capability declares no `### Requirement:`
- **THEN** the gate reports that whether its content has landed is undetermined, and does not refuse

#### Scenario: The base commit cannot be resolved
- **WHEN** the base commit a pull request is diffed against is not present in the checkout
- **THEN** the gate fails, says the range could not be computed, and does not report that the
  archive question was answered

### Requirement: The archive alarm SHALL survive a framework refresh
The assertion that the archive gate fires SHALL live in project-owned `internal/machinery/` and
SHALL execute the installed gate rather than restating its rule, so that a refresh of the
framework-owned `.workflow/bin/` which removes the gate turns this repository's own suite red.

#### Scenario: A refresh removes the gate
- **WHEN** the installed gate no longer performs the archive check
- **THEN** the project's own test suite fails, naming the assertions that no longer hold

#### Scenario: The installed gate carries its own proof
- **WHEN** the installed gate's `--self-test` is run
- **THEN** it exercises both the refusal and the in-flight case, so the patch offered upstream
  arrives with its evidence attached
