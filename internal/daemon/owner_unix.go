//go:build unix

package daemon

import (
	"fmt"
	"os"
	"syscall"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// confirmOwnerOnly answers, in three values, whether path is readable and writable by its owner
// ALONE and that owner is this process's user.
//
// THIS IS THE CONFIRMATION §4.6 REQUIRES, AND IT IS A REAL STAT. Criterion 22 asks that it be an
// observable step rather than an assumption, which is why it looks at the filesystem every time
// rather than trusting the mode this package created the file with — the two differ whenever a
// umask, an ACL-flattening filesystem or somebody's chmod has been between them.
//
// Three answers, deliberately:
//
//   - Yes: stat succeeded, no group or other bits are set, and the owner is this user.
//   - No: stat succeeded and one of those is false. A DETERMINED negative.
//   - Undetermined: the stat failed, or the system did not give back a structure carrying an owner.
//     Not a "no" (§4.3) — and it refuses the control API just as a "no" does, because §4.6 opens
//     only on a confirmed yes.
func confirmOwnerOnly(path string) (tri.Value, string) {
	info, err := os.Lstat(path)
	if err != nil {
		return tri.Undetermined, fmt.Sprintf("the permissions of %s could not be read: %v", path, err)
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// A filesystem or platform that does not publish an owner. Undetermined, and the control
		// API declines — which is the platforms ruling on Issue #2, reached by probing rather than
		// by naming an operating system.
		return tri.Undetermined, fmt.Sprintf("this system does not report an owner for %s, so owner-only access could not be confirmed", path)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return tri.No, fmt.Sprintf("%s is reachable by users other than its owner (mode %04o)", path, perm)
	}
	if int(sys.Uid) != os.Getuid() {
		return tri.No, fmt.Sprintf("%s is owned by uid %d, not by this user (uid %d)", path, sys.Uid, os.Getuid())
	}
	return tri.Yes, ""
}
