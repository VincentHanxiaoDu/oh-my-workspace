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
	"errors"
	"os"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// modelViewFor resolves the model configuration for a store, for inclusion in a Report.
//
// It is a var so that a test can drive the Report's model rendering without arranging a store on
// disk. The agreement test between the CLI and the control API deliberately does NOT stub it: a
// stub proves two renderers agree about a value, and only the real resolution proves they agree
// about the person's machine.
var modelViewFor = func(storeRoot string) model.View {
	return modelConfigFor(storeRoot, os.Getenv).View()
}

// modelConfigFor keeps the THREE outcomes of [store.Open] as three (Issue #68).
//
// AN UNREADABLE STORE IS NOT A STORE WITH NO MODEL IN IT. [model.Read] takes a nil store to mean
// "this caller has no store", which is a DETERMINED fact and renders as "no provider is chosen".
// That reading is right for a machine with no store and it is a lie for a machine whose store is
// there and would not open. This function used to collapse every store.Open error into that nil,
// so `omw daemon status` answered "no provider is chosen" about a store nobody could read, byte
// for byte identical to the honest negative — while `omw model show`, looking at the same store in
// the same moment, said it could not be read and exited 3. `could not determine` and `determined
// to be nothing` do not share a rendering here (§4.3, criterion 18).
//
// "The rest of the Report already says the store is unreadable" was the argument for the old
// shape, and it is not enough: a caller filtering for the model answer — which is exactly what an
// agent API consumer does — gets the determined negative alone, with nothing beside it.
func modelConfigFor(storeRoot string, getenv func(string) string) model.Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	s, err := store.Open(storeRoot)
	switch {
	case err == nil:
		return model.Read(getenv, s)
	case errors.Is(err, store.ErrNotFound):
		// THERE IS NO STORE, WHICH THE FILESYSTEM ANSWERED. `omw model show` treats this the same
		// way: nothing is recorded anywhere, so the environment alone is the configuration.
		return model.Read(getenv, nil)
	default:
		// THE WORDS ARE THE CLI'S WORDS, NOT A SECOND VOCABULARY. internal/commands' modelStore
		// prints these two sentences and exits 3; carrying them in the Config's Why puts them
		// through [model.View.Render] — the one renderer both surfaces call — so the CLI and the
		// control API cannot word this state differently.
		//
		// THE ENVIRONMENT IS NOT CONSULTED ON THIS PATH, DELIBERATELY. `omw model show` does not
		// read it either once the store failed: half of the configuration could not be seen, so
		// what the other half says is not the answer to "which provider is configured".
		return model.Config{
			Provider:   tri.Undetermined,
			Credential: tri.Undetermined,
			Why: "the store at " + storeRoot + " could not be read: " + err.Error() +
				"\n  An unreadable store is not one with no model recorded in it.",
		}
	}
}
