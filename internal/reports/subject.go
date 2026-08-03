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
	// Producer names what writes this subject's activity. EMPTY MEANS NOTHING IN THIS BUILD DOES,
	// and that is the difference between a quiet day and a subject nobody has ever observed
	// (Issue #67, Blocker 1).
	//
	// It is a name and not a bool so that the catalog says WHERE the activity would come from,
	// which is the thing a person reading `omw report subjects` needs and the thing a reviewer
	// needs in order to check the claim. A subject whose producer is named and does not exist is a
	// worse lie than one that admits it has none, so the name is the reviewable part.
	Producer string
	// About is one line, shown by `omw report subjects`.
	About string
}

// catalog is every subject this build knows.
//
// EVERY Producer IS EMPTY, AND THAT IS THE BUILD AND NOT AN OVERSIGHT (Issue #67, Blocker 1).
// [WriteActivity] has no caller outside tests: no ingestion of git, of model spend or of channel
// traffic exists yet. Writing a plausible producer name here would restore exactly the defect this
// field was added to end — a proxy that reads true and measures nothing. When an ingester lands it
// names itself here, in the same change that makes the claim true.
var catalog = []Subject{
	{Name: "git", Root: true, About: "work in this person's repositories"},
	{Name: "git.commit", About: "commits specifically — the narrower path inside git"},
	{Name: "token_usage", Root: true, About: "model spend"},
	{Name: "channel", Root: true, About: "channel traffic (§3.1)"},
	// The hub IS this subject's producer, which is why it has one while the local subjects do not:
	// with a hub configured there is something on the other end that writes these, and with none
	// configured the answer is StateNoHub rather than either an emptiness or an undetermined.
	{Name: "published_notes", Root: true, HubOnly: true, Producer: "the hub", About: "notes published to the hub (§3.11) — the hub supplies this one"},
}

// noProducerNote is what `omw report subjects` says beside a subject nothing writes. It is a
// constant because criterion 2 is an assertion about the advertisement, and an advertisement that
// each surface words differently is one a person cannot rely on.
const noProducerNote = "nothing in this build writes it, so it reports as undetermined"

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

// Advertisement is the one line `omw report subjects` prints about a subject, marks included.
//
// IT IS HERE AND NOT IN THE COMMAND so that a subject cannot be advertised without whatever
// qualifications it carries: the command formats the line, this decides what it must say.
func (s Subject) Advertisement() []string {
	var marks []string
	if s.Root {
		marks = append(marks, "named by *")
	}
	if s.HubOnly {
		marks = append(marks, "supplied by the hub")
	}
	if s.Producer != "" {
		if !s.HubOnly {
			marks = append(marks, "written by "+s.Producer)
		}
	} else {
		// SAID, NOT OMITTED (criterion 2). An advertised subject that can only ever report nothing
		// is worse than an absent one, and the person finding that out from a report that looks
		// like a quiet day is how this Issue was filed.
		marks = append(marks, noProducerNote)
	}
	return marks
}

// under reports whether path is the subject itself or lies beneath it in the dotted hierarchy.
//
// This is what makes `git:full` cover commits while `git.commit:event` covers only commits: the
// activity a subject reports is its own plus everything under it. It is a segment-wise test, so
// `gitlab` is not under `git`.
func under(path, subject string) bool {
	return path == subject || strings.HasPrefix(path, subject+".")
}
