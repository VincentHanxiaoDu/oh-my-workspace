# publish Specification

## Purpose

Publication is a **transfer**, and a transfer can fail. Everything here follows from refusing to be
vague about that.

**A note that did not arrive is still in the outbox — never both, never neither** (PRD §3.11). Those
are the two ways this can go wrong, and they are opposite: a note in both places will be published
twice by the next retry, and a note in neither has been silently destroyed. The client is killed
mid-transfer in its own tests precisely because a guarantee about interruption cannot be argued, only
survived.

**Interrupted means not published.** A person retries and does not get two copies.

**The client says which state a note is in** — `drafted`, `in flight`, `published`, or `refused` — and
these are four answers, not two with shading:

- `in flight` is the honest answer when an attempt left this machine and its outcome is unknown. It is
  reported as undetermined rather than guessed in either direction (PRD §4.3).
- `refused` carries the hub's reason, because a refusal a person cannot act on is barely better than
  silence.
- **A hub that cannot be reached is not a rejected note.** Those are different facts about different
  things — one is about the network, one is about the content — and the product reports them
  differently.

**Identifiers are unguessable, and this is where they are minted.** A note's id is random rather than
sequential, so publishing a note somebody cannot read does not shift the ids of the notes they can —
and the id space cannot be walked to count what is hidden from you. Minting refuses rather than
overwrites if it cannot find an unused id: a refused publication can be retried, whereas storing under
a taken id would destroy a note that already exists.

Nothing here reaches the network without a hub configured, and no part of it starts the daemon on a
person's behalf (PRD §4.2).
## Requirements
### Requirement: Exactly one container holds a note, always
A note SHALL be either in the person's outbox or on the hub, and the client SHALL answer which one
from a single durable record. After any completed publish attempt — accepted, refused, or a hub that
could not be reached — the note SHALL be in exactly one of the two, and never in both and never in
neither.

#### Scenario: The hub accepts the note
- **WHEN** a person publishes a draft and the hub accepts it
- **THEN** the note is retrievable from the hub, an outbox listing no longer includes it, and the
  client reports it as published

#### Scenario: The hub refuses the note
- **WHEN** a person publishes a draft and the hub refuses it
- **THEN** the hub holds no note for that draft, the draft is still listed in the outbox with its
  content unchanged, and the client reports it as refused

#### Scenario: The hub cannot be reached
- **WHEN** a person publishes a draft and no connection to the hub can be established
- **THEN** nothing was sent, the draft is still listed in the outbox with its content unchanged, and
  the client reports it as neither published nor refused

#### Scenario: An unsuccessful attempt leaves the note re-publishable
- **WHEN** a publish attempt has completed without the hub accepting the note
- **THEN** publishing the same draft again afterwards succeeds and puts it on the hub

### Requirement: Interrupted means not published, and a retry makes one copy
Killing the client between the request being sent and the outcome being recorded SHALL leave the
note in the outbox, in a state that is not `published`. Re-publishing that note SHALL result in one
note on the hub, not two, whether or not the hub in fact received the interrupted attempt.

#### Scenario: Killed after the hub stored the note
- **WHEN** the client is killed after the hub has stored the note and before the answer is read
- **THEN** the note is still in the outbox with its content intact, and the client does not report it
  as published

#### Scenario: Retrying an interrupted attempt the hub received
- **WHEN** that interrupted publish is run again
- **THEN** the hub holds exactly one note for that draft, and the client reports the note as published

#### Scenario: Retrying an interrupted attempt the hub never received
- **WHEN** a publish is interrupted before the hub stored anything, and is then run again
- **THEN** the hub holds exactly one note for that draft

### Requirement: The client says which of four states a note is in
For any note the client knows about, it SHALL report exactly one of `drafted`, `in flight`,
`published` or `refused`. The four SHALL be distinguishable from one another by inspecting the
output, and not only by exit code.

#### Scenario: Four notes in four states
- **WHEN** a person asks for the state of one note in each of the four states
- **THEN** the four outputs carry four different machine-checkable state lines

#### Scenario: A state that could not be read
- **WHEN** the record of what happened to a note exists and cannot be read
- **THEN** the state is reported as undetermined, is not reported as `drafted`, and no publication is
  attempted for that note

### Requirement: A refusal says why, and the reason survives
A note the hub refused SHALL be reported as `refused` and SHALL carry the hub's stated reason. A
refusal with no reason attached SHALL be reported as a defect rather than as a blank.

#### Scenario: A refusal is listed later
- **WHEN** a person lists their notes after the hub refused one
- **THEN** the refused note is listed as refused, with the hub's reason and a stable code

### Requirement: A hub that cannot be reached is not a rejected note
A refusal and an unreachable hub SHALL be reported differently in a machine-checkable way, and SHALL
NOT share an exit code. An unreachable hub SHALL never put a note into the `refused` state, and a
refusal SHALL never render as an unreachable hub.

#### Scenario: Both driven against the same draft
- **WHEN** the same draft meets a hub that refuses it and a hub whose address does not answer
- **THEN** the two answers carry different codes, leave different states, and exit with different
  codes

#### Scenario: After an unreachable-hub attempt
- **WHEN** an attempt has failed because the hub could not be reached
- **THEN** the note's state is neither `published` nor `refused`, and it is still in the outbox

### Requirement: With no hub configured, nothing is opened
With no hub configured, publishing SHALL perform zero outbound connection attempts, SHALL name that
no hub is configured, and SHALL leave the note drafted, unchanged and re-publishable. The answer
SHALL be distinguishable from both a hub refusal and an unreachable hub.

#### Scenario: Zero connection attempts
- **WHEN** a person publishes on a machine with no hub configured
- **THEN** no outbound connection is attempted at all — not one that fails fast, none

#### Scenario: Nothing half-happens
- **WHEN** that command has exited
- **THEN** the draft's files are byte-for-byte what they were, and no publication record exists for it

### Requirement: An undetermined outcome is its own answer and is resolvable
A note whose publication outcome is genuinely unknown to the client SHALL be reported as
undetermined — distinguishable from `published`, from `refused` and from silence — and SHALL NOT be
rendered as "not published" when the client does not know. Publishing that note again SHALL either
establish it as published without a second copy, or leave it in the outbox.

#### Scenario: The connection dropped after the request was sent
- **WHEN** the request was sent and no answer was read
- **THEN** the note is reported as `in flight`, its published answer is undetermined, and it is still
  in the outbox

#### Scenario: Resolving it
- **WHEN** that note is published again against a reachable hub
- **THEN** it becomes published and the hub holds exactly one note for it

### Requirement: Publishing requires the publish scope
Publishing SHALL require the `publish` scope. A caller holding only `read`, or only `write`, or both
of those and not `publish`, SHALL be refused; the refusal SHALL name the missing scope, SHALL be
distinguishable from a successful publish by the reported state, SHALL be distinguishable from an
unreachable hub by the same machine-checkable means, and SHALL leave the note in the outbox.

#### Scenario: A token that can read but not publish
- **WHEN** a caller holding only `read` publishes a note
- **THEN** the attempt is refused with the missing-scope code, the hub stores nothing, and the note is
  still in the outbox

#### Scenario: The refusal is not an unreachable hub
- **WHEN** a scope refusal and an unreachable hub are compared
- **THEN** they carry different codes and leave different states

#### Scenario: Nothing is granted by default
- **WHEN** no scopes are configured on the machine
- **THEN** publishing is refused rather than proceeding as though `publish` had been granted

### Requirement: Publishing starts nothing on the person's behalf
Publishing SHALL report whether the daemon is running, in three values, and SHALL start no process.

#### Scenario: The daemon is not running
- **WHEN** a person publishes with no daemon running
- **THEN** the command says the daemon is not running, says it started nothing, and no new process
  exists in its process group after it exits

### Requirement: The CLI and the control API report the same state
The state a note is in SHALL be reported identically through the CLI and through the local control
surface, for every one of the four states.

#### Scenario: All four states over a real connection
- **WHEN** the state of each of four notes is read through the CLI and asked of the control endpoint
- **THEN** each note yields the same one of the four states on both surfaces

### Requirement: A published note's identifier is unguessable and stable
An identifier minted at publication SHALL NOT be derivable from any other identifier, nor from the
order or the count of publications. Publishing a note a reader may not read SHALL change no
identifier that reader can observe, and SHALL NOT place the unreadable note within reach of one they
can. An identifier SHALL keep resolving to the same note for the life of the note, SHALL be usable as
a single path segment, and SHALL be safe to print in a terminal. A reader holding an identifier they
may not read SHALL still receive a refusal distinguishable from "no such note".

#### Scenario: A series of publications
- **WHEN** many notes are published
- **THEN** no identifier can be computed from another by a small step, sorting the identifiers does
  not reproduce the publication order, and the gaps between them are not constant

#### Scenario: A note the reader may not read is published
- **WHEN** one additional unreadable note is added to a corpus
- **THEN** every identifier the reader can observe is unchanged, and the unreadable note's identifier
  is not one step from any of them

#### Scenario: An identifier captured and used later
- **WHEN** an identifier is captured and the note is then amended, narrowed, widened, and surrounded
  by later publications
- **THEN** the identifier still resolves to the same note and to every point on its timeline

#### Scenario: Holding an identifier you may not read
- **WHEN** a reader reads a note that exists and is not visible to them, and a note that does not
  exist
- **THEN** the two answers carry different codes and different messages

#### Scenario: No randomness on the machine
- **WHEN** an identifier cannot be minted
- **THEN** the publication is refused and nothing is stored — there is no fallback to a counter, a
  clock, or a weaker source

