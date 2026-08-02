package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// newTestStore makes a real store in a directory of this test's own.
func newTestStore(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := store.Create(root); err != nil {
		t.Fatalf("could not create a store to test against: %v", err)
	}
	return root
}

// startTestDaemon starts a daemon whose write probe always succeeds and whose interval is short,
// and guarantees it is closed when the test ends.
func startTestDaemon(t *testing.T, root string, opts ...func(*Options)) *Daemon {
	t.Helper()
	o := Options{StorePath: root, Interval: 5 * time.Millisecond, Write: func() error { return nil }}
	for _, f := range opts {
		f(&o)
	}
	d, err := Start(o)
	if err != nil {
		t.Fatalf("the daemon did not start against %s: %v", root, err)
	}
	t.Cleanup(d.Close)
	return d
}

// serveInBackground runs the daemon's loop and gives back a channel carrying why it stopped.
func serveInBackground(t *testing.T, d *Daemon) <-chan error {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- d.Serve() }()
	// Wait until the first write has been proved, so a test that then asserts on health is
	// asserting on a daemon that has actually run rather than on one that has merely started.
	waitFor(t, "the daemon to prove it can write", func() bool { return d.Report().Healthy == tri.Yes })
	return ch
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestTheFourEndingsRenderDistinguishablyEndToEnd is criteria 10, 11 and 12 driven through the
// real lifecycle, and compared PAIRWISE.
//
// Each ending is produced by actually doing the thing — never running, stopping on purpose, losing
// the ability to write, and vanishing without recording an ending — and then the rendered report a
// person would read is captured. Comparing every pair of those renderings is what makes two of
// them collapsing into one sentence a failure; four assertions against four literals would not.
func TestTheFourEndingsRenderDistinguishablyEndToEnd(t *testing.T) {
	renderings := map[string]string{}
	endings := map[string]Ending{}

	record := func(name string, root string) {
		rep := Inspect(root)
		var b strings.Builder
		if _, err := rep.WriteTo(&b); err != nil {
			t.Fatalf("%s: rendering the report failed: %v", name, err)
		}
		// The store path differs per case and would make every rendering trivially distinct, so it
		// is removed: what is being compared is what the report SAYS, not where it was taken.
		renderings[name] = strings.ReplaceAll(b.String(), root, "<store>")
		endings[name] = rep.LastRun
	}

	// NEVER RUN — criterion 11.
	record("never run", newTestStore(t))

	// ENDED BY AN EXPLICIT STOP — criterion 10.
	{
		root := newTestStore(t)
		d, err := Start(Options{StorePath: root, Interval: 5 * time.Millisecond, Write: func() error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		done := serveInBackground(t, d)
		d.Stop()
		if err := <-done; err != nil {
			t.Fatalf("an explicit stop reported a failure: %v", err)
		}
		d.Close()
		record("explicit stop", root)
	}

	// ENDED BECAUSE IT COULD NOT WRITE — criterion 10, and criterion 16.
	{
		root := newTestStore(t)
		fail := errors.New("no space left on device")
		// Writable when it starts, and not afterwards: the failure has to arrive while the daemon
		// is running, or this would be testing the refusal to start rather than the stop.
		var writes int
		d, err := Start(Options{StorePath: root, Interval: 5 * time.Millisecond, Write: func() error {
			writes++
			if writes == 1 {
				return nil
			}
			return fail
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Serve(); !errors.Is(err, fail) {
			t.Fatalf("Serve returned %v; a daemon that cannot write must stop and say why", err)
		}
		d.Close()
		record("cannot write", root)
	}

	// ENDED WITHOUT RECORDING AN ENDING — criterion 10's third rendering. The daemon vanishes: the
	// lock goes and nothing is written, which is exactly what a killed process leaves behind.
	{
		root := newTestStore(t)
		d := startTestDaemon(t, root)
		d.control.Close()
		d.lock.release()
		record("crashed", root)
	}

	// UNREADABLE RECORD — criterion 12, which must be tellable apart from all four above.
	{
		root := newTestStore(t)
		d := startTestDaemon(t, root)
		serveInBackground(t, d)
		d.Close()
		p := pathsFor(root)
		if err := os.WriteFile(p.state, []byte("{this is not a run record"), 0o600); err != nil {
			t.Fatal(err)
		}
		record("unreadable", root)
	}

	wantEndings := map[string]Ending{
		"never run":     EndingNeverRun,
		"explicit stop": EndingStopped,
		"cannot write":  EndingCannotWrite,
		"crashed":       EndingCrashed,
		"unreadable":    EndingUndetermined,
	}
	for name, want := range wantEndings {
		if endings[name] != want {
			t.Errorf("%s produced Ending(%d) (%q); expected %q", name, endings[name], endings[name], want)
		}
	}

	// THE PAIRWISE COMPARISON. Any two of these five rendering the same way is the defect.
	names := []string{"never run", "explicit stop", "cannot write", "crashed", "unreadable"}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			a, b := names[i], names[j]
			if renderings[a] == renderings[b] {
				t.Errorf("%q and %q produce IDENTICAL reports:\n%s", a, b, renderings[a])
			}
			if lineOf(renderings[a], "last run:") == lineOf(renderings[b], "last run:") {
				t.Errorf("%q and %q report the same last-run line %q; criteria 10–12 require these told apart",
					a, b, lineOf(renderings[a], "last run:"))
			}
		}
	}
	for _, n := range names {
		if strings.TrimSpace(lineOf(renderings[n], "last run:")) == "last run:" {
			t.Errorf("%q rendered a blank last-run value; §4.3 forbids silence as an answer", n)
		}
	}
}

func lineOf(text, prefix string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// TestTheLastRunFactSurvivesTheProcessThatProducedIt is criterion 13: asking a NOT RUNNING daemon
// for its state does not start it, and still reports how the last run ended.
func TestTheLastRunFactSurvivesTheProcessThatProducedIt(t *testing.T) {
	root := newTestStore(t)
	d := startTestDaemon(t, root)
	done := serveInBackground(t, d)
	d.Stop()
	<-done
	d.Close()

	rep := Inspect(root)
	if rep.Running != tri.No {
		t.Fatalf("after a stop the daemon reports %v; criterion 2 says it is no longer running", rep.Running)
	}
	if rep.LastRun != EndingStopped {
		t.Errorf("the last run reads as %q after the process that produced it is gone; expected %q",
			rep.LastRun, EndingStopped)
	}
	// AND IT STARTED NOTHING (criterion 18): the store's lock is still free afterwards, which it
	// would not be if Inspect had brought a daemon up.
	if again := Inspect(root); again.Running != tri.No {
		t.Errorf("asking for the state twice left something running (%v); no command starts the daemon", again.Running)
	}
}

// TestAcrossRunsTheEndingIsTheOneBeforeThisOne pins the carrying-forward: a daemon that is running
// still reports how the PREVIOUS run ended, and reports the same thing the disk does.
func TestAcrossRunsTheEndingIsTheOneBeforeThisOne(t *testing.T) {
	root := newTestStore(t)

	first := startTestDaemon(t, root)
	done := serveInBackground(t, first)
	first.Stop()
	<-done
	first.Close()

	second := startTestDaemon(t, root)
	serveInBackground(t, second)
	rep := second.Report()
	if rep.LastRun != EndingStopped {
		t.Errorf("while a second run is in progress the last run reads as %q; expected %q", rep.LastRun, EndingStopped)
	}
	if disk := Inspect(root); disk.LastRun != rep.LastRun {
		t.Errorf("the daemon says the last run %q and a reader of the store says %q", rep.LastRun, disk.LastRun)
	}
}

// TestARunRecordThatCannotBeReadIsNeverAnAbsentOne is criterion 12 against the ways a record fails
// to be readable, not only the one that was easiest to write.
//
// FOUND BY MUTATION. Turning the "the file would not open" branch into "there is no record" left
// every test green, because the only unreadable record any test produced was one that opened fine
// and would not PARSE. Those are two branches and criterion 12 covers both: a record that exists
// and cannot be read is undetermined, and "never run" is a claim that nothing was ever there.
func TestARunRecordThatCannotBeReadIsNeverAnAbsentOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(t *testing.T, statePath string)
	}{
		{"it will not parse", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"it will not open", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			// A directory where the record should be: present, and no read of it can succeed.
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"it is a record this build does not understand", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(`{"format":99,"phase":"whatever"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newTestStore(t)
			d := startTestDaemon(t, root)
			done := serveInBackground(t, d)
			d.Stop()
			<-done
			d.Close()

			tc.spoil(t, pathsFor(root).state)
			rep := Inspect(root)
			if rep.LastRun == EndingNeverRun {
				t.Errorf("a run record that is present and unreadable was reported as never having run; that is an absence claimed from a failure to look")
			}
			if rep.LastRun != EndingUndetermined {
				t.Errorf("the ending reads as %q; a record that cannot be read is undetermined", rep.LastRun)
			}
			if rep.LastRunDetail == "" {
				t.Error("an undetermined ending was reported as silence; §4.3 forbids that")
			}
		})
	}
}

// TestStartingAgainstAMissingStoreCreatesNothing is criterion 3.
func TestStartingAgainstAMissingStoreCreatesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-a-store")
	_, err := Start(Options{StorePath: missing, Write: func() error { return nil }})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("starting against a missing store gave %v; expected %v so that the CLI names the store", err, store.ErrNotFound)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Errorf("starting against %s brought something into being; §4.2 says the store is created explicitly and by nothing else", missing)
	}
}

// TestASecondDaemonAgainstTheSameStoreIsRefusedAndTheFirstIsUnaffected is criteria 5 and 6.
func TestASecondDaemonAgainstTheSameStoreIsRefusedAndTheFirstIsUnaffected(t *testing.T) {
	root := newTestStore(t)
	first := startTestDaemon(t, root)
	serveInBackground(t, first)

	_, err := Start(Options{StorePath: root, Write: func() error { return nil }})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("a second daemon against the same store gave %v; expected %v", err, ErrLockHeld)
	}
	// CRITERION 6: it is not the missing-store failure and not a write failure. Distinguishable by
	// value, which is what lets the CLI give it its own sentence.
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrLockUndetermined) {
		t.Errorf("the lock conflict is indistinguishable from another start failure: %v", err)
	}
	// CRITERION 5, the verifiable half: the first daemon still answers on its control API.
	if first.Control() == nil {
		t.Skip("the control API did not open here, so 'still answering' cannot be observed; see the owner-only tests")
	}
	rep, qerr := queryControl(first.Control().Path())
	if qerr != nil {
		t.Fatalf("the first daemon stopped answering after a second was refused: %v", qerr)
	}
	if rep.Running != tri.Yes || rep.Healthy != tri.Yes {
		t.Errorf("after refusing a second daemon the first reports running=%v healthy=%v; it should be unaffected",
			rep.Running, rep.Healthy)
	}
}

// TestTwoDaemonsAgainstTwoStoresBothRun is criterion 7: the lock is per store, not per machine.
func TestTwoDaemonsAgainstTwoStoresBothRun(t *testing.T) {
	a, b := newTestStore(t), newTestStore(t)
	da := startTestDaemon(t, a)
	db := startTestDaemon(t, b)
	serveInBackground(t, da)
	serveInBackground(t, db)

	for name, d := range map[string]*Daemon{"the first": da, "the second": db} {
		if rep := d.Report(); rep.Running != tri.Yes || rep.Healthy != tri.Yes {
			t.Errorf("%s daemon reports running=%v healthy=%v; two stores on one machine both get a daemon",
				name, rep.Running, rep.Healthy)
		}
	}
	if Inspect(a).PID == Inspect(b).PID && Inspect(a).StorePath == Inspect(b).StorePath {
		t.Error("the two daemons are reported as the same one; the lock must be per store")
	}
}

// TestAStaleLockIsNeverReportedAsALiveDaemon is criterion 8.
//
// The lock file is left behind exactly as a killed daemon leaves it — content and all — and
// nothing holds the advisory lock, which is the state the kernel leaves after the holder dies.
func TestAStaleLockIsNeverReportedAsALiveDaemon(t *testing.T) {
	root := newTestStore(t)
	p := pathsFor(root)
	if err := ensureRunDir(p); err != nil {
		t.Fatal(err)
	}
	// A pid that is not this process and is almost certainly not alive. The assertion below does
	// not depend on that guess: it depends on nothing holding the lock.
	stale := `{"pid":999999,"started_at":"2020-01-01T00:00:00Z"}`
	if err := os.WriteFile(p.lock, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	if rep := Inspect(root); rep.Running != tri.No {
		t.Errorf("a lock file left by a dead process reports the daemon as %v; criterion 8 says it is never a live conflicting daemon", rep.Running)
	}

	d, err := Start(Options{StorePath: root, Interval: 5 * time.Millisecond, Write: func() error { return nil }})
	if err != nil {
		t.Fatalf("a start against a store with a stale lock failed permanently with %v; criterion 8 forbids that", err)
	}
	t.Cleanup(d.Close)
	if !d.StaleLockFound {
		t.Error("the stale lock was taken over silently; criterion 8 asks that it be reported precisely")
	}
	if d.StaleLockDetail == "" {
		t.Error("a stale lock was found and nothing was said about it")
	}
	if strings.Contains(strings.ToLower(d.StaleLockDetail), "another daemon") {
		t.Errorf("the stale lock is described as another daemon: %q", d.StaleLockDetail)
	}
}

// TestAHeldLockIsNotReportedAsStale is the other half of criterion 8, and the reason the test
// above is not satisfied by code that simply always says "stale".
func TestAHeldLockIsNotReportedAsStale(t *testing.T) {
	root := newTestStore(t)
	first := startTestDaemon(t, root)
	serveInBackground(t, first)

	_, err := Start(Options{StorePath: root, Write: func() error { return nil }})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("a live holder produced %v, not %v", err, ErrLockHeld)
	}
	if strings.Contains(strings.ToLower(err.Error()), "stale") {
		t.Errorf("a lock held by a living daemon was described as stale: %v", err)
	}
}

// TestTheDaemonStopsWhenItCannotWrite is criteria 15 and 16.
func TestTheDaemonStopsWhenItCannotWrite(t *testing.T) {
	root := newTestStore(t)
	var mu sync.Mutex
	failing := false
	boom := errors.New("read-only file system")
	d := startTestDaemon(t, root, func(o *Options) {
		o.Write = func() error {
			mu.Lock()
			defer mu.Unlock()
			if failing {
				return boom
			}
			return nil
		}
	})
	done := serveInBackground(t, d)

	mu.Lock()
	failing = true
	mu.Unlock()

	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("the daemon stopped for %v; expected the write failure %v", err, boom)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon kept running after it could not write; §4.3 says it stops")
	}
	d.Close()

	rep := Inspect(root)
	if rep.Running != tri.No {
		t.Errorf("after stopping for a write failure the daemon still reports %v", rep.Running)
	}
	if rep.LastRun != EndingCannotWrite {
		t.Errorf("the last run reads as %q; criterion 16 requires %q", rep.LastRun, EndingCannotWrite)
	}
	if !strings.Contains(rep.LastRunDetail, boom.Error()) {
		t.Errorf("the recorded reason %q does not name what actually failed", rep.LastRunDetail)
	}
}

// TestADaemonThatCannotWriteAtStartupDoesNotStart is the other half of criterion 15: the daemon
// never occupies a state in which it is running and cannot write, not even for one interval.
func TestADaemonThatCannotWriteAtStartupDoesNotStart(t *testing.T) {
	root := newTestStore(t)
	boom := errors.New("permission denied")
	d, err := Start(Options{StorePath: root, Interval: 5 * time.Millisecond, Write: func() error { return boom }})
	if err == nil {
		d.Close()
		t.Fatal("a daemon that cannot write started anyway; §4.3 says it stops rather than running in a state a person reads as healthy")
	}
	if !errors.Is(err, boom) {
		t.Errorf("the refusal to start does not carry the write failure: %v", err)
	}
	rep := Inspect(root)
	if rep.Running != tri.No {
		t.Errorf("after a start that could not write, the daemon reports %v", rep.Running)
	}
	if rep.LastRun != EndingCannotWrite {
		t.Errorf("the run reads as %q; it ended because it could not write (criterion 16)", rep.LastRun)
	}
}

// TestThereIsNoHealthyWindowAfterAWriteFails is criterion 17.
//
// A reader polls the daemon's state continuously while the write probe starts failing, and the
// assertion is a MONOTONICITY one: once a sample is not healthy, no later sample may be healthy,
// and the first not-healthy sample must arrive no later than the call that observed the failure
// returning. A test that merely checked the state after the daemon had exited would pass against
// code that reported "running, fine" for a full interval first.
func TestThereIsNoHealthyWindowAfterAWriteFails(t *testing.T) {
	root := newTestStore(t)
	var mu sync.Mutex
	failing := false
	boom := errors.New("disk full")
	d := startTestDaemon(t, root, func(o *Options) {
		o.Write = func() error {
			mu.Lock()
			defer mu.Unlock()
			if failing {
				return boom
			}
			return nil
		}
	})
	done := serveInBackground(t, d)

	stopPolling := make(chan struct{})
	var pollMu sync.Mutex
	var samples []tri.Value
	var poller sync.WaitGroup
	poller.Add(1)
	go func() {
		defer poller.Done()
		for {
			select {
			case <-stopPolling:
				return
			default:
			}
			s := d.Report().Healthy
			pollMu.Lock()
			samples = append(samples, s)
			pollMu.Unlock()
		}
	}()

	mu.Lock()
	failing = true
	mu.Unlock()

	if err := <-done; !errors.Is(err, boom) {
		t.Fatalf("the daemon stopped for %v, not for the write failure", err)
	}
	// THE DECISIVE SAMPLE: taken after the daemon has observed the failure. It must already be
	// not-healthy, with no scheduling in between that could have been given a "fine".
	afterObservation := d.Report()
	close(stopPolling)
	poller.Wait()

	if afterObservation.Healthy != tri.No {
		t.Errorf("the moment the daemon knew it could not write it reported health %v; criterion 17 requires no healthy state after that",
			afterObservation.Healthy)
	}
	if !strings.Contains(afterObservation.HealthDetail, boom.Error()) {
		t.Errorf("the unhealthy state does not name why: %q", afterObservation.HealthDetail)
	}

	pollMu.Lock()
	defer pollMu.Unlock()
	seenUnhealthy := false
	for i, s := range samples {
		if s == tri.No {
			seenUnhealthy = true
			continue
		}
		if seenUnhealthy && s == tri.Yes {
			t.Fatalf("sample %d reported healthy AFTER an unhealthy sample; there is an observable window in which the daemon says it is fine while writes fail", i)
		}
	}
	if !seenUnhealthy {
		t.Error("the poller never observed an unhealthy state, so this test proved nothing about the window")
	}
}

// TestARunningDaemonNeverClaimsHealthItHasNotEstablished covers the reader who has no control API.
//
// From outside the process, "something holds the lock" is establishable and "that something is
// fine" is not. Reporting the second from the first is the healthy-looking window criterion 17
// forbids, so the disk-only answer is UNDETERMINED — a real answer, and not one a person acts on
// as healthy.
func TestARunningDaemonNeverClaimsHealthItHasNotEstablished(t *testing.T) {
	got, detail := healthFromDisk(tri.Yes, "")
	if got == tri.Yes {
		t.Error("a reader outside the daemon claimed the daemon is healthy; nothing it can see establishes that")
	}
	if got != tri.Undetermined || detail == "" {
		t.Errorf("the disk-only health answer is %v with detail %q; it must be undetermined and say why", got, detail)
	}
	if got, detail := healthFromDisk(tri.No, ""); got != tri.No || detail == "" {
		t.Errorf("a daemon established as not running gave health %v (%q); that is a determined answer and it must be said", got, detail)
	}
}
