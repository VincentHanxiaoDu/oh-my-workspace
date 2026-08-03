package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// Where a signed-in machine keeps its credential, inside the store.
//
// INSIDE THE STORE, not in a dotfile in $HOME. The store is already the sole home of a person's
// unpublished material (PRD §3.14) and it is already the thing every surface resolves the same way;
// a credential somewhere else would be a second location to back up, to point at, and to get wrong
// when a person moves their store.
const (
	authDir        = "auth"
	credentialFile = "credential.json"
	// ownerOnlyDirMode and ownerOnlyFileMode: a credential another user on the machine can read is
	// not a credential. These are asserted by a test, not merely passed to MkdirAll — a umask sits
	// between the two.
	ownerOnlyDirMode  os.FileMode = 0o700
	ownerOnlyFileMode os.FileMode = 0o600
)

// Credential is what a signed-in machine has.
//
// THE SECRET IS A [Secret], so a Credential cannot be printed into a log by accident either — the
// redaction rides along through the struct.
type Credential struct {
	TokenID   TokenID      `json:"token_id"`
	Person    hub.PersonID `json:"person"`
	Scopes    []hub.Scope  `json:"scopes"`
	ExpiresAt time.Time    `json:"expires_at"`
	Secret    Secret       `json:"-"`
	// SecretMaterial is the only place the material is serialised, and it exists as a separate
	// field so that the `json:"-"` above is what happens to every OTHER use of the struct. A
	// reviewer reading this type sees exactly one field that can carry the secret anywhere.
	SecretMaterial string `json:"secret"`
}

// CredentialPath is where a store's credential lives.
func CredentialPath(storeRoot string) string {
	return filepath.Join(storeRoot, authDir, credentialFile)
}

// Save writes the credential, owner-only.
//
// IT IS THE ONLY FUNCTION IN THE PRODUCT THAT BRINGS A CREDENTIAL INTO EXISTENCE ON DISK, and
// criterion 2 is enforced as a structural test over that fact: nothing but the sign-in command
// calls it. "Nothing signs in silently" is not a property of good intentions spread over twenty
// files; it is a property of one file being called from one place.
//
// Written to a temporary file and renamed, so a crash mid-write leaves either the old credential
// or none — never half of one, which would be a credential that reads as "present and unreadable"
// and therefore as UNDETERMINED forever.
func Save(storeRoot string, c Credential) error {
	dir := filepath.Join(storeRoot, authDir)
	if err := os.MkdirAll(dir, ownerOnlyDirMode); err != nil {
		return fmt.Errorf("the credential directory could not be created: %w", err)
	}
	// MkdirAll respects the umask, so the mode above is a request. This is the guarantee.
	if err := os.Chmod(dir, ownerOnlyDirMode); err != nil {
		return fmt.Errorf("the credential directory could not be made owner-only: %w", err)
	}
	c.SecretMaterial = c.Secret.Expose()
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("the credential could not be encoded: %w", err)
	}
	tmp := CredentialPath(storeRoot) + ".tmp"
	if err := os.WriteFile(tmp, body, ownerOnlyFileMode); err != nil {
		return fmt.Errorf("the credential could not be written: %w", err)
	}
	if err := os.Chmod(tmp, ownerOnlyFileMode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("the credential could not be made owner-only: %w", err)
	}
	if err := os.Rename(tmp, CredentialPath(storeRoot)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("the credential could not be put in place: %w", err)
	}
	return nil
}

// errNoCredential means this machine has never signed in. A DETERMINED fact, distinguished from a
// read failure by being its own error.
var errNoCredential = errors.New("no credential on this machine")

// Load reads the credential.
//
// THREE OUTCOMES, NOT TWO. Absent is a determined "not signed in"; present-and-unparseable is
// UNDETERMINED and returns ErrCredentialUnreadable. Collapsing the second into the first is the
// §4.3 defect in its most tempting form — the code is shorter and the answer is wrong exactly when
// a person most needs it to be right.
func Load(storeRoot string) (*Credential, error) {
	body, err := os.ReadFile(CredentialPath(storeRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoCredential
	}
	if err != nil {
		return nil, hub.Refusedf(ErrCredentialUnreadable, "%v", err)
	}
	var c Credential
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, hub.Refusedf(ErrCredentialUnreadable, "it could not be parsed: %v", err)
	}
	c.Secret = SecretFromStored(c.SecretMaterial)
	c.SecretMaterial = ""
	if c.Secret.Empty() || c.TokenID == "" {
		return nil, hub.Refusedf(ErrCredentialUnreadable, "it carries no token")
	}
	return &c, nil
}

// Forget removes the credential. Signing out locally; it does NOT end the session at the hub,
// which is [Authority.Revoke]'s job, and the sign-out command says so rather than implying the
// token is dead.
func Forget(storeRoot string) error {
	err := os.Remove(CredentialPath(storeRoot))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
