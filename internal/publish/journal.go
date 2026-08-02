// The durable record that decides which container holds a note, and the one place it is written.
//
// # Why there is exactly one record and not two
//
// The obvious design keeps the client's belief in one place and the draft's bytes in another, and
// then has to keep them agreeing. It cannot: two writes have a moment between them, and a machine
// that dies in that moment leaves the two disagreeing forever, with nothing to say which is right.
// "Never both, never neither" then holds only while nothing goes wrong, which is precisely when
// nobody needed it.
//
// So the state of a publication is ONE FILE, written with ONE atomic rename, and every question
// about where a note is is answered from it and from nothing else:
//
//	no record            → drafted, in the outbox
//	phase "in-flight"    → an attempt is outstanding; still in the outbox
//	phase "refused"      → the hub said no; still in the outbox, with the reason
//	phase "published"    → on the hub
//
// # Why the ledger is beside the outbox and not inside each draft
//
// The first version of this file put the record inside the draft's own directory, which is tidier
// and is wrong: a published note's draft directory is DELETED, and the record went with it, so the
// client forgot every note it had ever published the instant it published one. The record has to
// outlive the draft, because "published" is a fact about a note whose draft is gone. So the ledger
// is a directory of its own inside the store, one small file per note, and the draft directory is
// the thing that comes and goes.
//
// # The one residual window, named rather than hidden
//
// A publication recorded as `published` still has a draft directory to delete, and a process killed
// between the rename and the delete leaves the directory behind. The LEDGER is authoritative —
// [StateOf] reports `published` and [Report.Container] answers `hub` — so nothing in this package is
// confused by it, but Issue #9's `omw outbox list` walks directories and would show it until
// something tidies up. [Reconcile] does, and runs at the start of every publish command. This is
// written down because it is the one place the invariant is carried by a convention rather than by
// a rename.
package publish

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// LedgerDirName is the ledger's name inside the store. One name, one place, written down once —
// the same rule Issue #9 applied to the outbox's own name.
const LedgerDirName = "publications"

// recordSuffix is what a ledger entry is called on disk.
const recordSuffix = ".publication"

// The phases the ledger records. They are the persisted spelling and are NOT the same strings as
// the [State] values a person reads: a state is a rendering decision and a phase is a storage
// format, and letting one change the other is how a reworded message silently invalidates every
// record on disk.
const (
	phaseInFlight  = "in-flight"
	phasePublished = "published"
	phaseRefused   = "refused"
)

type record struct {
	// Attempt is the idempotency key. Minted once per publication and reused by every retry — that
	// is the whole mechanism behind "a person retries and does not get two copies".
	Attempt string `json:"attempt"`
	Phase   string `json:"phase"`
	HubID   string `json:"hub_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Code    string `json:"code,omitempty"`
	At      string `json:"at"`
}

// ErrJournalUnreadable — the record of what happened to this note exists and could not be read.
//
// UNDETERMINED, AND NEVER `drafted`. Reporting a note as resting in the outbox when the record of an
// attempt on it cannot be read is the one lie this package is built to prevent: the person would
// publish again believing nothing had been sent.
var ErrJournalUnreadable = &hub.Error{
	Code: "publication-unreadable",
	Msg:  "the record of this note's publication could not be read, so where it stands is not known",
}

// ErrNoAttemptKey — an attempt key could not be minted, so nothing was sent.
var ErrNoAttemptKey = &hub.Error{
	Code: "attempt-key-unavailable",
	Msg:  "refused: an attempt key could not be minted, so nothing was sent and nothing was published",
}

// ErrBadNoteName — a note name that cannot be a ledger entry.
var ErrBadNoteName = &hub.Error{
	Code: "bad-note-name",
	Msg:  "refused: a note name may not contain a path separator or a dot segment",
}

// attemptKeyBytes is 16 bytes, 128 bits. The key never has to be unguessable — it is not an
// identifier anybody looks up — but it does have to be UNIQUE across every publication this person
// ever makes on every device they own, because a collision would make the hub answer a fresh
// publication with somebody else's note. Random beats a counter for the same reason it does for note
// ids: there is no shared place to keep a counter.
const attemptKeyBytes = 16

// randRead is crypto/rand.Read, injectable so the refusal path can be driven.
var randRead = rand.Read

func mintAttemptKey() (string, error) {
	b := make([]byte, attemptKeyBytes)
	if _, err := randRead(b); err != nil {
		return "", hub.Refusedf(ErrNoAttemptKey, "%v", err)
	}
	return "attempt-" + hex.EncodeToString(b), nil
}

// Ledger is this client's record of what it has tried to publish and what came of it.
type Ledger struct{ dir string }

// OpenLedger returns the ledger at dir, creating the directory if this is the first attempt.
//
// It creates a DIRECTORY INSIDE AN EXISTING STORE and nothing else — the store itself is the
// explicit act (PRD §4.2), exactly as Issue #9 argued for the outbox.
func OpenLedger(dir string) (*Ledger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Ledger{dir: dir}, nil
}

// InStore returns the ledger belonging to s.
func InStore(s *store.Store) (*Ledger, error) {
	if s == nil {
		return nil, hub.Refusedf(drafts.ErrNoStore, "no store was opened")
	}
	return OpenLedger(filepath.Join(s.Path(), LedgerDirName))
}

// Dir is where this ledger lives.
func (l *Ledger) Dir() string { return l.dir }

func (l *Ledger) pathFor(id hub.NoteID) (string, error) {
	s := string(id)
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) || strings.HasPrefix(s, ".") {
		return "", hub.Refusedf(ErrBadNoteName, "%q", s)
	}
	return filepath.Join(l.dir, s+recordSuffix), nil
}

// read returns the record, whether there is one, and any error reading it.
func (l *Ledger) read(id hub.NoteID) (record, bool, error) {
	path, err := l.pathFor(id)
	if err != nil {
		return record{}, false, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, hub.Refusedf(ErrJournalUnreadable, "%v", err)
	}
	var r record
	if err := json.Unmarshal(b, &r); err != nil {
		// PRESENT AND DAMAGED IS NOT ABSENT. An absent record means drafted; a damaged one means we
		// do not know, and the two must never share an answer.
		return record{}, true, hub.Refusedf(ErrJournalUnreadable, "the record is damaged: %v", err)
	}
	return r, true, nil
}

// write replaces the record with one atomic rename, and makes the rename durable.
//
// THE ORDER HERE IS THE INVARIANT. The temporary file is written and fsynced BEFORE it is renamed,
// so a rename that lands always lands over complete content; and the containing directory is
// fsynced AFTER, so a rename the caller has been told succeeded survives the machine losing power a
// microsecond later. Skipping either turns "never both, never neither" back into "usually neither
// both nor neither", which is not the same claim.
func (l *Ledger) write(id hub.NoteID, r record) error {
	path, err := l.pathFor(id)
	if err != nil {
		return err
	}
	r.At = time.Now().UTC().Format(time.RFC3339Nano)
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(l.dir, recordSuffix+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // a no-op once the rename has taken it away
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(l.dir)
}

// clear removes the record, returning the note to `drafted`.
//
// It is called on ONE path only: an attempt that provably never left this machine. Anywhere else it
// would be a client deciding that something it sent did not count.
func (l *Ledger) clear(id hub.NoteID) error {
	path, err := l.pathFor(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(l.dir)
}

// Notes returns every note the ledger knows about, ordered.
func (l *Ledger) Notes() ([]hub.NoteID, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	var out []hub.NoteID
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), recordSuffix) {
			continue
		}
		out = append(out, hub.NoteID(strings.TrimSuffix(e.Name(), recordSuffix)))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// syncDir makes a rename or an unlink durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// Some filesystems refuse fsync on a directory. That is a property of the filesystem and not an
	// error in the write that just landed, and failing the publication over it would be worse than
	// the residual risk it represents.
	_ = d.Sync()
	return nil
}

// StateOf reports where one note stands, from the ledger and the outbox.
//
// A note with a draft and no ledger record is `drafted`. A note with neither is one this client does
// not know about — [Report.Exists] is No, which is a determined answer and not a state.
func StateOf(l *Ledger, o *drafts.Outbox, id hub.NoteID) Report {
	r := Report{Note: id}
	rec, present, jerr := l.read(id)
	if jerr != nil {
		r.Known, r.Exists, r.Why, r.Code = tri.Undetermined, tri.Yes, jerr.Error(), hub.Code(jerr)
		return r
	}
	draftPresent, derr := draftExists(o, id)
	if derr != nil {
		r.Known, r.Exists, r.Why = tri.Undetermined, tri.Undetermined, derr.Error()
		return r
	}

	if present && rec.Phase == phasePublished {
		// PUBLISHED WINS OVER THE DIRECTORY STILL BEING THERE. See the file comment: the ledger is
		// authoritative, and a leftover directory is a deletion this client owes itself.
		r.Known, r.Exists, r.State = tri.Yes, tri.Yes, StatePublished
		r.HubID, r.Attempt = hub.NoteID(rec.HubID), rec.Attempt
		return r
	}
	if !draftPresent && !present {
		r.Known, r.Exists = tri.Yes, tri.No
		return r
	}
	if !draftPresent {
		// A record for a note with no draft and no publication. Somebody removed the draft from
		// under an outstanding attempt; that is a state nobody determined.
		r.Known, r.Exists, r.Attempt = tri.Undetermined, tri.Undetermined, rec.Attempt
		r.Why = "this note has an outstanding publication record and no draft beside it"
		return r
	}
	if !present {
		r.Known, r.Exists, r.State = tri.Yes, tri.Yes, StateDrafted
		return r
	}

	r.Known, r.Exists, r.Attempt = tri.Yes, tri.Yes, rec.Attempt
	switch rec.Phase {
	case phaseInFlight:
		r.State, r.Code = StateInFlight, rec.Code
	case phaseRefused:
		r.State, r.Reason, r.Code = StateRefused, rec.Reason, rec.Code
	default:
		// A phase this build does not know is not a fifth state; it is a record we cannot read.
		r.Known, r.Why = tri.Undetermined, fmt.Sprintf("the recorded phase %q is not one this build knows", rec.Phase)
	}
	return r
}

func draftExists(o *drafts.Outbox, id hub.NoteID) (bool, error) {
	dir, err := o.DraftDir(id)
	if err != nil {
		return false, err
	}
	_, serr := os.Stat(dir)
	switch {
	case serr == nil:
		return true, nil
	case errors.Is(serr, os.ErrNotExist):
		return false, nil
	default:
		return false, serr
	}
}

// Known returns every note this client knows about — drafts in the outbox and notes in the ledger,
// with no duplicates.
func Known(l *Ledger, o *drafts.Outbox) ([]hub.NoteID, error) {
	seen := map[hub.NoteID]bool{}
	var out []hub.NoteID
	ids, err := o.Drafts()
	if err != nil {
		return nil, err
	}
	recs, err := l.Notes()
	if err != nil {
		return nil, err
	}
	for _, group := range [][]hub.NoteID{ids, recs} {
		for _, id := range group {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// Reconcile finishes any work a previous process was killed part-way through.
//
// Today that is one thing: a note the ledger records as published whose draft directory is still on
// disk. It is called at the start of every publish command, so the residual window described in the
// file comment closes at the next command rather than lasting.
func Reconcile(l *Ledger, o *drafts.Outbox) (finished []hub.NoteID, err error) {
	ids, err := o.Drafts()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		rec, present, jerr := l.read(id)
		if jerr != nil || !present || rec.Phase != phasePublished {
			// An unreadable record is left alone on purpose. Removing a draft on the strength of a
			// record we could not read is the one irreversible mistake available here.
			continue
		}
		if rerr := o.Remove(id); rerr == nil {
			finished = append(finished, id)
		}
	}
	return finished, nil
}
