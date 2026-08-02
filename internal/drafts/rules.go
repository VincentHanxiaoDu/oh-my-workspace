// Issue #9 criterion 11: the rules `review` checks against are the person's own words, and they
// come back as they went in.
//
// WHY THERE IS NO NORMALISATION HERE, ANYWHERE.
//
// The obvious "tidying" — trim the text, collapse blank lines, lowercase for matching, split on
// newlines into a []string and join it back — each looks harmless and each one loses something a
// person meant. "never mention customer names" and "NEVER mention customer names" are the same rule
// to a reader and a different emphasis to a model. A rule written as a paragraph with a deliberate
// blank line between two thoughts becomes one run-on. A trailing "  — ask me if unsure" survives a
// trim and dies to a line-splitter. And the person cannot see any of it happen: they type their
// rules, the client prints something close enough, and the check runs against text they never wrote.
//
// So the text is stored as one string, byte for byte, and read back byte for byte. The only thing
// this file knows about the content is whether there is any.
package drafts

import (
	"errors"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ErrRulesUnreadable — rules are recorded and could not be read.
var ErrRulesUnreadable = &hub.Error{
	Code: "rules-unreadable",
	Msg:  "your review rules are recorded on this device and could not be read, so what review would check against is not known",
}

type rulesRecord struct {
	// Text is the person's wording. One string, not a list of lines: a list has to be joined back
	// together, and every join picks a separator the person did not choose.
	Text string `json:"text"`
}

// Rules is what the client knows about the person's recorded rules.
type Rules struct {
	// Recorded is Yes when rules are recorded, No when none ever were, Undetermined when the
	// answer could not be established. Three values, and the zero value is the third.
	Recorded tri.Value
	// Text is the person's wording, exactly as recorded, when Recorded is Yes.
	Text string
	// Why carries the reason when Recorded is Undetermined.
	Why string
}

// ReadRules returns the person's rules verbatim.
func ReadRules(s *store.Store) Rules {
	if s == nil {
		return Rules{Recorded: tri.Undetermined, Why: "no store was opened"}
	}
	var rec rulesRecord
	err := s.GetJSON(settingsKind, rulesRecordID, &rec)
	switch {
	case err == nil:
		return Rules{Recorded: tri.Yes, Text: rec.Text}
	case errors.Is(err, store.ErrRecordNotFound):
		return Rules{Recorded: tri.No}
	default:
		return Rules{Recorded: tri.Undetermined, Why: err.Error()}
	}
}

// WriteRules records the person's rules exactly as given.
//
// An empty string is accepted and means "recorded, and empty" — which is a different fact from
// "never recorded" and is the person's business either way. It is not this function's job to decide
// that somebody did not mean it.
func WriteRules(s *store.Store, text string) error {
	if s == nil {
		return hub.Refusedf(ErrNoStore, "no store was opened")
	}
	return s.PutJSON(settingsKind, rulesRecordID, rulesRecord{Text: text})
}
