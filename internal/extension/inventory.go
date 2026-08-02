package extension

import (
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
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
func (s Summary) AllLoaded() bool { return s.Failed == 0 && s.Undetermined == 0 }

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

// Find returns the entry for one name out of an inventory.
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
