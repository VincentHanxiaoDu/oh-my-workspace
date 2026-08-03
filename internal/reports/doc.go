// Package reports is subscriptions and the reports they produce (PRD §3.7, Issue #23).
//
// A SUBSCRIPTION IS A STANDING INSTRUCTION built from SELECTORS, each naming a SUBJECT and a
// GRANULARITY. The PRD's five examples are the specification:
//
//	git:full              every commit, with its message
//	token_usage:digest    model spend, rolled up
//	*:summary             one paragraph per subject, for everything
//	git.commit:event      commits without their text
//	*, !channel           everything except channel traffic
//
// # THE ONE DESIGN CONSTRAINT
//
// A granularity means THE SAME THING FOR EVERY SUBJECT. That is not decoration: it is what makes
// `*:summary` a sensible thing to type without knowing the subject list. So there is exactly one
// function in this package that turns activity into a body — [renderBody] — and it is a switch on
// the granularity with no subject in scope at all. A per-subject branch cannot be added to it
// without deleting it, which is the point. [TestWildcardSummaryEqualsNamingEverySubject] asserts
// the consequence directly: `*:summary` renders byte-identically to naming every root subject at
// `summary`.
//
// # THE ORDERING IS LOAD-BEARING
//
// full ⊃ event ⊃ digest ⊃ summary ⊃ count. This is asserted as a PROPERTY over generated activity
// (see the granularity tests), not as five assertions against five literals — the literal form of
// that test passes just as happily after two granularities are swapped.
//
// # THREE FACTS THAT MUST NEVER LOOK ALIKE
//
// This package exists downstream of §4.3, and its whole shape is the refusal to collapse these:
//
//   - A SELECTOR THAT IS MALFORMED is REFUSED, by name, at the point the subscription is written,
//     and nothing is stored. `git:enormous` is not a granularity; `:full` has no subject.
//   - A SELECTOR THAT NAMES NO KNOWN SUBJECT is stored (it is well formed) and every report it
//     appears in says, naming the selector, that it matched no known subject. It is NEVER silence.
//   - A SUBJECT WITH NO ACTIVITY is a determined, ordinary, successful answer, and reads as one.
//
// And beside them, two more the same rule produces: a subject whose source could not be READ is
// UNDETERMINED (never empty, never `count: 0`), and a subject only a hub can supply, with no hub
// configured, says exactly that (never empty, never unknown).
//
// # NO NETWORK, NO DAEMON
//
// Nothing here dials anything and nothing here starts anything (§4.2). The hub-supplied subject is
// answered from a flag the caller sets; the absence of a hub is a determined local fact, so
// establishing it requires no connection.
package reports
