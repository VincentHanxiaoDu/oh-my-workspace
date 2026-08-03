package hubd

import (
	"context"
	"fmt"
	"io"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// Usage is what `omw-hub` prints when it is not told what to do.
//
// THE DIRECTORY IS ALWAYS AN ARGUMENT AND NEVER A DEFAULT. PRD §4.2: nothing implicit. A hub that
// fell back to some conventional path would, on a mistyped invocation, quietly serve or create a
// second empty corpus — and an empty corpus is the one answer this product must never give by
// accident.
const Usage = `omw-hub — the hub for one company: it holds published notes and answers questions about them.

  omw-hub create <directory> [company]   make a hub in a directory that is not one yet
  omw-hub serve <directory>              run the hub against an existing hub directory
  omw-hub describe <directory>           say what a hub holds, and what it can read
  omw-hub what-i-can-read                print PRD §2.4, without opening anything

The directory is always named. This process never picks one for you and never makes one it was
not asked to make.

There is no network transport in this build: the client-to-hub wire is a separate piece of work.
A client with a hub configured therefore reports that the hub could not be reached — which is
"could not be determined", never "there is nothing there".
`

// Run is the hub process. It returns the exit code, and takes its streams so a test drives exactly
// what a person sees.
//
// serveFor is the context the `serve` command runs until. A caller with no reason to stop it passes
// context.Background(); the process's main passes one cancelled by a signal.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, Usage)
		return cli.ExitUsage
	}
	switch args[0] {
	case "what-i-can-read":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "omw-hub what-i-can-read takes no arguments\n")
			return cli.ExitUsage
		}
		fmt.Fprintln(stdout, OperatorReach)
		return cli.Success

	case "create":
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprintf(stderr, "omw-hub create <directory> [company]\n")
			return cli.ExitUsage
		}
		company := ""
		if len(args) == 3 {
			company = args[2]
		}
		if err := Create(args[1], Options{Company: company}); err != nil {
			return report(stderr, err)
		}
		fmt.Fprintf(stdout, "hub created in %s\n", args[1])
		fmt.Fprintf(stdout, "It holds nothing yet. Nothing is published to it by this command.\n\n")
		fmt.Fprintln(stdout, OperatorReach)
		return cli.Success

	case "describe":
		if len(args) != 2 {
			fmt.Fprintf(stderr, "omw-hub describe <directory>\n")
			return cli.ExitUsage
		}
		s, err := Open(args[1], Options{})
		if err != nil {
			return report(stderr, err)
		}
		defer func() { _ = s.Close() }()
		fmt.Fprint(stdout, s.Describe().Render())
		return cli.Success

	case "serve":
		if len(args) != 2 {
			fmt.Fprintf(stderr, "omw-hub serve <directory>\n")
			return cli.ExitUsage
		}
		return serve(ctx, args[1], stdout, stderr)

	default:
		fmt.Fprintf(stderr, "omw-hub: no such command %q\n\n%s", args[0], Usage)
		return cli.ExitUsage
	}
}

// serve runs the hub until the context is done, then stops.
//
// IT SAYS WHAT IT CANNOT DO, ON THE WAY UP. A person starting a server expects it to be reachable;
// in this build it is not, because the wire is separate work. Saying so at start-up is the same
// choice `auth.Unreachable` makes on the client side, and for the same reason: an unstated gap gets
// read as a working one.
func serve(ctx context.Context, dir string, stdout, stderr io.Writer) int {
	s, err := Open(dir, Options{})
	if err != nil {
		return report(stderr, err)
	}
	defer func() { _ = s.Close() }()

	fmt.Fprint(stdout, s.Describe().Render())
	fmt.Fprintf(stdout, "\nThis hub is running and is NOT reachable over a network: this build has no\n"+
		"client-to-hub transport. It answers in-process callers only.\n")

	<-ctx.Done()

	// A HALT IS NOT A CLEAN STOP. If the hub stopped because it could not write, the process says
	// so and exits non-zero, so whatever supervises it does not record a healthy shutdown.
	if h := s.Halted(); h != nil {
		fmt.Fprintf(stderr, "omw-hub: %v\n", h)
		return cli.ExitFailure
	}
	fmt.Fprintf(stdout, "hub stopped.\n")
	return cli.Success
}

// report prints a refusal and chooses its exit code.
//
// THE THIRD CODE IS THE POINT. A hub whose durable record could not be read has not determined that
// it holds nothing — it has determined nothing at all — and exits ExitUndetermined, which success
// and failure both do not use.
func report(stderr io.Writer, err error) int {
	code := hub.Code(err)
	fmt.Fprintf(stderr, "omw-hub: %v\n", err)
	if code != "" {
		fmt.Fprintf(stderr, "code: %s\n", code)
	}
	switch code {
	case ErrHubRecordUnreadable.Code, ErrHubFormat.Code, ErrHubHalted.Code, hub.ErrUndetermined.Code:
		return cli.ExitUndetermined
	}
	return cli.ExitFailure
}
