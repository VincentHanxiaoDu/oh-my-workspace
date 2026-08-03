// Package hubd is the hub PROCESS: the thing a company runs, which holds published notes and
// answers questions about them (Issue #103; PRD §2.2, §2.3, §2.4, §3.5, §3.10).
//
// `internal/hub` is the hub's RULES — visibility, scopes, search, statistics, references — as pure
// values and one in-memory store. Nothing in it survives the process that built it and nothing in
// it listens to anybody. This package is the process those rules run inside: it opens a directory,
// replays what was published into it, holds the store, authenticates whoever is asking, and refuses
// when it cannot write.
//
// # THERE IS NO WIRE HERE, AND THAT IS ON PURPOSE
//
// The client-to-hub transport is Issue #104 and it is a SIBLING of this one. Nothing in this
// package imports net, net/http, or os/exec, and [TestHubdOpensNoNetworkConnection] asserts that on
// the source rather than trusting the claim. Every operation here takes token material and returns
// an answer in Go; #104 attaches those operations to a wire without changing one of them.
//
// The honest consequence, stated rather than left for a reader to discover: A CLIENT ON ANOTHER
// MACHINE CANNOT REACH THIS PROCESS IN THIS BUILD. `omw` continues to answer "the hub could not be
// reached" — which renders as UNDETERMINED and never as "there is nothing there" — because that is
// true, and it stays true until #104 lands.
//
// # WHAT THE HUB CAN READ (PRD §2.4)
//
// Everything published to it, INCLUDING notes narrowed to named people, to a group, or to yourself.
// It has to: it indexes them so they can be found. Restriction controls which COLLEAGUES see a
// note; it is not a wall against whoever operates this process. See [OperatorReach], which is the
// text this package prints, and which is built from `hub.RestrictionStatement` so that it is the
// same sentence the client prints at the point of choice rather than a second, softer one.
//
// # THREE ANSWERS, IN A PROCESS THAT CAN FAIL
//
// PRD §4.3 binds a server exactly as it binds a client. Two shapes of it are specific to running as
// a process, and both are the difference between "I could not determine" and "there is nothing":
//
//   - A HALTED HUB ANSWERS NOTHING, AND SAYS SO. When the durable record cannot be written the
//     server halts (see [Server.Halted]) and every subsequent call — including every read — refuses
//     with [ErrHubHalted]. It does not keep serving reads from memory. A hub that answers a search
//     out of a store it can no longer add to is a hub reporting a corpus that is quietly frozen,
//     and a person reads that as an answer.
//   - A COUNT IS AN ANSWER. Nothing here reports zero of something it could not read. A refusal
//     returns a refusal; it never returns an empty [hub.Outcome] with Total 0.
//
// # AUTHENTICATION: THE TOKEN SAYS WHAT, THE SESSION SAYS WHO
//
// PRD §3.10, and Issue #103 criterion 6. Every operation takes an [auth.Secret], not a person id.
// The identity comes from the session the secret resolves to, through `auth.Authority`, and the
// scopes on that session limit what may be done WITHIN that identity. There is no argument anywhere
// in this package by which a caller states who they are, so "acting as somebody else" is not a
// thing a caller can express — not a thing they are checked for and refused.
//
// An unauthenticated caller therefore never reaches the visibility predicate at all. That matters
// because `hub.CanRead("")` answers UNDETERMINED for every note by design, and a server that let an
// empty identity through would hand that undetermined answer to a filter and get a list back. See
// [Server.grant], which is the one door.
package hubd
