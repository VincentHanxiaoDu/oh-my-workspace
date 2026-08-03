package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// theSecret is the sentinel this package's tests configure as a credential. It is a value nothing
// in the product could produce by accident, so a grep for it in an output stream is a real finding
// and never a coincidence (criterion 7).
const theSecret = "sk-ZQXJ-do-not-print-me-3f8a"

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("creating a store to test against: %v", err)
	}
	return s
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// unreadableFile makes a file that exists and cannot be read, or skips.
//
// THE UNDETERMINED CASE IS PROBED, NOT CONSTRUCTED. A test that builds a Config{Credential:
// tri.Undetermined} by hand proves that a struct field can hold a value. This produces the actual
// condition on the actual filesystem, so the branch under test is the branch a person reaches. Where
// the environment can read a 0o000 file anyway — running as root — it says so and skips rather than
// passing vacuously.
func unreadableFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(content), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("this environment can read a 0o000 file, so an unreadable credential file cannot be produced here")
	}
	return p
}

// pairwiseDistinct fails if any two of the renderings are equal, naming both.
//
// COMPARED WITH EACH OTHER, NOT WITH EXPECTED STRINGS. Every distinguishability criterion in Issue
// #18 is of the form "these must not render identically", and an assertion against a literal passes
// happily on the day two branches are made to return the same literal.
func pairwiseDistinct(t *testing.T, what string, renders map[string]string) {
	t.Helper()
	for aName, a := range renders {
		if strings.TrimSpace(a) == "" {
			t.Errorf("%s: the %q rendering is empty; silence is not one of the answers", what, aName)
		}
		for bName, b := range renders {
			if aName < bName && a == b {
				t.Errorf("%s: %q and %q render identically:\n  %s", what, aName, bName, a)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria 1, 2, 3 — choosing a provider and supplying a key
// ---------------------------------------------------------------------------

// CRITERION 1: configuring is an explicit act, and a subsequent read reports the chosen provider.
func TestChoosingAProviderIsRecordedAndReadBack(t *testing.T) {
	s := newStore(t)
	if got := Read(envOf(nil), s); got.Provider != tri.No {
		t.Fatalf("a fresh store reports provider %v, want no", got.Provider)
	}
	if err := Use(s, "acme"); err != nil {
		t.Fatalf("choosing a provider: %v", err)
	}
	got := Read(envOf(nil), s)
	if got.Provider != tri.Yes || got.Name != "acme" {
		t.Errorf("after choosing acme, read back provider=%v name=%q", got.Provider, got.Name)
	}
}

// CRITERION 1, the other half: no provider and no credential is ever configured as a side effect.
//
// It is driven at the STORE, not at the API. Anything that configured a model as a side effect
// would have had to write the record, whatever function it went through, so this reads the record
// directly after exercising every read path this package has.
func TestNothingConfiguresAModelAsASideEffect(t *testing.T) {
	s := newStore(t)
	env := envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret})

	for i := 0; i < 3; i++ {
		cfg := Read(env, s)
		_ = cfg.Render()
		_ = cfg.View().Render()
		_, _ = CredentialThrough(cfg.View())
		_ = cfg.Configured()
	}

	var rec record
	if err := s.GetJSON(recordKind, recordID, &rec); err == nil {
		t.Fatalf("reading the configuration wrote a model record: %+v — configuring is an explicit act (§4.2)", rec)
	}
}

// CRITERION 2: a configuration that names no provider and one that names a provider never render
// identically, and the naming one says WHICH.
func TestNamingAProviderAndNamingNoneNeverRenderTheSame(t *testing.T) {
	none := Read(envOf(nil), nil)
	named := Read(envOf(map[string]string{EnvProvider: "acme"}), nil)
	if !strings.Contains(named.Render(), "acme") {
		t.Errorf("a chosen provider's rendering does not name it:\n  %s", named.Render())
	}
	pairwiseDistinct(t, "naming a provider", map[string]string{
		"none named":  none.Render(),
		"acme named":  named.Render(),
		"other named": Read(envOf(map[string]string{EnvProvider: "beta"}), nil).Render(),
	})
}

// CRITERION 3: chosen-with-no-credential is its own answer. It is neither of the other two, in the
// values AND in the rendering.
func TestAProviderChosenWithNoCredentialIsItsOwnAnswer(t *testing.T) {
	nothing := Read(envOf(nil), nil)
	half := Read(envOf(map[string]string{EnvProvider: "acme"}), nil)
	whole := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), nil)

	if half.Provider != tri.Yes {
		t.Errorf("a provider chosen with no credential reports provider %v, want yes", half.Provider)
	}
	if half.Credential != tri.No {
		t.Errorf("a provider chosen with no credential reports credential %v, want no", half.Credential)
	}
	if half.Configured() == tri.Yes {
		t.Error("a provider with no credential reports as fully configured")
	}
	if half.Render() == nothing.Render() {
		t.Error("a provider chosen with no credential renders as no-provider-configured")
	}
	pairwiseDistinct(t, "the credential half", map[string]string{
		"nothing chosen":        nothing.Render(),
		"chosen, no credential": half.Render(),
		"chosen, credential":    whole.Render(),
	})
}

// ---------------------------------------------------------------------------
// Criterion 15 — undetermined is not "no"
// ---------------------------------------------------------------------------

// THE THIRD VALUE, FROM BOTH OF ITS SOURCES.
//
// #9 had one — an unreadable credential file — and chose it because it is probe-able. This Issue
// keeps it and adds the one #9 could not have: a recorded choice in the store that will not read
// (criterion 15's "the credential store cannot be read"). Both are produced on a real filesystem
// and both must be undetermined, never "no model configured".
func TestTheThirdValueComesFromBothItsSourcesAndIsNeverNo(t *testing.T) {
	nothing := Read(envOf(nil), nil)

	keyFile := unreadableFile(t, theSecret)
	fileUndet := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredentialFile: keyFile}), nil)
	if fileUndet.Credential != tri.Undetermined {
		t.Fatalf("an unreadable credential file reports credential %v, want undetermined — this is not 'no credential'", fileUndet.Credential)
	}
	if fileUndet.Configured() != tri.Undetermined {
		t.Errorf("an unreadable credential file reports configured %v, want undetermined", fileUndet.Configured())
	}

	s := newStore(t)
	if err := Use(s, "acme"); err != nil {
		t.Fatal(err)
	}
	corruptTheRecord(t, s)
	recUndet := Read(envOf(nil), s)
	if recUndet.Provider != tri.Undetermined {
		t.Fatalf("an unreadable recorded choice reports provider %v, want undetermined — this is not 'no provider'", recUndet.Provider)
	}

	pairwiseDistinct(t, "whether a model is configured", map[string]string{
		"nothing configured":           nothing.Render(),
		"credential file unreadable":   fileUndet.Render(),
		"recorded choice unreadable":   recUndet.Render(),
		"configured":                   Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), nil).Render(),
		"chosen, no credential at all": Read(envOf(map[string]string{EnvProvider: "acme"}), nil).Render(),
		"chosen, credential file gone": Read(envOf(map[string]string{EnvProvider: "acme", EnvCredentialFile: filepath.Join(t.TempDir(), "absent")}), nil).Render(),
	})
}

// A NAMED CREDENTIAL FILE THAT DOES NOT EXIST IS A DETERMINED NEGATIVE, and this is the assertion
// that pins the third value's boundary. See the package comment: the filesystem answered, so this
// is a "no" with a precise reason, and only "there is something here I cannot read" is the third
// value. If a future Issue rules the other way, this test is the one that has to change, on
// purpose.
func TestAMissingCredentialFileIsANoAndNotUndetermined(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "not-there")
	got := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredentialFile: absent}), nil)
	if got.Credential != tri.No {
		t.Errorf("a credential file that does not exist reports %v, want no", got.Credential)
	}
	if !strings.Contains(got.Render(), absent) {
		t.Errorf("the rendering does not name the file that is missing:\n  %s", got.Render())
	}
}

// The zero value of a Config is undetermined in every half — the same rule tri encodes, restated
// where a struct returned from an error path would otherwise read as a confident negative.
func TestTheZeroConfigIsUndeterminedAndNotNo(t *testing.T) {
	var c Config
	if c.Provider != tri.Undetermined || c.Credential != tri.Undetermined || c.Configured() != tri.Undetermined {
		t.Errorf("the zero Config is provider=%v credential=%v configured=%v; all three must be undetermined",
			c.Provider, c.Credential, c.Configured())
	}
	if strings.TrimSpace(c.Render()) == "" {
		t.Error("the zero Config renders as silence")
	}
}

// Configured() combines the halves PAIRWISE. Every combination is checked, so a branch that
// compared a name to a string literal instead of a tri.Value to a tri.Value would show up here.
func TestConfiguredCombinesTheHalvesPairwise(t *testing.T) {
	for _, tc := range []struct {
		p, k, want tri.Value
	}{
		{tri.Yes, tri.Yes, tri.Yes},
		{tri.Yes, tri.No, tri.No},
		{tri.No, tri.Yes, tri.No},
		{tri.No, tri.No, tri.No},
		{tri.Yes, tri.Undetermined, tri.Undetermined},
		{tri.Undetermined, tri.Yes, tri.Undetermined},
		{tri.No, tri.Undetermined, tri.Undetermined},
		{tri.Undetermined, tri.No, tri.Undetermined},
		{tri.Undetermined, tri.Undetermined, tri.Undetermined},
	} {
		if got := (Config{Provider: tc.p, Credential: tc.k}).Configured(); got != tc.want {
			t.Errorf("provider=%v credential=%v gives %v, want %v", tc.p, tc.k, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Precedence, and clearing
// ---------------------------------------------------------------------------

func TestTheEnvironmentOverridesTheRecordedChoice(t *testing.T) {
	s := newStore(t)
	if err := Use(s, "recorded"); err != nil {
		t.Fatal(err)
	}
	if got := Read(envOf(map[string]string{EnvProvider: "from-env"}), s); got.Name != "from-env" {
		t.Errorf("with $%s set, the provider is %q; the environment overrules the record", EnvProvider, got.Name)
	}
	if got := Read(envOf(nil), s); got.Name != "recorded" {
		t.Errorf("with nothing in the environment, the provider is %q, want the recorded one", got.Name)
	}
}

// Clearing forgets the record and says nothing about the person's own environment, because it
// cannot touch it. Read after Clear falls back to the environment, which is the honest result.
func TestClearingForgetsTheRecordAndNotTheEnvironment(t *testing.T) {
	s := newStore(t)
	if err := Use(s, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := UseCredentialFile(s, "/some/path"); err != nil {
		t.Fatal(err)
	}
	if err := Clear(s); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if got := Read(envOf(nil), s); got.Provider != tri.No {
		t.Errorf("after clearing, the provider is %v, want no", got.Provider)
	}
	if got := Read(envOf(map[string]string{EnvProvider: "acme", EnvCredential: theSecret}), s); got.Configured() != tri.Yes {
		t.Error("clearing the record was allowed to invalidate the person's own environment")
	}
	// Clearing what is already clear is not an error; the state afterwards is what was asked for.
	if err := Clear(s); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
}

// Recording a credential FILE records the path and never the bytes. This reads the raw record off
// disk, because the guarantee is about what is on the disk and not about what an accessor returns.
func TestRecordingACredentialFileRecordsThePathAndNotTheBytes(t *testing.T) {
	s := newStore(t)
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, []byte(theSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Use(s, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := UseCredentialFile(s, keyPath); err != nil {
		t.Fatal(err)
	}
	// The credential IS resolvable through the recorded path — otherwise the sweep below proves
	// nothing, because there would be no secret in play.
	if got := Read(envOf(nil), s); got.Secret() != theSecret {
		t.Fatalf("the recorded credential file did not resolve; this test is not holding the secret it means to check for")
	}
	assertStoreHoldsNoSecret(t, s)
}

// The store's own bytes never contain the credential (criterion 7's "a full export of the local
// store"). Every file under the store root is read and searched.
func assertStoreHoldsNoSecret(t *testing.T, s *store.Store) {
	t.Helper()
	found := 0
	err := filepath.Walk(s.Path(), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		found++
		if strings.Contains(string(b), theSecret) {
			t.Errorf("%s contains the person's credential", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the store: %v", err)
	}
	if found == 0 {
		t.Fatal("the store walk read no files at all, so its pass proves nothing")
	}
}

// corruptTheRecord makes the model record present and unparseable. It is the store's own on-disk
// layout, used the way internal/drafts' tests use it.
func corruptTheRecord(t *testing.T, s *store.Store) {
	t.Helper()
	p := filepath.Join(s.Path(), "records", string(recordKind), recordID+".rec")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the record this test means to corrupt is not at %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte("this is not a record"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A CONTROL: the corruption must actually make the record unreadable, not merely different.
	var rec record
	if err := s.GetJSON(recordKind, recordID, &rec); err == nil {
		t.Fatal("the record still reads after being corrupted, so this test is not producing the state it names")
	}
}
