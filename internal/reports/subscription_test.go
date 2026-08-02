package reports

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// newStore makes a store to work in. It probes rather than naming an environment: if a store cannot
// be created here at all, the tests that need one skip with the reason rather than failing for
// something that is not the subject under test.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(dir, store.AcceptUndeterminedLocation())
	if err != nil {
		t.Skipf("this environment cannot create a store to test against: %v", err)
	}
	return s
}

// CRITERION 24, THE HALF THAT IS EASY TO GET WRONG: a list with one bad selector in it stores
// NOTHING — not the good selectors, not a partial record, and it does not damage what was there.
func TestARefusedListStoresNothingAndLeavesTheOldOneAlone(t *testing.T) {
	s := newStore(t)

	if _, err := Save(s, "daily", "git:full, token_usage:digest"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, _, err := Load(s, "daily")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The good half comes FIRST, so a parser that wrote as it went would already have stored it.
	if _, err := Save(s, "daily", "git:full, channel:enormous"); err == nil {
		t.Fatal("a list with an unknown granularity in it was accepted")
	}
	after, _, err := Load(s, "daily")
	if err != nil {
		t.Fatalf("Load after the refusal: %v", err)
	}
	if strings.Join(after.Selectors, ", ") != strings.Join(before.Selectors, ", ") {
		t.Errorf("the refused write changed the stored subscription:\n  before: %v\n  after:  %v",
			before.Selectors, after.Selectors)
	}

	// And a refused write of a NEW name leaves no record at all — not an empty one.
	if _, err := Save(s, "fresh", ":full"); err == nil {
		t.Fatal("`:full` was accepted")
	}
	if _, _, err := Load(s, "fresh"); !errors.Is(err, ErrNoSuchSubscription) {
		t.Errorf("after a refusal, Load(fresh) = %v, want no such subscription — a refusal must not "+
			"leave a partially-applied subscription behind", err)
	}
}

// CRITERIA 1-4 THROUGH THE STORE: what is read back is what was written, including the dotted path.
func TestSubscriptionReadsBackExactlyAsWritten(t *testing.T) {
	s := newStore(t)
	written := "git:full, token_usage:digest, *:summary, git.commit:event, !channel"
	if _, err := Save(s, "everything", written); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sub, sels, err := Load(s, "everything")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := strings.Join(sub.Selectors, ", ")
	if got != written {
		t.Errorf("read back as %q, want %q", got, written)
	}
	if len(sels) != 5 {
		t.Fatalf("read back %d selectors, want 5", len(sels))
	}
	if sels[3].Subject != "git.commit" {
		t.Errorf("the dotted path came back as %q — a store round trip collapsed it", sels[3].Subject)
	}
}

// A SUBJECT THAT COULD NOT BE READ, DRIVEN FOR REAL. The record on disk is damaged after it is
// written, so the store's own ErrUnreadable is what reaches the report — not an error a test source
// invented. This is the path criterion 18 is actually about.
func TestADamagedRecordMakesItsSubjectUndeterminedAndNotEmpty(t *testing.T) {
	s := newStore(t)
	if err := WriteActivity(s, Item{ID: "c1", Subject: "git", Kind: "commit", Text: "a real commit"}); err != nil {
		t.Fatalf("WriteActivity: %v", err)
	}
	if err := WriteActivity(s, Item{ID: "s1", Subject: "token_usage", Kind: "spend", Text: "4210"}); err != nil {
		t.Fatalf("WriteActivity: %v", err)
	}
	src := StoreSource{Store: s, HubConfigured: false}

	// Healthy first, so the damage below is the only difference between the two reports.
	healthy := Build(mustParse(t, "git:count"), src)
	if !healthy.Determined() || !strings.Contains(healthy.Render(), "count: 1") {
		t.Fatalf("the healthy report is wrong before anything was damaged:\n%s", healthy.Render())
	}

	damage(t, s, ActivityKind("git"), "c1")

	r := Build(mustParse(t, "git:count, token_usage:count"), src)
	out := r.Render()
	if r.Determined() {
		t.Errorf("a damaged record read back as a determined answer:\n%s", out)
	}
	if strings.Contains(out, "count: 0") {
		t.Errorf("a damaged record rendered as `count: 0`:\n%s", out)
	}
	if !strings.Contains(out, undeterminedLine) {
		t.Errorf("a damaged record did not render as undetermined:\n%s", out)
	}
	// CRITERION 19 ON THE REAL PATH: the other subject still reports.
	if !strings.Contains(out, "token_usage:count") || !strings.Contains(out, "count: 1") {
		t.Errorf("the undamaged subject stopped reporting:\n%s", out)
	}
}

// damage corrupts a stored record's payload in place, the way a bad sector or a truncating backup
// tool would. It finds the file by listing the kind directory rather than by rebuilding the store's
// naming convention, so this helper does not silently stop damaging anything if that changes.
func damage(t *testing.T, s *store.Store, kind store.Kind, id string) {
	t.Helper()
	dir := filepath.Join(s.Path(), "records", string(kind))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	found := false
	for _, e := range entries {
		if !strings.Contains(e.Name(), id) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		// The checksum is over the payload, so altering the payload is what the store detects.
		altered := strings.Replace(string(body), `"data":"`, `"data":"AA`, 1)
		if altered == string(body) {
			t.Fatalf("the record envelope in %s is not the shape this helper knows how to damage", p)
		}
		if err := os.WriteFile(p, []byte(altered), 0o600); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
		found = true
	}
	if !found {
		t.Fatalf("no record for %q under %s — this helper damaged nothing, so a pass would say nothing", id, dir)
	}
}

// A store that holds no activity at all is a QUIET DAY, not an undetermined one. The opposite
// mistake to the test above, and just as easy to make.
func TestAnEmptyStoreIsAQuietDayNotAnUndeterminedOne(t *testing.T) {
	s := newStore(t)
	r := Build(mustParse(t, "git:count"), StoreSource{Store: s})
	out := r.Render()
	if !r.Determined() {
		t.Errorf("an empty store reported as undetermined:\n%s", out)
	}
	if !strings.Contains(out, noActivityLine) {
		t.Errorf("an empty store did not report a quiet day:\n%s", out)
	}
}

// The store-backed source answers the hub-supplied subject from a local fact and reads nothing.
func TestStoreSourceAnswersTheHubSubjectWithoutAHub(t *testing.T) {
	s := newStore(t)
	_, err := StoreSource{Store: s, HubConfigured: false}.Activity("published_notes")
	if !errors.Is(err, ErrNoHubConfigured) {
		t.Errorf("Activity(published_notes) with no hub = %v, want %v", err, ErrNoHubConfigured)
	}
	items, err := StoreSource{Store: s, HubConfigured: true}.Activity("published_notes")
	if err != nil || len(items) != 0 {
		t.Errorf("with a hub configured and nothing stored: items=%v err=%v, want an empty determined answer", items, err)
	}
}

// A subscription name is checked before anything is written, for the same reason a selector is.
func TestSubscriptionNamesAreChecked(t *testing.T) {
	s := newStore(t)
	for _, name := range []string{"", "../escape", "Daily", "with space"} {
		if _, err := Save(s, name, "git:full"); !errors.Is(err, ErrInvalidSubscriptionName) {
			t.Errorf("Save(%q) = %v, want an invalid-name refusal", name, err)
		}
	}
}
