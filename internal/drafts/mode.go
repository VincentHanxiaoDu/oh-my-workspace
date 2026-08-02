// Issue #9: how a draft leaves the outbox is the person's choice, and the client says which choice
// is in effect without ever guessing.
//
// THE THREE MODES ARE A CLOSED SET, and this file is the only place they are spelled. A fourth
// spelling somewhere else is how `review` quietly becomes `manual` on a machine where a typo went
// unnoticed: a mode name that is not one of the three is a refusal here, never a fallback.
package drafts

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Mode is how drafts leave the outbox.
type Mode string

const (
	// ModeManual — drafts go nowhere until the person says so. This is the default, and it is a
	// REAL VALUE: with nothing ever set, the effective mode is this, not blank and not absent.
	ModeManual Mode = "manual"
	// ModeReview — each draft is checked, on this machine, against rules the person wrote, using
	// their own model and key.
	ModeReview Mode = "review"
	// ModeAuto — no gate; the draft is handed onward as soon as it is written.
	ModeAuto Mode = "auto"
)

// DefaultMode is what is in effect when the person has never chosen (criterion 5).
const DefaultMode = ModeManual

// modes is the vocabulary, in the order a person should be offered it.
var modes = []Mode{ModeManual, ModeReview, ModeAuto}

// Modes returns the three modes. There is no fourth.
func Modes() []Mode { return append([]Mode(nil), modes...) }

// ErrUnknownMode — a mode name that is not one of the three.
var ErrUnknownMode = &hub.Error{
	Code: "unknown-mode",
	Msg:  "that is not a publication mode; the modes are manual, review and auto",
}

// ErrModeUnreadable — the person's recorded choice is on disk and could not be read.
//
// SEPARATE FROM "never chosen" on purpose. An unreadable choice rendered as "never chosen" would
// report `manual` to somebody who chose `auto`, which is §4.3's collapse done to the one setting
// that decides whether their writing leaves the machine.
var ErrModeUnreadable = &hub.Error{
	Code: "mode-unreadable",
	Msg:  "the recorded publication mode could not be read, so which mode is in effect is not known",
}

// ParseMode accepts exactly the three names, and nothing else.
//
// It does not lowercase, trim to taste, or accept prefixes. "Manual" and "man" are not the mode
// `manual`; they are a person who typed something else, and telling them so costs one retype while
// guessing costs them the thing they were guarding against.
func ParseMode(s string) (Mode, error) {
	for _, m := range modes {
		if s == string(m) {
			return m, nil
		}
	}
	names := make([]string, 0, len(modes))
	for _, m := range modes {
		names = append(names, string(m))
	}
	sort.Strings(names)
	return "", hub.Refusedf(ErrUnknownMode, "%q is not one of %s", s, strings.Join(names, ", "))
}

// settingsKind is the store record kind this capability owns.
const settingsKind = store.Kind("outbox")

// modeRecordID and rulesRecordID are separate records so that an unreadable set of rules does not
// take the mode down with it, and vice versa. Two facts, two answers.
const (
	modeRecordID  = "publication-mode"
	rulesRecordID = "review-rules"
)

type modeRecord struct {
	Mode string `json:"mode"`
}

// ModeSetting is everything the client knows about which mode is in effect.
//
// It is a struct and not a Mode because "manual because you chose it" and "manual because nobody
// has chosen anything" are different facts about the person, and criterion 19 requires that a
// third state — "I could not read your choice" — is neither of them.
type ModeSetting struct {
	// Known is Yes when the effective mode was established, Undetermined when it could not be.
	// It is never No: there is always an effective mode when we can read at all.
	Known tri.Value
	// Mode is the effective mode, meaningful when Known is Yes.
	Mode Mode
	// Chosen reports whether the person ever set one. False with Known==Yes is the default.
	Chosen bool
	// Why carries the reason when Known is Undetermined.
	Why string
}

// ReadMode reports the effective publication mode.
func ReadMode(s *store.Store) ModeSetting {
	if s == nil {
		return ModeSetting{Known: tri.Undetermined, Why: "no store was opened"}
	}
	var rec modeRecord
	err := s.GetJSON(settingsKind, modeRecordID, &rec)
	switch {
	case err == nil:
		m, perr := ParseMode(rec.Mode)
		if perr != nil {
			// SOMETHING IS RECORDED AND IT IS NOT A MODE. Not the default: the person set
			// something, and answering `manual` here would report a choice they did not make.
			return ModeSetting{Known: tri.Undetermined, Why: fmt.Sprintf("the recorded mode %q is not one of the three", rec.Mode)}
		}
		return ModeSetting{Known: tri.Yes, Mode: m, Chosen: true}
	case errors.Is(err, store.ErrRecordNotFound):
		// NEVER CHOSEN. Criterion 5: the effective mode is a real value, and it is manual.
		return ModeSetting{Known: tri.Yes, Mode: DefaultMode, Chosen: false}
	default:
		return ModeSetting{Known: tri.Undetermined, Why: err.Error()}
	}
}

// WriteMode records the person's choice. It refuses anything outside the vocabulary even though
// every caller has parsed already: the store is the thing that outlives the caller.
func WriteMode(s *store.Store, m Mode) error {
	if s == nil {
		return hub.Refusedf(ErrNoStore, "no store was opened")
	}
	if _, err := ParseMode(string(m)); err != nil {
		return err
	}
	return s.PutJSON(settingsKind, modeRecordID, modeRecord{Mode: string(m)})
}

// Render is the one rendering of the mode, and its three branches are pairwise distinct.
//
// They are distinct in what they SAY, not merely in punctuation: a person reading the default
// branch learns both that nothing was chosen and what is therefore in effect, which is the whole
// of criterion 5.
func (ms ModeSetting) Render() string {
	switch {
	case ms.Known != tri.Yes:
		return "mode: " + tri.Undetermined.String() + " — your recorded choice could not be read, so this is neither a mode you set nor the default"
	case ms.Chosen:
		return "mode: " + string(ms.Mode) + " (you set this)"
	default:
		return "mode: " + string(DefaultMode) + " (the default — no mode has ever been set on this device)"
	}
}
