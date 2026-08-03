package channels

import (
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// BackgroundName is what the daemon calls this work. The CLI prints it, so a person can see that
// ingestion is something the daemon is doing rather than something they have to remember.
const BackgroundName = "channel ingestion"

// LoopFactory is the factory the DAEMON'S ingestion uses.
//
// IT IS A VARIABLE AND [Ingest]'S OWN FACTORY IS A PARAMETER, and the difference is deliberate. The
// pass itself takes its factory as an argument so that criterion 11's assertion is about one call
// and not about global state. The daemon's loop is reached through an init and a registry, so there
// is no argument to pass — and criterion 4 ("with the daemon running, a ticket appears with no
// command typed") cannot be driven at all without a seam here, because this build has no transport
// and a real Teams account is not something a test may have.
//
// It defaults to [Builtin], which reaches nothing and says so. A test replaces it; nothing in the
// product does.
var LoopFactory Factory = Builtin

// now is the clock, replaceable by a test that needs a credential to expire.
var now = time.Now

func init() {
	// INGESTION IS A PROPERTY OF THE DAEMON RUNNING (§2.1, §3.1, criterion 4). This is the whole of
	// that sentence: there is no ingest command and no refresh command in this capability, and
	// registering here is the only way ingestion ever happens.
	daemon.RegisterBackground(daemon.Background{
		Name:     BackgroundName,
		Interval: IngestInterval,
		Run:      IngestPass,
	})
}

// IngestPass is one pass, for the daemon.
//
// IT SWALLOWS ITS FAILURES ON PURPOSE, AND THEY ARE NOT LOST. A store that cannot be opened or a
// channel that could not be reached is recorded against the channel and rendered by
// `omw channels list`; it is not a reason for the daemon to stop, because the daemon stops when IT
// cannot write and not when somebody else's credential expired (§4.3, and Issue #2's criterion 15).
func IngestPass(storePath string) {
	s, err := store.Open(storePath)
	if err != nil {
		return
	}
	_, _ = Ingest(s, LoopFactory, now().UTC())
}
