// Command `omw auth` — Issue #19: signing in to the hub on purpose, and seeing and ending
// everything signed in as you (PRD §3.10, §4.2, §4.3, §4.5).
//
// This is the ONLY file this Issue adds to package commands, so two Issues adding commands never
// touch the same line.
//
// THE ORDER OF THE CHECKS IN EVERY SUBCOMMAND IS THE FEATURE, and it is the same order every time:
//
//  1. Is a hub configured? A determined fact about this machine, needing nothing (§4.4, criteria 27
//     and 28). Answered FIRST, so no later step can reach for a network with no hub — criterion 26
//     is then a property of control flow rather than of every function downstream behaving.
//  2. Is the daemon running? Said, never fixed by starting one (§4.2, criterion 25). Three-valued,
//     through the product's one liveness answer (Issue #41).
//  3. Did the control API open? Where owner-only permissions cannot be confirmed it does not, and
//     a command depending on it says so instead of proceeding (§4.6, criterion 29).
//
// Each of those has its own code and its own exit status, so a script tells them apart without
// reading prose — and none of them is "not signed in", which is a fourth, separate fact.
package commands

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/auth"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "auth",
		Summary: "sign in to the hub, and see and end what is signed in as you",
		Run:     runAuth,
	})
}

// authHub is how this command reaches a hub.
//
// WHAT IS REAL AND WHAT IS FAKED, STATED WHERE SOMEBODY WILL READ IT. The default is
// [auth.Unreachable], and that is not a placeholder chosen for convenience: NO CLIENT-TO-HUB
// TRANSPORT EXISTS IN THIS BUILD, so a configured hub genuinely cannot be reached, and the honest
// answer is UNDETERMINED. `omw visibility` says the same thing about its own hub access.
//
// Tests substitute a real [auth.Authority] through this seam. That replaces the WIRE. Device
// codes, token minting, expiry, replay, revocation and the scope decision are the product's own
// code running for real on the other side of it — there is no path in this package that fabricates
// a successful sign-in.
var authHub = func(env cli.Env) auth.Hub { return auth.Unreachable{} }

// authPollInterval is how often `omw auth sign-in` asks whether the person has finished in their
// browser. A var so a test can drive the whole flow in milliseconds; a device-code test that waits
// real seconds is a test somebody eventually deletes.
var authPollInterval = 500 * time.Millisecond

func runAuth(env cli.Env) int {
	if len(env.Args) == 0 {
		authUsage(env.Stdout)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "status":
		return authStatus(env)
	case "scopes":
		return authScopes(env)
	case "sign-in":
		return authSignIn(env, env.Args[1:])
	case "sign-out":
		return authSignOut(env)
	case "sessions":
		return authSessions(env)
	case "revoke":
		return authRevoke(env, env.Args[1:])
	case "-h", "--help", "help":
		authUsage(env.Stdout)
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw auth: unknown subcommand %q\n", env.Args[0])
		fmt.Fprintf(env.Stderr, "run 'omw auth help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func authUsage(w io.Writer) {
	fmt.Fprint(w, `omw auth — signing in to the hub, on purpose

usage: omw auth <subcommand>

  status                     signed in, not signed in, or could not be determined
  scopes                     the one scope vocabulary: read, write, publish
  sign-in [--scope …] [--label …]
                             sign in by device code. THE ONLY WAY A CREDENTIAL IS EVER CREATED
  sign-out                   forget this machine's credential (does NOT end the hub session)
  sessions                   what has been signed in as you
  revoke <token-id>          end one of them, without ending the others

Nothing here signs you in on your behalf. Only 'sign-in', run by you, produces a credential.
`)
}

// authScopes prints the vocabulary. Entirely local: no hub, no daemon, no connection.
//
// CRITERIA 8, 30 AND 32. It prints `internal/hub`'s list — it does not have one of its own, which
// is how the CLI, the agent API and the hub cannot drift — and it states the operator fact where a
// person meets the question, as a DEPLOYMENT FACT and not as a grant. There is no fourth name here
// and there is no way to ask for one.
func authScopes(env cli.Env) int {
	for _, s := range hub.Vocabulary() {
		fmt.Fprintf(env.Stdout, "%s\n", string(s))
	}
	fmt.Fprintf(env.Stdout, "\n%d scopes, and there is no fourth.\n", len(hub.Vocabulary()))
	fmt.Fprint(env.Stdout, `read     everyday use. Tickets, drafts, and the hub content you may see.
write    change local drafts and tickets. It does not reach the hub.
publish  send a note to the hub. Its own grant, because a token that can publish
         must have been asked for on purpose (PRD §3.10).

`)
	fmt.Fprintf(env.Stdout, "%s\n", hub.RestrictionStatement)
	fmt.Fprint(env.Stdout, `Whoever operates the hub can read everything published to it, including notes
restricted to a group or to yourself (PRD §2.4). That is a DEPLOYMENT FACT. It is
not a scope: there is no name you can request, hold or delegate that grants it, and
a request for one is refused as an unknown scope.
`)
	return cli.Success
}

// authStatus answers the sign-in question, and answering IS its job — it does not refuse when the
// daemon is absent, it reports.
//
// CRITERIA 1, 3, 22, 25, 27, 28, 29. It reaches [auth.Observe], which is the same function the
// daemon's control API answers from (criterion 23), so the two cannot be two answers. The daemon
// and control-API lines are printed rather than made fatal: criterion 25 asks that an `auth`
// command SAY the daemon is not running, and a status command whose job is to say what it knows
// would be a strange place to refuse to say anything.
//
// IT NEVER OPENS A SIGN-IN. There is no branch here that mints, saves or requests a credential.
func authStatus(env cli.Env) int {
	root, rootErr := store.Resolve(env.Getenv)
	configured := auth.HubConfigured(env.Getenv)

	var st auth.State
	if configured && rootErr != nil {
		// Not knowing which store this is about establishes nothing about sign-in. Undetermined,
		// never "not signed in".
		st = auth.State{
			Signed: tri.Undetermined,
			Code:   auth.ErrSignInUndetermined.Code,
			Text:   "sign-in state " + tri.Undetermined.String(),
			Detail: rootErr.Error(),
		}
	} else {
		st = auth.Observe(root, configured, authHub(env))
	}

	// NEVER EMPTY OUTPUT (criterion 22's fourth case, and criterion 27's "never half-works"). Every
	// branch of auth.Observe fills Text, and this line always prints.
	fmt.Fprintf(env.Stdout, "auth: %s (code: %s)\n", st.Text, st.Code)
	if st.Detail != "" {
		fmt.Fprintf(env.Stdout, "  %s\n", st.Detail)
	}
	if st.TokenID != "" {
		fmt.Fprintf(env.Stdout, "  token: %s\n", st.TokenID)
		fmt.Fprintf(env.Stdout, "  scope: %s\n", st.Scopes.Render())
	}

	// SAID, NEVER ACTED ON (criterion 25). This is a report about the daemon, printed alongside the
	// answer; nothing here starts anything.
	if configured {
		live, why := daemonLiveness(env)
		fmt.Fprintf(env.Stdout, "  daemon: %s\n", live.Render("running", "not running, and omw does not start it for you"))
		if live != tri.Yes && why != "" {
			fmt.Fprintf(env.Stdout, "    %s\n", why)
		}
		if live == tri.Yes && rootErr == nil {
			// CRITERION 29, reported rather than assumed: where owner-only permissions could not be
			// confirmed the control API is not open, and that is said out loud here.
			rep := daemon.Inspect(root)
			fmt.Fprintf(env.Stdout, "  control API: %s\n", rep.Control.Render("open", "not open"))
			if rep.Control != tri.Yes && rep.ControlDetail != "" {
				fmt.Fprintf(env.Stdout, "    %s\n", rep.ControlDetail)
			}
		}
	}

	if st.Signed == tri.Undetermined {
		// NOT A "NO", AND NOT A SUCCESS EITHER. Its own exit code, so a script never reads "I could
		// not check" as "you are not signed in".
		return cli.ExitUndetermined
	}
	return cli.Success
}

// authNeedsHub is step 1 and 2 and 3 of the standing order, for the subcommands that genuinely
// need hub authority. It returns the store root and 0, or an exit code to return.
//
// The bool is the "keep going" answer; the int is only meaningful when it is false. A
// (root, code, ok) shape rather than an error because each refusal has its own already-decided
// exit status, and turning them back into an error to re-derive one is where two of them collapse.
func authNeedsHub(env cli.Env, what string) (root string, code int, ok bool) {
	if !auth.HubConfigured(env.Getenv) {
		// CRITERION 27 AND 28. It names the ABSENT HUB CONFIGURATION as the missing thing, and it
		// is a different sentence and a different code from "the hub could not be reached" and from
		// "you are signed out".
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		fmt.Fprintf(env.Stderr, "  what is missing is the hub configuration itself: %s is not set.\n", auth.HubEnv)
		fmt.Fprintf(env.Stderr, "  this is not 'the hub could not be reached' and it is not 'you are signed out';\n")
		fmt.Fprintf(env.Stderr, "  there is no hub here to be signed in to. Nothing was contacted.\n")
		return "", cli.ExitFailure, false
	}
	live, why := daemonLiveness(env)
	if live != tri.Yes {
		// CRITERION 25, through the product's ONE liveness answer and its one reporter. Nothing is
		// started, and "could not be determined" does not exit as "not running".
		return "", reportDaemonNotLive(env, what, live, why), false
	}
	root, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s: which store this is about %s (code: %s)\n", what, tri.Undetermined, auth.ErrSignInUndetermined.Code)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return "", cli.ExitUndetermined, false
	}
	// CRITERION 29. Distinguishable from success and from "the daemon is not running": its own
	// code, and — for the undetermined case — its own exit status too.
	rep := daemon.Inspect(root)
	if rep.Control != tri.Yes {
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, auth.ErrControlAPINotOpen, auth.ErrControlAPINotOpen.Code)
		if rep.ControlDetail != "" {
			fmt.Fprintf(env.Stderr, "  %s\n", rep.ControlDetail)
		}
		fmt.Fprintf(env.Stderr, "  the daemon IS running; this is not that. Nothing was signed in or ended.\n")
		if rep.Control == tri.Undetermined {
			return "", cli.ExitUndetermined, false
		}
		return "", cli.ExitFailure, false
	}
	return root, 0, true
}

// authSignIn is the device-code flow, and it is the ONLY way a credential comes into existence
// (criteria 2, 4, 5, 6, 7, 13, 15, 16).
//
// IT IS A DEVICE CODE FLOW, WHICH IS THE OWNER'S RULING AND ALSO WHAT §3.10 NEEDS: it prints a code
// and waits. It opens no browser, binds no port and needs no graphical session, so it works over
// SSH on a headless box — criterion 4 — and that is a property of this function containing no
// listener and no exec of a browser, not of a flag somebody remembered to pass.
//
// NOTHING IS WRITTEN UNTIL A TOKEN EXISTS. auth.Save is called on exactly one line, after Redeem
// has returned a token. Criterion 5 is that line's position.
func authSignIn(env cli.Env, args []string) int {
	const what = "omw auth sign-in"

	scopes := []hub.Scope{hub.ScopeRead}
	label := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--scope" && i+1 < len(args):
			i++
			scopes = parseScopeList(args[i])
		case strings.HasPrefix(args[i], "--scope="):
			scopes = parseScopeList(strings.TrimPrefix(args[i], "--scope="))
		case args[i] == "--label" && i+1 < len(args):
			i++
			label = args[i]
		case strings.HasPrefix(args[i], "--label="):
			label = strings.TrimPrefix(args[i], "--label=")
		default:
			fmt.Fprintf(env.Stderr, "%s: unexpected argument %q\n", what, args[i])
			return cli.ExitUsage
		}
	}

	root, code, ok := authNeedsHub(env, what)
	if !ok {
		return code
	}
	h := authHub(env)

	// REFUSED AT THE MOMENT IT IS ASKED (criterion 15, §4.5). An unknown scope name never gets as
	// far as a printed code; a scope wider than the person holds is refused when they approve, and
	// in both cases no token exists afterwards.
	da, err := h.StartSignIn(auth.SignInRequest{Scopes: scopes, Label: label})
	if err != nil {
		return authReportRefusal(env, what, err)
	}

	fmt.Fprintf(env.Stdout, "To sign in, open this page and enter the code:\n\n")
	if da.VerificationURI != "" {
		fmt.Fprintf(env.Stdout, "  %s\n", da.VerificationURI)
	}
	fmt.Fprintf(env.Stdout, "  code: %s\n\n", da.UserCode)
	fmt.Fprintf(env.Stdout, "asking for scope: %s\n", auth.RecordedScopes(scopes).Render())
	fmt.Fprintf(env.Stdout, "the code expires at %s. Nothing is signed in until you finish in the browser;\n",
		da.ExpiresAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(env.Stdout, "no credential exists on this machine yet.\n")

	for {
		iss, err := h.Redeem(da)
		if err == nil {
			return authCompleteSignIn(env, what, root, iss)
		}
		if hub.Code(err) == auth.ErrSignInPending.Code {
			time.Sleep(authPollInterval)
			continue
		}
		return authReportRefusal(env, what, err)
	}
}

// authCompleteSignIn writes the credential. THE ONLY CALLER OF auth.Save IN THE PRODUCT, and
// TestOnlyTheSignInCommandCanCreateACredential asserts that over the tree.
func authCompleteSignIn(env cli.Env, what, root string, iss auth.Issued) int {
	cred := auth.Credential{
		TokenID:   iss.ID,
		Person:    iss.Person,
		Scopes:    iss.Scopes,
		ExpiresAt: iss.ExpiresAt,
		Secret:    iss.Secret,
	}
	if err := auth.Save(root, cred); err != nil {
		fmt.Fprintf(env.Stderr, "%s: the sign-in succeeded and the credential could not be stored: %v\n", what, err)
		fmt.Fprintf(env.Stderr, "  a session now exists at the hub that this machine cannot use.\n")
		fmt.Fprintf(env.Stderr, "  'omw auth sessions' from a signed-in machine will show it, and 'omw auth revoke' will end it.\n")
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "\nsigned in as %s\n", iss.Person)
	fmt.Fprintf(env.Stdout, "  token: %s\n", iss.ID)
	// CRITERION 16: what is printed is what the token HAS, straight off the issued token. Not the
	// request echoed back — those are the two values the criterion forbids from differing, and
	// echoing the request would make them differ silently and undetectably.
	fmt.Fprintf(env.Stdout, "  scope: %s\n", auth.RecordedScopes(iss.Scopes).Render())
	fmt.Fprintf(env.Stdout, "  expires: %s\n", iss.ExpiresAt.UTC().Format(time.RFC3339))
	return cli.Success
}

// authReportRefusal words a refusal so that each is distinguishable from the others by its code,
// and gives back the exit status for it.
//
// AN UNREACHABLE HUB IS NOT A REFUSAL, and this is the function where that distinction is spent
// (criteria 6, 7, 11, 20). Everything the hub DECIDED exits ExitFailure; a hub that did not answer
// exits ExitUndetermined. A caller reading only the exit status can therefore tell "the hub said
// no" from "I never heard back", which is the whole of §3.11's separation carried into auth.
func authReportRefusal(env cli.Env, what string, err error) int {
	code := hub.Code(err)
	fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, err, code)
	switch code {
	case hub.ErrHubUnreachable.Code, hub.ErrUndetermined.Code, auth.ErrSignInUndetermined.Code, auth.ErrCredentialUnreadable.Code:
		fmt.Fprintf(env.Stderr, "  the hub did not answer. This is NOT a refusal: nothing was decided, and nothing changed.\n")
		return cli.ExitUndetermined
	}
	return cli.ExitFailure
}

// parseScopeList splits a comma-separated scope list.
//
// IT VALIDATES NOTHING. A name this build has never heard of is passed through verbatim to
// [hub.EvaluateGrantRequest], which refuses it as an unknown scope. A CLI-side allow-list would be
// a second vocabulary — the exact thing criterion 9 forbids — and it would be the one that goes
// stale.
func parseScopeList(s string) []hub.Scope {
	var out []hub.Scope
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, hub.Scope(part))
		}
	}
	return out
}

// authSignOut forgets this machine's credential, and SAYS WHAT IT DID NOT DO.
//
// Forgetting a credential locally does not end the session at the hub. A person who signs out and
// believes the token is dead has been misled by a command that was too pleased with itself, so
// this one names `omw auth revoke` explicitly.
func authSignOut(env cli.Env) int {
	const what = "omw auth sign-out"
	root, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s: which store this is about %s\n  %v\n", what, tri.Undetermined, err)
		return cli.ExitUndetermined
	}
	if err := auth.Forget(root); err != nil {
		fmt.Fprintf(env.Stderr, "%s: the credential could not be removed: %v\n", what, err)
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "this machine no longer holds a credential.\n")
	fmt.Fprintf(env.Stdout, "the hub session itself is UNCHANGED and still works for anything else holding the token.\n")
	fmt.Fprintf(env.Stdout, "to end it, sign in again and run 'omw auth revoke <token-id>'.\n")
	return cli.Success
}

// authSessions lists what has been signed in as this person (criteria 18, 19, 21, 24).
func authSessions(env cli.Env) int {
	const what = "omw auth sessions"
	root, code, ok := authNeedsHub(env, what)
	if !ok {
		return code
	}
	cred, err := auth.Load(root)
	if err != nil {
		return authReportNotSignedIn(env, what, err)
	}
	views, err := authHub(env).Sessions(cred.Person)
	if err != nil {
		return authReportRefusal(env, what, err)
	}
	if len(views) == 0 {
		// SAID, NOT BLANK. An empty listing while holding a credential is a strange enough state
		// that printing nothing would read as a broken command.
		fmt.Fprintf(env.Stdout, "the hub lists nothing signed in as %s.\n", cred.Person)
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "signed in as %s:\n\n", cred.Person)
	for _, v := range views {
		fmt.Fprint(env.Stdout, v.Render())
		if v.ID == cred.TokenID {
			fmt.Fprintf(env.Stdout, "  (this machine)\n")
		}
		fmt.Fprintln(env.Stdout)
	}
	fmt.Fprintf(env.Stdout, "end any one of them with 'omw auth revoke <token-id>'. The others keep working.\n")
	return cli.Success
}

// authRevoke ends one session (criteria 20, 21).
func authRevoke(env cli.Env, args []string) int {
	const what = "omw auth revoke"
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "%s: name exactly one token id. 'omw auth sessions' lists them.\n", what)
		return cli.ExitUsage
	}
	root, code, ok := authNeedsHub(env, what)
	if !ok {
		return code
	}
	cred, err := auth.Load(root)
	if err != nil {
		return authReportNotSignedIn(env, what, err)
	}
	id := auth.TokenID(args[0])
	if err := authHub(env).Revoke(cred.Person, id); err != nil {
		return authReportRefusal(env, what, err)
	}
	fmt.Fprintf(env.Stdout, "ended: %s\n", id)
	fmt.Fprintf(env.Stdout, "its next use will be refused by the hub. Every other session is untouched.\n")
	if id == cred.TokenID {
		fmt.Fprintf(env.Stdout, "that was this machine's own session; this machine is now signed out at the hub.\n")
	}
	return cli.Success
}

// authReportNotSignedIn distinguishes "no credential here" from "a credential is here and could
// not be read". The first is a determined negative; the second establishes nothing.
func authReportNotSignedIn(env cli.Env, what string, err error) int {
	if hub.Code(err) == auth.ErrCredentialUnreadable.Code {
		fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, err, auth.ErrCredentialUnreadable.Code)
		fmt.Fprintf(env.Stderr, "  this is not 'not signed in': something is there and it could not be read.\n")
		return cli.ExitUndetermined
	}
	fmt.Fprintf(env.Stderr, "%s: %v (code: %s)\n", what, auth.ErrNotSignedIn, auth.ErrNotSignedIn.Code)
	fmt.Fprintf(env.Stderr, "  no sign-in was started for you, and none will be: run 'omw auth sign-in' yourself.\n")
	return cli.ExitFailure
}
