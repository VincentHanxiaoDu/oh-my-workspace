package commands

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/inbox"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Issue #7 as a person meets it. The package-level facts about merging are driven in
// internal/inbox/merge_test.go and the interrupted merge in internal/inbox/mergecrash_test.go; what
// is here is what a person types and what they see, because most of Issue #7's criteria are stated
// as "distinguishable by exit status" and "the merged ticket shows", which are claims about output.
//
// Helpers shared with inbox_test.go in this package: inboxEnv, shortPathInboxEnv, seed, lineWith.

func runTicketCmd(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"ticket"}, args...), &out, &errb, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

// theScatteredLogin seeds the Issue's own scenario across two channels.
func theScatteredLogin(t *testing.T, s *store.Store) {
	t.Helper()
	seed(t, s,
		inbox.Ticket{ID: "mail1", Title: inbox.Text("SSO login fails for Ana"),
			Summary: inbox.Text("Locked out since the cutover."), Channel: inbox.Text("email"),
			Arrived: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)},
		inbox.Ticket{ID: "mail2", Title: inbox.Text("Re: SSO login fails for Ana"),
			Summary: inbox.Text("Still failing."), Channel: inbox.Text("email"),
			Arrived: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)},
		inbox.Ticket{ID: "chat1", Title: inbox.Text("login thread"),
			Summary: inbox.Text("Three people comparing notes."), Channel: inbox.Text("teams"),
			Arrived: time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC)},
	)
}

// mergeInvocation is the whole command line for the scenario's merge, in one place so that every
// test below merges the same way and a change to the flags moves them together.
func mergeInvocation(ids ...string) []string {
	args := []string{"merge",
		"--id", "login",
		"--title", "Ana cannot log in since the SSO cutover",
		"--summary", "One broken login reported by several people. Legacy accounts cannot " +
			"authenticate against the new identity provider.",
	}
	for _, id := range ids {
		args = append(args, "--why", id+"=the same broken login")
	}
	for _, id := range ids {
		if id == "mail1" {
			args = append(args, "--source", "mail1=<CAB-1@mail.example.invalid>")
		}
	}
	return append(args, ids...)
}

func inboxListing(t *testing.T, env map[string]string) string {
	t.Helper()
	code, out, errOut := runInboxCmd(t, env, "list")
	if code != cli.Success {
		t.Fatalf("`omw inbox list` exited %d: %s", code, errOut)
	}
	return out
}

// ---------------------------------------------------------------------------
// CRITERION 1 — the inbox shows one ticket where it showed the merged ones.
// ---------------------------------------------------------------------------

// ASSERTED THROUGH `omw inbox list`, NOT THROUGH THIS COMMAND'S OWN LISTING. Criterion 1 is about
// what the person's inbox shows, and a merge that only looks merged to the command that made it has
// not done the thing they asked for.
func TestAfterAMergeTheInboxListsOneTicketAndNotTheSources(t *testing.T) {
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)
	before := inboxListing(t, env)
	for _, id := range []string{"mail1", "mail2", "chat1"} {
		if !strings.Contains(before, "ticket "+id) {
			t.Fatalf("the scenario did not seed %q:\n%s", id, before)
		}
	}
	code, _, errOut := runTicketCmd(t, env, mergeInvocation("mail1", "mail2", "chat1")...)
	if code != cli.Success {
		t.Fatalf("the merge exited %d: %s", code, errOut)
	}
	after := inboxListing(t, env)
	if !strings.Contains(after, "ticket login") {
		t.Errorf("the merged ticket is not in the inbox:\n%s", after)
	}
	for _, id := range []string{"mail1", "mail2", "chat1"} {
		if strings.Contains(after, "ticket "+id+"\n") {
			t.Errorf("the merged-away ticket %q is still listed as a separate open item:\n%s", id, after)
		}
	}
	if !strings.Contains(after, "tickets:     1\n") {
		t.Errorf("the inbox does not report exactly one ticket:\n%s", after)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 2 — crossing channels is not refused, warned about, or degraded.
// ---------------------------------------------------------------------------

func TestMergingAcrossChannelsSucceedsOnTheSameTermsAsWithinOne(t *testing.T) {
	sameEnv, _, sameStore := inboxEnv(t)
	theScatteredLogin(t, sameStore)
	sameCode, sameOut, sameErr := runTicketCmd(t, sameEnv, mergeInvocation("mail1", "mail2")...)

	crossEnv, _, crossStore := inboxEnv(t)
	theScatteredLogin(t, crossStore)
	crossCode, crossOut, crossErr := runTicketCmd(t, crossEnv, mergeInvocation("mail1", "chat1")...)

	if sameCode != cli.Success || crossCode != cli.Success {
		t.Fatalf("same-channel exited %d (%s), cross-channel exited %d (%s); §3.2 merges across "+
			"channels by design", sameCode, sameErr, crossCode, crossErr)
	}
	if sameCode != crossCode {
		t.Errorf("a cross-channel merge exits differently from a same-channel one")
	}
	// NOT WARNED ABOUT. The cross-channel run must not say anything cautionary that the same-channel
	// run does not, so the words are looked for in the difference rather than in the whole.
	for _, word := range []string{"warning", "caution", "note that", "however", "degraded",
		"partial", "may not", "cannot be merged", "different channel", "mixed"} {
		inCross := strings.Contains(strings.ToLower(crossOut+crossErr), word)
		inSame := strings.Contains(strings.ToLower(sameOut+sameErr), word)
		if inCross && !inSame {
			t.Errorf("the cross-channel merge says %q and the same-channel one does not; no criterion "+
				"of the merge may degrade on the grounds that the sources are different channels", word)
		}
	}
	if crossErr != sameErr {
		t.Errorf("the two merges wrote different things to stderr:\n same:  %q\n cross: %q", sameErr, crossErr)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 3 — a written title and a written summary.
// ---------------------------------------------------------------------------

func TestAMergeWithoutAWrittenTitleOrSummaryIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no title", []string{"merge", "--id", "login", "--summary", "a real summary", "mail1", "mail2"}},
		{"no summary", []string{"merge", "--id", "login", "--title", "a real title", "mail1", "mail2"}},
		{"the source titles run together", []string{"merge", "--id", "login",
			"--title", "the login problem",
			"--summary", "SSO login fails for Ana Re: SSO login fails for Ana",
			"mail1", "mail2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _, s := inboxEnv(t)
			theScatteredLogin(t, s)
			before := inboxListing(t, env)
			code, out, _ := runTicketCmd(t, env, tc.args...)
			if code == cli.Success {
				t.Fatalf("the merge succeeded:\n%s", out)
			}
			if inboxListing(t, env) != before {
				t.Errorf("a refused merge changed the inbox")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 4 and 7 — the merge shows its working, and the origin is on the ticket.
// ---------------------------------------------------------------------------

func TestTheMergedTicketShowsWhatWasFoldedInWhereFromAndWhy(t *testing.T) {
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)
	if code, _, errOut := runTicketCmd(t, env, mergeInvocation("mail1", "mail2", "chat1")...); code != cli.Success {
		t.Fatalf("the merge exited %d: %s", code, errOut)
	}
	code, out, errOut := runTicketCmd(t, env, "show", "login")
	if code != cli.Success {
		t.Fatalf("`omw ticket show login` exited %d: %s", code, errOut)
	}

	// THREE THINGS FOR EVERY INPUT, COUNTED. A merged ticket with an input missing any of them is a
	// failure, so the count is what is asserted and not the presence of the words somewhere.
	for _, label := range []string{"was:", "channel:", "source:", "why:"} {
		if got := strings.Count(out, label); got < 3 {
			t.Errorf("`show` names %q %d times; there were 3 inputs and each must carry it:\n%s", label, got, out)
		}
	}
	// CRITERION 7: where each piece came from, readable from this ticket alone.
	for _, want := range []string{"email", "teams", "<CAB-1@mail.example.invalid>", "no channel was consulted"} {
		if !strings.Contains(out, want) {
			t.Errorf("`show` does not carry %q:\n%s", want, out)
		}
	}
	for _, id := range []string{"mail1", "mail2", "chat1"} {
		if !strings.Contains(out, id) {
			t.Errorf("`show` does not say that %q was folded in:\n%s", id, out)
		}
	}
	// And no field of the working is blank. A line whose value is empty is the silence criterion 12
	// forbids, and it is the shape a missing "why" would take.
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, label := range []string{"was:", "channel:", "source:", "why:"} {
			if strings.HasPrefix(trimmed, label) && strings.TrimSpace(strings.TrimPrefix(trimmed, label)) == "" {
				t.Errorf("a field of the merge's working is blank: %q", line)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 5 and 6 — reversible exactly, and visibly not the same as never merged.
// ---------------------------------------------------------------------------

func TestUnmergingPutsTheInboxBackAndSaysTheTicketsWereMerged(t *testing.T) {
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)
	seed(t, s, inbox.Ticket{ID: "elsewhere", Title: inbox.Text("an unrelated problem"),
		Summary: inbox.Text("Nothing to do with the login."), Channel: inbox.Text("email")})
	before := inboxListing(t, env)

	if code, _, errOut := runTicketCmd(t, env, mergeInvocation("mail1", "mail2", "chat1")...); code != cli.Success {
		t.Fatalf("the merge exited %d: %s", code, errOut)
	}
	code, _, errOut := runTicketCmd(t, env, "unmerge", "login")
	if code != cli.Success {
		t.Fatalf("the unmerge exited %d: %s", code, errOut)
	}
	// CRITERION 5, as a person checks it: the inbox reads exactly as it did.
	if after := inboxListing(t, env); after != before {
		t.Errorf("the inbox after a merge and an unmerge is not what it was:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// CRITERION 6: and yet the restored tickets are distinguishable from ones never merged.
	_, restoredOut, _ := runTicketCmd(t, env, "show", "mail1")
	_, untouchedOut, _ := runTicketCmd(t, env, "show", "elsewhere")
	restored := strings.TrimSpace(lineWith(restoredOut, "history:"))
	untouched := strings.TrimSpace(lineWith(untouchedOut, "history:"))
	if restored == "" || untouched == "" {
		t.Fatalf("`show` does not report whether a ticket was ever merged:\n%s", restoredOut)
	}
	// PAIRWISE. Each against its own expected wording passes just as happily after both have been
	// edited into the same sentence.
	if restored == untouched {
		t.Errorf("a ticket that was merged and unmerged renders identically to one that never was: %q", restored)
	}
	if restoredOut == untouchedOut {
		t.Errorf("the two whole outputs are identical")
	}
	if !strings.Contains(restoredOut, "login") {
		t.Errorf("the restored ticket does not say what it was merged into:\n%s", restoredOut)
	}
	// The same distinction is in the merge-aware listing.
	_, listOut, _ := runTicketCmd(t, env, "list")
	if strings.Count(listOut, "merged and then unmerged") != 3 {
		t.Errorf("`omw ticket list` does not mark exactly the three restored tickets:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 8 and 9 — refusals, by exit status alone, with the inbox unchanged.
// ---------------------------------------------------------------------------

func TestAMergeThatCannotHappenFailsByExitStatusAndChangesNothing(t *testing.T) {
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)
	before := inboxListing(t, env)

	okEnv, _, okStore := inboxEnv(t)
	theScatteredLogin(t, okStore)
	success, _, _ := runTicketCmd(t, okEnv, mergeInvocation("mail1", "mail2")...)
	if success != cli.Success {
		t.Fatalf("the control merge did not succeed, so there is nothing to be distinguishable from")
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"only one ticket named", mergeInvocation("mail1")},
		{"no tickets named", mergeInvocation()},
		{"a ticket that does not exist", mergeInvocation("mail1", "never-existed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runTicketCmd(t, env, tc.args...)
			if code == cli.Success {
				t.Fatalf("it exited 0:\n%s", out)
			}
			if code == success {
				t.Errorf("a merge that could not happen shares exit code %d with one that did", success)
			}
			if strings.Contains(strings.ToLower(out), "merged ") {
				t.Errorf("a refused merge told the person something was merged:\n%s", out)
			}
			if !strings.Contains(errOut, "Nothing was merged") && !strings.Contains(errOut, "No merge has been made") {
				t.Errorf("the refusal does not say nothing was merged:\n%s", errOut)
			}
			if after := inboxListing(t, env); after != before {
				t.Errorf("a refused merge changed the inbox:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if _, _, showErr := runTicketCmd(t, env, "show", "login"); !strings.Contains(showErr, "no ticket") {
				t.Errorf("a partial merge was left behind: %s", showErr)
			}
		})
	}
}

func TestUnmergingATicketThatWasNeverMergedFailsAndLeavesItAlone(t *testing.T) {
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)

	okEnv, _, okStore := inboxEnv(t)
	theScatteredLogin(t, okStore)
	if code, _, _ := runTicketCmd(t, okEnv, mergeInvocation("mail1", "mail2")...); code != cli.Success {
		t.Fatal("the control merge did not succeed")
	}
	success, _, _ := runTicketCmd(t, okEnv, "unmerge", "login")
	if success != cli.Success {
		t.Fatalf("the control unmerge exited %d", success)
	}

	before := inboxListing(t, env)
	for _, id := range []string{"mail1", "never-existed"} {
		code, out, errOut := runTicketCmd(t, env, "unmerge", id)
		if code == cli.Success {
			t.Errorf("unmerging %q, which was never merged, exited 0:\n%s", id, out)
		}
		if code == success {
			t.Errorf("unmerging something never merged shares exit code %d with a real unmerge", success)
		}
		if !strings.Contains(errOut, "exactly as it was") {
			t.Errorf("the refusal does not say the ticket is untouched:\n%s", errOut)
		}
	}
	if after := inboxListing(t, env); after != before {
		t.Errorf("a refused unmerge altered the inbox")
	}
}

// ---------------------------------------------------------------------------
// CRITERION 11 — nothing that was never a ticket becomes mergeable.
// ---------------------------------------------------------------------------

func TestAnAcknowledgementCannotBeMergedInAndNoMergeMakesOneATicket(t *testing.T) {
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)

	// Naming a piece of traffic that was correctly never turned into a ticket.
	code, _, errOut := runTicketCmd(t, env, mergeInvocation("mail1", "Hii")...)
	if code == cli.Success {
		t.Errorf("merging a piece of traffic that is not a ticket succeeded")
	}
	if !strings.Contains(errOut, "no such ticket") {
		t.Errorf("the refusal does not say the thing named is not a ticket:\n%s", errOut)
	}

	// And a merge cannot mint one either, by titling the result with an acknowledgement.
	args := []string{"merge", "--id", "login", "--title", "ok",
		"--summary", "A real summary of the one broken login.", "mail1", "mail2"}
	code, _, errOut = runTicketCmd(t, env, args...)
	if code == cli.Success {
		t.Errorf("a merged ticket titled \"ok\" was accepted")
	}
	if !strings.Contains(errOut, "no priority") {
		t.Errorf("the refusal does not say there is no priority to put it at:\n%s", errOut)
	}
	// There is no flag that would put it somewhere out of the way, because there is nowhere.
	_, usage, _ := runTicketCmd(t, env, "--help")
	for _, word := range []string{"priority", "rank", "severity", "low-priority", "score"} {
		if strings.Contains(strings.ToLower(usage), word) {
			t.Errorf("the command offers %q; §3.2: acknowledgements are not low-priority tickets", word)
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERION 12 — undetermined is never blank and never a value.
// ---------------------------------------------------------------------------

func TestAnUnresolvedOriginAnAbsentOneAndARealOneAreThreeDistinctLines(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s,
		inbox.Ticket{ID: "known", Title: inbox.Text("a real one"), Channel: inbox.Text("email")},
		inbox.Ticket{ID: "unresolved", Title: inbox.Text("a second"),
			Channel: inbox.Undetermined("the source channel could not be read")},
		inbox.Ticket{ID: "none", Title: inbox.Text("a third"), Channel: inbox.Absent()},
	)
	args := []string{"merge", "--id", "login",
		"--title", "one problem reported three ways",
		"--summary", "A written summary of the single underlying problem, in a sentence.",
		"--why", "known=the same problem", // the other two get no reason at all
		"--source", "known=<x@example.invalid>",
		"known", "unresolved", "none"}
	if code, _, errOut := runTicketCmd(t, env, args...); code != cli.Success {
		t.Fatalf("the merge exited %d: %s", code, errOut)
	}
	code, out, errOut := runTicketCmd(t, env, "show", "login")
	if code != cli.Success {
		t.Fatalf("`show` exited %d: %s", code, errOut)
	}

	// FROM THE WORKING ONLY. The merged ticket has a channel line of its own, and counting it made
	// the first version of this assertion look for three lines and find four — a failure that would
	// equally have masked a genuinely missing input.
	working := out[strings.Index(out, "what was folded in"):]
	channels := valuesAfter(working, "channel:")
	if len(channels) != 3 {
		t.Fatalf("`show` prints %d channel lines for three inputs:\n%s", len(channels), out)
	}
	// PAIRWISE, AND NEVER AGAINST A LITERAL. A real value, a recorded absence and an origin that
	// could not be resolved are three answers, so the assertion is between them.
	for i := range channels {
		if strings.TrimSpace(channels[i]) == "" {
			t.Errorf("a channel line is blank; an undetermined field never prints as blank:\n%s", out)
		}
		for j := i + 1; j < len(channels); j++ {
			if channels[i] == channels[j] {
				t.Errorf("two of the three channel renderings are the same: %q", channels[i])
			}
		}
	}
	whys := valuesAfter(working, "why:")
	if len(whys) != 3 {
		t.Fatalf("`show` prints %d why lines:\n%s", len(whys), out)
	}
	recorded, unrecorded := whys[0], whys[1]
	if recorded == unrecorded {
		t.Errorf("a why that was written and one that was not recorded render the same: %q", recorded)
	}
	if strings.TrimSpace(unrecorded) == "" {
		t.Errorf("an unrecorded why prints as blank")
	}
	if !strings.Contains(unrecorded, "could not be determined") {
		t.Errorf("an unrecorded why does not render as undetermined: %q", unrecorded)
	}
	// A source that is undetermined must not look like a real identifier either.
	sources := valuesAfter(working, "source:")
	if sources[0] == sources[1] {
		t.Errorf("a recorded source identifier and an unrecorded one render the same: %q", sources[0])
	}
}

// valuesAfter returns the text following every occurrence of label, in order.
func valuesAfter(s, label string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, label) {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, label)))
		}
	}
	return out
}

// The project's standing rule, on this command's own range of answers.
func TestOnTicketsCouldNotDetermineAndDeterminedToBeNothingNeverShareAnExitCode(t *testing.T) {
	emptyEnv, _, _ := inboxEnv(t)
	determinedNothing, _, _ := runTicketCmd(t, emptyEnv, "list") // no tickets: a real answer

	noStoreEnv := map[string]string{"HOME": t.TempDir()} // no OMW_STORE, no store anywhere
	couldNotDetermine, _, _ := runTicketCmd(t, noStoreEnv, "list")

	if determinedNothing != cli.Success {
		t.Errorf("'there is nothing to merge' exited %d; it is an answer and answering succeeds", determinedNothing)
	}
	if couldNotDetermine == determinedNothing {
		t.Errorf("'could not determine' and 'determined to be nothing' share exit code %d", determinedNothing)
	}
	// AND THE GENUINELY UNDETERMINED CASE, which is not the same as "there is no store": a store
	// that is there and cannot be read. It must not share a code with either of the two determined
	// answers above, nor with a refusal.
	damagedEnv, damagedRoot, _ := inboxEnv(t)
	if err := os.WriteFile(filepath.Join(damagedRoot, "store.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	undetermined, _, _ := runTicketCmd(t, damagedEnv, "list")
	if undetermined != cli.ExitUndetermined {
		t.Errorf("a store that could not be read exited %d; want %d", undetermined, cli.ExitUndetermined)
	}
	if undetermined == determinedNothing {
		t.Errorf("'could not be read' shares exit code %d with 'there is nothing to merge'", undetermined)
	}
	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)
	refused, _, _ := runTicketCmd(t, env, "unmerge", "mail1")
	if refused == undetermined {
		t.Errorf("'this was never merged' shares exit code %d with 'could not be determined'", refused)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 13 — nothing implicit: no daemon started, no hub contacted.
// ---------------------------------------------------------------------------

func TestMergingAndUnmergingNeverStartTheDaemonAndSayItIsNotRunning(t *testing.T) {
	env, root, s := inboxEnv(t)
	theScatteredLogin(t, s)
	sock := filepath.Join(root, inbox.ControlSocketName)
	if _, err := os.Stat(sock); err == nil {
		t.Fatalf("a control socket exists before anything ran, so this asserts nothing")
	}
	for _, args := range [][]string{mergeInvocation("mail1", "mail2", "chat1"), {"unmerge", "login"}} {
		code, out, errOut := runTicketCmd(t, env, args...)
		if code != cli.Success {
			t.Fatalf("`omw ticket %s` exited %d: %s", args[0], code, errOut)
		}
		if !strings.Contains(out, "not running") {
			t.Errorf("`omw ticket %s` does not say the daemon is not running:\n%s", args[0], out)
		}
		if !strings.Contains(out, "has not started anything") {
			t.Errorf("`omw ticket %s` does not say it started nothing:\n%s", args[0], out)
		}
		if _, err := os.Stat(sock); err == nil {
			t.Fatalf("`omw ticket %s` created a control socket — it started the daemon", args[0])
		}
	}
}

func TestNoMergeOperationTouchesAConfiguredHub(t *testing.T) {
	var requests atomic.Int64
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hub.Close()
	// The hub is reachable — proved, not assumed, so a zero count below is evidence about the merge
	// and not about a server that was never listening.
	resp, err := http.Get(hub.URL)
	if err != nil {
		t.Fatalf("the test's own hub is not reachable, so this would assert nothing: %v", err)
	}
	_ = resp.Body.Close()
	if requests.Load() != 1 {
		t.Fatal("the test's own hub did not count its own request")
	}
	requests.Store(0)

	env, _, s := inboxEnv(t)
	theScatteredLogin(t, s)
	for _, key := range []string{"OMW_HUB", "OMW_HUB_URL", "OMW_SERVER", "OMW_ENDPOINT"} {
		env[key] = hub.URL
	}
	steps := [][]string{mergeInvocation("mail1", "mail2", "chat1"), {"show", "login"}, {"list"}, {"unmerge", "login"}}
	for _, args := range steps {
		code, _, errOut := runTicketCmd(t, env, args...)
		if code != cli.Success {
			t.Fatalf("`omw ticket %s` exited %d with a hub configured; the local half stands alone "+
				"(PRD §4.4)\nstderr: %s", args[0], code, errOut)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("%d request(s) reached the hub; tickets are never published (§2.3), so no merge "+
			"operation ever contacts one", requests.Load())
	}
}

// CRITERION 14 — with NO hub configured, merge, unmerge and inspection all work fully. The server
// here is configured nowhere; anything reaching it would be reaching it by some route other than
// configuration.
func TestWithNoHubConfiguredMergingUnmergingAndInspectingAllWorkFully(t *testing.T) {
	var requests atomic.Int64
	hub := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer hub.Close()

	env, _, s := inboxEnv(t) // no hub key of any kind
	theScatteredLogin(t, s)
	for _, args := range [][]string{
		mergeInvocation("mail1", "mail2", "chat1"), {"show", "login"}, {"list"}, {"unmerge", "login"},
	} {
		code, out, errOut := runTicketCmd(t, env, args...)
		if code != cli.Success {
			t.Fatalf("`omw ticket %s` exited %d with no hub configured\nstderr: %s", args[0], code, errOut)
		}
		if strings.Contains(strings.ToLower(errOut), "hub") {
			t.Errorf("`omw ticket %s` complained about a hub:\n%s", args[0], errOut)
		}
		if args[0] == "show" && !strings.Contains(out, "channel:") {
			t.Errorf("inspection of the merge's working is not complete with no hub:\n%s", out)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("%d outbound request(s) with no hub configured", requests.Load())
	}
}

// ---------------------------------------------------------------------------
// CRITERION 15 — an unconfirmed control API is said, not gone round.
// ---------------------------------------------------------------------------

// PROBED, NOT NAMED. Whether this environment can hold a unix socket is discovered by trying, not by
// asking which operating system this is: §5.1 ships two and a test naming one says nothing about the
// other.
func TestWhenOwnerOnlyPermissionsCannotBeConfirmedMergeSaysSoRatherThanProceeding(t *testing.T) {
	env, root := shortPathInboxEnv(t)
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	theScatteredLogin(t, s)

	sock := filepath.Join(root, inbox.ControlSocketName)
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("a unix socket cannot be created at %s here, so this criterion cannot be staged: %v", sock, err)
	}
	defer l.Close()
	if err := os.Chmod(sock, 0o666); err != nil {
		t.Skipf("this filesystem does not honour a permission change on a socket: %v", err)
	}
	if info, serr := os.Stat(sock); serr != nil || info.Mode().Perm()&0o077 == 0 {
		t.Skipf("the permission change did not take on this filesystem, so nothing would be asserted")
	}

	before := inboxListing(t, env)
	code, out, errOut := runTicketCmd(t, env, mergeInvocation("mail1", "mail2")...)
	if code == cli.Success {
		t.Fatalf("the merge proceeded although owner-only permissions could not be confirmed:\n%s", out)
	}
	if !strings.Contains(errOut, "control API") {
		t.Errorf("the refusal does not name the control API:\n%s", errOut)
	}
	if !strings.Contains(errOut, "owner-only") {
		t.Errorf("the refusal does not say why the control API did not open:\n%s", errOut)
	}
	if inboxListing(t, env) != before {
		t.Errorf("the refused merge changed the inbox")
	}

	// DISTINGUISHABLE FROM "NO TICKETS TO MERGE", which is the confusion criterion 15 names.
	emptyEnv, _, _ := inboxEnv(t)
	emptyCode, emptyOut, emptyErr := runTicketCmd(t, emptyEnv, "list")
	if errOut == emptyErr && out == emptyOut {
		t.Errorf("an unavailable control API renders identically to an inbox with nothing in it")
	}
	if emptyCode == code {
		t.Errorf("an unavailable control API shares exit code %d with 'there is nothing to merge'", code)
	}
	// And unmerge refuses on the same terms.
	if unmergeCode, _, unmergeErr := runTicketCmd(t, env, "unmerge", "mail1"); unmergeCode == cli.Success {
		t.Errorf("unmerge proceeded although the control API is unavailable")
	} else if !strings.Contains(unmergeErr, "control API") {
		t.Errorf("unmerge's refusal does not name the control API:\n%s", unmergeErr)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 16 — nothing expires.
// ---------------------------------------------------------------------------

func TestATicketMergedLongAgoIsStillListedAndStillUnmergeable(t *testing.T) {
	env, _, s := inboxEnv(t)
	// Backdated past any plausible expiry window.
	seed(t, s,
		inbox.Ticket{ID: "old1", Title: inbox.Text("a very old problem"), Summary: inbox.Text("..."),
			Channel: inbox.Text("email"), Arrived: time.Now().AddDate(-40, 0, 0)},
		inbox.Ticket{ID: "old2", Title: inbox.Text("the same very old problem"), Summary: inbox.Text("..."),
			Channel: inbox.Text("teams"), Arrived: time.Now().AddDate(-40, 0, 0)},
	)
	before := inboxListing(t, env)
	if code, _, errOut := runTicketCmd(t, env, mergeInvocation("old1", "old2")...); code != cli.Success {
		t.Fatalf("the merge exited %d: %s", code, errOut)
	}
	// The merge record is not aged out: it is still there, still readable, and still shows working.
	code, out, _ := runTicketCmd(t, env, "show", "login")
	if code != cli.Success || !strings.Contains(out, "was:") {
		t.Fatalf("the working of an old merge is gone: %d\n%s", code, out)
	}
	if code, _, errOut := runTicketCmd(t, env, "unmerge", "login"); code != cli.Success {
		t.Fatalf("unmerging a very old merge exited %d: %s", code, errOut)
	}
	if after := inboxListing(t, env); after != before {
		t.Errorf("a very old merge did not come apart exactly")
	}
}

// ---------------------------------------------------------------------------
// Usage, and the structural guard.
// ---------------------------------------------------------------------------

func TestTicketUsageErrorsAreNamedNotDumped(t *testing.T) {
	env, _, _ := inboxEnv(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{nil, cli.ExitUsage},
		{[]string{"publish", "login"}, cli.ExitUsage},
		{[]string{"merge", "--frce", "a", "b"}, cli.ExitUsage},
		{[]string{"unmerge"}, cli.ExitUsage},
		{[]string{"unmerge", "a", "b"}, cli.ExitUsage},
		{[]string{"show"}, cli.ExitUsage},
		{[]string{"list", "extra"}, cli.ExitUsage},
	} {
		code, _, errOut := runTicketCmd(t, env, tc.args...)
		if code != tc.want {
			t.Errorf("`omw ticket %s` exited %d; want %d\n%s", strings.Join(tc.args, " "), code, tc.want, errOut)
		}
	}
	// The name is echoed back so a typo is visible, and `publish` in particular is answered by
	// saying it does not exist rather than by a wall of help.
	if _, _, errOut := runTicketCmd(t, env, "publish", "login"); !strings.Contains(errOut, `"publish"`) {
		t.Errorf("an unknown operation is not named back:\n%s", errOut)
	}
	// An unknown flag is not silently taken as a ticket identifier.
	if _, _, errOut := runTicketCmd(t, env, "merge", "--frce", "a", "b"); !strings.Contains(errOut, "NOT been treated as a ticket") {
		t.Errorf("an unknown flag may have been taken as a ticket identifier:\n%s", errOut)
	}
}

func TestTheTicketCommandIsRegisteredAndListed(t *testing.T) {
	var out bytes.Buffer
	if code := cli.Run([]string{"help"}, &out, &out, func(string) string { return "" }); code != cli.Success {
		t.Fatalf("`omw help` exited %d", code)
	}
	if !strings.Contains(out.String(), "ticket") {
		t.Errorf("`omw help` does not list the ticket command:\n%s", out.String())
	}
}

// The command file itself imports nothing that could reach a hub. Structural, and labelled as such:
// the behavioural assertion is TestNoMergeOperationTouchesAConfiguredHub above.
func TestTheTicketCommandImportsNothingThatCouldReachAHub(t *testing.T) {
	b, err := os.ReadFile("ticket.go")
	if err != nil {
		t.Fatalf("reading the command's own source: %v", err)
	}
	src := string(b)
	head := src
	if i := strings.Index(src, "\nfunc "); i > 0 {
		head = src[:i]
	}
	for _, imp := range []string{`"net"`, `"net/http"`, `"net/rpc"`, `"os/exec"`, `"net/smtp"`} {
		if strings.Contains(head, imp) {
			t.Errorf("internal/commands/ticket.go imports %s; a merged ticket is never published", imp)
		}
	}
}
