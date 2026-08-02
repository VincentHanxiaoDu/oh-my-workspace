package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func TestResolveTakesTheExplicitPathFirst(t *testing.T) {
	want := filepath.Join(t.TempDir(), "elsewhere")
	got, err := Resolve(envOf(map[string]string{PathEnv: want, "HOME": "/home/somebody"}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

// ONE STORE PER DEVICE (criterion 4). The answer must not depend on where the person is standing.
func TestResolveIsTheSameFromEveryWorkingDirectory(t *testing.T) {
	env := envOf(map[string]string{"HOME": "/home/somebody"})
	first, err := Resolve(env)
	if err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(deep)
	second, err := Resolve(env)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("Resolve depends on the working directory: %q then %q — that is how a second store appears in a project folder", first, second)
	}
	if !filepath.IsAbs(second) {
		t.Fatalf("Resolve = %q, which is not absolute", second)
	}
}

func TestResolveWithNoHomeIsUndeterminedNotAFallback(t *testing.T) {
	_, err := Resolve(envOf(map[string]string{}))
	if !errors.Is(err, ErrPathUndetermined) {
		t.Fatalf("Resolve with no HOME = %v; want ErrPathUndetermined — a store in the current directory is exactly the store this product promises not to create", err)
	}
	if !strings.Contains(err.Error(), PathEnv) {
		t.Errorf("the error %q does not tell the person what to set", err)
	}
}

func TestResolveHonoursXDGDataHome(t *testing.T) {
	got, err := Resolve(envOf(map[string]string{"XDG_DATA_HOME": "/data", "HOME": "/home/somebody"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/data", "omw", "store") {
		t.Fatalf("Resolve = %q", got)
	}
}
