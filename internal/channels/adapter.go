package channels

import (
	"fmt"
	"time"
)

// Message is one thing that arrived on a channel.
//
// IT IS NEVER STORED. A Message lives for the duration of one ingestion run and is then gone; what
// persists is a ticket with written text about the matter, and a count. §3.2 and §2.3: this product
// is not a second copy of the person's inbox, and a type whose bodies get written to disk is how it
// becomes one.
type Message struct {
	// ID is the channel's own identifier for the message.
	ID string
	// From is who sent it, as the channel names them.
	From string
	// Subject is the subject line, empty on channels that have none.
	Subject string
	// Body is what was said. It is read to decide whether there is anything to act on, and is
	// never copied into a ticket.
	Body string
	// At is when it arrived.
	At time.Time
	// Thread is the channel's own conversation identifier when it has one. It is the strongest
	// evidence that several messages are about one matter, and is preferred over the subject line.
	Thread string
}

// Adapter is how ingestion reaches one channel.
//
// THIS IS THE SEAM, AND IT IS THE ONLY WAY INTO THIS PACKAGE FROM OUTSIDE. Everything downstream of
// Fetch — grouping, refusing acknowledgements, writing tickets — is real code exercised by the
// tests. What is on the other side of Fetch in this build is stated plainly in [Builtin] below.
type Adapter interface {
	// Fetch returns what has arrived on the channel. An error wrapping [ErrUnreachable] means the
	// channel could not be reached, which criterion 10 requires be told apart from an empty return.
	Fetch(c Connection) ([]Message, error)
}

// Factory produces the adapter for a connection.
//
// IT IS A PARAMETER OF [Ingest] AND NOT A PACKAGE-LEVEL VARIABLE. A global would make criterion
// 11's assertion — that with no channel connected nothing is even constructed — a statement about
// whichever test ran last.
type Factory func(c Connection) (Adapter, error)

// AdapterFunc adapts a function to [Adapter].
type AdapterFunc func(c Connection) ([]Message, error)

// Fetch implements [Adapter].
func (f AdapterFunc) Fetch(c Connection) ([]Message, error) { return f(c) }

// Builtin is the factory for the channel kinds this build ships, and it is honest about what it
// has.
//
// READ THIS BEFORE YOU BELIEVE THIS PRODUCT TALKS TO TEAMS. Teams and email are built in as CHANNEL
// KINDS: a person can connect either without installing anything, both are enumerated by [Kinds],
// both are stored, listed, health-checked and ingested through the same code, and neither needs a
// hub. What this build does NOT have is a transport for either — no Graph client, no IMAP client,
// no HTTP at all. So the built-in adapter for both kinds reaches nothing and reports [ErrUnreachable]
// naming precisely what is missing.
//
// THAT IS A REAL STATE AND NOT A STUB PRETENDING TO WORK. It renders exactly as criterion 10
// requires an unreachable channel to render: distinguishably from a channel that was reached and
// found nothing, with the reason said out loud. It produces zero tickets, and it never renders as
// "nothing arrived". A person running this build is told the truth by the product itself.
//
// The transports belong to Issue #21, which owns the one extension mechanism Teams and email are
// instances of. When they land they replace this function and nothing else in this package changes.
func Builtin(c Connection) (Adapter, error) {
	if !c.Kind.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, c.Kind)
	}
	return AdapterFunc(func(c Connection) ([]Message, error) {
		return nil, fmt.Errorf("%w: this build has no transport for %s — the %s channel kind is "+
			"built in and connectable, and the client that speaks to the service is not in this "+
			"build (Issue #21)", ErrUnreachable, c.Kind, c.Kind)
	}), nil
}
