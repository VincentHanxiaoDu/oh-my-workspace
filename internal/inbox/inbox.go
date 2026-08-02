package inbox

import (
	"errors"
	"fmt"
	"sort"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Operation is one thing that can be done to a ticket.
//
// WHY THIS TYPE EXISTS AT ALL. Issue #8 criterion 6 requires that a driver "enumerates the
// operations available on a ticket and asserts none of them transfers it to a hub". An enumeration
// a test can read has to be a value in the program; a paragraph in a doc comment cannot be
// enumerated, and a list maintained separately in a test asserts about the test. So [Operations] is
// the closed set, and it is what both the CLI's help and the test read.
type Operation struct {
	// Name is what the person types after `omw inbox`.
	Name string
	// Summary is one line.
	Summary string
	// LeavesTheMachine is whether performing this operation transfers the ticket anywhere off this
	// machine. It is false for every operation here and there is no operation for which it is true
	// — §2.3, tickets are never published. It is a field rather than an assumption so the assertion
	// has something to assert on, and so that anybody adding an operation has to answer the
	// question in the same commit.
	LeavesTheMachine bool
}

// Operations is every operation the inbox offers on a ticket. It is the whole set: there is no
// publish, no share, no send, no upload, no sync and no export, under this or any other name.
func Operations() []Operation {
	return []Operation{
		{Name: "list", Summary: "list every ticket in the inbox, with its title and its summary"},
		{Name: "read", Summary: "read one ticket by its identifier"},
		{Name: "delete", Summary: "delete one ticket by its identifier — the only way a ticket leaves the inbox"},
	}
}

// Put writes a ticket, replacing any ticket with the same identifier.
//
// It REFUSES an acknowledgement with [ErrNotAnObligation] rather than storing it somewhere
// harmless. There is nowhere harmless: PRD §3.2 says an acknowledgement is not a low-priority
// ticket, it is not a ticket, and a list with `Hii` at the bottom of it is a list the person has to
// read past — which is the thing they said they did not want to do.
func Put(s *store.Store, t Ticket) error {
	if err := t.Validate(); err != nil {
		return err
	}
	body, err := encode(t)
	if err != nil {
		return fmt.Errorf("ticket %q could not be encoded: %w", t.ID, err)
	}
	return s.Put(store.Record{Kind: Kind, ID: t.ID, Data: body})
}

// Get returns one ticket. A ticket that is not there is [ErrNoSuchTicket]; a ticket that is there
// and cannot be read is [ErrUnreadableTicket] or the store's own error, never an empty Ticket.
func Get(s *store.Store, id string) (Ticket, error) {
	rec, err := s.Get(Kind, id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, store.ErrInvalidName) {
			return Ticket{}, fmt.Errorf("%w: %q", ErrNoSuchTicket, id)
		}
		return Ticket{}, err
	}
	return decode(id, rec.Data)
}

// List returns every ticket, ordered by identifier.
//
// ORDERED BY IDENTIFIER, WHICH IS NOT A RANKING. It is a stable presentation order so that two runs
// of `omw inbox list` show the same thing in the same places. Nothing in this product decides which
// obligation matters more than another, and the day something does, it will be a product decision
// with an Issue behind it and not a sort key that appeared here.
//
// NOTHING IS FILTERED. Not by age, not by state, not by any judgement about what is worth showing.
// A ticket in the store is a ticket in the listing until the person deletes it (§5.4). The absence
// of a clock in this function is the whole of criteria 9 and 10.
//
// A damaged ticket fails the call. It is never skipped: skipping is how an inbox with one damaged
// ticket reports as an inbox with one fewer thing to do, and the person acts on a list that is
// quietly short.
func List(s *store.Store) ([]Ticket, error) {
	recs, err := s.List(Kind)
	if err != nil {
		return nil, err
	}
	out := make([]Ticket, 0, len(recs))
	for _, r := range recs {
		t, derr := decode(r.ID, r.Data)
		if derr != nil {
			return nil, derr
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Delete removes exactly the ticket the person named, and REFUSES an identifier that is not there
// with [ErrNoSuchTicket].
//
// WHY IT READS BEFORE IT DELETES. The store's Delete is idempotent by design — the caller asked for
// the record to be gone and it is gone. That is right for the store and wrong for a person:
// criterion 11 requires that deleting an identifier not in the inbox exits non-zero, and a person
// who mistypes an identifier must not be told the ticket they meant has been deleted. So the
// existence check is here, in the caller that has a person on the other end of it.
//
// The check is not a lock and does not pretend to be: two deletes racing on the same identifier can
// both see it present. Both then remove the same file and the outcome — that ticket is gone, once —
// is the one the person asked for.
func Delete(s *store.Store, id string) error {
	if _, err := Get(s, id); err != nil {
		return err
	}
	return s.Delete(Kind, id)
}
