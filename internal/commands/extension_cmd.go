// Command `omw ext` — Issue #21: adding a channel and choosing a model through ONE mechanism.
//
// # THE SENTENCE THIS COMMAND EXISTS FOR
//
// PRD §2.5: "Channel adapters and model providers are the same mechanism with two interfaces. A
// company adding a channel and a company choosing a model do the same kind of thing, and should not
// learn two systems."
//
// Two people at one company say "we run on Slack, not Teams" and "our security people will not let
// us send anything to a model we don't have a contract with". Today they would be handed two
// guides. Here they type:
//
//	omw ext register slack
//	omw ext register acme
//
// The same command, the same arguments in the same order, differing only in the extension being
// registered (criterion 1). The interface is NOT an argument — see `extension.Register`, which
// explains why making it one would have satisfied the test and missed the point.
//
// # A NEW FILE, AND ONLY A NEW FILE
//
// Several Issues edit package commands at once, so this adds a file rather than a branch to
// existing ones. Two things it deliberately does not keep to itself:
//
//   - The daemon's liveness has exactly one definition in this package (`liveness.go`, Issue #41)
//     and this asks it. A private second answer here is the defect that Issue exists to have
//     removed.
//   - The model reviewer's resolution lives in `outbox_cmd.go` and this WRAPS it rather than
//     forking it — see [init] below, which is the one piece of this file a reviewer should read
//     twice.
//
// # NOTHING HERE OPENS A CONNECTION AND NOTHING HERE STARTS THE DAEMON (criteria 15, 16, §4.2)
//
// Registering, listing and configuring touch the store and nothing else. The daemon's state is
// REPORTED on every subcommand and started by none of them, exactly as `omw model` and
// `omw outbox` do it. Registering a model provider does not contact the provider's endpoint:
// `extension.Register` never calls Load.
//
// # WITH NO HUB CONFIGURED, ALL OF IT WORKS (criterion 18, §4.4)
//
// The only part of this command that has anything to do with a hub is the scope check below, and
// that is evaluated locally against what the person holds. There is no path from here to a network.
package commands

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/refusal"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// extensionRegistry is the registry this command reads. It is a var so a test can drive a machine
// that offers a broken extension without a global mutation that leaks into the next test.
var extensionRegistry = extension.Default

func init() {
	cli.Register(&cli.Command{
		Name:    "ext",
		Summary: "add a channel or choose a model — one mechanism, two interfaces",
		Run:     runExtension,
	})

	// CRITERION 10, AND THE ONE PIECE OF CLEVERNESS IN THIS FILE.
	//
	// `outboxReviewer` decides how `review` reaches the person's model. Issue #18 wrote it before
	// this mechanism existed, so it asks `model.Lookup` — which answers "is this provider compiled
	// into the build", not "did its extension load". A provider whose extension is broken therefore
	// gets OPENED, and whatever it does next is reported as a model that could not be reached, or
	// worse, folded into "no model is configured".
	//
	// §3.13 says no-model-configured is not a broken client. Issue #21's text: that sentence
	// "becomes a lie the moment a failed load is dressed up as an unconfigured one".
	//
	// So the extension state is consulted FIRST, and only then does #18's resolution run. This is a
	// wrap and not an edit because this branch may only add files to package commands; the honest
	// version of this is `outbox_cmd.go` calling `extension.ModelReadiness` directly, and that is
	// named in the pull request as the follow-up. The ordering is deterministic: package-level vars
	// are initialised before any init runs, so `outboxReviewer` is the real one when this captures
	// it, and nothing else in this package wraps it.
	inner := outboxReviewer
	outboxReviewer = func(env cli.Env, cfg model.Config) drafts.Reviewer {
		s, err := extensionOpenStoreQuietly(env)
		if err == nil {
			answer := model.Readiness(s, extensionRegistry, cfg.View())
			if answer.Situation == model.SituationExtensionFailedToLoad {
				return outboxUnreachableReviewer{why: answer.Reason + " (code: " + answer.Code + ")"}
			}
		}
		return inner(env, cfg)
	}
}

// extensionOpenStoreQuietly opens the store without printing anything. It is for the reviewer wrap,
// which runs inside another command's output and must not interleave with it.
func extensionOpenStoreQuietly(env cli.Env) (*store.Store, error) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		return nil, err
	}
	return store.Open(path)
}

func runExtension(env cli.Env) int {
	sub, rest := "list", []string(nil)
	if len(env.Args) > 0 {
		sub, rest = env.Args[0], env.Args[1:]
	}
	switch sub {
	case "-h", "--help", "help":
		extensionUsage(env.Stdout)
		return cli.Success
	}
	if code, ok := extensionPreflight(env, sub); !ok {
		return code
	}
	switch sub {
	case "list":
		return extensionList(env, rest)
	case "register":
		return extensionRegister(env, rest)
	case "deregister":
		return extensionDeregister(env, rest)
	case "configure":
		return extensionConfigure(env, rest)
	case "show":
		return extensionShow(env, rest)
	default:
		fmt.Fprintf(env.Stderr, "omw ext: %q is not an ext subcommand.\n", sub)
		extensionUsage(env.Stderr)
		return cli.ExitUsage
	}
}

func extensionUsage(w io.Writer) {
	fmt.Fprint(w, `omw ext — add a channel or choose a model, through one mechanism

usage: omw ext [subcommand]

  list                        every extension and its state — both interfaces, built-ins
                              included, nothing omitted (default)
  register <name>             register an extension. The SAME command and the same argument
                              whether it is a channel adapter or a model provider
  deregister <name>           undo that deliberate act
  configure <name> [k=v ...]  supply its settings. The same shape for both interfaces
  show <name>                 one extension's state

A channel adapter and a model provider are the same kind of thing here. They are registered the
same way, listed in the same listing, configured the same way, and when one is broken you are told
in the same words.

An extension that failed to load is reported as failed to load — never as absent, never as
present-but-idle, and never as silence.

omw takes no custody of credentials: a setting whose name looks like a secret is refused, not
recorded. Supply a credential through your environment or a file you own, and record its path.

Exit status: 0 every registered extension loaded; 1 at least one failed to load; 3 at least one
could not be determined. Those three are never the same number.
`)
}

// extensionPreflight applies the platform ruling and reports the daemon, in that order.
//
// THE DAEMON IS REPORTED AND NEVER REQUIRED (criterion 15, §4.2 and §4.4). §4.2 says commands say
// so when the daemon is not running; §4.4 says the local half stands alone. Both are satisfied by
// saying it on stderr and carrying on: registering an extension is a local act and a stopped daemon
// is no reason to refuse it. Making it fatal would be this capability half-working for want of
// something it does not need — and NOTHING here starts it.
func extensionPreflight(env cli.Env, what string) (int, bool) {
	// PRD §5.1: this build ships for macOS and Linux, and says so anywhere else rather than
	// half-attempting.
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		fmt.Fprintf(env.Stderr, "omw ext %s: this build ships for macOS and Linux; this is %s.\n", what, runtime.GOOS)
		return cli.ExitFailure, false
	}
	// ONE DEFINITION (Issue #41). daemonLiveness is what `omw daemon status` asks, so the two
	// cannot disagree, and it is three-valued so "not running" and "nothing was established" stay
	// different sentences here too.
	switch live, why := daemonLiveness(env); live {
	case tri.Yes:
		fmt.Fprintf(env.Stderr, "daemon: running — this command did not start it.\n")
	case tri.No:
		fmt.Fprintf(env.Stderr, "daemon: not running — nothing has been started on your behalf.\n")
	default:
		fmt.Fprintf(env.Stderr, "daemon: whether it is running %s; nothing has been started on your behalf.\n", tri.Undetermined)
		if why != "" {
			fmt.Fprintf(env.Stderr, "  %s\n", why)
		}
	}
	return cli.Success, true
}

// extensionStore opens the store this invocation is about, in the three outcomes it has.
//
// mustExist is true for the acts that RECORD something, because recording a registration into a
// store that is not there is not something to do quietly. Listing survives a missing store: this
// build still ships Teams and email, and reporting a person with no store as a person with no
// channels would be an absence rendered as a determined nothing.
func extensionStore(env cli.Env, what string, mustExist bool) (*store.Store, int, bool) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw ext %s: where your store lives %s.\n", what, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return nil, cli.ExitUndetermined, false
	}
	s, err := store.Open(path)
	switch {
	case err == nil:
		return s, cli.Success, true
	case errors.Is(err, store.ErrNotFound):
		if mustExist {
			fmt.Fprintf(env.Stderr, "omw ext %s: there is no store at %s, and a registration is recorded in your store.\n", what, path)
			fmt.Fprintf(env.Stderr, "  Nothing was written anywhere else. Run 'omw store create' to create one on purpose.\n")
			return nil, cli.ExitFailure, false
		}
		fmt.Fprintf(env.Stderr, "there is no store at %s, so nothing is registered there; listing what this build ships.\n", path)
		return nil, cli.Success, true
	default:
		fmt.Fprintf(env.Stderr, "omw ext %s: the store at %s could not be read: %v\n", what, path, err)
		fmt.Fprintf(env.Stderr, "  An unreadable store is not one with no extensions registered in it.\n")
		return nil, cli.ExitUndetermined, false
	}
}

// extensionList is criteria 2, 6, 7, 11, 12, 13, 14 and 21's CLI half.
//
// ONE LISTING. Both interfaces, the built-in Teams and email channels sorted in among everything
// registered through the extension point, every registered extension present whatever its state,
// and no entry rendered as an empty line. There is no `omw ext channels` and no `omw ext models`,
// because criterion 2 forbids a per-interface listing that shows something this does not.
func extensionList(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw ext list: this takes no arguments.\n")
		return cli.ExitUsage
	}
	s, code, ok := extensionStore(env, "list", false)
	if !ok {
		return code
	}
	entries, err := extension.Inventory(s, extensionRegistry)
	if err != nil {
		// The registration list could not be read. NOT "nothing is registered": the built-ins below
		// are still real, and this is said above them rather than instead of them.
		fmt.Fprintf(env.Stderr, "omw ext list: which extensions are registered %s: %v\n", tri.Undetermined, err)
		fmt.Fprintf(env.Stderr, "  This is NOT a report that none is registered. What follows is what this build ships.\n")
	}
	fmt.Fprint(env.Stdout, extension.Render(entries))
	sum := extension.Summarise(entries)
	code = extensionExitFor(sum)
	extensionSaySummary(env, sum)
	if err != nil && code == cli.Success {
		return cli.ExitUndetermined
	}
	return code
}

// extensionExitFor is CRITERION 12, and it is the whole of it.
//
// "Command exit status distinguishes 'every registered extension loaded' from 'at least one failed
// to load' — distinguishable by exit code alone, with no output parsing." And the project's
// standing rule on top of it: `could not determine` and `determined to be nothing` must never share
// an exit code.
//
// So there are three numbers and the order of the branches is the argument for them:
//
//   - At least one FAILED: the question "did every registered extension load?" has been ANSWERED,
//     and the answer is no. A determined negative is a successful determination of a failure, and
//     it exits ExitFailure — not ExitUndetermined, which would say we did not find out.
//   - Otherwise at least one UNDETERMINED: nothing failed, and we cannot claim everything loaded
//     either. ExitUndetermined. This branch is second, not first, because a tree containing a
//     known failure has a known answer whatever else is fuzzy.
//   - Otherwise Success.
//
// Not-registered entries count towards none of these; see `extension.Summary.AllLoaded`.
func extensionExitFor(sum extension.Summary) int {
	switch {
	case sum.Failed > 0:
		return cli.ExitFailure
	case sum.Undetermined > 0:
		return cli.ExitUndetermined
	default:
		return cli.Success
	}
}

// extensionSaySummary puts the exit code's reason in words on stderr, so a person reading the
// terminal is told the same thing a script reads from `$?`.
//
// # THE CODES HERE ARE INTERFACE-NEUTRAL, AND A REVIEWER HAD TO SAY SO
//
// This summarises a MIXED set — channel adapters and model providers together — so the code it
// prints cannot belong to either interface. It used `model.ErrProviderFailedToLoad.Code`
// unconditionally, and a machine whose only broken extension was a channel adapter told every
// machine reader that its MODEL PROVIDER was broken. The Slack adapter that will not load, reported
// as a model fault, which sends the reader to the wrong subsystem entirely.
//
// Picking the code from whichever interface failed is the repair that looks obvious and is a trap:
// with one broken extension of each it has to pick one anyway, and §2.5's symmetry breaks in
// exactly the line a script reads. TestTheFailureSummaryNamesNeitherInterface drives both biases.
//
// Per-ENTRY codes stay interface-specific and must — see `model.ErrProviderFailedToLoad`, which
// argues it. A summary over several is [extension.ErrFailedToLoad] and
// [extension.ErrLoadUndetermined], both neutral, one for each of the two non-success answers.
func extensionSaySummary(env cli.Env, sum extension.Summary) {
	switch {
	case sum.Failed > 0:
		fmt.Fprintf(env.Stderr, "omw ext list: %d extension(s) FAILED TO LOAD (code: %s).\n",
			sum.Failed, extension.ErrFailedToLoad.Code)
		fmt.Fprintf(env.Stderr, "  This is not a report that they are absent, and not a report that they are idle.\n")
	case sum.Undetermined > 0:
		fmt.Fprintf(env.Stderr, "omw ext list: whether %d extension(s) loaded %s (code: %s).\n",
			sum.Undetermined, tri.Undetermined, extension.ErrLoadUndetermined.Code)
		fmt.Fprintf(env.Stderr, "  This is NOT a report that they failed.\n")
	default:
		fmt.Fprintf(env.Stderr, "omw ext list: every registered extension loaded.\n")
	}
}

// extensionRegister is CRITERION 1: the same act, for both interfaces.
//
// `omw ext register slack` and `omw ext register acme` — one command, one argument, in one order.
// The test that registers one and then the other, changing only the extension identifier, passes
// both times, and it passes because there is no second thing to change.
func extensionRegister(env cli.Env, args []string) int {
	if len(args) < 1 {
		fmt.Fprintf(env.Stderr, "omw ext register: name exactly one extension.\n")
		fmt.Fprintf(env.Stderr, "  The same command registers a channel adapter and a model provider;\n")
		fmt.Fprintf(env.Stderr, "  which interface it implements comes from the extension, not from you.\n")
		return cli.ExitUsage
	}
	name, rest := args[0], args[1:]
	settings, code, ok := extensionParseSettings(env, "register", rest)
	if !ok {
		return code
	}
	// CRITERION 23, AND IT IS EVALUATED BEFORE ANYTHING IS WRITTEN. §4.5: "a grant that would let
	// something read more than its holder can is not narrowed at the edge; it is refused when it is
	// requested." A registration that asked for too much leaves NO record at all, which is also
	// criterion 19's "never left behind as a half-registered entry".
	if code, ok := extensionCheckScopes(env, "register", name, settings); !ok {
		return code
	}
	s, code, ok := extensionStore(env, "register", true)
	if !ok {
		return code
	}
	if err := extension.Register(s, extensionRegistry, name, settings); err != nil {
		fmt.Fprintf(env.Stderr, "omw ext register: %v (code: %s)\n", err, refusal.Code(err))
		fmt.Fprintf(env.Stderr, "  Nothing was registered, and no partial registration was left behind.\n")
		return cli.ExitFailure
	}
	reg, err := extension.Get(s, name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw ext register: %s was registered and reading it back %s: %v\n", name, tri.Undetermined, err)
		return cli.ExitUndetermined
	}
	fmt.Fprintf(env.Stdout, "registered: %s (%s)\n", reg.Name, reg.Interface)
	entries, _ := extension.Inventory(s, extensionRegistry)
	fmt.Fprint(env.Stdout, extension.Find(entries, name).Render())
	return extensionExitFor(extension.Summarise([]extension.Entry{extension.Find(entries, name)}))
}

func extensionDeregister(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw ext deregister: name exactly one extension.\n")
		return cli.ExitUsage
	}
	s, code, ok := extensionStore(env, "deregister", true)
	if !ok {
		return code
	}
	if err := extension.Deregister(s, args[0]); err != nil {
		fmt.Fprintf(env.Stderr, "omw ext deregister: %v (code: %s)\n", err, refusal.Code(err))
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "deregistered: %s\n", args[0])
	fmt.Fprintf(env.Stdout, "  It is still present on this machine and is now not registered, so it is not running.\n")
	return cli.Success
}

// extensionConfigure is CRITERION 4: the same shape for both interfaces.
//
// "Whatever a person types to supply an adapter's settings is what they type to supply a provider's
// settings (§3.13 credentials included); a test that configures one and then the other using the
// same command form succeeds for both."
//
// The credential half is honoured by REFUSING to hold one — see `extension.SecretishKeys` for why
// refusing at the point of record beats redacting at the point of display.
func extensionConfigure(env cli.Env, args []string) int {
	if len(args) < 1 {
		fmt.Fprintf(env.Stderr, "omw ext configure: name exactly one extension, then its settings as key=value.\n")
		return cli.ExitUsage
	}
	name, rest := args[0], args[1:]
	settings, code, ok := extensionParseSettings(env, "configure", rest)
	if !ok {
		return code
	}
	if code, ok := extensionCheckScopes(env, "configure", name, settings); !ok {
		return code
	}
	s, code, ok := extensionStore(env, "configure", true)
	if !ok {
		return code
	}
	if err := extension.Configure(s, name, settings); err != nil {
		fmt.Fprintf(env.Stderr, "omw ext configure: %v (code: %s)\n", err, refusal.Code(err))
		fmt.Fprintf(env.Stderr, "  Nothing was changed.\n")
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "configured: %s\n", name)
	reg, err := extension.Get(s, name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw ext configure: reading %s back %s: %v\n", name, tri.Undetermined, err)
		return cli.ExitUndetermined
	}
	for _, k := range sortedKeys(reg.Settings) {
		fmt.Fprintf(env.Stdout, "  %s = %s\n", k, reg.Settings[k])
	}
	if len(reg.Settings) == 0 {
		// SAID, NOT LEFT BLANK. No settings is a real state and an empty section reads as a bug.
		fmt.Fprintf(env.Stdout, "  it has no settings recorded.\n")
	}
	return cli.Success
}

func extensionShow(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw ext show: name exactly one extension.\n")
		return cli.ExitUsage
	}
	s, code, ok := extensionStore(env, "show", false)
	if !ok {
		return code
	}
	entries, _ := extension.Inventory(s, extensionRegistry)
	e := extension.Find(entries, args[0])
	fmt.Fprint(env.Stdout, e.Render())
	return extensionExitFor(extension.Summarise([]extension.Entry{e}))
}

// extensionParseSettings reads `key=value` arguments, in the one shape both interfaces use.
func extensionParseSettings(env cli.Env, what string, args []string) (map[string]string, int, bool) {
	if len(args) == 0 {
		return nil, cli.Success, true
	}
	out := map[string]string{}
	for _, a := range args {
		k, v, found := strings.Cut(a, "=")
		k = strings.TrimSpace(k)
		if !found || k == "" {
			fmt.Fprintf(env.Stderr, "omw ext %s: %q is not a setting; settings are key=value.\n", what, a)
			return nil, cli.ExitUsage, false
		}
		if _, dup := out[k]; dup {
			// REFUSED, not last-one-wins. A person who typed a key twice meant something, and
			// silently keeping one of the two values is a choice made on their behalf.
			fmt.Fprintf(env.Stderr, "omw ext %s: the setting %q was given twice; nothing was recorded.\n", what, k)
			return nil, cli.ExitUsage, false
		}
		out[k] = v
	}
	return out, cli.Success, true
}

// extensionScopeSetting is the setting an extension's requested scopes are named in.
const extensionScopeSetting = "scope"

// extensionScopesEnv is where the scopes THIS PERSON holds are read from.
//
// # A PLACEHOLDER, NAMED AS ONE
//
// Issue #19 owns sign-in and the token material a person's real scopes come from, and it is not on
// this branch. Issue #21 did not settle where they come from either. What Issue #21 DOES settle is
// criterion 23 — "a scope that would let it is refused when requested, not narrowed at the edge" —
// and that rule can be implemented and driven now against whatever supplies the holder later.
//
// So this reads the holder's scopes from the environment, defaulting to all three (on your own
// machine, you hold everything you can do), and the DECISION is delegated to
// `hub.EvaluateGrantRequest` — the function §4.5 lives in, which #12's own comment says Issue #19
// "must call and must not re-derive". When #19 lands, this variable is replaced and the rule below
// is untouched.
const extensionScopesEnv = "OMW_SCOPES"

// extensionCheckScopes is CRITERION 23.
//
// "Adding a channel adapter grants it nothing wider than the person it runs for (§4.5). An
// extension cannot read what its person cannot; a scope that would let it is refused when
// requested, not narrowed at the edge."
//
// The rule is not restated here. `hub.EvaluateGrantRequest` refuses a request for a scope the
// holder does not hold, ENTIRELY, and refuses a word outside the three-word vocabulary. This calls
// it and reports what it said. Returning the intersection — registering the extension with the
// scopes it was allowed — is the natural, helpful, forbidden thing.
func extensionCheckScopes(env cli.Env, what, name string, settings map[string]string) (int, bool) {
	raw, ok := settings[extensionScopeSetting]
	if !ok || strings.TrimSpace(raw) == "" {
		// An extension that asked for nothing is granted nothing, which is narrower than its person
		// and therefore fine. Nothing implicit (§4.2): no scope is assumed on its behalf.
		return cli.Success, true
	}
	var requested []hub.Scope
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			requested = append(requested, hub.Scope(p))
		}
	}
	holder := hub.Holder{Person: hub.PersonID("you"), Scopes: extensionHeldScopes(env)}
	if _, err := hub.EvaluateGrantRequest(holder, requested); err != nil {
		fmt.Fprintf(env.Stderr, "omw ext %s: %v (code: %s)\n", what, err, hub.Code(err))
		fmt.Fprintf(env.Stderr, "  %s was NOT registered with a narrower scope instead; it was not registered at all.\n", name)
		fmt.Fprintf(env.Stderr, "  The scope vocabulary is exactly: %s\n", extensionScopeVocabulary())
		return cli.ExitFailure, false
	}
	return cli.Success, true
}

// extensionHeldScopes is what the person running this holds. See [extensionScopesEnv].
func extensionHeldScopes(env cli.Env) []hub.Scope {
	raw := strings.TrimSpace(env.Getenv(extensionScopesEnv))
	if raw == "" {
		return hub.Vocabulary()
	}
	var out []hub.Scope
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, hub.Scope(p))
		}
	}
	return out
}

func extensionScopeVocabulary() string {
	words := make([]string, 0, len(hub.Vocabulary()))
	for _, s := range hub.Vocabulary() {
		words = append(words, string(s))
	}
	return strings.Join(words, ", ")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
