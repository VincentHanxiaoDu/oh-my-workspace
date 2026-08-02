// Command `omw note` — Issue #11, reading a note as it stood when someone acted on it.
//
// This file is the ONLY file this Issue adds to package commands, and it references nothing else in
// the package: the two environment variable names below are spelled out again rather than borrowed
// from the visibility command's constants, so that neither Issue's file appears in the other's diff.
package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "note",
		Summary: "read a note, its timeline, and any version as it stood",
		Run:     runNote,
	})
}

// NO SOCKET VARIABLE (Issue #41). This file used to name its own `OMW_CONTROL_SOCKET` constant —
// a second, independent copy of the same guess the visibility command was making — and stat the
// path it named. Nothing in the product ever set it, so it answered "not running" unconditionally.
// Liveness now has exactly one definition, in liveness.go, and it asks internal/daemon.
const (
	noteEnvHub = "OMW_HUB"
)

// noteSource is how this command reaches a hub.
//
// THERE IS STILL NO TRANSPORT IN THIS BUILD, and this says so rather than pretending. A configured
// hub this build cannot talk to is UNREACHABLE, which criterion 12 renders as a distinct report and
// never as an empty history. Tests replace it to drive the hub-backed paths against an in-memory
// store, and to drive the failing paths against a source that fails the way a transport will.
var noteSource = func(env cli.Env) (hub.VersionSource, *hub.Archive, error) {
	return nil, nil, hub.ErrHubUnreachable
}

func runNote(env cli.Env) int {
	if len(env.Args) == 0 {
		noteUsage(env.Stdout)
		return cli.ExitUsage
	}
	rest := env.Args[1:]
	switch env.Args[0] {
	case "versions":
		return noteVersions(env, rest)
	case "read":
		return noteRead(env, rest)
	case "show":
		return noteShow(env, rest)
	case "search":
		return noteSearch(env, rest)
	case "draft":
		return noteDraft(env, rest)
	case "schema":
		return noteSchema(env)
	case "-h", "--help", "help":
		noteUsage(env.Stdout)
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw note: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw note help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func noteUsage(w io.Writer) {
	fmt.Fprint(w, `omw note — a note, and the timeline you can read it along

usage: omw note <subcommand>

  show <note>            the note as it stands now (needs the hub)
  versions <note>        every version of the note, oldest first (needs the hub)
  read <ref>             one version, for example note-1@v2 (needs the hub)
  search <term>          notes whose current version matches (needs the hub)
  draft <...>            local draft revisions; works with no hub at all
  schema                 the agent API's schema for the version operations

Every read says its standing — current, superseded, or could not be determined — so you never
have to work out which one you are holding from the arguments you typed.

flags:
  --as <person>          read as that colleague
  --json                 the control API's form of the same answer
`)
}

// noteFlags are the flags every hub-facing subcommand takes.
type noteFlags struct {
	as   hub.PersonID
	json bool
	dir  string
	rest []string
}

func parseNoteFlags(args []string, stderr io.Writer) (noteFlags, bool) {
	var f noteFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--as" && i+1 < len(args):
			i++
			f.as = hub.PersonID(args[i])
		case strings.HasPrefix(a, "--as="):
			f.as = hub.PersonID(strings.TrimPrefix(a, "--as="))
		case a == "--json":
			f.json = true
		case a == "--dir" && i+1 < len(args):
			i++
			f.dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			f.dir = strings.TrimPrefix(a, "--dir=")
		case strings.HasPrefix(a, "--"):
			fmt.Fprintf(stderr, "omw note: unknown flag %q\n", a)
			return f, false
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f, true
}

// reachHub applies PRD §4.2 in order, and it is the same order the visibility command uses.
//
// NO HUB CONFIGURED is a determined fact about this machine and is said precisely (criterion 11).
// Only then is the daemon relevant, and a missing daemon is said, never started (criterion 10).
// Neither branch opens a connection — with no hub configured nothing is dialled at all.
func reachHub(env cli.Env, what string) (hub.VersionSource, *hub.Archive, int, bool) {
	if strings.TrimSpace(env.Getenv(noteEnvHub)) == "" {
		fmt.Fprintf(env.Stderr, "omw note %s: %v (code: %s)\n", what, hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		// PRECISE ABOUT WHAT IS MISSING (criterion 11). It does not print an empty timeline, it
		// does not print a one-line timeline, and it says which capability is the hub's.
		fmt.Fprintf(env.Stderr, "  a published note's timeline lives on the hub, and there is no hub to ask.\n")
		fmt.Fprintf(env.Stderr, "  nothing has been established about this note's versions.\n")
		fmt.Fprintf(env.Stderr, "  local draft revisions do not need a hub: see 'omw note draft'.\n")
		return nil, nil, cli.ExitFailure, false
	}
	// ONE DEFINITION OF LIVENESS, AND THREE ANSWERS (Issue #41). The same call the visibility
	// command makes and the same call `omw daemon status` renders, so this surface cannot report a
	// running daemon as absent — and an undetermined liveness is not written down as a stopped one.
	if live, why := daemonLiveness(env); live != tri.Yes {
		return nil, nil, reportDaemonNotLive(env, "omw note "+what, live, why), false
	}
	src, arch, err := noteSource(env)
	if err != nil || src == nil {
		if err == nil {
			err = hub.ErrHubUnreachable
		}
		// CRITERION 12: an unreachable hub is its own report. Not an empty timeline and not a
		// refusal — and its exit code is the undetermined one, which no refusal uses.
		fmt.Fprintf(env.Stdout, "%s\n", hub.UndeterminedTimelineLine)
		fmt.Fprintf(env.Stderr, "omw note %s: %v (code: %s)\n", what, err, hub.Code(err))
		return nil, nil, cli.ExitUndetermined, false
	}
	return src, arch, cli.Success, true
}

// reportHubError renders a refusal. The two codes it branches on are the two a version surface can
// produce for a whole note, and criterion 14 has already collapsed "you may not read it" into
// "there is no such note" inside package hub, so there is nothing to leak here.
func reportHubError(env cli.Env, what string, err error) int {
	fmt.Fprintf(env.Stderr, "omw note %s: %v (code: %s)\n", what, err, hub.Code(err))
	return cli.ExitFailure
}

func noteVersions(env cli.Env, args []string) int {
	f, ok := parseNoteFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw note versions: name exactly one note.\n")
		return cli.ExitUsage
	}
	src, arch, code, ok := reachHub(env, "versions")
	if !ok {
		return code
	}
	id := hub.NoteID(f.rest[0])
	if f.json {
		s, err := hub.TimelineJSON(src, arch, id, f.as)
		if err != nil {
			return reportHubError(env, "versions", err)
		}
		fmt.Fprint(env.Stdout, s)
		return timelineExit(src, arch, id, f.as)
	}
	t, err := hub.ListTimeline(src, arch, id, f.as)
	if err != nil {
		return reportHubError(env, "versions", err)
	}
	fmt.Fprint(env.Stdout, t.Render())
	if !t.Determined {
		return cli.ExitUndetermined
	}
	return cli.Success
}

// timelineExit re-derives the exit code for the JSON form from the same view the text form used, so
// the two surfaces cannot end up agreeing on the answer and disagreeing on the exit status —
// criterion 13 covers the status too, since that is the part a script reads.
func timelineExit(src hub.VersionSource, arch *hub.Archive, id hub.NoteID, as hub.PersonID) int {
	t, err := hub.ListTimeline(src, arch, id, as)
	if err != nil || !t.Determined {
		return cli.ExitUndetermined
	}
	return cli.Success
}

func noteRead(env cli.Env, args []string) int {
	f, ok := parseNoteFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw note read: name exactly one version, for example note-1@v2.\n")
		return cli.ExitUsage
	}
	ref, err := hub.ParseVersionRef(f.rest[0])
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw note read: %v (code: %s)\n", err, hub.Code(err))
		return cli.ExitUsage
	}
	src, arch, code, ok := reachHub(env, "read")
	if !ok {
		return code
	}
	return renderVersion(env, "read", src, arch, ref, f)
}

func noteShow(env cli.Env, args []string) int {
	f, ok := parseNoteFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw note show: name exactly one note.\n")
		return cli.ExitUsage
	}
	src, arch, code, ok := reachHub(env, "show")
	if !ok {
		return code
	}
	id := hub.NoteID(f.rest[0])
	// CRITERION 6: no version was named, so the CURRENT one is served — and if which one is
	// current could not be established, an undetermined answer is served rather than an older
	// version wearing the current label. That decision is inside hub.CurrentView, not here, so the
	// control API cannot make the opposite one.
	v, err := hub.CurrentView(src, arch, id, f.as)
	if err != nil {
		return reportHubError(env, "show", err)
	}
	if f.json {
		s, jerr := hub.CurrentJSON(src, arch, id, f.as)
		if jerr != nil {
			return reportHubError(env, "show", jerr)
		}
		fmt.Fprint(env.Stdout, s)
	} else {
		fmt.Fprint(env.Stdout, v.Render())
	}
	if !v.Determined() {
		return cli.ExitUndetermined
	}
	return cli.Success
}

func renderVersion(env cli.Env, what string, src hub.VersionSource, arch *hub.Archive, ref hub.VersionRef, f noteFlags) int {
	v, err := hub.ReadView(src, arch, ref, f.as)
	if err != nil {
		return reportHubError(env, what, err)
	}
	if f.json {
		s, jerr := hub.VersionJSON(src, arch, ref, f.as)
		if jerr != nil {
			return reportHubError(env, what, jerr)
		}
		fmt.Fprint(env.Stdout, s)
	} else {
		fmt.Fprint(env.Stdout, v.Render())
	}
	if !v.Determined() {
		return cli.ExitUndetermined
	}
	return cli.Success
}

func noteSearch(env cli.Env, args []string) int {
	f, ok := parseNoteFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) == 0 {
		fmt.Fprintf(env.Stderr, "omw note search: say what to look for.\n")
		return cli.ExitUsage
	}
	src, _, code, ok := reachHub(env, "search")
	if !ok {
		return code
	}
	store, isStore := src.(*hub.Store)
	if !isStore {
		// Search needs the corpus, not one note. A source that is not the store cannot answer, and
		// saying so is better than searching nothing and reporting no results.
		fmt.Fprintf(env.Stderr, "omw note search: %v (code: %s)\n", hub.ErrUndetermined, hub.ErrUndetermined.Code)
		fmt.Fprintf(env.Stderr, "  this build cannot search from here; nothing has been established.\n")
		return cli.ExitUndetermined
	}
	hits, undetermined := hub.SearchLatest(store, f.as, strings.Join(f.rest, " "))
	// SEARCH FINDS THE LATEST AND SAYS SO (criterion 4). Each result names its version with the
	// same reference the timeline carries, and the same standing sentence a direct read prints.
	fmt.Fprintf(env.Stdout, "results: %d\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(env.Stdout, "  %s  %s\n", h.Ref, h.Title)
		fmt.Fprintf(env.Stdout, "  %s\n", hub.StandingLine(h.Standing))
	}
	if len(undetermined) > 0 {
		// NOT DROPPED. "No results" must not absorb "I could not check these".
		fmt.Fprintf(env.Stdout, "could not be determined for %d note(s): they are neither shown as results nor ruled out\n", len(undetermined))
		fmt.Fprintf(env.Stderr, "omw note search: %v (code: %s)\n", hub.ErrUndetermined, hub.ErrUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}

func noteSchema(env cli.Env) int {
	s, err := hub.VersionAPISchemaJSON()
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw note schema: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprint(env.Stdout, s)
	return cli.Success
}

// noteDraft is the local half (criterion 11, PRD §4.4). It never reads OMW_HUB, never probes the
// daemon and never dials anything — a person with no hub gets a complete, working, addressable
// draft timeline.
func noteDraft(env cli.Env, args []string) int {
	if len(args) == 0 {
		noteDraftUsage(env.Stdout)
		return cli.ExitUsage
	}
	sub := args[0]
	f, ok := parseNoteFlags(args[1:], env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if sub == "-h" || sub == "--help" || sub == "help" {
		noteDraftUsage(env.Stdout)
		return cli.Success
	}
	if strings.TrimSpace(f.dir) == "" {
		fmt.Fprintf(env.Stderr, "omw note draft: say which outbox with --dir.\n")
		fmt.Fprintf(env.Stderr, "  omw does not conjure a store; 'omw note draft create --dir <path>' makes one on purpose.\n")
		return cli.ExitUsage
	}

	if sub == "create" {
		o, err := drafts.Create(f.dir)
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw note draft create: %v\n", err)
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stdout, "outbox: %s\n", o.Dir())
		fmt.Fprintf(env.Stdout, "local draft revisions work here with no hub configured.\n")
		return cli.Success
	}

	o, err := drafts.Open(f.dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw note draft %s: %v (code: %s)\n", sub, err, hub.Code(err))
		return cli.ExitFailure
	}

	switch sub {
	case "revise":
		if len(f.rest) < 2 {
			fmt.Fprintf(env.Stderr, "omw note draft revise: name a draft and give its new text.\n")
			return cli.ExitUsage
		}
		ref, err := o.Revise(hub.NoteID(f.rest[0]), strings.Join(f.rest[1:], " "))
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw note draft revise: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stdout, "revision: %s\n", ref)
		fmt.Fprintf(env.Stdout, "the earlier revisions are kept and remain addressable.\n")
		return cli.Success
	case "versions":
		if len(f.rest) != 1 {
			fmt.Fprintf(env.Stderr, "omw note draft versions: name exactly one draft.\n")
			return cli.ExitUsage
		}
		t, err := hub.ListTimeline(o, nil, hub.NoteID(f.rest[0]), "")
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw note draft versions: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitFailure
		}
		fmt.Fprint(env.Stdout, t.Render())
		if !t.Determined {
			return cli.ExitUndetermined
		}
		return cli.Success
	case "read":
		if len(f.rest) != 1 {
			fmt.Fprintf(env.Stderr, "omw note draft read: name exactly one revision, for example draft-a@v2.\n")
			return cli.ExitUsage
		}
		ref, err := hub.ParseVersionRef(f.rest[0])
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw note draft read: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitUsage
		}
		v, err := hub.ReadView(o, nil, ref, "")
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw note draft read: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitFailure
		}
		fmt.Fprint(env.Stdout, v.Render())
		if !v.Determined() {
			return cli.ExitUndetermined
		}
		return cli.Success
	case "list":
		ids, err := o.Drafts()
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw note draft list: %v\n", err)
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stdout, "drafts: %d\n", len(ids))
		for _, id := range ids {
			fmt.Fprintf(env.Stdout, "  %s\n", string(id))
		}
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw note draft: unknown subcommand %q\n", sub)
		return cli.ExitUsage
	}
}

func noteDraftUsage(w io.Writer) {
	fmt.Fprint(w, `omw note draft — local draft revisions, with no hub configured

usage: omw note draft <subcommand> --dir <outbox>

  create                 make an outbox at --dir, on purpose
  revise <id> <text>     add a revision; earlier ones are kept
  versions <id>          the draft's revisions, oldest first
  read <ref>             one revision as it stood, for example draft-a@v2
  list                   the drafts in this outbox

This half needs no hub and opens no connection. The published timeline does need the hub, and
says precisely that when there is none rather than showing you an empty list.
`)
}
