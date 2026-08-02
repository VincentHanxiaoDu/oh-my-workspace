package projects

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// MarshalJSON gives provenance a stable name on the wire.
//
// An integer would put criterion 6's whole distinction behind a number whose meaning lives in this
// file's constant ordering — and a control API written against `1` keeps compiling, and keeps
// reporting the wrong provenance, the day somebody inserts a constant above it.
func (p Provenance) MarshalJSON() ([]byte, error) {
	switch p {
	case DaemonPolled:
		return []byte(`"daemon-polled"`), nil
	case ExaminedNow:
		return []byte(`"examined-now"`), nil
	default:
		return []byte(`"unrecorded"`), nil
	}
}

// UnmarshalJSON is the inverse. An unrecognised name becomes ProvenanceUnrecorded rather than a
// guess: a surface that sent something this build does not know has not told us the provenance.
func (p *Provenance) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch s {
	case "daemon-polled":
		*p = DaemonPolled
	case "examined-now":
		*p = ExaminedNow
	default:
		*p = ProvenanceUnrecorded
	}
	return nil
}

// DescribeState is the ONE rendering of a project's directory state, and the three-way distinction
// criteria 8, 9, 10 and 20 require lives here in one switch.
//
// WHY ONE FUNCTION AND ONE SWITCH. The criteria demand that missing, unreadable and empty produce
// three renderings, "no two alike". Spread across three call sites, two of them converge the first
// time somebody tidies the wording — and the test that catches it must compare the three against
// each other, not against literals, because literals stay green after two are edited to match. One
// switch means the three phrases sit on adjacent lines where a reader can see they differ.
func DescribeState(s State) string {
	switch {
	case s.Present == tri.No:
		// CRITERION 8. Marked, never dropped. Notice there is no count here: "0 files" beside a
		// missing directory is criterion 9's named failure.
		return "MISSING — the directory is not there"
	case s.Present != tri.Yes:
		return "could not be determined — whether the directory exists could not be read"
	case s.Readable == tri.Undetermined && !s.PartiallyRead():
		// CRITERION 10. It exists; its state could not be read. Distinct from missing above and from
		// empty below, and the word "could not be determined" is tri's fixed wording for the third
		// answer, not a synonym invented here.
		return "could not be determined — the directory is there and could not be read"
	case s.PartiallyRead():
		// CRITERION 21. A partially-read project must never render as a complete scan, so the count
		// it does have is stated as a floor and the unreadable portion is named.
		return "partially read — " + strconv.Itoa(s.Files) + " file(s) reached, " +
			strconv.Itoa(len(s.UnreadablePaths)) + " unreadable: " + firstFew(s.UnreadablePaths)
	case s.Files == 0:
		// CRITERION 9 and 20. Empty is a determined answer and says so in words that share nothing
		// with the missing phrase above.
		return "empty — the directory is there and holds nothing"
	default:
		return strconv.Itoa(s.Files) + " file(s)"
	}
}

func firstFew(paths []string) string {
	const max = 3
	if len(paths) <= max {
		return join(paths)
	}
	return join(paths[:max]) + fmt.Sprintf(" (+%d more)", len(paths)-max)
}

func join(paths []string) string {
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// DescribeScan states what the walk itself did, separately from what it found. Criterion 16.
//
// SEPARATE FROM DescribeState BECAUSE TRUNCATION IS ORTHOGONAL. A truncated walk can find plenty of
// files or none; folding it into the state phrase would make "empty" and "truncated at depth 2 and
// empty so far" the same sentence, which is the "a truncated walk renders as a complete one" the
// criterion forbids. It returns "" only when there is nothing about the walk worth saying — for a
// missing directory there was no walk.
func DescribeScan(s State) string {
	if s.Present != tri.Yes {
		return ""
	}
	if s.DepthLimitReached {
		return "scan TRUNCATED at depth " + strconv.Itoa(s.DepthLimit) +
			" — content below that level is not reported"
	}
	return "scan complete to depth " + strconv.Itoa(s.DepthLimit)
}

// Render writes the listing a person sees. It is the CLI's whole rendering of a snapshot.
//
// EVERY ENTRY CARRIES ITS PROVENANCE, UNCONDITIONALLY — criterion 6. There is no quiet mode, no
// short form and no "only when it differs from the header": each of those is a build in which some
// listing is indistinguishable from the other case, which is exactly what the criterion is about.
// The header states the watching answer as well, because "nothing is watching" is itself something a
// person asked to be told.
func Render(w io.Writer, snap Snapshot) error {
	var err error
	p := func(format string, args ...any) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(w, format, args...)
	}

	switch snap.Watching {
	case tri.Yes:
		p("watching: yes — the daemon is watching these directories\n")
	case tri.No:
		p("watching: no — nothing is watching between commands\n")
	default:
		p("watching: %s — whether anything is watching could not be read\n", tri.Undetermined)
	}

	if len(snap.Entries) == 0 {
		// SAID, NOT BLANK. No projects is a real state and must not print as a rendering bug.
		p("no projects. Add one with 'omw projects add <directory>'.\n")
		return err
	}

	p("\n")
	for _, e := range snap.Entries {
		p("%s\n", e.Project.Path)
		p("    state:      %s\n", DescribeState(e.State))
		if scan := DescribeScan(e.State); scan != "" {
			p("    scan:       %s\n", scan)
		}
		// The provenance line is last and is never conditional. Criterion 6.
		p("    state from: %s\n", e.Provenance)
	}
	return err
}
