package drafts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("creating a store to test against: %v", err)
	}
	return s
}

// pairwiseDistinct is how every three-way rendering in this package is checked.
//
// WHY NOT ASSERT EACH AGAINST A LITERAL. Three assertions of the form `got == "no mode set"` all
// pass just as happily after two of the three renderings have been edited to the same wording —
// which is the collapse §4.3 forbids, passing a test suite that was written to catch it. Comparing
// the renderings against EACH OTHER catches it by construction, and does not have to be updated
// when somebody rewords one of them.
func pairwiseDistinct(t *testing.T, what string, renderings map[string]string) {
	t.Helper()
	for name, s := range renderings {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s: the %q rendering is blank; silence is not one of the three answers", what, name)
		}
	}
	seen := map[string]string{}
	for name, s := range renderings {
		if other, dup := seen[s]; dup {
			t.Errorf("%s: the %q and %q renderings are the same string:\n  %q\n"+
				"  Two of the three answers have collapsed into one.", what, other, name, s)
		}
		seen[s] = name
	}
}

// ---------------------------------------------------------------------------
// Criterion 5 — the default mode is a real value
// ---------------------------------------------------------------------------

func TestWithNoModeEverSetTheEffectiveModeIsManualAndIsARealValue(t *testing.T) {
	s := newTestStore(t)
	ms := ReadMode(s)
	if ms.Known != tri.Yes {
		t.Fatalf("Known = %v; with a readable store and no mode set, the effective mode is established", ms.Known)
	}
	if ms.Mode != ModeManual {
		t.Errorf("Mode = %q, want %q", ms.Mode, ModeManual)
	}
	if ms.Chosen {
		t.Errorf("Chosen = true, but nobody has chosen anything")
	}
	// A REAL VALUE, NOT BLANK AND NOT ABSENT. The rendering must contain the word, so that a person
	// reading the output learns what is in effect rather than seeing an empty field.
	r := ms.Render()
	if !strings.Contains(r, string(ModeManual)) {
		t.Errorf("the default renders as %q, which does not state the effective mode %q", r, ModeManual)
	}
	if strings.Contains(r, "mode: \n") || strings.HasSuffix(strings.TrimSpace(r), "mode:") {
		t.Errorf("the default renders blank: %q", r)
	}
}

// ---------------------------------------------------------------------------
// Criteria 6, 7, 19 — setting, reading back, refusing, and the third answer
// ---------------------------------------------------------------------------

func TestEachModeCanBeSetAndIsReportedBack(t *testing.T) {
	s := newTestStore(t)
	for _, m := range Modes() {
		if err := WriteMode(s, m); err != nil {
			t.Fatalf("WriteMode(%q): %v", m, err)
		}
		got := ReadMode(s)
		if got.Known != tri.Yes || got.Mode != m || !got.Chosen {
			t.Errorf("after setting %q, ReadMode = %+v", m, got)
		}
	}
}

func TestAModeOutsideTheVocabularyIsRefusedAndChangesNothing(t *testing.T) {
	s := newTestStore(t)
	if err := WriteMode(s, ModeAuto); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Manual", "man", "review ", "publish", "on"} {
		if _, err := ParseMode(bad); err == nil {
			t.Errorf("ParseMode(%q) accepted a name that is not one of the three", bad)
		}
		if err := WriteMode(s, Mode(bad)); err == nil {
			t.Errorf("WriteMode(%q) accepted a name that is not one of the three", bad)
		}
		if got := ReadMode(s); got.Mode != ModeAuto {
			t.Fatalf("after refusing %q the effective mode is %q; the previous choice was not left alone", bad, got.Mode)
		}
	}
}

func TestTheModesAreExactlyThree(t *testing.T) {
	if got := len(Modes()); got != 3 {
		t.Fatalf("there are %d modes; the vocabulary is manual, review and auto", got)
	}
}

func TestTheThreeModeAnswersRenderPairwiseDistinctly(t *testing.T) {
	s := newTestStore(t)
	def := ReadMode(s).Render()
	if err := WriteMode(s, ModeReview); err != nil {
		t.Fatal(err)
	}
	real := ReadMode(s).Render()
	undetermined := ModeSetting{Known: tri.Undetermined, Why: "the record could not be read"}.Render()
	pairwiseDistinct(t, "the publication mode", map[string]string{
		"a mode the person set":   real,
		"no mode ever set":        def,
		"could not be determined": undetermined,
	})
}

// CRITERION 19 driven against the disk rather than a constructed struct: a mode record that exists
// and cannot be read is undetermined, and is neither the default nor a mode.
func TestAModeRecordThatCannotBeReadIsUndeterminedAndNotTheDefault(t *testing.T) {
	s := newTestStore(t)
	if err := WriteMode(s, ModeAuto); err != nil {
		t.Fatal(err)
	}
	corruptRecord(t, s, modeRecordID)
	got := ReadMode(s)
	if got.Known != tri.Undetermined {
		t.Fatalf("ReadMode = %+v; a damaged record must not resolve to a mode", got)
	}
	if strings.Contains(got.Render(), "(the default") {
		t.Errorf("an unreadable choice renders as the default: %q", got.Render())
	}
}

// corruptRecord damages a record in place, which is how "present and unreadable" is produced
// without depending on file permissions (a test running as root can read anything).
func corruptRecord(t *testing.T, s *store.Store, id string) {
	t.Helper()
	path := filepath.Join(s.Path(), "records", string(settingsKind), id+".rec")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the record this test means to damage is not at %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Criterion 11 — the person's own words
// ---------------------------------------------------------------------------

// The wording here is chosen to break a naive normaliser: leading and trailing spaces, a deliberate
// blank line, mixed case, a tab, CRLF, a trailing newline, an em dash, and a line that a
// line-splitter would happily reorder or drop.
const awkwardRules = "  NEVER mention customer names.\n\n\tno half-finished reasoning — if I have not finished the thought, it is not a note.\r\n" +
	"  keep the trailing spaces here:   \nlowercase matters: Acme is a customer, acme is a package.\n"

func TestRulesAreRecordedAndReadBackExactly(t *testing.T) {
	s := newTestStore(t)
	if err := WriteRules(s, awkwardRules); err != nil {
		t.Fatalf("WriteRules: %v", err)
	}
	got := ReadRules(s)
	if got.Recorded != tri.Yes {
		t.Fatalf("Recorded = %v after recording rules", got.Recorded)
	}
	if got.Text != awkwardRules {
		t.Errorf("the rules came back changed.\n  wrote: %q\n  read:  %q", awkwardRules, got.Text)
	}
}

func TestNoRulesRecordedIsANegativeAndUnreadableRulesAreNot(t *testing.T) {
	s := newTestStore(t)
	if got := ReadRules(s); got.Recorded != tri.No {
		t.Errorf("with nothing recorded, Recorded = %v, want %v", got.Recorded, tri.No)
	}
	if err := WriteRules(s, awkwardRules); err != nil {
		t.Fatal(err)
	}
	corruptRecord(t, s, rulesRecordID)
	if got := ReadRules(s); got.Recorded != tri.Undetermined {
		t.Errorf("with a damaged record, Recorded = %v, want %v — unreadable rules are not 'no rules'", got.Recorded, tri.Undetermined)
	}
}

// ---------------------------------------------------------------------------
// Draft state
// ---------------------------------------------------------------------------

func newStateOutbox(t *testing.T) *Outbox {
	t.Helper()
	o, err := Create(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestANewDraftRestsInTheDraftedStateAndAnAbsentOneIsANegative(t *testing.T) {
	o := newStateOutbox(t)
	if _, err := o.Revise("d1", "hello"); err != nil {
		t.Fatal(err)
	}
	got := o.StateOf("d1")
	if got.Known != tri.Yes || got.Exists != tri.Yes || got.State != StateDrafted {
		t.Errorf("a freshly written draft reports %+v; it should be drafted and present", got)
	}
	absent := o.StateOf("nope")
	if absent.Known != tri.Yes || absent.Exists != tri.No {
		t.Errorf("an absent draft reports %+v; that it is absent is a determined answer", absent)
	}
}

func TestADraftWhoseStateCannotBeReadIsUndeterminedAndNotResting(t *testing.T) {
	o := newStateOutbox(t)
	if _, err := o.Revise("d1", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := o.SetState("d1", StateRefused, "a rule said no"); err != nil {
		t.Fatal(err)
	}
	// Damage the state record rather than chmod it: a suite running as root can read a 0o000 file,
	// and a test that silently skips there proves nothing.
	if err := os.WriteFile(filepath.Join(o.Dir(), "d1", stateFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := o.StateOf("d1")
	if got.Known != tri.Undetermined {
		t.Fatalf("StateOf = %+v; a damaged state record must not resolve to a state", got)
	}
	if got.State == StateDrafted {
		t.Errorf("an unreadable state reads as %q — 'resting, awaiting you' is exactly what it must not say", got.State)
	}
}

func TestTheStateRenderingsAreDistinctFromEachOther(t *testing.T) {
	o := newStateOutbox(t)
	if _, err := o.Revise("d1", "hello"); err != nil {
		t.Fatal(err)
	}
	renderings := map[string]string{}
	for _, st := range []State{StateDrafted, StateBlocked, StateReviewUndetermined, StateRefused, StateCleared, StateLeaving} {
		if err := o.SetState("d1", st, ""); err != nil {
			t.Fatal(err)
		}
		renderings[string(st)] = o.StateOf("d1").Render()
	}
	renderings["absent"] = o.StateOf("nope").Render()
	renderings["undetermined"] = StateReport{Known: tri.Undetermined, Exists: tri.Yes, Why: "unreadable"}.Render()
	pairwiseDistinct(t, "a draft's state", renderings)
}

// A state file must never be mistaken for a revision, or a person's timeline grows a version they
// did not write.
func TestRecordingAStateDoesNotAddARevision(t *testing.T) {
	o := newStateOutbox(t)
	if _, err := o.Revise("d1", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := o.SetState("d1", StateCleared, "fine"); err != nil {
		t.Fatal(err)
	}
	vs, err := o.Timeline("d1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Errorf("the draft has %d revisions after one write and one state change; want 1", len(vs))
	}
	ids, err := o.Drafts()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != hub.NoteID("d1") {
		t.Errorf("Drafts() = %v; the state file has become a draft", ids)
	}
}

// ---------------------------------------------------------------------------
// Criterion 16 — a review that could not be completed is not a pass
// ---------------------------------------------------------------------------

type answeringReviewer struct {
	answer string
	err    error
}

func (r answeringReviewer) Review(string, string) (string, error) { return r.answer, r.err }

func TestAnIncompleteReviewIsNeverAPass(t *testing.T) {
	cases := map[string]answeringReviewer{
		"the model could not be reached": {err: os.ErrDeadlineExceeded},
		"the model returned nothing":     {answer: ""},
		"the model returned whitespace":  {answer: "   \n\t"},
		"the model rambled":              {answer: "Well, it depends on what you mean by customer names…"},
		"the model answered in JSON":     {answer: `{"verdict":"pass"}`},
	}
	for name, r := range cases {
		got := Check(r, awkwardRules, "a draft")
		if got.Verdict != VerdictUndetermined {
			t.Errorf("%s: verdict = %v, want undetermined — this is not a pass and not a refusal", name, got.Verdict)
		}
		if got.StateFor() != StateReviewUndetermined {
			t.Errorf("%s: the draft would be left in state %q", name, got.StateFor())
		}
	}
	if got := Check(nil, awkwardRules, "a draft"); got.Verdict != VerdictUndetermined {
		t.Errorf("with no reviewer at all, verdict = %v", got.Verdict)
	}
}

func TestAReviewThatCompletesSaysWhichWay(t *testing.T) {
	if got := Check(answeringReviewer{answer: "pass"}, "r", "b"); got.Verdict != VerdictPassed {
		t.Errorf("a model that says pass gives %v", got.Verdict)
	}
	if got := Check(answeringReviewer{answer: "refuse: you named Acme"}, "r", "b"); got.Verdict != VerdictRefused {
		t.Errorf("a model that refuses gives %v", got.Verdict)
	} else if !strings.Contains(got.Reason, "Acme") {
		t.Errorf("the refusal loses the model's reason: %q", got.Reason)
	}
	// An error is not read for a verdict even when the text says pass.
	if got := Check(answeringReviewer{answer: "pass", err: os.ErrDeadlineExceeded}, "r", "b"); got.Verdict != VerdictUndetermined {
		t.Errorf("a model that errored after saying pass gives %v", got.Verdict)
	}
}

func TestTheThreeReviewOutcomesRenderPairwiseDistinctly(t *testing.T) {
	pairwiseDistinct(t, "a review outcome", map[string]string{
		"passed":       Check(answeringReviewer{answer: "pass"}, "r", "b").Render(),
		"refused":      Check(answeringReviewer{answer: "refuse: no"}, "r", "b").Render(),
		"undetermined": Check(answeringReviewer{err: os.ErrDeadlineExceeded}, "r", "b").Render(),
	})
}

// ---------------------------------------------------------------------------
// Criteria 18, 19 — the model, and the key
// ---------------------------------------------------------------------------

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const testSecret = "sk-ZQXJ-do-not-print-me-3f8a"

func TestWhetherAModelIsConfiguredHasThreeAnswersThatRenderDistinctly(t *testing.T) {
	yes := ReadModel(envOf(map[string]string{ModelEnv: "local-llama", ModelKeyEnv: testSecret}))
	if yes.Configured != tri.Yes {
		t.Errorf("a named model with a key reports %v", yes.Configured)
	}
	no := ReadModel(envOf(nil))
	if no.Configured != tri.No {
		t.Errorf("nothing configured reports %v", no.Configured)
	}

	// The undetermined answer is PROBED, not asserted into being: a key file that exists and cannot
	// be read. If this environment can read it anyway (running as root), the probe says so and the
	// case is skipped rather than passing vacuously.
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte(testSecret), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(keyFile); err == nil {
		t.Skip("this environment can read a 0o000 file, so an unreadable key file cannot be produced here")
	}
	und := ReadModel(envOf(map[string]string{ModelEnv: "local-llama", ModelKeyFileEnv: keyFile}))
	if und.Configured != tri.Undetermined {
		t.Fatalf("an unreadable key file reports %v, want undetermined — this is not 'no model'", und.Configured)
	}
	pairwiseDistinct(t, "whether a model is configured", map[string]string{
		"configured":              yes.Render(),
		"none configured":         no.Render(),
		"could not be determined": und.Render(),
	})
}

// CRITERION 18 at the level of the type: neither the rendering nor the default formatting of a
// ModelConfig may contain the key. The %v case is the one that bites — fmt reflects into unexported
// fields quite happily unless a String method stops it.
func TestTheKeyIsNeverInAnyRenderingOfTheModelConfig(t *testing.T) {
	cfg := ReadModel(envOf(map[string]string{ModelEnv: "local-llama", ModelKeyEnv: testSecret}))
	if cfg.Key() != testSecret {
		t.Fatalf("Key() = %q; this test is not holding the key it means to check for", cfg.Key())
	}
	for name, s := range map[string]string{
		"Render()": cfg.Render(),
		"String()": cfg.String(),
		"%v":       fmt.Sprintf("%v", cfg),
		"%s":       fmt.Sprintf("%s", cfg),
		"%+v":      fmt.Sprintf("%+v", cfg),
	} {
		if strings.Contains(s, testSecret) {
			t.Errorf("%s contains the person's key:\n  %s", name, s)
		}
	}
}
