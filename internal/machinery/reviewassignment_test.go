package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A REVIEW A ROLE HAS ALREADY GIVEN MUST NOT COME BACK TO IT, AND A REVIEW NOBODY HAS GIVEN MUST
// REACH EVERY ROLE THAT CAN GIVE IT (Issue #59).
//
// The measured defect: `reviews_waiting` in `.workflow/bin/queue.sh` suppressed a pull request on
// exactly one condition — the `Reviewed by an agent` commit status being `success`. A
// `changes-requested` verdict publishes `failure`, not `success`, so the role that had just refused
// a pull request was offered it again the next round, and the round after that, with nothing
// recording that it had ever looked. Two distinct failure modes were reported together and only one
// of them is this:
//
//	one role, twice, across rounds  → THIS. Fixed by reading the verdict attestation.
//	two roles, at once              → deliberate redundancy (#32's outage is the alternative).
//	                                  Asserted here as a property to KEEP, not a defect to remove.
//
// These tests drive the INSTALLED `.workflow/bin/queue.sh` against a stub `gh`. They do not restate
// its logic: a refresh from agent-dev-flow that drops the fix turns this suite red, which is the
// only reason this file is in `internal/` and not in `.workflow/`.
//
// WHAT THEY CANNOT DETERMINE, THEY SAY. Every external dependency is probed, never named and
// assumed; a missing one skips with a reason that states the check did not run and is NOT passing.

// queueScript locates the installed queue and probes that it is runnable here.
func queueScript(t *testing.T) string {
	t.Helper()
	needTool(t, "bash")
	needTool(t, "jq")
	path := filepath.Join(repoRoot(t), ".workflow", "bin", "queue.sh")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s is not present (%v), so nothing about review "+
			"assignment was checked. This is not evidence that the queue is correct.", path, err)
	}
	return path
}

// queueFixture builds a stub `gh` describing one open pull request and runs the queue against it.
// Nothing here talks to GitHub: the stub is the entire world the script sees.
type queueFixture struct {
	dir string
	// comments is the JSON array returned for the pull request's comments — where the
	// `Reviewed-by:` / `Reviewed-sha:` attestation lives.
	comments string
	// status is the JSON returned for the head sha's combined status.
	status string
	// fail makes every `gh` call exit non-zero.
	fail bool
	// failComments fails ONLY the comments query. A stub that fails everything cannot test this:
	// the queue exits non-zero on the FIRST failed call, so the arm passes without the code under
	// test ever running. Measured — a mutation that swallowed the comments failure stayed green
	// against the fail-everything stub.
	failComments bool
}

const fixtureHead = "cafe"

func (f *queueFixture) run(t *testing.T, role string) (string, int) {
	t.Helper()
	script := queueScript(t)
	if f.dir == "" {
		f.dir = t.TempDir()
	}
	comments := f.comments
	if comments == "" {
		comments = "[]"
	}
	status := f.status
	if status == "" {
		status = `{"statuses":[]}`
	}
	commentsArm := "cat <<'JSON'\n" + comments + "\nJSON"
	if f.failComments {
		commentsArm = "echo 'API rate limit exceeded for the comments endpoint' >&2; exit 1"
	}
	body := `#!/usr/bin/env bash
case "$*" in
  *"pulls/9/commits"*)          printf 'c9\tAgent: dev\n' ;;
  *"/commits/c9"*)              echo 'internal/a.go' ;;
  *"pulls?state=open"*)         echo '[{"number":9,"head":{"ref":"dev/feat/9-x","sha":"` + fixtureHead + `"},"title":"feat(x): y"}]' ;;
  *"/commits/` + fixtureHead + `/status"*) cat <<'JSON'
` + status + `
JSON
;;
  *"issues/9/comments"*)        ` + commentsArm + `
;;
  *)                            echo '[]' ;;
esac
`
	if f.fail {
		body = "#!/usr/bin/env bash\necho 'API rate limit exceeded' >&2\nexit 1\n"
	}
	stub := filepath.Join(f.dir, "gh")
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}

	cmd := exec.Command("bash", script, role)
	cmd.Env = append(os.Environ(), "PATH="+f.dir+string(os.PathListSeparator)+os.Getenv("PATH"), "REPO=x/y")
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("queue.sh could not be run at all: %v\n%s", err, out)
		}
		rc = ee.ExitCode()
	}
	return string(out), rc
}

// offered reports whether the queue told this role to review pull request 9.
func offered(out string) bool { return strings.Contains(out, "/review-pr 9") }

// verdict renders the attestation `check-review.sh` reads, as a comment body.
func verdict(role, sha, v string) string {
	return `[{"body":"Reviewed-by: ` + role + `\nReviewed-sha: ` + sha + `\nVerdict: ` + v + `"}]`
}

// TestARefusedReviewDoesNotComeBackToTheRoleThatRefusedIt is Issue #59 criterion 1 for the mode it
// actually applies to: one role, the same head, twice. The verdict is `changes-requested`, which
// leaves the commit status `failure` — the state under which the old queue re-offered it forever.
//
// AND CRITERION 2 IN THE SAME ARM, which is the point: suppressing it for qa must not suppress it
// for product. A fix that routed this head to one role would pass the first assertion and
// reintroduce the single point of failure that stopped the board in #32.
func TestARefusedReviewDoesNotComeBackToTheRoleThatRefusedIt(t *testing.T) {
	f := &queueFixture{
		comments: verdict("qa", fixtureHead, "changes-requested"),
		status:   `{"statuses":[{"context":"Reviewed by an agent","state":"failure"}]}`,
	}

	out, rc := f.run(t, "qa")
	if rc != 0 {
		t.Fatalf("queue.sh qa exited %d\n%s", rc, out)
	}
	if offered(out) {
		t.Errorf("qa posted a landed verdict on head %s and the queue offered that head to qa "+
			"again. Nothing records that it looked, so this repeats every round until somebody "+
			"else approves or the author pushes — the same review, done twice, by one role.\n%s",
			fixtureHead, out)
	}

	out, rc = f.run(t, "product")
	if rc != 0 {
		t.Fatalf("queue.sh product exited %d\n%s", rc, out)
	}
	if !offered(out) {
		t.Errorf("qa's refusal removed this head from PRODUCT's queue too. That is not deduplication, "+
			"it is one role's verdict deciding the work for every role — a single eligible reviewer, "+
			"which is exactly the outage (#32) the redundancy was chosen to prevent.\n%s", out)
	}
}

// TestAVerdictOnAnotherHeadDoesNotSuppressThisOne is criterion 3. The attestation carries the sha,
// so it releases itself: a push makes every prior verdict stale and the work reappears. A role that
// dies mid-review has posted nothing at all, so it holds nothing — there is no claim to expire,
// which is why this cannot produce the thing that looks busy and is actually gone.
func TestAVerdictOnAnotherHeadDoesNotSuppressThisOne(t *testing.T) {
	f := &queueFixture{
		comments: verdict("qa", strings.Repeat("0", 40), "approve"),
		status:   `{"statuses":[{"context":"Reviewed by an agent","state":"failure"}]}`,
	}
	out, rc := f.run(t, "qa")
	if rc != 0 {
		t.Fatalf("queue.sh qa exited %d\n%s", rc, out)
	}
	if !offered(out) {
		t.Errorf("a verdict naming a DIFFERENT head suppressed this one. A review is attested "+
			"against a sha precisely so that a push invalidates it; if a stale verdict can hold a "+
			"head out of the queue, the head is stranded by a claim nobody can release.\n%s", out)
	}
}

// TestAnUnreviewedHeadReachesEveryIndependentRoleAndNoAuthor pins the property Issue #59 says must
// survive: with no verdict anywhere, every independent role is offered the work, and the role that
// built it is not. The duplication between qa and product is DELIBERATE and asserted here so that a
// later attempt to remove it fails loudly rather than quietly recreating a single reviewer.
func TestAnUnreviewedHeadReachesEveryIndependentRoleAndNoAuthor(t *testing.T) {
	f := &queueFixture{}
	for _, role := range []string{"qa", "product"} {
		out, rc := f.run(t, role)
		if rc != 0 {
			t.Fatalf("queue.sh %s exited %d\n%s", role, rc, out)
		}
		if !offered(out) {
			t.Errorf("%s authored none of this pull request's commits and no verdict exists, yet "+
				"the queue offered it nothing. A head in nobody's queue is the deadlock.\n%s", role, out)
		}
	}
	out, rc := f.run(t, "dev")
	if rc != 0 {
		t.Fatalf("queue.sh dev exited %d\n%s", rc, out)
	}
	if offered(out) {
		t.Errorf("the queue offered this pull request to dev, which authored it. The gate refuses "+
			"that verdict, so the work is done twice and stays blocked.\n%s", out)
	}
}

// TestALookupFailureIsNotAnUnreviewedHead. `could not determine` and `determined to be nothing`
// must not share an exit code — the defect this whole area exists to prevent. If the comments
// cannot be read, the queue must not conclude that no verdict exists and hand out work already done.
func TestALookupFailureIsNotAnUnreviewedHead(t *testing.T) {
	f := &queueFixture{fail: true}
	out, rc := f.run(t, "qa")
	if rc == 0 {
		t.Errorf("every GitHub call failed and queue.sh exited 0. An agent reads that as an "+
			"answer.\n%s", out)
	}
	if offered(out) {
		t.Errorf("a total lookup failure produced a review assignment. Work was handed out on the "+
			"strength of a query that never ran.\n%s", out)
	}
}

// TestAFailedCommentsQueryIsNotAnAbsentVerdict is the same rule aimed at the ONE query this change
// adds. The fail-everything stub above cannot reach it: the queue dies on the first failed call, so
// that arm goes green whether or not this code handles anything. Only the comments query fails here,
// so every earlier call succeeds and the failure this asserts is the one under test.
func TestAFailedCommentsQueryIsNotAnAbsentVerdict(t *testing.T) {
	f := &queueFixture{failComments: true}
	out, rc := f.run(t, "qa")
	if rc == 0 {
		t.Errorf("the verdict record could not be read and queue.sh exited 0. `could not determine` "+
			"and `determined to be nothing` have collapsed into one answer, and the answer they "+
			"collapsed into is the one that hands out work already done.\n%s", out)
	}
	if offered(out) {
		t.Errorf("the comments query failed and the queue offered the review anyway — an outage "+
			"reads as 'nobody has reviewed this'.\n%s", out)
	}
}

// TestQueueSelfTestPasses runs the installed queue's own self-test. It touches no network, so a
// failure here is the script disagreeing with itself.
func TestQueueSelfTestPasses(t *testing.T) {
	script := queueScript(t)
	out, err := exec.Command("bash", script, "--self-test").CombinedOutput()
	if err != nil {
		t.Fatalf("queue.sh --self-test failed: %v\n%s", err, out)
	}
}
