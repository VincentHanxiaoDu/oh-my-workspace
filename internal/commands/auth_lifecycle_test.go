package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/auth"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// TestAnAbandonedSignInLeavesTheClientExactlyAsItWas is criterion 3, and criterion 6 at the CLI.
//
// The sign-in is started and never completed; the clock moves past the code's life; the command
// returns. What is then asserted is not only the command's answer but the CLIENT'S STATE: no
// credential, and "am I signed in" answering a DETERMINED no — distinguishable from undetermined by
// exit code alone.
func TestAnAbandonedSignInLeavesTheClientExactlyAsItWas(t *testing.T) {
	f := newAuthFixture(t, true)

	before := credentialExists(f.root)
	if before {
		t.Fatal("the fixture already holds a credential, so this test would prove nothing")
	}

	prev := authPollInterval
	authPollInterval = time.Millisecond
	t.Cleanup(func() { authPollInterval = prev })

	var out, errb lockedBuffer
	done := make(chan int, 1)
	go func() { done <- cli.Run([]string{"auth", "sign-in"}, &out, &errb, f.getenv) }()

	if code := waitForCode(t, &out); code == "" {
		t.Fatal("no device code was printed")
	}
	// Nobody goes to the browser. The clock does.
	f.clock.add(11 * time.Minute)

	var rc int
	select {
	case rc = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("an abandoned sign-in never returned; a device code that never dies is criterion 6's defect")
	}

	if rc == cli.Success {
		t.Errorf("an abandoned sign-in exited 0\n%s%s", out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), auth.ErrDeviceCodeExpired.Code) {
		t.Errorf("the outcome was not named as an expiry:\n%s", errb.String())
	}
	// CRITERION 6's DISTINGUISHABILITY, at the CLI: expiry is not a refusal for scope and not an
	// unreachable hub.
	for _, other := range []string{hub.ErrHubUnreachable.Code, hub.ErrGrantWiderThanHolder.Code} {
		if strings.Contains(errb.String(), other) {
			t.Errorf("an expired code was reported with %q:\n%s", other, errb.String())
		}
	}

	// CRITERION 3: exactly the pre-sign-in state.
	if credentialExists(f.root) {
		t.Error("an abandoned sign-in left a credential behind")
	}
	if n := len(f.authority.Sessions("alice")); n != 0 {
		t.Errorf("%d sessions exist at the hub after an abandoned sign-in", n)
	}

	// AND "AM I SIGNED IN" ANSWERS A DETERMINED NO. The exit code is the assertion, because it is
	// what a consumer reading only the status can act on.
	code, sOut, sErr := runAuthCmd(t, f.getenv, "status")
	if code != cli.Success {
		t.Errorf("`omw auth status` after an abandoned sign-in exited %d; a determined 'no' is a "+
			"successful answer, and %d is the code reserved for 'could not determine'\n%s%s",
			code, cli.ExitUndetermined, sOut, sErr)
	}
	if !strings.Contains(sOut, auth.ErrNotSignedIn.Code) {
		t.Errorf("the status did not report a determined 'not signed in':\n%s", sOut)
	}
	if strings.Contains(sOut, auth.ErrSignInUndetermined.Code) {
		t.Errorf("the status reported undetermined where the answer was established:\n%s", sOut)
	}
}

// TestARevokedTokenStopsWorkingAtTheClient is criterion 20 seen from the machine holding the token:
// the credential is still on disk, and the answer is a determined "not signed in" naming the
// revocation — never a hub that could not be reached.
func TestARevokedTokenStopsWorkingAtTheClient(t *testing.T) {
	f := newAuthFixture(t, true)
	if code, out, errOut := completeSignIn(t, f, "alice", "--label", "this laptop"); code != cli.Success {
		t.Fatalf("signing in exited %d\n%s%s", code, out, errOut)
	}
	cred, err := auth.Load(f.root)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if code, out, errOut := runAuthCmd(t, f.getenv, "revoke", string(cred.TokenID)); code != cli.Success {
		t.Fatalf("revoking this machine's own session exited %d\n%s%s", code, out, errOut)
	}

	code, out, _ := runAuthCmd(t, f.getenv, "status")
	if !strings.Contains(out, auth.ErrTokenRevoked.Code) {
		t.Errorf("after revocation the status does not name the revocation:\n%s", out)
	}
	if strings.Contains(out, hub.ErrHubUnreachable.Code) {
		t.Errorf("a revoked token was reported as an unreachable hub:\n%s", out)
	}
	if code != cli.Success {
		t.Errorf("a revoked token is a DETERMINED 'not signed in' and exited %d", code)
	}
	// AND IT ACTUALLY STOPPED WORKING — driven, not asserted from the rendering.
	if _, err := f.authority.Authenticate(cred.Secret); hub.Code(err) != auth.ErrTokenRevoked.Code {
		t.Errorf("the revoked token still authenticates: %v", err)
	}
	// The credential file is untouched: revocation is the hub's act, not a local deletion, and a
	// command that silently deleted it would hide the fact that the token was ended by somebody.
	if !credentialExists(f.root) {
		t.Error("revoking deleted the local credential; the person can no longer see what happened")
	}

	// AND THE RENDERING IS COMPARED AGAINST THE OTHER TWO, NOT AGAINST A CONSTANT.
	//
	// This block was added after a mutation showed it was needed: collapsing ErrTokenRevoked onto
	// ErrNoSuchToken's code and message turned the auth package red and left every assertion above
	// GREEN — because they all match on `auth.ErrTokenRevoked.Code`, which the mutation had
	// redefined. A test that reads its expectation from the thing under test cannot see the two
	// facts merge. So the three renderings are compared with each other, at the surface a person
	// actually reads.
	plantCredential(t, f.root) // a credential whose token the hub has never heard of
	_, neverOut, _ := runAuthCmd(t, f.getenv, "status")

	revokedLine, neverLine := firstLine(out), firstLine(neverOut)
	if revokedLine == neverLine {
		t.Errorf("a revoked token and a token that never existed both report %q at the CLI; "+
			"they are different facts (criterion 20, PRD §4.3)", revokedLine)
	}
	if strings.TrimSpace(neverLine) == "" || strings.TrimSpace(revokedLine) == "" {
		t.Errorf("one of the two states rendered as nothing: %q / %q", revokedLine, neverLine)
	}
}

// TestTheControlAPIAndTheCLIReportTheSameAuthState is criterion 23, INCLUDING the undetermined case.
//
// IT DOES NOT STUB THE SEAM. Both sides run the shipped [auth.Unreachable] path — the daemon has no
// other — so what is compared is two surfaces' real answers about the same machine at the same
// moment. The environment is set for real with t.Setenv because the daemon reads the process
// environment and the CLI is handed os.Getenv; two different readers would make agreement an
// accident of the fixture rather than a property of the product.
func TestTheControlAPIAndTheCLIReportTheSameAuthState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hub        string
		credential bool
	}{
		{"no hub configured", "", false},
		{"a hub and no credential", "https://hub.example", false},
		{"a hub that cannot be reached, holding a credential", "https://hub.example", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storeRoot := freshStore(t)
			t.Setenv(store.PathEnv, storeRoot)
			t.Setenv(auth.HubEnv, tc.hub)
			if tc.credential {
				plantCredential(t, storeRoot)
			}

			d, err := daemon.Start(daemon.Options{
				StorePath: storeRoot, Interval: 5 * time.Millisecond, Write: func() error { return nil },
			})
			if err != nil {
				t.Fatalf("the daemon did not start: %v", err)
			}
			defer d.Close()
			if d.Control() == nil {
				_, why := d.ControlState()
				t.Skipf("the control API did not open here, so there is no second surface to agree "+
					"with: %s", why)
			}

			// The control API's answer, fetched THROUGH the socket: Inspect returns the daemon's own
			// report when one is listening.
			rep := daemon.Inspect(storeRoot)
			if rep.Auth == "" {
				t.Fatal("the control API reported no auth state at all; empty is not one of the answers")
			}

			// The CLI's answer, from the same environment.
			code, out, errOut := runAuthCmd(t, os.Getenv, "status")
			if !strings.Contains(out, rep.Auth) {
				t.Errorf("the CLI and the control API disagree.\ncontrol API: %q (code %q)\nCLI:\n%s%s",
					rep.Auth, rep.AuthCode, out, errOut)
			}
			if !strings.Contains(out, rep.AuthCode) {
				t.Errorf("the CLI does not carry the control API's code %q:\n%s", rep.AuthCode, out)
			}
			// AND THE UNDETERMINED CASE IS ACTUALLY REACHED by one of these rows, or the criterion's
			// hardest half is untested.
			if tc.credential && rep.AuthCode != hub.ErrHubUnreachable.Code {
				t.Errorf("the row that exists to drive the undetermined case reported %q instead", rep.AuthCode)
			}
			if tc.credential && code != cli.ExitUndetermined {
				t.Errorf("the undetermined row exited %d", code)
			}
			_ = errOut
		})
	}
}

// freshStore makes a real store and returns its root. Unlike daemonTestStore it hands back no
// getenv, because the agreement test sets the REAL environment: the daemon reads os.Getenv and a
// map-backed reader for the CLI would make the two surfaces agree by construction of the fixture.
func freshStore(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("could not create a store to test against: %v", err)
	}
	return root
}

// plantCredential puts a credential on disk WITHOUT going through the sign-in command, so that the
// agreement test can reach the "holding a credential and unable to confirm it" state without a
// hub. It is a test writing a file, not a second sign-in path — TestOnlyTheSignInCommandCanCreate
// ACredential walks product files only.
func plantCredential(t *testing.T, root string) {
	t.Helper()
	err := auth.Save(root, auth.Credential{
		TokenID:   "0123456789abcdef0123456789abcdef",
		Person:    "alice",
		Scopes:    []hub.Scope{hub.ScopeRead},
		ExpiresAt: time.Now().Add(time.Hour),
		Secret:    auth.SecretFromStored("planted-material-for-a-test"),
	})
	if err != nil {
		t.Fatalf("planting a credential: %v", err)
	}
}

// TestAuthSaysSoWhenTheControlAPIDidNotOpen is criterion 29 (PRD §4.6, §5.1's ruling).
//
// The refusal is produced through the injected confirmation seam rather than by finding a platform
// where owner-only permissions are unconfirmable — a test that skips unless it is on such a
// platform never runs on either platform this product ships for.
func TestAuthSaysSoWhenTheControlAPIDidNotOpen(t *testing.T) {
	storeRoot, base := daemonTestStore(t)
	getenv := func(k string) string {
		if k == auth.HubEnv {
			return "https://hub.example"
		}
		return base(k)
	}
	swapAuthHub(t, func(cli.Env) auth.Hub { return auth.Unreachable{} })

	d, err := daemon.Start(daemon.Options{
		StorePath: storeRoot, Interval: 5 * time.Millisecond, Write: func() error { return nil },
		ConfirmOwnerOnly: func(string) (tri.Value, string) {
			return tri.Undetermined, "the owner of the socket could not be read on this filesystem"
		},
	})
	if err != nil {
		t.Fatalf("a daemon whose control API declines should still start: %v", err)
	}
	defer d.Close()
	if d.Control() != nil {
		t.Fatal("the control API opened despite an unconfirmable socket, so this test is not " +
			"exercising the state criterion 29 is about")
	}

	code, out, errOut := runAuthCmd(t, getenv, "sessions")

	// DISTINGUISHABLE FROM SUCCESS.
	if code == cli.Success {
		t.Errorf("`omw auth sessions` succeeded with no control API\n%s%s", out, errOut)
	}
	if !strings.Contains(errOut, auth.ErrControlAPINotOpen.Code) {
		t.Errorf("the reason was not named as the control API declining to open:\n%s", errOut)
	}
	// DISTINGUISHABLE FROM "THE DAEMON IS NOT RUNNING", which is the confusion §4.6 invites: the
	// daemon IS running here.
	if strings.Contains(errOut, hub.ErrDaemonNotRunning.Code) {
		t.Errorf("a declining control API was reported as a stopped daemon:\n%s", errOut)
	}
	if strings.Contains(errOut, codeDaemonUndetermined) {
		t.Errorf("a declining control API was reported as an undetermined daemon liveness:\n%s", errOut)
	}
	if !strings.Contains(errOut, "the daemon IS running") {
		t.Errorf("the message does not distinguish itself from a stopped daemon in prose:\n%s", errOut)
	}
	// AND NOTHING HAPPENED.
	if credentialExists(storeRoot) {
		t.Error("a credential exists after a command that refused to proceed")
	}
}
