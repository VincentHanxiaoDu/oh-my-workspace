package extension

import (
	"fmt"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Inventory is THE listing — one listing, both interfaces, built-ins included (criteria 2, 6, 14).
//
// # WHAT IS IN IT
//
//   - Every extension a deliberate act registered, whatever state it is in, INCLUDING the ones that
//     failed to load and the ones whose state could not be determined (criterion 14: "an entry that
//     has never done anything is a fact worth seeing, not an absence").
//   - Every extension this build ships — the built-in Teams and email channel kinds — sorted in
//     among the rest, in the same [Entry] shape (criterion 6).
//   - Every extension this machine OFFERS that nobody registered, as [NotRegistered] (criterion
//     17). Present on disk is not registered, and the way a person learns that is by seeing it said.
//
// # ONE FAILURE NEVER SUPPRESSES ANOTHER (criterion 11)
//
// There is no early return in the loop. Every extension is resolved independently and every result
// is appended, so a registry full of broken extensions produces a full listing of broken
// extensions rather than the first one and silence.
//
// # A MISSING STORE IS NOT AN EMPTY INVENTORY
//
// With no store there are no registrations, and that is a determined fact about registrations — but
// this build still ships Teams and email and still offers whatever is present. So the built-in and
// offered entries are produced anyway, and the caller is told separately that there is no store.
// Returning an empty list would report a person with no store as a person with no channels.
func Inventory(s *store.Store, r *Registry) ([]Entry, error) {
	l := Read(s, r)
	return l.Entries, l.readErr
}

// Read is [Inventory] as a [Listing] — the entries AND whether they are all of them.
//
// # PREFER THIS. Inventory's SIGNATURE IS THE DEFECT THAT GOT THIS BRANCH REFUSED
//
// `Inventory` returns `([]Entry, error)`, and a caller may write `entries, _ :=` — which is exactly
// what the control API did, and what `omw ext show` did. The error is the sentence "these may not
// be all of them", and dropping it turns an incomplete read into a confident listing. A `Listing`
// carries the incompleteness INSIDE the value that gets rendered and summarised, so there is no
// separate thing to drop.
func Read(s *store.Store, r *Registry) Listing {
	entries, err := inventory(s, r)
	l := Listing{Entries: entries, readErr: err}
	if err != nil {
		l.Incomplete = detailOf(err)
	}
	return l
}

// Listing is a whole inventory together with whether it is a WHOLE inventory.
//
// # WHY THE TWO ARE ONE VALUE
//
// Every surface that reports extensions must answer two questions, and the second is easy to lose:
// what is registered, and did we manage to read all of it. This branch lost it twice — the control
// API wrote `entries, _ :=` and dropped the warning the CLI printed, and the CLI's own summary line
// went on saying "every registered extension loaded" over an inventory it had failed to read.
//
// Both are the same defect: A DETERMINED ANSWER REPORTED FROM AN INCOMPLETE READ. The repair is not
// to remember harder in two places. It is for the incompleteness to travel inside the thing being
// rendered, so a surface cannot render the entries without it.
type Listing struct {
	// Entries is every extension, in listing order.
	Entries []Entry `json:"entries"`
	// Incomplete is why this listing may not be all of them, and is EMPTY WHEN IT IS COMPLETE.
	// A non-empty Incomplete does not mean the entries are wrong; it means there may be more.
	Incomplete string `json:"incomplete,omitempty"`

	// readErr is the error behind Incomplete, for a caller that wants the code. Unexported so it
	// cannot be serialised and cannot disagree with Incomplete.
	readErr error
}

// Complete reports whether the whole inventory was read.
func (l Listing) Complete() bool { return l.Incomplete == "" }

// Err is why the inventory could not be read in full, or nil.
func (l Listing) Err() error { return l.readErr }

// Summary counts the states, and CARRIES THE INCOMPLETENESS FORWARD so that nothing computed from
// it can claim completeness.
func (l Listing) Summary() Summary {
	sum := Summarise(l.Entries)
	sum.Incomplete = l.Incomplete
	return sum
}

// Render is the ONE rendering of a whole listing, and it is what the CLI and the control API both
// print (criterion 20).
//
// THE INCOMPLETENESS IS PART OF IT, above the entries, so a surface that prints this cannot print
// the entries without it.
func (l Listing) Render() string {
	var b strings.Builder
	if !l.Complete() {
		b.WriteString("extensions: WHICH EXTENSIONS ARE REGISTERED " +
			tri.Undetermined.String() + " — what follows may not be all of them.\n")
		b.WriteString("  " + l.Incomplete + "\n")
		b.WriteString("  This is NOT a report that none is registered.\n")
	}
	b.WriteString(Render(l.Entries))
	return b.String()
}

func inventory(s *store.Store, r *Registry) ([]Entry, error) {
	if r == nil {
		r = Default
	}

	entries := map[string]Entry{}
	// ORDER OF THE THREE PASSES IS LOAD-BEARING. Registrations are resolved last, so a person's
	// deliberate act is what the listing reports about an extension they also happen to be offered.
	for _, e := range r.Offered() {
		name := e.Name()
		if shipped(e) {
			// SHIPPED IS REGISTERED (§3.1). See [Shipped] for why this is not a hole in
			// criterion 17.
			state, detail := stateFromLoad(e.Load())
			entries[name] = newEntry(name, e.Interface(), state, detail)
			continue
		}
		entries[name] = newEntry(name, e.Interface(), NotRegistered,
			"this machine offers it and no deliberate act has registered it; "+
				"'omw ext register "+name+"' registers it, and nothing does that for you")
	}

	var readErr error
	if s != nil {
		regs, err := Registered(s)
		if err != nil {
			// The registration list itself could not be read. That is not "no extensions are
			// registered": it is a fact we failed to establish, so it is reported as such and the
			// built-ins are still listed.
			readErr = err
		}
		for _, reg := range regs {
			entries[reg.Name] = entryFor(r, reg)
		}
	}

	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Interface < out[j].Interface
	})
	return out, readErr
}

// entryFor resolves one registration into an entry.
func entryFor(r *Registry, reg Registration) Entry {
	if reg.readErr != nil {
		// UNDETERMINED, NOT DROPPED AND NOT "FAILED" (criteria 13, 14). A registration record we
		// could not read has not told us the extension is broken; it has told us nothing. Rendering
		// it as failed-to-load would be a confident claim built out of our own inability to read a
		// file.
		return newEntry(reg.Name, reg.Interface, Undetermined, detailOf(reg.readErr))
	}
	state, detail := r.load(reg.Name, reg.Interface)
	return newEntry(reg.Name, reg.Interface, state, detail)
}

// Summary is what a whole inventory amounts to, and it is what criterion 12's exit code is computed
// from.
type Summary struct {
	// Incomplete is why the inventory this counts may not be all of it, empty when it is complete.
	//
	// IT IS HERE SO THAT [Summary.AllLoaded] CANNOT SAY YES OVER A PARTIAL READ. A count taken from
	// an inventory that failed to read is a count of what happened to be readable, and a summary
	// built from it that claims "every registered extension loaded" is asserting something about
	// records it never saw.
	Incomplete string
	// Total is how many entries there are.
	Total int
	// Loaded, Failed, NotRegistered and Undetermined count the four states.
	Loaded, Failed, NotRegistered, Undetermined int
}

// Summarise counts the states in an inventory.
func Summarise(entries []Entry) Summary {
	var s Summary
	s.Total = len(entries)
	for _, e := range entries {
		switch e.Resolved() {
		case Loaded:
			s.Loaded++
		case FailedToLoad:
			s.Failed++
		case NotRegistered:
			s.NotRegistered++
		default:
			s.Undetermined++
		}
	}
	return s
}

// AllLoaded reports whether every REGISTERED extension loaded.
//
// Not-registered entries are excluded deliberately: criterion 12 asks the listing to distinguish
// "every registered extension loaded" from "at least one failed to load", and an extension nobody
// registered has not failed at anything. Counting it as a failure would make `omw ext list` exit
// non-zero on a machine that merely has an extension lying around unregistered, which criterion 17
// says is a normal state and not a fault.
func (s Summary) AllLoaded() bool {
	// AN INCOMPLETE READ CAN NEVER ANSWER YES. Whether every registered extension loaded is a claim
	// about every registered extension, and we do not know what they all are.
	return s.Incomplete == "" && s.Failed == 0 && s.Undetermined == 0
}

// Render is the one rendering of a whole inventory.
//
// AN EMPTY INVENTORY IS SAID OUT LOUD, not printed as an empty section. It is a real state and an
// empty section reads like a rendering bug rather than an answer — the rule `cli.usage` and
// `model.Names`'s caller already follow.
func Render(entries []Entry) string {
	if len(entries) == 0 {
		return "extensions: this machine has none — none shipped, none offered, none registered.\n"
	}
	var b strings.Builder
	b.WriteString("extensions:\n")
	for _, e := range entries {
		for _, line := range strings.Split(strings.TrimRight(e.Render(), "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// Find returns the entry for one name out of an inventory, WHATEVER INTERFACE IT IMPLEMENTS.
//
// # USE [FindAs] IF YOU CARE WHICH INTERFACE IT IS, AND YOU ALMOST ALWAYS DO
//
// This is the right function for a surface that is showing a person whatever is registered under a
// name they typed — `omw ext show slack` should describe slack, not tell them slack does not exist
// because they guessed the wrong interface.
//
// It is the WRONG function for a caller asking "is my model provider loaded", and that mistake got
// this branch refused. See [FindAs].
//
// AN ABSENT NAME COMES BACK AS A [NotRegistered] ENTRY, not as a false and not as a zero Entry.
// Criterion 21: no extension state is ever rendered as an empty string or an absent line, and the
// way a caller ends up printing one is by getting a zero value back and printing it.
func Find(entries []Entry, name string) Entry {
	name = strings.TrimSpace(name)
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	return newEntry(name, "", NotRegistered,
		"nothing on this machine offers it and no deliberate act has registered it")
}

// FindAs returns the entry for one name AS A GIVEN INTERFACE, and reports an extension of that name
// implementing the OTHER interface as not registered — because, as the thing that was asked for, it
// is not.
//
// # THE DEFECT THIS EXISTS FOR, AND WHY THIS ISSUE PRODUCED IT
//
// `model.Readiness` looked the chosen provider up with [Find], by name alone. Two documented
// commands, no file editing:
//
//	omw ext register slack     # registered: slack (channel-adapter), registered and it loaded
//	omw model use slack        # recorded — model.Use accepts any name, by Issue #18's design
//
// and the product then said "the model provider slack is chosen and its extension loaded". A
// confident claim that a MODEL extension loaded, when what loaded is a channel adapter. §4.3 from
// the direction that is easier to miss: not a determined thing rendered as undetermined, but an
// undetermined thing rendered as a confident yes.
//
// [Registry.load] does check the interface, and correctly — but only against the interface the
// record was registered UNDER, and a channel adapter registered as a channel adapter is legitimately
// [Loaded]. Nothing was wrong with that check; the caller had simply stopped asking the question.
//
// # THIS IS THE COST OF CRITERION 3, AND IT IS WORTH PAYING
//
// The two interfaces share a state vocabulary and render identically ON PURPOSE (§2.5, criterion 3).
// That is the whole point of this Issue — and it means the INTERFACE is the only thing left
// distinguishing them, so a lookup that drops it has nothing left to be right about. The sameness
// is correct; this is the one place the distinction still has to survive it, and it survives by
// being a parameter a caller cannot forget rather than a check a caller must remember.
//
// The mismatch is SAID rather than silently reported as absence, because a person who typed
// `omw model use slack` needs to know slack exists and is the wrong kind of thing — "no such
// provider" would send them looking for a typo.
func FindAs(entries []Entry, name string, iface Interface) Entry {
	name = strings.TrimSpace(name)
	var wrongInterface *Entry
	for i, e := range entries {
		if e.Name != name {
			continue
		}
		if e.Interface == iface {
			return e
		}
		wrongInterface = &entries[i]
	}
	if wrongInterface != nil {
		// SAID, AND ACTIONABLE FOR THE SITUATION THE PERSON IS ACTUALLY IN. "Not registered" alone
		// would send somebody who HAS registered it round a loop looking for a typo.
		return newEntry(name, iface, NotRegistered, fmt.Sprintf(
			"an extension called %s IS registered on this machine and it implements %s, not %s — "+
				"so as a %s it is not registered, and registering it again will not help",
			name, wrongInterface.Interface, iface, iface))
	}
	return newEntry(name, iface, NotRegistered, fmt.Sprintf(
		"nothing on this machine offers a %s called %s, and no deliberate act has registered one; "+
			"'omw ext register %s' registers it once it is present", iface, name, name))
}
