package store

import "errors"

// The store's failures, as distinct sentinel values.
//
// WHY SENTINELS AND NOT PROSE. Issue #3's criterion 6 requires that "this is Dropbox", "this path
// does not exist" and "I lack permission to write here" are three distinguishable outcomes, and
// criterion 13 requires that an unreadable store is never presented as an empty one. A caller that
// can only read an error's message has to match on strings to tell those apart, and the first
// reworded sentence turns a refusal into a silent success. Each of these is errors.Is-able, and the
// CLI chooses its wording and its exit code from the value, not from the text.
var (
	// ErrNotFound means there is no store at this path. It is NOT "the store is empty" — a store
	// that exists and holds nothing is a success, and the two must never render the same (§4.3).
	ErrNotFound = errors.New("no store here")

	// ErrAlreadyExists means a store is already present at this path. Create returns it WITHOUT
	// touching a byte of what is there.
	ErrAlreadyExists = errors.New("a store already exists here")

	// ErrPathSynchronising means the location was determined to be copied off this machine, so the
	// disk has stopped being the boundary (§4.1). The accompanying *PathError names the provider
	// and the ancestor directory the evidence was found in.
	ErrPathSynchronising = errors.New("this location synchronises off this machine")

	// ErrSyncUndetermined means the product could not determine whether the location synchronises.
	//
	// OPEN DECISION — Issue #3, "Blocked on a decision". §4.1 says the store refuses a
	// synchronising location; §4.3 says an undetermined state is never rendered as a "no". Those
	// combine for reporting but not for the ACTION: proceeding treats undetermined as "not
	// synchronising", halting makes the store uncreatable on any filesystem this probe cannot
	// classify, and an explicit override is a third answer the PRD does not fix either.
	//
	// Until the product rules, Create returns this error rather than picking one of the three. It
	// is deliberately not ErrPathSynchronising: a caller must not be able to report an
	// undetermined probe as a determined refusal, and must not be able to treat it as a pass.
	ErrSyncUndetermined = errors.New("could not determine whether this location synchronises off this machine")

	// ErrPathMissing means the directory the store would be created inside does not exist. Create
	// does not build a path a person did not ask for: a mistyped parent is a mistake worth hearing
	// about, not a tree to conjure.
	ErrPathMissing = errors.New("this path does not exist")

	// ErrPermissionDenied means the location exists but this user cannot write there.
	ErrPermissionDenied = errors.New("permission denied writing here")

	// ErrUnreadable means a store is present but cannot be read: a damaged marker, a record whose
	// payload does not match its checksum, a directory that will not list. The product says so and
	// exits non-zero; it NEVER presents an unreadable store as an empty one (criterion 13).
	ErrUnreadable = errors.New("this store cannot be read")

	// ErrRecordNotFound means the store is fine and holds no such record. Distinct from
	// ErrUnreadable for the same reason ErrNotFound is distinct from an empty store.
	ErrRecordNotFound = errors.New("no such record")

	// ErrInvalidName means a Kind or a record id is not usable as a single path segment.
	ErrInvalidName = errors.New("not a usable name")

	// ErrPathUndetermined means the store's location could not be worked out from the environment
	// — no explicit path, and no home directory to derive one from. Undetermined, not absent.
	ErrPathUndetermined = errors.New("could not determine where the store lives")
)

// PathError carries the sentinel together with what was being attempted and where.
//
// Detail is the human half — "Dropbox, detected at /Users/x/Dropbox" — and exists so the CLI can
// name the specific finding without this package deciding on the sentence around it.
type PathError struct {
	Op     string // "create", "open", "put", "get", "list"
	Path   string // the store or record path involved
	Detail string // what specifically was found; may be empty
	Err    error  // one of the sentinels above
}

func (e *PathError) Error() string {
	msg := e.Op + " " + e.Path + ": " + e.Err.Error()
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return msg
}

func (e *PathError) Unwrap() error { return e.Err }

func pathErr(op, path string, err error, detail string) error {
	return &PathError{Op: op, Path: path, Detail: detail, Err: err}
}
