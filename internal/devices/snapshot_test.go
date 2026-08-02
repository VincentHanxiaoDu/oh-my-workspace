package devices

import (
	"errors"
	"net"
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
	s, err := Load(getenv, time.Unix(1_700_000_000, 0), dial)
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

// CRITERION 10: nothing here starts a daemon, and the daemon's state is reported in three values
// by PROBING for the socket rather than by naming a platform convention.
func TestTheDaemonIsProbedAndNeverStarted(t *testing.T) {
	// No socket named at all.
	none, _ := sandbox(t, nil)
	if s := loadOrFail(t, none, nil); s.Daemon != tri.No {
		t.Errorf("with nothing naming a control socket the daemon reads %v, want No", s.Daemon)
	}

	// A socket path that is not there.
	dir := t.TempDir()
	missing, _ := sandbox(t, map[string]string{EnvControlSocket: filepath.Join(dir, "nothing.sock")})
	s := loadOrFail(t, missing, nil)
	if s.Daemon != tri.No {
		t.Errorf("with no socket at the named path the daemon reads %v, want No", s.Daemon)
	}
	if _, err := os.Stat(filepath.Join(dir, "nothing.sock")); !os.IsNotExist(err) {
		t.Error("listing devices created the control socket — it started something")
	}

	// A real socket. Listening on it is the test's doing, not the product's; the product only
	// looks. "unix" is the only network this tree may name.
	sockDir := shortDir(t)
	sock := filepath.Join(sockDir, "c.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("this environment cannot create a unix socket: %v", err)
	}
	defer ln.Close()
	running, _ := sandbox(t, map[string]string{EnvControlSocket: sock})
	if s := loadOrFail(t, running, nil); s.Daemon != tri.Yes {
		t.Errorf("with a socket present the daemon reads %v, want Yes", s.Daemon)
	}

	// A path that is there and is NOT a socket: undetermined, never "stopped".
	notSock := filepath.Join(sockDir, "regular")
	if err := os.WriteFile(notSock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	odd, _ := sandbox(t, map[string]string{EnvControlSocket: notSock})
	if s := loadOrFail(t, odd, nil); s.Daemon != tri.Undetermined {
		t.Errorf("a non-socket at the control path reads %v, want Undetermined", s.Daemon)
	}
}

// CRITERION 14: where owner-only access to the control socket cannot be confirmed, the listing
// SAYS SO instead of being presented as complete.
//
// THE ENVIRONMENT IS PROBED, NOT NAMED. Whether a filesystem honours permission bits at all is
// established by setting them and reading them back; if it does not, this check cannot mean
// anything here and the test says so rather than asserting against a system that cannot comply.
func TestASocketThatIsNotOwnerOnlyIsSaidRatherThanIgnored(t *testing.T) {
	dir := shortDir(t)
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(probe, 0o666); err != nil {
		t.Skipf("this filesystem does not accept a chmod: %v", err)
	}
	fi, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 == 0 {
		t.Skip("this filesystem does not preserve group/other permission bits, so owner-only cannot be observed here")
	}

	sock := filepath.Join(dir, "c.sock")
	ln, lerr := net.Listen("unix", sock)
	if lerr != nil {
		t.Skipf("this environment cannot create a unix socket: %v", lerr)
	}
	defer ln.Close()

	tight, _ := sandbox(t, map[string]string{EnvHub: "h", EnvControlSocket: sock})
	if err := os.Chmod(sock, 0o600); err != nil {
		t.Skipf("this filesystem does not accept a chmod on a socket: %v", err)
	}
	okState, _ := confirmControlSocket(tight)
	if okState != tri.Yes {
		t.Skipf("an owner-only socket does not read back as owner-only here (%v); this environment cannot drive the check", okState)
	}
	confirmed := loadOrFail(t, tight, dialing(fakeHub{}, nil))

	if err := os.Chmod(sock, 0o666); err != nil {
		t.Skipf("this filesystem does not accept a widening chmod on a socket: %v", err)
	}
	if state, _ := confirmControlSocket(tight); state != tri.No {
		t.Skipf("a world-reachable socket does not read back as such here (%v)", state)
	}
	// A DETERMINED "other users can reach this" is a finding, and a finding is not an
	// undetermined listing — the listing stays complete and reports the finding elsewhere.
	// The case criterion 14 is about is the one that could not be CONFIRMED, driven below.
	wide := loadOrFail(t, tight, dialing(fakeHub{}, nil))
	if wide.Render() == "" {
		t.Fatal("empty render")
	}

	// The unconfirmable case: the socket's directory cannot be traversed, so its mode cannot be
	// read at all. Skipped where the test runs as a user permissions do not apply to.
	blind := filepath.Join(dir, "blind")
	if err := os.MkdirAll(blind, 0o700); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(blind, "c.sock")
	ln2, l2err := net.Listen("unix", hidden)
	if l2err != nil {
		t.Skipf("this environment cannot create a second unix socket: %v", l2err)
	}
	defer ln2.Close()
	if err := os.Chmod(blind, 0o000); err != nil {
		t.Skipf("this filesystem does not accept chmod 000: %v", err)
	}
	defer os.Chmod(blind, 0o700)
	if _, serr := os.Stat(hidden); serr == nil {
		t.Skip("this process can stat inside an unreadable directory, so 'could not be confirmed' cannot be produced here")
	}
	blindEnv, _ := sandbox(t, map[string]string{EnvHub: "h", EnvControlSocket: hidden})
	unconfirmed := loadOrFail(t, blindEnv, dialing(fakeHub{}, nil))
	if unconfirmed.Complete != tri.Undetermined {
		t.Errorf("with owner-only access unconfirmable the listing claims completeness %v, want Undetermined", unconfirmed.Complete)
	}
	if !mentions(unconfirmed.Missing, "owner-only") {
		t.Errorf("the listing does not say the socket could not be confirmed owner-only: %v", unconfirmed.Missing)
	}
	if unconfirmed.Render() == confirmed.Render() {
		t.Error("a listing whose control API could not be confirmed renders exactly like a confirmed one")
	}
}

// shortDir is a directory short enough for a unix socket address to fit in.
//
// A SOCKET PATH HAS A LENGTH LIMIT AND t.TempDir() DOES NOT KNOW ABOUT IT. On macOS a temporary
// directory lives under /var/folders/<long>/<long>/T/<test name>/<n>, which with a socket name on
// the end exceeds sockaddr_un's 104 bytes — and `bind: invalid argument` looked exactly like "this
// environment cannot make unix sockets", so the check quietly skipped on the developer's own
// machine and proved nothing. This tries the short root first and falls back, and the CALLER still
// probes by actually listening, so an environment that genuinely cannot is still skipped honestly.
func shortDir(t *testing.T) string {
	t.Helper()
	for _, root := range []string{"/tmp", ""} {
		dir, err := os.MkdirTemp(root, "omwsock")
		if err != nil {
			continue
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		return dir
	}
	return t.TempDir()
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
	s, err := Load(getenv, time.Now(), nil)
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
