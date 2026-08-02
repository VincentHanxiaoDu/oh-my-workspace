package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// EnvHub names the hub. Empty means NO HUB CONFIGURED, and with no hub nothing reaches out
// (§4.2, criterion 15).
//
// It is spelled here rather than borrowed from another package's constant for the reason
// devices.EnvHub gives: one Issue's file should not appear in another's diff. The two are checked
// against each other by TestHubEnvNameMatchesTheOneDevicesReads, so the duplication cannot drift.
const EnvHub = "OMW_HUB"

// Query is everything one status screen needs from outside this package.
//
// THE DAEMON'S LIVENESS IS AN INPUT AND IS NOT WORKED OUT HERE (Issue #41). Whether a daemon is
// running against this store has exactly one definition, in internal/daemon, reached through
// package commands' daemonLiveness. Four surfaces once each made their own guess by stat'ing a
// path nothing ever set, and all four answered "not running" unconditionally. The status screen —
// the one surface whose entire job is to say whether things run — does not get to be the fifth.
type Query struct {
	// Getenv reads the environment. Nil reads nothing, which is the no-hub, no-overrides case.
	Getenv func(string) string
	// Now is this screen's instant, and the observation time stamped on every state this run
	// established for itself.
	Now time.Time
	// Daemon is whether the daemon is running, from the product's ONE answer, in three values.
	Daemon tri.Value
	// DaemonWhy is why that could not be established. Empty for a determined answer.
	DaemonWhy string
	// Report is the daemon's own report of itself — how its last run ended (criterion 2) and what
	// its control API did (criterion 17). It is passed in rather than fetched so that this package
	// makes no second call that could observe a different moment than the liveness answer did.
	//
	// ITS Running FIELD IS DELIBERATELY NOT READ. Daemon is the liveness answer; a second reading
	// of the same fact is a second chance to disagree with it.
	Report daemon.Report
	// Dial is the route to a hub. Nil means this build's real answer: there is no transport, so a
	// configured hub is UNREACHABLE — which is undetermined, and never a confident "not working".
	Dial devices.Dial
}

func (q Query) getenv() func(string) string {
	if q.Getenv != nil {
		return q.Getenv
	}
	return func(string) string { return "" }
}

func (q Query) now() time.Time {
	if q.Now.IsZero() {
		return time.Now().UTC()
	}
	return q.Now.UTC()
}

// Collect produces the whole screen.
//
// IT PRODUCES ALL SIX LINES ON EVERY PATH (criteria 1, 7, 14, 18). There is no early return in this
// function: each subsystem is asked separately, an unanswerable one becomes an undetermined line,
// and the next one is asked anyway. A dependency that is absent — no daemon, no store, no hub —
// costs exactly the facts that genuinely needed it and nothing else.
//
// IT MUTATES NOTHING (criteria 4, 16). It resolves paths, reads files and asks other packages for
// their snapshots. It does not create the store, does not start the daemon, and with no hub
// configured it never reaches [Query.Dial] at all.
func Collect(q Query) Screen {
	getenv, now := q.getenv(), q.now()

	root, rootErr := store.Resolve(getenv)
	var opened *store.Store
	var openErr error
	if rootErr == nil {
		opened, openErr = store.Open(root)
	}

	s := Screen{
		TakenAt: now,
		Subsystems: []Subsystem{
			daemonSubsystem(q, now),
			storeSubsystem(root, rootErr, opened, now),
			channelsSubsystem(opened, root, rootErr, openErr, now, q.Daemon),
			projectsSubsystem(opened, root, rootErr, openErr, getenv, now, q),
			devicesSubsystem(q, getenv, now),
			hubSubsystem(q, getenv, now),
		},
		// A POINTER, NOT AN IMPLEMENTATION. §3.9's other two bullets are separate Issues; status
		// says where they are and does not do their work.
		Pointers: []string{
			"for the deployment assumptions behind these answers, including disk encryption: omw health",
			"to hand this to whoever supports you: omw diagnostics",
		},
	}
	s.wire()
	return s
}

// daemonSubsystem is criteria 1, 2, 13 and 17.
//
// THREE FACTS SHARE THIS LINE AND THEY ARE SAID SEPARATELY: whether the daemon is running, how its
// last run ended, and what its control API did. Folding the last two into the first is how "the
// control API could not confirm its socket is owner-only" becomes "not running" — which is
// criterion 17's exact prohibition.
func daemonSubsystem(q Query, now time.Time) Subsystem {
	sub := Subsystem{Name: Daemon, State: fromTri(q.Daemon), ObservedAt: now}

	var b strings.Builder
	switch q.Daemon {
	case tri.Yes:
		b.WriteString("a daemon holds this store's lock and is running")
	case tri.No:
		// CRITERION 13 IN ONE SENTENCE. This is an answer that was delivered, not a failure to
		// deliver one, and the line says so where a person reads it. The invocation's exit code
		// says the same thing — see the command.
		b.WriteString("no daemon is running. This is an answer, not a failure to answer: " +
			"nothing has been started on your behalf (§4.2), and starting one is your decision — omw daemon start")
	default:
		why := q.DaemonWhy
		if why == "" {
			why = "no reason was recorded, which is itself a thing that could not be determined"
		}
		b.WriteString("whether a daemon is running " + tri.Undetermined.String() + ": " + why +
			". This is NOT a report that the daemon is stopped")
	}
	// CRITERION 2: how the last run ended, in its own sentence, from the daemon's own Ending.
	fmt.Fprintf(&b, "\n  last run: %s", q.Report.LastRun)
	if q.Report.LastRunDetail != "" {
		fmt.Fprintf(&b, " (%s)", q.Report.LastRunDetail)
	}
	// CRITERION 17: the control API's own state, including the case §4.6 names — it did not open
	// because owner-only access could not be confirmed. That is neither "not running" nor
	// "failing", and it gets its own words here.
	fmt.Fprintf(&b, "\n  control API: %s", q.Report.Control.Render(
		"open, and its socket was confirmed owner-only", "not open"))
	if q.Report.ControlDetail != "" {
		fmt.Fprintf(&b, " — %s", q.Report.ControlDetail)
	}
	sub.Detail = b.String()
	return sub
}

// storeSubsystem is criteria 14 and 16.
//
// The store's existence and its LOCATION are both facts establishable with no daemon, so both are
// reported with no daemon. A store that is not there is NOT CONFIGURED — a determined answer that
// reads as neither running nor failing — and nothing here creates one (§4.2).
func storeSubsystem(root string, rootErr error, opened *store.Store, now time.Time) Subsystem {
	sub := Subsystem{Name: Store, ObservedAt: now}
	if rootErr != nil {
		sub.State = Undetermined
		sub.Detail = "where this device's store lives could not be worked out: " + rootErr.Error()
		return sub
	}
	switch store.Exists(root) {
	case tri.Yes:
		sub.State = Working
		sub.Detail = "a store is present at " + root
		if opened != nil {
			if f := opened.SyncState(); f.Describe() != "" {
				sub.Detail += "\n  " + f.Describe()
			}
		}
	case tri.No:
		sub.State = NotConfigured
		sub.Detail = "no store exists at " + root + ". Nothing has created one: the store is made " +
			"by an explicit act (§4.2) — omw store create"
	default:
		sub.State = Undetermined
		sub.Detail = "a store may or may not be present at " + root + "; it could not be inspected"
	}
	return sub
}

// noStoreDetail is the one sentence for a subsystem whose facts live in a store this run could not
// open. Written once so that the channels line and the projects line cannot come to disagree about
// what "no readable store" means.
func noStoreDetail(what, root string, rootErr, openErr error) string {
	switch {
	case rootErr != nil:
		return "your " + what + " could not be read because where this device's store lives could " +
			"not be worked out: " + rootErr.Error()
	case openErr != nil:
		return "your " + what + " could not be read from the store at " + root + ": " + openErr.Error()
	default:
		return "your " + what + " could not be read and no reason was recorded"
	}
}

// channelsSubsystem is criteria 1, 5, 6 and 14.
//
// EVERY CHANNEL GETS ITS OWN ITEM WITH ITS OWN STATE, and the subsystem's state is the worst of
// them in a fixed precedence: an undetermined member outranks a failing one, because "I could not
// tell" must never be reported as the weaker, known fact. Channels are read from the store, so
// this line is fully answerable with no daemon running (criterion 14).
func channelsSubsystem(opened *store.Store, root string, rootErr, openErr error, now time.Time, running tri.Value) Subsystem {
	sub := Subsystem{Name: Channels, ObservedAt: now}
	if opened == nil {
		sub.State = Undetermined
		sub.Detail = noStoreDetail("channels", root, rootErr, openErr)
		return sub
	}
	list, err := channels.List(opened)
	if err != nil {
		sub.State = Undetermined
		sub.Detail = "the list of connected channels could not be read: " + err.Error()
		return sub
	}
	if len(list) == 0 {
		sub.State = NotConfigured
		sub.Detail = "no channels are connected. This is not a failure: nothing has been connected " +
			"on your behalf — omw channels connect"
		return sub
	}
	worst := Working
	for _, c := range list {
		h, why := c.Health(now)
		it := Item{Name: c.ID}
		switch h {
		case channels.HealthConnected:
			it.State, it.Detail = Working, "connected — "+why
		case channels.HealthCredentialExpired:
			it.State, it.Detail = NotWorking, "connected, and its credential has EXPIRED — "+why+
				"; sign in again to keep ingesting"
		case channels.HealthDisconnected:
			it.State, it.Detail = NotWorking, "not connected"
		default:
			it.State, it.Detail = Undetermined, "its connection state "+tri.Undetermined.String()+" — "+why
		}
		// CRITERION 6, FIRST OF THE THREE. An adapter the last attempt could not reach is
		// UNDETERMINED about that attempt, and its sentence says so in its own words — it is not
		// the sentence a disconnected channel gets, and it is not silence.
		if c.Last.Outcome == channels.OutcomeUnreachable {
			it.State = Undetermined
			it.Detail = "its adapter COULD NOT BE REACHED on the last attempt, so whether this " +
				"channel is working " + tri.Undetermined.String() + " — " + c.Last.RenderOutcome() +
				"; last successful ingestion: " + c.Last.RenderAsOf(running)
		} else {
			it.Detail += "; last successful ingestion: " + c.Last.RenderAsOf(running)
		}
		sub.Items = append(sub.Items, it)
		worst = worse(worst, it.State)
	}
	sub.State = worst
	sub.Detail = fmt.Sprintf("%d channel(s) are connected; each one's own state is below", len(list))
	return sub
}

// projectsSubsystem is criteria 1, 3, 5, 6 and 14.
//
// THE OBSERVATION TIME COMES FROM THE PROVENANCE, WHICH IS CRITERION 3. A state a daemon poll
// produced was observed when that poll ran, not now; a state this command walked was observed now.
// A polled state with no recorded poll time gets NO observation time — the line says it has none
// rather than being stamped with a substituted one.
func projectsSubsystem(opened *store.Store, root string, rootErr, openErr error, getenv func(string) string, now time.Time, q Query) Subsystem {
	sub := Subsystem{Name: Projects, ObservedAt: now}
	if opened == nil {
		sub.State = Undetermined
		sub.Detail = noStoreDetail("projects", root, rootErr, openErr)
		return sub
	}
	snap, err := projects.Take(opened, getenv, now, projects.Liveness{Running: q.Daemon, Detail: q.DaemonWhy})
	if err != nil {
		sub.State = Undetermined
		sub.Detail = "the list of watched projects could not be read: " + err.Error()
		return sub
	}
	if len(snap.Entries) == 0 {
		sub.State = NotConfigured
		sub.Detail = "no project directories have been added. Nothing is being watched because you " +
			"have pointed the client at nothing — omw projects add <dir>"
		return sub
	}

	worst := Working
	oldest := now
	anyPolled := false
	missingTime := false
	for _, e := range snap.Entries {
		it := Item{Name: e.Project.Path}
		switch {
		// CRITERION 6, SECOND OF THE THREE. A directory that has gone missing is its own finding
		// with its own sentence: the project is still listed, the state is about the DIRECTORY, and
		// the wording is not the one a subsystem confirmed not working gets.
		case e.State.Present == tri.No:
			it.State = NotWorking
			it.Detail = "the directory this project points at has GONE MISSING — it is still on " +
				"your list and there is nothing at " + e.Project.Path + " to watch"
		case e.State.Present == tri.Undetermined:
			it.State = Undetermined
			it.Detail = "whether this directory exists " + tri.Undetermined.String() +
				"; it has not been established to be missing"
		case e.State.Readable != tri.Yes:
			it.State = Undetermined
			it.Detail = "the directory is present and its contents " + tri.Undetermined.String() +
				" — " + projects.DescribeState(e.State)
		default:
			it.State = Working
			it.Detail = projects.DescribeState(e.State)
		}
		it.Detail += " [" + e.Provenance.String() + "]"
		if e.Provenance == projects.DaemonPolled {
			anyPolled = true
			if e.PolledAt.IsZero() {
				missingTime = true
			} else if e.PolledAt.Before(oldest) {
				oldest = e.PolledAt
			}
		}
		sub.Items = append(sub.Items, it)
		worst = worse(worst, it.State)
	}

	// The line's observation time is the OLDEST thing on it, because a line is only as fresh as its
	// stalest member. A polled state whose poll time was never recorded leaves the line with no
	// observation time at all rather than with a plausible-looking one.
	switch {
	case missingTime:
		sub.ObservedAt = time.Time{}
	case anyPolled:
		sub.ObservedAt = oldest
	}

	sub.State = worst
	sub.Detail = fmt.Sprintf("%d project(s) are on your list; watching: %s", len(snap.Entries),
		snap.Watching.Render("yes", "no — nothing is watching them right now"))
	if snap.WatchingDetail != "" {
		sub.Detail += " (" + snap.WatchingDetail + ")"
	}
	return sub
}

// devicesSubsystem is criteria 1, 5, 6 and 14.
//
// The registry is a file on this machine, so this line is answerable with no daemon and with no
// hub. What a hub would have added is named as missing rather than quietly left out (§4.4).
func devicesSubsystem(q Query, getenv func(string) string, now time.Time) Subsystem {
	sub := Subsystem{Name: Devices, ObservedAt: now}
	snap, err := devices.Load(devices.Query{
		Getenv: getenv, Now: now, Dial: q.Dial, Daemon: q.Daemon, DaemonWhy: q.DaemonWhy,
	})
	if err != nil {
		// AN UNREADABLE INVENTORY IS NOT AN EMPTY ONE, and it is not a broken registration either.
		sub.State = Undetermined
		sub.Detail = "your device registrations could not be read: " + err.Error()
		return sub
	}
	if len(snap.Devices) == 0 && snap.Complete != tri.Undetermined {
		sub.State = NotConfigured
		sub.Detail = "no devices are registered under your name — omw devices register <label>"
	} else {
		sub.State = Working
		sub.Detail = fmt.Sprintf("%d device(s) are registered; listing complete: %s",
			len(snap.Devices), snap.Complete.Render("yes", "no — this is only this machine's half"))
	}
	for _, m := range snap.Missing {
		sub.Detail += "\n  missing: " + m
	}
	if snap.Complete == tri.Undetermined {
		sub.State = Undetermined
	}

	worst := sub.State
	for _, d := range snap.Devices {
		it := Item{Name: string(d.Label)}
		switch d.CheckIn.State {
		case tri.Yes:
			it.State, it.Detail = Working, d.CheckIn.Describe()
		case tri.No:
			// CRITERION 6, THIRD OF THE THREE, AND §3.8 WORD FOR WORD: "a device that has not
			// checked in is a fact worth seeing, not an absence". It is registered — that part
			// worked — and it has never started. Its own sentence, shared with nothing else here.
			it.State = NotWorking
			it.Detail = "registered, and it has NEVER CHECKED IN — this machine is on your list " +
				"and has never been started. " + d.CheckIn.Describe()
		default:
			it.State = Undetermined
			it.Detail = "whether this device has checked in " + tri.Undetermined.String() +
				" — " + d.CheckIn.Describe()
		}
		sub.Items = append(sub.Items, it)
		worst = worse(worst, it.State)
	}
	// A never-checked-in device must not turn the REGISTRATION line into a failure — the
	// registration worked. Only an undetermined member clouds the line, because then the line
	// genuinely does not know what it is reporting.
	if worst == Undetermined {
		sub.State = Undetermined
	}
	return sub
}

// hubSubsystem is criteria 1, 15 and 18.
//
// WITH NO HUB CONFIGURED NOTHING IS DIALLED, and that is a property of the code's shape rather than
// of a check somebody has to keep correct: [Query.Dial] is unreachable from the branch below that
// finds the environment empty. A configured hub this build cannot talk to is UNDETERMINED and
// never a confident "not working" — and both are distinguishable from "not configured", which is
// criterion 15's three-way requirement.
func hubSubsystem(q Query, getenv func(string) string, now time.Time) Subsystem {
	sub := Subsystem{Name: Hub, ObservedAt: now}
	addr := strings.TrimSpace(getenv(EnvHub))
	if addr == "" {
		sub.State = NotConfigured
		sub.Detail = "no hub is configured, so nothing reached off this machine (§4.2). Every local " +
			"subsystem above is reporting its real state — the local half stands alone (§4.4). " +
			"Set $" + EnvHub + " to connect one"
		return sub
	}
	dial := q.Dial
	if dial == nil {
		dial = devices.NoTransport
	}
	src, err := dial(getenv)
	switch {
	case err != nil || src == nil:
		sub.State = Undetermined
		reason := "this build has no hub transport"
		if err != nil {
			reason = err.Error()
		}
		sub.Detail = "a hub is configured at " + addr + " and whether it is reachable " +
			tri.Undetermined.String() + ": " + reason + ". It has NOT been established to be down"
	default:
		sub.State = Working
		sub.Detail = "the hub at " + addr + " answered"
	}
	return sub
}

// worse folds a member's state into a subsystem's, in the precedence criterion 5 implies:
//
//	Undetermined  beats everything — a line containing something nobody could check does not get
//	              to report a confident state of its own
//	NotWorking    beats a working or unconfigured member
//	Working       and NotConfigured are the quiet ones
//
// The order is here, once, rather than at each subsystem, because three copies of a precedence are
// three chances for one of them to rank a failure above an unknown.
func worse(current, member State) State {
	if current == Undetermined || member == Undetermined {
		return Undetermined
	}
	if current == NotWorking || member == NotWorking {
		return NotWorking
	}
	if current == NotConfigured || member == NotConfigured {
		return NotConfigured
	}
	return Working
}
