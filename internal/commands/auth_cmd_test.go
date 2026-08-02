package commands

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/auth"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// countingHub wraps a real [auth.Authority] and counts what the CLI asked it to do.
//
// WHAT THIS FAKES AND WHAT IT DOES NOT, because a reviewer must not have to guess: it fakes the
// WIRE and nothing else. Every call goes straight through to a real Authority — real device codes,
// real minting, real expiry, real revocation, and a real [hub.EvaluateGrantRequest] decision. The
// counters exist so that criteria 1, 2 and 26 can assert on what was NOT asked, which is the only
// way to prove a command did not sign anybody in.
type countingHub struct {
	inner auth.Hub
	mu    sync.Mutex
	calls map[string]int
}

func newCountingHub(inner auth.Hub) *countingHub {
	return &countingHub{inner: inner, calls: map[string]int{}}
}

func (c *countingHub) count(name string) {
	c.mu.Lock()
	c.calls[name]++
	c.mu.Unlock()
}

func (c *countingHub) n(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[name]
}

func (c *countingHub) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := 0
	for _, v := range c.calls {
		t += v
	}
	return t
}

func (c *countingHub) StartSignIn(r auth.SignInRequest) (auth.DeviceAuthorization, error) {
	c.count("StartSignIn")
	return c.inner.StartSignIn(r)
}
func (c *countingHub) Redeem(d auth.DeviceAuthorization) (auth.Issued, error) {
	c.count("Redeem")
	return c.inner.Redeem(d)
}
func (c *countingHub) Authenticate(s auth.Secret) (hub.Grant, error) {
	c.count("Authenticate")
	return c.inner.Authenticate(s)
}
func (c *countingHub) Sessions(p hub.PersonID) ([]auth.SessionView, error) {
	c.count("Sessions")
	return c.inner.Sessions(p)
}
func (c *countingHub) Revoke(p hub.PersonID, id auth.TokenID) error {
	c.count("Revoke")
	return c.inner.Revoke(p, id)
}

// authFixture is a store, a running daemon, a configured hub and a real authority behind the seam.
type authFixture struct {
	root      string
	getenv    func(string) string
	authority *auth.Authority
	hub       *countingHub
	clock     *authClock
}

type authClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *authClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *authClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newAuthFixture starts a REAL daemon against a real store, because criteria 25 and 29 are about
// the daemon and the control API and a stub of either would prove only the rendering.
//
// It skips — loudly and with the reason — if the control API cannot open in this environment,
// exactly as TestHealthIsUnaffectedByTheControlAPIDeclining does. A silent skip is how four
// criteria were once reported as passing while nothing ran.
func newAuthFixture(t *testing.T, configureHub bool) *authFixture {
	t.Helper()
	root, base := daemonTestStore(t)

	d, err := daemon.Start(daemon.Options{
		StorePath: root, Interval: 5 * time.Millisecond, Write: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("the daemon did not start: %v", err)
	}
	t.Cleanup(d.Close)
	if d.Control() == nil {
		_, why := d.ControlState()
		t.Skipf("the control API did not open in this environment, so the auth surfaces that "+
			"depend on it cannot be driven here: %s", why)
	}

	c := &authClock{t: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)}
	a := auth.NewAuthority(auth.AuthorityOptions{
		Now: c.now, CodeLife: 10 * time.Minute, TokenLife: 24 * time.Hour,
		VerificationURI: "https://hub.example/device",
	})
	a.Register("alice", []hub.Scope{hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish})
	a.Register("bob", []hub.Scope{hub.ScopeRead})

	ch := newCountingHub(auth.Direct(a))
	swapAuthHub(t, func(cli.Env) auth.Hub { return ch })

	getenv := func(k string) string {
		if k == auth.HubEnv {
			if configureHub {
				return "https://hub.example"
			}
			return ""
		}
		return base(k)
	}
	return &authFixture{root: root, getenv: getenv, authority: a, hub: ch, clock: c}
}

// swapAuthHub replaces the hub seam for the duration of a test.
func swapAuthHub(t *testing.T, f func(cli.Env) auth.Hub) {
	t.Helper()
	prev := authHub
	authHub = f
	t.Cleanup(func() { authHub = prev })
}

func runAuthCmd(t *testing.T, getenv func(string) string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(append([]string{"auth"}, args...), &out, &errb, getenv)
	return code, out.String(), errb.String()
}

// runAuthCmdWithin drives a command and FAILS rather than hanging if it does not return.
//
// A command that never returns passes every assertion a test never reaches. Any test whose
// subject could plausibly wait — anything touching the sign-in poll — uses this.
func runAuthCmdWithin(t *testing.T, d time.Duration, getenv func(string) string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb lockedBuffer
	done := make(chan int, 1)
	go func() { done <- cli.Run(append([]string{"auth"}, args...), &out, &errb, getenv) }()
	select {
	case code := <-done:
		return code, out.String(), errb.String()
	case <-time.After(d):
		t.Fatalf("`omw auth %s` did not return within %s. It is supposed to refuse before it waits "+
			"for anything.\n%s%s", strings.Join(args, " "), d, out.String(), errb.String())
		return 0, "", ""
	}
}

func credentialExists(root string) bool {
	_, err := os.Stat(auth.CredentialPath(root))
	return err == nil
}

// completeSignIn drives the whole device-code flow through the CLI: it runs `omw auth sign-in` in
// the background, waits for the code to be printed, approves it as a person would in a browser,
// and returns what the command printed.
//
// THE APPROVAL IS A SEPARATE ACTOR ON PURPOSE (criterion 4 and §4.2). Nothing the command does
// approves anything; the sign-in completes only because something outside it acted.
func completeSignIn(t *testing.T, f *authFixture, as hub.PersonID, args ...string) (int, string, string) {
	t.Helper()
	prev := authPollInterval
	authPollInterval = time.Millisecond
	t.Cleanup(func() { authPollInterval = prev })

	var out, errb lockedBuffer
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(append([]string{"auth", "sign-in"}, args...), &out, &errb, f.getenv)
	}()

	code := waitForCode(t, &out)
	if code == "" {
		select {
		case rc := <-done:
			return rc, out.String(), errb.String()
		case <-time.After(2 * time.Second):
			t.Fatal("sign-in neither printed a code nor returned")
		}
	}
	// CRITERION 5, DRIVEN AT THE EXACT MOMENT IT IS ABOUT: the code has been printed and nobody has
	// completed the browser step.
	if credentialExists(f.root) {
		t.Fatal("a credential exists on disk between the code being printed and anybody completing " +
			"the browser step; criterion 5 says issuing a device code creates no token")
	}
	select {
	case <-done:
		t.Fatal("sign-in returned before anybody approved it")
	default:
	}

	if err := f.authority.Approve(code, as); err != nil {
		// Not fatal: some tests approve deliberately-refused requests and want the CLI's answer.
		t.Logf("approval answered: %v", err)
	}
	select {
	case rc := <-done:
		return rc, out.String(), errb.String()
	case <-time.After(5 * time.Second):
		t.Fatal("sign-in did not finish after the browser step was completed")
	}
	return 0, "", ""
}

// waitForCode reads the printed user code out of the command's output.
func waitForCode(t *testing.T, out *lockedBuffer) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			if _, after, ok := strings.Cut(line, "code: "); ok {
				return strings.TrimSpace(after)
			}
		}
		time.Sleep(time.Millisecond)
	}
	return ""
}

// lockedBuffer is a bytes.Buffer a test can read while a command writes to it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestSignInIsTheOnlyThingThatSignsAnybodyIn is criteria 2 and 1, driven.
//
// It exercises EVERY other `auth` surface with no credential on disk and asserts that afterwards
// there is still no credential and no sign-in was ever started at the hub. The counter is what
// makes this a real assertion rather than a hopeful one.
func TestSignInIsTheOnlyThingThatSignsAnybodyIn(t *testing.T) {
	f := newAuthFixture(t, true)

	surfaces := [][]string{
		{"status"}, {"scopes"}, {"sessions"}, {"revoke", "some-token-id"},
		{"sign-out"}, {"help"}, {},
	}
	for _, args := range surfaces {
		_, out, errOut := runAuthCmd(t, f.getenv, args...)
		if credentialExists(f.root) {
			t.Fatalf("`omw auth %s` produced a credential. Criterion 2: only the sign-in command "+
				"a person runs can do that.\n%s%s", strings.Join(args, " "), out, errOut)
		}
	}
	if n := f.hub.n("StartSignIn"); n != 0 {
		t.Errorf("%d sign-ins were started by commands that are not `sign-in`", n)
	}
	if n := f.hub.n("Redeem"); n != 0 {
		t.Errorf("%d device codes were redeemed by commands that are not `sign-in`", n)
	}
	for _, p := range []hub.PersonID{"alice", "bob"} {
		if n := len(f.authority.Sessions(p)); n != 0 {
			t.Errorf("%d hub sessions exist for %s and nobody ran `sign-in`", n, p)
		}
	}

	// CRITERION 1: a command needing hub authority names "not signed in" as the reason and does
	// not open a sign-in flow.
	code, out, errOut := runAuthCmd(t, f.getenv, "sessions")
	if code != cli.ExitFailure {
		t.Errorf("`omw auth sessions` with no credential exited %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "not signed in") {
		t.Errorf("the reason was not named as 'not signed in':\n%s", errOut)
	}
	if !strings.Contains(errOut, auth.ErrNotSignedIn.Code) {
		t.Errorf("the machine-readable code was not printed:\n%s", errOut)
	}
}

// TestADeviceCodeSignInCompletesWithoutABrowserOrAPort is criterion 4, and criteria 13 and 16 come
// out of the same run.
func TestADeviceCodeSignInCompletesWithoutABrowserOrAPort(t *testing.T) {
	f := newAuthFixture(t, true)

	code, out, errOut := completeSignIn(t, f, "alice", "--scope", "read,publish", "--label", "this laptop")
	if code != cli.Success {
		t.Fatalf("a completed device-code sign-in exited %d\n%s%s", code, out, errOut)
	}
	if !credentialExists(f.root) {
		t.Fatal("the sign-in succeeded and no credential was stored")
	}
	// CRITERION 16: the scope printed back is the scope the token has, exactly.
	if !strings.Contains(out, "scope: read, publish") {
		t.Errorf("the token's scope was not reported back as exactly what was asked for:\n%s", out)
	}
	cred, err := auth.Load(f.root)
	if err != nil {
		t.Fatalf("loading the credential the sign-in wrote: %v", err)
	}
	g, err := f.authority.Authenticate(cred.Secret)
	if err != nil {
		t.Fatalf("the stored credential does not authenticate: %v", err)
	}
	if !hub.Permits(g.Scopes, hub.ScopePublish) || !hub.Permits(g.Scopes, hub.ScopeRead) {
		t.Errorf("the token permits %v", g.Scopes)
	}
	if hub.Permits(g.Scopes, hub.ScopeWrite) {
		t.Errorf("the token permits write, which was never asked for: %v", g.Scopes)
	}
}

// TestADefaultSignInAsksForReadAlone is criterion 10: everyday use needs `read` and nothing else,
// with no further grant requested or prompted.
func TestADefaultSignInAsksForReadAlone(t *testing.T) {
	f := newAuthFixture(t, true)

	code, out, errOut := completeSignIn(t, f, "alice")
	if code != cli.Success {
		t.Fatalf("a default sign-in exited %d\n%s%s", code, out, errOut)
	}
	cred, err := auth.Load(f.root)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(cred.Scopes) != 1 || cred.Scopes[0] != hub.ScopeRead {
		t.Errorf("a sign-in with no --scope produced %v; criterion 10 says everyday use is read alone", cred.Scopes)
	}
	if strings.Contains(out, "publish") && !strings.Contains(out, "asking for scope: read\n") {
		t.Errorf("a default sign-in mentioned publish:\n%s", out)
	}
}

// TestAnOverWideRequestIsRefusedAndLeavesNoCredential is criterion 15 at the CLI.
func TestAnOverWideRequestIsRefusedAndLeavesNoCredential(t *testing.T) {
	f := newAuthFixture(t, true)

	// Bob holds read. He asks for publish.
	code, out, errOut := completeSignIn(t, f, "bob", "--scope", "publish")
	if code != cli.ExitFailure {
		t.Errorf("an over-wide request exited %d; the hub DECIDED, so it is a failure and not an "+
			"undetermined\n%s%s", code, out, errOut)
	}
	if !strings.Contains(errOut, hub.ErrGrantWiderThanHolder.Code) {
		t.Errorf("the refusal did not carry its code:\n%s", errOut)
	}
	if credentialExists(f.root) {
		t.Error("a credential exists after a refused request; criterion 15 says no token exists afterwards")
	}
	if n := len(f.authority.Sessions("bob")); n != 0 {
		t.Errorf("%d tokens exist for bob after a refused request", n)
	}
	// AND IT WAS NOT NARROWED. A `read` token quietly issued instead would be the exact defect.
	if strings.Contains(out, "signed in as") {
		t.Errorf("the refused request produced a sign-in:\n%s", out)
	}
}

// TestAnUnknownScopeIsRefusedAsUnknownAtTheCLI is criteria 8 and 31, and the operator ruling.
func TestAnUnknownScopeIsRefusedAsUnknownAtTheCLI(t *testing.T) {
	f := newAuthFixture(t, true)

	// BOUNDED, BECAUSE THE FAILURE MODE IS A HANG. This was written as a plain synchronous call
	// until a mutation that accepted unknown scope names made `sign-in` print a code and poll for
	// ever — the suite stopped rather than failing, and a hang names nothing. The refusal is
	// supposed to happen before any code is printed, so a second is generous.
	code, out, errOut := runAuthCmdWithin(t, time.Second, f.getenv, "sign-in", "--scope", "operate")
	if code != cli.ExitFailure {
		t.Errorf("asking for an unknown scope exited %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(errOut, hub.ErrUnknownScope.Code) {
		t.Errorf("an unknown scope was not refused AS UNKNOWN:\n%s", errOut)
	}
	if strings.Contains(errOut, hub.ErrGrantWiderThanHolder.Code) {
		t.Errorf("an unknown scope was refused as too wide; criterion 31 wants them told apart:\n%s", errOut)
	}
	if strings.Contains(out, "code:") {
		t.Errorf("a device code was printed for an unknown scope; the refusal is at request time:\n%s", out)
	}
	if credentialExists(f.root) {
		t.Error("a credential exists after an unknown-scope request")
	}
}

// TestScopesListsExactlyThreeAndStatesTheOperatorFact is criteria 30 and 32.
func TestScopesListsExactlyThreeAndStatesTheOperatorFact(t *testing.T) {
	f := newAuthFixture(t, false)

	code, out, errOut := runAuthCmd(t, f.getenv, "scopes")
	if code != cli.Success {
		t.Fatalf("`omw auth scopes` exited %d\n%s%s", code, out, errOut)
	}
	// The listing itself: the first lines are the vocabulary, one per line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var names []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			break
		}
		names = append(names, l)
	}
	if len(names) != 3 {
		t.Errorf("the vocabulary listing has %d names: %v", len(names), names)
	}
	for _, want := range []string{"read", "write", "publish"} {
		var found bool
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is not in the listing: %v", want, names)
		}
	}
	// CRITERION 32: the operator fact is stated as a deployment fact, at the point a person meets
	// the question, and explicitly NOT as a grant.
	if !strings.Contains(out, "DEPLOYMENT FACT") {
		t.Errorf("the operator's ability to read everything is not stated as a deployment fact:\n%s", out)
	}
	if !strings.Contains(out, "not a scope") {
		t.Errorf("the listing does not say the operator's access is not a scope:\n%s", out)
	}
}

// TestSessionsListsEverythingAndRevokeEndsExactlyOne is criteria 18, 19, 20, 21 and 24, driven
// end to end through the CLI.
func TestSessionsListsEverythingAndRevokeEndsExactlyOne(t *testing.T) {
	f := newAuthFixture(t, true)

	if code, out, errOut := completeSignIn(t, f, "alice", "--label", "this laptop"); code != cli.Success {
		t.Fatalf("signing in exited %d\n%s%s", code, out, errOut)
	}
	// A second session, signed in elsewhere: minted at the hub, never used, never on this machine.
	other := signInAtHub(t, f, "alice", "the build box", hub.ScopeRead)

	code, out, errOut := runAuthCmd(t, f.getenv, "sessions")
	if code != cli.Success {
		t.Fatalf("`omw auth sessions` exited %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "this laptop") || !strings.Contains(out, "the build box") {
		t.Errorf("not every session is listed:\n%s", out)
	}
	// CRITERION 18: the never-used one is SHOWN as never used, not omitted and not shown as used.
	if !strings.Contains(out, "never used") {
		t.Errorf("a session nobody has used is not shown as never used:\n%s", out)
	}
	if !strings.Contains(out, "scope:") || !strings.Contains(out, "last used:") {
		t.Errorf("criterion 19 asks for scope and last-use on every entry:\n%s", out)
	}

	// CRITERION 20: end one, the other keeps working.
	code, out, errOut = runAuthCmd(t, f.getenv, "revoke", string(other))
	if code != cli.Success {
		t.Fatalf("revoking exited %d\n%s%s", code, out, errOut)
	}
	cred, err := auth.Load(f.root)
	if err != nil {
		t.Fatalf("loading this machine's credential: %v", err)
	}
	if _, err := f.authority.Authenticate(cred.Secret); err != nil {
		t.Errorf("revoking another session broke this machine's: %v", err)
	}

	// CRITERION 21: still listed, never as active.
	_, out, _ = runAuthCmd(t, f.getenv, "sessions")
	for _, block := range strings.Split(out, "\n\n") {
		if strings.Contains(block, "the build box") && strings.Contains(block, "status:    active") {
			t.Errorf("a revoked session is listed as active:\n%s", block)
		}
	}
	if !strings.Contains(out, "revoked") {
		t.Errorf("the revoked session is neither shown as revoked nor removed:\n%s", out)
	}
}

// signInAtHub mints a session for a person WITHOUT going through this machine — the "script on the
// build box" and "the agent session I started last month" of the Issue's journey.
func signInAtHub(t *testing.T, f *authFixture, as hub.PersonID, label string, scopes ...hub.Scope) auth.TokenID {
	t.Helper()
	da, err := f.authority.StartSignIn(auth.SignInRequest{Scopes: scopes, Label: label})
	if err != nil {
		t.Fatalf("starting a sign-in at the hub: %v", err)
	}
	if err := f.authority.Approve(da.UserCode, as); err != nil {
		t.Fatalf("approving: %v", err)
	}
	iss, err := f.authority.Redeem(da)
	if err != nil {
		t.Fatalf("redeeming: %v", err)
	}
	return iss.ID
}

// TestWithNoHubConfiguredNothingReachesOutAndTheMissingThingIsNamed is criteria 26, 27 and 28.
func TestWithNoHubConfiguredNothingReachesOutAndTheMissingThingIsNamed(t *testing.T) {
	f := newAuthFixture(t, false)

	for _, args := range [][]string{
		// `sign-in` LAST, deliberately: it is the one that can wait, so when the no-hub check is
		// broken the other commands fail first and name the criterion, rather than the suite
		// reporting only a timeout.
		{"status"}, {"scopes"}, {"sign-out"}, {"sessions"}, {"revoke", "x"}, {"sign-in"},
	} {
		// BOUNDED: with the no-hub check removed, `sign-in` would print a code and poll for ever.
		// A hang is not a failure — it is the suite stopping — so the wait is explicit.
		code, out, errOut := runAuthCmdWithin(t, 2*time.Second, f.getenv, args...)
		name := "omw auth " + strings.Join(args, " ")

		// CRITERION 27: it never half-works. Empty output with a success status is the forbidden
		// combination, and it is checked as that combination rather than as two separate hopes.
		if code == cli.Success && strings.TrimSpace(out) == "" {
			t.Errorf("%s succeeded with empty output", name)
		}
		if strings.TrimSpace(out)+strings.TrimSpace(errOut) == "" {
			t.Errorf("%s said nothing at all", name)
		}
		// CRITERION 27's other branch: a command that could not complete NAMES the absent hub
		// configuration as the missing thing.
		if code != cli.Success {
			if !strings.Contains(errOut, hub.ErrNoHubConfigured.Code) {
				t.Errorf("%s failed without naming the missing hub configuration:\n%s", name, errOut)
			}
			if !strings.Contains(errOut, auth.HubEnv) {
				t.Errorf("%s did not say WHICH setting is absent:\n%s", name, errOut)
			}
		}
	}

	// CRITERION 26: zero outbound anything. The seam counts every call that would have gone to a
	// hub, and it is zero — which is stronger than "no socket was opened", because it catches a
	// call that would have reached out once a transport exists.
	if n := f.hub.total(); n != 0 {
		t.Errorf("%d hub calls were made with no hub configured; PRD §4.2 says nothing reaches out", n)
	}
}

// TestNoHubConfiguredIsNotUnreachableIsNotSignedOut is criterion 28's three facts, PAIRWISE.
func TestNoHubConfiguredIsNotUnreachableIsNotSignedOut(t *testing.T) {
	noHub := newAuthFixture(t, false)
	_, noHubOut, _ := runAuthCmd(t, noHub.getenv, "status")

	withHub := newAuthFixture(t, true)
	_, signedOutOut, _ := runAuthCmd(t, withHub.getenv, "status")

	// A configured hub, a credential on disk, and no transport: the unreachable case.
	unreachableFix := newAuthFixture(t, true)
	if code, out, errOut := completeSignIn(t, unreachableFix, "alice"); code != cli.Success {
		t.Fatalf("signing in exited %d\n%s%s", code, out, errOut)
	}
	swapAuthHub(t, func(cli.Env) auth.Hub { return auth.Unreachable{} })
	unreachableCode, unreachableOut, _ := runAuthCmd(t, unreachableFix.getenv, "status")

	renderings := map[string]string{
		"no hub configured": firstLine(noHubOut),
		"signed out":        firstLine(signedOutOut),
		"hub unreachable":   firstLine(unreachableOut),
	}
	for i, a := range keysOf(renderings) {
		for _, b := range keysOf(renderings)[i+1:] {
			if renderings[a] == renderings[b] {
				t.Errorf("%q and %q both report %q; they are three different facts", a, b, renderings[a])
			}
		}
	}
	// CRITERION 22: the undetermined case has its own exit code, so a consumer reading only the
	// status can tell it from both determined answers.
	if unreachableCode != cli.ExitUndetermined {
		t.Errorf("an unreachable hub exited %d; 'could not determine' and 'determined to be "+
			"nothing' must not share an exit code", unreachableCode)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Stable order so the pairwise loop is deterministic.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestNoAuthCommandStartsTheDaemon is criterion 25.
func TestNoAuthCommandStartsTheDaemon(t *testing.T) {
	root, getenv := daemonTestStore(t)
	withHub := func(k string) string {
		if k == auth.HubEnv {
			return "https://hub.example"
		}
		return getenv(k)
	}
	swapAuthHub(t, func(cli.Env) auth.Hub { return auth.Unreachable{} })

	for _, args := range [][]string{{"status"}, {"sessions"}, {"revoke", "x"}, {"sign-in"}} {
		name := "omw auth " + strings.Join(args, " ")
		_, out, errOut := runAuthCmdWithin(t, 5*time.Second, withHub, args...)
		said := out + errOut
		if !strings.Contains(said, "not running") {
			t.Errorf("%s did not say the daemon is not running:\n%s", name, said)
		}
		if rep := daemon.Inspect(root); rep.Running.String() == "yes" {
			t.Fatalf("%s started a daemon. PRD §4.2: no command starts the daemon on a person's behalf", name)
		}
	}
}

// TestSignOutSaysWhatItDidNotDo — the local half, and the misunderstanding it would otherwise leave.
func TestSignOutSaysWhatItDidNotDo(t *testing.T) {
	f := newAuthFixture(t, true)
	if code, out, errOut := completeSignIn(t, f, "alice"); code != cli.Success {
		t.Fatalf("signing in exited %d\n%s%s", code, out, errOut)
	}
	cred, err := auth.Load(f.root)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	code, out, errOut := runAuthCmd(t, f.getenv, "sign-out")
	if code != cli.Success {
		t.Fatalf("`omw auth sign-out` exited %d\n%s%s", code, out, errOut)
	}
	if credentialExists(f.root) {
		t.Error("the credential is still there after sign-out")
	}
	if !strings.Contains(out, "UNCHANGED") || !strings.Contains(out, "revoke") {
		t.Errorf("sign-out did not say that the hub session is untouched:\n%s", out)
	}
	if _, err := f.authority.Authenticate(cred.Secret); err != nil {
		t.Errorf("sign-out ended the hub session (%v) while telling the person it had not", err)
	}
}

// TestNoSurfaceEverPrintsTheTokenSecret is the key/secret rule driven the way Issue #9 drove it:
// configure a RECOGNISABLE secret and grep every output stream for it.
//
// The type-level guarantee is in `internal/auth`; this is the end-to-end confirmation across every
// command, including the one that has the material in its hand.
func TestNoSurfaceEverPrintsTheTokenSecret(t *testing.T) {
	f := newAuthFixture(t, true)

	code, signInOut, signInErr := completeSignIn(t, f, "alice", "--scope", "read,write,publish", "--label", "laptop")
	if code != cli.Success {
		t.Fatalf("signing in exited %d\n%s%s", code, signInOut, signInErr)
	}
	cred, err := auth.Load(f.root)
	if err != nil {
		t.Fatalf("loading the credential: %v", err)
	}
	material := cred.Secret.Expose()
	if material == "" {
		t.Fatal("the stored credential has no material, so this test would pass vacuously")
	}

	streams := map[string]string{"sign-in stdout": signInOut, "sign-in stderr": signInErr}
	for _, args := range [][]string{{"status"}, {"sessions"}, {"scopes"}, {"help"}} {
		_, out, errOut := runAuthCmd(t, f.getenv, args...)
		name := strings.Join(args, " ")
		streams[name+" stdout"] = out
		streams[name+" stderr"] = errOut
	}
	// And the daemon's control API, which serves an auth line of its own.
	streams["control API report"] = daemon.Inspect(f.root).Auth + daemon.Inspect(f.root).AuthDetail

	for name, s := range streams {
		if strings.Contains(s, material) {
			t.Errorf("%s printed the token secret:\n%s", name, s)
		}
	}
	// A CONTROL: the grep would find the material if it were there. Without this, a bug that made
	// every command print nothing would pass every assertion above.
	if !strings.Contains(streams["status stdout"]+material, material) {
		t.Fatal("the grep cannot find the material even when it is present; this test proves nothing")
	}
	if !strings.Contains(streams["sign-in stdout"], "signed in as alice") {
		t.Fatalf("the sign-in printed nothing recognisable, so the greps above are vacuous:\n%s", signInOut)
	}
}
