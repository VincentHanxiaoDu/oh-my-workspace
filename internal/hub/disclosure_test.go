package hub

import (
	"strings"
	"testing"
)

// The statement itself must say the three things §2.4 says. A statement edited down to "the hub can
// read your notes" would still satisfy every "contains the statement" assertion in this package,
// because those assertions compare against the constant — so the constant's CONTENT is pinned here,
// once, by meaning rather than by a copy of the whole sentence.
func TestRestrictionStatementSaysWhatSection24Says(t *testing.T) {
	s := strings.ToLower(RestrictionStatement)
	for _, must := range []string{"colleague", "not a wall", "operates the hub", "index"} {
		if !strings.Contains(s, must) {
			t.Errorf("the §2.4 statement does not mention %q: %q", must, RestrictionStatement)
		}
	}
}

// CRITERION 8, AS A GREP OVER THE PACKAGE'S OWN OUTPUT SURFACES.
//
// This is the test the Issue is for. It walks every string this package can put in front of a
// person and asserts that no surface asserts or implies a note is private from whoever operates the
// hub without the §2.4 statement being present in the same view. It also asserts criterion 7: a
// surface that OFFERS a choice carries the statement whether or not it uses any of those words.
//
// It greps the RENDERED text, not a list of intentions.
func TestSurfacesDoNotOverclaimPrivacy(t *testing.T) {
	schema, err := AgentAPISchemaJSON()
	if err != nil {
		t.Fatalf("AgentAPISchemaJSON: %v", err)
	}

	surfaces := []struct {
		name         string
		text         string
		offersChoice bool
	}{
		{"hub.ChoiceBlock", ChoiceBlock(), true},
		{"hub.VisibilityField description (agent API point of choice)", VisibilityField().Description, true},
		{"hub.AgentAPISchemaJSON", schema, true},
		{"hub.RestrictionStatement", RestrictionStatement, false},
		{"company-wide description", CompanyWide().Describe(), false},
		{"named-people description", mustPeople("alice", "bo").Describe(), false},
		{"group description", mustGroup("platform").Describe(), false},
		{"self-only description", SelfOnly().Describe(), false},
		{"undetermined description", UndeterminedDescription, false},
		{"choice syntax", strings.Join(ChoiceSyntax, "\n"), false},
	}
	for _, s := range surfaces {
		if err := CheckSurface(s.name, s.text, s.offersChoice); err != nil {
			t.Errorf("%v", err)
		}
	}
}

// CheckSurface must actually catch what it claims to catch. A rule nobody has watched reject
// anything is not a rule.
func TestCheckSurfaceCatchesOverclaimingAndMissingStatement(t *testing.T) {
	t.Run("an overclaiming word with no statement is caught", func(t *testing.T) {
		for _, w := range overclaimingWords {
			text := "This note is " + w + "."
			if err := CheckSurface("made up", text, false); err == nil {
				t.Errorf("CheckSurface allowed %q with no §2.4 statement", w)
			}
		}
	})
	t.Run("the same wording with the statement present is allowed", func(t *testing.T) {
		text := "This note is private to your group.\n\n" + RestrictionStatement
		if err := CheckSurface("made up", text, true); err != nil {
			t.Errorf("CheckSurface refused text that carries the statement: %v", err)
		}
	})
	t.Run("a point of choice without the statement is caught", func(t *testing.T) {
		if err := CheckSurface("made up", "company / people / group / self", true); err == nil {
			t.Error("CheckSurface allowed a point of choice with no §2.4 statement — criterion 7")
		}
	})
	t.Run("case does not let a claim through", func(t *testing.T) {
		if err := CheckSurface("made up", "This is SECRET.", false); err == nil {
			t.Error("CheckSurface is case-sensitive, so 'SECRET' slips past")
		}
	})
}

// CRITERION 7 on the agent API path specifically: the schema's visibility FIELD description carries
// it, not merely some sibling field or a preamble that a caller reading one field never sees.
func TestAgentAPIVisibilityFieldCarriesTheStatement(t *testing.T) {
	f := VisibilityField()
	if !strings.Contains(f.Description, RestrictionStatement) {
		t.Errorf("the agent API's visibility field description omits the §2.4 statement:\n%s", f.Description)
	}
	if f.Default != "company" {
		t.Errorf("the schema's default is %q, want %q — criterion 1 on the agent API path", f.Default, "company")
	}
	if f.Required {
		t.Error("the schema marks visibility required; omitting it must mean company-wide")
	}
	// CRITERION 9: it is on the field, so it is served on every call, not once at onboarding.
	for _, tool := range AgentAPISchema() {
		for _, field := range tool.Fields {
			if field.Name != "visibility" {
				continue
			}
			if !strings.Contains(field.Description, RestrictionStatement) {
				t.Errorf("tool %q offers a visibility choice without the statement", tool.Tool)
			}
		}
	}
}

// The choice block offers all four, every time.
func TestChoiceBlockOffersAllFourAndTheStatement(t *testing.T) {
	b := ChoiceBlock()
	for _, want := range []string{"company", "people:", "group:", "self"} {
		if !strings.Contains(b, want) {
			t.Errorf("the choice block does not offer %q", want)
		}
	}
	if !strings.Contains(b, RestrictionStatement) {
		t.Error("the choice block omits the §2.4 statement")
	}
}
