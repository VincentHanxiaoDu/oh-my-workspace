package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// clock is a movable time source, so expiry is DRIVEN rather than slept through.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

// testAuthority builds a hub with two people whose own capabilities differ, because almost every
// criterion here is about the difference.
//
//	alice  holds all three scopes — she can ask for publish
//	bob    holds read only — he cannot, and asking is refused rather than narrowed
func testAuthority(t *testing.T) (*Authority, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)}
	a := NewAuthority(AuthorityOptions{
		Now:             c.now,
		CodeLife:        10 * time.Minute,
		TokenLife:       24 * time.Hour,
		VerificationURI: "https://hub.example/device",
	})
	a.Register("alice", []hub.Scope{hub.ScopeRead, hub.ScopeWrite, hub.ScopePublish})
	a.Register("bob", []hub.Scope{hub.ScopeRead})
	return a, c
}

// signIn drives a complete device-code sign-in and fails the test if it does not complete.
func signIn(t *testing.T, a *Authority, as hub.PersonID, label string, scopes ...hub.Scope) Issued {
	t.Helper()
	da, err := a.StartSignIn(SignInRequest{Scopes: scopes, Label: label})
	if err != nil {
		t.Fatalf("starting a sign-in for %s with %v: %v", as, scopes, err)
	}
	if err := a.Approve(da.UserCode, as); err != nil {
		t.Fatalf("approving %s's sign-in: %v", as, err)
	}
	iss, err := a.Redeem(da)
	if err != nil {
		t.Fatalf("redeeming %s's completed device code: %v", as, err)
	}
	return iss
}

// TestIssuingADeviceCodeCreatesNoToken is criterion 5.
//
// The assertion that matters is the SECOND one: not merely that Redeem refused, but that the hub
// has no session at all. A flow that minted a token and withheld it would satisfy "the command has
// not succeeded" while having done the thing.
func TestIssuingADeviceCodeCreatesNoToken(t *testing.T) {
	a, _ := testAuthority(t)

	da, err := a.StartSignIn(SignInRequest{Scopes: []hub.Scope{hub.ScopeRead}})
	if err != nil {
		t.Fatalf("starting a sign-in: %v", err)
	}
	if da.UserCode == "" {
		t.Fatal("no code was printed, so there is nothing for a person to enter in a browser")
	}

	_, err = a.Redeem(da)
	if hub.Code(err) != ErrSignInPending.Code {
		t.Errorf("redeeming an unapproved device code answered %v (code %q); "+
			"criterion 5 wants it reported as not yet completed", err, hub.Code(err))
	}
	for _, p := range []hub.PersonID{"alice", "bob"} {
		if n := len(a.Sessions(p)); n != 0 {
			t.Errorf("%d sessions exist for %s between the code being printed and anybody completing "+
				"the browser step; criterion 5 says no token exists yet", n, p)
		}
	}
}

// TestAnUncompletedDeviceCodeExpiresAndCanNeverBecomeAToken is criterion 6.
func TestAnUncompletedDeviceCodeExpiresAndCanNeverBecomeAToken(t *testing.T) {
	a, c := testAuthority(t)

	da, err := a.StartSignIn(SignInRequest{Scopes: []hub.Scope{hub.ScopeRead}})
	if err != nil {
		t.Fatalf("starting a sign-in: %v", err)
	}
	c.add(11 * time.Minute)

	if _, err := a.Redeem(da); hub.Code(err) != ErrDeviceCodeExpired.Code {
		t.Errorf("redeeming an expired code answered %q; criterion 6 wants expiry named", hub.Code(err))
	}
	// AND IT IS DEAD, not merely late. Approving it afterwards must not resurrect it — that is the
	// "cannot later turn into a token" half, and it is the half a naive implementation gets wrong
	// by checking expiry only on the redeem path.
	if err := a.Approve(da.UserCode, "alice"); hub.Code(err) != ErrDeviceCodeExpired.Code {
		t.Errorf("an expired code was approved with %v; it must not be completable at all", err)
	}
	if _, err := a.Redeem(da); hub.Code(err) != ErrDeviceCodeExpired.Code {
		t.Errorf("an expired code turned into %q after a late approval", hub.Code(err))
	}
	if n := len(a.Sessions("alice")); n != 0 {
		t.Errorf("%d sessions exist after an expired sign-in; the client's state must be exactly "+
			"its pre-sign-in state", n)
	}

	// CRITERION 6's LAST CLAUSE, COMPARED PAIRWISE. Expiry, a refusal and an unreachable hub are
	// three outcomes and must not share a code.
	codes := map[string]string{
		"expired":     ErrDeviceCodeExpired.Code,
		"refused":     hub.ErrGrantWiderThanHolder.Code,
		"unreachable": hub.ErrHubUnreachable.Code,
	}
	assertPairwiseDistinct(t, codes)
}

// TestARedeemedDeviceCodeCannotBeReplayed is criterion 7.
//
// It counts sessions before and after the replay. "The refusal happened" is not the criterion; "no
// second token was minted" is, and only the count shows that.
func TestARedeemedDeviceCodeCannotBeReplayed(t *testing.T) {
	a, _ := testAuthority(t)

	da, err := a.StartSignIn(SignInRequest{Scopes: []hub.Scope{hub.ScopeRead}, Label: "laptop"})
	if err != nil {
		t.Fatalf("starting a sign-in: %v", err)
	}
	if err := a.Approve(da.UserCode, "alice"); err != nil {
		t.Fatalf("approving: %v", err)
	}
	if _, err := a.Redeem(da); err != nil {
		t.Fatalf("the first redemption should succeed: %v", err)
	}
	before := len(a.Sessions("alice"))

	_, err = a.Redeem(da)
	if hub.Code(err) != ErrDeviceCodeRedeemed.Code {
		t.Errorf("replaying a redeemed code answered %q; criterion 7 wants the replay refused as a replay", hub.Code(err))
	}
	if after := len(a.Sessions("alice")); after != before {
		t.Errorf("the replay changed the number of live sessions from %d to %d; it must mint nothing", before, after)
	}
	if hub.Code(err) == hub.ErrHubUnreachable.Code {
		t.Error("a replay refusal must not look like a hub that did not answer")
	}
}

// TestAGrantWiderThanItsHolderIsRefusedAndLeavesNothingBehind is criteria 15 and 16, and it is the
// one Issue #12 asked most insistently be kept calling EvaluateGrantRequest for.
func TestAGrantWiderThanItsHolderIsRefusedAndLeavesNothingBehind(t *testing.T) {
	a, _ := testAuthority(t)

	// Bob holds `read`. He asks for `publish`.
	da, err := a.StartSignIn(SignInRequest{Scopes: []hub.Scope{hub.ScopePublish}})
	if err != nil {
		t.Fatalf("naming a KNOWN scope must not be refused at the code step: %v", err)
	}
	err = a.Approve(da.UserCode, "bob")
	if hub.Code(err) != hub.ErrGrantWiderThanHolder.Code {
		t.Fatalf("bob holds only read and asked for publish; the answer was %v (code %q), and §4.5 "+
			"wants it refused as wider than its holder", err, hub.Code(err))
	}
	// NOTHING WAS NARROWED AND NOTHING WAS LEFT BEHIND.
	if n := len(a.Sessions("bob")); n != 0 {
		t.Errorf("%d tokens exist for bob after a refused request; criterion 15 says none does", n)
	}
	if _, err := a.Redeem(da); hub.Code(err) != hub.ErrGrantWiderThanHolder.Code {
		t.Errorf("polling after a refusal answered %q; the client must learn the request FAILED, "+
			"not keep waiting and not receive a narrower token", hub.Code(err))
	}
	if n := len(a.Sessions("bob")); n != 0 {
		t.Errorf("%d tokens exist for bob after polling a refused request", n)
	}
}

// TestTheScopeReportedIsTheScopeTheTokenHas is criterion 16 from the affirmative side.
func TestTheScopeReportedIsTheScopeTheTokenHas(t *testing.T) {
	a, _ := testAuthority(t)
	want := []hub.Scope{hub.ScopeRead, hub.ScopePublish}

	iss := signIn(t, a, "alice", "laptop", want...)
	if len(iss.Scopes) != len(want) {
		t.Fatalf("asked for %v and was given a token reporting %v", want, iss.Scopes)
	}
	for i := range want {
		if iss.Scopes[i] != want[i] {
			t.Fatalf("asked for %v and was given a token reporting %v", want, iss.Scopes)
		}
	}
	// AND THE TOKEN ACTUALLY HAS IT — the reported scope is checked against what the token DOES,
	// not only against what the mint said. A token whose report and behaviour differ is exactly
	// what criterion 16 forbids.
	g, err := a.Authenticate(iss.Secret)
	if err != nil {
		t.Fatalf("the token just minted does not authenticate: %v", err)
	}
	if !hub.Permits(g.Scopes, hub.ScopePublish) || !hub.Permits(g.Scopes, hub.ScopeRead) {
		t.Errorf("the token reports %v and permits %v", iss.Scopes, g.Scopes)
	}
	if hub.Permits(g.Scopes, hub.ScopeWrite) {
		t.Errorf("the token permits write, which was never asked for: %v", g.Scopes)
	}
}

// TestAnUnknownScopeIsRefusedAsUnknownAndNotAsTooWide is criteria 8, 30 and 31 — including the
// operations-is-not-a-scope ruling, driven by asking for exactly the names somebody would invent.
func TestAnUnknownScopeIsRefusedAsUnknownAndNotAsTooWide(t *testing.T) {
	a, _ := testAuthority(t)

	for _, name := range []string{"operate", "admin", "all", "operator", "read-all", "publish:any", "READ"} {
		_, err := a.StartSignIn(SignInRequest{Scopes: []hub.Scope{hub.Scope(name)}})
		if hub.Code(err) != hub.ErrUnknownScope.Code {
			t.Errorf("asking for scope %q answered %q; it is not in the vocabulary and must be "+
				"refused as UNKNOWN", name, hub.Code(err))
		}
		if hub.Code(err) == hub.ErrGrantWiderThanHolder.Code {
			t.Errorf("scope %q was refused as too wide; criterion 31 wants unknown and too-wide "+
				"told apart, because they are fixed by different things", name)
		}
	}

	// THE VOCABULARY IS EXACTLY THREE (criterion 30). Asserted against the hub's own list, and by
	// count, so a fourth name added anywhere fails here.
	v := hub.Vocabulary()
	if len(v) != 3 {
		t.Fatalf("the vocabulary has %d names: %v. It is ruled at three.", len(v), v)
	}
	want := map[hub.Scope]bool{hub.ScopeRead: true, hub.ScopeWrite: true, hub.ScopePublish: true}
	for _, s := range v {
		if !want[s] {
			t.Errorf("%q is in the vocabulary and is not one of read/write/publish", s)
		}
	}
}

// TestReadAloneIsEnoughForTheOrdinaryReadingPath is criterion 10, and TestAReadTokenCannotPublish
// below is 11 and 12. Both go through `internal/hub`'s existing ReadThrough and PublishThrough, so
// what is being driven is that a token from THIS package is a grant THAT code already governs —
// which is criterion 14, one scope meaning one thing on every surface.
func TestReadAloneIsEnoughForTheOrdinaryReadingPath(t *testing.T) {
	a, _ := testAuthority(t)
	s := hub.NewStore(nil)
	note, err := s.Publish(hub.Publication{Author: "alice", Body: "a note", Visibility: hub.CompanyWide()})
	if err != nil {
		t.Fatalf("publishing a note to read: %v", err)
	}

	iss := signIn(t, a, "alice", "laptop", hub.ScopeRead)
	g, err := a.Authenticate(iss.Secret)
	if err != nil {
		t.Fatalf("authenticating a read token: %v", err)
	}
	if _, err := hub.ReadThrough(s, g, note.ID); err != nil {
		t.Errorf("a token holding read alone could not read: %v. Criterion 10 says everyday use "+
			"needs read alone, with no further grant", err)
	}
}

func TestAReadOrWriteTokenCannotPublish(t *testing.T) {
	a, _ := testAuthority(t)
	s := hub.NewStore(nil)

	for _, tc := range []struct {
		name   string
		scopes []hub.Scope
	}{
		{"read only", []hub.Scope{hub.ScopeRead}},
		{"read and write", []hub.Scope{hub.ScopeRead, hub.ScopeWrite}},
		{"write only", []hub.Scope{hub.ScopeWrite}},
	} {
		iss := signIn(t, a, "alice", tc.name, tc.scopes...)
		g, err := a.Authenticate(iss.Secret)
		if err != nil {
			t.Fatalf("%s: authenticating: %v", tc.name, err)
		}
		_, err = hub.PublishThrough(s, g, hub.Publication{Author: "alice", Body: "x", Visibility: hub.CompanyWide()})
		if hub.Code(err) != hub.ErrPublishScopeRequired.Code {
			t.Errorf("%s: publishing answered %q; criteria 11 and 12 want it refused for want of "+
				"the publish scope", tc.name, hub.Code(err))
		}
		// THREE-WAY DISTINGUISHABILITY, COMPARED PAIRWISE. Not against a literal: against the other
		// two outcomes' codes.
		assertPairwiseDistinct(t, map[string]string{
			"refused for scope": hub.Code(err),
			"hub unreachable":   hub.ErrHubUnreachable.Code,
			"published":         "",
		})
	}
}

// TestPublishIsNeverAcquiredWithoutBeingAskedFor is criterion 13. No upgrade on use, no widening
// on retry, no implicit grant alongside read or write.
func TestPublishIsNeverAcquiredWithoutBeingAskedFor(t *testing.T) {
	a, _ := testAuthority(t)
	s := hub.NewStore(nil)

	// Alice CAN publish — she holds the scope herself. She just did not ask for it.
	iss := signIn(t, a, "alice", "laptop", hub.ScopeRead, hub.ScopeWrite)

	for attempt := 0; attempt < 3; attempt++ {
		g, err := a.Authenticate(iss.Secret)
		if err != nil {
			t.Fatalf("attempt %d: authenticating: %v", attempt, err)
		}
		if hub.Permits(g.Scopes, hub.ScopePublish) {
			t.Fatalf("attempt %d: a token that never asked for publish now carries it: %v", attempt, g.Scopes)
		}
		if _, err := hub.PublishThrough(s, g, hub.Publication{Author: "alice", Body: "x", Visibility: hub.CompanyWide()}); err == nil {
			t.Fatalf("attempt %d: the publish succeeded; retrying must not widen a token", attempt)
		}
	}
}

// TestRevocationActuallyStopsATokenAndOnlyThatToken is criterion 20, DRIVEN: the revoked token's
// next use is refused and the other one still authenticates.
func TestRevocationActuallyStopsATokenAndOnlyThatToken(t *testing.T) {
	a, _ := testAuthority(t)

	laptop := signIn(t, a, "alice", "this laptop", hub.ScopeRead)
	buildBox := signIn(t, a, "alice", "the build box", hub.ScopeRead)

	if _, err := a.Authenticate(laptop.Secret); err != nil {
		t.Fatalf("the laptop token should work before revocation: %v", err)
	}
	if err := a.Revoke("alice", laptop.ID); err != nil {
		t.Fatalf("revoking the laptop session: %v", err)
	}

	_, err := a.Authenticate(laptop.Secret)
	if hub.Code(err) != ErrTokenRevoked.Code {
		t.Errorf("a revoked token's next use answered %q; it must be refused AS REVOKED", hub.Code(err))
	}
	if hub.Code(err) == hub.ErrHubUnreachable.Code {
		t.Error("a revocation refusal must be distinguishable from a hub that did not answer")
	}
	if _, err := a.Authenticate(buildBox.Secret); err != nil {
		t.Errorf("revoking one session broke another: %v. Criterion 20 says the others keep working", err)
	}

	// CRITERION 21: still listed, and never as active.
	var seen bool
	for _, v := range a.Sessions("alice") {
		if v.ID != laptop.ID {
			continue
		}
		seen = true
		if v.Status == StatusActive {
			t.Errorf("the revoked session is listed as %s", v.Status)
		}
		if v.Status != StatusRevoked {
			t.Errorf("the revoked session is listed as %s rather than revoked", v.Status)
		}
	}
	if !seen {
		t.Error("the revoked session vanished from the listing entirely; criterion 21 permits " +
			"removal, but this implementation lists it and the test must match what it does")
	}
}

// TestRevokedAndNeverExistedAreDifferentFacts is criterion 20 read with §4.3, and it is the
// mutation the brief calls out by name. Three renderings, compared PAIRWISE.
func TestRevokedAndNeverExistedAreDifferentFacts(t *testing.T) {
	a, c := testAuthority(t)

	revoked := signIn(t, a, "alice", "revoked", hub.ScopeRead)
	if err := a.Revoke("alice", revoked.ID); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	_, revokedErr := a.Authenticate(revoked.Secret)

	expiring := signIn(t, a, "alice", "expiring", hub.ScopeRead)
	c.add(25 * time.Hour)
	_, expiredErr := a.Authenticate(expiring.Secret)

	_, neverErr := a.Authenticate(SecretFromStored("a token that was never minted"))

	got := map[string]string{
		"revoked":       hub.Code(revokedErr),
		"expired":       hub.Code(expiredErr),
		"never existed": hub.Code(neverErr),
		"unreachable":   hub.ErrHubUnreachable.Code,
	}
	assertPairwiseDistinct(t, got)

	// And the same pairwise check over the PROSE, because a code nobody prints is not a
	// distinction a person experiences.
	assertPairwiseDistinct(t, map[string]string{
		"revoked":       revokedErr.Error(),
		"expired":       expiredErr.Error(),
		"never existed": neverErr.Error(),
		"unreachable":   hub.ErrHubUnreachable.Error(),
	})

	if hub.Code(revokedErr) != ErrTokenRevoked.Code {
		t.Errorf("a revoked token answered %q", hub.Code(revokedErr))
	}
	if hub.Code(expiredErr) != ErrTokenExpired.Code {
		t.Errorf("an expired token answered %q", hub.Code(expiredErr))
	}
	if hub.Code(neverErr) != ErrNoSuchToken.Code {
		t.Errorf("a token that never existed answered %q", hub.Code(neverErr))
	}
}

// TestDelegationCannotWidenAndDiesWithItsParent is criterion 17.
func TestDelegationCannotWidenAndDiesWithItsParent(t *testing.T) {
	a, _ := testAuthority(t)

	// Alice signs in with read+write only, then tries to hand her AI something wider.
	parent := signIn(t, a, "alice", "laptop", hub.ScopeRead, hub.ScopeWrite)

	_, err := a.Delegate(parent.Secret, SignInRequest{Scopes: []hub.Scope{hub.ScopePublish}, Label: "my AI"})
	if hub.Code(err) != hub.ErrGrantWiderThanHolder.Code {
		t.Errorf("delegating publish from a token that has none answered %q; criterion 17 says a "+
			"delegated token never carries a capability absent from its parent", hub.Code(err))
	}
	if n := len(a.Sessions("alice")); n != 1 {
		t.Errorf("%d sessions exist after a refused delegation; only the parent should", n)
	}

	child, err := a.Delegate(parent.Secret, SignInRequest{Scopes: []hub.Scope{hub.ScopeRead}, Label: "my AI"})
	if err != nil {
		t.Fatalf("delegating read from a read+write token should be permitted: %v", err)
	}
	if _, err := a.Authenticate(child.Secret); err != nil {
		t.Fatalf("the delegated token does not work: %v", err)
	}

	if err := a.Revoke("alice", parent.ID); err != nil {
		t.Fatalf("revoking the parent: %v", err)
	}
	if _, err := a.Authenticate(child.Secret); hub.Code(err) != ErrTokenRevoked.Code {
		t.Errorf("a delegated token still works after its parent grant was revoked (%q); "+
			"criterion 17 forbids it outliving the revocation", hub.Code(err))
	}
}

// TestEverySessionIsListedIncludingOneNeverUsed is criteria 18, 19 and 24.
func TestEverySessionIsListedIncludingOneNeverUsed(t *testing.T) {
	a, c := testAuthority(t)

	used := signIn(t, a, "alice", "used", hub.ScopeRead)
	never := signIn(t, a, "alice", "never used", hub.ScopeRead)
	unknown := signIn(t, a, "alice", "unknown last use", hub.ScopeRead)
	noScope := signIn(t, a, "alice", "no scope recorded", hub.ScopeRead)

	c.add(time.Minute)
	if _, err := a.Authenticate(used.Secret); err != nil {
		t.Fatalf("using the first token: %v", err)
	}
	a.SetLastUseUndetermined(unknown.ID)
	a.ForgetScopeRecord(noScope.ID)

	views := map[TokenID]SessionView{}
	for _, v := range a.Sessions("alice") {
		views[v.ID] = v
	}
	for _, id := range []TokenID{used.ID, never.ID, unknown.ID, noScope.ID} {
		if _, ok := views[id]; !ok {
			t.Fatalf("session %s is missing from the listing; criterion 18 says every one is listed", id)
		}
	}

	// CRITERION 19, PAIRWISE. A real timestamp, "never used" and undetermined are three renderings
	// and none may equal another.
	assertPairwiseDistinct(t, map[string]string{
		"a real timestamp": views[used.ID].LastUse.Render(),
		"never used":       views[never.ID].LastUse.Render(),
		"undetermined":     views[unknown.ID].LastUse.Render(),
	})
	if views[never.ID].LastUse.State != tri.No {
		t.Errorf("a session nobody has used renders as %v; criterion 18 says it is shown as never "+
			"used — a determined negative, not an absence", views[never.ID].LastUse.State)
	}
	if views[never.ID].LastUse.Render() == views[unknown.ID].LastUse.Render() {
		t.Error("'never used' and 'could not be determined' render identically")
	}

	// CRITERION 24. Three scope renderings: absent, empty, real.
	assertPairwiseDistinct(t, map[string]string{
		"no scope recorded": views[noScope.ID].Scopes.Render(),
		"an empty list":     RecordedScopes([]hub.Scope{}).Render(),
		"a real scope":      views[used.ID].Scopes.Render(),
	})
	for name, r := range map[string]string{
		"no scope recorded": views[noScope.ID].Scopes.Render(),
		"an empty list":     RecordedScopes([]hub.Scope{}).Render(),
		"a real scope":      views[used.ID].Scopes.Render(),
	} {
		if strings.TrimSpace(r) == "" {
			t.Errorf("%s renders as empty output, which is silence and not an answer", name)
		}
	}
}

// TestRevokingSomethingThatWasNeverYours refuses without leaking a way to end other people's
// sessions, and keeps its own code.
func TestRevokingSomebodyElsesSessionIsRefused(t *testing.T) {
	a, _ := testAuthority(t)
	alices := signIn(t, a, "alice", "laptop", hub.ScopeRead)

	if err := a.Revoke("bob", alices.ID); hub.Code(err) != ErrNotYourSession.Code {
		t.Errorf("bob ending alice's session answered %q", hub.Code(err))
	}
	if _, err := a.Authenticate(alices.Secret); err != nil {
		t.Errorf("alice's session stopped working after bob's refused attempt: %v", err)
	}
	if err := a.Revoke("alice", "a token id nobody ever minted"); hub.Code(err) != ErrNoSuchToken.Code {
		t.Errorf("revoking a token that never existed answered %q", hub.Code(err))
	}
}

// TestDeactivatingAPersonEndsEverySessionInTheirName is Issue #22's dependency, driven here
// because this is where the sessions are.
func TestDeactivatingAPersonEndsEverySessionInTheirName(t *testing.T) {
	a, _ := testAuthority(t)
	one := signIn(t, a, "alice", "one", hub.ScopeRead)
	two := signIn(t, a, "alice", "two", hub.ScopePublish)
	bobs := signIn(t, a, "bob", "bob's", hub.ScopeRead)

	if n := a.DeactivatePerson("alice"); n != 2 {
		t.Errorf("deactivating alice ended %d sessions; she had 2", n)
	}
	for _, s := range []Issued{one, two} {
		if _, err := a.Authenticate(s.Secret); hub.Code(err) != ErrTokenRevoked.Code {
			t.Errorf("a session held in a deactivated person's name still answers %q", hub.Code(err))
		}
	}
	if _, err := a.Authenticate(bobs.Secret); err != nil {
		t.Errorf("deactivating alice ended bob's session: %v", err)
	}
}

// TestTokenIdsAreUnguessable is the note-id reasoning carried across (see material.go).
func TestTokenIdsAreUnguessable(t *testing.T) {
	a, _ := testAuthority(t)
	seen := map[TokenID]bool{}
	for i := 0; i < 64; i++ {
		iss := signIn(t, a, "alice", "s", hub.ScopeRead)
		if seen[iss.ID] {
			t.Fatalf("token id %s was minted twice", iss.ID)
		}
		seen[iss.ID] = true
		if len(iss.ID) != idBytes*2 {
			t.Fatalf("token id %q is %d characters; %d bits of entropy is %d hex characters",
				iss.ID, len(iss.ID), idBytes*8, idBytes*2)
		}
		// NOT ENUMERABLE. The specific failure being guarded is a counter — `alice-grant-1` — so
		// the assertion is that no id contains a small decimal that walks with the loop.
		if strings.Contains(string(iss.ID), "-") {
			t.Fatalf("token id %q looks structured; an id a person can pattern-match is an id they "+
				"can guess a neighbour of", iss.ID)
		}
	}
}

// assertPairwiseDistinct compares every rendering against every OTHER rendering.
//
// COMPARED AGAINST EACH OTHER, NOT AGAINST LITERALS. The defect these criteria describe is two
// outcomes becoming the same, and a test asserting `got == "expected string"` passes happily while
// two of them are equal to each other and to the literal.
func assertPairwiseDistinct(t *testing.T, renderings map[string]string) {
	t.Helper()
	names := make([]string, 0, len(renderings))
	for n := range renderings {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := renderings[names[i]], renderings[names[j]]
			if a == b {
				t.Errorf("%q and %q both render as %q; they are different facts and must not "+
					"be reported identically", names[i], names[j], a)
			}
		}
	}
}
