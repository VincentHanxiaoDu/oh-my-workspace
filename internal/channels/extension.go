// The channel adapter interface, plugged into the ONE extension mechanism (PRD §2.5, Issue #21).
//
// # WHY THIS IS IN internal/channels AND NOT IN internal/extension
//
// §2.5: "Channel adapters and model providers are the same mechanism with two interfaces." The
// MECHANISM — registering, the inventory, the four states, the wording, the exit code — is
// `internal/extension`, once, for both. What is here is the adapter that presents a channel kind as
// an extension, and it is here because it cannot be there: this package imports `internal/daemon`
// to register its ingestion loop, and `internal/daemon` imports `internal/extension` to serve
// extension state over the control API. An `internal/extension` that imported this package would
// close daemon → extension → channels → daemon, and Go would refuse to build it.
//
// So each interface plugs ITSELF in, from an init, exactly as `loop.go` registers this package's
// background work with the daemon and as `model.Register` registers a provider. Nothing about the
// shared half is duplicated here — there is no State, no Entry and no rendering in this file, and
// TestThisFileRestatesNoneOfTheSharedMechanism holds that.
package channels

import (
	"fmt"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/refusal"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Extension presents one channel kind as an [extension.Extension].
//
// It also satisfies [extension.Shipped], because §3.1 ships Teams and email built in: the
// deliberate act that registered them was made once by this project rather than once per person.
// See [extension.Shipped] for why that is not a hole in criterion 17.
type Extension struct{ Kind Kind }

// Name is the kind's own word — `teams`, `email` — which is what a person types.
func (e Extension) Name() string { return string(e.Kind) }

// Interface is the channel adapter interface.
func (e Extension) Interface() extension.Interface { return extension.Channel }

// Shipped: this build registered it by shipping it (§3.1).
func (e Extension) Shipped() bool { return true }

// Load reports whether this build has the kind.
//
// # A MISSING TRANSPORT IS NOT A FAILED LOAD, AND THAT DISTINCTION IS DELIBERATE
//
// This build has no Graph client and no IMAP client — `adapter.go` says so at length, and
// [Builtin] returns [ErrUnreachable] naming what is missing. It would be easy to call that a failed
// load here, and it would be wrong twice: the built-in kinds would then sit permanently in
// criterion 7's failure state, drowning the real failures criterion 11 asks be visible next to
// them; and a person CAN connect either kind, so reporting the adapter as unloadable would
// contradict the connect command that just succeeded.
//
// What loads is the kind. What is unreachable is the service, and ingestion says so at ingestion
// time, in [Ingestion.RenderOutcome], where it is a fact about an attempt rather than about a build.
func (e Extension) Load() error {
	if !e.Kind.Valid() {
		return fmt.Errorf("this build has no channel kind called %q; it has: %s", string(e.Kind), kindList())
	}
	return nil
}

// Adapter is the channel adapter for a connection, once loaded. It is [Builtin] — this file adds no
// second construction path.
func (e Extension) Adapter(c Connection) (Adapter, error) { return Builtin(c) }

// AdapterExtension is what `extension.ChannelFactory` requires of a channel-interface extension: an
// [extension.Extension] that can produce an [Adapter].
//
// It is declared HERE rather than in `internal/extension` for the same import reason the rest of
// this file is here. A registered third-party channel adapter implements it.
type AdapterExtension interface {
	extension.Extension
	Adapter(c Connection) (Adapter, error)
}

func init() {
	// TEAMS AND EMAIL ENTER THE ONE INVENTORY HERE (criterion 6). Not as a special section the
	// listing adds, not as "the built-ins" printed above "the extensions" — as ordinary offered
	// extensions, sorted in among everything else by name.
	for _, k := range Kinds() {
		extension.Default.Offer(Extension{Kind: k})
	}
}

// ErrAdapterFailedToLoad is what ingestion is told about a channel whose adapter did not load.
//
// It is its OWN CODE, distinct from [ErrUnreachable], because the two are different facts a person
// acts on differently: an unreachable channel is a network or a credential and they wait or sign in
// again; an adapter that failed to load is a broken installation and no amount of waiting fixes it.
var ErrAdapterFailedToLoad = &refusal.Error{
	Code: "channel-adapter-failed-to-load",
	Msg:  "this channel's adapter failed to load, so the channel was not attempted",
}

// ErrAdapterLoadUndetermined is the same question's third answer (§4.3).
var ErrAdapterLoadUndetermined = &refusal.Error{
	Code: "channel-adapter-load-undetermined",
	Msg:  "whether this channel's adapter loads could not be determined, so the channel was not attempted",
}

// ErrAdapterNotRegistered is what ingestion is told about a channel whose adapter nobody
// registered.
//
// IT IS NOT ErrAdapterFailedToLoad, AND A TEST CAUGHT THAT IT WAS. Wrapping the failed-to-load
// refusal for this case produced the sentence "no channel adapter called email is registered on
// this machine … : this channel's adapter failed to load", which tells a person two different
// things about their machine in one line. Criterion 7 asks that not-registered and failed-to-load
// be distinguishable; a shared base error makes them share a code, which is the machine-readable
// half of the same collapse.
var ErrAdapterNotRegistered = &refusal.Error{
	Code: "channel-adapter-not-registered",
	Msg:  "no adapter for this channel is registered, so the channel was not attempted",
}

// ExtensionFactory is the [Factory] ingestion runs with once extensions exist, and it is
// CRITERION 9.
//
// # THE FAILURE IT EXISTS TO PREVENT
//
// "A channel adapter that failed to load never causes ingestion to report 'no traffic on this
// channel'. A test that breaks a registered adapter and then asks what channels are ingesting sees
// that adapter reported as failed, not as quiet."
//
// The way a product acquires that bug is not by deciding to lie. It is that constructing the
// adapter fails, somebody writes `if err != nil { continue }` because a broken adapter obviously
// cannot be asked for messages, and the channel then reports zero messages — which renders exactly
// like a quiet channel. [Ingest] was built with that hole already closed: a factory error becomes
// [OutcomeUnreachable] carrying the reason, and its own comment says a skipped channel "would
// report as a run that found nothing". So the whole of criterion 9 on this side is that the LOAD
// STATE MUST ARRIVE AT THE FACTORY AS AN ERROR rather than being swallowed before it gets there,
// and that is this function. It never returns `nil, nil`.
//
// # IT RESOLVES THE STATE PER CALL AND NOT ONCE
//
// An extension that broke between two ingestion passes must be reported as broken on the second
// pass. Caching the inventory when the factory is constructed would make the daemon report a build
// it no longer has.
func ExtensionFactory(s *store.Store, r *extension.Registry) Factory {
	if r == nil {
		r = extension.Default
	}
	return func(c Connection) (Adapter, error) {
		// FindAs, NOT Find. A model provider registered under a channel kind's name must not be
		// mistaken for that channel's adapter. The type assertion below would catch it, but only
		// after this had already resolved it to Loaded — and it would then report "is registered as
		// a channel adapter and does not implement one", which is not what happened. Asking for the
		// interface up front makes the state and the sentence both right. See extension.FindAs.
		e := extension.FindAs(extension.Read(s, r).Entries, string(c.Kind), extension.Channel)
		switch e.Resolved() {
		case extension.Loaded:
			ext, ok := r.Find(e.Name)
			if !ok {
				return nil, refusal.Refusedf(ErrAdapterFailedToLoad,
					"%s loaded a moment ago and this machine no longer offers it", e.Name)
			}
			ae, ok := ext.(AdapterExtension)
			if !ok {
				return nil, refusal.Refusedf(ErrAdapterFailedToLoad,
					"%s is registered as a channel adapter and does not implement one", e.Name)
			}
			return ae.Adapter(c)
		case extension.FailedToLoad:
			return nil, refusal.Refusedf(ErrAdapterFailedToLoad,
				"the %s channel adapter FAILED TO LOAD, so this channel was not attempted; this "+
					"is NOT a report that nothing arrived on it: %s", c.Kind, e.Detail)
		case extension.Undetermined:
			return nil, refusal.Refusedf(ErrAdapterLoadUndetermined,
				"whether the %s channel adapter loads could not be determined, so this channel "+
					"was not attempted; this is NOT a report that nothing arrived on it: %s",
				c.Kind, e.Detail)
		default:
			return nil, refusal.Refusedf(ErrAdapterNotRegistered,
				"no channel adapter called %s is registered on this machine, so this channel was "+
					"not attempted; this is NOT a report that nothing arrived on it", c.Kind)
		}
	}
}
