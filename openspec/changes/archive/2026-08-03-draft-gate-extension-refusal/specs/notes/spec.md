# Drafting notes into the outbox

## ADDED Requirements

### Requirement: Writing a draft in review mode names a broken extension rather than a missing model
Creating a draft in review mode SHALL distinguish "no model is configured" from "the chosen
provider's extension failed to load", in the exit code, in the message a person reads, and in the
state reason recorded against the draft. The extension SHALL be consulted before the credential, so
that a person whose extension is broken and whose credential is also absent is told about the
broken client first.

#### Scenario: A broken extension and no credential
- **WHEN** a person writes a draft in review mode on a machine where a model provider is chosen, its
  extension is registered and fails to load, and no credential has been recorded
- **THEN** the command exits non-zero, reports the code `model-provider-extension-failed-to-load`,
  carries the extension's own reason for failing, does not report `no-model`, and the state recorded
  against that draft names the extension rather than saying no model is configured

#### Scenario: A working extension and no credential
- **WHEN** a person writes a draft in review mode on a machine where a model provider is chosen, its
  extension loads, and no credential has been recorded
- **THEN** the command exits non-zero, reports the code `no-model`, does not blame the extension, and
  the state recorded against that draft says no model is configured

#### Scenario: The two surfaces agree about one machine
- **WHEN** a person's extension listing reports the chosen model provider as having failed to load
- **THEN** writing a draft in review mode on that same machine reports the same failure with the same
  reason, rather than a different account of what is wrong

### Requirement: One statement of what is wrong with the model, for every review-mode gate
Every capability that refuses in review mode because of the model SHALL obtain the exit code, the
message and the recorded state reason from a single shared decision, and SHALL NOT determine for
itself whether the machine has no model configured or a broken extension. A capability MAY vary only
the sentence describing what that command has and has not done.

#### Scenario: Two gates, one machine, one answer
- **WHEN** a person writes a draft and then reviews it, in review mode, on a machine in the same
  model state throughout
- **THEN** both commands report the same code and the same reason for that state

#### Scenario: A capability that states the rule for itself
- **WHEN** any package states for itself which of "no model configured" and "the extension failed to
  load" a machine is in, rather than consulting the shared decision
- **THEN** the build's own checks fail and name the offending function

#### Scenario: The check reaches beyond the package holding the decision
- **WHEN** such a restatement is placed in a package other than the one the shared decision lives in
- **THEN** it is still named, because the check is made over every package rather than over the one
  the decision belongs to
