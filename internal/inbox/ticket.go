package inbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// Kind is the store kind every ticket is written under. Issues #6 and #7 use this constant rather
// than the string, so that the one place the inbox's records live is a compile-time reference.
const Kind = store.Kind("ticket")

// ticketFormat is the on-disk envelope version for a ticket, INSIDE the store's own envelope. A
// ticket written by a future build is unreadable and says so; it is never listed as an empty
// ticket, because "I cannot read this" and "this has no title" are different facts (§4.3).
const ticketFormat = 1

// Ticket is one thing the person has to act on.
//
// READ THE LIST OF FIELDS FOR WHAT IS NOT HERE. There is no priority, no rank, no severity, no
// score, no order and no "is this small talk" flag, and adding one is a product change and not a
// refactor — see the package comment. There is also no raw message body and no message list: a
// ticket is not a message, and a type that can hold the traffic verbatim is a type somebody will
// eventually fill with the traffic verbatim.
type Ticket struct {
	// ID identifies the ticket to the person and to the store. It is usable as one path segment.
	ID string
	// Title is the written one-line statement of what is being asked. Written — not the subject
	// line of the first email and not the body of the last chat message.
	Title Field
	// Summary is the written statement of what is being asked in full.
	Summary Field
	// Channel names where the obligation reached the person — "email", "teams". It is undetermined
	// when it could not be read, which is a state criterion 12 requires be visible.
	Channel Field
	// Arrived is when this obligation landed. The ZERO VALUE MEANS UNDETERMINED, and is rendered as
	// such.
	//
	// IT IS NOT AN EXPIRY CLOCK. Nothing in this package compares it to the present time; §5.4 is
	// ruled — tickets stay on the machine until the person deletes them. It is here so a person can
	// see how long something has been owed, which is a different thing from the product deciding it
	// has been owed too long to keep showing.
	Arrived time.Time
}

// ArrivedRender is when the ticket arrived, as a person reads it, with the zero time rendered as
// undetermined rather than as the first second of 1970 — which is a real-looking date and is what
// a %v on a zero time.Time would print.
func (t Ticket) ArrivedRender() string {
	if t.Arrived.IsZero() {
		return tri.Undetermined.Render("", "")
	}
	return t.Arrived.UTC().Format(time.RFC3339)
}

// Errors this package returns, as distinct, errors.Is-able values — for the reason the store's own
// errors.go gives: a caller that can only read an error's message has to match on strings, and the
// first reworded sentence turns a refusal into a silent success.
var (
	// ErrNoSuchTicket means the inbox is fine and holds no ticket with that identifier. It is NOT
	// "the inbox could not be read", and the CLI must never render the two the same way.
	//
	// It exists as its own value partly because the store's Delete is deliberately idempotent —
	// deleting a record that is not there is a success down there. Criterion 11 requires the
	// opposite of the INBOX: a person who names a ticket that does not exist has made a mistake and
	// must hear about it. So [Delete] establishes the ticket exists before removing it.
	ErrNoSuchTicket = errors.New("no such ticket in the inbox")

	// ErrNotAnObligation means what was offered is not a thing to act on — see [IsAcknowledgement].
	ErrNotAnObligation = errors.New("this is not a thing to act on, so it is not a ticket")

	// ErrInvalidTicket means the ticket is not storable: no identifier, or one unusable as a single
	// path segment.
	ErrInvalidTicket = errors.New("this ticket cannot be stored as it is")

	// ErrUnreadableTicket means a ticket record is present and cannot be understood. Distinct from
	// ErrNoSuchTicket for the same reason the store distinguishes unreadable from absent: a store
	// with one damaged ticket must never report as a store with one fewer ticket.
	ErrUnreadableTicket = errors.New("this ticket cannot be read")
)

// acknowledgements are the message bodies that are not obligations. PRD §3.2 names `yes`, `ok` and
// `Hii`; the rest are the same act said differently.
//
// This list is short ON PURPOSE. It is not a classifier and must not grow into one — deciding what
// is and is not an obligation from the traffic is Issue #6's whole job, and a half-classifier here
// would be a second opinion in a second place. What this list is for is the failure mode §3.2 names
// outright: a ticket whose title is the verbatim body of a message.
var acknowledgements = map[string]bool{
	"yes": true, "no": true, "y": true, "n": true, "ok": true, "okay": true, "k": true,
	"kk": true, "sure": true, "got it": true, "gotit": true, "noted": true, "ack": true,
	"acknowledged": true, "thanks": true, "thank you": true, "thx": true, "ty": true,
	"cheers": true, "np": true, "no worries": true, "welcome": true, "hi": true, "hii": true,
	"hiii": true, "hey": true, "hello": true, "yo": true, "morning": true, "gm": true,
	"good morning": true, "bye": true, "lol": true, "haha": true, "nice": true, "cool": true,
	"great": true, "perfect": true, "done": true, "same": true, "agreed": true, "+1": true,
}

// IsAcknowledgement reports whether s is an acknowledgement or a piece of small talk rather than
// something to act on.
//
// It normalises first — case folded, surrounding whitespace and punctuation and emoji removed — so
// that "OK!", "ok 👍" and "Ok." are the one answer they are to a person reading them.
//
// A DECISION THE ISSUE DID NOT SETTLE, STATED HERE SO IT CAN BE OVERRULED. Issue #8 requires that
// no listed title is the verbatim body of such a message and that no priority corresponds to one;
// it does not say where that is enforced. It could have been left entirely to Issue #6's ingestion,
// with this package trusting whatever it is handed. It is enforced here as well, at [Put], because
// a rule that lives only in the producer is a rule the next producer — a merge in #7, an import, a
// test fixture — does not have. The cost is that this package now has an opinion about text, and a
// person who genuinely owes somebody a ticket titled exactly "Done" cannot store it under that
// title. That trade is worth naming; it is not obviously right.
func IsAcknowledgement(s string) bool {
	n := strings.TrimFunc(strings.ToLower(strings.TrimSpace(s)), func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r)
	})
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		// An empty or punctuation-only title is not an acknowledgement — it is an empty title, and
		// [Field] already has a rendering that says so. Answering "yes" here would make Put refuse
		// the empty title criterion 1 requires be storable and renderable.
		return false
	}
	return acknowledgements[n]
}

// Validate reports whether the ticket can be stored, and why not when it cannot.
//
// It is exported because Issue #6 will want to ask before it has written anything, and a producer
// that can only find out by attempting the write learns too late.
func (t Ticket) Validate() error {
	if !validID(t.ID) {
		return fmt.Errorf("%w: %q is not usable as a ticket identifier — one path segment of "+
			"letters, digits, dash, underscore or dot, not starting with a dot or a dash", ErrInvalidTicket, t.ID)
	}
	if v, ok := t.Title.Value(); ok && IsAcknowledgement(v) {
		// PRD §3.2, criteria 4 and 5. Not stored at a low priority; not stored.
		return fmt.Errorf("%w: %q is an acknowledgement, not something to act on. "+
			"Acknowledgements are not low-priority tickets — there is no priority to put one at",
			ErrNotAnObligation, v)
	}
	return nil
}

// validID mirrors the store's own rule for a single path segment. It is restated rather than
// imported because the store does not export it; the duplication is deliberate and small, and the
// store still refuses anything this misses — this exists so the refusal names TICKETS.
func validID(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// ticketFile is the on-disk shape. The three text fields are POINTERS so that a key which is not
// there at all decodes to the same thing an explicit null does — absent — while a key holding ""
// decodes to a written empty value. That is criterion 1 at the storage layer.
type ticketFile struct {
	Format  int    `json:"format"`
	ID      string `json:"id"`
	Title   *Field `json:"title"`
	Summary *Field `json:"summary"`
	Channel *Field `json:"channel"`
	// Arrived is RFC3339, or absent when it is not known. Absent is not the epoch.
	Arrived string `json:"arrived,omitempty"`
}

func encode(t Ticket) ([]byte, error) {
	f := ticketFile{Format: ticketFormat, ID: t.ID, Title: &t.Title, Summary: &t.Summary, Channel: &t.Channel}
	if !t.Arrived.IsZero() {
		f.Arrived = t.Arrived.UTC().Format(time.RFC3339Nano)
	}
	return json.Marshal(f)
}

func decode(id string, body []byte) (Ticket, error) {
	var f ticketFile
	if err := json.Unmarshal(body, &f); err != nil {
		return Ticket{}, fmt.Errorf("%w: ticket %q is damaged: %v", ErrUnreadableTicket, id, err)
	}
	if f.Format != ticketFormat {
		return Ticket{}, fmt.Errorf("%w: ticket %q is format %d, which this build does not understand",
			ErrUnreadableTicket, id, f.Format)
	}
	t := Ticket{ID: f.ID}
	if t.ID == "" {
		t.ID = id
	}
	// A nil pointer is a key that was not there. Absent — a determined "this ticket has none" —
	// rather than undetermined, because the record was read in full and it does not have one.
	t.Title, t.Summary, t.Channel = Absent(), Absent(), Absent()
	if f.Title != nil {
		t.Title = *f.Title
	}
	if f.Summary != nil {
		t.Summary = *f.Summary
	}
	if f.Channel != nil {
		t.Channel = *f.Channel
	}
	if f.Arrived != "" {
		when, err := time.Parse(time.RFC3339Nano, f.Arrived)
		if err != nil {
			// UNDETERMINED, NOT DROPPED. A timestamp that cannot be parsed leaves Arrived zero,
			// which renders as undetermined — never as the epoch and never silently as "now".
			return t, nil
		}
		t.Arrived = when
	}
	return t, nil
}
