// Issue #9, `review` mode: the check that runs on the person's own machine, with their own model
// and their own key (PRD §3.13, and the §5.2 ruling — the hub is not consulted and cannot be).
//
// THREE OUTCOMES, AND THE THIRD IS NOT A PASS. A model that cannot be reached, one that errors, and
// one that answers something nobody can act on are all the same thing from here: the rules were not
// checked. The two ways to get that wrong are to publish anyway (the draft goes out unchecked,
// which is what the person chose review to prevent) and to report it as a refusal (the person goes
// looking for which rule they broke, and there isn't one). So the zero value of a verdict is
// undetermined, exactly as tri.Undetermined is, and it takes a positive answer to move off it.
package drafts

import (
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ErrModelUnreachable — the configured model could not be reached or errored.
var ErrModelUnreachable = &hub.Error{
	Code: "model-unreachable",
	Msg:  "your model could not be reached, so your rules were not checked",
}

// ErrReviewUnusable — the model answered with something that is not a verdict.
var ErrReviewUnusable = &hub.Error{
	Code: "review-unusable",
	Msg:  "your model answered, and its answer was not a verdict, so your rules were not checked",
}

// Verdict is what a completed review concluded.
type Verdict int

const (
	// VerdictUndetermined is the ZERO VALUE, for the same reason tri.Undetermined is: a verdict
	// nobody established must not read as a pass, and a struct returned from an error path must
	// not carry one.
	VerdictUndetermined Verdict = iota
	// VerdictPassed — the rules were checked and the draft passed.
	VerdictPassed
	// VerdictRefused — the rules were checked and the draft was refused.
	VerdictRefused
)

// Outcome is a review's result.
type Outcome struct {
	Verdict Verdict
	// Reason is the model's wording on a refusal, or why nothing was concluded.
	Reason string
}

// Reviewer is the person's model, seen from here.
//
// IT RETURNS THE MODEL'S RAW ANSWER, not an Outcome. A model does not return an enum; it returns
// text, and the interesting failure — "returns nothing usable" in criterion 16 — happens exactly at
// the seam where text becomes a verdict. An interface that returned an Outcome would put that seam
// inside each implementation, where it cannot be tested once and cannot be got right twice.
type Reviewer interface {
	// Review is given the person's rules verbatim and the draft body, and returns whatever the
	// model said.
	Review(rules, body string) (string, error)
}

// Interpret turns a model's answer into an outcome.
//
// It recognises two answers and nothing else. Anything it does not recognise — empty, whitespace,
// an apology, a wall of reasoning with no conclusion, a JSON blob from a model that ignored the
// instruction — is UNDETERMINED. The alternative, "if it doesn't say refuse then it passed", is a
// default-open gate: every failure mode of every model becomes a publication.
func Interpret(answer string) Outcome {
	trimmed := strings.TrimSpace(answer)
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "pass":
		return Outcome{Verdict: VerdictPassed}
	case lower == "refuse":
		return Outcome{Verdict: VerdictRefused, Reason: "your model refused this draft and gave no reason"}
	case strings.HasPrefix(lower, "refuse:"):
		reason := strings.TrimSpace(trimmed[len("refuse:"):])
		if reason == "" {
			reason = "your model refused this draft and gave no reason"
		}
		return Outcome{Verdict: VerdictRefused, Reason: reason}
	case trimmed == "":
		return Outcome{Reason: "your model returned nothing"}
	default:
		return Outcome{Reason: "your model's answer was not a verdict: " + firstLine(trimmed)}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// Check runs the review and never lets an error become a pass.
func Check(r Reviewer, rules, body string) Outcome {
	if r == nil {
		return Outcome{Reason: "no model is configured, so nothing checked your rules"}
	}
	answer, err := r.Review(rules, body)
	if err != nil {
		// THE ERROR IS THE ANSWER'S ABSENCE. It is not consulted for a verdict, and the answer text
		// is ignored entirely on an error path — a model that errors after emitting the word "pass"
		// has not passed anything.
		return Outcome{Reason: "your model could not be reached or errored: " + err.Error()}
	}
	return Interpret(answer)
}

// StateFor maps an outcome to the state the draft is left in. There is no branch that leaves a
// draft looking untouched.
func (o Outcome) StateFor() State {
	switch o.Verdict {
	case VerdictPassed:
		return StateCleared
	case VerdictRefused:
		return StateRefused
	default:
		return StateReviewUndetermined
	}
}

// Render is the one rendering of a review's outcome, and the three branches are pairwise distinct
// in what they claim: one says the rules were checked and nothing objected, one says they were
// checked and something did, and one says they were not checked at all.
func (o Outcome) Render() string {
	switch o.Verdict {
	case VerdictPassed:
		return "review: checked against your rules and passed"
	case VerdictRefused:
		s := "review: checked against your rules and refused"
		if o.Reason != "" {
			s += "\n  " + o.Reason
		}
		return s
	default:
		s := "review: " + tri.Undetermined.String() + " — your rules were NOT checked, so this is neither a pass nor a refusal"
		if o.Reason != "" {
			s += "\n  " + o.Reason
		}
		return s
	}
}
