// The model configuration, as the control API reports it (Issue #18 criterion 18; PRD §4.3, "the
// control API and the CLI report the same state").
//
// # WHY THE CONTROL API CARRIES THIS AT ALL
//
// Criterion 18: "The model configuration state reported through the CLI and through the control API
// is the same state, with the same three-way distinction between configured, not configured, and
// undetermined — with the credential value itself absent from both." A criterion of that shape
// cannot be satisfied by a CLI alone; there has to be a second surface for the first one to agree
// with. The control API is the surface this product has today, and Issue #16's agent API — which
// criterion 6 is really about — is the one that arrives later and must consume the same
// [model.View] rather than build a second projection.
//
// # THE TYPE THAT CROSSES THE WIRE HAS NOWHERE TO PUT A CREDENTIAL
//
// [Report] carries a [model.View], not a model.Config. Config holds the credential in an unexported
// field; View has no field for one at all. `encoding/json` skips unexported fields today, and the
// change that stops it skipping them is somebody exporting a field to fix an unrelated problem —
// so the guarantee is structural rather than a property of the serialiser's current behaviour.
// model.TestTheViewHasNowhereToPutACredential holds that line, and this file is why it matters.
//
// # THIS OPENS NO CONNECTION AND STARTS NOTHING
//
// It reads the store's model record and the process environment. §4.2's "no network connection
// without a hub configured" is unaffected: there is no hub in this path and nothing here dials.
package daemon

import (
	"os"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// modelViewFor resolves the model configuration for a store, for inclusion in a Report.
//
// A store that will not open yields the resolution with NO store — which is honest: the
// environment half of the answer is still readable and is still the person's configuration, and
// the recorded half is reported by [model.Read] as absent rather than as a failure this function
// invented. The store's own unreadability is already the subject of the rest of the Report.
//
// It is a var so that a test can drive the Report's model rendering without arranging a store on
// disk. The agreement test between the CLI and the control API deliberately does NOT stub it: a
// stub proves two renderers agree about a value, and only the real resolution proves they agree
// about the person's machine.
var modelViewFor = func(storeRoot string) model.View {
	s, err := store.Open(storeRoot)
	if err != nil {
		return model.Read(os.Getenv, nil).View()
	}
	return model.Read(os.Getenv, s).View()
}
