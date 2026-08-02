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

// runInboxCmd drives `omw inbox ...` the way a person does, through the real registry, and returns
// what they would see. Every assertion below is on the exit code AND the text, because Issue #8
// states most of its criteria as "distinguishable by exit code" and the rest as "says which".
func runInboxCmd(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"inbox"}, args...), &out, &errb, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

// inboxEnv is a sandboxed environment with a store created in it. HOME is redirected as well as
// OMW_STORE, because the device's store pointer lives under HOME and no test may touch the pointer
// belonging to the machine it runs on.
func inboxEnv(t *testing.T) (map[string]string, string, *store.Store) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating a store to test against: %v", err)
	}
	return map[string]string{store.PathEnv: root, "HOME": t.TempDir()}, root, s
}

// shortPathInboxEnv is inboxEnv for the one test that needs to bind a unix socket inside the store.
//
// WHY IT EXISTS. A unix socket path has a hard length limit of about a hundred bytes, and the
// default temporary directory on at least one of the two platforms this product ships for is long
// enough on its own — with the test's name embedded in it, as t.TempDir does — that the bind fails
// before the criterion has been staged. The first version of criterion 15's test SKIPPED for that
// reason, and a skipped test is not a test.
//
// The shorter location is FOUND, not named: each candidate is tried by actually binding a socket in
// it, and the first that works is used. Nothing here asks which operating system this is.
func shortPathInboxEnv(t *testing.T) (map[string]string, string) {
	t.Helper()
	for _, base := range []string{os.TempDir(), "/tmp", ""} {
		dir, err := os.MkdirTemp(base, "omw")
		if err != nil {
			continue
		}
		probe := filepath.Join(dir, "probe.sock")
		l, err := net.Listen("unix", probe)
		if err != nil {
			_ = os.RemoveAll(dir)
			continue
		}
		_ = l.Close()
		_ = os.Remove(probe)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		root := filepath.Join(dir, "s")
		if _, err := store.Create(root); err != nil {
			t.Fatalf("creating a store at %s: %v", root, err)
		}
		return map[string]string{store.PathEnv: root, "HOME": t.TempDir()}, root
	}
	t.Skip("no directory on this machine will hold a bindable unix socket, so this cannot be staged")
	return nil, ""
}

func seed(t *testing.T, s *store.Store, tickets ...inbox.Ticket) {
	t.Helper()
	for _, tk := range tickets {
		if err := inbox.Put(s, tk); err != nil {
			t.Fatalf("seeding ticket %q: %v", tk.ID, err)
		}
	}
}

// lineWith returns the first line of s containing sub, for assertions that compare one rendered
// field against another rather than against a literal.
func lineWith(s, sub string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// CRITERION 1 — a real value and a missing value never produce the same output.
// ---------------------------------------------------------------------------

func TestListingMarksAMissingFieldDifferentlyFromAnEmptyOne(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s,
		inbox.Ticket{ID: "a-written", Title: inbox.Text("Restore Ana's login"), Summary: inbox.Text("Locked out since the SSO cutover.")},
		inbox.Ticket{ID: "b-empty", Title: inbox.Text(""), Summary: inbox.Text("")},
		inbox.Ticket{ID: "c-missing", Title: inbox.Absent(), Summary: inbox.Absent()},
	)
	code, stdout, stderr := runInboxCmd(t, env, "list")
	if code != cli.Success {
		t.Fatalf("exit %d; want 0\nstderr: %s", code, stderr)
	}
	// Each ticket's block, so the comparison is between renderings of the same field.
	blocks := map[string]string{}
	for _, part := range strings.Split(stdout, "ticket ")[1:] {
		blocks[strings.SplitN(part, "\n", 2)[0]] = part
	}
	for _, field := range []string{"title:", "summary:"} {
		written := lineWith(blocks["a-written"], field)
		empty := lineWith(blocks["b-empty"], field)
		missing := lineWith(blocks["c-missing"], field)
		for _, l := range []string{written, empty, missing} {
			if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), field)) == "" {
				t.Fatalf("a %s rendered as nothing at all in:\n%s", field, stdout)
			}
		}
		if empty == missing {
			t.Errorf("an empty %s and a missing %s both render as %q — a real value and a missing "+
				"value must never produce the same output (criterion 1)", field, field, empty)
		}
		if written == empty || written == missing {
			t.Errorf("a written %s renders the same as an empty or missing one: %q", field, written)
		}
	}
	// The listing renders BOTH fields of every ticket, not just a title.
	for id := range blocks {
		if !strings.Contains(blocks[id], "summary:") {
			t.Errorf("ticket %s was listed without its summary", id)
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERION 2 — reading one ticket, and reading one that is not there.
// ---------------------------------------------------------------------------

func TestReadingATicketRendersItsTitleSummaryAndThatItIsAnInboxTicket(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s, inbox.Ticket{
		ID: "ana-sso", Title: inbox.Text("Restore Ana's login"),
		Summary: inbox.Text("Locked out since the SSO cutover; asked twice."),
		Channel: inbox.Text("email"),
	})
	code, stdout, stderr := runInboxCmd(t, env, "read", "ana-sso")
	if code != cli.Success {
		t.Fatalf("exit %d; want 0\nstderr: %s", code, stderr)
	}
	for _, want := range []string{"Restore Ana's login", "Locked out since the SSO cutover", "inbox ticket"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the read does not show %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "never published") {
		t.Errorf("the read does not say the ticket is never published (PRD §2.3):\n%s", stdout)
	}
}

func TestReadingAnIdentifierThatIsNotThereExitsNonZeroWithNothingOnStdout(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s, inbox.Ticket{ID: "real", Title: inbox.Text("a real obligation"), Summary: inbox.Text("...")})

	ok, okOut, _ := runInboxCmd(t, env, "read", "real")
	bad, badOut, badErr := runInboxCmd(t, env, "read", "not-a-ticket")

	if ok != cli.Success {
		t.Fatalf("reading a ticket that is there exited %d", ok)
	}
	if bad == cli.Success {
		t.Errorf("reading an identifier that is not in the inbox exited 0")
	}
	if bad == ok {
		t.Errorf("a successful read and a failed one share exit code %d; criterion 2 requires they "+
			"be distinguishable by exit code alone", ok)
	}
	if strings.TrimSpace(badOut) != "" {
		t.Errorf("the failed read wrote a ticket rendering to stdout:\n%s", badOut)
	}
	if okOut == badOut {
		t.Errorf("the successful and failed reads produced the same stdout")
	}
	if !strings.Contains(badErr, "not-a-ticket") {
		t.Errorf("the failure does not name the identifier that was asked for:\n%s", badErr)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 3 — an empty inbox and an unreadable one never render identically.
// ---------------------------------------------------------------------------

func TestAnEmptyInboxIsSaidExplicitlyAndSucceeds(t *testing.T) {
	env, _, _ := inboxEnv(t)
	code, stdout, stderr := runInboxCmd(t, env, "list")
	if code != cli.Success {
		t.Fatalf("listing an empty inbox exited %d; an empty inbox is a determined answer\nstderr: %s", code, stderr)
	}
	if !strings.Contains(strings.ToLower(stdout), "empty") {
		t.Errorf("an empty inbox is not stated explicitly:\n%s", stdout)
	}
}

func TestAnEmptyInboxAndAnInboxThatCouldNotBeReadDifferInTextAndInExitCode(t *testing.T) {
	// Empty, and read successfully.
	emptyEnv, _, _ := inboxEnv(t)
	emptyCode, emptyOut, _ := runInboxCmd(t, emptyEnv, "list")

	// Could not be performed at all: there is no store.
	noStoreEnv := map[string]string{store.PathEnv: filepath.Join(t.TempDir(), "nowhere"), "HOME": t.TempDir()}
	noStoreCode, noStoreOut, noStoreErr := runInboxCmd(t, noStoreEnv, "list")

	// Could not be performed at all: the store is there and its tickets cannot be read.
	unreadableEnv, unreadableRoot, us := inboxEnv(t)
	seed(t, us, inbox.Ticket{ID: "hidden", Title: inbox.Text("a real obligation"), Summary: inbox.Text("...")})
	dir := filepath.Join(unreadableRoot, "records", string(inbox.Kind))
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("making the tickets unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	// PROBED, NOT NAMED. Running as a user who is not stopped by the mode bits — root in a
	// container is the usual one — this arrangement is not unreadable at all, and the assertion
	// below would be about nothing. Ask the filesystem rather than asking who we are.
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("this user can read a directory with mode 000, so an unreadable inbox cannot be staged here")
	}
	unreadableCode, unreadableOut, unreadableErr := runInboxCmd(t, unreadableEnv, "list")

	if emptyCode != cli.Success {
		t.Errorf("an empty inbox exited %d", emptyCode)
	}
	if noStoreCode == emptyCode {
		t.Errorf("an empty inbox and a listing that could not be performed at all share exit code %d", emptyCode)
	}
	if unreadableCode == emptyCode {
		t.Errorf("an empty inbox and an unreadable inbox share exit code %d (criterion 3)", emptyCode)
	}
	if emptyOut == noStoreOut || emptyOut == unreadableOut {
		t.Errorf("an empty inbox and an inbox that could not be read render identically:\n%s", emptyOut)
	}
	for _, out := range []string{noStoreErr, unreadableErr} {
		if !strings.Contains(out, "NOT an empty inbox") {
			t.Errorf("a failure to read does not distinguish itself from an empty inbox:\n%s", out)
		}
	}
	// AND THE PROJECT'S STANDING RULE. "Could not determine" and "determined to be nothing" must
	// never share an exit code: the empty inbox is a determined nothing, the unreadable one is not
	// determined at all.
	if unreadableCode != cli.ExitUndetermined {
		t.Errorf("an inbox whose count could not be read exited %d; want %d so that it can never be "+
			"scripted as 'you have no tickets'", unreadableCode, cli.ExitUndetermined)
	}
	if noStoreOut == unreadableOut && noStoreErr == unreadableErr {
		t.Errorf("no store at all and a store that will not open render identically")
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 4 AND 5 — no message-shaped items, and no priority for one to sit at.
// ---------------------------------------------------------------------------

// The driver seeds tickets whose SOURCE MATERIAL included acknowledgements, then asserts on what a
// person sees. It asserts on the rendered listing rather than on the type, because criterion 4 is
// about what is listed.
func TestNoListedTitleIsTheVerbatimBodyOfAMessage(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s,
		inbox.Ticket{ID: "ana-sso", Title: inbox.Text("Restore Ana's login after the SSO change"),
			Summary: inbox.Text("Five emails, a chat thread and a follow-up ping about one broken login."),
			Channel: inbox.Text("email")},
		inbox.Ticket{ID: "vendor-invoice", Title: inbox.Text("Approve the Q3 vendor invoice"),
			Summary: inbox.Text("Finance has asked twice."), Channel: inbox.Text("teams")},
	)
	// The acknowledgements from the same traffic, offered to the inbox and refused.
	for _, body := range []string{"yes", "ok", "Hii", "Thanks", "ok 👍", "OK!"} {
		if err := inbox.Put(s, inbox.Ticket{ID: "ack", Title: inbox.Text(body), Summary: inbox.Text(body)}); err == nil {
			t.Errorf("the inbox accepted a ticket titled %q", body)
		}
	}

	code, stdout, stderr := runInboxCmd(t, env, "list")
	if code != cli.Success {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	for _, body := range []string{"yes", "ok", "Hii", "Thanks"} {
		if strings.Contains(stdout, "title:   "+body) {
			t.Errorf("the listing surfaced the raw message %q as a ticket title:\n%s", body, stdout)
		}
	}
	if n := strings.Count(stdout, "\nticket "); n != 2 {
		t.Errorf("the listing shows %d tickets; one broken login and one invoice are two obligations, "+
			"not a row per message:\n%s", n, stdout)
	}
	// CRITERION 5: nothing in what a person sees is a priority, a rank or a position an
	// acknowledgement could be filed at.
	for _, word := range []string{"priority", "rank", "severity", "urgency", "score", "low-priority", "acknowledg"} {
		if strings.Contains(strings.ToLower(stdout), word) {
			t.Errorf("the listing shows %q. Acknowledgements are not low-priority tickets; there is "+
				"no priority for one to be at (PRD §3.2):\n%s", word, stdout)
		}
	}
}

// ---------------------------------------------------------------------------
// CRITERION 6 — enumerate the operations; none publishes.
// ---------------------------------------------------------------------------

// The command's own help is BUILT from the enumeration, so this asserts that the operations a
// person is offered are exactly the operations the enumeration admits to — no fourth verb that the
// dispatcher accepts and the enumeration does not mention.
func TestTheCommandOffersExactlyTheEnumeratedOperationsAndNonePublishes(t *testing.T) {
	env, _, _ := inboxEnv(t)
	code, stdout, _ := runInboxCmd(t, env, "--help")
	if code != cli.Success {
		t.Fatalf("help exited %d", code)
	}
	for _, op := range inbox.Operations() {
		if !strings.Contains(stdout, op.Name) {
			t.Errorf("the help does not offer the enumerated operation %q:\n%s", op.Name, stdout)
		}
	}
	for _, verb := range []string{"publish", "share", "send", "upload", "export", "push"} {
		// The help SAYS these do not exist, so a bare substring match would find the sentence that
		// says so. What must not exist is one of them as an operation.
		c, _, _ := runInboxCmd(t, env, verb, "some-ticket")
		if c != cli.ExitUsage {
			t.Errorf("`omw inbox %s` exited %d rather than being refused as no operation at all", verb, c)
		}
	}
}

// Every operation the enumeration lists is wired up. Without this, criterion 6's enumeration could
// be trimmed to make the assertions pass while the dispatcher still answered a fourth verb.
func TestEveryEnumeratedOperationIsDispatched(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s, inbox.Ticket{ID: "t1", Title: inbox.Text("an obligation"), Summary: inbox.Text("...")})
	for _, op := range inbox.Operations() {
		args := []string{op.Name}
		if op.Name != "list" {
			args = append(args, "t1")
		}
		code, _, errOut := runInboxCmd(t, env, args...)
		if code == cli.ExitUsage {
			t.Errorf("operation %q is enumerated but the command does not accept it: %s", op.Name, errOut)
		}
		if op.Name != "delete" {
			continue
		}
		// Put it back for any later operation in the enumeration.
		seed(t, s, inbox.Ticket{ID: "t1", Title: inbox.Text("an obligation"), Summary: inbox.Text("...")})
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 7, 8 AND 13 — with a hub configured and reachable, nothing reaches it.
// ---------------------------------------------------------------------------

func TestNoInboxOperationTouchesAConfiguredHub(t *testing.T) {
	var requests atomic.Int64
	var published atomic.Int64
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			published.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hub.Close()
	// The hub is reachable — proved, not assumed, so that a zero count below is evidence about the
	// inbox and not about a server that was never listening.
	resp, err := http.Get(hub.URL)
	if err != nil {
		t.Fatalf("the test's own hub is not reachable, so this would assert nothing: %v", err)
	}
	_ = resp.Body.Close()
	if requests.Load() != 1 {
		t.Fatalf("the test's own hub did not count its own request")
	}
	requests.Store(0)
	published.Store(0)

	env, _, s := inboxEnv(t)
	seed(t, s, inbox.Ticket{ID: "t1", Title: inbox.Text("an obligation"), Summary: inbox.Text("...")})
	// Configured under every name a hub could plausibly be configured under, so that the assertion
	// does not depend on guessing which one a later Issue picks.
	for _, key := range []string{"OMW_HUB", "OMW_HUB_URL", "OMW_SERVER", "OMW_ENDPOINT"} {
		env[key] = hub.URL
	}

	for _, args := range [][]string{{"list"}, {"read", "t1"}, {"delete", "t1"}} {
		before := requests.Load()
		code, _, errOut := runInboxCmd(t, env, args...)
		if code != cli.Success {
			t.Fatalf("`omw inbox %s` exited %d with a hub configured; the local half stands alone "+
				"(PRD §4.4)\nstderr: %s", strings.Join(args, " "), code, errOut)
		}
		if got := requests.Load() - before; got != 0 {
			t.Errorf("`omw inbox %s` made %d request(s) to the hub", strings.Join(args, " "), got)
		}
	}
	if published.Load() != 0 {
		t.Errorf("the hub received %d write(s); tickets are never published (PRD §2.3)", published.Load())
	}
	if requests.Load() != 0 {
		t.Errorf("the hub was contacted %d time(s) across list, read and delete", requests.Load())
	}
}

// CRITERION 13's second half, and CRITERION 14: with NO hub configured, everything works and
// nothing reaches out. The listening server here is configured nowhere; if any inbox operation
// discovered it, it would be discovering it by some route other than configuration.
func TestWithNoHubConfiguredEveryInboxOperationWorksAndNothingReachesOut(t *testing.T) {
	var requests atomic.Int64
	hub := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer hub.Close()

	env, _, s := inboxEnv(t) // no hub key of any kind
	seed(t, s, inbox.Ticket{ID: "t1", Title: inbox.Text("an obligation"), Summary: inbox.Text("...")})
	for _, args := range [][]string{{"list"}, {"read", "t1"}, {"delete", "t1"}} {
		code, _, errOut := runInboxCmd(t, env, args...)
		if code != cli.Success {
			t.Errorf("`omw inbox %s` exited %d with no hub configured; every local capability works "+
				"with no hub (PRD §4.4)\nstderr: %s", strings.Join(args, " "), code, errOut)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("%d outbound request(s) with no hub configured", requests.Load())
	}
}

// ---------------------------------------------------------------------------
// CRITERIA 9 AND 10 — nothing expires; only the person's delete removes a ticket.
// ---------------------------------------------------------------------------

func TestExercisingEveryOperationLeavesTheTicketSetIdentical(t *testing.T) {
	env, _, s := inboxEnv(t)
	// Backdated past any plausible expiry window — a century, and a year, and an hour.
	seed(t, s,
		inbox.Ticket{ID: "ancient", Title: inbox.Text("owed for a century"), Summary: inbox.Text("..."),
			Arrived: time.Now().Add(-100 * 365 * 24 * time.Hour)},
		inbox.Ticket{ID: "old", Title: inbox.Text("owed for a year"), Summary: inbox.Text("..."),
			Arrived: time.Now().Add(-400 * 24 * time.Hour)},
		inbox.Ticket{ID: "recent", Title: inbox.Text("owed since this morning"), Summary: inbox.Text("..."),
			Arrived: time.Now().Add(-time.Hour)},
	)
	idsOf := func() []string {
		ts, err := inbox.List(s)
		if err != nil {
			t.Fatalf("listing: %v", err)
		}
		var out []string
		for _, tk := range ts {
			out = append(out, tk.ID)
		}
		return out
	}
	before := strings.Join(idsOf(), ",")

	// Every operation except a delete of one of them.
	if code, _, e := runInboxCmd(t, env, "list"); code != cli.Success {
		t.Fatalf("list exited %d: %s", code, e)
	}
	for _, id := range []string{"ancient", "old", "recent"} {
		if code, _, e := runInboxCmd(t, env, "read", id); code != cli.Success {
			t.Fatalf("a ticket owed for a long time is no longer readable: read %s exited %d: %s", id, code, e)
		}
	}
	runInboxCmd(t, env, "delete", "never-existed") // a refused delete
	runInboxCmd(t, env, "list")

	if after := strings.Join(idsOf(), ","); after != before {
		t.Fatalf("the ticket set changed from [%s] to [%s] without the person deleting anything", before, after)
	}
	// And then the person deletes one, which is the only thing that changes it.
	if code, _, e := runInboxCmd(t, env, "delete", "old"); code != cli.Success {
		t.Fatalf("delete exited %d: %s", code, e)
	}
	if after := strings.Join(idsOf(), ","); after != "ancient,recent" {
		t.Errorf("after the person deleted 'old' the inbox holds [%s]", after)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 11 — delete removes exactly what was named.
// ---------------------------------------------------------------------------

func TestDeleteRemovesExactlyThatTicketAndRefusesAnUnknownOne(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s,
		inbox.Ticket{ID: "one", Title: inbox.Text("first"), Summary: inbox.Text("...")},
		inbox.Ticket{ID: "two", Title: inbox.Text("second"), Summary: inbox.Text("...")},
		inbox.Ticket{ID: "three", Title: inbox.Text("third"), Summary: inbox.Text("...")},
	)
	okCode, _, _ := runInboxCmd(t, env, "delete", "two")
	if okCode != cli.Success {
		t.Fatalf("deleting a ticket that is there exited %d", okCode)
	}
	_, listOut, _ := runInboxCmd(t, env, "list")
	if strings.Contains(listOut, "ticket two") {
		t.Errorf("the deleted ticket is still listed:\n%s", listOut)
	}
	for _, id := range []string{"one", "three"} {
		if !strings.Contains(listOut, "ticket "+id) {
			t.Errorf("ticket %q is missing after deleting a different one:\n%s", id, listOut)
		}
	}
	if code, _, _ := runInboxCmd(t, env, "read", "two"); code == cli.Success {
		t.Errorf("the deleted ticket is still readable")
	}

	badCode, badOut, badErr := runInboxCmd(t, env, "delete", "never-existed")
	if badCode == cli.Success {
		t.Errorf("deleting an identifier that is not in the inbox exited 0")
	}
	if badCode == okCode {
		t.Errorf("a delete that removed a ticket and one that did not share exit code %d", okCode)
	}
	if strings.Contains(strings.ToLower(badOut), "deleted") {
		t.Errorf("a refused delete told the person something was deleted:\n%s", badOut)
	}
	if !strings.Contains(badErr, "nothing") {
		t.Errorf("a refused delete does not say nothing was deleted:\n%s", badErr)
	}
	_, afterOut, _ := runInboxCmd(t, env, "list")
	if afterOut != listOut {
		t.Errorf("a refused delete changed the listing:\nbefore:\n%s\nafter:\n%s", listOut, afterOut)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 12 — three renderings, compared pairwise.
// ---------------------------------------------------------------------------

func TestARealValueANegativeOneAndAnUndeterminedOneAreThreeDistinctOutputs(t *testing.T) {
	env, _, s := inboxEnv(t)
	seed(t, s,
		inbox.Ticket{ID: "real", Title: inbox.Text("t"),
			Summary: inbox.Text("The summary somebody wrote."), Channel: inbox.Text("email")},
		inbox.Ticket{ID: "negative", Title: inbox.Text("t"),
			Summary: inbox.Absent(), Channel: inbox.Absent()},
		inbox.Ticket{ID: "unknown", Title: inbox.Text("t"),
			Summary: inbox.Undetermined("the summary has not been written yet"),
			Channel: inbox.Undetermined("the source channel could not be read")},
	)
	read := func(id string) string {
		code, out, errOut := runInboxCmd(t, env, "read", id)
		if code != cli.Success {
			t.Fatalf("read %s exited %d: %s", id, code, errOut)
		}
		return out
	}
	renders := map[string]map[string]string{}
	for _, id := range []string{"real", "negative", "unknown"} {
		out := read(id)
		renders[id] = map[string]string{
			"summary:": strings.TrimSpace(lineWith(out, "summary:")),
			"channel:": strings.TrimSpace(lineWith(out, "channel:")),
		}
	}
	// PAIRWISE, AND NEVER AGAINST A LITERAL. Asserting each against its expected wording passes
	// just as happily after two of them have been edited to say the same thing.
	for _, field := range []string{"summary:", "channel:"} {
		a, b, c := renders["real"][field], renders["negative"][field], renders["unknown"][field]
		for _, pair := range [][2]string{{a, b}, {a, c}, {b, c}} {
			if pair[0] == pair[1] {
				t.Errorf("two of the three renderings of %s are the same: %q", field, pair[0])
			}
		}
		for _, v := range []string{a, b, c} {
			if v == "" || strings.TrimSpace(strings.TrimPrefix(v, field)) == "" {
				t.Errorf("a rendering of %s is silence; silence is not one of the three answers", field)
			}
		}
	}
	// The undetermined one names WHY, so that "could not be determined" is not the end of it.
	if !strings.Contains(read("unknown"), "not been written yet") {
		t.Errorf("the undetermined summary does not say what could not be determined:\n%s", read("unknown"))
	}
}

// The project's standing rule, asserted across the command's whole range of answers rather than in
// one place: no code means both "I could not determine this" and "I determined there is nothing".
func TestCouldNotDetermineAndDeterminedToBeNothingNeverShareAnExitCode(t *testing.T) {
	emptyEnv, _, _ := inboxEnv(t)
	determinedNothing, _, _ := runInboxCmd(t, emptyEnv, "list") // the inbox is empty: a real answer

	noStoreEnv := map[string]string{"HOME": t.TempDir()} // no OMW_STORE, no HOME store, nothing
	couldNotDetermine, _, _ := runInboxCmd(t, noStoreEnv, "list")

	if determinedNothing != cli.Success {
		t.Errorf("'the inbox is empty' exited %d; it is an answer and answering succeeds", determinedNothing)
	}
	if couldNotDetermine == determinedNothing {
		t.Errorf("'could not determine' and 'determined to be nothing' share exit code %d", determinedNothing)
	}
}

// ---------------------------------------------------------------------------
// CRITERION 13 — with no daemon running, an inbox command does not start it.
// ---------------------------------------------------------------------------

func TestAnInboxCommandSaysTheDaemonIsNotRunningAndDoesNotStartIt(t *testing.T) {
	env, root, s := inboxEnv(t)
	seed(t, s, inbox.Ticket{ID: "t1", Title: inbox.Text("an obligation"), Summary: inbox.Text("...")})
	sock := filepath.Join(root, inbox.ControlSocketName)
	if _, err := os.Stat(sock); err == nil {
		t.Fatalf("a control socket exists before anything ran, so this asserts nothing")
	}
	for _, args := range [][]string{{"list"}, {"read", "t1"}, {"delete", "t1"}} {
		code, stdout, errOut := runInboxCmd(t, env, args...)
		if code != cli.Success {
			t.Fatalf("`omw inbox %s` exited %d: %s", strings.Join(args, " "), code, errOut)
		}
		if args[0] != "list" {
			continue
		}
		if !strings.Contains(stdout, "not running") {
			t.Errorf("the listing does not say the daemon is not running:\n%s", stdout)
		}
		if !strings.Contains(stdout, "not a live inbox") {
			t.Errorf("the listing does not say it read the store rather than a live inbox:\n%s", stdout)
		}
	}
	if _, err := os.Stat(sock); err == nil {
		t.Errorf("an inbox command started the daemon; nothing is implicit (PRD §4.2)")
	}
}

// ---------------------------------------------------------------------------
// CRITERION 15 — the control API, and what it says when it will not open.
// ---------------------------------------------------------------------------

// PROBED, NOT NAMED. Whether this environment can hold a unix socket at this path is discovered by
// trying to make one; the test does not ask which operating system it is on, because PRD §5.1 ships
// two of them and an assertion that names one says nothing about the other.
func TestWhenOwnerOnlyPermissionsCannotBeConfirmedTheControlAPIDoesNotOpenAndSaysSo(t *testing.T) {
	env, root := shortPathInboxEnv(t)
	sock := filepath.Join(root, inbox.ControlSocketName)
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("a unix socket cannot be created at %s here, so this criterion cannot be staged: %v", sock, err)
	}
	defer l.Close()

	// Owner-only: the control API may open.
	if err := os.Chmod(sock, 0o600); err != nil {
		t.Fatalf("setting owner-only permissions: %v", err)
	}
	_, ownerOnly, _ := runInboxCmd(t, env, "list")

	// Not owner-only: it must NOT open, and must say which.
	if err := os.Chmod(sock, 0o666); err != nil {
		t.Skipf("this filesystem does not honour a permission change on a socket: %v", err)
	}
	if info, serr := os.Stat(sock); serr != nil || info.Mode().Perm()&0o077 == 0 {
		t.Skipf("the permission change did not take on this filesystem, so nothing would be asserted")
	}
	_, wideOpen, _ := runInboxCmd(t, env, "list")

	// And an empty inbox with no socket at all, to compare all three against.
	emptyEnv, _, _ := inboxEnv(t)
	_, emptyInbox, _ := runInboxCmd(t, emptyEnv, "list")

	line := func(s string) string { return strings.TrimSpace(lineWith(s, "control api:")) }
	if line(ownerOnly) == "" || line(wideOpen) == "" || line(emptyInbox) == "" {
		t.Fatalf("the control API is not reported at all:\n%s", emptyInbox)
	}
	if line(ownerOnly) == line(wideOpen) {
		t.Errorf("an owner-only socket and one anybody can reach report the same control API state: %q", line(ownerOnly))
	}
	if !strings.Contains(wideOpen, "owner-only") && !strings.Contains(wideOpen, "not open") {
		t.Errorf("the command does not say why the control API did not open:\n%s", wideOpen)
	}
	// Distinguishable from an empty inbox, which is the confusion criterion 15 names.
	if wideOpen == emptyInbox {
		t.Errorf("a control API that would not open renders identically to an empty inbox")
	}
	if strings.Contains(strings.ToLower(line(wideOpen)), "hub") {
		t.Errorf("the control API's refusal is worded as a hub error: %q", line(wideOpen))
	}
}

// ---------------------------------------------------------------------------
// Usage.
// ---------------------------------------------------------------------------

func TestInboxWithNoOperationIsAUsageError(t *testing.T) {
	env, _, _ := inboxEnv(t)
	if code, _, _ := runInboxCmd(t, env); code != cli.ExitUsage {
		t.Errorf("`omw inbox` with no operation exited %d; want %d", code, cli.ExitUsage)
	}
	if code, _, errOut := runInboxCmd(t, env, "read"); code != cli.ExitUsage {
		t.Errorf("`omw inbox read` with no identifier exited %d: %s", code, errOut)
	}
	if code, _, _ := runInboxCmd(t, env, "delete", "a", "b"); code != cli.ExitUsage {
		t.Errorf("`omw inbox delete` with two identifiers exited %d", code)
	}
}

// The command file itself imports nothing that could reach a hub. Structural, and labelled as such:
// the behavioural assertion is TestNoInboxOperationTouchesAConfiguredHub above.
func TestTheInboxCommandImportsNothingThatCouldReachAHub(t *testing.T) {
	b, err := os.ReadFile("inbox.go")
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
			t.Errorf("internal/commands/inbox.go imports %s; the inbox has no route to a hub", imp)
		}
	}
}
