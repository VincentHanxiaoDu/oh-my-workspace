package health

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CheckerFor returns the real full-disk encryption probe for a platform, or nil where this slice
// has none.
//
// SELECTED AT RUNTIME, NOT BY BUILD TAG. Both probes compile everywhere, so a test on any machine
// can construct either one and drive its parsing and its "the tool is not here" path. Behind build
// tags, the Linux probe's logic would be uncompiled — and therefore untested — on a macOS
// developer machine and on whichever single platform CI happens to run.
//
// Windows returns nil deliberately: PRD §5.1 ships macOS and Linux only for this slice, and the
// honest answer where there is no probe is the third value (criterion 15).
func CheckerFor(goos string) EncryptionChecker {
	switch goos {
	case "darwin":
		return DarwinChecker{}
	case "linux":
		return LinuxChecker{}
	default:
		return nil
	}
}

// errNoUsableOutput is the shape of every "the query returned nothing I can read" failure. It is an
// error and not a false, because a probe that cannot read the disk has not determined that the disk
// is unencrypted.
var errNoUsableOutput = errors.New("the encryption query returned nothing usable")

// ---------------------------------------------------------------------------
// macOS — FileVault
// ---------------------------------------------------------------------------

// DarwinChecker reads FileVault state with `fdesetup status` (criterion 10).
//
// `fdesetup status` exits 0 whether FileVault is on or off, so the ANSWER IS IN THE OUTPUT and a
// non-zero exit means the query itself failed. Reading only the exit code would report every
// machine as encrypted.
type DarwinChecker struct {
	// run is the command runner, replaced in tests. Nil means the real one.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Mechanism implements EncryptionChecker.
func (DarwinChecker) Mechanism() string { return "FileVault, via `fdesetup status`" }

// Enabled implements EncryptionChecker.
func (c DarwinChecker) Enabled(ctx context.Context) (bool, error) {
	run := c.run
	if run == nil {
		run = runCommand
	}
	out, err := run(ctx, "fdesetup", "status")
	if err != nil {
		// THE TOOL BEING ABSENT OR ERRORING IS UNDETERMINED, NEVER "NO" (criterion 13). It is
		// returned as an error so tri.FromError produces the third value.
		return false, fmt.Errorf("could not run `fdesetup status`: %w", err)
	}
	return parseFdesetupStatus(string(out))
}

// parseFdesetupStatus reads `fdesetup status` output.
//
// Pure and exported to the package's tests so the three outcomes are driven without a Mac. Anything
// that is not recognisably On or Off is an error: an unrecognised future wording must become the
// undetermined value, not a default of "off".
func parseFdesetupStatus(out string) (bool, error) {
	s := strings.ToLower(out)
	switch {
	case strings.Contains(s, "filevault is on"):
		return true, nil
	case strings.Contains(s, "filevault is off"):
		return false, nil
	default:
		trimmed := strings.TrimSpace(out)
		if trimmed == "" {
			return false, errNoUsableOutput
		}
		return false, fmt.Errorf("unrecognised `fdesetup status` output: %q", firstLine(trimmed))
	}
}

// ---------------------------------------------------------------------------
// Linux — LUKS
// ---------------------------------------------------------------------------

// LinuxChecker reads LUKS full-disk encryption state (criterion 11).
//
// TWO WAYS TO LOOK, BECAUSE ONE IS OFTEN UNAVAILABLE:
//
//  1. `lsblk -rno FSTYPE` — a crypto_LUKS filesystem type anywhere in the block tree means LUKS is
//     in use. A successful lsblk that lists block devices and shows no crypto_LUKS is a real
//     "not enabled": the query worked and the answer was no.
//  2. `/dev/mapper` plus `cryptsetup status <name>` — used when lsblk is absent or lists nothing,
//     which is the normal state inside a container where the block tree is not visible.
//
// If neither can be read the answer is the third value. An empty block tree is NOT "no": nothing
// was determined about the host's disks.
type LinuxChecker struct {
	run     func(ctx context.Context, name string, args ...string) ([]byte, error)
	mapper  string // directory listed for device-mapper names; empty means /dev/mapper
	readDir func(string) ([]os.DirEntry, error)
}

// Mechanism implements EncryptionChecker.
func (LinuxChecker) Mechanism() string {
	return "LUKS, via `lsblk` and `cryptsetup status` on /dev/mapper"
}

// Enabled implements EncryptionChecker.
func (c LinuxChecker) Enabled(ctx context.Context) (bool, error) {
	run := c.run
	if run == nil {
		run = runCommand
	}

	lsblkOut, lsblkErr := run(ctx, "lsblk", "-rno", "FSTYPE")
	if lsblkErr == nil {
		if found, ok := parseLsblkFSTypes(string(lsblkOut)); ok {
			return found, nil
		}
	}

	// lsblk was absent, errored, or listed nothing. Fall back to the device mapper.
	found, err := c.checkMapper(ctx, run)
	if err == nil {
		return found, nil
	}
	if lsblkErr != nil {
		return false, fmt.Errorf("could not read LUKS state: `lsblk` failed (%v) and %w", lsblkErr, err)
	}
	return false, fmt.Errorf("could not read LUKS state: `lsblk` listed no block devices and %w", err)
}

// checkMapper looks for an active LUKS mapping. A non-nil error means it could not tell.
func (c LinuxChecker) checkMapper(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error)) (bool, error) {
	dir := c.mapper
	if dir == "" {
		dir = "/dev/mapper"
	}
	readDir := c.readDir
	if readDir == nil {
		readDir = os.ReadDir
	}
	entries, err := readDir(dir)
	if err != nil {
		return false, fmt.Errorf("could not list %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		// `control` is the device-mapper control node, not a mapping.
		if e.Name() != "control" {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		// NO MAPPINGS IS NOT "NO LUKS" when this is the fallback path: it is reached precisely
		// because the block tree could not be read, so there is nothing to conclude from.
		return false, errNoUsableOutput
	}

	var lastErr error
	for _, name := range names {
		out, err := run(ctx, "cryptsetup", "status", filepath.Join(dir, name))
		if err != nil {
			lastErr = err
			continue
		}
		if isLUKSStatus(string(out)) {
			return true, nil
		}
		lastErr = nil
	}
	if lastErr != nil {
		return false, fmt.Errorf("could not run `cryptsetup status`: %w", lastErr)
	}
	// cryptsetup answered for every mapping and none of them is LUKS. That is a determined "no".
	return false, nil
}

// parseLsblkFSTypes reports whether a crypto_LUKS filesystem is present. The second result is false
// when the output says nothing about any block device, in which case the caller must look elsewhere
// rather than conclude a negative.
func parseLsblkFSTypes(out string) (found, usable bool) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.TrimSpace(line)
		if f == "" {
			continue
		}
		usable = true
		if f == "crypto_LUKS" {
			return true, true
		}
	}
	return false, usable
}

// isLUKSStatus reports whether `cryptsetup status` describes a LUKS mapping.
func isLUKSStatus(out string) bool {
	s := strings.ToLower(out)
	return strings.Contains(s, "type:") && strings.Contains(s, "luks")
}

// ---------------------------------------------------------------------------

// runCommand is the ONE place this package starts a process. It starts the platform probe and
// nothing else: health never starts the daemon (PRD §4.2).
//
// It is a var so a test can count what health starts. That count is the driver behind criteria 5
// and 6: a health run with an injected probe starts nothing at all, and a run with a real probe
// starts exactly the encryption query and no second process.
var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s is not available on this machine: %w", name, err)
	}
	// CombinedOutput, because these tools explain a refusal on stderr and that explanation is what
	// a person needs when the answer is "could not be determined".
	return exec.CommandContext(ctx, path, args...).CombinedOutput()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
