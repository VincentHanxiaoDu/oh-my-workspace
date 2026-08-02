package hub

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FieldSchema describes one field of the agent API (PRD §3.12: "a person's own AI, reading their
// own material").
//
// WHY THE SCHEMA IS BUILT HERE AND NOT WRITTEN AS A JSON FILE. Criterion 7 requires the §2.4
// statement on the agent API path too — "its schema/description for the visibility field" — and
// criterion 8 forbids overclaiming wording anywhere a visibility is chosen or displayed. A schema
// that is a static asset is a second copy of wording nobody greps. Built from the same constants
// the CLI prints, it is one wording, and [CheckSurface] can be run over the rendered JSON in a test.
type FieldSchema struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description"`
}

// ToolSchema is one agent API operation.
type ToolSchema struct {
	Tool        string        `json:"tool"`
	Description string        `json:"description"`
	Fields      []FieldSchema `json:"fields"`
	Scopes      []string      `json:"scopes"`
}

// VisibilityField is the agent API's description of the visibility field, and it is a POINT OF
// CHOICE: an agent calling publish chooses a visibility here, so §2.4 is stated here, in the
// description the caller reads, every time the schema is served (criterion 9 — the schema is not
// an onboarding step).
func VisibilityField() FieldSchema {
	var b strings.Builder
	b.WriteString("Who can see this note. Omit it and the note is company-wide, which is the product default.\n")
	for _, line := range ChoiceSyntax {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	b.WriteString("\n")
	b.WriteString(RestrictionStatement)
	return FieldSchema{
		Name:        "visibility",
		Type:        "string",
		Required:    false,
		Default:     CompanyWide().Token(),
		Enum:        []string{"company", "people:<a>,<b>", "group:<name>", "self"},
		Description: b.String(),
	}
}

// PublishTool is the agent API's publish operation.
func PublishTool() ToolSchema {
	return ToolSchema{
		Tool:        "notes.publish",
		Description: "Publish a note to the hub as the person this agent acts for.",
		Fields: []FieldSchema{
			{Name: "title", Type: "string", Required: true, Description: "One line naming what was worked out."},
			{Name: "body", Type: "string", Required: true, Description: "The note."},
			VisibilityField(),
		},
		// The SAME names the CLI prints and the store checks — criterion 13.
		Scopes: scopeStrings(ScopePublish),
	}
}

// SetVisibilityTool is the agent API's operation for changing an existing note's visibility. It is
// also a point of choice, so it also carries the statement, via the same field.
func SetVisibilityTool() ToolSchema {
	return ToolSchema{
		Tool:        "notes.set_visibility",
		Description: "Change who can see a note you wrote. This applies to the whole note, including every earlier version on its timeline.",
		Fields: []FieldSchema{
			{Name: "note_id", Type: "string", Required: true, Description: "The note to change."},
			VisibilityField(),
		},
		Scopes: scopeStrings(ScopeSetVisibility),
	}
}

// ReadTool is the agent API's read operation. It is not a point of choice, but it DISPLAYS a
// visibility, so criterion 8 binds it: no overclaiming wording without the statement.
func ReadTool() ToolSchema {
	return ToolSchema{
		Tool: "notes.read",
		Description: "Read a note as the person this agent acts for. An agent cannot read what its person cannot. " +
			"A note that exists but is not visible to you is refused with code " + ErrRefused.Code +
			"; a note that does not exist answers " + ErrNoSuchNote.Code +
			"; a visibility that could not be worked out answers " + ErrUndetermined.Code +
			" and is neither of the other two.",
		Fields: []FieldSchema{
			{Name: "note_id", Type: "string", Required: true, Description: "The note to read."},
			{Name: "version", Type: "integer", Required: false, Description: "A point on the note's timeline. The note's current visibility governs every version of it."},
		},
		Scopes: scopeStrings(ScopeReadVisible),
	}
}

// AgentAPISchema is every operation this Issue defines, as the agent API serves it.
func AgentAPISchema() []ToolSchema {
	return []ToolSchema{PublishTool(), SetVisibilityTool(), ReadTool()}
}

// AgentAPISchemaJSON renders the schema. This exact text is what a test greps for the §2.4
// statement, and what the CLI prints for `omw visibility schema`.
func AgentAPISchemaJSON() (string, error) {
	b, err := json.MarshalIndent(AgentAPISchema(), "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func scopeStrings(ss ...Scope) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !KnownScope(s) {
			// A schema advertising a scope the vocabulary does not contain would break criterion 13
			// the moment anything believed it. Programmer error, found by any test that renders the
			// schema.
			panic("hub: schema names unknown scope " + string(s))
		}
		out = append(out, string(s))
	}
	return out
}
