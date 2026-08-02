# Tasks

## Token material, without a second authority model

- [x] Add `internal/auth` holding credentials, expiry and revocation — the material Issue #12
      deferred here — and nothing that decides authority
- [x] Mint token ids and secrets from `crypto/rand`, 128 and 256 bits, and return the entropy
      source's error rather than falling back to anything weaker
- [x] Make token ids unguessable and unstructured rather than sequential, for the reason a note id
      is: a token id is the thing a person types to END a session, so it travels
- [x] Add a `Secret` type whose field is unexported and whose `String`, `GoString` and `Format` all
      redact, so no `fmt` verb can print the material by accident
- [x] Keep only the digest of a secret in the authority, and compare in constant time
- [x] Call `hub.EvaluateGrantRequest` for every decision about what a grant may carry — at the
      device-code step for the scope names, and at approval for whether the person may hold them —
      and add no second copy of the rule

## Device-code sign-in

- [x] Print a short user code from an alphabet with no I, O, 0 or U, and assert the alphabet's size
      in a test rather than leaving it to a panic in an init
- [x] Create NO token when a device code is issued: minting happens only after the browser step
- [x] Refuse a redeemed device code as a replay, before checking expiry, and mint nothing
- [x] Kill an expired device code on both paths, so a late approval cannot resurrect it
- [x] Record a refused approval on the pending sign-in so the client's next poll learns the request
      FAILED rather than waiting for ever or receiving a narrower token
- [x] Bind no port, spawn no process and open no browser, so the flow runs over SSH on a headless
      machine

## Seeing and ending what is signed in as you

- [x] List every session including one never used, with its scope, its status and its last-use state
- [x] Render "never used", a real timestamp and "could not be determined" as three distinct things,
      and "no scope recorded", "an empty scope list" and "a real scope" as three more
- [x] End one session without ending the others, and cascade only to tokens delegated from it
- [x] Keep a revoked session in the listing, marked revoked, never active
- [x] Refuse a delegation wider than its parent, and end a delegated token when its parent is revoked
- [x] Add `DeactivatePerson`, which Issue #22 depends on, next to the sessions it ends

## Nothing implicit

- [x] Write a credential from exactly one function, called from exactly one place
- [x] Answer the sign-in question from exactly one function, `auth.Observe`, called by the CLI and
      by the daemon
- [x] Reach for no hub at all when none is configured, as a property of control flow rather than of
      every downstream function behaving
- [x] Say the daemon is not running rather than starting it, through the product's one liveness
      answer and its one reporter
- [x] Say the control API did not open rather than proceeding, with a code that is neither
      "daemon-not-running" nor a success

## The CLI

- [x] Add `internal/commands/auth_cmd.go` — a NEW file, touching no existing one — with `status`,
      `scopes`, `sign-in`, `sign-out`, `sessions` and `revoke`
- [x] Print the scope the token HAS, taken from the issued token, never the request echoed back
- [x] Print the vocabulary from `hub.Vocabulary()` rather than a CLI-side list, and state the
      operator fact as a deployment fact at that point of choice
- [x] Say, on sign-out, that the hub session is unchanged and how to actually end it

## The control API agrees, because it is the same function

- [x] Add `auth`, `auth_code` and `auth_detail` to `daemon.Report`, filled by `auth.Observe`
- [x] Fill them on both paths — the daemon's own report and the disk-derived one — so a reader
      never gets a report with the field missing

## Driving it, and watching the tests fail

- [x] Drive every criterion against a real `auth.Authority` behind the transport seam, and against
      a real daemon with a real control API
- [x] Compare renderings PAIRWISE against each other rather than against string literals, spanning
      `auth` and `hub` because criterion 28's three facts are split across the two
- [x] Bound every test that drives `sign-in`, because the failure mode of the code under test is a
      hang and a hang names nothing
- [x] Add a structural test that nothing outside the sign-in command calls the credential writer,
      and make it fail if it finds no call at all
- [x] Mutate each of: a revoked token still working, revoked and never-existed rendering
      identically, a grant being narrowed instead of refused, a secret being printed, "never used"
      rendering as undetermined, a replay being allowed, undetermined collapsing to a negative, a
      second surface writing a credential, unknown scope names being accepted, the no-hub check
      being removed, and the daemon wording the auth state itself — confirm RED naming the defect,
      and revert
