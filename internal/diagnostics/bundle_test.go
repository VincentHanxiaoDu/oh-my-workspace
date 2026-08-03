package diagnostics

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/health"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// The three recognisable strings. They are nonsense on purpose: a substring search for them cannot
// match anything the product itself writes, so a hit is a hit and never a coincidence.
const (
	secretTicketBody  = "ZZQ-TICKET-BODY-4f1c9a2e-DO-NOT-DISCLOSE"
	secretDraftBody   = "ZZQ-DRAFT-BODY-8b3d7e15-DO-NOT-DISCLOSE"
	secretMessageBody = "ZZQ-MESSAGE-BODY-27ac6f90-DO-NOT-DISCLOSE"
	secretModelKey    = "ZZQ-MODEL-KEY-5e0b48d3-NEVER-IN-A-BUNDLE"
)

// seededStore creates a store holding one of each of the four things, and returns its root.
//
// THE CREDENTIAL IS A REAL RECORD IN A REAL STORE. Seeding it only in an environment variable would
// prove far less: the interesting failure is a bundle that dumps every kind in the store, and that
// failure is invisible to a test whose key never lived in the store.
func seededStore(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating a store at %s: %v", root, err)
	}
	put := func(kind store.Kind, id, body string) {
		t.Helper()
		if err := s.Put(store.Record{Kind: kind, ID: id, Data: []byte(body)}); err != nil {
			t.Fatalf("seeding a %s record: %v", kind, err)
		}
	}
	put(KindTicket, "t1", "subject line\n"+secretTicketBody+"\n")
	put(KindMessage, "m1", "from a channel\n"+secretMessageBody+"\n")
	put(KindModelCredential, "key", `{"provider":"acme","api_key":"`+secretModelKey+`"}`)

	// THE DRAFT IS SEEDED THE WAY THE PRODUCT WRITES ONE (Issue #67). It used to be put into a
	// store kind called "draft" that nothing in the product has ever written to, which made every
	// assertion below about drafts an assertion about a fixture. `omw outbox draft` revises a draft
	// in the outbox inside the store, so that is what this does.
	o, err := drafts.InStore(s)
	if err != nil {
		t.Fatalf("opening the outbox inside the seeded store: %v", err)
	}
	if _, err := o.Revise("d1", "draft heading\n"+secretDraftBody+"\n"); err != nil {
		t.Fatalf("seeding a draft: %v", err)
	}
	return root
}

// envFor gives a Getenv that points the product at a store and configures nothing else. HOME and
// XDG_DATA_HOME are pointed inside the test's own directory so nothing here can reach, or rewrite,
// the developer's real device pointer.
func envFor(t *testing.T, root string) func(string) string {
	t.Helper()
	sandbox := t.TempDir()
	vars := map[string]string{
		store.PathEnv:   root,
		"HOME":          sandbox,
		"XDG_DATA_HOME": sandbox,
	}
	return func(k string) string { return vars[k] }
}

func produce(t *testing.T, opts Options) Result {
	t.Helper()
	if opts.Dest == "" {
		opts.Dest = filepath.Join(t.TempDir(), "bundle")
	}
	if opts.Liveness == nil {
		opts.Liveness = func() (tri.Value, string) { return tri.No, "" }
	}
	res, err := Produce(opts)
	if err != nil {
		t.Fatalf("producing a bundle: %v", err)
	}
	return res
}

// bundleFiles returns every regular file in the bundle, bundle-relative, and their contents.
func bundleFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the bundle: %v", err)
	}
	if len(out) == 0 {
		// A SEARCH THAT EXAMINED NOTHING IS NOT A PASS. Every negative assertion below is driven
		// through this function, so an empty walk would make all of them vacuously true.
		t.Fatalf("the bundle at %s contains no files at all, so nothing was searched", root)
	}
	return out
}

func category(t *testing.T, m Manifest, name string) Category {
	t.Helper()
	for _, c := range m.Categories {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("the manifest has no category %q; it has %v", name, names(m))
	return Category{}
}

func names(m Manifest) []string {
	var out []string
	for _, c := range m.Categories {
		out = append(out, c.Name)
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// Criteria 4 and 5: the default bundle holds no body, driven negatively over every file.
// ---------------------------------------------------------------------------------------------

func TestDefaultBundleContainsNoTicketDraftOrMessageBody(t *testing.T) {
	root := seededStore(t)
	res := produce(t, Options{Getenv: envFor(t, root)})

	files := bundleFiles(t, res.Path)
	// THE SEARCH IS PROVEN TO BE ABLE TO FIND SOMETHING before it is trusted to find nothing: the
	// same matcher is run against the store's own files, where the strings certainly are. Without
	// this, a bug in the walk or in the matcher would read as a perfectly clean bundle.
	assertSearchWorks(t, root)

	for _, secret := range []string{secretTicketBody, secretDraftBody, secretMessageBody, secretModelKey} {
		for name, body := range files {
			if strings.Contains(body, secret) {
				t.Errorf("a default bundle disclosed material: %s appears in %s", secret, name)
			}
		}
	}
	if len(files) < 6 {
		t.Errorf("the default bundle has only %d files, which is too few to have gathered anything: %v", len(files), files)
	}
}

// assertSearchWorks proves the exhaustive search can find the seeded strings when they ARE present,
// by running it over the store itself.
func assertSearchWorks(t *testing.T, storeRoot string) {
	t.Helper()
	found := map[string]bool{}
	err := filepath.WalkDir(storeRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// The store base64s payloads inside its envelope, so the raw bytes are searched too.
		for _, s := range []string{secretTicketBody, secretDraftBody, secretMessageBody, secretModelKey} {
			if strings.Contains(string(body), s) || strings.Contains(decodeAll(string(body)), s) {
				found[s] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the store: %v", err)
	}
	for _, s := range []string{secretTicketBody, secretDraftBody, secretMessageBody, secretModelKey} {
		if !found[s] {
			t.Fatalf("the search could not find %s even in the store that holds it, so a clean bundle proves nothing", s)
		}
	}
}

// decodeAll pulls base64 payloads out of a store envelope so the plaintext can be searched for. It
// is deliberately crude: it exists to make assertSearchWorks honest, not to parse the store format.
func decodeAll(s string) string {
	var b strings.Builder
	for _, field := range strings.Split(s, `"`) {
		if d, err := b64(field); err == nil {
			b.WriteString(d)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------------------------
// Criteria 6 and 7: bodies only on an affirmative request, and the manifest says so either way.
// ---------------------------------------------------------------------------------------------

func TestOptInIncludesBodiesAndTheManifestSaysSo(t *testing.T) {
	root := seededStore(t)
	env := envFor(t, root)

	def := produce(t, Options{Getenv: env})
	opt := produce(t, Options{Getenv: env, IncludeBodies: true})

	// Distinguishable FROM THE MANIFEST ALONE (criterion 6) — read the way a person with only the
	// bundle would read it, not from the in-memory Result.
	dm, err := ReadManifest(def.Path)
	if err != nil {
		t.Fatalf("reading the default bundle's manifest: %v", err)
	}
	om, err := ReadManifest(opt.Path)
	if err != nil {
		t.Fatalf("reading the opt-in bundle's manifest: %v", err)
	}
	if dm.BodiesIncluded {
		t.Errorf("a bundle produced with no request says its bodies are included")
	}
	if !om.BodiesIncluded {
		t.Errorf("a bundle produced with an explicit request says its bodies are not included")
	}
	if dm.BodiesRequest == om.BodiesRequest {
		t.Errorf("the two bundles' manifests render the request identically as %q, so they are not distinguishable from the manifest alone", dm.BodiesRequest)
	}

	// EVERY BODY CATEGORY IS WITHHELD BY DEFAULT, whatever it can go on to establish.
	for _, name := range []string{CatTicketBodies, CatDraftBodies, CatMessageBodies} {
		if d := category(t, dm, name); d.State != StateWithheld {
			t.Errorf("%s in the default bundle is %q, want %q", name, d.State, StateWithheld)
		}
	}

	// Criterion 7: the SAME field that reads withheld by default reads as included, for the
	// categories this build can actually produce.
	for _, name := range []string{CatTicketBodies, CatDraftBodies} {
		o := category(t, om, name)
		if o.State != StateCollected {
			t.Errorf("%s in the opt-in bundle is %q, want %q", name, o.State, StateCollected)
		}
		if o.Items != 1 {
			t.Errorf("%s in the opt-in bundle collected %d items, want the 1 that was seeded", name, o.Items)
		}
	}

	// MESSAGES ARE NOT A THING THIS BUILD CAN COLLECT, and asking for bodies does not change that.
	// Nothing writes a raw message — channel ingestion stores tickets — so the opt-in reaches an
	// undetermined category rather than a confident zero. The seeded `message` record below is a
	// store record no part of the product wrote, and it stays out of the bundle for that reason.
	msg := category(t, om, CatMessageBodies)
	if msg.State != StateUndetermined || msg.Reason != ReasonNotInThisBuild {
		t.Errorf("%s under an explicit opt-in is %q/%q, want undetermined/%q", CatMessageBodies, msg.State, msg.Reason, ReasonNotInThisBuild)
	}

	// And the bodies really are there — an opt-in that quietly withheld would satisfy the negative
	// test above while making the feature useless.
	files := bundleFiles(t, opt.Path)
	for _, secret := range []string{secretTicketBody, secretDraftBody} {
		if !containsAny(files, secret) {
			t.Errorf("bodies were asked for and %s is nowhere in the bundle", secret)
		}
	}
	if containsAny(files, secretMessageBody) {
		t.Errorf("the bundle carried a record under a kind the product does not write, which it did not establish and cannot describe")
	}
}

// ---------------------------------------------------------------------------------------------
// PRD §3.13: a credential is not a body, and the opt-in does not reach it.
// ---------------------------------------------------------------------------------------------

func TestOptInDoesNotCarryACredential(t *testing.T) {
	root := seededStore(t)
	env := func(k string) string {
		if k == "OMW_MODEL_KEY" {
			return secretModelKey
		}
		return envFor(t, root)(k)
	}
	res := produce(t, Options{Getenv: env, IncludeBodies: true})

	for name, body := range bundleFiles(t, res.Path) {
		if strings.Contains(body, secretModelKey) {
			t.Errorf("the opt-in bundle disclosed the person's model key in %s", name)
		}
	}
	c := category(t, res.Manifest, CatModelKey)
	if c.State != StateWithheld || c.Reason != ReasonNeverCollected {
		t.Errorf("the model key category is %q/%q, want %q/%q — withholding it must be stated, not silent",
			c.State, c.Reason, StateWithheld, ReasonNeverCollected)
	}
}

// ---------------------------------------------------------------------------------------------
// Criteria 1, 2 and the manifest/contents agreement.
// ---------------------------------------------------------------------------------------------

func TestManifestAndContentsAgree(t *testing.T) {
	for _, bodies := range []bool{false, true} {
		root := seededStore(t)
		res := produce(t, Options{Getenv: envFor(t, root), IncludeBodies: bodies})
		m, err := ReadManifest(res.Path)
		if err != nil {
			t.Fatalf("reading the manifest: %v", err)
		}
		files := bundleFiles(t, res.Path)

		claimed := map[string]bool{manifestName: true}
		for _, c := range m.Categories {
			for _, f := range c.Files {
				claimed[f] = true
				if _, ok := files[f]; !ok {
					t.Errorf("bodies=%v: the manifest says %s produced %s, and that file is not in the bundle", bodies, c.Name, f)
				}
			}
			if c.State == StateCollected && len(c.Files) == 0 {
				t.Errorf("bodies=%v: %s is collected and names no file", bodies, c.Name)
			}
			if c.State != StateCollected && len(c.Files) != 0 {
				t.Errorf("bodies=%v: %s is %s and yet names %v", bodies, c.Name, c.State, c.Files)
			}
		}
		// THE OTHER DIRECTION, which is the one that catches a bundle carrying something its
		// manifest does not admit to.
		for f := range files {
			if !claimed[f] {
				t.Errorf("bodies=%v: the bundle contains %s and no category in the manifest names it", bodies, f)
			}
		}
	}
}

func TestManifestNamesEveryCategoryEveryTime(t *testing.T) {
	cases := map[string]Options{
		"seeded store":   {Getenv: envFor(t, seededStore(t))},
		"no store":       {Getenv: noStoreEnv(t)},
		"hub un-set":     {Getenv: envFor(t, seededStore(t))},
		"hub configured": {Getenv: withHub(envFor(t, seededStore(t)), "https://hub.example")},
	}
	for name, opts := range cases {
		res := produce(t, opts)
		m, err := ReadManifest(res.Path)
		if err != nil {
			t.Fatalf("%s: reading the manifest: %v", name, err)
		}
		if len(m.Categories) != len(categoryNames) {
			t.Errorf("%s: the manifest has %d categories, want %d: %v", name, len(m.Categories), len(categoryNames), names(m))
		}
		for _, want := range categoryNames {
			c := category(t, m, want)
			switch c.State {
			case StateCollected, StateWithheld, StateUndetermined:
			default:
				t.Errorf("%s: category %s has state %q, which is not one of the three", name, want, c.State)
			}
			if c.State != StateCollected && (c.Reason == "" || c.Detail == "") {
				t.Errorf("%s: category %s is %s with reason %q and detail %q — a person cannot say what they withheld", name, want, c.State, c.Reason, c.Detail)
			}
			if c.Reason == seedReason {
				t.Errorf("%s: category %s was never spoken for by any branch of the run", name, want)
			}
		}
	}
}

// Criterion 1: the manifest is READABLE WITHOUT OPENING THE COLLECTED DATA. Driven by deleting
// every file but the manifest and reading it anyway — a person who has the bundle and not the
// machine is in exactly that position with respect to the machine's own state.
func TestTheManifestIsReadableFromTheBundleAlone(t *testing.T) {
	res := produce(t, Options{Getenv: envFor(t, seededStore(t))})
	for f := range bundleFiles(t, res.Path) {
		if f != manifestName {
			if err := os.Remove(filepath.Join(res.Path, f)); err != nil {
				t.Fatalf("removing %s: %v", f, err)
			}
		}
	}
	m, err := ReadManifest(res.Path)
	if err != nil {
		t.Fatalf("the manifest could not be read on its own: %v", err)
	}
	if len(m.Categories) != len(categoryNames) {
		t.Errorf("the manifest read on its own describes %d categories, want %d", len(m.Categories), len(categoryNames))
	}
}

// ---------------------------------------------------------------------------------------------
// Criteria 8, 9, 12: the daemon, and the three values.
// ---------------------------------------------------------------------------------------------

// The three liveness answers must render pairwise-distinctly in the bundle, and none of them may be
// an absence. Compared PAIRWISE rather than against string literals: a test asserting the literal
// "no" would keep passing if every answer became "no".
func TestTheThreeDaemonAnswersRenderDistinguishably(t *testing.T) {
	root := seededStore(t)
	env := envFor(t, root)
	rendered := map[tri.Value]string{}
	for _, v := range []tri.Value{tri.Yes, tri.No, tri.Undetermined} {
		why := ""
		if v == tri.Undetermined {
			why = "the lock could not be read"
		}
		res := produce(t, Options{
			Getenv:   env,
			Liveness: func() (tri.Value, string) { return v, why },
		})
		body, err := os.ReadFile(filepath.Join(res.Path, "daemon.json"))
		if err != nil {
			t.Fatalf("reading daemon.json for %v: %v", v, err)
		}
		rendered[v] = string(body)
	}
	pairs := [][2]tri.Value{{tri.Yes, tri.No}, {tri.Yes, tri.Undetermined}, {tri.No, tri.Undetermined}}
	for _, p := range pairs {
		if rendered[p[0]] == rendered[p[1]] {
			t.Errorf("the bundle renders %v and %v identically, so a reader cannot tell them apart", p[0], p[1])
		}
	}
	// And specifically: the undetermined rendering must not be readable as the negative one.
	if !strings.Contains(rendered[tri.Undetermined], tri.Undetermined.String()) {
		t.Errorf("the undetermined daemon answer does not carry the product's undetermined wording:\n%s", rendered[tri.Undetermined])
	}
	if strings.Contains(rendered[tri.Undetermined], `"running": "no"`) {
		t.Errorf("an undetermined daemon answer rendered as a determined no")
	}
}

// Criterion 8: with a store and no daemon, the bundle is produced and the daemon is RECORDED as not
// running rather than omitted.
func TestBundleWithNoDaemonRecordsTheDaemonAsNotRunning(t *testing.T) {
	root := seededStore(t)
	// No daemon was ever started against this store; the real liveness answer is asked for, not
	// stubbed, so this drives the actual inspection.
	res, err := Produce(Options{Getenv: envFor(t, root), Dest: filepath.Join(t.TempDir(), "b")})
	if err != nil {
		t.Fatalf("producing a bundle with no daemon: %v", err)
	}
	c := category(t, res.Manifest, CatDaemonState)
	if c.State != StateCollected {
		t.Fatalf("the daemon category is %s/%s, want collected — daemon state must not be omitted because the daemon is dead", c.State, c.Reason)
	}
	body := readFile(t, filepath.Join(res.Path, c.Files[0]))
	if !strings.Contains(body, `"running"`) {
		t.Errorf("daemon.json does not record whether the daemon is running:\n%s", body)
	}
}

// ---------------------------------------------------------------------------------------------
// Criterion 10: no store at all.
// ---------------------------------------------------------------------------------------------

func noStoreEnv(t *testing.T) func(string) string {
	t.Helper()
	sandbox := t.TempDir()
	vars := map[string]string{"HOME": sandbox, "XDG_DATA_HOME": sandbox}
	return func(k string) string { return vars[k] }
}

func TestBundleWithNoStoreIsStillProducedAndSaysWhy(t *testing.T) {
	res := produce(t, Options{Getenv: noStoreEnv(t)})
	m, err := ReadManifest(res.Path)
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	for _, name := range []string{CatStoreLocation, CatTicketInventory, CatDraftInventory, CatMessageInventory} {
		c := category(t, m, name)
		if c.State != StateUndetermined {
			t.Errorf("%s on a machine with no store is %q, want %q", name, c.State, StateUndetermined)
		}
		if c.Reason != ReasonNoStore && c.Reason != ReasonCouldNotRead {
			t.Errorf("%s on a machine with no store gives reason %q, which does not say the store is why", name, c.Reason)
		}
	}
	// Criterion 10's distinguishability: unavailable-because-no-store versus present-but-empty.
	empty := filepath.Join(t.TempDir(), "empty-store")
	if _, err := store.Create(empty); err != nil {
		t.Fatalf("creating an empty store: %v", err)
	}
	res2 := produce(t, Options{Getenv: envFor(t, empty)})
	got := category(t, res2.Manifest, CatTicketInventory)
	want := category(t, m, CatTicketInventory)
	if got.State == want.State && got.Reason == want.Reason {
		t.Errorf("a store with no tickets and a machine with no store both render as %s/%s", got.State, got.Reason)
	}
	if got.State != StateCollected || got.Items != 0 {
		t.Errorf("an empty store's ticket inventory is %s with %d items, want collected with 0", got.State, got.Items)
	}
	// And health is still reported with no store at all (PRD §4.1).
	if c := category(t, m, CatEncryption); c.State != StateCollected {
		t.Errorf("full-disk encryption is %s on a machine with no store; §4.1 says health needs neither a store nor a daemon", c.State)
	}
}

// ---------------------------------------------------------------------------------------------
// Criterion 11: encryption and the control-API confirmation are separate manifest entries.
// ---------------------------------------------------------------------------------------------

func TestEncryptionAndControlPermissionsAreSeparateEntries(t *testing.T) {
	res := produce(t, Options{Getenv: envFor(t, seededStore(t))})
	enc := category(t, res.Manifest, CatEncryption)
	ctl := category(t, res.Manifest, CatControlOwnerOnly)
	if enc.Name == ctl.Name {
		t.Fatalf("the two facts share one manifest entry")
	}
	if enc.State != StateCollected {
		t.Errorf("full-disk encryption is %s/%s, want collected", enc.State, enc.Reason)
	}
	if ctl.State != StateCollected {
		t.Errorf("the control-API owner-only confirmation is %s/%s, want collected", ctl.State, ctl.Reason)
	}
	body := readFile(t, filepath.Join(res.Path, ctl.Files[0]))
	if !strings.Contains(body, "control_api_open") {
		t.Errorf("the file the control-API category names does not record the control API's state:\n%s", body)
	}
}

// The encryption fact is carried in three values and the three do not collapse (Issue #1, §4.1).
func TestEncryptionIsCarriedInThreeValues(t *testing.T) {
	env := envFor(t, seededStore(t))
	rendered := map[tri.Value]string{}
	for _, v := range []tri.Value{tri.Yes, tri.No, tri.Undetermined} {
		v := v
		res := produce(t, Options{
			Getenv: env,
			Health: func() health.Report {
				return health.Runner{GOOS: "linux", Checker: fixedChecker{v}, Getenv: env}.Run(context.Background())
			},
		})
		rendered[v] = readFile(t, filepath.Join(res.Path, "health.json"))
	}
	for _, p := range [][2]tri.Value{{tri.Yes, tri.No}, {tri.Yes, tri.Undetermined}, {tri.No, tri.Undetermined}} {
		if rendered[p[0]] == rendered[p[1]] {
			t.Errorf("the bundle renders encryption %v and %v identically", p[0], p[1])
		}
	}
}

// fixedChecker forces one of health's three outcomes without owning the machine. It returns
// (bool, error) because that is health's interface: the undetermined branch is an error, which is
// exactly the pair tri.FromError turns into the third value.
type fixedChecker struct{ v tri.Value }

func (c fixedChecker) Mechanism() string { return "a fixed probe, in this test" }

func (c fixedChecker) Enabled(context.Context) (bool, error) {
	switch c.v {
	case tri.Yes:
		return true, nil
	case tri.No:
		return false, nil
	default:
		return false, errors.New("the probe was told to determine nothing")
	}
}

// ---------------------------------------------------------------------------------------------
// Criteria 13 and 14: no hub.
// ---------------------------------------------------------------------------------------------

func withHub(base func(string) string, url string) func(string) string {
	return func(k string) string {
		if k == health.HubEnv {
			return url
		}
		return base(k)
	}
}

func TestHubDerivedStateIsNamedNotDropped(t *testing.T) {
	res := produce(t, Options{Getenv: envFor(t, seededStore(t))})
	c := category(t, res.Manifest, CatHubDerivedState)
	if c.State != StateUndetermined || c.Reason != ReasonNoHub {
		t.Errorf("hub-derived state with no hub is %s/%s, want %s/%s", c.State, c.Reason, StateUndetermined, ReasonNoHub)
	}
	// NEVER A NEGATIVE FINDING. tri's own negative wording must not appear as the answer here.
	if c.State == StateCollected {
		t.Errorf("hub-derived state was reported as collected with no hub to collect it from")
	}
	// Everything else was still produced fully.
	if s := category(t, res.Manifest, CatStoreLocation).State; s != StateCollected {
		t.Errorf("with no hub the store location is %s; §4.4 says the local half stands alone", s)
	}
	if s := category(t, res.Manifest, CatEncryption).State; s != StateCollected {
		t.Errorf("with no hub full-disk encryption is %s; §4.4 says the local half stands alone", s)
	}
}

// ---------------------------------------------------------------------------------------------
// Criterion 15: a failure leaves nothing that could be mistaken for a bundle.
// ---------------------------------------------------------------------------------------------

func TestAFailedRunLeavesNoPartialBundle(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "bundle")
	// A destination that is a file the run cannot write into is a determined failure.
	if err := os.WriteFile(dest, []byte("not a bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Produce(Options{Dest: dest, Getenv: envFor(t, seededStore(t))}); err == nil {
		t.Fatalf("producing a bundle over an existing path succeeded; it must refuse")
	}
	body := readFile(t, dest)
	if body != "not a bundle" {
		t.Errorf("the refused run altered what was already at the destination")
	}
	// And no staging directory is left beside it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "bundle" {
			t.Errorf("the refused run left %s behind", e.Name())
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Criterion 16: platform, and the device label this build cannot read.
// ---------------------------------------------------------------------------------------------

func TestPlatformIsRecordedAndTheMissingDeviceLabelIsNamed(t *testing.T) {
	res := produce(t, Options{Getenv: envFor(t, seededStore(t)), GOOS: "linux"})
	p := category(t, res.Manifest, CatPlatform)
	if p.State != StateCollected {
		t.Fatalf("the platform is %s/%s, want collected", p.State, p.Reason)
	}
	if body := readFile(t, filepath.Join(res.Path, p.Files[0])); !strings.Contains(body, "linux") {
		t.Errorf("the platform file does not name the platform:\n%s", body)
	}
	d := category(t, res.Manifest, CatDeviceLabel)
	if d.State != StateUndetermined || d.Reason != ReasonNotInThisBuild {
		t.Errorf("the device label is %s/%s; a capability this build does not have must be named as undetermined, not omitted", d.State, d.Reason)
	}
}

// ---------------------------------------------------------------------------------------------
// Structural: the opt-in reaches the three body kinds and nothing else.
// ---------------------------------------------------------------------------------------------

func TestTheOptInReachesOnlyBodies(t *testing.T) {
	if len(bodySources) != 3 {
		t.Fatalf("the opt-in reaches %d sources, want the three body categories: %v", len(bodySources), bodySources)
	}
	// THE CREDENTIAL IS NOT REACHABLE FROM ANY OF THEM. Since Issue #67 a body source is a
	// function rather than a kind, so this is driven rather than compared: each source is run
	// against a store holding a real key, with bodies asked for, and none of them may return it.
	root := seededStore(t)
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("opening the seeded store: %v", err)
	}
	for cat, src := range bodySources {
		recs, err := src.List(s, true)
		if err != nil {
			continue // an undetermined source discloses nothing, which is what this asserts
		}
		for _, r := range recs {
			if strings.Contains(r.Body, secretModelKey) {
				t.Errorf("category %s would disclose the model credential on opt-in", cat)
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Environment values are never carried, only names.
// ---------------------------------------------------------------------------------------------

func TestConfigurationCarriesNamesAndNotValues(t *testing.T) {
	root := seededStore(t)
	res := produce(t, Options{Getenv: withHub(envFor(t, root), "https://hub.example/"+secretModelKey)})
	body := readFile(t, filepath.Join(res.Path, "config.json"))
	if !strings.Contains(body, health.HubEnv) {
		t.Errorf("config.json does not name the hub variable that is set:\n%s", body)
	}
	if strings.Contains(body, secretModelKey) {
		t.Errorf("config.json carried an environment VALUE:\n%s", body)
	}
}

// b64 decodes a base64 field, so the store's own envelope can be searched in plaintext.
func b64(s string) (string, error) {
	if len(s) < 16 {
		return "", errors.New("too short to be a payload")
	}
	d, err := base64.StdEncoding.DecodeString(s)
	return string(d), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func containsAny(files map[string]string, s string) bool {
	for _, body := range files {
		if strings.Contains(body, s) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------------------------
// Criterion 12: a subsystem that could not be read is undetermined WITH A REASON — never absent,
// and never a confident negative.
// ---------------------------------------------------------------------------------------------

// The ticket directory is made unreadable on disk, so listing it genuinely fails. That is a
// different fact from "there are no tickets", and the two must not render alike.
func TestAnUnreadableSubsystemIsUndeterminedAndNotAbsent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read a directory with no permissions, so this cannot be staged")
	}
	root := seededStore(t)
	dir := filepath.Join(root, "records", string(KindTicket))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the seeded ticket directory is not where this test expects it: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("staging an unreadable ticket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	res := produce(t, Options{Getenv: envFor(t, root)})
	c := category(t, res.Manifest, CatTicketInventory)
	if c.State != StateUndetermined {
		t.Fatalf("an unreadable ticket directory renders as %s/%s with %d items; want undetermined — this build cannot see them, which is not the same as there being none",
			c.State, c.Reason, c.Items)
	}
	if c.Reason != ReasonCouldNotRead {
		t.Errorf("the reason is %q, which does not say the subsystem could not be read", c.Reason)
	}
	if c.Detail == "" {
		t.Errorf("an undetermined category carries no reason a person could read")
	}
	// And it is still IN the manifest, and distinguishable from an empty collection.
	empty := category(t, res.Manifest, CatDraftInventory)
	if empty.State == c.State && empty.Reason == c.Reason {
		t.Errorf("a readable category and an unreadable one render identically as %s/%s", c.State, c.Reason)
	}
}

// Criterion 15, on the path that matters: a run that fails once the staging directory is already
// full leaves NOTHING at the destination and nothing beside it. "A bundle exists" and "a bundle is
// complete" have to be the same statement.
func TestAFailureAfterGatheringLeavesNothingBehind(t *testing.T) {
	orig := writeManifest
	writeManifest = func(string, any) error { return errors.New("the manifest could not be written") }
	t.Cleanup(func() { writeManifest = orig })

	dir := t.TempDir()
	dest := filepath.Join(dir, "bundle")
	if _, err := Produce(Options{Dest: dest, Getenv: envFor(t, seededStore(t))}); err == nil {
		t.Fatalf("the run reported success although the manifest could not be written")
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Errorf("something exists at %s after a failed run (%v); a person could mistake it for a bundle", dest, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var left []string
		for _, e := range entries {
			left = append(left, e.Name())
		}
		t.Errorf("a failed run left %v beside the destination", left)
	}
}
