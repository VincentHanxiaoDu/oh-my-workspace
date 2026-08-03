// The daemon's copy of the auth answer, so that the control API and the CLI cannot disagree about
// it (Issue #19 criterion 23; PRD §4.3, "the control API and the CLI report the same state").
//
// WHY THIS IS THREE LINES AND NOT A SUBSYSTEM. The tempting shape is for the daemon to track
// sign-in state of its own — a field, updated when something happens. That is a second answer, and
// a second answer is a thing that can be stale exactly when the CLI's is not. Instead both callers
// reach the SAME function, [auth.Observe], against the same store root and the same hub setting,
// so agreement is structural. The test for criterion 23 then confirms the two surfaces reach it,
// rather than diffing two computations and hoping.
//
// WHY THE HUB SETTING IS READ FROM THE PROCESS ENVIRONMENT HERE. `auth.HubConfigured` is one
// reader of one variable and every surface calls it; the daemon has no injected Getenv to hand,
// and inventing one for this would be a second way to configure a hub.

package daemon

import (
	"os"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/auth"
)

// authStateFor answers the sign-in question for a store.
//
// It is a var so a test can drive the REPORTING of a state without arranging a credential; the
// agreement test deliberately does not stub it, for the same reason liveness.go's agreement tests
// do not stub theirs — a stub proves the rendering and nothing else.
var authStateFor = func(storeRoot string) (text, detail, code string) {
	st := auth.Observe(storeRoot, auth.HubConfigured(os.Getenv), auth.Unreachable{})
	return st.Text, st.Detail, st.Code
}
