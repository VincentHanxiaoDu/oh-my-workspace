package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
)

// sunPathLimit is the shortest socket-path limit among the platforms this product ships for.
//
// A UNIX SOCKET PATH IS NOT AN ORDINARY PATH. It is copied into a fixed-size field in the kernel —
// 104 bytes on the BSDs and macOS, 108 on Linux — and a longer one fails at bind() with "invalid
// argument", which reads like a programming error and is really a length. The smaller of the two
// is used so that a store path which works on one of the two platforms works on both.
const sunPathLimit = 104

// socketFor decides where a store's control socket lives.
//
// THE FIRST CHOICE IS INSIDE THE STORE, and it is the right one: the socket belongs to that store,
// its run directory is already owner-only, and everything about the daemon is then in one place a
// person can look at.
//
// THE FALLBACK EXISTS BECAUSE OF sunPathLimit, AND IT WAS FOUND BY A SKIPPED TEST. Under a
// temporary directory — which is where every test in this package puts its store, and where a
// macOS `$TMPDIR` already spends fifty characters — the in-store path is too long to bind, so the
// control API declined to open for a reason that has nothing to do with §4.6. Four criteria were
// being reported as passing while their tests skipped. Rather than shorten the tests to fit the
// code, the code takes a short path in a per-user runtime directory: still a unix socket, still
// owner-only and still confirmed before opening, and one socket per store because the name is a
// digest of the store's own path.
//
// It holds no data. §3.14's "the store is the sole home of unpublished data" is about records, and
// a control socket has no content at rest; the run record and the lock, which do say something
// about the person's store, stay inside it.
func socketFor(storeRoot string) (dir, path string) {
	inStore := filepath.Join(storeRoot, RunDir, socketName)
	if len(inStore) <= sunPathLimit {
		return filepath.Dir(inStore), inStore
	}
	sum := sha256.Sum256([]byte(storeRoot))
	short := filepath.Join(runtimeDir(), hex.EncodeToString(sum[:8])+".sock")
	if len(short) > sunPathLimit {
		// Nowhere short enough exists. Returning the in-store path means openControl fails with
		// the real bind error and the control API is reported as undetermined and NOT open, which
		// is the correct outcome — never a silent fallback to some other transport.
		return filepath.Dir(inStore), inStore
	}
	return filepath.Dir(short), short
}

// runtimeDir is the per-user directory short control sockets live in.
//
// $XDG_RUNTIME_DIR when the system provides one — it is already per-user and already owner-only —
// and otherwise a per-uid directory under the temporary directory. The uid is in the NAME rather
// than only in the permissions so that two users on one machine cannot land on the same directory
// and have the second one's creation fail against the first one's ownership.
func runtimeDir() string {
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		return filepath.Join(x, "omw")
	}
	return filepath.Join(os.TempDir(), "omw-"+strconv.Itoa(os.Getuid()))
}
