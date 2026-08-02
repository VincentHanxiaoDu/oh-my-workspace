//go:build !unix

package commands

import "os/exec"

// detachChild does nothing where this build has no session to detach into.
//
// It is not silently absent: on such a build the daemon's lifetime is tied to whatever started it,
// and the platforms ruling on Issue #2 already ships macOS and Linux only. This file exists so
// that the difference is a named, readable no-op rather than a compile error somebody fixes by
// deleting the call.
func detachChild(*exec.Cmd) {}
