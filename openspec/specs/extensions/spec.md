# extensions Specification

## Purpose
A person can add a channel to omw, or point omw at the model they want to use, and only has to
learn one thing. Both are extensions here: registered with the same command and the same arguments,
listed in the same listing beside the channels that ship built in, configured the same way, and —
when one is broken — described in the same words, whichever of the two it is. A company adding
Slack and a person choosing their own model are doing the same kind of thing, and this is where
that becomes true rather than merely intended (PRD §2.5).

The other half of what a person gets is an honest answer about what is broken. An extension that
failed to load is reported as having failed to load: never as absent, never as present-but-idle,
and never as silence. A channel whose adapter will not load is never reported as a quiet channel
with no traffic, and a model provider whose extension will not load is never reported as "no model
configured" — because a person told that goes and configures the model they already configured,
while the real fault goes unlooked at. Registering an extension contacts nothing and starts
nothing, and omw takes no custody of credentials: a setting that looks like a secret is refused
rather than recorded, so there is nothing anywhere for a later listing to leak.
## Requirements
### Requirement: A channel adapter and a model provider are registered by the same act
The product SHALL register a channel adapter and a model provider through one command taking the
same arguments in the same order, differing only in the extension being registered. The interface an
extension implements SHALL come from the extension itself and SHALL NOT be an argument a person
supplies.

#### Scenario: A person registers one of each
- **WHEN** a person registers a channel adapter and then registers a model provider, changing only
  the extension identifier
- **THEN** both registrations succeed, and the two invocations differ in exactly one argument

#### Scenario: Registration is attempted for an extension this machine does not have
- **WHEN** a person registers a name nothing on the machine offers
- **THEN** the registration is refused, and no record of it is left behind

### Requirement: There is one listing of extensions
The product SHALL report every extension through a single listing that covers both interfaces, and
each entry SHALL state which interface it implements. There SHALL NOT be a channel-only listing or a
model-only listing that shows anything the shared one does not.

#### Scenario: Both interfaces are registered
- **WHEN** a person lists extensions on a machine with a registered channel adapter and a registered
  model provider
- **THEN** both appear in the one listing, each stating its interface

#### Scenario: The built-in channels are listed
- **WHEN** a person lists extensions
- **THEN** the built-in channels appear in the same listing as anything registered through the
  extension point, in the same form, and not as a separate section

### Requirement: The state vocabulary is identical across the two interfaces
The product SHALL use one set of states for both interfaces — registered-and-loaded, failed-to-load,
not-registered and undetermined — and SHALL render a given state identically whichever interface the
extension implements, apart from the extension's own name and interface.

#### Scenario: A failed channel adapter and a failed model provider are compared
- **WHEN** a failed-to-load channel adapter entry and a failed-to-load model provider entry are
  rendered and compared with their names and interfaces normalised
- **THEN** the two renderings are identical

#### Scenario: A state is rendered
- **WHEN** any state is rendered for either interface
- **THEN** the rendering is non-empty, and no two of the four states render alike

### Requirement: Configuration is the same shape for both interfaces
The product SHALL accept an extension's settings through one command form for both interfaces.

#### Scenario: A person configures one of each
- **WHEN** a person configures a channel adapter and then a model provider using the same command
  form, changing only the extension identifier
- **THEN** both succeed and both record the settings supplied

### Requirement: Failed to load is its own answer
The product SHALL report a registered extension that raises on load as failed to load, distinguishably
from an extension that is not registered and from one that is registered and loaded, and SHALL carry
the reason for the failure attributable to that extension by name.

#### Scenario: A registered extension raises on load
- **WHEN** a person lists extensions with one registered extension that raises on load
- **THEN** it is reported as failed to load, with non-empty failure detail naming it, and its
  rendering differs from both the not-registered and the registered-and-loaded renderings

#### Scenario: A broken extension sits alongside a working one
- **WHEN** one registered extension fails to load and another loads
- **THEN** both are reported, each with its own state

### Requirement: A failed load is never reported as absence or as quiet
The product SHALL NOT report a channel whose adapter failed to load as a channel on which nothing
arrived, and SHALL NOT report a chosen model provider whose extension failed to load as a machine
with no model configured.

#### Scenario: Ingestion runs against a channel whose adapter failed to load
- **WHEN** ingestion runs for a channel whose registered adapter failed to load
- **THEN** the channel is reported as not reached, with the load failure as the reason, and it is
  not reported as reached with nothing on it

#### Scenario: A capability that needs a model runs with a broken provider extension
- **WHEN** a capability that needs a model runs with a provider chosen whose extension failed to
  load
- **THEN** it reports that the extension failed to load, with a code distinct from the
  no-model-configured code, and it does not report that no model is configured

### Requirement: Whether an extension loaded may be undetermined
The product SHALL render an extension whose load result could not be established as undetermined,
distinguishably from loaded, from failed-to-load and from not-registered, and SHALL NOT omit it from
the listing.

#### Scenario: A load result cannot be established
- **WHEN** a registered extension's load result cannot be established
- **THEN** it is listed with an undetermined state, its rendering is non-empty, and it differs from
  the other three renderings

#### Scenario: A registration record cannot be read
- **WHEN** a registration record is present and cannot be read
- **THEN** the extension is present in the listing with an undetermined state, and the other
  extensions are still reported

### Requirement: Exit status distinguishes loaded from failed from undetermined
The product SHALL exit with one status when every registered extension loaded, a different status
when at least one failed to load, and a third when at least one could not be determined, each
distinguishable without parsing output.

#### Scenario: Every registered extension loaded
- **WHEN** a person lists extensions and every registered one loaded
- **THEN** the command exits with the success status

#### Scenario: At least one failed and at least one could not be determined
- **WHEN** a person lists extensions with a failed one, and separately with an undetermined one
- **THEN** the two runs exit with statuses that differ from each other and from success

### Requirement: A summary over several extensions names neither interface
The product SHALL report a summary covering more than one extension using a code that belongs to
neither interface, whichever interface the extensions in it implement. Codes attached to a single
extension MAY remain specific to its interface.

#### Scenario: Only a channel adapter has failed
- **WHEN** a person lists extensions on a machine whose only failed extension is a channel adapter
- **THEN** the failure summary carries an interface-neutral code, and it does not name the model
  provider interface

#### Scenario: Only a model provider has failed
- **WHEN** a person lists extensions on a machine whose only failed extension is a model provider
- **THEN** the failure summary carries the same interface-neutral code as it does when a channel
  adapter has failed

### Requirement: Nothing about an extension is implicit
The product SHALL NOT start the daemon, open a network connection, or contact a provider's endpoint
as a side effect of registering, listing or configuring an extension, and SHALL treat an extension
present on the machine but not registered by a deliberate act as not registered.

#### Scenario: Extension commands are run with the daemon stopped
- **WHEN** a person registers, lists and configures extensions with the daemon not running
- **THEN** each command reports that the daemon is not running, and no daemon is running afterwards

#### Scenario: A model provider is registered
- **WHEN** a person registers a model provider
- **THEN** the provider's endpoint is not contacted and the extension is not loaded

#### Scenario: An extension is present and unregistered
- **WHEN** a person lists extensions on a machine offering an extension no deliberate act registered
- **THEN** it is reported as not registered, it is not ingesting and not serving a model, and the
  command does not treat it as a failure

### Requirement: The extension mechanism works with no hub
The product SHALL allow registering, listing, configuring and diagnosing extensions with no hub
configured, and SHALL leave no partially-completed registration behind when a registration is
refused.

#### Scenario: The whole sequence is run with no hub
- **WHEN** a person registers, lists, configures and inspects an extension with no hub configured
- **THEN** every step completes without a hub-related error

#### Scenario: A registration is refused
- **WHEN** a registration is refused for any reason
- **THEN** no record of it exists afterwards

### Requirement: An extension satisfies only the interface it implements
The product SHALL resolve an extension by name AND by interface wherever the answer depends on which
interface is meant. An extension registered under one interface SHALL NOT satisfy a question asked
about the other, and where a name resolves to the other interface the report SHALL say so rather
than reporting the name as absent.

#### Scenario: A channel adapter is chosen as a model provider
- **WHEN** a person registers a channel adapter and then chooses that name as their model provider
- **THEN** the product does not report that a model provider's extension loaded, and it says that
  what is registered under that name implements the channel adapter interface

#### Scenario: A model provider is chosen as a model provider
- **WHEN** a person registers a model provider and then chooses that name as their model provider
- **THEN** the product reports it as ready

#### Scenario: An extension is asked about without regard to interface
- **WHEN** a person asks to see whatever is registered under a name
- **THEN** the extension registered under that name is described, whichever interface it implements

### Requirement: An incomplete reading of the inventory is never reported as a complete one
The product SHALL report an extension whose registration record cannot be read as one undetermined
entry beside the extensions it could read, and SHALL NOT drop the others. Where the set of
registrations cannot be established at all, every surface SHALL say so, and no summary, exit status
or per-name answer derived from it SHALL claim a determined result.

#### Scenario: One registration record is damaged
- **WHEN** a person lists extensions with one registration record damaged and others intact
- **THEN** every registration is still listed, the damaged one is undetermined, and any extension
  that failed to load is still reported as failed to load

#### Scenario: A summary is produced over an inventory that could not be read in full
- **WHEN** a person lists extensions and part of the inventory could not be read
- **THEN** the report does not state that every registered extension loaded, and the command does
  not exit with the success status

#### Scenario: One extension is asked about while the inventory cannot be enumerated
- **WHEN** a person asks about an extension by name and the set of registrations could not be
  established
- **THEN** whether it is registered is reported as undetermined rather than as not registered

#### Scenario: One extension is asked about while only a single record is damaged
- **WHEN** a person asks about an extension by name, the set of registrations was established, and a
  different record could not be read
- **THEN** whether the named extension is registered is answered as a determined fact

### Requirement: The CLI and the control API report the same extension state
The product SHALL report extension state identically through the command line and through the
control API.

#### Scenario: A failed-to-load extension is read through both surfaces
- **WHEN** the same machine's extension state is read through the CLI and through the control API
- **THEN** both report the same state and the same failure reason

#### Scenario: The inventory cannot be read and both surfaces are consulted
- **WHEN** the set of registrations cannot be established and the machine's extension state is read
  through the CLI and through the control API
- **THEN** both report that the inventory may be incomplete

### Requirement: No extension state is silence
The product SHALL render every extension in the listing as exactly one non-empty entry, in every
state including undetermined.

#### Scenario: Every state is present in one listing
- **WHEN** a person lists extensions on a machine holding one extension in each of the four states
- **THEN** each produces exactly one entry, and no entry and no line within an entry is empty

### Requirement: An extension's credentials are never recorded or shown
The product SHALL NOT record a credential supplied as an extension setting, and SHALL NOT include a
credential in the extension listing, in a failure reason, or in a diagnostics bundle. A setting that
names a location rather than a value SHALL be recorded.

#### Scenario: A credential is supplied as a setting
- **WHEN** a person supplies a setting whose name says it holds a credential
- **THEN** the setting is refused, nothing else in that call is recorded, and the refusal does not
  echo the value back

#### Scenario: Extension output is inspected after a credential is configured
- **WHEN** a person configures a provider with a credential in their environment and then reads
  every extension-related output
- **THEN** the credential appears in none of them

#### Scenario: A setting names where a credential lives
- **WHEN** a person supplies a setting naming the path of a file holding their credential
- **THEN** the path is recorded and the file's contents are not read

### Requirement: An extension is granted nothing wider than its person
The product SHALL refuse a registration requesting a scope its person does not hold, rather than
registering the extension with a narrower scope, and SHALL refuse a scope outside the scope
vocabulary.

#### Scenario: An extension requests a scope wider than its person holds
- **WHEN** a person registers an extension requesting a scope they do not hold
- **THEN** the registration is refused entirely, and the extension is not registered with a narrower
  scope instead

#### Scenario: An extension requests a scope outside the vocabulary
- **WHEN** a person registers an extension requesting a scope that is not in the scope vocabulary
- **THEN** the registration is refused and names the vocabulary

