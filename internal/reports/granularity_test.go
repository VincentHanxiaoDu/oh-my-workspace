package reports

import (
	"sort"
	"strings"
	"testing"
)

// sampleActivity is one fixed set of underlying activity, used wherever the ordering is asserted.
// Six items across three kinds, every text distinct and none of them a substring of an id, so that
// "does the text appear in this rendering" is a question with an honest answer.
func sampleActivity(subject string) []Item {
	return []Item{
		{ID: "a1", Subject: subject, Kind: "commit", Text: "teach the parser about dotted paths"},
		{ID: "a2", Subject: subject, Kind: "commit", Text: "refuse an unknown granularity by name"},
		{ID: "a3", Subject: subject, Kind: "commit", Text: "keep the exclusion order-independent"},
		{ID: "a4", Subject: subject, Kind: "review", Text: "read the issue again before opening"},
		{ID: "a5", Subject: subject, Kind: "review", Text: "drive the half-fix and watch it stay green"},
		{ID: "a6", Subject: subject, Kind: "spend", Text: "one paragraph per subject, for everything"},
	}
}

// detail is what "more detailed" MEANS, measured off the rendered bytes rather than asserted
// against a literal.
//
// Four independent components, each countable, each one a thing a reader can see in the output:
// how many item texts survived, how many items are individually identified, how many kinds are
// named, and how many lines the body occupies.
type detail struct {
	texts int
	ids   int
	kinds int
	lines int
}

func measure(g Granularity, items []Item) detail {
	body := strings.Join(renderBody(g, items), "\n")
	d := detail{lines: len(renderBody(g, items))}
	seenKind := map[string]bool{}
	for _, it := range items {
		if strings.Contains(body, it.Text) {
			d.texts++
		}
		if strings.Contains(body, it.ID) {
			d.ids++
		}
		if !seenKind[it.Kind] && strings.Contains(body, it.Kind) {
			seenKind[it.Kind] = true
			d.kinds++
		}
	}
	return d
}

// THE ORDERING IS A PROPERTY, NOT FIVE LITERALS (criterion 8).
//
// This walks CONSECUTIVE PAIRS of Ordered() and requires, over one fixed set of activity, that
// every component of the detail measure is non-increasing AND that at least one strictly decreases.
// It never mentions which granularity is which.
//
// WHY IT CATCHES A SWAP, which the literal-per-granularity form does not: the measure is taken from
// renderBody, which switches on the constants and does not consult the ordering at all. Swap two
// entries in byDetail and one component goes UP across that pair, so the test names the pair and
// the component. The five-assertions-against-five-literals version of this test passes unchanged
// after the same swap, which is why it is not the test here.
func TestGranularityOrderingIsContainment(t *testing.T) {
	items := sampleActivity("git")
	ord := Ordered()
	if len(ord) != 5 {
		t.Fatalf("there are %d granularities, want the PRD's five", len(ord))
	}
	for i := 0; i+1 < len(ord); i++ {
		more, less := ord[i], ord[i+1]
		a, b := measure(more, items), measure(less, items)
		check := func(name string, hi, lo int) bool {
			if lo > hi {
				t.Errorf("%s is listed as LESS detailed than %s, but its rendering has more %s (%d > %d).\n"+
					"  The ordering full ⊃ event ⊃ digest ⊃ summary ⊃ count is load-bearing: it is what\n"+
					"  lets a person move up and down the list and get strictly less each time.",
					less, more, name, lo, hi)
				return false
			}
			return true
		}
		ok := check("item texts", a.texts, b.texts)
		ok = check("identified items", a.ids, b.ids) && ok
		ok = check("named kinds", a.kinds, b.kinds) && ok
		ok = check("body lines", a.lines, b.lines) && ok
		if !ok {
			continue
		}
		if a == b {
			t.Errorf("%s and %s render with identical detail %+v — two of the five granularities are "+
				"the same thing wearing different names", more, less, a)
		}
	}
}

// CRITERION 8's SECOND HALF, SEPARATELY: count is a quantity with NO per-item content.
func TestCountHasNoPerItemContent(t *testing.T) {
	items := sampleActivity("git")
	d := measure(Count, items)
	if d.texts != 0 || d.ids != 0 || d.kinds != 0 {
		t.Errorf("count rendered per-item content %+v, want a quantity and nothing else", d)
	}
	body := strings.Join(renderBody(Count, items), "\n")
	if !strings.Contains(body, "6") {
		t.Errorf("count body %q does not carry the quantity", body)
	}
}

// CRITERION 9: full and event are distinguishable on the same subject and the same activity, and
// are NEVER byte-identical. The asymmetry named in the Issue is asserted directly — full has the
// commit message, event has the commit without it.
func TestFullAndEventDifferByTheItemsText(t *testing.T) {
	items := sampleActivity("git")
	full := strings.Join(renderBody(Full, items), "\n")
	event := strings.Join(renderBody(Event, items), "\n")
	if full == event {
		t.Fatal("full and event rendered byte-identically over the same activity")
	}
	for _, it := range items {
		if !strings.Contains(full, it.Text) {
			t.Errorf("full is missing %q — full is every item WITH its message", it.Text)
		}
		if strings.Contains(event, it.Text) {
			t.Errorf("event carries %q — event is the item WITHOUT its text", it.Text)
		}
		if !strings.Contains(event, it.ID) {
			t.Errorf("event is missing item %q — event drops the text, not the item", it.ID)
		}
	}
}

// CRITERION 7: all five are accepted on ANY subject the client knows. There is no subject with a
// vocabulary of its own.
func TestEverySubjectAcceptsEveryGranularity(t *testing.T) {
	for _, sub := range Catalog() {
		for _, g := range Ordered() {
			list := sub.Name + ":" + g.String()
			sels, err := ParseSelectors(list)
			if err != nil {
				t.Errorf("%q refused: %v", list, err)
				continue
			}
			got, unmatched := resolve(sels)
			if len(unmatched) != 0 {
				t.Errorf("%q came back unmatched", list)
			}
			if len(got) != 1 || got[0].Gran != g {
				t.Errorf("%q resolved to %v, want the one subject at %s", list, got, g)
			}
		}
	}
}

// CRITERION 10, ASSERTED AS THE EQUIVALENCE THE ISSUE ASKS FOR.
//
// `*:summary` renders byte-identically to naming every root subject at `summary` explicitly. If
// `summary` ever meant something different for one subject than for another, these two would be
// producible only by luck — and the wildcard would be a lie.
func TestWildcardSummaryEqualsNamingEverySubject(t *testing.T) {
	src := fixedSource{
		"git":             sampleActivity("git"),
		"token_usage":     {{ID: "t1", Subject: "token_usage", Kind: "spend", Text: "4210 tokens on the parser"}},
		"channel":         {{ID: "m1", Subject: "channel", Kind: "message", Text: "can you look at this"}},
		"published_notes": {{ID: "n1", Subject: "published_notes", Kind: "note", Text: "how the ordering is tested"}},
	}
	var explicit []string
	for _, s := range RootSubjects() {
		explicit = append(explicit, s.Name+":summary")
	}
	wildcard := Build(mustParse(t, "*:summary"), src).Render()
	named := Build(mustParse(t, strings.Join(explicit, ", ")), src).Render()
	if wildcard != named {
		t.Errorf("`*:summary` and %q produced different reports.\n--- wildcard ---\n%s\n--- named ---\n%s",
			strings.Join(explicit, ", "), wildcard, named)
	}
}

// CRITERION 10 PROPERLY: A GRANULARITY MEANS THE SAME THING FOR EVERY SUBJECT.
//
// Every subject is given STRUCTURALLY IDENTICAL activity — same ids, same kinds, same texts — and
// the assertion is that the rendered body is then byte-identical across subjects, at every
// granularity. Identical input, identical output: that is what "means the same thing" is.
//
// WHY THIS EXISTS ALONGSIDE THE EQUIVALENCE TEST ABOVE, and why that one is not enough. The
// equivalence test compares `*:summary` against naming every subject explicitly, and BOTH sides go
// through the same renderer — so a per-subject special case in the renderer moves both sides
// together and the equivalence stays true while the meaning has already fractured. Found by writing
// exactly that special case and watching the equivalence test stay green.
func TestAGranularityMeansTheSameForEverySubject(t *testing.T) {
	sameActivity := func(subject string) []Item {
		return []Item{
			{ID: "x1", Subject: subject, Kind: "alpha", Text: "the first thing that happened"},
			{ID: "x2", Subject: subject, Kind: "alpha", Text: "the second thing that happened"},
			{ID: "x3", Subject: subject, Kind: "beta", Text: "the third thing that happened"},
		}
	}
	src := fixedSource{}
	for _, s := range RootSubjects() {
		src[s.Name] = sameActivity(s.Name)
	}

	for _, g := range Ordered() {
		var first, firstSubject string
		for _, s := range RootSubjects() {
			body := subjectBody(t, Build(mustParse(t, s.Name+":"+g.String()), src).Render(), s.Name+":"+g.String())
			if first == "" && firstSubject == "" {
				first, firstSubject = body, s.Name
				continue
			}
			if body != first {
				t.Errorf("at %s, %s and %s render differently over identical activity:\n--- %s ---\n%s\n--- %s ---\n%s\n"+
					"  A granularity that means one thing for one subject and another for the next makes\n"+
					"  `*:%s` a lie: the person typed one word and got two different things.",
					g, firstSubject, s.Name, firstSubject, first, s.Name, body, g)
			}
		}
	}
}

// subjectBody pulls one subject's body lines out of a rendered report, so the assertion is on what
// a person actually sees rather than on the function that happened to produce it. A renderer that
// special-cases a subject AFTER calling renderBody is caught here and nowhere else.
func subjectBody(t *testing.T, rendered, heading string) string {
	t.Helper()
	lines := strings.Split(rendered, "\n")
	var body []string
	in := false
	for _, l := range lines {
		switch {
		case l == heading:
			in = true
		case in && strings.HasPrefix(l, "  "):
			body = append(body, strings.TrimPrefix(l, "  "))
		case in:
			in = false
		}
	}
	if len(body) == 0 {
		t.Fatalf("no body found under %q in:\n%s", heading, rendered)
	}
	return strings.Join(body, "\n")
}

// A GRANULARITY THAT WAS NEVER SET IS NOT THE MOST DETAILED ONE. The zero value must not be Full:
// a struct field an error path left alone would otherwise ask for the firehose.
func TestZeroGranularityIsNotFull(t *testing.T) {
	var g Granularity
	if g == Full {
		t.Fatal("the zero Granularity is Full — an unset granularity must not be a request for everything")
	}
	if g.Detail() != -1 {
		t.Errorf("an unspecified granularity has detail rank %d, want -1 (no position in the ordering)", g.Detail())
	}
	if g.String() != "unspecified" {
		t.Errorf("the zero value renders as %q", g.String())
	}
}

// fixedSource answers from a map. A subject not in the map has no activity, determined.
type fixedSource map[string][]Item

func (f fixedSource) Activity(subject string) ([]Item, error) {
	var out []Item
	for path, items := range f {
		if under(path, subject) {
			out = append(out, items...)
		}
	}
	// Sorted because a map's iteration order is random and a report whose item order changed run to
	// run would make every byte-comparison in these tests flaky rather than false.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
