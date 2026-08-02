package store

import "strings"

// Kind groups records that mean the same sort of thing: tickets, draft notes, channel cursors,
// project state.
//
// THE STORE DOES NOT KNOW WHAT A TICKET IS, and must not learn. §2.3's three containers are not the
// same container, and each is owned by its own capability; if this package grew a Ticket struct
// then the inbox, the outbox and everything after them would all be editing one file. A Kind is
// whatever the owning package declares — `store.Kind("ticket")`, `store.Kind("draft")` — and this
// package's only interest is that it is usable as one directory name.
type Kind string

// Record is one stored thing: a kind, an id unique within that kind, and an opaque payload.
//
// Data is bytes on purpose. The store's guarantee is that what comes back is byte-identical to what
// went in, or that nothing comes back at all — a guarantee that would be weakened by this package
// re-encoding a caller's structure. Callers wanting a struct use [Store.PutJSON] and [Store.GetJSON],
// which are thin wrappers, not a second storage format.
type Record struct {
	Kind Kind
	ID   string
	Data []byte
}

// recordFile is the on-disk envelope.
//
// The checksum is what makes criterion 13 answerable. An interrupted write cannot produce a
// readable-but-wrong record — the atomic rename sees to that — but a bad sector, a truncating
// backup tool or a person with an editor can, and a store that hands back silently damaged bytes is
// worse than one that says it cannot be read.
// Data is a []byte and so is base64 in the file: a payload is arbitrary bytes, and a format that
// only holds valid JSON would quietly stop being able to hold a draft with an attachment in it.
type recordFile struct {
	Format int    `json:"format"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"data"`
}

// recordFormat is the envelope version. A file claiming a version this build does not know is
// unreadable — stated, never skipped, because skipping it would report a store full of records as
// an empty one.
const recordFormat = 1

// recordSuffix marks a completed record file. Anything in a kind directory without it — notably a
// temporary from a write that was interrupted — is not a record and is never listed or read.
const recordSuffix = ".rec"

// tempPrefix marks an in-progress write. It is a dot-prefix as well as a distinct prefix so that a
// leftover is invisible to a person listing the directory as well as to this package.
const tempPrefix = ".writing-"

// validName reports whether s is usable as one path segment inside the store.
//
// Deliberately strict. An id is chosen by a caller, and a caller that has not thought about it will
// eventually pass something containing a slash or a "..", at which point a record write becomes a
// write to an arbitrary place on the filesystem — and invariant 5, the store as the sole home of
// unpublished data, is gone.
func validName(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
