// `omw status` — the one screen that says whether everything runs (PRD §3.9, Issue #5).
//
// WHAT THIS FILE IS AND IS NOT. It is the CLI's half of ONE state: it gathers the inputs
// internal/status cannot get for itself, hands them over, and prints what comes back. Every
// judgement about what a subsystem's state IS lives in internal/status, and both surfaces this
// command offers — the screen and `--json` — render the same [status.Screen] value through that
// package's own two renderers. §4.3 asks that the control API and the CLI report the same state;
// the way that is true here is that there is only one state and one place it is decided.
//
// IT STARTS NOTHING (§4.2, criteria 4, 13, 16). There is no call in this file that starts a
// daemon, creates a store or opens a network connection. `omw status` against a machine with
// nothing set up prints six lines and leaves the machine exactly as it found it.
package commands

import (
	"fmt"
	"io"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/status"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

const statusUsage = `omw status — one screen: is everything running?

Usage:
  omw status [--json]

It reports every subsystem the client is made of, each on its own line, each with one of four
states: working, NOT working, could not be determined, or not configured. A state nobody could
establish is said out loud — it is never shown as a failure and never left off the screen.

  --json    the control API's form of the same answer, for a script or your own AI.
  --help    this text.

Exit codes:
  0   the screen was produced and every state on it was established. "The daemon is not running"
      is one of those answers, so a stopped daemon still exits 0.
  3   the screen was produced and something on it could not be determined.
  1   no screen could be produced, and stderr says why.

It never starts the daemon and never creates the store. For the deployment assumptions behind
these answers: omw health. To hand the details to whoever supports you: omw diagnostics.
`

func init() {
	cli.Register(&cli.Command{
		Name:    "status",
		Summary: "one screen: is everything running?",
		Run:     runStatus,
	})
}

func runStatus(env cli.Env) int {
	asJSON := false
	for _, a := range env.Args {
		switch a {
		case "--json":
			asJSON = true
		case "-h", "--help", "help":
			io.WriteString(env.Stdout, statusUsage)
			return cli.Success
		default:
			fmt.Fprintf(env.Stderr, "omw status: unknown argument %q\n", a)
			io.WriteString(env.Stderr, statusUsage)
			return cli.ExitUsage
		}
	}

	screen := collectStatus(env, time.Now().UTC())

	if asJSON {
		body, err := screen.ControlJSON()
		if err != nil {
			// NO PARTIAL SCREEN AND NO ZERO EXIT (criterion 19). A truncated answer that exits like
			// a whole one is the failure this criterion names, so nothing is printed on stdout at
			// all and the outcome says the tool could not answer.
			fmt.Fprintf(env.Stderr, "omw status: the screen could not be serialised: %v\n", err)
			fmt.Fprintf(env.Stderr, "  No status has been printed: a screen that cannot be produced is not a screen saying everything is fine.\n")
			return cli.ExitFailure
		}
		io.WriteString(env.Stdout, body)
	} else {
		io.WriteString(env.Stdout, screen.Render())
	}
	return statusCode(screen)
}

// collectStatus gathers the inputs and asks internal/status for the screen.
//
// THE LIVENESS ANSWER COMES FROM daemonLiveness AND FROM NOWHERE ELSE (Issue #41). The daemon's
// own report is read for the facts only it has — how the last run ended (criterion 2) and what its
// control API did (criterion 17) — and internal/status is documented not to read its Running
// field, so there is exactly one answer to "is it running" on this screen.
//
// A store path that will not resolve is NOT a reason to skip the screen: the zero Report carries an
// undetermined ending and an undetermined control state, which are the honest answers when the
// store this is all about could not be located, and the other five lines still get asked
// (criterion 7).
func collectStatus(env cli.Env, now time.Time) status.Screen {
	live, why := daemonLiveness(env)
	var rep daemon.Report
	if root, err := store.Resolve(env.Getenv); err == nil {
		rep = daemon.Inspect(root)
	}
	return status.Collect(status.Query{
		Getenv:    env.Getenv,
		Now:       now,
		Daemon:    live,
		DaemonWhy: why,
		Report:    rep,
	})
}

// statusCode is criterion 13's distinction, as an exit code.
//
// A SCREEN THAT WAS PRODUCED IS A SUCCESS, EVEN WHEN WHAT IT SAYS IS BAD NEWS. "The daemon is not
// running" is an answer the person asked for and got, so it exits 0 — the tool did its job. Only
// an answer the tool could not establish moves the code, and it moves to ExitUndetermined rather
// than to ExitFailure, so that a script can never read "I could not check" as "the answer is no".
// ExitFailure is reserved for the case where no screen exists at all.
func statusCode(screen status.Screen) int {
	if screen.AnyUndetermined() {
		return cli.ExitUndetermined
	}
	return cli.Success
}
