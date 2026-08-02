// Package health reports the deployment assumptions the product rests on (PRD §3.9), of which
// full-disk encryption (PRD §4.1) is the first.
//
// WHAT THIS PACKAGE IS CAREFUL ABOUT, AND WHY IT IS A PACKAGE AT ALL:
//
//   - The answer is three-valued and the third value is real. `enabled`, `not enabled`, and
//     `could not be determined on this platform` are three distinct answers, and the third is
//     never rendered as, nor collapsed into, the second (PRD §4.3). The three-valued type lives in
//     internal/tri and is NOT reinvented here — this package only decides which of the three the
//     machine is in, and how to say it.
//   - The platform probe is behind an interface. A test must be able to force each of the three
//     outcomes without owning the machine it runs on, so the probe is a value on the Runner rather
//     than a call to os/exec buried in a branch.
//   - It reads the machine, not the product. Health needs no store and no daemon (PRD §4.1), so
//     nothing here opens a store, starts a process other than the platform probe itself, or
//     touches the filesystem. It imports no network package, and a test asserts that by parsing
//     this package's own source.
//
// A REPORT, NEVER A BLOCKER (PRD §4.1). `not enabled` is a successful health run: the product
// answered the question it was asked. Only a run that could not complete the check reports the
// undetermined value, and only that produces a non-zero exit — see internal/commands/health.go.
package health

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// EncryptionAssumption is the name health prints for PRD §4.1, and the string a test looks for.
const EncryptionAssumption = "full-disk encryption"

// undeterminedSuffix qualifies tri's fixed undetermined wording for this capability.
//
// The third value's wording is NOT this package's to invent — tri owns it precisely so that no
// capability can spell it as something a reader might mistake for a negative. What is added here
// is only the qualifier PRD §4.1 gives the sentence: it could not be determined ON THIS PLATFORM.
// The rendering therefore reads "could not be determined on this platform" and remains distinct
// from both "enabled" and "not enabled" for exactly the reason tri makes it distinct.
const undeterminedSuffix = " on this platform"

// EncryptionChecker reads the machine's full-disk encryption state.
//
// It returns (bool, error) rather than a tri.Value on purpose: a checker's job is to report what it
// found and whether it could look, and tri.FromError is the one sanctioned place that pair becomes
// an answer. A checker that returned a tri.Value could quietly return No on a failure, which is the
// defect the whole design is here to make unavailable.
type EncryptionChecker interface {
	// Mechanism names what is being read, e.g. "FileVault, via `fdesetup status`". It is reported
	// alongside every value INCLUDING the undetermined one, so a person who is told the state
	// could not be determined is also told what the product tried.
	Mechanism() string
	// Enabled reports whether full-disk encryption is on. A non-nil error means the check could not
	// be completed and the answer is undetermined — never "not enabled".
	Enabled(ctx context.Context) (bool, error)
}

// Assumption is one deployment assumption, reported (PRD §3.9).
type Assumption struct {
	// Name is what was checked, e.g. EncryptionAssumption.
	Name string
	// Ref is the PRD section the assumption comes from, so a reader can go and read it.
	Ref string
	// Value is the answer, in three values.
	Value tri.Value
	// Mechanism is how the product looked. Present on all three values.
	Mechanism string
	// Reason is why the value could not be determined. Empty when it could.
	Reason string
}

// Rendered is the assumption's value as a person reads it: "enabled", "not enabled", or
// "could not be determined on this platform". Never empty — silence is not one of the three.
func (a Assumption) Rendered() string {
	s := a.Value.Render("enabled", "not enabled")
	if !a.Value.Determined() {
		return s + undeterminedSuffix
	}
	return s
}

// Report is the whole of what a health run found.
type Report struct {
	// Platform is the GOOS the run reports for.
	Platform string
	// Assumptions are the deployment assumptions, in report order. Always at least the encryption
	// one: health for a supported platform always emits a value for it.
	Assumptions []Assumption
	// HubConfigured records whether a hub was configured for this run. Health does not contact one
	// either way; this is reported so a reader knows which half of the product answered.
	HubConfigured bool
	// MissingForLackOfHub names precisely the parts of the report that are unavailable because no
	// hub is configured (PRD §4.4, criterion 8). It is empty when nothing is missing, and health
	// NAMES what is missing rather than omitting it silently.
	MissingForLackOfHub []string
}

// Determined reports whether every assumption in the report got a real answer either way. A
// summary may not lead with "all good" when this is false (PRD §4.3 read across to §3.9).
func (r Report) Determined() bool {
	for _, a := range r.Assumptions {
		if !a.Value.Determined() {
			return false
		}
	}
	return true
}

// Encryption returns the full-disk encryption assumption. The second result is false only if the
// report was built by something other than Runner.Run — Run always emits it.
func (r Report) Encryption() (Assumption, bool) {
	for _, a := range r.Assumptions {
		if a.Name == EncryptionAssumption {
			return a, true
		}
	}
	return Assumption{}, false
}

// Runner performs a health run. The zero Runner reports for the running platform using the real
// platform probe; a test sets Checker to force any of the three outcomes.
type Runner struct {
	// GOOS is the platform to report for. Empty means runtime.GOOS.
	GOOS string
	// Checker is the encryption probe. Nil means the real probe for GOOS — which, on a platform
	// with no probe (Windows is out of scope for this slice), is nil and yields the undetermined
	// value rather than a guess.
	Checker EncryptionChecker
	// Getenv reads configuration. Nil means nothing is configured, which is the state health must
	// work fully in (PRD §4.4).
	Getenv func(string) string
}

// HubEnv is the variable that configures a hub. Health reads it only to REPORT whether one is
// configured; it never contacts it. With no hub configured, health reports in full (PRD §4.4).
const HubEnv = "OMW_HUB"

// Run performs the health run and returns what it found. It never returns an error: a check that
// could not be completed is a REPORTED value, not a failure of the run (PRD §4.1, criterion 4).
func (r Runner) Run(ctx context.Context) Report {
	goos := r.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	getenv := r.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	checker := r.Checker
	if checker == nil {
		checker = CheckerFor(goos)
	}

	a := Assumption{Name: EncryptionAssumption, Ref: "PRD §4.1"}
	switch {
	case checker == nil:
		// NO PROBE FOR THIS PLATFORM. Windows is out of scope for this slice (criterion 15), and
		// the honest answer for a platform with no probe is the third value — not a guess, and
		// certainly not "not enabled", which would tell a person their disk is unprotected on the
		// strength of the product not having looked.
		a.Value = tri.Undetermined
		a.Mechanism = "no full-disk encryption probe for " + goos
		a.Reason = "this slice ships probes for macOS and Linux only"
	default:
		a.Mechanism = checker.Mechanism()
		ok, err := checker.Enabled(ctx)
		// THE ONE SANCTIONED CONVERSION. tri.FromError returns Undetermined whenever err != nil,
		// so an error can never become No here. Writing `if err != nil { ... } else if ok {...}`
		// by hand is how the two collapse.
		a.Value = tri.FromError(ok, err)
		if err != nil {
			a.Reason = err.Error()
		}
	}

	rep := Report{
		Platform:      goos,
		Assumptions:   []Assumption{a},
		HubConfigured: strings.TrimSpace(getenv(HubEnv)) != "",
	}
	// NOTHING IN THIS REPORT NEEDS A HUB, so nothing is missing for the lack of one. The field
	// exists and is deliberately left empty rather than absent: criterion 8 requires that anything
	// unavailable be NAMED, and a later assumption that does need a hub appends its name here.
	return rep
}

// Write renders the report the way a person reads it.
//
// EVERY ASSUMPTION PRINTS A LINE, whichever of the three values it holds. Undetermined is never
// silence (criterion 3): it is not an omitted line, not an empty value, and not a fallback.
func Write(w io.Writer, r Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "omw health — reported deployment assumptions (%s)\n\n", r.Platform)
	for _, a := range r.Assumptions {
		fmt.Fprintf(&b, "  %s (%s): %s\n", a.Name, a.Ref, a.Rendered())
		fmt.Fprintf(&b, "      checked by: %s\n", a.Mechanism)
		if a.Reason != "" {
			fmt.Fprintf(&b, "      why not determined: %s\n", a.Reason)
		}
	}
	b.WriteString("\n")
	if r.HubConfigured {
		b.WriteString("  hub: configured. Health did not contact it — it reads this machine.\n")
	} else {
		b.WriteString("  hub: not configured. Health reports in full without one and made no\n")
		b.WriteString("       outbound connection.\n")
	}
	if len(r.MissingForLackOfHub) > 0 {
		b.WriteString("  not reported for lack of a configured hub: " +
			strings.Join(r.MissingForLackOfHub, ", ") + "\n")
	}
	b.WriteString("  This run needed no store and no running daemon, and started neither.\n")
	_, err := io.WriteString(w, b.String())
	return err
}
