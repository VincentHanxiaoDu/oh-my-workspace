// The extension inventory, as the control API reports it (Issue #21 criterion 20; PRD §4.3, "the
// control API and the CLI report the same state").
//
// # WHY THE CONTROL API CARRIES THIS AT ALL
//
// Criterion 20: "Extension state is reported identically by the CLI and by the control API. A test
// reading a failed-to-load extension through both surfaces sees the same state and the same failure
// reason." A criterion of that shape cannot be satisfied by a CLI alone; there has to be a second
// surface for the first one to agree with, and the control API is the surface this product has.
//
// # THEY AGREE BECAUSE THERE IS ONE RENDERER, NOT BECAUSE TWO WERE WRITTEN CAREFULLY
//
// `extension.Entry.Render` is what both print, and `extension.Inventory` is what both ask. This
// file adds no formatting and no second projection — it puts the same []Entry on the wire. Issue
// #18 learned that lesson the hard way on the model view: `omw model show` grew two extra lines
// that `omw daemon status` did not have, and the agreement test went red on all four
// configurations. The fix that holds is for there to be nothing a surface may add.
//
// # THE TYPE THAT CROSSES THE WIRE HAS NOWHERE TO PUT A CREDENTIAL
//
// `extension.Entry` is Name, Interface, State and Detail (criterion 22). Like `model.View`, it is
// a struct with no field a credential fits in, rather than a struct somebody remembers to redact.
//
// # THIS OPENS NO CONNECTION AND STARTS NOTHING
//
// It reads the store's extension records and asks each offered extension whether it loads, and
// `extension.Extension.Load` is documented as contacting nothing. §4.2 is unaffected.
package daemon

import (
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// extensionsFor resolves the extension inventory for a store, for inclusion in a Report.
//
// # A STORE THAT WILL NOT OPEN STILL HAS AN INVENTORY
//
// `extension.Inventory` is given a nil store and answers with what this build SHIPS — the built-in
// Teams and email channels — plus everything offered and unregistered. That is honest and it is
// the same answer the CLI gives in the same situation: reporting a person whose store will not open
// as a person with no channels would be a failure to read rendered as a determined nothing, which
// is the collapse §4.3 forbids.
//
// It is a var so a test can drive the Report's extension rendering without arranging a store on
// disk. The agreement test between the CLI and the control API deliberately does NOT stub it: a
// stub proves two renderers agree about a value, and only the real resolution proves they agree
// about the person's machine.
var extensionsFor = func(storeRoot string) []extension.Entry {
	s, err := store.Open(storeRoot)
	if err != nil {
		entries, _ := extension.Inventory(nil, extension.Default)
		return entries
	}
	entries, _ := extension.Inventory(s, extension.Default)
	return entries
}
