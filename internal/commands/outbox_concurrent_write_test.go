package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
)

// Issue #69, third finding: a refused concurrent write leaked a raw Go error and an empty code.
//
//	omw outbox draft: open /tmp/.../outbox/fresh0/000001.body: file exists (code: )
//
// Every other refusal in this product names a code and explains itself. This one handed a person
// the errno text of a system call it made on their behalf, under a code field that was blank —
// which is worse than unhelpful, because a blank code is what a caller reads to mean "no refusal".
//
// The write itself was never unsafe: O_EXCL is what kept the data whole, and the losers exited
// non-zero. What they could not do was say WHY in this product's vocabulary.

// rawGoError matches the leak: the errno wording of an open(2) that this product should never show.
var rawGoError = regexp.MustCompile(`open /|: file exists`)

// codeField pulls what the CLI printed as the refusal's code.
var codeField = regexp.MustCompile(`\(code: ([^)]*)\)`)

func TestConcurrentWritersToOneDraftAreRefusedByNameOrNotAtAll(t *testing.T) {
	env := obWorld(t)
	const writers = 8
	const rounds = 25

	type outcome struct {
		code int
		text string
	}
	var mu sync.Mutex
	var results []outcome

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				got := runOutboxCmd(t, env, "draft", "contended", fmt.Sprintf("writer %d round %d", w, r))
				mu.Lock()
				results = append(results, outcome{code: got.code, text: got.all()})
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	succeeded := 0
	for _, got := range results {
		if got.code == cli.Success {
			succeeded++
			continue
		}
		// A REFUSAL, WHICH IS ALLOWED. What is not allowed is how it used to read.
		if loc := rawGoError.FindString(got.text); loc != "" {
			t.Errorf("a refused concurrent write leaked a raw Go error (%q):\n%s", loc, got.text)
		}
		m := codeField.FindStringSubmatch(got.text)
		if m == nil {
			t.Errorf("a refused concurrent write named no code at all:\n%s", got.text)
			continue
		}
		if strings.TrimSpace(m[1]) == "" {
			t.Errorf("a refused concurrent write printed an EMPTY code, which a caller reads as no refusal:\n%s", got.text)
		}
	}

	// NOT ONE REVISION MAY BE OVERWRITTEN. A retry that resolved the contention by clobbering
	// somebody's words would pass every assertion above and be the worse defect.
	dir := filepath.Join(obStorePath(t, env), drafts.OutboxDirName, "contended")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the contended draft: %v", err)
	}
	bodies := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".body") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Errorf("revision %s could not be read: %v", e.Name(), rerr)
			continue
		}
		if raw == nil || len(raw) == 0 {
			t.Errorf("revision %s is empty", e.Name())
		}
		if prev, dup := bodies[string(raw)]; dup {
			t.Errorf("revision %s and revision %s hold the same text, so a write was applied twice", e.Name(), prev)
		}
		bodies[string(raw)] = e.Name()
	}
	if len(bodies) != succeeded {
		t.Errorf("%d writes reported success and %d revisions are on disk", succeeded, len(bodies))
	}
	t.Logf("%d of %d concurrent writes succeeded, %d distinct revisions on disk",
		succeeded, writers*rounds, len(bodies))
}
