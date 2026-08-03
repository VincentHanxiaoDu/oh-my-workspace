package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// LastUse is when a session was last used, in the three answers §4.3 requires.
//
// CRITERION 18 AND 19 ARE ABOUT THE SAME MISTAKE FROM TWO SIDES. A session that has never been
// used must not be omitted from the listing and must not be shown as if it had been used; a
// last-use that could not be established must not render as "never used". Those are three states
// and a `time.Time` has one zero value, so a bare timestamp cannot carry them: an unset time and a
// session used at the epoch are the same bits. Hence the tri.
//
//	tri.Yes           used, and At says when
//	tri.No            DETERMINED never used — the session exists and nothing has ever presented it
//	tri.Undetermined  the hub could not say. Not "never".
type LastUse struct {
	State tri.Value
	At    time.Time
}

// neverUsedText and the tri package's own undetermined wording are the two fixed renderings. They
// are compared against each other in the test rather than against literals, because the defect
// this guards is the two becoming equal, not either being some particular phrase.
const neverUsedText = "never used"

// Render gives the three renderings. Every branch is non-empty: silence is not an answer.
func (l LastUse) Render() string {
	switch l.State {
	case tri.Yes:
		if l.At.IsZero() {
			// A "yes" with no timestamp is not a yes. Reporting it as used-at-the-epoch would be
			// a fabricated fact; reporting it as never used would be a determined negative nobody
			// established.
			return tri.Undetermined.String() + " (recorded as used, with no time recorded)"
		}
		return l.At.UTC().Format(time.RFC3339)
	case tri.No:
		return neverUsedText
	default:
		return tri.Undetermined.String()
	}
}

// NeverUsed is the DETERMINED negative, and it is what a freshly minted session gets.
func NeverUsed() LastUse { return LastUse{State: tri.No} }

// UsedAt is a real timestamp.
func UsedAt(t time.Time) LastUse { return LastUse{State: tri.Yes, At: t} }

// UnknownLastUse is the third answer.
func UnknownLastUse() LastUse { return LastUse{State: tri.Undetermined} }

// ScopeSet is a listing entry's scope, and it distinguishes THREE things criterion 24 says must
// never render the same:
//
//	Recorded=false            no scope was recorded for this entry at all
//	Recorded=true, len 0      a scope list was recorded and it is empty
//	Recorded=true, len > 0    a real scope
//
// A `[]Scope` alone collapses the first two — a nil slice and an empty slice both have len 0, and
// `strings.Join` renders both as "". The empty string is also, of course, silence.
type ScopeSet struct {
	Recorded bool
	Scopes   []hub.Scope
}

// RecordedScopes is the ordinary case.
func RecordedScopes(s []hub.Scope) ScopeSet { return ScopeSet{Recorded: true, Scopes: s} }

// NoRecordedScope is an entry the hub has no scope record for.
func NoRecordedScope() ScopeSet { return ScopeSet{} }

// Render gives the three distinguishable renderings.
func (s ScopeSet) Render() string {
	if !s.Recorded {
		return "no scope recorded (" + tri.Undetermined.String() + ")"
	}
	if len(s.Scopes) == 0 {
		return "none — an empty scope list, so this token can do nothing"
	}
	parts := make([]string, 0, len(s.Scopes))
	for _, sc := range s.Scopes {
		parts = append(parts, string(sc))
	}
	return strings.Join(parts, ", ")
}

// Status is a listed session's state. Criterion 21: a revoked session is never shown as active.
type Status int

const (
	// StatusActive — the session works right now.
	StatusActive Status = iota
	// StatusRevoked — its person ended it. Still LISTED (criterion 21 permits either listing it as
	// revoked or removing it; listing it is the choice this Issue made, because a person who has
	// just revoked something wants to see that it happened).
	StatusRevoked
	// StatusExpired — it aged out. Not the same fact as revoked.
	StatusExpired
)

func (s Status) String() string {
	switch s {
	case StatusRevoked:
		return "revoked"
	case StatusExpired:
		return "expired"
	default:
		return "active"
	}
}

// SessionView is one row of "what is signed in as me" (PRD §3.10, criterion 18).
//
// IT CARRIES NO SECRET. Not a redacted one, not a prefix of one — the field is absent, so a
// surface rendering a view cannot leak what it does not have.
type SessionView struct {
	ID       TokenID
	Person   hub.PersonID
	Label    string
	Scopes   ScopeSet
	Status   Status
	LastUse  LastUse
	IssuedAt time.Time
	// Parent is the session this one was delegated from, empty for a session a person signed in
	// themselves. Criterion 17: a delegated token cannot outlive its parent's revocation, and this
	// is the link that makes that checkable rather than asserted.
	Parent TokenID
}

// Render writes one line a person reads, with everything criterion 19 asks for.
func (v SessionView) Render() string {
	label := v.Label
	if label == "" {
		label = "(unnamed)"
	}
	line := fmt.Sprintf("%s  %s\n  scope:     %s\n  status:    %s\n  last used: %s\n",
		v.ID, label, v.Scopes.Render(), v.Status, v.LastUse.Render())
	if v.Parent != "" {
		line += fmt.Sprintf("  delegated from: %s\n", v.Parent)
	}
	return line
}
