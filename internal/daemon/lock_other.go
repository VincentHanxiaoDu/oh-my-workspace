//go:build !unix

package daemon

import "os"

// lockingIsAvailable is false where this package has no advisory lock it can rely on.
//
// The consequence is stated rather than papered over: without a lock there is no way to establish
// that exactly one daemon holds a store, so the answer to "is one running" is UNDETERMINED and a
// start refuses. Guessing from a pid file would be a determined-looking answer built out of a
// guess, which §4.3 forbids more clearly than it forbids saying "I cannot tell".
const lockingIsAvailable = false

func tryLockFile(*os.File) (bool, error) { return false, errNoLocking }

func unlockFile(*os.File) error { return errNoLocking }

func processIsAlive(int) bool { return false }
