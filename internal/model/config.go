// Package model is the ONE answer to "which model provider has this person chosen, and have they
// supplied a credential for it" (Issue #18; PRD §3.13, §4.2, §4.3, §4.4).
//
// # WHY THIS PACKAGE EXISTS RATHER THAN A FUNCTION IN THE PACKAGE THAT ASKED FIRST
//
// Issue #9 needed the answer before this Issue started, because `review` mode's whole point is what
// happens when there is no model, and it could not defer that to code that did not exist. So
// `internal/drafts/model.go` read $OMW_MODEL, $OMW_MODEL_KEY and $OMW_MODEL_KEY_FILE, and its
// author said in the pull request that #18 should take it over rather than sit beside it.
//
// Two implementations of "is a model configured" that disagree is worse than either alone — the
// same class of defect as PRD §3.14's two outboxes, and §4.3 ("the control API and the CLI report
// the same state") means the answer has to be ONE answer. So the resolution moved here, and
// `internal/drafts` no longer reads the environment at all: there is exactly one reader of
// $OMW_MODEL in the tree, and a test asserts it.
//
// # THE THIRD VALUE, DECIDED DELIBERATELY
//
// #9 chose "a key file that exists and cannot be read" as the undetermined case, and flagged it as
// an invention rather than a reading of the PRD. THIS ISSUE KEEPS THAT CHOICE and states why,
// because keeping it silently would be indistinguishable from never having examined it:
//
//   - A key file that is NAMED and DOES NOT EXIST is a determined negative. The filesystem answered;
//     there is no credential there. Rendering that as undetermined would make every typo in a path
//     look like a failure of the product rather than a fact about the machine.
//   - A key file that is NAMED and EXISTS and CANNOT BE READ is a failure to determine. Something is
//     there and we cannot see it, so "no credential is configured" would be a claim nobody
//     established — precisely the collapse §4.3 forbids, and precisely what tri.FromError encodes.
//
// That is the same line `tri.FromError` draws, which is the argument for it: the third value is not
// a new rule, it is the project's existing rule applied to a second source. What this Issue ADDS is
// the other half #9 could not have — a recorded configuration in the store that exists and cannot
// be read is undetermined for the same reason (criterion 15, "the credential store cannot be read").
//
// # THE CREDENTIAL IS NEVER STORED BY omw, AND THAT IS THE POINT
//
// PRD §3.13: "A key belongs to the person who supplied it." Read literally, the strongest form of
// that is custody: this product never takes any. What `omw model` records in the store is the
// provider's NAME and, if the person wants one, the PATH of a key file they own. The credential
// VALUE is read from the person's environment or from their file at the moment it is used, and is
// never written into a record, a note, a report or a log. Criterion 7 asks that a sentinel
// credential appear in no output and in no export of the local store; the way that is guaranteed
// here is that there is nothing in the store to export.
package model

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// The environment a person may supply their choice and their credential through.
//
// THE NAMES ARE #9'S NAMES. They were published in #38 and a person may already have them in a
// shell profile; renaming them to mark a change of owner would break a working configuration to
// make a point about which Issue wrote the file.
const (
	// EnvProvider names the provider the person chose.
	EnvProvider = "OMW_MODEL"
	// EnvCredential carries the credential itself, for a person who keeps secrets in their
	// environment.
	EnvCredential = "OMW_MODEL_KEY"
	// EnvCredentialFile names a file holding the credential, for a person who will not. It is also
	// the honest source of an UNDETERMINED answer — see the package comment.
	EnvCredentialFile = "OMW_MODEL_KEY_FILE"
)

// The store record this capability owns. The provider name and the key file's PATH live here. The
// credential value never does.
const (
	recordKind = store.Kind("model")
	recordID   = "provider"
)

// The refusals and failures this package can produce.
//
// THEY ARE DISTINCT VALUES WITH DISTINCT CODES, and a test asserts that no two share either. Every
// criterion in Issue #18 is of the form "these two outcomes must not look the same", so a caller
// has to be able to tell them apart without reading English.
var (
	// ErrNoModel — something that needs a model was asked for and there is no model to run it with.
	ErrNoModel = &hub.Error{
		Code: "no-model",
		Msg:  "no model is configured, and review mode checks your drafts with your own model",
	}

	// ErrNoCredential — a provider is chosen and nothing has been supplied to authenticate to it.
	//
	// SEPARATE FROM ErrNoModel because criterion 3 says so: "A provider chosen with no credential
	// yet supplied reports as such — it never reports as fully configured, and it never reports as
	// no-provider-configured." A person in this state has done half of a two-part act and needs to
	// be told which half is outstanding, not sent back to the beginning.
	ErrNoCredential = &hub.Error{
		Code: "no-model-credential",
		Msg:  "a model provider is chosen and no credential has been supplied for it",
	}

	// ErrUndetermined — whether a model is configured could not be established. Never a "no".
	ErrUndetermined = &hub.Error{
		Code: "model-undetermined",
		Msg:  "whether a model is configured could not be determined, which is neither yes nor no",
	}

	// ErrCredentialNotReadable — an API surface was asked for the credential VALUE.
	//
	// IT IS A REFUSAL, NOT AN EMPTY ANSWER (criterion 6). Returning "" or a row of asterisks would
	// be indistinguishable, to a caller, from "no credential is configured" — and a caller that
	// cannot tell those apart will report the wrong one to its person. So the request is refused,
	// with its own code, and "no credential is configured" is a different code on a successful
	// answer.
	ErrCredentialNotReadable = &hub.Error{
		Code: "model-credential-not-readable",
		Msg:  "refused: a model credential is never returned through an API; whether one is present is answerable, its value is not",
	}

	// ErrNoStore — configuring a model needs somewhere to record the choice.
	ErrNoStore = &hub.Error{
		Code: "no-store",
		Msg:  "no store is open, and a model choice is recorded in your store",
	}

	// ErrNoProviderNamed — `use` was given nothing to use.
	ErrNoProviderNamed = &hub.Error{
		Code: "no-provider-named",
		Msg:  "refused: choosing a provider means naming one",
	}
)

// allErrors is every error this package defines, for the test that asserts they are pairwise
// distinguishable in both code and message.
var allErrors = []*hub.Error{
	ErrNoModel, ErrNoCredential, ErrUndetermined, ErrCredentialNotReadable,
	ErrNoStore, ErrNoProviderNamed,
}

// record is the on-disk shape of the person's choice.
//
// NOTE WHAT IS NOT IN IT. There is no credential field, and adding one is not a small change: it
// would put a secret inside the thing criterion 7 exports and greps.
// TestTheViewHasNowhereToPutACredential reflects over this struct's fields and fails if a third one
// appears, so the absence is checked rather than remembered.
type record struct {
	// Provider is the name the person typed. Not a secret.
	Provider string `json:"provider"`
	// CredentialFile is the PATH of a file the person owns. A path is not a credential; the bytes
	// inside it are, and they are never copied in here.
	CredentialFile string `json:"credential_file,omitempty"`
}

// Config is what this machine knows about the person's model.
//
// THE TWO HALVES ARE TWO FIELDS, AND THAT IS CRITERION 3. "Which provider" and "is there a
// credential" are separate facts with separate three-valued answers, and a single tri.Value over
// both would make "chosen, no key yet" indistinguishable from "nothing chosen" — the exact pair the
// criterion requires be distinguishable. [Config.Configured] combines them for the one caller that
// genuinely only needs the combined answer, and it combines them PAIRWISE rather than by string.
type Config struct {
	// Provider is whether a provider has been chosen: Yes, No, or Undetermined.
	Provider tri.Value
	// Name is the chosen provider's name. Not a secret; a person needs to see which one would run.
	Name string
	// Credential is whether a credential has been supplied for it. Three values, same rule.
	Credential tri.Value
	// Source describes WHERE the credential came from — "$OMW_MODEL_KEY", or the file's path. It
	// is a description of a location and never the bytes at that location.
	Source string
	// Missing names the part that is absent, on a determined negative.
	Missing string
	// Why says what could not be read, on an undetermined answer.
	Why string

	// credential is unexported and stays that way. The only route to it is [Config.Secret], which
	// is called from exactly one place in the product — see [Config.Secret].
	credential string
}

// Configured is the combined answer, for the caller that needs one value: can something that
// requires a model run right now?
//
// IT IS COMPUTED PAIRWISE, comparing tri.Values to tri.Values. No branch here compares a string to
// a literal, because that is how a renaming turns a "yes" into a "no" with nothing failing to
// build. Undetermined dominates: if either half could not be established, the combined answer was
// not established either, and §4.3 will not have that reported as a negative.
func (c Config) Configured() tri.Value {
	if c.Provider == tri.Undetermined || c.Credential == tri.Undetermined {
		return tri.Undetermined
	}
	if c.Provider == tri.Yes && c.Credential == tri.Yes {
		return tri.Yes
	}
	return tri.No
}

// Secret returns the person's credential.
//
// THE ONE CALLER IS THE THING THAT AUTHENTICATES TO THEIR PROVIDER. Every other surface asks
// [Config.Credential] — a tri.Value — instead. TestNothingButTheProviderSeamCallsSecret walks the
// tree and fails if a second caller appears.
func (c Config) Secret() string { return c.credential }

// Render is the ONE rendering of a model configuration, and it DELEGATES TO [View.Render].
//
// It used to have its own switch, and the two drifted within an hour of being written: the Config's
// "configured" branch named where the credential came from and the View's did not, so criterion
// 18's agreement test — the CLI against the control API — went red on its first run. That is the
// whole failure mode in miniature, and the repair is not to re-synchronise two format strings but
// to have one. Everything a person may be told about their model configuration is in a View, so
// everything that renders one renders it the same way.
func (c Config) Render() string { return c.View().Render() }

func withDetail(s, detail string) string {
	if detail == "" {
		return s
	}
	return s + "\n  " + detail
}

// String makes the accident safe: `%v` and `%s` on a Config print the description, never the
// credential. fmt reflects into unexported fields quite happily when nothing stops it.
func (c Config) String() string { return c.Render() }

// GoString closes the other one. `%#v` ignores String and prints the struct literal, unexported
// fields and all — it is the single most likely way a debugging line in some future file leaks the
// credential, and it is the one nobody thinks of. It was measured: without this method, `%#v`
// printed the credential.
func (c Config) GoString() string { return "model.Config(" + c.Render() + ")" }

// Read is the ONE resolution of the person's model configuration.
//
// PRECEDENCE, AND WHY. The environment wins over the recorded choice, because the environment is
// what a person sets for one invocation and the record is what they set once. A person who exports
// $OMW_MODEL for a single command and gets their recorded provider instead has been overruled by
// their own configuration.
//
// s MAY BE NIL, and nil means "this caller has no store", NOT "the store could not be read". A
// caller that failed to open a store has already reported that failure in its own words and is
// about to exit; having this function invent a second undetermined answer for the same fact would
// print it twice. A store that is OPEN and whose record will not read is a different matter, and
// that one is undetermined here.
//
// IT OPENS NO CONNECTION (criterion 16, §4.2). It reads the environment and at most two files, both
// on this machine, both named by the person.
func Read(getenv func(string) string, s *store.Store) Config {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	rec, recUnreadable, recWhy := readRecord(s)

	name := strings.TrimSpace(getenv(EnvProvider))
	if name == "" {
		name = strings.TrimSpace(rec.Provider)
	}

	// BOTH HALVES ARE ALWAYS ANSWERED, and there is no early return.
	//
	// This is not tidiness. An early return on "no provider chosen" leaves Credential at its ZERO
	// VALUE, which is tri.Undetermined by design — so [Config.Configured] would report undetermined
	// for the most ordinary state the product has, a fresh machine with nothing configured. It was
	// written that way first and the suite caught it: `omw outbox model` exited 3 on an empty
	// configuration. The zero value being the third answer is correct and is exactly why every
	// field has to be filled deliberately.
	var c Config
	c.Credential, c.Source, c.Missing, c.Why, c.credential = readCredential(getenv, rec, recUnreadable, recWhy)

	switch {
	case name != "":
		c.Provider, c.Name = tri.Yes, name
	case recUnreadable:
		// NOT "no provider". A recorded choice that will not read is a configuration we cannot see,
		// and answering "no provider" would send a person off to choose the thing they already chose.
		c.Provider, c.Why = tri.Undetermined, recWhy
	default:
		c.Provider = tri.No
		c.Missing = "choose one with 'omw model use <provider>', or set $" + EnvProvider
	}
	return c
}

// readRecord reads the recorded choice. Three outcomes and they stay three: a record; no record
// ever written; and a record that is there and will not read.
func readRecord(s *store.Store) (rec record, unreadable bool, why string) {
	if s == nil {
		return record{}, false, ""
	}
	err := s.GetJSON(recordKind, recordID, &rec)
	switch {
	case err == nil:
		return rec, false, ""
	case errors.Is(err, store.ErrRecordNotFound):
		return record{}, false, ""
	default:
		return record{}, true, "your recorded model choice is in your store and could not be read: " + err.Error()
	}
}

// readCredential answers the credential half, three-valued.
func readCredential(getenv func(string) string, rec record, recUnreadable bool, recWhy string) (
	state tri.Value, source, missing, why, secret string) {

	if v := getenv(EnvCredential); strings.TrimSpace(v) != "" {
		return tri.Yes, "$" + EnvCredential, "", "", v
	}

	file := strings.TrimSpace(getenv(EnvCredentialFile))
	if file == "" {
		file = strings.TrimSpace(rec.CredentialFile)
	}
	if file != "" {
		b, err := os.ReadFile(file)
		switch {
		case err == nil:
			if strings.TrimSpace(string(b)) == "" {
				// A file that exists and is empty is a DETERMINED absence. It was read; there is
				// nothing in it.
				return tri.No, "", "the credential file " + file + " is empty", "", ""
			}
			return tri.Yes, "the file " + file, "", "", string(b)
		case errors.Is(err, fs.ErrNotExist):
			// SEE THE PACKAGE COMMENT. The filesystem answered, so this is a negative and not the
			// third value.
			return tri.No, "", "the credential file " + file + " does not exist", "", ""
		default:
			// SOMETHING IS THERE AND WE CANNOT SEE IT. Undetermined, and it says why.
			return tri.Undetermined, "", "", "the credential file " + file + " could not be read: " + err.Error(), ""
		}
	}

	if recUnreadable {
		return tri.Undetermined, "", "", recWhy, ""
	}
	return tri.No, "", "supply one by setting $" + EnvCredential + ", or 'omw model key file <path>'", "", ""
}

// Use records the person's choice of provider. An explicit act (§4.2, criterion 1): nothing else in
// the product calls it, and TestNothingConfiguresAModelAsASideEffect asserts that.
func Use(s *store.Store, provider string) error {
	if s == nil {
		return hub.Refusedf(ErrNoStore, "no store was opened")
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ErrNoProviderNamed
	}
	rec, _, _ := readRecord(s)
	rec.Provider = provider
	return s.PutJSON(recordKind, recordID, rec)
}

// UseCredentialFile records the PATH of the person's key file. The bytes at that path are not read
// here and are never copied into the store.
//
// The path is NOT checked for existence. A person configuring a machine before mounting the volume
// their key lives on has not made a mistake, and a check here would turn `Read`'s honest
// three-valued answer into a refusal at configuration time.
func UseCredentialFile(s *store.Store, path string) error {
	if s == nil {
		return hub.Refusedf(ErrNoStore, "no store was opened")
	}
	rec, _, _ := readRecord(s)
	rec.CredentialFile = strings.TrimSpace(path)
	return s.PutJSON(recordKind, recordID, rec)
}

// Clear removes the recorded choice. It does not, and cannot, clear a credential the person put in
// their own environment or their own file — those are theirs, and saying so is more honest than
// pretending to have deleted something.
func Clear(s *store.Store) error {
	if s == nil {
		return hub.Refusedf(ErrNoStore, "no store was opened")
	}
	err := s.Delete(recordKind, recordID)
	if errors.Is(err, store.ErrRecordNotFound) {
		return nil
	}
	return err
}
