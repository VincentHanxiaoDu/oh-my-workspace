package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
)

// TokenID names a session. It is UNGUESSABLE, and that is a decision, not a default.
//
// The obvious alternative is what `internal/hub`'s [hub.Ledger] does today for grants —
// `alice-grant-1`, `alice-grant-2` — and it is fine for a ledger nobody outside the process sees.
// It is not fine here. A token id is the thing criterion 20 has a person type in order to END a
// session, so it travels: into shell history, into a colleague's message, into a ticket. A
// sequential id also answers a question nobody asked — how many sessions this person has, and in
// what order — and it lets anybody who has seen one id name a neighbouring one. 128 bits from
// crypto/rand costs nothing and closes both.
//
// (The same reasoning is on record for note ids. When this branch was cut, `internal/hub/store.go`
// still minted `note-1`, `note-2`; whoever unifies that should take this file's approach, and the
// pull request for this Issue says so rather than reaching into another Issue's file.)
type TokenID string

// deviceCodeSecret is the code a client polls with. Unguessable for the same reason a token is:
// between issuance and redemption it is the only thing standing between a stranger and a token
// somebody else is about to be granted.
type deviceCodeSecret string

// idBytes is 128 bits. Not tuned; simply past any brute-force worth discussing, and short enough
// to paste.
const idBytes = 16

// secretBytes is 256 bits of token material.
const secretBytes = 32

// Secret is token material — the thing that authenticates. IT CANNOT BE PRINTED BY ACCIDENT.
//
// The field is unexported and String redacts, so every fmt verb, every %v in a log line and every
// error that wraps one renders the redaction. Exposing it takes [Secret.Expose] and a deliberate
// keystroke, and exactly one caller in the product does that: the credential file's writer.
//
// WHY A TYPE AND NOT A CONVENTION. Issue #9 drove the equivalent rule for model keys by grepping
// output streams for a recognisable value, and this package's tests do the same (see
// TestNoSurfaceEverPrintsTheTokenSecret). But a grep test only covers the paths a test drives. The
// type covers the paths nobody thought to drive, including ones added later.
type Secret struct{ v string }

// redaction is what a Secret renders as. One constant so that no surface can print a different
// placeholder that a reader might mistake for the material itself.
const redaction = "«token secret withheld»"

// String redacts. So do GoString and Format, because %#v and %q reach past String otherwise.
func (s Secret) String() string { return redaction }

// GoString redacts, for %#v.
func (s Secret) GoString() string { return redaction }

// Format redacts for every verb, including %q and %x, which do NOT consult String for a struct.
func (s Secret) Format(f fmt.State, verb rune) { _, _ = f.Write([]byte(redaction)) }

// Expose returns the material. Call it only where the material is genuinely required — writing the
// credential file, and presenting the token to the hub.
func (s Secret) Expose() string { return s.v }

// Empty reports whether there is no material here.
func (s Secret) Empty() bool { return s.v == "" }

// SecretFromStored rebuilds a Secret from the credential file. Not named "New": nothing outside a
// mint and a file read should be constructing one.
func SecretFromStored(v string) Secret { return Secret{v: v} }

// newRandomHex returns n bytes of crypto/rand as hex, or the error that stopped it.
//
// AN ERROR IS RETURNED, NEVER SWALLOWED. A failing entropy source that fell back to something
// weaker — or to a zero value — would produce a guessable token while every test still passed,
// which is the worst available outcome for this file.
func newRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("the system's entropy source could not be read, so no unguessable value was minted: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func newTokenID() (TokenID, error) {
	s, err := newRandomHex(idBytes)
	return TokenID(s), err
}

func newSecret() (Secret, error) {
	s, err := newRandomHex(secretBytes)
	return Secret{v: s}, err
}

func newDeviceCodeSecret() (deviceCodeSecret, error) {
	s, err := newRandomHex(idBytes)
	return deviceCodeSecret(s), err
}

// userCodeAlphabet drops the characters people confuse when reading a code off a terminal and
// typing it into a browser: I, O and 0, plus U, which keeps accidental words out. That leaves 23
// letters and the digits 1-9 — exactly the 32 symbols base32 needs.
//
// THE COUNT IS ASSERTED BY A TEST rather than left to base32.NewEncoding's panic. The first draft
// of this line had 31 symbols, and the way that surfaced was the whole test binary panicking in an
// init, which reports as a build failure and names nothing.
const userCodeAlphabet = "ABCDEFGHJKLMNPQRSTVWXYZ123456789"

var userCodeEncoding = base32.NewEncoding(userCodeAlphabet).WithPadding(base32.NoPadding)

// newUserCode returns the short code a person reads off the terminal and types into a browser.
//
// IT IS NOT A CREDENTIAL AND IT IS STILL UNGUESSABLE. It is printed on purpose — that is the whole
// device-code flow — and on its own it grants nothing: redemption needs the device code the client
// kept, and approval needs the person to be signed in to the hub in their browser. But it is what
// somebody would have to guess in order to approve a sign-in that is not theirs, so it carries 40
// bits rather than the four digits that would be convenient.
func newUserCode() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("the system's entropy source could not be read, so no sign-in code was minted: %w", err)
	}
	s := userCodeEncoding.EncodeToString(b)
	return s[:4] + "-" + s[4:], nil
}

// normaliseUserCode makes what a person typed comparable with what was printed. Case and the
// hyphen are presentation; a person who typed the code without the dash has typed the code.
func normaliseUserCode(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
}

// hashSecret is what the hub keeps. THE AUTHORITY DOES NOT STORE THE MATERIAL.
//
// Nothing in this build persists an Authority, so this is not yet protecting a database — it is
// making the shape right before there is one, and making it impossible for a dump of an
// Authority's memory to hand somebody a usable token. SHA-256 is right for a 256-bit random
// secret specifically because there is nothing to brute-force: it is not a password.
func hashSecret(s Secret) [32]byte { return sha256.Sum256([]byte(s.v)) }

// secretMatches compares in constant time. The comparison is on digests of high-entropy values, so
// a timing side channel here is not a realistic attack — it is constant time anyway because the
// variable-time version is the one somebody copies into a place where it does matter.
func secretMatches(want [32]byte, got Secret) bool {
	h := hashSecret(got)
	return subtle.ConstantTimeCompare(want[:], h[:]) == 1
}
