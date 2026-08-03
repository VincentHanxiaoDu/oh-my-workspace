package inbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Issue #7 at the package level. The CLI-level drives are in internal/commands/ticket_test.go and
// the interrupted-merge drive is in mergecrash_test.go; what is here is everything that is a fact
// about the merge itself rather than about how it is printed.

func mergeStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(t.TempDir() + "/store")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s
}

// theBrokenLogin is the Issue's own scenario: five emails, a chat thread and a follow-up ping about
// one broken login, arriving as separate tickets across two channels.
func theBrokenLogin(t *testing.T, s *store.Store) []string {
	t.Helper()
	tickets := []Ticket{
		{ID: "e1", Title: Text("SSO login fails for Ana"), Summary: Text("Ana cannot get in since the cutover."),
			Channel: Text("email"), Arrived: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)},
		{ID: "e2", Title: Text("Re: SSO login fails for Ana"), Summary: Text("Still failing this morning."),
			Channel: Text("email"), Arrived: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)},
		{ID: "c1", Title: Text("login thread"), Summary: Text("Three people comparing notes in a chat."),
			Channel: Text("teams"), Arrived: time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC)},
		{ID: "c2", Title: Text("is the login fixed yet"), Summary: Absent(),
			Channel: Undetermined("the source channel could not be read"),
			Arrived: time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC)},
	}
	ids := make([]string, 0, len(tickets))
	for _, tk := range tickets {
		if err := Put(s, tk); err != nil {
			t.Fatalf("seeding %q: %v", tk.ID, err)
		}
		ids = append(ids, tk.ID)
	}
	return ids
}

func specFor(id string, ids ...string) MergeSpec {
	spec := MergeSpec{
		ID:      id,
		Title:   Text("Ana cannot log in since the SSO cutover"),
		Summary: Text("One broken login, reported by five people across email and chat. Nobody can " +
			"authenticate against the new identity provider with a legacy account."),
		Channel: Undetermined("this ticket was merged from more than one channel"),
		When:    time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
	}
	for _, t := range ids {
		spec.Inputs = append(spec.Inputs, InputSpec{
			TicketID: t,
			Why:      Text("the same broken login"),
			Source:   Text("message-" + t + "@example.invalid"),
		})
	}
	return spec
}

func listIDs(t *testing.T, s *store.Store) []string {
	t.Helper()
	tickets, err := List(s)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	out := make([]string, 0, len(tickets))
	for _, tk := range tickets {
		out = append(out, tk.ID)
	}
	return out
}

// snapshotStore reads every ticket's stored payload. This is the "snapshot taken before the merge"
// criterion 5 names, and it is bytes rather than a struct on purpose: comparing decoded structs
// would pass for a restoration that silently dropped a field this build does not know about.
func snapshotStore(t *testing.T, s *store.Store) map[string][]byte {
	t.Helper()
	recs, err := s.List(Kind)
	if err != nil {
		t.Fatalf("listing records: %v", err)
	}
	out := map[string][]byte{}
	for _, r := range recs {
		out[r.ID] = append([]byte(nil), r.Data...)
	}
	return out
}

// ---------------------------------------------------------------------------
// CRITERION 1 — two or more tickets become one, and the sources stop being listed.
// ---------------------------------------------------------------------------

func TestMergingReplacesTheSourcesWithOneTicket(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	if got := listIDs(t, s); len(got) != 4 {
		t.Fatalf("the inbox starts with %v", got)
	}
	merged, err := Merge(s, specFor("login", ids...))
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	got := listIDs(t, s)
	if !reflect.DeepEqual(got, []string{"login"}) {
		t.Fatalf("after the merge the inbox lists %v; want exactly the merged ticket — the merged-away "+
			"tickets must not also be listed as separate open items", got)
	}
	if v, ok := merged.Title.Value(); !ok || v == "" {
		t.Errorf("the merged ticket has no written title: %s", merged.Title.Render())
	}
	// The merged ticket is owed since the FIRST piece landed, not since the merge.
	want := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	if !merged.Arrived.Equal(want) {
		t.Errorf("the merged ticket arrived %s; want the earliest of its inputs, %s", merged.ArrivedRender(), want)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 2 — crossing channels is not a lesser merge.
// ---------------------------------------------------------------------------

func TestACrossChannelMergeSucceedsOnTheSameTermsAsASameChannelOne(t *testing.T) {
	same := mergeStore(t)
	theBrokenLogin(t, same)
	sameMerged, sameErr := Merge(same, specFor("m", "e1", "e2")) // email + email

	cross := mergeStore(t)
	theBrokenLogin(t, cross)
	crossMerged, crossErr := Merge(cross, specFor("m", "e1", "c1")) // email + teams

	if sameErr != nil || crossErr != nil {
		t.Fatalf("same-channel = %v, cross-channel = %v; §3.2 merges across channels by design", sameErr, crossErr)
	}
	// The two results differ only in what came from the inputs. Nothing about the merged ticket
	// itself is degraded because the sources were of different kinds.
	if sameMerged.Title.Render() != crossMerged.Title.Render() ||
		sameMerged.Summary.Render() != crossMerged.Summary.Render() ||
		sameMerged.Channel.Render() != crossMerged.Channel.Render() {
		t.Errorf("a cross-channel merge produced a different ticket from a same-channel one:\n same:  %+v\n cross: %+v",
			sameMerged, crossMerged)
	}
	sameRec, _ := LoadMerge(same, "m")
	crossRec, _ := LoadMerge(cross, "m")
	if len(sameRec.Inputs) != len(crossRec.Inputs) {
		t.Errorf("the cross-channel merge recorded %d inputs and the same-channel one %d",
			len(crossRec.Inputs), len(sameRec.Inputs))
	}
	// The two source channels are both carried, distinctly. A merge that "normalised" them would
	// have lost the provenance criterion 7 requires.
	channels := []string{crossRec.Inputs[0].Channel.Render(), crossRec.Inputs[1].Channel.Render()}
	if channels[0] == channels[1] {
		t.Errorf("both inputs of a cross-channel merge report the channel %q", channels[0])
	}
}

// ---------------------------------------------------------------------------
// CRITERION 3 — a written title and a written summary, not a concatenation.
// ---------------------------------------------------------------------------

func TestAMergedTicketNeedsAWrittenTitleAndSummary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		title   Field
		summary Field
	}{
		{"no title at all", Absent(), Text("a real summary of the one problem")},
		{"an undetermined title", Undetermined("nobody wrote one"), Text("a real summary")},
		{"a written empty title", Text(""), Text("a real summary")},
		{"a whitespace title", Text("   "), Text("a real summary")},
		{"no summary", Text("a real title"), Absent()},
		{"a written empty summary", Text("a real title"), Text("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mergeStore(t)
			ids := theBrokenLogin(t, s)
			spec := specFor("login", ids...)
			spec.Title, spec.Summary = tc.title, tc.summary
			if _, err := Merge(s, spec); !errors.Is(err, ErrNotWritten) {
				t.Fatalf("Merge = %v; want ErrNotWritten", err)
			}
			if got := listIDs(t, s); len(got) != len(ids) {
				t.Errorf("a refused merge changed the inbox: %v", got)
			}
		})
	}
}

func TestASummaryThatIsTheSourceTitlesRunTogetherIsNotASummary(t *testing.T) {
	// A FRESH STORE PER CASE. The first version reused one, so the first merge succeeded and every
	// case after it failed for the unrelated reason that the identifier was taken — an assertion
	// passing on the wrong error, which is a green that says nothing.
	printers := func(t *testing.T) *store.Store {
		t.Helper()
		s := mergeStore(t)
		for _, tk := range []Ticket{
			{ID: "a", Title: Text("printer jam"), Summary: Text("x")},
			{ID: "b", Title: Text("second floor printer"), Summary: Text("y")},
		} {
			if err := Put(s, tk); err != nil {
				t.Fatal(err)
			}
		}
		return s
	}
	for _, summary := range []string{
		"printer jam second floor printer",
		"second floor printer, printer jam", // the same fragments, the other way round
		"Printer Jam; Second Floor Printer!",
	} {
		spec := specFor("p", "a", "b")
		spec.Summary = Text(summary)
		if _, err := Merge(printers(t), spec); !errors.Is(err, ErrNotWritten) {
			t.Errorf("a summary of %q was accepted (%v); §3.2 asks for a written summary, and the "+
				"source titles run together is the list of fragments the merge replaces", summary, err)
		}
	}
	// A summary that says something is accepted, even though it contains the source words.
	spec := specFor("p", "a", "b")
	spec.Summary = Text("The second floor printer has jammed twice this week and nobody has cleared it.")
	if _, err := Merge(printers(t), spec); err != nil {
		t.Errorf("a real summary was refused: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 4 and 7 — the merge shows its working, for every input.
// ---------------------------------------------------------------------------

func TestTheMergeRecordsWhatEachInputWasWhereItCameFromAndWhy(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	if _, err := Merge(s, specFor("login", ids...)); err != nil {
		t.Fatal(err)
	}
	record, err := LoadMerge(s, "login")
	if err != nil {
		t.Fatalf("LoadMerge: %v", err)
	}
	if len(record.Inputs) != len(ids) {
		t.Fatalf("the merge records %d inputs; %d were merged", len(record.Inputs), len(ids))
	}
	for _, in := range record.Inputs {
		for what, f := range map[string]Field{
			"what it was": in.What, "which channel": in.Channel,
			"which source": in.Source, "why": in.Why,
		} {
			if f.Render() == "" {
				t.Errorf("input %q does not say %s", in.TicketID, what)
			}
		}
		// CRITERION 7: the origin is readable from the merged ticket alone. Nothing above consulted
		// a channel, and the identifiers are on the record.
		if v, ok := in.Source.Value(); !ok || !strings.Contains(v, in.TicketID) {
			t.Errorf("input %q does not carry a source identifier: %s", in.TicketID, in.Source.Render())
		}
	}
}

// CRITERION 4's teeth. A record whose input does not carry one of the three is a FAILURE, and is
// never read back as a merge with a blank in it. Staged on disk, because that is the only place a
// field can be missing — in memory the type does not permit it, and the check therefore lives in
// the decoder.
func TestAMergeRecordMissingAnInputsWorkingIsAFailureAndNotABlank(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	if _, err := Merge(s, specFor("login", ids...)); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Get(MergeKind, "login")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"why", "channel", "source", "what"} {
		t.Run("without "+key, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(rec.Data, &raw); err != nil {
				t.Fatal(err)
			}
			inputs := raw["inputs"].([]any)
			delete(inputs[1].(map[string]any), key)
			body, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Put(store.Record{Kind: MergeKind, ID: "login", Data: body}); err != nil {
				t.Fatal(err)
			}
			_, lerr := LoadMerge(s, "login")
			if !errors.Is(lerr, ErrIncompleteWorking) {
				t.Fatalf("a merge record with no %q for an input read back as %v; criterion 4 calls "+
					"that a failure, not a field to print blank", key, lerr)
			}
			if !strings.Contains(lerr.Error(), key) && !strings.Contains(lerr.Error(), keyPhrase(key)) {
				t.Errorf("the refusal does not name what is missing: %v", lerr)
			}
		})
	}
}

func keyPhrase(key string) string {
	switch key {
	case "why":
		return "why it was merged"
	case "channel":
		return "which channel it came from"
	case "source":
		return "which source it came from"
	default:
		return "what it was"
	}
}

// ---------------------------------------------------------------------------
// CRITERION 5 — every merge is reversible, EXACTLY.
// ---------------------------------------------------------------------------

func TestUnmergingRestoresEverySourceByteForByte(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	before := snapshotStore(t, s)

	if _, err := Merge(s, specFor("login", ids...)); err != nil {
		t.Fatal(err)
	}
	restored, err := Unmerge(s, "login", time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Unmerge: %v", err)
	}
	if len(restored) != len(ids) {
		t.Fatalf("Unmerge returned %d tickets; %d were merged", len(restored), len(ids))
	}

	after := snapshotStore(t, s)
	if len(after) != len(before) {
		t.Fatalf("after the unmerge the inbox holds %d tickets; it held %d before the merge", len(after), len(before))
	}
	for id, want := range before {
		got, ok := after[id]
		if !ok {
			t.Errorf("ticket %q did not come back", id)
			continue
		}
		// CONTENT EQUALITY WITH THE SNAPSHOT, on the bytes. An approximate restoration — the same
		// title, a summary rebuilt from parts, a lost undetermined-ness — fails here.
		if !bytes.Equal(got, want) {
			t.Errorf("ticket %q came back different:\n before: %s\n after:  %s", id, want, got)
		}
	}
	if _, err := Get(s, "login"); !errors.Is(err, ErrNoSuchTicket) {
		t.Errorf("the merged ticket is still in the inbox after the unmerge: %v", err)
	}
	if _, err := LoadMerge(s, "login"); !errors.Is(err, ErrNotMerged) {
		t.Errorf("the merge record survived the unmerge: %v", err)
	}
}

// The undetermined-ness of a source field is part of its content, and is the part a reconstruction
// loses first. c2's channel is undetermined and its summary is absent; both must come back as they
// were and not as each other.
func TestAnUnmergeRestoresTheFourFieldStatesAndNotJustTheText(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	if _, err := Merge(s, specFor("login", ids...)); err != nil {
		t.Fatal(err)
	}
	if _, err := Unmerge(s, "login", time.Now()); err != nil {
		t.Fatal(err)
	}
	c2, err := Get(s, "c2")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Channel.State() != tri.Undetermined {
		t.Errorf("c2's channel came back as %s (%s); it was undetermined before the merge",
			c2.Channel.State(), c2.Channel.Render())
	}
	if c2.Summary.State() != tri.No {
		t.Errorf("c2's summary came back as %s (%s); it was a recorded absence before the merge",
			c2.Summary.State(), c2.Summary.Render())
	}
	if c2.Channel.Render() == c2.Summary.Render() {
		t.Errorf("an undetermined field and an absent one came back rendering identically: %q", c2.Channel.Render())
	}
}

// ---------------------------------------------------------------------------
// CRITERION 6 — merged-then-unmerged is not the same as never merged.
// ---------------------------------------------------------------------------

func TestATicketThatWasMergedAndUnmergedIsDistinguishableFromOneThatWasNot(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	if err := Put(s, Ticket{ID: "untouched", Title: Text("a different problem entirely"), Summary: Text("...")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(s, specFor("login", ids...)); err != nil {
		t.Fatal(err)
	}
	if _, err := Unmerge(s, "login", time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	was, wasPresent, err := LoadUnmerged(s, "e1")
	if err != nil {
		t.Fatal(err)
	}
	never, neverPresent, err := LoadUnmerged(s, "untouched")
	if err != nil {
		t.Fatal(err)
	}
	if wasPresent != tri.Yes {
		t.Fatalf("a restored ticket reports %s for 'was this ever merged'; the two situations must "+
			"never be indistinguishable", wasPresent)
	}
	if neverPresent != tri.No {
		t.Fatalf("a ticket that was never merged reports %s", neverPresent)
	}
	// COMPARED PAIRWISE. Asserting each against its expected wording passes just as happily after
	// both have been edited to say the same thing.
	if wasPresent.Render("merged and unmerged", "never merged") ==
		neverPresent.Render("merged and unmerged", "never merged") {
		t.Errorf("the two answers render identically")
	}
	if was.MergedInto != "login" {
		t.Errorf("the restored ticket does not say what it was merged into: %+v", was)
	}
	if was.Alongside != len(ids)-1 {
		t.Errorf("the restored ticket says it was alongside %d others; there were %d", was.Alongside, len(ids)-1)
	}
	if _, ok := never.Merged, neverPresent == tri.Yes; ok {
		t.Errorf("a ticket that was never merged carries a merge history")
	}
}

// ---------------------------------------------------------------------------
// CRITERION 8 and 9 — refusals, by value, with the inbox unchanged.
// ---------------------------------------------------------------------------

func TestARefusedMergeLeavesTheInboxExactlyAsItWas(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(ids []string) MergeSpec
		want  error
	}{
		{"only one ticket", func(ids []string) MergeSpec { return specFor("m", ids[0]) }, ErrTooFewInputs},
		{"no tickets", func([]string) MergeSpec { return specFor("m") }, ErrTooFewInputs},
		{"a ticket that does not exist", func(ids []string) MergeSpec { return specFor("m", ids[0], "never-existed") }, ErrNoSuchTicket},
		{"the same ticket twice", func(ids []string) MergeSpec { return specFor("m", ids[0], ids[0]) }, ErrRepeatedInput},
		{"an identifier a bystander holds", func(ids []string) MergeSpec { return specFor(ids[2], ids[0], ids[1]) }, ErrTicketIDTaken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mergeStore(t)
			ids := theBrokenLogin(t, s)
			before := snapshotStore(t, s)
			if _, err := Merge(s, tc.build(ids)); !errors.Is(err, tc.want) {
				t.Fatalf("Merge = %v; want %v", err, tc.want)
			}
			after := snapshotStore(t, s)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("a refused merge changed the inbox:\n before %v\n after  %v", keys(before), keys(after))
			}
			if merges, err := ListMerges(s); err != nil || len(merges) != 0 {
				t.Errorf("a refused merge left a merge record behind: %v / %v", merges, err)
			}
		})
	}
}

func TestUnmergingSomethingThatWasNeverMergedRefusesAndChangesNothing(t *testing.T) {
	s := mergeStore(t)
	theBrokenLogin(t, s)
	before := snapshotStore(t, s)
	for _, id := range []string{"e1", "never-existed"} {
		if _, err := Unmerge(s, id, time.Now()); !errors.Is(err, ErrNotMerged) {
			t.Errorf("Unmerge(%q) = %v; want ErrNotMerged", id, err)
		}
	}
	if !reflect.DeepEqual(before, snapshotStore(t, s)) {
		t.Errorf("a refused unmerge changed the inbox")
	}
	// And no trace was written: a ticket that was never merged must not now report that it was.
	if _, present, _ := LoadUnmerged(s, "e1"); present != tri.No {
		t.Errorf("a refused unmerge left a merge history on e1")
	}
}

// ---------------------------------------------------------------------------
// CRITERION 11 — nothing here resurrects what was never a ticket.
// ---------------------------------------------------------------------------

func TestAMergeCannotMintAnAcknowledgementAsATicket(t *testing.T) {
	s := mergeStore(t)
	theBrokenLogin(t, s)
	spec := specFor("login", "e1", "e2")
	spec.Title = Text("ok")
	if _, err := Merge(s, spec); !errors.Is(err, ErrNotAnObligation) {
		t.Fatalf("merging into a ticket titled \"ok\" = %v; want ErrNotAnObligation. §3.2: an "+
			"acknowledgement is not a low-priority ticket, it is not a ticket", err)
	}
	if got := listIDs(t, s); len(got) != 4 {
		t.Errorf("the refused merge changed the inbox: %v", got)
	}
}

// A merge input is an identifier of a STORED TICKET, resolved through the store. There is no
// parameter through which a piece of traffic that was never turned into a ticket could enter, so
// there is no path by which merging resurrects one.
func TestTheOnlyThingAMergeCanTakeAsInputIsAStoredTicket(t *testing.T) {
	s := mergeStore(t)
	theBrokenLogin(t, s)
	// "Hii" was correctly never turned into a ticket, so it is not in the store — and naming it is
	// a missing ticket, not an invitation to create one.
	if _, err := Merge(s, specFor("m", "e1", "Hii")); !errors.Is(err, ErrNoSuchTicket) {
		t.Fatalf("naming a piece of traffic that is not a ticket = %v; want ErrNoSuchTicket", err)
	}
	if _, err := Get(s, "Hii"); !errors.Is(err, ErrNoSuchTicket) {
		t.Errorf("the refused merge created it")
	}
	// STRUCTURAL, AND LABELLED AS SUCH. InputSpec's only way to name a thing is TicketID: a field
	// carrying a body, a message or raw text would be the route this criterion forbids.
	spec := reflect.TypeOf(InputSpec{})
	for i := 0; i < spec.NumField(); i++ {
		if spec.Field(i).Type.Kind() == reflect.String && spec.Field(i).Name != "TicketID" {
			t.Errorf("InputSpec has a plain-string field %q; the only thing that may name an input "+
				"is the identifier of a stored ticket", spec.Field(i).Name)
		}
	}
}

// The reflection guard the package comment demands, extended to this file's types. A merge is where
// a priority would arrive: somebody merges an acknowledgement in and needs somewhere to put it.
func TestNothingInAMergeCarriesAPriority(t *testing.T) {
	banned := []string{"priority", "rank", "severity", "score", "order", "weight", "importance", "urgency"}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(MergeRecord{}), reflect.TypeOf(MergeInput{}),
		reflect.TypeOf(MergeSpec{}), reflect.TypeOf(InputSpec{}), reflect.TypeOf(Unmerged{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			for _, b := range banned {
				if strings.Contains(name, b) {
					t.Errorf("%s has a field %q. There is no bottom of the list, so there is no "+
						"field that puts something at it", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERION 12 — undetermined is never blank and never a value.
// ---------------------------------------------------------------------------

func TestAnUnresolvedOriginAndAnUnrecordedWhyRenderUndeterminedAndDistinctly(t *testing.T) {
	s := mergeStore(t)
	if err := Put(s, Ticket{ID: "known", Title: Text("a"), Channel: Text("email")}); err != nil {
		t.Fatal(err)
	}
	if err := Put(s, Ticket{ID: "unresolved", Title: Text("b"),
		Channel: Undetermined("the source channel could not be read")}); err != nil {
		t.Fatal(err)
	}
	if err := Put(s, Ticket{ID: "none", Title: Text("c"), Channel: Absent()}); err != nil {
		t.Fatal(err)
	}
	spec := specFor("m", "known", "unresolved", "none")
	spec.Inputs[0].Why = Text("the same problem")
	spec.Inputs[1].Why = Undetermined("no reason was recorded")
	spec.Inputs[2].Why = Absent()
	spec.Inputs[2].Source = Absent()
	if _, err := Merge(s, spec); err != nil {
		t.Fatal(err)
	}
	record, err := LoadMerge(s, "m")
	if err != nil {
		t.Fatal(err)
	}

	// PAIRWISE, NOT AGAINST LITERALS. A real value, a recorded absence and an undetermined one are
	// three answers, and the assertion that they differ has to be between them.
	channels := []string{record.Inputs[0].Channel.Render(), record.Inputs[1].Channel.Render(), record.Inputs[2].Channel.Render()}
	whys := []string{record.Inputs[0].Why.Render(), record.Inputs[1].Why.Render(), record.Inputs[2].Why.Render()}
	for what, three := range map[string][]string{"channel": channels, "why": whys} {
		for i := range three {
			for j := i + 1; j < len(three); j++ {
				if three[i] == three[j] {
					t.Errorf("two of the three renderings of %s are the same: %q", what, three[i])
				}
			}
			if strings.TrimSpace(three[i]) == "" {
				t.Errorf("a rendering of %s is silence; an undetermined field never prints as blank", what)
			}
		}
	}
	if record.Inputs[1].Channel.State() != tri.Undetermined {
		t.Errorf("an origin that could not be resolved is recorded as %s", record.Inputs[1].Channel.State())
	}
	if record.Inputs[1].Why.State() != tri.Undetermined {
		t.Errorf("a why that was not recorded is %s", record.Inputs[1].Why.State())
	}
	if record.Inputs[2].Why.State() != tri.No {
		t.Errorf("a why recorded as none is %s; 'no reason' and 'could not tell' are different facts",
			record.Inputs[2].Why.State())
	}
	// IsMerged answers in three values, and an unreadable record is undetermined and not "no".
	if v, err := IsMerged(s, "m"); v != tri.Yes || err != nil {
		t.Errorf("IsMerged on a merged ticket = %s / %v", v, err)
	}
	if err := s.Put(store.Record{Kind: MergeKind, ID: "m", Data: []byte("not json")}); err != nil {
		t.Fatal(err)
	}
	if v, _ := IsMerged(s, "m"); v != tri.Undetermined {
		t.Errorf("IsMerged on an unreadable merge record = %s; a record that could not be read is "+
			"not evidence that the ticket is an ordinary one", v)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 16 — nothing expires.
// ---------------------------------------------------------------------------

func TestAMergeMadeLongAgoIsStillListedAndStillReversible(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	before := snapshotStore(t, s)
	spec := specFor("login", ids...)
	spec.When = time.Date(2009, 1, 1, 0, 0, 0, 0, time.UTC) // seventeen years before the Issue
	if _, err := Merge(s, spec); err != nil {
		t.Fatal(err)
	}
	merges, err := ListMerges(s)
	if err != nil || len(merges) != 1 {
		t.Fatalf("a merge from 2009 is listed as %v / %v; nothing here ages out", merges, err)
	}
	if _, err := Unmerge(s, "login", time.Now()); err != nil {
		t.Fatalf("unmerging a merge from 2009 = %v; a ticket merged long ago is still unmergeable "+
			"back into its sources", err)
	}
	if !reflect.DeepEqual(before, snapshotStore(t, s)) {
		t.Errorf("the sources of a very old merge did not come back exactly")
	}
}

// Nothing in this file consults the clock to decide what exists. Structural, and labelled as such:
// the behavioural half is the test above.
func TestTheMergeCodeNeverAsksWhatTimeItIs(t *testing.T) {
	src := sourcesOf(t, ".")["merge.go"]
	if src == "" {
		t.Fatal("merge.go was not read, so this scan would pass vacuously")
	}
	for _, forbidden := range []string{"time.Now(", "time.Since(", "time.Until(", ".After(time", ".Before(time"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("merge.go contains %q. §5.4 is ruled: nothing expires, and a merge record that "+
				"is compared to the present is a merge record that can age out", forbidden)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A merged ticket may take the identifier of one of its own inputs. The batch's ordering makes this
// safe and this is what says so — the alternative is a merged ticket that deletes itself.
func TestAMergedTicketMayTakeTheIdentifierOfOneOfItsInputs(t *testing.T) {
	s := mergeStore(t)
	ids := theBrokenLogin(t, s)
	before := snapshotStore(t, s)
	if _, err := Merge(s, specFor("e1", ids...)); err != nil {
		t.Fatalf("Merge onto an input's identifier: %v", err)
	}
	if got := listIDs(t, s); !reflect.DeepEqual(got, []string{"e1"}) {
		t.Fatalf("the inbox lists %v; want just the merged ticket under e1", got)
	}
	t1, err := Get(s, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := t1.Title.Value(); !strings.Contains(v, "SSO cutover") {
		t.Errorf("e1 is not the merged ticket: %s", t1.Title.Render())
	}
	if _, err := Unmerge(s, "e1", time.Now()); err != nil {
		t.Fatalf("Unmerge: %v", err)
	}
	if !reflect.DeepEqual(before, snapshotStore(t, s)) {
		t.Errorf("unmerging a merge that took an input's identifier did not restore everything exactly")
	}
}
