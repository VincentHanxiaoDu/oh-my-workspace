//go:build unix

package daemon

import (
	"os"
	"syscall"
)

// lockingIsAvailable reports whether this build can take the advisory lock the daemon's
// exclusivity depends on.
//
// IT IS A BUILD FACT, NOT A PLATFORM NAME. Code and tests ask this question rather than asking
// "is this macOS", so the answer comes from what was compiled in — which is the thing that
// actually determines the behaviour.
const lockingIsAvailable = true

// tryLockFile takes an exclusive advisory lock on an open file without blocking.
//
// flock, not fcntl. Two reasons, both load-bearing here. flock is held by the OPEN FILE
// DESCRIPTION, so two descriptors conflict even inside one process — which is what makes a test
// able to run a daemon and probe its lock in the same binary. And fcntl locks are dropped when the
// process closes ANY descriptor on the file, which would silently release the store's lock the
// first time an unrelated code path stat-ed it open.
//
// Returns (false, nil) when the lock is held by somebody else. That is not an error: it is the
// answer, and criterion 5 needs it distinguishable from a failure to ask.
func tryLockFile(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	// "Somebody else has it" arrives under different errno names on different unixes, and on some
	// of them two of these names are the same number — so this is a membership test rather than a
	// switch, which will not compile when two cases collide.
	for _, busy := range []syscall.Errno{syscall.EWOULDBLOCK, syscall.EAGAIN, syscall.EACCES} {
		if err == busy {
			return false, nil
		}
	}
	return false, err
}

// unlockFile releases the advisory lock. Closing the file releases it too; this exists so that a
// release is an explicit act at the point the daemon decides to stop holding the store.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// processIsAlive reports whether a pid names a live process, in the only way that does not require
// permission to signal it.
//
// IT IS NEVER WHAT DECIDES WHETHER A DAEMON HOLDS THE STORE — the lock decides that (see the
// package comment). This is used only to describe what a stale lock file contained, where being
// wrong costs a slightly less precise sentence rather than a wrong refusal.
func processIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
