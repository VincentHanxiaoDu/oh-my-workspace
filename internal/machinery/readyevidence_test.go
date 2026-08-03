package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `READY` IS THE SIGNAL A VERIFIER ACTS ON TO MERGE, AND IT FIRED WHEN NO GATE HAD RUN (Issue #89).
//
//	watch-prs.sh   [ "${pending:-0}" -eq 0 ] && emit READY "$num" "$title"
//	watch-prs.sh   pending=$(... '[.check_runs[]? | select(.status!="completed")] | length' ...)
//
// `pending` counts check runs that are NOT YET COMPLETED. **With no check runs at all the array is
// empty, its length is 0, and READY fires.** The condition is vacuously true.
//
// The two reds above it cannot save it, and this is the part that makes it a class of defect rather
// than a typo: BOTH are existence-dependent. `FAILING` needs a `failure`/`error` status to EXIST;
// `CHANGES` needs a review to EXIST. On a head where nothing has reported, all three fall through to
// the permissive answer.
//
// MEASURED, not argued. One sweep of one board emitted six READY lines: three genuine, and three for
// heads with nothing reported on them at all — a 50% false-positive rate on the signal that decides
// what gets merged, with two of the false ones on the release's blocking branches. Reproduced
// deterministically on #46 head `4e9ca30c`, twice seconds apart, while `pr.sh state 46` said
// correctly: "NOTHING HAS REPORTED on this head yet. That is not a pass — it is no answer."
//
// Two derivations disagreed and the wrong one was the automated signal; the right one was the
// command a person has to think to run.
//
// THE FIX IS POSITIVE EVIDENCE, and the no-answer case gets its OWN event. Suppressing the line
// would make silence mean both "nothing to report" and "no answer yet" — the same collapse one
// level up, in the file that exists because a dead watch and a quiet queue look identical.
//
// THESE TESTS EXECUTE THE INSTALLED `watch-prs.sh`. `.workflow/bin/` is replaced wholesale by the
// next `install.sh` run, so a restatement of the logic here would go green while the shipped watch
// advertised unbuilt branches as mergeable again.

// readyFixture is the whole world the watch sees: one open pull request on this role's own branch,
// with the evidence on its head under the caller's control.
type readyFixture struct {
	// checkRuns is the `check_runs` array. Empty is Issue #89's case: nothing has reported.
	checkRuns string
	// statuses is the `statuses` array, where the review verdict lives — and only there.
	statuses string
	// reviews is the pull request's reviews array.
	reviews string
	// failStatuses makes the commit-status query fail. An unreadable verdict is not an absent one.
	failStatuses bool
}

const (
	readyPRHead = "4e9ca30c"

	// A completed, passing build.
	oneGreenCheckRun = `{"name":"Build and tests","status":"completed","conclusion":"success"}`
	// A build that has started and not finished. Not a pass either.
	onePendingCheckRun = `{"name":"Build and tests","status":"in_progress","conclusion":null}`
	// The verdict, published as a commit status — deliberately not a check run, so the job can stay
	// green and auto-merge can arm.
	approvedVerdict = `{"context":"Reviewed by an agent","state":"success","description":"ok"}`
)

func (f readyFixture) stage(t *testing.T) string {
	t.Helper()
	watchScript(t, "watch-prs.sh")
	needTool(t, "jq")
	dir := t.TempDir()
	for _, name := range []string{"watch-prs.sh", "pr-authors.sh", "gh-budget.sh"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".workflow", "bin", name))
		if err != nil {
			t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s could not be read (%v), so nothing about "+
				"what READY requires was checked. This is not evidence that READY is sound.", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o755); err != nil {
			t.Fatalf("cannot stage %s: %v", name, err)
		}
	}
	statusArm := `echo '{"statuses":[` + f.statuses + `]}'`
	if f.failStatuses {
		statusArm = `echo 'boom' >&2; exit 1`
	}
	stub := `#!/usr/bin/env bash
case "$*" in
  *rate_limit*)         echo "4896 $(( $(date +%s) + 1500 ))" ;;
  *"check-runs"*)       echo '{"check_runs":[` + f.checkRuns + `]}' ;;
  *"/status"*)          ` + statusArm + ` ;;
  *"/reviews"*)         echo '[` + f.reviews + `]' ;;
  *"state=closed"*)     echo '[]' ;;
  *"pulls?state=open"*) echo '[{"number":46,"title":"feat(publish): the outbox or the hub","head":{"ref":"dev/feat/46-x","sha":"` + readyPRHead + `"}}]' ;;
  *"pulls/46"*)         echo '{"body":""}' ;;
  *)                    echo '[]' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	return dir
}

// sweep drives the one-pass entry point, which terminates on its own.
func (f readyFixture) sweep(t *testing.T) string {
	t.Helper()
	dir := f.stage(t)
	cmd := exec.Command("bash", filepath.Join(dir, "watch-prs.sh"), "dev", "--sweep")
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ADF_BUDGET_STATE_DIR="+filepath.Join(dir, "state"),
		"REPO=x/y",
	)
	out, _ := cmd.CombinedOutput() // a sweep exits non-zero on a failed lookup; the text is the assertion
	return string(out)
}

// monitor drives the long-running entry point. THE POINT OF THIS IS THE ENTRY POINT ITSELF: the
// Issue explicitly did not claim the monitor was affected, having driven only `--sweep`. It is the
// same loop — `--sweep` is that loop with an `exit 0` after one pass — but "documented as the same
// code" is not a measurement, so it is measured.
func (f readyFixture) monitor(t *testing.T, waitFor string) string {
	t.Helper()
	dir := f.stage(t)
	return runWatchUntil(t, dir, "watch-prs.sh", atLeast(waitFor, 1), "dev", "2")
}

func emitted(out, state string) bool { return strings.Contains(out, state+" #46") }

// TestTheReadyFixtureIsWellFormed. Every arm below asserts READY is ABSENT, and an absent string
// proves nothing if the fixture never produces one. This is the positive control: full evidence —
// a completed passing check run and a success verdict — must still produce READY.
//
// It is also the property the fix is one careless edit away from breaking. A watch that never says
// READY has replaced a false positive with a signal nobody can act on.
func TestTheReadyFixtureIsWellFormed(t *testing.T) {
	f := readyFixture{checkRuns: oneGreenCheckRun, statuses: approvedVerdict}
	out := f.sweep(t)
	if !emitted(out, "READY") {
		t.Fatalf("a completed passing check run with a success review verdict did not produce READY, "+
			"so nothing below is measuring what READY requires\n%s", out)
	}
}

// TestReadyDoesNotFireWhenNothingHasReported is Issue #89 itself, on the sweep. The head has an
// EMPTY check_runs array: nothing is pending, nothing is failing, and no review has refused.
func TestReadyDoesNotFireWhenNothingHasReported(t *testing.T) {
	out := readyFixture{}.sweep(t)
	if emitted(out, "READY") {
		t.Errorf("a head on which NOTHING has reported was announced READY. `pending` counts check "+
			"runs that are not completed, and with no check runs at all that count is zero — the "+
			"condition is vacuously true. READY is the signal a verifier acts on to MERGE, so this "+
			"merges a pull request whose build, gates and review verdict have all not run.\n%s", out)
	}
}

// TestNothingReportedGetsItsOwnEvent is the second half of the remedy, and the half most easily
// dropped. Suppressing the line would leave silence meaning both "nothing to report" and "no answer
// yet" — the same collapse one level up, in the file that exists because a dead watch and a quiet
// queue look identical.
func TestNothingReportedGetsItsOwnEvent(t *testing.T) {
	out := readyFixture{}.sweep(t)
	if !emitted(out, "NO-ANSWER") {
		t.Fatalf("a head on which nothing has reported produced no event at all. Silence now means "+
			"both 'nothing to report' and 'no answer yet'.\n%s", out)
	}
	// THE WORDING IS CARRIED ACROSS FROM `pr.sh state`, NOT REINVENTED. The two derivations
	// disagreeing is what made this findable, and matching vocabulary is what keeps them comparable.
	if !strings.Contains(out, "NOTHING HAS REPORTED") {
		t.Errorf("the no-answer event does not say that nothing has reported, in the words the rest "+
			"of this machinery already uses for it\n%s", out)
	}
	if !strings.Contains(out, "not a pass") && !strings.Contains(out, "NOT a pass") {
		t.Errorf("the no-answer event does not say it is not a pass — which is the whole distinction "+
			"it exists to carry\n%s", out)
	}
}

// TestTheMonitorSharesTheDefectWithTheSweep. The Issue drove only `--sweep` and deliberately refused
// to claim the monitor was affected without seeing it. This sees it: the same absent evidence
// through the long-running entry point, which is the one that actually runs all day.
func TestTheMonitorSharesTheDefectWithTheSweep(t *testing.T) {
	out := readyFixture{}.monitor(t, "NO-ANSWER")
	if emitted(out, "READY") {
		t.Errorf("the MONITOR announced READY for a head on which nothing has reported. The sweep and "+
			"the monitor share this branch — the sweep is the same loop with an exit after one "+
			"pass.\n%s", out)
	}
	if !emitted(out, "NO-ANSWER") {
		t.Errorf("the monitor emitted no no-answer event for a head with nothing reported\n%s", out)
	}
}

// TestAGreenBuildWithNoVerdictIsNotReady. The absence of a refusal is not the presence of an
// approval — and the verdict is the thing that actually blocks the merge, so a READY without one
// sends a verifier at a pull request the gate will refuse.
func TestAGreenBuildWithNoVerdictIsNotReady(t *testing.T) {
	out := readyFixture{checkRuns: oneGreenCheckRun}.sweep(t)
	if emitted(out, "READY") {
		t.Errorf("a green build with no review verdict published was announced READY\n%s", out)
	}
	if !emitted(out, "NO-ANSWER") {
		t.Errorf("a green build with no review verdict produced no event — the author is told nothing "+
			"about a pull request that is waiting on exactly one thing\n%s", out)
	}
}

// TestAStillRunningBuildIsNotReadyAndIsNotSilence. A run in progress was already excluded from
// READY, and was excluded by SILENCE — which is the same defect wearing the other face.
func TestAStillRunningBuildIsNotReadyAndIsNotSilence(t *testing.T) {
	out := readyFixture{checkRuns: onePendingCheckRun, statuses: approvedVerdict}.sweep(t)
	if emitted(out, "READY") {
		t.Errorf("a check run still in progress was announced READY\n%s", out)
	}
	if !emitted(out, "NO-ANSWER") {
		t.Errorf("a check run still in progress produced no event at all\n%s", out)
	}
}

// TestAnUnreadableVerdictIsNotAnAbsentOne. The commit-status query used to be wrapped in
// `if st=$(...); then ... fi`, so a FAILED lookup fell straight through to the permissive answer
// with nothing said. That is #79's rule in the branch that decides what gets merged.
func TestAnUnreadableVerdictIsNotAnAbsentOne(t *testing.T) {
	out := readyFixture{checkRuns: oneGreenCheckRun, failStatuses: true}.sweep(t)
	if emitted(out, "READY") {
		t.Errorf("the commit statuses could not be read — so the review verdict could not be read — "+
			"and the watch announced READY anyway\n%s", out)
	}
	if !strings.Contains(out, "LOOKUP FAILED") {
		t.Errorf("the commit-status lookup failed and the watch said nothing about it. A verdict that "+
			"could not be read is not a verdict that is absent.\n%s", out)
	}
}

// TestASweepThatHoldsStillEnds. FOUND WHILE BUILDING THIS FIXTURE, not by reading: with a stub `gh`
// that could not answer `rate_limit`, the budget guard reported a hold and `--sweep` slept and
// continued — forever. The ONE-PASS fallback for a dead watch hung exactly like the watch it
// replaces, which is the failure this file's own self-test says it exists to prevent, and it was
// invisible because the existing sweep arms all use stubs that answer.
//
// It exits non-zero: a hold is not an answer to a question asked once, and exit 0 with no events
// tells a caller its board is quiet.
func TestASweepThatHoldsStillEnds(t *testing.T) {
	watchScript(t, "watch-prs.sh")
	needTool(t, "jq")
	dir := t.TempDir()
	for _, name := range []string{"watch-prs.sh", "pr-authors.sh", "gh-budget.sh"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".workflow", "bin", name))
		if err != nil {
			t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s could not be read (%v).", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o755); err != nil {
			t.Fatalf("cannot stage %s: %v", name, err)
		}
	}
	// A rate limit BELOW the reserve, so the guard genuinely holds.
	stub := "#!/usr/bin/env bash\ncase \"$*\" in\n" +
		"  *rate_limit*) echo \"12 $(( 1 ))\" ;;\n" +
		"  *) echo '[]' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	cmd := exec.Command("bash", filepath.Join(dir, "watch-prs.sh"), "dev", "--sweep")
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ADF_BUDGET_STATE_DIR="+filepath.Join(dir, "state"),
		"REPO=x/y",
	)
	logPath := filepath.Join(dir, "sweep.out")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("cannot open the sweep log: %v", err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the sweep: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			raw, _ := os.ReadFile(logPath)
			t.Errorf("a sweep that held exited 0. A caller reads exit 0 with no events as `nothing is "+
				"waiting on you`, and a budget hold is not that.\n%s", raw)
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
		raw, _ := os.ReadFile(logPath)
		t.Fatalf("a sweep that held NEVER EXITED — it slept and continued, so the one-pass fallback "+
			"for a dead watch hangs exactly like the watch it replaces.\n%s", raw)
	}
}
