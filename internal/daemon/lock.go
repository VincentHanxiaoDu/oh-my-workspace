package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// lockBody is what a holder writes inside the lock file.
//
// NOTHING READS THIS TO DECIDE WHETHER A DAEMON IS RUNNING. It exists so that a lock which turns
// out to be stale can be DESCRIBED — "left by pid 4711, which is not running" — and so that a
// person looking in the directory can see who claims the store.
type lockBody struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// lockHandle is a held store lock.
type lockHandle struct {
	file *os.File
	// StaleFound is set when the lock this handle took had been left behind by a process that is
	// no longer alive. Criterion 8: that is reported precisely, and it is never reported as a live
	// conflicting daemon.
	StaleFound bool
	// StaleDetail names what was found, for the sentence the CLI prints.
	StaleDetail string
}

// acquireLock takes the store's exclusive lock, or says precisely why it could not.
//
// Three outcomes, three values: a handle; ErrLockHeld, meaning another daemon is genuinely holding
// this store right now (criterion 5); and ErrLockUndetermined, meaning the question could not be
// asked. The third is not folded into either of the others — a start that cannot tell must not
// report a conflicting daemon that may not exist (criterion 8), nor proceed as if the store were
// free.
func acquireLock(p runPaths) (*lockHandle, error) {
	if !lockingIsAvailable {
		return nil, fmt.Errorf("%w: %v", ErrLockUndetermined, errNoLockingReason())
	}
	if err := ensureRunDir(p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLockUndetermined, err)
	}
	f, err := os.OpenFile(p.lock, os.O_RDWR|os.O_CREATE, ownerOnlyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: the lock file could not be opened: %v", ErrLockUndetermined, err)
	}
	got, err := tryLockFile(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: %v", ErrLockUndetermined, err)
	}
	if !got {
		// HELD, AND WE KNOW IT IS LIVE. The kernel would have released this lock if its holder had
		// died, so there is no stale case to consider on this branch — which is exactly why the
		// lock rather than a pid decides.
		prev, _ := readLockBody(p)
		f.Close()
		if prev.PID != 0 {
			return nil, fmt.Errorf("%w (pid %d, since %s)", ErrLockHeld, prev.PID, prev.StartedAt.Format(time.RFC3339))
		}
		return nil, ErrLockHeld
	}

	// WE TOOK IT. Anything the file still said belonged to a process that is gone.
	h := &lockHandle{file: f}
	if prev, ok := readLockBody(p); ok && prev.PID != 0 && prev.PID != os.Getpid() {
		h.StaleFound = true
		h.StaleDetail = fmt.Sprintf("a lock left behind by pid %d was found and taken over; that process is %s",
			prev.PID, aliveWording(prev.PID))
	}

	body, err := json.Marshal(lockBody{PID: os.Getpid(), StartedAt: time.Now().UTC()})
	if err != nil {
		h.release()
		return nil, fmt.Errorf("%w: %v", ErrLockUndetermined, err)
	}
	if err := f.Truncate(0); err != nil {
		h.release()
		return nil, fmt.Errorf("%w: %v", ErrLockUndetermined, err)
	}
	if _, err := f.WriteAt(body, 0); err != nil {
		h.release()
		return nil, fmt.Errorf("%w: %v", ErrLockUndetermined, err)
	}
	return h, nil
}

// aliveWording describes a pid found in a stale lock. It is prose about something already decided,
// never an input to the decision.
func aliveWording(pid int) string {
	if processIsAlive(pid) {
		// A live pid holding no lock is a REUSED pid, or a process that released the lock and has
		// not exited. Either way the store is free, and saying "not running" about a pid that is
		// running would be a small lie in a sentence about trust.
		return "alive, but it does not hold this store"
	}
	return "no longer running"
}

// errNoLocking is the reason a build without advisory locking gives. It lives here rather than
// beside the build-tagged implementations so that both of them, and the wording, are one thing.
var errNoLocking = errors.New("this build has no advisory file locking, so one-daemon-per-store cannot be enforced")

func errNoLockingReason() error { return errNoLocking }

func readLockBody(p runPaths) (lockBody, bool) {
	var b lockBody
	raw, err := os.ReadFile(p.lock)
	if err != nil || len(raw) == 0 {
		return b, false
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, false
	}
	return b, true
}

func (h *lockHandle) release() {
	if h == nil || h.file == nil {
		return
	}
	_ = unlockFile(h.file)
	_ = h.file.Close()
	h.file = nil
}

// probeLock answers "does a live daemon hold this store", without taking it.
//
// It works by attempting the lock and immediately dropping it. flock is per open file description,
// so this is a real answer even when the holder is in this same process — which is what lets the
// tests drive a daemon and a status query in one binary.
//
// The returned error is ALWAYS an inability to ask. "Held" and "not held" are both answers and
// both come back with a nil error.
func probeLock(p runPaths) (held bool, holder int, err error) {
	if !lockingIsAvailable {
		return false, 0, fmt.Errorf("%w: %v", ErrLockUndetermined, errNoLocking)
	}
	if _, statErr := os.Stat(p.lock); statErr != nil {
		if os.IsNotExist(statErr) {
			// NO LOCK FILE IS A DETERMINED "NOTHING IS RUNNING", and it must not be turned into
			// one by creating the file: Inspect is a read, and a read that creates things inside a
			// store is how a store gets written to by `omw daemon status` on a full disk.
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("%w: %v", ErrLockUndetermined, statErr)
	}
	f, openErr := os.OpenFile(p.lock, os.O_RDWR, ownerOnlyFile)
	if openErr != nil {
		return false, 0, fmt.Errorf("%w: %v", ErrLockUndetermined, openErr)
	}
	defer f.Close()
	got, lockErr := tryLockFile(f)
	if lockErr != nil {
		return false, 0, fmt.Errorf("%w: %v", ErrLockUndetermined, lockErr)
	}
	if got {
		_ = unlockFile(f)
		return false, 0, nil
	}
	b, _ := readLockBody(p)
	return true, b.PID, nil
}
