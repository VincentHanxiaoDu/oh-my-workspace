package reports

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// stagedSource lets each subject be staged as one of the four things it can be: it has activity, it
// has none, it cannot be read, or it needs a hub there is none of.
type stagedSource struct {
	items        map[string][]Item
	unreadable   map[string]bool
	hubOnly      map[string]bool
	hubConfigure bool
}

func newStaged() *stagedSource {
	return &stagedSource{items: map[string][]Item{}, unreadable: map[string]bool{}, hubOnly: map[string]bool{}}
}

var errCannotRead = errors.New("the underlying source could not be read")

func (s *stagedSource) Activity(subject string) ([]Item, error) {
	if s.hubOnly[subject] && !s.hubConfigure {
		return nil, ErrNoHubConfigured
	}
	if s.unreadable[subject] {
		return nil, errCannotRead
	}
	var out []Item
	for path, items := range s.items {
		if under(path, subject) {
			out = append(out, items...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// THREE DISTINCT FACTS, THREE DISTINCT OUTPUTS (criteria 12, 13, 14).
//
// This is the heart of the Issue. It builds all three reports and asserts pairwise that no two of
// them render the same bytes — which is the only form of this assertion that cannot be satisfied by
// a renderer that happens to print something for each of them.
func TestActivityEmptinessAndUnknownSubjectAreThreeDistinctOutputs(t *testing.T) {
	withActivity := newStaged()
	withActivity.items["git"] = []Item{{ID: "c1", Subject: "git", Kind: "commit", Text: "the first one"}}
	hadActivity := Build(mustParse(t, "git:full"), withActivity).Render()

	quiet := newStaged()
	quietDay := Build(mustParse(t, "git:full"), quiet).Render()

	unknown := Build(mustParse(t, "nosuchsubject:full"), newStaged()).Render()

	pairs := []struct {
		aName, a string
		bName, b string
	}{
		{"a subject with activity", hadActivity, "a subject with no activity", quietDay},
		{"a subject with no activity", quietDay, "an unknown subject", unknown},
		{"a subject with activity", hadActivity, "an unknown subject", unknown},
	}
	for _, p := range pairs {
		if p.a == p.b {
			t.Errorf("%s and %s render identically:\n%s", p.aName, p.bName, p.a)
		}
	}

	// AND EACH SAYS WHICH IT IS, in words, by output alone and with no other information.
	if !strings.Contains(quietDay, noActivityLine) {
		t.Errorf("a quiet day renders as %q, which does not say it is a quiet day", quietDay)
	}
	if !strings.Contains(unknown, unmatchedPrefix) || !strings.Contains(unknown, "nosuchsubject") {
		t.Errorf("an unknown subject renders as %q — criterion 12 wants it stated AND the selector named", unknown)
	}
	// A quiet day is NOT an unmatched selector. This is the direction the defect actually goes:
	// the subject exists, so nothing may be reported as unmatched.
	if strings.Contains(quietDay, unmatchedPrefix) {
		t.Errorf("a real subject with no activity was reported as unmatched:\n%s", quietDay)
	}
	// An unknown subject is NOT a quiet day.
	if strings.Contains(unknown, noActivityLine) {
		t.Errorf("an unknown subject was reported as a quiet day:\n%s", unknown)
	}
}

// CRITERION 18: a subject that could not be READ is undetermined — never empty, never zero.
//
// The `count` case is asserted specifically because that is where the defect is invisible: an
// unreadable subject that renders `count: 0` is a report that looks completely normal.
func TestUnreadableSubjectIsUndeterminedAndNeverZero(t *testing.T) {
	broken := newStaged()
	broken.unreadable["git"] = true
	quiet := newStaged()

	for _, g := range Ordered() {
		list := "git:" + g.String()
		undet := Build(mustParse(t, list), broken)
		empty := Build(mustParse(t, list), quiet)

		if undet.Determined() {
			t.Errorf("%s: a subject that could not be read reports as determined", list)
		}
		if !empty.Determined() {
			t.Errorf("%s: a subject that was read and found empty reports as undetermined", list)
		}
		u, e := undet.Render(), empty.Render()
		if u == e {
			t.Errorf("%s: unreadable and empty render identically:\n%s", list, u)
		}
		if !strings.Contains(u, undeterminedLine) {
			t.Errorf("%s: an unreadable subject renders as %q", list, u)
		}
		if strings.Contains(u, "count: 0") {
			t.Errorf("%s: an unreadable subject rendered as `count: 0`.\n"+
				"  `count` returning 0 and `count` being unreadable are different facts (§4.3):\n%s", list, u)
		}
		if strings.Contains(u, noActivityLine) {
			t.Errorf("%s: an unreadable subject rendered as a quiet day:\n%s", list, u)
		}
	}
	// And `count: 0` remains the right answer for a subject that WAS read and had nothing — the fix
	// above must not have been made by deleting the zero.
	empty := Build(mustParse(t, "git:count"), quiet).Render()
	if !strings.Contains(empty, noActivityLine) {
		t.Errorf("a read-and-empty subject at count renders as %q", empty)
	}
}

// CRITERION 19: an undetermined subject inside `*:summary` does not remove the subject and does not
// abort the report.
func TestUndeterminedSubjectInsideAWildcardDoesNotAbortTheReport(t *testing.T) {
	s := newStaged()
	s.unreadable["channel"] = true
	s.items["git"] = []Item{{ID: "c1", Subject: "git", Kind: "commit", Text: "still reported"}}
	s.items["token_usage"] = []Item{{ID: "s1", Subject: "token_usage", Kind: "spend", Text: "4210"}}

	out := Build(mustParse(t, "*:summary"), s).Render()
	if !strings.Contains(out, "channel:summary") {
		t.Errorf("the undetermined subject dropped out of the report:\n%s", out)
	}
	if !strings.Contains(out, undeterminedLine) {
		t.Errorf("the undetermined subject is present but not marked as such:\n%s", out)
	}
	for _, other := range []string{"git:summary", "token_usage:summary"} {
		if !strings.Contains(out, other) {
			t.Errorf("%s stopped reporting because another subject was undetermined:\n%s", other, out)
		}
	}
	if !strings.Contains(out, "1 item(s)") {
		t.Errorf("the subjects that could be read reported nothing:\n%s", out)
	}
}

// CRITERION 15: in a mixed subscription the good selectors still report AND the bad one is still
// named. Neither half suppresses the other.
func TestMixedSubscriptionReportsBothHalves(t *testing.T) {
	s := newStaged()
	s.items["git"] = []Item{{ID: "c1", Subject: "git", Kind: "commit", Text: "a real commit"}}
	out := Build(mustParse(t, "git:full, nosuchsubject:count, token_usage:summary"), s).Render()

	if !strings.Contains(out, "a real commit") {
		t.Errorf("the matching selector was suppressed by the unmatched one:\n%s", out)
	}
	if !strings.Contains(out, "token_usage:summary") {
		t.Errorf("a second matching selector was suppressed:\n%s", out)
	}
	if !strings.Contains(out, `unmatched selector "nosuchsubject:count"`) {
		t.Errorf("the unmatched selector was dropped quietly:\n%s", out)
	}
}

// CRITERION 17: excluding a subject that does not exist is a typo, and the person is told — on the
// same terms as criterion 12, including the exclamation mark so they can see what they typed.
func TestExcludingAnUnknownSubjectIsReportedAsUnmatched(t *testing.T) {
	r := Build(mustParse(t, "*, !nosuchsubject"), newStaged())
	out := r.Render()
	if !r.HasUnmatched() {
		t.Fatalf("`!nosuchsubject` was treated as a no-op:\n%s", out)
	}
	if !strings.Contains(out, `unmatched selector "!nosuchsubject"`) {
		t.Errorf("the unmatched exclusion is not named as written:\n%s", out)
	}
	// And it excluded nothing, so every root subject is still present.
	for _, sub := range RootSubjects() {
		if !strings.Contains(out, sub.Name+":") {
			t.Errorf("%s went missing from a report whose only exclusion was of a subject that does not exist:\n%s", sub.Name, out)
		}
	}
}

// CRITERION 16: a wildcard is never unmatched merely because nothing has activity.
func TestWildcardIsNeverUnmatched(t *testing.T) {
	for _, list := range []string{"*", "*:summary", "*, !channel"} {
		r := Build(mustParse(t, list), newStaged())
		if r.HasUnmatched() {
			t.Errorf("%q was reported as unmatched over an empty world: %v", list, r.Unmatched)
		}
		out := r.Render()
		if !strings.Contains(out, noActivityLine) {
			t.Errorf("%q over an empty world does not say the subjects were quiet:\n%s", list, out)
		}
		if strings.Contains(out, unmatchedPrefix) {
			t.Errorf("%q over an empty world produced an unmatched line:\n%s", list, out)
		}
	}
}

// CRITERION 5's SECOND HALF, ON THE REPORT ITSELF: channel content appears at NO granularity.
func TestExcludedSubjectAppearsAtNoGranularity(t *testing.T) {
	s := newStaged()
	s.items["channel"] = []Item{{ID: "m1", Subject: "channel", Kind: "message", Text: "the noisy one"}}
	s.items["git"] = []Item{{ID: "c1", Subject: "git", Kind: "commit", Text: "the quiet one"}}

	for _, list := range []string{"*, !channel", "!channel, *", "*:count, !channel", "*:full, !channel"} {
		out := Build(mustParse(t, list), s).Render()
		if strings.Contains(out, "channel") {
			t.Errorf("%q still mentions channel:\n%s", list, out)
		}
		if strings.Contains(out, "the noisy one") {
			t.Errorf("%q carries channel content:\n%s", list, out)
		}
		if !strings.Contains(out, "git:") {
			t.Errorf("%q dropped git along with channel:\n%s", list, out)
		}
	}
}

// An exclusion of a NARROWER path takes those items out of the parent's report too, which is what
// `git:full, !git.commit` reads as.
func TestExcludingANarrowerPathRemovesItFromItsParent(t *testing.T) {
	s := newStaged()
	s.items["git.commit"] = []Item{{ID: "c1", Subject: "git.commit", Kind: "commit", Text: "a commit"}}
	s.items["git"] = []Item{{ID: "g1", Subject: "git", Kind: "branch", Text: "a branch"}}

	out := Build(mustParse(t, "git:full, !git.commit"), s).Render()
	if strings.Contains(out, "a commit") {
		t.Errorf("`!git.commit` did not take the commits out of git's report:\n%s", out)
	}
	if !strings.Contains(out, "a branch") {
		t.Errorf("`!git.commit` took the rest of git with it:\n%s", out)
	}
}

// `git:full` covers everything under git; `git.commit:event` covers only the commits. This is what
// makes the dotted path "the narrower subject" rather than a second name for the same thing.
func TestDottedPathSelectsTheNarrowerSubject(t *testing.T) {
	s := newStaged()
	s.items["git.commit"] = []Item{{ID: "c1", Subject: "git.commit", Kind: "commit", Text: "a commit"}}
	s.items["git"] = []Item{{ID: "g1", Subject: "git", Kind: "branch", Text: "a branch"}}

	wide := Build(mustParse(t, "git:event"), s).Render()
	if !strings.Contains(wide, "c1") || !strings.Contains(wide, "g1") {
		t.Errorf("git:event does not cover everything under git:\n%s", wide)
	}
	narrow := Build(mustParse(t, "git.commit:event"), s).Render()
	if !strings.Contains(narrow, "c1") {
		t.Errorf("git.commit:event missed the commit:\n%s", narrow)
	}
	if strings.Contains(narrow, "g1") {
		t.Errorf("git.commit:event reached outside the narrower subject:\n%s", narrow)
	}
	if !strings.Contains(narrow, "git.commit:event") {
		t.Errorf("the report collapsed the dotted path in its own heading:\n%s", narrow)
	}
}

// CRITERION 23: a hub-supplied subject with no hub says precisely what is missing, and is
// distinguishable from ALL THREE of real activity, no activity, and unknown subject.
func TestHubOnlySubjectWithNoHubIsItsOwnAnswer(t *testing.T) {
	s := newStaged()
	s.hubOnly["published_notes"] = true
	noHub := Build(mustParse(t, "published_notes:summary"), s).Render()

	if !strings.Contains(noHub, ErrNoHubConfigured.Error()) {
		t.Errorf("a hub-supplied subject with no hub renders as %q, which does not say a hub is what is missing", noHub)
	}
	if strings.Contains(noHub, noActivityLine) {
		t.Errorf("a hub-supplied subject with no hub rendered as empty:\n%s", noHub)
	}
	if strings.Contains(noHub, unmatchedPrefix) {
		t.Errorf("a hub-supplied subject with no hub rendered as unknown:\n%s", noHub)
	}

	active := newStaged()
	active.items["published_notes"] = []Item{{ID: "n1", Subject: "published_notes", Kind: "note", Text: "a note"}}
	quiet := newStaged()
	unknown := Build(mustParse(t, "nosuchsubject:summary"), s).Render()
	others := map[string]string{
		"real activity":     Build(mustParse(t, "published_notes:summary"), active).Render(),
		"no activity":       Build(mustParse(t, "published_notes:summary"), quiet).Render(),
		"unknown subject":   unknown,
		"could not be read": buildUnreadable(t),
	}
	for name, other := range others {
		if noHub == other {
			t.Errorf("no-hub and %s render identically:\n%s", name, noHub)
		}
	}
	// The subject is NOT omitted from the report (criterion 23's "rather than ... omitting it").
	if !strings.Contains(noHub, "published_notes:summary") {
		t.Errorf("the hub-supplied subject was omitted:\n%s", noHub)
	}
}

func buildUnreadable(t *testing.T) string {
	t.Helper()
	s := newStaged()
	s.unreadable["published_notes"] = true
	return Build(mustParse(t, "published_notes:summary"), s).Render()
}

// CRITERION 22: purely local subjects work end to end with no hub, with NO degradation and NO
// warning about a missing hub. The absence of a warning is asserted, because a warning here would
// be the degradation.
func TestLocalOnlySubscriptionSaysNothingAboutAHub(t *testing.T) {
	s := newStaged()
	s.hubOnly["published_notes"] = true
	s.items["git"] = []Item{{ID: "c1", Subject: "git", Kind: "commit", Text: "a local commit"}}
	s.items["token_usage"] = []Item{{ID: "s1", Subject: "token_usage", Kind: "spend", Text: "4210"}}

	r := Build(mustParse(t, "git:full, token_usage:digest"), s)
	out := r.Render()
	if r.HasMissingHub() {
		t.Errorf("a local-only subscription reported a missing hub:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "hub") {
		t.Errorf("a local-only subscription mentioned the hub at all:\n%s", out)
	}
	if !r.Determined() || r.HasUnmatched() {
		t.Errorf("a local-only subscription did not come back clean:\n%s", out)
	}
	if !strings.Contains(out, "a local commit") || !strings.Contains(out, "- spend: 1") {
		t.Errorf("a local-only subscription did not produce its content:\n%s", out)
	}
}
