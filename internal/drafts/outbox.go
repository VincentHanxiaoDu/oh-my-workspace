// Package drafts is the LOCAL half of Issue #11: versioning of drafts that have not been published.
//
// PRD §4.4 and criterion 11: "with no hub configured, versioning of drafts in the outbox works
// fully — successive local revisions of a draft are addressable and readable as they stood". Not
// partially, not in memory until the process exits, and not by asking a hub politely first. So this
// package touches no network, imports no transport, and works entirely against a directory the
// person named.
//
// # Why the directory is named per invocation and not configured
//
// PRD §4.2: "the store is created explicitly, by a command a person runs on purpose." Issue #3 owns
// the client store and will own where it lives; inventing a second global location here would mean
// two Issues each deciding where a person's material sits. So [Create] takes a path, the CLI takes
// a flag, and nothing is conjured: a directory that does not exist is an error saying so, never a
// silent mkdir behind a read.
//
// # Why it implements hub.VersionSource
//
// A draft's timeline and a published note's timeline are the same idea, and criterion 11 asks the
// person to be able to tell "this note has one version" from "the hub was not reachable". They can
// only do that if the two read alike. Implementing [github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub.VersionSource]
// means one renderer, one standing vocabulary and one reference format across both, rather than a
// local dialect that drifts.
//
// THE READER ARGUMENT IS IGNORED HERE, AND THAT IS NOT A GATE BYPASS. Visibility is the hub's
// question about colleagues (PRD §3.5); a draft in a person's own outbox has no audience yet — it
// is unpublished, on their machine, behind PRD §4.1's disk boundary. There is nobody to evaluate
// against and no membership record to evaluate with, so the honest implementation answers for the
// owner of the directory. Nothing in this package is reachable with a hub note id, and nothing in
// package hub reads from here.
package drafts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// The refusals this package can produce, in the same shape package hub uses, so that a surface
// prints one kind of code and a test asserts on one kind of thing.
var (
	// ErrNoOutbox — the directory named is not an outbox this build made.
	ErrNoOutbox = &hub.Error{Code: "no-outbox", Msg: "no draft outbox at that path, and omw does not create one behind a read"}
	// ErrBadDraftID — a draft name that cannot be a directory entry.
	ErrBadDraftID = &hub.Error{Code: "bad-draft-id", Msg: "refused: a draft name may not contain a path separator or a dot segment"}
	// ErrNoSuchDraft — no draft by that name.
	ErrNoSuchDraft = &hub.Error{Code: "no-such-draft", Msg: "no draft by that name in this outbox"}
)

// marker names the file that makes a directory an outbox.
//
// It exists so that [Open] can tell "you pointed me at your home directory" from "you pointed me at
// an outbox with no drafts in it yet". Without it, any empty or unrelated directory would read as
// an empty outbox, and an empty outbox is precisely the answer criterion 11 says must never stand
// in for "I could not look".
const marker = ".omw-outbox"

// revisionSuffix is the extension every revision file carries.
const revisionSuffix = ".body"

// Outbox is a person's local drafts, versioned.
type Outbox struct{ dir string }

// Create makes an outbox at dir. This is the explicit act PRD §4.2 requires; every other entry
// point in this package refuses to create anything.
func Create(dir string) (*Outbox, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("omw draft outbox\n"), 0o600); err != nil {
		return nil, err
	}
	return &Outbox{dir: dir}, nil
}

// Open returns the outbox at dir, or refuses. It creates nothing.
func Open(dir string) (*Outbox, error) {
	if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
		return nil, hub.Refusedf(ErrNoOutbox, "%q", dir)
	}
	return &Outbox{dir: dir}, nil
}

// Dir is where this outbox lives.
func (o *Outbox) Dir() string { return o.dir }

func (o *Outbox) pathFor(id hub.NoteID) (string, error) {
	s := string(id)
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) || strings.HasPrefix(s, ".") {
		return "", hub.Refusedf(ErrBadDraftID, "%q", s)
	}
	return filepath.Join(o.dir, s), nil
}

// Revise adds a revision to a draft, creating the draft if this is its first.
//
// IT APPENDS. It never rewrites revision N, never renumbers, and never removes one — criterion 1
// applied to the local half, and PRD §5.4 applied to a directory. The returned ref addresses the
// revision just written and keeps addressing it for as long as the directory exists.
func (o *Outbox) Revise(id hub.NoteID, body string) (hub.VersionRef, error) {
	dir, err := o.pathFor(id)
	if err != nil {
		return hub.VersionRef{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return hub.VersionRef{}, err
	}
	nums, err := o.numbers(dir)
	if err != nil {
		return hub.VersionRef{}, err
	}
	next := 1
	if len(nums) > 0 {
		next = nums[len(nums)-1] + 1
	}
	name := filepath.Join(dir, fmt.Sprintf("%06d%s", next, revisionSuffix))
	// O_EXCL: if a file for this number already exists we have miscounted, and the one thing worse
	// than failing here is overwriting a revision somebody wrote.
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return hub.VersionRef{}, err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return hub.VersionRef{}, err
	}
	if err := f.Close(); err != nil {
		return hub.VersionRef{}, err
	}
	return hub.VersionRef{Note: id, Number: next}, nil
}

// numbers returns the revision numbers present, ascending.
func (o *Outbox) numbers(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), revisionSuffix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(e.Name(), revisionSuffix))
		if err != nil || n < 1 {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

// Drafts lists the draft names in this outbox, ordered.
func (o *Outbox) Drafts() ([]hub.NoteID, error) {
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		return nil, err
	}
	var out []hub.NoteID
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, hub.NoteID(e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// Timeline implements hub.VersionSource.
func (o *Outbox) Timeline(id hub.NoteID, _ hub.PersonID) ([]hub.Version, error) {
	dir, err := o.pathFor(id)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, hub.Refusedf(ErrNoSuchDraft, "%q", string(id))
	}
	nums, err := o.numbers(dir)
	if err != nil {
		return nil, err
	}
	if len(nums) == 0 {
		return nil, hub.Refusedf(ErrNoSuchDraft, "%q has no revisions", string(id))
	}
	out := make([]hub.Version, 0, len(nums))
	for _, n := range nums {
		v, err := o.read(dir, id, n)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// VersionAt implements hub.VersionSource.
func (o *Outbox) VersionAt(id hub.NoteID, num int, _ hub.PersonID) (hub.Version, error) {
	dir, err := o.pathFor(id)
	if err != nil {
		return hub.Version{}, err
	}
	if _, err := os.Stat(dir); err != nil {
		return hub.Version{}, hub.Refusedf(ErrNoSuchDraft, "%q", string(id))
	}
	return o.read(dir, id, num)
}

func (o *Outbox) read(dir string, id hub.NoteID, num int) (hub.Version, error) {
	name := filepath.Join(dir, fmt.Sprintf("%06d%s", num, revisionSuffix))
	info, err := os.Stat(name)
	if err != nil {
		// NO SUCH REVISION, not an empty one — criterion 9 on the local half. A caller tells these
		// apart by code, without looking at the body.
		return hub.Version{}, hub.Refusedf(hub.ErrNoSuchVersion, "draft %q has no revision %d", string(id), num)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		// THE REVISION IS THERE AND WE COULD NOT READ IT. That is undetermined, and it is emphati-
		// cally not an empty draft: an unreadable file rendering as blank content under a success
		// is the exact defect criterion 8 names.
		return hub.Version{}, hub.Refusedf(hub.ErrVersionUnreadable, "draft %q revision %d: %v", string(id), num, err)
	}
	return hub.Version{Number: num, Body: string(b), At: info.ModTime()}, nil
}

// At is a convenience for reading a revision's write time without its body.
func (o *Outbox) At(id hub.NoteID, num int) (time.Time, error) {
	v, err := o.VersionAt(id, num, "")
	if err != nil {
		return time.Time{}, err
	}
	return v.At, nil
}
