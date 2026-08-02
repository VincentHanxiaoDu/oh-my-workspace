// Command `omw report` — Issue #23, subscribing to your own work (PRD §3.7).
//
// This file is the ONLY file this Issue adds to package commands. It shares no helper with any
// other command file: the two environment variable names below are spelled out again rather than
// borrowed, so that neither Issue's file appears in the other's diff.
package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/reports"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "report",
		Summary: "subscribe to your own work, and read what it produced",
		Run:     runReport,
	})
}

const reportEnvHub = "OMW_HUB"

func runReport(env cli.Env) int {
	if len(env.Args) == 0 {
		reportUsage(env.Stdout)
		return cli.ExitUsage
	}
	rest := env.Args[1:]
	switch env.Args[0] {
	case "subscribe":
		return reportSubscribe(env, rest)
	case "show":
		return reportShow(env, rest)
	case "list":
		return reportList(env, rest)
	case "run":
		return reportRun(env, rest)
	case "subjects":
		return reportSubjects(env)
	case "-h", "--help", "help":
		reportUsage(env.Stdout)
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw report: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw report help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func reportUsage(w io.Writer) {
	fmt.Fprint(w, `omw report — a standing instruction about your own work

usage: omw report <subcommand>

  subscribe <name> <selectors>   write a subscription, or refuse it and store nothing
  show <name>                    read a subscription back, exactly as written
  list                           the subscriptions on this machine
  run <name>                     the report that subscription produces now
  subjects                       the subjects this build knows

A selector names a subject and a granularity: git:full, token_usage:digest, *:summary,
git.commit:event, and a list may exclude: *, !channel

The five granularities are ordered by detail — full, event, digest, summary, count — and mean
the same thing for every subject, which is what makes *:summary worth typing.

A selector that cannot be read is REFUSED and nothing is stored. A selector that names no known
subject is reported as unmatched, by name, every time the report runs — it never comes back as
an empty report, because an empty report looks exactly like a quiet day.

Everything here is local: subscriptions live in this device's store, no command starts the
daemon, and with no hub configured nothing reaches out.
`)
}

type reportFlags struct {
	rest []string
}

func parseReportFlags(args []string, stderr io.Writer) (reportFlags, bool) {
	var f reportFlags
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			fmt.Fprintf(stderr, "omw report: unknown flag %q\n", a)
			return f, false
		}
		f.rest = append(f.rest, a)
	}
	return f, true
}

// openReportStore opens THIS DEVICE'S store, without creating one (§4.2). A missing store is said,
// never conjured, and never reported as a subscription list that happens to be empty.
//
// IT RESOLVES THE STORE THE SAME WAY daemonLiveness DOES, and this command takes no --store flag,
// because the two questions must be about the same store. A report over one store that reported the
// daemon state of another would be two answers to two different questions printed as one screen.
func openReportStore(env cli.Env, what string, f reportFlags) (*store.Store, int, bool) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw report %s: %v\n", what, err)
		return nil, cli.ExitUndetermined, false
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw report %s: %v\n", what, err)
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(env.Stderr, "  omw does not conjure a store; 'omw store create' makes one on purpose.\n")
			return nil, cli.ExitFailure, false
		}
		// An unreadable store is undetermined. It is not an absence of subscriptions.
		return nil, cli.ExitUndetermined, false
	}
	return s, cli.Success, true
}

// sayDaemon reports the daemon's state on EVERY subscription operation (criterion 20).
//
// Printed always, not only when it is running, because "the command says so rather than appearing
// to succeed silently" is a line about the case where it is NOT running. Nothing here starts it.
//
// THE ANSWER COMES FROM daemonLiveness AND IS NOT DERIVED HERE (Issue #41). This command previously
// stat'ed a path named by an environment variable nothing in the product sets, which answered "not
// running" unconditionally — a confident false negative that contradicted `omw daemon status` while
// looking entirely normal. No package outside `internal/daemon` derives a control socket path, and
// there is a test over the whole tree that keeps it that way.
//
// IT IS THREE-VALUED, and the third value is why this is a switch and not an if. Liveness that
// could not be established is rendered as undetermined, with its reason and with the sentence that
// stops a reader taking it for a stopped daemon (§4.3).
//
// The value does NOT change this command's exit code. These operations are local and need no daemon
// (§4.4); the daemon's state is reported because criterion 20 requires it be said, not because
// anything here depends on it. The exit code answers the question the person asked.
func sayDaemon(env cli.Env) {
	live, why := daemonLiveness(env)
	switch live {
	case tri.Yes:
		fmt.Fprintf(env.Stdout, "daemon: running\n")
	case tri.No:
		fmt.Fprintf(env.Stdout, "daemon: not running — subscriptions are written and read on this machine, and nothing here has started it\n")
	default:
		if why == "" {
			why = "no reason was recorded, which is itself a thing that could not be determined"
		}
		fmt.Fprintf(env.Stdout, "daemon: whether it is running %s (code: %s)\n", tri.Undetermined, codeDaemonUndetermined)
		fmt.Fprintf(env.Stdout, "  %s\n", why)
		fmt.Fprintf(env.Stdout, "  this is not a report that the daemon is stopped; nothing about it has been established.\n")
		fmt.Fprintf(env.Stdout, "  these subscriptions are local and need no daemon, so this report stands either way.\n")
	}
}

func reportSubscribe(env cli.Env, args []string) int {
	f, ok := parseReportFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) < 2 {
		fmt.Fprintf(env.Stderr, "omw report subscribe: name the subscription, then its selectors.\n")
		fmt.Fprintf(env.Stderr, "  for example: omw report subscribe daily 'git:full, token_usage:digest'\n")
		return cli.ExitUsage
	}
	name := f.rest[0]
	list := strings.Join(f.rest[1:], " ")

	// PARSED BEFORE THE STORE IS OPENED, let alone written. A refusal must not be able to leave a
	// trace, and the cheapest way to guarantee that is to have nothing open when it happens.
	sels, err := reports.ParseSelectors(list)
	if err != nil {
		// REFUSED, LOUDLY, BY NAME (criterion 11). ExitUsage — which no matched-nothing outcome
		// uses, so a script can tell a refusal from a report that found no subject.
		fmt.Fprintf(env.Stderr, "omw report subscribe: %v\n", err)
		fmt.Fprintf(env.Stderr, "  nothing has been stored. The subscription is unchanged.\n")
		return cli.ExitUsage
	}

	s, code, ok := openReportStore(env, "subscribe", f)
	if !ok {
		return code
	}
	sub, err := reports.Save(s, name, list)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw report subscribe: %v\n", err)
		fmt.Fprintf(env.Stderr, "  nothing has been stored.\n")
		return cli.ExitUsage
	}

	sayDaemon(env)
	fmt.Fprintf(env.Stdout, "subscription: %s\n", sub.Name)
	for _, sel := range sub.Selectors {
		fmt.Fprintf(env.Stdout, "  %s\n", sel)
	}
	// SAID AT WRITE TIME AS WELL AS AT RUN TIME. A person who mistypes a subject should not have to
	// run the report to find out, and the report says it again because that is where the silence
	// would otherwise be.
	unmatchedNow := unmatchedSelectors(sels)
	for _, u := range unmatchedNow {
		fmt.Fprintf(env.Stdout, "unmatched selector %q: no subject by that name is known to this client\n", u)
	}
	if len(unmatchedNow) > 0 {
		fmt.Fprintf(env.Stdout, "it is stored as written; its reports will say this each time rather than coming back empty.\n")
		return cli.ExitFailure
	}
	return cli.Success
}

// unmatchedSelectors names the selectors that match no known subject. A wildcard is never among
// them (criterion 16).
func unmatchedSelectors(sels []reports.Selector) []string {
	var out []string
	for _, s := range sels {
		if s.IsWildcard() {
			continue
		}
		if _, ok := reports.LookupSubject(s.Subject); !ok {
			out = append(out, s.String())
		}
	}
	return out
}

func reportShow(env cli.Env, args []string) int {
	f, ok := parseReportFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw report show: name exactly one subscription.\n")
		return cli.ExitUsage
	}
	s, code, ok := openReportStore(env, "show", f)
	if !ok {
		return code
	}
	sub, _, err := reports.Load(s, f.rest[0])
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw report show: %v\n", err)
		if errors.Is(err, reports.ErrNoSuchSubscription) {
			return cli.ExitFailure
		}
		return cli.ExitUndetermined
	}
	sayDaemon(env)
	fmt.Fprintf(env.Stdout, "subscription: %s\n", sub.Name)
	for _, sel := range sub.Selectors {
		fmt.Fprintf(env.Stdout, "  %s\n", sel)
	}
	return cli.Success
}

func reportList(env cli.Env, args []string) int {
	f, ok := parseReportFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	s, code, ok := openReportStore(env, "list", f)
	if !ok {
		return code
	}
	subs, err := reports.List(s)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw report list: %v\n", err)
		return cli.ExitUndetermined
	}
	sayDaemon(env)
	fmt.Fprintf(env.Stdout, "subscriptions: %d\n", len(subs))
	for _, sub := range subs {
		fmt.Fprintf(env.Stdout, "  %s: %s\n", sub.Name, strings.Join(sub.Selectors, ", "))
	}
	return cli.Success
}

func reportRun(env cli.Env, args []string) int {
	f, ok := parseReportFlags(args, env.Stderr)
	if !ok {
		return cli.ExitUsage
	}
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw report run: name exactly one subscription.\n")
		return cli.ExitUsage
	}
	s, code, ok := openReportStore(env, "run", f)
	if !ok {
		return code
	}
	sub, sels, err := reports.Load(s, f.rest[0])
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw report run: %v\n", err)
		if errors.Is(err, reports.ErrNoSuchSubscription) {
			return cli.ExitFailure
		}
		return cli.ExitUndetermined
	}

	// WHETHER A HUB IS CONFIGURED IS A LOCAL FACT, established by reading the environment. No
	// connection is opened to find out, and none is opened afterwards either (§4.2, criterion 21).
	hubConfigured := strings.TrimSpace(env.Getenv(reportEnvHub)) != ""
	src := reports.StoreSource{Store: s, HubConfigured: hubConfigured}

	sayDaemon(env)
	fmt.Fprintf(env.Stdout, "report: %s\n", sub.Name)
	r := reports.Build(sels, src)
	fmt.Fprint(env.Stdout, r.Render())

	// THE THREE OUTCOMES GET THREE CODES, and the undetermined one is checked first because "I
	// could not establish this" outranks "you asked for something that does not exist".
	switch {
	case !r.Determined():
		return cli.ExitUndetermined
	case r.HasUnmatched(), r.HasMissingHub():
		return cli.ExitFailure
	default:
		return cli.Success
	}
}

func reportSubjects(env cli.Env) int {
	fmt.Fprintf(env.Stdout, "subjects this build knows:\n")
	for _, s := range reports.Catalog() {
		marks := []string{}
		if s.Root {
			marks = append(marks, "named by *")
		}
		if s.HubOnly {
			marks = append(marks, "supplied by the hub")
		}
		fmt.Fprintf(env.Stdout, "  %-16s %s [%s]\n", s.Name, s.About, strings.Join(marks, "; "))
	}
	fmt.Fprintf(env.Stdout, "granularities, most detailed first: %s\n", reports.GranularityNames())
	return cli.Success
}
