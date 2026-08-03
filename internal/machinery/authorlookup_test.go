package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A FAILED AUTHOR LOOKUP IS NOT AN EMPTY AUTHOR SET (Issue #79).
//
// `reviews_waiting` in `.workflow/bin/queue.sh` derives independence by calling
// `pr-authors.sh --pr <n>` and discarding both its stderr and its exit code:
//
//	authors=$(... /pr-authors.sh --pr "$num" 2>/dev/null || echo "")
//
// An empty author set already carried two meanings, and the script handles both — no `Agent:`
// trailer anywhere is a commit defect the naming gate reports, and trailers that are all spec-only
// means nobody authored product judgement so every role is independent. There is a THIRD, and it is
// the one that was not handled: **the question could not be answered.** That is this project's own
// rule — `could not determine` and `determined to be nothing` must never collapse — broken inside
// the routing that enforces independence.
//
// The consequence is not a cosmetic blank. `grep -qx "$role"` against an empty string matches
// nothing, so the pull request is offered to EVERY role including the ones that wrote it, and the
// gate re-derives authorship from git at verdict time, where the lookup does not fail. The role does
// the whole review and has its verdict refused. Observed live during a secondary rate limit:
// `queue.sh dev` printed `#46 ... run /review-pr 46   (built by )` for a pull request carrying nine
// `Agent: dev` trailers.
//
// This is the sibling of `TestAFailedCommentsQueryIsNotAnAbsentVerdict` in
// `reviewassignment_test.go`, which pins the same rule for the VERDICT lookup, and it is built the
// same way: it EXECUTES the installed `.workflow/bin/queue.sh` against a stub `gh` rather than
// restating its logic. `.workflow/bin/` is replaced wholesale by the next `install.sh` run, so a
// refresh that removes the fix turns this suite red instead of quietly restoring the defect.
//
// WHAT IT CANNOT DETERMINE, IT SAYS. Every external dependency is probed rather than named and
// assumed; a missing one skips with a reason stating the check did not run and is NOT passing.

// queueAuthorFixture drives the installed queue against a stub `gh` in which the AUTHOR lookup can be
// made to fail while every other call succeeds.
//
// A STUB THAT FAILS EVERYTHING CANNOT TEST THIS. The queue exits non-zero on the first failed call,
// so such an arm goes green whether or not the author lookup handles anything — the same trap
// recorded on the comments query in `reviewassignment_test.go`. Here the failure is narrowed to one
// endpoint, so every earlier call succeeds and the failure asserted is the one under test.
type queueAuthorFixture struct {
	// failFirstCommitList fails the FIRST `pulls/9/commits` call and lets the rest succeed. This is
	// the reachable path exactly as it occurs under a SECONDARY rate limit, which fails
	// intermittently rather than outright: `pr-authors.sh --pr 9` fails and yields an empty set,
	// the follow-up `--pr 9 --all-trailers` succeeds and yields a trailer, so the guard that fires
	// only when BOTH come back empty does not fire.
	failFirstCommitList bool
	// failCommitFiles fails the per-commit file-list query, which is the second half of the same
	// lookup. The commit list arrives carrying `Agent: dev`, and the file list that decides whether
	// that commit is spec-only cannot be read. An unreadable file list is not a spec-only commit.
	failCommitFiles bool
	// specOnly makes dev's commit touch nothing outside `openspec/`. Every call succeeds, so the
	// empty author set this produces is a DETERMINED one and must keep its existing meaning.
	specOnly bool
}

const queueAuthorHead = "cafe"

// stub is the entire world the queue sees. Nothing here talks to GitHub.
const authorStub = `#!/usr/bin/env bash
bump() { # bump <file> -> the number of times this call has now been made
  local n=0
  [ -f "$1" ] && n=$(cat "$1")
  n=$((n+1)); printf '%s' "$n" > "$1"; printf '%s' "$n"
}
case "$*" in
  *"pulls/9/commits"*)
    if [ "${FAIL_FIRST_COMMIT_LIST:-0}" = 1 ] && [ "$(bump "$STUB_DIR/commit-list-calls")" = 1 ]; then
      echo 'You have exceeded a secondary rate limit' >&2; exit 1
    fi
    printf 'c9\tAgent: dev\n' ;;
  *"/commits/c9"*)
    if [ "${FAIL_COMMIT_FILES:-0}" = 1 ]; then
      echo 'You have exceeded a secondary rate limit' >&2; exit 1
    fi
    if [ "${SPEC_ONLY:-0}" = 1 ]; then echo 'openspec/changes/x/proposal.md'; else echo 'internal/a.go'; fi ;;
  *"pulls?state=open"*) echo '[{"number":9,"head":{"ref":"dev/feat/9-x","sha":"` + queueAuthorHead + `"},"title":"feat(x): y"}]' ;;
  *"/commits/` + queueAuthorHead + `/status"*) echo '{"statuses":[]}' ;;
  *) echo '[]' ;;
esac
`

func (f *queueAuthorFixture) run(t *testing.T, role string) (string, int) {
	t.Helper()
	script := queueScript(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(authorStub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	env := append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REPO=x/y",
		"STUB_DIR="+dir,
		"FAIL_FIRST_COMMIT_LIST="+boolFlag(f.failFirstCommitList),
		"FAIL_COMMIT_FILES="+boolFlag(f.failCommitFiles),
		"SPEC_ONLY="+boolFlag(f.specOnly),
	)
	cmd := exec.Command("bash", script, role)
	cmd.Env = env
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

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// TestTheAuthorLookupFixtureIsWellFormed. A red produced by a broken stub proves nothing, and every
// arm below asserts a red. This one asserts the healthy shape: the queue runs clean, offers the
// pull request to a role that authored none of it, and withholds it from the role that did. If this
// fails, the reds below are about the fixture and not about the queue.
func TestTheAuthorLookupFixtureIsWellFormed(t *testing.T) {
	f := &queueAuthorFixture{}
	out, rc := f.run(t, "product")
	if rc != 0 {
		t.Fatalf("the healthy stub made queue.sh product exit %d, so nothing below is measuring the "+
			"author lookup\n%s", rc, out)
	}
	if !offered(out) {
		t.Fatalf("the healthy stub did not offer this pull request to product, which authored none "+
			"of its commits\n%s", out)
	}
	out, rc = f.run(t, "dev")
	if rc != 0 {
		t.Fatalf("the healthy stub made queue.sh dev exit %d\n%s", rc, out)
	}
	if offered(out) {
		t.Fatalf("the healthy stub offered this pull request to dev, which authored it — the author "+
			"lookup is not working even when every call succeeds\n%s", out)
	}
}

// TestAnIntermittentAuthorLookupFailureOffersNothing is Issue #79's reachable path, driven end to
// end. The FIRST author lookup fails and the `--all-trailers` follow-up succeeds, which is what a
// secondary rate limit produces and why the existing both-empty guard does not fire.
//
// The role asserted is `dev`, which authored every commit here. Under the defect the empty set
// matched no role, so this is precisely the case where the queue sends a role to review its own
// work and the gate then refuses the verdict it did the work to produce.
func TestAnIntermittentAuthorLookupFailureOffersNothing(t *testing.T) {
	f := &queueAuthorFixture{failFirstCommitList: true}
	out, rc := f.run(t, "dev")
	if rc == 0 {
		t.Errorf("the author lookup failed and queue.sh exited 0. `could not determine who built "+
			"this` and `determined that nobody did` have collapsed into one answer, and an agent "+
			"reads a zero exit as an answer.\n%s", out)
	}
	if offered(out) {
		t.Errorf("the author lookup failed and the queue offered the review to dev, which authored "+
			"every commit here. The gate re-derives authorship from git at verdict time, where the "+
			"lookup does not fail, so it sees `dev` and refuses — the role does the whole review and "+
			"has it rejected.\n%s", out)
	}
}

// TestAnUnreadableCommitFileListIsNotASpecOnlyCommit is the same rule one layer down, inside
// `pr-authors.sh` itself. The commit list arrives and carries `Agent: dev`; the file list that
// decides whether that commit changed anything outside `openspec/` cannot be read.
//
// An EMPTY file list means a commit that changed nothing, which confers no authorship — a
// determined answer, and the right one. An UNREADABLE file list means the question was not
// answered, and it must not borrow that verdict: doing so silently converts the author of a code
// commit into an independent reviewer of it.
func TestAnUnreadableCommitFileListIsNotASpecOnlyCommit(t *testing.T) {
	f := &queueAuthorFixture{failCommitFiles: true}
	out, rc := f.run(t, "dev")
	if rc == 0 {
		t.Errorf("the file list of a commit could not be read and queue.sh exited 0. An unreadable "+
			"diff was treated as a diff that touched nothing outside openspec/, so a role that "+
			"wrote code became independent of it.\n%s", out)
	}
	if offered(out) {
		t.Errorf("the file list of dev's own commit could not be read and the queue offered the "+
			"review to dev.\n%s", out)
	}
}

// TestAFailedAuthorLookupOffersNothingToAnyRole. The defect's visible half is the author being sent
// its own pull request, but the rule is wider than that: when independence cannot be established it
// cannot be established FOR ANYBODY. Offering the work to a role that happens to be independent is
// still work handed out on the strength of a query that never ran, and the next outage that shifts
// which call fails turns it back into a self-review.
func TestAFailedAuthorLookupOffersNothingToAnyRole(t *testing.T) {
	for _, f := range []*queueAuthorFixture{
		{failFirstCommitList: true},
		{failCommitFiles: true},
	} {
		for _, role := range []string{"qa", "product"} {
			out, rc := f.run(t, role)
			if rc == 0 {
				t.Errorf("author lookup failure (%+v): queue.sh %s exited 0\n%s", *f, role, out)
			}
			if offered(out) {
				t.Errorf("author lookup failure (%+v): the queue offered this pull request to %s on "+
					"the strength of a query that never ran. Independence was not determined for "+
					"anyone here, and an answer nobody computed is not an answer.\n%s", *f, role, out)
			}
		}
	}
}

// TestTheAuthorLookupFailureIsNamedInTheOutput. A non-zero exit with nothing said is a role
// stopping without knowing why, and the queue's own convention — every filter names what it dropped,
// every failed query says it was a failure and not an empty answer — is that the reason is printed.
// The `2>/dev/null` on the call site is what removed it.
func TestTheAuthorLookupFailureIsNamedInTheOutput(t *testing.T) {
	f := &queueAuthorFixture{failFirstCommitList: true}
	out, _ := f.run(t, "dev")
	if !strings.Contains(out, "::error::") {
		t.Errorf("the author lookup failed and the queue printed no error. A role sees an empty "+
			"queue and a non-zero exit with no reason, which is the outage wearing the costume of a "+
			"bug in the queue.\n%s", out)
	}
}

// TestADeterminedEmptyAuthorSetStillReachesEveryRole is the property this change must NOT break, and
// it is asserted here because the fix above is one careless widening away from breaking it. When
// every commit is spec-only the author set is empty and that emptiness is an ANSWER: nobody authored
// product judgement, so every role is independent — including the role that pushed the commits.
//
// `check-review.sh` argues the same case and is right; the exemption is what unblocked a board of
// eleven pull requests with exactly one eligible reviewer between them (#32). Treating this empty set
// like the undetermined one would restore that deadlock, which is why it is a separate question and
// is recorded on #32 rather than folded in here.
func TestADeterminedEmptyAuthorSetStillReachesEveryRole(t *testing.T) {
	f := &queueAuthorFixture{specOnly: true}
	for _, role := range []string{"dev", "qa", "product"} {
		out, rc := f.run(t, role)
		if rc != 0 {
			t.Fatalf("every query succeeded and queue.sh %s exited %d. A determined empty author set "+
				"is an answer, not an outage.\n%s", role, rc, out)
		}
		if !offered(out) {
			t.Errorf("dev's only commit touched nothing outside openspec/, so nobody authored product "+
				"judgement and every role is independent — yet the queue offered this to no %s. That "+
				"is the deadlock the exemption exists to prevent (#32).\n%s", role, out)
		}
	}
}
