# tickets Specification

## Purpose

**A ticket is a thing you have to act on. It is not a message** (PRD §3.2). Five emails, a chat
thread and a follow-up ping about one broken login are **one ticket** — and making them one is this
capability.

**Merging crosses channels**, because a problem does not respect the boundary between a mailbox and a
chat client. The person experiencing it did not send five things; they had one problem and reached for
whatever was to hand. The inbox should show them what they actually have to deal with.

**Every merge is reversible and shows its working.** What was merged, from where, and why — and
unmerging restores exactly what was there before, byte for byte, not an approximation reconstructed
from what the product happens to remember. A merge that could not be undone would make the inbox a
place where information goes to be lost, and the store is the sole home of unpublished data
(PRD §3.14).

The reversal is also **legible**: a ticket that was merged and restored says so, and says when, and
that reads differently from a ticket that was never merged at all. Two different histories must not
render as one.

Three constraints follow, and each is a refusal:

- **Merging is not a back door.** Acknowledgements and small talk are not tickets, and combining them
  does not make them one. A merge whose title is empty, or whose summary is only the source titles run
  together, is refused — otherwise the rule that small talk is not a ticket could be evaded by merging
  small talk.
- **A merged ticket does not claim a channel it does not have.** Where sources came from different
  places, the channel is reported as *could not be determined*, with the reason — never as one of them
  chosen arbitrarily (PRD §4.3).
- **It is atomic against interruption.** A process killed mid-merge leaves a store that still opens,
  and the next reader completes or discards the operation rather than finding a half-merged ticket.
  Nothing here reaches the network, and tickets never leave the machine (PRD §2.3).
## Requirements
### Requirement: Scattered traffic about one problem becomes one ticket
The product SHALL merge two or more existing tickets into a single ticket by an explicit act of the
person, and SHALL thereafter list that one ticket where it previously listed the merged ones.

#### Scenario: Several tickets become one
- **WHEN** a person merges two or more tickets that are in the inbox
- **THEN** a listing of the inbox shows the merged ticket, and none of the merged-away tickets is
  listed as a separate open item

#### Scenario: The sources came from different channels
- **WHEN** the tickets being merged include one that originated in email and one that originated in
  chat
- **THEN** the merge succeeds on exactly the same terms as a merge of two tickets from one channel,
  and nothing in its output refuses, warns or degrades on the grounds that the sources differ

#### Scenario: The merged ticket is written, not assembled
- **WHEN** a merge is made
- **THEN** the merged ticket carries a non-empty written title and a non-empty written summary
- **AND** a merge whose summary is the source titles run together, in any order, is refused

### Requirement: A merge shows its working
The product SHALL record, for every input of a merge, what it was, which channel and which source it
came from, and why it was merged, and SHALL make all four readable from the merged ticket alone.

#### Scenario: Inspecting a merged ticket
- **WHEN** a person inspects a merged ticket
- **THEN** for every thing folded in, what it was, which channel and source it came from, and why it
  was merged are all shown

#### Scenario: The origin is on the ticket, not in the channel
- **WHEN** a person inspects a merged ticket without consulting any channel
- **THEN** the channel and source identifier of every folded-in piece are still readable

#### Scenario: A merge record that does not carry all of it
- **WHEN** a merge record is read whose input does not record one of those four things
- **THEN** reading it fails and names what is missing, rather than rendering that field as blank

### Requirement: Every merge is reversible exactly
The product SHALL restore, on an unmerge, each source ticket with the content it had immediately
before the merge, and SHALL make that restoration checkable against a snapshot taken beforehand.

#### Scenario: Taking a merge apart
- **WHEN** a person unmerges a merged ticket
- **THEN** every source ticket is back in the inbox with content equal to a snapshot taken before
  the merge, and the merged ticket is gone

#### Scenario: A field that could not be determined comes back that way
- **WHEN** a source ticket had a field recorded as undetermined and another recorded as absent, and
  the merge is undone
- **THEN** each comes back in the state it was in, and the two do not come back as each other

#### Scenario: A merge made long ago
- **WHEN** a person unmerges a ticket that was merged years earlier
- **THEN** it comes apart exactly, because no merge record is aged out or expired

### Requirement: A ticket that was merged and unmerged is not a ticket that was never merged
The product SHALL make a restored ticket distinguishable in output from one that was never merged,
and SHALL NOT render the two situations identically.

#### Scenario: Inspecting a restored ticket
- **WHEN** a person inspects a ticket that was merged and then unmerged, and separately one that
  never was
- **THEN** the two outputs differ, and the restored one reveals that a merge happened and was undone

### Requirement: Merging and unmerging are atomic
The product SHALL leave the inbox, after an interrupted merge or unmerge, in exactly one of two
states — every input still separate, or the one merged ticket — and SHALL leave the store readable.

#### Scenario: The machine dies partway through a merge
- **WHEN** a process performing a merge is killed before it completes
- **THEN** the store still opens, and the inbox holds either all of the inputs or the one merged
  ticket, never a mixture with duplicated or missing tickets

#### Scenario: A merge that was committed and not finished
- **WHEN** a store is opened whose last process died after committing a merge and before applying
  all of it
- **THEN** the merge is completed before anything is read, so no reader observes the half state

### Requirement: A merge that cannot happen fails by exit status and changes nothing
The product SHALL refuse a merge naming fewer than two tickets or naming a ticket that does not
exist, and SHALL refuse an unmerge of a ticket that was never merged, in each case leaving the inbox
unchanged.

#### Scenario: Naming one ticket, or a ticket that is not there
- **WHEN** a person attempts a merge naming only one ticket, or naming a ticket the inbox does not
  hold
- **THEN** the command exits non-zero, distinguishably from success by exit status alone, no merged
  ticket is produced, and no partial merge is left behind

#### Scenario: Unmerging something that was never merged
- **WHEN** a person unmerges a ticket that is not a merged ticket
- **THEN** the command exits non-zero, distinguishably from a successful unmerge by exit status
  alone, and the ticket is unaltered

### Requirement: Nothing that was never a ticket becomes mergeable
The product SHALL accept as a merge input only a ticket already in the inbox, and SHALL NOT provide
any path by which an acknowledgement or a piece of small talk becomes a ticket through merging.

#### Scenario: Naming a piece of traffic that is not a ticket
- **WHEN** a person names, as a merge input, traffic that was correctly not turned into a ticket
- **THEN** the merge is refused because there is no such ticket, and nothing is created

#### Scenario: Titling a merged ticket with an acknowledgement
- **WHEN** a merge would produce a ticket whose title is the verbatim body of an acknowledgement
- **THEN** it is refused, and the refusal says there is no priority at which to keep it instead

### Requirement: An unresolved origin and an unrecorded reason render as undetermined
The product SHALL render a merge field that could not be determined distinguishably from a real
value and from a recorded absence, and SHALL NOT render it as blank.

#### Scenario: Three states of one field
- **WHEN** a merged ticket is inspected whose inputs include one with a known channel, one whose
  channel could not be resolved, and one recorded as having none
- **THEN** the three render distinctly from one another, and none of them renders as nothing at all

#### Scenario: A merge whose reason nobody gave
- **WHEN** a person merges tickets without saying why for one of them
- **THEN** that reason is recorded and rendered as undetermined, never as an empty value

#### Scenario: The two failures never share an exit code
- **WHEN** a merge command determines there is nothing to merge, and separately cannot determine
  what is there
- **THEN** the two exit with different codes

### Requirement: Merging is local, explicit and self-sufficient
The product SHALL NOT start the daemon or contact a hub in the course of any merge operation, SHALL
perform merging, unmerging and inspection fully with no hub configured, and SHALL refuse rather than
proceed by another path when the control API cannot be opened.

#### Scenario: No daemon is running
- **WHEN** a person merges or unmerges with no daemon running
- **THEN** the command says the daemon is not running, does not start it, and does the local work

#### Scenario: Whether a daemon is running could not be established
- **WHEN** a person merges or unmerges and whether a daemon holds the store could not be determined
- **THEN** the command reports the third answer, does not change anything, and exits on a code no
  determined answer uses — an unestablished state is never treated as an absent daemon

#### Scenario: The surface agrees with the daemon's own report
- **WHEN** a daemon is running and a person runs a merge surface
- **THEN** nothing it prints claims the daemon is absent, and it does not justify what it shows by
  an absence the daemon's own status contradicts

#### Scenario: No hub is configured
- **WHEN** a person merges, unmerges and inspects a merge's working with no hub configured
- **THEN** all of it works fully, and nothing is sent anywhere

#### Scenario: Owner-only socket permissions cannot be confirmed
- **WHEN** a control socket is present and the product cannot confirm it is owner-only
- **THEN** merge and unmerge refuse and report an unavailable control API, worded and coded
  distinguishably from "there are no tickets to merge", and nothing is changed

