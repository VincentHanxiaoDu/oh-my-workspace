// Issue #9, the part that says WHERE the outbox is.
//
// Issue #11 built the outbox in this package and let the caller name its directory, on purpose: #11
// would not decide where a person's material lives while Issue #3 was still deciding it. #3 has now
// landed, so this file makes the decision #11 deferred, and makes it once.
//
// THE OUTBOX IS INSIDE THE STORE. PRD §3.14 calls the local store "the sole home of unpublished
// data", and a draft is unpublished data. An outbox anywhere else — a second directory, a config
// path, a temporary location a command conjured — is a second home, and then "sole" is false and
// nobody can answer "where are my unpublished drafts" with one path. So there is one outbox per
// store, at a fixed name inside it, and this file is the only place that name is written down.
//
// It does NOT create a store. The store is the explicit act (PRD §4.2); the outbox directory inside
// an existing store is not a second act to consent to, it is where the store keeps this kind of
// thing — so [InStore] will materialise the outbox directory inside a store that already exists,
// and refuses outright when there is no store.
package drafts

import (
	"path/filepath"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// OutboxDirName is the outbox's name inside the store. One name, one place.
const OutboxDirName = "outbox"

// ErrNoStore — there is no store, so there is nowhere for a draft to live.
//
// This is a REFUSAL AND NOT A FALLBACK (Issue #9 criterion 3). The tempting alternative — write the
// draft to a temporary directory and carry on — succeeds, exits zero, and loses the person's work
// somewhere they will never look for it.
var ErrNoStore = &hub.Error{
	Code: "no-store",
	Msg:  "no store on this device, and a draft is unpublished data that may live nowhere else",
}

// InStore returns the outbox belonging to s, creating the outbox directory inside the store if this
// is the first draft. It creates no store and no directory outside one.
func InStore(s *store.Store) (*Outbox, error) {
	if s == nil {
		return nil, hub.Refusedf(ErrNoStore, "no store was opened")
	}
	dir := filepath.Join(s.Path(), OutboxDirName)
	if o, err := Open(dir); err == nil {
		return o, nil
	}
	// The store exists; its outbox is part of it. Create is idempotent on the marker.
	return Create(dir)
}
