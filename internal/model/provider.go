// The provider interface and the registry that holds providers (PRD §2.5, Issue #18 criterion 1's
// "a model provider").
//
// # THIS IS DELIBERATELY SMALL, AND ISSUE #21 IS WHY
//
// PRD §2.5: "Channel adapters and model providers are the same mechanism with two interfaces. A
// company adding a channel and a company choosing a model do the same kind of thing, and should not
// learn two systems." Issue #21 owns that one mechanism and has not started. The wrong thing to do
// here is to build the general mechanism — lifecycle, configuration schema, discovery, versioning —
// because then #21 arrives to find a second extension system already load-bearing and its job
// becomes a migration instead of a design.
//
// So this is the smallest interface that lets a provider exist and be chosen, and nothing more:
// a name, and a way to bind a credential to a session. What #21 will need to unify is written down
// in the pull request body rather than guessed at here.
//
// # REGISTERING A PROVIDER CONTACTS NOTHING (§4.2, "nothing implicit")
//
// Register stores a value in a map. It does not call Open, it does not call Ask, it does not
// validate a credential against an endpoint, and it does not check that a host resolves. This is
// not an optimisation. A registry that reached out on registration would mean that merely BUILDING
// a binary with a provider compiled into it opens a connection at init time, on a machine with no
// hub and no model configured — which §4.2's "no network connection without a hub configured" and
// §4.4's "the local half stands alone" both forbid, and which would make this package's tests
// require a live network.
//
// TestRegisteringAProviderContactsNothing drives it with a provider that records every method call.
package model

import (
	"sort"
	"strings"
	"sync"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
)

// Provider is a model provider — an extension, in §2.5's sense.
//
// IT DOES NOT TAKE THE CREDENTIAL IN ITS CONSTRUCTOR AND IT DOES NOT HOLD ONE. A Provider is a
// registered, process-lifetime value shared by everything in the binary; a credential belongs to
// one person and one invocation. Binding them through Open keeps the credential out of any
// long-lived value and gives the seam where it enters exactly one location.
type Provider interface {
	// Name is the word a person types after `omw model use`.
	Name() string

	// Open binds this provider to a person's credential and returns something that can be asked a
	// question.
	//
	// IT MUST NOT CONTACT THE ENDPOINT. Open is called on paths that only want to know whether a
	// session can be constructed; a connection belongs in [Session.Ask], where a person has asked
	// for something that genuinely needs the model. An implementation that dials here turns "am I
	// configured?" into network traffic.
	Open(credential string) (Session, error)
}

// Session is a bound provider, ready to be asked something.
type Session interface {
	// Ask sends the prompt and returns the provider's answer verbatim.
	//
	// The answer is TEXT, not a verdict. `internal/drafts` turns text into a verdict in one place,
	// where the interesting failure — a model that answers with something that is not a verdict —
	// can be tested once. See drafts.Interpret.
	Ask(prompt string) (string, error)
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Provider{}
)

// Register adds a provider under its own name. It is called from an init in the provider's file,
// exactly as cli.Register is.
//
// It PANICS on a nil provider, an unnamed one, or a duplicate name, for the reason cli.Register
// does: silently keeping one of two providers with the same name means a person types a name and
// reaches whichever file the linker initialised second.
//
// It touches nothing but a map. See the file comment.
func Register(p Provider) {
	if p == nil {
		panic("model.Register: nil provider")
	}
	name := strings.TrimSpace(p.Name())
	if name == "" {
		panic("model.Register: provider with no name")
	}
	registryMu.Lock()
	if _, dup := registry[name]; dup {
		registryMu.Unlock()
		panic("model.Register: duplicate provider " + name)
	}
	registry[name] = p
	registryMu.Unlock()

	// ONE REGISTRATION, NOT TWO (Issue #21, §2.5). A provider becomes known to the one extension
	// mechanism here and nowhere else. The alternative — asking each provider's init to call
	// `extension.Default.Offer` as well — is two registrations of one thing that can disagree, so
	// that a provider is choosable by `omw model use` and invisible to `omw ext list`, or the other
	// way round. That is the "two systems" §2.5 forbids, rebuilt inside the fix for it.
	//
	// It is outside the lock because Offer takes its own, and a provider is never registered twice.
	extension.Default.Offer(Extension{P: p})
}

// Lookup returns the registered provider of that name.
//
// A false here is a DETERMINED fact — this build has no adapter for that name — and not an error
// and not "no provider is chosen". A person who chose `acme` on a build with no acme adapter has a
// provider chosen; what they lack is code to talk to it. Those are different sentences and
// [Config.Render] plus the `omw model` command keep them different.
func Lookup(name string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[strings.TrimSpace(name)]
	return p, ok
}

// Names returns the registered providers' names, ordered.
//
// AN EMPTY LIST IS A REAL STATE OF THIS BUILD and callers must say so rather than printing an empty
// section. Issue #21 is what puts providers in here; this Issue is what lets a person choose one
// and supply a key for it, and those are separable — a person may name a provider this build cannot
// yet talk to, and be told exactly that.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// unregister removes a provider. Tests only — the product never removes one, and a registry that
// could be emptied at runtime would make "which providers does this build have" a question with a
// different answer depending on when it was asked.
func unregister(name string) {
	registryMu.Lock()
	delete(registry, name)
	registryMu.Unlock()
	// BOTH, OR THE TWO DISAGREE. Register puts a provider in both places; a removal that only
	// emptied one would leave `omw ext list` reporting a provider `omw model use` no longer has —
	// and would make the very next Register of that name panic on a duplicate.
	extension.Default.Withdraw(name)
}
