package commands

import (
	"context"
	"fmt"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/health"
)

// `omw health` — reports the deployment assumptions (PRD §3.9), of which full-disk encryption
// (PRD §4.1) is the one this slice checks.
//
// THE EXIT CODE CARRIES THE THREE VALUES TOO, and it is where they most easily collapse:
//
//	enabled                                -> cli.Success (0)
//	not enabled                            -> cli.Success (0)   the run ANSWERED
//	could not be determined on this platform -> cli.ExitUndetermined (3)
//
// `not enabled` exits 0 because health is a report, never a blocker (PRD §4.1, criterion 4): the
// product was asked a question and answered it. Making it non-zero would put health in a person's
// way over a state that is theirs to fix, and would make "your disk is not encrypted" and "I could
// not check" indistinguishable to a script — the exact collapse the project's first rule forbids.
// Exit 3 is reserved for the case where the CHECK could not be completed, and is distinct from
// cli.ExitFailure so that "I could not check" is never scripted as "the answer is no".
//
// Because the reported value and the run's success are separately observable, a run that determined
// `not enabled` (exit 0, "not enabled" on stdout) is distinguishable by its TERMINATION ALONE from
// a run that could not execute the check (exit 3).
func init() {
	cli.Register(&cli.Command{
		Name:    "health",
		Summary: "report the deployment assumptions this product rests on",
		Run:     runHealth,
	})
}

// healthRunner is the runner the command uses, replaced in this package's tests so each of the
// three values can be driven through the real command without owning the machine.
var healthRunner = func(env cli.Env) health.Report {
	return health.Runner{Getenv: env.Getenv}.Run(context.Background())
}

func runHealth(env cli.Env) int {
	if len(env.Args) > 0 {
		// NAMED, NOT IGNORED. Silently accepting an argument health does not have would let a
		// person believe they had scoped the report.
		fmt.Fprintf(env.Stderr, "omw health: takes no arguments, got %q\n", env.Args[0])
		return cli.ExitUsage
	}

	rep := healthRunner(env)
	if err := health.Write(env.Stdout, rep); err != nil {
		fmt.Fprintf(env.Stderr, "omw health: could not write the report: %v\n", err)
		return cli.ExitFailure
	}

	// THE REPORT IS ALREADY WRITTEN before the exit code is decided, and it is written for all
	// three values. Health reports its findings regardless of which value it determined.
	if !rep.Determined() {
		return cli.ExitUndetermined
	}
	return cli.Success
}
