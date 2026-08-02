// Package store is the device's local store: the sole home of every ticket and every unpublished
// draft on this machine (PRD §2.1, §2.3, §3.14).
//
// It is the foundation five other capabilities sit on — the daemon, channels, tickets, the inbox
// and the status line all read and write through this package — so the invariants are stated here
// rather than rediscovered per caller.
//
// # THE FIVE INVARIANTS
//
//  1. A STORE IS CREATED BY AN EXPLICIT ACT (§4.2). [Create] is the only function in this package
//     that brings a store into being. [Open] never creates, never repairs and never "initialises on
//     first use"; a caller that finds [ErrNotFound] has found the truth and must say so. This is why
//     Open and Create are two functions and not one Open-or-create: a single function is how a
//     directory quietly appears the first time somebody runs an unrelated command.
//
//  2. A STORE REFUSES A SYNCHRONISING LOCATION (§4.1). Create probes the target's ancestry for
//     evidence that the location is copied off this machine — Dropbox, iCloud Drive, OneDrive, a
//     roaming profile, a network filesystem — and refuses with [ErrPathSynchronising]. The probe
//     looks for evidence on disk; it does not decide by naming an operating system, so it behaves
//     the same on macOS and on Linux. See [DetectSync].
//
//  3. THE THIRD ANSWER IS A REAL ANSWER (§4.3). Every determination this package makes that can
//     fail to be determined returns a [tri.Value], never a bool. Sync detection that could not
//     conclude returns [tri.Undetermined] and Create refuses with [ErrSyncUndetermined] — which is
//     neither the "confirmed local, created" outcome nor the "confirmed synchronising, refused"
//     outcome, and is deliberately its own error value so a caller cannot collapse it into either.
//     Whether an undetermined location SHOULD be creatable is not settled by the PRD; see the
//     "OPEN DECISION" note on [ErrSyncUndetermined].
//
//  4. A RECORD IS EITHER ABSENT OR COMPLETE, NEVER PARTIAL (§3.14). Every write goes to a temporary
//     file in the SAME directory as its destination, is fsynced, is renamed over the destination,
//     and the destination's directory is then fsynced. A process killed at any point leaves either
//     the previous content or the new content — never a truncated record, and never a record that
//     reads back as a real one with missing fields. Readers ignore anything that is not a completed
//     record file, so an abandoned temporary is invisible rather than half-readable. Each record
//     additionally carries a checksum of its payload, so a record damaged beneath the product
//     (rather than by an interrupted write) is reported as [ErrUnreadable] and never as an absence.
//
//  5. THE STORE IS THE SOLE HOME OF UNPUBLISHED DATA (§3.14). This package writes record content
//     nowhere but under [Store.Path] — no temporary directory, no cache, no log. Its temporary
//     files live inside the store because os.Rename is only atomic within one filesystem, and
//     because a draft in /tmp is a second copy of something the product promised had only one.
//
// # WHAT A RECORD IS, AND WHAT IT IS NOT
//
// A [Record] is a kind, an id and an opaque payload. This package does not know what a ticket is
// and must not learn: tickets, draft notes, channel cursors and project state are all records with
// different [Kind] values and different payloads, owned by their own packages. The store's job is
// that the bytes come back exactly as they went in, or do not come back at all.
package store
