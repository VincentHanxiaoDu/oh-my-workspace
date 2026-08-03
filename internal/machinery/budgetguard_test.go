package machinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A SECONDARY RATE LIMIT IS INVISIBLE IN THE PRIMARY QUOTA, AND THE GUARD READ THE PRIMARY QUOTA
// (Issue #81).
//
// `.workflow/bin/gh-budget.sh` answered the question "is there budget?" with
// `.resources.core.remaining`. That is the PRIMARY hourly allowance. Every 403 measured during the
// outage was a SECONDARY limit — GitHub's burst/concurrency throttle — which `GET /rate_limit` does
// not report at all. Measured live, while `queue.sh` and every sweep were returning HTTP 403
// `API rate limit exceeded`:
//
//	core     4896/5000  resets in 25m
//	graphql  4964/5000  resets in 41m
//
// Both essentially untouched. So the reserve was never reached, `HOLDING` never fired, and the
// watches polled straight through the event they were built to stand down for — each retry renewing
// the burst that caused it. **It failed in the trusting direction**: the guard said `4841`, exit 0,
// while nothing could be called at all.
//
// The fix is that the 403 itself is the signal, because it is the only place a secondary limit is
// ever visible: a watch whose poll is refused hands the output to `gh-budget.sh note-failure`, and
// every later `check` holds until the throttle is expected to have lifted. It lifts with QUIET
// rather than on the reset clock, so `hold-for` answers with the secondary's own cooldown and not
// with `reset-in`.
//
// THESE TESTS EXECUTE THE INSTALLED SCRIPTS. `.workflow/bin/` is replaced wholesale by the next
// `install.sh` run — #52 was fixed there and #58 deleted it — so a copy of the logic here would go
// green while the shipped guard was blind again. Every external dependency is PROBED, never named
// and assumed; one that is missing skips with a reason saying the check did not run and is NOT
// passing.

// budgetScript locates the installed budget guard and probes that it can be driven here.
func budgetScript(t *testing.T) string {
	t.Helper()
	needTool(t, "bash")
	path := filepath.Join(repoRoot(t), ".workflow", "bin", "gh-budget.sh")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s is not present (%v), so nothing about the "+
			"budget guard was checked. This is not evidence that the guard can see a secondary "+
			"rate limit.", path, err)
	}
	return path
}

func watchScript(t *testing.T, name string) string {
	t.Helper()
	needTool(t, "bash")
	path := filepath.Join(repoRoot(t), ".workflow", "bin", name)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s is not present (%v), so nothing about what the "+
			"watch does under a refused poll was checked.", path, err)
	}
	return path
}

// THE MEASURED OUTAGE, AS A STUB. `/rate_limit` reports a nearly-full primary quota — these are the
// numbers observed on the day — while the calls the watch actually makes come back 403. A guard
// that reads only this stub sees perfect health, which is the entire defect.
const healthyPrimaryStub = `#!/usr/bin/env bash
# Whatever is asked of it, the rate limit reads healthy. That is the point.
echo "4896 $(( $(date +%s) + 1500 ))"
`

// The body GitHub sends, and `gh` prints, when a secondary limit is in force.
const secondaryRefusal = "gh: You have exceeded a secondary rate limit and have been temporarily " +
	"blocked from content creation. Please retry your request again later. (HTTP 403)"

// A refusal that is NOT a throttle. Quiet does not fix a dial timeout, so it must stay a
// `LOOKUP FAILED` and must not put the watch to sleep.
const networkOutage = "gh: dial tcp 140.82.116.6:443: operation timed out"

// budgetFixture runs the installed guard with an isolated state directory, so nothing here reads or
// writes the state a real watch on this machine is using.
type budgetFixture struct {
	dir   string
	state string
}

func newBudgetFixture(t *testing.T) *budgetFixture {
	t.Helper()
	f := &budgetFixture{dir: t.TempDir(), state: t.TempDir()}
	if err := os.WriteFile(filepath.Join(f.dir, "gh"), []byte(healthyPrimaryStub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	return f
}

func (f *budgetFixture) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{budgetScript(t)}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+f.dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ADF_BUDGET_STATE_DIR="+f.state,
	)
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gh-budget.sh could not be run at all: %v\n%s", err, out)
		}
		rc = ee.ExitCode()
	}
	return string(out), rc
}

// TestTheBudgetFixtureIsWellFormed. A red produced by a broken stub proves nothing, and most arms
// below assert a red. This one asserts the healthy shape: with a full primary quota and no observed
// refusal the guard passes. If this fails, the reds below are about the fixture.
func TestTheBudgetFixtureIsWellFormed(t *testing.T) {
	f := newBudgetFixture(t)
	out, rc := f.run(t, "check", "1500")
	if rc != 0 {
		t.Fatalf("4896 remaining against a reserve of 1500 exited %d, so nothing below is measuring "+
			"the secondary limit\n%s", rc, out)
	}
}

// TestASecondaryRateLimitIsNotAHealthyBudget is Issue #81 itself. The primary quota reads 4896/5000
// — the number measured while every call was being refused — and a secondary limit has been
// observed. The guard must HOLD.
//
// Exit 1 specifically: 0 is "plenty, keep polling", which is what it said through the whole outage,
// and 2 is "could not be read", which is a different and also-wrong answer here.
func TestASecondaryRateLimitIsNotAHealthyBudget(t *testing.T) {
	f := newBudgetFixture(t)
	if out, rc := f.run(t, "note-failure", secondaryRefusal); rc != 0 {
		t.Fatalf("a 403 naming a secondary rate limit was not recognised as one (exit %d): %s", rc, out)
	}
	out, rc := f.run(t, "check", "1500")
	if rc == 0 {
		t.Errorf("GitHub is refusing every call with a secondary rate limit and the guard reported a "+
			"healthy budget (exit 0). The primary counter it watches never moves during a secondary "+
			"throttle, so the reserve is never reached and HOLDING never fires when it is most "+
			"needed.\n%s", out)
	}
	if rc != 1 {
		t.Errorf("a live secondary rate limit exited %d; the watches stand down on exit 1, so any "+
			"other code keeps them polling through it\n%s", rc, out)
	}
	// AC1: it must NAME the throttle, and say when it retries. A hold whose reason is not stated is
	// one a role cannot tell from primary exhaustion, and one it cannot plan around.
	low := strings.ToLower(out)
	for _, want := range []string{"secondary", "burst"} {
		if !strings.Contains(low, want) {
			t.Errorf("the hold does not contain %q, so it does not name itself as a burst throttle\n%s",
				want, out)
		}
	}
	if !strings.Contains(low, "standing down for") {
		t.Errorf("the hold does not say when it will retry\n%s", out)
	}
}

// TestAPrimaryExhaustionStillHolds. AC3, and the property this change is one careless edit away from
// breaking: the reserve that landed in today's refresh must go on firing. A fix that only handles
// the secondary case regresses what was just built.
func TestAPrimaryExhaustionStillHolds(t *testing.T) {
	f := newBudgetFixture(t)
	stub := "#!/usr/bin/env bash\necho \"900 $(( $(date +%s) + 300 ))\"\n"
	if err := os.WriteFile(filepath.Join(f.dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	out, rc := f.run(t, "check", "1500")
	if rc != 1 {
		t.Errorf("900 remaining against a reserve of 1500 exited %d, not 1 — the budget reserve no "+
			"longer stops a watch\n%s", rc, out)
	}
	if !strings.Contains(out, "resets in") {
		t.Errorf("being below the reserve did not say when it recovers — a stop with no recovery time "+
			"is one a role cannot plan around\n%s", out)
	}
}

// TestAnUnreadableLimitIsNeither. The third state, unchanged: exit 2, distinct from both "plenty"
// and "exhausted". `could not determine` and `determined to be nothing` must never share an exit
// code, and the secondary check runs BEFORE the primary read, so this is the arm that catches a
// widening of it.
func TestAnUnreadableLimitIsNeither(t *testing.T) {
	f := newBudgetFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, "gh"), []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	out, rc := f.run(t, "check", "1500")
	if rc != 2 {
		t.Errorf("an unreadable rate limit exited %d, not 2 — it must not share an answer with "+
			"'plenty' or with 'exhausted'\n%s", rc, out)
	}
}

// TestAHealthyPrimaryQuotaDoesNotClaimTheSecondaryOne is AC4. Reading `/rate_limit` cannot answer
// whether a secondary limit is in force, and nothing else can either without spending a call. What
// the guard knows is that none has been OBSERVED, which is a weaker claim than "there is none" — so
// the answer must say so rather than print a bare number that is true about a different limit.
func TestAHealthyPrimaryQuotaDoesNotClaimTheSecondaryOne(t *testing.T) {
	f := newBudgetFixture(t)
	out, rc := f.run(t, "check", "1500")
	if rc != 0 {
		t.Fatalf("the healthy case exited %d\n%s", rc, out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "primary") {
		t.Errorf("the healthy answer does not say the number is about the PRIMARY quota. A bare "+
			"number reads as 'calls will be answered', which is the sentence the outage disproved.\n%s",
			out)
	}
	if !strings.Contains(low, "determined") {
		t.Errorf("the healthy answer does not say the secondary limit could not be determined. An "+
			"undetermined answer must not wear a determined face.\n%s", out)
	}
}

// TestANetworkOutageIsNotRecordedAsAThrottle. AC2, from the other side: `HOLDING` and
// `LOOKUP FAILED` are different states because the responses to them differ. Quiet lifts a burst
// limit; quiet does nothing at all for a dial timeout, and parking every watch for two minutes on
// one would hide a real outage behind a wrong diagnosis.
func TestANetworkOutageIsNotRecordedAsAThrottle(t *testing.T) {
	f := newBudgetFixture(t)
	if out, rc := f.run(t, "note-failure", networkOutage); rc == 0 {
		t.Fatalf("a dial timeout was recorded as a secondary rate limit\n%s", out)
	}
	if out, rc := f.run(t, "check", "1500"); rc != 0 {
		t.Errorf("after a dial timeout the guard held (exit %d) — an outage was spelled the same way "+
			"as a throttle\n%s", rc, out)
	}
}

// TestTheHoldWaitsOnTheSecondaryCooldownNotThePrimaryReset. A secondary limit clears with quiet, so
// the reset clock is the wrong signal to wait on: the stub's primary quota resets in 1500 seconds,
// which is neither the right number nor a number this watch should ever sleep for.
func TestTheHoldWaitsOnTheSecondaryCooldownNotThePrimaryReset(t *testing.T) {
	f := newBudgetFixture(t)
	if _, rc := f.run(t, "note-failure", secondaryRefusal); rc != 0 {
		t.Fatalf("the fixture could not record a secondary rate limit")
	}
	out, rc := f.run(t, "hold-for", "60")
	if rc != 0 {
		t.Fatalf("hold-for exited %d\n%s", rc, out)
	}
	got := strings.TrimSpace(out)
	if got == "1500" {
		t.Errorf("hold-for answered %q, which is the PRIMARY reset clock. A secondary limit has "+
			"nothing to do with it.", got)
	}
	if got == "" || got == "0" {
		t.Errorf("hold-for answered %q under a live secondary limit — the watch would poll straight "+
			"back into the throttle", got)
	}
}

// TestARetryAfterIsObeyed. When GitHub says how long to be quiet, that is the number, not the
// default cooldown — the default is only what is used when it did not say.
func TestARetryAfterIsObeyed(t *testing.T) {
	f := newBudgetFixture(t)
	out, rc := f.run(t, "note-failure",
		"HTTP 403: You have exceeded a secondary rate limit\nRetry-After: 47\n")
	if rc != 0 {
		t.Fatalf("a 403 with a Retry-After was not recognised as a rate limit\n%s", out)
	}
	if got := strings.TrimSpace(out); got != "47" {
		t.Errorf("the refusal carried `Retry-After: 47` and the guard chose %q instead — GitHub said "+
			"exactly how long to be quiet and it was discarded", got)
	}
}

// runWatchBriefly starts a copy of an installed watch against stub dependencies, lets it complete a
// poll or two, and returns everything it printed. The watch never exits on its own; that is what it
// is for.
func runWatchBriefly(t *testing.T, dir, script string, args ...string) string {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(dir, script)}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ADF_BUDGET_STATE_DIR="+filepath.Join(dir, "state"),
		// A HOLD SHORT ENOUGH TO OBSERVE. The cooldown a real watch takes is minutes, which is the
		// right number there and an untestable one here.
		"ADF_SECONDARY_COOLDOWN=1",
		"REPO=x/y",
	)
	// TO A FILE, NOT A PIPE. `cmd.Wait` on a pipe blocks until every writer closes it, and killing
	// the watch leaves its `sleep` child holding the far end — the first version of this helper took
	// the watch's full hold to return, which is a test that hangs for exactly as long as the bug it
	// is measuring lasts.
	logPath := filepath.Join(dir, "watch.out")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("cannot open the watch log: %v", err)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start %s: %v", script, err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("cannot read the watch log: %v", err)
	}
	return string(raw)
}

// watchFixture assembles a directory holding the INSTALLED watch, the INSTALLED guard and their
// siblings, plus stubs for everything that would otherwise reach GitHub.
func watchFixture(t *testing.T, watch string, queueOutput string, queueExit int) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{watch, "gh-budget.sh", "pr-authors.sh"} {
		src := filepath.Join(repoRoot(t), ".workflow", "bin", name)
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s could not be read (%v), so what the watch "+
				"does under a refused poll was not checked.", src, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o755); err != nil {
			t.Fatalf("cannot stage %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(healthyPrimaryStub), 0o755); err != nil {
		t.Fatalf("cannot write the gh stub: %v", err)
	}
	// The queue refuses, the way the real one did. `watch-queue.sh` runs `queue.sh` from its own
	// directory, so this is the whole world it sees.
	stub := "#!/usr/bin/env bash\ncat <<'BODY' >&2\n" + queueOutput + "\nBODY\nexit " +
		string(rune('0'+queueExit)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "queue.sh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("cannot write the queue stub: %v", err)
	}
	return dir
}

// TestTheWatchHoldsOnASecondaryRateLimit is AC1 driven end to end through the installed watch. The
// budget guard sees a healthy primary quota — it always did — and the poll is refused with a 403.
// The watch must report `HOLDING`, name the burst throttle, and say when it retries. Under the
// defect it reported `LOOKUP FAILED` and polled again on the interval, which renews the burst that
// caused it.
func TestTheWatchHoldsOnASecondaryRateLimit(t *testing.T) {
	watchScript(t, "watch-queue.sh")
	dir := watchFixture(t, "watch-queue.sh", secondaryRefusal, 1)
	out := runWatchBriefly(t, dir, "watch-queue.sh", "dev", "1")
	if !strings.Contains(out, "HOLDING") {
		t.Errorf("a poll refused by a secondary rate limit did not put the watch into HOLDING. This "+
			"is the state that never fired when it was most needed, because the guard was watching a "+
			"counter that does not move during a secondary throttle.\n%s", out)
	}
	low := strings.ToLower(out)
	for _, want := range []string{"secondary", "burst"} {
		if !strings.Contains(low, want) {
			t.Errorf("the watch held without naming the throttle (%q missing), so a role cannot tell "+
				"it from primary exhaustion\n%s", want, out)
		}
	}
	if !strings.Contains(low, "standing down for") {
		t.Errorf("the watch held without saying when it will retry\n%s", out)
	}
	// A HOLD IS NOT A DEATH. Two of them prove the watch resumed rather than stopping on the first
	// refusal — a watch that stops looks exactly like a quiet board, which is the state this file's
	// siblings exist to remove.
	if n := strings.Count(out, "HOLDING"); n < 2 {
		t.Errorf("the watch held %d time(s) and did not come back. A throttle is transient and must "+
			"wake the role when it lifts, not end the watch.\n%s", n, out)
	}
}

// TestTheWatchStillReportsANonThrottleOutageAsLookupFailed is AC2. Three states, three renderings:
// a dial timeout is not a throttle and must not be dressed as one, and neither is reported as a
// quiet board. A fix that routed every failed poll into HOLDING would pass the arm above and hide
// every real outage behind a wrong diagnosis.
func TestTheWatchStillReportsANonThrottleOutageAsLookupFailed(t *testing.T) {
	watchScript(t, "watch-queue.sh")
	dir := watchFixture(t, "watch-queue.sh", networkOutage, 1)
	out := runWatchBriefly(t, dir, "watch-queue.sh", "dev", "1")
	if !strings.Contains(out, "LOOKUP FAILED") {
		t.Errorf("a dial timeout did not emit LOOKUP FAILED — an outage that emits nothing is "+
			"indistinguishable from an empty queue, which is the whole reason this watch exists\n%s",
			out)
	}
	if strings.Contains(out, "HOLDING") {
		t.Errorf("a dial timeout put the watch into HOLDING. Quiet lifts a burst limit and does "+
			"nothing at all for a timeout, so the watch would sit still for an outage it should be "+
			"shouting about.\n%s", out)
	}
}

// TestTheInstalledSelfTestCoversTheSecondaryCase. AC5. The shipped self-test drove 4900-vs-1500 and
// 900-vs-1500, both PRIMARY-only, which is exactly why a guard blind to secondary limits passed it
// and shipped. Running it here means a refresh that restores the primary-only self-test is caught by
// this suite as well as by the arms above.
func TestTheInstalledSelfTestCoversTheSecondaryCase(t *testing.T) {
	script := budgetScript(t)
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Skipf("COULD NOT DETERMINE, NOT PASSING: %s could not be read (%v).", script, err)
	}
	if !strings.Contains(strings.ToLower(string(raw)), "secondary rate limit") {
		t.Errorf("%s never mentions a secondary rate limit, so its self-test cannot be driving one. "+
			"The previous self-test passed a guard that was blind to the only limit that was ever "+
			"hit.", script)
	}
	cmd := exec.Command("bash", script, "--self-test")
	cmd.Env = append(os.Environ(), "ADF_BUDGET_STATE_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("the installed self-test failed: %v\n%s", err, out)
	}
}
