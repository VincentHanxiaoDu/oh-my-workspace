package agentapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// GrantKind is the record kind grants are stored under.
//
// DECLARED AS A TYPED CONSTANT RATHER THAN A CONVERSION. `store.Kind("agent-grant")` is a call
// expression, and TestTheAgentAPIDoesNotReimplementVisibility bans a call to anything named `Kind`
// anywhere in this package's product files — because `v.Kind()` on a [hub.Visibility] is the first
// line of a second visibility rule, which is the hazard Issue #12's package comment names. That
// check cannot tell a store record-kind conversion from a visibility inspection, and the right
// response was to stop writing the conversion rather than to teach the check about exceptions: an
// exception list is how the next `v.Kind()` gets waved through.
const GrantKind store.Kind = "agent-grant"

// grantRecord is one grant on disk.
//
// REVOCATION IS A FIELD, NOT A DELETION. A deleted record is indistinguishable from one that was
// never issued, and criterion 9 asks that a revoked grant be refused — which a person then wants to
// see is a revocation rather than a typo. [ErrGrantRevoked] and [ErrUnknownGrant] are two codes
// because this is two facts.
type grantRecord struct {
	Format  int      `json:"format"`
	ID      string   `json:"id"`
	Holder  string   `json:"holder"`
	Scopes  []string `json:"scopes"`
	Revoked bool     `json:"revoked"`
}

const grantFormat = 1

// StoreGrants is the grant ledger, kept in the local store so that a grant survives a daemon
// restart and a revocation is not undone by one.
//
// IT DELEGATES THE DECISION. Issue is [hub.EvaluateGrantRequest] followed by a write, in that
// order, so a refused request writes nothing — Issue #12's criterion 11, kept by calling the
// function rather than by re-deriving the §4.5 rule here. This type owns persistence and
// revocation; it owns no part of the authority rule.
type StoreGrants struct{ s *store.Store }

// NewStoreGrants returns the ledger backed by s.
func NewStoreGrants(s *store.Store) *StoreGrants { return &StoreGrants{s: s} }

// Issue evaluates and records.
func (g *StoreGrants) Issue(h hub.Holder, requested []hub.Scope) (hub.Grant, error) {
	scopes, err := hub.EvaluateGrantRequest(h, requested)
	if err != nil {
		// NOTHING WRITTEN. The evaluation is before the id is even minted.
		return hub.Grant{}, err
	}
	id, err := newGrantID()
	if err != nil {
		return hub.Grant{}, err
	}
	rec := grantRecord{Format: grantFormat, ID: id, Holder: string(h.Person), Scopes: scopeTexts(scopes)}
	if err := g.s.PutJSON(GrantKind, id, rec); err != nil {
		return hub.Grant{}, err
	}
	return hub.Grant{ID: hub.GrantID(id), Holder: h.Person, Scopes: scopes}, nil
}

// Lookup reads a grant back.
//
// A MISSING RECORD IS GrantUnknown WITH NO ERROR; AN UNREADABLE ONE IS AN ERROR. Those become a
// refusal and an undetermined answer respectively, and collapsing them would turn "I could not read
// the ledger" into "you have no such authority" — the confident negative built from a failure that
// this project's first convention forbids.
func (g *StoreGrants) Lookup(id hub.GrantID) (hub.Grant, GrantState, error) {
	var rec grantRecord
	err := g.s.GetJSON(GrantKind, string(id), &rec)
	switch {
	case errors.Is(err, store.ErrRecordNotFound), errors.Is(err, store.ErrNotFound),
		errors.Is(err, store.ErrInvalidName), errors.Is(err, os.ErrNotExist):
		// A DETERMINED ABSENCE. There is no such record, or the id could not name one — either way
		// the ledger answered, and the answer is "not a grant I issued".
		return hub.Grant{}, GrantUnknown, nil
	case err != nil:
		return hub.Grant{}, GrantUnknown, err
	}
	scopes := make([]hub.Scope, 0, len(rec.Scopes))
	for _, s := range rec.Scopes {
		sc := hub.Scope(s)
		if !hub.KnownScope(sc) {
			// A RECORD NAMING A SCOPE THIS BUILD DOES NOT KNOW IS NOT A GRANT WE CAN HONOUR, and it
			// is not a determined absence either — we cannot tell what it permits.
			return hub.Grant{}, GrantUnknown, hub.Refusedf(hub.ErrUnknownScope, "grant %q records %q", string(id), s)
		}
		scopes = append(scopes, sc)
	}
	if rec.Revoked {
		return hub.Grant{ID: id, Holder: hub.PersonID(rec.Holder), Scopes: scopes}, GrantRevoked, nil
	}
	return hub.Grant{ID: id, Holder: hub.PersonID(rec.Holder), Scopes: scopes}, GrantLive, nil
}

// Revoke marks a grant revoked.
func (g *StoreGrants) Revoke(id hub.GrantID) error {
	var rec grantRecord
	err := g.s.GetJSON(GrantKind, string(id), &rec)
	switch {
	case errors.Is(err, store.ErrRecordNotFound), errors.Is(err, store.ErrNotFound),
		errors.Is(err, store.ErrInvalidName), errors.Is(err, os.ErrNotExist):
		// Revoking something that was never issued leaves the world in the state the person asked
		// for. Not an error, and not a claim that a grant existed.
		return nil
	case err != nil:
		return err
	}
	rec.Revoked = true
	return g.s.PutJSON(GrantKind, rec.ID, rec)
}

// newGrantID mints an unguessable id.
//
// 128 BITS FROM crypto/rand, matching the owner's ruling for note ids. A sequential grant id would
// let anything that can reach the socket try `-grant-1`; the socket is owner-only, so that is
// defence in depth rather than the only line — but a dense id space is exactly the assumption the
// note-id ruling forbids depending on, and a grant is a more attractive thing to guess than a note.
func newGrantID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "grant-" + hex.EncodeToString(b[:]), nil
}
