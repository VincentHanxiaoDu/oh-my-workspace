// The `omw inbox` command: the person's list of things they have to act on, one ticket read in
// full, and the only way a ticket ever leaves — the person deleting it (PRD §2.3, §3.2, §4.2, §4.3,
// §4.4, §5.4; Issue #8).
//
// WHAT THIS FILE IS CAREFUL ABOUT, IN ORDER OF HOW EASY IT WOULD BE TO GET WRONG:
//
//  1. AN EMPTY INBOX AND AN INBOX THAT COULD NOT BE READ ARE DIFFERENT ANSWERS, with different
//     sentences and different exit codes (criterion 3). The tempting implementation prints nothing
//     and exits zero in both cases, and the person reads "you are done for the day" off a store
//     they do not have permission to open.
//  2. A MISSING FIELD AND AN EMPTY FIELD ARE DIFFERENT ANSWERS (criterion 1). This file never
//     prints a ticket field with %s or %q; every one goes through [inbox.Field.Render], which is
//     the only place the four states have their four sentences.
//  3. NOTHING HERE REACHES A HUB, and there is no configuration under which it would (criterion 6,
//     7, 8). There is no hub client imported, no address read, no connection opened. The header
//     says so rather than leaving its absence to be inferred from silence.
//  4. NOTHING HERE STARTS THE DAEMON (§4.2, criterion 13), AND NOTHING HERE WORKS OUT WHETHER ONE
//     IS RUNNING. It asks daemonLiveness, which is the same answer `omw daemon status` gives, and
//     it reports all three of that answer's values. It used to stat a socket whose name this
//     package had guessed, and printed a confident "not running" whatever the daemon was doing —
//     see the note on header, and Issue #41.
//
// (Detached from the package clause on purpose: doc.go carries this package's doc comment, and
// several Issues add files here concurrently.)

package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "inbox",
		Summary: "list the things you have to act on, read one, or delete one",
		Run:     runInbox,
	})
}

// inboxUsage is assembled from [inbox.Operations] rather than written out, so that the help and the
// enumeration criterion 6 asserts on cannot drift apart. Help text that is maintained separately
// from the operations is help text that will one day advertise an operation nobody implemented — or
// hide one somebody did.
func inboxUsage() string {
	var b strings.Builder
	b.WriteString("usage: omw inbox <")
	for i, op := range inbox.Operations() {
		if i > 0 {
			b.WriteString("|")
		}
		b.WriteString(op.Name)
	}
	b.WriteString("> [ticket]\n\n")
	for _, op := range inbox.Operations() {
		fmt.Fprintf(&b, "  %-7s %s\n", op.Name, op.Summary)
	}
	b.WriteString("\nThat is every operation the inbox has. Tickets are never published (PRD §2.3):\n")
	b.WriteString("there is no publish, share, send or export here, under any name.\n")
	return b.String()
}

func runInbox(env cli.Env) int {
	if len(env.Args) == 0 {
		fmt.Fprint(env.Stderr, inboxUsage())
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(env.Stdout, inboxUsage())
		return cli.Success
	}
	sub, rest := env.Args[0], env.Args[1:]
	known := false
	for _, op := range inbox.Operations() {
		if op.Name == sub {
			known = true
		}
	}
	if !known {
		// NAMED, NOT A USAGE DUMP ALONE — and the name is echoed so a typo is visible. `publish` is
		// the specific word worth being unambiguous about: it is not unimplemented here, it does
		// not exist, and the message says which.
		fmt.Fprintf(env.Stderr, "omw inbox: there is no %q operation on a ticket.\n", sub)
		fmt.Fprint(env.Stderr, inboxUsage())
		return cli.ExitUsage
	}
	if strings.HasPrefix(sub, "-") {
		fmt.Fprint(env.Stderr, inboxUsage())
		return cli.ExitUsage
	}

	switch sub {
	case "list":
		if len(rest) != 0 {
			fmt.Fprintf(env.Stderr, "omw inbox list: takes no arguments; got %q\n", strings.Join(rest, " "))
			return cli.ExitUsage
		}
		return inboxList(env)
	case "read":
		id, code := oneTicketID(env, "read", rest)
		if code != cli.Success {
			return code
		}
		return inboxRead(env, id)
	case "delete":
		id, code := oneTicketID(env, "delete", rest)
		if code != cli.Success {
			return code
		}
		return inboxDelete(env, id)
	}
	// Unreachable while Operations and this switch agree; asserted by
	// TestEveryEnumeratedOperationIsDispatched so that adding one to the enumeration without
	// wiring it is a red test rather than this line at runtime.
	fmt.Fprintf(env.Stderr, "omw inbox: %q is listed as an operation but is not wired up\n", sub)
	return cli.ExitFailure
}

func oneTicketID(env cli.Env, op string, rest []string) (string, int) {
	switch len(rest) {
	case 1:
		return rest[0], cli.Success
	case 0:
		fmt.Fprintf(env.Stderr, "omw inbox %s: name the ticket to %s. Run 'omw inbox list' to see them.\n", op, op)
		return "", cli.ExitUsage
	default:
		fmt.Fprintf(env.Stderr, "omw inbox %s: name exactly one ticket; got %d\n", op, len(rest))
		return "", cli.ExitUsage
	}
}

// openInbox resolves and opens this device's store, turning the store's distinct failures into the
// distinct sentences and exit codes criterion 3 and criterion 14 require.
//
// THE NO-STORE CASE IS A FAILURE AND NOT AN EMPTY INBOX. This is the single most important line in
// the file: a person with no store has not finished their work, and must never be shown a page that
// reads as though they had.
func openInbox(env cli.Env) (*store.Store, string, int) {
	root, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw inbox: %v\n", err)
		fmt.Fprintf(env.Stderr, "  This is not an empty inbox. Where the inbox lives could not be determined.\n")
		return nil, "", cli.ExitUndetermined
	}
	s, err := store.Open(root)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(env.Stderr, "omw inbox: there is no store at %s, so the inbox could not be read at all.\n", root)
		fmt.Fprintf(env.Stderr, "  This is NOT an empty inbox — nothing has been read. Run 'omw store create' first.\n")
		return nil, root, cli.ExitFailure
	case err != nil:
		fmt.Fprintf(env.Stderr, "omw inbox: the store at %s could not be opened: %v\n", root, err)
		fmt.Fprintf(env.Stderr, "  This is NOT an empty inbox — whether you have tickets could not be determined.\n")
		return nil, root, cli.ExitUndetermined
	}
	return s, root, cli.Success
}

// header states what the person is looking at BEFORE the tickets, because two of the criteria are
// about not being misled by a listing that looks live, or that looks like it consulted a hub.
//
// THE DAEMON ANSWER IS NOT THIS FILE'S TO WORK OUT (Issue #41). It used to be: this command asked
// an inbox-local probe that stat'ed a control socket whose name the inbox package had guessed, and
// it therefore printed a confident "not running" whatever the daemon was doing. Three surfaces made
// the same locally-reasonable mistake on the same day. The answer now comes from daemonLiveness,
// which asks daemon.Inspect — the same function `omw daemon status` answers from, which is the only
// construction under which the two cannot disagree.
//
// IT IS THREE-VALUED, AND THE THIRD VALUE CHANGES WHAT MAY BE SAID. The "read from the store on
// disk" sentence is printed ONLY when it is ESTABLISHED that no daemon is running. Where liveness
// could not be determined, the inbox has not established an absence and may not explain itself by
// one — that is #41's criterion 5, and it is the same rule as §4.3 read one level up: a claim that
// rests on a "no" must not be made on an "I could not tell".
func header(env cli.Env, root string) {
	fmt.Fprintf(env.Stdout, "inbox:       %s\n", root)

	// §4.2 / criterion 13. Said, in three values, and never by starting anything.
	live, why := daemonLiveness(env)
	fmt.Fprintf(env.Stdout, "daemon:      %s\n", live.Render("running", "not running"))
	if why != "" {
		fmt.Fprintf(env.Stdout, "             %s\n", why)
	}
	switch live {
	case tri.No:
		fmt.Fprintf(env.Stdout, "             these tickets were read from the store on disk, and this command\n")
		fmt.Fprintf(env.Stdout, "             has not started anything (PRD §4.2)\n")
	case tri.Undetermined:
		// NOT AN ABSENCE, AND SAID SO. Without this the reader takes the line above for a stopped
		// daemon, which is the exact collapse §4.3 forbids.
		fmt.Fprintf(env.Stdout, "             this is not a report that the daemon is stopped; nothing about it\n")
		fmt.Fprintf(env.Stdout, "             has been established. 'omw daemon status' reports the same state.\n")
	}

	// §4.6 / §5.1 / Issue #8 criterion 15. THE INBOX NO LONGER ANSWERS THIS, and the pointer is
	// what is left of it. Whether the control API is open — including the case where owner-only
	// socket permissions could not be confirmed and it therefore did not open — is reported by
	// `omw daemon status`, which reads it from the daemon that owns the socket. The inbox restating
	// it from its own probe is precisely the four-answers defect Issue #41 removed, and the answer
	// it used to give was wrong. See the pull request: this is a criterion of #8 that #41 has
	// reassigned to another surface, and it is flagged rather than quietly dropped.
	fmt.Fprintf(env.Stdout, "control api: reported by 'omw daemon status', which is the one surface for it\n")

	// §2.3 / criteria 6-8. Stated, not left to be inferred from an absence of output.
	fmt.Fprintf(env.Stdout, "hub:         not contacted, and there is no operation here that would.\n")
	fmt.Fprintf(env.Stdout, "             Tickets live on this machine and are never published.\n")
	fmt.Fprintln(env.Stdout)
}

// renderTicket prints one ticket. Every field goes through Render; nothing here formats a value
// directly, which is what keeps criterion 1 and criterion 12 true of the output and not merely of
// the type.
func renderTicket(env cli.Env, t inbox.Ticket) {
	fmt.Fprintf(env.Stdout, "ticket %s\n", t.ID)
	fmt.Fprintf(env.Stdout, "  title:   %s\n", t.Title.Render())
	fmt.Fprintf(env.Stdout, "  summary: %s\n", t.Summary.Render())
	fmt.Fprintf(env.Stdout, "  channel: %s\n", t.Channel.Render())
	fmt.Fprintf(env.Stdout, "  arrived: %s\n", t.ArrivedRender())
	for _, f := range []inbox.Field{t.Title, t.Summary, t.Channel} {
		if why := f.Reason(); why != "" {
			fmt.Fprintf(env.Stdout, "           (%s)\n", why)
		}
	}
}

func inboxList(env cli.Env) int {
	s, root, code := openInbox(env)
	if code != cli.Success {
		return code
	}
	header(env, root)

	tickets, err := inbox.List(s)
	if err != nil {
		// CRITERION 12: the inbox's own count unreadable is UNDETERMINED, and criterion 3: it is
		// not an empty inbox and does not share the empty inbox's exit code.
		fmt.Fprintf(env.Stdout, "tickets:     %s\n", tri.Undetermined.Render("", ""))
		fmt.Fprintf(env.Stderr, "omw inbox list: %v\n", err)
		fmt.Fprintf(env.Stderr, "  How many tickets you have could not be determined. This is NOT an empty inbox,\n")
		fmt.Fprintf(env.Stderr, "  and it is NOT a listing with that ticket left out.\n")
		return cli.ExitUndetermined
	}
	if len(tickets) == 0 {
		// EMPTY, AND SAID (criterion 3). A determined answer, on the success exit code, in wording
		// that cannot be read as a failure.
		fmt.Fprintf(env.Stdout, "tickets:     none — the inbox is empty and there is nothing you have to act on.\n")
		fmt.Fprintf(env.Stdout, "             The inbox was read successfully; nothing was hidden and nothing\n")
		fmt.Fprintf(env.Stdout, "             could not be read.\n")
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "tickets:     %d\n\n", len(tickets))
	for i, t := range tickets {
		if i > 0 {
			fmt.Fprintln(env.Stdout)
		}
		renderTicket(env, t)
	}
	return cli.Success
}

func inboxRead(env cli.Env, id string) int {
	s, root, code := openInbox(env)
	if code != cli.Success {
		return code
	}
	t, err := inbox.Get(s, id)
	if err != nil {
		if errors.Is(err, inbox.ErrNoSuchTicket) {
			// CRITERION 2: non-zero, with the ticket rendering ABSENT FROM STDOUT. Nothing about
			// this ticket has been printed — not even the header, which is why the header comes
			// after the read and not before it.
			fmt.Fprintf(env.Stderr, "omw inbox read: there is no ticket %q in the inbox at %s.\n", id, root)
			fmt.Fprintf(env.Stderr, "  Run 'omw inbox list' to see the tickets there are.\n")
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stdout, "ticket %s: %s\n", id, tri.Undetermined.Render("", ""))
		fmt.Fprintf(env.Stderr, "omw inbox read: %v\n", err)
		return cli.ExitUndetermined
	}
	header(env, root)
	renderTicket(env, t)
	// CRITERION 2: "the fact that it is an inbox ticket" is part of what a read renders.
	fmt.Fprintf(env.Stdout, "  this is an inbox ticket: it is held on this machine and is never published\n")
	return cli.Success
}

func inboxDelete(env cli.Env, id string) int {
	s, root, code := openInbox(env)
	if code != cli.Success {
		return code
	}
	if err := inbox.Delete(s, id); err != nil {
		if errors.Is(err, inbox.ErrNoSuchTicket) {
			// CRITERION 11: non-zero, and nothing else in the inbox has been touched.
			fmt.Fprintf(env.Stderr, "omw inbox delete: there is no ticket %q in the inbox at %s, so nothing\n", id, root)
			fmt.Fprintf(env.Stderr, "  has been deleted. Every other ticket is exactly as it was.\n")
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stderr, "omw inbox delete: ticket %q could not be deleted: %v\n", id, err)
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "deleted ticket %s from the inbox at %s\n", id, root)
	fmt.Fprintf(env.Stdout, "This is the only way a ticket leaves the inbox. Nothing expires on its own.\n")
	return cli.Success
}
