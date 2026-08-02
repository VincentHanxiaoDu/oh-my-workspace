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

const storeUsage = `usage: omw store <create|path|status> [path]

  create   create this device's store. Nothing else in omw ever creates one.
  path     print where the store lives, and whether one is there.
  status   report the store: present, readable, and whether its location synchronises.

The location comes from $` + store.PathEnv + ` if set, otherwise from a per-user data
directory. A path may be given to 'create' to override both.
`

func runStore(env cli.Env) int {
	if len(env.Args) == 0 {
		io.WriteString(env.Stderr, storeUsage)
		return cli.ExitUsage
	}
	switch env.Args[0] {
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

// resolveTarget picks the path to act on: an explicit argument, or the resolved per-device location.
//
// A location that cannot be worked out is ExitUndetermined, not ExitFailure. "I do not know where
// your store would live" is not "you have no store".
func resolveTarget(env cli.Env, args []string) (string, int) {
	if len(args) > 1 {
		io.WriteString(env.Stderr, storeUsage)
		return "", cli.ExitUsage
	}
	if len(args) == 1 {
		return args[0], cli.Success
	}
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw store: where the store lives %s.\n", tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  %v\n", err)
		fmt.Fprintf(env.Stderr, "  Set $%s to say where it should be, or pass a path.\n", store.PathEnv)
		return "", cli.ExitUndetermined
	}
	return path, cli.Success
}

func storeCreate(env cli.Env, args []string) int {
	target, code := resolveTarget(env, args)
	if code != cli.Success {
		return code
	}

	s, err := store.Create(target)
	if err == nil {
		// CRITERION 1: the absolute path it created, on success. CRITERION 9: this line is the
		// "confirmed local, created" rendering, and it must not be reachable by an undetermined
		// probe — which is why Create refuses that case rather than reporting it here.
		fmt.Fprintf(env.Stdout, "created the store at %s\n", s.Path())
		fmt.Fprintf(env.Stdout, "location: %s\n", s.SyncState().Describe())
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
		// THE OPEN DECISION, SAID OUT LOUD. Issue #3 asks whether an undetermined location may be
		// created in, and the PRD does not settle it. This build neither proceeds nor quietly
		// halts: it stops on its own exit code, says the probe did not conclude, and names the
		// decision as open. It must be impossible to mistake this for either settled outcome.
		fmt.Fprintf(env.Stderr, "omw store create: whether %s synchronises off this machine %s.\n", target, tri.Undetermined)
		fmt.Fprintf(env.Stderr, "  why: %s\n", detail)
		fmt.Fprintf(env.Stderr, "  This is NOT 'it does not synchronise' and NOT 'it does'. Nothing was created.\n")
		fmt.Fprintf(env.Stderr, "  The product has no ruling yet on whether creation should proceed here:\n")
		fmt.Fprintf(env.Stderr, "  Issue #3, 'Blocked on a decision', is open on exactly this question.\n")
		fmt.Fprintf(env.Stderr, "  Until it is ruled on, create the store somewhere this check can conclude.\n")
		return cli.ExitUndetermined

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
func storePath(env cli.Env, args []string) int {
	target, code := resolveTarget(env, args)
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
	target, code := resolveTarget(env, args)
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
