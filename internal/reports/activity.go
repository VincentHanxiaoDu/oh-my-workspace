package reports

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Item is one thing that happened, under one subject.
//
// It is the SAME type for every subject, and that is the reason granularity can mean the same thing
// everywhere. A per-subject item type would force a per-subject renderer, and `*:summary` would
// stop being expressible.
type Item struct {
	// ID identifies the item — a commit hash, a spend entry's id.
	ID string `json:"id"`
	// Subject is the item's dotted path. It may be narrower than the subject being reported on: a
	// commit is `git.commit` and appears in a report on `git`.
	Subject string `json:"subject"`
	// Kind is what sort of thing it is, within its subject: "commit", "spend", "message".
	Kind string `json:"kind"`
	// Text is the item's own words — a commit message. It appears at `full` and nowhere else.
	Text string `json:"text"`
}

// ErrNoHubConfigured means the subject is one only a hub can supply and no hub is configured.
//
// It is NOT an emptiness and NOT an unreadability: it is a determined fact about this machine,
// established without opening anything (§4.2), and criterion 23 requires it render as itself.
var ErrNoHubConfigured = errors.New("no hub is configured, and this subject is supplied by the hub")

// ErrNoProducer means nothing in this build writes activity for the subject, so its emptiness is
// not a fact about the period — it is a fact about this client (Issue #67, Blocker 1).
//
// IT IS NOT AN EMPTINESS. `omw report run daily` on a machine with a watched project and a commit
// eleven minutes old answered "no activity in this period" and exited 0, because the reader was
// correct and the producer was never built. A subject nobody observes has not had a quiet day.
var ErrNoProducer = errors.New("nothing in this build writes activity for this subject, so whether there was any could not be determined")

// Source is where a report's activity comes from.
//
// AN ERROR MEANS UNDETERMINED. A source that cannot read its data returns an error and the subject
// renders as undetermined — never as an empty list, which is the one return value that would make
// "I could not look" and "there is nothing" the same bytes on screen (§4.3, criterion 18).
type Source interface {
	// Activity returns the items under subject, including everything beneath it in the dotted
	// hierarchy. A subject with nothing to report returns an empty slice and a nil error — that is
	// a determined answer and a successful one.
	Activity(subject string) ([]Item, error)
}

// activityKindPrefix namespaces the store kinds this package reads.
const activityKindPrefix = "activity."

// ActivityKind is the store kind that holds one subject's items. Exported because whatever comes to
// ingest git or model spend writes through it, and a second spelling of this string is how the
// writer and the reader end up looking in two different directories.
func ActivityKind(subject string) store.Kind { return store.Kind(activityKindPrefix + subject) }

// StoreSource reads activity out of the device's local store.
//
// IT OPENS NO CONNECTION AND STARTS NOTHING. The hub-supplied subject is answered from HubConfigured
// — a determined local fact — so the "no hub" answer costs no network at all, which is what lets
// criterion 21 be asserted over the whole flow.
type StoreSource struct {
	Store *store.Store
	// HubConfigured is whether this machine has a hub. False is a determined answer, not a guess.
	HubConfigured bool
}

// Activity implements [Source].
func (s StoreSource) Activity(subject string) ([]Item, error) {
	if sub, ok := LookupSubject(subject); ok && sub.HubOnly && !s.HubConfigured {
		return nil, ErrNoHubConfigured
	}
	if s.Store == nil {
		// A source with no store cannot answer. That is undetermined, not empty.
		return nil, store.ErrNotFound
	}
	kinds, err := s.Store.Kinds()
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, k := range kinds {
		name := string(k)
		if !strings.HasPrefix(name, activityKindPrefix) {
			continue
		}
		path := strings.TrimPrefix(name, activityKindPrefix)
		if !under(path, subject) {
			continue
		}
		recs, err := s.Store.List(k)
		if err != nil {
			// UNDETERMINED FOR THIS SUBJECT ONLY, and it propagates as an error rather than as the
			// records that did read. Half a subject's activity presented as all of it is a report
			// that is wrong without looking wrong.
			return nil, err
		}
		for _, r := range recs {
			var it Item
			if err := json.Unmarshal(r.Data, &it); err != nil {
				return nil, store.ErrUnreadable
			}
			if it.ID == "" {
				it.ID = r.ID
			}
			if it.Subject == "" {
				it.Subject = path
			}
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		// ISSUE #67, BLOCKER 1. Nothing under this subject — and the answer to "was it a quiet
		// period" depends entirely on whether anything in this build would have written to it.
		//
		// WITH A PRODUCER, an empty result is a determined, successful answer: something looked and
		// there was nothing. WITHOUT ONE, the emptiness is a fact about this client and not about
		// the period, and reporting it as a quiet day is a confident negative derived from never
		// having looked. That is the exact shape §4.3 forbids, and it shipped: `omw report run
		// daily` said "no activity in this period" for `git` on a machine with a watched project
		// and a commit eleven minutes old, and exited 0.
		//
		// RECORDS PRESENT STILL WIN, which is why this is checked here and not before the read. If
		// something did write activity — an ingester that lands tomorrow, an import — the report is
		// about what is there, not about what the catalog believes.
		if sub, ok := LookupSubject(subject); ok && sub.Producer == "" {
			return nil, ErrNoProducer
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// WriteActivity stores one item. It is here rather than in a test helper because a subject with no
// writer is a subject that can only ever report nothing, and because the reader and the writer must
// agree on [ActivityKind] by construction.
func WriteActivity(s *store.Store, it Item) error {
	if it.Subject == "" {
		return errors.New("an activity item must name its subject")
	}
	return s.PutJSON(ActivityKind(it.Subject), it.ID, it)
}
