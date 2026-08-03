package commands

import (
	"fmt"
	"path/filepath"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/diagnostics"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// `omw diagnostics <path> [--include-bodies]` — PRD §3.9's support bundle.
//
// # THE FLAG IS THE ONLY WAY IN, AND IT IS SPELLED OUT
//
// Criterion 6: including bodies is "an affirmative act by the person, never a default and never
// implied by any other option". So this command has exactly one flag, it defaults to off, it has no
// short form that could be typed by accident, and no other argument sets it. There is deliberately
// no `--all`, no `--verbose` and no `--full`: an option whose name does not say "bodies" must never
// be the thing that puts a person's tickets in a file they are about to email.
//
// # WHY THE EXIT CODE IS NOT THREE-VALUED HERE
//
// Most commands in this package are, because most of them ANSWER a question. This one performs an
// act: it either produced a bundle or it did not (criterion 15). The three-valued answers live
// INSIDE the bundle — a subsystem that could not be read is an undetermined category in the
// manifest, not a non-zero exit — and a run that could not determine the daemon's state has still
// successfully produced a complete bundle saying so. Exiting 3 there would tell a script that no
// bundle exists when one does.
//
// # NOTHING IS STARTED AND NOTHING IS SENT
//
// PRD §4.2: no command starts the daemon. This one asks internal/diagnostics, which asks
// daemon.Inspect, which reads a lock file. Criterion 13: the command's last act is printing a path.
// It has no transport, no upload and no "would you like to send this?".
func init() {
	cli.Register(&cli.Command{
		Name:    "diagnostics",
		Summary: "write a support bundle that says what it contains and what it withholds",
		Run:     runDiagnostics,
	})
}

// includeBodiesFlag is the one affirmative act. Named as a constant so the test that asserts no
// other spelling turns bodies on has something to compare against.
const includeBodiesFlag = "--include-bodies"

// diagnosticsProduce is the bundle writer, replaced in tests that need to drive a failure path
// without arranging an unwritable filesystem.
var diagnosticsProduce = diagnostics.Produce

func runDiagnostics(env cli.Env) int {
	var dest string
	includeBodies := false
	for _, arg := range env.Args {
		switch {
		case arg == includeBodiesFlag:
			includeBodies = true
		case len(arg) > 0 && arg[0] == '-':
			// NAMED, NOT IGNORED, AND ESPECIALLY NOT TREATED AS A REQUEST FOR MORE. An unknown flag
			// on a command that can disclose a person's material is refused, because the failure
			// mode of guessing is disclosing something nobody asked for.
			fmt.Fprintf(env.Stderr, "omw diagnostics: unknown option %q\n", arg)
			fmt.Fprintf(env.Stderr, "usage: omw diagnostics <path> [%s]\n", includeBodiesFlag)
			return cli.ExitUsage
		case dest == "":
			dest = arg
		default:
			fmt.Fprintf(env.Stderr, "omw diagnostics: takes one path, got a second %q\n", arg)
			return cli.ExitUsage
		}
	}
	if dest == "" {
		fmt.Fprintf(env.Stderr, "omw diagnostics: needs a path to write the bundle to\n")
		fmt.Fprintf(env.Stderr, "usage: omw diagnostics <path> [%s]\n", includeBodiesFlag)
		return cli.ExitUsage
	}
	// THE DESTINATION IS ABSOLUTE, AND THAT IS A DECISION THIS COMMAND MAKES ON PURPOSE.
	//
	// A bundle is an artifact the person is about to hand to somebody else. Where it lands must be
	// unambiguous, and it must not depend on which directory they happened to be standing in — the
	// store package already refuses the same thing in the same words: "a store in whatever folder
	// somebody happened to be standing in is exactly the store this product promises not to create".
	//
	// It is also what makes `omw diagnostics list` a usage error rather than a directory called
	// `list` full of facts about somebody's machine, appearing wherever they ran it. That is not a
	// hypothetical: the suite's sweep over every registered command found exactly that.
	//
	// Issue #20 does not settle this, and it is flagged in the pull request as a decision taken here.
	if !filepath.IsAbs(dest) {
		fmt.Fprintf(env.Stderr, "omw diagnostics: %q is not an absolute path\n", dest)
		fmt.Fprintf(env.Stderr, "  a bundle is something you hand to somebody, so where it is written is stated in full\n")
		fmt.Fprintf(env.Stderr, "  rather than taken from the directory you happen to be in.\n")
		fmt.Fprintf(env.Stderr, "usage: omw diagnostics /absolute/path [%s]\n", includeBodiesFlag)
		return cli.ExitUsage
	}

	res, err := diagnosticsProduce(diagnostics.Options{
		Dest:          dest,
		IncludeBodies: includeBodies,
		Getenv:        env.Getenv,
		// THE PRODUCT'S ONE LIVENESS ANSWER (Issue #41). Not a probe of this command's own, and not
		// a socket path derived here — see liveness.go for why a second definition is the defect.
		Liveness: func() (tri.Value, string) { return daemonLiveness(env) },
	})
	if err != nil {
		// NON-ZERO AND NOTHING LEFT BEHIND (criterion 15). Produce stages the bundle elsewhere and
		// renames it into place, so a failure leaves no directory at the path a person would look at
		// and mistake for a bundle.
		fmt.Fprintf(env.Stderr, "omw diagnostics: no bundle was produced: %v\n", err)
		return cli.ExitFailure
	}

	fmt.Fprintf(env.Stdout, "bundle: %s\n", res.Path)
	fmt.Fprintf(env.Stdout, "manifest: %s\n", filepath.Join(res.Path, "manifest.json"))
	// THE SUMMARY IS PRINTED FROM THE MANIFEST, not from a second accounting of what happened. A
	// summary computed independently is a second statement of the contents, and two statements
	// eventually disagree.
	fmt.Fprintf(env.Stdout, "bodies: %s\n", res.Manifest.BodiesRequest)
	fmt.Fprintf(env.Stdout, "\nwhat this bundle holds, and what it does not:\n")
	for _, c := range res.Manifest.Categories {
		switch c.State {
		case diagnostics.StateCollected:
			fmt.Fprintf(env.Stdout, "  %-38s %s (%d)\n", c.Name, c.State, c.Items)
		default:
			fmt.Fprintf(env.Stdout, "  %-38s %s: %s\n", c.Name, c.State, c.Reason)
		}
	}
	// SAID OUT LOUD. The person is about to decide whether to send this, and the two facts they
	// need are that nothing has been sent and that the file says what is in it.
	fmt.Fprintf(env.Stdout, "\nnothing has been sent anywhere; handing this over is your act.\n")
	if !res.Manifest.BodiesIncluded {
		fmt.Fprintf(env.Stdout, "ticket, draft and message bodies are NOT in this bundle. Re-run with %s to include them.\n", includeBodiesFlag)
	} else {
		fmt.Fprintf(env.Stdout, "you asked for bodies, so ticket, draft and message text IS in this bundle. Your model key is not.\n")
	}
	return cli.Success
}
