# model Specification

## Purpose

A person chooses which model serves their agent subscriptions and their `review` publication mode,
and **supplies their own credentials for it** (PRD §3.13). The provider is an interface — the same
extension mechanism as a channel adapter (§2.5) — because a company adding a channel and a company
choosing a model are doing the same kind of thing and should not have to learn two systems.

**A key belongs to the person who supplied it.** It is not published, not synchronised between
devices, and **not readable through the agent API**. The store records which provider was chosen and
*where the credential lives* — a name and a path, never the bytes. A key that can be read back by
anything other than the person who typed it has stopped being theirs.

**No model configured is not a broken client.** This is the constraint that shapes the whole
capability. Everything that does not need a model keeps working exactly as before — no degraded mode,
no warning banner on unrelated commands, no exit code that makes a script think something is wrong.
And everything that *does* need one says precisely what is missing, rather than failing in a way the
person has to decode.

Three states, and they must never collapse into two (PRD §4.3):

- **no provider chosen** — nothing has been decided;
- **a provider chosen but no credential supplied** — a decision made and half-finished;
- **configured** — and even then, a model that cannot be reached is its own answer again: a review
  that could not be completed is **neither a pass nor a refusal**, and reporting it as either would
  put a draft through a gate nobody actually applied.

A draft blocked on a missing model is *blocked on a missing prerequisite*, not *awaiting the person*.
Those look identical in a list and mean opposite things about whose move it is.
## Requirements
### Requirement: Whether a model is configured has one definition
The product SHALL resolve "which model provider is chosen, and is a credential supplied for it" in
exactly one place. No other package SHALL read the environment variables that carry a person's model
choice or credential, and no other package SHALL implement a second resolution of that question.

#### Scenario: A surface needs to know whether a model is configured
- **WHEN** any command, gate or report needs the person's model configuration
- **THEN** it obtains the answer from the model package, and it does not read the model environment
  variables itself

#### Scenario: A second reader is introduced
- **WHEN** a file outside the model package's resolution names a model environment variable
- **THEN** the build fails and names the one definition that should have been consumed instead

#### Scenario: The structural search stops matching
- **WHEN** the search that enforces the single reader finds no occurrence of the variables anywhere
- **THEN** it refuses rather than passing, because a search that matches nothing is
  indistinguishable from a codebase with no readers at all

### Requirement: Choosing a provider and supplying a credential are explicit acts
The product SHALL configure a model provider and a credential only when a person asks for it. No
other command SHALL configure either as a side effect, and a subsequent read SHALL report the chosen
provider by name.

#### Scenario: A person chooses a provider
- **WHEN** a person chooses a provider and then reads their model configuration back
- **THEN** the read-out names the provider they chose

#### Scenario: Any other command runs
- **WHEN** any command other than the one that configures a model is run
- **THEN** no model configuration is recorded as a result

#### Scenario: A configuration naming no provider is read back
- **WHEN** a person reads their model configuration on a machine where no provider is chosen
- **THEN** the read-out reports that no provider is chosen, and it does not render identically to a
  read-out that names a provider

### Requirement: A provider chosen without a credential is its own answer
The product SHALL report a chosen provider with no credential supplied as such. It SHALL NOT report
that state as fully configured, and it SHALL NOT report it as no-provider-configured.

#### Scenario: A provider is chosen and no credential has been supplied
- **WHEN** a person chooses a provider and reads their configuration back before supplying a
  credential
- **THEN** the read-out says a provider is chosen and no credential has been supplied, and that
  output is distinguishable from both a fully configured read-out and a no-provider read-out

### Requirement: A credential belongs to the person who supplied it
The product SHALL NOT publish, synchronise, print or return a person's model credential. The
credential value SHALL NOT appear in any output stream, any diagnostic, any record written to the
local store, or any response from a programmatic interface.

#### Scenario: Any command is run with a credential configured
- **WHEN** a person has a credential configured and runs any command the product offers
- **THEN** the credential value appears in neither the command's answer nor its reasons

#### Scenario: The local store is exported in full
- **WHEN** every byte the product has written to the local store is read back
- **THEN** the credential value appears nowhere in it

#### Scenario: A programmatic interface reports the model configuration
- **WHEN** a programmatic interface reports a person's model configuration
- **THEN** it may report which provider is configured and whether a credential is present, and the
  credential value is absent from the response

#### Scenario: A programmatic interface asks for the credential value
- **WHEN** a caller asks a programmatic interface for the credential value itself
- **THEN** the request is refused with its own machine-readable identity, rather than answered with
  an empty or redacted value, and the refusal is distinguishable from a successful report that no
  credential is configured

#### Scenario: A credential exists and a caller asks for it
- **WHEN** a caller asks for the credential value on a machine where one is configured, and on a
  machine where one is not
- **THEN** the two refusals are identical, so that the refusal itself does not reveal whether a
  credential exists

### Requirement: No model configured is not a broken client
The product SHALL keep every capability that does not require a model fully working when no model is
configured, and SHALL report success for those capabilities without warning about the absent model.
A capability that does require a model SHALL name the missing model as the thing that is missing.

#### Scenario: A capability that needs no model is used with no model configured
- **WHEN** a person with no model configured uses a capability that does not require one
- **THEN** it works fully and reports success, with no warning or degradation attributable to the
  absent model

#### Scenario: The model configuration is read on a machine with no model
- **WHEN** a person with no model configured reads their model configuration
- **THEN** the product reports that no model is configured and reports the reading itself as having
  succeeded, because a determined negative is an answer

#### Scenario: A capability that needs a model is used with no model configured
- **WHEN** a person with no model configured uses a capability that requires one
- **THEN** the product names the absent model as what is missing, reports failure, and produces
  output distinguishable from a successful model-backed run

#### Scenario: A configured model fails
- **WHEN** a person's configured model rejects their credential or cannot be reached
- **THEN** that outcome is distinguishable from no model configured and from a successful run, and
  the three do not share an exit status

### Requirement: A model configuration that could not be determined is not a negative
The product SHALL report a model configuration state it could not establish as undetermined. That
state SHALL be distinguishable from a configured state, distinguishable from no-model-configured,
and SHALL NOT be rendered as silence. It SHALL NOT share an exit status with a determined negative.

#### Scenario: The credential source cannot be read
- **WHEN** a credential file is named, exists and cannot be read
- **THEN** the product reports whether a credential is supplied as undetermined, says why, and does
  not report that no credential is configured

#### Scenario: The recorded choice cannot be read
- **WHEN** a recorded model choice is present in the store and cannot be read
- **THEN** the product reports which provider is configured as undetermined, and does not report
  that no provider is chosen

#### Scenario: A named credential source is absent
- **WHEN** a credential file is named and does not exist
- **THEN** the product reports that no credential is supplied and names the file that is missing,
  because the absence was established rather than merely suspected

#### Scenario: A model configuration state is left unset
- **WHEN** a model configuration value is produced without being determined
- **THEN** its value is undetermined rather than a negative

### Requirement: Review runs on the client with the person's own model
The product SHALL evaluate a draft in `review` mode on the person's own machine, against rules they
wrote, using the provider and credential they configured locally. It SHALL NOT consult a hub to do
so, and no hub SHALL supply, select or override the model or credential used.

#### Scenario: A draft is reviewed with no hub configured
- **WHEN** a person with no hub configured and a model configured runs `review` over a draft
- **THEN** the review reaches a verdict, and nothing in the run consults or mentions a hub

#### Scenario: A draft is reviewed with a hub configured
- **WHEN** a person with a hub configured runs `review` over a draft
- **THEN** the provider and credential used are the locally configured ones

#### Scenario: Hub-touching operations are exercised with a credential configured
- **WHEN** every operation that involves a hub is run on a machine with a model configured
- **THEN** the locally configured provider and credential are unchanged afterwards

### Requirement: A model provider is an interface, and registering one contacts nothing
The product SHALL express a model provider as an interface. Registering a provider SHALL NOT contact
that provider's endpoint, bind a credential, or open any connection.

#### Scenario: A provider is registered
- **WHEN** a provider is registered, looked up and listed
- **THEN** nothing but its name is asked of it, and its endpoint is not contacted

#### Scenario: A model configuration is read
- **WHEN** a person's model configuration is read
- **THEN** no provider is opened and no endpoint is contacted

#### Scenario: A provider is chosen that this build has no adapter for
- **WHEN** a person chooses a provider for which this build registers no adapter
- **THEN** the choice is recorded, and the read-out reports the missing adapter as a determined fact
  distinct from no-provider-configured and from undetermined

### Requirement: The command line and the control interface report the same model state
The product SHALL report the same model configuration state through the command line and through the
control interface, with the same three-way distinction between configured, not configured and
undetermined, and with the credential value absent from both.

#### Scenario: The two surfaces are asked about the same machine
- **WHEN** the command line and the control interface report the model configuration for one machine
  at one moment
- **THEN** they describe the same state in the same words

#### Scenario: A surface words the state independently
- **WHEN** a surface formats the model configuration itself rather than rendering the shared state
- **THEN** the two surfaces disagree and the build fails

#### Scenario: The model state crosses the control interface
- **WHEN** a model configuration is carried over the control interface and read back
- **THEN** the three-way distinction survives unchanged, and the credential value is absent from the
  bytes that crossed

### Requirement: Configuring a model starts nothing and reaches out to nothing
The product SHALL NOT start the daemon on a person's behalf when they configure, read or clear their
model configuration, and SHALL say whether the daemon is running. With no hub configured, those acts
SHALL open no outbound connection.

#### Scenario: A model command is run with the daemon not running
- **WHEN** a person configures, reads or clears their model configuration while the daemon is not
  running
- **THEN** the command says the daemon is not running, says that it started nothing, and completes

#### Scenario: A model command is run with no hub configured
- **WHEN** a person configures, reads or clears their model configuration with no hub configured
- **THEN** the act completes fully and no outbound connection is opened

### Requirement: The local half of this capability stands alone
The product SHALL let a person choose a provider, supply a key, read the configuration back and run
`review` with no hub configured at all. Nothing in this capability SHALL require a hub, and nothing
in it SHALL half-work without one.

#### Scenario: The whole capability is exercised with no hub configured
- **WHEN** a person with no hub configured chooses a provider, names a credential file, reads their
  configuration back and clears it
- **THEN** every act succeeds, and none of them mentions a hub

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

