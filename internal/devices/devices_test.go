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

// sandbox returns a getenv over a map, with the product's per-user directory pointed inside a
// t.TempDir(). BOTH XDG_DATA_HOME and HOME are set: store.productDir reads XDG_DATA_HOME first and
// falls back to HOME, so setting one leaves the other live on the platform that uses it.
func sandbox(t *testing.T, extra map[string]string) (func(string) string, string) {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"XDG_DATA_HOME": dir, "HOME": dir}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }, dir
}

func mustRegistry(t *testing.T, getenv func(string) string) *Registry {
	t.Helper()
	r, err := Open(getenv)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

func mustRegister(t *testing.T, r *Registry, label, machine string) Device {
	t.Helper()
	d, err := r.Register(Label(label), Machine(machine), time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("Register(%q, %q): %v", label, machine, err)
	}
	return d
}

// THE CRITERION 4 AND 9 TEST, AND IT COMPARES THE THREE TO EACH OTHER.
//
// Asserting each rendering against a string literal would pass just as happily after two of them
// were edited to the same wording — the test would be comparing the constants with a copy of
// themselves. What the Issue asks is that no two of the three collapse, for the SAME device label,
// so the three are compared pairwise, and to the empty string, which is the silence §4.3 forbids.
func TestTheThreeCheckInRenderingsArePairwiseDistinct(t *testing.T) {
	long := time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC)
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)

	renderings := map[string]string{
		"never checked in":    NeverCheckedIn().Describe(),
		"checked in long ago": CheckedInAt(long).Describe(),
		"checked in now":      CheckedInAt(now).Describe(),
		"undetermined":        UndeterminedCheckIn("the record could not be read").Describe(),
	}
	seen := map[string]string{}
	for name, got := range renderings {
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s renders as blank — silence is not one of the answers (PRD §4.3)", name)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s and %s both render as %q — two distinct device states collapsed into one", name, other, got)
		}
		seen[got] = name
	}
	// PAIRWISE DISTINCTNESS IS NOT ENOUGH ON ITS OWN, and this was found by driving the mutation:
	// making the never-checked-in branch return the undetermined SENTENCE, with a different
	// trailing reason, kept all four strings distinct and this test stayed green. A reader would
	// have been told "whether this machine ever checked in could not be determined" about a machine
	// the product knows for certain has never checked in — §4.3's collapse, in the direction nobody
	// checks for. So the determined answers must not wear the third answer's wording, and the third
	// answer must. tri.undeterminedText is the product's single wording for it, so this is one
	// constant, not a copy of one.
	third := tri.Undetermined.String()
	for name, got := range map[string]string{
		"never checked in":  NeverCheckedIn().Describe(),
		"checked in":        CheckedInAt(now).Describe(),
		"never (word)":      CheckInWord(NeverCheckedIn()),
		"checked in (word)": CheckInWord(CheckedInAt(now)),
	} {
		if strings.Contains(got, third) || strings.Contains(got, "undetermined") {
			t.Errorf("%s renders as %q, which carries the third answer's wording — a DETERMINED "+
				"answer is being shown as one the product could not work out", name, got)
		}
	}
	if !strings.Contains(UndeterminedCheckIn("the record could not be read").Describe(), third) {
		t.Errorf("the undetermined check-in does not carry the product's one wording for the third answer (%q)", third)
	}

	// The machine-readable word must keep the three apart too, or the control API collapses them
	// where the text does not.
	words := map[string]string{
		"never":        CheckInWord(NeverCheckedIn()),
		"checked in":   CheckInWord(CheckedInAt(now)),
		"undetermined": CheckInWord(UndeterminedCheckIn("x")),
	}
	seenWord := map[string]string{}
	for name, w := range words {
		if w == "" {
			t.Errorf("%s has an empty control-API word", name)
		}
		if other, dup := seenWord[w]; dup {
			t.Errorf("%s and %s share the control-API word %q", name, other, w)
		}
		seenWord[w] = name
	}
}

// The zero CheckIn must be Undetermined, not "never checked in". A struct field an error path
// never assigned must not read as a device the product is confident never started.
func TestZeroCheckInIsUndeterminedNotNever(t *testing.T) {
	var c CheckIn
	if c.State() != tri.Undetermined {
		t.Fatalf("the zero CheckIn is %v, want Undetermined", c.State())
	}
	if c.Describe() == NeverCheckedIn().Describe() {
		t.Fatal("an unset check-in renders as 'never checked in' — an answer nobody gave became a determined fact")
	}
}

// CRITERION 1: one registered machine is listed under the label it was registered with.
func TestOneRegisteredMachineIsListedUnderItsLabel(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")

	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d devices, want 1: %+v", len(list), list)
	}
	if list[0].Label != "laptop" {
		t.Errorf("the machine is listed as %q, not the label it was registered with", list[0].Label)
	}
}

// CRITERION 2: two labels are two entries, neither collapsed into the other.
func TestTwoLabelsAreTwoSeparateEntries(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")
	mustRegister(t, r, "desktop", "store-B")

	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("two machines were registered and %d are listed: %+v", len(list), list)
	}
	labels := map[Label]Machine{}
	for _, d := range list {
		if prev, dup := labels[d.Label]; dup {
			t.Fatalf("the label %q appears twice, for machines %q and %q", d.Label, prev, d.Machine)
		}
		labels[d.Label] = d.Machine
	}
	if labels["laptop"] == labels["desktop"] {
		t.Errorf("both entries point at the same machine %q — one entry is standing for both", labels["laptop"])
	}
	if labels["laptop"] != "store-A" || labels["desktop"] != "store-B" {
		t.Errorf("the entries do not carry their own machines: %+v", labels)
	}
}

// CRITERION 3 AND 6: a registration writes "never checked in" as a FACT, and a listing taken
// before the machine has ever checked in and one taken after differ ONLY in that device's state.
func TestListingsBeforeAndAfterAFirstCheckInDifferOnlyInThatDevicesState(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")
	mustRegister(t, r, "the-box-i-never-started", "store-B")

	before, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("want both devices listed before either checked in, got %+v", before)
	}
	var box Device
	for _, d := range before {
		if d.Label == "the-box-i-never-started" {
			box = d
		}
	}
	if box.Label == "" {
		t.Fatal("the never-started machine is NOT in the listing — PRD §3.8's exact prohibition")
	}
	if box.CheckIn.State() != tri.No {
		t.Fatalf("the never-started machine's check-in state is %v, want a determined 'never'", box.CheckIn.State())
	}
	if strings.TrimSpace(box.CheckIn.Describe()) == "" {
		t.Fatal("the never-started machine's check-in renders blank — the entry is present but silent")
	}

	at := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	if err := r.RecordCheckIn("the-box-i-never-started", at); err != nil {
		t.Fatal(err)
	}
	after, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a check-in changed how many devices are listed: %d then %d", len(before), len(after))
	}
	// ONLY THAT DEVICE'S STATE MOVED. Everything else — labels, machines, the other device's
	// check-in — must be identical, which is what "the two listings differ only in that device's
	// check-in state" means.
	for i := range before {
		if before[i].Label != after[i].Label || before[i].Machine != after[i].Machine {
			t.Errorf("entry %d changed identity across a check-in: %+v then %+v", i, before[i], after[i])
		}
		same := before[i].CheckIn.Describe() == after[i].CheckIn.Describe()
		if before[i].Label == "the-box-i-never-started" {
			if same {
				t.Errorf("the device checked in and its listing did not change: %q", after[i].CheckIn.Describe())
			}
			if after[i].CheckIn.State() != tri.Yes {
				t.Errorf("after checking in the state is %v, want Yes", after[i].CheckIn.State())
			}
		} else if !same {
			t.Errorf("a check-in on one device changed another device's state: %q -> %q",
				before[i].CheckIn.Describe(), after[i].CheckIn.Describe())
		}
	}
}

// CRITERION 5: a label that was never registered is not a device.
func TestNeverRegisteredIsNotADeviceWhoseStateIsUnknown(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")

	if _, err := r.Lookup("laptop"); err != nil {
		t.Fatalf("a registered label did not resolve: %v", err)
	}
	_, err := r.Lookup("a-label-nobody-registered")
	if !errors.Is(err, ErrNoSuchDevice) {
		t.Fatalf("looking up an unregistered label gave %v, want ErrNoSuchDevice", err)
	}
}

// CRITERION 7: a duplicate label is refused, the inventory is unchanged, and the first machine
// keeps its registration.
func TestADuplicateLabelIsRefusedAndTheFirstMachineKeepsIt(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")

	before, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	raw := readInventory(t, r)

	_, err = r.Register("laptop", "store-B", time.Unix(1_700_000_100, 0))
	if !errors.Is(err, ErrDuplicateLabel) {
		t.Fatalf("registering a second machine under a taken label gave %v, want ErrDuplicateLabel", err)
	}

	after, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("a refused registration changed the number of devices: %d then %d", len(before), len(after))
	}
	if got := readInventory(t, r); got != raw {
		t.Errorf("a refused registration rewrote the inventory file:\nbefore:\n%s\nafter:\n%s", raw, got)
	}
	d, lerr := r.Lookup("laptop")
	if lerr != nil {
		t.Fatalf("after the refusal the first machine's label no longer resolves: %v", lerr)
	}
	if d.Machine != "store-A" {
		t.Errorf("the label now resolves to %q — the second machine inherited the first's registration", d.Machine)
	}
}

// One machine, one label: registering the same machine again under a different name would put it
// in the inventory twice, and §3.8 says a machine is registered under A label.
func TestTheSameMachineCannotBeRegisteredTwice(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")
	_, err := r.Register("laptop-again", "store-A", time.Unix(1_700_000_100, 0))
	if !errors.Is(err, ErrMachineAlreadyRegistered) {
		t.Fatalf("re-registering one machine under a second label gave %v, want ErrMachineAlreadyRegistered", err)
	}
}

// A check-in never registers a machine. Registration is the explicit act (PRD §4.2).
func TestCheckingInDoesNotRegisterAMachine(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	if err := r.RecordCheckIn("never-registered", time.Now()); !errors.Is(err, ErrNoSuchDevice) {
		t.Fatalf("check-in for an unregistered label gave %v, want ErrNoSuchDevice", err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a check-in registered a device implicitly: %+v", list)
	}
}

// CRITERION 9: a check-in state that cannot be worked out is UNDETERMINED, the entry is still
// listed, and the three states remain distinct in one listing.
func TestAnUnreadableCheckInIsUndeterminedAndTheDeviceIsStillListed(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "checked-in", "store-A")
	mustRegister(t, r, "never-started", "store-B")
	mustRegister(t, r, "damaged", "store-C")
	if err := r.RecordCheckIn("checked-in", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// Damage exactly one entry's check-in instant, leaving the rest of the file intact.
	body := readInventory(t, r)
	head := strings.Index(body, `"label": "damaged"`)
	if head < 0 {
		t.Fatalf("the entry this test means to damage is not in the inventory:\n%s", body)
	}
	tail := strings.Replace(body[head:], `"state": "never"`, `"state": "at",
        "at": "not-a-time"`, 1)
	damaged := body[:head] + tail
	if damaged == body {
		t.Fatalf("the test did not manage to damage the record it meant to; the inventory is:\n%s", body)
	}
	if err := os.WriteFile(r.Path(), []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := r.List()
	if err != nil {
		t.Fatalf("one damaged check-in made the whole inventory unreadable: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("a device with an unreadable check-in was dropped from the listing: %+v", list)
	}
	byLabel := map[Label]Device{}
	for _, d := range list {
		byLabel[d.Label] = d
	}
	if got := byLabel["damaged"].CheckIn.State(); got != tri.Undetermined {
		t.Errorf("an unreadable check-in reads as %v, want Undetermined — 'could not determine' became a determined answer", got)
	}
	// Three-way distinct, in one real listing, for three real entries.
	three := []Device{byLabel["checked-in"], byLabel["never-started"], byLabel["damaged"]}
	seen := map[string]Label{}
	for _, d := range three {
		got := d.CheckIn.Describe()
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s renders a blank check-in", d.Label)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s and %s render the same check-in %q", d.Label, other, got)
		}
		seen[got] = d.Label
	}
}

// A damaged inventory is NOT an empty one. This is the "present thing rendered as absent" defect
// the project exists to remove, applied to the device list.
func TestADamagedInventoryIsAnErrorNotAnEmptyList(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "laptop", "store-A")
	if err := os.WriteFile(r.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := r.List()
	if !errors.Is(err, ErrRegistryUnreadable) {
		t.Fatalf("a damaged inventory gave (%+v, %v), want ErrRegistryUnreadable", list, err)
	}
	if list != nil {
		t.Errorf("a damaged inventory produced a list as well as an error: %+v", list)
	}
	// And it must not be silently replaced by a fresh one on the next registration.
	if _, rerr := r.Register("desktop", "store-B", time.Now()); !errors.Is(rerr, ErrRegistryUnreadable) {
		t.Errorf("registering over a damaged inventory gave %v — it would have deleted the devices already in it", rerr)
	}
}

// A person with no devices has an empty inventory, and that is not an error.
func TestNoInventoryYetIsAnEmptyListAndNotAnError(t *testing.T) {
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	list, err := r.List()
	if err != nil {
		t.Fatalf("a person who has registered nothing got an error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("an inventory that was never written listed %+v", list)
	}
	if _, serr := os.Stat(r.Path()); !os.IsNotExist(serr) {
		t.Errorf("listing devices created %s — reading is not writing", r.Path())
	}
}

func readInventory(t *testing.T, r *Registry) string {
	t.Helper()
	b, err := os.ReadFile(r.Path())
	if err != nil {
		t.Fatalf("reading the inventory: %v", err)
	}
	return string(b)
}

// The inventory lives beside the product's own per-user state, and NOT in the store.
func TestTheInventoryIsNotInTheStore(t *testing.T) {
	getenv, dir := sandbox(t, nil)
	p, err := RegistryPath(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, filepath.Join(dir, "omw")+string(filepath.Separator)) {
		t.Errorf("the inventory is at %s, outside the product directory under %s", p, dir)
	}
}

// THE TWO "CANNOT BE READ" BRANCHES OF decodeCheckIn, WHICH THE PACKAGE'S OWN DOC COMMENT CALLS THE
// POINT OF THE DESIGN AND WHICH NOTHING DROVE.
//
// Review found both surviving mutation: turning either into NeverCheckedIn() left every test green,
// so an unrecognised state word on disk — or a record with no check-in state at all — could have
// silently become "has never checked in". That is the §4.3 collapse this package exists to prevent,
// arriving through the one door nobody was watching.
//
// Each case is driven from a real inventory file, and each asserts the state, the prose and the
// machine-readable word together, so no single surface can carry the pass alone.
func TestARecordThisBuildCannotReadIsUndeterminedAndNeverNever(t *testing.T) {
	cases := map[string]string{
		"an unrecognised state word":      `"state": "wat"`,
		"a state word from a later build": `"state": "checked-in-via-hub"`,
		"no check-in state at all":        `"nothing": "here"`,
		"a null check-in object":          `"state": ""`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			getenv, _ := sandbox(t, nil)
			r := mustRegistry(t, getenv)
			mustRegister(t, r, "subject", "store-A")
			mustRegister(t, r, "control-never", "store-B")
			replaceCheckInBody(t, r, "subject", body)

			list, err := r.List()
			if err != nil {
				t.Fatalf("a record this build cannot read made the whole inventory unreadable: %v", err)
			}
			var subject, control Device
			for _, d := range list {
				switch d.Label {
				case "subject":
					subject = d
				case "control-never":
					control = d
				}
			}
			if subject.Label == "" {
				t.Fatalf("the device was dropped from the listing entirely: %+v", list)
			}
			if subject.CheckIn.State() == tri.No {
				t.Fatalf("a check-in this build cannot read became a determined 'has never checked in' — "+
					"'could not determine' turned into 'determined to be nothing' (%q)", subject.CheckIn.Describe())
			}
			if subject.CheckIn.State() != tri.Undetermined {
				t.Fatalf("a check-in this build cannot read reads as %v, want Undetermined", subject.CheckIn.State())
			}
			// The control is a genuine never-checked-in device in the SAME listing, so the two
			// cannot be telling the same story.
			if subject.CheckIn.Describe() == control.CheckIn.Describe() {
				t.Errorf("the unreadable record and a genuinely never-started machine render identically: %q",
					subject.CheckIn.Describe())
			}
			if CheckInWord(subject.CheckIn) == CheckInWord(control.CheckIn) {
				t.Errorf("the unreadable record and a never-started machine share the machine-readable word %q",
					CheckInWord(subject.CheckIn))
			}
			if strings.TrimSpace(subject.CheckIn.Why()) == "" {
				t.Error("an undetermined check-in carries no reason at all")
			}
		})
	}
}

// replaceCheckInBody swaps one device's whole check-in object for arbitrary JSON, so states this
// build would never itself write can still be driven from the disk an inventory really is.
func replaceCheckInBody(t *testing.T, r *Registry, label, body string) {
	t.Helper()
	raw := readInventory(t, r)
	head := strings.Index(raw, `"label": "`+label+`"`)
	if head < 0 {
		t.Fatalf("no entry for %q:\n%s", label, raw)
	}
	tail := strings.Replace(raw[head:], `"state": "never"`, body, 1)
	if tail == raw[head:] {
		t.Fatalf("the entry for %q was not rewritten; the test would prove nothing:\n%s", label, raw)
	}
	if err := os.WriteFile(r.Path(), []byte(raw[:head]+tail), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A CHECK-IN WITH NO INSTANT IS THE THIRD ANSWER, AT EVERY DOOR IT CAN COME IN THROUGH.
//
// This is the defect review found, guarded at the value rather than at a renderer. It used to be
// that Describe alone knew a zero-instant "yes" was really undetermined, so State, CheckInWord, the
// exit-code predicates and every future consumer disagreed with it — a person read "could not be
// determined" while their script read "checked in", about one device at one moment.
//
// The fix is that the VALUE carries it, so the test asserts the value, not the wording, and asserts
// it at all three ingresses. A test that only checked the sentence would have gone green against
// the original defect: the sentence was the one thing that was already right.
func TestACheckInWithNoInstantIsNeverAValidYes(t *testing.T) {
	// Ingress 1: the constructor. Nothing else in the package builds a CheckIn.
	got := CheckedInAt(time.Time{})
	if got.State() == tri.Yes {
		t.Fatalf("CheckedInAt(zero) built a 'yes' with no instant: %+v", got)
	}
	if got.State() != tri.Undetermined {
		t.Fatalf("CheckedInAt(zero) is %v, want Undetermined", got.State())
	}
	if got.Determined() {
		t.Error("a check-in with no instant reports itself as determined")
	}
	if CheckInWord(got) != CheckInWord(UndeterminedCheckIn("x")) {
		t.Errorf("the machine-readable word for a check-in with no instant is %q, and does not match the third answer's", CheckInWord(got))
	}
	// EVERY SURFACE AGREES. This is the property, stated once: whatever State says, the prose and
	// the machine-readable word say the same thing about the same value.
	for _, c := range []CheckIn{got, NeverCheckedIn(), CheckedInAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), UndeterminedCheckIn("y")} {
		saysUndetermined := strings.Contains(c.Describe(), tri.Undetermined.String())
		if saysUndetermined != (c.State() == tri.Undetermined) {
			t.Errorf("the prose and the state disagree: Describe()=%q, State()=%v", c.Describe(), c.State())
		}
		if (CheckInWord(c) == "undetermined") != (c.State() == tri.Undetermined) {
			t.Errorf("the machine-readable word and the state disagree: %q, State()=%v", CheckInWord(c), c.State())
		}
		if c.State() == tri.Yes && c.At().IsZero() {
			t.Errorf("a Yes with no instant exists: %q", c.Describe())
		}
	}

	// Ingress 2: the disk. Go's zero time is a real RFC3339 value an inventory can hold.
	getenv, _ := sandbox(t, nil)
	r := mustRegistry(t, getenv)
	mustRegister(t, r, "from-disk", "store-A")
	replaceCheckInBody(t, r, "from-disk", `"state": "at",
        "at": "0001-01-01T00:00:00Z"`)
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want one device, got %+v", list)
	}
	if list[0].CheckIn.State() == tri.Yes {
		t.Errorf("a zero instant on disk came back as a checked-in device: %q", list[0].CheckIn.Describe())
	}
	if list[0].CheckIn.State() != tri.Undetermined {
		t.Errorf("a zero instant on disk reads as %v, want Undetermined", list[0].CheckIn.State())
	}

	// Ingress 3: the hub. A Source is outside this package and its Devices are not this package's
	// to trust, so a hub reporting a check-in with no instant must not seed one either.
	hubbed, _ := sandbox(t, map[string]string{EnvHub: "h"})
	s := loadOrFail(t, hubbed, dialing(fakeHub{devices: []Device{
		{Label: "from-hub", CheckIn: CheckedInAt(time.Time{})},
	}}, nil))
	for _, d := range s.Devices {
		if d.CheckIn.State() == tri.Yes && d.CheckIn.At().IsZero() {
			t.Errorf("a hub seeded a checked-in device with no instant: %q", d.CheckIn.Describe())
		}
	}
	if !s.AnyUndetermined() {
		t.Error("a listing holding a check-in with no instant reports everything as determined")
	}
}
