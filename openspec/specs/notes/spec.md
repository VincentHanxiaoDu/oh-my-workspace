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

Before any of that, a person has to be able to write the thing down. Someone has just worked
something out — with their AI or on their own — and it is worth other people finding (PRD §3.3).
Drafts collect in the **outbox**, which lives on their machine and only there: unpublished data has
exactly one home, the device's local store (§2.3, §3.14). With no store, drafting does not quietly
succeed somewhere convenient; it says the store is missing and writes nothing. Nothing leaves the
machine because a draft was written.

How drafts leave the outbox is the person's decision, not the product's, and the product does not
guess. `manual` is the default and it is a real value, not a blank: drafts rest until the person acts.
`review` has an AI check each draft against rules the person wrote **in their own words** — kept
verbatim, never tidied or normalised away — and it is purely personal (§5.2): it runs on the client
with the person's own model and key, so it works with no hub at all, and the key is never printed by
anything here (§3.13). `auto` publishes directly. Choosing a mode acts on drafts from then on and
never reaches back to ones already resting.

The sharpest guarantees are negative. `review` with **no model configured** must say what is
missing. It must not quietly behave like `manual`, leaving a draft to look as though the person
simply had not got to it; and it must not quietly behave like `auto`, publishing something nobody
checked. A silent fallback in either direction is the defect this capability exists to prevent. In
the same spirit, a review that could not be completed — an unreachable model, an unusable answer — is
**undetermined**, never a pass and never a refusal (§4.3), and nothing is published on the strength
of it. An empty outbox and an outbox that could not be read are different answers with different exit
codes.

Nothing here starts a daemon on the person's behalf and nothing opens a connection without a hub
(§4.2). With no hub configured, drafting and `manual` mode work fully, with no degradation and no
nagging about a hub that was never asked for (§4.4); where a hub genuinely is required, the command
names precisely what is missing and refuses rather than half-working. And nothing in the outbox
expires — a draft left untouched is still there, for as long as the person leaves it (§5.4).

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

### Requirement: A draft lives in the local store and nowhere else
Creating a draft SHALL write it into this device's local store, and SHALL refuse — naming the
missing store and exiting non-zero — when no store has been created.

#### Scenario: Drafting with a store
- **WHEN** a person creates a draft on a device that has a store
- **THEN** the draft is written inside that store, is listed in the outbox, and is reported as a
  draft

#### Scenario: Drafting with no store
- **WHEN** a person creates a draft on a device where no store has ever been created
- **THEN** the command exits non-zero, names the missing store, and the draft's text exists nowhere
  on the machine — not in a temporary directory and not in the person's home directory

#### Scenario: A draft outlives the process that wrote it
- **WHEN** the process that wrote a draft has exited and another process reads the outbox
- **THEN** the draft and its state are still there

### Requirement: An empty outbox and an unreadable one are different answers
Listing the outbox SHALL distinguish "there are no drafts" from "the outbox could not be read", in
both its output and its exit code.

#### Scenario: The outbox is empty
- **WHEN** a person lists an outbox holding no drafts
- **THEN** the command reports zero drafts as a determined answer and exits zero

#### Scenario: The outbox cannot be read
- **WHEN** a person lists an outbox that cannot be read
- **THEN** the command says nothing has been established about their drafts, does not report zero
  drafts, and exits with a code that success does not use

### Requirement: The publication mode is the person's choice and is always a real value
The client SHALL report an effective publication mode of `manual` when none has ever been set, SHALL
accept exactly `manual`, `review` and `auto`, and SHALL report undetermined when a recorded choice
cannot be read.

#### Scenario: No mode has ever been set
- **WHEN** a person asks which publication mode is in effect on a device where none was ever set
- **THEN** the client answers `manual`, states it as a real value rather than as blank or absent,
  and says that this is the default rather than a choice they made

#### Scenario: A mode outside the vocabulary
- **WHEN** a person sets a mode name that is not one of the three
- **THEN** the command exits non-zero, and reading the mode back shows the previously effective mode
  unchanged

#### Scenario: The recorded choice cannot be read
- **WHEN** the record of the person's chosen mode exists and cannot be read
- **THEN** the client reports undetermined, in wording distinct from both a mode it could report and
  from the default, and exits with the undetermined code

#### Scenario: Changing the mode does not act on drafts already written
- **WHEN** a person switches from `manual` to `auto`
- **THEN** the drafts already resting in the outbox are still there, in the state they were in

### Requirement: Review checks the person's own words, on their own machine
`review` mode SHALL check a draft against rules recorded in the person's own wording, read back
byte for byte, using the person's own model, and SHALL perform that check without a hub.

#### Scenario: Rules are read back as they were written
- **WHEN** a person records rules containing leading spaces, blank lines, tabs, mixed case and
  trailing whitespace, and reads them back
- **THEN** the text read back is byte-for-byte the text recorded

#### Scenario: Review with no hub configured
- **WHEN** a person with a configured model reviews a draft on a device with no hub configured
- **THEN** the check runs and reports its verdict

### Requirement: Review with no model says what is missing and publishes nothing
Attempting to draft or publish under `review` with no model configured SHALL name the missing model
configuration, SHALL exit non-zero, SHALL leave the draft in the outbox, and SHALL report the draft
in a state distinguishable from a draft its author has simply not published.

#### Scenario: The person picks review and has no model
- **WHEN** a person under `review` writes a draft or attempts to publish one, with no model
  configured
- **THEN** the output names the missing model, the command exits non-zero, the draft is still in the
  outbox, and neither the command's output nor the draft's reported state matches what `manual`
  produces for the same draft

#### Scenario: Nothing is published while the check cannot run
- **WHEN** a person under `review` with no model attempts to publish, on a device with a hub
  configured
- **THEN** no connection is opened, the draft is not published, and its state does not say it has
  been handed onward

### Requirement: A review that could not be completed is not a pass
Where the configured model is unreachable, errors, or returns an answer that is not a verdict, the
draft SHALL NOT be published and the outcome SHALL be reported as undetermined, distinguishable in
output and in exit code from both a pass and a refusal.

#### Scenario: The model cannot be reached
- **WHEN** a review is attempted and the model errors or cannot be reached
- **THEN** the outcome is undetermined, the draft stays in the outbox, and the exit code is the
  undetermined one, which neither a pass nor a refusal uses

#### Scenario: The model answers something that is not a verdict
- **WHEN** the model returns nothing, whitespace, or prose with no conclusion
- **THEN** the outcome is undetermined rather than a pass

#### Scenario: A refused draft
- **WHEN** a review completes and refuses a draft
- **THEN** the draft is still in the outbox, is reported as refused by review with the model's
  reason, and reads differently from a draft that has never been reviewed

### Requirement: The person's key is never printed
No command in this capability SHALL write the person's model key to any output stream.

#### Scenario: A configured key across every command
- **WHEN** a model is configured with a key and every subcommand of this capability is run, under a
  passing, a refusing and an unreachable model
- **THEN** the key's value appears on neither stdout nor stderr of any of them

### Requirement: Nothing implicit, and no network without a hub
No command in this capability SHALL start the daemon, and with no hub configured none SHALL open a
network connection; with a hub configured, purely local work SHALL still open none.

#### Scenario: The daemon is not running
- **WHEN** any command in this capability is run with the daemon stopped
- **THEN** it says the daemon is not running, and the daemon is still not running afterwards

#### Scenario: Local work beside a configured hub
- **WHEN** drafting in `manual` mode, listing the outbox, setting the mode and reading it back are
  run on a device whose configured hub address is a live listener
- **THEN** that listener receives no connection

#### Scenario: The local half with no hub at all
- **WHEN** a person with no hub configured drafts, lists, and reads their drafts in `manual` mode
- **THEN** every command succeeds, and none of them warns about a missing hub

### Requirement: Where a hub or an owner-only socket is required, the command says so
Where the transfer of a draft genuinely requires a hub, or where the daemon reports that its
control API is not open on an owner-only socket — or that this could not be confirmed — the command
SHALL say precisely what is missing and exit non-zero rather than half-working.

#### Scenario: Auto mode with no hub
- **WHEN** a person under `auto` writes a draft on a device with no hub configured
- **THEN** the command names the missing hub, exits non-zero, and the draft rests in the outbox

#### Scenario: A control API that is not open, or cannot be confirmed
- **WHEN** any command in this capability is run while a daemon is running and reports that its
  control API is not open, or that whether it is open could not be established
- **THEN** the command says which of the two it is and exits non-zero rather than proceeding, and
  the two do not share an exit code

### Requirement: Nothing in the outbox expires
No draft SHALL be removed from the outbox by age.

#### Scenario: An old draft
- **WHEN** a draft written years ago and never touched is listed, repeatedly
- **THEN** it is still listed every time

