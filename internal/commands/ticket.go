// The `omw ticket` command: fold the scattered traffic about one problem into a single ticket, take
// it apart again, and read the merge's working (PRD §2.3, §3.2, §3.14, §4.2, §4.3, §4.6, §5.4;
// Issue #7).
//
// WHAT THIS FILE IS CAREFUL ABOUT, IN ORDER OF HOW EASY IT WOULD BE TO GET WRONG:
//
//  1. THE WORKING IS THE POINT, NOT A FOOTNOTE (criterion 4). For every input the merge shows what
//     it was, which channel and source it came from, and why it was folded in. Every one of those
//     goes through [inbox.Field.Render], so an origin that could not be resolved says so instead of
//     printing a blank — criterion 12 — and a merged ticket whose record does not carry all three
//     for an input is reported as a failure rather than shown with a gap in it.
//  2. A MERGE THAT DID NOT HAPPEN NEVER LOOKS LIKE ONE THAT DID (criteria 8 and 9). Naming one
//     ticket, naming a ticket that is not there, and unmerging something that was never merged each
//     leave the inbox untouched and exit non-zero — with `could not determine` and `determined to be
//     nothing` on DIFFERENT codes, which is the project's standing rule.
//  3. NOTHING HERE REACHES A HUB, and there is no configuration under which it would (§2.3,
//     criterion 13). Tickets are never published, so a merge has nothing to say to a hub. There is
//     no hub client imported, no address read, no connection opened.
//  4. NOTHING HERE STARTS THE DAEMON (§4.2, criterion 13), AND IT DOES NOT GUESS WHETHER ONE IS
//     RUNNING. The answer comes from [daemonLiveness] — the one definition in liveness.go, wrapping
//     daemon.Inspect — and it is THREE-VALUED. This surface originally stat'd a socket path of its
//     own, which is Issue #41's defect: it printed a confident "not running" over a live daemon.
//     No path is derived or named here; internal/daemon owns it and falls back to a per-user
//     runtime directory above the sun_path limit, so any second copy of that rule is wrong rather
//     than merely duplicated.
//
// (Detached from the package clause on purpose: doc.go carries this package's doc comment, and
// several Issues add files here concurrently.)

package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "ticket",
		Summary: "merge the traffic about one problem into a single ticket, and take it apart again",
		Run:     runTicket,
	})
}

const ticketUsage = `usage: omw ticket <merge|unmerge|show|list> [options]

  merge    fold two or more tickets into one ticket with a written title and summary
  unmerge  take a merged ticket apart, restoring every source exactly as it was
  show     read one ticket, the working of the merge that produced it, and whether it
           was ever merged and unmerged
  list     list the inbox, marking which tickets are merges and which have been merged
           and unmerged

options for 'merge':
  --id ID            the identifier the merged ticket will have (required)
  --title TEXT       the written one-line statement of the one problem (required)
  --summary TEXT     the written statement of it in full (required). It may not be the
                     source titles run together — that is the list of fragments the
                     merge exists to replace.
  --channel TEXT     the merged ticket's own channel. Omitted, it is recorded as
                     undetermined, which is the honest answer for a merge that crosses
                     channels.
  --why TICKET=TEXT  why that ticket was folded in. Omitted for a ticket, its reason is
                     recorded as undetermined and prints as such — never as blank.
  --source TICKET=ID the identifier that piece had in its channel. Omitted, undetermined.

Then name two or more tickets. Merging crosses channels by design: an email-originated
ticket and a chat-originated one merge on exactly the same terms as two of a kind.

Tickets live on this machine and are never published (PRD §2.3). No merge operation
contacts a hub, and nothing here starts the daemon.
`

func runTicket(env cli.Env) int {
	if len(env.Args) == 0 {
		fmt.Fprint(env.Stderr, ticketUsage)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(env.Stdout, ticketUsage)
		return cli.Success
	case "merge":
		return ticketMerge(env, env.Args[1:])
	case "unmerge":
		return ticketUnmerge(env, env.Args[1:])
	case "show":
		return ticketShow(env, env.Args[1:])
	case "list":
		return ticketList(env, env.Args[1:])
	default:
		fmt.Fprintf(env.Stderr, "omw ticket: there is no %q operation on a ticket.\n", env.Args[0])
		fmt.Fprint(env.Stderr, ticketUsage)
		return cli.ExitUsage
	}
}

// openTickets resolves and opens this device's store, and then decides whether a merge may proceed
// at all. It turns the store's distinct failures into distinct sentences and distinct exit codes.
//
// THE NO-STORE CASE IS A FAILURE AND NOT AN EMPTY INBOX, for the reason inbox.go gives at length.
func openTickets(env cli.Env, op string) (*store.Store, string, int) {
	root, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v\n", op, err)
		fmt.Fprintf(env.Stderr, "  This is not 'there is nothing to merge'. Where the inbox lives could not\n")
		fmt.Fprintf(env.Stderr, "  be determined, so nothing was read and nothing was changed.\n")
		return nil, "", cli.ExitUndetermined
	}
	s, err := store.Open(root)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(env.Stderr, "omw ticket %s: there is no store at %s, so the inbox could not be read at all.\n", op, root)
		fmt.Fprintf(env.Stderr, "  This is NOT 'there are no tickets to merge' — nothing has been read.\n")
		fmt.Fprintf(env.Stderr, "  Run 'omw store create' first.\n")
		return nil, root, cli.ExitFailure
	case err != nil:
		fmt.Fprintf(env.Stderr, "omw ticket %s: the store at %s could not be opened: %v\n", op, root, err)
		fmt.Fprintf(env.Stderr, "  This is NOT 'there are no tickets to merge' — whether you have any could\n")
		fmt.Fprintf(env.Stderr, "  not be determined, and nothing was changed.\n")
		return nil, root, cli.ExitUndetermined
	}
	return s, root, cli.Success
}

// ticketControlAPI answers whether the control API is open for the store at root, in three values,
// with the reason when it is not.
//
// IT ASKS daemon.Inspect, WHICH IS THE SAME CALL `omw daemon status` MAKES. It does not derive,
// name or stat a socket path: that path is chosen by internal/daemon's socketFor, which falls back
// to a per-user runtime directory whenever the in-store path would exceed the kernel's sun_path
// limit, and a caller reproducing that rule is one release away from reproducing an older version
// of it (Issue #41). There is no `daemonLiveness` equivalent for the control question, so the
// report is read directly — the same source, not a second opinion.
//
// It is a var so a test can drive this surface's RENDERING of a control API that would not open,
// which is a state no test can stage without reaching for the path this rule forbids. The tests
// that use it say so; the daemon's own liveness is never stubbed.
var ticketControlAPI = func(root string) (tri.Value, string) {
	rep := daemon.Inspect(root)
	why := rep.ControlDetail
	if why == "" {
		switch rep.Control {
		case tri.Yes:
			why = "the control API is open"
		case tri.No:
			why = "the control API is not open"
		default:
			why = "whether the control API is open could not be determined"
		}
	}
	return rep.Control, why
}

// controlGate is §4.6 / §5.1 / criterion 15, and the reading it takes is worth stating because the
// criteria pull three ways and only one arrangement satisfies all of them.
//
//   - NO DAEMON RUNNING. Criterion 13 requires that be SAID and not fixed: this command does not
//     start one. Merging is local work against the local store, and criterion 14 requires that
//     local half work fully with no hub and no daemon — so it proceeds.
//   - A DAEMON IS RUNNING AND ITS CONTROL API IS NOT OPEN. §4.6 read literally. Criterion 15
//     requires merge and unmerge to SAY SO "rather than proceeding through some other path", so
//     this refuses, and the refusal names an unavailable control API in wording that cannot be read
//     as "there is nothing to merge".
//   - WHETHER A DAEMON IS RUNNING COULD NOT BE ESTABLISHED. THE THIRD ANSWER, and it is not a "no"
//     (PRD §4.3, Issue #41). Proceeding here would be treating "I could not tell" as "there is no
//     daemon", which is the confident false negative #41 removed from four surfaces at once. It is
//     reported through reportDaemonNotLive — the shared rendering, on ExitUndetermined, whose
//     sentence never contains the negative's — and nothing is changed.
//   - THE CONTROL API'S STATE COULD NOT BE DETERMINED, with a daemon running. Undetermined again,
//     its own exit code, nothing changed.
//
// The distinction that makes this coherent is between an ABSENT control API and an UNCONFIRMED one.
// Refusing on the first would make merging impossible on every machine with no daemon, which
// criterion 14 forbids; proceeding on the second would be the "some other path" criterion 15 names.
func controlGate(env cli.Env, op, root string) int {
	live, why := daemonLiveness(env)
	switch live {
	case tri.No:
		// Determined: nothing holds this store. The local work is this command's to do.
		return cli.Success
	case tri.Undetermined:
		return reportDaemonNotLive(env, "omw ticket "+op, live, why)
	}

	control, controlWhy := ticketControlAPI(root)
	switch control {
	case tri.Yes:
		return cli.Success
	case tri.No:
		fmt.Fprintf(env.Stderr, "omw ticket %s: the control API is not open, so this has not been done.\n", op)
		fmt.Fprintf(env.Stderr, "  %s\n", controlWhy)
		fmt.Fprintf(env.Stderr, "  This is NOT 'there are no tickets to merge' and NOT 'the merge failed' —\n")
		fmt.Fprintf(env.Stderr, "  the control API is unavailable and nothing has been changed. This command\n")
		fmt.Fprintf(env.Stderr, "  has not gone round it by another path.\n")
		return cli.ExitFailure
	default:
		fmt.Fprintf(env.Stderr, "omw ticket %s: whether the control API is open %s.\n", op, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %s\n", controlWhy)
		fmt.Fprintf(env.Stderr, "  This is not a report that it is closed. Nothing has been changed.\n")
		return cli.ExitUndetermined
	}
}

// ticketHeader states what the person is looking at BEFORE anything else, because three of the
// criteria are about not being misled by output that looks live or that looks like it consulted a
// hub. It is the same shape as the inbox command's header and says the same things, deliberately:
// two commands over the same store that describe it differently are two answers to one question.
func ticketHeader(env cli.Env, root string) {
	fmt.Fprintf(env.Stdout, "inbox:       %s\n", root)

	// §4.2 / criterion 13, IN THREE VALUES AND FROM THE ONE DEFINITION. Never by starting anything,
	// and never by this file forming its own opinion — see the note on ticketControlAPI.
	live, why := daemonLiveness(env)
	fmt.Fprintf(env.Stdout, "daemon:      %s\n", live.Render("running", "not running"))
	if why != "" {
		fmt.Fprintf(env.Stdout, "             %s\n", why)
	}
	switch live {
	case tri.No:
		// SAID ONLY WHERE IT IS TRUE. This sentence used to be printed unconditionally, off a probe
		// that always answered "not running" — so it appeared over a running daemon, which is
		// Issue #41 criterion 5. It is now gated on the established negative.
		fmt.Fprintf(env.Stdout, "             this is the store on disk, and this command has not started anything\n")
	case tri.Undetermined:
		fmt.Fprintf(env.Stdout, "             this is not a report that the daemon is stopped; nothing about it\n")
		fmt.Fprintf(env.Stdout, "             has been established, and this command has not started anything\n")
	}

	// §4.6 / §5.1 / criterion 15. Its own line, distinguishable from an empty inbox and from any
	// hub wording, because "the control API declined to open" is a third thing and not either.
	control, controlWhy := ticketControlAPI(root)
	fmt.Fprintf(env.Stdout, "control api: %s\n", control.Render("open", "not open"))
	fmt.Fprintf(env.Stdout, "             %s\n", controlWhy)

	// §2.3 / criteria 13, 14. Stated, not left to be inferred from an absence of output.
	fmt.Fprintf(env.Stdout, "hub:         not contacted, and there is no operation here that would.\n")
	fmt.Fprintf(env.Stdout, "             A merged ticket is a ticket: it lives on this machine and is never\n")
	fmt.Fprintf(env.Stdout, "             published, so no merge has anything to send.\n")
	fmt.Fprintln(env.Stdout)
}

// ---------------------------------------------------------------------------
// merge
// ---------------------------------------------------------------------------

type mergeArgs struct {
	id      string
	title   string
	summary string
	channel string
	// hasChannel distinguishes a channel the person wrote as empty from one they did not give,
	// which are two states of a [inbox.Field] and must not collapse into one here either.
	hasChannel bool
	why        map[string]string
	source     map[string]string
	tickets    []string
}

// parseMergeArgs reads the arguments and REFUSES ANYTHING FLAG-SHAPED THAT IT DOES NOT KNOW, for the
// reason store.go's parser gives: an unknown flag quietly treated as a positional becomes a ticket
// identifier that does not exist, and the person is told their ticket is missing when their typing
// was the problem.
func parseMergeArgs(args []string) (mergeArgs, error) {
	out := mergeArgs{why: map[string]string{}, source: map[string]string{}}
	need := func(i int, flag string) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s needs a value after it", flag)
		}
		return args[i+1], nil
	}
	pair := func(flag, v string) (string, string, error) {
		k, rest, ok := strings.Cut(v, "=")
		if !ok || k == "" {
			return "", "", fmt.Errorf("%s takes TICKET=TEXT; got %q", flag, v)
		}
		return k, rest, nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			out.tickets = append(out.tickets, args[i+1:]...)
			return out, nil
		case a == "--id", a == "--title", a == "--summary", a == "--channel":
			v, err := need(i, a)
			if err != nil {
				return out, err
			}
			i++
			switch a {
			case "--id":
				out.id = v
			case "--title":
				out.title = v
			case "--summary":
				out.summary = v
			case "--channel":
				out.channel, out.hasChannel = v, true
			}
		case a == "--why", a == "--source":
			v, err := need(i, a)
			if err != nil {
				return out, err
			}
			i++
			k, text, err := pair(a, v)
			if err != nil {
				return out, err
			}
			if a == "--why" {
				out.why[k] = text
			} else {
				out.source[k] = text
			}
		case strings.HasPrefix(a, "-") && a != "-":
			return out, fmt.Errorf("unknown option %q; run 'omw ticket --help' for the options this "+
				"build has.\n  It has NOT been treated as a ticket identifier, and nothing was merged", a)
		default:
			out.tickets = append(out.tickets, a)
		}
	}
	return out, nil
}

func ticketMerge(env cli.Env, args []string) int {
	parsed, err := parseMergeArgs(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw ticket merge: %v\n", err)
		return cli.ExitUsage
	}
	switch {
	case parsed.id == "":
		fmt.Fprintf(env.Stderr, "omw ticket merge: --id names the merged ticket; nothing was merged.\n")
		return cli.ExitUsage
	case parsed.title == "":
		fmt.Fprintf(env.Stderr, "omw ticket merge: --title is the written statement of the one problem,\n")
		fmt.Fprintf(env.Stderr, "  and a merge without one leaves the person with the fragments they merged\n")
		fmt.Fprintf(env.Stderr, "  to be rid of. Nothing was merged.\n")
		return cli.ExitUsage
	case parsed.summary == "":
		fmt.Fprintf(env.Stderr, "omw ticket merge: --summary is the written statement in full, and it may\n")
		fmt.Fprintf(env.Stderr, "  not be the source titles run together. Nothing was merged.\n")
		return cli.ExitUsage
	}
	// CRITERION 8, THE FIRST HALF. Naming one ticket is not a merge and must not silently produce
	// one. It exits here, before the store is even opened, so there is nothing to leave behind.
	if len(parsed.tickets) < 2 {
		fmt.Fprintf(env.Stderr, "omw ticket merge: name two or more tickets to merge; %d named.\n", len(parsed.tickets))
		fmt.Fprintf(env.Stderr, "  Merging one thing is not a merge. Nothing was merged and the inbox is\n")
		fmt.Fprintf(env.Stderr, "  exactly as it was.\n")
		return cli.ExitUsage
	}

	s, root, code := openTickets(env, "merge")
	if code != cli.Success {
		return code
	}
	if code := controlGate(env, "merge", root); code != cli.Success {
		return code
	}

	// A --why or --source naming a ticket that is not being merged is a typo with a silent failure
	// mode: the reason lands nowhere and the input it was meant for records "undetermined".
	named := map[string]bool{}
	for _, id := range parsed.tickets {
		named[id] = true
	}
	for _, m := range []struct {
		flag string
		vals map[string]string
	}{{"--why", parsed.why}, {"--source", parsed.source}} {
		for k := range m.vals {
			if !named[k] {
				fmt.Fprintf(env.Stderr, "omw ticket merge: %s names %q, which is not one of the tickets\n", m.flag, k)
				fmt.Fprintf(env.Stderr, "  being merged. Nothing was merged.\n")
				return cli.ExitUsage
			}
		}
	}

	spec := inbox.MergeSpec{
		ID:      parsed.id,
		Title:   inbox.Text(parsed.title),
		Summary: inbox.Text(parsed.summary),
		// UNDETERMINED, NOT EMPTY, when the person did not say. A merge crosses channels by design
		// (§3.2), so "which channel is this ticket from" frequently has no answer — and no answer is
		// not the same fact as the answer being blank.
		Channel: inbox.Undetermined("this ticket was merged from more than one piece of traffic, so it " +
			"has no single channel of its own"),
		When: time.Now().UTC(),
	}
	if parsed.hasChannel {
		spec.Channel = inbox.Text(parsed.channel)
	}
	for _, id := range parsed.tickets {
		in := inbox.InputSpec{
			TicketID: id,
			// CRITERION 12 BY CONSTRUCTION. A reason nobody gave and an identifier nobody recorded
			// are UNDETERMINED — a state with its own rendering — and never the empty string.
			Why: inbox.Undetermined("no reason was recorded for folding this in"),
			Source: inbox.Undetermined("nothing recorded the identifier this piece had in its channel; " +
				"the ticket carries no such field"),
		}
		if v, ok := parsed.why[id]; ok {
			in.Why = inbox.Text(v)
		}
		if v, ok := parsed.source[id]; ok {
			in.Source = inbox.Text(v)
		}
		spec.Inputs = append(spec.Inputs, in)
	}

	merged, err := inbox.Merge(s, spec)
	if err != nil {
		return reportMergeFailure(env, "merge", err)
	}

	ticketHeader(env, root)
	fmt.Fprintf(env.Stdout, "merged %d tickets into one: %s\n\n", len(spec.Inputs), merged.ID)
	renderTicket(env, merged)
	fmt.Fprintln(env.Stdout)
	record, lerr := inbox.LoadMerge(s, merged.ID)
	if lerr != nil {
		// The merge is done; the working could not be read back. Said, and on the undetermined code,
		// because criterion 14 forbids half-working silently — this is the half, and it is named.
		fmt.Fprintf(env.Stderr, "omw ticket merge: the merge was written, and its working could not be\n")
		fmt.Fprintf(env.Stderr, "  read back: %v\n", lerr)
		return cli.ExitUndetermined
	}
	renderWorking(env, record)
	fmt.Fprintf(env.Stdout, "\nThis merge is reversible exactly: 'omw ticket unmerge %s' restores every\n", merged.ID)
	fmt.Fprintf(env.Stdout, "source with the content it had. Nothing here expires (PRD §5.4).\n")
	return cli.Success
}

// ---------------------------------------------------------------------------
// unmerge
// ---------------------------------------------------------------------------

func ticketUnmerge(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw ticket unmerge: name exactly one merged ticket; got %d.\n", len(args))
		fmt.Fprintf(env.Stderr, "  Nothing was changed.\n")
		return cli.ExitUsage
	}
	id := args[0]
	s, root, code := openTickets(env, "unmerge")
	if code != cli.Success {
		return code
	}
	if code := controlGate(env, "unmerge", root); code != cli.Success {
		return code
	}

	restored, err := inbox.Unmerge(s, id, time.Now().UTC())
	if err != nil {
		return reportMergeFailure(env, "unmerge", err)
	}
	ticketHeader(env, root)
	fmt.Fprintf(env.Stdout, "unmerged %s: %d tickets are back in the inbox with the content they had.\n\n", id, len(restored))
	for i, t := range restored {
		if i > 0 {
			fmt.Fprintln(env.Stdout)
		}
		renderTicket(env, t)
	}
	fmt.Fprintf(env.Stdout, "\nEach of these is recorded as having been merged and unmerged; 'omw ticket show'\n")
	fmt.Fprintf(env.Stdout, "on any of them says so, which is how a restored ticket differs from one that\n")
	fmt.Fprintf(env.Stdout, "was never merged.\n")
	return cli.Success
}

// ---------------------------------------------------------------------------
// show, list
// ---------------------------------------------------------------------------

func ticketShow(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw ticket show: name exactly one ticket; got %d.\n", len(args))
		return cli.ExitUsage
	}
	id := args[0]
	s, root, code := openTickets(env, "show")
	if code != cli.Success {
		return code
	}
	t, err := inbox.Get(s, id)
	if err != nil {
		if errors.Is(err, inbox.ErrNoSuchTicket) {
			fmt.Fprintf(env.Stderr, "omw ticket show: there is no ticket %q in the inbox at %s.\n", id, root)
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stdout, "ticket %s: %s\n", id, tri.Undetermined.Render("", ""))
		fmt.Fprintf(env.Stderr, "omw ticket show: %v\n", err)
		return cli.ExitUndetermined
	}
	ticketHeader(env, root)
	renderTicket(env, t)

	// IS THIS A MERGE — in three values, because a merge record that cannot be read is not evidence
	// that this is an ordinary ticket.
	record, merr := inbox.LoadMerge(s, id)
	switch {
	case merr == nil:
		fmt.Fprintln(env.Stdout)
		renderWorking(env, record)
	case errors.Is(merr, inbox.ErrNotMerged):
		fmt.Fprintf(env.Stdout, "  merged:  no — this ticket was not produced by a merge\n")
	default:
		fmt.Fprintf(env.Stdout, "  merged:  %s\n", tri.Undetermined.Render("", ""))
		fmt.Fprintf(env.Stderr, "omw ticket show: %v\n", merr)
		return cli.ExitUndetermined
	}

	// CRITERION 6. A ticket that was merged and then unmerged says so, and a ticket that never was
	// says the other thing. The two never render identically because these are two different lines.
	trace, present, uerr := inbox.LoadUnmerged(s, id)
	switch present {
	case tri.Yes:
		fmt.Fprintf(env.Stdout, "  history: this ticket was merged into %s and the merge was undone.\n", trace.MergedInto)
		fmt.Fprintf(env.Stdout, "           merged %s, undone %s, alongside %d other pieces of traffic.\n",
			trace.MergedRender(), trace.UndoneRender(), trace.Alongside)
		fmt.Fprintf(env.Stdout, "           Its content is exactly what it was before that merge.\n")
	case tri.No:
		fmt.Fprintf(env.Stdout, "  history: this ticket has never been merged and unmerged.\n")
	default:
		fmt.Fprintf(env.Stdout, "  history: whether this ticket was ever merged and unmerged %s\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw ticket show: %v\n", uerr)
		return cli.ExitUndetermined
	}
	return cli.Success
}

func ticketList(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw ticket list: takes no arguments; got %q\n", strings.Join(args, " "))
		return cli.ExitUsage
	}
	s, root, code := openTickets(env, "list")
	if code != cli.Success {
		return code
	}
	ticketHeader(env, root)
	tickets, err := inbox.List(s)
	if err != nil {
		fmt.Fprintf(env.Stdout, "tickets:     %s\n", tri.Undetermined.Render("", ""))
		fmt.Fprintf(env.Stderr, "omw ticket list: %v\n", err)
		fmt.Fprintf(env.Stderr, "  This is NOT an empty inbox.\n")
		return cli.ExitUndetermined
	}
	if len(tickets) == 0 {
		fmt.Fprintf(env.Stdout, "tickets:     none — the inbox is empty and there is nothing to merge.\n")
		fmt.Fprintf(env.Stdout, "             The inbox was read successfully; nothing was hidden.\n")
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "tickets:     %d\n\n", len(tickets))
	for i, t := range tickets {
		if i > 0 {
			fmt.Fprintln(env.Stdout)
		}
		renderTicket(env, t)
		merged, merr := inbox.IsMerged(s, t.ID)
		if merr != nil && merged == tri.Undetermined {
			fmt.Fprintf(env.Stderr, "omw ticket list: %v\n", merr)
		}
		fmt.Fprintf(env.Stdout, "  merged:  %s\n", merged.Render(
			"yes — this ticket was produced by a merge; 'omw ticket show' has its working",
			"no — this ticket was not produced by a merge"))
		_, was, _ := inbox.LoadUnmerged(s, t.ID)
		fmt.Fprintf(env.Stdout, "  history: %s\n", was.Render(
			"this ticket was merged and then unmerged",
			"never merged and unmerged"))
	}
	return cli.Success
}

// renderWorking is criterion 4 and criterion 7 on the page: for every input, what it was, which
// channel and source it came from, and why it was folded in.
//
// EVERY FIELD GOES THROUGH Render. Nothing here formats a value with %s or %q, which is what keeps
// criterion 12 true of the output and not merely of the type: an origin that could not be resolved
// prints its own sentence, and never a blank and never something a reader would take for a value.
func renderWorking(env cli.Env, m inbox.MergeRecord) {
	fmt.Fprintf(env.Stdout, "  this ticket is a merge of %d pieces of traffic, merged %s.\n",
		len(m.Inputs), m.MergedRender())
	fmt.Fprintf(env.Stdout, "  what was folded in, where each came from, and why:\n")
	for i, in := range m.Inputs {
		fmt.Fprintf(env.Stdout, "    %d. was:      %s\n", i+1, in.What.Render())
		fmt.Fprintf(env.Stdout, "       channel:  %s\n", in.Channel.Render())
		fmt.Fprintf(env.Stdout, "       source:   %s\n", in.Source.Render())
		fmt.Fprintf(env.Stdout, "       why:      %s\n", in.Why.Render())
		fmt.Fprintf(env.Stdout, "       ticket:   %s (restored under this identifier by an unmerge)\n", in.TicketID)
		for _, f := range []inbox.Field{in.What, in.Channel, in.Source, in.Why} {
			if why := f.Reason(); why != "" {
				fmt.Fprintf(env.Stdout, "                 (%s)\n", why)
			}
		}
	}
	fmt.Fprintf(env.Stdout, "  Every origin above is read from this ticket alone; no channel was consulted.\n")
}

// reportMergeFailure turns the inbox package's distinct error values into distinct sentences and
// distinct exit codes. Criteria 8 and 9 are stated as "distinguishable by exit status alone", and a
// command that collapsed two of these into one code would have lost that.
func reportMergeFailure(env cli.Env, op string, err error) int {
	switch {
	case errors.Is(err, inbox.ErrTooFewInputs):
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  Nothing was merged and the inbox is exactly as it was.\n")
		return cli.ExitUsage

	case errors.Is(err, inbox.ErrNoSuchTicket):
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  No merge has been made, no ticket has been changed, and no partial merge\n")
		fmt.Fprintf(env.Stderr, "  has been left behind. Run 'omw inbox list' to see the tickets there are.\n")
		return cli.ExitFailure

	case errors.Is(err, inbox.ErrNotMerged):
		// CRITERION 9. Non-zero, and the ticket is untouched.
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  There is nothing to take apart, so nothing has been taken apart and the\n")
		fmt.Fprintf(env.Stderr, "  ticket is exactly as it was.\n")
		return cli.ExitFailure

	case errors.Is(err, inbox.ErrNotWritten):
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  PRD §3.2: a merged ticket has a written title and a written summary — not\n")
		fmt.Fprintf(env.Stderr, "  five items titled `yes`, `ok` and `Hii`, and not those five on one line.\n")
		return cli.ExitFailure

	case errors.Is(err, inbox.ErrNotAnObligation):
		// CRITERION 11. There is no low-priority shelf to put it on, because there is no priority.
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  Nothing was merged. An acknowledgement is not a low-priority ticket; there\n")
		fmt.Fprintf(env.Stderr, "  is no priority in this product to put one at.\n")
		return cli.ExitFailure

	case errors.Is(err, inbox.ErrIncompleteWorking):
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  A merge that cannot show its working is a merge nobody can inspect, which\n")
		fmt.Fprintf(env.Stderr, "  PRD §3.2 does not allow. Nothing was changed.\n")
		return cli.ExitFailure

	case errors.Is(err, inbox.ErrUnreadableMerge), errors.Is(err, inbox.ErrUnreadableTicket),
		errors.Is(err, store.ErrUnreadable):
		// UNDETERMINED, NOT A FAILURE OF THE MERGE. Something is there and could not be read, which
		// is not the same answer as the merge being refused — and must not share its exit code.
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  Nothing was changed. This is NOT 'there is nothing there'.\n")
		return cli.ExitUndetermined

	default:
		fmt.Fprintf(env.Stderr, "omw ticket %s: %v.\n", op, err)
		fmt.Fprintf(env.Stderr, "  Nothing was changed.\n")
		return cli.ExitFailure
	}
}
