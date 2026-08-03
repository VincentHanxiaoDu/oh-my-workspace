package extension

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Extension is what BOTH interfaces have in common, and it is the whole of the shared mechanism.
//
// Three methods, because three is what the shared half needs: something to type, which interface it
// implements, and whether it loads. Fetching a message and answering a prompt are NOT here; those
// belong to [ChannelExtension] and [ModelExtension], which is §2.5's "two interfaces" said in Go.
type Extension interface {
	// Name is what a person types to register it and to ask about it.
	Name() string

	// Interface is which of the two this implements.
	Interface() Interface

	// Load prepares the extension, and reports whether it can be.
	//
	// IT MUST NOT CONTACT ANYTHING (criterion 16, §4.2). Load is called every time somebody asks
	// for the inventory — `omw ext list` on a laptop with no hub and no network must work — so an
	// implementation that dials here turns "what have I got?" into traffic. `model.Provider.Open`
	// carries the same rule for the same reason and says so in its own comment.
	//
	// Returning an error that wraps [ErrLoadUndetermined] means the answer could not be
	// ESTABLISHED, which is a different thing from establishing that it failed. Any other error is
	// a failure to load.
	Load() error
}

// # WHERE THE TWO INTERFACE-SPECIFIC HALVES LIVE, AND WHY NOT HERE
//
// The channel half is `channels.Extension` and the model half is `model.Extension`, each in the
// package that owns that interface, each with an init that offers itself into [Default] — exactly
// as `channels.loop.go` registers its background work with the daemon and as `model.Register`
// registers a provider.
//
// THIS IS NOT THE MECHANISM SPLITTING IN TWO. Everything §2.5 says must be one is one and is here:
// the act of registering, the inventory, the four states, the wording, the exit code. What lives
// with each interface is the six-line adapter that presents it as an [Extension] — and it lives
// there because it cannot live here. `internal/channels` imports `internal/daemon` to register its
// ingestion loop, and `internal/daemon` must import THIS package to serve extension state over the
// control API (criterion 20). A package here that imported `internal/channels` would close that
// into daemon → extension → channels → daemon, and Go would refuse to build it.
//
// So this package imports neither of the two, which is also what keeps it safe for anything to
// depend on — including `internal/daemon`, and including `internal/channels`, whose Issue #6 guard
// forbids it from reaching the hub transitively.

// Registry is what this machine OFFERS — the extensions present, before anybody has registered one.
//
// # IT IS A VALUE AND NOT A PACKAGE GLOBAL, AND THAT IS ON PURPOSE
//
// Criterion 17 says an extension present on disk but not registered by a deliberate act is not
// registered. A test of that claim has to control exactly what is present, and a global registry
// makes "what is present" a statement about whichever test ran last and whichever init fired. The
// product uses [Default]; every test builds its own.
type Registry struct {
	mu      sync.RWMutex
	offered map[string]Extension
}

// NewRegistry returns an empty registry — a machine that offers nothing, which is a real state and
// the one every test should start from.
func NewRegistry() *Registry { return &Registry{offered: map[string]Extension{}} }

// Default is the registry the product uses. It is EMPTY until something offers into it, and what
// fills it is an init in the package that owns each interface: `internal/channels` offers the
// built-in Teams and email kinds, and `model.Register` offers every provider it is given.
//
// An empty Default is therefore a statement about what is LINKED IN, which is the same property
// `cli.Commands` has, and it is why `Inventory` says "this machine has none" out loud rather than
// printing an empty section.
var Default = NewRegistry()

// Offer adds an extension to what this machine has available. It is NOT registration — nothing is
// loaded, nothing runs, and until a deliberate act registers it the inventory reports it as
// [NotRegistered] (criterion 17).
//
// It PANICS on a nil or unnamed extension and on a duplicate name, for the reason `cli.Register`
// and `model.Register` do: silently keeping one of two extensions with the same name means a person
// types a name and reaches whichever file the linker initialised second.
func (r *Registry) Offer(e Extension) {
	if e == nil {
		panic("extension.Offer: nil extension")
	}
	name := strings.TrimSpace(e.Name())
	if name == "" {
		panic("extension.Offer: extension with no name")
	}
	if !e.Interface().Valid() {
		panic("extension.Offer: " + name + " implements neither of the two interfaces")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.offered[name]; dup {
		panic("extension.Offer: duplicate extension " + name)
	}
	r.offered[name] = e
}

// Withdraw removes an offered extension, and returns whether there was one.
//
// IT EXISTS FOR TESTS AND FOR REPLACING A BUILT-IN WITH A BROKEN ONE IN A TEST, and the product
// never calls it: a registry that could be emptied at runtime would make "what does this machine
// offer" a question with a different answer depending on when it was asked, which is the reason
// `model.unregister` is unexported. It is exported here only because the tests that must drive
// criteria 7, 9 and 13 live in other packages, and the alternative — a build-tag seam — hides the
// same power behind more machinery.
func (r *Registry) Withdraw(name string) bool {
	name = strings.TrimSpace(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, had := r.offered[name]
	delete(r.offered, name)
	return had
}

// Offered returns everything this machine has available, ordered by name.
//
// AN EMPTY LIST IS A REAL STATE OF THIS BUILD and callers say so rather than printing an empty
// section — the rule `model.Names` already states for providers, kept here for both interfaces.
func (r *Registry) Offered() []Extension {
	r.mu.RLock()
	out := make([]Extension, 0, len(r.offered))
	for _, e := range r.offered {
		out = append(out, e)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Find returns the offered extension of that name.
//
// A false is a DETERMINED fact — this machine offers nothing by that name — and not an error and
// not "it failed to load". #18's `model.Lookup` draws the same line and says why.
func (r *Registry) Find(name string) (Extension, bool) {
	name = strings.TrimSpace(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.offered[name]
	return e, ok
}

// load resolves a registered extension's state, and it is the ONE place the four-way answer is
// decided for either interface.
//
// A registered extension this machine no longer offers has FAILED TO LOAD (criterion 7 and 8): the
// person registered something deliberately and the code behind it is gone, which is a broken
// installation and not an absence. Reporting it as [NotRegistered] would erase their act; reporting
// it as [Loaded] would be a lie; reporting it as [Undetermined] would hide a determined fact behind
// the third value.
func (r *Registry) load(name string, iface Interface) (State, string) {
	e, ok := r.Find(name)
	if !ok {
		return FailedToLoad, fmt.Sprintf(
			"%s is registered, and this machine offers no extension of that name — the "+
				"registration is intact and the code behind it is not present", name)
	}
	if got := e.Interface(); got != iface {
		// The record says one interface and the code says another. DETERMINED, and broken.
		return FailedToLoad, fmt.Sprintf(
			"%s is registered as a %s and the extension present implements %s instead",
			name, iface, got)
	}
	return stateFromLoad(e.Load())
}

// Shipped is implemented by an extension that this BUILD registered by shipping it — the built-in
// channel kinds of §3.1, and nothing else.
//
// # WHY THIS IS NOT A HOLE IN CRITERION 17
//
// Criterion 17: "an extension present on disk but not registered by a deliberate act is not
// registered". Shipping Teams and email IS the deliberate act, made once by this project rather
// than once per person — §3.1 says they ship built in, so demanding `omw ext register email` before
// email works would be this Issue overruling that one. What criterion 17 is about is an extension
// somebody DROPPED ON THE MACHINE: it is offered, it is listed, and it is [NotRegistered] until a
// person says otherwise. Nothing but the built-in channel kinds implements Shipped, and
// channels.TestOnlyTheBuiltInChannelKindsAreShipped holds that.
//
// It marks how an extension came to be registered. It does NOT reach [Entry] — criterion 6 wants
// one inventory, and a rendered built-in marker is how a listing splits back into two.
type Shipped interface {
	Extension
	// Shipped reports that this build registered it.
	Shipped() bool
}

// shipped reports whether e is registered by virtue of shipping.
func shipped(e Extension) bool {
	s, ok := e.(Shipped)
	return ok && s.Shipped()
}
