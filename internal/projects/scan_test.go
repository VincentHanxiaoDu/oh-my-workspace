package projects_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// mkfile creates a file, making its parents.
func mkfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// nested returns root/d1/d2/.../dN.
func nested(root string, levels int) string {
	p := root
	for i := 1; i <= levels; i++ {
		p = filepath.Join(p, "d"+string(rune('0'+i)))
	}
	return p
}

// unreadableDirsWork PROBES whether this environment can actually make a directory unreadable,
// rather than naming an operating system or a user.
//
// WHY PROBE. `if os.Geteuid() == 0 { skip }` is a name, and it is wrong in both directions: a
// container with CAP_DAC_OVERRIDE reads a 0000 directory as a non-root user, and some CI images run
// as uid 0 with the capability dropped. The only honest question is "can I, here, right now, be
// refused by a directory I have chmodded" — so this asks it. A test that cannot be set up must skip
// loudly; one that skips because of a guess about the environment is the wrong test.
func unreadableDirsWork(t *testing.T) bool {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "probe")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("probe mkdir: %v", err)
	}
	mkfile(t, filepath.Join(dir, "f"), "x")
	if err := os.Chmod(dir, 0o000); err != nil {
		return false
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err := os.ReadDir(dir)
	return err != nil
}

// CRITERION 15, first half: a file eight directory levels down is reached by a default scan.
func TestADefaultScanReachesEightLevelsDown(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(nested(root, 8), "deep.txt"), "hello")

	st := projects.Scan(root, projects.DefaultDepth)
	if st.Files != 1 {
		t.Errorf("a file 8 levels below the root was not reached by a default scan (depth %d): "+
			"Files=%d, DepthLimitReached=%v", projects.DefaultDepth, st.Files, st.DepthLimitReached)
	}
	if st.DepthLimitReached {
		t.Errorf("the default scan reported truncation for a tree that fits inside its own default")
	}
}

// CRITERION 15, second half: the limit is not fixed at build time, and a smaller value demonstrably
// changes what is reported for the SAME tree.
func TestASmallerDepthLimitChangesWhatTheSameTreeReports(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "top.txt"), "x")
	mkfile(t, filepath.Join(nested(root, 5), "deep.txt"), "x")

	deep := projects.Scan(root, 8)
	shallow := projects.Scan(root, 2)

	if deep.Files == shallow.Files {
		t.Errorf("the same tree reports %d files at depth 8 and at depth 2 — "+
			"the limit is not actually in force", deep.Files)
	}
	if deep.Files != 2 {
		t.Errorf("depth 8 over this tree: Files=%d, want 2", deep.Files)
	}
	if shallow.Files != 1 {
		t.Errorf("depth 2 over this tree: Files=%d, want 1 (only the top-level file)", shallow.Files)
	}
}

// DepthFor reads the limit from the environment, so the daemon — which has no command line — uses
// the same limit as the CLI. Criterion 15 with criterion 14.
func TestTheDepthLimitComesFromTheEnvironment(t *testing.T) {
	cases := map[string]int{
		"":       projects.DefaultDepth,
		"3":      3,
		"  4  ":  4,
		"banana": projects.DefaultDepth, // a typo must not silently scan nothing
		"-1":     projects.DefaultDepth,
	}
	for raw, want := range cases {
		got := projects.DepthFor(func(k string) string {
			if k == projects.DepthEnv {
				return raw
			}
			return ""
		})
		if got != want {
			t.Errorf("$%s=%q gave depth %d, want %d", projects.DepthEnv, raw, got, want)
		}
	}
}

// CRITERION 16: reaching the limit is visible, and a truncated walk is distinguishable from a
// complete one. Compared AGAINST EACH OTHER, not against literals.
func TestATruncatedWalkNeverRendersAsACompleteOne(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(nested(root, 4), "deep.txt"), "x")

	truncated := projects.Scan(root, 2)
	complete := projects.Scan(root, 8)

	if !truncated.DepthLimitReached {
		t.Errorf("the walk stopped at the limit and did not record it — "+
			"content below depth 2 is unreported and the listing does not say so: %+v", truncated)
	}
	if complete.DepthLimitReached {
		t.Errorf("a walk that finished inside the limit claimed it was truncated")
	}

	a, b := projects.DescribeScan(truncated), projects.DescribeScan(complete)
	if a == b {
		t.Errorf("a truncated walk and a complete walk render identically: %q", a)
	}
	if a == "" || b == "" {
		t.Errorf("a walk rendered as silence: truncated=%q complete=%q", a, b)
	}
}

// CRITERION 17: symlinks are not followed, including one pointing at its own ancestor. The walk
// terminating at all is half the assertion; the other half is that the link's target is not counted.
func TestSymlinksAreNotFollowedAndDoNotLoop(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "real.txt"), "x")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// A link pointing at its own ancestor. Followed, this walk never ends.
	if err := os.Symlink(root, filepath.Join(sub, "loop")); err != nil {
		t.Skipf("this filesystem will not make a symlink: %v", err)
	}
	// A link to somewhere else entirely, whose contents must not be counted as this project's.
	other := t.TempDir()
	mkfile(t, filepath.Join(other, "not-mine.txt"), "x")
	if err := os.Symlink(other, filepath.Join(root, "elsewhere")); err != nil {
		t.Fatal(err)
	}

	st := projects.Scan(root, projects.DefaultDepth) // hangs forever if links are followed
	if st.Files != 1 {
		t.Errorf("Files=%d, want 1 — the walk counted something reached through a symlink", st.Files)
	}
}

// CRITERION 18: pruned names are SKIPPED DURING the walk, not walked and filtered afterwards.
//
// Driven exactly as the criterion suggests: a deep tree under a pruned name, and an assertion that
// the scan did not traverse it. DirsVisited is what makes "did not traverse" observable — a
// post-filter produces the same Files count and would pass a test that only checked the count.
func TestPrunedDirectoriesAreNotTraversedAtAll(t *testing.T) {
	for _, name := range []string{
		"node_modules", ".venv", "venv", "__pycache__", ".git",
		"dist", "build", ".next", "target", ".cache", "vendor",
		".anything-dot-prefixed",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			mkfile(t, filepath.Join(root, "kept.txt"), "x")
			// Twelve levels of directories under the pruned name, each holding a file.
			deep := filepath.Join(root, name)
			for i := 0; i < 12; i++ {
				deep = filepath.Join(deep, "x")
				mkfile(t, filepath.Join(deep, "buried.txt"), "x")
			}

			st := projects.Scan(root, projects.DefaultDepth)
			if st.Files != 1 {
				t.Errorf("%s: Files=%d, want 1 — content inside a pruned directory is in the state", name, st.Files)
			}
			// The root, and nothing else. Entering the pruned directory even once is the
			// walk-then-filter shape the criterion rules out.
			if st.DirsVisited != 1 {
				t.Errorf("%s: the walk entered %d directories, want 1 (the root alone).\n"+
					"  The pruned directory was traversed and its contents filtered afterwards, "+
					"which criterion 18 forbids: on a real repository that is the difference between "+
					"a poll that fits in the two-second interval and one that does not.", name, st.DirsVisited)
			}
		})
	}
}

// IsPruned is asserted against the criterion's own list rather than against the implementation's
// copy of it, so a name silently dropped from the map is caught here.
func TestThePruneListIsTheOneTheIssueNames(t *testing.T) {
	for _, name := range strings.Fields(
		"node_modules .venv venv __pycache__ .git dist build .next target .cache vendor") {
		if !projects.IsPruned(name) {
			t.Errorf("%s is on Issue #4 criterion 18's prune list and this build does not prune it", name)
		}
	}
	if !projects.IsPruned(".anything") {
		t.Error("a dot-prefixed directory is not pruned; criterion 18 prunes every one of them")
	}
	if projects.IsPruned("src") {
		t.Error("an ordinary directory is being pruned")
	}
}

// CRITERION 19: inside a git repository the ignore set is git's, including cases a naive .gitignore
// reader gets wrong. This build parses no ignore file anywhere.
func TestInsideAGitRepositoryTheIgnoreSetIsGitsOwn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed here; the git-backed path cannot be driven")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")

	// The cases a naive reader gets wrong, all at once:
	//   - a NESTED ignore file that only applies below itself
	//   - a NEGATION pattern re-including something an outer rule excluded
	//   - a REPOSITORY-LEVEL EXCLUDE that lives in no .gitignore at all
	mkfile(t, filepath.Join(root, "kept.txt"), "x")
	mkfile(t, filepath.Join(root, ".gitignore"), "*.log\n!keep-me.log\n")
	mkfile(t, filepath.Join(root, "dropped.log"), "x")
	mkfile(t, filepath.Join(root, "keep-me.log"), "x")
	mkfile(t, filepath.Join(root, "sub", ".gitignore"), "*.tmp\n")
	mkfile(t, filepath.Join(root, "sub", "gone.tmp"), "x")
	mkfile(t, filepath.Join(root, "sub", "here.txt"), "x")
	mkfile(t, filepath.Join(root, "top.tmp"), "x") // NOT ignored: the .tmp rule is nested under sub/
	mkfile(t, filepath.Join(root, ".git", "info", "exclude"), "secret-*\n")
	mkfile(t, filepath.Join(root, "secret-thing"), "x")

	st := projects.Scan(root, projects.DefaultDepth)
	if st.IgnoreSource != "git" {
		t.Fatalf("IgnoreSource=%q inside a git repository, want %q", st.IgnoreSource, "git")
	}

	// kept.txt, keep-me.log, top.tmp, sub/here.txt. The .gitignore files themselves are dot-prefixed
	// and pruned by criterion 18, which applies on top of git's answer.
	if st.Files != 4 {
		t.Errorf("Files=%d, want 4.\n"+
			"  Expected present: kept.txt, keep-me.log (negation), top.tmp (nested rule does not "+
			"reach the root), sub/here.txt.\n"+
			"  Expected absent: dropped.log, sub/gone.tmp (nested rule), secret-thing "+
			"(.git/info/exclude — in no .gitignore at all).", st.Files)
	}
}

// The negative half of criterion 19: OUTSIDE a repository the prune list and the dot rule are the
// WHOLE policy, and no ignore file changes the result.
func TestOutsideAGitRepositoryNoIgnoreFileIsRead(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, ".gitignore"), "ignored.txt\n*\n")
	mkfile(t, filepath.Join(root, "ignored.txt"), "x")
	mkfile(t, filepath.Join(root, "kept.txt"), "x")

	st := projects.Scan(root, projects.DefaultDepth)
	if st.IgnoreSource != "prune-list" {
		t.Fatalf("IgnoreSource=%q outside a repository, want %q", st.IgnoreSource, "prune-list")
	}
	// Both files are present; the .gitignore itself is dot-prefixed and so is not content.
	if st.Files != 2 {
		t.Errorf("Files=%d, want 2 — a .gitignore outside a repository changed the result, "+
			"so something parsed it", st.Files)
	}
}

// CRITERION 21: an unreadable subdirectory mid-walk is reported as unreadable, does not abort the
// walk, and the entry is distinguishable from one produced by a walk that read everything.
func TestAnUnreadableSubdirectoryIsReportedAndTheWalkContinues(t *testing.T) {
	if !unreadableDirsWork(t) {
		t.Skip("this environment reads a 0000 directory anyway; an unreadable subdirectory cannot be built here")
	}
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "a.txt"), "x")
	mkfile(t, filepath.Join(root, "readable", "b.txt"), "x")
	locked := filepath.Join(root, "locked")
	mkfile(t, filepath.Join(locked, "hidden.txt"), "x")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	partial := projects.Scan(root, projects.DefaultDepth)
	if !partial.PartiallyRead() {
		t.Fatalf("an unreadable subdirectory was not reported: %+v", partial)
	}
	if partial.Files != 2 {
		t.Errorf("Files=%d, want 2 — the walk did not continue past the unreadable directory", partial.Files)
	}
	if partial.Readable == tri.Yes {
		t.Error("a partially-read project claims to have been readable; " +
			"part of it was not read and that is not a determined 'readable: yes'")
	}
	if partial.Empty() == tri.No || partial.Empty() == tri.Yes {
		t.Errorf("a partially-read project determined its emptiness (%s); it has not", partial.Empty())
	}

	// The other half: distinguishable from a complete scan.
	clean := t.TempDir()
	mkfile(t, filepath.Join(clean, "a.txt"), "x")
	mkfile(t, filepath.Join(clean, "readable", "b.txt"), "x")
	full := projects.Scan(clean, projects.DefaultDepth)
	if projects.DescribeState(partial) == projects.DescribeState(full) {
		t.Errorf("a partially-read project renders exactly as a complete scan: %q",
			projects.DescribeState(partial))
	}
}

// CRITERION 10 at the root: a directory that exists and whose own contents cannot be read.
func TestADirectoryThatExistsAndCannotBeReadIsUndetermined(t *testing.T) {
	if !unreadableDirsWork(t) {
		t.Skip("this environment reads a 0000 directory anyway")
	}
	root := filepath.Join(t.TempDir(), "locked")
	mkfile(t, filepath.Join(root, "x.txt"), "x")
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	st := projects.Scan(root, projects.DefaultDepth)
	if st.Present != tri.Yes {
		t.Errorf("Present=%s, want yes — the directory is there, it just cannot be read", st.Present)
	}
	if st.Readable != tri.Undetermined {
		t.Errorf("Readable=%s, want %s", st.Readable, tri.Undetermined)
	}
	if st.Empty() != tri.Undetermined {
		t.Errorf("Empty()=%s — a directory that could not be read has been "+
			"DETERMINED TO BE NOTHING, which is the one collapse this project forbids", st.Empty())
	}
}

// CRITERION 13's other half, at the scan: a path that is not a directory is not a present project.
func TestAPathThatIsNotADirectoryIsNotAPresentProject(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a-file")
	mkfile(t, f, "x")
	st := projects.Scan(f, projects.DefaultDepth)
	if st.Present == tri.Yes {
		t.Error("a regular file scanned as a present project directory")
	}
}
