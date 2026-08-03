# agent Specification

## Purpose

This is how a person's own AI reads what only that person can see — their tickets, their drafts, and
the hub as **they** are permitted to read it (PRD §3.12). It is the reading half of the loop in §1:
what the AI reads, it can be told to write up, so that the next person with the same problem finds an
answer instead of starting again.

Two properties make it safe to point an AI at somebody's private material, and both are stated as
refusals:

**It is scoped to that person and nothing wider.** An agent cannot read what its person cannot. There
is no elevated mode, no service account, no scope that widens the view — the same authority model as
every other surface (PRD §4.5), and the same three names: `read`, `write`, `publish`. A `read` grant
cannot write a draft and cannot publish, and each refusal is distinguishable from an empty result
**and** from an unreachable hub. Those are three different facts, and collapsing any two of them would
let an agent report "nothing there" when the truth was "you were not allowed" or "nobody answered".

**It is local.** The agent reaches the daemon over the control API — a unix socket on this machine
(PRD §4.6) — never over the network. Nothing in this path opens an outbound connection, and the
product holds itself to that structurally rather than by intention.

**And the person's model credential is not readable through it.** A key belongs to whoever supplied
it (PRD §3.13). It is not published, not synchronised, and does not appear here — not in a listing,
not in an error message, not in diagnostics. Whether a model is configured is a fair question for an
agent to ask; what the key is, is not.

The visibility rules the hub enforces reach all the way through: a note restricted away from this
person leaves **no trace** in what their agent can see — not in a count, not in an identifier, not in
an ordering. An agent that could infer the existence of what its person may not read would be a way
around §3.5, arriving through a door nobody was watching.
## Requirements
### Requirement: A person's own AI reads their own material
The product SHALL offer an interface through which a person's own AI reads that person's inbox
tickets, that person's unpublished drafts, and the hub as that person is permitted to read it. What
this interface returns for a person at a moment SHALL equal what the command line returns for the
same person at the same moment, including which items could not be determined.

#### Scenario: Tickets read through the agent API and through the command line
- **WHEN** a daemon is running against a store holding tickets, and both the agent API and the
  equivalent command-line reading are asked for that person's tickets
- **THEN** both return the same tickets, and neither returns one the other does not

#### Scenario: A draft is served as unpublished
- **WHEN** the outbox holds an unpublished draft and the agent API is asked for the person's drafts
- **THEN** that draft is returned carrying its state as drafted and not published, so that a reader
  does not have to infer which of the four note states it is in

#### Scenario: Hub material is served as that person may read it
- **WHEN** a hub is configured and reachable and the agent API is asked for hub material
- **THEN** exactly the notes that person is permitted to read are returned, and the visibility
  decision is the product's one visibility predicate rather than a second one

### Requirement: An agent cannot read what its person cannot
The product SHALL NOT return, through the agent API, any trace of a note the person is not permitted
to read: not its body, not its title, not an identifier, and not a count that changes with its
existence.

#### Scenario: A colleague's note is restricted away from this person
- **WHEN** a colleague publishes a note with a visibility that excludes this person, and this
  person's agent asks for hub material
- **THEN** the note is absent from the result entirely, and the serialised result is byte-identical
  to the result from a hub that does not contain that note at all

#### Scenario: The unreadable note is addressed directly
- **WHEN** an agent asks for a note that exists and its person may not read
- **THEN** the request is refused, and the refusal is distinguishable by a machine-readable code
  from the answer given for a note that does not exist

#### Scenario: A note's readability could not be determined
- **WHEN** whether the person may read a note cannot be worked out
- **THEN** that note is neither listed as readable nor silently dropped, the answer says how many
  such notes there were, and the outcome is undetermined rather than a completed list

### Requirement: An agent's authority never exceeds its person's
The product SHALL refuse, at the moment it is requested, any agent request presenting a scope its
person does not hold or a scope that was not granted to that agent. The product SHALL NOT narrow
such a request to what the holder can do. The scope vocabulary SHALL be exactly read, write and
publish, and the agent API SHALL NOT introduce a fourth.

#### Scenario: A grant is asked for that is wider than its holder
- **WHEN** a grant is requested carrying a scope the person does not hold
- **THEN** the request is refused entirely, no grant is issued, and the refusal is distinguishable
  from an empty successful result by the response itself rather than by an absence of items

#### Scenario: An agent presents a scope it was not granted
- **WHEN** an agent whose person holds a scope presents that scope on a request, and its own grant
  does not carry it
- **THEN** the request is refused before anything is read, with a code distinct from the code used
  when the person themselves does not hold the scope

#### Scenario: A read-scoped agent attempts to write or publish
- **WHEN** an agent holding only the read scope attempts to change a draft, or to publish a note
- **THEN** both attempts are refused, the draft is unchanged, nothing appears on the hub, and each
  refusal is distinguishable from an empty result and from a failure to reach the hub

#### Scenario: An agent's authority is revoked
- **WHEN** an agent's grant is revoked and a further request is made under it
- **THEN** that request is refused, with a code distinguishable from the code for a grant that was
  never issued, and an earlier successful request does not keep the later one alive

### Requirement: The agent API is local and is served over the control API
The product SHALL serve every agent API request over the daemon's local control API and SHALL NOT
offer any network transport for it. No agent API request SHALL start the daemon.

#### Scenario: No daemon is running
- **WHEN** an agent API request is made and no daemon is running against the store
- **THEN** the request fails naming the daemon as not running, and the daemon is not started

#### Scenario: Owner-only socket permissions cannot be confirmed
- **WHEN** the daemon is running and its control API did not open because owner-only permissions on
  its socket could not be confirmed
- **THEN** the agent API does not serve, and the failure says so in wording and in a code
  distinguishable from "the daemon is not running" and from a refusal for scope

#### Scenario: A connection is attempted from another machine
- **WHEN** anything off this machine attempts to reach the agent API
- **THEN** it does not reach it, whether or not a hub is configured, because the only transport is a
  local socket with no address an off-host packet can name

### Requirement: The local half of the agent API stands alone
The product SHALL serve tickets and drafts through the agent API with no hub configured, and SHALL
open no outbound connection in that condition.

#### Scenario: Tickets and drafts with no hub configured
- **WHEN** no hub is configured and an agent API session reads tickets and drafts
- **THEN** both succeed in full and no outbound connection is opened

#### Scenario: Hub material is asked for with no hub configured
- **WHEN** no hub is configured and the agent API is asked for hub material
- **THEN** it states precisely that no hub is configured, does not return an empty result as though
  the hub held nothing, and does not return local material under a label implying hub coverage

### Requirement: The model credential is not readable through the agent API
The product SHALL NOT return a configured model credential through any agent API operation, in any
successful answer, error message or diagnostic payload. The product SHALL still make it possible to
learn whether a model is configured.

#### Scenario: A credential is configured and the agent API is exercised
- **WHEN** a model provider and a credential are configured and every agent API operation is
  exercised, including its refusing and undetermined paths
- **THEN** no response contains the credential

#### Scenario: Whether a model is configured is asked for
- **WHEN** the agent API is asked about the model
- **THEN** it answers configured, not configured, or could not be determined, and those three are
  distinguishable from each other, without the credential's value being readable

#### Scenario: No model is configured
- **WHEN** no model is configured and the agent API is asked for tickets, drafts or hub material
- **THEN** those reads still succeed, because no model configured is not a broken client

### Requirement: The agent API answers in three values
The product SHALL render a state the agent API could not determine as undetermined, never as a
negative and never as silence, and SHALL NOT let an undetermined answer share an exit code with a
determined one.

#### Scenario: The hub is configured and cannot be reached
- **WHEN** a hub is configured and does not answer
- **THEN** the result is undetermined, with an exit code distinct from both a successful empty read
  and a refusal, and wording that is neither a rejected request nor an empty hub

#### Scenario: An answer nobody produced is read back
- **WHEN** an agent API outcome is absent or unrecognised
- **THEN** it is treated as undetermined rather than as a success or a negative

