// The `omw store` command: the explicit act that brings a device's local store into being, and the
// two read-only questions about it (PRD §3.14, §4.1, §4.2, §4.3; Issue #3).
//
// WHY THE EXIT CODES ARE THE INTERFACE. Nearly every criterion on Issue #3 is stated as "exits
// non-zero", "distinguishable from the first-run success by exit code alone", or "distinguishable
// from both". Prose is for the person; the exit code is for the person's scripts, and the two must
// agree. This file's whole job is to turn the store package's distinct error values into distinct
// codes and distinct sentences, and never to collapse two of them into one.
//
// (Detached from the package clause on purpose: doc.go carries this package's doc comment, and this
// package is edited concurrently by several Issues — a second doc comment here would be a conflict
// in a file nobody needs to share.)

package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "store",
		Summary: "create the device's local store, or say where it is and what state it is in",
		Run:     runStore,
	})
}

// overrideFlag is the explicit act the product ruling allows for an undetermined location.
//
// ITS NAME IS THE POINT. It is not `--force` and not `--yes`, because it does not force anything and
// there is exactly one thing it accepts: a location whose sync status could not be determined. A
// person reading it in their shell history can tell what they agreed to. `--force` would read as
// "override the Dropbox refusal too", which criterion 24 forbids and this build will not do.
const overrideFlag = "--accept-undetermined-location"

const storeUsage = `usage: omw store <create|path|status> [options] [path]

  create   create this device's store. Nothing else in omw ever creates one.
  path     print where the store lives, and whether one is there.
  status   report the store: present, readable, and whether its location synchronises.

options for 'create':
  ` + overrideFlag + `
           create the store even though it could not be determined whether the
           location synchronises off this machine. This does NOT override the
           refusal for a location that is KNOWN to synchronise; nothing does.
  -h, --help
           print this and do nothing else.

The location comes from $` + store.PathEnv + `, else this device's registered store,
else a per-user data directory. A path may be given to override all three. Use --
before a path that begins with a dash.
`

// storeArgs is what a `omw store <sub>` invocation parsed to.
type storeArgs struct {
	path     string // empty means "wherever this device's store is"
	override bool
	help     bool
}

// parseStoreArgs reads the arguments, and REFUSES ANYTHING FLAG-SHAPED THAT IT DOES NOT KNOW.
//
// WHY THIS FUNCTION EXISTS. Before it, every argument was treated as a path, so `omw store create
// --help` created a store in a directory called `--help` and exited zero — while silently discarding
// the $OMW_STORE the person had set. Somebody typing `--help` is asking what the command does; they
// end up believing they created a store, at a path they never chose, while the store they wanted
// does not exist.
//
// That trap sits directly beside the override: the undetermined refusal tells a person they cannot
// proceed, and the next thing a person types is `--force` or `--yes`. Both must fail loudly and
// point at the real flag, never quietly become a directory name.
func parseStoreArgs(args []string) (storeArgs, error) {
	var out storeArgs
	seenPath := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// Everything after this is a path, even if it begins with a dash.
			for _, rest := range args[i+1:] {
				if seenPath {
					return out, fmt.Errorf("more than one path was given (%q and %q); a store has one location", out.path, rest)
				}
				out.path, seenPath = rest, true
			}
			return out, nil
		case a == "-h" || a == "--help":
			out.help = true
			return out, nil
		case a == overrideFlag:
			out.override = true
		case a == "--force" || a == "--yes" || a == "-f" || a == "-y":
			// NAMED, NOT GUESSED AT. These are what a person reaches for after the undetermined
			// refusal, so the error says what the real flag is and what it will and will not do.
			return out, fmt.Errorf("there is no %s flag.\n"+
				"  To create a store where the sync status COULD NOT BE DETERMINED, type:\n"+
				"    %s\n"+
				"  Nothing overrides the refusal for a location that is KNOWN to synchronise", a, overrideFlag)
		case strings.HasPrefix(a, "-") && a != "-":
			return out, fmt.Errorf("unknown option %q; run 'omw store --help' for the options this build has.\n"+
				"  It has NOT been treated as a path, and no store was created", a)
		default:
			if seenPath {
				return out, fmt.Errorf("more than one path was given (%q and %q); a store has one location", out.path, a)
			}
			out.path, seenPath = a, true
		}
	}
	return out, nil
}

func runStore(env cli.Env) int {
	if len(env.Args) == 0 {
		io.WriteString(env.Stderr, storeUsage)
		return cli.ExitUsage
	}
	switch env.Args[0] {
	case "-h", "--help":
		io.WriteString(env.Stdout, storeUsage)
		return cli.Success
	case "create":
		return storeCreate(env, env.Args[1:])
	case "path":
		return storePath(env, env.Args[1:])
	case "status":
		return storeStatus(env, env.Args[1:])
	default:
		fmt.Fprintf(env.Stderr, "omw store: unknown subcommand %q\n", env.Args[0])
		io.WriteString(env.Stderr, storeUsage)
		return cli.ExitUsage
	}
}

// resolveTarget picks the path to act on: the explicit one the person gave, or this device's store.
//
// A location that cannot be worked out is ExitUndetermined, not ExitFailure. "I do not know where
// your store would live" is not "you have no store".
func resolveTarget(env cli.Env, explicit string) (path string, fromPerson bool, code int) {
	if explicit != "" {
		return explicit, true, cli.Success
	}
	if p := env.Getenv(store.PathEnv); p != "" {
		fromPerson = true
	}
	resolved, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw store: where the store lives %s.\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		fmt.Fprintf(env.Stderr, "  Set $%s to say where it should be, or pass a path.\n", store.PathEnv)
		return "", fromPerson, cli.ExitUndetermined
	}
	return resolved, fromPerson, cli.Success
}

// readArgs parses and reports, so each subcommand does not repeat it.
func readArgs(env cli.Env, args []string) (storeArgs, int) {
	parsed, err := parseStoreArgs(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw store: %v\n", err)
		return parsed, cli.ExitUsage
	}
	if parsed.help {
		io.WriteString(env.Stdout, storeUsage)
		return parsed, -1 // handled; exit zero without doing anything else
	}
	return parsed, cli.Success
}

func storeCreate(env cli.Env, args []string) int {
	parsed, code := readArgs(env, args)
	if code == -1 {
		return cli.Success
	}
	if code != cli.Success {
		return code
	}
	target, fromPerson, code := resolveTarget(env, parsed.path)
	if code != cli.Success {
		return code
	}

	// THE STORE'S OWN CONTAINING DIRECTORY, WHEN IT IS THE PRODUCT'S TO MAKE.
	//
	// `~/Library/Application Support` exists on every Mac; `…/omw` never does until something makes
	// it. Without this, the default location is unreachable on exactly the machine the default
	// exists for, and the first thing a person meets is "the path does not exist — mkdir it
	// yourself". Making the store's own parent is part of the explicit act of creating a store, so
	// the command does it and SAYS it did.
	//
	// It does this ONLY for the location the product chose. A path the person typed, or set in
	// $OMW_STORE, is theirs: a missing parent there is a mistyped path, and conjuring it would
	// silently create a store somewhere they did not mean (criterion 6's "this path does not
	// exist" is a real and useful answer).
	if !fromPerson {
		parent := filepath.Dir(target)
		if _, err := os.Stat(parent); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(parent, 0o700); err != nil {
				fmt.Fprintf(env.Stderr, "omw store create: %s could not be created: %v\n", parent, err)
				return cli.ExitFailure
			}
			fmt.Fprintf(env.Stdout, "created the directory the store lives in: %s\n", parent)
		}
	}

	opts := []store.CreateOption{store.AsDeviceStore(env.Getenv)}
	if parsed.override {
		opts = append(opts, store.AcceptUndeterminedLocation())
	}

	s, err := store.Create(target, opts...)
	if err == nil {
		// CRITERION 1: the absolute path it created, on success.
		fmt.Fprintf(env.Stdout, "created the store at %s\n", s.Path())
		fmt.Fprintf(env.Stdout, "location: %s\n", s.SyncState().Describe())
		if s.CreatedAtUndeterminedLocation() {
			// CRITERION 25. An override is not a clean bill of health, and this success must not
			// read like the one at a confirmed non-synchronising path.
			fmt.Fprintf(env.Stdout, "You created this store with %s, so whether it stays\n", overrideFlag)
			fmt.Fprintf(env.Stdout, "on this machine is not something the product has been able to confirm.\n")
		}
		fmt.Fprintf(env.Stdout, "This store is the only home of your tickets and unpublished drafts.\n")
		return cli.Success
	}
	return reportCreateFailure(env, target, err)
}

// reportCreateFailure is where criterion 6 lives: each of these is a different sentence, and the
// three the criterion names — a synchronising location, a path that does not exist, and a
// permission wall — are three separate branches that cannot render as one another.
func reportCreateFailure(env cli.Env, target string, err error) int {
	var pe *store.PathError
	detail := ""
	if errors.As(err, &pe) {
		detail = pe.Detail
	}

	switch {
	case errors.Is(err, store.ErrAlreadyExists):
		fmt.Fprintf(env.Stderr, "omw store create: a store already exists at %s.\n", target)
		fmt.Fprintf(env.Stderr, "  It has not been overwritten, emptied or touched.\n")
		fmt.Fprintf(env.Stderr, "  One store per device is the design; 'omw store status' reports on it.\n")
		return cli.ExitFailure

	case errors.Is(err, store.ErrPathSynchronising):
		fmt.Fprintf(env.Stderr, "omw store create: refused — %s synchronises off this machine.\n", target)
		fmt.Fprintf(env.Stderr, "  detected: %s\n", detail)
		fmt.Fprintf(env.Stderr, "  The store holds tickets and drafts that have never been published.\n")
		fmt.Fprintf(env.Stderr, "  Somewhere that copies itself to another company's servers is not your disk,\n")
		fmt.Fprintf(env.Stderr, "  so put the store outside it and run this again.\n")
		return cli.ExitFailure

	case errors.Is(err, store.ErrSyncUndetermined):
		// THE RULING, IN ONE PLACE (Issue #3): halt, override available. The default is to stop,
		// and to say the state is UNDETERMINED rather than that the path synchronises — §4.3 —
		// while telling the person the exact thing they can type to proceed on purpose (§4.2).
		fmt.Fprintf(env.Stderr, "omw store create: whether %s synchronises off this machine %s.\n", target, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  why: %s\n", detail)
		fmt.Fprintf(env.Stderr, "  This is NOT 'it does not synchronise' and NOT 'it does'. Nothing was created.\n")
		fmt.Fprintf(env.Stderr, "  Nothing has been guessed on your behalf. If you know this location stays on\n")
		fmt.Fprintf(env.Stderr, "  this machine, say so explicitly and it will be created:\n")
		fmt.Fprintf(env.Stderr, "    omw store create %s%s\n", overrideFlag, pathArgument(target))
		fmt.Fprintf(env.Stderr, "  The store's location will still report as %s afterwards.\n", tri.Undetermined)
		return cli.ExitUndetermined

	case errors.Is(err, store.ErrAnotherStoreRegistered):
		// CRITERION 4. One store per device is not advice; a second store at another path splits
		// the sole home of unpublished data in two.
		fmt.Fprintf(env.Stderr, "omw store create: this device already has a store.\n")
		fmt.Fprintf(env.Stderr, "  %s\n", detail)
		fmt.Fprintf(env.Stderr, "  Nothing was created at %s. Run 'omw store path' to see the one you have.\n", target)
		return cli.ExitFailure

	case errors.Is(err, store.ErrPathMissing):
		fmt.Fprintf(env.Stderr, "omw store create: %s cannot be created because the path does not exist.\n", target)
		fmt.Fprintf(env.Stderr, "  %s\n", detail)
		fmt.Fprintf(env.Stderr, "  Make the containing directory yourself, or give a path that is already there.\n")
		return cli.ExitFailure

	case errors.Is(err, store.ErrPermissionDenied):
		fmt.Fprintf(env.Stderr, "omw store create: refused — you do not have permission to write at %s.\n", target)
		fmt.Fprintf(env.Stderr, "  %s\n", detail)
		return cli.ExitFailure

	case errors.Is(err, store.ErrPathUndetermined):
		fmt.Fprintf(env.Stderr, "omw store create: the store's location %s.\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		return cli.ExitUndetermined

	case errors.Is(err, store.ErrUnreadable):
		fmt.Fprintf(env.Stderr, "omw store create: something is at %s and it could not be read.\n", target)
		fmt.Fprintf(env.Stderr, "  %s\n", detail)
		fmt.Fprintf(env.Stderr, "  Nothing was created; an unreadable store is not an empty one.\n")
		return cli.ExitFailure

	default:
		fmt.Fprintf(env.Stderr, "omw store create: %v\n", err)
		return cli.ExitFailure
	}
}

// storePath answers criterion 15: the product exposes the store's path, so a person can see what is
// being backed up and what is not — and so criterion 14 is drivable at all.
//
// It prints the path even when no store is there, because "where would it go" is a useful and
// separate question from "is it there", and it renders the presence answer in three values.
// pathArgument renders the path back into the command line only when the person had to say it —
// suggesting a command with a path they never typed would be telling them to do something else.
func pathArgument(target string) string {
	return " -- " + target
}

func storePath(env cli.Env, args []string) int {
	parsed, code := readArgs(env, args)
	if code == -1 {
		return cli.Success
	}
	if code != cli.Success {
		return code
	}
	target, _, code := resolveTarget(env, parsed.path)
	if code != cli.Success {
		return code
	}
	fmt.Fprintln(env.Stdout, target)
	switch present := store.Exists(target); present {
	case tri.Yes:
		fmt.Fprintf(env.Stdout, "a store is present here\n")
		return cli.Success
	case tri.No:
		// NAMED, NOT SILENT (criterion 2). The absence of a store is the reason this command can
		// say no more, and it is a different fact from a store that is present and empty.
		fmt.Fprintf(env.Stdout, "no store is present here — nothing has created one, and this command will not\n")
		fmt.Fprintf(env.Stdout, "create one either. Run 'omw store create' to create it on purpose.\n")
		return cli.ExitFailure
	default:
		fmt.Fprintf(env.Stderr, "whether a store is present here %s\n", present)
		return cli.ExitUndetermined
	}
}

// storeStatus answers criterion 8 and criterion 16: for each thing that can be determined about the
// store — is it there, can it be read, does its location synchronise — three renderings, none of
// them silence, and an undetermined answer anywhere keeps the command off the success exit code.
func storeStatus(env cli.Env, args []string) int {
	parsed, code := readArgs(env, args)
	if code == -1 {
		return cli.Success
	}
	if code != cli.Success {
		return code
	}
	target, _, code := resolveTarget(env, parsed.path)
	if code != cli.Success {
		return code
	}
	fmt.Fprintf(env.Stdout, "path:     %s\n", target)

	s, err := store.Open(target)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(env.Stdout, "present:  %s\n", tri.No.Render("yes", "no — no store has been created on this device"))
		fmt.Fprintf(env.Stdout, "readable: %s\n", tri.Undetermined.Render("", ""))
		fmt.Fprintf(env.Stdout, "          there is nothing here to read; this is not an empty store\n")
		fmt.Fprintf(env.Stdout, "location: %s\n", store.DetectSync(target).Describe())
		return cli.ExitFailure

	case err != nil && errors.Is(err, store.ErrUnreadable), err != nil && errors.Is(err, store.ErrPermissionDenied):
		// CRITERION 13. Present, and unreadable. Never rendered as empty.
		fmt.Fprintf(env.Stdout, "present:  %s\n", tri.Yes.Render("yes", "no"))
		fmt.Fprintf(env.Stdout, "readable: %s\n", tri.No.Render("yes", "no — this store cannot be read"))
		fmt.Fprintf(env.Stderr, "omw store status: %v\n", err)
		return cli.ExitFailure

	case err != nil:
		fmt.Fprintf(env.Stdout, "present:  %s\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw store status: %v\n", err)
		return cli.ExitUndetermined
	}

	fmt.Fprintf(env.Stdout, "present:  %s\n", tri.Yes.Render("yes", "no"))
	fmt.Fprintf(env.Stdout, "readable: %s\n", tri.Yes.Render("yes", "no"))
	fmt.Fprintf(env.Stdout, "store id: %s\n", s.ID())
	if s.CreatedAtUndeterminedLocation() {
		// CRITERION 25: an override is remembered and shown, so this report can never be mistaken
		// for one about a store at a confirmed non-synchronising path.
		fmt.Fprintf(env.Stdout, "created:  with %s, at a location whose sync\n", overrideFlag)
		fmt.Fprintf(env.Stdout, "          status was never confirmed\n")
	}

	// CRITERION 8: refusal is not a one-time gate. The location is re-probed every time, because a
	// directory can be moved under a sync root long after the store was legitimately created.
	sync := s.SyncState()
	fmt.Fprintf(env.Stdout, "location: %s\n", sync.Describe())

	kinds, kerr := s.Kinds()
	if kerr != nil {
		fmt.Fprintf(env.Stdout, "records:  %s\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "omw store status: %v\n", kerr)
		return cli.ExitUndetermined
	}
	if len(kinds) == 0 {
		// EMPTY, AND SAID. An empty store is a determined answer and a different fact from an
		// absent one (criterion 2) — so it gets its own sentence rather than a blank section.
		fmt.Fprintf(env.Stdout, "records:  none yet — the store is empty\n")
	}
	for _, k := range kinds {
		recs, lerr := s.List(k)
		if lerr != nil {
			fmt.Fprintf(env.Stdout, "records:  %s for kind %q\n", tri.Undetermined, k)
			fmt.Fprintf(env.Stderr, "omw store status: %v\n", lerr)
			return cli.ExitFailure
		}
		fmt.Fprintf(env.Stdout, "records:  %d of kind %q\n", len(recs), k)
	}

	if sync.State == tri.Yes {
		fmt.Fprintf(env.Stderr, "omw store status: this store's location synchronises off this machine.\n")
		fmt.Fprintf(env.Stderr, "  Unpublished tickets and drafts are being copied off your disk.\n")
		return cli.ExitFailure
	}
	if !sync.State.Determined() {
		return cli.ExitUndetermined
	}
	return cli.Success
}
