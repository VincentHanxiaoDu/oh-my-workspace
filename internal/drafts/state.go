// Issue #9: a draft's state, and the fact that "resting because you have not published it" is not
// the same fact as any of the ways a draft can be stuck.
//
// THE STATES EXIST BECAUSE OF ONE FAILURE. A person chooses `review`, has no model, and their
// drafts sit there looking exactly like a `manual` person's drafts. Nothing is wrong on the screen;
// nothing is going anywhere either. The only way a person can tell those apart is if the draft
// itself carries WHY it is sitting there — so a draft blocked on a missing prerequisite is a
// different state from a draft the person simply has not published, and both are different from a
// draft a review refused and from a draft whose review could not be completed.
package drafts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// State is where a draft stands.
type State string

const (
	// StateDrafted — written, resting in the outbox, awaiting the person. The `manual` resting
	// state, and the state of every newly written draft.
	StateDrafted State = "drafted"
	// StateBlocked — the mode the person chose cannot run because something is missing. The draft
	// is in the outbox and it is NOT merely awaiting them.
	StateBlocked State = "blocked-on-a-missing-prerequisite"
	// StateReviewUndetermined — a review was attempted and could not be completed. Not a pass.
	StateReviewUndetermined State = "review-could-not-be-completed"
	// StateRefused — a review was completed and refused the draft.
	StateRefused State = "refused-by-review"
	// StateCleared — a review was completed and passed the draft. Still in the outbox: passing the
	// person's own gate is not the transfer, and the transfer is Issue #10.
	StateCleared State = "cleared-by-review"
	// StateLeaving — the mode acted and the draft has been handed onward. It is no longer in the
	// `manual` resting state. What happens in flight is Issue #10's.
	StateLeaving State = "handed-to-publication"
)

// stateFileName sits inside the draft's own directory, beside its revisions.
//
// The dot prefix is not decoration: [Outbox.Drafts] lists directories and [Outbox.numbers] counts
// files ending in the revision suffix, so this file is invisible to both. A state file that read as
// a revision would show up in the person's timeline as a version they never wrote.
const stateFileName = ".state"

type stateFile struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// StateReport is what the client knows about one draft's state.
type StateReport struct {
	// Known is Yes when the state was established, Undetermined when it could not be.
	Known tri.Value
	// Exists is Yes when there is such a draft, No when there is not, Undetermined when that
	// itself could not be established.
	Exists tri.Value
	State  State
	Detail string
	Why    string
}

// SetState records where a draft stands. It does not create the draft.
func (o *Outbox) SetState(id hub.NoteID, st State, detail string) error {
	dir, err := o.pathFor(id)
	if err != nil {
		return err
	}
	if _, serr := os.Stat(dir); serr != nil {
		return hub.Refusedf(ErrNoSuchDraft, "%q", string(id))
	}
	body, err := json.Marshal(stateFile{State: string(st), Detail: detail})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stateFileName), body, 0o600)
}

// StateOf reports where a draft stands.
//
// A draft with no state file is [StateDrafted] — a REAL VALUE, not an absence. Every draft that
// exists is somewhere, and "written and resting" is where a draft is the moment it is written.
// A state file that exists and cannot be read is undetermined, and is emphatically not `drafted`:
// reporting a draft as resting when the record of what happened to it is unreadable is the
// specific lie this file is built to prevent.
func (o *Outbox) StateOf(id hub.NoteID) StateReport {
	dir, err := o.pathFor(id)
	if err != nil {
		return StateReport{Known: tri.Undetermined, Exists: tri.Undetermined, Why: err.Error()}
	}
	if _, serr := os.Stat(dir); serr != nil {
		if errors.Is(serr, os.ErrNotExist) {
			return StateReport{Known: tri.Yes, Exists: tri.No}
		}
		return StateReport{Known: tri.Undetermined, Exists: tri.Undetermined, Why: serr.Error()}
	}
	body, rerr := os.ReadFile(filepath.Join(dir, stateFileName))
	if rerr != nil {
		if errors.Is(rerr, os.ErrNotExist) {
			return StateReport{Known: tri.Yes, Exists: tri.Yes, State: StateDrafted}
		}
		return StateReport{Known: tri.Undetermined, Exists: tri.Yes, Why: rerr.Error()}
	}
	var sf stateFile
	if jerr := json.Unmarshal(body, &sf); jerr != nil {
		return StateReport{Known: tri.Undetermined, Exists: tri.Yes, Why: "the draft's state record is damaged: " + jerr.Error()}
	}
	return StateReport{Known: tri.Yes, Exists: tri.Yes, State: State(sf.State), Detail: sf.Detail}
}

// InOutbox reports whether the draft is still in the outbox — which is every state this capability
// can put it in, since nothing here transfers anything. Issue #10 owns leaving.
func (r StateReport) InOutbox() tri.Value {
	if r.Exists != tri.Yes {
		return r.Exists
	}
	return tri.Yes
}

// Render is the one rendering of a draft's state. Each branch says what a person needs to do next,
// because the difference between these states is precisely what a person would otherwise have to
// guess at.
func (r StateReport) Render() string {
	switch {
	case r.Exists == tri.No:
		return "state: no such draft in this outbox"
	case r.Known != tri.Yes:
		s := "state: " + tri.Undetermined.String() + " — this draft is here and where it stands could not be read"
		if r.Why != "" {
			s += " (" + r.Why + ")"
		}
		return s
	}
	base := "state: " + string(r.State)
	switch r.State {
	case StateDrafted:
		base += " — in your outbox, awaiting you; nothing is outstanding and nothing is in flight"
	case StateBlocked:
		base += " — in your outbox and NOT merely awaiting you: something the mode you chose needs is missing"
	case StateReviewUndetermined:
		base += " — in your outbox; the review was attempted and could not be completed, which is not a pass"
	case StateRefused:
		base += " — in your outbox; the review was completed and refused it"
	case StateCleared:
		base += " — in your outbox; the review was completed and passed it"
	case StateLeaving:
		base += " — no longer awaiting you; the mode you chose acted on it"
	default:
		return "state: " + tri.Undetermined.String() + " — the recorded state " + quoteState(string(r.State)) + " is not one this build knows"
	}
	if r.Detail != "" {
		base += "\n  " + r.Detail
	}
	return base
}

func quoteState(s string) string { return "\"" + s + "\"" }
