package hubd

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"

// OperatorReach is what this process can read, stated plainly (PRD §2.4; Issue #103 criteria 3
// and 9).
//
// WHY IT IS BUILT FROM hub.RestrictionStatement RATHER THAN WRITTEN AFRESH. The client already says
// this sentence at every point where a person narrows a note. If the server said it in its own
// words, there would be two statements of the same promise, and the one nobody greps is the one
// that gets softened — "your restricted notes are kept separately", "the hub only indexes
// metadata". Criterion 9 is "no more, and no quieter", and the cheapest way to hold a wording
// steady is to have one of it. `hub.CheckSurface` is run over this text in the test, the same rule
// the client's surfaces are held to.
//
// The three sentences added around it are the OPERATOR-SIDE half, which the client has no reason to
// say: what a person running this process can do with the directory it holds. They add no reach
// beyond §2.4 — they describe the same reach from the other side of it, which is the whole point of
// the section being called "what the hub can read, stated plainly".
const OperatorReach = "What this hub can read:\n" +
	"\n" +
	"  " + hub.RestrictionStatement + "\n" +
	"\n" +
	"  Every published note is held in this hub's directory in full — its title, its body, every\n" +
	"  version of it, its author, and who it was narrowed to. Whoever can read that directory can\n" +
	"  read all of it, without a token and without any note's author being told. That is a\n" +
	"  deployment fact about running a hub; it is not a permission, and there is no scope that\n" +
	"  grants or withholds it.\n" +
	"\n" +
	"  What this hub does NOT hold is anything nobody published: no tickets, no drafts, and no\n" +
	"  outbox. Those never leave the machine they were made on."

// CheckOperatorReach runs the product's own §2.4 rule over this package's statement, so that the
// server's disclosure is held to the same standard as every client surface that offers a narrowing.
//
// It is exported so the test drives the REAL rule rather than a copy of the reasoning behind it.
// offersChoice is true: this text enumerates the narrowings, and a surface that names them is a
// surface that must carry the statement.
func CheckOperatorReach() error {
	return hub.CheckSurface("hubd operator reach", OperatorReach, true)
}
