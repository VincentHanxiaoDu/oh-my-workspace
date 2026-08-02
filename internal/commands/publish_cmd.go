// Command `omw publish` — Issue #10: taking a draft out of the outbox and putting it on the hub,
// and saying which of the four states it is in at every moment in between.
//
// A NEW FILE, AND ONLY THIS FILE. Several Issues edit package commands at once; this one adds a
// file and touches nothing that already exists — including Issue #9's `omw outbox`, which owns the
// LOCAL gate (`review` against the person's own rules) and hands the transfer here.
//
// WHY IT IS A COMMAND OF ITS OWN AND NOT `omw outbox publish`. Issue #9's subcommand exists, says
// the transfer is Issue #10's and exits undetermined, which is honest. Editing it would put two
// branches in one file for no reason; and the state a person asks about here — drafted / in flight
// / published / refused — is a publication state, which is a different question from the state
// Issue #9's `omw outbox state` answers about the local review gate. They are separate surfaces
// because they are separate facts. Whether they should later become one screen is not something
// Issue #10 settles, and it is flagged in the pull request rather than decided here.
package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/publish"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "publish",
		Summary: "send a draft to the hub, and say which state it is in",
		Run:     runPublish,
	})
}

// The environment this command reads.
//
// PUB_ENV_IDENTITY AND PUB_ENV_SCOPES ARE ISSUE #19'S, BORROWED. §3.10 says a person signs in and
// their client authenticates as them; #19 owns sign-in, tokens and the scopes a token carries, and
// has not landed. Until it does, "who am I" and "what may I do" come from the environment, which is
// stated here and in the pull request rather than left for a reader to discover. There is no secret
// on the wire — see internal/publish/wire.go — because a placeholder secret is a security property
// this build does not have, written down as though it did.
const (
	pubEnvHub      = "OMW_HUB"
	pubEnvIdentity = "OMW_IDENTITY"
	pubEnvScopes   = "OMW_TOKEN_SCOPES"
)

// pubDaemonRunning asks Issue #2's own answer. There is ONE liveness answer in this product and
// this is not a second one: [daemon.Inspect] reads the store's lock and run record, starts nothing,
// and answers in three values.
var pubDaemonRunning = func(storeRoot string) tri.Value { return daemon.Inspect(storeRoot).Running }

func runPublish(env cli.Env) int {
	if len(env.Args) == 0 {
		publishUsage(env.Stdout)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help", "help":
		publishUsage(env.Stdout)
		return cli.Success
	case "state":
		return publishState(env, env.Args[1:])
	case "list":
		return publishList(env, env.Args[1:])
	case "note":
		return publishNote(env, env.Args[1:])
	default:
		fmt.Fprintf(env.Stderr, "omw publish: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw publish help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func publishUsage(w io.Writer) {
	fmt.Fprint(w, `omw publish — let a draft go, and always know where it is

usage: omw publish <subcommand>

  note <id> [title]   send one draft to the hub
  state <id>          which of the four states that note is in
  list                every note this client knows about, and where each one is

The four states are: drafted, in flight, published, refused. A note is in your outbox
or on the hub, never both and never neither. A hub that could not be reached is not a
refused note, and with no hub configured nothing is opened at all.
`)
}

// ---------------------------------------------------------------------------
// Opening the outbox, and saying what the daemon is doing while we do it
// ---------------------------------------------------------------------------

// publishOpen resolves the store and the outbox inside it, and reports the daemon's state on the
// way past.
//
// CRITERION 15: the daemon is REPORTED and started by nothing here. It goes to stderr because it is
// not the answer the person asked for, and it is said anyway because a person whose daemon is down
// should not have to infer it.
func publishOpen(env cli.Env, what string) (*publish.Ledger, *drafts.Outbox, int, bool) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw publish %s: where your store lives %s.\n", what, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return nil, nil, cli.ExitUndetermined, false
	}
	switch pubDaemonRunning(path) {
	case tri.Yes:
		fmt.Fprintf(env.Stderr, "daemon: running — this command did not start it.\n")
	case tri.No:
		fmt.Fprintf(env.Stderr, "daemon: not running — nothing has been started on your behalf.\n")
	default:
		fmt.Fprintf(env.Stderr, "daemon: whether it is running %s; nothing has been started on your behalf.\n", tri.Undetermined)
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw publish %s: the store at %s could not be opened: %v\n", what, path, err)
		fmt.Fprintf(env.Stderr, "  An unreadable store is not an empty one, and nothing was sent.\n")
		return nil, nil, cli.ExitUndetermined, false
	}
	o, err := drafts.InStore(s)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw publish %s: your outbox inside %s could not be opened: %v\n", what, s.Path(), err)
		return nil, nil, cli.ExitUndetermined, false
	}
	l, err := publish.InStore(s)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw publish %s: the record of your publications inside %s could not be opened: %v\n", what, s.Path(), err)
		fmt.Fprintf(env.Stderr, "  Without it, where your notes stand is not known, so nothing was sent.\n")
		return nil, nil, cli.ExitUndetermined, false
	}
	// FINISH WHAT A KILLED PROCESS LEFT. A note the ledger records as published may still have a
	// draft directory behind it; this is where that gets tidied, before anything is listed or sent.
	if finished, rerr := publish.Reconcile(l, o); rerr == nil {
		for _, id := range finished {
			fmt.Fprintf(env.Stderr, "reconciled: %s was published by an earlier run and its draft has now been removed from your outbox.\n", string(id))
		}
	}
	return l, o, cli.Success, true
}

// ---------------------------------------------------------------------------
// state, list
// ---------------------------------------------------------------------------

func publishState(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw publish state: name exactly one note.\n")
		return cli.ExitUsage
	}
	l, o, code, ok := publishOpen(env, "state")
	if !ok {
		return code
	}
	r := publish.StateOf(l, o, hub.NoteID(args[0]))
	io.WriteString(env.Stdout, r.Render())
	return publishStateCode(r)
}

// publishStateCode maps a state to an exit code. `refused` is a determined answer and exits
// ExitFailure; `in flight` is not an answer at all and exits ExitUndetermined. They never share.
func publishStateCode(r publish.Report) int {
	switch {
	case r.Exists == tri.No:
		return cli.ExitFailure
	case r.Known != tri.Yes:
		return cli.ExitUndetermined
	case r.State == publish.StateInFlight:
		return cli.ExitUndetermined
	case r.State == publish.StateRefused:
		return cli.ExitFailure
	default:
		return cli.Success
	}
}

func publishList(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw publish list: this takes no arguments.\n")
		return cli.ExitUsage
	}
	l, o, code, ok := publishOpen(env, "list")
	if !ok {
		return code
	}
	ids, err := publish.Known(l, o)
	if err != nil {
		fmt.Fprintf(env.Stdout, "notes: %s\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw publish list: your outbox could not be read: %v\n", err)
		fmt.Fprintf(env.Stderr, "  This is NOT an empty outbox.\n")
		return cli.ExitUndetermined
	}
	if len(ids) == 0 {
		fmt.Fprintf(env.Stdout, "notes: 0 — nothing is waiting to be published, and that is a determined answer\n")
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "notes: %d\n", len(ids))
	worst := cli.Success
	for _, id := range ids {
		r := publish.StateOf(l, o, id)
		for _, line := range strings.Split(strings.TrimRight(r.Render(), "\n"), "\n") {
			fmt.Fprintf(env.Stdout, "  %s\n", line)
		}
		if c := publishStateCode(r); c == cli.ExitUndetermined || (c != cli.Success && worst == cli.Success) {
			worst = c
		}
	}
	return worst
}

// ---------------------------------------------------------------------------
// note — the transfer
// ---------------------------------------------------------------------------

func publishNote(env cli.Env, args []string) int {
	if len(args) < 1 {
		fmt.Fprintf(env.Stderr, "omw publish note: name exactly one draft, and optionally a title.\n")
		return cli.ExitUsage
	}
	l, o, code, ok := publishOpen(env, "note")
	if !ok {
		return code
	}
	id := hub.NoteID(args[0])
	title := strings.Join(args[1:], " ")
	if title == "" {
		title = string(id)
	}

	res := publish.Transfer(l, o, id, publish.Config{
		HubAddr: strings.TrimSpace(env.Getenv(pubEnvHub)),
		Author:  hub.PersonID(strings.TrimSpace(env.Getenv(pubEnvIdentity))),
		Scopes:  publishScopes(env),
		Title:   title,
	})

	// THE STATE IS ALWAYS PRINTED, ON EVERY BRANCH. A person who ran publish and got an error still
	// needs to know where their note ended up, and "it said something went wrong" is not that.
	io.WriteString(env.Stdout, res.Report.Render())

	switch res.Attempt {
	case publish.AttemptPublished:
		fmt.Fprintf(env.Stdout, "outcome: published — one note on the hub, and it has left your outbox.\n")
		return cli.Success

	case publish.AttemptAlreadyPublished:
		// CRITERION 5 AND 14, SAID OUT LOUD. This is what a resolved interruption looks like.
		fmt.Fprintf(env.Stdout, "outcome: published — by an earlier attempt. No second copy was made.\n")
		if res.Detail != "" {
			fmt.Fprintf(env.Stdout, "  %s\n", res.Detail)
		}
		return cli.Success

	case publish.AttemptRefused:
		// A DETERMINED ANSWER FROM THE HUB. Non-zero, and never the undetermined code.
		fmt.Fprintf(env.Stdout, "outcome: refused by the hub — this is not 'the hub could not be reached'.\n")
		fmt.Fprintf(env.Stderr, "omw publish note: the hub refused this note (code: %s)\n", res.Code)
		fmt.Fprintf(env.Stderr, "  %s\n", res.Detail)
		fmt.Fprintf(env.Stderr, "  Your note is still in your outbox, unchanged.\n")
		return cli.ExitFailure

	case publish.AttemptUnreachable:
		// CRITERION 8 AND 9. Nothing was sent, so nothing was considered — and this is undetermined
		// territory, with its own exit code, never a refusal.
		fmt.Fprintf(env.Stdout, "outcome: %s — the hub could not be reached, so it never saw this note.\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw publish note: %v (code: %s)\n", hub.ErrHubUnreachable, res.Code)
		fmt.Fprintf(env.Stderr, "  %s\n", res.Detail)
		fmt.Fprintf(env.Stderr, "  This is NOT a refusal. Your note is still in your outbox and was never judged.\n")
		return cli.ExitUndetermined

	case publish.AttemptNoHub:
		// CRITERION 11 AND 12. Distinguishable from both a refusal and an unreachable hub, and
		// nothing was opened to find it out.
		fmt.Fprintf(env.Stdout, "outcome: no hub configured — nothing was opened and nothing was sent.\n")
		fmt.Fprintf(env.Stderr, "omw publish note: %v (code: %s)\n", hub.ErrNoHubConfigured, res.Code)
		fmt.Fprintf(env.Stderr, "  Set $%s to the hub's address. Your note is untouched and still drafted.\n", pubEnvHub)
		return cli.ExitFailure

	case publish.AttemptLocalFailure:
		fmt.Fprintf(env.Stdout, "outcome: nothing was sent.\n")
		fmt.Fprintf(env.Stderr, "omw publish note: %s (code: %s)\n", res.Detail, res.Code)
		if res.Report.Known != tri.Yes {
			return cli.ExitUndetermined
		}
		return cli.ExitFailure

	default:
		// CRITERION 13. Sent, outcome unknown. Not "not published", not refused, and not silence.
		fmt.Fprintf(env.Stdout, "outcome: %s — the request was sent and the hub's answer never arrived.\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw publish note: whether this note reached the hub %s (code: %s)\n", tri.Undetermined, res.Code)
		fmt.Fprintf(env.Stderr, "  %s\n", res.Detail)
		fmt.Fprintf(env.Stderr, "  Run this command again to resolve it. The retry carries the same attempt,\n")
		fmt.Fprintf(env.Stderr, "  so the hub cannot end up with two copies.\n")
		return cli.ExitUndetermined
	}
}

// publishScopes reads what the caller holds.
//
// IT DOES NOT DEFAULT TO `publish`. PRD §3.10: a token that can publish was asked for on purpose,
// and a client that quietly assumes the grant is a client that has made the asking meaningless. An
// unset variable means no scopes, the hub refuses, and the refusal names the missing scope.
func publishScopes(env cli.Env) []hub.Scope {
	raw := strings.TrimSpace(env.Getenv(pubEnvScopes))
	if raw == "" {
		return nil
	}
	var out []hub.Scope
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, hub.Scope(p))
		}
	}
	return out
}
