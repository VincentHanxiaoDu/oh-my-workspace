# notes Specification

## Purpose

A knowledge system that defaults to private has no knowledge in it (PRD §3.3). So a note is
company-wide unless its author says otherwise, and narrowing it — to named people, to a group, or to
yourself — is a deliberate act the author performs, not a default they fall into.

The harder half of this capability is honesty about what narrowing *is*. Restriction controls which
**colleagues** can read a note. **It is not a wall against whoever operates the hub**, which stores
and indexes every published note precisely so it can be found — including notes narrowed to a group
or to yourself (PRD §2.4). A product that let "only me" imply "nobody else can technically read
this" would be selling a privacy guarantee it does not provide. The genuinely private note is the one
never published, and that is the third branch of the loop in PRD §1.

So the statement belongs **at the point of choice**, next to the narrowing itself, not in
documentation and not in a one-time onboarding screen a person clicks past and never sees again.

Two constraints follow:

- **Visibility precedes ranking, and precedes disclosure of any kind.** What a reader may see is
  settled before anything is ordered, counted or summarised, so no surface reveals that a note the
  reader cannot open exists. A note narrowed away from someone is indistinguishable, to them, from a
  note that was never written.
- **Publishing is its own grant.** Changing who can see a note is part of publishing it, so it
  requires the `publish` scope — never `read`, and never `write`. A grant that could publish was
  asked for on purpose (PRD §3.10). The hub operator's ability to read everything is a deployment
  fact stated plainly, not a scope anyone holds or delegates.
## Requirements
### Requirement: Default visibility is company-wide and is a real value
A note published with no visibility choice expressed SHALL be company-wide, and reading its
visibility back SHALL report the company-wide value rather than an empty value, "unset", or a null
the caller must interpret.

#### Scenario: A note is published with no choice expressed
- **WHEN** a note is published and no visibility is given
- **THEN** its visibility reads back as `company`, and every colleague at the company can read it

#### Scenario: A visibility value nobody set is not an audience
- **WHEN** a visibility value is left at its zero value by a caller or an error path
- **THEN** it is not company-wide, and evaluating it answers undetermined rather than granting or
  refusing access

### Requirement: A note can be narrowed to named people
A note SHALL be publishable to a named set of people, readable by exactly those people and its
author, and by no other colleague.

#### Scenario: A colleague outside the named set
- **WHEN** a colleague who is not named tries to read the note
- **THEN** the read is refused, and the note does not appear in that colleague's listings

#### Scenario: Naming nobody
- **WHEN** a narrowing to named people names nobody
- **THEN** the publication is refused, and no note is published to an empty audience

### Requirement: A note can be narrowed to a group
A note SHALL be publishable to one group, readable by exactly the group's current members per the
hub's own membership record.

#### Scenario: Membership changes after publication
- **WHEN** a colleague joins the group after the note was published
- **THEN** that colleague can read the note, and a colleague who leaves the group can no longer
  read it

#### Scenario: A group the hub does not know
- **WHEN** a note is published narrowed to a group name the hub has no membership record for
- **THEN** the publication is refused with a distinguishable failure, and the note is not published
  company-wide as a fallback and not published to an empty audience

### Requirement: A note can be narrowed to yourself
A note SHALL be publishable so that only its author can read it.

#### Scenario: A colleague who belongs to every group
- **WHEN** a colleague who is a member of every group the hub knows tries to read a self-only note
- **THEN** the read is refused, and the note does not appear in that colleague's listings or search
  results

### Requirement: The four states render distinguishably
The product SHALL render company-wide, named people, group and self distinguishably, and no two
different narrowings SHALL render identically.

#### Scenario: The four renderings are compared with each other
- **WHEN** the renderings of the four states and of the undetermined answer are compared pairwise
- **THEN** no two of the five are equal, and none is blank

### Requirement: Restriction is stated where the person chooses it
Every surface where a person chooses a narrowed visibility SHALL state, at that point of choice,
that restriction controls which colleagues see the note and is not a wall against whoever operates
the hub. No surface SHALL describe a narrowed note as private, encrypted, secret, or visible only to
the author without that statement in the same view.

#### Scenario: Choosing a visibility on the command line
- **WHEN** a person runs any command-line surface that offers a visibility choice
- **THEN** the §2.4 statement appears in the same output

#### Scenario: An agent reads the publish schema
- **WHEN** an agent reads the agent API's schema for the visibility field
- **THEN** the §2.4 statement is part of that field's own description

#### Scenario: The hundredth publication
- **WHEN** a person narrows a note for the hundredth time
- **THEN** the statement appears exactly as it did the first time

### Requirement: The version timeline is not a bypass around visibility
Changing a note's visibility SHALL take effect for its earlier versions too: a reader excluded by a
narrowing SHALL NOT be able to read any version of that note through the addressable timeline.

#### Scenario: A reader is narrowed out after reading version 1
- **WHEN** a colleague who could read version 1 is excluded by a later narrowing
- **THEN** that colleague can read neither the note nor version 1 through the timeline

### Requirement: Groups resolve against the hub's own membership record
Evaluating a group narrowing SHALL consult the hub's own membership record and SHALL NOT perform any
directory, LDAP or SSO lookup. Evaluation SHALL work with no directory integration present.

#### Scenario: No directory integration exists
- **WHEN** a group narrowing is evaluated on a hub with no directory integration configured
- **THEN** a member reads the note and a non-member is refused, both determined answers

### Requirement: A grant wider than its holder is refused when it is requested
A request for a visibility-related grant that would let its holder read more than the person it acts
for can read SHALL be refused at the moment it is requested, with a refusal distinguishable from
success without parsing prose, and SHALL NOT result in a narrower grant being issued instead.

#### Scenario: A colleague asks for a grant carrying the operator's read scope
- **WHEN** the request is made
- **THEN** it is refused with a machine-readable code, no grant is issued, and the set of grants
  attributable to that person is unchanged

#### Scenario: An agent reads a note its person cannot
- **WHEN** a note narrowed to exclude the person is read through a grant they hold
- **THEN** the read is refused, and the refusal is distinguishable from "no such note"

### Requirement: A visibility that cannot be determined is rendered as undetermined
If a note's visibility cannot be determined — the hub is unreachable, the group's membership cannot
be resolved, or the record is unreadable — the product SHALL render it as undetermined, distinct in
both wording and exit code from a real value and from a negative one, and SHALL NOT behave as if the
note were company-wide or self-only.

#### Scenario: The hub cannot be reached
- **WHEN** a person asks for a note's visibility and the hub cannot be reached
- **THEN** the answer renders as undetermined, the exit code is the undetermined one and is shared
  with neither success nor failure, and neither the company-wide nor the self-only wording appears

#### Scenario: A group's membership cannot be resolved
- **WHEN** a note narrowed to a group is read and the membership record cannot answer
- **THEN** the answer is undetermined rather than a refusal, and the note is reported as
  unevaluated rather than omitted from a listing

### Requirement: The local half stands alone
With no hub configured, choosing and recording a draft's intended visibility SHALL work fully and
locally, no visibility operation SHALL open a network connection, and any part that genuinely
requires the hub SHALL state that no hub is configured rather than reporting an empty audience or a
success.

#### Scenario: Planning a draft's visibility with no hub
- **WHEN** a person states a draft's intended visibility with no hub configured
- **THEN** the choice is accepted and reported back as expressed, together with the §2.4 statement

#### Scenario: Asking who can read a published note with no hub
- **WHEN** a person asks with no hub configured
- **THEN** the product says no hub is configured, does not report success, and does not report an
  empty audience

#### Scenario: No daemon is running
- **WHEN** a visibility surface needs the daemon and none is running
- **THEN** the command says the daemon is not running and does not start it

### Requirement: Changing who can see a note requires the publish scope
Setting a note's visibility, or narrowing it later, is part of publishing that note, and the system
SHALL refuse it to a grant that does not carry the publish scope, leaving the note's visibility
unchanged.

#### Scenario: a read-only grant tries to narrow a note
- **WHEN** a grant carrying only the read scope calls the visibility-setting entry point
- **THEN** the call is refused with a distinct machine-readable code
- **AND** the note's visibility afterwards is the visibility it had before the attempt

#### Scenario: a read-only grant tries to publish
- **WHEN** a grant carrying only the read scope calls the publishing entry point
- **THEN** the call is refused with the same code, distinguishably from success
- **AND** no note is published

#### Scenario: a grant carrying the publish scope succeeds
- **WHEN** a grant carrying the publish scope sets a note's visibility
- **THEN** the visibility is changed, so the refusal above is shown to be the scope check and not a
  blanket refusal

