# reports

## ADDED Requirements

### Requirement: A subject nothing produces is undetermined, not a quiet period
A subject the client advertises SHALL report as undetermined when nothing in the build writes its
activity and no activity is stored for it. It SHALL NOT report as a period with no activity, and the
command SHALL NOT exit as though it had answered.

An emptiness is a determined answer only when something would have written to the subject had there
been anything to write. Where there is no such producer, the emptiness is a fact about the client and
not about the period, and reporting it as a quiet period is a confident negative derived from never
having looked.

Activity that is present SHALL take precedence over what the catalogue believes about a producer, so
that a report is about what is there.

#### Scenario: A report is run on a subject nothing writes
- **WHEN** a report is run for a subject whose activity nothing in this build writes, on a machine
  where no activity is stored for it
- **THEN** the report says the subject could not be determined, does not use the wording it uses for
  a period with no activity, and the command exits with the undetermined code

#### Scenario: The same subject once activity exists
- **WHEN** a report is run for that same subject on a machine where activity for it is stored
- **THEN** the report carries that activity and the command exits successfully, and the two reports
  differ both in what they print and in the code they exit with

#### Scenario: A subject something writes, with nothing to report
- **WHEN** a report is run for a subject something writes, and there is no activity for it
- **THEN** the report says there was no activity in the period, and that is a determined, successful
  answer

### Requirement: The catalogue of subjects does not promise silently
The list of subjects the client can report on SHALL state, for each subject, whether anything in the
build writes its activity. A subject that can only ever report nothing SHALL NOT be advertised
without that qualification.

The qualification SHALL be produced by the subject itself rather than by each surface that displays
it, so that two surfaces cannot word it differently.

#### Scenario: A person asks which subjects the client knows
- **WHEN** the subjects this build knows are listed
- **THEN** each subject nothing in this build writes is marked as such, on its own line
