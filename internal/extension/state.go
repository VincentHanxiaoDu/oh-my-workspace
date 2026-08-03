package extension

import (
	"errors"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/refusal"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Interface is which of §2.5's two interfaces an extension implements.
//
// IT IS A LABEL ON ONE MECHANISM, NOT TWO MECHANISMS. Everything in this package that takes an
// Interface uses it to SAY which one an entry is, and nothing branches on it to decide how an entry
// behaves. The one place that would be tempting — [Entry.Render] — is where criterion 3 forbids it.
type Interface string

const (
	// Channel is the channel adapter interface (PRD §3.1).
	Channel Interface = "channel-adapter"
	// Model is the model provider interface (PRD §3.13).
	Model Interface = "model-provider"
)

// Interfaces enumerates the two, in a stable order. A function rather than a sentence in a comment
// so the CLI's help, the validation and the tests read one list.
func Interfaces() []Interface { return []Interface{Channel, Model} }

// Valid reports whether i is one of the two.
func (i Interface) Valid() bool {
	for _, k := range Interfaces() {
		if k == i {
			return true
		}
	}
	return false
}

// State is the ONE vocabulary of states an extension can be in, identical for both interfaces
// (criterion 3).
//
// THE ZERO VALUE IS UNDETERMINED, following [tri.Value] and for the same reason: a state nobody
// established must not read as "registered and fine", and must not read as "not registered" either.
// A struct field an error path never assigned is precisely a state that could not be determined.
type State int

const (
	// Undetermined means whether the extension loaded could not be worked out at all (criterion
	// 13). It is NOT a no. It is not the same answer as [NotRegistered], and an entry in this state
	// is still listed (criterion 14).
	Undetermined State = iota
	// NotRegistered means no deliberate act registered this extension. An extension present on
	// disk that nobody registered is in this state, and it is not ingesting and not serving a
	// model (criterion 17, §4.2).
	NotRegistered
	// Loaded means registered, and it loaded.
	Loaded
	// FailedToLoad means registered, and loading it raised. Criterion 7: its own answer, distinct
	// from NotRegistered and distinct from Loaded. Criteria 9 and 10 are what that distinction is
	// FOR — a channel adapter in this state is never reported as a quiet channel, and a model
	// provider in this state is never reported as no model configured.
	FailedToLoad
)

// States enumerates all four, for the tests that compare them pairwise.
func States() []State { return []State{Undetermined, NotRegistered, Loaded, FailedToLoad} }

// String renders the state as a person reads it. Four branches, four distinct non-empty sentences,
// and NONE of them is the empty string (criterion 21).
//
// TestTheFourStatesRenderPairwiseDistinctly compares every state against every other rather than
// each against its own literal: asserting each against a literal it was written next to passes just
// as happily after two of them have been edited into the same wording.
//
// The wording contains no extension name and no interface. That is what makes criterion 3's diff
// possible, and it is not a formatting preference: a state sentence that named the interface would
// be two vocabularies wearing one type.
func (s State) String() string {
	switch s {
	case Loaded:
		return "registered, and it loaded"
	case FailedToLoad:
		return "registered, and IT FAILED TO LOAD — this is not 'absent' and not 'present but idle'"
	case NotRegistered:
		return "not registered — nothing has registered it, so it is not running"
	default:
		return "whether it loaded " + tri.Undetermined.String() + " — this is NOT a report that it failed"
	}
}

// Loadedness projects the state onto the product's three-valued answer.
//
// FailedToLoad is a DETERMINED no: we asked and it did not load. Undetermined is the third value.
// NotRegistered is a determined no as well — an extension nobody registered has determinedly not
// loaded — but the two negatives do not render alike, which is criterion 7, and this function is
// deliberately not what any surface renders through.
func (s State) Loadedness() tri.Value {
	switch s {
	case Loaded:
		return tri.Yes
	case FailedToLoad, NotRegistered:
		return tri.No
	default:
		return tri.Undetermined
	}
}

// The refusals this package produces, as codes a caller reads without parsing prose.
var (
	// ErrNotOffered — nothing on this machine offers an extension by that name. Registering it
	// would leave a record naming code that does not exist, which is criterion 19's
	// half-registered entry.
	ErrNotOffered = &refusal.Error{
		Code: "extension-not-offered",
		Msg:  "no extension of that name is present on this machine",
	}
	// ErrAlreadyRegistered — a deliberate act already registered it. Registering again is refused
	// rather than silently replacing what is there.
	ErrAlreadyRegistered = &refusal.Error{
		Code: "extension-already-registered",
		Msg:  "an extension is already registered under that name",
	}
	// ErrNotRegistered — asked about an extension no deliberate act registered.
	ErrNotRegistered = &refusal.Error{
		Code: "extension-not-registered",
		Msg:  "no deliberate act has registered an extension under that name",
	}
	// ErrNoStore — there is no store, and registration is recorded in the store.
	ErrNoStore = &refusal.Error{
		Code: "extension-no-store",
		Msg:  "there is no store, and an extension registration is recorded in your store",
	}
	// ErrUnreadableRecord — a registration record is present and cannot be understood. Distinct
	// from ErrNotRegistered for the reason the channel store distinguishes unreadable from absent:
	// a listing with one damaged record must never report as a listing with one fewer extension.
	ErrUnreadableRecord = &refusal.Error{
		Code: "extension-record-unreadable",
		Msg:  "an extension registration record is present and cannot be read",
	}
	// ErrFailedToLoad is the INTERFACE-NEUTRAL failed-to-load code: the one a surface reporting
	// over a MIXED set of extensions uses.
	//
	// # WHY THIS EXISTS, AND WHAT ITS ABSENCE COST
	//
	// Each interface has its own failed-to-load code, and must: `channels.ErrAdapterFailedToLoad`
	// and `model.ErrProviderFailedToLoad` tell a caller WHICH subsystem is broken, and
	// `internal/model/extension.go` argues at length why they may not be collapsed — "sharing a
	// code would make the two situations indistinguishable to exactly the caller that has no
	// English to inspect".
	//
	// That is right for a code attached to ONE entry. It is wrong for a summary over several, and
	// this package shipped [ErrLoadUndetermined] — neutral — with no neutral twin beside it. So
	// `omw ext list`'s failure summary had no correct code to reach for and reached into `model`
	// for one, and a machine whose only broken extension was a CHANNEL ADAPTER printed
	// `code: model-provider-extension-failed-to-load`. A reviewer drove it and refused the pull
	// request.
	//
	// That is the Issue's own opening story — the Slack adapter that will not load — reported to
	// every machine reader as a model fault, which is worse than criterion 9's "no traffic on this
	// channel" because it sends the reader to the wrong subsystem entirely. It also breaks §2.5's
	// symmetry in the one place a script looks: the two interfaces share a state vocabulary, and
	// the summary line was picking a side.
	//
	// BOTH FACTS ARE TRUE AT ONCE, which is how the gap opened: per-ENTRY codes stay
	// interface-specific, and a summary over a MIXED set is neutral. This is the neutral one.
	ErrFailedToLoad = &refusal.Error{
		Code: "extension-failed-to-load",
		Msg:  "at least one registered extension failed to load",
	}

	// ErrLoadUndetermined is what an extension's Load returns when it could not establish whether
	// it loaded — as opposed to establishing that it did not.
	//
	// It is the neutral twin of [ErrFailedToLoad], and it was here first — see that comment for
	// what having only one of the pair cost.
	//
	// IT IS THE WHOLE OF CRITERION 13. Without a distinct value here, [Registry.load] would have a
	// (bool, error) whose error becomes a negative, and the fourth rendering would be unreachable.
	ErrLoadUndetermined = &refusal.Error{
		Code: "extension-load-undetermined",
		Msg:  "whether this extension loads could not be determined",
	}
)

// allErrors is every refusal this package defines, for the test that asserts they are pairwise
// distinguishable by code and by message.
var allErrors = []*refusal.Error{
	ErrNotOffered, ErrAlreadyRegistered, ErrNotRegistered, ErrNoStore,
	ErrUnreadableRecord, ErrFailedToLoad, ErrLoadUndetermined,
}

// stateFromLoad turns an [Extension.Load] outcome into a state, and it is the only place that
// mapping is written.
//
// A nil error is Loaded. An error wrapping [ErrLoadUndetermined] is Undetermined. ANY OTHER ERROR
// IS FailedToLoad. That last clause is the safe default on purpose: an extension whose failure this
// build does not recognise has failed, and calling it undetermined would let a broken extension
// hide behind the third value — the mirror image of the collapse §4.3 forbids.
func stateFromLoad(err error) (State, string) {
	switch {
	case err == nil:
		return Loaded, ""
	case errors.Is(err, ErrLoadUndetermined):
		return Undetermined, detailOf(err)
	default:
		return FailedToLoad, detailOf(err)
	}
}

// detailOf is the reason as a person reads it, and it is NEVER empty for a non-nil error
// (criteria 8 and 21: a failed-to-load entry carries non-empty failure detail).
func detailOf(err error) string {
	if err == nil {
		return ""
	}
	if d := strings.TrimSpace(err.Error()); d != "" {
		return d
	}
	return "the extension reported a failure with no message, which is itself all that is known"
}
