package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// EnvHub is the one environment name this package reads. It is spelled here rather than borrowed
// from another package's constants so that this Issue's file does not appear in another's diff.
//
// EnvHub names the hub. Empty means NO HUB CONFIGURED, and with no hub nothing reaches out
// (PRD §4.2) — so it is also the reason a listing cannot claim to be the person's whole inventory
// (PRD §4.4, criterion 12).
//
// THERE IS NO CONTROL-SOCKET NAME HERE, ON PURPOSE. Whether a daemon is running against a store has
// one definition and it lives in internal/daemon (Issue #41); the socket path is chosen by that
// package's socketFor, which falls back to a per-user runtime directory above the sun_path limit.
// A copy of that rule here would be wrong on the fallback path rather than merely duplicated, so
// this package does not derive, name or stat a socket. It is TOLD the answer — see Query.Daemon.
const EnvHub = "OMW_HUB"

// Source is a hub that can report the devices registered to this person.
//
// It is an interface with no transport behind it in this build. That is stated rather than hidden:
// a configured hub this build cannot talk to is UNREACHABLE, which is an undetermined listing and
// never a short one presented as whole.
type Source interface {
	Devices() ([]Device, error)
}

// ErrHubUnreachable is a configured hub that could not be talked to. It is NOT a hub that reported
// no devices, and the two never render alike (PRD §3.11's rule, read across to listing).
var ErrHubUnreachable = errors.New("the configured hub could not be reached")

// Dial is how a Snapshot reaches a hub.
//
// IT IS A PARAMETER AND NOT A PACKAGE-LEVEL VARIABLE. A hub connection this package could reach
// for on its own is a hub connection that could happen without a caller deciding to allow one, and
// PRD §4.2 says nothing implicit. Passing nil means this build's real answer: THERE IS NO
// TRANSPORT YET, so a configured hub is unreachable, which is an undetermined listing and never a
// short one presented as whole.
type Dial func(getenv func(string) string) (Source, error)

// NoTransport is the default Dial: this build has no hub transport, and says so.
func NoTransport(func(string) string) (Source, error) { return nil, ErrHubUnreachable }

// Query is everything one listing needs from outside this package.
//
// THE DAEMON'S STATE IS AN INPUT, NOT SOMETHING THIS PACKAGE WORKS OUT. Whether a daemon is running
// against a store has one definition, in internal/daemon, reached through package commands'
// daemonLiveness (Issue #41). Four surfaces once each stat'd a path named by an environment
// variable nothing ever set, so all four answered "not running" unconditionally. This package does
// not get to be the fifth: it is told the answer, in three values, and renders what it is told.
type Query struct {
	// Getenv reads the environment. Nil reads nothing, which is the no-hub case.
	Getenv func(string) string
	// Now is this listing's instant.
	Now time.Time
	// Dial is the route to a hub. Nil means this build's real answer: no transport.
	Dial Dial
	// Daemon is whether the daemon is running, from the product's ONE answer. Undetermined is a
	// real value here and is not a stopped daemon: it is carried into the listing's completeness,
	// because a listing that could not consult the daemon may not present itself as whole.
	Daemon tri.Value
	// DaemonWhy is why the daemon's state could not be established. Empty for a determined answer.
	DaemonWhy string
}

// Snapshot is one answer to "what machines are registered under my name", including how much of
// that answer this run was able to establish.
type Snapshot struct {
	// Devices is every device this run knows about, ordered by label. Every device, including one
	// never started (PRD §3.8).
	Devices []Device
	// Complete is whether this listing is the person's WHOLE inventory.
	//
	//   Yes           the hub answered and this is all of them
	//   No            it is known to be partial — with no hub configured, this machine's half is
	//                 all there is, and that is a determined fact, not an unknown one
	//   Undetermined  whether it is whole could not be worked out
	//
	// It is a tri and not a bool because "I know this is partial" and "I do not know whether this
	// is partial" are different facts and a person acts differently on them.
	Complete tri.Value
	// Missing says PRECISELY what is not in the listing and why (PRD §4.4). It is non-empty
	// whenever Complete is not Yes, and a Render that omitted it would be the partial-list-read-as-
	// complete that criterion 12 forbids.
	Missing []string
	// Daemon is whether the daemon is running, in three values. Looking is not starting: nothing
	// in this package starts anything (PRD §4.2, criterion 10).
	Daemon    tri.Value
	DaemonWhy string
}

// Load builds a snapshot: this machine's inventory, the hub's if there is one and it answers, and
// an honest account of what is missing.
//
// The registry failing to read is the one error returned, because then there is no listing at all
// and the caller must say so rather than print an empty one.
func Load(q Query) (Snapshot, error) {
	getenv := q.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	dial := q.Dial
	if dial == nil {
		dial = NoTransport
	}
	reg, err := Open(getenv)
	if err != nil {
		return Snapshot{}, err
	}
	local, err := reg.List()
	if err != nil {
		return Snapshot{}, err
	}

	snap := Snapshot{Devices: local, Complete: tri.Yes, Daemon: q.Daemon, DaemonWhy: q.DaemonWhy}

	// THE HUB HALF. With no hub configured nothing is dialled — there is no code path here that
	// reaches a transport when EnvHub is empty, which is criterion 11 as a structural property and
	// not a runtime check.
	if strings.TrimSpace(getenv(EnvHub)) == "" {
		snap.Complete = tri.No
		snap.Missing = append(snap.Missing,
			"no hub is configured, so any machine you registered on ANOTHER device is not in this "+
				"list. What is listed is this machine's inventory and nothing wider. Set $"+EnvHub+" to "+
				"see the devices registered under your name across all your machines.")
	} else {
		src, serr := dial(getenv)
		switch {
		case serr != nil || src == nil:
			snap.Complete = tri.Undetermined
			snap.Missing = append(snap.Missing, fmt.Sprintf(
				"a hub is configured and it could not be reached, so whether this list is all of "+
					"your devices %s: %v", tri.Undetermined, firstErr(serr)))
		default:
			remote, rerr := src.Devices()
			if rerr != nil {
				snap.Complete = tri.Undetermined
				snap.Missing = append(snap.Missing, fmt.Sprintf(
					"the hub did not answer with your devices, so whether this list is all of them %s: %v",
					tri.Undetermined, rerr))
			} else {
				snap.Devices = merge(local, remote)
			}
		}
	}

	// CRITERION 14, AND IT IS NOW THE CALLER'S ONE ANSWER RATHER THAN A SECOND GUESS.
	//
	// §4.6: the control API does not open unless it can confirm its socket is owner-only, and
	// Issue #41 put "could the daemon be established at all" in exactly one place. An UNDETERMINED
	// liveness is that refusal reaching this listing — owner-only could not be confirmed, the lock
	// could not be read, the store could not be resolved — and §4.3 will not let it read as a
	// stopped daemon or be quietly left out. A listing that could not consult the daemon is not a
	// listing that may present itself as whole.
	//
	// A DETERMINED "not running" does NOT demote the listing. That is an established fact, and the
	// inventory is a file this command reads without any daemon; saying otherwise would make every
	// machine with a stopped daemon report a listing it could not complete.
	if q.Daemon == tri.Undetermined {
		if snap.Complete == tri.Yes {
			snap.Complete = tri.Undetermined
		}
		why := strings.TrimSpace(q.DaemonWhy)
		if why == "" {
			why = "no reason was recorded, which is itself a thing that could not be determined"
		}
		snap.Missing = append(snap.Missing, "whether the daemon is running "+tri.Undetermined.String()+
			", so whatever it would add to this listing is not in it, and this listing is not "+
			"whatever a running daemon would have reported: "+why)
	}

	sort.Slice(snap.Devices, func(i, j int) bool { return snap.Devices[i].Label < snap.Devices[j].Label })
	return snap, nil
}

func firstErr(err error) error {
	if err == nil {
		return ErrHubUnreachable
	}
	return err
}

// merge combines this machine's inventory with the hub's.
//
// IT MERGES BY LABEL AND BY LABEL ONLY, and two different labels are never folded together —
// PRD §3.8, "devices are separate and are shown as separate", is a property of this function. The
// only thing taken from a hub entry for a label this machine already knows is a REAL check-in: a
// machine this device has never seen check in may well have checked in to the hub, and the hub
// knowing that is not a reason to overwrite anything else.
func merge(local, remote []Device) []Device {
	out := append([]Device(nil), local...)
	index := map[Label]int{}
	for i, d := range out {
		index[d.Label] = i
	}
	for _, r := range remote {
		i, seen := index[r.Label]
		if !seen {
			r.Source = SourceHub
			out = append(out, r)
			index[r.Label] = len(out) - 1
			continue
		}
		if r.CheckIn.State == tri.Yes && out[i].CheckIn.State != tri.Yes {
			out[i].CheckIn = r.CheckIn
		}
	}
	return out
}

// Determined reports whether this run could say everything it was asked. A listing that is not
// determined must not exit zero.
func (s Snapshot) Determined() bool {
	if s.Complete != tri.Yes {
		return false
	}
	for _, d := range s.Devices {
		if !d.CheckIn.State.Determined() {
			return false
		}
	}
	return true
}

// AnyUndetermined reports whether anything in this snapshot could not be worked out — as opposed
// to being determined to be incomplete. The two get different exit codes.
func (s Snapshot) AnyUndetermined() bool {
	if s.Complete == tri.Undetermined {
		return true
	}
	for _, d := range s.Devices {
		if !d.CheckIn.State.Determined() {
			return true
		}
	}
	return false
}

// Render is the listing a person reads.
//
// EVERY DEVICE GETS A LINE AND EVERY LINE CARRIES A CHECK-IN STATE (criterion 6). There is no
// branch that omits a device and no branch that renders a check-in as a blank field, because
// Describe has no such branch either.
func (s Snapshot) Render() string {
	var b strings.Builder
	if len(s.Devices) == 0 {
		// AN EMPTY LISTING IS SAID, NOT SHOWN AS NOTHING. And it is said differently depending on
		// whether it is a determined emptiness or a listing that could not be completed —
		// criterion 12's last sentence.
		if s.Complete == tri.Yes {
			b.WriteString("no devices: you have registered no machines. This listing is complete.\n")
		} else {
			b.WriteString("no devices are in this listing, and that is NOT the same as having none —\n")
			b.WriteString("this listing could not be completed. See what is missing, below.\n")
		}
	} else {
		fmt.Fprintf(&b, "devices (%d):\n", len(s.Devices))
		for _, d := range s.Devices {
			fmt.Fprintf(&b, "  %s\n", d.Render())
		}
	}
	fmt.Fprintf(&b, "listing complete: %s\n", s.Complete.Render(
		"yes — every machine registered under your name is above",
		"no — this is only part of your inventory"))
	for _, m := range s.Missing {
		fmt.Fprintf(&b, "  missing: %s\n", m)
	}
	fmt.Fprintf(&b, "daemon: %s\n", s.Daemon.Render("running", "not running"))
	if s.DaemonWhy != "" {
		fmt.Fprintf(&b, "  %s\n", s.DaemonWhy)
	}
	return b.String()
}

// controlDevice is one device as the control API reports it.
type controlDevice struct {
	Label string `json:"label"`
	// CheckInState is the three-valued answer as a word. It is derived from the same tri.Value the
	// text rendering uses.
	CheckInState string `json:"check_in_state"`
	// CheckIn is the SAME STRING the CLI prints, from the same Describe. Criterion 13 says the two
	// surfaces report the same state; one rendering function called by both is how that is true by
	// construction rather than by two implementations that happen to agree today.
	CheckIn string `json:"check_in"`
	Source  string `json:"source"`
}

type controlSnapshot struct {
	Devices   []controlDevice `json:"devices"`
	Complete  string          `json:"listing_complete"`
	Missing   []string        `json:"missing,omitempty"`
	Daemon    string          `json:"daemon"`
	DaemonWhy string          `json:"daemon_detail,omitempty"`
}

// CheckInWord is the three-valued check-in state as a single machine-readable word. The three are
// distinct constants and none of them is empty.
func CheckInWord(c CheckIn) string {
	switch c.State {
	case tri.Yes:
		return "checked_in"
	case tri.No:
		return "never_checked_in"
	default:
		return "undetermined"
	}
}

// ControlJSON is the control API's form of this exact snapshot (PRD §4.3: "the control API and the
// CLI report the same state").
func (s Snapshot) ControlJSON() (string, error) {
	cs := controlSnapshot{
		Devices:   make([]controlDevice, 0, len(s.Devices)),
		Complete:  s.Complete.Render("yes", "no"),
		Missing:   s.Missing,
		Daemon:    s.Daemon.Render("running", "not running"),
		DaemonWhy: s.DaemonWhy,
	}
	for _, d := range s.Devices {
		cs.Devices = append(cs.Devices, controlDevice{
			Label:        string(d.Label),
			CheckInState: CheckInWord(d.CheckIn),
			CheckIn:      d.CheckIn.Describe(),
			Source:       d.Source,
		})
	}
	body, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}
