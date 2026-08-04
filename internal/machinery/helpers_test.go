package machinery

// SHARED HELPERS, CARRIED VERBATIM when the script-driving suites were replaced by the framework's
// own Python suite.
//
// They lived in reviewgate_test.go, which was one of the files that went. archivegate_test.go and
// frameworkpaths_test.go still need them: those two are about THIS repository — its unarchived
// changes, and its commits on framework-owned paths — rather than about framework logic, so they
// stayed and are still driven from Go.
//
// `repoRoot` walks up to the go.mod ON PURPOSE and is not reimplemented over `git rev-parse`: it
// works in a checkout with no git available and in a worktree, and rewriting a helper while moving
// it is how a move becomes a change nobody reviewed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A MISSING TOOL IS A SKIP AND IT IS NAMED. Not a pass: a suite that goes green because the thing
// it drives is absent has reported success having examined nothing, which is the failure this
// repository exists to be about. The skip names the tool, so the gap is visible rather than silent.
func needTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not on PATH, so the machinery cannot be driven here", name)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("cannot resolve working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %q, so the repository root cannot be found", dir)
		}
		dir = parent
	}
}
