// Issue #101, acceptance criterion 6: the structural guard.
//
// "No surface renders a count derived from a read that failed." This is the criterion the Issue
// says matters, because it is now the THIRD independent place (after #67's three and #69) where an
// unreadable or absent thing became a determined number, and point fixes have not stopped it.
//
// A point fix is a branch; a structural guard is a property of the type. Every count this surface
// serves is either a pointer — absent when nothing established it, which is [Response.UndeterminedNotes]'
// own reasoning — or it does not exist. A plain int count added to this surface is zero on every
// failure path, and `"revisions":0` reads as "I counted them and there were none": a determined
// claim about work nobody did. This test fails on the FIELD NAME, so whoever adds one is told which.
package agentapi

import (
	"reflect"
	"strings"
	"testing"
)

// countShapedFieldNames are the field names that carry "how many of something there are". They are
// matched by shape rather than listed exhaustively, so a count added under a new name is covered
// the moment it is added rather than the moment somebody remembers this file.
func looksLikeACount(name string, f reflect.StructField) bool {
	if f.Type.Kind() != reflect.Int && f.Type.Kind() != reflect.Ptr {
		return false
	}
	n := strings.ToLower(name)
	for _, w := range []string{"count", "revisions", "total", "undetermined"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// TestNoCountOnTheAgentSurfaceCanBeAZeroNobodyEstablished walks every type this package serves.
func TestNoCountOnTheAgentSurfaceCanBeAZeroNobodyEstablished(t *testing.T) {
	served := []reflect.Type{
		reflect.TypeOf(Response{}),
		reflect.TypeOf(DraftView{}),
		reflect.TypeOf(TicketView{}),
		reflect.TypeOf(NoteView{}),
		reflect.TypeOf(GrantView{}),
	}
	checked := 0
	for _, ty := range served {
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			if !looksLikeACount(f.Name, f) {
				continue
			}
			checked++
			if f.Type.Kind() != reflect.Ptr {
				t.Errorf("criterion 6: %s.%s is a %s, so it is 0 on every path that could not read anything.\n"+
					"  A count served as 0 by a failed read is a determined answer about work nobody did — #67, #69 and #101.\n"+
					"  Make it a pointer: absent means nothing was established, 0 means it was established and came to none.",
					ty.Name(), f.Name, f.Type)
			}
		}
	}
	if checked == 0 {
		t.Fatal("this guard matched no field at all, so it would pass against any type — the matcher, not the types, is what is broken")
	}
}

// TestARevisionCountNobodyEstablishedNeverRendersAsANumber is criterion 4 at the rendering, over
// the one function every surface goes through.
func TestARevisionCountNobodyEstablishedNeverRendersAsANumber(t *testing.T) {
	undetermined := DraftView{ID: "d2", State: UndeterminedState}
	got := undetermined.RenderRevisions()
	if strings.ContainsAny(got, "0123456789") {
		t.Errorf("criterion 4: a draft whose revisions could not be counted renders as %q, which contains a number", got)
	}
	if undetermined.Determined() {
		t.Error("criterion 4: a draft with no established state and no established count reports itself as determined")
	}

	// AND THE DETERMINED ZERO IS STILL REACHABLE. A fix that made every count undetermined would
	// pass the assertion above and destroy the surface; an outbox directory with no revisions in it
	// is a real, established zero and must still say so.
	zero := 0
	established := DraftView{ID: "d3", State: DraftedState, Revisions: &zero}
	if established.RenderRevisions() != "0 revision(s)" {
		t.Errorf("an established zero must still render as one; got %q", established.RenderRevisions())
	}
	if !established.Determined() {
		t.Error("a draft whose state and count were both established reports itself as undetermined")
	}
}
