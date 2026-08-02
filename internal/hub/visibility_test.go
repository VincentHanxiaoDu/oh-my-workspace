package hub

import (
	"errors"
	"strings"
	"testing"
)

// CRITERION 1. The default is a real value, decided in one place.
func TestDefaultIsCompanyWideAndNotEmpty(t *testing.T) {
	v := Default()
	if v.Kind() != KindCompany {
		t.Fatalf("Default() kind = %v, want KindCompany", v.Kind())
	}
	if got := v.Token(); got != "company" {
		t.Errorf("Default().Token() = %q, want %q — criterion 1 forbids an empty value or 'unset'", got, "company")
	}
	if v.IsUnset() {
		t.Error("Default() reports IsUnset — the default must be a real value, not an absence")
	}
	if strings.TrimSpace(v.Describe()) == "" {
		t.Error("Default().Describe() is blank — silence is not one of the answers")
	}
}

// The zero value is NOT an audience. If this ever becomes company-wide, a Visibility left zero by
// an error path silently exposes a note to the whole company.
func TestZeroVisibilityIsNotAnAudience(t *testing.T) {
	var v Visibility
	if v.Kind() != KindUnset {
		t.Fatalf("zero Visibility kind = %v, want KindUnset", v.Kind())
	}
	if !v.IsUnset() {
		t.Error("zero Visibility does not report IsUnset")
	}
	if v.Kind() == KindCompany {
		t.Error("the zero value is company-wide — an unfilled struct field would publish to everyone")
	}
}

// CRITERION 5, tested PAIRWISE.
//
// Asserting each rendering against a string literal would pass just as happily after two of them
// were edited into the same sentence, because each assertion would have been updated to match. The
// property criterion 5 actually states is that no two are equal, so that is what is asserted.
func TestFourStatesAndUndeterminedRenderPairwiseDistinct(t *testing.T) {
	got := AllDescriptions()
	if len(got) != 5 {
		t.Fatalf("AllDescriptions has %d entries, want 5 (four states plus undetermined)", len(got))
	}
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if got[a] == got[b] {
				t.Errorf("%s and %s render identically (%q) — criterion 5: two different narrowings never render identically, and criterion 16: undetermined differs from a real value and from a negative one", a, b, got[a])
			}
			if strings.TrimSpace(got[a]) == "" {
				t.Errorf("%s renders blank", a)
			}
		}
	}
}

// CRITERION 16, stated as its own case so a failure names the right thing: the undetermined
// rendering is distinct from company-wide (a real value) and from self-only (a negative one).
func TestUndeterminedIsNeitherCompanyWideNorSelfOnly(t *testing.T) {
	u := UndeterminedDescription
	if u == CompanyWide().Describe() {
		t.Error("undetermined renders as company-wide")
	}
	if u == SelfOnly().Describe() {
		t.Error("undetermined renders as self-only")
	}
	if !strings.Contains(strings.ToLower(u), "could not be determined") {
		t.Errorf("undetermined rendering %q does not say it could not be determined", u)
	}
}

// The four tokens are distinct too — a caller branching on Token must be able to.
func TestTokensPairwiseDistinct(t *testing.T) {
	vs := []Visibility{CompanyWide(), mustPeople("a"), mustGroup("g"), SelfOnly()}
	for i := 0; i < len(vs); i++ {
		for j := i + 1; j < len(vs); j++ {
			if vs[i].Token() == vs[j].Token() {
				t.Errorf("tokens collide: %q", vs[i].Token())
			}
		}
	}
}

func TestParseChoice(t *testing.T) {
	t.Run("nothing said means company-wide", func(t *testing.T) {
		v, err := ParseChoice("")
		if err != nil {
			t.Fatalf("ParseChoice(%q) errored: %v", "", err)
		}
		if v.Kind() != KindCompany {
			t.Errorf("ParseChoice(%q) = %v, want company-wide (criterion 1)", "", v.Kind())
		}
	})
	t.Run("the four choices", func(t *testing.T) {
		cases := map[string]Kind{
			"company":         KindCompany,
			"self":            KindSelf,
			"me":              KindSelf,
			"group:platform":  KindGroup,
			"people:alice,bo": KindPeople,
		}
		for in, want := range cases {
			v, err := ParseChoice(in)
			if err != nil {
				t.Errorf("ParseChoice(%q) errored: %v", in, err)
				continue
			}
			if v.Kind() != want {
				t.Errorf("ParseChoice(%q) kind = %v, want %v", in, v.Kind(), want)
			}
		}
	})
	t.Run("people are sorted and deduplicated", func(t *testing.T) {
		v, err := ParseChoice("people: bo , alice, bo ")
		if err != nil {
			t.Fatalf("ParseChoice errored: %v", err)
		}
		got := v.People()
		if len(got) != 2 || got[0] != "alice" || got[1] != "bo" {
			t.Errorf("People() = %v, want [alice bo]", got)
		}
	})
	t.Run("naming nobody is refused, not an empty audience", func(t *testing.T) {
		_, err := ParseChoice("people:  ")
		if !errors.Is(err, ErrEmptyAudience) {
			t.Errorf("ParseChoice(people with no names) = %v, want ErrEmptyAudience", err)
		}
	})
	t.Run("nonsense is refused with a code", func(t *testing.T) {
		_, err := ParseChoice("everyone-ish")
		if Code(err) != ErrUnknownVisibility.Code {
			t.Errorf("code = %q, want %q", Code(err), ErrUnknownVisibility.Code)
		}
	})
}

// Every hub error must be tellable apart from every other one, by code and by wording. Criteria 10,
// 12 and 15 are all "these two outcomes must not look the same".
func TestHubErrorsArePairwiseDistinguishable(t *testing.T) {
	for i := 0; i < len(allErrors); i++ {
		for j := i + 1; j < len(allErrors); j++ {
			a, b := allErrors[i], allErrors[j]
			if a.Code == b.Code {
				t.Errorf("two errors share the code %q", a.Code)
			}
			if a.Msg == b.Msg {
				t.Errorf("two errors share the message %q", a.Msg)
			}
		}
		if allErrors[i].Code == "" || allErrors[i].Msg == "" {
			t.Errorf("error %d has an empty code or message", i)
		}
	}
}

// Code survives wrapping, so a surface may add detail and a script still branches on the same value.
func TestCodeSurvivesWrapping(t *testing.T) {
	err := Refusedf(ErrUnknownGroup, "%q", "platform")
	if Code(err) != ErrUnknownGroup.Code {
		t.Errorf("Code(wrapped) = %q, want %q", Code(err), ErrUnknownGroup.Code)
	}
	if !errors.Is(err, ErrUnknownGroup) {
		t.Error("errors.Is does not see through the wrap")
	}
	if !strings.Contains(err.Error(), "platform") {
		t.Errorf("wrapped error lost its detail: %q", err.Error())
	}
}
