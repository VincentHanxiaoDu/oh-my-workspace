// Package refusal is the product's one typed-refusal vocabulary: a stable code a caller can read
// WITHOUT parsing prose, and the two functions that put one into an error and get one back out.
//
// # WHY IT IS NOT IN internal/hub ANY MORE
//
// It was, and `hub.Error` is still the name most of the tree spells it by — the aliases at the
// bottom of `internal/hub/errors.go` keep that true, so nothing outside this file changed shape.
// What moved is where the TYPE lives, and the reason is a collision that only appears once two
// green branches are in the same tree:
//
//   - Issue #6 asserts, structurally, that `internal/channels` cannot reach `internal/hub` at all,
//     transitively. "A connected channel never reaches the hub as part of ingesting, and ingested
//     material never leaves the machine." That is a real guarantee and it is worth keeping whole.
//   - Issue #18's `internal/model` needed typed refusals, and the only typed-refusal type in the
//     tree was `hub.Error`. So it imported `internal/hub` — for a struct with two string fields.
//   - `internal/channels` imports `internal/daemon` to register its background work; #18 made
//     `internal/daemon` report model state, so it imports `internal/model`.
//
// channels → daemon → model → hub. Both branches pass alone; merged, #6's guard goes red, and it
// is right to: a package that can name the hub is a package somebody can make talk to the hub.
// The repair that a person reaches for first is to relax #6's ban to a direct-import check, which
// keeps the merge green and throws away the guarantee. The repair that holds is for the shared
// vocabulary not to be the hub's property in the first place, because it never was one: `drafts`,
// `model` and `commands` all use it and none of them is about a hub.
//
// So the type is here, the hub's own refusal VALUES stay in the hub where they belong, and #6's
// ban is untouched and still transitive.
//
// # THIS PACKAGE REACHES NOTHING
//
// Two standard-library imports, no store, no network, no other internal package. It is safe for
// anything to depend on, which is the property that makes it usable as the one vocabulary.
package refusal

import (
	"errors"
	"fmt"
)

// Error is a refusal or failure a caller can tell apart from another one without reading the prose.
//
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

// Code returns the stable code of the refusal inside err, or "" if err is not one.
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

// Refusedf wraps a refusal with detail while keeping its code readable through errors.As.
func Refusedf(base *Error, format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), base)
}
