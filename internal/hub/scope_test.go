package hub

import (
	"errors"
	"testing"
)

// A colleague's own scopes: everything a person can hold, which is NOT ScopeReadAll.
func colleagueScopes() []Scope {
	return []Scope{ScopeReadOwn, ScopeReadVisible, ScopePublish, ScopeSetVisibility}
}

// CRITERION 10 and CRITERION 11.
func TestGrantWiderThanItsHolderIsRefusedAndIssuesNothing(t *testing.T) {
	l := NewLedger()
	h := Holder{Person: "alice", Scopes: colleagueScopes()}

	// A legitimate grant first, so "unchanged" means something other than "empty".
	if _, err := l.Request(h, []Scope{ScopeReadVisible}); err != nil {
		t.Fatalf("a grant within the holder's own scopes was refused: %v", err)
	}
	before := l.Grants("alice")
	if len(before) != 1 {
		t.Fatalf("ledger has %d grants, want 1", len(before))
	}

	g, err := l.Request(h, []Scope{ScopeReadVisible, ScopeReadAll})
	if err == nil {
		t.Fatalf("a grant wider than its holder was issued: %v", g)
	}
	if Code(err) != ErrGrantWiderThanHolder.Code {
		t.Errorf("code = %q, want %q — criterion 10: distinguishable from success without parsing prose",
			Code(err), ErrGrantWiderThanHolder.Code)
	}
	if g.ID != "" || len(g.Scopes) != 0 {
		t.Errorf("a grant value came back with the refusal: %+v", g)
	}

	after := l.Grants("alice")
	if len(after) != len(before) {
		t.Errorf("alice has %d grants after the refused request, had %d — criterion 11: no token at all, not a narrower one",
			len(after), len(before))
	}
	for _, got := range after {
		if Permits(got.Scopes, ScopeReadAll) {
			t.Errorf("grant %s carries the operator scope", got.ID)
		}
	}
}

// The refusal is TOTAL, not an intersection. Refusing by dropping the offending scope would issue a
// token the caller believes carries what it asked for.
func TestRefusedGrantIsNotNarrowedInstead(t *testing.T) {
	h := Holder{Person: "alice", Scopes: colleagueScopes()}
	scopes, err := EvaluateGrantRequest(h, []Scope{ScopePublish, ScopeManageGroups})
	if err == nil {
		t.Fatalf("wider grant permitted, scopes = %v", scopes)
	}
	if scopes != nil {
		t.Errorf("EvaluateGrantRequest returned %v alongside a refusal — nothing may be issued", scopes)
	}
	if !errors.Is(err, ErrGrantWiderThanHolder) {
		t.Errorf("err = %v, want ErrGrantWiderThanHolder", err)
	}
}

func TestGrantRequestRejectsUnknownAndEmptyScopes(t *testing.T) {
	h := Holder{Person: "alice", Scopes: colleagueScopes()}
	if _, err := EvaluateGrantRequest(h, nil); Code(err) != ErrUnknownScope.Code {
		t.Errorf("an empty request = %v, want %q", err, ErrUnknownScope.Code)
	}
	if _, err := EvaluateGrantRequest(h, []Scope{"notes:read:everything"}); Code(err) != ErrUnknownScope.Code {
		t.Errorf("an unknown scope = %v, want %q", err, ErrUnknownScope.Code)
	}
}

// CRITERION 13: ONE vocabulary. The names the CLI prints, the names the agent API schema advertises
// and the names the hub validates are the same set.
//
// The CLI is not consulted here by name — it prints hub.Vocabulary() and a command test asserts
// that. What this test pins is that every scope the agent API schema advertises is in the
// vocabulary, and that no surface has invented one.
func TestOneScopeVocabularyAcrossSurfaces(t *testing.T) {
	vocab := map[string]bool{}
	for _, s := range Vocabulary() {
		vocab[string(s)] = true
	}
	if len(vocab) != len(vocabulary) {
		t.Fatalf("Vocabulary() has duplicates: %v", Vocabulary())
	}
	for _, tool := range AgentAPISchema() {
		if len(tool.Scopes) == 0 {
			t.Errorf("agent API tool %q advertises no scope — a capability with no grant behind it", tool.Tool)
		}
		for _, s := range tool.Scopes {
			if !vocab[s] {
				t.Errorf("agent API tool %q names scope %q, which is not in the vocabulary", tool.Tool, s)
			}
		}
	}
	// A scope that permits publishing permits it identically everywhere, because there is one
	// Permits and one list. A scope that does not, permits it nowhere.
	if !Permits([]Scope{ScopePublish}, ScopePublish) {
		t.Error("ScopePublish does not permit publishing")
	}
	if Permits([]Scope{ScopeReadVisible}, ScopePublish) {
		t.Error("a read scope permits publishing — scopes must not imply one another")
	}
}

// CRITERION 12 through a grant: an agent cannot read what its person cannot, and the refusal is
// still distinguishable from "no such note".
func TestGrantReadsExactlyWhatItsPersonCan(t *testing.T) {
	s := testStore(t)
	n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: mustPeople("carol")})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	l := NewLedger()

	carolGrant, err := l.Request(Holder{Person: "carol", Scopes: colleagueScopes()}, []Scope{ScopeReadVisible})
	if err != nil {
		t.Fatalf("carol's grant: %v", err)
	}
	danGrant, err := l.Request(Holder{Person: "dan", Scopes: colleagueScopes()}, []Scope{ScopeReadVisible})
	if err != nil {
		t.Fatalf("dan's grant: %v", err)
	}

	if _, err := ReadThrough(s, carolGrant, n.ID); err != nil {
		t.Errorf("carol's agent cannot read what carol can: %v", err)
	}
	_, refused := ReadThrough(s, danGrant, n.ID)
	if Code(refused) != ErrRefused.Code {
		t.Errorf("dan's agent read a note dan cannot: %v", refused)
	}
	_, missing := ReadThrough(s, danGrant, "no-such-id")
	if Code(missing) != ErrNoSuchNote.Code {
		t.Errorf("missing note through a grant = %q, want %q", Code(missing), ErrNoSuchNote.Code)
	}
	if Code(refused) == Code(missing) {
		t.Error("refused and no-such-note are indistinguishable through the agent API")
	}
}

// The narrower read scope really is narrower.
func TestReadOwnGrantCannotReadOthersNotes(t *testing.T) {
	s := testStore(t)
	n, _ := s.Publish(Publication{Author: "alice", Title: "t", Body: "b"}) // company-wide
	g, err := NewLedger().Request(Holder{Person: "carol", Scopes: colleagueScopes()}, []Scope{ScopeReadOwn})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := ReadThrough(s, g, n.ID); !errors.Is(err, ErrRefused) {
		t.Errorf("a 'read my own notes' grant read somebody else's note: %v", err)
	}
	own, _ := s.Publish(Publication{Author: "carol", Title: "t", Body: "b"})
	if _, err := ReadThrough(s, g, own.ID); err != nil {
		t.Errorf("a 'read my own notes' grant cannot read its holder's own note: %v", err)
	}
}
