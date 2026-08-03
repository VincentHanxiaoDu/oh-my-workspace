package auth

import (
	"errors"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// HubEnv is the environment variable that configures a hub. The same one `omw visibility` reads;
// there is one hub setting, not one per command.
const HubEnv = "OMW_HUB"

// HubConfigured reports whether a hub is configured, from one reader.
//
// EVERY SURFACE CALLS THIS ONE FUNCTION, including the daemon. Criterion 23 says the CLI and the
// control API report the same auth state for the same machine at the same moment, and two readers
// of the same variable with slightly different trimming is precisely how that stops being true.
func HubConfigured(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	return strings.TrimSpace(getenv(HubEnv)) != ""
}

// Hub is the seam between a client and a hub.
//
// WHAT IS REAL AND WHAT IS NOT, SAID HERE SO NOBODY HAS TO INFER IT: [Authority] is a real
// implementation of the hub's behaviour, and [Unreachable] is what the shipped client actually
// gets, because NO TRANSPORT TO A REMOTE HUB EXISTS IN THIS BUILD. Tests substitute an Authority
// through this interface. That fakes the WIRE. It does not fake device codes, token minting,
// revocation, expiry or the scope decision — all of those are the real code, running.
type Hub interface {
	StartSignIn(SignInRequest) (DeviceAuthorization, error)
	Redeem(DeviceAuthorization) (Issued, error)
	Authenticate(Secret) (hub.Grant, error)
	Sessions(hub.PersonID) ([]SessionView, error)
	Revoke(hub.PersonID, TokenID) error
}

// Direct wraps an [Authority] as a [Hub], for a caller holding one in the same process.
func Direct(a *Authority) Hub { return direct{a} }

type direct struct{ a *Authority }

func (d direct) StartSignIn(r SignInRequest) (DeviceAuthorization, error) { return d.a.StartSignIn(r) }
func (d direct) Redeem(x DeviceAuthorization) (Issued, error)             { return d.a.Redeem(x) }
func (d direct) Authenticate(s Secret) (hub.Grant, error)                 { return d.a.Authenticate(s) }
func (d direct) Sessions(p hub.PersonID) ([]SessionView, error)           { return d.a.Sessions(p), nil }
func (d direct) Revoke(p hub.PersonID, id TokenID) error                  { return d.a.Revoke(p, id) }

// Unreachable is the [Hub] this build ships with: a hub is configured and there is no way to talk
// to it, because the client-to-hub transport is not written yet.
//
// IT IS STATED, NOT STUBBED SILENTLY, and every one of its answers is hub.ErrHubUnreachable —
// which renders as UNDETERMINED and exits ExitUndetermined, never as "not signed in". That is both
// the honest answer for this build and exactly the path a genuinely unreachable hub will take once
// a transport exists, so nothing about the surfaces changes when one arrives.
type Unreachable struct{}

func (Unreachable) StartSignIn(SignInRequest) (DeviceAuthorization, error) {
	return DeviceAuthorization{}, hub.ErrHubUnreachable
}
func (Unreachable) Redeem(DeviceAuthorization) (Issued, error) {
	return Issued{}, hub.ErrHubUnreachable
}
func (Unreachable) Authenticate(Secret) (hub.Grant, error) {
	return hub.Grant{}, hub.ErrHubUnreachable
}
func (Unreachable) Sessions(hub.PersonID) ([]SessionView, error) { return nil, hub.ErrHubUnreachable }
func (Unreachable) Revoke(hub.PersonID, TokenID) error           { return hub.ErrHubUnreachable }

// State is the answer to "is this machine signed in", in the form every surface renders.
//
// FOUR FACTS, NOT TWO, and criterion 28 names three of them explicitly: no hub configured, a hub
// configured but unreachable, and signed out. The fourth is signed in. Signed is three-valued on
// top of that because "unreachable" is not a negative.
type State struct {
	// Signed is yes / no / could not be determined.
	Signed tri.Value
	// Code is the stable machine-readable code. A script reads this, never the prose.
	Code string
	// Text is the one-line rendering. NEVER EMPTY on any path: empty output is the fourth thing
	// criterion 22 says a consumer must be able to tell this apart from.
	Text string
	// Detail says more, and is empty only when there is nothing more to say.
	Detail string
	// TokenID and Scopes describe the credential when there is one.
	TokenID TokenID
	Scopes  ScopeSet
}

// Codes this package's states carry that are not errors.
const (
	// CodeSignedIn is the affirmative. It is a code rather than the absence of one so that a
	// script matching on `code:` never has to treat "no code" as meaning success.
	CodeSignedIn = "signed-in"
)

// Observe answers the sign-in question for a machine, and is THE ONE PLACE THAT ANSWERS IT.
//
// The CLI calls it. The daemon calls it for the control API. That is criterion 23: not two
// implementations kept in agreement by a test, but one implementation that the test then confirms
// both surfaces reach.
//
// IT NEVER INITIATES A SIGN-IN (criteria 1 and 2). It reads a credential and, if a hub is
// configured and there is one to read, asks whether it still works. There is no branch here that
// mints anything, and Save is not called from this file.
//
// WITH NO HUB CONFIGURED IT DOES NOT TOUCH h AT ALL (criterion 26). The check is first, before any
// possible call into the seam, so "no network connection without a hub configured" is a property
// of the control flow rather than of every implementation of Hub behaving.
func Observe(storeRoot string, hubConfigured bool, h Hub) State {
	if !hubConfigured {
		// A DETERMINED FACT ABOUT THIS MACHINE, and its own one. Not "signed out" — there is
		// nothing to be signed out OF — and not undetermined, because nothing failed.
		return State{
			Signed: tri.No,
			Code:   hub.ErrNoHubConfigured.Code,
			Text:   "no hub configured",
			Detail: "there is no hub to sign in to, so nothing on this machine is signed in to one; every local capability still works (PRD §4.4)",
		}
	}

	cred, err := Load(storeRoot)
	switch {
	case errors.Is(err, errNoCredential):
		return State{
			Signed: tri.No,
			Code:   ErrNotSignedIn.Code,
			Text:   "not signed in",
			Detail: "a hub is configured and this machine holds no credential for it; run 'omw auth sign-in' to sign in on purpose",
		}
	case err != nil:
		return State{
			Signed: tri.Undetermined,
			Code:   hub.Code(err),
			Text:   "sign-in state " + tri.Undetermined.String(),
			Detail: err.Error(),
		}
	}

	base := State{TokenID: cred.TokenID, Scopes: RecordedScopes(cred.Scopes)}
	if h == nil {
		h = Unreachable{}
	}
	g, err := h.Authenticate(cred.Secret)
	if err != nil {
		code := hub.Code(err)
		switch code {
		case ErrTokenRevoked.Code, ErrTokenExpired.Code, ErrNoSuchToken.Code:
			// DETERMINED NEGATIVES, each keeping its own code. The hub answered; this machine is
			// not signed in, and the three reasons are three different things to do about it.
			base.Signed = tri.No
			base.Code = code
			base.Text = "not signed in: " + strings.TrimSuffix(strings.TrimPrefix(err.Error(), "refused: "), "\n")
			base.Detail = "a credential is present on this machine and the hub does not honour it"
			return base
		}
		// EVERYTHING ELSE IS UNDETERMINED, and an unreachable hub is the common case of it. A
		// credential that could not be confirmed is not a credential that was rejected.
		base.Signed = tri.Undetermined
		base.Code = code
		if base.Code == "" {
			base.Code = ErrSignInUndetermined.Code
		}
		base.Text = "sign-in state " + tri.Undetermined.String()
		base.Detail = err.Error()
		return base
	}

	base.Signed = tri.Yes
	base.Code = CodeSignedIn
	base.Text = "signed in as " + string(g.Holder)
	// THE SCOPE REPORTED IS THE SCOPE THE TOKEN HAS (criterion 16). It comes back from the hub's
	// answer, not from what the credential file remembers being asked for — those are the two
	// values criterion 16 forbids from differing, so the authoritative one is the one printed.
	base.Scopes = RecordedScopes(g.Scopes)
	return base
}
