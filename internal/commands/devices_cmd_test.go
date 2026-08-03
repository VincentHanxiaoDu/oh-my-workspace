package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// devicesEnv sandboxes the product's per-user directory. BOTH XDG_DATA_HOME and HOME are set,
// because the device pointer and this Issue's device inventory both resolve from XDG_DATA_HOME
// first and HOME second — setting one leaves the other live on the platform that uses it, and this
// Issue writes to that directory more than any other.
func devicesEnv(t *testing.T, extra map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"XDG_DATA_HOME": dir, "HOME": dir}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func runDevices2(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runDevices(cli.Env{
		Args:   args,
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string { return env[k] },
	})
	return code, out.String(), errb.String()
}

func registerVia(t *testing.T, env map[string]string, label, machine string) (int, string, string) {
	t.Helper()
	return runDevices2(t, env, "register", label, "--machine", machine)
}

// withFixedClock pins the clock so a registration's instant is not the wall clock.
func withFixedClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := devicesNow
	devicesNow = func() time.Time { return at }
	t.Cleanup(func() { devicesNow = prev })
}

// withHub makes the hub reachable and reporting what it is given. Without it this build has no hub
// transport, so this is the only way the genuinely-complete path is exercised at all rather than
// merely described in a comment.
func withHub(t *testing.T, list []devices.Device) {
	t.Helper()
	prev := devicesDial
	devicesDial = func(func(string) string) (devices.Source, error) { return fixedHub(list), nil }
	t.Cleanup(func() { devicesDial = prev })
}

type fixedHub []devices.Device

func (f fixedHub) Devices() ([]devices.Device, error) { return []devices.Device(f), nil }

// CRITERION 1: the one machine a person registered is listed, under its label.
func TestDevicesListsTheOneMachineRegistered(t *testing.T) {
	env := devicesEnv(t, nil)
	if code, _, errOut := registerVia(t, env, "laptop", "store-A"); code != cli.Success {
		t.Fatalf("register exited %d: %s", code, errOut)
	}
	_, out, _ := runDevices2(t, env, "list")
	if !strings.Contains(out, "laptop") {
		t.Fatalf("the registered machine is not in the listing:\n%s", out)
	}
}

// CRITERION 2: two machines, two entries, neither collapsed.
func TestDevicesShowsTwoMachinesAsTwoEntries(t *testing.T) {
	env := devicesEnv(t, nil)
	registerVia(t, env, "laptop", "store-A")
	registerVia(t, env, "desktop", "store-B")

	_, out, _ := runDevices2(t, env, "list", "--json")
	var got struct {
		Devices []struct {
			Label string `json:"label"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the listing is not the control API's form: %v\n%s", err, out)
	}
	if len(got.Devices) != 2 {
		t.Fatalf("two machines registered, %d listed:\n%s", len(got.Devices), out)
	}
	if got.Devices[0].Label == got.Devices[1].Label {
		t.Fatalf("both entries carry the same label — one entry is standing for both:\n%s", out)
	}
}

// CRITERION 4 AND 9, DRIVEN THROUGH THE REAL COMMAND, AND COMPARED PAIRWISE.
//
// Three devices in one listing: one checked in a moment ago, one checked in years ago, one
// registered and never started, and one whose state could not be read. Their four rendered
// check-in lines must be four different strings. No literal is asserted, so editing the wording of
// any one of them cannot make this pass while two of them mean the same thing to a reader.
func TestTheCheckInStatesInARealListingArePairwiseDistinct(t *testing.T) {
	env := devicesEnv(t, nil)
	withFixedClock(t, time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC))
	for label, machine := range map[string]string{
		"recent": "store-A", "ancient": "store-B", "never": "store-C", "unreadable": "store-D",
	} {
		if code, _, e := registerVia(t, env, label, machine); code != cli.Success {
			t.Fatalf("register %s exited %d: %s", label, code, e)
		}
	}
	runDevices2(t, env, "check-in", "recent")
	withFixedClock(t, time.Date(2011, 1, 2, 3, 4, 5, 0, time.UTC))
	runDevices2(t, env, "check-in", "ancient")
	damageCheckIn(t, env, "unreadable")

	_, out, _ := runDevices2(t, env, "list", "--json")
	var got struct {
		Devices []struct {
			Label   string `json:"label"`
			State   string `json:"check_in_state"`
			CheckIn string `json:"check_in"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(got.Devices) != 4 {
		t.Fatalf("four devices were registered and %d are listed:\n%s", len(got.Devices), out)
	}
	seen := map[string]string{}
	for _, d := range got.Devices {
		if strings.TrimSpace(d.CheckIn) == "" {
			t.Errorf("%s has a blank check-in — the entry is in the listing and says nothing", d.Label)
		}
		if other, dup := seen[d.CheckIn]; dup {
			t.Errorf("%s and %s render the same check-in %q — two distinct states collapsed", d.Label, other, d.CheckIn)
		}
		seen[d.CheckIn] = d.Label
	}
	// DISTINCT STRINGS ARE NOT ENOUGH, and a driven mutation proved it: rendering the never-started
	// machine with the UNDETERMINED sentence and a different trailing reason kept every string
	// distinct and this test green, while telling a reader the product could not work out something
	// it knows for certain. So a determined answer must not wear the third answer's wording.
	third := tri.Undetermined.String()
	for _, d := range got.Devices {
		if d.State == "undetermined" {
			continue
		}
		if strings.Contains(d.CheckIn, third) || strings.Contains(d.CheckIn, "undetermined") {
			t.Errorf("%s is a DETERMINED state and renders as %q, which carries the third answer's wording", d.Label, d.CheckIn)
		}
	}

	// The never-started one and the unreadable one are the pair the Issue is most about.
	states := map[string]string{}
	for _, d := range got.Devices {
		states[d.Label] = d.State
	}
	if states["never"] == states["unreadable"] {
		t.Errorf("'never checked in' and 'could not be determined' share the state %q", states["never"])
	}
	if states["never"] == states["recent"] || states["recent"] != states["ancient"] {
		t.Errorf("the machine-readable states are wrong: %v", states)
	}
}

// damageCheckIn makes one device's recorded check-in unreadable, leaving every other entry intact.
func damageCheckIn(t *testing.T, env map[string]string, label string) {
	t.Helper()
	path, err := devices.RegistryPath(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Index(string(body), `"label": "`+label+`"`)
	if head < 0 {
		t.Fatalf("no entry for %q to damage:\n%s", label, body)
	}
	tail := strings.Replace(string(body)[head:], `"state": "never"`, `"state": "at",
        "at": "not-a-time"`, 1)
	if tail == string(body)[head:] {
		t.Fatalf("the entry for %q was not damaged; the test would prove nothing:\n%s", label, body)
	}
	if err := os.WriteFile(path, []byte(string(body)[:head]+tail), 0o600); err != nil {
		t.Fatal(err)
	}
}

// CRITERION 3 AND 6: the listing before a device's first check-in and after it both contain the
// device, and differ only in its state.
func TestTheNeverStartedBoxIsInTheListingBeforeAndAfterItStarts(t *testing.T) {
	env := devicesEnv(t, nil)
	withFixedClock(t, time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC))
	registerVia(t, env, "laptop", "store-A")
	registerVia(t, env, "the-box", "store-B")

	_, before, _ := runDevices2(t, env, "list")
	if !strings.Contains(before, "the-box") {
		t.Fatalf("the never-started machine is missing from the listing — PRD §3.8's exact prohibition:\n%s", before)
	}
	if code, _, e := runDevices2(t, env, "check-in", "the-box"); code != cli.Success {
		t.Fatalf("check-in exited %d: %s", code, e)
	}
	_, after, _ := runDevices2(t, env, "list")
	if !strings.Contains(after, "the-box") {
		t.Fatalf("after checking in the machine left the listing:\n%s", after)
	}
	if before == after {
		t.Fatal("the listing did not change when a never-started machine checked in")
	}
	// ONLY THAT DEVICE'S LINE MOVED.
	changed := changedLines(before, after)
	if len(changed) != 1 {
		t.Fatalf("a single check-in changed %d lines, want 1:\n%v", len(changed), changed)
	}
	if !strings.Contains(changed[0], "the-box") {
		t.Errorf("the line that changed is not the checked-in device's: %q", changed[0])
	}
}

func changedLines(before, after string) []string {
	b := strings.Split(before, "\n")
	a := strings.Split(after, "\n")
	var out []string
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			out = append(out, a[i])
		}
	}
	for i := len(a); i < len(b); i++ {
		out = append(out, b[i])
	}
	return out
}

// CRITERION 5: registered-but-never-started and never-registered are different results, and the
// second is not reported as a device that exists.
func TestNeverCheckedInIsNotTheSameAnswerAsNeverRegistered(t *testing.T) {
	env := devicesEnv(t, nil)
	registerVia(t, env, "the-box", "store-B")

	regCode, regOut, regErr := runDevices2(t, env, "show", "the-box")
	missCode, missOut, missErr := runDevices2(t, env, "show", "a-label-nobody-registered")

	if regOut+regErr == missOut+missErr {
		t.Fatal("a registered machine that never started and a label nobody registered produce the same output")
	}
	if regCode == missCode {
		t.Errorf("both answers exit %d — the two are indistinguishable to a script", regCode)
	}
	if regCode != cli.Success {
		t.Errorf("asking about a registered, never-started machine exited %d; that is a real device", regCode)
	}
	if missCode == cli.Success {
		t.Error("a label nobody registered was answered as a device that exists")
	}
	if strings.Contains(missOut, "registered: yes") {
		t.Errorf("an unregistered label was reported as registered:\n%s", missOut)
	}
}

// CRITERION 7 AND 8: a duplicate label is refused, by exit code, with the inventory unchanged and
// the first machine keeping its registration.
func TestADuplicateLabelIsRefusedAndIsDistinguishableByExitCodeAlone(t *testing.T) {
	env := devicesEnv(t, nil)
	okCode, _, _ := registerVia(t, env, "laptop", "store-A")
	if okCode != cli.Success {
		t.Fatalf("the first registration exited %d", okCode)
	}
	beforeCode, before, _ := runDevices2(t, env, "list", "--json")

	dupCode, dupOut, _ := registerVia(t, env, "laptop", "store-B")
	if dupCode == cli.Success {
		t.Fatalf("registering a second machine under a taken label succeeded:\n%s", dupOut)
	}
	if dupCode == okCode {
		t.Errorf("a refused registration and a successful one both exit %d", dupCode)
	}

	afterCode, after, _ := runDevices2(t, env, "list", "--json")
	if before != after || beforeCode != afterCode {
		t.Errorf("the refused registration changed the inventory:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// The label still resolves to the FIRST machine, and the second inherited nothing.
	if !strings.Contains(after, "store-A") && !strings.Contains(after, "laptop") {
		t.Errorf("the first machine's registration did not survive:\n%s", after)
	}
	if strings.Contains(after, "store-B") {
		t.Errorf("the refused machine is in the inventory:\n%s", after)
	}
}

// CRITERION 12: no hub means the listing cannot claim to be whole, exits non-zero, and does not
// render like one that is.
func TestWithNoHubTheListingSaysWhatIsMissingAndExitsNonZero(t *testing.T) {
	noHub := devicesEnv(t, nil)
	registerVia(t, noHub, "laptop", "store-A")
	partialCode, partial, _ := runDevices2(t, noHub, "list")
	if partialCode == cli.Success {
		t.Errorf("a listing that is only this machine's half exited 0:\n%s", partial)
	}
	if !strings.Contains(partial, "missing:") {
		t.Errorf("the listing does not state what is missing (PRD §4.4):\n%s", partial)
	}

	hub := devicesEnv(t, map[string]string{"OMW_HUB": "https://hub.example"})
	registerVia(t, hub, "laptop", "store-A")
	withHub(t, []devices.Device{{Label: "laptop", CheckIn: devices.NeverCheckedIn(), Source: devices.SourceHub}})
	completeCode, complete, _ := runDevices2(t, hub, "list")
	if completeCode != cli.Success {
		t.Errorf("a complete one-device listing exited %d:\n%s", completeCode, complete)
	}
	if partial == complete {
		t.Errorf("the no-hub listing and the genuine one render identically:\n%s", partial)
	}
}

// An unreachable hub and a missing hub must not read alike: one is "I know this is partial", the
// other is "I could not tell". That is the project's standing rule, applied here.
//
// ISSUE #67 CHANGED WHERE THE DISTINCTION LIVES. It used to be exit 1 versus exit 3, and #67's UAT
// on `main` established that spending the FAILURE code on a listing that was produced makes every
// script treating non-zero as failure call a healthy inventory broken. Both now exit 3, and the
// distinction is asserted where it is actually read — the `listing complete:` line and the
// `missing:` line, which must not collapse into one another.
func TestKnownPartialAndCouldNotTellDoNotReadAlike(t *testing.T) {
	noHub := devicesEnv(t, nil)
	registerVia(t, noHub, "laptop", "store-A")
	knownPartial, knownOut, _ := runDevices2(t, noHub, "list")

	unreachable := devicesEnv(t, map[string]string{"OMW_HUB": "https://hub.example"})
	registerVia(t, unreachable, "laptop", "store-A")
	// devicesDial is left at this build's real default: no transport, so a configured hub is
	// unreachable.
	couldNotTell, couldNotOut, _ := runDevices2(t, unreachable, "list")

	if knownPartial == cli.Success || couldNotTell == cli.Success {
		t.Fatalf("one of the two incomplete listings exited 0 (%d, %d)", knownPartial, couldNotTell)
	}
	if knownPartial != cli.ExitUndetermined {
		t.Errorf("a no-hub listing exits %d, want ExitUndetermined (%d)", knownPartial, cli.ExitUndetermined)
	}
	if couldNotTell != cli.ExitUndetermined {
		t.Errorf("an unreachable hub exits %d, want ExitUndetermined (%d)", couldNotTell, cli.ExitUndetermined)
	}
	if knownOut == couldNotOut {
		t.Fatalf("'this list is known to be partial' and 'whether it is partial could not be determined' render identically:\n%s", knownOut)
	}
	if !strings.Contains(knownOut, "listing complete: no") {
		t.Errorf("the known-partial listing does not report a DETERMINED incompleteness:\n%s", knownOut)
	}
	if !strings.Contains(couldNotOut, "listing complete: "+tri.Undetermined.String()) {
		t.Errorf("the unreachable-hub listing does not report an UNDETERMINED completeness:\n%s", couldNotOut)
	}
}

// CRITERION 13: the CLI and the control API's form report the same labels and the same check-in
// states, for the same moment.
func TestTheCLIAndTheControlFormReportTheSameDevices(t *testing.T) {
	env := devicesEnv(t, nil)
	withFixedClock(t, time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC))
	registerVia(t, env, "laptop", "store-A")
	registerVia(t, env, "the-box", "store-B")
	runDevices2(t, env, "check-in", "laptop")

	_, text, _ := runDevices2(t, env, "list")
	_, body, _ := runDevices2(t, env, "list", "--json")
	var got struct {
		Devices []struct {
			Label   string `json:"label"`
			CheckIn string `json:"check_in"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if len(got.Devices) != 2 {
		t.Fatalf("the control form lists %d devices, two were registered:\n%s", len(got.Devices), body)
	}
	for _, d := range got.Devices {
		if !strings.Contains(text, d.Label) {
			t.Errorf("the control form has %q and the CLI listing does not:\n%s", d.Label, text)
		}
		if !strings.Contains(text, d.CheckIn) {
			t.Errorf("the two surfaces disagree about %q: the control form says %q, which is not in\n%s",
				d.Label, d.CheckIn, text)
		}
	}
	if strings.Count(text, "  —  ") != len(got.Devices) {
		t.Errorf("the CLI lists a different number of devices than the control form:\n%s\n%s", text, body)
	}
}

// PRD §4.2: nothing implicit. Listing devices creates no store, no inventory and no socket.
func TestListingDevicesCreatesNothing(t *testing.T) {
	env := devicesEnv(t, nil)
	dir := env["XDG_DATA_HOME"]
	before := treeSnapshot(t, dir)
	code, out, _ := runDevices2(t, env, "list")
	if code == cli.Success {
		t.Errorf("with no hub and no devices the listing exited 0:\n%s", out)
	}
	if got := treeSnapshot(t, dir); len(got) != len(before) {
		t.Errorf("listing devices wrote something: %v", got)
	}
}

// A check-in never registers a machine, and a person is told to register it on purpose.
func TestCheckInDoesNotRegisterADeviceImplicitly(t *testing.T) {
	env := devicesEnv(t, nil)
	code, _, errOut := runDevices2(t, env, "check-in", "never-registered")
	if code == cli.Success {
		t.Fatal("checking in an unregistered label succeeded")
	}
	if !strings.Contains(errOut, "register") {
		t.Errorf("the refusal does not point at the explicit act:\n%s", errOut)
	}
	_, out, _ := runDevices2(t, env, "list", "--json")
	if strings.Contains(out, "never-registered") {
		t.Errorf("a check-in registered a device implicitly:\n%s", out)
	}
}

// A label this build will not register is refused loudly and registers nothing. The uniqueness
// scope is settled by the Issue; the FORMAT is not, so these three are the only refusals and each
// is a case where accepting would break something already settled.
func TestARefusedLabelRegistersNothing(t *testing.T) {
	for _, label := range []string{"", "   ", "two\nlines"} {
		env := devicesEnv(t, nil)
		code, _, _ := registerVia(t, env, label, "store-A")
		if code == cli.Success {
			t.Errorf("the label %q was registered", label)
		}
		_, out, _ := runDevices2(t, env, "list", "--json")
		if strings.Contains(out, `"label"`) {
			t.Errorf("a refused label %q left something in the inventory:\n%s", label, out)
		}
	}
	// And a label that merely LOOKS awkward is accepted: this build does not invent a format.
	env := devicesEnv(t, nil)
	if code, _, e := registerVia(t, env, "Ada's MacBook Pro (2019) 💻", "store-A"); code != cli.Success {
		t.Errorf("a perfectly usable label was refused (exit %d): %s", code, e)
	}
	// Case is not folded: two labels differing only in case are two machines, not one.
	if code, _, _ := registerVia(t, env, "laptop", "store-B"); code != cli.Success {
		t.Fatalf("registering a second machine failed: %d", code)
	}
	if code, _, _ := registerVia(t, env, "LAPTOP", "store-C"); code != cli.Success {
		t.Error("\"LAPTOP\" was refused as a duplicate of \"laptop\" — this build folded case on the person's behalf")
	}
}

// CRITERION 10 AND 11, OBSERVED ON THE REAL BINARY.
//
// The daemon is not running before the command and is not running after it; the command does not
// hang; and with no hub configured the output is byte-identical whether or not anything outbound
// could work. The second half is driven by poisoning every proxy variable a Go program would
// honour and pointing the resolver at a black hole: if the command reached out, it would either
// change what it printed or take noticeably longer.
func TestDevicesListStartsNothingAndReachesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	build := exec.Command(goTool, "build", "-o", bin, "./cmd/omw")
	build.Dir = repoRoot(t)
	if out, berr := build.CombinedOutput(); berr != nil {
		t.Fatalf("building omw: %v\n%s", berr, out)
	}

	sandbox := t.TempDir()
	run := func(extra []string, args ...string) (int, string, *os.ProcessState) {
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// THE DEVICE POINTER AND THIS ISSUE'S INVENTORY MUST BOTH BE SANDBOXED. Both resolve from
		// XDG_DATA_HOME, else HOME, so BOTH are set: inheriting the developer's environment here
		// would rewrite their real device pointer to a t.TempDir() that is then deleted, and the
		// product would report no store while their tickets sat on disk unreferenced.
		cmd.Env = append(os.Environ(),
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox, "OMW_HUB=",
		)
		cmd.Env = append(cmd.Env, extra...)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out), cmd.ProcessState
	}

	// NOTHING IS RUNNING BEFORE.
	if leftovers := processGroupMembers(t, os.Getpid()); len(leftovers) > 0 {
		t.Logf("this test's own process group already holds %v; the after-check is per-command", leftovers)
	}

	start := time.Now()
	code, out, state := run(nil, "devices", "list")
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("omw devices list took %s — it is waiting for something", elapsed)
	}
	if code == 0 {
		t.Errorf("with no hub configured the listing claimed to be complete (exit 0):\n%s", out)
	}
	if !strings.Contains(out, "missing:") {
		t.Errorf("the listing does not say what is missing:\n%s", out)
	}
	// NOTHING IS RUNNING AFTER. The child had its own process group; a started daemon would be in it.
	for _, pid := range processGroupMembers(t, state.Pid()) {
		if pid != strconv.Itoa(state.Pid()) {
			t.Errorf("omw devices list left a process running in its group: pid %s", pid)
		}
	}

	// WITH OUTBOUND NETWORKING UNAVAILABLE, BYTE-FOR-BYTE THE SAME. 203.0.113.0/24 is reserved for
	// documentation and routes nowhere.
	poison := []string{
		"http_proxy=http://203.0.113.1:9", "https_proxy=http://203.0.113.1:9",
		"HTTP_PROXY=http://203.0.113.1:9", "HTTPS_PROXY=http://203.0.113.1:9",
		"ALL_PROXY=socks5://203.0.113.1:9", "no_proxy=", "NO_PROXY=",
	}
	start = time.Now()
	poisonedCode, poisoned, _ := run(poison, "devices", "list")
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("with networking poisoned the listing took %s — it tried to reach out", elapsed)
	}
	if poisonedCode != code || poisoned != out {
		t.Errorf("the listing differs when outbound networking is unavailable:\nwith network (%d):\n%s\nwithout (%d):\n%s",
			code, out, poisonedCode, poisoned)
	}

	// Registering behaves the same way: no network, nothing started.
	regCode, regOut, regState := run(poison, "devices", "register", "the-box", "--machine", "store-Z")
	if regCode != 0 {
		t.Fatalf("registering with networking poisoned exited %d:\n%s", regCode, regOut)
	}
	for _, pid := range processGroupMembers(t, regState.Pid()) {
		if pid != strconv.Itoa(regState.Pid()) {
			t.Errorf("omw devices register left a process running in its group: pid %s", pid)
		}
	}
	// And the machine it just registered is in the listing, saying it has never checked in.
	_, listOut, _ := run(nil, "devices", "list")
	if !strings.Contains(listOut, "the-box") {
		t.Errorf("the machine that was just registered is not in the listing:\n%s", listOut)
	}
	if !strings.Contains(listOut, devices.NeverCheckedIn().Describe()) {
		t.Errorf("the never-started machine's listing does not say it has never checked in:\n%s", listOut)
	}
}

// processGroupMembers reports the pids in a process group, PROBING for pgrep rather than assuming.
func processGroupMembers(t *testing.T, pgid int) []string {
	t.Helper()
	pgrep, err := exec.LookPath("pgrep")
	if err != nil {
		t.Log("pgrep is not on PATH; the leftover-process check was not run")
		return nil
	}
	out, _ := exec.Command(pgrep, "-g", strconv.Itoa(pgid)).Output()
	return strings.Fields(string(out))
}

// withDaemonLiveness drives a RENDERING with the daemon's answer forced.
//
// Stubbing is right here and wrong elsewhere. liveness.go says the agreement tests must NOT stub —
// a stub proves the rendering and only a started daemon proves the answer — and these are rendering
// tests: they assert what the listing SAYS when told each of the three, not what the answer is.
// TestTheDevicesListingUsesTheProductsRealLivenessAnswer below runs unstubbed.
func withDaemonLiveness(t *testing.T, live tri.Value, why string) {
	t.Helper()
	prev := daemonLiveness
	daemonLiveness = func(cli.Env) (tri.Value, string) { return live, why }
	t.Cleanup(func() { daemonLiveness = prev })
}

// CRITERION 14 AT THE CLI LAYER: where the daemon's state could not be established — which is
// where §4.6's "owner-only could not be confirmed" refusal arrives — the CLI says so and does not
// present the listing as complete.
func TestAnUnestablishableDaemonIsSaidAndTheListingIsNotCalledComplete(t *testing.T) {
	env := devicesEnv(t, map[string]string{"OMW_HUB": "https://hub.example"})
	registerVia(t, env, "laptop", "store-A")
	withHub(t, []devices.Device{{Label: "laptop", CheckIn: devices.NeverCheckedIn(), Source: devices.SourceHub}})

	withDaemonLiveness(t, tri.Yes, "")
	okCode, okOut, _ := runDevices2(t, env, "list")

	withDaemonLiveness(t, tri.Undetermined, "owner-only access to the control socket could not be confirmed")
	badCode, badOut, _ := runDevices2(t, env, "list")

	if okCode != cli.Success {
		t.Errorf("with a running daemon and a hub that answered, the listing exited %d:\n%s", okCode, okOut)
	}
	if badCode != cli.ExitUndetermined {
		t.Errorf("with the daemon unestablishable the listing exited %d, want ExitUndetermined (%d):\n%s",
			badCode, cli.ExitUndetermined, badOut)
	}
	if okOut == badOut {
		t.Errorf("a listing that could not consult the daemon renders exactly like one that could:\n%s", okOut)
	}
	if !strings.Contains(badOut, "owner-only") {
		t.Errorf("the listing does not say why the daemon could not be established:\n%s", badOut)
	}
	// AND IT IS NOT REPORTED AS A STOPPED DAEMON. That collapse is the defect #41 removed.
	withDaemonLiveness(t, tri.No, "")
	stoppedCode, stoppedOut, _ := runDevices2(t, env, "list")
	if stoppedOut == badOut {
		t.Error("an undetermined daemon renders identically to a stopped one")
	}
	if stoppedCode != cli.Success {
		t.Errorf("a determined 'not running' made the listing incomplete (exit %d); the inventory needs no daemon:\n%s",
			stoppedCode, stoppedOut)
	}
}

// The listing must go through the product's ONE liveness answer, unstubbed.
//
// WHY THIS EXISTS AS ITS OWN TEST. Every other devices test here runs with the real daemonLiveness,
// which is what makes their exit codes meaningful — but that is invisible, and a future change that
// made it answer Undetermined in a sandbox would silently reroute all of them onto the undetermined
// exit code while they carried on passing. This pins the assumption: in a sandbox with no daemon,
// the product's one answer is a DETERMINED negative, so a no-hub listing's exit 3 is the missing
// hub half speaking and not an unestablishable daemon wearing it.
func TestTheDevicesListingUsesTheProductsRealLivenessAnswer(t *testing.T) {
	env := devicesEnv(t, nil)
	live, why := daemonLiveness(cli.Env{Getenv: func(k string) string { return env[k] }})
	if live == tri.Undetermined {
		t.Skipf("the product cannot establish liveness in this sandbox (%s); the assumption below cannot be pinned here", why)
	}
	if live != tri.No {
		t.Fatalf("a sandbox with no daemon reports liveness %v, want a determined No", live)
	}
	registerVia(t, env, "laptop", "store-A")
	code, out, _ := runDevices2(t, env, "list")
	if code != cli.ExitUndetermined {
		t.Errorf("a no-hub listing with a determinedly-stopped daemon exited %d, want ExitUndetermined (%d):\n%s",
			code, cli.ExitUndetermined, out)
	}
	// The point this test pins is unchanged by #67: the DETERMINED negative from the product's one
	// liveness answer must be visible in the rendering, so a sandbox that silently started
	// answering Undetermined cannot pass as this case.
	if !strings.Contains(out, "daemon: not running") {
		t.Errorf("the listing does not report the determined 'not running' the check above pinned:\n%s", out)
	}
}

// THE ZERO INSTANT, DRIVEN FROM DISK, ACROSS ALL THREE SURFACES.
//
// Go's zero time.Time is year 0001 and RFC3339 encodes it happily, so
// `"check_in": {"state":"at","at":"0001-01-01T00:00:00Z"}` is a record the inventory can really
// hold. A check-in with no instant is not a check-in anyone can point at — it is a fact that could
// not be determined, and §4.3 says so in EVERY surface, not just the prose one.
//
// WHY THIS ASSERTS THREE THINGS AND NOT ONE. The defect it was written for rendered the undetermined
// WORDING while the exit code and the agent-facing field both said determined — the person and their
// script were told different things about the same device at the same moment. A test that checked
// only the sentence would have gone green against exactly that.
func TestACheckInWithNoInstantIsUndeterminedInEverySurface(t *testing.T) {
	env := devicesEnv(t, map[string]string{"OMW_HUB": "https://hub.example"})
	registerVia(t, env, "no-instant", "store-A")
	writeCheckInInstant(t, env, "no-instant", "0001-01-01T00:00:00Z")
	withHub(t, nil) // a hub that answers, so nothing else can make the listing undetermined

	// (a) the control API's machine-readable field
	listCode, listOut, _ := runDevices2(t, env, "list", "--json")
	var got struct {
		Devices []struct {
			Label   string `json:"label"`
			State   string `json:"check_in_state"`
			CheckIn string `json:"check_in"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(listOut), &got); err != nil {
		t.Fatalf("%v\n%s", err, listOut)
	}
	if len(got.Devices) != 1 {
		t.Fatalf("want the one device to be listed, got %d:\n%s", len(got.Devices), listOut)
	}
	d := got.Devices[0]
	if d.State != "undetermined" {
		t.Errorf("the control API says check_in_state=%q for a check-in with no instant, want undetermined", d.State)
	}
	// (b) the prose
	if !strings.Contains(d.CheckIn, tri.Undetermined.String()) {
		t.Errorf("the prose for a check-in with no instant is %q, which does not say it could not be determined", d.CheckIn)
	}
	// The two must AGREE — that is the whole defect.
	if (d.State == "undetermined") != strings.Contains(d.CheckIn, tri.Undetermined.String()) {
		t.Errorf("the prose and the machine-readable field disagree about the same device at the same moment: %q vs %q", d.CheckIn, d.State)
	}
	// (c) the exit codes
	if listCode != cli.ExitUndetermined {
		t.Errorf("list exited %d with an undetermined check-in and a hub that answered, want ExitUndetermined (%d):\n%s",
			listCode, cli.ExitUndetermined, listOut)
	}
	showCode, showOut, showErr := runDevices2(t, env, "show", "no-instant")
	if showCode != cli.ExitUndetermined {
		t.Errorf("show exited %d for a check-in with no instant, want ExitUndetermined (%d):\n%s%s",
			showCode, cli.ExitUndetermined, showOut, showErr)
	}
}

// writeCheckInInstant rewrites one device's recorded check-in to a literal instant, so a state the
// product would not itself write can still be driven from the disk a person's inventory really is.
func writeCheckInInstant(t *testing.T, env map[string]string, label, instant string) {
	t.Helper()
	rewriteCheckIn(t, env, label, `"state": "at",
        "at": "`+instant+`"`)
}

// rewriteCheckIn replaces one device's whole check-in object body, leaving every other entry intact.
func rewriteCheckIn(t *testing.T, env map[string]string, label, body string) {
	t.Helper()
	path, err := devices.RegistryPath(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Index(string(raw), `"label": "`+label+`"`)
	if head < 0 {
		t.Fatalf("no entry for %q to rewrite:\n%s", label, raw)
	}
	tail := strings.Replace(string(raw)[head:], `"state": "never"`, body, 1)
	if tail == string(raw)[head:] {
		t.Fatalf("the entry for %q was not rewritten; the test would prove nothing:\n%s", label, raw)
	}
	if err := os.WriteFile(path, []byte(string(raw)[:head]+tail), 0o600); err != nil {
		t.Fatal(err)
	}
}
