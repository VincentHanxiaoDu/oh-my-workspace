// Package projects holds the directories a person has pointed the client at, and the one
// determination of what state each of them is in (PRD §3.6, §4.2, §4.3, §4.4; Issue #4).
//
// # WHY THIS IS A PACKAGE AND NOT A COMMAND
//
// Issue #4 criterion 14 requires that "the control API and the CLI report the same project state":
// the provenance of criterion 6, the missing marking of criterion 8 and the undetermined marking of
// criterion 10 must agree between the two surfaces. Two surfaces agree reliably when they are two
// renderings of ONE value, and disagree eventually when they are two computations of the same
// question. So [Snapshot] is the single determination. The CLI renders it with [Render]; the
// control API — Issue #2's, which does not exist on this branch — must serve this same struct.
// See "THE BOUNDARY WITH ISSUE #2" below for exactly what is and is not guaranteed here.
//
// # THE PROVENANCE RULE IS THE HEART OF THE ISSUE (criterion 6)
//
// PRD §3.6: while the daemon runs it re-examines each watched directory every couple of seconds;
// with no daemon running NOTHING watches, and a listing looks at each directory during that command
// "and says which of the two happened". Both cases produce the same fields. What must differ is the
// statement of where the fields came from, and it must differ IN THE OUTPUT — a reader must not have
// to time the command, check whether a daemon is up, or know anything the listing did not print.
//
// [Provenance] therefore is not a derived convenience. It is recorded at the moment the state is
// produced: [Poll] stamps [DaemonPolled] on what it writes, [Snapshot] stamps [ExaminedNow] on what
// it scans itself, and [Render] prints the stamp on every entry, unconditionally. There is no code
// path that produces an entry without one — [ProvenanceUnrecorded] is the zero value and renders as
// a loud defect marker rather than as either real answer, for the same reason [tri.Undetermined] is
// the zero value of the three-valued answer.
//
// # NOTHING WATCHES WITHOUT THE DAEMON (criteria 5, 11)
//
// There is no goroutine, timer, filesystem notification or background process anywhere in this
// package that starts on its own. [Poll] advances state and nothing else does; a caller that never
// calls it has a client in which no state anywhere advances between commands, which is criterion 5
// stated as a property of the code rather than hoped for. [Snapshot] never calls [Poll], never
// writes a heartbeat and never spawns anything — running a listing does not turn the client into a
// watcher, which is criterion 11 and PRD §4.2's "nothing implicit".
//
// The daemon's liveness is read, never asserted: [Watching] reports whether a heartbeat written by
// [Poll] is fresh enough to mean something is currently watching. It returns a [tri.Value] because
// "the store could not be read" is not "nothing is watching".
//
// # THE THREE-WAY DISTINCTION (criteria 8, 9, 10, 20)
//
// A missing directory, a directory that exists but cannot be read, and a directory that exists and
// is empty are three different facts about a person's work and are three distinct renderings. This
// package keeps them apart in the DATA, not only in the wording: see [State], whose Present and
// Readable fields are [tri.Value]s and whose Files count is meaningless unless Readable is
// [tri.Yes]. A walk that collapsed them would have to first collapse two tri values into one.
//
// # NO HUB, NO NETWORK (criteria 11, 12)
//
// Nothing here imports a network package, and a structural test enforces that. Every capability in
// this package — add, list, remove, scan, poll — is complete with no hub configured, because none
// of it has anything to ask a hub. That is PRD §4.4 for projects, satisfied by having no remote
// half at all rather than by degrading gracefully.
//
// # THE BOUNDARY WITH ISSUE #2 (criterion 14)
//
// Issue #2 owns the daemon and the control API. It is under review on another branch and cannot be
// imported from here, so criterion 14 is NOT fully driven on this branch. What is done here:
//
//   - [Snapshot] is the single determination, and [MarshalSnapshot] is its wire form. A control API
//     that serves [MarshalSnapshot] and a CLI that calls [Render] cannot disagree about provenance,
//     missing or undetermined, because both read the same [Entry] values.
//   - A test in this package drives both renderings off one snapshot and asserts they agree on all
//     three markings, which is the half of criterion 14 that lives on this side.
//   - What is NOT done: no control API is contacted, because inventing one here would guarantee
//     agreement with a surface Issue #2 is not building. The remaining half is a test that runs both
//     real surfaces, and it belongs on the branch where both exist.
//
// The daemon side of the contract is one call: a daemon that watches projects runs [Poll] on a
// [PollInterval] ticker against the device's store. It needs nothing else from this package.
package projects
