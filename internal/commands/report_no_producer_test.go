// Issue #67, BLOCKER 1: `omw report run` reported "no activity in this period" forever.
//
// `git` and `token_usage` are advertised by `omw report subjects`, and nothing in the build has
// ever written activity for them. The report was taken with the daemon running, the project on the
// watched list and a commit eleven minutes old, and it came back as a quiet day — exit 0. The
// feature's own help text names precisely this failure: "it never comes back as an empty report,
// because an empty report looks exactly like a quiet day."
//
// The two facts these tests hold apart:
//
//	nothing to report because there is nothing   a determined, successful answer
//	nothing to report because nobody ever looked  an undetermined one
//
// They must not share a rendering and must not share an exit code. A test that asserted only that
// the undetermined case renders SOMETHING passes against the build this Issue was filed on.
package commands

import (
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/reports"
)

// ISSUE #67 CRITERION 1, DRIVEN THROUGH `omw report run`.
//
// The contrast is between one subject with nothing behind it and the same subject once activity
// really exists — which is what a producer landing would look like from here.
func TestASubjectWithNoProducerIsUndeterminedAndNotAQuietDay(t *testing.T) {
	// No producer, no activity: the machine in the Issue's transcript.
	silent := newReportRunner(t)
	if code, _, errOut := silent.run("subscribe", "daily", "git:full"); code != cli.Success {
		t.Fatalf("subscribing exited %d: %s", code, errOut)
	}
	silentCode, silentOut, _ := silent.run("run", "daily")

	// The same subject, with activity that really is there.
	busy := newReportRunner(t)
	if code, _, errOut := busy.run("subscribe", "daily", "git:full"); code != cli.Success {
		t.Fatalf("subscribing exited %d: %s", code, errOut)
	}
	busy.stage(reports.Item{ID: "c1", Subject: "git", Kind: "commit", Text: "a real commit"})
	busyCode, busyOut, _ := busy.run("run", "daily")

	// THE TWO MUST DIFFER, IN BOTH CHANNELS.
	if silentOut == busyOut {
		t.Errorf("a subject nothing writes and a subject with activity render identically:\n%s", silentOut)
	}
	if silentCode == busyCode {
		t.Errorf("a subject nothing writes and a subject with activity both exit %d", silentCode)
	}

	if busyCode != cli.Success {
		t.Errorf("a report that found real activity exited %d, want Success:\n%s", busyCode, busyOut)
	}
	if silentCode != cli.ExitUndetermined {
		t.Errorf("a report on a subject nothing in this build writes exited %d, want ExitUndetermined (%d):\n%s",
			silentCode, cli.ExitUndetermined, silentOut)
	}

	// AND THE WORDS. "no activity in this period" is a claim about a quiet day, and this build
	// cannot make it about git.
	if strings.Contains(silentOut, "no activity in this period") {
		t.Errorf("the report claims a quiet day for a subject it cannot observe at all:\n%s", silentOut)
	}
	if !strings.Contains(silentOut, "could not be determined") {
		t.Errorf("the report does not say the subject could not be determined:\n%s", silentOut)
	}
}

// ISSUE #67 CRITERION 2: an advertised subject that can only ever report nothing is worse than an
// absent one. `omw report subjects` must not promise silently.
func TestReportSubjectsSaysWhichSubjectsNothingWrites(t *testing.T) {
	r := newReportRunner(t)
	code, out, errOut := r.run("subjects")
	if code != cli.Success {
		t.Fatalf("omw report subjects exited %d: %s", code, errOut)
	}
	for _, name := range []string{"git", "token_usage"} {
		sub, ok := reports.LookupSubject(name)
		if !ok {
			t.Fatalf("%s is not in the catalog at all", name)
		}
		if sub.Producer != "" {
			continue // something writes it; it may be advertised without qualification
		}
		for _, line := range strings.Split(out, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), name+" ") {
				continue
			}
			if !strings.Contains(line, "nothing in this build") {
				t.Errorf("%s is advertised with no note that nothing writes it:\n%s", name, line)
			}
		}
	}
}

// The three answers a subject can end in must be three renderings, not two.
//
// DRIVEN AT THE LEVEL WHERE ALL THREE ARE REACHABLE. No subject in this build has a producer yet,
// so "read, and genuinely empty" cannot be reached through the CLI — asserting it there would mean
// asserting nothing. Here a Source is supplied directly and all three are driven for real.
func TestNoActivityAndNoProducerAndUnreadableAreThreeRenderings(t *testing.T) {
	sels, err := reports.ParseSelectors("git:count")
	if err != nil {
		t.Fatalf("parsing a selector: %v", err)
	}
	render := func(src reports.Source) (string, bool) {
		rep := reports.Build(sels, src)
		return rep.Render(), rep.Determined()
	}

	empty, emptyDetermined := render(fixedSource{items: nil, err: nil})
	noProducer, noProducerDetermined := render(fixedSource{err: reports.ErrNoProducer})
	unreadable, unreadableDetermined := render(fixedSource{err: errUnreadableSubject})

	if empty == noProducer {
		t.Errorf("'read it, there is nothing' and 'nothing writes it' render identically:\n%s", empty)
	}
	if empty == unreadable || noProducer == unreadable {
		t.Errorf("an unreadable subject shares a rendering with another answer:\n%s", unreadable)
	}
	if !emptyDetermined {
		t.Errorf("a subject that was read and was empty is not a determined answer:\n%s", empty)
	}
	if noProducerDetermined {
		t.Errorf("a subject nothing writes counts as determined, so the report would exit 0:\n%s", noProducer)
	}
	if unreadableDetermined {
		t.Errorf("a subject that could not be read counts as determined:\n%s", unreadable)
	}
}

type fixedSource struct {
	items []reports.Item
	err   error
}

func (f fixedSource) Activity(string) ([]reports.Item, error) { return f.items, f.err }

var errUnreadableSubject = errStr("this subject's records could not be read")

type errStr string

func (e errStr) Error() string { return string(e) }
