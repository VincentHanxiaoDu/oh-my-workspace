// Command `omw visibility` — Issue #12, the CLI half of choosing who can see a note.
//
// This file is the ONLY file this Issue adds to package commands. Nothing here is referenced by
// another command file, so two Issues adding commands never touch the same line.
package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "visibility",
		Summary: "choose and read back who can see a note",
		Run:     runVisibility,
	})
}

// Environment this command reads.
//
// THERE IS NO SOCKET VARIABLE HERE ANY MORE (Issue #41). This file used to name its own
// `OMW_CONTROL_SOCKET` constant and stat whatever it pointed at; nothing in the product ever set
// it, so the check answered "not running" whatever the daemon was doing. Liveness now has one
// definition for the whole product — see liveness.go — and it is derived from the store, not from
// an environment variable a person would have had to know to set.
const (
	envHub = "OMW_HUB"
)

// visibilitySource is how this command reaches a hub.
//
// THERE IS NO TRANSPORT IN THIS BUILD, and that is stated rather than stubbed silently. Issue #12
// is the hub-side foundation; nothing has yet built the client-to-hub transport, so the default
// implementation reports the hub as unreachable — which, per criterion 16, renders as UNDETERMINED
// and exits cli.ExitUndetermined. That is the honest answer for a configured hub this build cannot
// talk to, and it is also exactly the path a real unreachable hub will take once a transport exists.
//
// Tests replace it to drive the hub-backed paths against an in-memory [hub.Store].
var visibilitySource = func(env cli.Env) (*hub.Store, error) {
	return nil, hub.ErrHubUnreachable
}

func runVisibility(env cli.Env) int {
	if len(env.Args) == 0 {
		visibilityUsage(env.Stdout)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "choices":
		return visibilityChoices(env)
	case "schema":
		return visibilitySchema(env)
	case "plan":
		return visibilityPlan(env, env.Args[1:])
	case "show":
		return visibilityShow(env, env.Args[1:])
	case "scopes":
		return visibilityScopes(env)
	case "-h", "--help", "help":
		visibilityUsage(env.Stdout)
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw visibility: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw visibility help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func visibilityUsage(w io.Writer) {
	fmt.Fprint(w, `omw visibility — who can see a note

usage: omw visibility <subcommand>

  choices              the four choices, and what restriction does and does not do
  plan <choice>        state a draft's intended visibility (local; no hub needed)
  show <note> [--as p] read a published note's visibility back (needs the hub)
  schema               the agent API's schema for the visibility field
  scopes               the one scope vocabulary, shared by CLI, agent API and hub
`)
}

// visibilityChoices is a POINT OF CHOICE and carries the §2.4 statement, every time (criteria 7
// and 9). It is entirely local: it reaches nothing (criterion 19).
func visibilityChoices(env cli.Env) int {
	fmt.Fprint(env.Stdout, hub.ChoiceBlock())
	fmt.Fprintf(env.Stdout, "\nSay nothing and a note is %s\n", hub.Default().Describe())
	return cli.Success
}

// visibilityPlan records a draft's intended visibility.
//
// LOCAL AND COMPLETE AS FAR AS THIS ISSUE OWNS IT (criterion 20, PRD §4.4). It parses and echoes
// the choice with the §2.4 statement and needs no hub. PERSISTING the choice onto the draft is the
// outbox's job, and the outbox is Issue #9 — this command deliberately does not invent one. A group
// name is NOT resolved here: resolving membership needs the hub, and criterion 21 says a part that
// genuinely needs the hub must say precisely what is missing rather than half-work.
func visibilityPlan(env cli.Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(env.Stderr, "omw visibility plan: say which of the four choices you want.\n\n")
		fmt.Fprint(env.Stderr, hub.ChoiceBlock())
		return cli.ExitUsage
	}
	v, err := hub.ParseChoice(strings.Join(args, " "))
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw visibility plan: %v (code: %s)\n\n", err, hub.Code(err))
		fmt.Fprint(env.Stderr, hub.ChoiceBlock())
		return cli.ExitFailure
	}

	// THE STATEMENT COMES FIRST AND UNCONDITIONALLY. Not only when the choice narrows — the person
	// is at the point of choice either way, and a statement that appears only on some branches is a
	// statement somebody has to be already looking for.
	fmt.Fprint(env.Stdout, hub.RestrictionStatement+"\n\n")
	fmt.Fprintf(env.Stdout, "visibility: %s\n", v.Token())
	fmt.Fprintf(env.Stdout, "%s\n", v.Describe())

	if v.Kind() == hub.KindGroup {
		if strings.TrimSpace(env.Getenv(envHub)) == "" {
			// Precise about WHAT is missing, and does not claim the group is empty or unknown.
			fmt.Fprintf(env.Stdout, "\nnot checked: %v (code: %s)\n", hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
			fmt.Fprintf(env.Stdout, "the group name is retained on the draft as written; whether the hub knows it is settled when you publish.\n")
		}
	}
	return cli.Success
}

// visibilityShow reads a published note's visibility back.
//
// This is where the three-way distinction is spent: a real value, a refusal, and undetermined,
// each with its own exit code and its own wording.
func visibilityShow(env cli.Env, args []string) int {
	var id string
	as := hub.PersonID("")
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--as" && i+1 < len(args):
			i++
			as = hub.PersonID(args[i])
		case strings.HasPrefix(args[i], "--as="):
			as = hub.PersonID(strings.TrimPrefix(args[i], "--as="))
		case id == "":
			id = args[i]
		default:
			fmt.Fprintf(env.Stderr, "omw visibility show: unexpected argument %q\n", args[i])
			return cli.ExitUsage
		}
	}
	if id == "" {
		fmt.Fprintf(env.Stderr, "omw visibility show: name a note.\n")
		return cli.ExitUsage
	}

	// NOTHING IMPLICIT, IN THIS ORDER. No hub configured is a determined fact about this machine
	// and is reported as such (criterion 21). Only with a hub configured is the daemon relevant,
	// and a missing daemon is said, never started (criterion 18). Neither path opens a connection.
	if strings.TrimSpace(env.Getenv(envHub)) == "" {
		fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		// PRECISE ABOUT WHAT IS MISSING, and it claims nothing about the note's audience — a
		// command that has not asked the hub knows nothing about who can read anything, and saying
		// so in those terms is criterion 21.
		fmt.Fprintf(env.Stderr, "  who may read a published note is the hub's answer, and there is no hub to ask.\n")
		fmt.Fprintf(env.Stderr, "  configure one and ask again; nothing about this note has been established.\n")
		return cli.ExitFailure
	}
	// ONE DEFINITION OF LIVENESS, AND THREE ANSWERS (Issue #41). A daemon that is running is not
	// reported as absent, and a liveness that could not be established is not reported as a
	// stopped daemon — the second is the specific defect #41 exists to remove.
	if live, why := daemonLiveness(env); live != tri.Yes {
		return reportDaemonNotLive(env, "omw visibility show", live, why)
	}

	store, err := visibilitySource(env)
	if err != nil || store == nil {
		if err == nil {
			err = hub.ErrHubUnreachable
		}
		// CRITERION 16: an unreachable hub is UNDETERMINED. Not company-wide, not self-only, and
		// not silence — and its exit code is not the one a refusal uses.
		fmt.Fprintf(env.Stdout, "visibility: %s\n", hub.UndeterminedDescription)
		fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", err, hub.Code(err))
		return cli.ExitUndetermined
	}

	v, err := store.VisibilityOf(hub.NoteID(id))
	if err != nil {
		switch hub.Code(err) {
		case hub.ErrNoSuchNote.Code:
			fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", err, hub.ErrNoSuchNote.Code)
			return cli.ExitFailure
		case hub.ErrUndetermined.Code, hub.ErrHubUnreachable.Code:
			fmt.Fprintf(env.Stdout, "visibility: %s\n", hub.UndeterminedDescription)
			fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitUndetermined
		default:
			fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitFailure
		}
	}

	fmt.Fprintf(env.Stdout, "visibility: %s\n", v.Token())
	fmt.Fprintf(env.Stdout, "%s\n", v.Describe())
	// Displayed, not chosen — but criterion 8 binds a display too, so the statement rides along
	// whenever the note is narrowed.
	if v.IsNarrowing() {
		fmt.Fprintf(env.Stdout, "\n%s\n", hub.RestrictionStatement)
	}

	if as != "" {
		_, rerr := store.Read(hub.NoteID(id), as)
		switch {
		case rerr == nil:
			fmt.Fprintf(env.Stdout, "\n%s can read it: %s\n", as, tri.Yes.Render("yes", "no"))
		case hub.Code(rerr) == hub.ErrRefused.Code:
			fmt.Fprintf(env.Stdout, "\n%s can read it: %s\n", as, tri.No.Render("yes", "no"))
		case hub.Code(rerr) == hub.ErrNoSuchNote.Code:
			fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", rerr, hub.ErrNoSuchNote.Code)
			return cli.ExitFailure
		default:
			fmt.Fprintf(env.Stdout, "\n%s can read it: %s\n", as, tri.Undetermined.Render("yes", "no"))
			fmt.Fprintf(env.Stderr, "omw visibility show: %v (code: %s)\n", rerr, hub.Code(rerr))
			return cli.ExitUndetermined
		}
	}
	return cli.Success
}

// visibilitySchema prints the agent API's schema — the second point of choice (criterion 7).
func visibilitySchema(env cli.Env) int {
	s, err := hub.AgentAPISchemaJSON()
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw visibility schema: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprint(env.Stdout, s)
	return cli.Success
}

// visibilityScopes prints the one scope vocabulary (criterion 13). The CLI does not have its own
// list; it prints the hub's.
func visibilityScopes(env cli.Env) int {
	for _, s := range hub.Vocabulary() {
		fmt.Fprintf(env.Stdout, "%s\n", string(s))
	}
	fmt.Fprintf(env.Stdout, "\n%d scopes. The same names mean the same thing on the CLI, the agent API and the hub.\n",
		len(hub.Vocabulary()))
	fmt.Fprintf(env.Stdout, "%s\n", hub.RestrictionStatement)
	return cli.Success
}
