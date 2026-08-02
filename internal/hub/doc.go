// Package hub is the company-side half of the product: the service that holds published notes,
// their versions, who may read them, and the hub's own record of people and groups (PRD §2.2).
//
// It is the FIRST hub-side code in the repository, and five later Issues build on it — #11
// versions, #13 corpus statistics, #14 references, #15 search, #22 departed colleagues' notes. So
// the surface here is deliberately wider than Issue #12 alone needs, and deliberately boring:
// small value types, one store, and one pure predicate.
//
// # The predicate is the point
//
// [CanRead] is the whole of visibility evaluation, and it is a free function over values rather
// than a method on the store. PRD §3.5 says "visibility is a precondition of ranking" — search
// (#15) must settle what a searcher may see BEFORE it orders anything, and it must settle it with
// the same rule the store uses, not a second implementation that agrees today. A method on *Store
// would have forced search either to hold a store or to write its own copy; a free function over
// (Visibility, author, reader, Membership) can be called from anywhere, including from a ranking
// loop that never touches a note body.
//
// # Three answers, not two
//
// Readability is a [tri.Value], not a bool. A group whose membership cannot be resolved, a record
// that cannot be read, a hub that cannot be reached: none of those is "no". PRD §4.3 and the
// project's standing rule — `could not determine` and `determined to be nothing` never share a
// rendering and never share an exit code — apply to visibility exactly as they apply to health.
//
// # What this package does NOT contain, on purpose
//
//   - No local store and no outbox. Those are Issue #3 and Issue #9, on other branches. When a
//     publication is refused here, this package refuses it completely and distinguishably; the
//     client-side consequence ("the note remains in the outbox", PRD §3.11) is #9's to wire.
//   - No sign-in flow and no token material. That is Issue #19. What lives here is the scope
//     VOCABULARY (PRD §4.5, one vocabulary across CLI, agent API and hub) and the refusal rule for
//     a grant wider than its holder — see [EvaluateGrantRequest].
//   - No directory, LDAP or SSO client. PRD §5.3 rules that the hub owns group membership. Group
//     narrowing is evaluated against [Membership] and nothing else, and evaluation works with no
//     directory integration present because there is no place to plug one in.
//   - No network transport. Nothing in this package imports net, and a test asserts that.
//
// # §2.4 is not decoration
//
// The hub can read everything published to it, including notes narrowed to a group or to yourself,
// because it indexes them. [RestrictionStatement] says so, and [CheckSurface] is the rule that any
// surface which offers or displays a narrowing must carry it. That is enforced by test against the
// real command output, not by convention.
package hub
