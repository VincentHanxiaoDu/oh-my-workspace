// Command `omw search` — Issue #15, the CLI half of finding an answer in the corpus.
//
// This file is the ONLY file this Issue adds to package commands, and it references nothing in the
// other command files: every identifier here is prefixed `search`, so two Issues adding commands
// concurrently never touch the same line.
//
// # What this command is actually about
//
// Three outcomes that a lazier command would render as one blank screen, and one that would be
// rendered as a leak:
//
//	found nothing            the search ran, matched nothing        exit 0
//	could not run            no hub, no daemon, not signed in       exit 1
//	could not determine      hub unreachable, partial coverage      exit 3
//
// `could not determine` and `determined to be nothing` never share an exit code — the project's
// standing rule, and criterion 15 states it for search specifically. The fourth, the leak, is
// handled entirely in internal/hub: this command prints [hub.Outcome.Render] and adds nothing of
// its own to the result block, because a count assembled here would be a second count.
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
		Name:    "search",
		Summary: "find an answer in the corpus, scoped to a person, a group, or the company",
		Run:     runSearch,
	})
}

// Environment this command reads. Its own constants, not shared with another command file.
const (
	searchEnvHub      = "OMW_HUB"
	searchEnvSocket   = "OMW_CONTROL_SOCKET"
	searchEnvIdentity = "OMW_IDENTITY"
	searchEnvScopes   = "OMW_SCOPES"
)

// searchSource is how this command reaches a hub.
//
// THERE IS NO CLIENT-TO-HUB TRANSPORT IN THIS BUILD, and that is stated rather than stubbed
// silently — the same position Issue #12's `omw visibility` took. A configured hub this build
// cannot talk to is UNREACHABLE, which is criterion 15: said, non-zero, never an empty result set.
// Tests replace this to drive the hub-backed paths against an in-memory store.
//
// NOTE FOR CRITERION 21: neither this default nor anything it calls opens a socket. The no-hub
// branch in runSearch returns before this variable is read at all, and
// TestSearchOpensNoNetworkWithoutAHub asserts the source is never consulted on that path.
var searchSource = func(env cli.Env) (*hub.Store, error) {
	return nil, hub.ErrHubUnreachable
}

// searchRoster supplies the hub's record of who is still with the company (PRD §5.4). Nil means
// this build cannot tell, which renders as undetermined rather than as "everyone is still here".
var searchRoster = func(env cli.Env) *hub.Roster { return nil }

// searchDaemonRunning reports whether the daemon is up.
//
// IT PROBES, IT DOES NOT NAME. The socket path comes from the environment and its existence is the
// answer; nothing here assumes a platform's convention for where a socket lives. PRD §4.2 and
// criterion 20: the daemon is never started on the person's behalf.
var searchDaemonRunning = func(env cli.Env) bool {
	p := strings.TrimSpace(env.Getenv(searchEnvSocket))
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// searchGrant builds the grant this search is issued under.
//
// Sign-in and token material are Issue #19's. Until they exist the identity and the held scopes
// come from the environment, which is enough for the distinction criterion 16 asks for: no
// identity is NOT SIGNED IN, and an identity holding only `write` or only `publish` is refused by
// [hub.SearchThrough] with its own code.
var searchGrant = func(env cli.Env) (hub.Grant, error) {
	who := strings.TrimSpace(env.Getenv(searchEnvIdentity))
	if who == "" {
		return hub.Grant{}, hub.ErrNotSignedIn
	}
	var scopes []hub.Scope
	for _, s := range strings.Split(env.Getenv(searchEnvScopes), ",") {
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

func runSearch(env cli.Env) int {
	if len(env.Args) == 0 {
		searchUsage(env.Stdout)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help", "help":
		searchUsage(env.Stdout)
		return cli.Success
	case "scopes":
		return searchScopes(env)
	}

	var terms []string
	scopeArg := ""
	as := hub.PersonID("")
	for i := 0; i < len(env.Args); i++ {
		a := env.Args[i]
		switch {
		case a == "--scope" && i+1 < len(env.Args):
			i++
			scopeArg = env.Args[i]
		case strings.HasPrefix(a, "--scope="):
			scopeArg = strings.TrimPrefix(a, "--scope=")
		case a == "--as" && i+1 < len(env.Args):
			i++
			as = hub.PersonID(env.Args[i])
		case strings.HasPrefix(a, "--as="):
			as = hub.PersonID(strings.TrimPrefix(a, "--as="))
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(env.Stderr, "omw search: unknown option %q\n", a)
			return cli.ExitUsage
		default:
			terms = append(terms, a)
		}
	}
	if len(terms) == 0 {
		fmt.Fprintf(env.Stderr, "omw search: say what you are looking for.\n")
		return cli.ExitUsage
	}

	scope, err := hub.ParseSearchScope(scopeArg)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n\n", err, hub.Code(err))
		searchScopeBlock(env.Stderr)
		return cli.ExitUsage
	}

	// NOTHING IMPLICIT, IN THIS ORDER (PRD §4.2, §4.4).
	//
	// No hub configured is a DETERMINED FACT about this machine and is stated precisely — criterion
	// 22 — and it is checked FIRST so that criterion 21 holds by construction: the function that
	// would reach a hub is not reached. It is not the same answer as "the hub did not respond", and
	// it carries its own code.
	if strings.TrimSpace(env.Getenv(searchEnvHub)) == "" {
		fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n", hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		fmt.Fprintf(env.Stderr, "  the corpus lives on the hub, and there is no hub to ask. No connection was attempted.\n")
		fmt.Fprintf(env.Stderr, "  this is NOT an empty result: nothing has been established about what is or is not written up.\n")
		return cli.ExitFailure
	}
	// Criterion 20: said, never started. A search does not bring the daemon up on the person's
	// behalf, and this command has no code that could.
	if !searchDaemonRunning(env) {
		fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n", hub.ErrDaemonNotRunning, hub.ErrDaemonNotRunning.Code)
		return cli.ExitFailure
	}

	grant, err := searchGrant(env)
	if err != nil {
		// Criterion 16: not signed in, said, and distinguishable from both "found nothing" and
		// "the hub could not be reached".
		fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n", err, hub.Code(err))
		return cli.ExitFailure
	}

	store, err := searchSource(env)
	if err != nil || store == nil {
		if err == nil {
			err = hub.ErrHubUnreachable
		}
		// CRITERION 15. Never an empty result set: there is no results block on this path at all,
		// and the exit code is the undetermined one, not the one an empty search uses.
		fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n", err, hub.Code(err))
		fmt.Fprintf(env.Stderr, "  the search could not be performed. This is not the same as finding nothing.\n")
		return cli.ExitUndetermined
	}

	out, err := hub.SearchThroughWith(store, grant, as, hub.Query{Terms: strings.Join(terms, " "), Scope: scope}, nil, searchRoster(env))
	if err != nil {
		switch hub.Code(err) {
		case hub.ErrUndetermined.Code, hub.ErrHubUnreachable.Code:
			// CRITERION 19. Undetermined is a third value and gets the third exit code.
			fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n", err, hub.Code(err))
			fmt.Fprintf(env.Stderr, "  this is undetermined, not a negative answer.\n")
			return cli.ExitUndetermined
		default:
			// CRITERION 4 and CRITERION 12 both land here, each with its own code: an unknown scope
			// and a refused-at-request scope are failures, not zero-result searches.
			fmt.Fprintf(env.Stderr, "omw search: %v (code: %s)\n", err, hub.Code(err))
			return cli.ExitFailure
		}
	}

	// THE RESULT BLOCK IS THE HUB'S, BYTE FOR BYTE. This command does not add a count, a total or a
	// "showing N of M" of its own — a second count is a second chance to include something the
	// searcher may not read.
	fmt.Fprint(env.Stdout, out.Render())
	if out.Coverage != tri.Yes {
		// Criterion 17 and 18: an incomplete answer is never silent and never exits as though it
		// were complete.
		fmt.Fprintf(env.Stderr, "omw search: the corpus was not fully covered (code: %s)\n", hub.ErrUndetermined.Code)
		return cli.ExitUndetermined
	}
	return cli.Success
}

func searchUsage(w io.Writer) {
	fmt.Fprint(w, `omw search — find an answer in the corpus

usage: omw search <terms...> [--scope <scope>] [--as <person>]

  --scope <scope>   company (default), person:<id>, or group:<id>
  --as <person>     search as that person; refused unless your grant acts as them

  scopes            the three search scopes and what they mean

Searching requires the read scope. A search that could not run is never shown as a search that
found nothing: it says why, and it exits non-zero.
`)
}

func searchScopes(env cli.Env) int {
	searchScopeBlock(env.Stdout)
	return cli.Success
}

// searchScopeBlock prints the three SEARCH scopes, and says plainly that they are not the
// capability vocabulary — which remains exactly read / write / publish.
func searchScopeBlock(w io.Writer) {
	fmt.Fprintf(w, "What are you searching?\n")
	for _, l := range hub.SearchScopeSyntax {
		fmt.Fprintf(w, "  %s\n", l)
	}
	fmt.Fprintf(w, "\nThese are search subjects, not capabilities. The capability vocabulary is unchanged and is:\n")
	for _, s := range hub.Vocabulary() {
		fmt.Fprintf(w, "  %s\n", string(s))
	}
	fmt.Fprintf(w, "\nSearching needs %q, and nothing else. Search added no fourth capability.\n", string(hub.ScopeRead))
}
