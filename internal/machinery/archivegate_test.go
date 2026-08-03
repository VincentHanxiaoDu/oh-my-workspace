package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A SHIPPED CHANGE LEFT IN `openspec/changes/` WAS CAUGHT ELEVEN TIMES IN ONE DAY BY A PERSON
// HAPPENING TO LOOK, AND ZERO TIMES BY A MECHANISM (Issue #77, carried verbatim into #108).
// Occurrence eleven landed inside the 171-second window in which the repair for eight-to-ten was
// open, and all three roles have done it. Convention was not the missing piece; a gate was.
//
// THE GATE ITSELF LIVES IN `.workflow/bin/check-generated.sh`, WHICH IS FRAMEWORK-OWNED AND IS
// REPLACED WHOLESALE BY EVERY `install.sh` RUN. That happened twice in one day and deleted a merged
// fix both times (#52→#58, #80→#95). So this file is the half that survives: it EXECUTES the
// installed script and goes red the moment a refresh removes the arm. It deliberately does NOT
// reimplement the rule in Go — a restatement passes while the real gate is broken, which is the
// failure this whole family of tests exists to avoid.
//
// THE RULE UNDER TEST, and it is the hard part:
//
//	A change has SHIPPED when every `### Requirement:` heading in its delta
//	`openspec/changes/<slug>/specs/<x>/spec.md` is already present in `openspec/specs/<x>/spec.md`.
//
// That is the literal post-condition of `openspec archive`, not a guess about intent. Ticked tasks
// were considered and rejected: measured on this repository, the shipped change and the in-flight
// one BOTH had every box ticked.

// archiveGate runs the installed gate inside dir against base and returns its combined output and
// exit code.
func archiveGate(t *testing.T, dir, base string) (string, int) {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".workflow", "bin", "check-generated.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the installed gate is not at %s, so nothing was driven: %v", script, err)
	}
	cmd := exec.Command("bash", script, base)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("could not execute %s: %v", script, err)
	}
	return string(out), code
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// seedProject builds a spec-driven repository holding one capability spec and one in-flight change
// whose delta declares deltaSpec. It returns the directory and the base sha every arm diffs against.
func seedProject(t *testing.T, specBody, deltaSpec string) (dir, base string) {
	t.Helper()
	needTool(t, "git")
	needTool(t, "bash")
	dir = t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")
	write(t, filepath.Join(dir, "openspec", "config.yaml"), "schema: spec-driven\n")
	write(t, filepath.Join(dir, "openspec", "specs", "notes", "spec.md"), specBody)
	write(t, filepath.Join(dir, "openspec", "changes", "c1", "specs", "notes", "spec.md"), deltaSpec)
	write(t, filepath.Join(dir, "openspec", "changes", "c1", "tasks.md"), "- [x] a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: seed a spec and an in-flight change")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("cannot read the seed sha: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

// realDelta reads a fixture's delta spec out of THIS repository. The two fixtures are real, they
// were both on `main` when this was written, and they are the pair the gate must tell apart. When
// one of them is finally archived the file moves, and this SKIPS with a reason rather than passing
// — a green from an arm whose fixture vanished says nothing.
func realDelta(t *testing.T, slug, capability string) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "openspec", "changes", slug, "specs", capability, "spec.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("fixture %s is no longer in openspec/changes/ (%v), so this arm determined nothing", slug, err)
	}
	return string(b)
}

// requirementHeadings returns the `### Requirement:` lines of a delta — what "already present in the
// generated spec" is measured over.
func requirementHeadings(t *testing.T, delta string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(delta, "\n") {
		if strings.HasPrefix(line, "### Requirement:") {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		t.Fatalf("this fixture declares no '### Requirement:' heading, so it cannot exercise the rule")
	}
	return out
}

// CRITERION 1 AND 4. The spec is regenerated, the change directory is left standing, and the gate
// must fail — naming the directory and the command that resolves it. A red that does not say what
// to do is how the same omission got worked out from first principles eleven separate times.
//
// DRIVEN WITH THE REAL `outbox-drafts-and-modes` DELTA, ten requirements and all of them, rather
// than a one-line toy: the live cases were large deltas and a rule that only works on one heading
// would have looked green here.
func TestARegeneratedSpecLeavingItsChangeDirectoryIsRefused(t *testing.T) {
	delta := realDelta(t, "outbox-drafts-and-modes", "notes")
	// The spec as archiving would leave it: every one of the delta's requirements merged in.
	spec := "# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n\n" +
		strings.Join(requirementHeadings(t, delta), "\nIt SHALL.\n\n") + "\nIt SHALL.\n"

	dir, base := seedProject(t, "# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n", delta)
	write(t, filepath.Join(dir, "openspec", "specs", "notes", "spec.md"), spec)
	// The archive of some other change, so the pre-existing hand-edit arm is satisfied and the
	// ONLY thing this arm can be reporting is the unarchived directory.
	write(t, filepath.Join(dir, "openspec", "changes", "archive", "2026-01-01-other", "tasks.md"), "- [x] a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: regenerate the spec")

	out, code := archiveGate(t, dir, base)
	if code == 0 {
		t.Fatalf("a spec regenerated with its change directory left in place PASSED. That is the defect, unguarded:\n%s", out)
	}
	if !strings.Contains(out, "openspec/changes/c1/") {
		t.Errorf("the refusal does not NAME the change directory, so a reader must go and find it:\n%s", out)
	}
	if !strings.Contains(out, "openspec archive c1") {
		t.Errorf("the refusal does not name the `openspec archive` command that resolves it:\n%s", out)
	}
}

// CRITERION 2. A gate that fails everything satisfies criterion 1 and stops all work, so the
// correct action passing UNCHANGED is half the claim and not a decoration.
func TestACorrectArchivePassesUnchanged(t *testing.T) {
	delta := realDelta(t, "outbox-drafts-and-modes", "notes")
	spec := "# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n\n" +
		strings.Join(requirementHeadings(t, delta), "\nIt SHALL.\n\n") + "\nIt SHALL.\n"

	dir, base := seedProject(t, "# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n", delta)
	write(t, filepath.Join(dir, "openspec", "specs", "notes", "spec.md"), spec)
	// Archiving: the directory moves under archive/ and the spec is regenerated, in one commit.
	git(t, dir, "mv", filepath.Join("openspec", "changes", "c1"),
		filepath.Join("openspec", "changes", "archive-tmp"))
	if err := os.MkdirAll(filepath.Join(dir, "openspec", "changes", "archive"), 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	git(t, dir, "mv", filepath.Join("openspec", "changes", "archive-tmp"),
		filepath.Join("openspec", "changes", "archive", "2026-01-01-c1"))
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: archive c1")

	out, code := archiveGate(t, dir, base)
	if code != 0 {
		t.Fatalf("a correctly archived change was BLOCKED, which makes the right action impossible:\n%s", out)
	}
}

// CRITERION 3, AND IT IS THE ONE THAT MAKES THE GATE SAFE TO ENABLE. `outbox-drafts-and-modes` is
// legitimately in flight; blocking it blocks #38 and #46, which are the release critical path. A
// gate that stops in-flight work is worse than no gate.
//
// AND NOT BLOCKING IS NOT THE SAME ACT AS ANSWERING 'NO'. The first version of this arm rendered a
// `0 of N` result as `so its work has not landed`, which is a determination — and on real `main` it
// was FALSE about `unplaceable-verdict-reported`, a change that had shipped in #98, printed in the
// same sentence shape and under the same label as the genuinely in-flight directory beside it.
// PRD §4.3 does not stop applying because the thing rendering the state is a gate. So this arm
// pins BOTH halves: the pass, and the refusal to claim more than the pass supports.
//
// The delta is the REAL one and the spec is the REAL `main` state — none of its ten requirements
// merged — beside somebody else's regeneration of the same capability file.
func TestAnInFlightChangeIsNotAccused(t *testing.T) {
	delta := realDelta(t, "outbox-drafts-and-modes", "notes")
	before := "# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n"
	after := before + "\n### Requirement: Something another change added\nIt SHALL.\n"

	dir, base := seedProject(t, before, delta)
	write(t, filepath.Join(dir, "openspec", "specs", "notes", "spec.md"), after)
	write(t, filepath.Join(dir, "openspec", "changes", "archive", "2026-01-01-other", "tasks.md"), "- [x] a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: archive a different change and regenerate notes")

	out, code := archiveGate(t, dir, base)
	if code != 0 {
		t.Fatalf("an in-flight change was accused of owing an archive. This blocks the release path:\n%s", out)
	}
	// PASSING FOR THE RIGHT REASON. A pass because the arm never looked at c1 is the vacuous green
	// this repository has been burned by; the gate must say it considered it.
	if !strings.Contains(out, "cannot tell for c1") {
		t.Errorf("the change passed without the gate saying it had looked at it:\n%s", out)
	}
	if !strings.Contains(out, "UNDETERMINED from this repository") {
		t.Errorf("a 0-of-N result was reported as a determination rather than as undetermined:\n%s", out)
	}
	// THE EXACT SENTENCE THAT WAS FALSE ON `main`. Pinned by its own assertion so a refresh, or a
	// later simplification, cannot quietly restore it.
	if strings.Contains(out, "so its work has not landed") {
		t.Errorf("the confident sentence is back. A 0-of-N result is not a finding that the work has "+
			"not landed — it was printed about `unplaceable-verdict-reported`, which had shipped:\n%s", out)
	}
}

// A PARTIAL MATCH IS THE SAME KIND OF FACT AS AN ABSENT ONE, and it reaches the branch by different
// arithmetic — a branch written for `hit == 0` can be wrong for `0 < hit < n`. Half a change's
// requirements present says nothing about whether the other half is unwritten or merely unmerged.
func TestAPartialMatchIsUndeterminedRatherThanAnswered(t *testing.T) {
	delta := "## ADDED Requirements\n\n### Requirement: One\nIt SHALL.\n\n#### Scenario: a\n" +
		"- **WHEN** a\n- **THEN** b\n\n### Requirement: Two\nIt SHALL.\n\n#### Scenario: c\n" +
		"- **WHEN** c\n- **THEN** d\n"
	before := "# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n"
	after := before + "\n### Requirement: One\nIt SHALL.\n\n#### Scenario: a\n- **WHEN** a\n- **THEN** b\n"

	dir, base := seedProject(t, before, delta)
	write(t, filepath.Join(dir, "openspec", "specs", "notes", "spec.md"), after)
	write(t, filepath.Join(dir, "openspec", "changes", "archive", "2026-01-01-other", "tasks.md"), "- [x] a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: half of c1's requirements have landed")

	out, code := archiveGate(t, dir, base)
	if code != 0 {
		t.Fatalf("a partial match was BLOCKED, which is a determination the gate cannot support:\n%s", out)
	}
	if !strings.Contains(out, "1 of 2 requirements") {
		t.Errorf("the gate did not report the count it actually measured:\n%s", out)
	}
	if !strings.Contains(out, "UNDETERMINED from this repository") {
		t.Errorf("a partial match was not reported as undetermined:\n%s", out)
	}
}

// A DELTA THE RULE CANNOT ANSWER FOR MUST BE UNDETERMINED, NOT GREEN BY VACUITY. "Every one of zero
// requirements is present" is true, and would accuse a delta that declares nothing. `could not
// determine` and `determined to be nothing` are different values here, and this is that rule applied
// to a gate rather than to a command.
func TestADeltaWithNoRequirementsIsUndeterminedRatherThanAccused(t *testing.T) {
	dir, base := seedProject(t,
		"# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n",
		"## ADDED Requirements\n")
	write(t, filepath.Join(dir, "openspec", "specs", "notes", "spec.md"),
		"# notes Specification\n\n## Purpose\nRewritten.\n\n## Requirements\n\n### Requirement: X\nIt SHALL.\n")
	write(t, filepath.Join(dir, "openspec", "changes", "archive", "2026-01-01-other", "tasks.md"), "- [x] a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: regenerate beside an empty delta")

	out, code := archiveGate(t, dir, base)
	if code != 0 {
		t.Fatalf("a delta the rule cannot answer for was BLOCKED rather than reported as undetermined:\n%s", out)
	}
	if !strings.Contains(out, "UNDETERMINED") {
		t.Errorf("a delta the rule cannot answer for passed SILENTLY, which reads as 'checked and fine':\n%s", out)
	}
}

// SCOPING IS THE OTHER HALF OF CRITERION 3'S SAFETY. A pull request that regenerates no capability
// spec must not be made responsible for a change directory somebody else left standing — that is
// how a gate becomes something every author routes around.
func TestAPullRequestThatRegeneratesNothingIsNotJudged(t *testing.T) {
	dir, base := seedProject(t,
		"# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n",
		realDelta(t, "outbox-drafts-and-modes", "notes"))
	write(t, filepath.Join(dir, "elsewhere.txt"), "unrelated\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: something else entirely")

	out, code := archiveGate(t, dir, base)
	if code != 0 {
		t.Fatalf("a pull request regenerating nothing was blocked by the archive arm:\n%s", out)
	}
	if !strings.Contains(out, "regenerates no capability spec") {
		t.Errorf("the arm did not say WHY it had nothing to judge, so the pass reads as a check that ran:\n%s", out)
	}
}

// A LOOKUP FAILURE IS NOT A VERDICT. An unreachable base must fail and must not report that the
// archive question was answered — the same shape as every other gate in this directory.
func TestAnUnreachableBaseIsNotReportedAsAClearArchive(t *testing.T) {
	dir, _ := seedProject(t,
		"# notes Specification\n\n## Purpose\nNotes.\n\n## Requirements\n",
		"## ADDED Requirements\n\n### Requirement: X\nIt SHALL.\n")
	out, code := archiveGate(t, dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if code == 0 {
		t.Fatalf("an unreachable base PASSED:\n%s", out)
	}
	if strings.Contains(out, "no change directory was found whose content has already landed") {
		t.Errorf("a lookup failure was rendered as a completed archive check:\n%s", out)
	}
}

// AND THE INSTALLED SCRIPT MUST CARRY ITS OWN PROOF. `run-gates.sh` runs every gate's `--self-test`
// first, so the arms have to be in the shipped file rather than only here — that is what makes the
// upstream patch arrive with its evidence attached.
func TestTheInstalledGateSelfTestCoversTheArchiveArm(t *testing.T) {
	needTool(t, "bash")
	script := filepath.Join(repoRoot(t), ".workflow", "bin", "check-generated.sh")
	cmd := exec.Command("bash", script, "--self-test")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the installed gate's own self-test failed:\n%s", out)
	}
	if !strings.Contains(string(out), "change directory standing") ||
		!strings.Contains(string(out), "UNDETERMINED rather than answered as no") {
		t.Errorf("the self-test passed without claiming to cover the archive arm, so a refresh could remove it silently:\n%s", out)
	}
}
