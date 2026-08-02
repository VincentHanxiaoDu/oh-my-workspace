package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// newStore creates a store in a clean temporary directory and returns it.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Create(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s
}

// snapshot records every file under dir with its content, so a later comparison can say "byte
// identical" rather than "still about the same size".
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", dir, err)
	}
	return out
}

// CRITERION 1.
func TestCreateMakesExactlyOneStoreAndNamesIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	s, err := Create(root)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !filepath.IsAbs(s.Path()) {
		t.Errorf("Path() = %q, which is not absolute — criterion 1 asks for the absolute path", s.Path())
	}
	if s.Path() != root {
		t.Errorf("Path() = %q, want %q", s.Path(), root)
	}
	if Exists(root) != tri.Yes {
		t.Errorf("Exists = %v after Create; want Yes", Exists(root))
	}
	if s.ID() == "" {
		t.Error("the store has no id")
	}
}

// CRITERION 3: a second creation is not a second store, does not empty the first, and differs from
// the first run by exit-code-bearing error value alone.
func TestSecondCreateRefusesAndLeavesTheStoreByteIdentical(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	s, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutJSON("ticket", "abc", map[string]string{"title": "do the thing"}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)

	second, err := Create(root)
	if second != nil {
		t.Fatalf("Create returned a second store at %s", second.Path())
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Create = %v; want ErrAlreadyExists", err)
	}
	after := snapshot(t, root)
	if len(before) != len(after) {
		t.Fatalf("the store changed: %d files before, %d after", len(before), len(after))
	}
	for name, content := range before {
		if after[name] != content {
			t.Errorf("%s is not byte-identical after the refused second create", name)
		}
	}
	if got, err := s.Get("ticket", "abc"); err != nil || !strings.Contains(string(got.Data), "do the thing") {
		t.Fatalf("the existing record did not survive the refused second create: %v / %s", err, got.Data)
	}
}

// CRITERION 5: refused, and nothing left behind at the target.
func TestCreateRefusesASynchronisingLocationAndLeavesNothing(t *testing.T) {
	_, inside := syncRoot(t, "Dropbox", ".dropbox")
	_, err := Create(inside)
	if !errors.Is(err, ErrPathSynchronising) {
		t.Fatalf("Create into a Dropbox root = %v; want ErrPathSynchronising", err)
	}
	if _, statErr := os.Stat(inside); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("something was left at %s after the refusal (%v) — criterion 5 requires no directory or file behind", inside, statErr)
	}
	var pe *PathError
	if !errors.As(err, &pe) || !strings.Contains(pe.Detail, "Dropbox") {
		t.Errorf("the refusal does not name Dropbox: %v", err)
	}
}

// CRITERION 6: the three failures are three, and they are told apart by VALUE, not by prose.
func TestTheThreeCreationFailuresAreDistinguishable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which cannot be denied write permission")
	}
	_, dropbox := syncRoot(t, "Dropbox", ".dropbox")

	missing := filepath.Join(t.TempDir(), "no", "such", "parent", "store")

	readonly := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readonly, 0o755) })

	cases := []struct {
		name   string
		path   string
		want   error
		notThe []error
	}{
		{"this is Dropbox", dropbox, ErrPathSynchronising, []error{ErrPathMissing, ErrPermissionDenied}},
		{"this path does not exist", missing, ErrPathMissing, []error{ErrPathSynchronising, ErrPermissionDenied}},
		{"I lack permission", filepath.Join(readonly, "store"), ErrPermissionDenied, []error{ErrPathSynchronising, ErrPathMissing}},
	}
	messages := map[string]string{}
	for _, c := range cases {
		_, err := Create(c.path)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: Create = %v; want %v", c.name, err, c.want)
		}
		for _, other := range c.notThe {
			if errors.Is(err, other) {
				t.Errorf("%s: the failure is ALSO %v — criterion 6 requires these three be told apart", c.name, other)
			}
		}
		messages[c.name] = err.Error()
	}
	for a := range messages {
		for b := range messages {
			if a != b && messages[a] == messages[b] {
				t.Errorf("%q and %q produce the same message: %s", a, b, messages[a])
			}
		}
	}
}

// CRITERION 7 (the positive half): a confirmed non-synchronising path succeeds. The negative half —
// a synthetic sync root is refused on whatever platform this runs on — is in sync_test.go, and it
// names no operating system on purpose.
func TestCreateSucceedsAtAConfirmedLocalPath(t *testing.T) {
	s := newStore(t)
	if got := s.SyncState(); got.State != tri.No {
		t.Fatalf("a store in a plain temporary directory reports %v (%s); want No", got.State, got.Describe())
	}
}

// CRITERION 9 and the OPEN DECISION: undetermined is neither the success nor the refusal, and has
// its own error value so no caller can collapse it into either.
func TestUndeterminedSyncIsNeitherCreatedNorRefusedAsSynchronising(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	parent := filepath.Join(t.TempDir(), "opaque")
	if err := os.MkdirAll(filepath.Join(parent, "here"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o111); err != nil {
		t.Skipf("permissions not honoured here: %v", err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })

	target := filepath.Join(parent, "here", "store")
	s, err := Create(target)
	if s != nil {
		t.Fatalf("a store was created at %s while the sync probe was undetermined", target)
	}
	if !errors.Is(err, ErrSyncUndetermined) {
		t.Fatalf("Create = %v; want ErrSyncUndetermined", err)
	}
	if errors.Is(err, ErrPathSynchronising) {
		t.Error("the undetermined case reports as a determined refusal — §4.3 forbids rendering an undetermined state as a settled one, in either direction")
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("something was left at %s", target)
	}
}

// CRITERION 2, the part this branch can drive: Open never creates. Every command in the product
// reaches a store through Open, so a store cannot appear as a side effect of reading.
func TestOpenNeverCreates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	for i := 0; i < 3; i++ {
		if _, err := Open(root); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Open on a missing store = %v; want ErrNotFound", err)
		}
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open created %s", root)
	}
	if Exists(root) != tri.No {
		t.Fatalf("Exists = %v; want No", Exists(root))
	}
}

// A store that is present and empty and a store that is absent are DIFFERENT FACTS (criterion 2).
func TestAnEmptyStoreIsNotAnAbsentStore(t *testing.T) {
	s := newStore(t)
	recs, err := s.List("ticket")
	if err != nil {
		t.Fatalf("List on an empty store = %v; an empty store is a success", err)
	}
	if len(recs) != 0 {
		t.Fatalf("List = %d records on a fresh store", len(recs))
	}
	if Exists(s.Path()) != tri.Yes {
		t.Fatal("a fresh empty store does not report as present")
	}
}

// CRITERION 13: unreadable is never presented as empty. Two ways in — a damaged marker, and a
// damaged record — and both have to say so.
func TestAnUnreadableStoreIsNeverAnEmptyOne(t *testing.T) {
	t.Run("damaged marker", func(t *testing.T) {
		s := newStore(t)
		if err := os.WriteFile(filepath.Join(s.Path(), markerName), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Open(s.Path())
		if !errors.Is(err, ErrUnreadable) {
			t.Fatalf("Open on a damaged marker = %v; want ErrUnreadable", err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("a damaged store reports as an absent one")
		}
		if Exists(s.Path()) != tri.Undetermined {
			t.Errorf("Exists on a damaged store = %v; want Undetermined — it was not established that there is no store", Exists(s.Path()))
		}
	})

	t.Run("damaged record", func(t *testing.T) {
		s := newStore(t)
		if err := s.Put(Record{Kind: "ticket", ID: "one", Data: []byte("the body")}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Path(), recordsDir, "ticket", "one"+recordSuffix)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Replace the payload while leaving the checksum alone: damage beneath the product, of the
		// kind an atomic rename cannot and does not protect against. The envelope stays perfectly
		// well-formed JSON, so only the checksum can catch this.
		var envelope recordFile
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.Data = []byte("the bady")
		damaged, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, damaged, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get("ticket", "one"); !errors.Is(err, ErrUnreadable) {
			t.Fatalf("Get on a damaged record = %v; want ErrUnreadable", err)
		}
		if _, err := s.List("ticket"); !errors.Is(err, ErrUnreadable) {
			t.Fatalf("List with a damaged record = %v; want ErrUnreadable — skipping it would report the store as holding one fewer ticket, silently", err)
		}
	})
}

// CRITERION 11, the non-crash half: a payload that does not fit is an error, never a zeroed struct.
func TestGetJSONNeverHandsBackAPlausibleEmptyRecord(t *testing.T) {
	s := newStore(t)
	if err := s.Put(Record{Kind: "ticket", ID: "one", Data: []byte(`"a bare string, not a ticket"`)}); err != nil {
		t.Fatal(err)
	}
	var ticket struct {
		Title string `json:"title"`
	}
	err := s.GetJSON("ticket", "one", &ticket)
	if err == nil {
		t.Fatalf("GetJSON succeeded and produced title=%q — a missing value and a real value must never render identically", ticket.Title)
	}
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("GetJSON = %v; want ErrUnreadable", err)
	}
}

// CRITERION 19: the local half stands alone. No hub, no model, and the store holds both containers.
func TestTheStoreHoldsBothContainersWithNoHubAndNoModel(t *testing.T) {
	for _, k := range []string{"OMW_HUB", "OMW_MODEL", "OMW_TOKEN"} {
		t.Setenv(k, "")
	}
	root := filepath.Join(t.TempDir(), "store")
	s, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutJSON("ticket", "t1", map[string]string{"title": "a ticket"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutJSON("draft", "d1", map[string]string{"body": "a draft note"}); err != nil {
		t.Fatal(err)
	}

	// A SUBSEQUENT COMMAND MUST NOT FIND IT ABSENT OR UNUSABLE. Reopened from scratch, as the next
	// process would.
	again, err := Open(root)
	if err != nil {
		t.Fatalf("reopening a store that reported success = %v — it half-worked", err)
	}
	kinds, err := again.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 {
		t.Fatalf("kinds = %v; want the inbox and the outbox as two separate kinds", kinds)
	}
	var ticket struct {
		Title string `json:"title"`
	}
	if err := again.GetJSON("ticket", "t1", &ticket); err != nil || ticket.Title != "a ticket" {
		t.Fatalf("ticket = %+v, err = %v", ticket, err)
	}
}

// A record comes back byte-identical or not at all — including bytes that are not valid JSON and
// not valid UTF-8, because a draft may carry an attachment.
func TestRecordsRoundTripArbitraryBytes(t *testing.T) {
	s := newStore(t)
	payload := []byte{0x00, 0xff, 0xfe, '\n', 'h', 'i', 0x80}
	if err := s.Put(Record{Kind: "draft", ID: "d1", Data: payload}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("draft", "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, payload) {
		t.Fatalf("round trip changed the bytes: %v -> %v", payload, got.Data)
	}
}

func TestMissingRecordIsNotAnUnreadableStore(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("ticket", "nope"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Get on a missing record = %v; want ErrRecordNotFound", err)
	}
	if _, err := s.Get("ticket", "nope"); errors.Is(err, ErrUnreadable) {
		t.Error("a missing record reports as an unreadable store")
	}
}

// An id that escapes its directory would put unpublished data outside the store — invariant 5.
func TestRecordNamesCannotEscapeTheStore(t *testing.T) {
	s := newStore(t)
	for _, bad := range []string{"../escape", "a/b", "", ".hidden", "..", strings.Repeat("x", 200)} {
		if err := s.Put(Record{Kind: "ticket", ID: bad, Data: []byte("x")}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Put with id %q = %v; want ErrInvalidName", bad, err)
		}
		if err := s.Put(Record{Kind: Kind(bad), ID: "ok", Data: []byte("x")}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Put with kind %q = %v; want ErrInvalidName", bad, err)
		}
	}
}

// CRITERION 8: the sync answer is re-asked, not remembered. A store created legitimately and later
// moved under a sync root is reported as synchronising.
func TestAStoreThatBecomesSynchronisingIsReported(t *testing.T) {
	s := newStore(t)
	if got := s.SyncState(); got.State != tri.No {
		t.Fatalf("fresh store reports %v", got.State)
	}
	// The location is placed under a sync root after the fact.
	if err := os.WriteFile(filepath.Join(filepath.Dir(s.Path()), ".dropbox"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.SyncState()
	if got.State != tri.Yes {
		t.Fatalf("after the location was placed under a sync root, SyncState = %v (%s); want Yes — the refusal is not a one-time gate", got.State, got.Describe())
	}
	if got.Provider != "Dropbox" {
		t.Errorf("provider = %q", got.Provider)
	}
}

// CRITERION 16, as a property: every determination has three renderings and none is silence.
func TestEveryDeterminationHasThreeRenderings(t *testing.T) {
	s := newStore(t)
	seen := map[string]bool{}
	for _, v := range []tri.Value{tri.Yes, tri.No, tri.Undetermined} {
		r := v.Render("present", "absent")
		if strings.TrimSpace(r) == "" {
			t.Fatalf("%v renders as silence", v)
		}
		if seen[r] {
			t.Fatalf("two answers render as %q", r)
		}
		seen[r] = true
	}
	if Exists(s.Path()) != tri.Yes || Exists(filepath.Join(t.TempDir(), "nope")) != tri.No {
		t.Fatal("Exists does not answer the two determined cases")
	}
}
