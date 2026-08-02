// Package cli is the command registry `omw` dispatches through.
//
// WHY A REGISTRY AND NOT A SWITCH IN main. There are twenty-three open feature Issues, each adding
// at least one subcommand. A switch statement in main.go is a single file that all twenty-three
// branches must edit, so the first to merge conflicts with the other twenty-two — a serialisation
// with no design reason behind it, produced entirely by where the code was put. Here a command is
// a file: it calls Register from an init, and adding one touches nothing that already exists.
//
// The registry is also why `omw` can refuse an unknown command by name instead of falling through
// to a usage dump — see Run.
package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// ExitCode values with meaning beyond "it worked".
//
// `could not determine` and `determined to be nothing` must never share an exit code — the
// project's standing rule. A command that established a negative answer succeeded at answering and
// exits Success; a command that could not establish anything exits ExitUndetermined. A caller
// scripting against `omw` can tell those apart without parsing prose, which is the whole point.
const (
	// Success means the command answered. It does NOT mean the answer was affirmative: health that
	// determined full-disk encryption is OFF has succeeded (PRD §4.1, "a report, never a blocker").
	Success = 0
	// ExitFailure means the command could not do what was asked, and says why on stderr.
	ExitFailure = 1
	// ExitUsage means the invocation itself was wrong — unknown command, bad flags.
	ExitUsage = 2
	// ExitUndetermined means the command ran but could not determine the answer it was asked for.
	// Distinct from ExitFailure so that "I could not check" is never scripted as "the answer is no".
	ExitUndetermined = 3
)

// Command is one `omw` subcommand.
type Command struct {
	// Name is the word typed after `omw`. Sub-subcommands (`store create`) are the command's own
	// business: it receives its remaining arguments in Args and may dispatch further.
	Name string
	// Summary is one line, shown in the command list.
	Summary string
	// Run does the work and returns the process exit code. It is given its streams rather than
	// reaching for os.Stdout, so a test can drive a command and read what a person would see —
	// which is what most of the acceptance criteria in the open Issues actually assert on.
	Run func(env Env) int
}

// Env is everything a command may touch from the outside world.
//
// Commands take this rather than reaching for os.Stdout / os.Args / os.Getenv directly, because
// nearly every open Issue asserts on exact output and on the distinction between stdout and
// stderr. A command that writes to a package-level stream cannot be driven by a test without a
// global swap, and two such tests cannot run in parallel.
type Env struct {
	// Args are the arguments after the command name.
	Args []string
	// Stdout is where the answer goes.
	Stdout io.Writer
	// Stderr is where the reason goes when something could not be done.
	Stderr io.Writer
	// Getenv reads the environment. Injected so a test can drive configuration — notably "no hub
	// configured", which many Issues require be observable — without mutating the real process.
	Getenv func(string) string
}

var (
	mu       sync.Mutex
	registry = map[string]*Command{}
)

// Register adds a command. It is called from an init in the command's own file.
//
// It PANICS on a duplicate name or a malformed command. This is deliberate: both are programmer
// errors fixed before the binary can usefully run, and the alternative — silently keeping one of
// two commands with the same name — means a person types `omw status` and reaches whichever file
// the linker happened to initialise second. A crash at startup is found by any test that builds
// the binary; a coin-flip dispatch is found by nobody.
func Register(c *Command) {
	if c == nil {
		panic("cli.Register: nil command")
	}
	if c.Name == "" {
		panic("cli.Register: command with no name")
	}
	if c.Run == nil {
		panic("cli.Register: command " + c.Name + " has no Run")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[c.Name]; dup {
		panic("cli.Register: duplicate command " + c.Name)
	}
	registry[c.Name] = c
}

// Commands returns the registered commands, ordered by name.
func Commands() []*Command {
	mu.Lock()
	defer mu.Unlock()
	out := make([]*Command, 0, len(registry))
	for _, c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup returns a registered command by name.
func Lookup(name string) (*Command, bool) {
	mu.Lock()
	defer mu.Unlock()
	c, ok := registry[name]
	return c, ok
}

// Run dispatches argv (without the program name) and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if len(args) == 0 {
		usage(stdout)
		return ExitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(stdout)
		return Success
	}
	cmd, ok := Lookup(args[0])
	if !ok {
		// NAMED, NOT A USAGE DUMP. "unknown command: staus" tells a person what went wrong; a wall
		// of help text makes them find it. The name is echoed back so a typo is visible.
		fmt.Fprintf(stderr, "omw: unknown command %q\n", args[0])
		fmt.Fprintf(stderr, "run 'omw help' for the commands this build has.\n")
		return ExitUsage
	}
	return cmd.Run(Env{
		Args:   args[1:],
		Stdout: stdout,
		Stderr: stderr,
		Getenv: getenv,
	})
}

func usage(w io.Writer) {
	var b strings.Builder
	b.WriteString("omw — a local client for your workspace\n\nusage: omw <command> [arguments]\n\n")
	cmds := Commands()
	if len(cmds) == 0 {
		// SAID, NOT LEFT BLANK. An empty command list is a real state of this build (it is the
		// state right after this scaffold lands) and it must not print as an empty section that
		// reads like a rendering bug.
		b.WriteString("This build has no commands registered.\n")
	} else {
		b.WriteString("commands:\n")
		width := 0
		for _, c := range cmds {
			if len(c.Name) > width {
				width = len(c.Name)
			}
		}
		for _, c := range cmds {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, c.Name, c.Summary)
		}
	}
	io.WriteString(w, b.String())
}
