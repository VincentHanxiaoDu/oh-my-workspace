// The ONE answer to "is the daemon running against this store", for every surface in package
// commands (Issue #41; PRD §4.3, §4.6).
//
// WHY THIS FILE EXISTS AT ALL. Until now each command that needed the answer made its own guess by
// stat'ing a path named by `OMW_CONTROL_SOCKET` — a placeholder written before the daemon existed.
// Nothing in the product ever set that variable, so every one of those guesses answered "not
// running" unconditionally, whatever the daemon was doing. Three pull requests printed a confident
// false negative from it on the same day (#29, #34, #35), and each was doing the locally reasonable
// thing: the defect was that the shared probe was wrong, so a per-command fix would have produced a
// fourth guess rather than one answer.
//
// WHY THE PATH IS NOT RECONSTRUCTED HERE. `internal/daemon` derives the control socket from the
// store root via socketFor, which falls back to a per-user runtime directory whenever the in-store
// path would exceed the kernel's sun_path limit. Any caller reproducing that rule is one release
// away from reproducing an older version of it, and the two would then disagree about the same
// running daemon. The rule is that no package outside `internal/daemon` derives or stats a control
// socket path, and TestNoPackageOutsideDaemonDerivesAControlSocketPath enforces it over the tree.
//
// WHY IT IS THREE-VALUED. `omw daemon status` already reports liveness as a tri.Value, because a
// lock that cannot be read is not a daemon that is absent (PRD §4.3). A surface that collapsed that
// into a bool would reintroduce the confident false negative this file exists to remove — in the
// one case where the person most needs to be told that nothing was established.

package commands

import (
	"fmt"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// daemonLiveness answers whether the daemon is running against the store this invocation is about,
// and — when it could not be established — why not.
//
// It resolves the store the same way `omw daemon status` does and then asks the same function,
// which is the only construction under which the two cannot disagree. The reason string is empty
// for a determined answer and never empty for an undetermined one.
//
// It is a var so a test can drive a surface's rendering without a real daemon. The agreement tests
// deliberately do NOT stub it: a stub proves the rendering, and only a started daemon proves the
// answer.
var daemonLiveness = func(env cli.Env) (tri.Value, string) {
	root, err := store.Resolve(env.Getenv)
	if err != nil {
		// NOT "not running". Not knowing which store this is about establishes nothing about
		// whether a daemon holds it.
		return tri.Undetermined, err.Error()
	}
	rep := daemon.Inspect(root)
	switch rep.Running {
	case tri.Yes, tri.No:
		return rep.Running, ""
	default:
		why := rep.HealthDetail
		if why == "" {
			why = "whether a daemon holds " + root + " could not be determined"
		}
		return tri.Undetermined, why
	}
}

// reportDaemonNotLive writes the report for a surface that needs the daemon and has not got it, and
// gives back the exit code that surface must return.
//
// THE TWO CASES ARE WRITTEN ONCE, HERE, AND THEY DO NOT SHARE WORDING OR AN EXIT CODE. "The daemon
// is not running" is an established fact and exits ExitFailure with the product's existing code;
// "whether the daemon is running could not be determined" is not a negative and exits
// ExitUndetermined, and its sentence never contains the phrase the determined answer uses. A caller
// must not be able to grep for one and match the other.
//
// It is only ever called for a non-Yes value; passing tri.Yes is a programmer error and is reported
// as such rather than silently rendered as an absence.
func reportDaemonNotLive(env cli.Env, what string, live tri.Value, why string) int {
	switch live {
	case tri.No:
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, hub.ErrDaemonNotRunning, hub.ErrDaemonNotRunning.Code)
		return cli.ExitFailure
	case tri.Yes:
		fmt.Fprintf(env.Stderr, "%s: internal error: the daemon is running and no report was due\n", what)
		return cli.ExitFailure
	}
	if why == "" {
		why = "no reason was recorded, which is itself a thing that could not be determined"
	}
	fmt.Fprintf(env.Stderr, "%s: whether the daemon is running %s (code: %s)\n",
		what, tri.Undetermined, codeDaemonUndetermined)
	fmt.Fprintf(env.Stderr, "  %s\n", why)
	// SAID OUT LOUD, because this is the sentence the person needs in order not to read the line
	// above as a stopped daemon.
	fmt.Fprintf(env.Stderr, "  this is not a report that the daemon is stopped; nothing about it has been established.\n")
	fmt.Fprintf(env.Stderr, "  'omw daemon status' reports the same state for this store.\n")
	return cli.ExitUndetermined
}

// codeDaemonUndetermined is the machine-readable code for the third answer. It exists so that a
// script can tell it from hub.ErrDaemonNotRunning's code without parsing prose — the exit code
// distinction restated where a caller reading stderr will meet it.
const codeDaemonUndetermined = "daemon-liveness-undetermined"
