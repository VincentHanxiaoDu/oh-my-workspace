// Issue #10, the two things the transfer needs from the outbox and nothing else does.
//
// A NEW FILE RATHER THAN TWO METHODS ADDED TO outbox.go, because Issue #9 and Issue #10 are two
// branches and a shared file is a conflict with no design reason behind it.
//
// [Outbox.Remove] is the ONLY way anything in this repository takes a draft out of the outbox, and
// it exists because PRD §2.3 has exactly two containers: leaving a published note in the outbox
// would make "never both" false, and there is no third place to move it to.
package drafts

import (
	"os"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// DraftDir is where one draft's revisions live. It refuses a name that cannot be a directory entry,
// with the same rule and the same code every other entry point in this package uses — a second
// spelling of that rule is a second chance to get it wrong.
//
// It does not create the directory and does not require it to exist: a caller asking where a draft
// WOULD live is asking a legitimate question, and answering it is not the same as answering whether
// there is one.
func (o *Outbox) DraftDir(id hub.NoteID) (string, error) { return o.pathFor(id) }

// Remove takes a draft out of the outbox, with everything under it.
//
// IT IS NOT IDEMPOTENT BY ACCIDENT, IT IS IDEMPOTENT ON PURPOSE. Removing a draft that is already
// gone succeeds, because the caller that needs this is finishing a removal a previous process was
// killed part-way through, and a second attempt meeting "already gone" has got what it wanted.
// A caller wanting to know whether the draft was there asks [Outbox.StateOf] first.
func (o *Outbox) Remove(id hub.NoteID) error {
	dir, err := o.pathFor(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
