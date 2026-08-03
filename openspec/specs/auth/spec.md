# auth Specification

## Purpose

A person has a hub at work. They want to sign in once, on purpose, and then have their client act as
them against it — and not only their client: their own AI reading their tickets through the agent
API, a script on the build box, a tool they handed a token to. Everything that acts in their name
acts under something they said yes to out loud. This capability exists so that "signed in" is a
thing a person did, and so that they can later look at everything acting as them and end any of it
(PRD §3.10, §4.5).

A token carries a **scope, not an identity**. The vocabulary is exactly three names — `read`,
`write`, `publish` — and it is one vocabulary across the CLI, the agent API and the hub. Everyday
use is `read` alone and needs no thought. `publish` is its own grant, because PRD §3.10 requires
that a token which can publish was asked for on purpose: reading somebody's notes and speaking to
the company in their name are not the same favour. There is no fourth name, no synonym and no
surface-local spelling; a listing of the vocabulary returns those three and nothing else.

**运维不算 scope.** The hub operator can read everything published to the hub, including notes
narrowed to a group or to yourself — that is a deployment fact, stated at the point where a person
chooses visibility (PRD §2.4: restriction controls which colleagues see a note, it is not a wall
against whoever operates the server). It is deliberately *not* expressed through the scope system.
No scope name grants reading other people's restricted notes, and asking for one is refused as an
unknown scope — a different refusal from asking for a known scope that is too wide. The genuinely
private note is the one never published, and no sign-in, token or agent authorisation can be made
to say otherwise.

**Nothing signs in silently.** A first sign-in happens only through a sign-in command the person
runs, by device code: the CLI prints a code, the person enters it in a browser, and the flow needs
no graphical session and no callback listener — it completes over SSH on a headless machine. Issuing
the code creates no token; between the code being printed and the browser step being finished, no
credential exists and "am I signed in" still answers no. A code the person abandons expires and can
never later become a token, and a code already redeemed cannot be replayed into a second token.

**Nothing is implicitly wider than what was asked for** (PRD §4.5). A request for more than the
requesting person themselves holds is refused *at the moment it is asked*. It does not succeed with
a quietly narrowed scope, and no token exists afterwards to be discovered at the edge later. The
scope reported for a token is the scope the token actually has. Delegation cannot widen, and a
delegated token does not outlive the revocation of the grant it came from.

The negatives are the guarantee. An `auth` command never starts the daemon on the person's behalf,
and with no hub configured it opens no network connection at all — it says the hub configuration is
what is missing, which is a different fact from "the hub is configured but unreachable" and a
different fact again from "signed out" (PRD §4.2, §4.4, §3.11). A session that has never been used
is listed and shown as never used, never omitted and never rendered as though it had been used; an
unknown last-use renders as undetermined, distinct from both. And when the client cannot determine
sign-in state at all, it says so — undetermined is never collapsed into a "no" and never rendered as
silence (PRD §4.3).
## Requirements
### Requirement: A first sign-in is an act the person performs
The product SHALL create a credential only through a sign-in command a person runs. No other
command, no agent-API call and no scheduled work SHALL produce a first sign-in, and no command SHALL
initiate a sign-in flow on a person's behalf.

#### Scenario: A command needs hub authority and nobody is signed in
- **WHEN** a hub is configured, no credential exists, and a person runs a command that would need
  hub authority
- **THEN** the command reports that the person is not signed in, names that as the reason, exits
  without opening a sign-in flow, and no credential exists afterwards

#### Scenario: Every other surface is exercised with no credential present
- **WHEN** every auth surface other than the sign-in command is run against a machine holding no
  credential
- **THEN** no credential exists afterwards, no sign-in was started at the hub, and no token was
  minted for anybody

#### Scenario: A second surface starts writing credentials
- **WHEN** any file outside the sign-in command's own path calls the function that writes a
  credential
- **THEN** the build fails and names the file and line, and the check itself fails if it finds no
  call to that function anywhere

### Requirement: Sign-in is by device code and needs no browser on the client
The product SHALL sign a person in by printing a code they enter in a browser elsewhere. The client
SHALL NOT open a browser, bind a port, or require a graphical session.

#### Scenario: A person signs in from a headless machine
- **WHEN** a person runs the sign-in command where no browser can be opened and no inbound port can
  be bound
- **THEN** the command prints a code and a verification address, waits, and completes only after the
  person has completed the browser step elsewhere

#### Scenario: A device code is issued and nobody completes it
- **WHEN** a device code has been printed and the browser step has not been completed
- **THEN** no credential exists on the machine, no token exists at the hub, the command has not
  succeeded, and the state is reported as a sign-in not yet completed rather than as a refusal

#### Scenario: A device code is abandoned until it expires
- **WHEN** a printed device code is never completed and its life runs out
- **THEN** the command reports the expiry, the code can never afterwards become a token even if it
  is approved late, the machine is in exactly its pre-sign-in state, and the expiry is reported
  differently from a refusal and from a hub that could not be reached

#### Scenario: A redeemed device code is presented a second time
- **WHEN** a device code that has already produced a token is presented again
- **THEN** the replay is refused as a replay, the number of sessions is unchanged, and the refusal
  is reported differently from a hub that could not be reached

### Requirement: A token carries a scope, and the vocabulary is exactly three names
The product SHALL accept exactly the scope names `read`, `write` and `publish`, with the same
meaning on the CLI, the agent API and the hub. It SHALL refuse any other name as an unknown scope.

#### Scenario: The vocabulary is listed
- **WHEN** a person asks what the scopes are
- **THEN** exactly three names are returned — `read`, `write`, `publish` — and no fourth

#### Scenario: A scope outside the vocabulary is requested
- **WHEN** a grant is requested naming any name that is not one of the three
- **THEN** the request is refused as an UNKNOWN scope, no device code is printed, no token exists
  afterwards, and the refusal is reported differently from a refusal for a scope that is known but
  wider than the requester holds

#### Scenario: A person asks about the hub operator's access
- **WHEN** a person reads the scope vocabulary
- **THEN** the hub operator's ability to read everything published to it is stated there as a
  deployment fact, and it is stated that no sign-in, token or delegation can produce it

#### Scenario: Everyday reading is attempted with `read` alone
- **WHEN** a token holding only `read` is used for the ordinary reading path
- **THEN** it succeeds with no further grant requested or prompted

#### Scenario: Publishing is attempted without the publish scope
- **WHEN** a token holding `read`, or `read` and `write`, is used to publish
- **THEN** the publish is refused for want of the publish scope, and the refusal is distinguishable
  by exit status alone from a successful publish and from a hub that could not be reached

#### Scenario: A token is used repeatedly without ever having asked for publish
- **WHEN** a token created without asking for `publish` is used, and used again after a refusal
- **THEN** it never acquires `publish`: there is no upgrade on use, no widening on retry, and no
  implicit grant alongside `read` or `write`

### Requirement: Nothing is implicitly wider than what was asked for
The product SHALL refuse a grant request that would let the holder do more than the requesting
person can themselves, at the moment it is requested, and SHALL NOT issue a narrowed grant instead.

#### Scenario: A person requests a grant wider than they hold
- **WHEN** a person holding only `read` requests a token carrying `publish`
- **THEN** the request fails, no token exists afterwards, and no narrower token was issued in its
  place

#### Scenario: A token's scope is reported back
- **WHEN** a token is issued for a request that was permitted
- **THEN** the scope reported for that token equals the scope requested exactly, and equals what the
  token actually permits when it is used

#### Scenario: A token is minted for delegation
- **WHEN** a person's AI or a script is given a token delegated from one the person holds
- **THEN** the delegated token carries no capability absent from its parent, and a request for one
  is refused rather than narrowed

#### Scenario: A parent grant is revoked
- **WHEN** a grant from which another token was delegated is revoked
- **THEN** the delegated token stops working too, and its next use is refused as revoked

### Requirement: A token is revocable and its use is visible
The product SHALL let a person list every session signed in as them and end any one of them without
ending the others. A revoked token's next use SHALL be refused.

#### Scenario: A person lists what is signed in as them
- **WHEN** a person lists their sessions
- **THEN** every session is listed, including one that has never been used, and each entry shows at
  minimum its scope and its last-use state

#### Scenario: A session has never been used
- **WHEN** a session that nothing has ever presented appears in the listing
- **THEN** it is shown as never used — neither omitted, nor shown as if it had been used, nor shown
  as a last use that could not be determined

#### Scenario: A last use cannot be established
- **WHEN** the hub cannot say when a session was last used
- **THEN** the entry renders that as undetermined, distinguishably from "never used" and from a real
  timestamp

#### Scenario: One of several sessions is ended
- **WHEN** a person ends one of two sessions signed in as them
- **THEN** that token's next use is refused by the hub, the refusal is distinguishable from a hub
  that could not be reached, and the other session still authenticates

#### Scenario: A revoked session is listed again
- **WHEN** a person lists their sessions after ending one
- **THEN** the ended session is either shown as revoked or absent, and is never shown as active

#### Scenario: A token that never existed is presented
- **WHEN** material that was never issued is presented to the hub
- **THEN** the refusal says the token never existed, and that answer differs from the refusal for a
  revoked token, from the refusal for an expired token, and from a hub that could not be reached

#### Scenario: An entry has no recorded scope
- **WHEN** the listing contains an entry with no recorded scope, an entry with an empty scope list,
  and an entry with a real scope
- **THEN** the three render as three distinguishable things, none of them as empty output

### Requirement: Undetermined is never a "no" and never silence
The product SHALL report a sign-in state it could not determine as undetermined, distinguishably
from "signed in", from "not signed in", and from empty output, and SHALL exit with the code reserved
for an undetermined answer.

#### Scenario: The hub cannot be reached to confirm a credential
- **WHEN** a credential is present and the hub cannot be reached to confirm it
- **THEN** the state is reported as undetermined, not as "not signed in", and the exit status is the
  one reserved for an undetermined answer

#### Scenario: A credential is present and cannot be read
- **WHEN** a credential file exists and cannot be parsed
- **THEN** the state is reported as undetermined and says a credential is present, never as "not
  signed in"

#### Scenario: The CLI and the control API are asked at the same moment
- **WHEN** the sign-in state is read from the CLI and from the daemon's control API for the same
  machine at the same moment
- **THEN** the two report the same state and the same machine-readable code, including when that
  state is undetermined

### Requirement: The local half stands alone and says what is missing
The product SHALL open no network connection when no hub is configured, and every auth command in
that state SHALL either complete fully or name the absent hub configuration as the missing thing.

#### Scenario: Every auth command is run with no hub configured
- **WHEN** each auth command is run on a machine with no hub configured
- **THEN** none of them contacts a hub, none produces empty output with a success status, and any
  that cannot complete names the absent hub configuration and the setting that is not set

#### Scenario: The three hub-related facts are reported
- **WHEN** a machine has no hub configured, a machine has a hub it cannot reach, and a machine has a
  hub and no credential
- **THEN** the three are reported as three different facts, with three different codes and three
  different sentences

### Requirement: No auth command starts the daemon or proceeds without the control API
The product SHALL report that the daemon is not running rather than starting it, and SHALL report
that the control API did not open rather than proceeding without it.

#### Scenario: An auth command is run with the daemon stopped
- **WHEN** a person runs an auth command against a store whose daemon is not running
- **THEN** the command says the daemon is not running, and afterwards the daemon is still not
  running

#### Scenario: Owner-only socket permissions cannot be confirmed
- **WHEN** the daemon is running and its control API declined to open because owner-only permissions
  could not be confirmed, and a person runs an auth command that depends on it
- **THEN** the command says the control API is not open, does not proceed, and that answer is
  distinguishable from success and from "the daemon is not running"

### Requirement: Token material is never printed
The product SHALL NOT print a token secret on any output stream.

#### Scenario: A sign-in completes and every surface is run afterwards
- **WHEN** a machine holds a credential and every auth surface is run
- **THEN** no output stream contains the token's material, including the stream of the command that
  had the material in its hand

#### Scenario: A secret is passed to a formatting verb
- **WHEN** token material is passed to any formatting verb, embedded in a struct, wrapped in an
  error, or serialised
- **THEN** a redaction is produced rather than the material

