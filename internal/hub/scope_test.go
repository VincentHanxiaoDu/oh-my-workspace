package hub

import (
	"errors"
	"strings"
	"testing"
)

// colleagueScopes is what an ordinary colleague holds: everything in the ruled vocabulary.
//
// A person who has signed in holds all three; the question criterion 10 asks is whether they can
// DELEGATE more than they hold, so the narrower holders below are the interesting ones.
func colleagueScopes() []Scope { return []Scope{ScopeRead, ScopeWrite, ScopePublish} }

// readOnlyColleague holds `read` and `write` but has never been granted `publish` — PRD §3.10's
// "a token that can do the second was asked for on purpose", from the person's side.
func readOnlyColleague(p PersonID) Holder {
	return Holder{Person: p, Scopes: []Scope{ScopeRead, ScopeWrite}}
}

// THE VOCABULARY IS THE RULED ONE. Issue #12's `## Ruled` section fixes it at read / write /
// publish, and #19 owns it. This asserts the literal three, deliberately and unlike every other
// test in this file: the rest read Vocabulary() dynamically because what they are about is
// cross-surface CONSISTENCY, which must not be pinned to particular words. This one is about the
// words, because an earlier revision of this branch invented six of its own and nothing caught it.
func TestVocabularyIsTheRuledThree(t *testing.T) {
	got := map[string]bool{}
	for _, s := range Vocabulary() {
		got[string(s)] = true
	}
	want := []string{"read", "write", "publish"}
	if len(got) != len(want) {
		t.Errorf("the vocabulary has %d scopes (%v), want exactly the ruled three %v", len(got), Vocabulary(), want)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("the ruled scope %q is missing from the vocabulary %v", w, Vocabulary())
		}
	}
}

// CRITERION 10 and CRITERION 11.
func TestGrantWiderThanItsHolderIsRefusedAndIssuesNothing(t *testing.T) {
	l := NewLedger()
	h := Holder{Person: "alice", Scopes: colleagueScopes()}

	// A legitimate grant first, so "unchanged" means something other than "empty".
	if _, err := l.Request(h, []Scope{ScopeRead}); err != nil {
		t.Fatalf("a grant within the holder's own scopes was refused: %v", err)
	}
	before := l.Grants("alice")
	if len(before) != 1 {
		t.Fatalf("ledger has %d grants, want 1", len(before))
	}

	// alice herself was never granted `publish`, so a token asking for it asks for more than she
	// holds — PRD §4.5, refused when requested rather than narrowed at the edge.
	narrow := readOnlyColleague("alice")
	if _, err := l.Request(narrow, []Scope{ScopeRead}); err != nil {
		t.Fatalf("a second legitimate grant was refused: %v", err)
	}
	before = l.Grants("alice")

	g, err := l.Request(narrow, []Scope{ScopeRead, ScopePublish})
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
		if Permits(got.Scopes, ScopePublish) {
			t.Errorf("grant %s carries the publish scope its holder was never granted", got.ID)
		}
	}
}

// The refusal is TOTAL, not an intersection. Refusing by dropping the offending scope would issue a
// token the caller believes carries what it asked for.
func TestRefusedGrantIsNotNarrowedInstead(t *testing.T) {
	h := readOnlyColleague("alice")
	scopes, err := EvaluateGrantRequest(h, []Scope{ScopeRead, ScopePublish})
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
	if _, err := EvaluateGrantRequest(h, []Scope{"notes:read:all"}); Code(err) != ErrUnknownScope.Code {
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
	// CRITERION 13, in its own words: "`publish` permits publishing a company-wide note identically
	// on all three, and `read` and `write` permit it on none."
	for _, s := range []Scope{ScopeRead, ScopeWrite} {
		if Permits([]Scope{s}, ScopePublish) {
			t.Errorf("%q permits publishing — scopes must not imply one another", s)
		}
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

	carolGrant, err := l.Request(Holder{Person: "carol", Scopes: colleagueScopes()}, []Scope{ScopeRead})
	if err != nil {
		t.Fatalf("carol's grant: %v", err)
	}
	danGrant, err := l.Request(Holder{Person: "dan", Scopes: colleagueScopes()}, []Scope{ScopeRead})
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

// A grant with no read scope cannot read, and says so with its own code rather than looking like a
// visibility refusal — the two are fixed by different things.
func TestGrantWithoutReadScopeCannotRead(t *testing.T) {
	s := testStore(t)
	n, _ := s.Publish(Publication{Author: "carol", Title: "t", Body: "b"}) // company-wide
	g, err := NewLedger().Request(Holder{Person: "carol", Scopes: colleagueScopes()}, []Scope{ScopeWrite})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	_, err = ReadThrough(s, g, n.ID)
	if Code(err) != ErrReadScopeRequired.Code {
		t.Errorf("a write-only grant reading a note = %v (code %q), want %q", err, Code(err), ErrReadScopeRequired.Code)
	}
	if Code(err) == ErrRefused.Code {
		t.Error("'your grant may not do this' is indistinguishable from 'this note is not visible to you'")
	}
}

// ==============================================================================================
// CRITERION 10a — the write path. Setting a note's visibility is part of publishing it.
// ==============================================================================================

// All three of 10a's clauses, on both write entry points: refused, distinguishably, and the note's
// visibility is UNCHANGED afterwards.
//
// The third clause is the one a naive fix drops. A refusal that has already written passes "was
// refused" and "looks different from success" while having done the thing, so this reads the stored
// visibility back rather than trusting the returned error.
func TestOnlyThePublishScopeCanSetVisibility(t *testing.T) {
	for _, held := range [][]Scope{{ScopeRead}, {ScopeWrite}, {ScopeRead, ScopeWrite}} {
		t.Run(scopeList(held), func(t *testing.T) {
			s := testStore(t)
			n, err := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: SelfOnly()})
			if err != nil {
				t.Fatalf("Publish: %v", err)
			}
			before, err := s.VisibilityOf(n.ID)
			if err != nil {
				t.Fatalf("VisibilityOf: %v", err)
			}

			g, err := NewLedger().Request(Holder{Person: "alice", Scopes: colleagueScopes()}, held)
			if err != nil {
				t.Fatalf("grant: %v", err)
			}

			// Clause 1: refused.
			got, err := SetVisibilityThrough(s, g, n.ID, CompanyWide())
			if err == nil {
				t.Fatalf("a grant holding %s changed who can see a note", scopeList(held))
			}
			if got != nil {
				t.Error("a note came back alongside the refusal")
			}
			// Clause 2: distinguishably from success, without parsing prose.
			if Code(err) != ErrPublishScopeRequired.Code {
				t.Errorf("code = %q, want %q", Code(err), ErrPublishScopeRequired.Code)
			}
			// Clause 3: unchanged afterwards.
			after, err := s.VisibilityOf(n.ID)
			if err != nil {
				t.Fatalf("VisibilityOf after the refusal: %v", err)
			}
			if !after.Equal(before) {
				t.Errorf("the note's visibility changed from %q to %q despite the refusal — criterion 10a's third clause",
					before.Token(), after.Token())
			}
			if after.Token() != "self" {
				t.Errorf("visibility is now %q, want %q", after.Token(), "self")
			}
		})
	}
}

// The same rule on the way in: a grant that cannot publish cannot create a note either, and the hub
// holds nothing afterwards.
func TestOnlyThePublishScopeCanPublish(t *testing.T) {
	s := testStore(t)
	before := s.Count()
	g, err := NewLedger().Request(Holder{Person: "alice", Scopes: colleagueScopes()}, []Scope{ScopeRead, ScopeWrite})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	n, err := PublishThrough(s, g, Publication{Title: "t", Body: "b"})
	if err == nil {
		t.Fatalf("a read/write grant published note %v", n.ID)
	}
	if Code(err) != ErrPublishScopeRequired.Code {
		t.Errorf("code = %q, want %q", Code(err), ErrPublishScopeRequired.Code)
	}
	if s.Count() != before {
		t.Errorf("the hub holds %d notes after a refused publication, was %d", s.Count(), before)
	}
}

// And the publish scope does work, so the tests above are not passing because everything is refused.
func TestThePublishScopeCanPublishAndSetVisibility(t *testing.T) {
	s := testStore(t)
	g, err := NewLedger().Request(Holder{Person: "alice", Scopes: colleagueScopes()}, []Scope{ScopeRead, ScopePublish})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	n, err := PublishThrough(s, g, Publication{Title: "t", Body: "b"})
	if err != nil {
		t.Fatalf("a publish grant could not publish: %v", err)
	}
	if n.Author != "alice" {
		t.Errorf("the note was published as %q, want the grant's holder", n.Author)
	}
	if _, err := SetVisibilityThrough(s, g, n.ID, SelfOnly()); err != nil {
		t.Fatalf("a publish grant could not change visibility: %v", err)
	}
	v, _ := s.VisibilityOf(n.ID)
	if v.Token() != "self" {
		t.Errorf("visibility is %q, want %q", v.Token(), "self")
	}
}

// A grant acts as its holder and cannot publish words as somebody else.
func TestAGrantCannotPublishAsSomebodyElse(t *testing.T) {
	s := testStore(t)
	g, err := NewLedger().Request(Holder{Person: "alice", Scopes: colleagueScopes()}, []Scope{ScopePublish})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := PublishThrough(s, g, Publication{Author: "bo", Title: "t", Body: "b"}); !errors.Is(err, ErrRefused) {
		t.Errorf("alice's grant published as bo: %v", err)
	}
	if s.Count() != 0 {
		t.Error("a note was stored despite the refusal")
	}
}

// A grant that may publish still cannot change a note it does not own — the scope check does not
// replace the authorship check, it precedes it.
func TestPublishScopeDoesNotOverrideAuthorship(t *testing.T) {
	s := testStore(t)
	n, _ := s.Publish(Publication{Author: "alice", Title: "t", Body: "b", Visibility: SelfOnly()})
	g, err := NewLedger().Request(Holder{Person: "bo", Scopes: colleagueScopes()}, []Scope{ScopePublish})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := SetVisibilityThrough(s, g, n.ID, CompanyWide()); !errors.Is(err, ErrRefused) {
		t.Errorf("bo's publish grant changed alice's note: %v", err)
	}
	v, _ := s.VisibilityOf(n.ID)
	if v.Token() != "self" {
		t.Errorf("visibility is now %q — a refusal must leave the note as it was", v.Token())
	}
}

func scopeList(ss []Scope) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, "+")
}
