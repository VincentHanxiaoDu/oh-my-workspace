// The `omw projects` command: point the client at directories, see their state, and see whether
// anything is watching (PRD §3.6, §4.2, §4.3, §4.4; Issue #4).
//
// WHAT THIS FILE IS AND IS NOT. It is a rendering surface. Every determination — what state a
// directory is in, whether anything is watching, and above all WHERE THE STATE CAME FROM — is made
// in internal/projects and rendered here by calling projects.Render. That split is criterion 14:
// the control API Issue #2 owns must serve the same projects.Snapshot, and two surfaces that render
// one value cannot disagree about it. A convenience formatting of the state inside this file, "just
// for the CLI", is how they start to.
//
// NOTHING HERE STARTS THE DAEMON (criterion 11, PRD §4.2). There is no code path in this file, or
// anything it calls, that writes a heartbeat, spawns a process or launches a goroutine. Listing with
// the daemon stopped leaves it stopped, and the listing says so.
//
// (Detached from the package clause: doc.go carries this package's doc comment, and several Issues
// add files here concurrently.)

package commands

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "projects",
		Summary: "point the client at directories, and see their state and whether anything is watching",
		Run:     runProjects,
	})
}

var projectsUsage = `usage: omw projects <list|add|remove> [directory]

  list     show every project, its state, and — for each one — whether that state
           came from the daemon's polling or from this command examining the
           directory just now.
  add      point the client at a directory. Adding the same one twice is one project.
  remove   stop tracking a directory. The directory itself is not touched.

Watching is a poll, not an instant notification: while the daemon runs it
re-examines each directory every couple of seconds. With no daemon running
NOTHING watches between commands, and 'list' walks the directories itself.

The scan descends ` + strconv.Itoa(projects.DefaultDepth) + ` directory levels by default; set $` + projects.DepthEnv + ` to change
it. Symbolic links are not followed. node_modules, .venv, venv, __pycache__,
.git, dist, build, .next, target, .cache, vendor and every dot-directory are
skipped. Inside a git repository the ignored set is whatever git itself reports;
no .gitignore file is parsed by this client.

This command needs no hub and makes no network connection.
`

func runProjects(env cli.Env) int {
	if len(env.Args) == 0 {
		io.WriteString(env.Stderr, projectsUsage)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help":
		io.WriteString(env.Stdout, projectsUsage)
		return cli.Success
	case "list":
		return projectsList(env, env.Args[1:])
	case "add":
		return projectsAdd(env, env.Args[1:])
	case "remove":
		return projectsRemove(env, env.Args[1:])
	default:
		fmt.Fprintf(env.Stderr, "omw projects: unknown subcommand %q\n", env.Args[0])
		io.WriteString(env.Stderr, projectsUsage)
		return cli.ExitUsage
	}
}

// openStore finds this device's store WITHOUT CREATING ONE (PRD §4.2, and the store package's first
// invariant). A project command on a machine with no store says there is no store; it does not
// conjure one as a side effect of listing.
//
// "I could not work out where your store would live" exits ExitUndetermined and "there is no store
// there" exits ExitFailure, because those are different answers and the standing rule is that they
// never share an exit code.
func openStore(env cli.Env) (*store.Store, int) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw projects: where the store lives %s.\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return nil, cli.ExitUndetermined
	}
	s, err := store.Open(path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(env.Stderr, "omw projects: there is no store at %s.\n", path)
			fmt.Fprintf(env.Stderr, "  Create one on purpose with 'omw store create'. "+
				"No omw command creates one for you.\n")
			return nil, cli.ExitFailure
		}
		fmt.Fprintf(env.Stderr, "omw projects: the store at %s could not be opened.\n  %v\n", path, err)
		return nil, cli.ExitUndetermined
	}
	return s, cli.Success
}

// oneDirectory reads the single directory argument, refusing flag-shaped arguments rather than
// treating them as a path. `omw projects add --help` must not register a project called "--help".
func oneDirectory(env cli.Env, args []string, verb string) (string, int) {
	var path string
	seen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			for _, rest := range args[i+1:] {
				if seen {
					fmt.Fprintf(env.Stderr, "omw projects %s: more than one directory was given.\n", verb)
					return "", cli.ExitUsage
				}
				path, seen = rest, true
			}
			i = len(args)
		case a == "-h" || a == "--help":
			io.WriteString(env.Stdout, projectsUsage)
			return "", -1
		case strings.HasPrefix(a, "-") && a != "-":
			fmt.Fprintf(env.Stderr, "omw projects %s: unknown option %q. "+
				"It has NOT been treated as a directory.\n", verb, a)
			return "", cli.ExitUsage
		default:
			if seen {
				fmt.Fprintf(env.Stderr, "omw projects %s: more than one directory was given.\n", verb)
				return "", cli.ExitUsage
			}
			path, seen = a, true
		}
	}
	if !seen {
		fmt.Fprintf(env.Stderr, "omw projects %s: which directory?\n", verb)
		io.WriteString(env.Stderr, projectsUsage)
		return "", cli.ExitUsage
	}
	return path, cli.Success
}

func projectsList(env cli.Env, args []string) int {
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" {
			io.WriteString(env.Stdout, projectsUsage)
			return cli.Success
		}
		fmt.Fprintf(env.Stderr, "omw projects list: takes no arguments, got %q\n", args[0])
		return cli.ExitUsage
	}
	s, code := openStore(env)
	if code != cli.Success {
		return code
	}
	// THE ONE LIVENESS ANSWER (Issue #41). Not a socket path stat'd here, not a heartbeat this
	// package writes and reads back: the same call `omw daemon status` renders. The projects
	// package takes it as an argument and has no opinion about how it was established, so there is
	// nothing here that can drift away from what the daemon surface says about the same machine.
	live, why := daemonLiveness(env)
	snap, err := projects.Take(s, env.Getenv, time.Now().UTC(), projects.Liveness{Running: live, Detail: why})
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw projects list: the listing could not be produced.\n  %v\n", err)
		// ExitUndetermined, not ExitFailure: we did not determine the state, and a script must not
		// read this as "you have no projects".
		return cli.ExitUndetermined
	}
	if err := projects.Render(env.Stdout, snap); err != nil {
		fmt.Fprintf(env.Stderr, "omw projects list: %v\n", err)
		return cli.ExitFailure
	}
	// A LISTING THAT COULD NOT SAY WHETHER ANYTHING IS WATCHING HAS NOT ANSWERED THE QUESTION THE
	// ISSUE IS ABOUT. The rows printed above are honest — each carries its own provenance, and that
	// provenance is itself undetermined here — but the person asked "is anything keeping up with
	// these", and nothing established an answer. Exiting Success would let a script read an
	// undetermined watcher as a determined "no", which is the one thing this exit code exists to
	// prevent. The listing is still PRINTED: the state is real, and withholding it would be
	// answering a question nobody asked.
	if !snap.Watching.Determined() {
		return cli.ExitUndetermined
	}
	return cli.Success
}

func projectsAdd(env cli.Env, args []string) int {
	path, code := oneDirectory(env, args, "add")
	if code == -1 {
		return cli.Success
	}
	if code != cli.Success {
		return code
	}
	s, code := openStore(env)
	if code != cli.Success {
		return code
	}
	p, err := projects.Add(s, path)
	if err != nil {
		if errors.Is(err, projects.ErrNotAProject) {
			// CRITERION 13, by exit status alone. The prose below is for the person; the non-zero
			// exit is what a script reads, and it is what makes "refused" distinguishable from
			// "accepted" without parsing a sentence.
			fmt.Fprintf(env.Stderr, "omw projects add: %s is not a directory that exists.\n", path)
			fmt.Fprintf(env.Stderr, "  Nothing was added. A project is a directory; "+
				"a file, or a path that is not there, is not one.\n")
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stderr, "omw projects add: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "project: %s\n", p.Path)
	return cli.Success
}

func projectsRemove(env cli.Env, args []string) int {
	path, code := oneDirectory(env, args, "remove")
	if code == -1 {
		return cli.Success
	}
	if code != cli.Success {
		return code
	}
	s, code := openStore(env)
	if code != cli.Success {
		return code
	}
	removed, err := projects.Remove(s, path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw projects remove: %v\n", err)
		return cli.ExitFailure
	}
	if !removed {
		// SAID, NOT SILENTLY SUCCESSFUL. The person asked to remove something that was not tracked;
		// they are told, and it is still a success because what they wanted is now true.
		fmt.Fprintf(env.Stdout, "not a project: %s (nothing was removed)\n", path)
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "removed: %s (the directory itself is untouched)\n", path)
	return cli.Success
}
