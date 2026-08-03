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
	"syscall"
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
	// ErrDraftWriteRaced — another writer kept taking the revision number this one had just
	// claimed, and this build will not overwrite somebody's revision to break the tie.
	//
	// IT IS A REFUSAL WITH A CODE, and that is Issue #69's third finding. The bare O_EXCL failure
	// used to reach a person as `open /…/000001.body: file exists (code: )` — a raw Go error and
	// an empty code, in a product where every other refusal names itself.
	ErrDraftWriteRaced = &hub.Error{Code: "draft-write-raced", Msg: "another writer is adding revisions to this draft; nothing was written and nothing was overwritten"}
)

// marker names the file that makes a directory an outbox.
//
// It exists so that [Open] can tell "you pointed me at your home directory" from "you pointed me at
// an outbox with no drafts in it yet". Without it, any empty or unrelated directory would read as
// an empty outbox, and an empty outbox is precisely the answer criterion 11 says must never stand
// in for "I could not look".
const marker = ".omw-outbox"

// MarkerName is [marker], for the one caller outside this package that must tell "there is no
// outbox here" from "the outbox is here and would not be read" before [Open] folds both into its
// no-outbox refusal. Exported rather than respelled, because a second copy of this name is how a
// reader and a writer end up looking at two different files.
const MarkerName = marker

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
	if err := replaceFileSynced(dir, filepath.Join(dir, marker), []byte("omw draft outbox\n")); err != nil {
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

// stagingPrefix marks a draft, or a revision, that is being built and is not yet a draft.
//
// The dot prefix is the whole point: [Outbox.Drafts] and [Outbox.numbers] both skip it, so nothing
// a person or a reader can reach ever sees a half-built thing. A leftover from a killed process is
// litter, not a draft — which is exactly the difference Issue #69 was about.
const stagingPrefix = ".writing-"

// Revise adds a revision to a draft, creating the draft if this is its first.
//
// IT APPENDS. It never rewrites revision N, never renumbers, and never removes one — criterion 1
// applied to the local half, and PRD §5.4 applied to a directory. The returned ref addresses the
// revision just written and keeps addressing it for as long as the directory exists.
//
// # ISSUE #69: A DRAFT BECOMES VISIBLE WHOLE, OR IT DOES NOT BECOME VISIBLE
//
// This function used to `MkdirAll` the draft's directory and then write into it. A process killed
// between those two acts left a directory with no state file and no body — and [Outbox.StateOf]
// maps a missing state file to [StateDrafted], because "every draft that exists is somewhere". So
// the product told a person their destroyed draft was resting safely in their outbox, and exited 0.
// Sixty interrupted writes produced sixty of those.
//
// THE FIX IS NOT A CHECK ON THE READ. A reader that inspects a directory and decides whether it
// looks finished is guessing, and the next shape of damage is one it has not been taught. Instead
// the invalid state is made UNREPRESENTABLE, the way [github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices]
// makes an invalid check-in unrepresentable: a draft is assembled COMPLETE — first revision and
// state file both — under a staging name no reader looks at, fsynced, and moved into place with a
// single rename. There is no instant at which a partial draft has a name, so there is nothing for
// a reader to misread and no comment left asserting something untrue.
func (o *Outbox) Revise(id hub.NoteID, body string) (hub.VersionRef, error) {
	dir, err := o.pathFor(id)
	if err != nil {
		return hub.VersionRef{}, err
	}
	if _, serr := os.Stat(dir); serr != nil {
		if !errors.Is(serr, os.ErrNotExist) {
			return hub.VersionRef{}, serr
		}
		ref, cerr := o.createDraft(id, dir, body)
		if cerr == nil {
			return ref, nil
		}
		if !errors.Is(cerr, os.ErrExist) {
			return hub.VersionRef{}, cerr
		}
		// Somebody else created this draft between the stat and the rename. Theirs stands; ours
		// becomes a revision on top of it.
	}
	return o.appendRevision(id, dir, body)
}

// createDraft builds a whole draft under a staging name and moves it into place atomically.
func (o *Outbox) createDraft(id hub.NoteID, dir, body string) (hub.VersionRef, error) {
	staging, err := os.MkdirTemp(o.dir, stagingPrefix+"draft-")
	if err != nil {
		return hub.VersionRef{}, err
	}
	defer os.RemoveAll(staging) // A no-op once the rename has taken the directory away.

	if err := writeFileSynced(filepath.Join(staging, fmt.Sprintf("%06d%s", 1, revisionSuffix)), []byte(body)); err != nil {
		return hub.VersionRef{}, err
	}
	// THE STATE FILE IS WRITTEN BY THE WRITER, not by whichever caller remembers to. A draft that
	// exists is `drafted` because the file inside it says so, not because nothing contradicts it.
	st, err := marshalState(StateDrafted, "")
	if err != nil {
		return hub.VersionRef{}, err
	}
	if err := writeFileSynced(filepath.Join(staging, stateFileName), st); err != nil {
		return hub.VersionRef{}, err
	}
	if err := syncDir(staging); err != nil {
		return hub.VersionRef{}, err
	}
	// os.Rename onto an existing DIRECTORY fails rather than replacing it, which is what makes the
	// loser of a race fall through to appending instead of destroying the winner's draft.
	if err := os.Rename(staging, dir); err != nil {
		if isDirExists(err) {
			return hub.VersionRef{}, os.ErrExist
		}
		return hub.VersionRef{}, err
	}
	if err := syncDir(o.dir); err != nil {
		return hub.VersionRef{}, err
	}
	return hub.VersionRef{Note: id, Number: 1}, nil
}

// reviseAttempts bounds the retries when concurrent writers keep claiming the same number.
//
// Retrying is not politeness: the revision number is derived from what is on disk, so a writer
// that loses the race has not been refused anything — it has simply read a stale count. What is
// NOT negotiable is the refusal to overwrite, so every attempt still creates exclusively, and a
// writer that cannot win in this many tries is told so by name.
const reviseAttempts = 64

// appendRevision adds one revision to a draft that already exists.
func (o *Outbox) appendRevision(id hub.NoteID, dir, body string) (hub.VersionRef, error) {
	for attempt := 0; attempt < reviseAttempts; attempt++ {
		nums, err := o.numbers(dir)
		if err != nil {
			return hub.VersionRef{}, err
		}
		next := 1
		if len(nums) > 0 {
			next = nums[len(nums)-1] + 1
		}
		name := filepath.Join(dir, fmt.Sprintf("%06d%s", next, revisionSuffix))
		err = linkFileSynced(dir, name, []byte(body))
		if err == nil {
			return hub.VersionRef{Note: id, Number: next}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return hub.VersionRef{}, err
		}
		// We miscounted because somebody else wrote that number first. Count again.
	}
	return hub.VersionRef{}, hub.Refusedf(ErrDraftWriteRaced, "draft %q, after %d attempts", string(id), reviseAttempts)
}

// writeFileSynced writes a file and fsyncs it. The caller owns the directory it sits in.
func writeFileSynced(name string, body []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	return f.Close()
}

// linkFileSynced puts body at dest, durably, without ever replacing what is already there.
//
// The bytes go to a staging file first and reach the disk there, so dest never exists holding a
// partial revision. os.Link is what publishes it: it is atomic, and unlike os.Rename it FAILS when
// dest exists, which is the O_EXCL guarantee this package has always made — the one thing worse
// than refusing a write is overwriting a revision somebody wrote.
func linkFileSynced(dir, dest string, body []byte) error {
	tmp, err := os.CreateTemp(dir, stagingPrefix+filepath.Base(dest)+"-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
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
	if err := os.Link(tmpName, dest); err != nil {
		return err
	}
	return syncDir(dir)
}

// replaceFileSynced puts body at dest, durably, REPLACING what is there.
//
// Used for the state file, where the newest answer is the right one and a half-written record of
// where a draft stands is the thing Issue #69 exists to prevent.
func replaceFileSynced(dir, dest string, body []byte) error {
	tmp, err := os.CreateTemp(dir, stagingPrefix+filepath.Base(dest)+"-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
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

// syncDir fsyncs a directory so that a rename or a link inside it survives a power loss. The
// directory entry is itself a write, and an unsynced one can be lost — taking with it a draft the
// person was told had been written.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// isDirExists reports whether a rename failed because the destination directory is already there.
//
// POSIX gives this two spellings — EEXIST and ENOTEMPTY — and treating only one of them as "the
// other writer won" would turn a lost race into a hard error on half the platforms.
func isDirExists(err error) bool {
	return errors.Is(err, os.ErrExist) || errors.Is(err, syscall.ENOTEMPTY)
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
		// A staging directory is not a draft, and neither is anything else dot-prefixed: a draft
		// id may not begin with a dot (see pathFor), so nothing addressable is skipped here.
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
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
