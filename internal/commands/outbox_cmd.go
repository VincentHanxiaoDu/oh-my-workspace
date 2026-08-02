// Command `omw outbox` — Issue #9: drafting notes into the outbox, and choosing how they leave it.
//
// THE FAILURE THIS COMMAND EXISTS TO PREVENT. A person chooses `review` because they do not trust
// themselves to catch what they should not have written. They have no model configured. Two things
// the client could do here are both catastrophic and both look like nothing at all: behave like
// `manual`, so their drafts pile up unchecked and unpublished with nothing said; or behave like
// `auto`, so their drafts publish with nobody having read them. Each is the client silently doing
// something other than what the person chose. So `review` with no model NAMES the missing model,
// exits non-zero, and leaves the draft in a state that reads differently from "you simply have not
// published this yet" — see [outboxReviewGate].
//
// A NEW FILE, AND ONLY THIS FILE. Several Issues edit package commands at once; this one adds a
// file and touches nothing that already exists. The two environment variable names below are
// spelled out again rather than borrowed from a neighbour's constants for the same reason.
package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "outbox",
		Summary: "draft notes into your outbox, and choose how they leave it",
		Run:     runOutbox,
	})
}

const (
	outboxEnvHub    = "OMW_HUB"
	outboxEnvSocket = "OMW_CONTROL_SOCKET"
)

// outboxReviewer is how this command reaches the person's model.
//
// THERE IS NO MODEL TRANSPORT IN THIS BUILD, and this says so rather than pretending. Issue #18
// owns model configuration and the transport; until it lands, a configured model is one this build
// cannot reach, which is UNDETERMINED and never a pass (criterion 16). Tests replace this to drive
// the passing, refusing and unusable paths.
var outboxReviewer = func(env cli.Env, cfg drafts.ModelConfig) drafts.Reviewer {
	return outboxUnreachableReviewer{}
}

type outboxUnreachableReviewer struct{}

func (outboxUnreachableReviewer) Review(rules, body string) (string, error) {
	return "", errors.New("this build has no transport to a model (Issue #18)")
}

// outboxDaemonRunning asks Issue #2's own answer rather than inventing a second one.
//
// [daemon.Inspect] reads the store's lock and run record and starts nothing, and its answer is
// three-valued — which is the point: "the daemon is not running" and "I could not tell whether the
// daemon is running" are different things to say to a person, and a private socket-stat here could
// only ever say the first. It is a var so a test can drive the three branches without a daemon.
var outboxDaemonRunning = func(storeRoot string) tri.Value {
	return daemon.Inspect(storeRoot).Running
}

func runOutbox(env cli.Env) int {
	if len(env.Args) == 0 {
		outboxUsage(env.Stdout)
		return cli.ExitUsage
	}
	sub, rest := env.Args[0], env.Args[1:]
	switch sub {
	case "-h", "--help", "help":
		outboxUsage(env.Stdout)
		return cli.Success
	}
	if code, ok := outboxPreflight(env, sub); !ok {
		return code
	}
	switch sub {
	case "draft":
		return outboxDraft(env, rest)
	case "list":
		return outboxList(env, rest)
	case "state":
		return outboxState(env, rest)
	case "mode":
		return outboxMode(env, rest)
	case "rules":
		return outboxRules(env, rest)
	case "model":
		return outboxModel(env, rest)
	case "review":
		return outboxReview(env, rest)
	case "publish":
		return outboxPublish(env, rest)
	default:
		fmt.Fprintf(env.Stderr, "omw outbox: unknown subcommand %q\n", sub)
		fmt.Fprintf(env.Stderr, "run 'omw outbox help' for what this build has.\n")
		return cli.ExitUsage
	}
}

func outboxUsage(w io.Writer) {
	fmt.Fprint(w, `omw outbox — your drafts, and how they leave

usage: omw outbox <subcommand>

  draft <id> <text>      write a draft into your outbox (adds a revision to an existing one)
  list                   the drafts in your outbox, and where each one stands
  state <id>             where one draft stands
  mode                   the publication mode in effect
  mode set <mode>        choose manual, review or auto
  rules                  your review rules, exactly as you recorded them
  rules set <text>       record the rules review checks against, in your own words
  model                  whether a model is configured for review (never prints your key)
  review <id>            check one draft against your rules, on this machine
  publish <id>           run your chosen gate and try to publish

Drafting, listing and manual mode need no hub and open no connection. Where a hub is genuinely
needed this says exactly what is missing and exits non-zero; it never half-works.
`)
}

// ---------------------------------------------------------------------------
// Preflight: platform, the control socket, and the daemon
// ---------------------------------------------------------------------------

// outboxPreflight applies criteria 20 and 23 to every subcommand, in that order.
//
// It is one function so that no subcommand can be the one that quietly skips it, and it is called
// from the dispatcher rather than from each subcommand for the same reason.
func outboxPreflight(env cli.Env, what string) (int, bool) {
	// CRITERION 23, platforms. PRD §5.1 ships macOS and Linux; anywhere else the answer is that
	// this build does not run here, said rather than half-attempted.
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		fmt.Fprintf(env.Stderr, "omw outbox %s: this build ships for macOS and Linux; this is %s.\n", what, runtime.GOOS)
		return cli.ExitFailure, false
	}
	// CRITERION 23, the socket. If the control API's socket is named and is NOT owner-only, the
	// commands say so and stop; if its permissions cannot be confirmed at all, that is undetermined
	// and also stops. Nothing here opens the socket — this is about refusing to proceed beside one
	// whose permissions are wrong, which is §4.6 read literally.
	if p := strings.TrimSpace(env.Getenv(outboxEnvSocket)); p != "" {
		info, err := os.Stat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Nothing is there, so there is no socket whose permissions could be wrong. The daemon
			// note below is what a person needs here, not a refusal.
		case err != nil:
			fmt.Fprintf(env.Stderr, "omw outbox %s: whether the control socket is owner-only %s.\n", what, tri.Undetermined)
			fmt.Fprintf(env.Stderr, "  %v\n", err)
			fmt.Fprintf(env.Stderr, "  This is NOT 'the permissions are fine'. Nothing was done.\n")
			return cli.ExitUndetermined, false
		case info.Mode().Perm()&0o077 != 0:
			fmt.Fprintf(env.Stderr, "omw outbox %s: refused — the control socket at %s is not owner-only (%04o).\n", what, p, info.Mode().Perm())
			fmt.Fprintf(env.Stderr, "  Your outbox holds writing that has never left this machine.\n")
			return cli.ExitFailure, false
		}
	}
	// CRITERION 20: the daemon's state is REPORTED, on every command, and started by none of them.
	// It goes to stderr because it is not the answer the person asked for; it is said anyway
	// because a person whose daemon is down should never have to infer it.
	//
	// A store this command cannot even locate is not reported on: the subcommand is about to say
	// so precisely, and "the daemon is not running" said about a store nobody found would be a
	// determined answer nobody determined.
	if root, rerr := store.Resolve(env.Getenv); rerr == nil {
		switch outboxDaemonRunning(root) {
		case tri.Yes:
			fmt.Fprintf(env.Stderr, "daemon: running — this command did not start it.\n")
		case tri.No:
			fmt.Fprintf(env.Stderr, "daemon: not running — nothing has been started on your behalf.\n")
		default:
			fmt.Fprintf(env.Stderr, "daemon: whether it is running %s; nothing has been started on your behalf.\n", tri.Undetermined)
		}
	}
	return cli.Success, true
}

// ---------------------------------------------------------------------------
// The store, and the outbox inside it
// ---------------------------------------------------------------------------

// outboxOpenStore is criterion 3. A missing store is NAMED and non-zero; a draft is never written
// to a temporary location so that a command can exit zero.
func outboxOpenStore(env cli.Env, what string) (*store.Store, int, bool) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw outbox %s: where your store lives %s.\n", what, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return nil, cli.ExitUndetermined, false
	}
	s, err := store.Open(path)
	switch {
	case err == nil:
		return s, cli.Success, true
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(env.Stderr, "omw outbox %s: there is no store at %s, and %v.\n", what, path, drafts.ErrNoStore)
		fmt.Fprintf(env.Stderr, "  Nothing was written anywhere else — not to a temporary directory, not\n")
		fmt.Fprintf(env.Stderr, "  to your home directory. Run 'omw store create' to create one on purpose.\n")
		return nil, cli.ExitFailure, false
	case errors.Is(err, store.ErrUnreadable), errors.Is(err, store.ErrPermissionDenied):
		fmt.Fprintf(env.Stderr, "omw outbox %s: the store at %s could not be read: %v\n", what, path, err)
		fmt.Fprintf(env.Stderr, "  An unreadable store is not an empty one.\n")
		return nil, cli.ExitUndetermined, false
	default:
		fmt.Fprintf(env.Stderr, "omw outbox %s: the store at %s %s: %v\n", what, path, tri.Undetermined, err)
		return nil, cli.ExitUndetermined, false
	}
}

func outboxOpen(env cli.Env, what string) (*store.Store, *drafts.Outbox, int, bool) {
	s, code, ok := outboxOpenStore(env, what)
	if !ok {
		return nil, nil, code, false
	}
	o, err := drafts.InStore(s)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw outbox %s: your outbox inside %s could not be opened: %v\n", what, s.Path(), err)
		fmt.Fprintf(env.Stderr, "  This is not an empty outbox.\n")
		return nil, nil, cli.ExitUndetermined, false
	}
	return s, o, cli.Success, true
}

// outboxHubIsConfigured is a determined fact about this machine, and nothing in this file dials anything
// when the answer is no.
func outboxHubIsConfigured(env cli.Env) bool {
	return strings.TrimSpace(env.Getenv(outboxEnvHub)) != ""
}

// ---------------------------------------------------------------------------
// draft
// ---------------------------------------------------------------------------

func outboxDraft(env cli.Env, args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(env.Stderr, "omw outbox draft: name the draft and give its text.\n")
		return cli.ExitUsage
	}
	s, o, code, ok := outboxOpen(env, "draft")
	if !ok {
		return code
	}
	id := hub.NoteID(args[0])
	body := strings.Join(args[1:], " ")

	before := o.StateOf(id)
	if _, err := o.Revise(id, body); err != nil {
		fmt.Fprintf(env.Stderr, "omw outbox draft: %v (code: %s)\n", err, hub.Code(err))
		return cli.ExitFailure
	}
	if before.Exists != tri.Yes {
		// A NEW DRAFT IS `drafted`, WRITTEN DOWN. The state is recorded rather than inferred from
		// the absence of a file, so that "resting" and "the record of what happened to this draft
		// could not be read" can never be the same answer later.
		if err := o.SetState(id, drafts.StateDrafted, ""); err != nil {
			fmt.Fprintf(env.Stderr, "omw outbox draft: the draft was written and its state was not: %v\n", err)
			return cli.ExitUndetermined
		}
	}
	fmt.Fprintf(env.Stdout, "draft: %s\n", string(id))
	fmt.Fprintf(env.Stdout, "outbox: %s\n", o.Dir())

	ms := drafts.ReadMode(s)
	fmt.Fprintf(env.Stdout, "%s\n", ms.Render())
	if ms.Known != tri.Yes {
		// The draft is written and safe; which mode should act on it is not known, so nothing acts.
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		fmt.Fprintf(env.Stderr, "omw outbox draft: %v\n", drafts.ErrModeUnreadable)
		return cli.ExitUndetermined
	}

	switch ms.Mode {
	case drafts.ModeManual:
		// CRITERION 8 AND 22: no connection, no hub, no warning about a hub. This is the whole
		// working local half, and it says so without apologising for anything.
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		return cli.Success

	case drafts.ModeReview:
		// CRITERION 14, AT THE MOMENT THE PERSON WRITES. A `review` person with no model must not
		// be able to accumulate drafts in silence; the missing model is named the first time it
		// matters, not only when they eventually try to publish.
		cfg := drafts.ReadModel(env.Getenv)
		fmt.Fprintf(env.Stdout, "%s\n", cfg.Render())
		switch cfg.Configured {
		case tri.Yes:
			fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
			fmt.Fprintf(env.Stdout, "your rules will be checked when you run 'omw outbox review %s' or publish it.\n", string(id))
			return cli.Success
		case tri.No:
			_ = o.SetState(id, drafts.StateBlocked, "you chose review mode and no model is configured, so nothing can check your rules")
			fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
			fmt.Fprintf(env.Stderr, "omw outbox draft: %v (code: %s)\n", drafts.ErrNoModel, drafts.ErrNoModel.Code)
			fmt.Fprintf(env.Stderr, "  You chose review. This draft has NOT been checked and will not be published.\n")
			return cli.ExitFailure
		default:
			_ = o.SetState(id, drafts.StateBlocked, "you chose review mode and whether a model is configured could not be determined")
			fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
			fmt.Fprintf(env.Stderr, "omw outbox draft: %v (code: %s)\n", drafts.ErrModelUndetermined, drafts.ErrModelUndetermined.Code)
			return cli.ExitUndetermined
		}

	default: // auto
		// CRITERION 9: the mode had an effect. The draft leaves the resting state — and what
		// happens to it in flight is Issue #10's, which this says outright rather than claiming a
		// publication this build cannot perform.
		if !outboxHubIsConfigured(env) {
			// CRITERION 22: a hub genuinely is required here, so say precisely what is missing.
			fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
			fmt.Fprintf(env.Stderr, "omw outbox draft: %v (code: %s)\n", hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
			fmt.Fprintf(env.Stderr, "  You chose auto, and auto sends drafts to a hub. There is no hub configured,\n")
			fmt.Fprintf(env.Stderr, "  so this draft is resting in your outbox and nothing has been sent.\n")
			return cli.ExitFailure
		}
		_ = o.SetState(id, drafts.StateLeaving, "auto mode acted on this draft; the transfer itself is Issue #10 and this build has no transport, so it has not left this machine")
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		fmt.Fprintf(env.Stdout, "published: %s — the transfer out of the outbox is not part of this capability\n", tri.Undetermined)
		return cli.ExitUndetermined
	}
}

// ---------------------------------------------------------------------------
// list, state
// ---------------------------------------------------------------------------

// outboxList is criterion 4: an empty outbox and an unreadable one are different sentences and
// different exit codes.
func outboxList(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw outbox list: this takes no arguments.\n")
		return cli.ExitUsage
	}
	_, o, code, ok := outboxOpen(env, "list")
	if !ok {
		return code
	}
	ids, err := o.Drafts()
	if err != nil {
		fmt.Fprintf(env.Stdout, "drafts: %s\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw outbox list: your outbox could not be read: %v\n", err)
		fmt.Fprintf(env.Stderr, "  This is NOT an empty outbox. Nothing has been established about your drafts.\n")
		return cli.ExitUndetermined
	}
	if len(ids) == 0 {
		fmt.Fprintf(env.Stdout, "drafts: 0 — your outbox is empty, and that is a determined answer\n")
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "drafts: %d\n", len(ids))
	worst := cli.Success
	for _, id := range ids {
		r := o.StateOf(id)
		fmt.Fprintf(env.Stdout, "  %s\n", string(id))
		for _, line := range strings.Split(r.Render(), "\n") {
			fmt.Fprintf(env.Stdout, "    %s\n", strings.TrimSpace(line))
		}
		if r.Known != tri.Yes {
			worst = cli.ExitUndetermined
		}
	}
	return worst
}

func outboxState(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw outbox state: name exactly one draft.\n")
		return cli.ExitUsage
	}
	_, o, code, ok := outboxOpen(env, "state")
	if !ok {
		return code
	}
	r := o.StateOf(hub.NoteID(args[0]))
	fmt.Fprintf(env.Stdout, "%s\n", r.Render())
	switch {
	case r.Known != tri.Yes:
		return cli.ExitUndetermined
	case r.Exists == tri.No:
		return cli.ExitFailure
	default:
		return cli.Success
	}
}

// ---------------------------------------------------------------------------
// mode
// ---------------------------------------------------------------------------

func outboxMode(env cli.Env, args []string) int {
	s, code, ok := outboxOpenStore(env, "mode")
	if !ok {
		return code
	}
	if len(args) == 0 {
		ms := drafts.ReadMode(s)
		fmt.Fprintf(env.Stdout, "%s\n", ms.Render())
		if ms.Known != tri.Yes {
			fmt.Fprintf(env.Stderr, "omw outbox mode: %v (code: %s)\n", drafts.ErrModeUnreadable, drafts.ErrModeUnreadable.Code)
			if ms.Why != "" {
				fmt.Fprintf(env.Stderr, "  %s\n", ms.Why)
			}
			return cli.ExitUndetermined
		}
		return cli.Success
	}
	if args[0] != "set" {
		fmt.Fprintf(env.Stderr, "omw outbox mode: unknown argument %q; say 'omw outbox mode' or 'omw outbox mode set <mode>'.\n", args[0])
		return cli.ExitUsage
	}
	if len(args) != 2 {
		fmt.Fprintf(env.Stderr, "omw outbox mode set: name exactly one mode: manual, review or auto.\n")
		return cli.ExitUsage
	}
	m, err := drafts.ParseMode(args[1])
	if err != nil {
		// CRITERION 7: refused, non-zero, and NOTHING WRITTEN. The mode that was in effect before
		// is still in effect, and the message says so rather than leaving the person to check.
		fmt.Fprintf(env.Stderr, "omw outbox mode set: %v (code: %s)\n", err, hub.Code(err))
		fmt.Fprintf(env.Stderr, "  Nothing was changed. %s\n", drafts.ReadMode(s).Render())
		return cli.ExitUsage
	}
	if err := drafts.WriteMode(s, m); err != nil {
		fmt.Fprintf(env.Stderr, "omw outbox mode set: %v\n", err)
		return cli.ExitFailure
	}
	// CRITERION 6: the client reports back the mode that is NOW IN EFFECT, and it reports it by
	// reading it back rather than by echoing what it was told.
	fmt.Fprintf(env.Stdout, "%s\n", drafts.ReadMode(s).Render())
	// CRITERION 10: said out loud, because a person switching to auto reasonably wonders.
	fmt.Fprintf(env.Stdout, "drafts already in your outbox have not been acted on by this change.\n")
	return cli.Success
}

// ---------------------------------------------------------------------------
// rules
// ---------------------------------------------------------------------------

// outboxRules is criterion 11. On the read side, stdout carries the person's bytes and NOTHING
// ELSE — every word this command has to say about the rules goes to stderr, so that reading them
// back cannot pick up a heading, an indent, or a tidy-up on the way through.
func outboxRules(env cli.Env, args []string) int {
	s, code, ok := outboxOpenStore(env, "rules")
	if !ok {
		return code
	}
	if len(args) == 0 {
		r := drafts.ReadRules(s)
		switch r.Recorded {
		case tri.Yes:
			fmt.Fprintf(env.Stderr, "these are your rules, exactly as you recorded them:\n")
			io.WriteString(env.Stdout, r.Text)
			if !strings.HasSuffix(r.Text, "\n") {
				io.WriteString(env.Stdout, "\n")
			}
			return cli.Success
		case tri.No:
			fmt.Fprintf(env.Stdout, "rules: none have been recorded — review would have nothing to check against\n")
			return cli.Success
		default:
			fmt.Fprintf(env.Stdout, "rules: %s — this is not 'you have not recorded any'\n", tri.Undetermined)
			fmt.Fprintf(env.Stderr, "omw outbox rules: %v (code: %s)\n", drafts.ErrRulesUnreadable, drafts.ErrRulesUnreadable.Code)
			if r.Why != "" {
				fmt.Fprintf(env.Stderr, "  %s\n", r.Why)
			}
			return cli.ExitUndetermined
		}
	}
	if args[0] != "set" {
		fmt.Fprintf(env.Stderr, "omw outbox rules: unknown argument %q; say 'omw outbox rules' or 'omw outbox rules set <text>'.\n", args[0])
		return cli.ExitUsage
	}
	if len(args) < 2 {
		fmt.Fprintf(env.Stderr, "omw outbox rules set: give the rules, in your own words.\n")
		return cli.ExitUsage
	}
	// JOINED WITH A SINGLE SPACE AND OTHERWISE UNTOUCHED. The shell has already split the person's
	// words; joining them back is the one unavoidable reconstruction, and it is the reason the
	// command also accepts the whole thing as one quoted argument, which is what the help says.
	text := strings.Join(args[1:], " ")
	if err := drafts.WriteRules(s, text); err != nil {
		fmt.Fprintf(env.Stderr, "omw outbox rules set: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stderr, "recorded, in your words. 'omw outbox rules' reads them back unchanged.\n")
	return cli.Success
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

func outboxModel(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw outbox model: this takes no arguments; configuring a model is Issue #18.\n")
		return cli.ExitUsage
	}
	cfg := drafts.ReadModel(env.Getenv)
	fmt.Fprintf(env.Stdout, "%s\n", cfg.Render())
	switch cfg.Configured {
	case tri.Yes:
		return cli.Success
	case tri.No:
		// A DETERMINED NEGATIVE SUCCEEDS AT ANSWERING. Asking whether a model is configured and
		// being told "no" is an answer; it is the attempt to REVIEW without one that fails.
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw outbox model: %v (code: %s)\n", drafts.ErrModelUndetermined, drafts.ErrModelUndetermined.Code)
		return cli.ExitUndetermined
	}
}

// ---------------------------------------------------------------------------
// review, publish
// ---------------------------------------------------------------------------

// outboxGateResult is what the gate concluded, in the terms the callers need.
type outboxGateResult struct {
	mayLeave bool
	code     int
}

// outboxReviewGate runs the person's chosen gate over one draft.
//
// IT NEVER DEPENDS ON THE HUB (criterion 12, PRD §5.2). The hub is not read, not dialled and not
// mentioned; a person with no hub at all gets the whole check. It is also the only place in this
// file that decides a draft may leave, so there is exactly one path to guard.
func outboxReviewGate(env cli.Env, s *store.Store, o *drafts.Outbox, id hub.NoteID, what string) outboxGateResult {
	ms := drafts.ReadMode(s)
	fmt.Fprintf(env.Stdout, "%s\n", ms.Render())
	if ms.Known != tri.Yes {
		fmt.Fprintf(env.Stderr, "omw outbox %s: %v (code: %s)\n", what, drafts.ErrModeUnreadable, drafts.ErrModeUnreadable.Code)
		return outboxGateResult{code: cli.ExitUndetermined}
	}
	if ms.Mode != drafts.ModeReview {
		// manual and auto have no gate of their own: the person's act is the gate in manual, and
		// auto is the deliberate absence of one.
		return outboxGateResult{mayLeave: true, code: cli.Success}
	}

	cfg := drafts.ReadModel(env.Getenv)
	fmt.Fprintf(env.Stdout, "%s\n", cfg.Render())
	switch cfg.Configured {
	case tri.No:
		// THE CENTRAL REFUSAL (criteria 13, 14, 15). It names the missing model, it exits non-zero,
		// the draft is left in a state that does not read as "awaiting you", and NOTHING IS
		// PUBLISHED. Behaving like manual here (say nothing, leave it resting) and behaving like
		// auto (publish it unchecked) are the two failures this Issue exists to prevent.
		_ = o.SetState(id, drafts.StateBlocked, "you chose review mode and no model is configured, so nothing can check your rules")
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		fmt.Fprintf(env.Stderr, "omw outbox %s: %v (code: %s)\n", what, drafts.ErrNoModel, drafts.ErrNoModel.Code)
		fmt.Fprintf(env.Stderr, "  %s\n", cfg.Missing)
		fmt.Fprintf(env.Stderr, "  Nothing has been published, and this draft is not merely awaiting you.\n")
		return outboxGateResult{code: cli.ExitFailure}
	case tri.Undetermined:
		_ = o.SetState(id, drafts.StateBlocked, "you chose review mode and whether a model is configured could not be determined")
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		fmt.Fprintf(env.Stderr, "omw outbox %s: %v (code: %s)\n", what, drafts.ErrModelUndetermined, drafts.ErrModelUndetermined.Code)
		return outboxGateResult{code: cli.ExitUndetermined}
	}

	rules := drafts.ReadRules(s)
	if rules.Recorded == tri.Undetermined {
		// Rules that cannot be read are not "no rules". Checking a draft against an empty rule set
		// would pass everything, which is `auto` wearing `review`'s name.
		_ = o.SetState(id, drafts.StateReviewUndetermined, "your rules could not be read, so nothing was checked")
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		fmt.Fprintf(env.Stderr, "omw outbox %s: %v (code: %s)\n", what, drafts.ErrRulesUnreadable, drafts.ErrRulesUnreadable.Code)
		return outboxGateResult{code: cli.ExitUndetermined}
	}

	body, berr := outboxLatestBody(o, id)
	if berr != nil {
		_ = o.SetState(id, drafts.StateReviewUndetermined, "this draft's text could not be read, so nothing was checked")
		fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
		fmt.Fprintf(env.Stderr, "omw outbox %s: %v (code: %s)\n", what, berr, hub.Code(berr))
		return outboxGateResult{code: cli.ExitUndetermined}
	}

	outcome := drafts.Check(outboxReviewer(env, cfg), rules.Text, body)
	if err := o.SetState(id, outcome.StateFor(), outcome.Reason); err != nil {
		fmt.Fprintf(env.Stderr, "omw outbox %s: the review ran and its result could not be recorded: %v\n", what, err)
		return outboxGateResult{code: cli.ExitUndetermined}
	}
	fmt.Fprintf(env.Stdout, "%s\n", outcome.Render())
	fmt.Fprintf(env.Stdout, "%s\n", o.StateOf(id).Render())
	switch outcome.Verdict {
	case drafts.VerdictPassed:
		return outboxGateResult{mayLeave: true, code: cli.Success}
	case drafts.VerdictRefused:
		fmt.Fprintf(env.Stderr, "omw outbox %s: your rules refused this draft; it is still in your outbox.\n", what)
		return outboxGateResult{code: cli.ExitFailure}
	default:
		// CRITERION 16: not a pass, not a refusal, and its own exit code.
		fmt.Fprintf(env.Stderr, "omw outbox %s: %v (code: %s)\n", what, drafts.ErrModelUnreachable, drafts.ErrModelUnreachable.Code)
		return outboxGateResult{code: cli.ExitUndetermined}
	}
}

// outboxLatestBody reads the draft's most recent revision, through the same version machinery Issue #11
// built, so an unreadable revision is undetermined here too rather than an empty body.
func outboxLatestBody(o *drafts.Outbox, id hub.NoteID) (string, error) {
	versions, err := o.Timeline(id, "")
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", hub.Refusedf(drafts.ErrNoSuchDraft, "%q has no revisions", string(id))
	}
	return versions[len(versions)-1].Body, nil
}

func outboxReview(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw outbox review: name exactly one draft.\n")
		return cli.ExitUsage
	}
	s, o, code, ok := outboxOpen(env, "review")
	if !ok {
		return code
	}
	id := hub.NoteID(args[0])
	if st := o.StateOf(id); st.Exists != tri.Yes {
		return outboxReportMissingDraft(env, "review", id, st)
	}
	res := outboxReviewGate(env, s, o, id, "review")
	if drafts.ReadMode(s).Mode != drafts.ModeReview && res.code == cli.Success {
		// Asked to review under a mode that has no review. Said, not silently passed.
		fmt.Fprintf(env.Stdout, "review: not run — the mode in effect does not review drafts\n")
	}
	return res.code
}

func outboxPublish(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw outbox publish: name exactly one draft.\n")
		return cli.ExitUsage
	}
	s, o, code, ok := outboxOpen(env, "publish")
	if !ok {
		return code
	}
	id := hub.NoteID(args[0])
	if st := o.StateOf(id); st.Exists != tri.Yes {
		return outboxReportMissingDraft(env, "publish", id, st)
	}

	// THE GATE FIRST, AND THE HUB AFTER (criterion 12). The check runs on this machine and does not
	// consult the hub, so a person with no hub still finds out what their rules think.
	res := outboxReviewGate(env, s, o, id, "publish")
	if !res.mayLeave {
		fmt.Fprintf(env.Stdout, "published: no — this draft is still in your outbox\n")
		return res.code
	}

	if !outboxHubIsConfigured(env) {
		// CRITERION 22: the transfer genuinely needs a hub, so this says precisely that and exits
		// non-zero. It does not half-work and it does not pretend.
		fmt.Fprintf(env.Stdout, "published: no — this draft is still in your outbox\n")
		fmt.Fprintf(env.Stderr, "omw outbox publish: %v (code: %s)\n", hub.ErrNoHubConfigured, hub.ErrNoHubConfigured.Code)
		fmt.Fprintf(env.Stderr, "  Publishing sends a note to a hub, and there is no hub configured.\n")
		return cli.ExitFailure
	}
	// THE BOUNDARY WITH ISSUE #10, SAID OUT LOUD. This capability decides whether a draft may
	// leave; the transfer that takes it is Issue #10 and is not in this build. Reporting success
	// here would be the client telling a person their note is out in the world when it is on their
	// disk — so the outcome is undetermined, with its own exit code, and the draft stays put.
	fmt.Fprintf(env.Stdout, "published: %s — the transfer out of the outbox is Issue #10 and this build has no transport\n", tri.Undetermined)
	fmt.Fprintf(env.Stdout, "your draft is still in your outbox and nothing has left this machine.\n")
	fmt.Fprintf(env.Stderr, "omw outbox publish: the gate passed and the transfer was not performed.\n")
	return cli.ExitUndetermined
}

func outboxReportMissingDraft(env cli.Env, what string, id hub.NoteID, st drafts.StateReport) int {
	fmt.Fprintf(env.Stdout, "%s\n", st.Render())
	if st.Exists == tri.No {
		fmt.Fprintf(env.Stderr, "omw outbox %s: there is no draft %q in your outbox.\n", what, string(id))
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stderr, "omw outbox %s: whether there is a draft %q %s.\n", what, string(id), tri.Undetermined)
	return cli.ExitUndetermined
}
