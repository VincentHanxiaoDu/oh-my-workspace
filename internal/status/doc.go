// Package status is the one screen that says whether everything runs (PRD §3.9, Issue #5).
//
// WHAT THIS PACKAGE IS FOR. A person who has just installed the client wants one answer, not five
// commands: is the daemon up, is the store there, are my channels connected, are my projects
// watched, are my devices registered, is the hub reachable. §2.1 names those six components; this
// package renders exactly those six, each on its own named line, and never silently drops one.
//
// THREE OUTCOMES, PLUS "NOT CONFIGURED", AND THEY ARE FOUR VALUES. §4.3 puts the three-valued
// answer first: working, not working, and could-not-be-determined are three different facts and a
// person acts differently on each. Issue #5 criterion 1 adds a fourth rendering — a subsystem
// nobody has configured is not failing and is not unknown — and criterion 5 requires the
// undetermined rendering to be tellable from the not-working rendering BY THAT LINE ALONE. So the
// distinction lives in a type with four constants whose zero value is Undetermined, following
// [tri.Value] for the reason that package gives, rather than in four format strings that were
// distinct on the day somebody wrote them.
//
// ONE STATE, TWO SURFACES. [Screen] is the whole determination. [Screen.Render] is what a person
// reads and [Screen.ControlJSON] is what the control API and a person's own AI read, and BOTH take
// their state word from [State.Word] — one function, so the two surfaces cannot report different
// states for the same subsystem (§4.3, criteria 9–12). The rendering is data-driven over the
// subsystem slice, which is criterion 10: a subsystem this build's renderer has never heard of is
// still printed, because there is no switch on names to fall off the end of.
//
// THIS PACKAGE DETERMINES NOTHING IT CAN BORROW. Daemon liveness comes from the product's one
// answer, passed in (Issue #41). The last run's ending comes from [daemon.Report]. Project state
// comes from [projects.Take], device state from [devices.Load], channel state from
// [channels.Connection.Health]. Re-deriving any of those here would be a second opinion about a
// fact that already has an owner, and the six lines would then disagree with the six commands.
//
// AND IT CHANGES NOTHING. Criterion 4: status is a report. Nothing in this package writes to the
// store, starts a process, creates a store or opens a network connection — with no hub configured
// there is no code path here that reaches a transport at all (criterion 15, §4.2).
package status
