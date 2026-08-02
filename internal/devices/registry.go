package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// registryName is the file holding this machine's copy of the person's device inventory.
//
// It sits beside store.RegistryPath's device-store pointer, in the product's own per-user
// directory, and NOT in the store. The store is the sole home of unpublished data (§3.14); a list
// of machine labels is neither a ticket nor a draft, and putting it there would mean a person with
// no store yet could not be told what devices they have registered.
const registryName = "devices.json"

// registryFormat is this file's layout version. A file this build does not understand is
// UNREADABLE, never an empty inventory.
const registryFormat = 1

// The three spellings of a check-in state on disk. They are written out in full rather than
// inferred from which fields are present, because "never checked in" is a fact the product
// recorded and must not be reconstructed from an absence (PRD §3.8).
const (
	diskCheckedIn    = "at"
	diskNeverStarted = "never"
)

type diskCheckIn struct {
	State string `json:"state"`
	At    string `json:"at,omitempty"`
	Why   string `json:"why,omitempty"`
}

type diskDevice struct {
	Label        string      `json:"label"`
	Machine      string      `json:"machine"`
	RegisteredAt string      `json:"registered_at"`
	CheckIn      diskCheckIn `json:"check_in"`
}

type diskFile struct {
	Format  int          `json:"format"`
	Devices []diskDevice `json:"devices"`
}

// Registry is this machine's copy of the person's device inventory.
type Registry struct {
	path string
}

// RegistryPath is where the inventory lives. It is derived from the store package's per-device
// pointer path so that there is one answer to "where does the product keep its own state", and so
// that a test which sandboxes XDG_DATA_HOME and HOME sandboxes this too.
func RegistryPath(getenv func(string) string) (string, error) {
	pointer, err := store.RegistryPath(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(pointer), registryName), nil
}

// Open names the inventory. IT DOES NOT CREATE ONE, and it does not fail when none is there: a
// person who has registered no devices has an empty inventory, which is a real and different state
// from an inventory that could not be read.
func Open(getenv func(string) string) (*Registry, error) {
	path, err := RegistryPath(getenv)
	if err != nil {
		return nil, err
	}
	return &Registry{path: path}, nil
}

// Path is where this inventory is kept.
func (r *Registry) Path() string { return r.path }

// read loads the file. A missing file is an empty inventory; anything else that goes wrong is
// ErrRegistryUnreadable, so a damaged inventory is never rendered as "you have no devices".
func (r *Registry) read() (diskFile, error) {
	var df diskFile
	body, err := os.ReadFile(r.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return diskFile{Format: registryFormat}, nil
		}
		return df, fmt.Errorf("%w: %v", ErrRegistryUnreadable, err)
	}
	if err := json.Unmarshal(body, &df); err != nil {
		return df, fmt.Errorf("%w: %s is damaged: %v", ErrRegistryUnreadable, r.path, err)
	}
	if df.Format != registryFormat {
		return df, fmt.Errorf("%w: %s is in format %d, which this build does not understand",
			ErrRegistryUnreadable, r.path, df.Format)
	}
	return df, nil
}

// List returns the person's locally-known devices, ordered by label.
//
// A SINGLE UNREADABLE ENTRY DOES NOT REMOVE A DEVICE FROM THE LISTING. An entry whose check-in
// state cannot be parsed still has a label, and §3.8 says every device is listed — so the entry
// appears with an UNDETERMINED check-in (criterion 9) rather than being dropped, which would be
// the silence criterion 6 forbids. Only a failure to read the inventory AS A WHOLE is an error,
// because then there is no list to be honest about.
func (r *Registry) List() ([]Device, error) {
	df, err := r.read()
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(df.Devices))
	for _, d := range df.Devices {
		if d.Label == "" {
			// An entry with no label names no machine; it cannot be shown as a device and it is
			// not silently a device either. It makes the inventory unreadable.
			return nil, fmt.Errorf("%w: %s contains an entry with no label", ErrRegistryUnreadable, r.path)
		}
		dev := Device{
			Label:   Label(d.Label),
			Machine: Machine(d.Machine),
			CheckIn: decodeCheckIn(d.CheckIn),
			Source:  SourceLocal,
		}
		if t, terr := time.Parse(time.RFC3339, d.RegisteredAt); terr == nil {
			dev.RegisteredAt = t
		}
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// decodeCheckIn is where the three values are recovered from disk, and the default matters more
// than the two named cases: ANYTHING THIS BUILD CANNOT READ IS UNDETERMINED. A missing field, an
// unrecognised state word, or an instant that will not parse are each "could not be determined",
// never "has never checked in" — that collapse is the one the product's first rule forbids.
func decodeCheckIn(c diskCheckIn) CheckIn {
	switch c.State {
	case diskNeverStarted:
		return NeverCheckedIn()
	case diskCheckedIn:
		t, err := time.Parse(time.RFC3339, c.At)
		// tri.FromError, spelled out: a check-in whose instant will not parse is undetermined.
		if err != nil {
			return UndeterminedCheckIn(fmt.Sprintf("this device's recorded check-in time %q could not be read", c.At))
		}
		return CheckedInAt(t)
	case "":
		return UndeterminedCheckIn("this device's record carries no check-in state at all")
	default:
		why := c.Why
		if why == "" {
			why = fmt.Sprintf("this device's record says %q, which this build does not understand", c.State)
		}
		return UndeterminedCheckIn(why)
	}
}

func encodeCheckIn(c CheckIn) diskCheckIn {
	switch c.State {
	case tri.Yes:
		return diskCheckIn{State: diskCheckedIn, At: c.At.UTC().Format(time.RFC3339)}
	case tri.No:
		return diskCheckIn{State: diskNeverStarted}
	default:
		return diskCheckIn{State: "undetermined", Why: c.Why}
	}
}

// Lookup answers criterion 5: a registered label, or ErrNoSuchDevice.
func (r *Registry) Lookup(label Label) (Device, error) {
	list, err := r.List()
	if err != nil {
		return Device{}, err
	}
	for _, d := range list {
		if d.Label == label {
			return d, nil
		}
	}
	return Device{}, fmt.Errorf("%w: %q", ErrNoSuchDevice, string(label))
}

// Register is THE ONLY WAY A DEVICE ENTERS THE INVENTORY (PRD §4.2, nothing implicit). Nothing
// else in this package or in the commands that use it adds one, and no command adds one as a
// side effect of doing something else.
//
// It writes the device as NEVER CHECKED IN. That is criterion 3 and criterion 6 in one line: from
// the instant a machine is registered, "has never checked in" is a recorded fact in the inventory,
// so a listing taken before the machine is ever started contains an entry for it that says so.
//
// Criterion 7 is the two refusals, and both leave the file exactly as it was: the write happens
// only after both checks pass, and it is atomic, so a refused registration cannot half-apply.
func (r *Registry) Register(label Label, machine Machine, now time.Time) (Device, error) {
	if err := CheckLabel(label); err != nil {
		return Device{}, err
	}
	if machine == "" {
		return Device{}, fmt.Errorf("%w: no machine identity was given for %q", ErrMachineUndetermined, string(label))
	}
	df, err := r.read()
	if err != nil {
		// A REFUSAL, NOT AN OVERWRITE. If the inventory cannot be read, this build will not start
		// a fresh one on top of it — that would delete devices a person registered.
		return Device{}, err
	}
	for _, d := range df.Devices {
		if Label(d.Label) == label {
			return Device{}, fmt.Errorf("%w: %q is registered to machine %s", ErrDuplicateLabel, string(label), d.Machine)
		}
		if Machine(d.Machine) == machine {
			return Device{}, fmt.Errorf("%w: this machine is already registered as %q", ErrMachineAlreadyRegistered, d.Label)
		}
	}
	dev := Device{
		Label:        label,
		Machine:      machine,
		RegisteredAt: now.UTC(),
		CheckIn:      NeverCheckedIn(),
		Source:       SourceLocal,
	}
	df.Format = registryFormat
	df.Devices = append(df.Devices, diskDevice{
		Label:        string(label),
		Machine:      string(machine),
		RegisteredAt: dev.RegisteredAt.Format(time.RFC3339),
		CheckIn:      encodeCheckIn(dev.CheckIn),
	})
	if err := r.write(df); err != nil {
		return Device{}, err
	}
	return dev, nil
}

// RecordCheckIn marks a registered device as having checked in at t. It does NOT register a device
// that is not there: a check-in from an unregistered label is ErrNoSuchDevice, because registration
// is the explicit act and a check-in must never perform it quietly.
func (r *Registry) RecordCheckIn(label Label, t time.Time) error {
	df, err := r.read()
	if err != nil {
		return err
	}
	for i := range df.Devices {
		if Label(df.Devices[i].Label) == label {
			df.Devices[i].CheckIn = encodeCheckIn(CheckedInAt(t))
			return r.write(df)
		}
	}
	return fmt.Errorf("%w: %q", ErrNoSuchDevice, string(label))
}

// write replaces the inventory atomically: a temporary in the same directory, fsynced, renamed
// over the destination. An interrupted write leaves the previous inventory, never a truncated one
// that would read back as "you have fewer devices than you have".
func (r *Registry) write(df diskFile) error {
	body, err := json.MarshalIndent(df, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	tmp, err := os.CreateTemp(dir, registryName+".tmp-*")
	if err != nil {
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	if err := os.Rename(tmpName, r.path); err != nil {
		return fmt.Errorf("the device inventory could not be written: %w", err)
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
