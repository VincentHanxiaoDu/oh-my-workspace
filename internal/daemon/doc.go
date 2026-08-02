// Package daemon is the long-running process on one person's machine, and the record of how its
// last run ended (PRD §2.1, §4.2, §4.3, §4.6; Issue #2).
//
// It owns three things that are easy to get subtly wrong, so each is stated here rather than
// rediscovered by the next caller.
//
// # ONE DAEMON PER STORE, AND THE LOCK IS PER STORE
//
// The exclusivity in §2.1 is a property of a STORE, not of a machine: two stores on one laptop each
// get their own daemon. So the lock is a file inside the store's own run directory, taken with an
// advisory whole-file lock ([tryLockFile]).
//
// AN ADVISORY LOCK IS USED RATHER THAN "IS THIS PID ALIVE?" BECAUSE THE KERNEL RELEASES IT WHEN THE
// HOLDER DIES. A lock left behind by a daemon that was killed is therefore not a conflict at all —
// the next start takes it. That is Issue #2's criterion 8 as a property of the mechanism rather
// than as a heuristic that has to guess whether pid 4711 is the same pid 4711 that wrote the file.
// The pid recorded inside the file is used only to SAY that what was found was stale; it is never
// what decides whether a live daemon exists.
//
// # HOW THE LAST RUN ENDED IS A FIVE-VALUED ANSWER, AND NONE OF THEM IS SILENCE
//
// [Ending] distinguishes: never run; ended by an explicit stop; ended because it could not write;
// ended without recording an ending (crash, power loss); and could not be determined. §4.3 forbids
// collapsing the last into any of the others, and Issue #2's criteria 10–12 forbid collapsing the
// first four into each other. [Ending.String] is the single place they are spelled, and
// TestEndingRenderingsAreDistinctPairwise compares every pair — asserting each against its own
// literal passes just as happily after two of them are edited to the same sentence.
//
// The crash rendering is not stored; it is INFERRED. A run record that still says "running" while
// nothing holds the store's lock describes a process that did not get to write its ending, which is
// exactly what a crash is. Storing "crashed" would require the crashed process to have written it.
//
// # THE CONTROL API REFUSES TO OPEN IF IT CANNOT PROVE ITS SOCKET IS OWNER-ONLY
//
// §4.6 read literally, and the platforms ruling on Issue #2 confirms it: the refusal is the
// feature. [Control.Open] confirms the run directory is owner-only BEFORE it listens, and confirms
// the socket itself after — and on anything other than a confirmed yes it closes what it opened,
// removes the socket and returns. It does NOT fall back to a TCP listener or to any other
// transport (criterion 24): there is no code path in this package that listens on a network
// address, and TestNoNetworkTransportExistsInThisPackage reads the package's own syntax tree to
// keep it that way.
//
// A refusal does not stop the daemon. The daemon still runs, still watches, still records how its
// run ended, and `omw health` is unaffected — Issue #1's criterion 14, carried forward.
//
// # THE DAEMON STOPS WHEN IT CANNOT WRITE, WITH NO HEALTHY WINDOW AFTER IT KNOWS
//
// The write capability is probed through [Options.Write], and the ONLY caller of it is
// [Daemon.pumpOnce], which flips the daemon's phase to "stopping, cannot write" under the same
// mutex every state read takes, BEFORE the error is returned to anything. There is therefore no
// interleaving in which a reader observes "running, fine" after a write has been observed to fail:
// the observation and the state change are one critical section. §4.3, and criterion 17.
package daemon
