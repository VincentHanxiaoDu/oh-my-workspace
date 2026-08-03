package auth

import (
	"sync"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// Authority is the hub's half of sign-in: it issues device codes, mints tokens, authenticates
// them, lists them and ends them.
//
// IT IS REAL PRODUCT CODE. What this build does not have is a WIRE between a client and a remote
// one — see the package comment. Tests drive this object directly and through the [Hub] seam, and
// every behaviour Issue #19 asks to be "driven, not asserted" is driven against this.
//
// IT DOES NOT CONTAIN THE AUTHORISATION RULE. Every decision about what a grant may carry is
// [hub.EvaluateGrantRequest], called from here. Issue #12 asked for exactly that and the request is
// worth honouring literally: the rule refuses at request time and returns no narrowed alternative,
// and any second implementation of it would eventually be the lenient one.
type Authority struct {
	mu sync.Mutex

	// now is injectable so that expiry can be DRIVEN rather than slept through. A test that proves
	// expiry by sleeping proves it slowly and flakily; one that cannot move the clock usually ends
	// up not proving it at all.
	now func() time.Time

	// people is what each person can themselves do — the deployment fact against which
	// [hub.EvaluateGrantRequest] measures every request. Note what is NOT here: an operator entry.
	// The hub operator's ability to read everything is §2.4's deployment fact and is not a holding
	// anybody has in this map, because it is not expressed through the scope system at all.
	people map[hub.PersonID][]hub.Scope

	pending    map[deviceCodeSecret]*pendingSignIn
	byUserCode map[string]deviceCodeSecret

	sessions map[TokenID]*session
	byHash   map[[32]byte]TokenID
	order    []TokenID

	codeLife  time.Duration
	tokenLife time.Duration
	verifyURI string
}

// AuthorityOptions configures a hub authority.
type AuthorityOptions struct {
	// Now is the clock. Defaults to time.Now.
	Now func() time.Time
	// CodeLife is how long a device code stays redeemable. Defaults to fifteen minutes.
	CodeLife time.Duration
	// TokenLife is how long a minted token lives. Defaults to thirty days.
	TokenLife time.Duration
	// VerificationURI is where a person goes to complete a sign-in.
	VerificationURI string
}

type pendingSignIn struct {
	userCode  string
	device    deviceCodeSecret
	requested []hub.Scope
	label     string
	expiresAt time.Time

	approvedBy hub.PersonID
	approved   bool
	// refusal is a decision that has already been made and must be reported to the client the next
	// time it polls, rather than being lost. An abandoned refusal that let a later poll succeed
	// would be criterion 15's "quietly widened" defect wearing a different hat.
	refusal  error
	redeemed bool
}

type session struct {
	id        TokenID
	person    hub.PersonID
	label     string
	scopes    []hub.Scope
	hash      [32]byte
	issuedAt  time.Time
	expiresAt time.Time
	revoked   bool
	lastUse   LastUse
	parent    TokenID
	// scopeUnrecorded exists ONLY so that criterion 24's third rendering has something real to
	// come from. A hub that has a session and no scope record for it is a genuine state — a
	// partially written row, an older format — and the listing must be able to say so rather than
	// print an empty scope that reads as "no permissions".
	scopeUnrecorded bool
}

const (
	defaultCodeLife  = 15 * time.Minute
	defaultTokenLife = 30 * 24 * time.Hour
)

// NewAuthority returns an empty hub authority.
func NewAuthority(opts AuthorityOptions) *Authority {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.CodeLife <= 0 {
		opts.CodeLife = defaultCodeLife
	}
	if opts.TokenLife <= 0 {
		opts.TokenLife = defaultTokenLife
	}
	return &Authority{
		now:        opts.Now,
		people:     map[hub.PersonID][]hub.Scope{},
		pending:    map[deviceCodeSecret]*pendingSignIn{},
		byUserCode: map[string]deviceCodeSecret{},
		sessions:   map[TokenID]*session{},
		byHash:     map[[32]byte]TokenID{},
		codeLife:   opts.CodeLife,
		tokenLife:  opts.TokenLife,
		verifyURI:  opts.VerificationURI,
	}
}

// Register records what a person can themselves do.
func (a *Authority) Register(p hub.PersonID, scopes []hub.Scope) {
	a.mu.Lock()
	defer a.mu.Unlock()
	held := make([]hub.Scope, len(scopes))
	copy(held, scopes)
	a.people[p] = held
}

// DeviceAuthorization is what a sign-in command has after asking, and BEFORE anybody has approved
// anything.
//
// CRITERION 5 IS THE SHAPE OF THIS TYPE: it carries no token, because at this moment none exists.
// The only secret in it is the device code the client keeps in memory to poll with — never written
// to disk, because a credential on disk is precisely what criterion 5 says must not be there yet.
type DeviceAuthorization struct {
	// UserCode is printed for the person to type into a browser.
	UserCode string
	// VerificationURI is where they type it.
	VerificationURI string
	// ExpiresAt is when the code dies ungranted (criterion 6).
	ExpiresAt time.Time

	device deviceCodeSecret
}

// SignInRequest is what a client asks for.
type SignInRequest struct {
	// Scopes the token should carry. Named by the person, on purpose — `publish` is here only
	// because somebody typed it (criterion 13).
	Scopes []hub.Scope
	// Label is what the person will see in their session list: "this laptop", "the build box".
	Label string
}

// StartSignIn issues a device code, or refuses.
//
// WHAT IS DECIDED HERE AND WHAT IS DECIDED AT APPROVAL, AND WHY THE SPLIT IS NOT A DODGE. A device
// code flow does not know WHO is signing in until the browser step — that is the point of it. So:
//
//   - The scope NAMES are checked here, against the one vocabulary, and an unknown name is refused
//     with ErrUnknownScope before any code is minted (criteria 8 and 31). Nothing is printed, and
//     there is nothing pending to complete.
//   - Whether the person may HAVE those scopes is [hub.EvaluateGrantRequest] at [Authority.Approve],
//     because that is the first instant at which there is a holder to measure against. It is still
//     "at the moment I ask" in the sense criterion 15 means: the request FAILS, the pending sign-in
//     dies, and NO TOKEN EXISTS AFTERWARDS. Nothing is narrowed and nothing is left behind.
//
// It calls [hub.EvaluateGrantRequest] for the name check too, against a holder holding the whole
// vocabulary, rather than reimplementing "is this a known scope" — so the empty-request case and
// the unknown-name case are worded by the same function that words them everywhere else.
func (a *Authority) StartSignIn(req SignInRequest) (DeviceAuthorization, error) {
	if _, err := hub.EvaluateGrantRequest(hub.Holder{Person: "any signed-in person", Scopes: hub.Vocabulary()}, req.Scopes); err != nil {
		return DeviceAuthorization{}, err
	}

	user, err := newUserCode()
	if err != nil {
		return DeviceAuthorization{}, err
	}
	dev, err := newDeviceCodeSecret()
	if err != nil {
		return DeviceAuthorization{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	scopes := make([]hub.Scope, len(req.Scopes))
	copy(scopes, req.Scopes)
	p := &pendingSignIn{
		userCode:  user,
		device:    dev,
		requested: scopes,
		label:     req.Label,
		expiresAt: a.now().Add(a.codeLife),
	}
	a.pending[dev] = p
	a.byUserCode[normaliseUserCode(user)] = dev
	return DeviceAuthorization{
		UserCode:        user,
		VerificationURI: a.verifyURI,
		ExpiresAt:       p.expiresAt,
		device:          dev,
	}, nil
}

// Approve is the browser step: the person, already signed in to the hub in a browser, says yes to
// the code they are looking at.
//
// THIS IS WHERE §4.5 IS ENFORCED, BY CALLING [hub.EvaluateGrantRequest] AND NOTHING ELSE. A refusal
// is recorded on the pending sign-in and returned, so the client's next poll learns the request
// failed — it does not silently keep waiting, and it certainly does not later receive a narrower
// token.
func (a *Authority) Approve(userCode string, as hub.PersonID) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	dev, ok := a.byUserCode[normaliseUserCode(userCode)]
	if !ok {
		return ErrNoSuchDeviceCode
	}
	p := a.pending[dev]
	switch {
	case p == nil:
		return ErrNoSuchDeviceCode
	case p.redeemed:
		return ErrDeviceCodeRedeemed
	case !a.now().Before(p.expiresAt):
		return ErrDeviceCodeExpired
	}

	held, known := a.people[as]
	if !known {
		return ErrUnknownPerson
	}
	if _, err := hub.EvaluateGrantRequest(hub.Holder{Person: as, Scopes: held}, p.requested); err != nil {
		p.refusal = err
		return err
	}
	p.approved = true
	p.approvedBy = as
	return nil
}

// Issued is a freshly minted token. The ONLY place the material is ever handed out.
type Issued struct {
	ID     TokenID
	Person hub.PersonID
	Scopes []hub.Scope
	Secret Secret
	// ExpiresAt is when this token stops working on its own.
	ExpiresAt time.Time
}

// Redeem turns a completed device code into a token, or says precisely why it did not.
//
// THE ORDER OF THESE CASES IS LOAD-BEARING AND THEY ARE ALL DIFFERENT ERRORS (criteria 5, 6, 7).
// "Already redeemed" is checked BEFORE expiry, because a code that was used and then aged out was
// used — telling a person it expired would suggest their sign-in never happened. "Refused" is
// checked before "pending", because a decision that has been made is not a wait.
//
// Redeeming a code marks it redeemed WHETHER OR NOT THIS IS THE FIRST TIME, so a replay finds a
// dead code rather than a live one (criterion 7).
func (a *Authority) Redeem(d DeviceAuthorization) (Issued, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	p, ok := a.pending[d.device]
	if !ok || p == nil {
		return Issued{}, ErrNoSuchDeviceCode
	}
	switch {
	case p.redeemed:
		return Issued{}, ErrDeviceCodeRedeemed
	case p.refusal != nil:
		// The decision stands. The pending sign-in is dead: marking it redeemed here means a retry
		// gets the "already dealt with" answer rather than a second chance at approval.
		p.redeemed = true
		return Issued{}, p.refusal
	case !a.now().Before(p.expiresAt):
		return Issued{}, ErrDeviceCodeExpired
	case !p.approved:
		return Issued{}, ErrSignInPending
	}

	iss, err := a.mintLocked(p.approvedBy, p.requested, p.label, "")
	if err != nil {
		return Issued{}, err
	}
	p.redeemed = true
	return iss, nil
}

// mintLocked creates a session. a.mu must be held.
//
// It does NOT decide anything. Every caller has already been through [hub.EvaluateGrantRequest],
// and putting the decision here as well would be the second copy of the rule this package exists
// not to have.
func (a *Authority) mintLocked(person hub.PersonID, scopes []hub.Scope, label string, parent TokenID) (Issued, error) {
	id, err := newTokenID()
	if err != nil {
		return Issued{}, err
	}
	sec, err := newSecret()
	if err != nil {
		return Issued{}, err
	}
	held := make([]hub.Scope, len(scopes))
	copy(held, scopes)
	now := a.now()
	s := &session{
		id:        id,
		person:    person,
		label:     label,
		scopes:    held,
		hash:      hashSecret(sec),
		issuedAt:  now,
		expiresAt: now.Add(a.tokenLife),
		// CRITERION 18: a session begins DETERMINEDLY never used. Not zero-valued, not unknown.
		lastUse: NeverUsed(),
		parent:  parent,
	}
	a.sessions[id] = s
	a.byHash[s.hash] = id
	a.order = append(a.order, id)
	return Issued{ID: id, Person: person, Scopes: held, Secret: sec, ExpiresAt: s.expiresAt}, nil
}

// Authenticate turns token material into a [hub.Grant], or refuses.
//
// THE RETURN TYPE IS THE POINT. A grant is what `internal/hub`'s ReadThrough, PublishThrough and
// SetVisibilityThrough already take, so criteria 11, 12 and 14 — a `read` token cannot publish, a
// `read`+`write` token cannot publish, and a scope means the same thing on every surface — are
// satisfied by the code that already enforces them rather than by a second check here. There is no
// scope logic in this function at all, deliberately.
//
// The three refusals are three errors: revoked, expired, and never existed.
func (a *Authority) Authenticate(sec Secret) (hub.Grant, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	id, ok := a.byHash[hashSecret(sec)]
	if !ok {
		return hub.Grant{}, ErrNoSuchToken
	}
	s := a.sessions[id]
	if s == nil || !secretMatches(s.hash, sec) {
		return hub.Grant{}, ErrNoSuchToken
	}
	// REVOKED IS CHECKED BEFORE EXPIRY. A token its person ended, which then aged out, was ended;
	// reporting expiry would tell them their revocation was never the reason it stopped.
	if s.revoked {
		return hub.Grant{}, ErrTokenRevoked
	}
	if !a.now().Before(s.expiresAt) {
		return hub.Grant{}, ErrTokenExpired
	}
	// A REFUSED PRESENTATION IS NOT A USE. The last-use stamp moves only on the path that returns a
	// grant, so "last used" means "last did something", not "last was tried".
	s.lastUse = UsedAt(a.now())
	scopes := make([]hub.Scope, len(s.scopes))
	copy(scopes, s.scopes)
	return hub.Grant{ID: hub.GrantID(s.id), Holder: s.person, Scopes: scopes}, nil
}

// Delegate mints a token for something the person delegates to — their AI, a script — from a token
// they already hold.
//
// CRITERION 17, AND IT IS ONE LINE OF POLICY BECAUSE THE POLICY ALREADY EXISTS: the parent's own
// scopes are the holder, and [hub.EvaluateGrantRequest] refuses anything wider. A delegated token
// cannot carry what its parent does not, and the parent link means [Authority.Revoke] ends the
// child with the parent.
//
// Authenticating the parent first is what makes a revoked parent unable to delegate at all.
func (a *Authority) Delegate(parent Secret, req SignInRequest) (Issued, error) {
	g, err := a.Authenticate(parent)
	if err != nil {
		return Issued{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	scopes, err := hub.EvaluateGrantRequest(hub.Holder{Person: g.Holder, Scopes: g.Scopes}, req.Scopes)
	if err != nil {
		return Issued{}, err
	}
	return a.mintLocked(g.Holder, scopes, req.Label, TokenID(g.ID))
}

// Sessions lists what has been signed in as a person (PRD §3.10, criterion 18).
//
// EVERY SESSION IS LISTED, including one never used and one revoked. Ordered by issuance so the
// listing is stable, which matters for a person comparing two runs of it.
func (a *Authority) Sessions(p hub.PersonID) []SessionView {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	out := make([]SessionView, 0, len(a.order))
	for _, id := range a.order {
		s := a.sessions[id]
		if s == nil || s.person != p {
			continue
		}
		v := SessionView{
			ID: s.id, Person: s.person, Label: s.label,
			LastUse: s.lastUse, IssuedAt: s.issuedAt, Parent: s.parent,
		}
		if s.scopeUnrecorded {
			v.Scopes = NoRecordedScope()
		} else {
			v.Scopes = RecordedScopes(append([]hub.Scope(nil), s.scopes...))
		}
		switch {
		case s.revoked:
			v.Status = StatusRevoked
		case !now.Before(s.expiresAt):
			v.Status = StatusExpired
		default:
			v.Status = StatusActive
		}
		out = append(out, v)
	}
	return out
}

// Revoke ends one session, and every session delegated from it.
//
// IT ENDS ONE. Criterion 20 is precise: the other sessions continue to work. The cascade is not an
// exception to that — a delegated token was never a session in its own right, it was an extension
// of its parent, and leaving it alive would be exactly the "outlives its parent's revocation" that
// criterion 17 forbids.
//
// Revoking a token that never existed is ErrNoSuchToken, and revoking one already revoked is
// ErrTokenRevoked: three facts, three answers, and none of them silence.
func (a *Authority) Revoke(as hub.PersonID, id TokenID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	if s == nil {
		return ErrNoSuchToken
	}
	if s.person != as {
		// NOT ErrNoSuchToken. The Issue's own reasoning (see hub.ErrNoSuchNote) prefers the honest
		// distinction over the confirmation-hiding one, and consistency with it matters more than
		// re-litigating that here.
		return ErrNotYourSession
	}
	if s.revoked {
		return ErrTokenRevoked
	}
	s.revoked = true
	for _, other := range a.order {
		if c := a.sessions[other]; c != nil && c.parent == id && !c.revoked {
			c.revoked = true
		}
	}
	return nil
}

// ForgetScopeRecord drops a session's scope record while keeping the session.
//
// THIS EXISTS TO MAKE A REAL STATE REACHABLE, AND IT IS FAIR TO BE SUSPICIOUS OF IT. Criterion 24
// requires that "no recorded scope", "an empty scope list" and "a real scope" render as three
// different things, and the first of those is a state a hub genuinely lands in — a half-written
// row, a record from a format that predates the field. Without a way to produce it, that criterion
// could only ever be claimed at the rendering level, never observed in a listing.
//
// It is not reachable from any command, and no `omw` surface calls it.
func (a *Authority) ForgetScopeRecord(id TokenID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[id]; s != nil {
		s.scopeUnrecorded = true
		s.scopes = nil
	}
}

// SetLastUseUndetermined records that a session's last use could not be established.
//
// Same justification as [Authority.ForgetScopeRecord]: criterion 19's third rendering describes a
// hub that cannot say when a token was last used, and a listing test needs that state to exist.
func (a *Authority) SetLastUseUndetermined(id TokenID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[id]; s != nil {
		s.lastUse = UnknownLastUse()
	}
}

// DeactivatePerson ends every session held in a person's name (Issue #22's dependency, stated here
// because it is one line against this data and a second implementation of it elsewhere would be a
// second place for a session to survive).
func (a *Authority) DeactivatePerson(p hub.PersonID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, id := range a.order {
		if s := a.sessions[id]; s != nil && s.person == p && !s.revoked {
			s.revoked = true
			n++
		}
	}
	return n
}
