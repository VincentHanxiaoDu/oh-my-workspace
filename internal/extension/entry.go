package extension

import (
	"fmt"
	"strings"
)

// Entry is one extension in the one inventory, whichever interface it implements.
//
// # THERE IS NO FIELD A CREDENTIAL CAN GO IN (criterion 22)
//
// Name, Interface, State, Detail. Issue #18's `model.View` made this structural rather than
// careful — "a struct with nowhere to put one … the difference between a rule and a guarantee" —
// and this preserves the property on the surface that lists BOTH interfaces, which is the one a
// person greps after configuring a provider with a real key. TestNoEntryFieldCanHoldACredential
// asserts the shape by reflection, so it fails on a field ADDED later rather than on one instance
// of a leak.
//
// # THERE IS NO built-in FIELD EITHER, AND THAT IS CRITERION 6
//
// "So a person sees one inventory of what can reach them, not 'the built-ins' plus 'the
// extensions'." Teams and email are entries of exactly this shape, sitting in exactly this listing,
// sorted in among everything else. A `BuiltIn bool` would be a field whose only use is to split the
// listing back into the two halves the criterion exists to forbid — and, rendered, it would break
// criterion 3's diff the first time a built-in failed to load.
type Entry struct {
	// Name is what the person typed to register it, and what they type to ask about it.
	Name string `json:"name"`
	// Interface is which of §2.5's two this implements.
	Interface Interface `json:"interface"`
	// State is one of the four, from the one vocabulary.
	//
	// It crosses the wire as its own rendering rather than as an integer, for the reason
	// `model.View`'s tri fields do: the zero value is meaningful, and a future reordering of the
	// constants would silently reinterpret every number already sent.
	State State `json:"-"`
	// StateText is State as the wire and the control API carry it.
	StateText string `json:"state"`
	// Detail is why, for a state that has a why. Built from load failures and from unreadable
	// records; NEVER from a credential, and there is no code path in this package from
	// `model.Config`'s secret to here.
	Detail string `json:"detail,omitempty"`
}

// newEntry builds an entry with StateText and State in agreement. It is the only constructor, so
// the two cannot drift.
func newEntry(name string, iface Interface, state State, detail string) Entry {
	return Entry{
		Name:      name,
		Interface: iface,
		State:     state,
		StateText: state.String(),
		Detail:    detail,
	}
}

// Resolved reads the wire text back into a State, so a consumer of an Entry that crossed the
// control API compares values PAIRWISE instead of matching strings against literals it spelled
// itself.
//
// A word this build does not know is, precisely, a state it could not determine.
func (e Entry) Resolved() State {
	for _, s := range States() {
		if s.String() == e.StateText {
			return s
		}
	}
	return Undetermined
}

// Render is the ONE rendering of an entry, and it is what every surface prints — the CLI and the
// control API both (criterion 20, §4.3: "The control API and the CLI report the same state").
//
// # IT TAKES NO BRANCH ON THE INTERFACE, AND THAT IS CRITERION 3
//
// "A test that captures a failed-to-load channel adapter line and a failed-to-load model provider
// line and diffs them, with names normalised, finds no difference."
//
// The way to fail that test is not to write two renderings on purpose. It is to write one, and then
// for somebody to improve the channel wording six months later because they were reading the
// channel code that day. So there is nowhere to put the improvement: the name and the interface are
// the only values interpolated, and everything else comes from [State.String], which does not know
// which interface it is describing.
//
// # IT IS NEVER EMPTY (criterion 21)
//
// Every state produces a name line, an interface line and a state line, all non-empty, including
// [Undetermined]. The detail line is omitted when there is nothing to say, which is not the same
// as an entry rendering blank — an entry always has three lines.
func (e Entry) Render() string {
	var b strings.Builder
	name := e.Name
	if strings.TrimSpace(name) == "" {
		// AN UNNAMED ENTRY IS STILL AN ENTRY. Printing "" here would be the empty listing row
		// criterion 21 forbids, and dropping it would be criterion 14's dropped registration.
		name = "(an extension whose name could not be read)"
	}
	fmt.Fprintf(&b, "%s\n", name)
	iface := string(e.Interface)
	if iface == "" {
		iface = "(which interface it implements could not be read)"
	}
	fmt.Fprintf(&b, "  interface:  %s\n", iface)
	fmt.Fprintf(&b, "  state:      %s\n", e.StateText)
	if e.Detail != "" {
		fmt.Fprintf(&b, "  detail:     %s\n", e.Detail)
	}
	return b.String()
}
