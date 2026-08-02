package store

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// SyncFinding is the answer to "does this location get copied off this machine?" (§4.1).
//
// It is a three-valued answer with its evidence attached, not a bool. The evidence matters because
// criterion 6 requires a refusal to name WHICH synchronising location was detected and WHERE, so
// that a person can tell "this is Dropbox" from the other ways creation can fail.
type SyncFinding struct {
	// State is Yes when the location was determined to synchronise off the machine, No when it was
	// determined not to, and Undetermined when the probe could not conclude. Undetermined is the
	// zero value, so a SyncFinding nobody filled in never reads as a confident "not synchronising".
	State tri.Value

	// Provider names what was detected — "Dropbox", "iCloud Drive", "OneDrive", "a roaming
	// profile", "a network filesystem (nfs4)". Empty unless State is Yes.
	Provider string

	// Evidence is the path the determination was made from: the ancestor directory holding the
	// provider's marker, or the mount point. Empty unless State is Yes.
	Evidence string

	// Reason says why the probe could not conclude. Empty unless State is Undetermined.
	Reason string
}

// Describe renders the finding in one line, with all three states distinguishable and none of them
// blank. Rendering lives here rather than at each call site because that is where two of the three
// collapse into one.
func (f SyncFinding) Describe() string {
	switch f.State {
	case tri.Yes:
		return "synchronises off this machine — " + f.Provider + ", detected at " + f.Evidence
	case tri.No:
		return "confirmed off the sync path — no evidence of Dropbox, iCloud Drive, OneDrive, a roaming profile or a network filesystem in this location's ancestry"
	default:
		reason := f.Reason
		if reason == "" {
			reason = "the probe did not complete"
		}
		return "whether this location synchronises off this machine " + tri.Undetermined.String() + " — " + reason
	}
}

// syncMarkers maps a marker file or directory name, as it appears INSIDE a synchronising root, to
// the provider it identifies.
//
// THE PROBE LOOKS FOR EVIDENCE, IT DOES NOT NAME AN OPERATING SYSTEM. Criterion 7 forbids a
// macOS-only check that silently passes everything on Linux, and the way to fail that criterion is
// to write `if runtime.GOOS == "darwin"`. Every marker below is a file the provider itself puts on
// disk, and every one of them is looked for on every platform: Dropbox's client writes `.dropbox`
// on macOS, on Linux and on Windows, and a Dropbox folder mounted or copied onto a Linux box is
// still a Dropbox folder. The one platform-specific source in this file — the Linux mount table —
// is additive: where it is absent the marker probe still runs, and its absence is not an excuse to
// answer "no".
var syncMarkers = map[string]string{
	".dropbox":       "Dropbox",
	".dropbox.cache": "Dropbox",
	".dropbox.attr":  "Dropbox",

	// The GUID file OneDrive drops in a synced root, plus the client's own dot-directory.
	".849C9593-D756-4E56-8D6E-42412F2A707B": "OneDrive",
	".onedrive":                             "OneDrive",

	// A Windows roaming profile, seen from a machine that has it mounted.
	"ntuser.dat": "a roaming profile",
	"NTUSER.DAT": "a roaming profile",
}

// syncDirNames maps a DIRECTORY name that is itself a synchronising root to its provider. These are
// the roots that carry no marker file of their own.
var syncDirNames = map[string]string{
	"Mobile Documents":       "iCloud Drive", // ~/Library/Mobile Documents
	"com~apple~CloudDocs":    "iCloud Drive",
	"CloudStorage":           "iCloud Drive", // ~/Library/CloudStorage/<Provider>-<Account>
	"iCloud Drive (Archive)": "iCloud Drive",
}

// remoteFSTypes are filesystem types whose contents are not on this disk. A store on one of these
// is off the machine before a single sync client is involved.
var remoteFSTypes = map[string]string{
	"nfs": "NFS", "nfs4": "NFS", "cifs": "SMB/CIFS", "smbfs": "SMB/CIFS", "smb3": "SMB/CIFS",
	"afs": "AFS", "ncpfs": "NCP", "9p": "9P", "ceph": "Ceph", "glusterfs": "Gluster",
	"fuse.sshfs": "SSHFS", "fuse.s3fs": "S3", "fuse.rclone": "rclone", "fuse.gvfsd-fuse": "GVFS",
	"fuse.dropbox": "Dropbox", "fuse.onedriver": "OneDrive", "fuse.rclone-mount": "rclone",
}

// DetectSync probes whether path is copied off this machine.
//
// It walks the path's ancestry — starting at the nearest ancestor that exists, so an as-yet
// uncreated store location can be judged before it is created — and at each level looks for
// evidence a synchronising client left on disk. It also consults the mount table where the system
// publishes one, so a network filesystem is caught even where no client marker exists.
//
// THE FAILURE MODE THIS FUNCTION MUST NOT HAVE is answering No because it could not look. A
// directory it cannot list, or a path it cannot resolve, produces Undetermined with the reason
// attached — never tri.No. A caller receiving No has been told that the ancestry WAS inspected and
// held no evidence.
func DetectSync(path string) SyncFinding {
	abs, err := filepath.Abs(path)
	if err != nil {
		return SyncFinding{Reason: "the path could not be made absolute: " + err.Error()}
	}

	start, err := nearestExisting(abs)
	if err != nil {
		return SyncFinding{Reason: "no part of " + abs + " could be inspected: " + err.Error()}
	}
	// Resolve symlinks so a store pointed at a link into a sync root is judged by where it lands,
	// not by where it is spelled. A resolution that fails is undetermined, not clean.
	if resolved, rerr := filepath.EvalSymlinks(start); rerr == nil {
		start = resolved
	} else {
		return SyncFinding{Reason: "the links in " + start + " could not be followed: " + rerr.Error()}
	}

	if f, ok := mountFinding(start); ok {
		return f
	}

	var undetermined *SyncFinding
	for dir := start; ; dir = filepath.Dir(dir) {
		if provider, ok := syncDirNames[filepath.Base(dir)]; ok {
			return SyncFinding{State: tri.Yes, Provider: provider, Evidence: dir}
		}
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			// COULD NOT LOOK IS NOT "NOTHING THERE". Remember the first such level and keep
			// climbing: an unreadable directory below a readable Dropbox root must still come back
			// as Dropbox, because a determined Yes beats an undetermined maybe.
			if undetermined == nil && !errors.Is(rerr, fs.ErrNotExist) {
				undetermined = &SyncFinding{Reason: dir + " could not be listed: " + rerr.Error()}
			}
		}
		for _, e := range entries {
			if provider, ok := syncMarkers[e.Name()]; ok {
				return SyncFinding{State: tri.Yes, Provider: provider, Evidence: dir}
			}
			// iCloud leaves a `.<name>.icloud` placeholder beside every evicted file.
			if strings.HasSuffix(e.Name(), ".icloud") {
				return SyncFinding{State: tri.Yes, Provider: "iCloud Drive", Evidence: dir}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	if undetermined != nil {
		return *undetermined
	}
	return SyncFinding{State: tri.No}
}

// nearestExisting returns the deepest ancestor of path (path itself included) that exists.
func nearestExisting(path string) (string, error) {
	for dir := path; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			return dir, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("reached the filesystem root without finding an existing directory")
		}
	}
}

// mountTablePaths are the places a system publishes its mount table. Absent on macOS, which is why
// its absence is silence rather than an answer.
var mountTablePaths = []string{"/proc/self/mounts", "/proc/mounts"}

// mountFinding reports a remote filesystem under path, using the mount table where one exists.
//
// It returns ok=false — NOT a No — when there is no mount table to read. "This system does not
// publish its mounts" is not evidence that the location is local; the marker probe still has to
// run, and it runs on every platform.
func mountFinding(path string) (SyncFinding, bool) {
	var f *os.File
	for _, p := range mountTablePaths {
		if fh, err := os.Open(p); err == nil {
			f = fh
			break
		}
	}
	if f == nil {
		return SyncFinding{}, false
	}
	defer f.Close()

	best, bestType := "", ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		point, fstype := unescapeMount(fields[1]), fields[2]
		if _, remote := remoteFSTypes[fstype]; !remote {
			continue
		}
		if underOrEqual(path, point) && len(point) > len(best) {
			best, bestType = point, fstype
		}
	}
	if best == "" {
		return SyncFinding{}, false
	}
	return SyncFinding{
		State:    tri.Yes,
		Provider: remoteFSTypes[bestType] + " (a " + bestType + " filesystem, not this disk)",
		Evidence: best,
	}, true
}

// unescapeMount undoes the octal escaping the mount table uses for spaces and tabs.
func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func underOrEqual(path, dir string) bool {
	if path == dir {
		return true
	}
	if dir == string(filepath.Separator) {
		return strings.HasPrefix(path, dir)
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
