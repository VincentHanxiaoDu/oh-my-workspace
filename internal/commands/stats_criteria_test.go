package commands

// Issue #72's statistics criteria, carried forward from #13 at closure.
//
// # What this file is for
//
// #13 closed with two regions recorded as REAL VALUES rather than rounded to a pass: criterion 9
// (`unobservable` — there is no second surface to compare against) and the second half of criterion
// 12 (`unreachable` — no client→hub transport exists, so a DETERMINED empty answer from a REACHABLE
// hub cannot be produced). #72 restates them as criteria of its own. None of them can be driven in
// this build.
//
// THE FAILURE MODE THIS FILE EXISTS TO PREVENT IS A QUIET ONE. A criterion that cannot be driven
// gets argued from shared code instead — "the CLI and the agent API render the same [hub.Report], so
// they agree by construction" — and construction is not observation. The Issue says so in as many
// words. An argument is not a driven criterion, and a criterion nobody drives is one nobody notices
// has stopped being true.
//
// So each criterion below is written as the test that WILL drive it, and each one PROBES for the
// capability it needs rather than naming a build or a machine. Absent the capability it SKIPS WITH A
// REASON that says outright it has not passed and has determined nothing — `could not determine` and
// `determined to be nothing` are different values here (PRD §4.3), and a skip that read like a pass
// would be the second wearing the first's clothes.
//
// AND THE SKIPS CANNOT ROT INTO PERMANENT SILENCE. Each probe is of the product's own seam, so the
// day the seam gains the capability the test stops skipping and starts asserting. The criterion-9
// test goes further and fails outright the moment a second surface appears while the comparison is
// still unwritten — a skip nobody revisits is exactly how #13's `unobservable` would become a
// permanent one.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// --- the probes ---------------------------------------------------------------------------------

// defaultStatsSource is the product's own way of reaching a hub, captured before any test swaps it.
//
// Package-level initialisation is ordered by dependency, so this is the value stats_cmd.go assigns
// and never a harness's replacement — which matters, because every probe below would otherwise
// answer "yes, there is a transport" whenever a test in this package happened to have injected one.
var defaultStatsSource = statsSource

// probeHubTransport asks the product whether it can reach a hub, BY ASKING IT TO.
//
// It is a probe and not a name: it neither tests for a build tag nor checks a version, it calls the
// one seam `omw stats` calls and reports what came back. On the day #10 lands a client→hub
// transport, this starts returning a store and every test gated on it starts asserting, with no edit
// here. Today it returns the reason code the seam gave, which is what the skip messages carry.
//
// A NOTE FOR WHOEVER LANDS #10. This calls the real seam, so once that seam dials, this probe dials.
// The hub it is pointed at must therefore be one this suite stood up itself — `internal/commands`
// tests may not reach another machine, and TestEveryListenAndDialIsAUnixSocket is what says so.
func probeHubTransport(t *testing.T) (*hub.Store, string) {
	t.Helper()
	env := cli.Env{
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		Getenv: func(k string) string {
			if k == statsEnvHub {
				return "probe.invalid"
			}
			return ""
		},
	}
	s, err := defaultStatsSource(env)
	switch {
	case err != nil:
		return nil, "the one seam that reaches a hub answered " + hub.Code(err)
	case s == nil:
		return nil, "the one seam that reaches a hub returned no hub and no reason"
	}
	return s, ""
}

// probeStatisticsSurfaces counts the surfaces of this build that serve the statistics capability.
//
// CRITERION 9 IS ABOUT THERE BEING TWO OF THEM, so the thing to establish is how many there are. A
// surface is a non-test file outside `internal/hub` that reaches for this capability's schema —
// `internal/hub` declares the capability and does not serve it. Today that is exactly one file, the
// CLI, which is #13's `unobservable` stated as a number.
//
// It reads the product's own tree rather than a list kept here, because a list kept here is a list
// that goes stale the moment somebody adds the surface this is waiting for.
//
// IT PARSES RATHER THAN GREPS, and that is not fastidiousness. A text match counts a mention in a
// comment — this file's own prose names the function repeatedly — and the criterion-9 test FAILS
// when the count rises. A grep would therefore turn writing a sentence about the capability into a
// red build about the agent API: a false red, on a gate, of exactly the kind that teaches a reader
// to wave the next one through.
func probeStatisticsSurfaces(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs("..") // internal/
	if err != nil {
		t.Fatalf("resolving the source tree: %v", err)
	}
	var found []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		if filepath.Base(filepath.Dir(p)) == "hub" {
			return nil // declares the capability; does not serve it
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", p, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "StatsAPISchema" {
				return true
			}
			rel, _ := filepath.Rel(root, p)
			found = append(found, rel)
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source tree: %v", err)
	}
	if len(found) == 0 {
		t.Fatalf("no surface in this build serves the statistics capability at all; " +
			"`omw stats schema` was one, so something that existed has been removed")
	}
	return found
}

// --- criterion 1: the CLI and the agent API return identical statistics --------------------------

// TestCriterion1CLIAndTheAgentAPIAreTwoSurfaces is #72's criterion 1, and today it is the record
// that the criterion is NOT met.
//
// WHAT IS ALREADY DRIVEN, AND WHY IT IS NOT THIS. TestStatsCLIAndAgentAPIAgree compares `omw stats`
// against `omw stats --json` across every undetermined reason, and it is a good test — but both
// renderings come from one in-process [hub.Report], so it establishes that nobody has added a SECOND
// COMPUTATION behind the same value. Criterion 1 asks for something else: that two SURFACES agree.
// With one surface that is true by construction, and #13 recorded it as `unobservable` rather than
// claiming it.
//
// THIS TEST FAILS THE DAY IT BECOMES DRIVABLE, and that is deliberate. A skip that says "come back
// when #16 lands" is a note nobody reads; a red build the day #16 lands is not.
func TestCriterion1CLIAndTheAgentAPIAreTwoSurfaces(t *testing.T) {
	surfaces := probeStatisticsSurfaces(t)
	if len(surfaces) > 1 {
		t.Fatalf("this build now serves the statistics capability from %d surfaces (%s), so #72's "+
			"criterion 1 is DRIVABLE and is not yet driven. Write it here: issue the same request "+
			"for the same scope through both surfaces and require the payloads to match, including "+
			"every undetermined marker and reason code. Do not argue it from the shared [hub.Report] "+
			"— identity by construction is what #13 recorded as unobservable.",
			len(surfaces), strings.Join(surfaces, ", "))
	}
	t.Skipf("criterion 1 (the CLI and the agent API return identical statistics) COULD NOT BE "+
		"DETERMINED and THIS TEST HAS NOT PASSED. This build serves the statistics capability from "+
		"one surface only (%s); `--json` is the same in-process report, so there is no second "+
		"surface to compare against and identity holds by construction rather than by observation. "+
		"Drivable when #16's agent API ships. Nothing here establishes that the two surfaces agree.",
		surfaces[0])
}

// --- criterion 2: a reachable hub with nothing readable renders a DETERMINED zero ----------------

// TestCriterion2AReachableHubWithNothingReadableIsNotAnUnreachableOne is #72's criterion 2.
//
// WHAT IS ALREADY DRIVEN, AND WHY IT IS NOT THIS. TestStatsHubUnreachableIsNotTheHubReportingNothing
// drives exactly this distinction — with a [hub.Store] INJECTED into `statsSource` by the harness.
// That establishes the RENDERING is distinguishable, which is worth having, and #13 recorded it
// honestly as "only unit tests cover it". The criterion says "drive both against a real hub", and a
// store handed to the command in-process is the library seam wearing a command's clothes: no
// transport was exercised, so nothing was established about what this command prints when a hub
// genuinely answers.
func TestCriterion2AReachableHubWithNothingReadableIsNotAnUnreachableOne(t *testing.T) {
	reachable, why := probeHubTransport(t)
	if reachable == nil {
		t.Skipf("criterion 2 (a reachable hub with nothing readable in scope renders a DETERMINED "+
			"zero, distinguishable from hub-unreachable) COULD NOT BE DETERMINED and THIS TEST HAS "+
			"NOT PASSED: %s, so this build has no client→hub transport and a hub that ANSWERS cannot "+
			"be produced here at all. Drivable when #10 lands. Note what is NOT being claimed: the "+
			"rendering distinction is covered by unit tests against an injected store, and that is "+
			"not this criterion.", why)
	}

	// A transport exists. Drive both halves of the distinction through the real command, and require
	// that they do not print the same thing.
	unreachable := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	unreachable.run(t, nil)

	answered := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
	answered.run(t, reachable)

	for _, name := range []string{"notes", "subjects", "recency"} {
		a := statLine(t, unreachable.out(), "hub", name)
		b := statLine(t, answered.out(), "hub", name)
		if a == b {
			t.Fatalf("%s: a hub that could not be reached and a hub that ANSWERED with nothing "+
				"readable both rendered as %q", name, a)
		}
	}
	if got := statLine(t, answered.out(), "hub", "notes"); strings.Contains(got, hub.UndeterminedToken) {
		t.Fatalf("a hub that answered rendered notes as %q; the criterion wants a DETERMINED zero", got)
	}
	if got := statLine(t, answered.out(), "hub", "recency"); strings.Contains(got, hub.UndeterminedToken) {
		t.Fatalf("a hub that answered rendered recency as %q; criterion 13 wants a determined none", got)
	}
	if !strings.Contains(statLine(t, unreachable.out(), "hub", "notes"), hub.ErrHubUnreachable.Code) {
		t.Fatalf("the unreachable hub did not name its reason:\n%s", unreachable.out())
	}
}

// --- criterion 3: publishing what the reader cannot see moves nothing ----------------------------

// TestCriterion3PublishingAnInvisibleNoteLeavesTheReadersStatisticsByteIdentical is #72's criterion
// 3, driven end to end through `omw stats` rather than at the library seam.
//
// THE CONTROL IS THE TEST. "Publishing a note they cannot see changed nothing" is satisfied by a
// build that publishes nothing at all, by one whose statistics never move, and by one whose hub half
// is undetermined throughout — so on its own it proves nothing. The control is the second half: a
// note the reader CAN see must move the count AND the recency through the same path. Only the pair
// says anything.
func TestCriterion3PublishingAnInvisibleNoteLeavesTheReadersStatisticsByteIdentical(t *testing.T) {
	h, why := probeHubTransport(t)
	if h == nil {
		t.Skipf("criterion 3 (publishing a note the reader cannot see leaves their statistics "+
			"byte-identical, re-driven end to end) COULD NOT BE DETERMINED and THIS TEST HAS NOT "+
			"PASSED: %s, so nothing can be published to a hub this build can then read back. "+
			"Drivable when #10 lands. The library-seam version is TestCountIsTheReadableSubset in "+
			"internal/hub, and the whole point of this criterion is that it is NOT that test.", why)
	}

	read := func() string {
		w := newStatsWorld(t).withHub().withDaemon(t).as("searcher").scopes("read")
		w.run(t, h)
		if w.code != cli.Success && w.code != cli.ExitUndetermined {
			t.Fatalf("`omw stats` exited %d:\n%s", w.code, w.all())
		}
		return w.out()
	}

	before := read()

	// THE PROBE: something the reader may not read.
	if _, err := h.Publish(hub.Publication{Author: "ada", Title: "hidden", Body: "b", Visibility: hub.SelfOnly()}); err != nil {
		t.Fatalf("publishing an invisible note: %v", err)
	}
	if after := read(); after != before {
		t.Fatalf("publishing a note the reader cannot see moved their statistics.\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// THE CONTROL: something they may. Without this the assertion above is satisfied by a build in
	// which nothing ever moves.
	if _, err := h.Publish(hub.Publication{Author: "ada", Title: "visible", Body: "b"}); err != nil {
		t.Fatalf("publishing a visible note: %v", err)
	}
	after := read()
	if after == before {
		t.Fatalf("the CONTROL did not move: a note the reader CAN see left their statistics "+
			"byte-identical, so the probe above establishes nothing.\n%s", after)
	}
	if statLine(t, after, "hub", "notes") == statLine(t, before, "hub", "notes") {
		t.Fatalf("the control did not move the COUNT:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if statLine(t, after, "hub", "recency") == statLine(t, before, "hub", "recency") {
		t.Fatalf("the control did not move the RECENCY:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// --- the watch item #72 records, which is not a defect today -------------------------------------

// TestTheLocalHalfIsAboutTheLocalOutboxAndSaysSo re-drives #72's third section: `omw stats --scope
// person:bob` renders the local half as a confident `notes: 0` about another person's corpus.
//
// THAT IS CORRECT AND IT STAYS CORRECT. Criterion 5 positively REQUIRES a scope the reader cannot
// see into to be indistinguishable from a genuinely empty one — otherwise the count leaks the
// existence of unreadable material — and alice's local outbox does contain zero notes authored by
// bob. This test pins the honesty of the WHOLE report, which is the property #72 says is worth
// re-driving: the hub half must not be quietly determined alongside it, and the exit code must say
// the answer is incomplete. A future build in which the local zero stands next to a determined hub
// half is the one a consumer would misread, and it goes red here.
func TestTheLocalHalfIsAboutTheLocalOutboxAndSaysSo(t *testing.T) {
	w := newStatsWorld(t).withHub().withDaemon(t).as("alice").scopes("read").withOutbox(t, "one", "two")
	w.run(t, nil, "--scope", "person:bob")

	if got := statLine(t, w.out(), "local", "notes"); got != "0" {
		t.Fatalf("local notes for a scope the outbox has nothing in = %q, want a determined 0: "+
			"criterion 5 requires this to be indistinguishable from a genuinely empty scope", got)
	}
	if got := statLine(t, w.out(), "hub", "notes"); !strings.Contains(got, hub.UndeterminedToken) {
		t.Fatalf("hub notes = %q — the hub half was determined next to a local zero about somebody "+
			"else's corpus, and a consumer reading the report would take the whole thing as settled", got)
	}
	if w.code != cli.ExitUndetermined {
		t.Fatalf("exit %d, want %d: the report is incomplete and the exit code is what a script reads",
			w.code, cli.ExitUndetermined)
	}
	if !strings.Contains(w.out(), "could not be determined") {
		t.Fatalf("the report does not say any statistic was undetermined:\n%s", w.out())
	}
	// The local half is a determined answer, so it says so rather than hedging.
	if got := statLine(t, w.out(), "local", "coverage"); got != tri.Yes.Render("complete", "incomplete") {
		t.Fatalf("local coverage = %q, want the determined rendering", got)
	}
}
