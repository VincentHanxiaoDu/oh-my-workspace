package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// undeterminedPath builds a location whose sync status genuinely cannot be determined.
func undeterminedPath(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read a directory whose permissions forbid it")
	}
	opaque := filepath.Join(t.TempDir(), "opaque")
	if err := os.MkdirAll(filepath.Join(opaque, "here"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(opaque, 0o111); err != nil {
		t.Skipf("this filesystem will not take the permission bits: %v", err)
	}
	t.Cleanup(func() { os.Chmod(opaque, 0o755) })
	return filepath.Join(opaque, "here", "store")
}

// CRITERION 23: the override creates, and the creation is otherwise a normal one.
func TestAcceptUndeterminedLocationCreates(t *testing.T) {
	target := undeterminedPath(t)

	if _, err := Create(target); !errors.Is(err, ErrSyncUndetermined) {
		t.Fatalf("without the option, Create = %v; want ErrSyncUndetermined", err)
	}
	s, err := Create(target, AcceptUndeterminedLocation())
	if err != nil {
		t.Fatalf("with the option, Create = %v; want a store", err)
	}
	if s.Path() != target {
		t.Errorf("Path() = %q, want %q", s.Path(), target)
	}
	if err := s.PutJSON("ticket", "t1", map[string]string{"title": "a ticket"}); err != nil {
		t.Errorf("the overridden store cannot hold a record: %v", err)
	}
}

// CRITERION 24: the override does not reach the determined refusal. This is the one that matters
// most — an override that leaked into this branch would make §4.1 a preference.
func TestAcceptUndeterminedLocationDoesNotOverrideAKnownSyncRoot(t *testing.T) {
	_, inside := syncRoot(t, "Dropbox", ".dropbox")

	_, plain := Create(inside)
	_, overridden := Create(inside, AcceptUndeterminedLocation())

	if !errors.Is(overridden, ErrPathSynchronising) {
		t.Fatalf("Create with the override into Dropbox = %v; want ErrPathSynchronising — §4.1's refusal is not overridable", overridden)
	}
	if overridden.Error() != plain.Error() {
		t.Errorf("the override changed the refusal:\nwith:    %v\nwithout: %v", overridden, plain)
	}
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the overridden refusal left something at %s", inside)
	}
}

// CRITERION 25: the store remembers, and the probe is re-run rather than replaced.
func TestAnOverriddenStoreRemembersAndStillProbes(t *testing.T) {
	target := undeterminedPath(t)
	s, err := Create(target, AcceptUndeterminedLocation())
	if err != nil {
		t.Fatal(err)
	}
	if !s.CreatedAtUndeterminedLocation() {
		t.Error("the store does not record that its location was never confirmed")
	}
	if got := s.SyncState(); got.State != tri.Undetermined {
		t.Fatalf("after an override, SyncState = %v (%s); want Undetermined — the person's decision is not a determination", got.State, got.Describe())
	}

	// A fresh process must see the same thing.
	again, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	if !again.CreatedAtUndeterminedLocation() {
		t.Error("the override is forgotten when the store is reopened")
	}

	// And a store created normally must NOT carry the flag, or the distinction is worthless.
	clean, err := Create(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	if clean.CreatedAtUndeterminedLocation() {
		t.Error("a store created at a confirmed location claims it was created under the override")
	}
}

// CRITERION 4 at the package level: one store per device, enforced through the registry.
func TestAsDeviceStoreRefusesASecondStoreElsewhere(t *testing.T) {
	home := t.TempDir()
	env := envOf(map[string]string{"HOME": home})
	dir := t.TempDir()
	a, b := filepath.Join(dir, "A"), filepath.Join(dir, "B")

	if _, err := Create(a, AsDeviceStore(env)); err != nil {
		t.Fatalf("the first Create = %v", err)
	}
	second, err := Create(b, AsDeviceStore(env))
	if second != nil {
		t.Fatalf("a second store was created at %s", second.Path())
	}
	if !errors.Is(err, ErrAnotherStoreRegistered) {
		t.Fatalf("Create at a second path = %v; want ErrAnotherStoreRegistered", err)
	}
	if _, serr := os.Stat(b); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("something was left at %s", b)
	}

	// Resolve must find the registered one, from an environment naming nothing else.
	got, rerr := Resolve(env)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if got != a {
		t.Fatalf("Resolve = %q; want the registered store %q", got, a)
	}
}

// Without the option, Create does not touch the device registry at all — the daemon and the tests
// both need a store that is not "the" store.
func TestCreateWithoutAsDeviceStoreRegistersNothing(t *testing.T) {
	home := t.TempDir()
	env := envOf(map[string]string{"HOME": home})

	if _, err := Create(filepath.Join(t.TempDir(), "store")); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Registered(env); found != tri.No || err != nil {
		t.Fatalf("Registered = %v/%v after a Create with no AsDeviceStore; want No", found, err)
	}
}

// A pointer to a store that no longer exists is stale, not binding: otherwise deleting a store
// leaves the machine permanently unable to make another.
func TestAStalePointerDoesNotBlockCreation(t *testing.T) {
	home := t.TempDir()
	env := envOf(map[string]string{"HOME": home})
	dir := t.TempDir()
	a, b := filepath.Join(dir, "A"), filepath.Join(dir, "B")

	if _, err := Create(a, AsDeviceStore(env)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(b, AsDeviceStore(env)); err != nil {
		t.Fatalf("after the registered store was deleted, Create = %v — the machine has no way back", err)
	}
	got, err := Resolve(env)
	if err != nil || got != b {
		t.Fatalf("Resolve = %q, %v; want the new store %q", got, err, b)
	}
}

// A pointer that CANNOT BE READ is a different matter from one that is absent: ignoring it is how
// the second store gets made.
func TestAnUnreadablePointerIsUndeterminedNotAbsent(t *testing.T) {
	home := t.TempDir()
	env := envOf(map[string]string{"HOME": home})

	path, err := RegistryPath(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := Registered(env); found != tri.Undetermined {
		t.Fatalf("Registered with a damaged pointer = %v; want Undetermined", found)
	}
	if _, err := Resolve(env); !errors.Is(err, ErrUnreadable) && !errors.Is(err, ErrPathUndetermined) {
		t.Fatalf("Resolve with a damaged pointer = %v; want an undetermined or unreadable failure, never a silent fall through to the default", err)
	}
	if _, err := Create(filepath.Join(t.TempDir(), "store"), AsDeviceStore(env)); err == nil {
		t.Fatal("Create went ahead while it could not tell whether this device already has a store")
	}
}

// The registry holds a path and nothing else — it must never become a second copy of a person's
// unpublished work (criterion 14).
func TestTheRegistryHoldsOnlyAPath(t *testing.T) {
	home := t.TempDir()
	env := envOf(map[string]string{"HOME": home})
	root := filepath.Join(t.TempDir(), "store")

	s, err := Create(root, AsDeviceStore(env))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "ZQXJ-A-DRAFT-NOBODY-ELSE-SHOULD-HOLD"
	if err := s.PutJSON("draft", "d1", map[string]string{"body": secret}); err != nil {
		t.Fatal(err)
	}

	path, err := RegistryPath(env)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 512 {
		t.Errorf("the device pointer is %d bytes; it should hold one path", len(body))
	}
	for _, phrase := range []string{secret, base64Middle(secret, 0), base64Middle(secret, 1), base64Middle(secret, 2)} {
		if phrase != "" && strings.Contains(string(body), phrase) {
			t.Fatalf("the device pointer contains unpublished content: %s", body)
		}
	}
}
