// Package inbox is the person's list of things they have to act on (PRD §2.3, §3.2; Issue #8).
//
// # A TICKET IS A THING YOU HAVE TO ACT ON. IT IS NOT A MESSAGE.
//
// Five emails, a chat thread and a follow-up ping about one broken login are ONE ticket, with a
// written title and a written summary — not five items titled `yes`, `ok` and `Hii`. This package
// is the type that says so, and it enforces the sentence in two ways rather than restating it:
//
//   - There is NO priority, rank, severity, score or ordering field on [Ticket], and no function in
//     this package accepts or returns one. PRD §3.2: "Acknowledgements and small talk are not
//     low-priority tickets. They are not tickets." A priority field is how that ends: somebody has
//     an acknowledgement, needs somewhere to put it, and puts it at the bottom of the list. There
//     is no bottom of the list. Listing order is by identifier — a stable presentation order, not a
//     judgement about which obligation matters more, which this product does not make.
//   - [Put] REFUSES a ticket whose title is the verbatim body of an acknowledgement with
//     [ErrNotAnObligation]. See [IsAcknowledgement] for the wording of that decision and for the
//     part of it the Issue did not settle.
//
// # TICKETS NEVER LEAVE THE MACHINE (§2.3)
//
// The inbox has no route to the hub at all. The closed set of things that can be done to a ticket
// is [Operations] — list, read, delete — and it is a function rather than a paragraph so that a
// test can enumerate it and assert nothing publishes, shares or sends. This package imports nothing
// from the standard library's networking, and there is no configuration under which it would.
//
// # NOTHING EXPIRES (§5.4, ruled)
//
// Nothing in this package reads the clock to decide what is in the inbox. A ticket carries when it
// arrived so a person can see it; no operation here filters, hides, archives or removes by age. A
// ticket leaves the inbox only through [Delete], which is a person naming it.
//
// # UNDETERMINED IS NOT "NO" (§4.3)
//
// A ticket's title, summary and channel are each a [Field], which has FOUR distinguishable states
// and renders them distinguishably: a written value, a written but empty value, a field that was
// never recorded, and a field whose value could not be determined. A missing field and an empty
// field are not the same fact and never render the same — Issue #8 criterion 1. A summary that has
// not been written yet and a channel that could not be read are undetermined, not absent and not
// empty — criterion 12.
//
// # FOR ISSUE #6 (INGESTION) AND ISSUE #7 (MERGING)
//
// [Ticket], [Field] and [Kind] are the contract. Ingestion builds tickets and calls [Put]; merging
// reads with [List] / [Get], writes the merged ticket and [Delete]s the ones it subsumed. Both will
// want fields this type does not have — merge provenance above all (§3.2: "every merge is
// reversible and shows its working"). Add them; do not add a priority.
package inbox
