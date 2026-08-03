package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A REVIEW A ROLE HAS ALREADY GIVEN MUST NOT COME BACK TO A DIFFERENT ONE, AND A HEAD NOBODY HAS
// REVIEWED MUST BE IN SOMEBODY'S QUEUE (Issue #59, re-aimed at the model that replaced it in #117).
//
// The measured defect: `reviews_waiting` in `.workflow/bin/queue.sh` suppressed a pull request on
// exactly one condition — the `Reviewed by an agent` commit status being `success`. A
// `changes-requested` verdict publishes `failure`, not `success`, so the role that had just refused
// a pull request was offered it again the next round, and the round after that, with nothing
// recording that it had ever looked.
//
// THE ROUTING MODEL THIS FILE WAS WRITTEN AGAINST NO LONGER EXISTS, and that is a deliberate
// upstream change, not a regression. `reviews_waiting` is gone. No role is offered another role's
// pull request at all: ownership comes from the branch name (`<role>/<type>/<issue>-<slug>`, which
// the naming gate already enforces), the author dispatches one reviewer sub-agent in its own
// session, and a refusal sends the work back to THAT SAME reviewer by name — `re-review by qa
// (round 2)`. Three rounds of `changes-requested` stops being a review and escalates to product.
//
// SO #59's DEFECT IS CLOSED BY CONSTRUCTION and cannot be asserted in its original terms: there is
// no queue for a refusing role to be re-offered the work from. What is asserted here instead is the
// same fail-open class on the mechanism that replaced it, plus the two properties that carried over
// unchanged — a verdict is bound to a sha, and an unreadable review history is not an absent one.
//
// ONE PROPERTY WAS REMOVED ON PURPOSE AND IS NOT REPLACED. The old
// `TestAnUnreviewedHeadReachesEveryIndependentRoleAndNoAuthor` pinned DELIBERATE REDUNDANCY — qa
// and product both offered the same head — "so that a later attempt to remove it fails loudly
// rather than quietly recreating a single reviewer". It fired exactly as designed against the
// refresh. The redundancy is gone: there is one reviewer per pull request now, and the release
// valve for a review that cannot converge is the three-round escalation to product rather than a
// second role picking it up. **#32 — the outage redundancy was chosen to prevent — is still open on
// this board, and this is a live trade, not a solved problem.** It is recorded here rather than
// deleted so that the next person reads it.
//
// These tests drive the INSTALLED `.workflow/bin/queue.sh` against a stub `gh`. They do not restate
// its logic: a refresh from agent-dev-flow that drops the behaviour turns this suite red, which is
// the only reason this file is in `internal/` and not in `.workflow/`.
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
	// branch is the head ref, which is what decides WHOSE pull request this is under the model that
	// replaced the author lookup. Defaults to a branch owned by dev.
	branch string
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

// ghStubBody renders the whole world the queue sees.
//
// THE SHAPES MATTER AND A WRONG ONE IS NOT A NEUTRAL SIMPLIFICATION. An earlier version of this
// fixture returned a bare `[]` for the single-pull-request and check-run endpoints, which the real
// API never returns; `pr.sh state` then printed a raw `jq: error ... Cannot index array with string
// "head"` into the status column of every arm. It went unnoticed because no assertion read that
// column — a fixture defect riding along inside otherwise green tests. `noJQError` now fails on it.
func ghStubBody(branch, commentsArm, status string) string {
	return `#!/usr/bin/env bash
case "$*" in
  *"issues/9/comments"*) ` + commentsArm + `
;;
  *"pulls?state=open"*)
    echo '[{"number":9,"head":{"ref":"` + branch + `","sha":"` + fixtureHead + `"},"title":"feat(x): y"}]' ;;
  *"pulls/9"*)
    echo '{"number":9,"mergeable":true,"head":{"ref":"` + branch + `","sha":"` + fixtureHead + `"},"title":"feat(x): y"}' ;;
  *"check-runs"*)  echo '{"check_runs":[]}' ;;
  *"/commits/` + fixtureHead + `/status"*) cat <<'JSON'
` + status + `
JSON
;;
  *) echo '[]' ;;
esac
`
}

func (f *queueFixture) run(t *testing.T, role string) (string, int) {
	t.Helper()
	script := queueScript(t)
	if f.dir == "" {
		f.dir = t.TempDir()
	}
	branch := f.branch
	if branch == "" {
		branch = "dev/feat/9-x"
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
	body := ghStubBody(branch, commentsArm, status)
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

// awaitingReview reports whether the queue told this role that pull request 9 has no verdict on its
// current head and needs one — under either rendering, a first review or a re-review.
func awaitingReview(out string) bool {
	return needsFirstReview(out) || strings.Contains(out, "re-review by")
}

// needsFirstReview reports the specific claim that NOBODY has reviewed this head.
func needsFirstReview(out string) bool { return strings.Contains(out, "NO REVIEW HAS HAPPENED") }

// reReviewBy reports whether the queue named this role as the one to go back to.
func reReviewBy(out, role string) bool { return strings.Contains(out, "re-review by "+role) }

// noJQError guards every arm against the fixture defect described on ghStubBody. A raw jq error in
// the output means the stub returned a shape the real API does not, and whatever the arm concluded
// was concluded about a broken world.
func noJQError(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "jq: error") {
		t.Fatalf("the stub produced a jq error, so this arm measured a broken fixture and not the "+
			"queue:\n%s", out)
	}
}

// verdict renders the attestation the queue reads, as a comment body. The `[role]` marker alone on
// the first line is what `queue.sh` matches on, and a verdict without it is invisible to it.
func verdict(role, sha, v string) string {
	return `[{"body":"[` + role + `]\nReviewed-by: ` + role + `\nReviewed-sha: ` + sha +
		`\nVerdict: ` + v + `"}]`
}

// TestTheReviewAssignmentFixtureIsWellFormed is the control every arm below needs. Several of them
// assert that something is ABSENT from the output, and a broken stub makes everything absent. This
// one asserts the healthy shape: dev's own pull request, with no verdict anywhere, is in dev's queue
// as work needing a reviewer.
func TestTheReviewAssignmentFixtureIsWellFormed(t *testing.T) {
	f := &queueFixture{}
	out, rc := f.run(t, "dev")
	noJQError(t, out)
	if rc != 0 {
		t.Fatalf("the healthy stub made queue.sh dev exit %d, so nothing below is measuring review "+
			"assignment\n%s", rc, out)
	}
	if !needsFirstReview(out) {
		t.Fatalf("the healthy stub did not tell dev that its own unreviewed pull request needs a "+
			"reviewer dispatched\n%s", out)
	}
}

// TestAnUnreviewedHeadIsItsAuthorsWorkAndNobodysElse pins the model that replaced #59's routing.
// Whose pull request this is comes from the branch name; the author is told to dispatch a reviewer,
// and no other role is told to review it.
//
// BOTH DIRECTIONS, because either alone is passed by a wrong fix. Missing from the author's queue is
// a pull request nobody is holding; present in another role's is the alternating loop that cost #53
// eleven verdicts in eighteen hours.
//
// READ THE HEADER OF THIS FILE BEFORE TREATING THE SECOND HALF AS SETTLED. It asserts that the
// redundancy #59 deliberately preserved is gone, because it is, upstream and on purpose. #32 is the
// open Issue that says what that redundancy was for.
func TestAnUnreviewedHeadIsItsAuthorsWorkAndNobodysElse(t *testing.T) {
	f := &queueFixture{}
	out, rc := f.run(t, "dev")
	noJQError(t, out)
	if rc != 0 {
		t.Fatalf("queue.sh dev exited %d\n%s", rc, out)
	}
	if !needsFirstReview(out) {
		t.Errorf("dev opened this pull request and no verdict exists, yet dev was not told to "+
			"dispatch a reviewer. A head in nobody's queue is the deadlock.\n%s", out)
	}
	for _, role := range []string{"qa", "product"} {
		out, rc := f.run(t, role)
		noJQError(t, out)
		if rc != 0 {
			t.Fatalf("queue.sh %s exited %d\n%s", role, rc, out)
		}
		if awaitingReview(out) {
			t.Errorf("%s was told to review dev's pull request. Review is dispatched by the author "+
				"now, and a second role picking it up is the alternating loop the change "+
				"removed.\n%s", role, out)
		}
	}
}

// TestARefusedHeadGoesBackToTheReviewerThatRefusedIt is #59's rule in the terms that survive. qa
// refused an earlier head; the author pushed, which makes a new head, and the work must come back —
// to QA BY NAME, not to a fresh reviewer with no context, and not to nobody.
//
// A fresh reviewer re-opens findings the first one settled, which is the mechanism behind #53's
// eleven alternating verdicts. Naming the owner is the whole of the anti-ping-pong rule here.
func TestARefusedHeadGoesBackToTheReviewerThatRefusedIt(t *testing.T) {
	f := &queueFixture{
		comments: verdict("qa", strings.Repeat("0", 40), "changes-requested"),
		status:   `{"statuses":[{"context":"Reviewed by an agent","state":"failure"}]}`,
	}
	out, rc := f.run(t, "dev")
	noJQError(t, out)
	if rc != 0 {
		t.Fatalf("queue.sh dev exited %d\n%s", rc, out)
	}
	if !awaitingReview(out) {
		t.Fatalf("qa refused an earlier head and the author pushed; the new head has no verdict and "+
			"is in nobody's queue at all. The pull request is stranded.\n%s", out)
	}
	if !reReviewBy(out, "qa") {
		t.Errorf("the queue did not send this back to qa, which already reviewed it and still holds "+
			"its findings. A fresh reviewer re-opens what the first one settled.\n%s", out)
	}
	if needsFirstReview(out) {
		t.Errorf("the queue said NO REVIEW HAS HAPPENED about a pull request qa has already "+
			"refused. A round that left a verdict on the record is not no review.\n%s", out)
	}
}

// TestAVerdictOnTheCurrentHeadSettlesIt is the other side of the same rule, and the one that stops
// the queue nagging. A landed verdict on the head being asked about is an answer, whichever way it
// went: the ball is with the author, and pushing makes a new head that no verdict names.
func TestAVerdictOnTheCurrentHeadSettlesIt(t *testing.T) {
	for _, v := range []string{"approve", "changes-requested"} {
		f := &queueFixture{comments: verdict("qa", fixtureHead, v)}
		out, rc := f.run(t, "dev")
		noJQError(t, out)
		if rc != 0 {
			t.Fatalf("queue.sh dev exited %d with a %s verdict on the current head\n%s", rc, v, out)
		}
		if awaitingReview(out) {
			t.Errorf("qa posted a %s verdict on head %s and the queue still asked for a review of "+
				"that same head. Nothing records that it looked, so this repeats every round — the "+
				"same review, done twice (#59).\n%s", v, fixtureHead, out)
		}
	}
}

// TestAVerdictOnAnotherHeadDoesNotSuppressThisOne carried over unchanged, because the attestation
// still carries the sha and still releases itself: a push makes every prior verdict stale and the
// work reappears. A role that dies mid-review has posted nothing at all, so it holds nothing —
// there is no claim to expire, which is why this cannot produce the thing that looks busy and is
// actually gone.
func TestAVerdictOnAnotherHeadDoesNotSuppressThisOne(t *testing.T) {
	f := &queueFixture{
		comments: verdict("qa", strings.Repeat("0", 40), "approve"),
		status:   `{"statuses":[{"context":"Reviewed by an agent","state":"failure"}]}`,
	}
	out, rc := f.run(t, "dev")
	noJQError(t, out)
	if rc != 0 {
		t.Fatalf("queue.sh dev exited %d\n%s", rc, out)
	}
	if !awaitingReview(out) {
		t.Errorf("a verdict naming a DIFFERENT head suppressed this one. A review is attested "+
			"against a sha precisely so that a push invalidates it; if a stale verdict can hold a "+
			"head out of the queue, the head is stranded by a claim nobody can release.\n%s", out)
	}
}

// TestALookupFailureIsNotAnUnreviewedHead. `could not determine` and `determined to be nothing`
// must not share an exit code — the defect this whole area exists to prevent. If the record cannot
// be read, the queue must not conclude that no verdict exists and send a reviewer at work already
// done.
func TestALookupFailureIsNotAnUnreviewedHead(t *testing.T) {
	f := &queueFixture{fail: true}
	out, rc := f.run(t, "dev")
	if rc == 0 {
		t.Errorf("every GitHub call failed and queue.sh exited 0. An agent reads that as an "+
			"answer.\n%s", out)
	}
	if needsFirstReview(out) {
		t.Errorf("a total lookup failure was rendered as 'no review has happened'. Work was "+
			"described on the strength of a query that never ran.\n%s", out)
	}
}

// TestAFailedCommentsQueryIsNotAnAbsentVerdict is the same rule aimed at the ONE query that decides
// it, and it is Issue #79's shape on the call that inherited it. The fail-everything stub above
// cannot reach it: the queue dies on the first failed call, so that arm goes green whether or not
// this code handles anything. Only the comments query fails here, so every earlier call succeeds and
// the failure this asserts is the one under test.
//
// THE ROLE IS `dev`, WHICH OWNS THE BRANCH. The review history is read only for the pull requests
// that are yours, so pointing this at qa reaches no such call and the arm passes without measuring
// anything — the identical trap this comment records one level up.
func TestAFailedCommentsQueryIsNotAnAbsentVerdict(t *testing.T) {
	f := &queueFixture{failComments: true}
	out, rc := f.run(t, "dev")
	if rc == 0 {
		t.Errorf("the verdict record could not be read and queue.sh exited 0. `could not determine` "+
			"and `determined to be nothing` have collapsed into one answer, and the answer they "+
			"collapsed into is the one that sends a reviewer at work already done.\n%s", out)
	}
	if needsFirstReview(out) {
		t.Errorf("the review history could not be read and the queue said NO REVIEW HAS HAPPENED — "+
			"an outage reads as 'nobody has reviewed this'.\n%s", out)
	}
	if !strings.Contains(out, "::error::") {
		t.Errorf("the review history could not be read and the queue printed no error. A role sees "+
			"a short queue and a non-zero exit with no reason, which is the outage wearing the "+
			"costume of a bug in the queue.\n%s", out)
	}
}

// TestThreeRoundsOfChangesLeaveTheReviewLoopAndReachProduct pins the release valve that replaced the
// redundancy. #53 took eleven verdicts in eighteen hours and stayed open with every check green; a
// disagreement that survives three reviews is a question about what the project wants, and no
// further round can answer it.
//
// BOTH HALVES, because either alone is a dead end: the author must be told to stop pushing, and
// product must be told to pick it up. An escalation nobody is holding is the orphan one level up.
func TestThreeRoundsOfChangesLeaveTheReviewLoopAndReachProduct(t *testing.T) {
	thrash := `[{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a1\nVerdict: changes-requested"},` +
		`{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a2\nVerdict: changes-requested"},` +
		`{"body":"[qa]\nReviewed-by: qa\nReviewed-sha: a3\nVerdict: changes-requested"}]`
	f := &queueFixture{comments: thrash}

	out, rc := f.run(t, "dev")
	noJQError(t, out)
	if rc != 0 {
		t.Fatalf("queue.sh dev exited %d\n%s", rc, out)
	}
	if !strings.Contains(out, "ESCALATED") {
		t.Errorf("three rounds of changes-requested and the queue asked dev for a fourth. The only "+
			"visible option is still to push again, which is the loop this limit exists to "+
			"end.\n%s", out)
	}

	out, rc = f.run(t, "product")
	noJQError(t, out)
	if rc != 0 {
		t.Fatalf("queue.sh product exited %d\n%s", rc, out)
	}
	if !strings.Contains(out, "DID NOT CONVERGE") {
		t.Errorf("a review that ran out of rounds was in nobody's queue — dev was told to stop and "+
			"product, the only role that may put it to the owner, was never told to pick it up. It "+
			"stops there.\n%s", out)
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
