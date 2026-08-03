package channels

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// RecordKind is the store kind a channel connection is written under.
const RecordKind = store.Kind("channel")

// connectionFormat is the on-disk envelope version, INSIDE the store's own envelope. A record
// written by a future build is unreadable and says so; it is never listed as a channel with
// nothing in it, because "I cannot read this" and "this has nothing" are different facts (§4.3).
const connectionFormat = 1

// Kind is which of the two built-in places people reach this person.
//
// BOTH ARE BUILT IN (criterion 1). They are constants in this package, [Kinds] enumerates them, and
// nothing has to be installed for either to be connectable. What is NOT built in is a transport —
// see adapter.go, which is explicit about it.
type Kind string

const (
	// KindTeams is Microsoft Teams.
	KindTeams Kind = "teams"
	// KindEmail is the person's mailbox.
	KindEmail Kind = "email"
)

// Kinds is every channel kind this build has, in a stable order. It is a function rather than a
// sentence in a comment so that the CLI's help, the connect command's validation and the tests all
// read the same list — a list maintained in three places is a list that disagrees with itself.
func Kinds() []Kind { return []Kind{KindEmail, KindTeams} }

// Valid reports whether k is a kind this build knows.
func (k Kind) Valid() bool {
	for _, c := range Kinds() {
		if c == k {
			return true
		}
	}
	return false
}

// String renders the kind as a person reads it. The two renderings differ from each other, which is
// the second half of criterion 1: a listing in which Teams and email look the same has not told
// anybody which is which.
func (k Kind) String() string {
	switch k {
	case KindTeams:
		return "Microsoft Teams"
	case KindEmail:
		return "email"
	default:
		return "a channel kind this build does not know (" + string(k) + ")"
	}
}

// Health is the state of a channel's connection, in the four states criterion 13 requires be told
// apart.
//
// THE ZERO VALUE IS UNDETERMINED, following [tri.Value] and for the same reason: a health nobody
// established must not read as "connected and fine".
type Health int

const (
	// HealthUndetermined means the connection's state could not be worked out.
	HealthUndetermined Health = iota
	// HealthConnected means connected, with a credential that has not expired.
	HealthConnected
	// HealthCredentialExpired means the person connected this channel and its credential is no
	// longer usable. Criterion 13: NOT the same state as disconnected, and NOT the same state as
	// connected-and-healthy. A person whose credential expired has a channel; it needs signing in
	// again, which is a thing they do and not a thing done for them.
	HealthCredentialExpired
	// HealthDisconnected means there is no such channel connected. It is a real, determined answer
	// about a channel somebody named — never the rendering of a channel we failed to read.
	HealthDisconnected
)

// String renders the four states, each as its own sentence.
//
// TestHealthRendersItsFourStatesPairwiseDistinctly compares every pair against every other pair
// rather than against string literals: asserting each against its own literal passes just as
// happily after two of them have been edited into the same wording.
func (h Health) String() string {
	switch h {
	case HealthConnected:
		return "connected, and its credential has not expired"
	case HealthCredentialExpired:
		return "connected, and its credential has expired — sign in again to keep ingesting"
	case HealthDisconnected:
		return "not connected"
	default:
		return "its connection state " + tri.Undetermined.String()
	}
}

// Outcome is what happened the last time ingestion tried this channel.
//
// CRITERION 10 IS THIS TYPE. "Reached it, nothing new" and "could not reach it" both produce zero
// tickets and are not the same fact, so they are not the same value and do not render the same.
type Outcome int

const (
	// OutcomeNotAttempted means ingestion has not tried this channel yet.
	OutcomeNotAttempted Outcome = iota
	// OutcomeReached means the channel answered. How much it had is a separate number.
	OutcomeReached
	// OutcomeUnreachable means the attempt failed: a rejected credential, an endpoint that did not
	// answer, a transport this build does not have.
	OutcomeUnreachable
)

// Ingestion is what is known about a channel's last ingestion.
//
// LAST-SUCCESSFUL-INGESTION IS THREE-VALUED (criterion 3). State is Yes with a timestamp, No
// meaning determined never to have ingested, or Undetermined meaning it could not be read. None of
// the three is silence and no two of them render alike.
type Ingestion struct {
	// State is whether there has ever been a successful ingestion.
	State tri.Value
	// At is when the last successful ingestion was, meaningful only when State is Yes.
	At time.Time
	// StateDetail is why the state could not be determined, when it could not.
	StateDetail string

	// Outcome is what the last ATTEMPT did, which is not the same question as whether there has
	// ever been a success: a channel that ingested yesterday and was unreachable a minute ago has a
	// real last-success timestamp AND a failed last attempt, and a person needs both.
	Outcome Outcome
	// OutcomeDetail names the specific finding when the last attempt failed.
	OutcomeDetail string
	// Messages is how many messages the last attempt saw.
	Messages int
	// Tickets is how many tickets the last attempt produced. IT IS A SEPARATE NUMBER FROM Messages
	// AND THAT IS CRITERION 8: if the two were one field nobody could see that ingestion is not a
	// mirror of the traffic.
	Tickets int
}

// Render is the last-successful-ingestion fact as a person reads it, in its three distinguishable
// forms. Compared PAIRWISE by TestLastIngestionRendersThreeWaysPairwiseDistinctly.
func (i Ingestion) Render() string {
	switch i.State {
	case tri.Yes:
		return i.At.UTC().Format(time.RFC3339)
	case tri.No:
		return "never ingested"
	default:
		return "when it last ingested " + tri.Undetermined.String()
	}
}

// RenderAsOf is the last-successful-ingestion fact for a reader who has been told whether ingestion
// is currently running.
//
// CRITERION 6. When the daemon is not running, a bare timestamp is a lie of presentation: it is
// true that ingestion last succeeded then, and false that it is keeping up now. So the currency of
// the fact is attached to the fact, in the same string, rather than left to a banner three lines
// above that a person may not read.
func (i Ingestion) RenderAsOf(running tri.Value) string {
	switch running {
	case tri.Yes:
		return i.Render()
	case tri.No:
		return i.Render() + " (recorded then; NOT CURRENT — ingestion is not running)"
	default:
		return i.Render() + " (recorded then; whether it is current " + tri.Undetermined.String() + " — " +
			"whether ingestion is running " + tri.Undetermined.String() + ")"
	}
}

// RenderOutcome is what the last attempt did, and it is where criterion 10 shows up in the output.
func (i Ingestion) RenderOutcome() string {
	switch i.Outcome {
	case OutcomeReached:
		return fmt.Sprintf("reached; it saw %d message(s) and wrote %d ticket(s)", i.Messages, i.Tickets)
	case OutcomeUnreachable:
		return "COULD NOT BE REACHED: " + i.OutcomeDetail
	default:
		return "not attempted yet"
	}
}

// Connection is one channel a person has connected.
type Connection struct {
	// ID identifies the channel to the person and to the store; one path segment.
	ID string
	// Kind is which of the built-in kinds this is.
	Kind Kind
	// Account is what the person signed in as — a mailbox, a Teams identity. Rendered; it is how a
	// person tells two email channels apart.
	Account string
	// ConnectedAt is when the person connected it.
	ConnectedAt time.Time
	// Credential is the token the person supplied by their own explicit act. IT IS NEVER RENDERED
	// by anything in this package — TestNothingRendersTheCredential holds that.
	Credential string
	// CredentialExpiresAt is when the supplied credential stops working, zero when the person's
	// sign-in artifact did not say. A zero expiry is NOT treated as expired and NOT treated as
	// eternally healthy: see [Connection.Health].
	CredentialExpiresAt time.Time
	// Last is what is known about ingestion on this channel.
	Last Ingestion
}

// Health is the connection's state as of now.
//
// A zero expiry is UNDETERMINED, not healthy. A sign-in artifact that did not say when it stops
// working has not told us the credential is fine; answering "connected and healthy" would be a
// (bool, error) with the error dropped, in a different shape.
func (c Connection) Health(now time.Time) (Health, string) {
	switch {
	case c.CredentialExpiresAt.IsZero():
		return HealthUndetermined, "the credential supplied for this channel does not say when it expires, " +
			"so whether it is still usable could not be determined"
	case !now.Before(c.CredentialExpiresAt):
		return HealthCredentialExpired, "the credential expired at " + c.CredentialExpiresAt.UTC().Format(time.RFC3339)
	default:
		return HealthConnected, "the credential is good until " + c.CredentialExpiresAt.UTC().Format(time.RFC3339)
	}
}

// Render is the whole channel as a person reads it in a listing, given whether ingestion is
// currently running.
//
// IT CONTAINS NO CREDENTIAL. Not truncated, not fingerprinted: absent.
func (c Connection) Render(now time.Time, running tri.Value) string {
	h, why := c.Health(now)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", c.ID)
	fmt.Fprintf(&b, "  kind:                       %s\n", c.Kind)
	fmt.Fprintf(&b, "  account:                    %s\n", c.Account)
	fmt.Fprintf(&b, "  connection:                 %s\n", h)
	fmt.Fprintf(&b, "                              %s\n", why)
	fmt.Fprintf(&b, "  last successful ingestion:  %s\n", c.Last.RenderAsOf(running))
	fmt.Fprintf(&b, "  last attempt:               %s\n", c.Last.RenderOutcome())
	return b.String()
}

// The failures this package returns, as distinct errors.Is-able values, for the reason the store's
// own errors.go gives: a caller that can only read an error's message has to match on strings, and
// the first reworded sentence turns a refusal into a silent success.
var (
	// ErrNoSuchChannel means no channel with that identifier is connected. Determined, not
	// undetermined: the store was read and there is none.
	ErrNoSuchChannel = errors.New("no channel is connected under that identifier")

	// ErrAlreadyConnected means a channel with that identifier is already connected. Connecting
	// does not silently replace one — a person who reuses an identifier by accident would lose the
	// channel they had.
	ErrAlreadyConnected = errors.New("a channel is already connected under that identifier")

	// ErrUnknownKind means the kind named is not one this build has.
	ErrUnknownKind = errors.New("this build has no channel of that kind")

	// ErrInvalidConnection means the connection cannot be stored as it is.
	ErrInvalidConnection = errors.New("this channel cannot be connected as it is")

	// ErrNoCredential means no credential was supplied. Criterion 13: a channel is connected by an
	// explicit act INCLUDING its sign-in, and nothing here obtains a credential on anybody's
	// behalf, so a connect with nothing signed in is refused rather than completed half way.
	ErrNoCredential = errors.New("no credential was supplied, and none will be obtained on your behalf")

	// ErrUnreadableConnection means a channel record is present and cannot be understood. Distinct
	// from ErrNoSuchChannel for the reason the store distinguishes unreadable from absent: a
	// channel list with one damaged record must never report as a list with one fewer channel.
	ErrUnreadableConnection = errors.New("this channel record cannot be read")

	// ErrUnreachable is what an adapter returns when it could not reach its service. Criterion 10
	// rests on this being its own value: an unreachable channel is not an empty one.
	ErrUnreachable = errors.New("this channel could not be reached")
)

// connectionFile is the on-disk shape.
type connectionFile struct {
	Format      int    `json:"format"`
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Account     string `json:"account"`
	ConnectedAt string `json:"connected_at,omitempty"`
	Credential  string `json:"credential,omitempty"`
	ExpiresAt   string `json:"credential_expires_at,omitempty"`

	// LastSuccessAt is absent when there has never been a successful ingestion. ABSENT IS NOT THE
	// EPOCH and it is not undetermined either: a record read in full that has no success recorded
	// has determined that there has been none (criterion 3's middle rendering).
	LastSuccessAt string `json:"last_success_at,omitempty"`
	LastOutcome   string `json:"last_outcome,omitempty"`
	LastDetail    string `json:"last_detail,omitempty"`
	LastMessages  int    `json:"last_messages,omitempty"`
	LastTickets   int    `json:"last_tickets,omitempty"`
}

const (
	outcomeReachedText     = "reached"
	outcomeUnreachableText = "unreachable"
)

func encodeConnection(c Connection) ([]byte, error) {
	f := connectionFile{
		Format:       connectionFormat,
		ID:           c.ID,
		Kind:         string(c.Kind),
		Account:      c.Account,
		Credential:   c.Credential,
		LastMessages: c.Last.Messages,
		LastTickets:  c.Last.Tickets,
		LastDetail:   c.Last.OutcomeDetail,
	}
	if !c.ConnectedAt.IsZero() {
		f.ConnectedAt = c.ConnectedAt.UTC().Format(time.RFC3339Nano)
	}
	if !c.CredentialExpiresAt.IsZero() {
		f.ExpiresAt = c.CredentialExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if c.Last.State == tri.Yes && !c.Last.At.IsZero() {
		f.LastSuccessAt = c.Last.At.UTC().Format(time.RFC3339Nano)
	}
	switch c.Last.Outcome {
	case OutcomeReached:
		f.LastOutcome = outcomeReachedText
	case OutcomeUnreachable:
		f.LastOutcome = outcomeUnreachableText
	}
	return json.Marshal(f)
}

func decodeConnection(id string, body []byte) (Connection, error) {
	var f connectionFile
	if err := json.Unmarshal(body, &f); err != nil {
		return Connection{}, fmt.Errorf("%w: channel %q is damaged: %v", ErrUnreadableConnection, id, err)
	}
	if f.Format != connectionFormat {
		return Connection{}, fmt.Errorf("%w: channel %q is format %d, which this build does not understand",
			ErrUnreadableConnection, id, f.Format)
	}
	c := Connection{ID: f.ID, Kind: Kind(f.Kind), Account: f.Account, Credential: f.Credential}
	if c.ID == "" {
		c.ID = id
	}
	c.ConnectedAt = parseTimeOrZero(f.ConnectedAt)
	c.CredentialExpiresAt = parseTimeOrZero(f.ExpiresAt)

	switch {
	case f.LastSuccessAt == "":
		// DETERMINED NEVER. The record was read in full and holds no success.
		c.Last.State = tri.No
	default:
		when, err := time.Parse(time.RFC3339Nano, f.LastSuccessAt)
		if err != nil {
			// UNDETERMINED, NOT DROPPED AND NOT "NEVER". A timestamp that will not parse is a fact
			// we could not read, and reporting it as "never ingested" would be a failure to read
			// rendered as a determined negative — the exact collapse §4.3 forbids.
			c.Last.State = tri.Undetermined
			c.Last.StateDetail = "the recorded last-ingestion time could not be read: " + err.Error()
		} else {
			c.Last.State, c.Last.At = tri.Yes, when
		}
	}
	switch f.LastOutcome {
	case outcomeReachedText:
		c.Last.Outcome = OutcomeReached
	case outcomeUnreachableText:
		c.Last.Outcome = OutcomeUnreachable
	}
	c.Last.OutcomeDetail = f.LastDetail
	c.Last.Messages, c.Last.Tickets = f.LastMessages, f.LastTickets
	return c, nil
}

func parseTimeOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Validate reports whether the connection can be stored, and why not when it cannot.
func (c Connection) Validate() error {
	if !validID(c.ID) {
		return fmt.Errorf("%w: %q is not usable as a channel identifier — one path segment of "+
			"letters, digits, dash, underscore or dot, not starting with a dot or a dash",
			ErrInvalidConnection, c.ID)
	}
	if !c.Kind.Valid() {
		return fmt.Errorf("%w: %q. This build has: %s", ErrUnknownKind, c.Kind, kindList())
	}
	if strings.TrimSpace(c.Account) == "" {
		return fmt.Errorf("%w: a channel needs the account it is connected as", ErrInvalidConnection)
	}
	if strings.TrimSpace(c.Credential) == "" {
		return ErrNoCredential
	}
	return nil
}

func kindList() string {
	names := make([]string, 0, len(Kinds()))
	for _, k := range Kinds() {
		names = append(names, string(k))
	}
	return strings.Join(names, ", ")
}

// validID mirrors the store's own rule for a single path segment. Restated rather than imported
// because the store does not export it; the store still refuses anything this misses — this exists
// so the refusal names CHANNELS.
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

// Connect records a channel the person has connected.
//
// IT REFUSES TO REPLACE ONE SILENTLY, and it refuses a connection with no credential. Criterion 13:
// the sign-in is part of the person's explicit act, and this function has no way to perform one —
// there is no code path in this package that obtains a credential.
func Connect(s *store.Store, c Connection) error {
	if err := c.Validate(); err != nil {
		return err
	}
	switch _, err := Get(s, c.ID); {
	case err == nil:
		return fmt.Errorf("%w: %q", ErrAlreadyConnected, c.ID)
	case errors.Is(err, ErrNoSuchChannel):
		// The identifier is free. Carry on.
	default:
		return err
	}
	// A NEWLY CONNECTED CHANNEL HAS NEVER INGESTED, and that is a determined answer, not a blank.
	c.Last.State = tri.No
	body, err := encodeConnection(c)
	if err != nil {
		return fmt.Errorf("channel %q could not be encoded: %w", c.ID, err)
	}
	return s.Put(store.Record{Kind: RecordKind, ID: c.ID, Data: body})
}

// Save writes a channel back, replacing what was there. It is how ingestion records what it did.
func Save(s *store.Store, c Connection) error {
	body, err := encodeConnection(c)
	if err != nil {
		return fmt.Errorf("channel %q could not be encoded: %w", c.ID, err)
	}
	return s.Put(store.Record{Kind: RecordKind, ID: c.ID, Data: body})
}

// Get returns one connected channel. A channel that is not connected is [ErrNoSuchChannel]; one
// that is there and cannot be read is [ErrUnreadableConnection] — never an empty Connection.
func Get(s *store.Store, id string) (Connection, error) {
	rec, err := s.Get(RecordKind, id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) || errors.Is(err, store.ErrInvalidName) {
			return Connection{}, fmt.Errorf("%w: %q", ErrNoSuchChannel, id)
		}
		return Connection{}, err
	}
	return decodeConnection(id, rec.Data)
}

// List returns every connected channel, ordered by identifier.
//
// A DAMAGED RECORD FAILS THE CALL. It is never skipped: skipping is how a person with one damaged
// channel record is shown a list that is quietly short and reads as complete.
func List(s *store.Store) ([]Connection, error) {
	recs, err := s.List(RecordKind)
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(recs))
	for _, r := range recs {
		c, derr := decodeConnection(r.ID, r.Data)
		if derr != nil {
			return nil, derr
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Disconnect removes a channel, and REFUSES an identifier that is not connected.
//
// The store's Delete is idempotent by design, which is right for the store and wrong for a person:
// somebody who mistypes an identifier must not be told the channel they meant is disconnected.
func Disconnect(s *store.Store, id string) error {
	if _, err := Get(s, id); err != nil {
		return err
	}
	return s.Delete(RecordKind, id)
}
