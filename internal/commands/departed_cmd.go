// Command `omw departed` — Issue #22, finding a departed colleague's notes, still attributed to
// them.
//
// "Priya left in March. She was the only person who ever understood why the billing reconciliation
// job runs twice. I know she wrote it up — I read it once. Now I search for it and I need to
// actually find it, and I need to know it was her." That sentence is the whole command.
//
// THIS FILE IS THE ONLY FILE THIS ISSUE ADDS TO PACKAGE COMMANDS, and it references nothing in the
// other command files except the two things the project has ruled must not be duplicated:
// [daemonLiveness] and [reportDaemonNotLive], which are Issue #41's one answer to "is the daemon
// running". No probe is written here and no control socket path is named here.
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
		Name:    "departed",
		Summary: "find a departed colleague's notes, still attributed to them",
		Run:     runDeparted,
	})
}

// departedSource is how this command reaches a hub.
//
// THERE IS NO TRANSPORT IN THIS BUILD, stated rather than stubbed silently, exactly as
// `omw visibility` states it. The default reports the hub unreachable, which renders as UNDETERMINED
// and exits cli.ExitUndetermined — the honest answer for a configured hub this build cannot talk to,
// and the same path a real unreachable hub will take once a transport exists.
//
// CRITERION 20 IS DRIVEN THROUGH THIS VARIABLE. With no hub configured this function is never
// called, and the test replaces it with one that fails the test if it is — a live assertion that the
// no-hub path reaches for nothing, which an import ban cannot make and a socket count would only
// make for the transports somebody thought of.
var departedSource = func(env cli.Env) (*hub.Store, error) {
	return nil, hub.ErrHubUnreachable
}

// departedPeople is the hub's record of who has left, alongside the store.
//
// It is a separate variable from [departedSource] only because this build has no transport to carry
// either; when one exists both come from the same connection. What must not happen is two records —
// the attribution a reader sees comes from the store's own [hub.Store.PeopleStatusOf] whenever the
// store has one, and this is only the fallback for a store that does not.
var departedPeople = func(env cli.Env, s *hub.Store) hub.PeopleStatus {
	if s == nil {
		return nil
	}
	return s.PeopleStatusOf()
}

func runDeparted(env cli.Env) int {
	if len(env.Args) == 0 {
		departedUsage(env.Stdout)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "notes":
		return departedNotes(env, env.Args[1:])
	case "show":
		return departedShow(env, env.Args[1:])
	case "versions":
		return departedVersions(env, env.Args[1:])
	case "refs":
		return departedRefs(env, env.Args[1:])
	case "corpus":
		return departedCorpus(env, env.Args[1:])
	case "-h", "--help", "help":
		departedUsage(env.Stdout)
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw departed: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw departed help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func departedUsage(w io.Writer) {
	fmt.Fprint(w, `omw departed — a colleague's notes outlive their employment

usage: omw departed <subcommand>

  notes --by <person> [--as <reader>]   what they published that you can read
  show <note> [--as <reader>]           one note, with who wrote it and whether they are still here
  versions <note> [--as <reader>]       the note's whole timeline, attributed the same way throughout
  refs <note> [--as <reader>]           what it refers to, and what refers to it
  corpus [--as <reader>]                how much you can read, and how much of it is archived

A deactivated person's notes are archived, not deleted. They stay findable by exactly the
colleagues who could see them before, and they stay theirs.
`)
}

// departedArgs is the flag shape every subcommand here shares.
type departedArgs struct {
	subject string       // the note or person named positionally, if any
	by      hub.PersonID // --by
	as      hub.PersonID // --as
}

func parseDepartedArgs(env cli.Env, what string, args []string) (departedArgs, int, bool) {
	var out departedArgs
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--as" && i+1 < len(args):
			i++
			out.as = hub.PersonID(args[i])
		case strings.HasPrefix(args[i], "--as="):
			out.as = hub.PersonID(strings.TrimPrefix(args[i], "--as="))
		case args[i] == "--by" && i+1 < len(args):
			i++
			out.by = hub.PersonID(args[i])
		case strings.HasPrefix(args[i], "--by="):
			out.by = hub.PersonID(strings.TrimPrefix(args[i], "--by="))
		case out.subject == "" && !strings.HasPrefix(args[i], "-"):
			out.subject = args[i]
		default:
			fmt.Fprintf(env.Stderr, "%s: unexpected argument %q\n", what, args[i])
			return out, cli.ExitUsage, false
		}
	}
	return out, cli.Success, true
}

// reach is the precondition every hub-dependent subcommand runs, IN THIS ORDER, and it is written
// once so that five subcommands cannot answer the same question five ways.
//
//  1. NO HUB CONFIGURED is a determined fact about this machine (criterion 21). It is said
//     precisely — that archived-note lookup is a hub capability and there is no hub — it exits
//     ExitFailure, which is neither Success nor the ExitUndetermined an unreachable hub gets, and
//     it reaches for NOTHING (criterion 20): departedSource is not called on this path at all.
//  2. THE DAEMON IS NOT STARTED (criterion 19). Liveness has one definition in this package and
//     three answers; a daemon that could not be checked is not reported as a stopped one.
//  3. AN UNREACHABLE HUB IS UNDETERMINED (§4.3), and exits ExitUndetermined — distinguishable from
//     both of the above and from a genuine zero-results answer.
//
// It returns (store, people, exitCode, ok). When ok is false the caller returns exitCode untouched.
func reach(env cli.Env, what string) (*hub.Store, hub.PeopleStatus, int, bool) {
	if strings.TrimSpace(env.Getenv(envHub)) == "" {
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		// PRECISE ABOUT WHAT IS MISSING, and it claims nothing about how many notes exist. A
		// command that has not asked the hub knows nothing about anybody's notes, and criterion 21
		// is that this must never read as "no notes by that person".
		fmt.Fprintf(env.Stderr, "  looking up an archived note is a hub capability: the notes and the record of who has left both live on the hub, and there is no hub configured to ask.\n")
		fmt.Fprintf(env.Stderr, "  this is not a report that there are no such notes. nothing has been established, and nothing was looked at.\n")
		fmt.Fprintf(env.Stderr, "  set %s and ask again.\n", envHub)
		return nil, nil, cli.ExitFailure, false
	}
	if live, why := daemonLiveness(env); live != tri.Yes {
		return nil, nil, reportDaemonNotLive(env, what, live, why), false
	}
	s, err := departedSource(env)
	if err != nil || s == nil {
		if err == nil {
			err = hub.ErrHubUnreachable
		}
		fmt.Fprintf(env.Stdout, "%s\n", hub.UndeterminedDescription)
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, err, hub.Code(err))
		fmt.Fprintf(env.Stderr, "  a hub is configured and it did not answer. this is not a report that there are no such notes.\n")
		return nil, nil, cli.ExitUndetermined, false
	}
	return s, departedPeople(env, s), cli.Success, true
}

// departedNotes is criteria 4, 6, 9 and 21: what a person published that this reader may read.
func departedNotes(env cli.Env, args []string) int {
	const what = "omw departed notes"
	a, code, ok := parseDepartedArgs(env, what, args)
	if !ok {
		return code
	}
	if a.by == "" && a.subject != "" {
		a.by = hub.PersonID(a.subject)
	}
	if a.by == "" {
		fmt.Fprintf(env.Stderr, "%s: name whose notes with --by.\n", what)
		return cli.ExitUsage
	}
	s, ps, code, ok := reach(env, what)
	if !ok {
		return code
	}

	l := hub.NotesBy(s, ps, a.by, a.as)
	// THE PERSON'S OWN STATE IS STATED FIRST, AND IT IS STATED EVEN WHEN THEY HAVE NO NOTES. A
	// listing of zero notes by an active person and a listing of zero notes by a departed one are
	// different answers, and neither is the no-hub answer above.
	fmt.Fprintf(env.Stdout, "%s\n", hub.Attribution{Author: a.by, Active: l.AuthorState}.Line())
	if l.AuthorState == tri.No {
		fmt.Fprintf(env.Stdout, "%s\n", hub.RetentionLine)
	}
	fmt.Fprintf(env.Stdout, "notes you can read: %d\n", len(l.Notes))
	for _, v := range l.Notes {
		fmt.Fprintf(env.Stdout, "\n%s", v.Render())
	}
	if len(l.Notes) == 0 {
		// A GENUINE ZERO, SAID AS ONE (criterion 21). It is a determined answer — the hub was asked
		// — and its wording shares nothing with the no-hub wording above.
		fmt.Fprintf(env.Stdout, "the hub was asked and it holds no notes by %s that you can read.\n", string(a.by))
	}
	return reportUndetermined(env, what, l.Undetermined)
}

// reportUndetermined states the notes whose readability could not be worked out, and chooses the
// exit code.
//
// RETURNED, NOT DROPPED, AND NOT COUNTED IN THE ANSWER. A reader told "3 notes" when the truth is
// "3 notes and two I could not check" has been handed a complete-looking answer that is not one, and
// the exit code is the part a script reads.
func reportUndetermined(env cli.Env, what string, ids []hub.NoteID) int {
	if len(ids) == 0 {
		return cli.Success
	}
	fmt.Fprintf(env.Stderr, "%s: whether you may read %d further note(s) %s (code: %s)\n",
		what, len(ids), tri.Undetermined, hub.ErrUndetermined.Code)
	for _, id := range ids {
		fmt.Fprintf(env.Stderr, "  %s\n", string(id))
	}
	fmt.Fprintf(env.Stderr, "  they are not included above and they are not a determined absence.\n")
	return cli.ExitUndetermined
}

// departedShow is criteria 1, 9, 10, 11, 12 and 18: one note, with an author, always.
func departedShow(env cli.Env, args []string) int {
	const what = "omw departed show"
	a, code, ok := parseDepartedArgs(env, what, args)
	if !ok {
		return code
	}
	if a.subject == "" {
		fmt.Fprintf(env.Stderr, "%s: name a note.\n", what)
		return cli.ExitUsage
	}
	s, ps, code, ok := reach(env, what)
	if !ok {
		return code
	}

	n, by, err := hub.AttributedRead(s, ps, hub.NoteID(a.subject), a.as)
	if err != nil {
		return reportNoteError(env, what, err)
	}
	fmt.Fprintf(env.Stdout, "note: %s\n", string(n.ID))
	if n.Title != "" {
		fmt.Fprintf(env.Stdout, "title: %s\n", n.Title)
	}
	fmt.Fprint(env.Stdout, by.Render())
	latest := n.Latest()
	fmt.Fprintf(env.Stdout, "current: %s\n", hub.VersionRef{Note: n.ID, Number: latest.Number})
	fmt.Fprintf(env.Stdout, "versions: %d\n", len(n.Versions))
	fmt.Fprintf(env.Stdout, "body:\n%s\n", latest.Body)
	// The exit code follows the AUTHOR STATE, not the read. A note read successfully whose author's
	// state could not be worked out has not been fully answered, and §4.3 says so in the exit code
	// as well as in the prose — criterion 18's "distinct from all three of the other cases" reaches
	// what a script sees, not only what a person reads.
	if by.Active == tri.Undetermined {
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, hub.ErrPersonStateUndetermined, hub.ErrPersonStateUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}

// reportNoteError renders a read failure. CRITERION 11: a refusal for visibility and a note that is
// not there are different facts with different codes, and neither of them is what an archived note
// produces — an archived note produces a note.
func reportNoteError(env cli.Env, what string, err error) int {
	switch hub.Code(err) {
	case hub.ErrNoSuchNote.Code, hub.ErrRefused.Code:
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, err, hub.Code(err))
		return cli.ExitFailure
	case hub.ErrUndetermined.Code, hub.ErrHubUnreachable.Code, hub.ErrPersonStateUndetermined.Code:
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, err, hub.Code(err))
		return cli.ExitUndetermined
	default:
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, err, hub.Code(err))
		return cli.ExitFailure
	}
}

// departedVersions is criteria 2 and 13: every version still addressable, all attributed the same.
func departedVersions(env cli.Env, args []string) int {
	const what = "omw departed versions"
	a, code, ok := parseDepartedArgs(env, what, args)
	if !ok {
		return code
	}
	if a.subject == "" {
		fmt.Fprintf(env.Stderr, "%s: name a note.\n", what)
		return cli.ExitUsage
	}
	s, ps, code, ok := reach(env, what)
	if !ok {
		return code
	}

	n, by, err := hub.AttributedRead(s, ps, hub.NoteID(a.subject), a.as)
	if err != nil {
		return reportNoteError(env, what, err)
	}
	fmt.Fprintf(env.Stdout, "note: %s\n", string(n.ID))
	fmt.Fprintf(env.Stdout, "versions: %d\n", len(n.Versions))
	for _, v := range n.Versions {
		ref := hub.VersionRef{Note: n.ID, Number: v.Number}
		_, vby, verr := hub.AttributedVersion(s, ps, ref, a.as)
		if verr != nil {
			return reportNoteError(env, what, verr)
		}
		fmt.Fprintf(env.Stdout, "\nversion: %s\n", ref)
		// THE ATTRIBUTION IS PRINTED PER VERSION, and it is read from the note each time. That is
		// criterion 13 driven rather than asserted: if attribution ever came from the version, this
		// loop is where the latest and the older ones would start disagreeing.
		fmt.Fprint(env.Stdout, vby.Render())
	}
	if by.Active == tri.Undetermined {
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, hub.ErrPersonStateUndetermined, hub.ErrPersonStateUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}

// departedRefsIndex is the reference index this build reads. Issue #14 owns references; until it
// lands there is nowhere for a real one to come from, so the default is empty and a test supplies
// one. An empty index yields an empty answer that SAYS it is empty, never silence.
var departedRefsIndex = func(env cli.Env, s *hub.Store) *hub.RefIndex { return hub.NewRefIndex() }

// departedRefs is criterion 3: references survive a departure in both directions.
func departedRefs(env cli.Env, args []string) int {
	const what = "omw departed refs"
	a, code, ok := parseDepartedArgs(env, what, args)
	if !ok {
		return code
	}
	if a.subject == "" {
		fmt.Fprintf(env.Stderr, "%s: name a note.\n", what)
		return cli.ExitUsage
	}
	s, ps, code, ok := reach(env, what)
	if !ok {
		return code
	}
	x := departedRefsIndex(env, s)
	id := hub.NoteID(a.subject)

	out := x.Resolve(s, ps, id, a.as)
	back := x.Backlinks(s, ps, id, a.as)
	fmt.Fprintf(env.Stdout, "note: %s\n", string(id))
	renderResolution(env, "refers to", out)
	renderResolution(env, "referred to by", back)
	if len(out.Undetermined)+len(back.Undetermined) > 0 {
		fmt.Fprintf(env.Stderr, "%s: %d reference(s) %s (code: %s)\n", what,
			len(out.Undetermined)+len(back.Undetermined), tri.Undetermined, hub.ErrUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}

func renderResolution(env cli.Env, heading string, r hub.Resolution) {
	fmt.Fprintf(env.Stdout, "%s: %d\n", heading, len(r.Resolved))
	for _, v := range r.Resolved {
		fmt.Fprintf(env.Stdout, "  %s  %s\n", string(v.Note), v.By.Line())
	}
	// A reference the reader may not follow is NAMED as refused, not dropped. "This refers to
	// something you cannot see" and "this refers to nothing" are different facts.
	if len(r.Refused) > 0 {
		fmt.Fprintf(env.Stdout, "  %d further reference(s) are not visible to you\n", len(r.Refused))
	}
	if len(r.Undetermined) > 0 {
		fmt.Fprintf(env.Stdout, "  %d reference(s) could not be determined\n", len(r.Undetermined))
	}
}

// departedCorpus is criterion 8: statistics an agent may ground itself on.
func departedCorpus(env cli.Env, args []string) int {
	const what = "omw departed corpus"
	a, code, ok := parseDepartedArgs(env, what, args)
	if !ok {
		return code
	}
	s, ps, code, ok := reach(env, what)
	if !ok {
		return code
	}
	c := hub.Summarise(s, ps, a.as)
	fmt.Fprint(env.Stdout, c.Render())
	return reportUndetermined(env, what, c.Undetermined)
}
