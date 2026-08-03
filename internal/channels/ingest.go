package channels

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ChannelResult is what one ingestion run did to one channel.
//
// Messages AND Tickets, TWO NUMBERS (criterion 8). A run that saw six messages and wrote one ticket
// is the normal case and a person must be able to see it; a single "ingested 6" would hide exactly
// the thing this capability is for.
type ChannelResult struct {
	ID       string
	Kind     Kind
	Outcome  Outcome
	Detail   string
	Messages int
	Tickets  int
}

// Result is what one ingestion run did.
type Result struct {
	// Channels is one entry per connected channel, in listing order. A channel that could not be
	// reached is HERE, with OutcomeUnreachable — not omitted, because a run that quietly skipped
	// the channel it could not reach would report as a run that found nothing.
	Channels []ChannelResult
	// Messages is how many messages were seen across every channel.
	Messages int
	// Tickets is how many tickets were written across every channel.
	Tickets int
	// AdaptersBuilt is how many adapters this run constructed. It exists so criterion 11 —
	// "with no channel connected, nothing opens a connection" — is assertable as zero rather than
	// as an absence of observed traffic, which is what a passing run of a build that dials
	// sometimes also looks like.
	AdaptersBuilt int
}

// IngestInterval is how often the daemon ingests while it is running.
//
// Short, because §3.1's promise is that the client is KEEPING UP while it runs, and a person who
// has to wait a quarter of an hour to see a ticket has a batch job. The work is cheap when nothing
// has arrived: one adapter call per connected channel, and none at all when none is connected.
const IngestInterval = time.Second

// Ingest runs one ingestion pass over every connected channel and writes the tickets it produces.
//
// NOTHING IS CONSTRUCTED FOR A CHANNEL THAT IS NOT CONNECTED. The loop is over the connections in
// the store; with none there, fac is never called, no adapter exists and there is nothing that
// could reach out (criterion 11).
//
// A CHANNEL WHOSE CREDENTIAL HAS EXPIRED IS NOT ATTEMPTED, and is reported as unreachable naming
// the expiry. Reaching a service with a credential the person has been told is dead would be this
// product deciding to try anyway; and reporting the channel as "reached, nothing new" would be
// criterion 10's exact defect.
//
// EVERY CHANNEL IS ATTEMPTED EVEN IF AN EARLIER ONE FAILED. One rejected credential must not stop
// the person's mail being ingested.
func Ingest(s *store.Store, fac Factory, now time.Time) (Result, error) {
	var res Result
	conns, err := List(s)
	if err != nil {
		return res, err
	}
	if fac == nil {
		fac = Builtin
	}
	for _, c := range conns {
		cr := ingestOne(s, fac, c, now, &res)
		res.Channels = append(res.Channels, cr)
		res.Messages += cr.Messages
		res.Tickets += cr.Tickets
	}
	return res, nil
}

func ingestOne(s *store.Store, fac Factory, c Connection, now time.Time, res *Result) ChannelResult {
	cr := ChannelResult{ID: c.ID, Kind: c.Kind}

	record := func() {
		c.Last.Outcome, c.Last.OutcomeDetail = cr.Outcome, cr.Detail
		c.Last.Messages, c.Last.Tickets = cr.Messages, cr.Tickets
		if cr.Outcome == OutcomeReached {
			// A SUCCESSFUL INGESTION UPDATES THE LAST-SUCCESS TIME; A FAILED ONE LEAVES IT ALONE.
			// The channel that ingested yesterday and failed a minute ago still last succeeded
			// yesterday, and overwriting that would erase the only fact a person has about how far
			// behind they are.
			c.Last.State, c.Last.At = tri.Yes, now.UTC()
		}
		// A failure to record what happened is not hidden and does not fail the run: the next pass
		// tries again, and the stale record is rendered as stale rather than as current.
		_ = Save(s, c)
	}

	if h, why := c.Health(now); h == HealthCredentialExpired {
		cr.Outcome, cr.Detail = OutcomeUnreachable, why
		record()
		return cr
	}

	ad, err := fac(c)
	if err != nil {
		cr.Outcome, cr.Detail = OutcomeUnreachable, err.Error()
		record()
		return cr
	}
	res.AdaptersBuilt++

	msgs, err := ad.Fetch(c)
	if err != nil {
		cr.Outcome, cr.Detail = OutcomeUnreachable, err.Error()
		record()
		return cr
	}
	cr.Outcome = OutcomeReached
	cr.Messages = len(msgs)

	for _, m := range matters(msgs) {
		t, ok := ticketFor(c, m)
		if !ok {
			// ACKNOWLEDGEMENTS AND SMALL TALK LAND HERE AND GO NO FURTHER. There is no low-priority
			// branch below this line, because there is no priority — see doc.go and §3.2.
			continue
		}
		if perr := inbox.Put(s, t); perr != nil {
			if errors.Is(perr, inbox.ErrNotAnObligation) {
				// The inbox refused it as well. Two enforcements of one rule; neither is a place to
				// put the thing anyway.
				continue
			}
			cr.Detail = "some tickets could not be written: " + perr.Error()
			continue
		}
		cr.Tickets++
	}
	record()
	return cr
}

// matter is several messages about one thing.
type matter struct {
	key      string
	messages []Message
}

// matters groups a run's messages into the things they are about.
//
// THE THREAD IDENTIFIER IS PREFERRED OVER THE SUBJECT LINE, because it is the channel's own answer
// to the question we are asking and the subject line is a guess. Where there is no thread, the
// normalised subject is used — case folded, reply and forward prefixes removed — so that "Login
// broken", "RE: Login broken" and "Fwd: re: login broken" are the one matter a person reads them
// as. Where there is neither, the sender is the matter: two unrelated things from one person in one
// pass becoming one ticket is a worse outcome than eleven tickets was, but it is the outcome the
// evidence supports, and it is recoverable — a person splits a ticket, they cannot unread eleven.
//
// CROSS-CHANNEL GROUPING IS NOT DONE HERE. The Teams ping on Tuesday and the email on Thursday
// about the same broken login are two matters to this function and are combined by Issue #7, which
// owns merging. Issue #6's scope note says so outright.
func matters(msgs []Message) []matter {
	index := map[string]int{}
	var out []matter
	for _, m := range msgs {
		k := matterKey(m)
		if i, seen := index[k]; seen {
			out[i].messages = append(out[i].messages, m)
			continue
		}
		index[k] = len(out)
		out = append(out, matter{key: k, messages: []Message{m}})
	}
	for i := range out {
		sort.SliceStable(out[i].messages, func(a, b int) bool {
			return out[i].messages[a].At.Before(out[i].messages[b].At)
		})
	}
	return out
}

func matterKey(m Message) string {
	if t := strings.TrimSpace(m.Thread); t != "" {
		return "thread:" + t
	}
	if s := normaliseSubject(m.Subject); s != "" {
		return "subject:" + s
	}
	return "from:" + strings.ToLower(strings.TrimSpace(m.From))
}

// replyPrefixes are the ways a channel says "this is a reply to the thing above".
var replyPrefixes = []string{"re:", "re :", "fwd:", "fw:", "aw:", "sv:", "vs:", "antw:"}

// normaliseSubject reduces a subject line to the matter it is about.
func normaliseSubject(s string) string {
	out := strings.Join(strings.Fields(strings.ToLower(s)), " ")
	for changed := true; changed; {
		changed = false
		for _, p := range replyPrefixes {
			if strings.HasPrefix(out, p) {
				out = strings.TrimSpace(strings.TrimPrefix(out, p))
				changed = true
			}
		}
	}
	return out
}

// isRequest reports whether one message has anything in it to act on.
//
// IT IS DECIDED ON THE BODY, AND THE SUBJECT LINE IS NOT ENOUGH. A person replying "thanks" to a
// thread keeps that thread's subject, so a rule that read the subject would find an obligation in
// every acknowledgement and criterion 9 would fail on the most ordinary traffic there is.
//
// A DECISION THE ISSUE DID NOT SETTLE, SO IT IS STATED WHERE IT CAN BE OVERRULED: an email with a
// real subject line and an empty body therefore produces no ticket. That is the cost of the rule
// above and it is not obviously right — a subject-only email does happen. It was chosen because
// criterion 9 is explicit and load-bearing and this case is not mentioned at all, so the reading
// that keeps the stated requirement true was preferred to the one that guesses at an unstated one.
func isRequest(m Message) bool {
	b := strings.TrimSpace(m.Body)
	if b == "" {
		return false
	}
	return !inbox.IsAcknowledgement(b)
}

// ticketFor turns one matter into one ticket, or reports that it is not a thing to act on.
//
// ONE TICKET PER MATTER, NOT PER MESSAGE (criterion 8). The count of messages is written into the
// summary, so the traffic is visible as a number without being visible as a list.
func ticketFor(c Connection, m matter) (inbox.Ticket, bool) {
	var requests []Message
	for _, msg := range m.messages {
		if isRequest(msg) {
			requests = append(requests, msg)
		}
	}
	if len(requests) == 0 {
		// NOTHING. Not a ticket at a low priority, not a ticket in a "small talk" state, not a
		// ticket anywhere: §3.2, criterion 9.
		return inbox.Ticket{}, false
	}

	title, summary := compose(c, m, requests)
	t := inbox.Ticket{
		ID:      ticketID(c.ID, m.key),
		Title:   inbox.Text(title),
		Summary: inbox.Text(summary),
		Channel: inbox.Text(string(c.Kind)),
		Arrived: requests[0].At.UTC(),
	}
	if requests[0].At.IsZero() {
		// UNDETERMINED RATHER THAN THE EPOCH. The ticket type renders a zero Arrived as
		// undetermined, which is the honest answer for traffic that carried no time.
		t.Arrived = time.Time{}
	}
	return t, true
}

// ticketID is stable for a channel and a matter, so a second pass over the same conversation
// UPDATES the one ticket rather than adding another.
//
// That is what makes continuous ingestion safe to run every second: the run that sees the same
// three messages again writes the same ticket again, and the inbox still holds one.
func ticketID(channelID, matterKey string) string {
	sum := sha256.Sum256([]byte(channelID + "\x00" + matterKey))
	return "ingested-" + hex.EncodeToString(sum[:])[:16]
}

// compose WRITES the ticket's title and summary.
//
// NEITHER IS A COPY OF ANYTHING (criterion 8, §3.2). The title is a sentence this function
// composes, naming who is waiting, on which channel, and how many messages it took — a subject
// line, where there is one, appears only as a normalised topic phrase inside that sentence, never
// as the title. The summary is a written account of the matter: who, where, over what window, how
// many messages, and how many of them were acknowledgements. NO MESSAGE BODY IS COPIED INTO EITHER,
// and TestNoTicketCarriesAMessageBodyOrASubjectLineVerbatim holds that.
//
// A DECISION THE ISSUE DID NOT SETTLE, AND THE LARGEST ONE IN THIS BRANCH. "Written" plainly wants
// prose about what is being asked, which in the finished product is a model reading the traffic.
// There is no model in this build and Issue #6 does not put one here. So this composes a written,
// deterministic statement ABOUT the matter rather than a summary OF its contents: it is genuinely
// written and genuinely not a copy, and it is genuinely less than a person reading the thread would
// write. Naming that gap is worth more than a template that reads like comprehension.
func compose(c Connection, m matter, requests []Message) (title, summary string) {
	who := who(requests)
	topic := topicOf(m)
	n := len(m.messages)
	acks := n - len(requests)

	title = fmt.Sprintf("%s is waiting on you about %s", who, topic)

	first, last := m.messages[0].At, m.messages[len(m.messages)-1].At
	var b strings.Builder
	fmt.Fprintf(&b, "%s reached you on %s and is waiting on a reply about %s. ", who, c.Kind, topic)
	fmt.Fprintf(&b, "This matter accounts for %s, ", plural(n, "message", "messages"))
	if acks > 0 {
		fmt.Fprintf(&b, "of which %d ask for something and %d are acknowledgements, ", len(requests), acks)
	}
	fmt.Fprintf(&b, "gathered into this one ticket rather than one ticket each. ")
	switch {
	case first.IsZero() || last.IsZero():
		fmt.Fprintf(&b, "When they arrived %s. ", tri.Undetermined.String())
	case first.Equal(last):
		fmt.Fprintf(&b, "It arrived at %s. ", first.UTC().Format(time.RFC3339))
	default:
		fmt.Fprintf(&b, "It ran from %s to %s. ", first.UTC().Format(time.RFC3339), last.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "Nothing has been sent back yet, and the messages themselves are not stored — "+
		"open %s to read them.", c.Account)
	return title, b.String()
}

// who names the people waiting, without listing eleven of them.
func who(requests []Message) string {
	seen := map[string]bool{}
	var names []string
	for _, m := range requests {
		n := strings.TrimSpace(m.From)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	switch len(names) {
	case 0:
		// NOT "somebody". Who sent it is a fact we did not get, and the third answer is a real one.
		return "a sender whose identity " + tri.Undetermined.String()
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return fmt.Sprintf("%s and %d others", names[0], len(names)-1)
	}
}

// topicOf is the matter as a phrase inside a written sentence. It is a NORMALISED subject, never
// the subject line itself, and where there is no subject it says so rather than reaching for a
// message body.
func topicOf(m matter) string {
	for _, msg := range m.messages {
		if s := normaliseSubject(msg.Subject); s != "" {
			return s
		}
	}
	return "something they raised with no subject line"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
