// Command `omw devices` — Issue #17, the person's own inventory of the machines carrying their
// store (PRD §3.8).
//
// THE EXIT CODES ARE THREE FACTS, NOT TWO. This command can end in three genuinely different
// places and the Issue asks for all three to be told apart without reading prose:
//
//	0  the listing is this person's whole inventory and every check-in state was determined
//	1  the listing is KNOWN to be partial (no hub configured), or a registration was refused
//	3  something could not be determined — a check-in state, the hub's reachability, or whether
//	   the control API's socket is owner-only
//
// 1 and 3 are the project's standing rule applied to a listing: "this list is missing your other
// machines and I know it" and "I could not tell whether this list is missing anything" are
// different answers, and a script must not have to parse a sentence to tell them apart.
//
// This file is the only file this Issue adds to package commands and it references nothing else in
// it, so it never appears in another Issue's diff.
package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "devices",
		Summary: "list every machine registered under your name, including one never started",
		Run:     runDevices,
	})
}

// devicesNow is the clock, replaced in tests so a registration's instant is not the wall clock.
var devicesNow = time.Now

// devicesDial is this command's route to a hub. THIS BUILD HAS NO HUB TRANSPORT, and it says so
// rather than pretending: a configured hub is reported as unreachable, which makes the listing
// undetermined and never a short list presented as whole. Tests replace it to drive the reachable
// hub, so the "genuinely complete listing" path is exercised and not merely described.
var devicesDial devices.Dial = devices.NoTransport

// devicesQuery assembles one listing's inputs, and it is where this command asks the product's ONE
// question about the daemon.
//
// IT CALLS daemonLiveness AND DERIVES NOTHING ITSELF (Issue #41). The socket path is chosen by
// internal/daemon's socketFor, which falls back to a per-user runtime directory whenever the
// in-store path would exceed the kernel's sun_path limit — so a second copy of that rule here would
// not be a duplicate, it would be WRONG on the fallback path, and this listing would disagree with
// `omw daemon status` about a daemon that is running. An earlier version of this file stat'd a path
// named by an environment variable nothing in the product ever set, which answered "not running"
// unconditionally; that is exactly the defect #41 removed, and it is deleted rather than updated.
//
// The three-valued answer is carried through unchanged, because it is the same rule this Issue is
// built on: a daemon whose state could not be established is a fact worth seeing, not an absence.
func devicesQuery(env cli.Env) devices.Query {
	live, why := daemonLiveness(env)
	return devices.Query{
		Getenv:    env.Getenv,
		Now:       devicesNow(),
		Dial:      devicesDial,
		Daemon:    live,
		DaemonWhy: why,
	}
}

// devicesMachine answers "which machine is this" — and it does not invent an identity.
//
// Issue #17 says each device has exactly one store, "so device identity and store identity are the
// same question asked twice". So this build uses the store id and mints nothing beside it. A
// machine with no store yet has no identity this build is willing to make up: registering there is
// refused as UNDETERMINED, with the two things a person can do about it.
var devicesMachine = func(env cli.Env) (devices.Machine, error) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		return "", fmt.Errorf("%w: where this device's store lives could not be worked out: %v",
			devices.ErrMachineUndetermined, err)
	}
	s, err := store.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w: this device's store at %s could not be opened: %v",
			devices.ErrMachineUndetermined, path, err)
	}
	return devices.Machine(s.ID()), nil
}

const devicesUsage = `usage: omw devices <list|show|register|check-in> [options]

  list                    every machine registered under your name, including one
                          that has never been started.
  show <label>            one machine, by the label it was registered under.
  register <label>        register a machine under a label. This is the only thing
                          that registers a device; nothing does it for you.
  check-in <label>        record that a registered machine has reported in.

options:
  --json                  the control API's form of the same answer.
  --machine <id>          register a machine other than this one, by its store id.
                          Without it, the machine registered is THIS one.
  -h, --help              print this and do nothing else.

A device that has been registered and never started is listed, and says so. That is
not an absence and not an error: it is a state worth seeing (PRD §3.8, §4.3).
`

type devicesFlags struct {
	json    bool
	machine string
	help    bool
	rest    []string
}

func parseDevicesFlags(args []string) (devicesFlags, error) {
	var f devicesFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			f.help = true
			return f, nil
		case a == "--json":
			f.json = true
		case a == "--machine":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--machine needs the machine's store id after it")
			}
			i++
			f.machine = args[i]
		case a == "--":
			f.rest = append(f.rest, args[i+1:]...)
			return f, nil
		case strings.HasPrefix(a, "-") && a != "-":
			// NAMED, NOT TREATED AS A LABEL. Silently registering a machine under a label called
			// "--force" is how a person ends up with an inventory entry they never meant.
			return f, fmt.Errorf("unknown option %q; it has NOT been treated as a label, and nothing was registered", a)
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f, nil
}

func runDevices(env cli.Env) int {
	if len(env.Args) == 0 {
		io.WriteString(env.Stderr, devicesUsage)
		return cli.ExitUsage
	}
	sub := env.Args[0]
	if sub == "-h" || sub == "--help" || sub == "help" {
		io.WriteString(env.Stdout, devicesUsage)
		return cli.Success
	}
	f, err := parseDevicesFlags(env.Args[1:])
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw devices %s: %v\n", sub, err)
		return cli.ExitUsage
	}
	if f.help {
		io.WriteString(env.Stdout, devicesUsage)
		return cli.Success
	}
	switch sub {
	case "list":
		return devicesList(env, f)
	case "show":
		return devicesShow(env, f)
	case "register":
		return devicesRegister(env, f)
	case "check-in":
		return devicesCheckIn(env, f)
	default:
		fmt.Fprintf(env.Stderr, "omw devices: unknown subcommand %q\n", sub)
		io.WriteString(env.Stderr, devicesUsage)
		return cli.ExitUsage
	}
}

// devicesList is criteria 1, 2, 3, 6, 10, 12 and 14.
//
// It reads a file and probes for a socket. It does not start the daemon, it does not create a
// store, it does not register anything, and with no hub configured it dials nothing.
func devicesList(env cli.Env, f devicesFlags) int {
	if len(f.rest) > 0 {
		fmt.Fprintf(env.Stderr, "omw devices list: takes no arguments, got %q\n", f.rest[0])
		return cli.ExitUsage
	}
	snap, err := devices.Load(devicesQuery(env))
	if err != nil {
		// AN UNREADABLE INVENTORY IS NOT AN EMPTY ONE. No listing is printed at all, because a
		// printed empty listing is exactly the false "you have no devices" this refuses to say.
		fmt.Fprintf(env.Stderr, "omw devices list: %v\n", err)
		fmt.Fprintf(env.Stderr, "  No listing has been printed: an inventory that cannot be read is not an empty one.\n")
		return cli.ExitFailure
	}
	if f.json {
		body, jerr := snap.ControlJSON()
		if jerr != nil {
			fmt.Fprintf(env.Stderr, "omw devices list: %v\n", jerr)
			return cli.ExitFailure
		}
		io.WriteString(env.Stdout, body)
	} else {
		io.WriteString(env.Stdout, snap.Render())
	}
	return devicesCode(snap)
}

// devicesCode is where the three endings are decided, and the order is the rule: an UNDETERMINED
// anything outranks a determined incompleteness, because "I could not tell" must never be scripted
// as the weaker, known fact.
func devicesCode(snap devices.Snapshot) int {
	if snap.AnyUndetermined() {
		return cli.ExitUndetermined
	}
	if snap.Complete != tri.Yes {
		return cli.ExitFailure
	}
	return cli.Success
}

// devicesShow is criterion 5: a registered-but-never-started label and a label that was never
// registered are different answers, and the second is never reported as a device that exists.
func devicesShow(env cli.Env, f devicesFlags) int {
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw devices show: name exactly one label.\n")
		return cli.ExitUsage
	}
	label := devices.Label(f.rest[0])
	snap, err := devices.Load(devicesQuery(env))
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw devices show: %v\n", err)
		return cli.ExitFailure
	}
	for _, d := range snap.Devices {
		if d.Label != label {
			continue
		}
		if f.json {
			one := snap
			one.Devices = []devices.Device{d}
			body, jerr := one.ControlJSON()
			if jerr != nil {
				fmt.Fprintf(env.Stderr, "omw devices show: %v\n", jerr)
				return cli.ExitFailure
			}
			io.WriteString(env.Stdout, body)
		} else {
			fmt.Fprintf(env.Stdout, "device: %s\n", d.Label)
			fmt.Fprintf(env.Stdout, "registered: yes\n")
			fmt.Fprintf(env.Stdout, "check-in: %s\n", d.CheckIn.Describe())
		}
		if !d.CheckIn.Determined() {
			return cli.ExitUndetermined
		}
		return cli.Success
	}
	// NOT A DEVICE. Not a device with an unknown state, not a blank entry: the label names nothing
	// that was ever registered, and that sentence is the answer (PRD §4.3).
	fmt.Fprintf(env.Stderr, "omw devices show: no device is registered under the label %q.\n", string(label))
	fmt.Fprintf(env.Stderr, "  This is NOT a registered machine whose check-in state is unknown, and it is NOT\n")
	fmt.Fprintf(env.Stderr, "  a machine that has never checked in. Nothing has been registered under this label.\n")
	if snap.Complete != tri.Yes {
		for _, m := range snap.Missing {
			fmt.Fprintf(env.Stderr, "  note: %s\n", m)
		}
	}
	return cli.ExitFailure
}

// devicesRegister is criteria 7 and 8, and PRD §4.2's "registration is an explicit act": this
// function runs only when a person typed `omw devices register`.
func devicesRegister(env cli.Env, f devicesFlags) int {
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw devices register: name exactly one label for the machine.\n")
		fmt.Fprintf(env.Stderr, "  Nothing is registered on your behalf and no label is guessed from this machine's name.\n")
		return cli.ExitUsage
	}
	label := devices.Label(f.rest[0])

	machine := devices.Machine(f.machine)
	if machine == "" {
		m, err := devicesMachine(env)
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw devices register: %v\n", err)
			fmt.Fprintf(env.Stderr, "  Nothing was registered. Either create this device's store on purpose with\n")
			fmt.Fprintf(env.Stderr, "  'omw store create', or name the machine you mean with --machine <store id>.\n")
			return cli.ExitUndetermined
		}
		machine = m
	}

	reg, err := devices.Open(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw devices register: %v\n", err)
		return cli.ExitUndetermined
	}
	d, err := reg.Register(label, machine, devicesNow())
	switch {
	case err == nil:
		fmt.Fprintf(env.Stdout, "registered: %s\n", d.Label)
		fmt.Fprintf(env.Stdout, "machine: %s\n", d.Machine)
		fmt.Fprintf(env.Stdout, "check-in: %s\n", d.CheckIn.Describe())
		fmt.Fprintf(env.Stdout, "It is in your device listing from now, and it will say it has never\n")
		fmt.Fprintf(env.Stdout, "checked in until it does.\n")
		return cli.Success

	case errors.Is(err, devices.ErrDuplicateLabel):
		// CRITERION 7. The first machine keeps the label; the second inherits nothing.
		fmt.Fprintf(env.Stderr, "omw devices register: refused — %v.\n", err)
		fmt.Fprintf(env.Stderr, "  A label is unique to you, so this machine has NOT been registered and has NOT\n")
		fmt.Fprintf(env.Stderr, "  taken over the other machine's registration. Nothing in your inventory changed.\n")
		fmt.Fprintf(env.Stderr, "  Pick another label and run this again.\n")
		return cli.ExitFailure

	case errors.Is(err, devices.ErrMachineAlreadyRegistered):
		fmt.Fprintf(env.Stderr, "omw devices register: refused — %v.\n", err)
		fmt.Fprintf(env.Stderr, "  One machine, one label. Nothing in your inventory changed.\n")
		return cli.ExitFailure

	case errors.Is(err, devices.ErrLabelRefused):
		fmt.Fprintf(env.Stderr, "omw devices register: %v.\n", err)
		fmt.Fprintf(env.Stderr, "  Nothing was registered.\n")
		return cli.ExitUsage

	case errors.Is(err, devices.ErrMachineUndetermined):
		fmt.Fprintf(env.Stderr, "omw devices register: %v.\n", err)
		return cli.ExitUndetermined

	default:
		fmt.Fprintf(env.Stderr, "omw devices register: %v\n", err)
		fmt.Fprintf(env.Stderr, "  Nothing was registered.\n")
		return cli.ExitFailure
	}
}

// devicesCheckIn records that a registered machine reported in. It never registers one: a check-in
// from a label nobody registered is refused, because registration is the explicit act (§4.2).
func devicesCheckIn(env cli.Env, f devicesFlags) int {
	if len(f.rest) != 1 {
		fmt.Fprintf(env.Stderr, "omw devices check-in: name exactly one label.\n")
		return cli.ExitUsage
	}
	reg, err := devices.Open(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw devices check-in: %v\n", err)
		return cli.ExitUndetermined
	}
	label := devices.Label(f.rest[0])
	now := devicesNow()
	if err := reg.RecordCheckIn(label, now); err != nil {
		fmt.Fprintf(env.Stderr, "omw devices check-in: %v\n", err)
		if errors.Is(err, devices.ErrNoSuchDevice) {
			fmt.Fprintf(env.Stderr, "  Checking in does not register a machine. Run 'omw devices register %s' on purpose first.\n", string(label))
		}
		return cli.ExitFailure
	}
	fmt.Fprintf(env.Stdout, "%s: %s\n", label, devices.CheckedInAt(now).Describe())
	return cli.Success
}
