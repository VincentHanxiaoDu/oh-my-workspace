package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// A SET OF WRITES THAT IS EITHER ALL APPLIED OR NONE (invariant 4, widened from one record to many).
//
// WHY THIS EXISTS, AND WHY IT IS HERE RATHER THAN IN A CALLER. Invariant 4 promises that one record
// is absent or complete. It promises nothing about TWO records, and there are operations whose whole
// meaning is that several records change together: Issue #7 merges N tickets into one, which is one
// write and N deletions, and criterion 10 says an interrupted merge must leave the inbox with all N
// inputs separate or with the one merged ticket — never a mixture.
//
// No ordering of individual Puts and Deletes can deliver that. Write the merged ticket first and a
// crash leaves it beside its own inputs; delete the inputs first and a crash leaves them gone with
// nothing in their place. Both are the "half-merged" state the criterion forbids, and the window is
// not small: every Put here fsyncs twice.
//
// It could not live in the inbox either. A caller can only recover a half-applied batch by being
// asked to, and the reader that must never see the half state is `omw inbox list` — a command that
// knows nothing about merging and should not have to. So the commit point and the replay live down
// here, where every reader passes through [Open] and therefore through recovery.
//
// THE STORE STILL DOES NOT KNOW WHAT A TICKET IS. An [Op] is a kind, an id and opaque bytes, exactly
// as a [Record] is. This package has learnt "these writes belong together", which is a fact about
// writes, not a fact about tickets.
//
// HOW IT WORKS, AND WHERE THE COMMIT POINT IS.
//
//  1. The whole batch is serialised and written, by [atomicWrite], to journal/<name>.rec. That
//     single rename IS the commit point: before it the batch does not exist, after it the batch has
//     happened as far as any future reader is concerned.
//  2. The ops are applied — puts, then deletes.
//  3. The journal file is removed.
//
// A process killed before step 1 completes leaves no journal and no op applied: the store is in its
// prior state. A process killed during step 2 or 3 leaves the journal, and the next [Open] replays
// it in full and removes it. Every op is idempotent — a Put overwrites and a Delete of an absent
// record is a success — so a replay of a batch that was already half applied, or wholly applied,
// reaches the same end state.
//
// WHAT THIS DOES NOT CLAIM. It is not a transaction: there is no isolation and no rollback. A reader
// running concurrently with step 2, in another process, can still see the batch part-applied. What
// it guarantees is what criterion 10 asks for — that a process which DIES cannot leave a half state
// behind it, because the next reader to open the store finishes the job before it reads.

// Op is one write in a batch: a record to put, or a record to delete.
type Op struct {
	Kind Kind
	ID   string
	// Data is the payload for a put. It is ignored when Delete is set.
	Data []byte
	// Delete makes this op a removal rather than a write.
	Delete bool
}

// journalDir holds the batches that have been committed and may not yet have been applied. It sits
// beside records/ rather than inside it so that [Store.Kinds] — which lists the record directories
// — never reports a journal as a kind of thing the person has.
const journalDir = "journal"

// journalFormat is the on-disk version of a batch. A journal this build cannot understand is
// [ErrUnreadable] and stops the Open: a committed batch that cannot be replayed is not something to
// step over quietly, because stepping over it is exactly the half-applied state this file prevents.
const journalFormat = 1

type journalFile struct {
	Format int         `json:"format"`
	Name   string      `json:"name"`
	SHA256 string      `json:"sha256"`
	Ops    []journalOp `json:"ops"`
}

type journalOp struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Data   []byte `json:"data,omitempty"`
	Delete bool   `json:"delete,omitempty"`
}

// ErrEmptyBatch means Apply was given nothing to do. It is an error and not a quiet success because
// a caller that computed an empty set of writes has a bug the store cannot fix by ignoring it.
var ErrEmptyBatch = errors.New("a batch with no operations in it")

// Apply performs every op, or — if this process dies partway — leaves the batch to be completed by
// whoever opens the store next. There is no outcome in which some ops happened and the rest never
// will.
//
// name identifies the batch on disk and must be usable as one path segment. It is the caller's, so
// that a caller can tell its own abandoned batch from somebody else's: two concurrent Applies with
// the SAME name are two writers to one journal file and the later one wins, which is a caller error
// this package cannot detect.
//
// EVERY OP IS VALIDATED BEFORE THE COMMIT POINT. An unusable kind or id fails the whole call with
// nothing written, rather than being discovered on the third op with two already applied.
func (s *Store) Apply(name string, ops []Op) error {
	if len(ops) == 0 {
		return ErrEmptyBatch
	}
	if !validName(name) {
		return pathErr("apply", s.root, ErrInvalidName, "batch name "+quote(name)+" is not usable as a file name")
	}
	jf := journalFile{Format: journalFormat, Name: name, Ops: make([]journalOp, 0, len(ops))}
	for _, op := range ops {
		if _, _, err := s.recordPath(op.Kind, op.ID, "apply"); err != nil {
			return err
		}
		jf.Ops = append(jf.Ops, journalOp{Kind: string(op.Kind), ID: op.ID, Data: op.Data, Delete: op.Delete})
	}

	dir := filepath.Join(s.root, journalDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return pathErr("apply", dir, ErrPermissionDenied, err.Error())
	}
	body, err := encodeJournal(jf)
	if err != nil {
		return pathErr("apply", dir, ErrUnreadable, err.Error())
	}
	path := filepath.Join(dir, name+recordSuffix)

	// THE COMMIT POINT. Above this line the batch has not happened; below it, it has.
	if err := writeFileAtomic(path, body); err != nil {
		return pathErr("apply", path, ErrPermissionDenied, err.Error())
	}
	return s.runJournal(path, jf)
}

// encodeJournal serialises a batch with a checksum over its ops, for the reason [recordFile] carries
// one: the atomic rename rules out a torn write, and rules out nothing about damage beneath the
// product. A journal that does not match its checksum is unreadable, never a shorter batch.
func encodeJournal(jf journalFile) ([]byte, error) {
	payload, err := json.Marshal(jf.Ops)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	jf.SHA256 = hex.EncodeToString(sum[:])
	return json.Marshal(jf)
}

func decodeJournal(path string, body []byte) (journalFile, error) {
	var jf journalFile
	if err := json.Unmarshal(body, &jf); err != nil {
		return jf, pathErr("apply", path, ErrUnreadable, "the batch journal is damaged: "+err.Error())
	}
	if jf.Format != journalFormat {
		return jf, pathErr("apply", path, ErrUnreadable,
			"the batch journal is format "+itoa(jf.Format)+", which this build does not understand")
	}
	payload, err := json.Marshal(jf.Ops)
	if err != nil {
		return jf, pathErr("apply", path, ErrUnreadable, err.Error())
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != jf.SHA256 {
		return jf, pathErr("apply", path, ErrUnreadable,
			"the batch journal does not match its checksum, so it has been damaged since it was written")
	}
	return jf, nil
}

// runJournal applies a committed batch and then removes its journal.
//
// PUTS BEFORE DELETES, and it matters for the one case a merge produces: a batch that writes a
// record and deletes another with the same kind and id — a merged ticket taking one of its inputs'
// identifiers. Deleting last would remove what was just written. Doing puts last would be the
// symmetric bug for the reverse batch; this order is the one the callers here need, and it is stated
// so that a caller relying on the other order is relying on something that is not promised.
func (s *Store) runJournal(path string, jf journalFile) error {
	for _, op := range jf.Ops {
		if op.Delete {
			continue
		}
		if err := s.Put(Record{Kind: Kind(op.Kind), ID: op.ID, Data: op.Data}); err != nil {
			return err
		}
	}
	for _, op := range jf.Ops {
		if !op.Delete {
			continue
		}
		if err := s.Delete(Kind(op.Kind), op.ID); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return pathErr("apply", path, ErrPermissionDenied, err.Error())
	}
	return syncDir(filepath.Dir(path))
}

// recoverBatches finishes every batch that was committed and not applied. [Open] calls it, so no
// reader of this store can observe a half-applied batch left by a process that died.
//
// IT NEVER STEPS OVER A JOURNAL IT CANNOT READ. An unreadable journal fails the Open with
// [ErrUnreadable], because the alternative is opening a store whose contents are mid-sentence and
// reporting them as the truth — the shape of defect this whole package exists to remove.
//
// A store with no journal directory is the ordinary case and costs one failed ReadDir.
func (s *Store) recoverBatches() error {
	dir := filepath.Join(s.root, journalDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			return pathErr("apply", dir, ErrPermissionDenied, err.Error())
		}
		return pathErr("apply", dir, ErrUnreadable, err.Error())
	}
	var names []string
	for _, e := range entries {
		// An abandoned temporary from an interrupted journal write is not a committed batch, and
		// isRecordFile is what says so — the same rule the records themselves are read by.
		if !e.IsDir() && isRecordFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return pathErr("apply", path, ErrUnreadable, rerr.Error())
		}
		jf, derr := decodeJournal(path, body)
		if derr != nil {
			return derr
		}
		if err := s.runJournal(path, jf); err != nil {
			return err
		}
	}
	return nil
}
