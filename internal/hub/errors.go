package hub

import (
	"errors"
	"fmt"
)

// Error is a hub refusal or failure that a caller can tell apart from another one WITHOUT reading
// the prose.
//
// Issue #12 criterion 10 asks that a refused grant be "distinguishable from success by the caller
// without parsing prose", and criterion 12 that "refused" and "no such note" be distinguishable.
// Prose is translated, reworded and line-wrapped; a code is not. Every surface that reports one of
// these prints its Code alongside its message, and every test asserts on the Code.
type Error struct {
	// Code is stable, lowercase, hyphenated, and part of the contract. Changing one is a breaking
	// change to anything scripting against `omw`.
	Code string
	// Msg is the sentence a person reads.
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// The refusals and failures this package can produce.
//
// THEY ARE DISTINCT VALUES, NOT ONE ERROR WITH A STRING. A test asserts that no two share a Code
// and no two share a Msg, because the criteria that matter here are all of the form "these two
// outcomes must not look the same".
var (
	// ErrNoSuchNote — there is no note with that id.
	//
	// NOTE A DELIBERATE TENSION, FLAGGED RATHER THAN QUIETLY RESOLVED. The usual security instinct
	// is to answer "not found" for a note the reader may not read, so that the refusal does not
	// confirm the note exists. Issue #12 criterion 12 explicitly requires the two be
	// DISTINGUISHABLE, so this package implements what the Issue says. The consequence is that a
	// caller can learn a note id exists without being allowed to read it — recorded here so the
	// next person meets the decision rather than the symptom.
	ErrNoSuchNote = &Error{Code: "no-such-note", Msg: "no such note"}

	// ErrRefused — the note exists and the reader may not read it.
	ErrRefused = &Error{Code: "visibility-refused", Msg: "refused: this note is not visible to you"}

	// ErrUnknownGroup — a narrowing named a group the hub has no membership record for.
	// Criterion 15: the publication is refused. It is NOT published company-wide as a fallback and
	// NOT published to an empty audience.
	ErrUnknownGroup = &Error{Code: "unknown-group", Msg: "refused: the hub has no membership record for that group"}

	// ErrUndetermined — the answer could not be determined. Never a "no".
	ErrUndetermined = &Error{Code: "undetermined", Msg: "the note's visibility could not be determined"}

	// ErrHubUnreachable — a hub is configured but could not be reached. Criterion 16: undetermined,
	// distinct from both company-wide and self-only.
	ErrHubUnreachable = &Error{Code: "hub-unreachable", Msg: "the hub could not be reached"}

	// ErrNoHubConfigured — no hub is configured at all.
	//
	// DISTINCT FROM ErrHubUnreachable, and the distinction is a judgement this Issue had to make:
	// "there is no hub" is a determined fact about this machine's configuration, whereas "the hub
	// did not answer" is a failure to determine. Criterion 21 wants the first stated precisely;
	// criterion 16 wants the second rendered undetermined. Collapsing them would break one or the
	// other.
	ErrNoHubConfigured = &Error{Code: "no-hub-configured", Msg: "no hub configured, so who may read a published note cannot be evaluated here"}

	// ErrDaemonNotRunning — PRD §4.2. Said, never fixed by starting it.
	ErrDaemonNotRunning = &Error{Code: "daemon-not-running", Msg: "the daemon is not running, and omw does not start it for you"}

	// ErrGrantWiderThanHolder — PRD §4.5. Refused when requested, never narrowed at the edge.
	ErrGrantWiderThanHolder = &Error{Code: "grant-wider-than-holder", Msg: "refused: the grant asked to read more than its holder can"}

	// ErrUnknownScope — a grant request named a scope that is not in the one vocabulary.
	ErrUnknownScope = &Error{Code: "unknown-scope", Msg: "refused: that scope is not in the scope vocabulary"}

	// ErrEmptyAudience — a narrowing to named people named nobody. Refused rather than published to
	// an audience of zero, which criterion 15 forbids as an outcome of a refused narrowing and
	// which is nonsense as a choice in its own right.
	ErrEmptyAudience = &Error{Code: "empty-audience", Msg: "refused: narrowing to named people named nobody"}

	// ErrUnknownVisibility — a visibility choice that could not be parsed.
	ErrUnknownVisibility = &Error{Code: "unknown-visibility", Msg: "refused: that is not one of the four visibility choices"}

	// ErrNoAuthor — a note with no author cannot be published; "yourself" would have no referent.
	ErrNoAuthor = &Error{Code: "no-author", Msg: "refused: a note must have an author"}
)

// allErrors is every error this package defines, for the test that asserts they are pairwise
// distinguishable. A new error added without being listed here is invisible to that test, so the
// same test also checks the list against the package's own source is not attempted — instead the
// list is short enough to read, and adding to it is part of adding an error.
var allErrors = []*Error{
	ErrNoSuchNote, ErrRefused, ErrUnknownGroup, ErrUndetermined, ErrHubUnreachable,
	ErrNoHubConfigured, ErrDaemonNotRunning, ErrGrantWiderThanHolder, ErrUnknownScope,
	ErrEmptyAudience, ErrUnknownVisibility, ErrNoAuthor,
}

// Code returns the stable code of the hub error inside err, or "" if err is not one.
//
// It walks wrapped errors, so a caller may add context with %w and a script still reads the same
// code. This is the function a surface calls; nothing prints a bare error string as its only signal.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Refusedf wraps a hub error with detail while keeping its code readable through errors.As.
func Refusedf(base *Error, format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), base)
}
