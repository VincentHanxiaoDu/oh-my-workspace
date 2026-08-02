// Package extension is the ONE extension mechanism, with two interfaces (PRD §2.5, Issue #21).
//
// PRD §2.5: "Channel adapters and model providers are the same mechanism with two interfaces. A
// company adding a channel and a company choosing a model do the same kind of thing, and should not
// learn two systems."
//
// # WHAT "ONE MECHANISM" IS MADE OF HERE, AND WHAT IT DELIBERATELY IS NOT
//
// It is NOT one interface that both a channel adapter and a model provider implement by having
// their methods merged. A channel adapter fetches messages; a model provider answers prompts. Those
// are two interfaces and §2.5 says so in the same sentence.
//
// What is one is everything AROUND them: the act of registering, the inventory a person lists, the
// vocabulary of states an entry can be in, the way a failure is reported, the exit code. A person
// adding Slack and a person pointing omw at their own model endpoint type the same command with the
// same arguments in the same order, read the same listing, and — when it is broken — are told in
// the same words.
//
// So [Extension] has exactly the three methods that the SHARED half needs: a name, which of the two
// interfaces it implements, and whether it loads. [ChannelExtension] and [ModelExtension] add the
// interface-specific method on top. Nothing here knows how to fetch a message or ask a model.
//
// # THIS PACKAGE UNIFIES TWO EXISTING IMPLEMENTATIONS RATHER THAN ADDING A THIRD
//
// Issue #6 built `internal/channels` — the channel adapter interface, Teams and email built in.
// Issue #18 built `internal/model` — the provider interface and its registry, with a `View` that
// reports WHETHER a credential is present and never WHICH. Both were right about their own half and
// #18 says in its own source that it kept its registry deliberately small because "#21 arrives to
// find a second extension system already load-bearing and its job becomes a migration instead of a
// design".
//
// This package is the mechanism they are both instances of. It does not copy either:
//
//   - `model.Provider` and `model.Register` are still the model side's own interface and registry;
//     [FromProvider] wraps a `model.Provider` into an [Extension], and [Offered] pulls the model
//     registry's names in through [WithModelRegistry] rather than keeping a second copy of them.
//   - `channels.Kind` and `channels.Adapter` are still the channel side's own. [FromChannelKind]
//     wraps the built-in kinds so that Teams and email appear in the SAME inventory as anything
//     registered through the extension point (criterion 6), and [ChannelFactory] hands ingestion a
//     `channels.Factory` that refuses to construct an adapter whose extension failed to load.
//
// The unification is therefore at the STATE and the ACT, which is where §2.5's sentence actually
// bites, and neither package had to be rewritten to get it.
//
// # THE FOUR STATES ARE ONE VOCABULARY (criteria 3, 7, 13)
//
// [State] has four values and they are the same four whichever interface an entry implements.
// [Entry.Render] takes no branch on the interface, which is why criterion 3's test — capture a
// failed-to-load channel adapter line and a failed-to-load model provider line, normalise the name
// and the interface, diff — finds nothing. A rendering with an `if e.Interface == …` in it would
// pass that test on the day it was written and fail it the first time somebody improved one of the
// two messages.
//
// # NOTHING HERE OPENS A CONNECTION AND NOTHING HERE STARTS THE DAEMON (criteria 15, 16, §4.2)
//
// [Register] writes a record to the store. It does not call [Extension.Load], it does not resolve a
// host, and for a model provider it does not contact the provider's endpoint. Loading happens when
// somebody ASKS for the inventory, and [Extension.Load] is documented as contacting nothing either.
// TestRegisteringContactsNothing and the repository-wide listen/dial guard both hold this.
//
// # NO CREDENTIAL CAN REACH AN ENTRY (criterion 22)
//
// Issue #18 made this structural rather than careful: its `View` has no field a credential can be
// put in. [Entry] preserves that property — Name, Interface, State and Detail, where Detail is
// built from load failures and never from a `model.Config`'s secret. There is no code path from
// `model.Config.Secret` into this package, and TestNoEntryFieldCanHoldACredential asserts the shape
// rather than grepping one instance of it.
package extension
