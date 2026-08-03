// Package auth is token MATERIAL — credentials, expiry, revocation — and the device-code sign-in
// that produces it (Issue #19; PRD §3.10, §4.2, §4.3, §4.5).
//
// # WHAT THIS PACKAGE IS NOT
//
// It is NOT the authority model. `internal/hub` already owns that: one scope vocabulary of exactly
// three names, and [hub.EvaluateGrantRequest], which refuses a grant wider than its holder AT THE
// MOMENT IT IS REQUESTED. Issue #12 deferred token material to this Issue and asked, in as many
// words, that the rule keep being called rather than re-derived. So every place in this package
// that decides what a grant may carry calls that function, and there is no second copy of the rule
// here — a grep for `EvaluateGrantRequest` finds every authorisation decision this package makes.
//
// THERE IS NO FOURTH SCOPE. The vocabulary is `read` / `write` / `publish`, ruled twice by the
// owner, and the second ruling is the one worth restating here because it is the one that looks
// like an omission: **the hub operator's ability to read everything is a deployment fact and is
// NOT expressed through the scope system** (PRD §2.4). There is deliberately no `operate`, no
// `admin` and no `all`. A request for one is refused as an UNKNOWN scope — criterion 31 — which is
// a different refusal from "known, but wider than you hold", because the two are fixed by
// different things: the first by asking for something that exists, the second by not asking at all.
//
// # THE TWO HALVES, AND WHICH ONE IS REAL
//
//	[Authority]   the hub's half. Device codes, tokens, revocation, delegation. REAL CODE — it is
//	              the product's authority behaviour, exercised directly by tests.
//	[Credential]  the client's half. What a signed-in machine has on disk, at 0600.
//	[Observe]     the one function that answers "is this machine signed in", for every surface.
//
// WHAT DOES NOT EXIST IN THIS BUILD IS THE TRANSPORT BETWEEN THEM. There is no wire protocol from
// a client to a remote hub, because no Issue has built one yet (`internal/commands/visibility_cmd.go`
// says the same thing about its own hub access, and for the same reason). A client with a hub
// configured therefore cannot reach it, and that renders as UNDETERMINED — never as "not signed
// in", which is §4.3's whole point. Tests wire an [Authority] in directly through the [Hub] seam;
// that is a fake TRANSPORT around REAL authority code, not a fake sign-in.
//
// # THE SECRET IS UNPRINTABLE BY CONSTRUCTION
//
// [Secret] is a struct with an unexported field and a String method that redacts. A caller who
// hands one to fmt gets `«withheld»`, not the credential. That is stronger than a code review
// habit: to print the material you have to call [Secret.Expose] and mean it, and only the
// credential file's writer does.
package auth
