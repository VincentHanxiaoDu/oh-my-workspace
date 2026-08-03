// The driver: one draft, one attempt, and the order the durable writes happen in.
//
// # The order IS the correctness
//
//  1. No hub configured?      → answer, having opened nothing. Nothing is written.
//  2. Read the ledger.        → an outstanding attempt is REUSED, key and all.
//  3. Write phase=in-flight.  → durable, BEFORE the dial.
//  4. Dial.
//     failed to connect     → clear the record. Nothing was sent, so `drafted` is the truth.
//     connected             → send, read the reply.
//  5. published → phase=published (durable), then delete the draft.
//     refused   → phase=refused with the hub's reason.
//     no answer → the record stays at in-flight. That is `in flight`, and it is honest.
//
// Step 3 before step 4 is the whole of "interrupted means not published". A key written after the
// send would be lost by exactly the interruption it exists to survive, and the retry would arrive
// as a new attempt and make the second copy.
//
// Step 1 before anything else is PRD §4.2 read literally: with no hub there is not a connection
// that fails fast, there is no connection.
package publish

import (
	"fmt"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Attempt is what one call to [Transfer] concluded. It is separate from [State] because they answer
// different questions: a State is where the note IS, an Attempt is what just happened to it, and
// two attempts with different outcomes can leave a note in the same state.
type Attempt int

const (
	// AttemptUndetermined is the ZERO VALUE, for the same reason [tri.Undetermined] is: a result
	// nobody established must not read as anything else. It means the request was sent and the
	// outcome was never learned.
	AttemptUndetermined Attempt = iota
	// AttemptPublished — the hub has it.
	AttemptPublished
	// AttemptAlreadyPublished — the hub had it already, from an earlier attempt with this key. A
	// SUCCESS, and the observable form of "a person retries and does not get two copies".
	AttemptAlreadyPublished
	// AttemptRefused — the hub was asked and said no.
	AttemptRefused
	// AttemptUnreachable — no connection was established, so nothing was sent and nothing was
	// considered. NOT a refusal (criterion 8) and not a determination about the note.
	AttemptUnreachable
	// AttemptNoHub — no hub is configured on this machine. A determined fact, settled without
	// opening anything (criterion 10).
	AttemptNoHub
	// AttemptLocalFailure — something on this machine stopped the attempt before it began: no such
	// draft, an unreadable revision, a ledger entry that could not be written.
	AttemptLocalFailure
	// AttemptGateRefused — the PERSON'S OWN gate refused this draft. Nothing was sent and the hub
	// never saw it. Distinct from [AttemptRefused], which means the hub was asked and said no:
	// "your rules said no" and "the hub said no" are different facts about different judges, and a
	// person told the wrong one goes looking in the wrong place.
	AttemptGateRefused
	// AttemptGateUndetermined — whether this draft may leave could not be established, so it did
	// not. Not a refusal: there is no rule the person broke. Not a pass either.
	AttemptGateUndetermined
)

// Result is what a caller needs to report and to exit on.
type Result struct {
	Attempt Attempt
	// Report is where the note stands AFTER the attempt, read back from the ledger rather than
	// composed from what this function believes it did. A report composed from intentions agrees
	// with the code and not with the disk.
	Report Report
	// Code is the stable error code, when there is one.
	Code string
	// Detail is the sentence underneath.
	Detail string
	// Fresh reports whether this attempt is what created the note on the hub.
	Fresh bool
}

// Config is what a transfer needs from outside the outbox.
type Config struct {
	// HubAddr is $OMW_HUB. Empty means no hub is configured, which is a determined fact and not a
	// failure to reach one.
	HubAddr string
	// Author is who the note is published as. Issue #19 owns sign-in; until it lands this comes
	// from the environment — see the CLI.
	Author hub.PersonID
	// Scopes are what the caller holds. Publishing requires `publish` (PRD §3.10); the hub decides,
	// not this client, because a client that filtered its own requests would report a refusal the
	// hub never made.
	Scopes []hub.Scope
	// Title is the note's title on the hub.
	Title string
	// Gate is the person's publication gate, and [Transfer] refuses without one. See gate.go: a
	// nil Gate is UNDETERMINED, never permitted, because "no caller told me about a gate" is not
	// "this person has no gate".
	Gate Gate
}

// ErrNoAuthorConfigured — nothing says who this note would be published as.
var ErrNoAuthorConfigured = &hub.Error{
	Code: "no-author-configured",
	Msg:  "refused: nothing on this machine says who you are, and a note is published as somebody",
}

// Transfer attempts to publish one draft, and returns where it stands afterwards.
func Transfer(l *Ledger, o *drafts.Outbox, id hub.NoteID, cfg Config) Result {
	before := StateOf(l, o, id)

	// A NOTE THIS CLIENT DOES NOT KNOW ABOUT, or one whose record could not be read. Neither is a
	// publication attempt, and neither may quietly become one.
	switch {
	case before.Exists == tri.No:
		return Result{Attempt: AttemptLocalFailure, Report: before, Code: drafts.ErrNoSuchDraft.Code,
			Detail: fmt.Sprintf("there is no draft %q in your outbox", string(id))}
	case before.Known != tri.Yes:
		return Result{Attempt: AttemptLocalFailure, Report: before, Code: firstNonEmpty(before.Code, ErrJournalUnreadable.Code),
			Detail: "where this note stands could not be read, so nothing was sent: " + before.Why}
	}

	// ALREADY PUBLISHED. Finish the deletion this client owes and say so; do not send anything.
	if before.State == StatePublished {
		if err := o.Remove(id); err != nil {
			return Result{Attempt: AttemptAlreadyPublished, Report: before, Detail: "this note is on the hub; the local draft could not be removed: " + err.Error()}
		}
		return Result{Attempt: AttemptAlreadyPublished, Report: StateOf(l, o, id),
			Detail: "this note was already published by an earlier attempt; nothing was sent and no second copy was made"}
	}

	// CRITERION 10, AND IT IS THE FIRST THING THAT COULD REACH OUT. With no hub there is no dial,
	// no ledger write, and no change of any kind — criterion 12's "unchanged and re-publishable".
	if strings.TrimSpace(cfg.HubAddr) == "" {
		return Result{Attempt: AttemptNoHub, Report: before, Code: hub.ErrNoHubConfigured.Code,
			Detail: "no hub is configured on this machine, so nothing was opened and nothing was sent"}
	}
	// THE GATE, AND NOTHING REACHES THE HUB WITHOUT IT (product's ruling, 2026-08-03).
	//
	// It sits here — after "no hub configured", which opens nothing and changes nothing, and before
	// the body is read, the key is minted, the record is written or anything is dialled. Everything
	// below this line is the transfer; nothing below it runs unless the gate granted.
	//
	// A REFUSAL AND AN UNDETERMINED ANSWER ARE DIFFERENT RESULTS with different exit codes, and
	// neither is [AttemptRefused]: that one means the HUB was asked and said no, and the hub has
	// not been asked anything here. Conflating them would be the refused-vs-unreachable collapse
	// again, one layer up.
	switch d := gateDecision(cfg.Gate, o, id); d.Permission {
	case PermissionGranted:
	case PermissionRefused:
		return Result{Attempt: AttemptGateRefused, Report: StateOf(l, o, id), Code: d.Code, Detail: d.Detail}
	default:
		return Result{Attempt: AttemptGateUndetermined, Report: StateOf(l, o, id), Code: d.Code, Detail: d.Detail}
	}

	if strings.TrimSpace(string(cfg.Author)) == "" {
		return Result{Attempt: AttemptLocalFailure, Report: before, Code: ErrNoAuthorConfigured.Code,
			Detail: ErrNoAuthorConfigured.Msg}
	}

	body, berr := latestBody(o, id)
	if berr != nil {
		return Result{Attempt: AttemptLocalFailure, Report: before, Code: hub.Code(berr),
			Detail: "this draft's text could not be read, so nothing was sent: " + berr.Error()}
	}

	// THE ATTEMPT KEY IS REUSED IF THERE IS ONE. This is the single line that makes a retry a retry
	// rather than a new publication, and every path that leaves the record in place — in flight,
	// refused — is relying on it.
	key := before.Attempt
	if key == "" {
		k, err := mintAttemptKey()
		if err != nil {
			return Result{Attempt: AttemptLocalFailure, Report: before, Code: hub.Code(err), Detail: err.Error()}
		}
		key = k
	}

	// DURABLE, AND BEFORE THE DIAL. See the file comment.
	if err := l.write(id, record{Attempt: key, Phase: phaseInFlight}); err != nil {
		return Result{Attempt: AttemptLocalFailure, Report: StateOf(l, o, id), Code: hub.Code(err),
			Detail: "the attempt could not be recorded, so nothing was sent: " + err.Error()}
	}

	scopes := make([]string, 0, len(cfg.Scopes))
	for _, s := range cfg.Scopes {
		scopes = append(scopes, string(s))
	}
	resp, sent, err := send(cfg.HubAddr, Request{
		Attempt: key, Author: string(cfg.Author), Title: cfg.Title, Body: body, Scopes: scopes,
	})

	switch {
	case err != nil && !sent:
		// NOTHING LEFT THE MACHINE. The record is cleared, because an attempt that was never made
		// must not leave a note looking as though its fate is unknown. This is the branch criterion
		// 8 and criterion 9 are about, and it is emphatically not a refusal.
		if cerr := l.clear(id); cerr != nil {
			return Result{Attempt: AttemptUnreachable, Report: StateOf(l, o, id), Code: hub.Code(err),
				Detail: err.Error() + "; the attempt record could not be cleared: " + cerr.Error()}
		}
		return Result{Attempt: AttemptUnreachable, Report: StateOf(l, o, id), Code: hub.ErrHubUnreachable.Code,
			Detail: "the hub could not be reached, so this note was never considered by it: " + err.Error()}

	case err != nil:
		// SENT, AND THE OUTCOME IS NOT KNOWN (criterion 13). The record stays at in-flight, which
		// keeps the key and makes the retry safe.
		return Result{Attempt: AttemptUndetermined, Report: StateOf(l, o, id), Code: hub.Code(err),
			Detail: "the request was sent and its outcome " + tri.Undetermined.String() + ": " + err.Error()}

	case resp.Outcome == outcomeRefused:
		if werr := l.write(id, record{Attempt: key, Phase: phaseRefused, Reason: resp.Reason, Code: resp.Code}); werr != nil {
			return Result{Attempt: AttemptRefused, Report: StateOf(l, o, id), Code: resp.Code,
				Detail: "the hub refused this note and the refusal could not be recorded: " + werr.Error()}
		}
		return Result{Attempt: AttemptRefused, Report: StateOf(l, o, id), Code: resp.Code, Detail: resp.Reason}
	}

	// PUBLISHED. The ledger is written FIRST and the draft removed after: the rename is the moment
	// the note changes container, and a deletion that happens before it would leave the note in
	// neither container if the machine died between the two.
	if werr := l.write(id, record{Attempt: key, Phase: phasePublished, HubID: resp.NoteID}); werr != nil {
		// The hub has it and this client could not write that down. Undetermined, not published:
		// claiming a publication we cannot re-read after a restart is the claim §4.3 forbids.
		return Result{Attempt: AttemptUndetermined, Report: StateOf(l, o, id), Code: hub.Code(werr),
			Detail: "the hub accepted this note and the outcome could not be recorded: " + werr.Error()}
	}
	after := StateOf(l, o, id)
	if rerr := o.Remove(id); rerr != nil {
		return Result{Attempt: AttemptPublished, Report: after, Fresh: resp.Fresh,
			Detail: "published; the local draft could not be removed: " + rerr.Error()}
	}
	if !resp.Fresh {
		return Result{Attempt: AttemptAlreadyPublished, Report: after, Fresh: false,
			Detail: "the hub had already published this attempt, so this retry made no second copy"}
	}
	return Result{Attempt: AttemptPublished, Report: after, Fresh: true}
}

// latestBody reads the draft's most recent revision through Issue #11's version machinery, so an
// unreadable revision is undetermined here too rather than an empty note published over the top of
// somebody's writing.
func latestBody(o *drafts.Outbox, id hub.NoteID) (string, error) {
	versions, err := o.Timeline(id, "")
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", hub.Refusedf(drafts.ErrNoSuchDraft, "%q has no revisions", string(id))
	}
	return versions[len(versions)-1].Body, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
