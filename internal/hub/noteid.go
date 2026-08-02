package hub

import (
	"crypto/rand"
	"encoding/hex"
)

// Note ids are UNGUESSABLE, and this file is the only place they are minted.
//
// # The ruling
//
// Issue #15's `## Ruled` section, binding #10, #12, #14 and #15 together:
//
//	笔记 ID 改成不可猜的 — 用随机/不连续的 ID（如 note-7f3a9c）。两条需求就都能成立：
//	既可以区分「无权」和「不存在」（对拿到合法链接的人有用），又没法枚举。
//
//	(Note ids become unguessable — random/non-sequential, e.g. note-7f3a9c. That lets both
//	requirements hold: "not permitted" stays distinguishable from "does not exist" — useful to
//	someone holding a legitimate link — while the id space cannot be enumerated.)
//
// # What was wrong, and why it was not a small thing
//
// Ids were `note-%d` from a shared counter. Issue #12's criterion 12 REQUIRES that "refused" and
// "no such note" be distinguishable, and a caller who can tell those apart across a dense,
// predictable id space can walk it and count exactly how many notes are hidden from them. PRD §3.5
// says ranking never surfaces the existence of something the searcher cannot read; the counter
// surfaced it through the id door instead of the results door.
//
// THE SECOND CONSEQUENCE WAS WORSE AND IS THE ONE TO REMEMBER. A shared counter means publishing an
// UNREADABLE note shifted the ids of readable ones, so the same note was `note-1` to one reader and
// `note-2` to another. An identifier that is a function of the reader's own blind spots is not an
// identifier. [TestANoteIDDoesNotDependOnWhatTheReaderCannotSee] pins that separately from
// unguessability, because a fix for enumeration alone would not have fixed it.
//
// # Two things the ruling does NOT settle, decided here
//
// The ruling gives `note-7f3a9c` as an EXAMPLE of the shape ("如" — e.g.), not as a specification.
// Length and collision behaviour were mine to choose, and both are recorded in the pull request:
//
//   - LENGTH: 128 bits, 32 hex characters. The example's six hex characters are 24 bits — about
//     17 million ids, which a determined caller can walk in an afternoon against a real service,
//     and the stated purpose of the ruling is that the space cannot be enumerated at all. Going
//     ABOVE the example is compatible with "random/non-sequential"; going below is not compatible
//     with "没法枚举". The cost is a longer thing to type, and it is a constant
//     ([noteIDBytes]) if the owner would rather have the ergonomics.
//   - COLLISION: retried, then REFUSED — never overwritten. At 128 bits a collision will not happen,
//     which is exactly why the branch would never be exercised and would rot into a silent
//     overwrite of somebody's note. It is handled explicitly and tested by injecting the generator.
//
// # No fallback, on purpose
//
// If the system's randomness cannot be read, publication is REFUSED. It does not fall back to a
// counter, to the clock, or to math/rand. A fallback would reintroduce guessable ids at exactly the
// moment nobody is watching, and a note published with a guessable id stays guessable forever.

// noteIDBytes is how many random bytes back an id: 16 bytes, 128 bits, 32 hex characters.
// See the length note above — this is the one place to change it.
const noteIDBytes = 16

// noteIDPrefix keeps ids self-describing in logs and on a command line.
const noteIDPrefix = "note-"

// randRead is crypto/rand.Read, injectable so that the refusal path can be driven. Tests replace
// it; nothing else does.
var randRead = rand.Read

// mintNoteID returns a fresh unguessable id.
//
// It returns an error rather than panicking or falling back, because "the system has no randomness"
// is a real state of a machine and the honest response to it is to refuse the publication with
// something the caller can branch on.
func mintNoteID() (NoteID, error) {
	b := make([]byte, noteIDBytes)
	if _, err := randRead(b); err != nil {
		return "", Refusedf(ErrIDUnavailable, "%v", err)
	}
	return NoteID(noteIDPrefix + hex.EncodeToString(b)), nil
}

// mintNoteIDAttempts is how many times a collision is retried before the publication is refused.
// Three is not a tuning parameter; it is "more than once, and bounded".
const mintNoteIDAttempts = 3

// mintUnusedNoteIDLocked mints an id that is not already in use.
//
// THE CALLER MUST HOLD THE STORE'S WRITE LOCK. Checking for a collision and then storing under the
// id are one atomic act; doing them under separate locks is how two publications race onto the same
// id and one note quietly replaces the other.
func (s *Store) mintUnusedNoteIDLocked() (NoteID, error) {
	for i := 0; i < mintNoteIDAttempts; i++ {
		id, err := mintNoteID()
		if err != nil {
			return "", err
		}
		if _, taken := s.notes[id]; !taken {
			return id, nil
		}
	}
	// REFUSED, NOT OVERWRITTEN. Storing under a taken id would destroy a published note, which is
	// a far worse outcome than a refused publication the caller can retry.
	return "", Refusedf(ErrIDUnavailable, "%d attempts all collided", mintNoteIDAttempts)
}

// ErrIDUnavailable — a note id could not be minted, so nothing was published.
//
// It is its own code because a caller must tell it from a refusal ABOUT THE NOTE: this one says
// nothing about the author, the audience or the content, and retrying it is reasonable, which is
// not true of ErrRefused or ErrUnknownGroup.
var ErrIDUnavailable = &Error{
	Code: "note-id-unavailable",
	Msg:  "refused: a note id could not be minted, so nothing was published",
}
