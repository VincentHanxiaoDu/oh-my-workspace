package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// WHOSE PULL REQUEST THIS IS MUST BE A DETERMINED ANSWER, AND A FAILED LOOKUP IS NOT ONE
// (Issue #79, re-aimed at the mechanism that replaced it in #117).
//
// THE ORIGINAL DEFECT, and it is worth keeping the record because the shape recurs. `reviews_waiting`
// in `.workflow/bin/queue.sh` derived independence by calling `pr-authors.sh --pr <n>` and
// discarding both its stderr and its exit code:
//
//	authors=$(... /pr-authors.sh --pr "$num" 2>/dev/null || echo "")
//
// An empty author set already carried two meanings — no `Agent:` trailer anywhere, and trailers that
// are all spec-only — and there was a THIRD that was not handled: **the question could not be
// answered.** That is this project's own rule, `could not determine` and `determined to be nothing`
// must never collapse, broken inside the routing that enforces independence. `grep -qx "$role"`
// against an empty string matched nothing, so the pull request was offered to EVERY role including
// the ones that wrote it. Observed live during a secondary rate limit: `queue.sh dev` printed
// `#46 ... run /review-pr 46   (built by )` for a pull request carrying nine `Agent: dev` trailers.
//
// THE QUEUE NO LONGER ASKS WHO AUTHORED ANYTHING, so that call site is gone and the fix has no
// region left to sit in. Ownership comes from the branch name — `<role>/<type>/<issue>-<slug>`,
// which `check-naming.sh` already enforces — and the independence test that does still need the
// trailers lives in `check-review.sh`, which re-derives authorship from git at verdict time and has
// its own arms for exactly this. `pr-authors.sh`'s half of #79, the `return 3` on an unreadable
// lookup, is untouched and is covered by `prauthors_test.go`.
//
// WHAT THIS FILE PINS NOW is the replacement: ownership routing in BOTH directions, driven against
// the installed script. The old file asserted the same rule through the author lookup; this asserts
// it through the branch name. The `could not determine` half moved to the review-history query and
// is asserted by `TestAFailedCommentsQueryIsNotAnAbsentVerdict` in `reviewassignment_test.go`.
//
// WHAT IS DELIBERATELY NOT ASSERTED HERE, SO THAT ITS ABSENCE IS NOT READ AS ITS BEING FINE.
// `check-naming.sh` accepts `(dev|qa|product|ops|flow)` as the role component of a branch name and
// this routing has queues for a narrower set, so a branch on `flow/…` passes the naming gate and
// lands in NO role's queue at all — measured live on #121, which is the pull request that lands
// this very refresh. **That is Issue #126 and it is open.** It is not asserted here because both
// files are framework-owned and the fix belongs upstream, and because which of the two lists is
// wrong is a decision about whether `flow` is a role — not something to settle inside an install.
// #126 criterion 3 is the test that belongs in this package once it is.
//
// These tests EXECUTE the installed `.workflow/bin/queue.sh` rather than restating its logic.
// `.workflow/bin/` is replaced wholesale by the next `install.sh` run, so a refresh that removes the
// behaviour turns this suite red instead of quietly restoring the defect.
//
// WHAT IT CANNOT DETERMINE, IT SAYS. Every external dependency is probed rather than named and
// assumed; a missing one skips with a reason stating the check did not run and is NOT passing.

// TestOwnershipComesFromTheBranchNameInBothDirections. A pull request on `<role>/…` is that role's
// own work: it appears in that role's queue as work needing a reviewer, and in no other role's.
//
// BOTH DIRECTIONS, because either alone is passed by a wrong fix. A routing rule that matched
// nothing would satisfy "not in anybody else's queue" while putting every pull request in nobody's —
// which is #79's consequence with the sign flipped, and is the failure mode the empty author set
// produced in the first place.
func TestOwnershipComesFromTheBranchNameInBothDirections(t *testing.T) {
	for _, owner := range []string{"dev", "qa", "product"} {
		f := &queueFixture{branch: owner + "/feat/9-x"}
		out, rc := f.run(t, owner)
		noJQError(t, out)
		if rc != 0 {
			t.Fatalf("queue.sh %s exited %d on its own branch %s\n%s", owner, rc, f.branch, out)
		}
		if !needsFirstReview(out) {
			t.Errorf("the branch is %s, so this pull request is %s's own work and %s must be told to "+
				"dispatch a reviewer for it. It was not, so the pull request is in nobody's "+
				"queue.\n%s", f.branch, owner, owner, out)
		}
		for _, other := range []string{"dev", "qa", "product"} {
			if other == owner {
				continue
			}
			out, rc := f.run(t, other)
			noJQError(t, out)
			if rc != 0 {
				t.Fatalf("queue.sh %s exited %d\n%s", other, rc, out)
			}
			if awaitingReview(out) {
				t.Errorf("the branch is %s, yet %s was told to review it. Independence is not "+
					"decided here any more, but whose queue something is in still is, and a "+
					"second role picking up the work is the alternating loop the change "+
					"removed.\n%s", f.branch, other, out)
			}
		}
	}
}

// TestADeterminedAnswerAndAnUndeterminedOneStillDiffer is the invariant that outlived the mechanism,
// and it is asserted here because it is the whole of #79 once the author lookup is subtracted.
//
// A queue that ran and found no work for this role exits 0 and says so. A queue that could not read
// the board must not produce the same pair of observations, because an agent reads a zero exit and a
// short list as an answer and goes and does something else.
func TestADeterminedAnswerAndAnUndeterminedOneStillDiffer(t *testing.T) {
	determined, drc := (&queueFixture{branch: "qa/fix/9-x"}).run(t, "dev")
	noJQError(t, determined)
	if drc != 0 {
		t.Fatalf("a readable board with nothing for dev to review made queue.sh dev exit %d, so "+
			"the comparison below has no determined side\n%s", drc, determined)
	}
	if awaitingReview(determined) {
		t.Fatalf("the fixture is wrong: dev was offered a review of qa's branch, so this is not the "+
			"'nothing to review' case it needs to be\n%s", determined)
	}

	undetermined, urc := (&queueFixture{fail: true}).run(t, "dev")
	if urc == 0 {
		t.Errorf("every call to GitHub failed and queue.sh still exited 0 — byte-identical in exit "+
			"code to the run above, where the board was read and simply held nothing for this role. "+
			"`could not determine` and `determined to be nothing` have collapsed.\n%s", undetermined)
	}
	if !strings.Contains(undetermined, "::error::") {
		t.Errorf("the board could not be read and the queue printed no error. A role sees a short "+
			"queue and no reason, which is an outage wearing the costume of an empty board.\n%s",
			undetermined)
	}
}

// TestQueueRefusesARoleItHasNoQueueFor. The complement of the routing above: an unknown role is a
// typo or a rename, and answering it with an empty queue is the same fail-open one level out.
func TestQueueRefusesARoleItHasNoQueueFor(t *testing.T) {
	script := queueScript(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"),
		[]byte("#!/usr/bin/env bash\necho '[]'\n"), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	cmd := exec.Command("bash", script, "devv")
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "REPO=x/y")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("queue.sh answered for the role 'devv', which has no queue, and exited 0. A typo "+
			"then reads as a clean board and the role does nothing, having been told nothing.\n%s",
			out)
	}
}
