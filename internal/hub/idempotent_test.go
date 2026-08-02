package hub

import (
	"strconv"
	"sync"
	"testing"
)

// Issue #10 criterion 5, the hub half: "a person retries and does not get two copies".

func publisherGrant(holder PersonID) Grant {
	return Grant{ID: "g", Holder: holder, Scopes: []Scope{ScopeRead, ScopePublish}}
}

func TestTheSameAttemptTwiceMakesOneNote(t *testing.T) {
	s, o := NewStore(nil), NewOnce()
	g := publisherGrant("ada")
	p := Publication{Author: "ada", Title: "t", Body: "b"}

	first, fresh, err := PublishOnce(s, o, g, "attempt-1", p)
	if err != nil || !fresh {
		t.Fatalf("first publication: fresh=%v err=%v", fresh, err)
	}
	second, fresh, err := PublishOnce(s, o, g, "attempt-1", p)
	if err != nil {
		t.Fatalf("the retry was refused: %v", err)
	}
	if fresh {
		t.Error("the retry reports itself as the call that created the note")
	}
	if second.ID != first.ID {
		t.Errorf("the retry produced note %q and the first produced %q", second.ID, first.ID)
	}
	if s.Count() != 1 {
		t.Fatalf("the hub holds %d notes after one attempt retried; want 1", s.Count())
	}
}

// A DIFFERENT ATTEMPT WITH IDENTICAL CONTENT IS A DIFFERENT NOTE. This is the direction content
// hashing gets wrong: a person who publishes the same one-line note twice on purpose meant it.
func TestTwoAttemptsWithIdenticalContentMakeTwoNotes(t *testing.T) {
	s, o := NewStore(nil), NewOnce()
	g := publisherGrant("ada")
	p := Publication{Author: "ada", Title: "t", Body: "b"}

	a, _, err := PublishOnce(s, o, g, "attempt-1", p)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := PublishOnce(s, o, g, "attempt-2", p)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("two deliberate publications of the same text were collapsed into one note")
	}
	if s.Count() != 2 {
		t.Fatalf("the hub holds %d notes; want 2", s.Count())
	}
}

// A refusal is NOT remembered: the person fixes the reason and retries with the same key.
func TestARefusedAttemptIsNotRememberedAndTheSameKeyCanSucceed(t *testing.T) {
	s, o := NewStore(nil), NewOnce()
	readOnly := Grant{ID: "g", Holder: "ada", Scopes: []Scope{ScopeRead}}
	p := Publication{Author: "ada", Title: "t", Body: "b"}

	if _, _, err := PublishOnce(s, o, readOnly, "attempt-1", p); Code(err) != ErrPublishScopeRequired.Code {
		t.Fatalf("code = %q, want %q", Code(err), ErrPublishScopeRequired.Code)
	}
	if s.Count() != 0 {
		t.Fatalf("a refused publication stored something")
	}
	if _, ok := o.Seen("attempt-1"); ok {
		t.Fatal("a refused attempt was recorded, so fixing the reason and retrying would be blocked")
	}
	n, fresh, err := PublishOnce(s, o, publisherGrant("ada"), "attempt-1", p)
	if err != nil || !fresh {
		t.Fatalf("the fixed retry: fresh=%v err=%v", fresh, err)
	}
	if s.Count() != 1 {
		t.Fatalf("the hub holds %d notes; want 1 (%q)", s.Count(), n.ID)
	}
}

// A publication with no attempt named is REFUSED. Publishing anyway would be the one branch with
// no protection against a double copy, taken by exactly the callers that forgot about retries.
func TestAPublicationWithNoAttemptKeyIsRefused(t *testing.T) {
	s, o := NewStore(nil), NewOnce()
	if _, _, err := PublishOnce(s, o, publisherGrant("ada"), "", Publication{Author: "ada", Body: "b"}); err == nil {
		t.Fatal("a publication with no attempt key succeeded")
	} else if Code(err) != ErrNoIdempotencyKey.Code {
		t.Fatalf("code = %q, want %q", Code(err), ErrNoIdempotencyKey.Code)
	}
	if s.Count() != 0 {
		t.Fatalf("the hub stored a publication that named no attempt")
	}
}

// TWO RETRIES ARRIVING AT ONCE. The check and the publication are one act under one lock; doing
// them under separate locks is how both find the key unseen and both publish.
func TestConcurrentRetriesOfOneAttemptMakeOneNote(t *testing.T) {
	s, o := NewStore(nil), NewOnce()
	g := publisherGrant("ada")
	const n = 32
	var wg sync.WaitGroup
	ids := make([]NoteID, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			note, _, err := PublishOnce(s, o, g, "attempt-1", Publication{Author: "ada", Title: "t" + strconv.Itoa(i), Body: "b"})
			if err == nil {
				ids[i] = note.ID
			}
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d was refused: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("call %d got %q and call 0 got %q", i, ids[i], ids[0])
		}
	}
	if got := s.Count(); got != 1 {
		t.Fatalf("%d concurrent retries of one attempt made %d notes; want 1", n, got)
	}
}

// A grant with no publish scope cannot publish through this path either. The idempotency record
// must not become a way around the scope check.
func TestTheAttemptRecordIsNotAWayAroundTheScopeCheck(t *testing.T) {
	s, o := NewStore(nil), NewOnce()
	p := Publication{Author: "ada", Title: "t", Body: "b"}
	if _, _, err := PublishOnce(s, o, publisherGrant("ada"), "attempt-1", p); err != nil {
		t.Fatal(err)
	}
	// Somebody else's grant, replaying the key.
	other := Grant{ID: "g2", Holder: "grace", Scopes: []Scope{ScopeRead, ScopePublish}}
	_, _, err := PublishOnce(s, o, other, "attempt-1", p)
	if err == nil {
		t.Fatal("replaying another person's attempt key returned their note")
	}
	if Code(err) != ErrUndetermined.Code && Code(err) != ErrRefused.Code {
		t.Fatalf("code = %q; a replay by a different holder must not read as a clean success", Code(err))
	}
	if s.Count() != 1 {
		t.Fatalf("the hub holds %d notes", s.Count())
	}
}
