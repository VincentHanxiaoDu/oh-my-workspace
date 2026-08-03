package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These drive store/batch.go: a set of writes that is either all applied or none. Issue #7's
// criterion 10 rests on it, and the subprocess-kill drive of that criterion lives in
// internal/inbox/mergecrash_test.go — this file is the unit-level half, including the one state a
// killed process leaves behind that no in-process test can produce by itself: a committed journal
// that was never applied. That state is STAGED here by writing the journal directly, which is
// exactly what the child process leaves on disk.

func batchStore(t *testing.T) *Store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := Create(root)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s
}

func TestApplyPerformsEveryPutAndEveryDelete(t *testing.T) {
	s := batchStore(t)
	for _, id := range []string{"a", "b"} {
		if err := s.Put(Record{Kind: "ticket", ID: id, Data: []byte("before-" + id)}); err != nil {
			t.Fatal(err)
		}
	}
	err := s.Apply("merge-m1", []Op{
		{Kind: "ticket", ID: "m1", Data: []byte("the merged one")},
		{Kind: "ticket-merge", ID: "m1", Data: []byte("the working")},
		{Kind: "ticket", ID: "a", Delete: true},
		{Kind: "ticket", ID: "b", Delete: true},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	recs, err := s.List("ticket")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != "m1" || string(recs[0].Data) != "the merged one" {
		t.Fatalf("after the batch the tickets are %v; want exactly m1", recs)
	}
	if _, err := s.Get("ticket-merge", "m1"); err != nil {
		t.Fatalf("the batch's second put did not happen: %v", err)
	}
	// The journal is gone once the batch is applied: a leftover would be replayed on every Open
	// for the rest of the store's life.
	if entries, _ := os.ReadDir(filepath.Join(s.Path(), journalDir)); len(entries) != 0 {
		t.Errorf("the journal directory still holds %d entries after a completed batch", len(entries))
	}
}

// A put and a delete of the SAME record in one batch: the caller that merges N tickets into the
// identifier of one of them produces this shape, and the ordering rule runJournal states is what
// keeps the put from being undone. Asserted because the rule is otherwise only a comment.
func TestAPutSurvivesADeleteOfTheSameRecordInOneBatch(t *testing.T) {
	s := batchStore(t)
	if err := s.Put(Record{Kind: "ticket", ID: "a", Data: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply("b1", []Op{
		{Kind: "ticket", ID: "a", Data: []byte("new")},
		{Kind: "ticket", ID: "a", Delete: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("ticket", "a"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Get after put-then-delete = %v; puts run before deletes, so the delete wins", err)
	}
}

func TestAnEmptyBatchIsRefusedRatherThanQuietlyDoingNothing(t *testing.T) {
	s := batchStore(t)
	if err := s.Apply("b", nil); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("Apply with no ops = %v; want ErrEmptyBatch", err)
	}
}

// EVERY OP IS CHECKED BEFORE THE COMMIT POINT. A batch with one unusable id must write nothing at
// all — not the good ops, and not a journal that a later Open would replay into the same failure.
func TestABatchWithAnUnusableOpWritesNothing(t *testing.T) {
	s := batchStore(t)
	err := s.Apply("b", []Op{
		{Kind: "ticket", ID: "good", Data: []byte("x")},
		{Kind: "ticket", ID: "../escape", Data: []byte("x")},
	})
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Apply with an unusable id = %v; want ErrInvalidName", err)
	}
	if _, err := s.Get("ticket", "good"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("a refused batch applied one of its ops anyway: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Path(), journalDir)); len(entries) != 0 {
		t.Errorf("a refused batch left a journal behind; a later Open would replay it")
	}
}

// THE STATE A KILLED PROCESS LEAVES. A journal is on disk and its ops have not been applied. Opening
// the store must finish the batch before handing it to anybody — that is what makes criterion 10's
// "never a half-merged state" true for readers that know nothing about merging.
func TestOpeningAStoreFinishesABatchThatWasCommittedAndNeverApplied(t *testing.T) {
	s := batchStore(t)
	if err := s.Put(Record{Kind: "ticket", ID: "a", Data: []byte("a")}); err != nil {
		t.Fatal(err)
	}
	stageJournal(t, s.Path(), "merge-m1", []journalOp{
		{Kind: "ticket", ID: "m1", Data: []byte("the merged one")},
		{Kind: "ticket", ID: "a", Delete: true},
	})

	// A reader that knows nothing about batches. It sees the finished state or the test fails.
	reopened, err := Open(s.Path())
	if err != nil {
		t.Fatalf("Open with an unapplied batch on disk = %v; it must recover, not refuse", err)
	}
	recs, err := reopened.List("ticket")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != "m1" {
		t.Fatalf("after recovery the tickets are %v; want exactly the merged one — the half state is "+
			"what criterion 10 forbids", ids(recs))
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Path(), journalDir)); len(entries) != 0 {
		t.Errorf("recovery left the journal in place, so it will be replayed forever")
	}
}

// Replaying a batch that was ALREADY fully applied must reach the same place. Every op is idempotent
// and this is what says so: a process killed after the last op but before the journal was removed
// leaves exactly this.
func TestReplayingAnAlreadyAppliedBatchChangesNothing(t *testing.T) {
	s := batchStore(t)
	if err := s.Apply("b1", []Op{{Kind: "ticket", ID: "m1", Data: []byte("merged")}}); err != nil {
		t.Fatal(err)
	}
	stageJournal(t, s.Path(), "b1", []journalOp{
		{Kind: "ticket", ID: "m1", Data: []byte("merged")},
		{Kind: "ticket", ID: "gone", Delete: true},
	})
	reopened, err := Open(s.Path())
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	rec, err := reopened.Get("ticket", "m1")
	if err != nil || string(rec.Data) != "merged" {
		t.Fatalf("replaying an applied batch changed it: %v / %q", err, rec.Data)
	}
}

// A JOURNAL THAT CANNOT BE READ IS NEVER STEPPED OVER. Opening a store whose committed batch is
// damaged must fail — quietly ignoring it opens a store that is mid-sentence and reports the result
// as the truth.
func TestAnUnreadableJournalFailsTheOpenRatherThanBeingSkipped(t *testing.T) {
	s := batchStore(t)
	dir := filepath.Join(s.Path(), journalDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b1"+recordSuffix), []byte(`{"format":1,"ops":[],"sha256":"nope"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(s.Path())
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Open with a damaged journal = %v; want ErrUnreadable — a committed batch that "+
			"cannot be replayed is not something to step over", err)
	}
	if err != nil && !strings.Contains(err.Error(), "checksum") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
}

// An abandoned temporary from an interrupted JOURNAL write is not a committed batch. If it were
// read, a batch that never reached its commit point would be applied — which is the pre-commit
// state's whole guarantee, inverted.
func TestAnInterruptedJournalWriteIsNotACommittedBatch(t *testing.T) {
	s := batchStore(t)
	dir := filepath.Join(s.Path(), journalDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, tempPrefix+"b1"+recordSuffix+"-123"), []byte("half a journ"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Path())
	if err != nil {
		t.Fatalf("Open with an abandoned journal temporary = %v; it is not a record and must be invisible", err)
	}
	recs, err := reopened.List("ticket")
	if err != nil || len(recs) != 0 {
		t.Fatalf("tickets after opening past an abandoned temporary: %v / %v", ids(recs), err)
	}
}

// A journal is not a kind of thing the person has. `omw store status` lists kinds, and a batch in
// flight must not appear there as records the person owns.
func TestAJournalIsNotReportedAsAKindOfRecord(t *testing.T) {
	s := batchStore(t)
	stageJournal(t, s.Path(), "b1", []journalOp{{Kind: "ticket", ID: "x", Data: []byte("x")}})
	kinds, err := s.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if string(k) == journalDir {
			t.Errorf("Kinds() reports %q; the journal is machinery, not the person's records", k)
		}
	}
}

// stageJournal writes a committed-but-unapplied batch, which is what a process killed mid-Apply
// leaves behind.
func stageJournal(t *testing.T, root, name string, ops []journalOp) {
	t.Helper()
	dir := filepath.Join(root, journalDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := encodeJournal(journalFile{Format: journalFormat, Name: name, Ops: ops})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+recordSuffix), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func ids(recs []Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}
