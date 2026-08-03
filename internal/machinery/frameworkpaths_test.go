package machinery

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A FIX MADE IN A FRAMEWORK-OWNED FILE IS REVERTED BY THE NEXT INSTALL, SILENTLY. `.workflow/bin/`
// and `.github/` are installed by agent-dev-flow and replaced wholesale on every refresh; a commit
// of ours landing there is undone as a normal part of upgrading, and `git status` afterwards is the
// only place it shows. That is not a control. Issue #52 was found here, fixed here, reviewed here
// and merged here, and the next `install.sh` run deleted it — 2 insertions, 24 deletions (#58).
//
// This test is the detection. It walks `git log` over the framework-owned paths, separates commits
// that REFRESH the framework from commits that are LOCAL fixes to it, and names the files whose
// most recent commit is a local one — the files that the next install will revert.
//
// WHAT THIS CANNOT DO, AND WHOSE IT IS. Criterion 1 of #58 asks that the report be driven by
// "running the installer, and asserting it says what it is about to overwrite". `install.sh` is not
// in this repository — it lives under `.agent-dev-flow/`, which is gitignored — so making the
// installer announce its overwrites is a change to the installer and cannot be made here. What is
// here is the other half: the same fact, detected from a project-owned file, on every `make ci`.
//
// `.claude/commands/` is framework-owned too, and is deliberately NOT in the list below: it is
// gitignored here, so git history cannot see it and a check over it would examine nothing while
// looking like it examined something. Said out loud rather than silently dropped.
var frameworkOwned = []string{".workflow/bin/", ".github/"}

// THE DISCRIMINATOR, and criterion 2. A file that differs because the framework moved forward is a
// normal upgrade; a file carrying a local commit is not. Both are `M` in `git status`.
//
// The two shapes are told apart by the ROLE that made the commit, read from its `Agent:` trailer —
// the same trailer every handoff in this repository already carries. `pm` installs and refreshes
// the framework; every other role builds against it. So a commit over these paths whose trailer is
// `pm` is a refresh, and one by any other role — or with no trailer at all — is a local edit.
//
// Why this and not the subject line: `chore: refresh agent-dev-flow` versus `fix(machinery): …` is
// prose, and a subject-prefix match fails the moment someone writes `chore(machinery): …` for a
// real local fix — which is exactly the subject this Issue carries. The trailer is structural, it
// is written by the handoff rather than by taste, and on this repository's history it separates the
// six commits over these paths correctly: five refreshes by `pm`, and 17c8f9c, the local fix that
// was reverted.
//
// What it misses, stated so it is not discovered later: a local machinery fix committed BY pm reads
// as a refresh. Absence of a trailer is treated as local, not as a refresh, so an untrailered
// commit is reported rather than waved through.
const installingRole = "pm"

var agentTrailer = regexp.MustCompile(`(?m)^Agent:[ \t]*(\S+)[ \t]*$`)

type fwCommit struct {
	sha   string
	role  string // "" when the commit carries no Agent: trailer
	files []string
}

func (c fwCommit) local() bool { return c.role != installingRole }

func (c fwCommit) why() string {
	if c.role == "" {
		return "no Agent: trailer, so it is not demonstrably a refresh"
	}
	return "Agent: " + c.role + ", which is not the installing role"
}

// gitOut runs git in dir and fails the test if it cannot.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// upstreamStamp PROBES for the installer's stamp rather than naming it as a requirement. The stamp
// is gitignored, so it is present on a developer's machine and absent on CI, and the two cases are
// different answers: the sha it records, or a STATED reason it could not be determined. Never the
// empty string, and never silence — "could not be determined" and "there is nothing there" are
// different values in this project, and a probe that collapsed them would be the same class of
// defect this whole file exists to catch.
func upstreamStamp(root string) (sha, reason string) {
	path := filepath.Join(root, ".agent-dev-flow")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Sprintf("the installer stamp %s could not be read (%v), so the installed "+
			"framework revision could not be determined here — it is gitignored and absent on CI", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "sha="); ok && v != "" {
			return v, ""
		}
	}
	return "", fmt.Sprintf("%s exists but records no sha=, so the installed framework revision "+
		"could not be determined", path)
}

// frameworkCommits walks the history of the framework-owned paths, newest first.
func frameworkCommits(t *testing.T, root string) []fwCommit {
	t.Helper()
	args := []string{"log", "--format=%x1e%H%x1f%B%x1f", "--name-only", "--"}
	args = append(args, frameworkOwned...)

	var out []fwCommit
	for _, rec := range strings.Split(gitOut(t, root, args...), "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 3)
		if len(parts) < 3 {
			t.Fatalf("cannot parse a git log record: %q", rec)
		}
		c := fwCommit{sha: strings.TrimSpace(parts[0])}
		if m := agentTrailer.FindAllStringSubmatch(parts[1], -1); len(m) > 0 {
			c.role = m[len(m)-1][1] // the last trailer wins, as git interpret-trailers reads them
		}
		for _, f := range strings.Split(parts[2], "\n") {
			if f = strings.TrimSpace(f); f != "" {
				c.files = append(c.files, f)
			}
		}
		out = append(out, c)
	}
	return out
}

// declared reads the reconciliation file: the set of (sha-prefix, path) pairs a human has claimed
// are no longer a loss waiting to happen, with the reason they gave.
func declared(t *testing.T, root string) map[string]string {
	t.Helper()
	path := filepath.Join(root, "internal", "machinery", "framework-local-commits.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s, which is the file this check reads its declarations from: %v", path, err)
	}
	out := map[string]string{}
	for n, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) < 3 {
			t.Fatalf("%s:%d is not `<commit-sha> <path> <reason>`: %q", path, n+1, line)
		}
		if strings.TrimSpace(f[2]) == "" {
			t.Fatalf("%s:%d declares %s with no reason. A declaration without a reason is a "+
				"silencer, and this file is not one.", path, n+1, f[1])
		}
		out[f[0]+" "+f[1]] = f[2]
	}
	return out
}

// TestNoUndeclaredLocalCommitsOnFrameworkOwnedPaths is #58 criteria 1 (the half that is ours) and
// 2. It NAMES THE FILES rather than counting them, because a count is not something a dev can act
// on: the answer to "this will be reverted" is to move the fix, and moving it requires knowing
// which one.
func TestNoUndeclaredLocalCommitsOnFrameworkOwnedPaths(t *testing.T) {
	needTool(t, "git")
	root := repoRoot(t)
	if err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Run(); err != nil {
		t.Skipf("%s is not a git working tree, so whether a framework-owned file carries a local "+
			"commit COULD NOT BE DETERMINED here — this is not a pass", root)
	}

	commits := frameworkCommits(t, root)

	// THE CONTROL. A walk that found nothing has not shown that nothing is wrong; it has shown
	// that it looked in the wrong place — a renamed framework directory, a shallow clone, a
	// pathspec that stopped matching. A check that examined nothing must not report success.
	if len(commits) == 0 {
		t.Fatalf("no commit in this repository's history touches any of %v, which cannot be true "+
			"of a repository that has agent-dev-flow installed. This check examined nothing, so "+
			"it is red rather than green: fix the paths or the clone depth.", frameworkOwned)
	}
	t.Logf("%d commits touch %v", len(commits), frameworkOwned)

	tracked := map[string]bool{}
	for _, f := range strings.Fields(gitOut(t, root, append([]string{"ls-files", "--"}, frameworkOwned...)...)) {
		tracked[f] = true
	}
	if len(tracked) == 0 {
		t.Fatalf("git tracks no file under %v, so there is nothing here to protect and this "+
			"check is asserting nothing", frameworkOwned)
	}

	// Newest first, so the first commit seen touching a file is the last one to have touched it.
	// A refresh landing after a local fix means upstream has since spoken for that file.
	lastTouch := map[string]fwCommit{}
	for _, c := range commits {
		for _, f := range c.files {
			if _, seen := lastTouch[f]; !seen && tracked[f] {
				lastTouch[f] = c
			}
		}
	}

	decls := declared(t, root)
	used := map[string]bool{}
	var reported []string

	for f, c := range lastTouch {
		if !c.local() {
			continue
		}
		matched := ""
		for key := range decls {
			sha, path, _ := strings.Cut(key, " ")
			if path == f && strings.HasPrefix(c.sha, sha) {
				matched, used[key] = key, true
				break
			}
		}
		if matched != "" {
			t.Logf("%s carries local commit %s, declared reconciled: %s", f, c.sha[:7], decls[matched])
			continue
		}
		reported = append(reported, fmt.Sprintf("  %s\n      last touched by %s (%s)", f, c.sha[:7], c.why()))
	}

	for key := range decls {
		if !used[key] {
			// Not an error: a later refresh over the same file makes the declaration unnecessary,
			// and going red for that would be crying wolf over a normal upgrade. Worth saying so
			// the file can be pruned.
			t.Logf("declaration %q no longer applies and can be removed", key)
		}
	}

	if len(reported) > 0 {
		stamp, reason := upstreamStamp(root)
		where := "installed framework revision: " + stamp
		if stamp == "" {
			where = reason
		}
		t.Fatalf("these framework-owned files carry a LOCAL commit and will be reverted, without "+
			"warning, by the next `install.sh` run:\n\n%s\n\n"+
			"They are framework-owned: the installer replaces `.workflow/bin/` and `.github/` "+
			"wholesale, and it is documented as doing so. A fix that must survive an upgrade goes "+
			"in a project-owned file — `internal/machinery/` for an assertion, `.workflow/<role>/"+
			"AGENT.md` for an instruction. If the fix belongs upstream, send it to agent-dev-flow, "+
			"reinstall, and declare it in internal/machinery/framework-local-commits.txt with the "+
			"reason.\n%s", strings.Join(reported, "\n"), where)
	}
}

// TestUpstreamStampProbeIsThreeValued pins the probe's contract rather than its answer, because
// its answer differs by machine: the stamp is gitignored, so it is there locally and gone on CI.
// What must hold everywhere is that the two absences are distinguishable — a sha, or a REASON.
// An empty reason for an empty sha would be a probe that answered "nothing is installed" when it
// meant "I could not look", and this project treats those as different values.
func TestUpstreamStampProbeIsThreeValued(t *testing.T) {
	root := repoRoot(t)

	sha, reason := upstreamStamp(root)
	switch {
	case sha != "" && reason != "":
		t.Errorf("the probe answered both a sha (%q) and a reason (%q); it must answer one", sha, reason)
	case sha == "" && reason == "":
		t.Error("the probe answered neither a sha nor a reason, so a caller cannot tell " +
			"'could not be determined' from 'determined to be nothing'")
	case sha != "":
		t.Logf("installed framework revision: %s", sha)
	default:
		t.Logf("not determined, with a reason: %s", reason)
	}

	// The absent case, forced, so it is exercised on the machine where the stamp is present too.
	if s, r := upstreamStamp(t.TempDir()); s != "" || r == "" {
		t.Errorf("with no stamp present the probe answered sha=%q reason=%q, want an empty sha "+
			"and a stated reason", s, r)
	}
}
