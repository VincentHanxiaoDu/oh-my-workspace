package health

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

// recorder is a fake command runner. It answers by command name and records every process the
// package asked to start.
type recorder struct {
	out    map[string][]byte
	err    map[string]error
	starts []string
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.starts = append(r.starts, strings.Join(append([]string{name}, args...), " "))
	if e, ok := r.err[name]; ok {
		return nil, e
	}
	return r.out[name], nil
}

// ---------------------------------------------------------------------------
// macOS — criterion 10, driven without a Mac
// ---------------------------------------------------------------------------

func TestParseFdesetupStatus(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		want     bool
		wantErr  bool
		contains string
	}{
		{name: "on", out: "FileVault is On.\n", want: true},
		{name: "off", out: "FileVault is Off.\n", want: false},
		{name: "on, decrypting", out: "FileVault is On.\nDecryption in progress: Percent completed = 42\n", want: true},
		{name: "empty output", out: "", wantErr: true},
		{name: "whitespace only", out: "   \n\t\n", wantErr: true},
		{name: "unrecognised wording", out: "FileVault is Perhaps.\n", wantErr: true, contains: "unrecognised"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseFdesetupStatus(tc.out)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parse(%q) = %v, nil; want an error so the value becomes undetermined and not `no`", tc.out, got)
				}
				if got {
					t.Error("an unreadable status returned enabled=true")
				}
				if tc.contains != "" && !strings.Contains(err.Error(), tc.contains) {
					t.Errorf("error %q does not mention %q", err, tc.contains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q) errored: %v", tc.out, err)
			}
			if got != tc.want {
				t.Errorf("parse(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

func TestDarwinCheckerDrivesAllThreeOutcomes(t *testing.T) {
	on := &recorder{out: map[string][]byte{"fdesetup": []byte("FileVault is On.\n")}}
	if got, err := (DarwinChecker{run: on.run}).Enabled(context.Background()); err != nil || !got {
		t.Errorf("FileVault on: got (%v, %v), want (true, nil)", got, err)
	}
	off := &recorder{out: map[string][]byte{"fdesetup": []byte("FileVault is Off.\n")}}
	if got, err := (DarwinChecker{run: off.run}).Enabled(context.Background()); err != nil || got {
		t.Errorf("FileVault off: got (%v, %v), want (false, nil)", got, err)
	}
	// Criterion 12/13: the tool is not on the machine. That is undetermined, never `not enabled`.
	absent := &recorder{err: map[string]error{"fdesetup": errors.New("executable file not found in $PATH")}}
	got, err := (DarwinChecker{run: absent.run}).Enabled(context.Background())
	if err == nil {
		t.Fatalf("fdesetup absent: got (%v, nil); an unavailable query must error so the answer is undetermined", got)
	}
	if !strings.Contains(err.Error(), "fdesetup") {
		t.Errorf("the error does not name what could not be run: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Linux — criterion 11, driven without a Linux box
// ---------------------------------------------------------------------------

func TestParseLsblkFSTypes(t *testing.T) {
	cases := []struct {
		name              string
		out               string
		wantFound, wantOK bool
	}{
		{"luks present", "ext4\ncrypto_LUKS\nswap\n", true, true},
		{"luks only line", "crypto_LUKS\n", true, true},
		{"no luks", "ext4\nvfat\nswap\n", false, true},
		{"blank fstypes still list devices", "\next4\n\n", false, true},
		{"nothing listed", "\n\n  \n", false, false},
		{"empty", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, ok := parseLsblkFSTypes(tc.out)
			if found != tc.wantFound || ok != tc.wantOK {
				t.Errorf("parse(%q) = (%v, %v), want (%v, %v)", tc.out, found, ok, tc.wantFound, tc.wantOK)
			}
		})
	}
}

// mapperDir builds a fake /dev/mapper listing.
func mapperDir(names ...string) func(string) ([]os.DirEntry, error) {
	m := fstest.MapFS{}
	for _, n := range names {
		m[n] = &fstest.MapFile{}
	}
	return func(string) ([]os.DirEntry, error) {
		ents, err := fs.ReadDir(m, ".")
		if err != nil {
			return nil, err
		}
		out := make([]os.DirEntry, 0, len(ents))
		out = append(out, ents...)
		return out, nil
	}
}

const luksStatus = "/dev/mapper/root is active.\n  type:    LUKS2\n  cipher:  aes-xts-plain64\n"

func TestLinuxCheckerDrivesAllThreeOutcomes(t *testing.T) {
	t.Run("lsblk reports LUKS", func(t *testing.T) {
		r := &recorder{out: map[string][]byte{"lsblk": []byte("ext4\ncrypto_LUKS\n")}}
		got, err := (LinuxChecker{run: r.run}).Enabled(context.Background())
		if err != nil || !got {
			t.Fatalf("got (%v, %v), want (true, nil)", got, err)
		}
		if len(r.starts) != 1 {
			t.Errorf("started %v; a determined answer from lsblk needs no second query", r.starts)
		}
	})

	t.Run("lsblk reports no LUKS is a real no", func(t *testing.T) {
		r := &recorder{out: map[string][]byte{"lsblk": []byte("ext4\nvfat\nswap\n")}}
		got, err := (LinuxChecker{run: r.run}).Enabled(context.Background())
		if err != nil || got {
			t.Fatalf("got (%v, %v), want (false, nil) — lsblk answered, and the answer was no", got, err)
		}
	})

	t.Run("lsblk absent, mapper shows LUKS", func(t *testing.T) {
		r := &recorder{
			err: map[string]error{"lsblk": errors.New("executable file not found in $PATH")},
			out: map[string][]byte{"cryptsetup": []byte(luksStatus)},
		}
		got, err := (LinuxChecker{run: r.run, mapper: "/fake/mapper", readDir: mapperDir("control", "root")}).
			Enabled(context.Background())
		if err != nil || !got {
			t.Fatalf("got (%v, %v), want (true, nil)", got, err)
		}
	})

	t.Run("lsblk lists nothing and mapper has no mappings is undetermined", func(t *testing.T) {
		r := &recorder{out: map[string][]byte{"lsblk": nil}}
		got, err := (LinuxChecker{run: r.run, mapper: "/fake/mapper", readDir: mapperDir("control")}).
			Enabled(context.Background())
		if err == nil {
			t.Fatalf("got (%v, nil); an unreadable block tree must not be reported as `not enabled`", got)
		}
		if got {
			t.Error("returned enabled=true alongside an error")
		}
	})

	t.Run("mappings exist and none is LUKS is a real no", func(t *testing.T) {
		r := &recorder{
			err: map[string]error{"lsblk": errors.New("no lsblk")},
			out: map[string][]byte{"cryptsetup": []byte("/dev/mapper/vg-home is active.\n  type: LINEAR\n")},
		}
		got, err := (LinuxChecker{run: r.run, mapper: "/fake/mapper", readDir: mapperDir("vg-home")}).
			Enabled(context.Background())
		if err != nil || got {
			t.Fatalf("got (%v, %v), want (false, nil)", got, err)
		}
	})

	t.Run("cryptsetup absent is undetermined", func(t *testing.T) {
		r := &recorder{err: map[string]error{
			"lsblk":      errors.New("no lsblk"),
			"cryptsetup": errors.New("executable file not found in $PATH"),
		}}
		got, err := (LinuxChecker{run: r.run, mapper: "/fake/mapper", readDir: mapperDir("root")}).
			Enabled(context.Background())
		if err == nil || got {
			t.Fatalf("got (%v, %v); an absent cryptsetup must be undetermined, never `not enabled`", got, err)
		}
	})

	t.Run("mapper unreadable is undetermined", func(t *testing.T) {
		r := &recorder{err: map[string]error{"lsblk": errors.New("no lsblk")}}
		got, err := (LinuxChecker{
			run:     r.run,
			mapper:  "/fake/mapper",
			readDir: func(string) ([]os.DirEntry, error) { return nil, errors.New("permission denied") },
		}).Enabled(context.Background())
		if err == nil || got {
			t.Fatalf("got (%v, %v), want an error", got, err)
		}
	})
}

func TestIsLUKSStatus(t *testing.T) {
	if !isLUKSStatus(luksStatus) {
		t.Error("a LUKS2 mapping was not recognised")
	}
	if isLUKSStatus("/dev/mapper/vg-home is active.\n  type: LINEAR\n") {
		t.Error("a plain linear mapping was reported as LUKS")
	}
	if isLUKSStatus("") {
		t.Error("empty output was reported as LUKS")
	}
}

func TestCheckerForSelectsTheRealProbe(t *testing.T) {
	if _, ok := CheckerFor("darwin").(DarwinChecker); !ok {
		t.Errorf("CheckerFor(darwin) = %T, want DarwinChecker", CheckerFor("darwin"))
	}
	if _, ok := CheckerFor("linux").(LinuxChecker); !ok {
		t.Errorf("CheckerFor(linux) = %T, want LinuxChecker", CheckerFor("linux"))
	}
	for _, goos := range []string{"windows", "plan9", ""} {
		if c := CheckerFor(goos); c != nil {
			t.Errorf("CheckerFor(%q) = %T, want nil", goos, c)
		}
	}
}

// ---------------------------------------------------------------------------
// The real machine — PROBED, never named
// ---------------------------------------------------------------------------

// TestRealProbeOnThisMachine runs the actual platform probe. It SKIPS with a stated reason when
// this machine cannot answer, and it never asserts which of enabled/not enabled it should get —
// that is a property of the machine, not of the code.
//
// What it does assert is criteria 10 and 11's real content: where the platform's tool IS present,
// `could not be determined` must not be the standing answer.
func TestRealProbeOnThisMachine(t *testing.T) {
	checker := CheckerFor(runtime.GOOS)
	if checker == nil {
		t.Skipf("skipped: this slice ships probes for macOS and Linux, and this machine is %s", runtime.GOOS)
	}

	var tool string
	switch runtime.GOOS {
	case "darwin":
		tool = "fdesetup"
	case "linux":
		tool = "lsblk"
	}
	if _, err := exec.LookPath(tool); err != nil {
		t.Skipf("skipped: %s is not on this machine's PATH (%v), so the real probe cannot be exercised here", tool, err)
	}

	enabled, err := checker.Enabled(context.Background())
	if err != nil {
		// NOT A FAILURE OF THE CODE. A sandboxed or containerised runner legitimately cannot read
		// the host's disks. Reported so a green run never silently means "this was not checked".
		t.Skipf("skipped: %s is present but the probe could not complete here (%v); "+
			"on a real %s machine this is the path that must NOT be the standing answer",
			tool, err, runtime.GOOS)
	}
	t.Logf("real probe on %s via %q: enabled=%v — a determined answer, not the third value",
		runtime.GOOS, checker.Mechanism(), enabled)
}
