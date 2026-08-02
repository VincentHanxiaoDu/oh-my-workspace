package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// markerName is the file whose presence IS the store.
//
// A store is not "a directory that exists" — an empty directory somebody made by hand, or the
// parent of a path a person mistyped, must not read as a store. It is this file, parseable, with a
// format this build understands. That is also what makes criterion 3 answerable: Create can see a
// store is already there without reading a single record.
const markerName = "store.json"

// recordsDir holds one subdirectory per Kind.
const recordsDir = "records"

// storeFormat is the layout version of the store as a whole.
const storeFormat = 1

type marker struct {
	Format     int    `json:"format"`
	StoreID    string `json:"store_id"`
	CreatedUTC string `json:"created_utc"`
}

// Store is an opened local store. It is safe for concurrent use by one process.
//
// Nothing in this type caches record content: every read goes to disk, because the daemon and the
// CLI are separate processes over the same directory and a cache would let one of them report a
// state the other has already changed (§4.3, "the control API and the CLI report the same state").
type Store struct {
	root string
	id   string
}

// Path is the absolute path of the store's root.
//
// Exposed because criterion 15 requires it: a person has to be able to see what is being backed up
// and what is not, and criterion 14 — that no unpublished body exists anywhere else on the machine
// — is not drivable by anyone who cannot ask the product where the store is.
func (s *Store) Path() string { return s.root }

// ID is the store's identity, generated once at creation. It is how a device tells "my store" from
// "a store somebody restored from a backup over the top of mine".
func (s *Store) ID() string { return s.id }

// Create brings a store into being at path, and is the ONLY function here that does (§4.2).
//
// It refuses, each with its own error value, when: a store is already there ([ErrAlreadyExists]);
// the containing directory does not exist ([ErrPathMissing]); the location is determined to
// synchronise off the machine ([ErrPathSynchronising]); whether it synchronises could not be
// determined ([ErrSyncUndetermined] — see the OPEN DECISION note there); this user cannot write
// there ([ErrPermissionDenied]).
//
// THE ORDER OF THOSE CHECKS IS PART OF THE BEHAVIOUR, because criterion 6 requires "this is
// Dropbox", "this path does not exist" and "I lack permission" to be tellable apart, and a check
// that runs first decides which of two simultaneous truths a person hears. It is: already-a-store,
// then does-the-parent-exist, then does-it-synchronise, then can-I-write. Existence first because a
// mistyped path makes every later answer meaningless; the sync refusal before the permission probe
// because probing writability means writing into the very location we may be about to refuse.
//
// NOTHING IS LEFT BEHIND BY A REFUSAL (criterion 5). No directory is made until every check has
// passed, and the writability probe removes its own file.
func Create(path string) (*Store, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, pathErr("create", path, ErrPathUndetermined, err.Error())
	}

	// 1. Already a store? Say so, and touch nothing.
	if _, err := os.Stat(filepath.Join(root, markerName)); err == nil {
		return nil, pathErr("create", root, ErrAlreadyExists,
			"nothing was written; the existing store is untouched")
	} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
		// ENOTDIR is "an ancestor is a file", which is not a store and not an unreadable one — it
		// falls through to the missing-path check below, which names the ancestor.
		if errors.Is(err, fs.ErrPermission) {
			return nil, pathErr("create", root, ErrPermissionDenied, err.Error())
		}
		return nil, pathErr("create", root, ErrUnreadable,
			"a store may be present here but could not be inspected: "+err.Error())
	}

	// 2. Does the containing directory exist? Create does not conjure a path nobody asked for.
	parent := filepath.Dir(root)
	if fi, err := os.Stat(parent); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, pathErr("create", root, ErrPathMissing,
				"the directory "+parent+" does not exist, so nothing was created")
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, pathErr("create", root, ErrPermissionDenied, err.Error())
		}
		return nil, pathErr("create", root, ErrUnreadable, err.Error())
	} else if !fi.IsDir() {
		return nil, pathErr("create", root, ErrPathMissing,
			parent+" exists but is not a directory, so nothing was created")
	}

	// 3. Does it synchronise off the machine? Three answers, three outcomes (§4.1, §4.3).
	switch f := DetectSync(root); f.State {
	case tri.Yes:
		return nil, pathErr("create", root, ErrPathSynchronising,
			f.Provider+", detected at "+f.Evidence+"; nothing was created")
	case tri.Undetermined:
		return nil, pathErr("create", root, ErrSyncUndetermined, f.Reason+"; nothing was created")
	}

	// 4. Can this user write here?
	if err := probeWritable(parent); err != nil {
		return nil, err
	}

	// Every check has passed; now, and only now, does anything appear on disk.
	if err := os.Mkdir(root, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			// The directory is there without a marker — a leftover, or a half-created store from a
			// build that crashed before the marker landed. Adopt it rather than refuse: the marker
			// write below is what makes it a store, and it is atomic.
		} else if errors.Is(err, fs.ErrPermission) {
			return nil, pathErr("create", root, ErrPermissionDenied, err.Error())
		} else {
			return nil, pathErr("create", root, ErrUnreadable, err.Error())
		}
	}
	if err := os.Mkdir(filepath.Join(root, recordsDir), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, pathErr("create", root, ErrPermissionDenied, err.Error())
	}

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, pathErr("create", root, ErrUnreadable, "no source of randomness: "+err.Error())
	}
	id := hex.EncodeToString(idBytes)
	body, err := json.Marshal(marker{
		Format:     storeFormat,
		StoreID:    id,
		CreatedUTC: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, pathErr("create", root, ErrUnreadable, err.Error())
	}
	// THE MARKER LANDS ATOMICALLY TOO. A machine that dies here leaves a directory that is not yet
	// a store, which Open reports as ErrNotFound — an honest answer. It must never leave a marker
	// that parses halfway, because that is a store the product would report as unreadable forever.
	if err := writeFileAtomic(filepath.Join(root, markerName), body); err != nil {
		return nil, pathErr("create", root, ErrPermissionDenied, err.Error())
	}
	if err := syncDir(parent); err != nil {
		return nil, pathErr("create", root, ErrUnreadable, err.Error())
	}
	return &Store{root: root, id: id}, nil
}

// probeWritable answers "can I write here?" by writing, then removing what it wrote.
//
// Stat-and-compare-permission-bits is the wrong probe: it gets ACLs, read-only mounts, immutable
// flags and mandatory access control wrong, and each of those failures shows up as a successful
// creation that then cannot store anything.
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, tempPrefix+"probe-")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return pathErr("create", dir, ErrPermissionDenied,
				"a test write into "+dir+" was refused; nothing was created")
		}
		if errors.Is(err, fs.ErrNotExist) {
			return pathErr("create", dir, ErrPathMissing, err.Error())
		}
		return pathErr("create", dir, ErrPermissionDenied, err.Error())
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

// Open opens an existing store. IT NEVER CREATES ONE (§4.2, criterion 2).
//
// Three outcomes, and they are three: a store ([*Store]); no store here ([ErrNotFound]); a store
// that cannot be read ([ErrUnreadable]). The second and third are separate values because "there is
// nothing here" and "there is something here I cannot read" are different facts about a person's
// data, and a caller that renders both as an empty inbox has lost the difference.
func Open(path string) (*Store, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, pathErr("open", path, ErrPathUndetermined, err.Error())
	}
	body, err := os.ReadFile(filepath.Join(root, markerName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return nil, pathErr("open", root, ErrNotFound,
				"there is no store at this path; one is created with 'omw store create'")
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, pathErr("open", root, ErrPermissionDenied, err.Error())
		}
		return nil, pathErr("open", root, ErrUnreadable, err.Error())
	}
	var m marker
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, pathErr("open", root, ErrUnreadable,
			"the store marker "+markerName+" is damaged: "+err.Error())
	}
	if m.Format != storeFormat {
		return nil, pathErr("open", root, ErrUnreadable,
			"this store is format "+itoa(m.Format)+", which this build does not understand")
	}
	return &Store{root: root, id: m.StoreID}, nil
}

// Exists answers whether a store is present at path, in three values.
//
// Yes: a store is there. No: the path was inspected and holds no store. Undetermined: it could not
// be inspected — a permission wall, an I/O error, a marker that will not parse. §4.3 in one call:
// a caller that cannot look has not established an absence.
func Exists(path string) tri.Value {
	_, err := Open(path)
	switch {
	case err == nil:
		return tri.Yes
	case errors.Is(err, ErrNotFound):
		return tri.No
	default:
		return tri.Undetermined
	}
}

// SyncState reports whether the OPEN store's own location synchronises off the machine, in three
// values with its evidence (criterion 8).
//
// The refusal at creation is not a one-time gate: a directory can be moved under a sync root
// afterwards, and a store that was legitimate on Tuesday is a leak on Wednesday. Callers reporting
// on the store — the status line, health — ask this every time rather than trusting creation.
func (s *Store) SyncState() SyncFinding { return DetectSync(s.root) }

// Put writes a record. It replaces any record with the same kind and id.
//
// CRASH SAFETY (invariant 4). The payload is written to a temporary file in the destination's own
// directory, fsynced, and renamed over the destination; the directory is then fsynced so the rename
// itself survives. At no instant is the destination path a partial file. A process killed mid-Put
// leaves either the previous record or the new one.
func (s *Store) Put(r Record) error {
	return s.PutStream(r.Kind, r.ID, bytes.NewReader(r.Data))
}

// PutStream is Put for a payload that arrives as a stream, and carries the same guarantee.
//
// It exists for two reasons. A draft with an attachment should not have to be held in memory
// twice — so the payload is encoded straight into the temporary file as it arrives, never
// buffered whole. And a write that takes real time is the only way to TEST the crash guarantee: a
// test that kills a process mid-write needs a write there is a middle of, and a Put that buffers
// its input before touching the disk has no middle to be killed in.
//
// The checksum is written as the LAST field of the envelope, because it is only known once the
// payload has been streamed. JSON object members are unordered, so this costs the reader nothing.
func (s *Store) PutStream(kind Kind, id string, src io.Reader) error {
	dir, dest, err := s.recordPath(kind, id, "put")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return pathErr("put", dir, ErrPermissionDenied, err.Error())
	}
	err = atomicWrite(dest, func(w io.Writer) error {
		if _, err := fmt.Fprintf(w, `{"format":%d,"kind":%q,"id":%q,"data":"`, recordFormat, string(kind), id); err != nil {
			return err
		}
		h := sha256.New()
		enc := base64.NewEncoder(base64.StdEncoding, w)
		if _, err := io.Copy(enc, io.TeeReader(src, h)); err != nil {
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, `","sha256":%q}`, hex.EncodeToString(h.Sum(nil)))
		return err
	})
	if err != nil {
		return pathErr("put", dest, ErrPermissionDenied, err.Error())
	}
	return nil
}

// Get returns one record, byte-identical to what was written.
//
// A record whose checksum does not match its payload is [ErrUnreadable], never a Record with
// something plausible in it. Criterion 11: a missing value and a real value must never render
// identically, so a damaged record is not silently handed over with a shorter body.
func (s *Store) Get(kind Kind, id string) (Record, error) {
	_, path, err := s.recordPath(kind, id, "get")
	if err != nil {
		return Record{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Record{}, pathErr("get", path, ErrRecordNotFound, "")
		}
		if errors.Is(err, fs.ErrPermission) {
			return Record{}, pathErr("get", path, ErrPermissionDenied, err.Error())
		}
		return Record{}, pathErr("get", path, ErrUnreadable, err.Error())
	}
	return decodeRecord(path, body)
}

func decodeRecord(path string, body []byte) (Record, error) {
	var rf recordFile
	if err := json.Unmarshal(body, &rf); err != nil {
		return Record{}, pathErr("get", path, ErrUnreadable, "the record is damaged: "+err.Error())
	}
	if rf.Format != recordFormat {
		return Record{}, pathErr("get", path, ErrUnreadable,
			"the record is format "+itoa(rf.Format)+", which this build does not understand")
	}
	sum := sha256.Sum256(rf.Data)
	if hex.EncodeToString(sum[:]) != rf.SHA256 {
		return Record{}, pathErr("get", path, ErrUnreadable,
			"the record's content does not match its checksum, so it has been damaged since it was written")
	}
	return Record{Kind: Kind(rf.Kind), ID: rf.ID, Data: rf.Data}, nil
}

// List returns every complete record of a kind, ordered by id.
//
// AN INTERRUPTED WRITE IS INVISIBLE HERE, not half-present: only files carrying the completed-record
// suffix are considered, and a temporary from a killed process carries a different name entirely.
//
// A record that is present but unreadable fails the whole call with [ErrUnreadable] rather than
// being skipped. Skipping is how a store with one damaged ticket reports as a store with one fewer
// ticket — a silent loss, which is precisely what criterion 13 forbids.
func (s *Store) List(kind Kind) ([]Record, error) {
	dir, err := s.kindDir(kind, "list")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // No record of this kind has ever been written. Empty, and determined.
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, pathErr("list", dir, ErrPermissionDenied, err.Error())
		}
		return nil, pathErr("list", dir, ErrUnreadable, err.Error())
	}
	var out []Record
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !isRecordFile(name) {
			continue
		}
		path := filepath.Join(dir, name)
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, pathErr("list", path, ErrUnreadable, rerr.Error())
		}
		rec, rerr := decodeRecord(path, body)
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// isRecordFile reports whether a directory entry is a completed record.
//
// The temporary prefix is excluded explicitly as well as by the suffix rule, so that a future change
// to how temporaries are named cannot make one readable by accident.
func isRecordFile(name string) bool {
	return len(name) > len(recordSuffix) &&
		filepath.Ext(name) == recordSuffix &&
		name[0] != '.' &&
		!hasPrefix(name, tempPrefix)
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// Delete removes a record. Removing one that is not there is not an error: the caller asked for it
// to be gone, and it is gone.
func (s *Store) Delete(kind Kind, id string) error {
	dir, path, err := s.recordPath(kind, id, "delete")
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return pathErr("delete", path, ErrPermissionDenied, err.Error())
	}
	return syncDir(dir)
}

// Kinds returns every kind that holds at least one record directory, ordered.
func (s *Store) Kinds() ([]Kind, error) {
	dir := filepath.Join(s.root, recordsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, pathErr("list", dir, ErrUnreadable, err.Error())
	}
	var out []Kind
	for _, e := range entries {
		if e.IsDir() && validName(e.Name()) {
			out = append(out, Kind(e.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// PutJSON stores v as a record's payload. A convenience over Put, not a second format.
func (s *Store) PutJSON(kind Kind, id string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return pathErr("put", string(kind)+"/"+id, ErrUnreadable, err.Error())
	}
	return s.Put(Record{Kind: kind, ID: id, Data: body})
}

// GetJSON reads a record's payload into v.
//
// A payload that will not decode into v is [ErrUnreadable], not a zeroed v: a ticket that comes back
// with an empty title because its JSON did not fit the struct is exactly the "missing value and a
// real value render identically" failure criterion 11 names.
func (s *Store) GetJSON(kind Kind, id string, v any) error {
	rec, err := s.Get(kind, id)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(rec.Data, v); err != nil {
		_, path, _ := s.recordPath(kind, id, "get")
		return pathErr("get", path, ErrUnreadable, "the record's payload is not what was expected: "+err.Error())
	}
	return nil
}

func (s *Store) kindDir(kind Kind, op string) (string, error) {
	if !validName(string(kind)) {
		return "", pathErr(op, s.root, ErrInvalidName, "kind "+quote(string(kind))+" is not usable as a directory name")
	}
	return filepath.Join(s.root, recordsDir, string(kind)), nil
}

func (s *Store) recordPath(kind Kind, id, op string) (dir, path string, err error) {
	dir, err = s.kindDir(kind, op)
	if err != nil {
		return "", "", err
	}
	if !validName(id) {
		return "", "", pathErr(op, s.root, ErrInvalidName, "id "+quote(id)+" is not usable as a file name")
	}
	return dir, filepath.Join(dir, id+recordSuffix), nil
}

// atomicWrite is the whole of invariant 4, and every ordering in it is load-bearing.
//
//	temp file in the SAME directory   — os.Rename is only atomic within one filesystem, and a
//	                                    temporary in /tmp is also a second copy of unpublished data
//	f.Sync() BEFORE the rename        — without it the rename can reach the disk before the bytes,
//	                                    and a crash leaves a record file full of zeros that parses
//	                                    as far as its length and then does not
//	os.Rename                         — the instant the record becomes visible; atomic on POSIX
//	syncDir AFTER the rename          — the directory entry is itself a write, and an unsynced one
//	                                    can be lost, taking a record that was reported as stored
//
// A failure at any step removes the temporary and leaves the destination exactly as it was. So
// does a process killed at any step: the temporary is named so that no reader will ever look at
// it, and the destination is not touched until the rename.
func atomicWrite(dest string, write func(io.Writer) error) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, tempPrefix+filepath.Base(dest)+"-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if err := write(tmp); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return err
	}
	return syncDir(dir)
}

// writeFileAtomic writes a whole slice under the same guarantee.
func writeFileAtomic(dest string, body []byte) error {
	return atomicWrite(dest, func(w io.Writer) error {
		_, err := w.Write(body)
		return err
	})
}

// syncDir fsyncs a directory so that a rename or a removal inside it survives a power loss.
//
// Opening a directory read-only and calling Sync is the POSIX way; on filesystems that refuse it
// the error is returned rather than swallowed, because a silently skipped directory sync is a
// crash-safety guarantee that is not there.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func itoa(i int) string { return strconv.Itoa(i) }

func quote(s string) string { return "\"" + s + "\"" }
