package auth

import (
	"encoding/base32"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// TestEveryAuthOutcomeIsPairwiseDistinguishable is the general form of the half-dozen criteria
// that each say "these two must not look the same".
//
// IT COMPARES EVERY OUTCOME AGAINST EVERY OTHER, codes against codes and prose against prose,
// SPANNING BOTH PACKAGES — because criterion 28's three facts are split across `auth` and `hub`
// and a check that stayed inside one package would be checking the easy half.
func TestEveryAuthOutcomeIsPairwiseDistinguishable(t *testing.T) {
	all := append(append([]*hub.Error{}, allAuthErrors...), borrowedErrors...)

	codes := map[string]string{}
	prose := map[string]string{}
	for _, e := range all {
		if e.Code == "" {
			t.Errorf("%q has no code; a script cannot tell it from anything", e.Msg)
		}
		if strings.TrimSpace(e.Msg) == "" {
			t.Errorf("%q has no message; silence is not an answer", e.Code)
		}
		codes["code "+e.Code] = e.Code
		prose[e.Code] = e.Msg
	}
	if len(codes) != len(all) {
		t.Errorf("%d errors produced %d distinct codes; two of them share one", len(all), len(codes))
	}
	assertPairwiseDistinct(t, prose)

	// AND THE AFFIRMATIVE IS IN THE COMPARISON TOO. CodeSignedIn must not collide with any refusal;
	// a success sharing a code with a failure is the worst of the collisions available.
	prose["signed in"] = CodeSignedIn
	for _, e := range all {
		if e.Code == CodeSignedIn {
			t.Errorf("the success code %q is also a failure's code", CodeSignedIn)
		}
	}
}

// TestTheFourSignInStatesRenderDistinguishably is criteria 22, 27 and 28: signed in, not signed in,
// no hub configured, and undetermined are four facts.
func TestTheFourSignInStatesRenderDistinguishably(t *testing.T) {
	root := t.TempDir()
	a, _ := testAuthority(t)

	// No hub configured. Nothing is consulted; the seam would panic if it were.
	noHub := Observe(root, false, panickingHub{t})

	// A hub, and no credential.
	notSignedIn := Observe(root, true, Direct(a))

	// A hub, a credential, and no way to reach the hub — this build's real shipped state.
	iss := signIn(t, a, "alice", "laptop", hub.ScopeRead)
	writeCredential(t, root, iss)
	undetermined := Observe(root, true, Unreachable{})

	// A hub that answers.
	signedIn := Observe(root, true, Direct(a))

	assertPairwiseDistinct(t, map[string]string{
		"no hub configured": noHub.Text,
		"not signed in":     notSignedIn.Text,
		"undetermined":      undetermined.Text,
		"signed in":         signedIn.Text,
	})
	assertPairwiseDistinct(t, map[string]string{
		"no hub configured": noHub.Code,
		"not signed in":     notSignedIn.Code,
		"undetermined":      undetermined.Code,
		"signed in":         signedIn.Code,
	})

	// NONE OF THEM IS EMPTY. Criterion 22's fourth thing to be distinguishable from is empty output.
	for name, st := range map[string]State{
		"no hub configured": noHub, "not signed in": notSignedIn,
		"undetermined": undetermined, "signed in": signedIn,
	} {
		if strings.TrimSpace(st.Text) == "" {
			t.Errorf("%s renders as empty output", name)
		}
	}

	// AND THE THIRD ANSWER IS NOT A "NO" (§4.3).
	if undetermined.Signed != tri.Undetermined {
		t.Errorf("a hub that could not be reached rendered sign-in as %v; it must be undetermined, "+
			"never a negative", undetermined.Signed)
	}
	if notSignedIn.Signed != tri.No || signedIn.Signed != tri.Yes || noHub.Signed != tri.No {
		t.Errorf("determined states came back as %v / %v / %v",
			notSignedIn.Signed, signedIn.Signed, noHub.Signed)
	}
}

// TestARevokedCredentialReadsAsNotSignedInAndNotAsUndetermined checks the seam between the two:
// the hub ANSWERED, so this is a determined negative, and it keeps the revocation's own code.
func TestARevokedCredentialReadsAsNotSignedInAndNotAsUndetermined(t *testing.T) {
	root := t.TempDir()
	a, _ := testAuthority(t)
	iss := signIn(t, a, "alice", "laptop", hub.ScopeRead)
	writeCredential(t, root, iss)

	if err := a.Revoke("alice", iss.ID); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	st := Observe(root, true, Direct(a))
	if st.Signed != tri.No {
		t.Errorf("a revoked credential rendered as %v; the hub answered, so this is determined", st.Signed)
	}
	if st.Code != ErrTokenRevoked.Code {
		t.Errorf("a revoked credential carries code %q rather than the revocation's own", st.Code)
	}
	if st.Code == hub.ErrHubUnreachable.Code {
		t.Error("a revoked credential is reported as an unreachable hub")
	}
}

// TestObserveNeverTouchesTheHubWithNoHubConfigured is criterion 26 at the function that every
// surface goes through: the seam is a hub that fails the test if it is called at all.
func TestObserveNeverTouchesTheHubWithNoHubConfigured(t *testing.T) {
	root := t.TempDir()
	a, _ := testAuthority(t)
	writeCredential(t, root, signIn(t, a, "alice", "laptop", hub.ScopeRead))

	st := Observe(root, false, panickingHub{t})
	if st.Code != hub.ErrNoHubConfigured.Code {
		t.Errorf("with no hub configured the state is %q", st.Code)
	}
}

// panickingHub fails the test if anything reaches for a hub.
type panickingHub struct{ t *testing.T }

func (p panickingHub) fail(what string) {
	p.t.Helper()
	p.t.Fatalf("%s was called with no hub configured; PRD §4.2 says nothing reaches out", what)
}
func (p panickingHub) StartSignIn(SignInRequest) (DeviceAuthorization, error) {
	p.fail("StartSignIn")
	return DeviceAuthorization{}, nil
}
func (p panickingHub) Redeem(DeviceAuthorization) (Issued, error) {
	p.fail("Redeem")
	return Issued{}, nil
}
func (p panickingHub) Authenticate(Secret) (hub.Grant, error) {
	p.fail("Authenticate")
	return hub.Grant{}, nil
}
func (p panickingHub) Sessions(hub.PersonID) ([]SessionView, error) {
	p.fail("Sessions")
	return nil, nil
}
func (p panickingHub) Revoke(hub.PersonID, TokenID) error { p.fail("Revoke"); return nil }

// TestASecretCannotBePrintedByAccident is the key/secret rule (PRD §3.13's analogue, driven the
// way Issue #9 drove it for model keys) at the type level.
//
// EVERY fmt VERB, not just %v. %#v and %q reach past String on an ordinary type, which is exactly
// how a redaction that only implements String leaks.
func TestASecretCannotBePrintedByAccident(t *testing.T) {
	const recognisable = "RECOGNISABLE-SECRET-MATERIAL-9d2f"
	s := SecretFromStored(recognisable)

	for _, verb := range []string{"%v", "%s", "%q", "%#v", "%+v", "%x", "%d"} {
		got := fmt.Sprintf(verb, s)
		if strings.Contains(got, recognisable) {
			t.Errorf("fmt %s printed the secret: %s", verb, got)
		}
	}
	// Inside a struct, and inside an error, which is how it actually escapes in practice.
	cred := Credential{TokenID: "t", Person: "alice", Secret: s}
	if got := fmt.Sprintf("%+v", cred); strings.Contains(got, recognisable) {
		t.Errorf("a Credential printed the secret: %s", got)
	}
	if got := fmt.Errorf("something went wrong with %v", s).Error(); strings.Contains(got, recognisable) {
		t.Errorf("an error printed the secret: %s", got)
	}
	// A SessionView carries no secret at all — not even a redacted one.
	if got := fmt.Sprintf("%+v", SessionView{ID: "t", Person: "alice"}); strings.Contains(got, recognisable) {
		t.Errorf("a SessionView printed the secret: %s", got)
	}
	// And a JSON encoding, which does not consult String or Format.
	body, err := json.Marshal(struct {
		S Secret `json:"s"`
	}{s})
	if err == nil && strings.Contains(string(body), recognisable) {
		t.Errorf("json.Marshal serialised the secret: %s", body)
	}
	// The exposure is deliberate and works, or the credential file would be useless.
	if s.Expose() != recognisable {
		t.Errorf("Expose did not give back the material")
	}
}

// TestTheUserCodeAlphabetIsExactlyThirtyTwoSymbols catches at test time what base32 catches with a
// panic in an init — which reports as a build failure and names nothing.
func TestTheUserCodeAlphabetIsExactlyThirtyTwoSymbols(t *testing.T) {
	if len(userCodeAlphabet) != 32 {
		t.Fatalf("the user-code alphabet has %d symbols; base32 needs exactly 32", len(userCodeAlphabet))
	}
	seen := map[rune]bool{}
	for _, r := range userCodeAlphabet {
		if seen[r] {
			t.Errorf("%q appears twice in the alphabet", r)
		}
		seen[r] = true
	}
	for _, bad := range []rune{'I', 'O', '0', 'U'} {
		if seen[bad] {
			t.Errorf("%q is in the alphabet and is one of the characters people mistype", bad)
		}
	}
	if _, err := base32.NewEncoding(userCodeAlphabet).WithPadding(base32.NoPadding).DecodeString("AAAAAAAA"); err != nil {
		t.Errorf("the encoding does not round-trip: %v", err)
	}
}

// TestUserCodesAreUnguessableAndReadable — printed on purpose, and still not guessable.
func TestUserCodesAreUnguessableAndReadable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := newUserCode()
		if err != nil {
			t.Fatalf("minting a user code: %v", err)
		}
		if seen[c] {
			t.Fatalf("user code %q was minted twice in 200 tries", c)
		}
		seen[c] = true
		if len(c) != 9 || c[4] != '-' {
			t.Fatalf("user code %q is not the XXXX-XXXX shape a person is asked to type", c)
		}
		if normaliseUserCode(strings.ToLower(c)) != normaliseUserCode(c) {
			t.Fatalf("a person who typed %q in lower case would not be recognised", c)
		}
	}
}

// TestACredentialIsOwnerOnlyOnDisk. A credential another user can read is not a credential.
func TestACredentialIsOwnerOnlyOnDisk(t *testing.T) {
	root := t.TempDir()
	a, _ := testAuthority(t)
	iss := signIn(t, a, "alice", "laptop", hub.ScopeRead)
	writeCredential(t, root, iss)

	info, err := os.Stat(CredentialPath(root))
	if err != nil {
		t.Fatalf("the credential was not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the credential is mode %04o — readable by users other than its owner", perm)
	}
	dir, err := os.Stat(filepath.Dir(CredentialPath(root)))
	if err != nil {
		t.Fatalf("the credential directory: %v", err)
	}
	if perm := dir.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the credential directory is mode %04o", perm)
	}

	// AND IT ROUND-TRIPS, including the material — a credential that cannot be read back is a
	// sign-in that has to be done again every time.
	back, err := Load(root)
	if err != nil {
		t.Fatalf("loading the credential just written: %v", err)
	}
	if back.Secret.Expose() != iss.Secret.Expose() {
		t.Error("the credential did not round-trip its material")
	}
	if back.TokenID != iss.ID || back.Person != iss.Person {
		t.Errorf("the credential round-tripped as %s / %s", back.TokenID, back.Person)
	}
}

// TestAPresentButUnreadableCredentialIsUndeterminedAndNotSignedOut is §4.3's most tempting
// collapse: the short version of Load returns "no credential" for both, and is wrong exactly when
// it matters.
func TestAPresentButUnreadableCredentialIsUndeterminedAndNotSignedOut(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(CredentialPath(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CredentialPath(root), []byte("{this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(root); hub.Code(err) != ErrCredentialUnreadable.Code {
		t.Errorf("a corrupt credential answered %q; something IS there and it could not be read", hub.Code(err))
	}
	st := Observe(root, true, Unreachable{})
	if st.Signed != tri.Undetermined {
		t.Errorf("a corrupt credential rendered sign-in as %v; it must not be a determined negative", st.Signed)
	}
	if st.Code == ErrNotSignedIn.Code {
		t.Error("a corrupt credential is reported as 'not signed in'")
	}
}

// writeCredential puts an issued token on disk the way the sign-in command does.
func writeCredential(t *testing.T, root string, iss Issued) {
	t.Helper()
	err := Save(root, Credential{
		TokenID: iss.ID, Person: iss.Person, Scopes: iss.Scopes,
		ExpiresAt: iss.ExpiresAt, Secret: iss.Secret,
	})
	if err != nil {
		t.Fatalf("writing a credential: %v", err)
	}
}

// TestForgettingACredentialDoesNotEndTheSession — the local half of sign-out, and the thing a
// person would otherwise assume wrongly.
func TestForgettingACredentialDoesNotEndTheSession(t *testing.T) {
	root := t.TempDir()
	a, _ := testAuthority(t)
	iss := signIn(t, a, "alice", "laptop", hub.ScopeRead)
	writeCredential(t, root, iss)

	if err := Forget(root); err != nil {
		t.Fatalf("forgetting: %v", err)
	}
	if _, err := Load(root); err == nil {
		t.Error("the credential is still on disk after Forget")
	}
	if _, err := a.Authenticate(iss.Secret); err != nil {
		t.Errorf("forgetting the local credential ended the hub session (%v); it must not, which is "+
			"why the command says so out loud", err)
	}
	// Forgetting twice is not an error: a person running sign-out on a machine that was never
	// signed in has achieved what they asked for.
	if err := Forget(root); err != nil {
		t.Errorf("forgetting a credential that is not there answered %v", err)
	}
}

// TestLastUseRenderingsAreThree at the type level, independent of any authority, because these are
// the three renderings criterion 19 names and they must be distinguishable in isolation.
func TestLastUseRenderingsAreThree(t *testing.T) {
	assertPairwiseDistinct(t, map[string]string{
		"used":         UsedAt(time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)).Render(),
		"never used":   NeverUsed().Render(),
		"undetermined": UnknownLastUse().Render(),
		// A "used" with no timestamp is a fourth thing and must not silently render as any of the
		// three above — least of all as a real time.
		"used, no time recorded": LastUse{State: tri.Yes}.Render(),
	})
	for name, r := range map[string]string{
		"used": UsedAt(time.Now()).Render(), "never used": NeverUsed().Render(),
		"undetermined": UnknownLastUse().Render(),
	} {
		if strings.TrimSpace(r) == "" {
			t.Errorf("%s renders empty", name)
		}
	}
}
