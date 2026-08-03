# The inbox

## ADDED Requirements

### Requirement: The inbox is a list of obligations, not a mirror of the traffic
The product SHALL render every ticket in the inbox with both its title and its summary, and SHALL
render a field that is absent distinguishably from a field that is the empty string.

#### Scenario: A listing shows both fields of every ticket
- **WHEN** a person lists an inbox holding tickets
- **THEN** every ticket is rendered with its title and its summary

#### Scenario: A missing field and an empty field
- **WHEN** a ticket in the store has a title or a summary that was never recorded, and another has
  one recorded as the empty string
- **THEN** the two render differently, and neither renders as nothing at all — a real value and a
  missing value never produce the same output

#### Scenario: Reading one ticket by its identifier
- **WHEN** a person reads a ticket that is in the inbox
- **THEN** its title, its summary, and the fact that it is an inbox ticket held on this machine are
  rendered, and the command exits zero

#### Scenario: Reading an identifier that is not in the inbox
- **WHEN** a person reads an identifier the inbox does not hold
- **THEN** the command exits non-zero with no ticket rendering on standard output, distinguishable
  from a successful read by exit code alone

#### Scenario: An empty inbox and an inbox that could not be read
- **WHEN** a person lists an inbox that holds no tickets, and separately lists one that could not be
  read at all
- **THEN** the empty inbox is stated explicitly and succeeds, the unreadable one says it is not an
  empty inbox, and the two never render identically nor share an exit code

### Requirement: An acknowledgement is not a ticket, and there is no priority for one to sit at
The product SHALL NOT list any ticket whose title is the verbatim body of an incoming message such
as `yes`, `ok` or `Hii`, and SHALL NOT expose any per-ticket priority, rank or ordering value with a
value corresponding to an acknowledgement or to small talk.

#### Scenario: Tickets seeded from traffic containing acknowledgements
- **WHEN** the source material for the inbox included messages whose bodies were `yes`, `ok` and
  `Hii`, alongside one request that is genuinely owed
- **THEN** no listed title is any of those bodies, and the traffic about one problem is one ticket

#### Scenario: An acknowledgement offered to the inbox
- **WHEN** something whose title is an acknowledgement is offered to the inbox
- **THEN** it is refused, and it is not stored at any priority level — there is no priority level

#### Scenario: The set of listed tickets contains no acknowledgement at any rank
- **WHEN** the tickets in a listing are enumerated
- **THEN** none is categorised as an acknowledgement at any priority, and no ranking, priority or
  ordering value is exposed on a ticket at all

### Requirement: Tickets never leave the machine
The product SHALL offer no inbox operation that publishes, shares or sends a ticket under any name,
and SHALL open no connection to a hub while listing, reading or deleting tickets, whether or not a
hub is configured.

#### Scenario: The operations available on a ticket are enumerated
- **WHEN** the operations the inbox offers on a ticket are enumerated
- **THEN** they are list, read and delete, and none of them transfers a ticket to a hub

#### Scenario: A hub is configured and reachable
- **WHEN** a hub is configured and listening, and a person lists, reads and deletes tickets
- **THEN** no request reaches it and its set of published notes is unchanged

#### Scenario: No hub is configured
- **WHEN** no hub is configured and a person lists, reads and deletes tickets
- **THEN** every operation works fully and succeeds, and no outbound connection is opened

### Requirement: Nothing expires, and only the person removes a ticket
The product SHALL keep every ticket in the inbox until the person deletes that ticket, regardless of
how long it has been there, and SHALL remove exactly the ticket the person named.

#### Scenario: A ticket that has been owed for a very long time
- **WHEN** a ticket has been in the inbox for longer than any plausible expiry window
- **THEN** it is still listed and still readable, and its identifier is unchanged

#### Scenario: Every operation exercised
- **WHEN** every inbox operation other than deleting a given ticket has been exercised
- **THEN** the set of tickets is identical to what it was before

#### Scenario: Deleting a ticket the person named
- **WHEN** a person deletes a ticket that is in the inbox
- **THEN** exactly that ticket is absent from the subsequent listing and no longer readable, and
  every other ticket is still listed

#### Scenario: Deleting an identifier that is not in the inbox
- **WHEN** a person deletes an identifier the inbox does not hold
- **THEN** the command exits non-zero, distinguishable by exit code from a delete that removed a
  ticket, and the set of tickets is unchanged

### Requirement: The inbox says what it could not determine, and never as a "no"
The product SHALL render any part of a ticket's state that could not be determined as undetermined,
distinguishably from both a real value and a negative or empty one, and SHALL NOT give "could not be
determined" the same exit code as "determined to be nothing".

#### Scenario: Three states of the same field
- **WHEN** the same field of three tickets holds a real value, no value, and a value that could not
  be determined
- **THEN** the three render as three distinct outputs, none of them silence

#### Scenario: The inbox's own count cannot be read
- **WHEN** the tickets in the store cannot be listed
- **THEN** the count is rendered as undetermined, the command says it is not an empty inbox, and it
  exits on a code that no successful listing uses

### Requirement: The inbox is local, explicit, and says what it did not do
The product SHALL NOT start the daemon on a person's behalf, SHALL say that the daemon is not
running rather than appear to have read a live inbox, and SHALL NOT open the control API where
owner-only socket permissions cannot be confirmed.

#### Scenario: An inbox command with no daemon running
- **WHEN** a person runs an inbox command and no daemon is running
- **THEN** the daemon is still not running afterwards, and the command says it is not running and
  that what was read is the store on disk

#### Scenario: Owner-only socket permissions cannot be confirmed
- **WHEN** the control socket's permissions cannot be confirmed to be owner-only
- **THEN** the control API does not open, the command says so, and that output is distinguishable
  from an empty inbox and from a hub error

#### Scenario: A local capability that cannot be performed
- **WHEN** some part of an inbox operation cannot be performed
- **THEN** the command names precisely what is missing and exits non-zero, and never returns success
  with a partial listing nor an empty listing in place of a failed one
