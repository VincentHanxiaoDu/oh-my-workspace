package hubd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/auth"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
)

// Server is the hub process's whole behaviour, minus the wire.
//
// Every method that answers a question about notes takes an [auth.Secret] and nothing that names a
// person. See the package comment: the token says what may be done, the session says who is doing
// it (PRD §3.10, Issue #103 criterion 6).
type Server struct {
	mu      sync.Mutex
	dir     string
	company string
	store   *hub.Store
	members *hub.Record
	journal *journal
	auth    *auth.Authority
	// revoked is the durable set of ended sessions, replayed at start. A session ended by its
	// person stays ended across a restart of this process — criterion 7's "at the hub, not only
	// locally", which a purely in-memory table cannot promise.
	revoked map[string]string
	// halted is set the first time a durable write fails and is never cleared. See [ErrHubHalted].
	halted error
	// truncated records that the durable record's final line did not finish. It is REPORTED, in
	// [Server.Describe], not swallowed.
	truncated bool
	closed    bool
}

// Options configure a hub process.
type Options struct {
	// Company is what this hub is the hub for (PRD §2.2, one per company). Recorded at creation.
	Company string
	// Now is the clock, for tests that need a deterministic timeline.
	Now func() time.Time
}

// Create initialises a hub directory. It is an EXPLICIT ACT and the only one: no other function in
// this package brings a hub into existence, and [Open] refuses a directory this has not touched.
func Create(dir string, opts Options) error {
	if dir == "" {
		return hub.Refusedf(ErrNoHubDirectory, "no directory was named")
	}
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err == nil {
		return hub.Refusedf(ErrHubDirectoryExists, "%q", dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return hub.Refusedf(ErrHubRecordUnreadable, "%q could not be examined: %v", dir, err)
	}
	if err := os.MkdirAll(dir, ownerOnlyDirMode); err != nil {
		return hub.Refusedf(ErrHubUnwritable, "%q could not be created: %v", dir, err)
	}
	if err := os.Chmod(dir, ownerOnlyDirMode); err != nil {
		return hub.Refusedf(ErrHubUnwritable, "%q could not be made owner-only: %v", dir, err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	body, err := json.MarshalIndent(marker{Format: FormatVersion, Company: opts.Company, Created: now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, markerFile), append(body, '\n'), ownerOnlyFileMode); err != nil {
		return hub.Refusedf(ErrHubUnwritable, "the hub marker could not be written: %v", err)
	}
	return nil
}

// Open starts a hub process against an existing hub directory: it replays the durable record, opens
// it for appending, and refuses if it cannot do either.
//
// IT REFUSES TO START WHEN IT CANNOT WRITE. PRD §4.3 — "the daemon stops when it cannot write
// rather than continuing in a state a person reads as healthy" — read at the moment of starting.
// A hub that came up read-only would serve every search correctly and lose every publication, and
// nothing on the outside would look wrong.
func Open(dir string, opts Options) (*Server, error) {
	if dir == "" {
		return nil, hub.Refusedf(ErrNoHubDirectory, "no directory was named")
	}
	body, err := os.ReadFile(filepath.Join(dir, markerFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, hub.Refusedf(ErrNoHubDirectory, "%q holds no %s", dir, markerFile)
	}
	if err != nil {
		return nil, hub.Refusedf(ErrHubRecordUnreadable, "the hub marker in %q could not be read: %v", dir, err)
	}
	var m marker
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, hub.Refusedf(ErrHubRecordUnreadable, "the hub marker in %q could not be parsed: %v", dir, err)
	}
	if m.Format != FormatVersion {
		return nil, hub.Refusedf(ErrHubFormat, "the hub in %q records format %d; this build reads format %d", dir, m.Format, FormatVersion)
	}

	entries, truncated, err := readJournal(dir)
	if err != nil {
		return nil, hub.Refusedf(ErrHubRecordUnreadable, "%v", err)
	}
	store, members, revoked, err := replay(entries)
	if err != nil {
		return nil, hub.Refusedf(ErrHubRecordUnreadable, "%v", err)
	}
	j, err := openJournal(dir)
	if err != nil {
		return nil, hub.Refusedf(ErrHubUnwritable, "%q could not be opened for appending: %v", filepath.Join(dir, journalFile), err)
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if opts.Now != nil {
		store.SetClock(opts.Now)
	}
	return &Server{
		dir:     dir,
		company: m.Company,
		store:   store,
		members: members,
		journal: j,
		auth:    auth.NewAuthority(auth.AuthorityOptions{Now: now}),
		revoked: revoked,
		// The membership record is replayed too, and people the record knows are the people the
		// authority may issue tokens to. Registration happens through [Server.AddPerson].
		truncated: truncated,
	}, nil
}

// Close stops the process's hold on the directory. A closed server answers nothing.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.journal.close()
}

// Halted reports why this hub stopped answering, or nil if it has not.
func (s *Server) Halted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.halted
}

// live must be called with s.mu held by every operation, before it does anything.
func (s *Server) live() error {
	if s.halted != nil {
		return s.halted
	}
	if s.closed {
		return hub.Refusedf(ErrHubHalted, "this hub has been closed")
	}
	return nil
}

// halt records that a write failed and stops the hub. Called with s.mu held.
func (s *Server) halt(err error) error {
	if s.halted == nil {
		s.halted = hub.Refusedf(ErrHubHalted, "the hub could not write its durable record and stopped: %v", err)
	}
	return s.halted
}

// write appends a durable entry, halting the hub if it cannot. Called with s.mu held.
//
// IT IS CALLED BEFORE THE IN-MEMORY EFFECT IS REPORTED, never after. A publication acknowledged and
// then not written is a note the person believes is published and the hub does not hold — PRD
// §3.11's "never both, never neither", from the hub's side of the transfer.
func (s *Server) write(e entry) error {
	if err := s.journal.append(e); err != nil {
		return s.halt(err)
	}
	return nil
}

// AddPerson records a person the hub knows, and what they may themselves do.
//
// This is an OPERATOR act, not a request from a caller: PRD §3.10's "nothing signs in silently"
// means a person's account is not created by a token arriving. It is durable, so the hub knows the
// same people after a restart.
func (s *Server) AddPerson(p hub.PersonID, holds []hub.Scope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.live(); err != nil {
		return err
	}
	if p == "" {
		return hub.ErrNoAuthor
	}
	if err := s.write(entry{Op: opPerson, Person: string(p)}); err != nil {
		return err
	}
	s.members.AddPerson(p)
	s.auth.Register(p, holds)
	return nil
}

// DefineGroup records a group and its members, durably. PRD §5.3's open question is not answered
// here: the hub owns the record, and nothing in this package reads a company directory.
func (s *Server) DefineGroup(g hub.GroupID, members ...hub.PersonID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.live(); err != nil {
		return err
	}
	ms := make([]string, 0, len(members))
	for _, m := range members {
		ms = append(ms, string(m))
	}
	// SORTED, so that two hubs given the same group in a different argument order write the same
	// record and answer the same way (criterion 8).
	ms = sortedStrings(ms)
	if err := s.write(entry{Op: opGroup, Group: string(g), Members: ms}); err != nil {
		return err
	}
	ps := make([]hub.PersonID, 0, len(ms))
	for _, m := range ms {
		ps = append(ps, hub.PersonID(m))
	}
	s.members.DefineGroup(g, ps...)
	return nil
}

// Authority is the hub's sign-in half, for the operator-side approval step and for #104 to attach a
// wire to. It is the REAL authority code — device codes, token minting, revocation, expiry.
func (s *Server) Authority() *auth.Authority { return s.auth }

// Company is what this hub is the hub for.
func (s *Server) Company() string { return s.company }

// grant is THE ONE DOOR. Every question about notes goes through it, and nothing else in this
// package turns token material into an identity.
//
// Called with s.mu held.
func (s *Server) grant(sec auth.Secret) (hub.Grant, error) {
	if err := s.live(); err != nil {
		return hub.Grant{}, err
	}
	if sec.Empty() {
		// EXPLICIT, AND FIRST. See [ErrNoCredentialPresented] for why this is not left to fall
		// through into "no such token": an empty identity reaching the visibility predicate is the
		// shape of Issue #62.
		return hub.Grant{}, ErrNoCredentialPresented
	}
	g, err := s.auth.Authenticate(sec)
	if err != nil {
		return hub.Grant{}, err
	}
	if g.Holder == "" {
		return hub.Grant{}, ErrUnidentifiedSession
	}
	if _, ended := s.revoked[string(g.ID)]; ended {
		// The durable revocation wins over any live session table. A session its person ended stays
		// ended, including across a restart of this process.
		return hub.Grant{}, auth.ErrTokenRevoked
	}
	return g, nil
}

// Publish stores a note, durably, as the person the token's session names.
//
// The author is NOT a parameter. `hub.PublishThrough` fills it from the grant's holder and refuses a
// mismatch; there is no way to express one here at all.
func (s *Server) Publish(sec auth.Secret, title, body string, v hub.Visibility) (*hub.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return nil, err
	}
	n, err := hub.PublishThrough(s.store, g, hub.Publication{Author: g.Holder, Title: title, Body: body, Visibility: v})
	if err != nil {
		return nil, err
	}
	// DURABLE BEFORE ANSWERED. If this fails the hub halts and the caller is told it halted — never
	// handed a note id for something that is not on the disk.
	if err := s.write(entry{
		Op:         opPublish,
		ID:         string(n.ID),
		Author:     string(n.Author),
		Title:      n.Title,
		Visibility: recordVisibility(n.Visibility),
		Versions:   amendmentsOf(n),
	}); err != nil {
		return nil, err
	}
	return n, nil
}

// Amend adds a version to a note, durably.
func (s *Server) Amend(sec auth.Secret, id hub.NoteID, body string) (*hub.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return nil, err
	}
	if !hub.Permits(g.Scopes, hub.ScopePublish) {
		return nil, hub.Refusedf(hub.ErrPublishScopeRequired, "grant %q carries no %q scope", string(g.ID), string(hub.ScopePublish))
	}
	existing, err := s.store.Read(id, g.Holder)
	if err != nil {
		return nil, err
	}
	if existing.Author != g.Holder {
		return nil, hub.Refusedf(hub.ErrRefused, "only the author may amend a note")
	}
	n, err := s.store.Amend(id, body)
	if err != nil {
		return nil, err
	}
	// The version is recorded WITH ITS TIME, so a replay reproduces the timeline as it stood rather
	// than re-stamping it with the restart's clock. See replay's opAmend branch.
	latest := n.Latest()
	if err := s.write(entry{Op: opAmend, ID: string(id), Versions: []verRecord{{Number: latest.Number, Body: latest.Body, At: latest.At}}}); err != nil {
		return nil, err
	}
	return n, nil
}

// SetVisibility changes who can see a note, durably.
func (s *Server) SetVisibility(sec auth.Secret, id hub.NoteID, v hub.Visibility) (*hub.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return nil, err
	}
	n, err := hub.SetVisibilityThrough(s.store, g, id, v)
	if err != nil {
		return nil, err
	}
	if err := s.write(entry{Op: opVisibility, ID: string(id), By: string(g.Holder), Visibility: recordVisibility(v)}); err != nil {
		return nil, err
	}
	return n, nil
}

// Read returns a note as the token's person may read it.
func (s *Server) Read(sec auth.Secret, id hub.NoteID) (*hub.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return nil, err
	}
	return hub.ReadThrough(s.store, g, id)
}

// Search answers a query, with visibility settled before anything is ranked (PRD §3.5).
//
// THE SCOPES ARE THE THREE AND THERE IS NO FOURTH (criterion 4). This method does not parse a
// scope; `hub.ParseSearchScope` does, and it refuses `team:` and `project:` today. Nothing here
// widens that.
//
// A REFUSAL IS A REFUSAL, NOT AN EMPTY RESULT. #101: a count is an answer, and `0 results` for a
// corpus this hub could not read is a lie. Every failure path here returns an error.
func (s *Server) Search(sec auth.Secret, q hub.Query) (hub.Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return hub.Outcome{}, err
	}
	return hub.SearchThrough(s.store, g, g.Holder, q)
}

// Statistics answers what an agent needs to search well rather than guess (PRD §3.5).
func (s *Server) Statistics(sec auth.Secret, scope hub.SearchScope) (hub.Statistics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return hub.Statistics{}, err
	}
	return hub.StatisticsThrough(s.store, g, g.Holder, scope)
}

// Sessions lists what has been signed in as the token's person (PRD §3.10, criterion 7's first
// half). A person sees their OWN sessions; the identity is the session's, not an argument.
func (s *Server) Sessions(sec auth.Secret) ([]auth.SessionView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return nil, err
	}
	return s.auth.Sessions(g.Holder), nil
}

// Revoke ends a session, at the hub, durably (criterion 7).
//
// The revocation is written BEFORE it is reported, and it is replayed at start-up, so a person who
// ends a session and then sees the hub restart does not find it working again. That is the
// difference between ending a session and forgetting a credential.
func (s *Server) Revoke(sec auth.Secret, id auth.TokenID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.grant(sec)
	if err != nil {
		return err
	}
	if err := s.auth.Revoke(g.Holder, id); err != nil {
		return err
	}
	if err := s.write(entry{Op: opRevoke, Revoked: &revokeEntry{Person: string(g.Holder), Token: string(id)}}); err != nil {
		return err
	}
	s.revoked[string(id)] = string(g.Holder)
	return nil
}

// Description is what this process says about itself, for an operator reading it.
//
// IT IS NOT A HEALTH SUMMARY THAT LEADS WITH "fine". The two facts that can be undetermined — a
// durable record whose last line did not finish, and a halted hub — are stated, in their own words.
type Description struct {
	Directory string
	Company   string
	Notes     int
	// Truncated says the durable record's final line did not finish. It does not mean the corpus is
	// wrong; it means one write did not land, and a person is told rather than not.
	Truncated bool
	Halted    error
}

// Describe answers an operator. It does NOT take a token: it says nothing about any note's content
// and it is the process describing itself.
func (s *Server) Describe() Description {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Description{
		Directory: s.dir,
		Company:   s.company,
		Notes:     s.store.Count(),
		Truncated: s.truncated,
		Halted:    s.halted,
	}
}

// Render writes the description for a person to read, with the §2.4 statement always attached.
func (d Description) Render() string {
	out := fmt.Sprintf("hub directory:   %s\ncompany:         %s\nnotes held:      %d\n", d.Directory, companyOrUnnamed(d.Company), d.Notes)
	if d.Truncated {
		out += "durable record:  the last entry did not finish being written; everything before it is intact\n"
	}
	if d.Halted != nil {
		out += "state:           HALTED — " + d.Halted.Error() + "\n"
	}
	return out + "\n" + OperatorReach + "\n"
}

func companyOrUnnamed(c string) string {
	if c == "" {
		return "(unnamed)"
	}
	return c
}
