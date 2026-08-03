package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/extension"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// =================================================================================================
// CRITERION 9, THROUGH THE COMMAND A PERSON TYPES.
//
// "A channel adapter that failed to load never causes ingestion to report 'no traffic on this
// channel'. A test that breaks a registered adapter and then ASKS WHAT CHANNELS ARE INGESTING sees
// that adapter reported as failed, not as quiet."
//
// `channels.ExtensionFactory` already carried that, and `channels/extension_test.go` drove it by
// calling the factory directly — which passes over a function the daemon never reached, because
// `LoopFactory` defaulted to `Builtin`. The two surfaces then answered one fact differently:
// `omw ext list` said the teams adapter FAILED TO LOAD, and `omw channels list` said this build has
// no transport for Microsoft Teams. This test asks the question the way a person asks it — the real
// `omw channels list`, after the real daemon has ingested — so a build whose ingestion is not
// pointed at the extension mechanism fails here rather than in a unit test's private call.
// =================================================================================================

// brokenChannelExt is a channel adapter this machine offers and which will not load. It is the
// state a person is in after an upgrade that half-installed the adapter they registered.
type brokenChannelExt struct {
	name string
	why  error
}

func (b brokenChannelExt) Name() string                   { return b.name }
func (b brokenChannelExt) Interface() extension.Interface { return extension.Channel }
func (b brokenChannelExt) Load() error                    { return b.why }

// withBrokenRegistry points BOTH surfaces at one machine that offers a broken adapter: the `ext`
// command's registry and the daemon's ingestion. They must be the same registry, because the whole
// question here is whether the two agree about one fact.
func withBrokenRegistry(t *testing.T, exts ...extension.Extension) *extension.Registry {
	t.Helper()
	r := xtRegistry(t, exts...) // swaps the ext command's registry and puts it back
	prev := channels.LoopRegistry
	channels.LoopRegistry = r
	t.Cleanup(func() { channels.LoopRegistry = prev })
	return r
}

func TestChannelsListNamesTheFailedLoadRatherThanTheMissingTransport(t *testing.T) {
	const boom = "libteams.so refuses to load"

	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	env := map[string]string{store.PathEnv: root, "HOME": t.TempDir(), "OMW_HUB": ""}

	// A machine offering a BROKEN teams adapter, REGISTERED BY A DELIBERATE ACT — criterion 9 says
	// "a test that breaks a REGISTERED adapter", and an adapter nobody registered is a different
	// state with a different answer.
	r := withBrokenRegistry(t, brokenChannelExt{name: "teams", why: loadErr(boom)})
	if err := extension.Register(s, r, "teams", nil); err != nil {
		t.Fatalf("registering the teams adapter: %v", err)
	}

	code, _, stderr := runChannelsCmd(t, env, "connect", "teams",
		"--account", "ana@example.com", "--credential-file", credFile(t, "tok", time.Now().Add(24*time.Hour)))
	if code != cli.Success {
		t.Fatalf("connecting teams exited %d: %s", code, stderr)
	}

	// THE REAL DAEMON, doing the real background ingestion. Nothing is typed to make ingestion
	// happen (§3.1, criterion 4) and no factory is injected: this is the product's own path.
	d, derr := daemon.Start(daemon.Options{StorePath: root, Interval: 10 * time.Millisecond})
	if derr != nil {
		t.Fatalf("the daemon did not start: %v", derr)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Serve() }()
	t.Cleanup(func() { d.Close(); <-done })

	// Wait until a pass has recorded an attempt, then read what a person reads.
	var stdout string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, stdout, _ = runChannelsCmd(t, env, "list")
		if strings.Contains(strings.ToUpper(stdout), "FAILED TO LOAD") || strings.Contains(stdout, boom) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	block := chanBlock(stdout, "teams-ana-example-com")
	if block == "" {
		t.Fatalf("the connected teams channel is not listed at all:\n%s", stdout)
	}
	attempt := lineWith(block, "last attempt:")
	if attempt == "" {
		t.Fatalf("the listing does not say what the last attempt did:\n%s", block)
	}

	if !strings.Contains(strings.ToUpper(block), "FAILED TO LOAD") {
		t.Errorf("`omw channels list` does not say the registered teams adapter FAILED TO LOAD. A "+
			"person reading this cannot tell a broken installation from a build that never had the "+
			"transport, and only one of those is fixed by reinstalling:\n%s", block)
	}
	if !strings.Contains(block, boom) {
		t.Errorf("`omw channels list` does not name the load failure %q, so the reason the daemon "+
			"had is not the reason the person is given:\n%s", boom, block)
	}
	// The reason the wrong layer gives. `Builtin` answers about the BUILD — true of every teams
	// channel on this machine, registered adapter or not — and it is the sentence that appeared
	// here while the daemon's ingestion went straight to it.
	if strings.Contains(block, "this build has no transport") {
		t.Errorf("`omw channels list` blames the missing built-in transport for a REGISTERED "+
			"adapter that failed to load; the daemon's ingestion is not going through the "+
			"extension mechanism:\n%s", block)
	}
	// And never the quiet channel. This is the defect criterion 9 is named for.
	if strings.Contains(attempt, "it saw") {
		t.Errorf("the last attempt claims to have seen messages on a channel that was never "+
			"attempted:\n%s", attempt)
	}
}

// CRITERION 3, ACROSS THE TWO SURFACES: one fact, one reason. `omw ext list` and `omw channels
// list` are reached through different code — the inventory and ingestion's recorded outcome — and
// before this they gave two different reasons for the same broken adapter.
func TestExtListAndChannelsListGiveTheSameReasonForOneBrokenAdapter(t *testing.T) {
	const boom = "libteams.so refuses to load"

	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	env := map[string]string{store.PathEnv: root, "HOME": t.TempDir(), "OMW_HUB": ""}

	r := withBrokenRegistry(t, brokenChannelExt{name: "teams", why: loadErr(boom)})
	if err := extension.Register(s, r, "teams", nil); err != nil {
		t.Fatalf("registering the teams adapter: %v", err)
	}
	code, _, stderr := runChannelsCmd(t, env, "connect", "teams",
		"--account", "ana@example.com", "--credential-file", credFile(t, "tok", time.Now().Add(24*time.Hour)))
	if code != cli.Success {
		t.Fatalf("connecting teams exited %d: %s", code, stderr)
	}

	d, derr := daemon.Start(daemon.Options{StorePath: root, Interval: 10 * time.Millisecond})
	if derr != nil {
		t.Fatalf("the daemon did not start: %v", derr)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Serve() }()
	t.Cleanup(func() { d.Close(); <-done })

	var chanOut string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, chanOut, _ = runChannelsCmd(t, env, "list")
		if strings.Contains(chanOut, boom) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	extOut := runExtCmd(t, env, "list").all()

	// THE SAME FACT MUST APPEAR IN BOTH, IN THE SAME WORDS a person can act on: it failed to load,
	// and here is why. Compared pairwise on the two properties rather than by diffing the two
	// renderings, which are legitimately different shapes.
	for name, out := range map[string]string{"omw ext list": extOut, "omw channels list": chanOut} {
		if !strings.Contains(strings.ToUpper(out), "FAILED TO LOAD") {
			t.Errorf("`%s` does not report the teams adapter as having failed to load:\n%s", name, out)
		}
		if !strings.Contains(out, boom) {
			t.Errorf("`%s` does not give the load failure %q as the reason:\n%s", name, boom, out)
		}
	}
}

// loadErr is a load failure with a sentence of its own, and not one of this package's refusals: it
// stands in for whatever a third-party adapter's own Load returns.
func loadErr(msg string) error { return &plainErr{msg} }

type plainErr struct{ msg string }

func (e *plainErr) Error() string { return e.msg }
