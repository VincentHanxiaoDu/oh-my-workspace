// Command `omw references` — Issue #14, following a note's edges and asking what else was written
// about something.
//
// This file is the ONLY file this Issue adds to package commands, and it edits none of the others.
// It reuses `envHub`, `envSocket` and the `daemonRunning` probe declared by `omw visibility`
// rather than declaring a second pair: "is the daemon running" is one product question, and two
// probes are two answers waiting to disagree.
package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "references",
		Summary: "follow a note's references, and ask what else was written about something",
		Run:     runReferences,
	})
}

// referencesSource is how this command reaches a hub.
//
// THERE IS STILL NO TRANSPORT IN THIS BUILD, exactly as for `omw visibility`, and it is stated
// rather than stubbed silently: the default reports the hub unreachable, which renders every
// reference as UNDETERMINED and exits cli.ExitUndetermined. That is the honest answer for a
// configured hub this build cannot talk to, and it is the same path a genuinely unreachable hub
// will take once a transport exists. Tests replace it with an in-memory [hub.Store].
var referencesSource = func(env cli.Env) (*hub.Store, error) {
	return nil, hub.ErrHubUnreachable
}

func runReferences(env cli.Env) int {
	if len(env.Args) == 0 {
		referencesUsage(env.Stdout)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "syntax":
		return referencesSyntax(env)
	case "scan":
		return referencesScan(env, env.Args[1:])
	case "of":
		return referencesOf(env, env.Args[1:])
	case "to":
		return referencesToCmd(env, env.Args[1:])
	case "-h", "--help", "help":
		referencesUsage(env.Stdout)
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw references: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw references help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func referencesUsage(w io.Writer) {
	fmt.Fprint(w, `omw references — a note's edges to people, groups and other notes

usage: omw references <subcommand>

  syntax                  how a reference is written, and how each state reads (local)
  scan <file>             the references in a local draft (local; resolving them needs the hub)
  of <note> [--as p] [--version n]
                          list a published note's references as that reader sees them
  to <kind:target> [--as p]
                          what else was written about this — the notes that reference it
`)
}

// referencesSyntax is entirely local and reaches nothing (criterion 15).
func referencesSyntax(env cli.Env) int {
	fmt.Fprint(env.Stdout, "How a reference is written:\n")
	for _, line := range hub.ReferenceSyntax {
		fmt.Fprintf(env.Stdout, "  %s\n", line)
	}
	fmt.Fprint(env.Stdout, "\nHow each state reads in a note's body:\n")
	// The three states, from the hub's own renderer. The CLI does not spell them a second time.
	r := hub.Reference{Kind: hub.RefNote, Target: "note-9"}
	for _, st := range []hub.RefState{hub.StateResolved, hub.StateUnresolved, hub.StateUndetermined} {
		fmt.Fprintf(env.Stdout, "  %-13s %s\n", st.String(), hub.RenderReference(r, st))
	}
	fmt.Fprint(env.Stdout, "\nA reference to something you cannot see is not shown at all, and is not counted.\n")
	fmt.Fprint(env.Stdout, "That is not the same fact as a reference whose target is gone, which is shown and marked.\n")
	return cli.Success
}

// referencesScan is PRD §4.4 and criterion 16: the local half, standing alone.
//
// Extracting the references from a draft is COMPLETE with no hub — it is a property of the text.
// Resolving them is not: who a person is, what a group is, and whether a note exists are all the
// hub's answers (PRD §5.3). So this command does the local half fully, says precisely what is
// missing and names the hub as the missing piece, and reports the result as PARTIAL — exit
// cli.ExitUndetermined, distinguishable by exit status alone from the complete answer a draft with
// no references gets.
func referencesScan(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw references scan: name one file holding the draft.\n")
		return cli.ExitUsage
	}
	body, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw references scan: %v\n", err)
		return cli.ExitFailure
	}
	refs := hub.ParseReferences(string(body))
	if len(refs) == 0 {
		// COMPLETE, and it needed nothing. A draft with no references has a fully determined
		// reference set, and saying so is not the same as saying nothing.
		fmt.Fprintf(env.Stdout, "references: 0\nthis draft references nothing, and that is a complete answer.\n")
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "references: %d\n", len(refs))
	for _, r := range refs {
		fmt.Fprintf(env.Stdout, "  %-6s %s\n", string(r.Kind), r.Target)
	}
	hubName := strings.TrimSpace(env.Getenv(envHub))
	fmt.Fprintf(env.Stdout, "\nwhat these point at: %s\n", tri.Undetermined.String())
	if hubName == "" {
		fmt.Fprintf(env.Stderr, "omw references scan: %v (code: %s)\n", hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
	} else {
		fmt.Fprintf(env.Stderr, "omw references scan: this build has no client-to-hub transport (code: %s)\n", hub.ErrHubUnreachable.Code)
	}
	// PRECISE ABOUT WHAT IS MISSING, and it claims nothing about the targets. Not "no such note",
	// not "unresolved" — neither was established.
	fmt.Fprintf(env.Stderr, "  the hub is the missing piece: who a person is, what a group is, and whether a note\n")
	fmt.Fprintf(env.Stderr, "  exists are its answers. The references above were read from your draft and are complete;\n")
	fmt.Fprintf(env.Stderr, "  whether each one resolves is not, so this answer is partial.\n")
	return cli.ExitUndetermined
}

// reach settles the local preconditions and returns a store, or the exit code to return.
//
// NOTHING IMPLICIT, IN THIS ORDER, and it is the order `omw visibility` uses: no hub configured is
// a determined fact about this machine; only then is the daemon relevant, and a missing daemon is
// said, never started (criterion 15). Neither path opens a connection.
func reach(env cli.Env, verb string) (*hub.Store, int, bool) {
	if strings.TrimSpace(env.Getenv(envHub)) == "" {
		fmt.Fprintf(env.Stderr, "omw references %s: %v (code: %s)\n", verb, hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		fmt.Fprintf(env.Stderr, "  a published note's references are the hub's answer, and there is no hub to ask.\n")
		return nil, cli.ExitFailure, false
	}
	if !daemonRunning(env) {
		fmt.Fprintf(env.Stderr, "omw references %s: %v (code: %s)\n", verb, hub.ErrDaemonNotRunning, hub.ErrDaemonNotRunning.Code)
		return nil, cli.ExitFailure, false
	}
	store, err := referencesSource(env)
	if err != nil || store == nil {
		if err == nil {
			err = hub.ErrHubUnreachable
		}
		// UNDETERMINED, never "there are none" (criterion 14).
		fmt.Fprintf(env.Stdout, "references: %s\n", tri.Undetermined.String())
		fmt.Fprintf(env.Stderr, "omw references %s: %v (code: %s)\n", verb, err, hub.Code(err))
		return nil, cli.ExitUndetermined, false
	}
	return store, cli.Success, true
}

// parseAs pulls `--as <person>` and `--version <n>` out of args and returns the positional word.
func parseAs(args []string) (positional string, as hub.PersonID, version int, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--as" && i+1 < len(args):
			i++
			as = hub.PersonID(args[i])
		case strings.HasPrefix(a, "--as="):
			as = hub.PersonID(strings.TrimPrefix(a, "--as="))
		case a == "--version" && i+1 < len(args):
			i++
			if _, e := fmt.Sscanf(args[i], "%d", &version); e != nil {
				return "", "", 0, fmt.Errorf("--version wants a number, not %q", args[i])
			}
		case strings.HasPrefix(a, "--version="):
			if _, e := fmt.Sscanf(strings.TrimPrefix(a, "--version="), "%d", &version); e != nil {
				return "", "", 0, fmt.Errorf("--version wants a number, not %q", a)
			}
		case positional == "":
			positional = a
		default:
			return "", "", 0, fmt.Errorf("unexpected argument %q", a)
		}
	}
	return positional, as, version, nil
}

// referencesOf lists a note's outbound references as one reader sees them.
func referencesOf(env cli.Env, args []string) int {
	id, as, version, err := parseAs(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw references of: %v\n", err)
		return cli.ExitUsage
	}
	if id == "" {
		fmt.Fprintf(env.Stderr, "omw references of: name a note.\n")
		return cli.ExitUsage
	}
	store, code, ok := reach(env, "of")
	if !ok {
		return code
	}

	l, err := hub.OutboundReferences(store, hub.NoteID(id), version, as)
	if err != nil {
		switch hub.Code(err) {
		case hub.ErrUndetermined.Code, hub.ErrHubUnreachable.Code:
			fmt.Fprintf(env.Stdout, "references: %s\n", tri.Undetermined.String())
			fmt.Fprintf(env.Stderr, "omw references of: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitUndetermined
		default:
			fmt.Fprintf(env.Stderr, "omw references of: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitFailure
		}
	}

	// THE COUNT IS THE READER'S COUNT (criterion 18). hub.Listing has no other one to print.
	// Issue #11's ref spelling, so the version these references belong to can be pasted straight
	// into `omw versions`.
	fmt.Fprintf(env.Stdout, "references of %s\n", l.Ref)
	fmt.Fprintf(env.Stdout, "references: %d\n", l.Count())
	for _, v := range l.Refs {
		switch v.State {
		case hub.StateUndetermined:
			// No kind, no target: it is undetermined whether this reader may see it.
			fmt.Fprintf(env.Stdout, "  %s\n", hub.RenderReference(v.Ref, v.State))
		default:
			fmt.Fprintf(env.Stdout, "  %-6s %-12s %s\n", string(v.Ref.Kind), v.State.String(), v.Ref.Target)
		}
	}
	fmt.Fprintf(env.Stdout, "\nbody:\n%s\n", l.Body)
	if l.Undetermined() > 0 {
		fmt.Fprintf(env.Stderr, "omw references of: %d reference(s) could not be determined (code: %s)\n",
			l.Undetermined(), hub.ErrUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}

// referencesToCmd is "what else was written about this" (criterion 6).
func referencesToCmd(env cli.Env, args []string) int {
	spec, as, _, err := parseAs(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw references to: %v\n", err)
		return cli.ExitUsage
	}
	kind, target, found := strings.Cut(spec, ":")
	if !found || !hub.KnownRefKind(hub.RefKind(kind)) || strings.TrimSpace(target) == "" {
		fmt.Fprintf(env.Stderr, "omw references to: name a target as person:<id>, group:<name> or note:<id>.\n")
		return cli.ExitUsage
	}
	store, code, ok := reach(env, "to")
	if !ok {
		return code
	}

	b, err := hub.ReferencesTo(store, hub.Reference{Kind: hub.RefKind(kind), Target: strings.TrimSpace(target)}, as)
	if err != nil {
		fmt.Fprintf(env.Stdout, "notes referencing this: %s\n", tri.Undetermined.String())
		fmt.Fprintf(env.Stderr, "omw references to: %v (code: %s)\n", err, hub.Code(err))
		return cli.ExitUndetermined
	}

	// CRITERION 8 AND 9: this output is built from the reader's readable set alone. Nothing was
	// looked up about the target, so a target that does not exist and a target this reader may not
	// see produce this same text — including the count, which is a count of THIS READER's notes.
	fmt.Fprintf(env.Stdout, "notes referencing %s %s: %d\n", kind, strings.TrimSpace(target), b.Count())
	for _, n := range b.Notes {
		fmt.Fprintf(env.Stdout, "  %-10s %s\n", string(n.ID), n.Title)
	}
	if b.Undetermined > 0 {
		// SAID, NOT FOLDED IN. These notes were not examined, so the answer above is partial.
		fmt.Fprintf(env.Stderr, "omw references to: %d note(s) could not be examined because whether you may read them could not be determined (code: %s)\n",
			b.Undetermined, hub.ErrUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}
