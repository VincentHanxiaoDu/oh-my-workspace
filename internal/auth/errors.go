package auth

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"

// The refusals and failures this package can produce.
//
// THEY REUSE [hub.Error] RATHER THAN DEFINING A SECOND ERROR TYPE. A script reading `omw` output
// already learns one thing — a stable `code:` it can match without parsing prose — and a second
// error shape with its own conventions would make that two things. [hub.Code] walks wrapped errors,
// so every one of these is readable through an errors.As from any surface.
//
// THE POINT OF ALMOST EVERY ONE OF THESE IS THAT IT IS NOT ONE OF THE OTHERS. Issue #19 asks, over
// and over, that two outcomes a person would confuse be told apart: revoked from never-existed,
// expired from revoked, "no hub" from "hub unreachable" from "signed out", a replayed device code
// from a hub that did not answer. TestEveryAuthOutcomeIsPairwiseDistinguishable asserts that
// pairwise over this list, comparing the renderings AGAINST EACH OTHER rather than against string
// literals a test author chose — a literal assertion passes happily while two of them are equal.
var (
	// ErrNotSignedIn is a DETERMINED negative: this machine has no credential, and that was
	// established, not guessed. Its exit code is not ExitUndetermined's.
	ErrNotSignedIn = &hub.Error{Code: "not-signed-in", Msg: "not signed in to the hub"}

	// ErrSignInUndetermined is the third answer: a credential exists and whether it is still good
	// could not be established. NOT a "no" (§4.3, criterion 22).
	ErrSignInUndetermined = &hub.Error{Code: "sign-in-undetermined", Msg: "whether this machine is signed in could not be determined"}

	// ErrNoSuchToken — no token with that id has ever existed.
	//
	// DISTINCT FROM ErrTokenRevoked, and criterion 20 read with §4.3 requires it: "a revoked token
	// and a token that never existed are different facts". A person who revoked a session and sees
	// "never existed" would reasonably conclude their revocation did not take.
	ErrNoSuchToken = &hub.Error{Code: "no-such-token", Msg: "refused: no token with that id has ever existed"}

	// ErrTokenRevoked — the token existed, was good, and was ended by its person.
	ErrTokenRevoked = &hub.Error{Code: "token-revoked", Msg: "refused: that token was revoked"}

	// ErrTokenExpired — the token existed and its life ran out. Nobody ended it; it aged out.
	// Distinct from ErrTokenRevoked because the remedy differs: sign in again versus find out who
	// ended it and why.
	ErrTokenExpired = &hub.Error{Code: "token-expired", Msg: "refused: that token has expired"}

	// ErrSignInPending — a device code has been issued and NOBODY HAS COMPLETED THE BROWSER STEP.
	// Criterion 5: this is the state in which no token exists, and it must not read as a failure
	// of the hub nor as a refusal.
	ErrSignInPending = &hub.Error{Code: "sign-in-pending", Msg: "the sign-in has not been completed in a browser yet, so no token exists"}

	// ErrDeviceCodeExpired — criterion 6. The code was never completed and is now dead; it cannot
	// later turn into a token.
	ErrDeviceCodeExpired = &hub.Error{Code: "device-code-expired", Msg: "refused: that device code expired before anyone completed the browser step"}

	// ErrDeviceCodeRedeemed — criterion 7. A code is redeemable ONCE. A replay mints nothing.
	ErrDeviceCodeRedeemed = &hub.Error{Code: "device-code-already-redeemed", Msg: "refused: that device code was already redeemed and cannot be replayed"}

	// ErrNoSuchDeviceCode — no sign-in was ever started with that code.
	ErrNoSuchDeviceCode = &hub.Error{Code: "no-such-device-code", Msg: "refused: no sign-in was ever started with that device code"}

	// ErrNotYourSession — a person tried to end a session signed in as somebody else. Criterion 20
	// is about ending YOUR OWN sessions; nothing here is a route to ending anybody else's.
	ErrNotYourSession = &hub.Error{Code: "not-your-session", Msg: "refused: that session was not signed in as you"}

	// ErrControlAPINotOpen — PRD §4.6 and criterion 29. Where owner-only socket permissions cannot
	// be confirmed the control API does not open, and a surface that needs it SAYS SO rather than
	// proceeding. Its own code because it is neither success nor "the daemon is not running".
	ErrControlAPINotOpen = &hub.Error{Code: "control-api-not-open", Msg: "the control API is not open, so this could not be answered through it"}

	// ErrUnknownPerson — the hub has no record of the person a sign-in names. A refusal, and not
	// the same one as a scope refusal.
	ErrUnknownPerson = &hub.Error{Code: "unknown-person", Msg: "refused: the hub has no record of that person"}

	// ErrCredentialUnreadable — a credential file is present and could not be read or parsed.
	// UNDETERMINED territory. It is emphatically not "not signed in": something is there.
	ErrCredentialUnreadable = &hub.Error{Code: "credential-unreadable", Msg: "a credential is present on this machine and could not be read"}
)

// allAuthErrors is every error this package defines, for the pairwise test. Adding an error means
// adding it here; the test is worthless against a list that does not contain the new one.
var allAuthErrors = []*hub.Error{
	ErrNotSignedIn, ErrSignInUndetermined, ErrNoSuchToken, ErrTokenRevoked, ErrTokenExpired,
	ErrSignInPending, ErrDeviceCodeExpired, ErrDeviceCodeRedeemed, ErrNoSuchDeviceCode,
	ErrNotYourSession, ErrControlAPINotOpen, ErrUnknownPerson, ErrCredentialUnreadable,
}

// borrowedErrors are the errors from `internal/hub` that auth surfaces also produce. They are in
// the pairwise test too: criterion 28's three facts are spread across both packages
// (ErrNoHubConfigured and ErrHubUnreachable live there, ErrNotSignedIn lives here), and a pairwise
// check that did not span the two would be checking the easy half.
var borrowedErrors = []*hub.Error{
	hub.ErrNoHubConfigured, hub.ErrHubUnreachable, hub.ErrDaemonNotRunning,
	hub.ErrGrantWiderThanHolder, hub.ErrUnknownScope,
}
