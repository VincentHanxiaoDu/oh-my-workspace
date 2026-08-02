// Issue #10, the hub half of "a person retries and does not get two copies" (PRD §3.11).
//
// # Why the hub has to hold this and the client cannot
//
// The interesting interruption is the one where the hub DID receive the note and the client never
// learned so. From the client's side those two worlds are identical: a socket that went quiet says
// nothing about whether the bytes were acted on. So whatever the client does on retry, only the hub
// can know whether it has already acted, and the only way it can know is if the client names the
// ATTEMPT rather than merely repeating its content.
//
// Hence [IdempotencyKey]: minted once by the client per publication, written to the client's
// journal BEFORE the request leaves, and resent unchanged on every retry of that publication. The
// hub records key → note id on success and, seeing the key again, returns the note it already made.
//
// # Content hashing was considered and rejected
//
// The tempting alternative is to deduplicate on (author, title, body). It is wrong in both
// directions: a person who publishes the same one-line note twice on purpose gets one note and no
// explanation, and a person who edits a single character between an interrupted attempt and its
// retry gets two. An explicit key says what is actually meant — "this is the same attempt" — and
// says nothing about what the note contains.
//
// # Only successes are recorded
//
// A refused publication is not remembered. A refusal is a thing the person can fix — a missing
// scope, a group the hub does not know — and the natural next act is to fix it and retry with the
// same key. Remembering the refusal would make the fix take effect only after the person invented
// a new attempt, which nothing tells them to do.
package hub

import "sync"

// IdempotencyKey names one publication attempt, across all of its retries.
//
// It is opaque here. The client mints it (see internal/publish) and this package never parses it,
// so nothing about the key's format is a contract between the two halves beyond "the same string
// means the same attempt".
type IdempotencyKey string

// ErrNoIdempotencyKey — a publication arrived without one.
//
// REFUSED RATHER THAN PUBLISHED. Publishing anyway would be the one branch through this file with
// no protection against a double copy, and it would be taken by exactly the callers that forgot to
// think about retries.
var ErrNoIdempotencyKey = &Error{
	Code: "no-idempotency-key",
	Msg:  "refused: a publication must name the attempt it belongs to, so that a retry cannot make a second copy",
}

// Once is the hub's record of publication attempts it has already acted on.
//
// It is separate from [Store] rather than a field on it because Issues #11, #13, #14 and #15 all
// edit this package concurrently and none of them needs to know this exists. A persistent hub must
// keep this record in the same transaction that stores the note; in memory that is what holding the
// store's write lock across both amounts to.
type Once struct {
	mu   sync.Mutex
	done map[IdempotencyKey]attempt
}

// attempt is what one completed key produced, and WHO produced it.
//
// The holder is recorded because a key is not a secret and a retry is only a retry when it comes
// from the same person. Without it, anybody who learned somebody else's key would receive their
// note back from a publication that stored nothing — a read of a note they were never granted,
// through the write path.
type attempt struct {
	note   NoteID
	holder PersonID
}

// NewOnce returns an empty record.
func NewOnce() *Once { return &Once{done: map[IdempotencyKey]attempt{}} }

// Seen reports the note a key already produced, if any.
func (o *Once) Seen(k IdempotencyKey) (NoteID, bool) {
	if o == nil {
		return "", false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	a, ok := o.done[k]
	return a.note, ok
}

// PublishOnce publishes p through g, unless key has already published something.
//
// fresh reports whether this call is what created the note. A retry of an attempt the hub already
// completed returns (the same note, false, nil) — SUCCESS, not a refusal, because from the person's
// point of view their note is on the hub and that is what they asked for. Returning an error here
// would make the honest retry look like a failure and invite them to retry differently, which is
// how the second copy actually gets made.
func PublishOnce(s *Store, o *Once, g Grant, key IdempotencyKey, p Publication) (n *Note, fresh bool, err error) {
	if key == "" {
		return nil, false, ErrNoIdempotencyKey
	}
	if o == nil {
		return nil, false, Refusedf(ErrNoIdempotencyKey, "this hub keeps no record of attempts, so a retry could not be told from a new publication")
	}
	// THE LOCK SPANS THE CHECK AND THE PUBLICATION. Checking under one lock and publishing under
	// another is how two retries of the same attempt, arriving together, both find the key unseen
	// and both publish — the exact defect this file exists to prevent, reintroduced by the shape of
	// the locking rather than by the logic.
	o.mu.Lock()
	defer o.mu.Unlock()
	if a, ok := o.done[key]; ok {
		if a.holder != g.Holder {
			// A KEY IS NOT A SECRET AND IT IS NOT A CAPABILITY. Somebody else's attempt is not this
			// caller's retry, and answering with the note it produced would hand over a read this
			// caller was never granted, through the publication path.
			return nil, false, Refusedf(ErrRefused, "attempt %q belongs to %q, not to %q", string(key), string(a.holder), string(g.Holder))
		}
		existing, rerr := s.Read(a.note, g.Holder)
		if rerr != nil {
			// The note this key made is no longer readable to its own author. That is not "publish
			// it again": a second copy is precisely what the key exists to prevent, and the honest
			// answer is that we cannot determine the state of the first.
			return nil, false, Refusedf(ErrUndetermined, "attempt %q already published note %q and it could not be read back: %v", string(key), string(a.note), rerr)
		}
		return existing, false, nil
	}
	note, perr := PublishThrough(s, g, p)
	if perr != nil {
		// NOT RECORDED. See the file comment: a refusal is fixable and the retry must carry the
		// same key.
		return nil, false, perr
	}
	o.done[key] = attempt{note: note.ID, holder: g.Holder}
	return note, true, nil
}
