package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Issue #69, second finding: the stale-lock notice was a 100% false positive.
//
// Every clean `daemon stop` → `daemon start` printed "a lock left behind by pid N was found and
// taken over". Ten crash recoveries printed the same sentence, word for word. So the notice carried
// no information at all: a person could not tell a machine that had died from one they had stopped
// themselves, which is the entire reason for saying anything.
//
// THE SECOND DIRECTION IS THE TEST THAT MATTERS. A notice that always fires passes any test that
// only checks it appears, which is why the tree was green with this defect in it. Both directions
// are asserted here, in one function, so neither can be quietly dropped.

func lockTestPaths(t *testing.T) runPaths {
	t.Helper()
	if !lockingIsAvailable {
		t.Skip("this build has no advisory locking, so there is no lock to leave behind")
	}
	return pathsFor(filepath.Join(t.TempDir(), "store"))
}

func TestTheStaleLockNoticeAppearsOnlyAfterAnUncleanExit(t *testing.T) {
	p := lockTestPaths(t)

	// A first start on a store that has never had a daemon: nothing was left behind.
	first, err := acquireLock(p)
	if err != nil {
		t.Fatalf("the first acquire: %v", err)
	}
	if first.StaleFound {
		t.Errorf("the first start on a fresh store reported a stale lock: %s", first.StaleDetail)
	}

	// A CLEAN STOP, then a start. This is the direction that was broken.
	first.release()
	second, err := acquireLock(p)
	if err != nil {
		t.Fatalf("acquiring after a clean release: %v", err)
	}
	if second.StaleFound {
		t.Errorf("a start after a CLEAN stop reported a lock left behind, so the notice cannot "+
			"distinguish crash recovery from a normal restart: %s", second.StaleDetail)
	}
	second.release()

	// AN UNCLEAN EXIT. A process that is killed never reaches release, so its body is still in the
	// file when the next daemon takes the lock. That is the case the notice exists for, and it must
	// still fire — a fix that simply stops saying anything is worse than the defect.
	body, err := json.Marshal(lockBody{PID: os.Getpid() + 1, StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("marshalling a lock body: %v", err)
	}
	if err := os.WriteFile(p.lock, body, ownerOnlyFile); err != nil {
		t.Fatalf("leaving a lock behind: %v", err)
	}
	third, err := acquireLock(p)
	if err != nil {
		t.Fatalf("acquiring after an unclean exit: %v", err)
	}
	if !third.StaleFound {
		t.Error("a start after an UNCLEAN exit said nothing about the lock it took over")
	}
	third.release()
}
