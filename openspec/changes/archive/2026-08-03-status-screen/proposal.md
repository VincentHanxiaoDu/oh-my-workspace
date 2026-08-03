# The status screen

## Why

PRD §3.9's first bullet: "**Status** — one screen that says whether everything runs."

The person in Issue #5 has just installed the client. Teams is hooked up, two project directories are
pointed at, there is a hub at work. Before typing anything real they want one screen — not a log, not
five commands — that says whether all of that is actually running.

The screen is easy to build and easy to build dishonestly, and the Issue is written almost entirely
about the dishonest versions:

- **A tidy red cross for something nobody could check.** §4.3: "a state that could not be determined
  is shown as undetermined, never as a 'no'." A person who is sent hunting for a problem that is not
  there has been actively harmed by the screen, and the screen looked fine while doing it.
- **A cheerful summary over an unchecked subsystem.** "Everything is fine" printed above a line
  nobody could read is the same lie with better manners.
- **A subsystem quietly absent.** A line that is not there reads as nothing to worry about.
- **Two surfaces that disagree.** §4.3: "the control API and the CLI report the same state." A
  person's own AI reads this machine through the control API; if it sees a different screen than
  they do, one of them is wrong and neither knows which.

## What Changes

- **A new `internal/status` package** holding the whole determination as one `Screen` value. Both
  surfaces render *that value*: `Screen.Render` is the screen a person reads and
  `Screen.ControlJSON` is what the control API and a person's own AI read. They cannot disagree,
  because there is one state and both take every state token from one `State.Word`.
- **Four states, in a type, with `Undetermined` as the zero value.** Working, not working, could not
  be determined, not configured. The fourth is Issue #5's criterion 1 — a hub nobody has set up is
  not a broken hub — and the zero value is criterion 5 defended at the type level: a subsystem an
  error path returned without setting a state must not read as a confident negative.
- **Six lines, every time, from a named list.** The six §2.1 names — daemon, local store, configured
  channels, watched projects, devices registration, hub connection — are constants with a `Required()`
  list, so "no subsystem is silently omitted" is a property a test can state rather than a habit.
- **Members have states of their own.** A subsystem line carries `Item`s: one channel, one project,
  one device. Criterion 6 needs an unreachable adapter, a directory that has gone missing and a
  machine that has never checked in each to appear with their own state and their own wording, and a
  line that collapsed its members into a count could not do that.
- **The summary is derived, in one function, and undetermined wins outright.** `Summarise` checks for
  anything undetermined FIRST — subsystem or member — before it looks at anything else. Criterion 8
  is the one to get right and this ordering is it.
- **`Render` is data-driven over the slice.** There is no switch on subsystem names, so there is no
  default arm for an unknown subsystem to fall off (criterion 10), and no early return for an
  undetermined one to abort on (criterion 7).
- **A new `omw status` command**, with `--json` for the control API's form of the same answer.
  Exit 0 when the screen was produced and every state on it was established — *including* "the
  daemon is not running", which is criterion 13's delivered answer. Exit 3 when something could not
  be determined. Exit 1 only when no screen exists at all.
- **Nothing implicit.** No call in either file starts a daemon, creates a store or opens a network
  connection. With no hub configured `Query.Dial` is unreachable from the branch that finds the
  environment empty — criterion 15 as a property of the code's shape rather than of a check.
- **A pointer, not an implementation.** §3.9's other two bullets are separate Issues — the health
  report (#1, merged) and the diagnostics bundle (#20, PR #50). Status names them at the foot of the
  screen and does none of their work.

## What Issue #5 did NOT settle, and what this build does about it

Reported rather than decided quietly:

- **What "the control API" means for a whole status screen.** `internal/daemon`'s control socket
  serves exactly one message today — a `daemon.Report` about the daemon itself — and extending that
  wire format is Issue #2's territory, not this one's. So this build follows the convention already
  in the tree (`devices.Snapshot.ControlJSON`, Issue #17): the control API's *form* of the answer is
  the serialised `Screen`, reached through `omw status --json`, and criteria 9–12 are driven by
  obtaining both surfaces and comparing them subsystem by subsystem. What is **not** driven here is
  a status screen served over the daemon's unix socket; that needs the daemon's protocol to grow a
  second message and belongs with #2 or #16.
- **The agent API (§2.1, criterion 12)** is Issue #16 and does not exist yet. This build gives it one
  representation to read rather than a second, softer one — the same `ControlJSON` the CLI's `--json`
  prints. That the agent API will use it is a construction, not an observation.
- **Where "not configured" sits among the three outcomes.** Criterion 5 asks for three and criterion
  1 asks for a fourth rendering. This build makes it a fourth *state* rather than a decoration on one
  of the three, because a flag beside a two-valued answer is how two renderings eventually collapse.
- **A never-checked-in device is a determined negative here, not an unknown.** #17 owns what such a
  device looks like and records it as `tri.No`. Criterion 6 requires only that it not render like a
  plain failure, and it does not — it gets its own sentence, compared pairwise against a genuine
  confirmed failure. Calling it undetermined instead would have contradicted #17.
- **Precedence when a subsystem's members disagree.** Undetermined outranks not-working outranks
  not-configured. The Issue does not say so; the alternative ranks a known failure above an unknown,
  which is the collapse §4.3 exists to prevent.
