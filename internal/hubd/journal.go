package hubd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// The names inside a hub directory. There is one durable record and one marker; a hub that is not
// marked is not a hub, and [Open] refuses rather than conjuring one (PRD §4.2, "nothing implicit").
const (
	markerFile  = "hub.json"
	journalFile = "corpus.journal"
	// ownerOnlyDirMode / ownerOnlyFileMode: the corpus includes notes narrowed to one person. The
	// hub operator can read them (§2.4) — every other account on the machine is not the operator.
	ownerOnlyDirMode  os.FileMode = 0o700
	ownerOnlyFileMode os.FileMode = 0o600
)

// FormatVersion is the durable record's format. A record written by a newer hub is REFUSED rather
// than half-read: a corpus read with fields skipped is a corpus that answers differently from the
// one that was written, which is criterion 8 broken by an upgrade.
const FormatVersion = 1

type marker struct {
	Format  int       `json:"format"`
	Company string    `json:"company"`
	Created time.Time `json:"created"`
}

// entry is one durable fact. The record is append-only: a note is published once and amended or
// re-scoped afterwards, and replaying the entries in order reproduces the corpus exactly.
//
// EVERY FIELD IS A FACT, NOT A RENDERING. Visibility is stored structurally rather than as the
// token a person types, because `Visibility.Token()` is display text — it says "people" without
// saying which people — and a record that cannot reproduce the audience is a record that cannot
// reproduce who could read a note.
type entry struct {
	Op string `json:"op"`

	// person / group entries — the membership record visibility is evaluated against.
	Person  string   `json:"person,omitempty"`
	Group   string   `json:"group,omitempty"`
	Members []string `json:"members,omitempty"`

	// note entries.
	ID         string       `json:"id,omitempty"`
	Author     string       `json:"author,omitempty"`
	Title      string       `json:"title,omitempty"`
	Body       string       `json:"body,omitempty"`
	At         time.Time    `json:"at,omitempty"`
	Visibility *visRecord   `json:"visibility,omitempty"`
	By         string       `json:"by,omitempty"`
	Versions   []verRecord  `json:"versions,omitempty"`
	Revoked    *revokeEntry `json:"revoked,omitempty"`
}

type verRecord struct {
	Number int       `json:"number"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

// revokeEntry records that a person ended a session. It is durable so that criterion 7's "ending it
// takes effect at the hub, not only locally" survives the hub being restarted: a token id recorded
// here is refused for good, whatever any in-memory session table says.
type revokeEntry struct {
	Person string `json:"person"`
	Token  string `json:"token"`
}

// visRecord is a visibility, stored so it can be rebuilt exactly.
type visRecord struct {
	Kind   string   `json:"kind"`
	Group  string   `json:"group,omitempty"`
	People []string `json:"people,omitempty"`
}

const (
	opPerson     = "person"
	opGroup      = "group"
	opPublish    = "publish"
	opAmend      = "amend"
	opVisibility = "visibility"
	opRevoke     = "revoke"
)

func recordVisibility(v hub.Visibility) *visRecord {
	r := &visRecord{Kind: v.Token()}
	switch v.Kind() {
	case hub.KindGroup:
		r.Group = string(v.Group())
	case hub.KindPeople:
		for _, p := range v.People() {
			r.People = append(r.People, string(p))
		}
	}
	return r
}

// rebuild turns a stored visibility back into one.
//
// AN UNRECOGNISED KIND IS AN ERROR, NOT A DEFAULT. Falling back to company-wide would widen a note
// its author narrowed, on a code path nobody watches — the worst possible place for a default.
// Falling back to self-only would hide a note the company was told it could read. Neither is an
// answer; refusing to start is.
func (r *visRecord) rebuild() (hub.Visibility, error) {
	if r == nil {
		return hub.Visibility{}, fmt.Errorf("a note was recorded with no visibility at all")
	}
	switch r.Kind {
	case "company":
		return hub.CompanyWide(), nil
	case "self":
		return hub.SelfOnly(), nil
	case "group":
		return hub.ToGroup(hub.GroupID(r.Group))
	case "people":
		ids := make([]hub.PersonID, 0, len(r.People))
		for _, p := range r.People {
			ids = append(ids, hub.PersonID(p))
		}
		return hub.ToPeople(ids...)
	default:
		return hub.Visibility{}, fmt.Errorf("a note was recorded with visibility %q, which is not one of the four choices", r.Kind)
	}
}

// journal is the append-only durable record of everything published to this hub.
//
// IT IS FSYNCED BEFORE AN OPERATION IS REPORTED AS DONE. Criterion 1 is "stores a published note
// durably... it survives a restart of the process", and a note acknowledged out of a write buffer
// does not survive the machine losing power a moment later. The cost is one fsync per publication,
// which is the correct trade for a corpus a company relies on.
type journal struct {
	f *os.File
}

func openJournal(dir string) (*journal, error) {
	f, err := os.OpenFile(filepath.Join(dir, journalFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, ownerOnlyFileMode)
	if err != nil {
		return nil, err
	}
	return &journal{f: f}, nil
}

// append writes one entry and does not return until it is on the disk.
func (j *journal) append(e entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := j.f.Write(b); err != nil {
		return err
	}
	return j.f.Sync()
}

func (j *journal) close() error {
	if j == nil || j.f == nil {
		return nil
	}
	return j.f.Close()
}

// readJournal reads every entry in order.
//
// A TRUNCATED LAST LINE IS NOT A CORRUPT CORPUS. A crash between the write and the sync can leave a
// partial final line; everything before it was synced and is good. So a partial FINAL line is
// dropped with the fact reported, and a malformed line anywhere else is an error — the difference
// between "the last thing did not finish" and "this file is not what it claims to be".
func readJournal(dir string) (entries []entry, truncated bool, err error) {
	f, err := os.Open(filepath.Join(dir, journalFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	for lineNo := 1; ; lineNo++ {
		line, readErr := r.ReadString('\n')
		atEOF := errors.Is(readErr, io.EOF)
		if readErr != nil && !atEOF {
			return nil, false, readErr
		}
		complete := strings.HasSuffix(line, "\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if atEOF {
				return entries, truncated, nil
			}
			continue
		}
		if !complete && atEOF {
			// The write that did not finish. Reported, never silently swallowed.
			return entries, true, nil
		}
		var e entry
		if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
			return nil, false, fmt.Errorf("the hub's durable record is unreadable at line %d: %w", lineNo, err)
		}
		entries = append(entries, e)
		if atEOF {
			return entries, truncated, nil
		}
	}
}

// replay rebuilds a store and the set of revoked token ids from the durable record.
//
// IT IS THE ONLY PATH FROM DISK TO A LIVE CORPUS, and it is deliberately strict: any entry it
// cannot honour is an error and the hub does not start. A hub that starts having dropped entries it
// did not understand is a hub whose answers differ from the corpus it holds — which is exactly what
// criterion 8 forbids between two hubs, and it is no better between a hub and itself.
func replay(entries []entry) (*hub.Store, *hub.Record, map[string]string, error) {
	members := hub.NewRecord()
	store := hub.NewStore(members)
	revoked := map[string]string{}

	for i, e := range entries {
		switch e.Op {
		case opPerson:
			members.AddPerson(hub.PersonID(e.Person))

		case opGroup:
			ps := make([]hub.PersonID, 0, len(e.Members))
			for _, m := range e.Members {
				ps = append(ps, hub.PersonID(m))
			}
			members.DefineGroup(hub.GroupID(e.Group), ps...)

		case opPublish:
			v, err := e.Visibility.rebuild()
			if err != nil {
				return nil, nil, nil, fmt.Errorf("entry %d (note %q): %w", i+1, e.ID, err)
			}
			versions := make([]hub.Version, 0, len(e.Versions))
			for _, ver := range e.Versions {
				versions = append(versions, hub.NoteAt(ver.Number, ver.Body, ver.At))
			}
			if len(versions) == 0 {
				versions = append(versions, hub.NoteAt(1, e.Body, e.At))
			}
			n := &hub.Note{
				ID:         hub.NoteID(e.ID),
				Author:     hub.PersonID(e.Author),
				Title:      e.Title,
				Visibility: v,
				Versions:   versions,
			}
			if err := store.RestoreNote(n); err != nil {
				return nil, nil, nil, fmt.Errorf("entry %d (note %q) could not be restored: %w", i+1, e.ID, err)
			}

		case opAmend:
			// THE RECORDED TIME, NEVER THE REPLAY TIME. Store.Amend would stamp the clock of the
			// process doing the replaying, so two hubs replaying one record would report different
			// recency for the same corpus — criterion 8, broken by a clock. See RestoreVersion.
			if len(e.Versions) != 1 {
				return nil, nil, nil, fmt.Errorf("entry %d (note %q) records %d versions for an amendment, which records exactly one", i+1, e.ID, len(e.Versions))
			}
			v := e.Versions[0]
			if err := store.RestoreVersion(hub.NoteID(e.ID), hub.NoteAt(v.Number, v.Body, v.At)); err != nil {
				return nil, nil, nil, fmt.Errorf("entry %d (note %q) could not be amended: %w", i+1, e.ID, err)
			}

		case opVisibility:
			v, err := e.Visibility.rebuild()
			if err != nil {
				return nil, nil, nil, fmt.Errorf("entry %d (note %q): %w", i+1, e.ID, err)
			}
			if _, err := store.SetVisibility(hub.NoteID(e.ID), hub.PersonID(e.By), v); err != nil {
				return nil, nil, nil, fmt.Errorf("entry %d (note %q) could not be re-scoped: %w", i+1, e.ID, err)
			}

		case opRevoke:
			if e.Revoked == nil {
				return nil, nil, nil, fmt.Errorf("entry %d records a revocation with no token", i+1)
			}
			revoked[e.Revoked.Token] = e.Revoked.Person

		default:
			// An operation this build does not know is not something to skip. See replay's comment.
			return nil, nil, nil, fmt.Errorf("entry %d records operation %q, which this hub does not understand", i+1, e.Op)
		}
	}
	return store, members, revoked, nil
}

// amendmentsOf turns a note's versions into the record form.
func amendmentsOf(n *hub.Note) []verRecord {
	out := make([]verRecord, 0, len(n.Versions))
	for _, v := range n.Versions {
		out = append(out, verRecord{Number: v.Number, Body: v.Body, At: v.At})
	}
	return out
}

// sortedStrings is used where a record must not depend on map iteration order — two hubs writing
// the same corpus must write the same bytes (criterion 8).
func sortedStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}
