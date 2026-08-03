package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A REFUSED REVIEW AND AN ABSENT ONE MUST NOT RENDER THE SAME SENTENCE. Both are red, and the
// colour is not the point: one means "fix your comment, it never parsed", the other means "fix the
// code, a reviewer refused it". A reviewer that read the wrong one went looking for a formatting
// fault in its own verdict and re-posted it (Issue #52).
//
// These tests drive the SHIPPED machinery end to end — `.workflow/bin/check-review.sh` for the
// exit code, and the publish step's script lifted out of `.github/workflows/gates.yml` for the
// wording — so the thing under test is the thing that runs in CI, not a restatement of it. What
// cannot be driven here is GitHub itself: `steps.check.outcome` is supplied as the value Actions
// documents it to take (the result BEFORE `continue-on-error`), and the `gh api` call that files
// the status is stubbed out.

const (
	// The two descriptions whose collapse into one is the defect. Matched loosely, on the phrase
	// that carries the meaning, so rewording the sentence does not fail the test but erasing the
	// distinction does.
	absentPhrase  = "No current review"
	refusedPhrase = "Changes requested"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("cannot resolve working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %q, so the repository root cannot be found", dir)
		}
		dir = parent
	}
}

func needTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not on PATH, so the shell machinery cannot be driven here", name)
	}
}

// publishScript lifts the `run:` body of the "Publish the verdict as a commit status" step out of
// gates.yml and makes it runnable: the workflow expressions this test controls become environment
// variables, and `gh` becomes a no-op so nothing is filed anywhere.
//
// AN UNRECOGNISED `${{ }}` FAILS THE TEST rather than being papered over. If the installer adds an
// expression, its value is a thing this test is silently assuming, and a silent assumption in the
// test that guards the wording is how the wording broke.
func publishScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".github", "workflows", "gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")

	start := -1
	for i, l := range lines {
		if strings.Contains(l, "name: Publish the verdict as a commit status") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no step named 'Publish the verdict as a commit status'. Either it was "+
			"renamed — update this test — or the step that publishes the review status is gone, "+
			"which is a far larger problem than a renamed test.", path)
	}

	runAt, indent := -1, 0
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "run: |" {
			runAt = i
			indent = len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
			break
		}
		if strings.HasPrefix(trimmed, "- ") && i > start {
			break // ran into the next step
		}
	}
	if runAt < 0 {
		t.Fatalf("the publish step in %s has no `run: |` block", path)
	}

	body := indent + 2
	var out []string
	for i := runAt + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			out = append(out, "")
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) < body {
			break
		}
		out = append(out, l[body:])
	}
	script := strings.Join(out, "\n")

	for expr, sub := range map[string]string{
		"${{ steps.selftest.outcome }}":    "$SELFTEST_OUTCOME",
		"${{ steps.check.outcome }}":       "$CHECK_OUTCOME",
		"${{ steps.pr.outputs.head_sha }}": "$HEAD_SHA",
		"${{ github.run_id }}":             "$GITHUB_RUN_ID",
	} {
		script = strings.ReplaceAll(script, expr, sub)
	}
	if i := strings.Index(script, "${{"); i >= 0 {
		rest := script[i:]
		if j := strings.Index(rest, "}}"); j >= 0 {
			rest = rest[:j+2]
		}
		t.Fatalf("the publish step uses the workflow expression %q, which this test does not know "+
			"how to supply. Add it to the substitution table above with the value CI would give "+
			"it — leaving it unsubstituted would let this test pass on a script that is not the "+
			"one CI runs.", rest)
	}

	// Nothing here talks to GitHub. The step's own `echo "published: …"` is the observation.
	return "gh() { return 0; }\n" + script
}

// publish runs the lifted step and returns the description it would file.
func publish(t *testing.T, dir, selftest, check, reviewRC string) (state, desc string) {
	t.Helper()
	if reviewRC != "" {
		if err := os.WriteFile(filepath.Join(dir, "review.rc"), []byte(reviewRC+"\n"), 0o644); err != nil {
			t.Fatalf("cannot seed review.rc: %v", err)
		}
	} else {
		os.Remove(filepath.Join(dir, "review.rc"))
	}

	cmd := exec.Command("bash", "-c", publishScript(t))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SELFTEST_OUTCOME="+selftest,
		"CHECK_OUTCOME="+check,
		"HEAD_SHA=0123456789012345678901234567890123456789",
		"GITHUB_SERVER_URL=https://github.com",
		"GITHUB_REPOSITORY=x/y",
		"GITHUB_RUN_ID=1",
		"REPO=x/y",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the publish step failed to run (selftest=%s check=%s rc=%s): %v\n%s",
			selftest, check, reviewRC, err, out)
	}
	for _, l := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(l, "published: "); ok {
			state, desc, _ = strings.Cut(rest, " — ")
			return state, desc
		}
	}
	t.Fatalf("the publish step printed no `published:` line, so what it would file cannot be read:\n%s", out)
	return "", ""
}

// reviewFixture is a throwaway git repository shaped like a pull request: one base commit and one
// commit authored by `dev`.
type reviewFixture struct {
	dir, head, base string

	// policy, when set, is written to a file the gate is pointed at with REVIEW_POLICY_FILE.
	// `.workflow/review-policy` is PROJECT-owned and this repository's says `self-allowed`, so a
	// fixture that does not say which policy it is under is testing whichever one the temp
	// directory happens to imply — the strict default. Issue #82 lives entirely in the permissive
	// one, so it says so.
	policy string
}

// selfAllowed returns the fixture under the `self-allowed` policy — the one 11605b5 enabled and
// the one Issue #82 is about.
func (f reviewFixture) selfAllowed(t *testing.T) reviewFixture {
	t.Helper()
	path := filepath.Join(f.dir, "policy")
	if err := os.WriteFile(path, []byte("self-allowed\n"), 0o644); err != nil {
		t.Fatalf("cannot write the policy file: %v", err)
	}
	f.policy = path
	return f
}

func newReviewFixture(t *testing.T) reviewFixture {
	t.Helper()
	needTool(t, "bash")
	needTool(t, "git")
	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "chore: seed")
	base := run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "g"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "fix(x): y\n\nAgent: dev")
	return reviewFixture{dir: dir, head: run("rev-parse", "HEAD"), base: base}
}

// checkRC writes the given review comment bodies and returns check-review.sh's exit code — the
// value the workflow records in review.rc.
func (f reviewFixture) checkRC(t *testing.T, bodies ...string) int {
	t.Helper()
	rc, _ := f.checkOut(t, bodies...)
	return rc
}

// checkOut is checkRC plus what the gate said. The exit code alone cannot tell "no review exists"
// from "a review exists and I refuse to attribute it" — both are 1, deliberately, because both are
// "this head is not certified". The distinction lives in the message, so the message is asserted.
func (f reviewFixture) checkOut(t *testing.T, bodies ...string) (int, string) {
	t.Helper()
	quoted := make([]string, 0, len(bodies))
	for _, b := range bodies {
		quoted = append(quoted, `{"body":`+jsonQuote(b)+`}`)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "comments.json"),
		[]byte("["+strings.Join(quoted, ",")+"]"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot(t), ".workflow", "bin", "check-review.sh")
	cmd := exec.Command("bash", script, f.head, "comments.json", f.base)
	cmd.Dir = f.dir
	if f.policy != "" {
		cmd.Env = append(os.Environ(), "REVIEW_POLICY_FILE="+f.policy)
	}
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("cannot run %s: %v\n%s", script, err, out)
		}
		rc = ee.ExitCode()
	}
	t.Logf("check-review.sh exited %d\n%s", rc, out)
	return rc, string(out)
}

// jsonQuote quotes a string as a JSON scalar. `encoding/json` would do it, but the bodies here
// are short and the escaping is the whole of what is needed.
func jsonQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

// review is a verdict block with no `[role]` marker above it — the shape every verdict in this
// repository had before Issue #65. It is kept for the tests that are about something else.
func review(reviewer, sha, verdict string) string {
	return "Reviewed-by: " + reviewer + "\nReviewed-sha: " + sha + "\nVerdict: " + verdict
}

// posted is a verdict as a whole COMMENT: the `[role]` marker that says who is speaking, then the
// block that says what the verdict is. `poster` and `declared` are separate arguments precisely
// because Issue #65 is about them being allowed to differ.
func posted(poster, declared, sha, verdict string) string {
	return "[" + poster + "]\n" + review(declared, sha, verdict)
}

// fenced wraps text in a Markdown code fence — a comment QUOTING a verdict rather than giving one.
func fenced(marker, text string) string {
	return marker + "\n" + text + "\n" + marker
}

// TestQuotedVerdictIsNotAVerdict is Issue #65. The gate took the reviewer's name from the comment's
// TEXT, so any role could mint any other role's approval — and product did it by accident on #63 by
// quoting the verdict template to ASK for a verdict. The quote sat inside a code fence; `jq`'s
// `test()` and the `sed -n 's/^Reviewed-by:…'` extraction have no notion of fences, and sed's `^`
// matches inside one.
//
// WHAT THIS TEST CAN AND CANNOT ESTABLISH, said here rather than in a commit message nobody reads:
// every role on this repository posts through the SAME GitHub account, so `.user.login` separates
// the human from nobody. The poster's role is derived from the `[role]` marker the roles already
// sign every comment with and that `queue.sh` already routes on. That marker is a CONVENTION, not
// an authenticated fact. This closes the accident — a quoted template can no longer be read as a
// verdict, and a verdict can no longer name somebody other than its poster — and it does NOT make
// the attestation unforgeable by a role that sets out to forge one.
func TestQuotedVerdictIsNotAVerdict(t *testing.T) {
	f := newReviewFixture(t) // the one commit here is `Agent: dev`

	// `product` authored nothing in this range, so under the OLD gate every quoted block below
	// yields a clean independent approve and exits 0. That is the red these cases are written to
	// see: a wrong name would make them fail for the independence rule instead, and pass for the
	// wrong reason.
	quote := review("product", f.head, "approve")

	cases := []struct {
		name    string
		comment string
		wantRC  int
		want    string // phrase the gate's output must carry
	}{
		{
			// THE #63 NEAR-MISS, DRIVEN. product asks dev to re-attest by quoting the template.
			name:    "a fenced quote of a verdict is not a verdict",
			comment: "[product]\ncould you re-attest this? post exactly:\n\n" + fenced("```", quote) + "\n\nthanks",
			wantRC:  1,
			want:    "no review found",
		},
		{
			// The audit on this Issue noted its own limit: a quoted verdict placed at the very TOP
			// of a comment would have passed it. Placement must not matter.
			name:    "a fenced quote at the top of the comment is still not a verdict",
			comment: "[product]\n" + fenced("```", quote) + "\n\n^ that is the shape we want",
			wantRC:  1,
			want:    "no review found",
		},
		{
			// `~~~` is a fence too, and a fix that only knew about backticks would be a fix for
			// the one comment that happened to cause the incident.
			name:    "a tilde fence is a fence",
			comment: "[product]\nexample:\n" + fenced("~~~", quote),
			wantRC:  1,
			want:    "no review found",
		},
		{
			// A verdict quoted with `>` is being discussed, not given. sed's `^` misses this one
			// today, so it is not the live hole — it is the next one, and it costs nothing here.
			name:    "a blockquoted verdict is not a verdict",
			comment: "[product]\n> Reviewed-by: product\n> Reviewed-sha: " + f.head + "\n> Verdict: approve",
			wantRC:  1,
			want:    "no review found",
		},
		{
			// CRITERION 2. Not silently re-attributed to the poster — REFUSED, saying they
			// disagree. Silent correction hides an attempt to forge.
			name:    "a verdict naming somebody other than its poster is refused, and says so",
			comment: posted("product", "qa", f.head, "approve"),
			wantRC:  1,
			want:    "DISAGREE",
		},
		{
			// CRITERION 5, against a reviewer that LIED about its name. This is the case the
			// independence rule has never been tested against: `dev` built this branch, and under
			// the old gate typing somebody else's name was the whole of what it took.
			name:    "an author cannot certify its own work by typing another role's name",
			comment: posted("dev", "product", f.head, "approve"),
			wantRC:  1,
			want:    "DISAGREE",
		},
		{
			// CRITERION 5 with the truth told. Unchanged behaviour, now reached by a derived name.
			name:    "an author that names itself still cannot certify its own work",
			comment: posted("dev", "dev", f.head, "approve"),
			wantRC:  1,
			want:    "authored commits in this PR",
		},
		{
			// CRITERION 4, THE OTHER DIRECTION. A fix that refuses everything passes every case
			// above and breaks the workflow entirely.
			name:    "a genuine independent verdict still passes",
			comment: posted("product", "product", f.head, "approve"),
			wantRC:  0,
			want:    "review ok",
		},
		{
			// A genuine verdict is allowed to CONTAIN a fence — reviewers paste output. Only the
			// fenced text is discarded, never the comment.
			name: "a genuine verdict is not lost because it also quotes something",
			comment: posted("product", "product", f.head, "approve") +
				"\n\nI ran it:\n" + fenced("```", "$ make ci\nok"),
			wantRC: 0,
			want:   "review ok",
		},
		{
			// THE UNDECIDED PATH, REFUSING LOUDLY. Nothing in this Issue's criteria says what a
			// verdict with no `[role]` marker means, and there is no honest default: reading its
			// `Reviewed-by:` is the hole this Issue is about, and inventing a poster is worse. So
			// the gate says it COULD NOT DETERMINE who posted it — which is a different value from
			// "no review exists" and from "this review is forged", and it must not be spelled like
			// either. See the PR body; the Issue stays open on this.
			name:    "a verdict whose poster cannot be determined is refused as undetermined",
			comment: review("product", f.head, "approve"),
			wantRC:  1,
			want:    "COULD NOT BE DETERMINED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, out := f.checkOut(t, tc.comment)
			if rc != tc.wantRC {
				t.Errorf("check-review.sh exited %d, want %d", rc, tc.wantRC)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the gate said %q, which does not carry %q", strings.TrimSpace(out), tc.want)
			}
		})
	}

	// AND THE #63 CONFIGURATION ITSELF: the quote FIRST, the genuine verdict AFTER. On #63 this
	// came out right only because `| last` happened to pick the real one. Order must stop
	// mattering, so drive it in the order that hid the defect AND in the order that exposes it.
	t.Run("a quote after a genuine verdict does not displace it", func(t *testing.T) {
		rc, out := f.checkOut(t,
			posted("product", "product", f.head, "approve"),
			"[qa]\nfor the record, the verdict was:\n"+fenced("```", quote),
		)
		if rc != 0 {
			t.Errorf("check-review.sh exited %d, want 0 — a later quotation displaced a real "+
				"verdict, which is the `| last` hazard from the other side\n%s", rc, out)
		}
	})
	t.Run("a quote before a genuine verdict does not become the verdict", func(t *testing.T) {
		rc, out := f.checkOut(t,
			"[qa]\nplease post:\n"+fenced("```", quote),
			posted("product", "product", f.head, "approve"),
		)
		if rc != 0 {
			t.Errorf("check-review.sh exited %d, want 0\n%s", rc, out)
		}
	})
}

// stateFor is the outcome Actions reports for the check step, given its exit code. `outcome` is
// the result BEFORE `continue-on-error`, which is why a non-zero exit reads as `failure` even
// though the step is allowed to keep the job green.
func stateFor(rc int) string {
	if rc == 0 {
		return "success"
	}
	return "failure"
}

// TestRefusedReviewDoesNotReadAsNoReview is Issue #52 criteria 1–4, driven: the exit code comes
// from the real checker and the sentence from the real workflow step, for the same head.
func TestRefusedReviewDoesNotReadAsNoReview(t *testing.T) {
	f := newReviewFixture(t)

	cases := []struct {
		name     string
		comments []string
		wantRC   int
		want     string // phrase the description must carry
		notWant  string // phrase it must not
	}{
		{
			// Criterion 1. `product` authored nothing here, so the refusal is independent.
			name:     "an independent refusal says changes were requested",
			comments: []string{posted("product", "product", f.head, "changes-requested")},
			wantRC:   2,
			want:     refusedPhrase,
			notWant:  absentPhrase,
		},
		{
			// THE PR #42 CASE, and the one that was broken. `dev` authored the commit, so the
			// review is both a refusal AND non-independent. Two complaints, and the refusal is the
			// one the reviewer needs back: it had landed, and the status said it had not.
			name:     "a refusal by an author of the branch still says changes were requested",
			comments: []string{posted("dev", "dev", f.head, "changes-requested")},
			wantRC:   2,
			want:     refusedPhrase,
			notWant:  absentPhrase,
		},
		{
			// Criterion 2.
			name:     "no review for this head says no review exists",
			comments: []string{},
			wantRC:   1,
			want:     absentPhrase,
			notWant:  refusedPhrase,
		},
		{
			// Criterion 3. A push invalidates a review; a stale refusal is not a live one.
			//
			// IT NAMES `f.base`, A REAL EARLIER COMMIT, NOT FORTY ZEROS. That is what a stale
			// review actually is, and since Issue #84 the two are different facts: a sha this
			// repository knows is silently stale, a sha naming no object at all is reported rather
			// than dropped. `TestAVerdictNamingAnUnknownShaIsReported` covers the other one.
			name:     "a refusal naming another sha reads as no current review",
			comments: []string{posted("product", "product", f.base, "changes-requested")},
			wantRC:   1,
			want:     absentPhrase,
			notWant:  refusedPhrase,
		},
		{
			name:     "an independent approve passes",
			comments: []string{posted("product", "product", f.head, "approve")},
			wantRC:   0,
			want:     "Reviewed by an agent",
			notWant:  absentPhrase,
		},
	}

	descs := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := f.checkRC(t, tc.comments...)
			if rc != tc.wantRC {
				t.Errorf("check-review.sh exited %d, want %d", rc, tc.wantRC)
			}
			state, desc := publish(t, f.dir, "success", stateFor(rc), strconv.Itoa(rc))
			t.Logf("published: %s — %s", state, desc)

			wantState := "failure"
			if tc.wantRC == 0 {
				wantState = "success"
			}
			if state != wantState {
				t.Errorf("state is %q, want %q", state, wantState)
			}
			if !strings.Contains(desc, tc.want) {
				t.Errorf("description %q does not carry %q", desc, tc.want)
			}
			if tc.notWant != "" && strings.Contains(desc, tc.notWant) {
				t.Errorf("description %q carries %q, which belongs to a different state — this is "+
					"Issue #52: a reviewer cannot tell what the gate is telling it", desc, tc.notWant)
			}
			descs[tc.name] = desc
		})
	}

	// Criterion 2's second half, and criterion 4: the two red sentences must actually DIFFER. A
	// fix that made both say the same new thing is not a fix.
	refused := descs["an independent refusal says changes were requested"]
	absent := descs["no review for this head says no review exists"]
	if refused != "" && refused == absent {
		t.Errorf("a refusal and an absent review publish the identical description %q", refused)
	}
}

// TestPublishDistinguishesRC1FromRC2 is criterion 4 at the wiring alone: whatever the checker did,
// the two recorded exit codes must not be able to produce the same sentence. It also pins the
// behaviour when review.rc is missing or empty — a step that died before writing it must not be
// read as a refusal.
func TestPublishDistinguishesRC1FromRC2(t *testing.T) {
	needTool(t, "bash")
	dir := t.TempDir()

	_, one := publish(t, dir, "success", "failure", "1")
	_, two := publish(t, dir, "success", "failure", "2")
	if one == two {
		t.Fatalf("rc=1 and rc=2 both publish %q, so the exit code the checker went to the trouble "+
			"of splitting reaches nobody", one)
	}
	if !strings.Contains(two, refusedPhrase) {
		t.Errorf("rc=2 publishes %q, which does not say changes were requested", two)
	}
	if strings.Contains(two, absentPhrase) {
		t.Errorf("rc=2 publishes %q, which claims no review exists", two)
	}
	if !strings.Contains(one, absentPhrase) {
		t.Errorf("rc=1 publishes %q, which does not say no review exists", one)
	}

	for _, rc := range []string{"", "0", "banana"} {
		if _, d := publish(t, dir, "success", "failure", rc); strings.Contains(d, refusedPhrase) {
			t.Errorf("review.rc=%q publishes %q — an unwritten or unreadable exit code must not be "+
				"read as a landed refusal", rc, d)
		}
	}

	// AN UNFINISHED CHECK MUST NOT ACCUSE. `cancelled`, `skipped` and the empty string are not
	// evidence that no review exists.
	for _, outcome := range []string{"cancelled", "skipped", ""} {
		s, d := publish(t, dir, "success", outcome, "1")
		if s != "pending" {
			t.Errorf("outcome %q publishes state %q, want pending (%s)", outcome, s, d)
		}
	}
	// A broken checker holds rather than approving.
	if s, d := publish(t, dir, "failure", "success", "0"); s != "pending" {
		t.Errorf("a failed self-test publishes state %q, want pending (%s)", s, d)
	}
}

// TestCheckerSelfTestPasses runs the installed checker's own self-test. The workflow refuses to
// publish a pass when this fails, so a repository whose checker cannot verify itself has no
// working review gate at all — worth knowing from `make ci` rather than from CI.
func TestCheckerSelfTestPasses(t *testing.T) {
	needTool(t, "bash")
	script := filepath.Join(repoRoot(t), ".workflow", "bin", "check-review.sh")
	out, err := exec.Command("bash", script, "--self-test").CombinedOutput()
	if err != nil {
		t.Fatalf("check-review.sh --self-test failed: %v\n%s", err, out)
	}
}

// TestASelfApproveDoesNotEraseARefusal is Issue #82. `11605b5` enabled self-review and stated its
// own invariant — "this widens WHO may certify, never WHAT counts as certified" — and the invariant
// did not hold: the gate selected exactly ONE verdict block for the head, `| last`, so `refused` was
// computed from the final comment alone and an earlier `changes-requested` was never read. An author
// erased an independent refusal by posting a self-approve after it. No code change, no new commit.
//
// BOTH CONTROLS ARE DRIVEN, and they are the point. The defect's signature is that the test row is
// byte-identical in outcome to the no-refusal control, so a probe that cannot tell the two controls
// apart proves nothing by agreeing with either of them.
//
// THE RULE CHOSEN, and why. A refusal is cleared only by a LATER VERDICT FROM THE SAME REVIEWER —
// the gate keeps each reviewer's most recent verdict for the head and refuses while any of them is
// `changes-requested`. That makes "a reviewer changed its mind" the only thing that clears a
// refusal, which is criterion 4, and it means nobody can vote away somebody else's refusal —
// neither the author (criterion 1) nor a second independent reviewer, which the Issue records as
// the pre-existing half of the same defect. A push still clears everything, because a verdict is
// bound to a head sha (criterion 3), so a refused branch is fixed by fixing it and never trapped.
func TestASelfApproveDoesNotEraseARefusal(t *testing.T) {
	f := newReviewFixture(t).selfAllowed(t) // the one commit here is `Agent: dev`

	// `dev` built this branch, so a verdict from dev is a SELF-review; `qa` built none of it.
	selfApprove := posted("dev", "dev", f.head, "approve")
	qaRefusal := posted("qa", "qa", f.head, "changes-requested")

	cases := []struct {
		name     string
		kind     string // CONTROL or TEST
		comments []string
		wantRC   int
		want     string
		notWant  string
	}{
		{
			// CONTROL. An independent refusal, alone, refuses with its own exit code.
			name:     "control: an independent refusal alone refuses",
			kind:     "CONTROL",
			comments: []string{qaRefusal},
			wantRC:   2,
			want:     "requests changes",
		},
		{
			// CONTROL, and CRITERION 2. A self-approve with nothing before it still passes as a
			// self-review. A fix that refuses everything satisfies criterion 1 and disables the
			// feature 11605b5 had just enabled.
			name:     "control: a self-approve alone still passes as SELF-REVIEWED",
			kind:     "CONTROL",
			comments: []string{selfApprove},
			wantRC:   3,
			want:     "SELF-REVIEWED",
		},
		{
			// THE DEFECT, AND CRITERION 1. Under the shipped gate this row was byte-identical to
			// the control above: rc=3, "no independent agent has looked" — while an independent
			// agent had looked and had said no.
			name:     "an author cannot erase an independent refusal with a self-approve",
			kind:     "TEST",
			comments: []string{qaRefusal, selfApprove},
			wantRC:   2,
			want:     "requests changes",
			notWant:  "SELF-REVIEWED",
		},
		{
			// CRITERION 4. A reviewer changing its own mind must remain possible, and it is the
			// ONLY thing that clears a refusal. `qa` refuses, then `qa` approves.
			name:     "a reviewer may withdraw its own refusal",
			kind:     "TEST",
			comments: []string{qaRefusal, posted("qa", "qa", f.head, "approve")},
			wantRC:   0,
			want:     "review ok",
		},
		{
			// CRITERION 4's other half, stated as behaviour rather than left implicit: a SECOND
			// independent reviewer cannot vote away the first one's refusal either. The Issue
			// records this as the pre-existing half of the defect and calls it less alarming; it is
			// the same act, and a human resolving a disagreement can do so by having the refuser
			// withdraw.
			name:     "a second independent reviewer cannot vote away the first one's refusal",
			kind:     "TEST",
			comments: []string{qaRefusal, posted("product", "product", f.head, "approve")},
			wantRC:   2,
			want:     "requests changes",
		},
		{
			// AND THE ORDER MUST NOT MATTER. An approve followed by a refusal already refused under
			// the old gate — for the wrong reason, because `last` happened to point at the refusal.
			// It must still refuse now that `last` is not what decides.
			name:     "a refusal posted after an approve still refuses",
			kind:     "TEST",
			comments: []string{posted("qa", "qa", f.head, "approve"), qaRefusal},
			wantRC:   2,
			want:     "requests changes",
		},
		{
			// THE REFUSAL MUST BE ATTRIBUTED. "changes were requested" without saying by whom sends
			// the author looking through every comment on the pull request.
			name:     "the refusal names who refused",
			kind:     "TEST",
			comments: []string{qaRefusal, selfApprove},
			wantRC:   2,
			want:     "qa",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, out := f.checkOut(t, tc.comments...)
			if rc != tc.wantRC {
				t.Errorf("[%s] check-review.sh exited %d, want %d", tc.kind, rc, tc.wantRC)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("[%s] the gate said %q, which does not carry %q",
					tc.kind, strings.TrimSpace(out), tc.want)
			}
			if tc.notWant != "" && strings.Contains(out, tc.notWant) {
				t.Errorf("[%s] the gate said %q, which carries %q — an independent refusal was "+
					"erased and the result is indistinguishable from there never having been one",
					tc.kind, strings.TrimSpace(out), tc.notWant)
			}
		})
	}

	// CRITERION 3. A REFUSED BRANCH IS FIXED BY FIXING IT, NEVER TRAPPED. A push makes a new head,
	// and a verdict is bound to the head it names — so the refusal above does not follow the code
	// that answered it. Without this, criterion 1's fix makes every refused pull request
	// permanently unmergeable, which is a worse failure than the one being fixed.
	t.Run("a refusal does not carry over to a new head", func(t *testing.T) {
		g := newReviewFixture(t).selfAllowed(t)
		oldHead := g.head

		run := exec.Command("git", "commit", "-q", "--allow-empty", "-m", "fix(x): answer the review\n\nAgent: dev")
		run.Dir = g.dir
		run.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("cannot push a new head onto the fixture: %v\n%s", err, out)
		}
		rev := exec.Command("git", "rev-parse", "HEAD")
		rev.Dir = g.dir
		newHeadRaw, err := rev.Output()
		if err != nil {
			t.Fatalf("cannot read the new head: %v", err)
		}
		g.head = strings.TrimSpace(string(newHeadRaw))
		if g.head == oldHead {
			t.Fatalf("the fixture did not actually advance: head is still %s", oldHead)
		}

		// The refusal names the OLD head; the approve names the new one.
		rc, out := g.checkOut(t,
			posted("qa", "qa", oldHead, "changes-requested"),
			posted("qa", "qa", g.head, "approve"),
		)
		if rc != 0 {
			t.Errorf("check-review.sh exited %d, want 0 — a refusal of a PREVIOUS head followed the "+
				"code that answered it, so a refused branch can never be fixed\n%s", rc, out)
		}
	})
}

// ghostSha returns a 40-hex string that SHARES ITS FIRST EIGHT CHARACTERS with real and names no
// object in the fixture. The prefix collision is the point and not decoration: on #38 the posted
// sha and the head agreed for eight hex characters, which is not chance, and every human and agent
// that skim-read them side by side — including the reviewer that filed the Issue — read them as
// equal. A fixture whose two shas differ obviously would not represent the case that bit.
func ghostSha(t *testing.T, dir, real string) string {
	t.Helper()
	if len(real) != 40 {
		t.Fatalf("the fixture head %q is not a 40-character sha", real)
	}
	rot := strings.NewReplacer(
		"0", "5", "1", "6", "2", "7", "3", "8", "4", "9",
		"5", "0", "6", "1", "7", "2", "8", "3", "9", "4",
		"a", "f", "b", "e", "c", "d", "d", "c", "e", "b", "f", "a")
	ghost := real[:8] + rot.Replace(real[8:])
	if ghost == real {
		t.Fatalf("the ghost sha is identical to the head %q, so the fixture proves nothing", real)
	}
	if ghost[:8] != real[:8] {
		t.Fatalf("the ghost sha %q does not share the head's eight-character prefix", ghost)
	}

	// THE ISSUE'S OWN CONTROL, RUN AS A CONTROL. `git cat-file` must fail on the ghost and succeed
	// on the head. If either half does not hold, the fixture is not the case under test and this
	// SKIPS WITH A REASON rather than passing — a 40-hex string colliding with a real object is
	// vanishingly unlikely, but "vanishingly unlikely" is not "checked".
	resolves := func(sha string) bool {
		cmd := exec.Command("git", "cat-file", "-e", sha+"^{commit}")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		return cmd.Run() == nil
	}
	if !resolves(real) {
		t.Skipf("the fixture head %s does not resolve in %s, so nothing here could be determined "+
			"and this test is NOT passing", real, dir)
	}
	if resolves(ghost) {
		t.Skipf("the constructed ghost sha %s unexpectedly resolves, so this fixture is not the "+
			"case under test — nothing was determined and this test is NOT passing", ghost)
	}
	return ghost
}

// TestAVerdictNamingAnUnknownShaIsReported is Issue #84, the third defect in this gate and the
// quietest. A verdict whose `Reviewed-sha:` names no git object was discarded in silence: the gate
// correctly declined to apply it, and then told nobody. On #38 a role's UAT refusal named
// `e7e1368a7f…` while the head was `e7e1368a36…`; the gate ran 28 seconds later and published
// `success`, so the board read green while a role believed it had blocked the pull request.
//
// THE EXACT-SHA MATCHING IS CORRECT AND IS NOT TOUCHED. It is what makes a verdict stale when
// somebody pushes, and loosening it would let a stale review certify new code. Two cases are
// distinguished instead:
//
//	a sha this repository KNOWS but is not the head -> an ordinary stale review, silent, unchanged
//	a sha naming NO OBJECT AT ALL                   -> nothing was ever reviewed there; reported
func TestAVerdictNamingAnUnknownShaIsReported(t *testing.T) {
	f := newReviewFixture(t)
	ghost := ghostSha(t, f.dir, f.head)
	genuine := posted("qa", "qa", f.head, "approve")

	cases := []struct {
		name     string
		comments []string
		wantRC   int
		want     []string
		notWant  []string
	}{
		{
			// THE #38 SHAPE. A refusal that cannot be placed, and a genuine approve that can. This
			// is the configuration that published `success` while a refusal lay unread.
			name:     "an unplaceable refusal is not swallowed by a genuine approve",
			comments: []string{posted("product", "product", ghost, "changes-requested"), genuine},
			wantRC:   4,
			want:     []string{ghost, f.head, "COULD NOT BE PLACED"},
		},
		{
			// CASE 1, AND IT MUST STAY SILENT. The base commit is a sha this repository knows and
			// is not the head — an ordinary stale review, which a push is expected to produce.
			// Reporting these would make every pushed-to branch noisy and teach everyone to ignore
			// the message that matters.
			name:     "a verdict naming a known commit that is not the head stays silent",
			comments: []string{posted("product", "product", f.base, "changes-requested"), genuine},
			wantRC:   0,
			want:     []string{"review ok"},
			notWant:  []string{"COULD NOT BE PLACED"},
		},
		{
			// A QUOTED unplaceable verdict is not a verdict, so it is not an unplaceable one
			// either. #65 strips fences before parsing and that must hold here too, or every
			// postmortem quoting a bad sha turns a pull request red.
			name: "a fenced quotation naming an unknown sha is not reported",
			comments: []string{
				"[product]\nfor the record this is what went wrong:\n" +
					fenced("```", review("product", ghost, "changes-requested")),
				genuine,
			},
			wantRC:  0,
			want:    []string{"review ok"},
			notWant: []string{"COULD NOT BE PLACED"},
		},
		{
			// WITH NO OTHER VERDICT AT ALL, the unplaceable one is still the more actionable fact.
			// "no review exists" would be true and would send the reader looking for a missing
			// comment that is in fact sitting right there, naming a sha nobody can find.
			name:     "an unplaceable verdict is reported even when no other review exists",
			comments: []string{posted("product", "product", ghost, "changes-requested")},
			wantRC:   4,
			want:     []string{ghost, "COULD NOT BE PLACED"},
		},
		{
			// AN UNPLACEABLE APPROVE COUNTS TOO. The Issue is about a refusal because that is what
			// it cost, but the defect is the silence and it does not know what the verdict said.
			name:     "an unplaceable approve is reported as well",
			comments: []string{posted("product", "product", ghost, "approve")},
			wantRC:   4,
			want:     []string{ghost, "COULD NOT BE PLACED"},
		},
		{
			// PRECEDENCE, PINNED. A landed refusal outranks an unplaceable verdict: it is concrete,
			// it is already red, and it already tells the author what to do. The unplaceable notice
			// still prints, so nothing is lost by its not owning the exit code — and this arm is
			// here so that ordering is a decision rather than a consequence of line order.
			name: "a landed refusal outranks an unplaceable verdict, and both are said",
			comments: []string{
				posted("product", "product", ghost, "approve"),
				posted("qa", "qa", f.head, "changes-requested"),
			},
			wantRC: 2,
			want:   []string{"COULD NOT BE PLACED", "requests changes"},
		},
		{
			// AND THE ORDINARY PATH IS UNTOUCHED. A fix that reports something on every pull
			// request satisfies every arm above and makes the gate useless.
			name:     "a clean independent approve is unaffected",
			comments: []string{genuine},
			wantRC:   0,
			want:     []string{"review ok"},
			notWant:  []string{"COULD NOT BE PLACED"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, out := f.checkOut(t, tc.comments...)
			if rc != tc.wantRC {
				t.Errorf("check-review.sh exited %d, want %d", rc, tc.wantRC)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("the gate said %q, which does not carry %q", strings.TrimSpace(out), w)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(out, w) {
					t.Errorf("the gate said %q, which carries %q and should not",
						strings.TrimSpace(out), w)
				}
			}
		})
	}

	// A SHALLOW CLONE CANNOT ANSWER THIS, AND MUST SAY SO RATHER THAN ACCUSE. `git cat-file` fails
	// on an object that exists but was never fetched, so in a shallow checkout "unplaceable" and
	// "not downloaded" are the same observation — and reporting the first would be a false alarm of
	// exactly the shape this Issue is about, pointed the other way. The review job checks out with
	// fetch-depth: 0 today, but that is a framework-owned workflow file the installer replaces, so
	// the script probes rather than trusting it.
	t.Run("a shallow clone reports that it could not determine, and does not accuse", func(t *testing.T) {
		needTool(t, "git")
		dst := filepath.Join(t.TempDir(), "shallow")
		// Depth 2 so the base commit is present — the gate refuses earlier without it, and this
		// arm is about the sha scan and not about the checkout guard.
		clone := exec.Command("git", "clone", "--quiet", "--depth", "2", "file://"+f.dir, dst)
		clone.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := clone.CombinedOutput(); err != nil {
			t.Skipf("could not make a shallow clone here, so nothing was determined and this is "+
				"NOT passing: %v\n%s", err, out)
		}
		isShallow := exec.Command("git", "rev-parse", "--is-shallow-repository")
		isShallow.Dir = dst
		if out, err := isShallow.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
			t.Skipf("the clone did not come out shallow (%q, %v), so this arm is not the case "+
				"under test and is NOT passing", strings.TrimSpace(string(out)), err)
		}

		g := reviewFixture{dir: dst, head: f.head, base: f.base}
		rc, out := g.checkOut(t, posted("product", "product", ghost, "changes-requested"), genuine)
		if rc == 4 {
			t.Errorf("check-review.sh exited 4 in a SHALLOW clone — it cannot tell an unknown "+
				"object from an unfetched one there, so this is an accusation it has no basis "+
				"for\n%s", out)
		}
		if !strings.Contains(out, "COULD NOT BE DETERMINED") {
			t.Errorf("a shallow clone published %q, which does not say the question could not be "+
				"answered — silence here reads as 'every verdict is fine'", strings.TrimSpace(out))
		}
	})
}

// TestPublishDistinguishesTheUnplaceableVerdict is Issue #84 at the wiring. The exit code the
// checker goes to the trouble of splitting reaches nobody unless the publish step renders it, and
// this repository has already shipped one defect of exactly that shape — #52, where a landed
// refusal and an absent review shared a sentence.
func TestPublishDistinguishesTheUnplaceableVerdict(t *testing.T) {
	needTool(t, "bash")
	dir := t.TempDir()

	state, four := publish(t, dir, "success", "failure", "4")
	if state != "failure" {
		t.Errorf("rc=4 publishes state %q, want failure — a verdict nobody can place must not pass", state)
	}
	if strings.Contains(four, absentPhrase) {
		t.Errorf("rc=4 publishes %q, which claims no review exists. One does; it names a sha this "+
			"repository cannot find, and that is what the reader has to be told", four)
	}
	if strings.Contains(four, refusedPhrase) {
		t.Errorf("rc=4 publishes %q, which says changes were requested — that is a different fact", four)
	}
	for _, other := range []string{"1", "2"} {
		if _, d := publish(t, dir, "success", "failure", other); d == four {
			t.Errorf("rc=%s and rc=4 both publish %q", other, d)
		}
	}
}
