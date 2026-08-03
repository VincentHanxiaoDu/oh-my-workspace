package extension

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/refusal"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// RecordKind is the store kind a registration is written under.
const RecordKind = store.Kind("extension")

// recordFormat is the on-disk envelope version, INSIDE the store's own envelope. A record written
// by a future build is unreadable and says so; it is never listed as an extension with nothing
// known about it, because "I cannot read this" and "this is fine" are different facts (§4.3), and
// it is never dropped, because that would be criterion 14's missing entry.
const recordFormat = 1

// Registration is the record a deliberate act leaves behind.
//
// # WHAT IT DOES NOT CONTAIN
//
// A credential. Configuration for an extension is supplied the same way for both interfaces
// (criterion 4), and for a model provider that means #18's rule unchanged: what is recorded is the
// provider's name and the PATH of a file the person owns; the credential value is read from their
// environment or their file when it is used and never enters the store. [Settings] is therefore
// locations and choices, and [Register] refuses a setting whose name says it is a secret rather
// than accepting it and hoping.
type Registration struct {
	// Name is the extension's name — one path segment, and the identifier for both interfaces.
	Name string
	// Interface is which of the two the extension implemented WHEN IT WAS REGISTERED. It is
	// recorded rather than re-derived so that an extension that vanished from the machine can
	// still be listed under the interface the person registered it as, instead of vanishing from
	// the listing along with its code (criterion 14).
	Interface Interface
	// At is when the deliberate act happened.
	At time.Time
	// Settings is the configuration the person supplied, in the same shape for both interfaces
	// (criterion 4). Keys and values are locations and choices, never credentials.
	Settings map[string]string

	// readErr is why this registration could not be read, when it could not.
	//
	// UNEXPORTED, so it cannot be serialised and cannot be set from outside this package: a
	// Registration a caller built has no read error, and one this package returns carries the real
	// one. [Inventory] turns it into an [Undetermined] entry rather than dropping the extension
	// (criterion 14).
	readErr error
}

// ReadError is why this registration could not be read, or nil.
func (r Registration) ReadError() error { return r.readErr }

// SecretishKeys are setting names this package refuses to record.
//
// # WHY REFUSE RATHER THAN REDACT
//
// Criterion 22 asks that a provider's credentials never appear in the listing, in failure reasons
// or in a diagnostics bundle. Redacting at the point of DISPLAY is a rule somebody has to keep true
// on every surface, forever, including the ones not written yet (#20's bundle is not on this
// branch). Refusing at the point of RECORD means the value is never in the store, so there is
// nothing for a future surface to leak. It is the same choice #18 made by keeping credentials out
// of the store entirely, applied to the shared configuration shape.
//
// The refusal names the alternative, because a person typing `omw ext configure acme key=…` has a
// real need and must not be left with only a "no".
var SecretishKeys = []string{"key", "secret", "token", "password", "credential", "apikey", "api_key", "auth"}

// LocationSuffixes name a setting that holds WHERE something is rather than WHAT it is.
//
// # A TEST CAUGHT THAT THIS WAS MISSING, AND THE BUG WAS AN OWN GOAL
//
// Without it, `omw ext configure acme key_file=/home/me/.omw/key` was REFUSED — by a message that
// tells the person to "supply a credential through your environment or a file you own, and record
// its PATH", which is exactly what they had just done. The guard was blocking the one workflow
// §3.13 prescribes, and it was blocking it because `key_file` contains `key`.
//
// A path is not a credential. `key_file`, `token-path` and `credential_file` name locations, and
// Issue #18 already established that recording a path while never reading its bytes is the
// product's model for this: "what is recorded is the provider's name and the PATH of a file the
// person owns; the credential value is read from their environment or their file when it is used
// and never enters the store."
var LocationSuffixes = []string{"_file", "-file", "_path", "-path", "_dir", "-dir", "file", "path"}

// ErrSettingLooksLikeASecret — a setting was refused because omw takes no custody of credentials.
var ErrSettingLooksLikeASecret = &refusal.Error{
	Code: "extension-setting-looks-like-a-secret",
	Msg:  "refused: omw does not take custody of credentials, so that setting is not recorded",
}

func init() { allErrors = append(allErrors, ErrSettingLooksLikeASecret) }

// looksLikeASecret reports whether a setting name is one of [SecretishKeys], comparing on the
// name's own words so that `api-key`, `API_KEY` and `providerToken` are all caught.
func looksLikeASecret(name string) bool {
	folded := strings.ToLower(strings.TrimSpace(name))
	// A LOCATION IS CHECKED FIRST AND WINS. `key_file` is a path; `key` is a credential.
	for _, loc := range LocationSuffixes {
		if strings.HasSuffix(folded, loc) {
			return false
		}
	}
	for _, bad := range SecretishKeys {
		if strings.Contains(folded, bad) {
			return true
		}
	}
	return false
}

type recordFile struct {
	Format    int               `json:"format"`
	Name      string            `json:"name"`
	Interface string            `json:"interface"`
	At        string            `json:"at,omitempty"`
	Settings  map[string]string `json:"settings,omitempty"`
}

// Register is THE deliberate act, and it is the same act for both interfaces (criterion 1).
//
// # THE SIGNATURE IS THE CRITERION
//
// "Registering a channel adapter and registering a model provider are the same act: the same
// command, invoked with the same arguments in the same order, differing only in the extension being
// registered."
//
// So the interface is NOT a parameter. It comes from the extension itself, through
// [Registry.Find]. A caller — including the CLI — cannot pass a different argument for a channel
// than for a model, because there is no argument to differ in. Making the interface a parameter
// would have made criterion 1's test pass with `omw ext register --channel slack` and
// `omw ext register --model acme`, which is two systems wearing one command name.
//
// # IT CONTACTS NOTHING (criterion 16)
//
// It looks the name up in a map, validates, and writes a file. [Extension.Load] is NOT called:
// registering a model provider does not contact the provider's endpoint, and registering a channel
// adapter does not reach the channel. Whether it loads is answered when somebody asks the
// inventory. TestRegisteringContactsNothing drives it with an extension that records every call.
//
// # IT IS ALL OR NOTHING (criterion 19)
//
// Every refusal below happens BEFORE the single Put, so a refused registration leaves no record at
// all — never a half-registered entry. There is exactly one write in this function, which is what
// makes that assertable rather than argued.
func Register(s *store.Store, r *Registry, name string, settings map[string]string) error {
	if s == nil {
		return ErrNoStore
	}
	name = strings.TrimSpace(name)
	if !validName(name) {
		return refusal.Refusedf(ErrNotOffered,
			"%q is not usable as an extension name — one path segment of letters, digits, dash, "+
				"underscore or dot, not starting with a dot or a dash", name)
	}
	e, ok := r.Find(name)
	if !ok {
		return refusal.Refusedf(ErrNotOffered,
			"nothing on this machine offers an extension called %q; nothing was registered", name)
	}
	iface := e.Interface()
	if !iface.Valid() {
		return refusal.Refusedf(ErrNotOffered,
			"%q implements neither the channel adapter interface nor the model provider interface", name)
	}
	for k := range settings {
		if looksLikeASecret(k) {
			return refusal.Refusedf(ErrSettingLooksLikeASecret,
				"the setting %q on %s was not recorded, and neither was anything else in this "+
					"registration; supply a credential through your environment or a file you own, "+
					"and record its PATH", k, name)
		}
	}
	switch _, err := Get(s, name); {
	case err == nil:
		return refusal.Refusedf(ErrAlreadyRegistered, "%q", name)
	case errors.Is(err, ErrNotRegistered):
		// Free. Carry on.
	default:
		return err
	}
	return put(s, Registration{Name: name, Interface: iface, At: time.Now().UTC(), Settings: settings})
}

// Configure supplies an extension's settings, and it is THE SAME SHAPE FOR BOTH (criterion 4).
//
// "Whatever a person types to supply an adapter's settings is what they type to supply a provider's
// settings (§3.13 credentials included)." There is one function, it takes one map, and it branches
// on nothing. The credential half of that sentence is honoured by refusing to hold one — see
// [SecretishKeys].
//
// It replaces the settings wholesale rather than merging, because a merge has no way to remove a
// setting and a person who cannot remove one cannot correct a mistake.
func Configure(s *store.Store, name string, settings map[string]string) error {
	if s == nil {
		return ErrNoStore
	}
	name = strings.TrimSpace(name)
	for k := range settings {
		if looksLikeASecret(k) {
			return refusal.Refusedf(ErrSettingLooksLikeASecret,
				"the setting %q on %s was not recorded, and neither was anything else in this "+
					"call; supply a credential through your environment or a file you own, and "+
					"record its PATH", k, name)
		}
	}
	reg, err := Get(s, name)
	if err != nil {
		return err
	}
	reg.Settings = settings
	return put(s, reg)
}

// Deregister undoes the deliberate act, and REFUSES a name no act registered.
//
// The store's Delete is idempotent by design, which is right for the store and wrong for a person:
// somebody who mistypes a name must not be told the extension they meant is gone.
func Deregister(s *store.Store, name string) error {
	if s == nil {
		return ErrNoStore
	}
	name = strings.TrimSpace(name)
	if _, err := Get(s, name); err != nil {
		return err
	}
	return s.Delete(RecordKind, name)
}

// Get returns one registration. A name nobody registered is [ErrNotRegistered]; a record that is
// there and cannot be read is [ErrUnreadableRecord] — never an empty Registration.
func Get(s *store.Store, name string) (Registration, error) {
	if s == nil {
		return Registration{}, ErrNoStore
	}
	rec, err := s.Get(RecordKind, strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, store.ErrInvalidName) {
			return Registration{}, refusal.Refusedf(ErrNotRegistered, "%q", name)
		}
		return Registration{}, err
	}
	return decode(rec.ID, rec.Data)
}

// Registered returns every registration, ordered by name.
//
// # A DAMAGED RECORD IS RETURNED, NOT SKIPPED AND NOT FATAL (criteria 11 and 14)
//
// `channels.List` fails the whole call on a damaged record, which is right there: a channel listing
// that is quietly one short reads as complete. Here the requirement is stronger and pulls the other
// way — criterion 11 says one extension failing must not suppress the reporting of the others, and
// criterion 14 says a registered extension whose state is unknown is present with an undetermined
// state, not dropped. So a record that will not decode comes back as a Registration carrying the
// read error, and [Inventory] renders it [Undetermined] — present, listed, and honest.
//
// # IT ENUMERATES NAMES AND READS EACH RECORD ITSELF, AND THAT IS THE WHOLE POINT
//
// This was built on `store.List`, and the per-record path below was UNREACHABLE for the case it
// exists to handle. `store.List` refuses the whole kind when any single record's checksum is bad —
// correctly, for its own contract — so one damaged record made this return `nil, err`, EVERY
// registration vanished, and `omw ext list` printed "every registered extension loaded" over an
// inventory it had just failed to read. Two registered, failed-to-load extensions reported as
// absent, under a footer saying everything was fine. QA drove it and refused the pull request.
//
// That is the Issue's own opening story arriving through the store instead of through the loader,
// and the shape of the bug is the one this Issue is about: A DETERMINED ANSWER REPORTED FROM AN
// INCOMPLETE READ. Patching the summary line would have hidden it; the read is what was wrong.
//
// So it enumerates ids through `store.IDs`, which decodes nothing and therefore cannot be failed by
// any record's contents, and then reads each one on its own. One bad record is one bad entry.
//
// A non-nil error here means the ENUMERATION failed — the directory could not be read at all — and
// no per-record degradation can rescue that. Callers must treat it as "I do not know what is
// registered" and must not report completeness.
func Registered(s *store.Store) ([]Registration, error) {
	if s == nil {
		return nil, ErrNoStore
	}
	ids, err := s.IDs(RecordKind)
	if err != nil {
		return nil, err
	}
	out := make([]Registration, 0, len(ids))
	for _, id := range ids {
		rec, gerr := s.Get(RecordKind, id)
		if gerr != nil {
			// UNDETERMINED, AND STILL LISTED. A record we could not read has told us nothing about
			// the extension — not that it is absent, and not that it is broken.
			out = append(out, Registration{Name: id, readErr: refusal.Refusedf(ErrUnreadableRecord,
				"extension %q could not be read: %v", id, gerr)})
			continue
		}
		reg, derr := decode(rec.ID, rec.Data)
		if derr != nil {
			reg = Registration{Name: rec.ID, readErr: derr}
		}
		if reg.Name == "" {
			reg.Name = id
		}
		out = append(out, reg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func put(s *store.Store, reg Registration) error {
	f := recordFile{
		Format:    recordFormat,
		Name:      reg.Name,
		Interface: string(reg.Interface),
		Settings:  reg.Settings,
	}
	if !reg.At.IsZero() {
		f.At = reg.At.UTC().Format(time.RFC3339Nano)
	}
	body, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("extension %q could not be encoded: %w", reg.Name, err)
	}
	return s.Put(store.Record{Kind: RecordKind, ID: reg.Name, Data: body})
}

func decode(id string, body []byte) (Registration, error) {
	var f recordFile
	if err := json.Unmarshal(body, &f); err != nil {
		return Registration{}, refusal.Refusedf(ErrUnreadableRecord,
			"extension %q is damaged: %v", id, err)
	}
	if f.Format != recordFormat {
		return Registration{}, refusal.Refusedf(ErrUnreadableRecord,
			"extension %q is format %d, which this build does not understand", id, f.Format)
	}
	reg := Registration{Name: f.Name, Interface: Interface(f.Interface), Settings: f.Settings}
	if reg.Name == "" {
		reg.Name = id
	}
	if f.At != "" {
		if t, err := time.Parse(time.RFC3339Nano, f.At); err == nil {
			reg.At = t
		}
	}
	return reg, nil
}

// validName mirrors the store's own rule for a single path segment, and `channels.validID`'s
// wording, restated for the same reason that one is: the store still refuses anything this misses;
// this exists so the refusal names EXTENSIONS.
func validName(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
