package hubd

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"

// The refusals this package adds. They are `hub.Error` values so that `hub.Code` reads them through
// the same call every other surface uses, and so a script never has to tell a hub-side refusal from
// a client-side one by parsing prose.
var (
	// ErrNoHubDirectory — [Open] was pointed at a directory that is not a hub. NOTHING IS CREATED:
	// PRD §4.2, the store is created by an explicit act. A hub that made itself when a path was
	// mistyped would answer an empty corpus with complete confidence, which is #101's defect with a
	// whole company on the other end of it.
	ErrNoHubDirectory = &hub.Error{Code: "no-hub-directory", Msg: "refused: that directory is not a hub, and this process does not make one for you"}

	// ErrHubDirectoryExists — [Create] was pointed at a directory that is already a hub. Refused
	// rather than re-initialised, because re-initialising is how a corpus is lost.
	ErrHubDirectoryExists = &hub.Error{Code: "hub-directory-exists", Msg: "refused: that directory is already a hub"}

	// ErrHubRecordUnreadable — the durable record exists and could not be read or replayed. THIS IS
	// UNDETERMINED, NOT AN EMPTY CORPUS, and the process does not start. Starting with what could be
	// parsed would serve a corpus smaller than the one on disk while looking entirely healthy.
	ErrHubRecordUnreadable = &hub.Error{Code: "hub-record-unreadable", Msg: "the hub's durable record could not be read, so what this hub holds could not be determined"}

	// ErrHubUnwritable — the hub cannot write its durable record. PRD §4.3: the daemon stops when it
	// cannot write rather than continuing in a state a person reads as healthy.
	ErrHubUnwritable = &hub.Error{Code: "hub-unwritable", Msg: "the hub cannot write its durable record"}

	// ErrHubHalted — a write failed, so this hub has stopped answering. Every call after that point
	// gets this, INCLUDING READS. See the package comment: a hub still answering searches out of a
	// store it can no longer add to is reporting a corpus that has silently stopped growing.
	ErrHubHalted = &hub.Error{Code: "hub-halted", Msg: "this hub halted when it could not write, and is answering nothing"}

	// ErrHubFormat — the durable record is a format this build does not read. Refused whole.
	ErrHubFormat = &hub.Error{Code: "hub-format", Msg: "the hub's durable record is in a format this build does not read"}

	// ErrNoCredentialPresented — a call arrived with no token material at all.
	//
	// IT IS ITS OWN REFUSAL AND NOT "no such token". An unidentified caller is the one case that
	// must never reach the visibility predicate: `hub.CanRead("")` answers UNDETERMINED for every
	// note, by design, and a caller who slipped through with an empty identity would be handed
	// "undetermined" for the whole corpus rather than a refusal. Issue #62 is exactly this defect
	// one layer down. It is refused HERE, first, explicitly.
	ErrNoCredentialPresented = &hub.Error{Code: "no-credential-presented", Msg: "refused: no token was presented, and this hub does not answer an unidentified caller"}

	// ErrUnidentifiedSession — a session resolved to no person. Cannot happen through the sign-in
	// path; asserted anyway, because the consequence of it happening is the whole corpus.
	ErrUnidentifiedSession = &hub.Error{Code: "unidentified-session", Msg: "refused: that session names no person, so nothing about what it may read could be determined"}
)

// allHubdErrors is every refusal this package defines, for the test that asserts they are pairwise
// distinguishable in both code and message.
var allHubdErrors = []*hub.Error{
	ErrNoHubDirectory, ErrHubDirectoryExists, ErrHubRecordUnreadable, ErrHubUnwritable,
	ErrHubHalted, ErrHubFormat, ErrNoCredentialPresented, ErrUnidentifiedSession,
}
