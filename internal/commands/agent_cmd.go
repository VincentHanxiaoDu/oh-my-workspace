// The `omw agent` command: what a person points their own AI at (PRD §2.1, §3.12, §4.2, §4.3,
// §4.4, §4.6; Issue #16).
//
// WHAT THIS FILE IS CAREFUL ABOUT, IN ORDER OF HOW EASY IT WOULD BE TO GET WRONG:
//
//  1. IT DECIDES NOTHING. Every answer here came back over the control API from `internal/daemon`,
//     which asked `internal/agentapi`, which asked `internal/hub`. This file renders. A visibility
//     check, a scope check or a "helpful" filter added here would be the second implementation
//     Issue #12's package comment warns about — one that agrees today.
//  2. THE FOUR WAYS THIS CAN FAIL ARE FOUR SENTENCES WITH FOUR CODES AND THREE EXIT CODES: the
//     daemon is not running (§4.2, criterion 10); whether it is running could not be determined
//     (§4.3); the control API did not open because owner-only permissions could not be confirmed,
//     so the agent API does not serve (§4.6, criterion 12); and the request itself was refused for
//     scope (§4.5, criterion 6). Criterion 12 asks for exactly this and names the two it must not
//     be confused with.
//  3. NOTHING HERE STARTS THE DAEMON, and nothing here derives a socket path. Liveness comes from
//     daemonLiveness — the one answer, Issue #41 — and the socket is named only inside
//     `internal/daemon`, which is the package the structural test permits to name it.
//  4. NOTHING HERE REACHES A HUB. The daemon does that, if one is configured. With no hub
//     configured the local half answers in full (§4.4, criterion 17) and the hub half says
//     precisely that there is no hub rather than returning empty.
//
// (Detached from the package clause on purpose: doc.go carries this package's doc comment, and
// several Issues add files here concurrently.)

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/agentapi"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "agent",
		Summary: "what your own AI reads your tickets, drafts and the hub through",
		Run:     runAgent,
	})
}

func agentUsage(w io.Writer) {
	var b strings.Builder
	b.WriteString(`omw agent — your own AI, reading your own material

usage: omw agent <operation> [arguments] [--grant <id>]

`)
	for _, op := range agentapi.Operations() {
		need, needs := agentapi.ScopeFor(op)
		scope := "no scope — you, on your own machine"
		if needs {
			scope = string(need)
		}
		fmt.Fprintf(&b, "  %-12s %-32s (%s)\n", string(op), agentOpSummary(op), scope)
	}
	b.WriteString(`
  schema       the agent API's own description of itself

flags:
  --grant <id>        the authority to act under; ask for one with 'omw agent grant'
  --scope <a,b>       the scopes this request claims, checked before anything is read
  --visibility <v>    who may see a published note
  --json              the control API's form of the same answer

An agent cannot read what its person cannot (PRD §3.12). The scope vocabulary is `)
	b.WriteString(scopeVocabularyLine())
	b.WriteString(`,
and there is no fourth. Everything here goes over the local control API; nothing goes over a
network, and no operation starts the daemon for you.
`)
	fmt.Fprint(w, b.String())
}

func agentOpSummary(op agentapi.Op) string {
	switch op {
	case agentapi.OpTickets:
		return "your inbox"
	case agentapi.OpDrafts:
		return "your outbox, all unpublished"
	case agentapi.OpHub:
		return "hub notes you may read"
	case agentapi.OpNote:
		return "<id> — one hub note"
	case agentapi.OpDraftWrite:
		return "<id> <text> — revise a draft"
	case agentapi.OpPublish:
		return "<title> <body> — publish"
	case agentapi.OpModel:
		return "whether a model is configured"
	case agentapi.OpGrant:
		return "issue an agent a grant"
	case agentapi.OpRevoke:
		return "revoke one"
	}
	return ""
}

func scopeVocabularyLine() string {
	var parts []string
	for _, s := range hub.Vocabulary() {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, " / ")
}

// agentFlags are the flags every operation takes.
type agentFlags struct {
	grant      string
	scopes     string
	visibility string
	json       bool
	rest       []string
}

func parseAgentFlags(args []string, stderr io.Writer) (agentFlags, bool) {
	var f agentFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--grant" && i+1 < len(args):
			i++
			f.grant = args[i]
		case strings.HasPrefix(a, "--grant="):
			f.grant = strings.TrimPrefix(a, "--grant=")
		case a == "--scope" && i+1 < len(args):
			i++
			f.scopes = args[i]
		case strings.HasPrefix(a, "--scope="):
			f.scopes = strings.TrimPrefix(a, "--scope=")
		case a == "--visibility" && i+1 < len(args):
			i++
			f.visibility = args[i]
		case strings.HasPrefix(a, "--visibility="):
			f.visibility = strings.TrimPrefix(a, "--visibility=")
		case a == "--json":
			f.json = true
		case strings.HasPrefix(a, "--"):
			fmt.Fprintf(stderr, "omw agent: unknown flag %q\n", a)
			return f, false
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f, true
}

func runAgent(env cli.Env) int {
	if len(env.Args) == 0 {
		agentUsage(env.Stderr)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help", "help":
		agentUsage(env.Stdout)
		return cli.Success
	case "schema":
		return agentSchema(env)
	}

	op := agentapi.Op(env.Args[0])
	if !agentapi.KnownOp(op) {
		// NAMED, AND THE NAME ECHOED BACK. `read-everything` is the specific word worth being
		// unambiguous about: it is not unimplemented, it does not exist, and no scope grants it.
		fmt.Fprintf(env.Stderr, "omw agent: there is no %q operation on the agent API.\n", env.Args[0])
		agentUsage(env.Stderr)
		return cli.ExitUsage
	}
	f, ok := parseAgentFlags(env.Args[1:], env.Stderr)
	if !ok {
		return cli.ExitUsage
	}

	req := agentapi.Request{Op: op, Grant: hub.GrantID(f.grant), Visibility: f.visibility}
	if f.scopes != "" {
		scopes, err := agentapi.ParseScopes(f.scopes)
		if err != nil {
			// REFUSED, NOT NARROWED. A scope outside the vocabulary is not quietly dropped from the
			// list; the request does not happen (§4.5).
			fmt.Fprintf(env.Stderr, "omw agent %s: %v (code: %s)\n", op, err, hub.Code(err))
			return cli.ExitUsage
		}
		req.Scopes = scopes
	}
	switch op {
	case agentapi.OpNote:
		if len(f.rest) != 1 {
			fmt.Fprintf(env.Stderr, "omw agent note: name exactly one note.\n")
			return cli.ExitUsage
		}
		req.NoteID = f.rest[0]
	case agentapi.OpDraftWrite:
		if len(f.rest) < 2 {
			fmt.Fprintf(env.Stderr, "omw agent draft.write: name a draft and give its new text.\n")
			return cli.ExitUsage
		}
		req.NoteID, req.Body = f.rest[0], strings.Join(f.rest[1:], " ")
	case agentapi.OpPublish:
		if len(f.rest) < 2 {
			fmt.Fprintf(env.Stderr, "omw agent publish: give a title and a body.\n")
			return cli.ExitUsage
		}
		req.Title, req.Body = f.rest[0], strings.Join(f.rest[1:], " ")
	}

	resp, code, ok := askDaemon(env, op, req)
	if !ok {
		return code
	}
	return renderAgentResponse(env, resp, f.json)
}

// askDaemon applies §4.2's order and produces criterion 10 and criterion 12's distinguishable
// failures before anything is dialled.
//
// THE LIVENESS ANSWER IS NOT THIS FILE'S TO WORK OUT (Issue #41). daemonLiveness is the same call
// `omw daemon status` renders from, so this surface cannot report a running daemon as absent, and
// an undetermined liveness is not written down as a stopped one.
func askDaemon(env cli.Env, op agentapi.Op, req agentapi.Request) (agentapi.Response, int, bool) {
	if live, why := daemonLiveness(env); live != tri.Yes {
		// CRITERION 10: named as not running, and NOT started. reportDaemonNotLive is the one
		// place the two negative answers are worded, and they share neither wording nor exit code.
		return agentapi.Response{}, reportDaemonNotLive(env, "omw agent "+string(op), live, why), false
	}
	root, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw agent %s: %v\n", op, err)
		fmt.Fprintf(env.Stderr, "  where the store lives could not be determined, so nothing was asked.\n")
		return agentapi.Response{}, cli.ExitUndetermined, false
	}
	resp, err := daemon.Ask(root, req)
	if err != nil {
		return agentapi.Response{}, reportAskFailure(env, op, err), false
	}
	return resp, cli.Success, true
}

// reportAskFailure words the failures that happened before the agent API could answer at all.
func reportAskFailure(env cli.Env, op agentapi.Op, err error) int {
	code := hub.Code(err)
	switch code {
	case agentapi.ErrControlAPINotOpen.Code:
		// CRITERION 12, AND IT IS NEITHER OF THE OTHER TWO. The daemon is running; its control API
		// declined to open; the agent API therefore does not serve. Saying "the daemon is not
		// running" here would send the person to start something that is already started.
		fmt.Fprintf(env.Stderr, "omw agent %s: %v (code: %s)\n", op, err, code)
		fmt.Fprintf(env.Stderr, "  the daemon IS running. This is not that, and it is not a refusal for scope:\n")
		fmt.Fprintf(env.Stderr, "  the control API is the only way in (PRD §3.12, §4.6) and it did not open.\n")
		fmt.Fprintf(env.Stderr, "  'omw daemon status' reports the same control state for this store.\n")
		return cli.ExitFailure
	case hub.ErrDaemonNotRunning.Code:
		fmt.Fprintf(env.Stderr, "omw agent %s: %v (code: %s)\n", op, err, code)
		return cli.ExitFailure
	default:
		fmt.Fprintf(env.Stderr, "omw agent %s: %v (code: %s)\n", op, err, code)
		fmt.Fprintf(env.Stderr, "  nothing was established; this is not an empty answer and not a refusal.\n")
		return cli.ExitUndetermined
	}
}

// renderAgentResponse prints the answer and returns the outcome's exit code.
//
// THE EXIT CODE COMES FROM THE OUTCOME AND NOTHING ELSE, so a refusal and an undetermined answer
// cannot end up sharing one by two branches drifting apart. cli.ExitUndetermined's value is
// asserted equal to the outcome mapping's by TestTheAgentOutcomeExitCodesAreTheProductsExitCodes.
func renderAgentResponse(env cli.Env, resp agentapi.Response, asJSON bool) int {
	if asJSON {
		body, err := agentapi.MarshalResponse(resp)
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw agent: %v\n", err)
			return cli.ExitFailure
		}
		var pretty any
		_ = json.Unmarshal(body, &pretty)
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Fprintf(env.Stdout, "%s\n", out)
		return resp.Outcome.Exit()
	}

	fmt.Fprintf(env.Stdout, "operation:   %s\n", resp.Op)
	fmt.Fprintf(env.Stdout, "outcome:     %s\n", resp.Outcome)
	if resp.Person != "" {
		fmt.Fprintf(env.Stdout, "as:          %s\n", resp.Person)
	} else {
		// AN UNIDENTIFIED READER IS SAID, not left blank. hub.CanRead answers undetermined for one,
		// and a person reading an empty hub list needs to know that is why.
		//
		// THIS LINE USED TO BE PRINTED ON EVERY REFUSAL, FALSELY. Refusals did not carry Person at
		// all, so a machine with OMW_PERSON=priya was told "no person is configured" the moment
		// anything was refused — and its output was byte-identical to the same refusal on a machine
		// with no person set, so the two could not be told apart. The response now carries the
		// person whatever the outcome; this branch means what it says again.
		fmt.Fprintf(env.Stdout, "as:          %s — no person is configured, so who may read what could not be worked out\n",
			tri.Undetermined)
	}
	// TWO QUESTIONS, TWO LINES. "Is a hub configured" is a fact about this machine and is answerable
	// even for a request that was refused before anything was consulted; "was one contacted" is
	// about this request. Reporting them as one field is what made a refusal on a hub-less machine
	// claim the hub state was unknown.
	fmt.Fprintf(env.Stdout, "hub:         %s\n", resp.HubConfigured.Render("configured", "none configured"))
	fmt.Fprintf(env.Stdout, "             %s\n", resp.HubContacted.Render("contacted, and it answered", "not contacted"))
	if resp.Code != "" {
		fmt.Fprintf(env.Stdout, "code:        %s\n", resp.Code)
	}
	if resp.Message != "" {
		fmt.Fprintf(env.Stdout, "             %s\n", resp.Message)
	}

	switch {
	case resp.Op == agentapi.OpTickets:
		fmt.Fprintf(env.Stdout, "tickets:     %d\n", len(resp.Tickets))
		for _, t := range resp.Tickets {
			fmt.Fprintf(env.Stdout, "  %s  %s\n", t.ID, t.Title.Render())
		}
	case resp.Op == agentapi.OpDrafts || resp.Op == agentapi.OpDraftWrite:
		fmt.Fprintf(env.Stdout, "drafts:      %d\n", len(resp.Drafts))
		for _, d := range resp.Drafts {
			// STATE ON EVERY LINE (criterion 2). A draft that did not say it was unpublished would
			// be a draft somebody's summary calls published.
			fmt.Fprintf(env.Stdout, "  %s  %s  (%d revision(s))\n", d.ID, d.State, d.Revisions)
		}
	case resp.Op == agentapi.OpHub:
		fmt.Fprintf(env.Stdout, "notes:       %d\n", len(resp.Notes))
		for _, n := range resp.Notes {
			fmt.Fprintf(env.Stdout, "  %s  %s  [%s]\n", n.ID, n.Title, n.Visibility)
		}
		if resp.UndeterminedNotes != nil && *resp.UndeterminedNotes > 0 {
			// NOT DROPPED AND NOT COUNTED IN. "No results" must not absorb "I could not check".
			fmt.Fprintf(env.Stdout, "could not be determined for %d note(s): they are neither listed nor ruled out\n",
				*resp.UndeterminedNotes)
		}
	}
	if resp.Note != nil {
		fmt.Fprintf(env.Stdout, "note:        %s  %s  [%s]\n", resp.Note.ID, resp.Note.Title, resp.Note.Visibility)
		if resp.Note.Body != "" {
			fmt.Fprintf(env.Stdout, "%s\n", resp.Note.Body)
		}
	}
	if resp.Model != nil {
		// model.View.Render IS THE ONE RENDERING, and this surface adds nothing to it. Issue #18's
		// own comment records why: `omw model show` grew two extra lines that `omw daemon status`
		// did not have, and the agreement test between the CLI and the control API went red on all
		// four configurations. Everything a person may be told about their model configuration is
		// in the View, so every surface that shows one shows it the same way.
		fmt.Fprintf(env.Stdout, "%s\n", resp.Model.Render())
		// PRD §3.13, SAID ON THE SURFACE THAT COULD HAVE LEAKED IT. This is about the AGENT API —
		// which operations exist — rather than about the configuration, so it is not something the
		// View could carry.
		fmt.Fprintf(env.Stdout, "credential:  not readable through the agent API, by any operation\n")
	}
	if resp.Grant != nil {
		fmt.Fprintf(env.Stdout, "grant:       %s\n", resp.Grant.ID)
		fmt.Fprintf(env.Stdout, "scopes:      %s\n", strings.Join(resp.Grant.Scopes, ", "))
		fmt.Fprintf(env.Stdout, "live:        %t\n", resp.Grant.Live)
	}
	return resp.Outcome.Exit()
}

// agentSchema prints the agent API's own description of itself. It needs no daemon: it is a
// property of the build, not of a running process, and a person wiring their AI up wants to read it
// before anything is started.
func agentSchema(env cli.Env) int {
	s, err := hub.AgentAPISchemaJSON()
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw agent schema: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprint(env.Stdout, s)
	fmt.Fprintf(env.Stdout, "\noperations: ")
	var names []string
	for _, op := range agentapi.Operations() {
		names = append(names, string(op))
	}
	fmt.Fprintf(env.Stdout, "%s\n", strings.Join(names, ", "))
	fmt.Fprintf(env.Stdout, "scopes: %s — there is no fourth, and the hub operator's read-everything is a\n", scopeVocabularyLine())
	fmt.Fprintf(env.Stdout, "deployment fact rather than a scope (PRD §2.4, §4.5).\n")
	return cli.Success
}
