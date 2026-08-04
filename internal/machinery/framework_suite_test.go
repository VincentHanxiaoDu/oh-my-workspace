package machinery

// THE CONSUMER'S JOB CHANGED WHEN THE MACHINERY STOPPED BEING SHELL.
//
// These tests existed because the framework was bash, and bash can only be tested from outside it:
// stub `gh` onto PATH, run the script, match strings in its output. Seven files and roughly three
// thousand lines did that, and they earned their place — every one of them pins a defect that
// reached production here, and several caught a refresh reverting a merged fix.
//
// The machinery is Python now and carries its own suite, in the same repository as the code, run by
// the framework's CI and by `--self-test` here. Re-implementing those assertions in Go would mean
// two suites drifting apart over one behaviour — which is the exact failure this project keeps
// finding in itself, one level up: `pr-authors.sh` exists because independence was derived in three
// places and two of them disagreed.
//
// SO THE PROPERTY THIS FILE HOLDS IS NARROWER AND IT IS THE ONE THAT IS THIS REPOSITORY'S TO HOLD:
// **the framework installed here still passes its own tests, and it is asked in OUR CI rather than
// only in its own.** A framework that is green upstream and broken in this checkout is exactly what
// a refresh produces, and it is what `frameworkpaths_test.go` and the installer's refusal guard
// against from the other side.
//
// WHAT WAS DELIBERATELY NOT DELETED:
//   - frameworkpaths_test.go — about THIS repository's commits on framework paths, not about
//     framework logic. Language-independent and still load-bearing.
//   - archivegate_test.go    — drives check-generated.sh, which is still bash and still ours to
//     drive, and pins the eleven-in-one-day unarchived-change defect.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A SUITE THAT CANNOT BE RUN IS NOT A PASSING SUITE. Every failure mode below is reported as a
// failure, never skipped: "could not check" and "checked and fine" is the one confusion this whole
// repository is against, and a t.Skip() here would reintroduce it in the test that exists to
// prevent it.
func TestTheInstalledFrameworkPassesItsOwnSuite(t *testing.T) {
	root := repoRoot(t)
	adf := filepath.Join(root, ".workflow", "adf")
	if _, err := os.Stat(adf); err != nil {
		t.Fatalf(".workflow/adf is not installed (%v). The machinery this repository runs on is "+
			"absent, so nothing below was checked — that is a broken install, not a passing test.", err)
	}

	cmd := exec.Command("python3", filepath.Join(adf, "queue.py"), "--self-test")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	text := string(out)

	if err != nil {
		if strings.Contains(text, "is `uv` installed?") {
			t.Fatalf("the framework's suite could not be RUN here (uv missing), so this repository "+
				"has no evidence its machinery works:\n%s", text)
		}
		t.Fatalf("the framework's own suite fails in this checkout:\n%s", text)
	}
	if !strings.Contains(text, "self-test passed") {
		t.Fatalf("the suite exited 0 without saying it passed. A run that COLLECTED NOTHING exits "+
			"0 too, and the two must not look the same:\n%s", text)
	}
}

// AND THE ENTRY POINTS THE REST OF THIS REPOSITORY CALLS MUST STILL ANSWER. gates.yml, the role
// prompts and run-gates.sh all invoke `.workflow/bin/<name>.sh`; the implementation moved beneath
// them and the paths did not. A shim that does not resolve is a gate that cannot run.
func TestEveryEntryPointStillAnswers(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{
		"queue.sh", "pr.sh", "check-review.sh", "gh-budget.sh", "pr-authors.sh",
		"watch-prs.sh", "watch-queue.sh", "watch-all.sh",
	} {
		p := filepath.Join(root, ".workflow", "bin", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s is missing, and gates.yml or a role prompt invokes it: %v", name, err)
			continue
		}
		cmd := exec.Command("bash", p, "--self-test")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s --self-test failed: %v\n%s", name, err, out)
		}
	}
}

