package reports

import "strings"

// Subject is something the client can report on.
//
// # THE TAXONOMY IS DELIBERATELY SMALL
//
// The PRD names three subjects — `git`, `token_usage`, `channel` — and one narrower path,
// `git.commit`. Those are here, plus exactly one subject that only a hub can supply
// (`published_notes`), because criterion 23 requires that case to exist before it can be reported
// on. Nothing else was invented: a taxonomy guessed ahead of the capabilities that fill it is a
// list of subjects that report nothing and cannot be removed later without breaking someone's
// stored subscription. Adding one is a line in [catalog] plus whatever writes its activity.
type Subject struct {
	// Name is the dotted path a selector writes.
	Name string
	// Root marks a subject the wildcard enumerates. A narrower path (`git.commit`) is selectable by
	// name but is NOT enumerated by `*`, because its activity is already inside its parent's and
	// `*` would otherwise report every commit twice.
	Root bool
	// HubOnly marks a subject no amount of local data can answer. With no hub configured it is
	// neither empty nor unknown — see [StateNoHub].
	HubOnly bool
	// About is one line, shown by `omw report subjects`.
	About string
}

// catalog is every subject this build knows.
var catalog = []Subject{
	{Name: "git", Root: true, About: "work in this person's repositories"},
	{Name: "git.commit", About: "commits specifically — the narrower path inside git"},
	{Name: "token_usage", Root: true, About: "model spend"},
	{Name: "channel", Root: true, About: "channel traffic (§3.1)"},
	{Name: "published_notes", Root: true, HubOnly: true, About: "notes published to the hub (§3.11) — the hub supplies this one"},
}

// Catalog returns every known subject, in the order a person should read them.
func Catalog() []Subject {
	out := make([]Subject, len(catalog))
	copy(out, catalog)
	return out
}

// RootSubjects are the subjects a wildcard enumerates.
func RootSubjects() []Subject {
	var out []Subject
	for _, s := range catalog {
		if s.Root {
			out = append(out, s)
		}
	}
	return out
}

// LookupSubject finds a subject by its exact dotted path.
//
// EXACT, NOT PREFIX. `git.commit` is its own subject and `gitx` is not `git`; a prefix match here
// is how a typo becomes a plausible-looking report about something else.
func LookupSubject(name string) (Subject, bool) {
	for _, s := range catalog {
		if s.Name == name {
			return s, true
		}
	}
	return Subject{}, false
}

// under reports whether path is the subject itself or lies beneath it in the dotted hierarchy.
//
// This is what makes `git:full` cover commits while `git.commit:event` covers only commits: the
// activity a subject reports is its own plus everything under it. It is a segment-wise test, so
// `gitlab` is not under `git`.
func under(path, subject string) bool {
	return path == subject || strings.HasPrefix(path, subject+".")
}
