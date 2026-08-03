package kindguard

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// repo is the product's own source. The guard that matters runs over this and nothing else.
func repo(t *testing.T) Report {
	t.Helper()
	rep, err := Analyze(os.DirFS("../.."))
	if err != nil {
		t.Fatalf("analysing this repository: %v", err)
	}
	// A SCAN THAT EXAMINED NOTHING IS NOT A PASS. Every assertion below is a negative one, so the
	// analysis is required to have found real kinds first — otherwise a broken walk, a broken
	// parse or a renamed store method makes all of them vacuously true, which is the single mistake
	// that has cost this project the most.
	if len(rep.Reads) == 0 || len(rep.Writes) == 0 {
		t.Fatalf("the analysis found %d reads and %d writes in this repository; it examined nothing meaningful",
			len(rep.Reads), len(rep.Writes))
	}
	if !hasKind(rep.Writes, "ticket") || !hasKind(rep.Reads, "ticket") {
		t.Fatalf("the analysis did not find the inbox writing and reading `ticket`, which it certainly does; "+
			"reads=%v writes=%v", rep.Reads, rep.Writes)
	}
	return rep
}

func hasKind(uses []Use, kind string) bool {
	for _, u := range uses {
		if u.Kind == kind {
			return true
		}
	}
	return false
}

// THE CHECK ITSELF (Issue #67, criterion 6): no store kind in this product is read by one package
// and written by none, unless somebody has said why.
func TestNoStoreKindIsReadByOnePackageAndWrittenByNone(t *testing.T) {
	rep := repo(t)
	for _, v := range rep.Undeclared() {
		t.Errorf("%s.\n"+
			"  A directory nobody writes to reads as zero records, so this does not crash — it renders as a\n"+
			"  confident zero. Either give the kind a producer, stop reading it, or declare it in\n"+
			"  kindguard.Declared with the Issue that will settle it.", v)
	}
}

// THE DECLARATION MECHANISM STILL WORKS, AND IS STILL VISIBLE.
//
// `Declared` is empty today — the one entry it held kept a wrong answer on a person's screen and was
// removed in review of PR #92 by fixing the answer. It is kept because the next known gap will need
// it, and an untested escape hatch is one nobody can trust when they reach for it. A declared
// finding is still REPORTED; the declaration only takes it out of what the build fails on.
func TestADeclaredFindingIsStillReportedAndNotFailed(t *testing.T) {
	prev := Declared
	Declared = map[string]string{"ghost": "Issue #999: recorded, not suppressed"}
	t.Cleanup(func() { Declared = prev })

	rep := analyzeSource(t, preamble+`
const KindGhost = store.Kind("ghost")

func Count(s *store.Store) int { r, _ := s.List(KindGhost); return len(r) }
`)
	if len(rep.Violations) != 1 {
		t.Fatalf("a declared kind is no longer reported as a finding at all: %v", rep.Violations)
	}
	if rep.Violations[0].Declared == "" {
		t.Errorf("the finding does not carry the reason it is tolerated: %+v", rep.Violations[0])
	}
	if len(rep.Undeclared()) != 0 {
		t.Errorf("a declared finding still fails the build: %v", rep.Undeclared())
	}
}

// A DECLARATION CANNOT OUTLIVE THE THING IT EXCUSES.
func TestNoDeclarationIsStale(t *testing.T) {
	rep := repo(t)
	for _, kind := range rep.StaleDeclarations() {
		t.Errorf("kindguard.Declared still excuses store kind %q, which is no longer read-with-no-writer. "+
			"Delete the entry: a declaration nobody removes is how the next one of these gets waved through.", kind)
	}
	for kind, why := range Declared {
		if !strings.Contains(why, "#") {
			t.Errorf("the declaration for %q names no Issue: %q", kind, why)
		}
	}
}

// The indirections this analysis cannot see through are LISTED, not assumed harmless. A new one
// turns this red so that somebody looks at it rather than it being absorbed silently.
func TestTheUnresolvedReadsAreTheKnownOnes(t *testing.T) {
	rep := repo(t)
	// These read kinds they were HANDED rather than kinds they named, which is a different thing
	// from a kind with no writer and cannot be checked here:
	//
	//   internal/reports/activity.go   enumerates every kind in the store and filters by prefix
	//   internal/commands/store.go     `omw store` reports every kind it finds, whatever they are
	//   internal/store/store.go        the store's own generic accessors take the kind as a parameter
	//   internal/status/collect.go     `recordItems` lists every kind [store.Store.Kinds] reports,
	//     one `s.List(k)` per loop variable. There is no literal kind to check because naming one
	//     would be the defect: #101 required the status screen to report whatever the store holds,
	//     and a fixed list would silently omit a kind the store grew. The indirection is the
	//     requirement, so this read is unchecked and recorded as such (#67, #105).
	allowed := map[string]bool{
		"internal/reports/activity.go": true,
		"internal/commands/store.go":   true,
		"internal/store/store.go":      true,
		"internal/status/collect.go":   true,
	}
	for _, u := range rep.Unresolved {
		file := u.Pos[:strings.Index(u.Pos, ":")]
		if !allowed[file] {
			t.Errorf("a store kind is read at %s through an indirection this guard cannot resolve. "+
				"It is therefore unchecked. Either read the kind directly, or add the file here with the reason.", u.Pos)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// ASSERT IT FIRES. Criterion 6's own words: "introduce a read of a never-written kind and watch it
// go red." These drive the analysis against source trees built for the purpose.
// ---------------------------------------------------------------------------------------------

const preamble = "package p\n\nimport \"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store\"\n\n"

func analyzeSource(t *testing.T, src string) Report {
	t.Helper()
	rep, err := Analyze(fstest.MapFS{"internal/p/p.go": &fstest.MapFile{Data: []byte(src)}})
	if err != nil {
		t.Fatalf("analysing the fixture: %v", err)
	}
	return rep
}

func TestAReadOfANeverWrittenKindIsAViolation(t *testing.T) {
	rep := analyzeSource(t, preamble+`
const KindGhost = store.Kind("ghost")

func Count(s *store.Store) int {
	recs, err := s.List(KindGhost)
	if err != nil {
		return 0
	}
	return len(recs)
}
`)
	if len(rep.Undeclared()) != 1 {
		t.Fatalf("a read of a never-written kind produced %d undeclared violations, want 1: %v",
			len(rep.Undeclared()), rep.Violations)
	}
	if got := rep.Undeclared()[0].Kind; got != "ghost" {
		t.Errorf("the violation names kind %q, want ghost", got)
	}
	if !strings.Contains(rep.Undeclared()[0].String(), "internal/p/p.go") {
		t.Errorf("the violation does not say where the read is: %s", rep.Undeclared()[0])
	}
}

// THE SAME KIND, WITH A WRITER, IS NOT A VIOLATION. Without this the check above would also pass
// against an analysis that flagged everything.
func TestAKindWithAWriterIsNotAViolation(t *testing.T) {
	rep := analyzeSource(t, preamble+`
const KindReal = store.Kind("real")

func Count(s *store.Store) int {
	recs, _ := s.List(KindReal)
	return len(recs)
}

func Add(s *store.Store, id string, v any) error {
	return s.PutJSON(KindReal, id, v)
}
`)
	if len(rep.Violations) != 0 {
		t.Errorf("a kind that is both read and written was reported: %v", rep.Violations)
	}
	if !hasKind(rep.Reads, "real") || !hasKind(rep.Writes, "real") {
		t.Errorf("the analysis did not see both halves: reads=%v writes=%v", rep.Reads, rep.Writes)
	}
}

// A WRITE THROUGH A RECORD LITERAL COUNTS — that is how internal/inbox and internal/channels write.
func TestAWriteThroughARecordOrOpLiteralCounts(t *testing.T) {
	rep := analyzeSource(t, preamble+`
const (
	KindA = store.Kind("a")
	KindB = store.Kind("b")
)

func ReadBoth(s *store.Store) {
	s.List(KindA)
	s.Get(KindB, "id")
}

func WriteA(s *store.Store, body []byte) error {
	return s.Put(store.Record{Kind: KindA, ID: "x", Data: body})
}

func WriteB(body []byte) []store.Op {
	return []store.Op{{Kind: KindB, ID: "y", Data: body}}
}
`)
	if len(rep.Violations) != 0 {
		t.Errorf("writes through Record and Op literals were not counted: %v", rep.Violations)
	}
}

// DELETING IS NOT WRITING. A kind that is only ever removed from is still a kind nothing produces.
func TestDeleteIsNotAWriter(t *testing.T) {
	rep := analyzeSource(t, preamble+`
const KindGone = store.Kind("gone")

func Purge(s *store.Store, id string) error {
	if _, err := s.Get(KindGone, id); err != nil {
		return err
	}
	return s.Delete(KindGone, id)
}
`)
	if len(rep.Undeclared()) != 1 {
		t.Fatalf("a kind that is read and deleted but never written produced %d violations, want 1: %v",
			len(rep.Undeclared()), rep.Violations)
	}
}

// THE INDIRECTION THAT ACTUALLY SHIPPED. Blocker 2's read went through a package-level map, so an
// analysis that only understood literal arguments would have missed it entirely.
func TestAReadThroughAPackageLevelMapIsResolved(t *testing.T) {
	rep := analyzeSource(t, preamble+`
const KindGhost = store.Kind("ghost")

var inventoryKinds = map[string]store.Kind{"inventory": KindGhost}

func Inventory(s *store.Store, name string) int {
	kind := inventoryKinds[name]
	recs, _ := s.List(kind)
	return len(recs)
}
`)
	if len(rep.Undeclared()) != 1 {
		t.Fatalf("a read through a package-level map produced %d violations, want 1 (unresolved: %v)",
			len(rep.Undeclared()), rep.Unresolved)
	}
	if got := rep.Undeclared()[0].Kind; got != "ghost" {
		t.Errorf("the violation names %q, want ghost", got)
	}
}

// BOTH SPELLINGS OF A KIND DECLARATION ARE UNDERSTOOD.
//
// `const K store.Kind = "lit"` is how internal/agentapi declares its grant kind, and an analysis
// that only understood the conversion spelling left that whole package unchecked. It was found
// because the unresolved list is asserted rather than ignored; this keeps it found.
func TestATypedKindDeclarationIsResolved(t *testing.T) {
	rep := analyzeSource(t, preamble+`
const KindGhost store.Kind = "ghost"

func Count(s *store.Store) int {
	recs, _ := s.List(KindGhost)
	return len(recs)
}
`)
	if len(rep.Unresolved) != 0 {
		t.Errorf("a typed kind declaration was not resolved: %v", rep.Unresolved)
	}
	if len(rep.Undeclared()) != 1 || rep.Undeclared()[0].Kind != "ghost" {
		t.Fatalf("a typed kind read with no writer produced %v", rep.Violations)
	}
}

// A READ INSIDE A FUNCTION LITERAL IS SEEN.
//
// THIS IS THE SHAPE THE FIX FOR BLOCKER 2 IS ONE REFACTOR AWAY FROM. `internal/diagnostics` now
// holds a package-level map of `recordSource{List: …}`; writing those closures inline instead of as
// named functions is an ordinary tidy-up, and an analysis that only walked `*ast.FuncDecl` bodies
// would have stopped seeing the very code this guard was written to protect — not by flagging it,
// and not by listing it as unresolved, but by silently finding nothing at all. Reviewed on PR #92,
// where reintroducing Blocker 2 through a `func` literal turned diagnostics and commands red and
// left this package green.
func TestAReadInsideAFunctionLiteralIsNotInvisible(t *testing.T) {
	rep := analyzeSource(t, preamble+`
type src struct{ List func(s *store.Store) int }

var sources = map[string]src{
	"draft": {List: func(s *store.Store) int { r, _ := s.List(store.Kind("ghost")); return len(r) }},
}
`)
	if len(rep.Undeclared()) != 1 {
		t.Fatalf("a read inside a function literal produced %d violations, want 1 (reads=%v unresolved=%v)",
			len(rep.Undeclared()), rep.Reads, rep.Unresolved)
	}
	if got := rep.Undeclared()[0].Kind; got != "ghost" {
		t.Errorf("the violation names %q, want ghost", got)
	}
}

// A WRITE INSIDE A FUNCTION LITERAL COUNTS TOO. Without this, walking literals would turn every
// closure-written kind into a false violation — the opposite failure, and just as wrong.
func TestAWriteInsideAFunctionLiteralCounts(t *testing.T) {
	rep := analyzeSource(t, preamble+`
type src struct {
	List  func(s *store.Store) int
	Write func(s *store.Store) error
}

var sources = map[string]src{
	"real": {
		List:  func(s *store.Store) int { r, _ := s.List(store.Kind("real")); return len(r) },
		Write: func(s *store.Store) error { return s.PutJSON(store.Kind("real"), "id", nil) },
	},
}
`)
	if len(rep.Violations) != 0 {
		t.Errorf("a kind read and written inside function literals was reported: %v", rep.Violations)
	}
	if !hasKind(rep.Reads, "real") || !hasKind(rep.Writes, "real") {
		t.Errorf("the analysis did not see both halves inside literals: reads=%v writes=%v", rep.Reads, rep.Writes)
	}
}

// NO STORE-SHAPED CALL IS EVER SILENTLY DROPPED, whatever syntax it is written in.
//
// The two tests above name shapes somebody thought of. This one asserts the property they are
// instances of: every call this package recognises as a store read is accounted for, as a resolved
// read or as an unresolved one. A shape nobody anticipated shows up here as unresolved — visible,
// which is the outcome the design requires — rather than as nothing at all.
func TestEveryReadShapedCallIsAccountedFor(t *testing.T) {
	rep := analyzeSource(t, preamble+`
type holder struct{ fn func() }

var atPackageLevel = func(s *store.Store) int { r, _ := s.List(store.Kind("one")); return len(r) }

func Named(s *store.Store) int {
	inner := func() int { r, _ := s.List(store.Kind("two")); return len(r) }
	go func() { s.List(store.Kind("three")) }()
	defer func() { s.Get(store.Kind("four"), "id") }()
	h := holder{fn: func() { s.List(store.Kind("five")) }}
	_ = h
	return inner()
}
`)
	for _, want := range []string{"one", "two", "three", "four", "five"} {
		if !hasKind(rep.Reads, want) {
			t.Errorf("the read of kind %q was not accounted for at all; reads=%v unresolved=%v",
				want, rep.Reads, rep.Unresolved)
		}
	}
}

// A KIND BUILT AT RUNTIME IS UNRESOLVED, AND SAID TO BE. Not silently treated as fine.
func TestARuntimeKindIsReportedAsUnresolvedAndNotAsClean(t *testing.T) {
	rep := analyzeSource(t, preamble+`
func Activity(s *store.Store, subject string) int {
	recs, _ := s.List(store.Kind("activity." + subject))
	return len(recs)
}
`)
	if len(rep.Unresolved) != 1 {
		t.Fatalf("a kind computed at runtime produced %d unresolved reads, want 1: %+v", len(rep.Unresolved), rep)
	}
	if len(rep.Violations) != 0 {
		t.Errorf("an unresolved read was also reported as a violation, which would be a guess: %v", rep.Violations)
	}
}

// AN EMPTY TREE IS AN ERROR, NOT A PASS.
func TestAnalyzingNothingIsAnError(t *testing.T) {
	if _, err := Analyze(fstest.MapFS{}); err == nil {
		t.Error("analysing a tree with no Go files in it reported success")
	}
}
