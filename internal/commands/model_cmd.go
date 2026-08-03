// Command `omw model` — Issue #18: choosing a model provider, supplying your own key, and being
// told plainly when neither has happened.
//
// # THE FAILURE THIS COMMAND EXISTS TO PREVENT
//
// PRD §3.13: "No model configured is not a broken client. Everything that does not need one keeps
// working, and everything that does says what is missing." The tempting shape for a command like
// this is two states — configured, or not — and it is wrong three times over:
//
//   - A person who chose a provider and has not yet supplied a key is in neither state. Told "no
//     model configured" they will choose the provider again; told "configured" they will wonder why
//     review does nothing. It is its own answer (criterion 3).
//   - A key file that cannot be read is not an absent key (criterion 15, §4.3). Rendering it as
//     "not configured" is the product claiming to know something it does not.
//   - "No model configured" is not an error. `omw model` on a fresh machine has ANSWERED, and it
//     exits zero saying so (criteria 8, 9). Only a state it could not establish exits 3.
//
// # A NEW FILE, AND ONLY A NEW FILE
//
// Several Issues edit package commands at once, so this adds a file rather than a branch to an
// existing one. What it does NOT keep to itself is the daemon's liveness — that has exactly one
// definition in this package (`liveness.go`, Issue #41) and this asks it, because a private second
// answer here is the defect that Issue exists to have removed.
//
// # NOTHING HERE OPENS A CONNECTION, AND NOTHING HERE STARTS THE DAEMON
//
// Criterion 16, PRD §4.2. Configuring, reading and clearing a model touch the environment and the
// store, and nothing else. The daemon's state is REPORTED on every subcommand and started by none
// of them, exactly as `omw outbox` does it.
package commands

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "model",
		Summary: "choose a model provider and supply your own key",
		Run:     runModel,
	})
}

func runModel(env cli.Env) int {
	sub, rest := "show", []string(nil)
	if len(env.Args) > 0 {
		sub, rest = env.Args[0], env.Args[1:]
	}
	switch sub {
	case "-h", "--help", "help":
		modelUsage(env.Stdout)
		return cli.Success
	}
	if code, ok := modelPreflight(env, sub); !ok {
		return code
	}
	switch sub {
	case "show":
		return modelShow(env, rest)
	case "use":
		return modelUse(env, rest)
	case "key":
		return modelKey(env, rest)
	case "clear":
		return modelClear(env, rest)
	case "providers":
		return modelProviders(env, rest)
	default:
		fmt.Fprintf(env.Stderr, "omw model: %q is not a model subcommand.\n", sub)
		modelUsage(env.Stderr)
		return cli.ExitUsage
	}
}

func modelUsage(w io.Writer) {
	fmt.Fprint(w, `omw model — the model that serves your review mode and your subscriptions

usage: omw model [subcommand]

  show                 which provider is chosen, and whether a credential is supplied (default)
  use <provider>       choose a provider. An explicit act; nothing else in omw does it for you
  key file <path>      name a file holding your credential. The PATH is recorded; the bytes never are
  clear                forget the recorded choice. Your environment and your files are yours
  providers            the providers this build can talk to

Your credential is never printed, never published, never sent to a hub, and never returned through
an API. omw does not store it: it is read from your environment or your own file when it is used.
Choosing a provider needs no hub and opens no connection.
`)
}

// modelPreflight applies the platform ruling and reports the daemon, in that order.
//
// THE DAEMON IS REPORTED AND NEVER REQUIRED (criteria 16 and 17). §4.2 says commands say so when
// the daemon is not running; §4.4 says the local half stands alone. Both are satisfied by saying it
// on stderr and carrying on: choosing a provider is a local act and a stopped daemon is no reason
// to refuse it. Making it fatal would be this capability half-working for want of something it
// does not need.
func modelPreflight(env cli.Env, what string) (int, bool) {
	// PRD §5.1: this build ships for macOS and Linux, and says so anywhere else rather than
	// half-attempting.
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		fmt.Fprintf(env.Stderr, "omw model %s: this build ships for macOS and Linux; this is %s.\n", what, runtime.GOOS)
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

// modelStore opens the store this invocation is about.
//
// THE THREE OUTCOMES ARE THREE. A store that does not exist is a determined fact and is NOT a
// failure for `show`: the person's environment is still their configuration and §4.4 says the local
// half works. A store that exists and cannot be read is undetermined, and `show` must not answer
// from the environment alone as though the recorded half had been consulted — so it says so and
// exits 3 (criterion 15).
//
// mustExist is true for the acts that record something, because recording a choice into a store
// that is not there is not something to do quietly.
func modelStore(env cli.Env, what string, mustExist bool) (*store.Store, int, bool) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw model %s: where your store lives %s.\n", what, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return nil, cli.ExitUndetermined, false
	}
	s, err := store.Open(path)
	switch {
	case err == nil:
		return s, cli.Success, true
	case errors.Is(err, store.ErrNotFound):
		if mustExist {
			fmt.Fprintf(env.Stderr, "omw model %s: there is no store at %s, and a model choice is recorded in your store.\n", what, path)
			fmt.Fprintf(env.Stderr, "  Nothing was written anywhere else. Run 'omw store create' to create one on purpose.\n")
			return nil, cli.ExitFailure, false
		}
		fmt.Fprintf(env.Stderr, "there is no store at %s, so nothing is recorded there; reading your environment only.\n", path)
		return nil, cli.Success, true
	default:
		fmt.Fprintf(env.Stderr, "omw model %s: the store at %s could not be read: %v\n", what, path, err)
		fmt.Fprintf(env.Stderr, "  An unreadable store is not one with no model recorded in it.\n")
		return nil, cli.ExitUndetermined, false
	}
}

// modelShow is criteria 1, 2, 3, 15 and 18's CLI half.
//
// IT RENDERS THROUGH THE VIEW, not through the Config. Criterion 18 asks that the CLI and the
// control API report the same state; the way to be wrong about that is two format strings that
// agreed on the day they were written, so there is one Render and both surfaces call it. The View
// also has nowhere to put a credential, so this path cannot print one even by accident.
func modelShow(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw model show: this takes no arguments.\n")
		return cli.ExitUsage
	}
	s, code, ok := modelStore(env, "show", false)
	if !ok {
		return code
	}
	cfg := model.Read(env.Getenv, s)
	view := model.ViewOn(s, extensionRegistry, cfg)
	fmt.Fprintf(env.Stdout, "%s\n", view.Render())

	// THERE IS NOTHING TO ADD HERE, AND THAT IS THE DESIGN. Whether this build has an adapter for
	// the chosen provider is part of the state and is carried in the View, so this surface prints
	// the View and stops. An earlier draft appended those lines here and only here, and criterion
	// 18's agreement test caught `omw daemon status` saying something different about the same
	// machine — see model.View.Adapter.
	switch cfg.Configured() {
	case tri.Yes, tri.No:
		// A DETERMINED NEGATIVE SUCCEEDS AT ANSWERING (criterion 8). Being told "no model is
		// configured" is an answer to the question that was asked, and a non-zero exit here would
		// make a fresh machine look broken — the exact reading §3.13 forbids. It is the attempt to
		// REVIEW without one that fails.
		return cli.Success
	default:
		fmt.Fprintf(env.Stderr, "omw model show: %v (code: %s)\n", model.ErrUndetermined, model.ErrUndetermined.Code)
		fmt.Fprintf(env.Stderr, "  This is NOT a report that no model is configured; nothing about it has been established.\n")
		return cli.ExitUndetermined
	}
}

// modelUse is criterion 1: an explicit act, by a person, on purpose.
//
// IT DOES NOT REFUSE A PROVIDER THIS BUILD HAS NO ADAPTER FOR. Choosing a provider and being able
// to talk to one are two capabilities and two Issues (§2.5, #21). A person configuring a machine
// ahead of installing an adapter has not made a mistake, and a refusal here would make the read-out
// unable to ever show criterion 3's "chosen, no credential" state on a build with an empty
// registry. It is said, clearly, and it is recorded.
func modelUse(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw model use: name exactly one provider.\n")
		return cli.ExitUsage
	}
	s, code, ok := modelStore(env, "use", true)
	if !ok {
		return code
	}
	if err := model.Use(s, args[0]); err != nil {
		fmt.Fprintf(env.Stderr, "omw model use: %v\n", err)
		return cli.ExitFailure
	}
	name := strings.TrimSpace(args[0])
	fmt.Fprintf(env.Stdout, "provider: %s is now your chosen provider.\n", name)
	if _, known := model.Lookup(name); !known {
		fmt.Fprintf(env.Stdout, "  this build has no adapter for %s; your choice is recorded and review cannot run yet.\n", name)
	}
	fmt.Fprintf(env.Stdout, "%s\n", model.ViewOn(s, extensionRegistry, model.Read(env.Getenv, s)).Render())
	return cli.Success
}

// modelKey records WHERE the person's credential lives. It never reads it and never stores it.
func modelKey(env cli.Env, args []string) int {
	if len(args) != 2 || args[0] != "file" {
		fmt.Fprintf(env.Stderr, "omw model key: usage is 'omw model key file <path>'.\n")
		fmt.Fprintf(env.Stderr, "  There is deliberately no 'omw model key <value>': a credential typed as an\n")
		fmt.Fprintf(env.Stderr, "  argument lands in your shell history, and omw does not take custody of keys.\n")
		return cli.ExitUsage
	}
	s, code, ok := modelStore(env, "key", true)
	if !ok {
		return code
	}
	if err := model.UseCredentialFile(s, args[1]); err != nil {
		fmt.Fprintf(env.Stderr, "omw model key file: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "credential file: %s\n", args[1])
	fmt.Fprintf(env.Stdout, "  The path is recorded. Its contents are not, and are read only when your model runs.\n")
	fmt.Fprintf(env.Stdout, "%s\n", model.ViewOn(s, extensionRegistry, model.Read(env.Getenv, s)).Render())
	return cli.Success
}

// modelClear forgets the recorded choice, and says precisely what it did NOT clear.
//
// A credential in $OMW_MODEL_KEY or in a file belongs to the person who put it there. Claiming to
// have cleared it would be a lie a person might rely on.
func modelClear(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw model clear: this takes no arguments.\n")
		return cli.ExitUsage
	}
	s, code, ok := modelStore(env, "clear", true)
	if !ok {
		return code
	}
	if err := model.Clear(s); err != nil {
		fmt.Fprintf(env.Stderr, "omw model clear: %v\n", err)
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "the recorded model choice on this device has been forgotten.\n")
	fmt.Fprintf(env.Stdout, "  $%s and $%s are yours and were not touched.\n", model.EnvProvider, model.EnvCredential)
	fmt.Fprintf(env.Stdout, "%s\n", model.ViewOn(s, extensionRegistry, model.Read(env.Getenv, s)).Render())
	return cli.Success
}

// modelProviders lists what this build can talk to.
//
// AN EMPTY LIST IS SAID, NOT LEFT BLANK. It is a real state of this build — providers are
// registered by Issue #21's mechanism, which has not landed — and an empty section reads like a
// rendering bug rather than an answer.
func modelProviders(env cli.Env, args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(env.Stderr, "omw model providers: this takes no arguments.\n")
		return cli.ExitUsage
	}
	names := model.Names()
	if len(names) == 0 {
		fmt.Fprintf(env.Stdout, "providers: this build registers none.\n")
		fmt.Fprintf(env.Stdout, "  You can still choose one with 'omw model use <provider>' and supply your key;\n")
		fmt.Fprintf(env.Stdout, "  what is missing is the adapter, not your configuration.\n")
		return cli.Success
	}
	fmt.Fprintf(env.Stdout, "providers:\n")
	for _, n := range names {
		fmt.Fprintf(env.Stdout, "  %s\n", n)
	}
	return cli.Success
}
