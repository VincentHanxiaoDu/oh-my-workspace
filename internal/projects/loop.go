package projects

import (
	"os"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// BackgroundName is what this work is called where a person can see it.
const BackgroundName = "projects"

// Watching projects is a property of the daemon RUNNING, not of a command being typed (PRD §3.6,
// Issue #4 criterion 4). This registers it, from this package's own init, exactly as
// `internal/channels` registers ingestion.
//
// IT USED TO LIVE IN internal/commands, called by hand from daemonRun, because this registry was on
// Issue #6's branch and not on main. That was said on the pull request and in the file, with a note
// that it MUST move rather than sit alongside — two ways to run daemon background work is a second
// mechanism, and this branch had already been refused once for shipping a second answer to a
// settled question. Issue #6 has merged, so it has moved. The daemon imports nothing of this
// package; it knows only that something asked to be run.
func init() {
	daemon.RegisterBackground(daemon.Background{
		Name:     BackgroundName,
		Interval: PollInterval,
		Run:      PollPass,
	})
}

// PollPass is one poll, for the daemon.
//
// IT SWALLOWS ITS FAILURES ON PURPOSE, AND THEY ARE NOT LOST. PRD §4.3's "the daemon stops when it
// cannot write" is already enforced by the daemon's own write probe against this same store, so a
// second exit decision here would be made on less evidence than the one already being made — and a
// pass that gave up on a transient error would stop watching a person's directories for the rest of
// the run while the daemon went on reporting itself healthy.
//
// A failed pass writes no state record, and a project with no record is examined by the listing and
// stamped [ExaminedNow]. So a run whose passes are all failing degrades, per project and visibly, to
// exactly what a person sees with no daemon at all — rather than to a stale number wearing a
// daemon-polled stamp.
func PollPass(storePath string) {
	s, err := store.Open(storePath)
	if err != nil {
		return
	}
	_ = Poll(s, os.Getenv, time.Now().UTC())
}
