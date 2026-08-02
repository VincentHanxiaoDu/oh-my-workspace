package devices

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

type fakeHub struct {
	devices []Device
	err     error
}

func (f fakeHub) Devices() ([]Device, error) { return f.devices, f.err }

func dialing(h Source, err error) Dial {
	return func(func(string) string) (Source, error) { return h, err }
}

func loadOrFail(t *testing.T, getenv func(string) string, dial Dial) Snapshot {
	t.Helper()
	return loadWithDaemon(t, getenv, dial, tri.No, "")
}

// loadWithDaemon is the same listing with the daemon's ONE answer supplied, which is how this
// package now learns it — see Query.Daemon.
func loadWithDaemon(t *testing.T, getenv func(string) string, dial Dial, live tri.Value, why string) Snapshot {
	t.Helper()
	s, err := Load(Query{
		Getenv: getenv, Now: time.Unix(1_700_000_000, 0), Dial: dial, Daemon: live, DaemonWhy: why,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

// CRITERION 12, AND THE SENTENCE THE BRIEF SINGLED OUT: a one-device list because there is no hub,
// and a genuine one-device list, must not render identically.
//
// Both listings contain exactly one device, with the same label and the same check-in state, so
// the ONLY thing that can distinguish them is the product saying what it does and does not know.
// That is the whole of PRD §4.4's "never returns a partial list presented as complete".
func TestAOneDeviceListWithNoHubDoesNotRenderLikeAGenuineOne(t *testing.T) {
	noHub, _ := sandbox(t, nil)
	r := mustRegistry(t, noHub)
	mustRegister(t, r, "laptop", "store-A")

	withHub, dir2 := sandbox(t, map[string]string{EnvHub: "https://hub.example"})
	_ = dir2
	r2 := mustRegistry(t, withHub)
	mustRegister(t, r2, "laptop", "store-A")

	partial := loadOrFail(t, noHub, nil)
	// The hub answers, and it reports the same single device — a genuinely complete one-device list.
	complete := loadOrFail(t, withHub, dialing(fakeHub{devices: []Device{{Label: "laptop", CheckIn: NeverCheckedIn(), Source: SourceHub}}}, nil))

	if len(partial.Devices) != 1 || len(complete.Devices) != 1 {
		t.Fatalf("this test only means something if both listings hold one device; got %d and %d",
			len(partial.Devices), len(complete.Devices))
	}
	if partial.Devices[0].Label != complete.Devices[0].Label {
		t.Fatalf("the two listings are not about the same device, so comparing them proves nothing")
	}
	if partial.Render() == complete.Render() {
		t.Fatalf("a listing that is only this machine's half renders identically to a complete one:\n%s", partial.Render())
	}
	if partial.Complete != tri.No {
		t.Errorf("with no hub the listing claims completeness %v, want a determined No", partial.Complete)
	}
	if len(partial.Missing) == 0 {
		t.Error("with no hub the listing says nothing about what is missing — §4.4 requires it be stated precisely")
	}
	if complete.Complete != tri.Yes {
		t.Errorf("with a hub that answered, completeness is %v, want Yes", complete.Complete)
	}
	if len(complete.Missing) != 0 {
		t.Errorf("a complete listing claims something is missing: %v", complete.Missing)
	}
}

// CRITERION 12's last sentence: an empty listing for a person with no devices must be
// distinguishable from a listing that could not be completed.
func TestAnEmptyInventoryDoesNotRenderLikeAListingThatFailed(t *testing.T) {
	withHub, _ := sandbox(t, map[string]string{EnvHub: "https://hub.example"})
	emptyAndComplete := loadOrFail(t, withHub, dialing(fakeHub{}, nil))

	noHub, _ := sandbox(t, nil)
	emptyBecausePartial := loadOrFail(t, noHub, nil)

	unreachable, _ := sandbox(t, map[string]string{EnvHub: "https://hub.example"})
	emptyBecauseUnreachable := loadOrFail(t, unreachable, dialing(nil, ErrHubUnreachable))

	for name, s := range map[string]Snapshot{
		"complete":    emptyAndComplete,
		"no hub":      emptyBecausePartial,
		"unreachable": emptyBecauseUnreachable,
	} {
		if len(s.Devices) != 0 {
			t.Fatalf("%s: this test needs all three listings empty, got %+v", name, s.Devices)
		}
	}
	renders := map[string]string{
		"complete":    emptyAndComplete.Render(),
		"no hub":      emptyBecausePartial.Render(),
		"unreachable": emptyBecauseUnreachable.Render(),
	}
	seen := map[string]string{}
	for name, got := range renders {
		if other, dup := seen[got]; dup {
			t.Errorf("the %s empty listing and the %s empty listing render identically:\n%s", name, other, got)
		}
		seen[got] = name
	}
	if emptyAndComplete.Complete != tri.Yes {
		t.Errorf("a genuinely empty inventory is reported as %v, want Yes", emptyAndComplete.Complete)
	}
	if emptyBecauseUnreachable.Complete != tri.Undetermined {
		t.Errorf("an unreachable hub gives completeness %v, want Undetermined", emptyBecauseUnreachable.Complete)
	}
	// AND THE THREE ANSWERS DO NOT SHARE A TERMINATION. "no hub, so I know this is partial" is a
	// determined fact; "the hub did not answer" is not. They must not collapse.
	if emptyBecausePartial.AnyUndetermined() {
		t.Error("a determined incompleteness is reporting itself as undetermined")
	}
	if !emptyBecauseUnreachable.AnyUndetermined() {
		t.Error("an unreachable hub is reporting itself as determined")
	}
}

// A hub that answers must not be able to erase, rename or merge this machine's devices.
func TestTheHubHalfNeverCollapsesTwoLabels(t *testing.T) {
	getenv, _ := sandbox(t, map[string]string{EnvHub: "https://hub.example"})
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")

	at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	s := loadOrFail(t, getenv, dialing(fakeHub{devices: []Device{
		{Label: "desktop", CheckIn: CheckedInAt(at)},
		{Label: "the-box", CheckIn: NeverCheckedIn()},
		{Label: "laptop", CheckIn: CheckedInAt(at)}, // the hub has seen the local machine check in
	}}, nil))

	labels := map[Label]int{}
	for _, d := range s.Devices {
		labels[d.Label]++
	}
	for _, want := range []Label{"laptop", "desktop", "the-box"} {
		if labels[want] != 1 {
			t.Errorf("label %q appears %d times in the merged listing, want exactly 1", want, labels[want])
		}
	}
	if len(s.Devices) != 3 {
		t.Fatalf("merging three names produced %d entries: %+v", len(s.Devices), s.Devices)
	}
	// The never-started machine the hub knows about survives the merge with its state intact.
	for _, d := range s.Devices {
		if d.Label == "the-box" && d.CheckIn.State != tri.No {
			t.Errorf("a never-started device came through the merge as %v", d.CheckIn.State)
		}
		if d.Label == "laptop" && d.CheckIn.State != tri.Yes {
			t.Errorf("the hub saw laptop check in and the merged entry says %v", d.CheckIn.State)
		}
	}
}

// CRITERION 10 AND 14, AS THIS PACKAGE NOW SEES THEM.
//
// This package no longer probes for a daemon — it is told, in three values, by the product's one
// answer (Issue #41). What it owes is that it renders all three, starts nothing, and lets an
// UNDETERMINED liveness reach the listing's completeness rather than passing as a stopped daemon.
func TestTheDaemonsThreeAnswersAreRenderedAndNothingIsStarted(t *testing.T) {
	getenv, dir := sandbox(t, map[string]string{EnvHub: "h"})
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")

	running := loadWithDaemon(t, getenv, dialing(fakeHub{}, nil), tri.Yes, "")
	stopped := loadWithDaemon(t, getenv, dialing(fakeHub{}, nil), tri.No, "")
	unknown := loadWithDaemon(t, getenv, dialing(fakeHub{}, nil), tri.Undetermined,
		"owner-only access to the control socket could not be confirmed")

	// The three render distinguishably, compared with each other rather than against wording.
	renders := map[string]string{"running": running.Render(), "stopped": stopped.Render(), "undetermined": unknown.Render()}
	seen := map[string]string{}
	for name, got := range renders {
		if other, dup := seen[got]; dup {
			t.Errorf("the %s daemon and the %s daemon render identically:\n%s", name, other, got)
		}
		seen[got] = name
	}

	// CRITERION 14. An undetermined liveness is §4.6's refusal reaching this listing, so the
	// listing must not present itself as whole, and must say why.
	if unknown.Complete != tri.Undetermined {
		t.Errorf("with the daemon's state undetermined the listing claims completeness %v, want Undetermined", unknown.Complete)
	}
	if !mentions(unknown.Missing, "owner-only") {
		t.Errorf("the listing does not carry the reason the daemon could not be established: %v", unknown.Missing)
	}

	// A DETERMINED "not running" is an established fact and must NOT demote the listing — the
	// inventory is a file, readable with no daemon at all. Without this, every machine with a
	// stopped daemon would report a listing it could not complete.
	if stopped.Complete != tri.Yes {
		t.Errorf("a stopped daemon made the listing incomplete (%v); the inventory needs no daemon", stopped.Complete)
	}
	if running.Complete != tri.Yes {
		t.Errorf("a running daemon gave completeness %v, want Yes", running.Complete)
	}

	// NOTHING WAS STARTED, AND NOTHING WAS WRITTEN. Listing is reading.
	entries, err := os.ReadDir(filepath.Join(dir, "omw"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Type()&os.ModeSocket != 0 {
			t.Errorf("listing devices left a socket at %s", e.Name())
		}
	}
}

// An undetermined liveness with no reason recorded must still not render as silence.
func TestAnUndeterminedDaemonWithNoReasonStillSaysSomething(t *testing.T) {
	getenv, _ := sandbox(t, map[string]string{EnvHub: "h"})
	s := loadWithDaemon(t, getenv, dialing(fakeHub{}, nil), tri.Undetermined, "")
	if len(s.Missing) == 0 {
		t.Fatal("an undetermined daemon produced no explanation at all")
	}
	for _, m := range s.Missing {
		if strings.TrimSpace(m) == "" {
			t.Error("an undetermined daemon produced a blank explanation — silence is not an answer")
		}
	}
}

func mentions(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// An inventory that cannot be read produces NO listing at all — the caller must not be handed an
// empty Snapshot it could print as "you have no devices".
func TestLoadRefusesRatherThanReturningAnEmptySnapshot(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")
	if err := os.WriteFile(r.Path(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(Query{Getenv: getenv, Now: time.Now()})
	if !errors.Is(err, ErrRegistryUnreadable) {
		t.Fatalf("Load over a damaged inventory gave %v, want ErrRegistryUnreadable", err)
	}
	if len(s.Devices) != 0 || s.Complete != tri.Undetermined {
		t.Errorf("Load returned a usable-looking snapshot alongside its error: %+v", s)
	}
}

// CRITERION 13: the control API's form and the CLI's form report the same devices and the same
// check-in states. One rendering function serves both, so the test asserts the two surfaces agree
// rather than asserting either against wording.
func TestTheControlFormAndTheTextFormAgree(t *testing.T) {
	getenv, _ := sandbox(t, map[string]string{EnvHub: "h"})
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")
	mustRegister(t, r, "never-started", "store-B")
	if err := r.RecordCheckIn("laptop", time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	s := loadOrFail(t, getenv, dialing(fakeHub{devices: []Device{{Label: "from-hub", CheckIn: NeverCheckedIn()}}}, nil))

	text := s.Render()
	body, err := s.ControlJSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Devices) != 3 {
		t.Fatalf("this test needs three devices to be worth running, got %+v", s.Devices)
	}
	for _, d := range s.Devices {
		line := string(d.Label) + "  —  " + d.CheckIn.Describe()
		if !strings.Contains(text, line) {
			t.Errorf("the text listing does not carry %q:\n%s", line, text)
		}
		if !strings.Contains(body, `"label": "`+string(d.Label)+`"`) {
			t.Errorf("the control form is missing the device %q:\n%s", d.Label, body)
		}
		if !strings.Contains(body, `"check_in": "`+d.CheckIn.Describe()+`"`) {
			t.Errorf("the control form reports a different check-in for %q than the text does:\n%s", d.Label, body)
		}
	}
	// And neither surface invents a device the other does not have.
	if got := strings.Count(body, `"label": "`); got != len(s.Devices) {
		t.Errorf("the control form lists %d devices, the snapshot has %d", got, len(s.Devices))
	}
	if got := strings.Count(text, "  —  "); got != len(s.Devices) {
		t.Errorf("the text form lists %d devices, the snapshot has %d", got, len(s.Devices))
	}
}
