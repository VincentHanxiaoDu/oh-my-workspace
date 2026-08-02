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

### Notes are versioned, and the timeline is addressable

A note is not a mutable cell. Amending one **adds a version and never overwrites**, because somebody
may have acted on what it said last month and *"what did this say when I read it"* has to remain a
question with an answer (PRD §3.3). Nothing expires — no version is removed by age, by count, or by
any other rule (§5.4).

Search finds the latest. But the older versions stay readable, and the product's obligation is to
never let a reader confuse the two:

- **Every read states its standing** — current, superseded, or could not be determined. A person
  learns which one they are holding from the output, not by reasoning about the arguments they typed.
- **An unqualified request never yields superseded content.** Asking for "the note" gets the note as
  it stands.
- **A version that does not exist and a version that is empty are different answers**, and so are an
  empty body and a body that could not be read. The last of those is stated outright rather than
  shown as blank space, because blank space and "there is nothing here" are the same pixels.

**The timeline is not a bypass around visibility.** A reader narrowed out of a note today cannot
enumerate the versions written while they were included — version access runs through exactly the
same permission check as reading the note, so there is one answer to "may this person see this" and
not two that can drift apart.
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

### Requirement: Amending a note adds a version and never overwrites one
Publishing a changed body for an existing note SHALL append a new version, and every earlier
version SHALL remain retrievable with its body byte-identical to what was published.

#### Scenario: A note is amended and the earlier text is asked for
- **WHEN** a note is published and then a changed body is published for the same note, and the
  earlier version is retrieved
- **THEN** the earlier body is returned exactly as it was published, and it is not the later body

### Requirement: The timeline is enumerable and never renders as empty
For any note a person may read, the system SHALL list that note's versions in order, each carrying
an identifier that can be passed straight back in to read that version, and a note with one version
SHALL list exactly one entry.

#### Scenario: A note that has never been amended is listed
- **WHEN** the timeline of a note with a single version is listed
- **THEN** exactly one entry is shown, its identifier reads that version back, and the output is
  not an empty timeline

### Requirement: A version identifier is addressable and stable
An identifier obtained at any earlier point SHALL return the same content on every later read, and
SHALL NOT change when newer versions are published to the same note.

#### Scenario: An identifier kept before many later publications
- **WHEN** a version identifier is kept, two hundred further versions are published to that note,
  and the kept identifier is read again
- **THEN** it returns the same content it returned the first time, and it is reported as superseded

### Requirement: Search returns the latest version and identifies it
A search SHALL match only the current version of a note, and every result SHALL name the version it
refers to; for a note that has never been amended that identifier SHALL be the one its sole timeline
entry carries.

#### Scenario: A term that survives only in a superseded version
- **WHEN** a note is amended so that a term appears only in a version that is no longer current, and
  that term is searched for
- **THEN** the note is not presented as though its current version contained the term

### Requirement: Every read states whether it is current or superseded
Reading any version SHALL produce output that states its standing, distinguishable by content rather
than by the identifier the caller passed, and a reader who named no version SHALL still be told
which they are holding.

#### Scenario: An old version is read by someone who did not choose it
- **WHEN** a version that is no longer current is read
- **THEN** the output states that it is superseded, and with the identifier removed it is still
  different from the output of reading the current version

### Requirement: An unqualified request never yields superseded content
A request for a note that names no version SHALL return the current version, and where the current
version cannot be identified it SHALL report that the state could not be determined rather than
returning any older version.

#### Scenario: The current version cannot be identified
- **WHEN** a note is requested without naming a version and the timeline cannot be established
- **THEN** no body is returned, the output says the state could not be determined, and no earlier
  version is served in its place

### Requirement: Nothing expires
No version SHALL be removed by age, by count, by the publication of newer versions, or by any
retention setting, and the system SHALL expose no mechanism that deletes or truncates a note's
history; a deactivated person's notes and their full history SHALL remain readable to those
permitted to read them, marked archived rather than absent.

#### Scenario: An arbitrary amount of time and many publications pass
- **WHEN** hundreds of versions are published to one note and the clock is advanced far past any
  plausible retention window
- **THEN** every version, including the first, is still addressable and returns its original body

#### Scenario: The author of a note leaves the company
- **WHEN** a note's author is deactivated and a colleague reads the note's timeline
- **THEN** every version is still listed and readable, and the output marks the note archived

### Requirement: An undetermined version state is rendered as undetermined
Where it cannot be established which version is current, or whether the version in hand is
superseded, or what a version's content is, the output SHALL say the state could not be determined,
distinguishably from both "this is the current version" and "this is a superseded version", and
SHALL be neither silence nor an empty timeline.

#### Scenario: A version's content cannot be retrieved
- **WHEN** a version exists and is permitted but its content cannot be read
- **THEN** the output says the content could not be read, no body is shown, and the result is
  distinguishable from a successful read of a version whose body is empty

### Requirement: A version that does not exist differs from one that is empty
Requesting an unknown version identifier, or a version of an unknown note, SHALL fail
distinguishably from a successful read of a version whose body is empty, by exit status alone and
without parsing the body.

#### Scenario: An unknown version number is requested
- **WHEN** a version number the note does not have is requested
- **THEN** the request fails with an identifier-specific code, and no blank content is returned
  under a success result

### Requirement: Reading versions starts nothing and reaches nowhere implicitly
Reading a note's timeline or a specific version SHALL NOT start the daemon, and SHALL NOT open a
network connection unless a hub is configured.

#### Scenario: No daemon is running
- **WHEN** a timeline is requested on a machine with a hub configured and no daemon running
- **THEN** the command says the daemon is not running, does not start it, and does not reach the hub

### Requirement: The local half of versioning stands alone
With no hub configured, versioning of local drafts SHALL work fully — successive revisions
addressable and readable as they stood — and any part that genuinely requires the hub SHALL state
precisely what is missing rather than returning an empty timeline, a partial one, or a silent
success.

#### Scenario: A person with no hub revises a draft three times
- **WHEN** three revisions of a local draft are written and the draft's timeline is listed
- **THEN** all three are listed and each is readable as it stood, and asking for a published note's
  timeline instead says that a published timeline lives on the hub and there is none configured

### Requirement: An unreachable hub is not an empty history
A hub that cannot be reached while listing versions or reading one SHALL produce a report distinct
from a note that legitimately has the versions shown and from a refusal.

#### Scenario: The hub is configured and does not answer
- **WHEN** a timeline is requested and the hub cannot be reached
- **THEN** the report says the timeline could not be determined, shows no versions, and carries an
  exit status that no refusal and no successful listing uses

### Requirement: The CLI and the control API report the same version state
For the same note, the current-version identifier, the timeline and the current/superseded labelling
SHALL agree between the two surfaces, and neither SHALL show a version the other does not.

#### Scenario: The same note is read on both surfaces
- **WHEN** a note's timeline is requested through the CLI and through the control API
- **THEN** both name the same current version, list the same version identifiers, give each the same
  standing, and exit with the same status

### Requirement: Visibility precedes version access
A person who may not read a note SHALL NOT read any of its versions, and the existence of a version
SHALL NOT be surfaced to them through the timeline, through search, or through an identifier they
obtained some other way; their result SHALL be indistinguishable from that for a note that does not
exist.

#### Scenario: A note is narrowed away from a colleague who had read it
- **WHEN** a colleague who could read a note is excluded by a later narrowing and then asks for the
  timeline, an earlier version, the current version, or searches for its text
- **THEN** every one of those is refused, no body is returned, and the refusal reads the same as it
  would for a note that does not exist

### Requirement: A note carries inline references to people, groups and other notes
A note body SHALL be able to carry an inline reference to a person, to a group, and to another
note, and each SHALL be recoverable from the published note as a reference whose kind is part of
what is returned, distinguishable from ordinary body text that happens to contain the same
characters.

#### Scenario: All three kinds are retained on publication
- **WHEN** a note is published whose body references a person, a group and another note
- **THEN** all three are recoverable as references, each carrying its kind

#### Scenario: Prose containing the same characters is not a reference
- **WHEN** a body contains the reference characters as ordinary prose, or escaped
- **THEN** no reference is parsed from it, and it renders as the characters the author wrote

#### Scenario: Same name, different kind
- **WHEN** a person and a note have the same display name and both are referenced
- **THEN** the two references are distinguishable in output without the reader inspecting the target

### Requirement: A version's references are that version's
Retrieving an addressable earlier version of a note SHALL yield the references as they stood in
that version, not the current ones.

#### Scenario: A reference added and a reference removed
- **WHEN** version 2 adds one reference and removes another
- **THEN** version 1's reference set contains the removed one and not the added one

### Requirement: Publishing a reference the author cannot see is refused
Publishing a note whose body references a target its author cannot see SHALL be refused at
publication, and the refusal SHALL say why. The note SHALL NOT be published with the reference
stripped, narrowed or downgraded.

#### Scenario: An author references a note they may not read
- **WHEN** the author publishes a body referencing a note they are not permitted to read
- **THEN** the publication is refused with a code naming the reason, and nothing is stored

#### Scenario: The author's access could not be determined
- **WHEN** whether the author may read a referenced target could not be determined
- **THEN** the publication is refused with a different code, and neither answer is reported as the
  other

#### Scenario: An agent publishing on the author's behalf
- **WHEN** the same body is published through a grant carrying the publish scope
- **THEN** it is refused for the same reason, because an agent cannot point at what its person
  cannot read

### Requirement: A reference the reader may not see is not disclosed
Visibility SHALL be settled before any reference is rendered or counted, and a reference to a
target the reader may not read SHALL produce no title, identifier, slug, placeholder, marker,
per-reference error, count difference or gap in the reader's output.

#### Scenario: Two readers retrieve the same note
- **WHEN** a reader permitted to see a referenced note and a reader not permitted retrieve the same
  referencing note
- **THEN** the excluded reader's output is indistinguishable from the output for a note that
  references nothing restricted, including its count and the rendered prose

#### Scenario: Following a reference grants nothing
- **WHEN** a reader lists the references of a note that references a target they may not read
- **THEN** they still cannot read that target, directly or through any grant they hold

### Requirement: What else was written about this is answerable
Given a note, a person or a group, a reader SHALL be able to ask what references it and receive the
referencing notes they are permitted to read.

#### Scenario: The reverse question is answered
- **WHEN** two notes reference a subject and the reader may read one of them
- **THEN** the reader receives that one, and the other is absent rather than redacted, stubbed or
  represented by a gap

#### Scenario: An invisible target and a target that does not exist
- **WHEN** the reader asks about a target that exists but is invisible to them, and about a target
  that does not exist
- **THEN** the two produce the same observable outcome, including exit status

#### Scenario: Notes whose readability could not be determined
- **WHEN** the corpus contains notes whose readability for this reader could not be determined
- **THEN** they are reported as unexamined and the answer is reported as partial, rather than
  counted as matches or silently dropped

### Requirement: A reference that no longer resolves is shown, never dropped
A reference whose target the reader is permitted to see but which no longer resolves SHALL be
rendered as an unresolved reference: present, marked, and distinguishable from a resolved reference
and from plain body text. It SHALL NOT be interchanged with a reference the reader may not see.

#### Scenario: A dangling reference among others
- **WHEN** a note references a target that is gone and another that resolves
- **THEN** the dangling one is shown and marked, and the other still renders

#### Scenario: A note by a departed colleague
- **WHEN** a reference points at a note whose author has been deactivated and whose notes are
  archived
- **THEN** the reference resolves as a normal reference, because archiving is not unresolution

### Requirement: A reference that could not be resolved is undetermined
Where whether a reference resolves could not be determined, it SHALL be rendered as undetermined —
distinguishable from a resolved reference, from an unresolved one, and from silence — and SHALL NOT
be rendered as "does not exist" or omitted. Undetermined and determined-to-be-nothing SHALL NOT
share an exit code.

#### Scenario: The hub could not be reached
- **WHEN** a reference listing is asked for and the hub cannot be reached
- **THEN** the answer is undetermined, with its own exit status, and no reference is reported as
  absent

#### Scenario: A group that was dissolved
- **WHEN** a referenced note is narrowed to a group whose membership can no longer be resolved
- **THEN** the reference is undetermined rather than refused or missing, and its target is not named

### Requirement: Reference commands are implicit about nothing
No reference-reading or reference-listing command SHALL start the daemon, and with no hub
configured no reference operation SHALL open a network connection. Where the local half of the
capability is complete and the hub-dependent half is not, the answer SHALL be reported as partial
and SHALL be distinguishable from a complete one by exit status alone.

#### Scenario: No daemon running
- **WHEN** a reference command that needs the hub is run with no daemon running
- **THEN** it says so and does not start one

#### Scenario: A local draft with no hub configured
- **WHEN** a local draft's references are scanned with no hub configured
- **THEN** every reference is extracted, the hub is named as the piece missing for resolving them,
  and the exit status differs from that of a draft whose answer is complete

