// The model provider interface, plugged into the ONE extension mechanism (PRD §2.5, Issue #21).
//
// # WHY THIS IS IN internal/model AND NOT IN internal/extension
//
// The same reason `channels/extension.go` gives: the MECHANISM is `internal/extension`, once, for
// both interfaces, and what is here is the six-line adapter presenting a [Provider] as one. Keeping
// the adapter next to the interface it adapts is also what lets [Register] be the single place a
// provider becomes known — a provider that had to register itself twice, once here and once with
// the extension registry, is one that can be present in one and absent from the other, which is
// exactly the "two systems" §2.5 forbids rebuilt inside the fix for it.
//
// # WHAT ISSUE #18 LEFT FOR THIS FILE
//
// `provider.go` says: "The wrong thing to do here is to build the general mechanism … because then
// #21 arrives to find a second extension system already load-bearing and its job becomes a
// migration instead of a design. So this is the smallest interface that lets a provider exist and
// be chosen, and nothing more." That judgement held. Nothing in `provider.go` or `config.go` was
// rewritten to reach one mechanism; this file was added, and [Register] gained one line.
package model

import (
	"fmt"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/refusal"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Extension presents a [Provider] as an [extension.Extension].
type Extension struct{ P Provider }

// Name is the provider's own name — the word a person types after `omw model use`, and the same
// word they type after `omw ext register`. ONE identifier for one thing.
func (e Extension) Name() string {
	if e.P == nil {
		return ""
	}
	return e.P.Name()
}

// Interface is the model provider interface.
func (e Extension) Interface() extension.Interface { return extension.Model }

// Load reports whether the provider can be loaded.
//
// IT CALLS NOTHING ON THE PROVIDER, and in particular it does not call [Provider.Open]. Open binds
// a CREDENTIAL, and the inventory has no business holding one (criterion 22) — nor is there a
// credential to hand it, since `omw ext list` is asked by people who have not configured a key.
// A provider whose code is present has loaded; the questions that need a credential or an endpoint
// are asked when somebody asks the model something, which is [Session.Ask].
func (e Extension) Load() error {
	if e.P == nil {
		return fmt.Errorf("the registered provider is nil, so there is nothing to load")
	}
	if e.P.Name() == "" {
		return fmt.Errorf("the registered provider has no name, so nothing can refer to it")
	}
	return nil
}

// Provider is the provider itself, once loaded.
func (e Extension) Provider() Provider { return e.P }

// ---------------------------------------------------------------------------
// CRITERION 10 — a provider that failed to load is never "no model configured"
// ---------------------------------------------------------------------------

// ErrProviderFailedToLoad is criterion 10's code: the one a caller reads to tell a broken extension
// from an unconfigured model WITHOUT parsing prose.
//
// It is deliberately NOT [ErrNoModel]. Sharing a code would make the two situations
// indistinguishable to exactly the caller — a script, an agent, the control API — that has no
// English to inspect, which is the whole reason this product has codes.
var ErrProviderFailedToLoad = &refusal.Error{
	Code: "model-provider-extension-failed-to-load",
	Msg:  "the chosen model provider's extension failed to load, which is not 'no model configured'",
}

// Situation is which situation a capability that needs a model is in.
//
// # THE TWO THIS TYPE EXISTS TO KEEP APART
//
// PRD §3.13: "no model configured is not a broken client." Issue #21: that sentence "becomes a lie
// the moment a failed load is dressed up as an unconfigured one". A person told "no model
// configured" goes and configures a model — which they already did — and the real fault, an
// extension that will not load, is never looked at.
//
// THE ZERO VALUE IS UNDETERMINED, following [tri.Value]: a situation nobody established is not
// "no model configured".
type Situation int

const (
	// SituationUndetermined — which situation this is could not be worked out.
	SituationUndetermined Situation = iota
	// SituationReady — a provider is chosen, its extension loaded, and a credential is supplied.
	SituationReady
	// SituationNoModelConfigured — §3.13's state. NOT a broken client.
	SituationNoModelConfigured
	// SituationExtensionFailedToLoad — a provider is chosen and its extension will not load. The
	// client IS broken, and says so.
	SituationExtensionFailedToLoad
)

// Answer is a situation with the sentence and the code that go with it.
type Answer struct {
	Situation Situation
	// Code is the stable machine-readable code, so a caller distinguishes the situations without
	// parsing prose. Empty only for [SituationReady].
	Code string
	// Reason is the sentence a person reads. NEVER empty, for any situation — criterion 21's "no
	// state is ever rendered as an empty string" applied here.
	Reason string
}

// Readiness answers, for a capability that needs a model, which situation it is in.
//
// It takes the [View] this package already resolves — nothing here re-derives "is a model
// configured", because two answers to that question which disagree is the defect this package's own
// commit message calls "the same class as the two outboxes §3.14 forbids". What it ADDS is the
// question Issue #18 could not ask, because the mechanism did not exist: does the chosen provider's
// extension load?
//
// # THE ORDER OF THE BRANCHES IS THE CRITERION
//
// The extension is consulted BEFORE the credential. A person whose extension is broken and whose
// key is also missing has a broken client first, and telling them "no credential has been supplied"
// sends them to fix the smaller of two problems while the larger one goes unmentioned.
func Readiness(s *store.Store, r *extension.Registry, v View) Answer {
	if r == nil {
		r = extension.Default
	}
	switch v.Chosen() {
	case tri.No:
		return Answer{
			Situation: SituationNoModelConfigured,
			Code:      ErrNoModel.Code,
			Reason: "no model provider is chosen. This is not a broken client (§3.13): everything " +
				"that does not need a model keeps working.",
		}
	case tri.Undetermined:
		return Answer{
			Situation: SituationUndetermined,
			Code:      ErrUndetermined.Code,
			Reason: "which provider is configured could not be determined. This is NOT a report " +
				"that no model is configured.",
		}
	}

	// FindAs, NOT Find. By name alone, a CHANNEL ADAPTER registered under the provider's name came
	// back Loaded and this function said "the model provider is chosen and its extension loaded" —
	// a confident claim about a model extension when what loaded was a channel adapter. Reachable
	// with `omw ext register slack` followed by `omw model use slack`, both documented, both exit 0.
	// See extension.FindAs.
	e := extension.FindAs(extension.Read(s, r).Entries, v.Provider, extension.Model)
	switch e.Resolved() {
	case extension.FailedToLoad:
		return Answer{
			Situation: SituationExtensionFailedToLoad,
			Code:      ErrProviderFailedToLoad.Code,
			Reason: fmt.Sprintf("the model provider %s IS chosen and its extension FAILED TO LOAD. "+
				"This is not 'no model configured' — your configuration is intact and the "+
				"extension behind it is broken: %s", v.Provider, e.Detail),
		}
	case extension.NotRegistered:
		// THE ENTRY'S OWN DETAIL IS CARRIED, NOT REPLACED. `FindAs` distinguishes "nothing of that
		// name is here" from "something of that name is here and it implements the OTHER
		// interface", and the advice differs completely: the first person runs `omw ext register`,
		// the second has already done that and needs to know they registered a channel adapter.
		// Telling the second to register what they have registered sends them round a loop.
		advice := fmt.Sprintf("'omw ext register %s' registers it", v.Provider)
		if e.Detail != "" {
			advice = e.Detail
		}
		return Answer{
			Situation: SituationExtensionFailedToLoad,
			Code:      ErrProviderFailedToLoad.Code,
			Reason: fmt.Sprintf("the model provider %s IS chosen and no model-provider extension "+
				"for it is registered on this machine. This is not 'no model configured' — your "+
				"configuration is intact and the code to talk to it is not registered: %s",
				v.Provider, advice),
		}
	case extension.Undetermined:
		return Answer{
			Situation: SituationUndetermined,
			Code:      extension.ErrLoadUndetermined.Code,
			Reason: fmt.Sprintf("the model provider %s is chosen and whether its extension loads "+
				"could not be determined. This is NOT 'no model configured' and NOT a report "+
				"that it failed: %s", v.Provider, e.Detail),
		}
	}

	switch v.Present() {
	case tri.No:
		return Answer{
			Situation: SituationNoModelConfigured,
			Code:      ErrNoCredential.Code,
			Reason: fmt.Sprintf("the model provider %s is chosen and its extension loaded, and no "+
				"credential has been supplied for it.", v.Provider),
		}
	case tri.Undetermined:
		return Answer{
			Situation: SituationUndetermined,
			Code:      ErrUndetermined.Code,
			Reason: fmt.Sprintf("the model provider %s is chosen and its extension loaded, and "+
				"whether a credential is supplied could not be determined. This is NOT 'no "+
				"credential'.", v.Provider),
		}
	}
	return Answer{
		Situation: SituationReady,
		Reason: fmt.Sprintf("the model provider %s is chosen, its extension loaded, and a "+
			"credential is supplied (the credential itself is never printed).", v.Provider),
	}
}

// ---------------------------------------------------------------------------
// THE ADAPTER FACT, ABOUT THIS MACHINE RATHER THAN ABOUT THE BUILD
// ---------------------------------------------------------------------------

// ViewOn is [Config.View] with the adapter question answered from the extension mechanism.
//
// # THE DEFECT IT CLOSES
//
// [Config.View] derives Adapter from [Lookup], which answers "is there code for this provider
// COMPILED INTO THIS BUILD". That was the only answer available before this mechanism existed, and
// once it exists it is the wrong one: a person who registered a provider extension that will not
// load was told "this build has no adapter for teams" — a fact true of every machine running this
// binary — at the moment `omw ext list` told them their registered extension had FAILED TO LOAD.
// One fact, two surfaces, two reasons. Commit f55a176 fixed exactly this on the channel side; this
// is the model side, and it deliberately reuses that finding's shape rather than inventing a third
// vocabulary for it.
//
// # IT IS A FUNCTION AND NOT A METHOD ON Config, ON PURPOSE
//
// The answer depends on the store and on the registry, and a [Config] holds neither. A method that
// reached for [extension.Default] would make the answer depend on process-global state, which is
// the reason [extension.Registry] is a value — see [Readiness], which takes the same two arguments
// for the same reason.
//
// # AGREEMENT STAYS STRUCTURAL
//
// There is still ONE renderer, [View.Render]. This adds a field to the value both surfaces render,
// not a sentence one surface appends — which is what [View.Adapter]'s own doc says is the fix that
// holds.
//
// It opens no connection: [extension.Extension.Load] is documented as contacting nothing.
func ViewOn(s *store.Store, r *extension.Registry, c Config) View {
	v := c.View()
	if c.Provider != tri.Yes {
		// Whether this machine can talk to a provider whose name is not established is not a
		// question with an answer.
		return v
	}
	if r == nil {
		r = extension.Default
	}
	// FindAs, NOT Find, for the reason Readiness gives: a CHANNEL adapter registered under the
	// provider's name is not a model adapter for it.
	e := extension.FindAs(extension.Read(s, r).Entries, c.Name, extension.Model)
	switch e.Resolved() {
	case extension.Loaded:
		// A registered extension that loads IS the adapter, whether or not the build ships one.
		v.Adapter = tri.Yes.String()
	case extension.FailedToLoad:
		v.Adapter = tri.No.String()
		v.AdapterDetail = "the registered " + c.Name + " model-provider extension FAILED TO LOAD, so review " +
			"cannot run against it. Your configuration is intact and the extension behind it is broken: " + e.Detail
	case extension.Undetermined:
		v.Adapter = tri.Undetermined.String()
		v.AdapterDetail = "whether the registered " + c.Name + " model-provider extension loads " +
			tri.Undetermined.String() + ", which is NOT a report that it failed and NOT a report that " +
			"the code to talk to it is absent: " + e.Detail
	}
	// NotRegistered is left alone deliberately: nothing of that name has been registered on this
	// machine, so what this BUILD ships is the whole of the answer and the existing sentence is the
	// right one.
	return v
}
