// Issue #11 — the control API's version surface.
//
// CRITERION 13 IS WHY THIS FILE IS THIN. "The CLI and the control API report the same version
// state ... neither shows a version the other does not." The cheapest way to make two surfaces
// agree is to give them one source of truth and let each one only choose an encoding. So the
// answer types here are built from [ListTimeline], [ReadView] and [CurrentView] — the same three
// functions the CLI calls — and this file contains no logic about which version is current, no
// second visibility check, and no second standing vocabulary.
//
// A test drives both surfaces over one store and compares them field by field. It compares the two
// surfaces to EACH OTHER, not each to a fixture, because two fixtures edited apart is the failure
// this criterion is about.
package hub

import (
	"encoding/json"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// StandingToken is the machine-readable standing: "current", "superseded", or "undetermined".
//
// It is a THIRD token and not a boolean with a null. A JSON `"current": false` for a version whose
// standing was never established is the collapse PRD §4.3 forbids, expressed in a wire format where
// nobody notices it.
func StandingToken(v tri.Value) string {
	switch v {
	case tri.Yes:
		return "current"
	case tri.No:
		return "superseded"
	default:
		return "undetermined"
	}
}

// VersionAnswer is one version as the control API serves it.
type VersionAnswer struct {
	Ref      string `json:"ref"`
	Standing string `json:"standing"`
	// BodyKnown false means Body is absent, NOT empty. A caller that reads Body without checking
	// this gets "" — so the field is named for the check rather than for the content, and the JSON
	// omits Body entirely when it is not known, so an unreadable body cannot decode as a blank one.
	BodyKnown bool   `json:"body_known"`
	Body      string `json:"body,omitempty"`
	Written   string `json:"written,omitempty"`
	Archived  bool   `json:"archived"`
	// Note is the reason line when the version's state could not be established.
	Note string `json:"note,omitempty"`
}

// TimelineAnswer is a note's timeline as the control API serves it.
type TimelineAnswer struct {
	Note string `json:"note"`
	// Determined false means Versions is absent, not empty (criterion 12).
	Determined bool            `json:"determined"`
	Current    string          `json:"current,omitempty"`
	Versions   []VersionAnswer `json:"versions,omitempty"`
	Archived   bool            `json:"archived"`
	Reason     string          `json:"reason,omitempty"`
	Code       string          `json:"code,omitempty"`
}

// AnswerFor encodes a view. The CLI prints [VersionView.Render]; this encodes the same struct.
func AnswerFor(v VersionView) VersionAnswer {
	a := VersionAnswer{
		Ref:       v.Ref.String(),
		Standing:  StandingToken(v.Standing),
		BodyKnown: v.BodyKnown,
		Archived:  v.Archived,
	}
	if v.BodyKnown {
		a.Body = v.Body
	} else {
		a.Note = BodyUnreadableLine
	}
	if !v.At.IsZero() {
		a.Written = v.At.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return a
}

// TimelineAnswerFor encodes a timeline view.
func TimelineAnswerFor(t TimelineView) TimelineAnswer {
	a := TimelineAnswer{Note: string(t.Note), Determined: t.Determined, Archived: t.Archived}
	if !t.Determined {
		a.Reason = UndeterminedTimelineLine
		if t.Why != nil {
			a.Code = Code(t.Why)
		}
		return a
	}
	a.Current = t.Current.String()
	for _, e := range t.Entries {
		// The body is not carried in a timeline listing on either surface. A listing is an index,
		// and a listing that carried bodies would be a second way to read a version — a second way
		// being a second place the gate has to be right. So the entry is encoded with its body
		// dropped and body_known true, which is accurate: the listing knows the body exists and is
		// simply not showing it.
		e.Body = ""
		a.Versions = append(a.Versions, AnswerFor(e))
	}
	return a
}

// TimelineJSON is what the control API returns for a timeline request.
func TimelineJSON(src VersionSource, arch *Archive, id NoteID, reader PersonID) (string, error) {
	t, err := ListTimeline(src, arch, id, reader)
	if err != nil {
		return "", err
	}
	b, merr := json.MarshalIndent(TimelineAnswerFor(t), "", "  ")
	if merr != nil {
		return "", merr
	}
	return string(b) + "\n", nil
}

// VersionJSON is what the control API returns for a request to read one version.
func VersionJSON(src VersionSource, arch *Archive, ref VersionRef, reader PersonID) (string, error) {
	v, err := ReadView(src, arch, ref, reader)
	if err != nil {
		return "", err
	}
	b, merr := json.MarshalIndent(AnswerFor(v), "", "  ")
	if merr != nil {
		return "", merr
	}
	return string(b) + "\n", nil
}

// CurrentJSON is what the control API returns for a request that names no version (criterion 6).
func CurrentJSON(src VersionSource, arch *Archive, id NoteID, reader PersonID) (string, error) {
	v, err := CurrentView(src, arch, id, reader)
	if err != nil {
		return "", err
	}
	b, merr := json.MarshalIndent(AnswerFor(v), "", "  ")
	if merr != nil {
		return "", merr
	}
	return string(b) + "\n", nil
}

// VersionAPISchema is the agent API's description of the version operations.
//
// It is a SEPARATE function from [AgentAPISchema] rather than an edit to it. That keeps Issue #12's
// file untouched, and it is also the honest shape: these are this Issue's operations, and a surface
// that serves both simply serves both lists.
//
// THE SCOPES ARE THE EXISTING THREE. Reading a version is reading, so it is [ScopeRead]; there is
// no `history` scope and no operator scope, because the hub operator's reach is a deployment fact
// stated by [RestrictionStatement], not a capability anybody is granted.
func VersionAPISchema() []ToolSchema {
	return []ToolSchema{
		{
			Tool: "notes.versions",
			Description: "List a note's timeline, oldest first. Each entry carries a reference of the form note-1" + refSeparator + "3 " +
				"that can be passed straight back to notes.read_version. A note you may not read, and a note that does not exist, " +
				"answer the same " + ErrNoSuchNote.Code + "; the existence of a version is never surfaced to someone who may not read the note. " +
				"A timeline that could not be established answers determined=false and shows no versions — that is not a note with no history.",
			Fields: []FieldSchema{
				{Name: "note_id", Type: "string", Required: true, Description: "The note whose timeline to list."},
			},
			Scopes: scopeStrings(ScopeRead),
		},
		{
			Tool: "notes.read_version",
			Description: "Read a note as it stood at one point on its timeline. The answer always states its standing — " +
				"current, superseded, or undetermined — so a reader who did not choose the version can still tell which they are holding. " +
				"A reference to a version that does not exist answers " + ErrNoSuchVersion.Code +
				", which is distinct from a successful read of a version whose body is empty. " +
				"A version whose content could not be retrieved answers " + ErrVersionUnreadable.Code +
				" as body_known=false, and is never served as an empty body. " +
				"The note's current visibility governs every version of it.",
			Fields: []FieldSchema{
				{Name: "ref", Type: "string", Required: true, Description: "A version reference, for example note-1" + refSeparator + "3."},
			},
			Scopes: scopeStrings(ScopeRead),
		},
		{
			Tool: "notes.search",
			Description: "Find notes whose CURRENT version matches. Text that exists only in a superseded version is not matched, " +
				"and every result names the version it refers to. Visibility is settled before anything is ordered.",
			Fields: []FieldSchema{
				{Name: "term", Type: "string", Required: true, Description: "What to look for."},
			},
			Scopes: scopeStrings(ScopeRead),
		},
	}
}

// VersionAPISchemaJSON renders the version schema, for `omw note schema`.
func VersionAPISchemaJSON() (string, error) {
	b, err := json.MarshalIndent(VersionAPISchema(), "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}
