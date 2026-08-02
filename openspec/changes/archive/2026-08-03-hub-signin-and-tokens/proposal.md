# Sign in to the hub on purpose, and see and end everything signed in as you

## Why

A person has a hub at work. They want to sign in **once, on purpose**, and then have their client —
and their own AI, and a script on the build box — act as them against it. Reading their notes and
publishing as them are **not the same favour**, and they want to have said yes to the second one out
loud. Later they want to look at what is signed in as them and **end any one of it** without ending
the others. And they never want to discover that something signed itself in on their behalf, or that
a token they asked for turned out to be wider than what they asked for.

`internal/hub` already owns the authority model, and it is finished: one vocabulary of exactly three
scopes — `read` / `write` / `publish` — and `EvaluateGrantRequest`, which refuses a grant wider than
its holder **at the moment it is requested** rather than narrowing it at the edge (PRD §4.5). What it
has no notion of is **token material**. `hub.Grant` says so in as many words: "there is no secret
here, no expiry and no signature". So the ledger can show that a refused request issued nothing, and
nothing in the product can yet be signed in, revoked, or shown as expired.

Three things make this more than "add a token type":

- **A revoked token, an expired token and a token that never existed are three different facts**
  (PRD §4.3). So are "no hub configured", "hub configured but unreachable" and "signed out". Each
  pair is one a person would act on differently, and each is one that collapses into the other the
  moment somebody writes the shorter version of the code.
- **Nothing signs in silently** (PRD §4.2). That is not a property of careful commands; it is a
  property of exactly one function being able to write a credential, and of exactly one caller
  calling it.
- **An unreachable hub is not a rejection.** This build has no client-to-hub transport at all, so
  the shipped answer for a configured hub is UNDETERMINED — which is the honest answer and also the
  path a genuinely unreachable hub will take once a transport exists.

## What Changes

- **A new `internal/auth` package** holding token material and nothing else: unguessable token ids
  and secrets from `crypto/rand`, a device-code flow, an owner-only credential file, revocation,
  expiry, delegation, and the session listing.
- **The authorisation rule is not reimplemented.** Every decision about what a grant may carry is a
  call to `hub.EvaluateGrantRequest`, as Issue #12 asked in as many words. There is no scope logic
  in `Authenticate`: it returns a `hub.Grant`, so the existing `ReadThrough`, `PublishThrough` and
  `SetVisibilityThrough` govern what a token can do — which is how one scope means one thing on the
  CLI, the agent API and the hub without a second table.
- **No fourth scope.** The vocabulary stays at three. The hub operator's ability to read everything
  is stated as a **deployment fact** where a person meets the question, and a request for any scope
  naming it is refused as an *unknown* scope — a different refusal, with a different code, from
  "known, but wider than you hold".
- **A `Secret` type that cannot be printed by accident**: unexported field, redacting `String`,
  `GoString` and `Format`, so every `fmt` verb yields a placeholder. Exposing the material takes a
  deliberate call, and one caller in the product makes it.
- **A new `internal/commands/auth_cmd.go`** with `omw auth status | scopes | sign-in | sign-out |
  sessions | revoke`. Every subcommand applies the same three checks in the same order: no hub
  configured, then daemon liveness through the product's one answer, then whether the control API
  opened — each with its own code and its own exit status.
- **`daemon.Report` gains `auth`, `auth_code` and `auth_detail`**, filled by the same
  `auth.Observe` the CLI calls. PRD §4.3 requires the control API and the CLI to report the same
  state; making them one function is stronger than making two agree.
- **Device-code sign-in only**, per the owner's ruling: the CLI prints a code, the person enters it
  in a browser, and the command polls. It opens no browser, binds no port and needs no graphical
  session, so it works over SSH on a headless machine.
- **What is faked is the wire, and only the wire.** The shipped `Hub` is `auth.Unreachable`, which
  answers every call with `hub-unreachable`. Tests substitute a real `auth.Authority` through that
  seam: real device codes, real minting, real expiry, real replay refusal, real revocation, real
  scope decisions. No test fabricates a successful sign-in.
