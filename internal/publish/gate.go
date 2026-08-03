// Issue #10, product's ruling of 2026-08-03: the person's publication gate lives HERE, beside the
// transfer, and not in whatever command happens to be calling it.
//
// # Why it is in this file and not in the caller
//
// The gate used to live in `omw outbox publish`. Then `omw publish note` appeared, performed the
// real transfer, and did not know the gate existed: a draft in `review` mode with no model — a draft
// the client itself described as "has NOT been checked and will not be published" — went to a real
// hub, exit 0, and left the outbox. Two commands over the same drafts, one of them gated.
//
// The lesson product drew is the one implemented here: a gate in the caller is a gate the NEXT
// caller bypasses, and the next caller is Issue #16's agent API, which now exists. So [Transfer] —
// the single function through which a note can reach a hub — asks the gate itself. There is one
// path to the hub and the check is on it.
//
// # The zero value is UNDETERMINED, and that is the whole design
//
// [PermissionUndetermined] is the zero value of [Permission] for the same reason [tri.Undetermined]
// and [drafts.VerdictUndetermined] are theirs: a permission nobody established must never read as
// one that was granted. Every way of failing to reach an answer — a mode that cannot be read, a
// model whose configuration cannot be determined, rules that cannot be read, a model that cannot be
// reached or answers something that is not a verdict, a [Config] carrying no gate at all — lands on
// the zero value, and the zero value does not publish. Getting this backwards is the defect the
// ruling exists to prevent, so it is structural rather than remembered: there is no `return
// PermissionGranted` anywhere except the two places an answer was positively established.
package publish

import (
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Permission is what a gate concluded about one draft.
type Permission int

const (
	// PermissionUndetermined is the ZERO VALUE. Whether this draft may leave was not established,
	// which is neither a grant nor a refusal and must never become either. It does not publish.
	PermissionUndetermined Permission = iota
	// PermissionGranted — the draft may leave. Reached only from a positively established answer.
	PermissionGranted
	// PermissionRefused — the draft may NOT leave, and this is a determined fact about it.
	PermissionRefused
)

// Decision is a gate's answer, in the terms [Transfer] needs to report and exit on.
type Decision struct {
	Permission Permission
	// Code is the stable error code, when there is one.
	Code string
	// Detail is the sentence underneath.
	Detail string
}

// Gate decides whether one draft may leave this machine.
//
// IT IS CONSULTED BY [Transfer] AND BY NOTHING ELSE, which is the point of it. A caller cannot
// substitute its own judgement, because a caller does not get to reach the hub without coming
// through here.
type Gate interface {
	// MayLeave reports whether this draft may leave. It is given the outbox because a gate that
	// checks a draft's text has to read it, and the draft's text is there.
	MayLeave(o *drafts.Outbox, id hub.NoteID) Decision
}

// ErrNoGate — a transfer was attempted with no gate supplied.
//
// UNDETERMINED, NOT PERMITTED. A [Config] with no [Gate] is a caller that has not said what the
// person's rules are, and "nobody told me about a gate" is not "there is no gate". The alternative —
// treating an absent gate as an open one — reintroduces the exact defect this file exists for, at
// the one seam where a new caller is most likely to arrive.
var ErrNoGate = &hub.Error{
	Code: "no-gate",
	Msg:  "no publication gate was supplied, so whether this draft may leave your machine is not known",
}

// ErrGateRefused — the person's own gate refused this draft. NOT a hub refusal: nothing was sent
// and the hub never saw it. The two must not share a code any more than they share an exit code.
var ErrGateRefused = &hub.Error{
	Code: "gate-refused",
	Msg:  "your publication gate refused this draft, so nothing was sent",
}

// gateDecision is the ONE place a gate is consulted, and the one place a missing gate is answered.
//
// A nil check written at the call site would be a nil check somebody can forget to write at the
// next call site. There is one call site, in [Transfer], and it cannot reach a hub around this.
func gateDecision(g Gate, o *drafts.Outbox, id hub.NoteID) Decision {
	if g == nil {
		return Decision{Code: ErrNoGate.Code, Detail: ErrNoGate.Msg}
	}
	return g.MayLeave(o, id)
}

// ReviewGate is the person's chosen mode, applied to one draft.
//
// It is the same rule `omw outbox review` applies, reading the same three records through the same
// functions in [drafts] — [drafts.ReadMode], [drafts.ReadModel], [drafts.ReadRules] and
// [drafts.Check]. It is deliberately NOT a second implementation of the review: a second spelling of
// "may this leave" is a second chance to answer it differently.
type ReviewGate struct {
	// Store holds the person's mode and rules.
	Store *store.Store
	// Model is what the review would run with.
	Model drafts.ModelConfig
	// Reviewer is the person's model, seen from here. A nil Reviewer is UNDETERMINED and not a
	// pass — see [drafts.Check], which is where that is decided rather than here.
	Reviewer drafts.Reviewer
}

// MayLeave applies the person's mode.
//
// EVERY BRANCH THAT DOES NOT ESTABLISH AN ANSWER RECORDS WHY ON THE DRAFT, exactly as the `outbox`
// gate does, so a person whose drafts are sitting still can find out that they are blocked rather
// than merely unattended. That is Issue #9's criterion and it survives the move.
func (g ReviewGate) MayLeave(o *drafts.Outbox, id hub.NoteID) Decision {
	ms := drafts.ReadMode(g.Store)
	if ms.Known != tri.Yes {
		// WHICH MODE IS IN EFFECT IS NOT KNOWN. Not `manual` and not `auto`: answering either would
		// be reporting a choice the person may not have made, about the one setting that decides
		// whether their writing leaves the machine.
		return Decision{Code: drafts.ErrModeUnreadable.Code,
			Detail: drafts.ErrModeUnreadable.Msg + "; " + ms.Why}
	}
	if ms.Mode != drafts.ModeReview {
		// `manual` and `auto` have no gate of their own. In `manual` the person's act IS the gate,
		// and running this command is that act; `auto` is the deliberate absence of one.
		return Decision{Permission: PermissionGranted}
	}

	switch g.Model.Configured {
	case tri.No:
		// THE CENTRAL REFUSAL, and it names the mode. The person chose `review` and there is
		// nothing to review with, so this draft has not been checked and does not leave.
		_ = o.SetState(id, drafts.StateBlocked,
			"you chose review mode and no model is configured, so nothing can check your rules")
		return Decision{Permission: PermissionRefused, Code: drafts.ErrNoModel.Code,
			Detail: "you chose " + string(drafts.ModeReview) + " mode and " + g.Model.Missing +
				", so this draft has NOT been checked and was not sent"}
	case tri.Undetermined:
		_ = o.SetState(id, drafts.StateBlocked,
			"you chose review mode and whether a model is configured could not be determined")
		return Decision{Code: drafts.ErrModelUndetermined.Code,
			Detail: "you chose " + string(drafts.ModeReview) + " mode and " + drafts.ErrModelUndetermined.Msg}
	}

	rules := drafts.ReadRules(g.Store)
	if rules.Recorded == tri.Undetermined {
		// Rules that cannot be read are not "no rules". Checking against an empty rule set passes
		// everything, which is `auto` wearing `review`'s name.
		_ = o.SetState(id, drafts.StateReviewUndetermined, "your rules could not be read, so nothing was checked")
		return Decision{Code: drafts.ErrRulesUnreadable.Code, Detail: drafts.ErrRulesUnreadable.Msg}
	}

	body, berr := latestBody(o, id)
	if berr != nil {
		_ = o.SetState(id, drafts.StateReviewUndetermined, "this draft's text could not be read, so nothing was checked")
		return Decision{Code: hub.Code(berr), Detail: "this draft's text could not be read, so nothing was checked: " + berr.Error()}
	}

	outcome := drafts.Check(g.Reviewer, rules.Text, body)
	_ = o.SetState(id, outcome.StateFor(), outcome.Reason)
	switch outcome.Verdict {
	case drafts.VerdictPassed:
		return Decision{Permission: PermissionGranted}
	case drafts.VerdictRefused:
		return Decision{Permission: PermissionRefused, Code: ErrGateRefused.Code,
			Detail: "your rules refused this draft: " + outcome.Reason}
	default:
		// CHECKED NOTHING. Not a pass and not a refusal — the person goes looking for the rule they
		// broke and there isn't one, so saying "refused" here would send them after a fiction.
		return Decision{Code: drafts.ErrModelUnreachable.Code,
			Detail: "your rules were NOT checked, so this is neither a pass nor a refusal: " + outcome.Reason}
	}
}
