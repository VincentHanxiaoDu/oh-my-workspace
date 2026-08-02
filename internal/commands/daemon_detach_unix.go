//go:build unix

package commands

import (
	"os/exec"
	"syscall"
)

// detachChild puts the daemon in its own session.
//
// WITHOUT THIS, THE DAEMON DIES WITH THE TERMINAL that started it — the shell sends SIGHUP to its
// foreground process group on exit, and the daemon would be in it. Criterion 1 says the daemon is
// running after `start` returns; a person who then closes their laptop lid's terminal would find
// it was not, and the last-run record would say "ended by an explicit stop" about a stop nobody
// made.
func detachChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
