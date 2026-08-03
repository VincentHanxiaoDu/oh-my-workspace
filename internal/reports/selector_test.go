package reports

import (
	"errors"
	"strings"
	"testing"
)

// THE PRD'S OWN FIVE EXAMPLES, EACH INDEPENDENTLY (criteria 1-5). They are separate subtests rather
// than one table with a shared assertion because criterion 1..5 are five criteria, and a table that
// asserts only "parses without error" would pass for a parser that read them all as `git:full`.
func TestThePRDsFiveExamples(t *testing.T) {
	t.Run("git:full", func(t *testing.T) {
		sels := mustParse(t, "git:full")
		if len(sels) != 1 {
			t.Fatalf("got %d selectors, want 1", len(sels))
		}
		if sels[0].Subject != "git" || sels[0].Gran != Full {
			t.Errorf("read back as subject %q at %s, want git at full", sels[0].Subject, sels[0].Gran)
		}
	})

	t.Run("token_usage:digest", func(t *testing.T) {
		sels := mustParse(t, "token_usage:digest")
		if sels[0].Subject != "token_usage" || sels[0].Gran != Digest {
			t.Errorf("read back as subject %q at %s, want token_usage at digest", sels[0].Subject, sels[0].Gran)
		}
	})

	t.Run("*:summary is a wildcard, not a subject named star", func(t *testing.T) {
		sels := mustParse(t, "*:summary")
		if !sels[0].IsWildcard() || sels[0].Gran != Summary {
			t.Fatalf("read back as %v, want a wildcard at summary", sels[0])
		}
		// COVERS EVERY SUBJECT KNOWN TO THE CLIENT, NOT A SUBSET FIXED AT WRITE TIME (criterion 3).
		// Resolution consults the catalogue at run time; this asserts the resolved set IS the
		// catalogue's roots rather than anything captured when the selector was parsed.
		got, _ := resolve(sels)
		var names []string
		for _, s := range got {
			names = append(names, s.Subject)
		}
		var want []string
		for _, s := range RootSubjects() {
			want = append(want, s.Name)
		}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("*:summary covers %v, want every root subject %v", names, want)
		}
	})

	t.Run("git.commit:event keeps its dotted form", func(t *testing.T) {
		sels := mustParse(t, "git.commit:event")
		if sels[0].Subject != "git.commit" {
			t.Errorf("subject read back as %q — the dotted path was collapsed", sels[0].Subject)
		}
		if sels[0].Gran != Event {
			t.Errorf("granularity read back as %s, want event", sels[0].Gran)
		}
		// AND IT SURVIVES THE ROUND TRIP THROUGH THE FORM IT IS STORED IN (criterion 4). Storing
		// the canonical string is what a subscription does, so a collapse here is a collapse on disk.
		if sels[0].String() != "git.commit:event" {
			t.Errorf("canonical form is %q, want git.commit:event", sels[0].String())
		}
	})

	t.Run("*, !channel", func(t *testing.T) {
		sels := mustParse(t, "*, !channel")
		if len(sels) != 2 {
			t.Fatalf("got %d selectors, want 2", len(sels))
		}
		if !sels[0].IsWildcard() {
			t.Errorf("first selector is %v, want the wildcard", sels[0])
		}
		if !sels[1].Negated || sels[1].Subject != "channel" {
			t.Errorf("second selector is %v, want an exclusion of channel", sels[1])
		}
	})
}

// CRITERION 5 AND 6 TOGETHER, ON THE RESOLVED SET. The exclusion must bite, and it must bite the
// same whichever side of the wildcard it was written on — a person reading the PRD's example is not
// told an evaluation order and must not need one.
func TestExclusionAppliesRegardlessOfWrittenOrder(t *testing.T) {
	for _, list := range []string{"*, !channel", "!channel, *"} {
		t.Run(list, func(t *testing.T) {
			got, unmatched := resolve(mustParse(t, list))
			if len(unmatched) != 0 {
				t.Fatalf("unmatched %v, want none", unmatched)
			}
			var names []string
			for _, s := range got {
				names = append(names, s.Subject)
			}
			for _, n := range names {
				if n == "channel" {
					t.Errorf("%q still selects channel: %v", list, names)
				}
			}
			if len(names) == 0 {
				t.Fatalf("%q selected nothing at all; the exclusion swallowed the wildcard", list)
			}
			// The other root subjects are all still there — an exclusion excludes ONE subject.
			for _, s := range RootSubjects() {
				if s.Name == "channel" {
					continue
				}
				if !contains(names, s.Name) {
					t.Errorf("%q dropped %s as well as channel: %v", list, s.Name, names)
				}
			}
		})
	}
}

// MALFORMED SELECTORS ARE REFUSED, NAMED, AND MATCH NOTHING QUIETLY NEVER (criterion 11).
//
// Every case asserts THREE things: an error came back, it is errors.Is-able so the CLI picks an
// exit code from the value, and the message names the offending token. A test that asserted only
// "err != nil" would pass for a parser that refused everything.
func TestMalformedSelectorsAreRefusedByName(t *testing.T) {
	cases := []struct {
		list      string
		wantClass error
		names     string // a substring the refusal must contain: the offending token
	}{
		{"git:enormous", ErrUnknownGranularity, "enormous"},
		{":full", ErrMalformedSelector, ":full"},
		{"git:", ErrUnknownGranularity, "git:"},
		{"git:FULL", ErrUnknownGranularity, "FULL"},
		{"git.:full", ErrMalformedSelector, "git.:full"},
		{"git..commit:full", ErrMalformedSelector, "git..commit"},
		{"Git:full", ErrMalformedSelector, "Git:full"},
		{"git.*:full", ErrMalformedSelector, "git.*"},
		{"!channel:full", ErrMalformedSelector, "!channel:full"},
		{"git:full,", ErrMalformedSelector, "git:full,"},
		{"", ErrMalformedSelector, ""},
	}
	for _, c := range cases {
		t.Run(c.list, func(t *testing.T) {
			_, err := ParseSelectors(c.list)
			if err == nil {
				t.Fatalf("ParseSelectors(%q) was ACCEPTED. A malformed selector that is accepted "+
					"goes on to match nothing, which reads exactly like a quiet day.", c.list)
			}
			if !errors.Is(err, c.wantClass) {
				t.Errorf("error %v is not %v — the CLI picks its exit code from the value", err, c.wantClass)
			}
			if c.names != "" && !strings.Contains(err.Error(), c.names) {
				t.Errorf("refusal %q does not name the offending token %q", err.Error(), c.names)
			}
		})
	}
}

// A GRANULARITY IS NEVER COERCED TO A NEIGHBOUR (criterion 11). `summry` is nearly `summary` and
// `ful` is a prefix of `full`; both are refused rather than helpfully rounded to the nearest one.
func TestNearMissGranularitiesAreNotCoerced(t *testing.T) {
	for _, tok := range []string{"summry", "ful", "counts", "EVENT", "dig"} {
		if g, err := ParseGranularity(tok); err == nil {
			t.Errorf("ParseGranularity(%q) = %s, want a refusal — a guessed granularity produces a "+
				"report the person cannot tell from the one they asked for", tok, g)
		}
	}
}

// A WELL-FORMED SELECTOR NAMING AN UNKNOWN SUBJECT IS NOT A PARSE ERROR (criteria 12, 17). It
// parses, it is stored, and the REPORT is where it is reported as unmatched. This asserts the
// halves stay apart: refusing here would collapse "you typed nonsense" into "this build has no
// such subject", which are different facts with different fixes.
func TestUnknownButWellFormedSubjectParses(t *testing.T) {
	for _, list := range []string{"nosuchsubject:full", "!nosuchsubject", "deploy.rollback:count"} {
		if _, err := ParseSelectors(list); err != nil {
			t.Errorf("ParseSelectors(%q) refused with %v — a well-formed name for a subject this "+
				"build does not have is reported by the report, not refused by the parser", list, err)
		}
	}
}

// THE DEFAULT GRANULARITY IS WRITTEN DOWN AND OBSERVABLE. `*, !channel` names no granularity, and
// what it means is a decision this test pins so it cannot drift silently.
func TestSelectorWithNoGranularityTakesTheDefault(t *testing.T) {
	sels := mustParse(t, "*, !channel")
	if sels[0].Gran != DefaultGranularity {
		t.Errorf("`*` came out at %s, want the default %s", sels[0].Gran, DefaultGranularity)
	}
	if sels[1].Gran != GranularityUnspecified {
		t.Errorf("an exclusion came out at %s, want unspecified — an exclusion has no granularity", sels[1].Gran)
	}
}

// WHEN TWO SELECTORS BOTH MATCH, THE MORE DETAILED ONE WINS — and it does so from either written
// order, for the same reason criterion 6 exists.
func TestMostDetailedSelectorWinsFromEitherOrder(t *testing.T) {
	for _, list := range []string{"*:count, git:full", "git:full, *:count"} {
		got, _ := resolve(mustParse(t, list))
		for _, s := range got {
			if s.Subject == "git" && s.Gran != Full {
				t.Errorf("%q resolved git at %s, want full", list, s.Gran)
			}
			if s.Subject == "token_usage" && s.Gran != Count {
				t.Errorf("%q resolved token_usage at %s, want count", list, s.Gran)
			}
		}
	}
}

func mustParse(t *testing.T, list string) []Selector {
	t.Helper()
	sels, err := ParseSelectors(list)
	if err != nil {
		t.Fatalf("ParseSelectors(%q): %v", list, err)
	}
	return sels
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
