package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE WATCHES DETECT THE EVENT AND THEN PRINT THE PART THAT IS NOT THE REASON (Issue #64).
//
// Two defects, one shape: the signal fires correctly and the text attached to it is about something
// else, so a correct alarm reads as noise and gets treated as noise.
//
//  1. `watch-queue.sh` captured `queue.sh` with `2>&1` and kept `cut -c1-200` — the FIRST 200
//     characters. What comes back is everything the queue printed before it died, so the head is
//     headings and work items and the failure is the LAST line. A real
//     `dial tcp 140.82.116.6:443: operation timed out` sat past character 200 and was cut off; the
//     emission was read as a misfire, and a round of a release day went on proving the watch right.
//
//  2. `watch-prs.sh` reported `MAIN IS RED at 19f05904 (failure) — YOU merged into it, so this is
//     yours to fix`. Main WAS red and the alarm was right to fire, but the merges did not cause it:
//     the failing check was `Branch name and commit convention` on a DIRECT PUSH to main by the
//     framework, one parent, which the merge-commit exemption does not cover. The watch inferred the
//     cause from who merged last — a proxy for authorship that stops measuring it the moment
//     anything else can redden main, and something else can, by design. Sending the merger to fix a
//     commit they did not write is the same error `pr-authors.sh` exists to end.
//
// THESE TESTS EXECUTE THE INSTALLED SCRIPTS, for the reason recorded in `doc.go`: `.workflow/bin/`
// is replaced wholesale by the next `install.sh` run, and a restatement of the logic here would go
// green while the shipped watch was printing the wrong half again.

// A queue that answers at length and THEN fails. A stub that fails immediately cannot observe this
// defect at all — the output has to overflow the budget before the truncation can throw anything
// away, which is exactly why the shipped self-test never saw it.
const longThenFailingQueue = `#!/usr/bin/env bash
echo "FEATURES WHOSE WORK HAS LANDED — UAT on main and CLOSE"
for i in $(seq 1 12); do echo "  #$i  feat(notes): draft notes into the outbox and publish them"; done
echo "gh: dial tcp 140.82.116.6:443: operation timed out" >&2
exit 1
`

// The whole of its output is the reason, well under the budget. Nothing may be trimmed from it and
// nothing may be added to it.
const shortFailingQueue = `#!/usr/bin/env bash
echo "boom" >&2
exit 1
`

// queueReasonFixture stages the installed watch with a queue stub of the caller's choosing.
func queueReasonFixture(t *testing.T, queueStub string) string {
	t.Helper()
	watchScript(t, "watch-queue.sh")
	dir := t.TempDir()
	for _, name := range []string{"watch-queue.sh", "gh-budget.sh"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".workflow", "bin", name))
		if err != nil {
			t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s could not be read (%v), so what the watch "+
				"prints for a failed poll was not checked.", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o755); err != nil {
			t.Fatalf("cannot stage %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(healthyPrimaryStub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "queue.sh"), []byte(queueStub), 0o755); err != nil {
		t.Fatalf("cannot write the queue stub: %v", err)
	}
	return dir
}

func lookupFailedLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "LOOKUP FAILED") {
			return l
		}
	}
	return ""
}

// TestALookupFailureShowsTheEndOfTheOutputNotTheBeginning is Issue #64's first half, driven with an
// output long enough for the truncation to matter. This is the arm whose absence shipped the defect
// and kept it shipped.
func TestALookupFailureShowsTheEndOfTheOutputNotTheBeginning(t *testing.T) {
	dir := queueReasonFixture(t, longThenFailingQueue)
	out := runWatchUntil(t, dir, "watch-queue.sh", atLeast("LOOKUP FAILED", 1), "dev", "1")
	line := lookupFailedLine(out)
	if line == "" {
		t.Fatalf("the poll failed and no LOOKUP FAILED line was emitted at all\n%s", out)
	}
	if !strings.Contains(line, "operation timed out") {
		t.Errorf("the poll died of a network timeout and the emitted reason does not contain it. The "+
			"queue's output is captured with 2>&1, so the failure is the LAST line and keeping the "+
			"first 200 characters keeps the headings and discards the diagnosis — which was read as a "+
			"misfire on the day it happened.\ngot: %s", line)
	}
}

// TestAShortLookupFailureIsShownWhole is the property this change must not break. The common case is
// an output that is entirely the reason, and a fix that always printed a tail would put an ellipsis
// on something that was never truncated.
func TestAShortLookupFailureIsShownWhole(t *testing.T) {
	dir := queueReasonFixture(t, shortFailingQueue)
	out := runWatchUntil(t, dir, "watch-queue.sh", atLeast("LOOKUP FAILED", 1), "dev", "1")
	line := lookupFailedLine(out)
	if line != "LOOKUP FAILED: boom" {
		t.Errorf("a reason that fits in the budget was not rendered byte-identically.\n"+
			"want: %q\ngot:  %q", "LOOKUP FAILED: boom", line)
	}
}

// mainStateFixture stages the installed watch and a stub `gh` answering the push-run query and the
// jobs query. The default is the observed event: main red, the naming gate failing, on a commit no
// merge here produced.
type mainStateFixture struct {
	conclusion string // the push run's conclusion
	failJobs   bool   // the jobs query cannot be answered
}

const redMainSha = "19f05904aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func (f mainStateFixture) run(t *testing.T, args ...string) string {
	t.Helper()
	watchScript(t, "watch-prs.sh")
	needTool(t, "jq")
	dir := t.TempDir()
	for _, name := range []string{"watch-prs.sh", "pr-authors.sh", "gh-budget.sh"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".workflow", "bin", name))
		if err != nil {
			t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s could not be read (%v), so what the watch "+
				"says about a red main was not checked.", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o755); err != nil {
			t.Fatalf("cannot stage %s: %v", name, err)
		}
	}
	concl := f.conclusion
	if concl == "" {
		concl = "failure"
	}
	jobs := `{"jobs":[{"name":"Build and tests","conclusion":"success"},` +
		`{"name":"Branch name and commit convention","conclusion":"failure"}]}`
	jobsArm := "echo '" + jobs + "'"
	if f.failJobs {
		jobsArm = "echo 'boom' >&2; exit 1"
	}
	stub := "#!/usr/bin/env bash\ncase \"$*\" in\n" +
		"  *\"/jobs\"*) " + jobsArm + " ;;\n" +
		"  *) echo '{\"workflow_runs\":[{\"id\":77,\"status\":\"completed\",\"conclusion\":\"" +
		concl + "\",\"head_sha\":\"" + redMainSha + "\"}]}' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	cmd := exec.Command("bash", append([]string{filepath.Join(dir, "watch-prs.sh"), "--main-state"}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REPO=x/y",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("watch-prs.sh --main-state failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestTheMainStateFixtureIsWellFormed. Most arms below assert something is absent from this output,
// and an absent string proves nothing if the fixture produced no output at all.
func TestTheMainStateFixtureIsWellFormed(t *testing.T) {
	got := mainStateFixture{conclusion: "success"}.run(t)
	if !strings.Contains(got, "GREEN") {
		t.Fatalf("a successful push run was not reported as green, so nothing below is measuring the "+
			"red path\n%s", got)
	}
	if red := (mainStateFixture{}.run(t)); !strings.Contains(red, "MAIN IS RED") {
		t.Fatalf("a failed push run was not reported as red\n%s", red)
	}
}

// TestARedMainNamesTheFailingCheck is AC3's first half. `(failure)` sends the reader to the Actions
// tab for what the watch had already been told, and the gate name is the half of the diagnosis that
// says which rule fired.
func TestARedMainNamesTheFailingCheck(t *testing.T) {
	got := mainStateFixture{}.run(t)
	if !strings.Contains(got, "Branch name and commit convention") {
		t.Errorf("a red main did not name the check that is red\n%s", got)
	}
}

// TestARedMainDoesNotTellTheLastMergerItIsTheirs is AC3's second half, and the one that was acted on
// twice. The failing commit here is a direct push by the framework; no merge produced it.
func TestARedMainDoesNotTellTheLastMergerItIsTheirs(t *testing.T) {
	got := mainStateFixture{}.run(t)
	if strings.Contains(got, "yours to fix") {
		t.Errorf("main is red on a commit no merge here produced, and the watch still told the reader "+
			"it is theirs to fix. That is an attribution derived from who merged last rather than from "+
			"the diff — the error `pr-authors.sh` exists to end, one layer up.\n%s", got)
	}
	if !strings.Contains(got, "CAUSE NOT DETERMINED") {
		t.Errorf("the cause could not be derived and the watch did not say so. An undetermined answer "+
			"must not wear the face of a determined one.\n%s", got)
	}
}

// TestAnAttributableRedMayStillSaySo is AC4, and the opposite error. Refusing to attribute a red
// that IS the caller's own merge commit would be its own kind of unhelpful — the rule is that the
// claim must be derived, not that it must never be made.
func TestAnAttributableRedMayStillSaySo(t *testing.T) {
	got := mainStateFixture{}.run(t, redMainSha)
	if !strings.Contains(got, "yours to fix") {
		t.Errorf("main's failing commit IS the merge commit this caller produced, and the watch did "+
			"not say so\n%s", got)
	}
}

// TestAnUnreadableJobListIsNotAnAbsentFailingCheck. The project's own rule, in the lookup that names
// the check: a jobs query that could not be answered must not render as a red run with no red job
// in it.
func TestAnUnreadableJobListIsNotAnAbsentFailingCheck(t *testing.T) {
	got := mainStateFixture{failJobs: true}.run(t)
	if !strings.Contains(got, "MAIN IS RED") {
		t.Errorf("the jobs query failed and main stopped being reported as red at all — the colour was "+
			"read from a different query and is still known\n%s", got)
	}
	// MATCHED ON THE PHRASE THAT NAMES THIS LOOKUP, not on `NOT DETERMINED` alone. The first version
	// of this arm did the latter and passed with the message deleted, because the ATTRIBUTION
	// sentence on the same line already contains `CAUSE NOT DETERMINED` — a check whose corpus
	// includes an unrelated answer to a different question. A mutation run found it; reading did not.
	if !strings.Contains(got, "failing check NOT DETERMINED") {
		t.Errorf("the jobs query failed and the watch did not say the failing check was undetermined. "+
			"A red run with no failing job named reads as `there was nothing more to say`.\n%s", got)
	}
}
