package inbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ControlSocketName is where the daemon's control socket sits inside the store.
//
// A PLACEHOLDER THAT IS HONEST ABOUT BEING ONE. Issue #2 owns the daemon and the control API and
// has not landed, so there is no package to ask. Issue #8 criterion 13 nonetheless requires that an
// inbox command say the daemon is not running rather than appear to have read a live inbox, and
// criterion 15 requires that it say when owner-only socket permissions cannot be confirmed. Both
// are answerable from the socket's own file, which is what [Probe] reads. When #2 lands, this
// constant and [Probe] are replaced by its API; nothing outside this file assumes the path.
const ControlSocketName = "control.sock"

// Presence is what could be determined about the two things an inbox command must not lie about:
// whether a daemon is running, and whether the control API is open.
//
// EVERY ANSWER IS A [tri.Value] AND NOT A bool. "There is no socket" and "I could not look" are
// different answers, and §4.2's promise — no command starts the daemon, and if it is not running
// commands say so — is only kept if the command can tell those apart. A failed stat rendered as
// "not running" is a guess presented as a fact.
type Presence struct {
	// Running is whether a daemon is listening. No means determined not to be.
	Running tri.Value
	// RunningWhy is the specific finding behind Running, for the command to name.
	RunningWhy string
	// ControlAPIOpen is whether the control API is open and serving. PRD §4.6: it does not open
	// unless it can prove its socket is owner-only, so a socket whose ownership cannot be confirmed
	// is NOT an open control API — and is not the same answer as no socket at all.
	ControlAPIOpen tri.Value
	// ControlWhy is the specific finding behind ControlAPIOpen.
	ControlWhy string
}

// Probe reads what can be read about the daemon and the control API for the store at root.
//
// IT OPENS NOTHING AND STARTS NOTHING. It stats one path. §4.2: no command starts the daemon on a
// person's behalf, and a probe that connected in order to answer would be a command that woke
// something up in order to report that it was asleep.
//
// IT DOES NOT NAME AN OPERATING SYSTEM. Whether this platform publishes an owner for a file is
// discovered by asking for one and seeing whether the answer arrives — the type assertion below —
// rather than by comparing runtime.GOOS against a list. PRD §5.1 ships macOS and Linux and the
// answer must be produced the same way on both; a test that names a platform passes on the platform
// it names and says nothing about the other.
func Probe(root string) Presence {
	path := filepath.Join(root, ControlSocketName)
	info, err := os.Lstat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		// DETERMINED. There is no socket, so nothing is listening on one.
		return Presence{
			Running:        tri.No,
			RunningWhy:     "there is no control socket at " + path,
			ControlAPIOpen: tri.No,
			ControlWhy:     "the control API is not open — nothing is listening",
		}
	case err != nil:
		// UNDETERMINED. A permission error on the containing directory is not evidence of absence.
		return Presence{
			Running:        tri.Undetermined,
			RunningWhy:     "the control socket at " + path + " could not be examined: " + err.Error(),
			ControlAPIOpen: tri.Undetermined,
			ControlWhy:     "whether the control API is open could not be determined",
		}
	}

	if info.Mode()&fs.ModeSocket == 0 {
		return Presence{
			Running:        tri.Undetermined,
			RunningWhy:     path + " exists but is not a socket, so what is there could not be determined",
			ControlAPIOpen: tri.Undetermined,
			ControlWhy:     "whether the control API is open could not be determined",
		}
	}

	owner, ownerKnown := fileOwner(info)
	switch {
	case !ownerKnown:
		// §4.6 READ LITERALLY, WHICH IS THE RULING ON §5.1. The socket exists, and this platform
		// will not tell us who owns it, so owner-only permissions cannot be CONFIRMED — and an
		// unconfirmed control API is not an open one.
		return Presence{
			Running:        tri.Yes,
			RunningWhy:     "a control socket is present at " + path,
			ControlAPIOpen: tri.No,
			ControlWhy: "the control API is not open: this platform does not report the socket's " +
				"owner, so owner-only permissions could not be confirmed",
		}
	case info.Mode().Perm()&0o077 != 0:
		return Presence{
			Running:        tri.Yes,
			RunningWhy:     "a control socket is present at " + path,
			ControlAPIOpen: tri.No,
			ControlWhy:     "the control API is not open: the socket's permissions are not owner-only",
		}
	case owner != os.Getuid():
		return Presence{
			Running:        tri.Yes,
			RunningWhy:     "a control socket is present at " + path,
			ControlAPIOpen: tri.No,
			ControlWhy:     "the control API is not open: the socket belongs to another user",
		}
	}
	return Presence{
		Running:        tri.Yes,
		RunningWhy:     "a control socket is present at " + path,
		ControlAPIOpen: tri.Yes,
		ControlWhy:     "the control API is open, on an owner-only socket",
	}
}

// fileOwner asks the platform for a file's owning user id, and reports whether it answered.
//
// The type assertion IS the probe. A platform that does not publish an owner this way returns
// ok=false and the caller renders that as "could not be confirmed" — never as "confirmed owner-only"
// and never as "somebody else owns it".
func fileOwner(info fs.FileInfo) (uid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, false
	}
	return int(st.Uid), true
}
