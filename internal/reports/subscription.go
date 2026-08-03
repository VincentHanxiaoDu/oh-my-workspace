package reports

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// SubscriptionKind is the store kind a subscription is filed under.
const SubscriptionKind = store.Kind("subscription")

// Subscription is a standing instruction: a name, and the selectors it was written with.
type Subscription struct {
	Name string `json:"name"`
	// Selectors are stored in their canonical written form, one string each. They are stored as
	// text and re-parsed on read so that what comes back is what was written — including the dotted
	// path in `git.commit:event`, which a stored "subject + granularity" pair would have had to
	// reconstruct and could have reconstructed as `git`.
	Selectors []string `json:"selectors"`
}

// ErrNoSuchSubscription means the store holds no subscription by that name. Distinct from a
// subscription with no selectors, which cannot exist because ParseSelectors refuses one.
var ErrNoSuchSubscription = errors.New("no such subscription")

// ErrInvalidSubscriptionName means the name is not usable. Names are the same shape as subject
// paths minus the dots, because a name becomes a filename in the store.
var ErrInvalidSubscriptionName = errors.New("not a usable subscription name")

func validSubscriptionName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: it is empty", ErrInvalidSubscriptionName)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return fmt.Errorf("%w: %q is lower-case letters, digits, '_' and '-'", ErrInvalidSubscriptionName, name)
		}
	}
	return nil
}

// Save parses the whole selector list and only then writes. ALL OR NOTHING (criterion 24).
//
// The parse is complete before the store is touched, so a list with one bad selector in it leaves
// the store exactly as it was — there is no ordering of these two statements under which half a
// list lands on disk. The write itself is [store.Store.PutJSON], which is atomic, so the other half
// of "no half-work" is the store's invariant 4 rather than a promise made again here.
func Save(s *store.Store, name, list string) (Subscription, error) {
	if err := validSubscriptionName(name); err != nil {
		return Subscription{}, err
	}
	sels, err := ParseSelectors(list)
	if err != nil {
		return Subscription{}, err
	}
	sub := Subscription{Name: name, Selectors: canonical(sels)}
	if err := s.PutJSON(SubscriptionKind, name, sub); err != nil {
		return Subscription{}, err
	}
	return sub, nil
}

func canonical(sels []Selector) []string {
	out := make([]string, 0, len(sels))
	for _, s := range sels {
		out = append(out, s.String())
	}
	return out
}

// Load reads a subscription back.
//
// It RE-PARSES what it read rather than trusting it. A selector list on disk that this build cannot
// read is an unreadable subscription and says so; silently dropping the selectors it does not
// understand would produce a report that is missing a subject with nothing on screen to say so.
func Load(s *store.Store, name string) (Subscription, []Selector, error) {
	var sub Subscription
	if err := s.GetJSON(SubscriptionKind, name, &sub); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			return Subscription{}, nil, fmt.Errorf("%w: %q", ErrNoSuchSubscription, name)
		}
		return Subscription{}, nil, err
	}
	sels, err := ParseSelectors(strings.Join(sub.Selectors, ", "))
	if err != nil {
		return sub, nil, err
	}
	return sub, sels, nil
}

// List returns every stored subscription, by name.
func List(s *store.Store) ([]Subscription, error) {
	recs, err := s.List(SubscriptionKind)
	if err != nil {
		return nil, err
	}
	out := make([]Subscription, 0, len(recs))
	for _, r := range recs {
		var sub Subscription
		if err := json.Unmarshal(r.Data, &sub); err != nil {
			return nil, store.ErrUnreadable
		}
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
