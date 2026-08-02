// Command `omw stats` — Issue #13, the CLI half of corpus statistics.
//
// This file is the ONLY file this Issue adds to package commands, and every identifier in it is
// prefixed `stats`, so two Issues adding commands concurrently never touch the same line.
//
// # What this command is about, in one distinction
//
// `notes: 0` and `notes: undetermined` are DIFFERENT FACTS and this command exists to keep them
// apart. A zero says "I looked and there is nothing there", and an agent will build a plan on it.
// An unknown printed as a zero is a lie that plan rests on (PRD §4.3, Issue #13's opening).
//
// # And in one shape
//
// A statistics request NEVER FAILS WHOLE OR SUCCEEDS WHOLE (criterion 8). There is no early return
// that abandons the response: every path assembles the same [hub.Report] with a local half and a
// hub half, and every statistic in it is either a determined value or an undetermined one carrying
// its reason code. "No hub is configured" does not suppress the local numbers — PRD §4.4, the
// local half stands alone — and an unreachable hub does not suppress them either.
//
// The exit code is the project's standing rule and nothing more: 0 when every statistic was
// determined, 3 when any was not. It is never the SOLE carrier of the distinction — criterion 6
// says a consumer must tell undetermined from zero by inspecting the output alone, so the reason
// code is on the line itself.
package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "stats",
		Summary: "the shape of the corpus you can read: what exists, how much, how recent",
		Run:     runStats,
	})
}

// Environment this command reads. Its own constants, not shared with another command file.
const (
	statsEnvHub      = "OMW_HUB"
	statsEnvSocket   = "OMW_CONTROL_SOCKET"
	statsEnvIdentity = "OMW_IDENTITY"
	statsEnvScopes   = "OMW_SCOPES"
	statsEnvOutbox   = "OMW_OUTBOX"
)

// statsSource is how this command reaches a hub.
//
// There is no client-to-hub transport in this build, stated rather than stubbed silently — the
// position `omw search` and `omw visibility` both took. A configured hub this build cannot talk to
// is UNREACHABLE, which is criterion 12: undetermined with its own reason, never an empty corpus.
//
// NOTE FOR CRITERION 11: neither this default nor anything it calls opens a socket, and the no-hub
// branch in runStats returns before this variable is read at all.
// TestStatsOpensNoNetworkWithoutAHub asserts the source is never consulted on that path.
var statsSource = func(env cli.Env) (*hub.Store, error) {
	return nil, hub.ErrHubUnreachable
}

// statsDaemonRunning reports whether the daemon is up.
//
// IT PROBES, IT DOES NOT NAME. The socket path comes from the environment and its existence is the
// answer; nothing here assumes a platform's convention for where a socket lives. PRD §4.2 and
// criterion 10: the daemon is never started on the person's behalf, and this command contains no
// code that could start one.
var statsDaemonRunning = func(env cli.Env) bool {
	p := strings.TrimSpace(env.Getenv(statsEnvSocket))
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// statsGrant builds the grant this request is issued under. Sign-in and token material are Issue
// #19's; until they exist the identity and held scopes come from the environment.
var statsGrant = func(env cli.Env) (hub.Grant, error) {
	who := strings.TrimSpace(env.Getenv(statsEnvIdentity))
	if who == "" {
		return hub.Grant{}, hub.ErrNotSignedIn
	}
	var scopes []hub.Scope
	for _, s := range strings.Split(env.Getenv(statsEnvScopes), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !hub.KnownScope(hub.Scope(s)) {
			return hub.Grant{}, hub.Refusedf(hub.ErrUnknownScope, "%q", s)
		}
		scopes = append(scopes, hub.Scope(s))
	}
	return hub.Grant{ID: hub.GrantID(who + "-env"), Holder: hub.PersonID(who), Scopes: scopes}, nil
}

type statsFlags struct {
	scope  string
	outbox string
	as     hub.PersonID
	json   bool
}

func runStats(env cli.Env) int {
	f := statsFlags{outbox: strings.TrimSpace(env.Getenv(statsEnvOutbox))}
	for i := 0; i < len(env.Args); i++ {
		a := env.Args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			statsUsage(env.Stdout)
			return cli.Success
		case a == "schema":
			return statsSchema(env)
		case a == "--scope" && i+1 < len(env.Args):
			i++
			f.scope = env.Args[i]
		case strings.HasPrefix(a, "--scope="):
			f.scope = strings.TrimPrefix(a, "--scope=")
		case a == "--outbox" && i+1 < len(env.Args):
			i++
			f.outbox = env.Args[i]
		case strings.HasPrefix(a, "--outbox="):
			f.outbox = strings.TrimPrefix(a, "--outbox=")
		case a == "--as" && i+1 < len(env.Args):
			i++
			f.as = hub.PersonID(env.Args[i])
		case strings.HasPrefix(a, "--as="):
			f.as = hub.PersonID(strings.TrimPrefix(a, "--as="))
		case a == "--json":
			f.json = true
		default:
			fmt.Fprintf(env.Stderr, "omw stats: unknown argument %q\n", a)
			statsUsage(env.Stderr)
			return cli.ExitUsage
		}
	}

	// CRITERION 2. A scope that is not one of the three is REFUSED here, before anything is
	// computed. It is not widened to the company and it is not narrowed to an empty answer, and
	// the parser is the one search uses so the two cannot drift.
	scope, err := hub.ParseSearchScope(f.scope)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw stats: %v (code: %s)\n\n", err, hub.Code(err))
		statsScopeBlock(env.Stderr)
		return cli.ExitUsage
	}

	identity := f.as
	if identity == "" {
		identity = hub.PersonID(strings.TrimSpace(env.Getenv(statsEnvIdentity)))
	}

	report := hub.Report{
		Scope: scope,
		Local: statsLocal(scope, identity, f.outbox),
	}

	hubHalf, refusal := statsHub(env, scope, f.as)
	if refusal != nil {
		// The scope was refused by the hub itself — an unknown person or group. THAT IS NOT AN
		// UNDETERMINED STATISTIC, it is a refusal of the request, so it does not get a report body
		// with plausible-looking undetermined fields in it.
		fmt.Fprintf(env.Stderr, "omw stats: %v (code: %s)\n", refusal, hub.Code(refusal))
		return cli.ExitFailure
	}
	report.Hub = hubHalf

	if f.json {
		s, err := report.JSON()
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw stats: %v\n", err)
			return cli.ExitFailure
		}
		fmt.Fprint(env.Stdout, s)
	} else {
		fmt.Fprint(env.Stdout, report.Render())
	}

	if !report.Determined() {
		// The standing rule: could-not-determine and determined-to-be-nothing never share an exit
		// code. The output already said which statistics and why; this is for the script.
		return cli.ExitUndetermined
	}
	return cli.Success
}

// statsHub computes the hub half, or returns a refusal.
//
// THE ORDER OF THE CHECKS IS THE ISSUE, and each produces a DIFFERENT reason code on the same
// shape of answer:
//
//	no hub configured   criterion 11 — determined fact about this machine; nothing is dialled
//	daemon not running  criterion 10 — said, never started, and told apart from any other reason
//	not signed in       there is no identity whose readable set the numbers could be computed over
//	hub unreachable     criterion 12 — NOT an empty corpus, and not "the hub reports nothing"
//
// The no-hub branch is FIRST so that criterion 11 holds by construction: the function that would
// reach a hub is not reached at all.
func statsHub(env cli.Env, scope hub.SearchScope, as hub.PersonID) (hub.Statistics, error) {
	if strings.TrimSpace(env.Getenv(statsEnvHub)) == "" {
		return hub.UndeterminedStatistics(scope, as, hub.ErrNoHubConfigured), nil
	}
	if !statsDaemonRunning(env) {
		return hub.UndeterminedStatistics(scope, as, hub.ErrDaemonNotRunning), nil
	}
	grant, err := statsGrant(env)
	if err != nil {
		return hub.UndeterminedStatistics(scope, as, statsErrorOf(err)), nil
	}
	store, err := statsSource(env)
	if err != nil || store == nil {
		return hub.UndeterminedStatistics(scope, as, hub.ErrHubUnreachable), nil
	}
	reader := as
	if reader == "" {
		reader = grant.Holder
	}
	st, err := hub.StatisticsThrough(store, grant, reader, scope)
	if err != nil {
		switch hub.Code(err) {
		case hub.ErrUnknownSearchScope.Code:
			return hub.Statistics{}, err
		default:
			return hub.UndeterminedStatistics(scope, reader, statsErrorOf(err)), nil
		}
	}
	return st, nil
}

// statsErrorOf recovers the hub error inside err so its CODE reaches the report. A reason that
// arrived as prose would make criterion 12's distinction unreadable by a script.
func statsErrorOf(err error) *hub.Error {
	for _, candidate := range []*hub.Error{
		hub.ErrNotSignedIn, hub.ErrUnknownScope, hub.ErrReadScopeRequired,
		hub.ErrGrantWiderThanHolder, hub.ErrHubUnreachable, hub.ErrNoHubConfigured,
		hub.ErrDaemonNotRunning, hub.ErrUndetermined,
	} {
		if hub.Code(err) == candidate.Code {
			return candidate
		}
	}
	return hub.ErrUndetermined
}

// statsLocal computes the local half from the local outbox, and from nothing else.
//
// PRD §4.4: the local half stands alone. It reads no hub environment variable, probes no daemon
// and dials nothing, so criterion 11's "local statistics that can be computed locally are returned
// as determined values" holds on the no-hub path with no special case for it.
//
// A local draft has NO AUDIENCE — it has not been published, so it is in no group's scope and in
// nobody's scope but its author's. That is why a group-scoped local count is a determined zero
// rather than an undetermined: we know what a draft is, and we know it is not in a group.
func statsLocal(scope hub.SearchScope, identity hub.PersonID, dir string) hub.Statistics {
	if strings.TrimSpace(dir) == "" {
		// PRD §4.2: omw does not conjure a store. "There is nowhere to look" is a determined fact
		// and it is still not a zero — nothing has been established about local material.
		return hub.UndeterminedStatistics(scope, identity, hub.ErrNoLocalStore)
	}
	o, err := drafts.Open(dir)
	if err != nil {
		return hub.UndeterminedStatistics(scope, identity, hub.ErrNoLocalStore)
	}

	st := hub.Statistics{
		Scope:             scope,
		Reader:            identity,
		UndeterminedNotes: hub.DeterminedCount(0),
		Coverage:          tri.Yes,
	}

	// Whether the drafts fall in the requested scope.
	switch scope.Kind() {
	case hub.SearchGroup:
		// Determined: an unpublished draft has no audience, so it is in no group.
		st.Notes = hub.DeterminedCount(0)
		st.Subjects = hub.DeterminedSubjects(nil, nil)
		st.Recency = hub.NoRecency()
		return st
	case hub.SearchPerson:
		if identity == "" {
			// We cannot tell whether these drafts are the scoped person's, because we do not know
			// who we are. Undetermined, not zero.
			return hub.UndeterminedStatistics(scope, identity, hub.ErrNotSignedIn)
		}
		if scope.Person() != identity {
			st.Notes = hub.DeterminedCount(0)
			st.Subjects = hub.DeterminedSubjects(nil, nil)
			st.Recency = hub.NoRecency()
			return st
		}
	}

	ids, err := o.Drafts()
	if err != nil {
		return hub.UndeterminedStatistics(scope, identity, hub.ErrUndetermined)
	}
	counted := 0
	newest := hub.NoRecency()
	undetermined := 0
	for _, id := range ids {
		versions, err := o.Timeline(id, identity)
		if err != nil || len(versions) == 0 {
			// A draft directory we could not read is not a draft that does not exist.
			undetermined++
			continue
		}
		counted++
		at := versions[len(versions)-1].At // §3.3: the LATEST version, exactly as on the hub side.
		if cur, ok := newest.At(); !ok || at.After(cur) {
			newest = hub.DeterminedRecency(at)
		}
	}
	st.UndeterminedNotes = hub.DeterminedCount(undetermined)
	if undetermined > 0 {
		st.Notes = hub.UndeterminedCount(hub.ErrUndetermined)
		st.Recency = hub.UndeterminedRecency(hub.ErrUndetermined)
		st.Subjects = hub.UndeterminedSubjects(hub.ErrUndetermined)
		st.Coverage = tri.Undetermined
		return st
	}
	st.Notes = hub.DeterminedCount(counted)
	st.Recency = newest
	if counted == 0 {
		st.Subjects = hub.DeterminedSubjects(nil, nil)
	} else if identity == "" {
		// The material is here and we can count it; whose it is, we cannot say.
		st.Subjects = hub.UndeterminedSubjects(hub.ErrNotSignedIn)
		st.Coverage = tri.Undetermined
	} else {
		st.Subjects = hub.DeterminedSubjects([]hub.PersonID{identity}, nil)
	}
	return st
}

func statsSchema(env cli.Env) int {
	for _, tool := range hub.StatsAPISchema() {
		fmt.Fprintf(env.Stdout, "tool: %s\n", tool.Tool)
		fmt.Fprintf(env.Stdout, "  %s\n", tool.Description)
		fmt.Fprintf(env.Stdout, "  scopes: %s\n", strings.Join(tool.Scopes, ", "))
	}
	return cli.Success
}

func statsUsage(w io.Writer) {
	fmt.Fprint(w, `omw stats — the shape of the corpus you can read

usage: omw stats [--scope <scope>] [--as <person>] [--outbox <dir>] [--json]

  --scope <scope>   company (default), person:<id>, or group:<id>
  --as <person>     compute the statistics as that person; refused unless your grant acts as them
  --outbox <dir>    the local outbox to count local material in (or $OMW_OUTBOX)
  --json            the agent API's rendering of the very same report

  schema            what the agent API says about this capability

Every statistic is either a determined value or the explicit undetermined marker, and the two never
print the same. A count of 0 means there is nothing there; `+"`undetermined`"+` means it could not be
worked out, and nothing has been established either way. Exit 3 says at least one statistic was
undetermined.
`)
}

// statsScopeBlock prints the three SEARCH scopes and says plainly that they are not the capability
// vocabulary — which remains exactly read / write / publish. Statistics added no fourth scope.
func statsScopeBlock(w io.Writer) {
	fmt.Fprintf(w, "Which corpus do you want the shape of?\n")
	for _, l := range hub.SearchScopeSyntax {
		fmt.Fprintf(w, "  %s\n", l)
	}
	fmt.Fprintf(w, "\nThese are subjects, not capabilities. The capability vocabulary is unchanged and is:\n")
	for _, s := range hub.Vocabulary() {
		fmt.Fprintf(w, "  %s\n", string(s))
	}
	fmt.Fprintf(w, "\nStatistics need %q, and nothing else. This added no fourth capability.\n", string(hub.ScopeRead))
}
