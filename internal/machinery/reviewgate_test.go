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
			name:     "a refusal naming another sha reads as no current review",
			comments: []string{posted("product", "product", strings.Repeat("0", 40), "changes-requested")},
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
