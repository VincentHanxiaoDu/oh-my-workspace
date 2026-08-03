// Package diagnostics produces the support bundle of PRD §3.9: "a bundle a person can hand to
// whoever supports them. The bundle states what it contains, and withholds identifying data by
// default — raw message bodies are not in it unless asked for."
//
// # THE TWO HALVES PULL AGAINST EACH OTHER, AND THE RESOLUTION IS THAT THE BUNDLE IS EXPLICIT
//
// A bundle useful to a supporter and a bundle that does not hand over the person's material are in
// tension. This package does not resolve that tension by choosing a middle amount of data. It
// resolves it by making the trade READABLE: every category the bundle could hold is named in a
// manifest that travels inside the bundle, and each says whether it was collected, withheld, or
// could not be determined — so a person can state what they handed over before they press send, and
// a supporter can state what they are missing.
//
// Two failures follow from that, and both are tested:
//
//   - A bundle that SILENTLY OMITS something is a defect. There is no such thing as a category that
//     is simply absent: the category list is fixed (see categoryNames) and a manifest missing one
//     of them fails TestManifestNamesEveryCategoryEveryTime.
//   - A bundle that SILENTLY INCLUDES a body is a defect. The default gather reads no record
//     payload at all — see the inventory loop in gather, which reads ids and sizes and never touches
//     Record.Data.
//
// # WHY THE MANIFEST IS MACHINE-CHECKABLE AND NOT PROSE
//
// "The bundle states what it contains" is worth nothing if the statement can drift from the
// contents. So each category names the FILES it produced, and TestManifestAndContentsAgree asserts
// the two directions separately: every file a category names exists, and every file in the bundle
// is named by some category. A manifest that drifts from its payload is worse than no manifest,
// because it is the thing the person read before deciding to send it.
//
// # WHY A CREDENTIAL IS NOT A BODY
//
// Bodies are withheld BY DEFAULT and can be asked for. A model key is withheld ALWAYS. PRD §3.13
// puts the person's key outside what is published, synchronised, or readable through the agent API,
// and a supporter's need to diagnose a client is not a reason to hold it. The opt-in switch is
// deliberately wired only to the three body categories, and TestOptInDoesNotCarryACredential drives
// that with a real key in a real store.
//
// # WHAT THIS PACKAGE DOES NOT DO (PRD §4.2, §4.4)
//
// It starts no daemon: daemon state comes from daemon.Inspect, which reads a lock and a run record.
// It opens no network connection: it holds no transport at all, and TestEveryListenAndDialIsAUnixSocket
// over internal/ is what keeps that true as this file grows. It transmits nothing anywhere — handing
// the bundle over is the person's act, and this package's last statement is a path on disk.
package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/health"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// State is what happened to one category of data. There are exactly three, and they are PRD §4.3's
// three: the product says what it knows and what it does not.
//
// Undetermined is a real answer and is never rendered as an absence or as a negative finding. A
// category the bundle could not read is undetermined WITH a reason; it is never dropped, and it is
// never rendered as "there is none of that".
type State string

const (
	// StateCollected means the category was read and its data is in the bundle. A collected
	// category with nothing in it is rendered as collected with a count of zero — present-but-empty
	// is a fact, and it is distinguishable from the states below.
	StateCollected State = "collected"
	// StateWithheld means the data exists, or may exist, and this bundle deliberately does not
	// carry it. Withholding is a choice the bundle states, not an omission.
	StateWithheld State = "withheld"
	// StateUndetermined means the bundle could not establish the category's contents. Never a "no".
	StateUndetermined State = "undetermined"
)

// Reason is WHY a category is not collected, in machine-readable form. Criterion 3 asks a person
// reading only the manifest to be able to say what they handed over and what they did not, and to
// tell "withheld by default" from "could not be determined" from "not applicable on this machine".
// Prose cannot be relied on for that, so the distinction is a code and the prose is colour.
type Reason string

const (
	// ReasonNone is the empty reason a collected category carries.
	ReasonNone Reason = ""
	// ReasonWithheldByDefault is the person's material, kept out unless asked for.
	ReasonWithheldByDefault Reason = "withheld-by-default"
	// ReasonNeverCollected is a credential. PRD §3.13 — it is not the supporter's business at any
	// opt-in level, so this reason has no switch that turns it into a collection.
	ReasonNeverCollected Reason = "never-collected-credential"
	// ReasonNoStore is criterion 10: unavailable because this machine has no store. Distinguishable
	// in the manifest from a collected category that happens to be empty.
	ReasonNoStore Reason = "not-applicable-no-store-on-this-machine"
	// ReasonNoHub is criterion 14: unavailable because no hub is configured (PRD §4.4). Named,
	// never dropped, and never rendered as a negative finding about the hub.
	ReasonNoHub Reason = "not-applicable-no-hub-configured"
	// ReasonNoNetwork is the hub-derived state when a hub IS configured: producing a bundle opens
	// no network connection (criterion 13), so hub-side state is still not established here.
	ReasonNoNetwork Reason = "not-collected-bundle-generation-opens-no-network-connection"
	// ReasonNotInThisBuild is a capability whose package is not in this build yet. Undetermined
	// with a reason, because "this build cannot ask" is not "the answer is no".
	ReasonNotInThisBuild Reason = "could-not-determine-capability-not-in-this-build"
	// ReasonCouldNotRead is a subsystem that is here and would not answer.
	ReasonCouldNotRead Reason = "could-not-determine-unreadable"
)

// Category is one line of the manifest: what the bundle could have held, and what it did with it.
type Category struct {
	// Name identifies the category. Stable, machine-readable, and drawn from categoryNames.
	Name string `json:"name"`
	// Describes what this category is, so a person reading only the manifest knows what they are
	// deciding about.
	Describes string `json:"describes"`
	// State is one of the three. Never empty.
	State State `json:"state"`
	// Reason is the code for a non-collected state. Empty exactly when State is StateCollected.
	Reason Reason `json:"reason"`
	// Detail is the sentence a person reads. Never empty for a non-collected state.
	Detail string `json:"detail"`
	// Items is how many things were collected. Meaningful only when State is StateCollected, where
	// zero means present-but-empty — a different fact from every other state here.
	Items int `json:"items"`
	// Files are the bundle-relative paths this category produced. Empty for a non-collected state,
	// because a category that collected nothing wrote nothing. The manifest/contents agreement test
	// checks both directions of this.
	Files []string `json:"files"`
}

// undeterminedBy fills in the two undetermined endings a record source can reach, and keeps them
// APART (Issue #67, criterion 4). "This build has nothing that writes these records" and "the
// records are there and would not be read" are different facts with different reasons, and neither
// of them is a count of zero. verb is what could not be done — "counted", "read".
func (c Category) undeterminedBy(err error, noun, path, verb string) Category {
	c.State = StateUndetermined
	if errors.Is(err, errNoProducerInThisBuild) {
		c.Reason = ReasonNotInThisBuild
		c.Detail = "nothing in this build writes " + noun + " records, so how many there are could not be " +
			verb + "; this is NOT a report that there are none"
		return c
	}
	c.Reason = ReasonCouldNotRead
	c.Detail = "the " + noun + " records in " + path + " could not be " + verb + ": " + err.Error() +
		"; this is NOT a report that there are none"
	return c
}

// BodiesRequest records whether the person affirmatively asked for bodies. It is a separate,
// spelled-out field rather than a bare bool so that criterion 6's "distinguishable from the manifest
// alone" holds for a human reader as well as for a program.
type BodiesRequest string

const (
	// BodiesNotRequested is the default, and it is the only value a bundle produced with no
	// argument can carry.
	BodiesNotRequested BodiesRequest = "not-requested-bodies-withheld"
	// BodiesRequested is only reachable through the explicit opt-in. No other option implies it.
	BodiesRequested BodiesRequest = "explicitly-requested-bodies-included"
)

// Manifest is what the bundle says about itself. It travels INSIDE the bundle (manifestName), so a
// person holding only the bundle, and not the machine it came from, can read what it contains
// without opening a single collected file (criterion 1).
type Manifest struct {
	Format      int       `json:"format"`
	Product     string    `json:"product"`
	GeneratedAt time.Time `json:"generated_at"`
	// BodiesIncluded and BodiesRequest are the same fact twice, for a program and for a person.
	// Criterion 7: no bundle exists whose manifest understates what it contains — these are set
	// from the same value the gather is driven by, in newManifest, so they cannot disagree with it.
	BodiesIncluded bool          `json:"bodies_included"`
	BodiesRequest  BodiesRequest `json:"bodies_request"`
	// Categories is every category, always. Never a subset: see categoryNames.
	Categories []Category `json:"categories"`
}

// manifestName is the manifest's file name inside the bundle.
const manifestName = "manifest.json"

// manifestFormat is the manifest layout version.
const manifestFormat = 1

// Category names. THE LIST IS FIXED AND EXHAUSTIVE. Every bundle carries a line for every one of
// these, whatever the machine looked like — that is what makes "no category is silently missing"
// (criterion 12) checkable rather than aspirational.
const (
	CatDaemonState      = "daemon-state"
	CatControlOwnerOnly = "control-api-owner-only-permissions"
	CatEncryption       = "full-disk-encryption"
	CatPlatform         = "platform"
	CatDeviceLabel      = "device-label"
	CatStoreLocation    = "store-location"
	CatTicketInventory  = "ticket-inventory"
	CatDraftInventory   = "draft-inventory"
	CatMessageInventory = "message-inventory"
	CatTicketBodies     = "ticket-bodies"
	CatDraftBodies      = "draft-note-bodies"
	CatMessageBodies    = "ingested-message-bodies"
	CatModelKey         = "model-key"
	CatHubConfiguration = "hub-configuration"
	CatHubDerivedState  = "hub-derived-state"
	CatEnvConfiguration = "environment-configuration"
)

// categoryNames is the exhaustive list, in manifest order.
var categoryNames = []string{
	CatDaemonState,
	CatControlOwnerOnly,
	CatEncryption,
	CatPlatform,
	CatDeviceLabel,
	CatStoreLocation,
	CatTicketInventory,
	CatDraftInventory,
	CatMessageInventory,
	CatTicketBodies,
	CatDraftBodies,
	CatMessageBodies,
	CatModelKey,
	CatHubConfiguration,
	CatHubDerivedState,
	CatEnvConfiguration,
}

// Record kinds this bundle knows about.
//
// THEY ARE DECLARED HERE AND NOT IMPORTED because the packages that own them — the inbox (#8), the
// outbox (#9), channel ingestion (#6) — are not all in this build yet. The store deliberately does
// not know what a ticket is (see internal/store's Kind doc), so somebody has to name the kinds, and
// naming them here means this bundle keeps working as those packages land rather than failing to
// compile against them. When they land, these constants are the ones to delete in favour of theirs.
const (
	KindTicket = store.Kind("ticket")
	// KindMessage is a raw message ingested from a channel (PRD §2.1). Criterion 5 is about this
	// one specifically.
	KindMessage = store.Kind("message")
	// KindModelCredential is the person's model key (PRD §3.13, Issue #18). It is named here ONLY
	// so that it can be excluded by name and so that a test can prove the exclusion against a real
	// record. Nothing in this package ever reads its payload.
	KindModelCredential = store.Kind("model-credential")
)

// THERE IS NO KindDraft, AND ITS ABSENCE IS THE FIX FOR ISSUE #67, BLOCKER 2.
//
// This package used to declare `store.Kind("draft")` and count the records under it. Nothing in the
// product has ever written a record under that kind: `omw outbox draft` writes revision files into
// the outbox inside the store (internal/drafts). The reader and the writer never met, so a bundle
// taken on a machine with two drafts reported `draft-inventory  collected (0)` — and a supporter
// reading that bundle concludes the person has no drafts. An asserted zero is worse than a
// withholding, because a withholding is visibly a gap.
//
// So drafts are now enumerated through the outbox that writes them, by [listDrafts]. The general
// lesson is enforced structurally by internal/kindguard, which fails when a store kind is read here
// and written nowhere.

// collectedRecord is one thing a category can carry.
type collectedRecord struct {
	// ID is the record's identifier, as its own subsystem names it.
	ID string
	// Size is how big the record is, in bytes. It is the ONLY thing the default bundle reports
	// about a record besides its id.
	Size int
	// Body is the record's content. It is populated only when bodies were affirmatively asked for
	// — a source that filled this in regardless would put the person's material one careless
	// `writeCollected` away from a bundle they are about to email.
	Body string
}

// recordSource is where one category's records come from.
//
// IT IS A FUNCTION AND NOT A store.Kind (Issue #67). A kind is a directory name, and a directory
// name that nobody writes to still reads perfectly well — as zero records. Naming the SUBSYSTEM
// that owns the records instead means a category can only be added by pointing at something that
// produces them, and a subsystem this build does not have returns [errNoProducerInThisBuild]
// rather than an empty list.
type recordSource struct {
	// Noun names the thing, in prose and in the bundle's file names.
	Noun string
	// List returns every record. wantBodies asks for the content as well as the metadata.
	//
	// AN ERROR IS UNDETERMINED, NEVER EMPTY. A source that cannot read its records must not return
	// an empty slice: that is the one return value that makes "I could not look" and "there is
	// nothing" the same bytes in a manifest.
	List func(st *store.Store, wantBodies bool) ([]collectedRecord, error)
}

// errNoProducerInThisBuild is a category whose records nothing in this build writes. It renders as
// undetermined with [ReasonNotInThisBuild] — "this build cannot ask" is not "the answer is none".
var errNoProducerInThisBuild = errors.New("nothing in this build produces these records")

// listTickets enumerates the inbox's tickets (internal/inbox writes them under this kind).
func listTickets(st *store.Store, wantBodies bool) ([]collectedRecord, error) {
	recs, err := st.List(KindTicket)
	if err != nil {
		return nil, err
	}
	return fromStoreRecords(recs, wantBodies), nil
}

// listMessages enumerates raw ingested messages.
//
// NOTHING IN THIS BUILD WRITES THEM. Channel ingestion turns a message into a TICKET
// (internal/channels/ingest.go) and stores no raw message. Reading the kind anyway returns zero
// records from a directory that has never existed, which is the same defect as Blocker 2 — so this
// says so instead. Recorded as debt on Issue #32; Issue #67 named three surfaces and this is not
// one of them.
func listMessages(st *store.Store, wantBodies bool) ([]collectedRecord, error) {
	recs, err := st.List(KindMessage)
	if err != nil {
		return nil, err
	}
	return fromStoreRecords(recs, wantBodies), nil
}

func fromStoreRecords(recs []store.Record, wantBodies bool) []collectedRecord {
	out := make([]collectedRecord, 0, len(recs))
	for _, r := range recs {
		// SIZE ALWAYS, BODY ONLY ON REQUEST. r.Data is in hand here and its contents are
		// deliberately reached only through wantBodies: this is the line where a body would enter a
		// default bundle, and it is the line the mutation test changes to prove the negative search
		// is live.
		c := collectedRecord{ID: r.ID, Size: len(r.Data)}
		if wantBodies {
			c.Body = string(r.Data)
		}
		out = append(out, c)
	}
	return out
}

// listDrafts enumerates the drafts in this store's outbox — the place `omw outbox draft` writes and
// `omw outbox list` reads (Issue #67, criterion 3).
//
// IT CREATES NOTHING. drafts.InStore would materialise the outbox directory, which is right for a
// command a person ran on purpose and wrong for a diagnostic read. A store whose outbox does not
// exist yet has no drafts, and that is a DETERMINED zero: the outbox is created by the first draft,
// so its absence is evidence and not ignorance. Anything else that goes wrong is undetermined.
func listDrafts(st *store.Store, wantBodies bool) ([]collectedRecord, error) {
	dir := filepath.Join(st.Path(), drafts.OutboxDirName)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	o, err := drafts.Open(dir)
	if err != nil {
		return nil, err
	}
	ids, err := o.Drafts()
	if err != nil {
		return nil, err
	}
	out := make([]collectedRecord, 0, len(ids))
	for _, id := range ids {
		versions, err := o.Timeline(id, "")
		if err != nil {
			// ONE UNREADABLE DRAFT MAKES THE CATEGORY UNDETERMINED, rather than being dropped from
			// a count that then presents as complete. Half an inventory reported as all of it is
			// wrong without looking wrong.
			return nil, fmt.Errorf("draft %q: %w", string(id), err)
		}
		c := collectedRecord{ID: string(id)}
		for _, v := range versions {
			c.Size += len(v.Body)
		}
		if wantBodies && len(versions) > 0 {
			// The draft AS IT STANDS. Its earlier revisions are the outbox's business and are not
			// what a supporter is looking at.
			c.Body = versions[len(versions)-1].Body
		}
		out = append(out, c)
	}
	return out, nil
}

// bodySources is exactly what the opt-in switch reaches.
//
// KindModelCredential IS NOT REACHABLE FROM HERE AND MUST NOT BE MADE SO. The opt-in is a person
// saying "my tickets and drafts are less private to me than my broken client is important"; it is
// not a person handing over a key, and §3.13 does not have a switch.
var bodySources = map[string]recordSource{
	CatTicketBodies:  {Noun: "ticket", List: listTickets},
	CatDraftBodies:   {Noun: "draft", List: listDrafts},
	CatMessageBodies: {Noun: "message", List: listMessages},
}

// inventorySources maps an inventory category to the subsystem it counts.
var inventorySources = map[string]recordSource{
	CatTicketInventory:  {Noun: "ticket", List: listTickets},
	CatDraftInventory:   {Noun: "draft", List: listDrafts},
	CatMessageInventory: {Noun: "message", List: listMessages},
}

// Options is a bundle run.
type Options struct {
	// Dest is where the bundle is placed. It must not already exist: overwriting a bundle is how a
	// person ends up sending yesterday's.
	Dest string
	// IncludeBodies is the affirmative act of criterion 6. It is set by nothing but the person's
	// own explicit request — no other option in this struct implies it, and the command has exactly
	// one flag that sets it.
	IncludeBodies bool
	// Getenv reads configuration. Nil means nothing is configured, which is a state the bundle must
	// be producible in (PRD §4.4).
	Getenv func(string) string
	// Liveness is the product's ONE answer to "is the daemon running against this store" — the
	// function in internal/commands that resolves it through internal/daemon. It is injected rather
	// than reimplemented because a second definition of liveness is exactly the defect Issue #41
	// removed. Nil means the answer could not be asked for, which renders as undetermined.
	Liveness func() (tri.Value, string)
	// Health runs the health report. Nil means the real runner for this platform.
	Health func() health.Report
	// GOOS is the platform to report. Empty means the running one.
	GOOS string
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

// Result is what the caller tells the person.
type Result struct {
	// Path is the bundle on disk. Nothing has been sent anywhere: handing it over is the person's
	// act (criterion 13).
	Path string
	// Manifest is what the bundle says about itself, so a caller can print a summary without
	// re-reading the file it just wrote.
	Manifest Manifest
}

// ErrDestExists is a refusal to write over something already there.
var ErrDestExists = errors.New("a bundle already exists at that path")

// Produce writes a bundle and returns where it is.
//
// # WHY IT IS ASSEMBLED ELSEWHERE AND MOVED IN (criterion 15)
//
// Everything is written into a sibling staging directory and renamed into place as the last act.
// A run that fails removes the staging directory and leaves NOTHING at Dest. So "a bundle exists"
// and "a bundle is complete" are the same statement — there is no partial artifact that a person
// could mistake for a finished one, and no window in which the manifest is on disk without the
// files it names.
func Produce(opts Options) (Result, error) {
	if opts.Dest == "" {
		return Result{}, errors.New("no destination for the bundle")
	}
	dest, err := filepath.Abs(opts.Dest)
	if err != nil {
		return Result{}, fmt.Errorf("resolving %s: %w", opts.Dest, err)
	}
	if _, err := os.Lstat(dest); err == nil {
		return Result{}, fmt.Errorf("%w: %s", ErrDestExists, dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("checking %s: %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Result{}, fmt.Errorf("preparing %s: %w", filepath.Dir(dest), err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(dest), ".omw-bundle-")
	if err != nil {
		return Result{}, fmt.Errorf("preparing a staging directory beside %s: %w", dest, err)
	}
	// A failed run leaves nothing. The deferred removal is a no-op once the rename has happened,
	// because the staging directory no longer exists under that name.
	defer os.RemoveAll(staging)

	man, err := gather(staging, opts)
	if err != nil {
		return Result{}, err
	}
	if err := writeManifest(filepath.Join(staging, manifestName), man); err != nil {
		return Result{}, err
	}
	if err := os.Rename(staging, dest); err != nil {
		return Result{}, fmt.Errorf("placing the bundle at %s: %w", dest, err)
	}
	return Result{Path: dest, Manifest: man}, nil
}

// writeManifest writes the manifest as the last act before the bundle is moved into place.
//
// IT IS A VAR SO THAT A TEST CAN FAIL IT. Criterion 15's interesting case is a run that fails AFTER
// the staging directory is full — the moment at which a naive implementation would already have
// left a half-built bundle where the person will look. A failure before staging exists proves
// nothing about that, and the test that used to stand for this criterion only exercised the refusal
// to overwrite. This is the smallest seam that drives the real path.
var writeManifest = writeJSON

// gather fills the staging directory and returns the manifest describing exactly what it put there.
//
// It is ONE function on purpose: the manifest entry and the file it describes are produced by the
// same statement, so the only way to write a file the manifest does not name is to write it outside
// this function — which is what the agreement test looks for.
func gather(dir string, opts Options) (Manifest, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	man := newManifest(now(), opts.IncludeBodies)
	set := func(c Category) { man.set(c) }

	// ---- store ------------------------------------------------------------------------------
	//
	// Resolved first because six categories depend on whether there is one. A machine with no store
	// still gets a bundle (criterion 10); it gets one whose store-derived categories say why they
	// are empty.
	root, rootErr := store.Resolve(getenv)
	var st *store.Store
	var openErr error
	if rootErr == nil {
		st, openErr = store.Open(root)
	}

	switch {
	case rootErr != nil:
		set(Category{
			Name: CatStoreLocation, Describes: "where this device's store is, and whether that location synchronises off the machine",
			State: StateUndetermined, Reason: ReasonNoStore,
			Detail: "no store location could be resolved on this machine: " + rootErr.Error(),
		})
	case openErr != nil:
		exists := store.Exists(root)
		reason := ReasonNoStore
		detail := "no store is present at " + root + ": " + openErr.Error()
		if exists == tri.Undetermined {
			reason = ReasonCouldNotRead
			detail = "whether a store is present at " + root + " " + tri.Undetermined.String() + ": " + openErr.Error()
		}
		set(Category{
			Name: CatStoreLocation, Describes: "where this device's store is, and whether that location synchronises off the machine",
			State: StateUndetermined, Reason: reason, Detail: detail,
		})
	default:
		sync := st.SyncState()
		f, err := writeCollected(dir, "store.json", map[string]any{
			"path":   st.Path(),
			"id":     st.ID(),
			"exists": store.Exists(st.Path()).String(),
			// THE STORE'S LOCATION STATE IS REPORTED IN ITS THREE VALUES AND NOT RESOLVED.
			// Issue #20's Related note on Issue #3: the bundle reports `undetermined` as itself. A
			// location the probe could not conclude about is not a location that is fine.
			"synchronising_location":           sync.State.String(),
			"synchronising_provider":           sync.Provider,
			"synchronising_evidence":           sync.Evidence,
			"synchronising_undetermined_why":   sync.Reason,
			"created_at_undetermined_location": st.CreatedAtUndeterminedLocation(),
		})
		if err != nil {
			return man, err
		}
		set(Category{
			Name: CatStoreLocation, Describes: "where this device's store is, and whether that location synchronises off the machine",
			State: StateCollected, Items: 1, Files: []string{f},
		})
	}

	// ---- inventories (metadata only; no payload is read here) --------------------------------
	for _, name := range []string{CatTicketInventory, CatDraftInventory, CatMessageInventory} {
		src := inventorySources[name]
		describes := "how many " + src.Noun + " records exist, their identifiers and their sizes — no content"
		if st == nil {
			set(Category{
				Name: name, Describes: describes,
				State: StateUndetermined, Reason: ReasonNoStore,
				Detail: "there is no readable store on this machine, so " + src.Noun + " records could not be counted; this is not a report that there are none",
			})
			continue
		}
		recs, err := src.List(st, false)
		if err != nil {
			set(Category{Name: name, Describes: describes}.undeterminedBy(err, src.Noun, st.Path(), "counted"))
			continue
		}
		items := make([]map[string]any, 0, len(recs))
		for _, r := range recs {
			items = append(items, map[string]any{"id": r.ID, "size_bytes": r.Size})
		}
		f, err := writeCollected(dir, src.Noun+"-inventory.json", map[string]any{
			"kind": src.Noun, "count": len(items), "records": items,
		})
		if err != nil {
			return man, err
		}
		// Items is the count, and zero here means present-but-empty — a store with no tickets,
		// which is a different manifest line from a machine with no store, and a different one
		// again from records this build cannot enumerate at all.
		set(Category{Name: name, Describes: describes, State: StateCollected, Items: len(items), Files: []string{f}})
	}

	// ---- bodies (withheld by default) --------------------------------------------------------
	for _, name := range []string{CatTicketBodies, CatDraftBodies, CatMessageBodies} {
		src := bodySources[name]
		describes := "the full text of every " + src.Noun + " record on this machine"
		if !opts.IncludeBodies {
			// THE DEFAULT. PRD §2.3's containers do not leave the machine except by publication,
			// and a support bundle is not a publication.
			set(Category{
				Name: name, Describes: describes,
				State: StateWithheld, Reason: ReasonWithheldByDefault,
				Detail: "no " + src.Noun + " content is in this bundle; it is included only when explicitly asked for",
			})
			continue
		}
		if st == nil {
			set(Category{
				Name: name, Describes: describes,
				State: StateUndetermined, Reason: ReasonNoStore,
				Detail: "bodies were asked for, but there is no readable store on this machine to read them from",
			})
			continue
		}
		recs, err := src.List(st, true)
		if err != nil {
			set(Category{Name: name, Describes: describes}.undeterminedBy(err, src.Noun, st.Path(), "read"))
			continue
		}
		items := make([]map[string]any, 0, len(recs))
		for _, r := range recs {
			items = append(items, map[string]any{"id": r.ID, "body": r.Body})
		}
		f, err := writeCollected(dir, filepath.Join("bodies", src.Noun+".json"), map[string]any{
			"kind": src.Noun, "count": len(items), "records": items,
		})
		if err != nil {
			return man, err
		}
		set(Category{Name: name, Describes: describes, State: StateCollected, Items: len(items), Files: []string{f}})
	}

	// ---- the model key, at every opt-in level ------------------------------------------------
	//
	// PRD §3.13 and Issue #20's Related note on Issue #18. There is no branch here on
	// opts.IncludeBodies, and that absence is the point: a key is not a body and the opt-in does not
	// reach it. Stated in the manifest rather than left out, so a supporter knows it was considered
	// and refused rather than forgotten.
	set(Category{
		Name: CatModelKey, Describes: "the person's model provider key",
		State: StateWithheld, Reason: ReasonNeverCollected,
		Detail: "a model key is never in a support bundle, at any level of disclosure (PRD §3.13); asking for bodies does not ask for this",
	})

	// ---- daemon --------------------------------------------------------------------------
	//
	// NOTHING IS STARTED (PRD §4.2, criterion 9). daemon.Inspect reads a lock and a run record, and
	// the liveness answer is the product's single one — this package does not derive a control
	// socket path and does not probe one.
	if rootErr != nil {
		nostore := "the daemon's state is reported per store, and no store location could be resolved: " + rootErr.Error()
		set(Category{Name: CatDaemonState, Describes: "whether the daemon is running against this store, how its last run ended, and whether it is healthy",
			State: StateUndetermined, Reason: ReasonNoStore, Detail: nostore})
		set(Category{Name: CatControlOwnerOnly, Describes: "whether owner-only permissions on the control API socket could be confirmed (PRD §4.6, §5.1)",
			State: StateUndetermined, Reason: ReasonNoStore, Detail: nostore})
	} else {
		rep := daemon.Inspect(root)
		live, why := tri.Undetermined, "the product's liveness answer was not available to this run"
		if opts.Liveness != nil {
			live, why = opts.Liveness()
		}
		f, err := writeCollected(dir, "daemon.json", map[string]any{
			"store_path": rep.StorePath,
			// Three-valued and rendered through tri, so "not running" and "could not be
			// determined" cannot collapse into one string here (criterion 9).
			"running":            live.String(),
			"running_reason":     why,
			"healthy":            rep.Healthy.String(),
			"health_detail":      rep.HealthDetail,
			"last_run":           rep.LastRunText,
			"last_run_detail":    rep.LastRunDetail,
			"pid":                rep.PID,
			"control_api_open":   rep.Control.String(),
			"control_api_detail": rep.ControlDetail,
		})
		if err != nil {
			return man, err
		}
		set(Category{Name: CatDaemonState, Describes: "whether the daemon is running against this store, how its last run ended, and whether it is healthy",
			State: StateCollected, Items: 1, Files: []string{f}})
		// A SEPARATE MANIFEST ENTRY, because criterion 11 asks for it separately: "whether
		// owner-only socket permissions on the control API could be confirmed" is its own fact, and
		// on a platform where the product REFUSES to open the control API (PRD §5.1) that refusal
		// is recorded here as a fact rather than as an omission (criterion 16).
		set(Category{Name: CatControlOwnerOnly, Describes: "whether owner-only permissions on the control API socket could be confirmed (PRD §4.6, §5.1)",
			State: StateCollected, Items: 1, Files: []string{f}})
	}

	// ---- health (PRD §4.1; needs neither a store nor a daemon) --------------------------------
	runHealth := opts.Health
	if runHealth == nil {
		runHealth = func() health.Report { return health.Runner{GOOS: goos, Getenv: getenv}.Run(context.Background()) }
	}
	hrep := runHealth()
	enc, haveEnc := hrep.Encryption()
	encPayload := map[string]any{}
	if haveEnc {
		encPayload = map[string]any{
			"assumption": enc.Name,
			"ref":        enc.Ref,
			// THREE VALUES, NOT TWO. Issue #20's Related note on Issue #1: the bundle reports FDE in
			// its three values without collapsing any two of them.
			"value":     enc.Value.String(),
			"rendered":  enc.Rendered(),
			"mechanism": enc.Mechanism,
			"reason":    enc.Reason,
		}
	}
	fh, err := writeCollected(dir, "health.json", map[string]any{
		"platform":                hrep.Platform,
		"hub_configured":          hrep.HubConfigured,
		"missing_for_lack_of_hub": hrep.MissingForLackOfHub,
		"encryption":              encPayload,
		"encryption_reported":     haveEnc,
	})
	if err != nil {
		return man, err
	}
	if haveEnc {
		set(Category{Name: CatEncryption, Describes: "full-disk encryption on this machine, in three values (PRD §4.1)",
			State: StateCollected, Items: 1, Files: []string{fh}})
	} else {
		set(Category{Name: CatEncryption, Describes: "full-disk encryption on this machine, in three values (PRD §4.1)",
			State: StateUndetermined, Reason: ReasonCouldNotRead,
			Detail: "the health run did not report a full-disk-encryption assumption on this platform"})
	}

	// ---- platform and device ----------------------------------------------------------------
	fd, err := writeCollected(dir, "device.json", map[string]any{
		"platform": goos,
		"arch":     runtime.GOARCH,
	})
	if err != nil {
		return man, err
	}
	set(Category{Name: CatPlatform, Describes: "the operating system and architecture this bundle came from",
		State: StateCollected, Items: 1, Files: []string{fd}})
	// NAMED RATHER THAN OMITTED. PRD §3.8's device label is owned by Issue #17, whose package is not
	// in this build. A supporter reading this manifest learns that the bundle cannot say which
	// device it came from — which is a worse bundle than one that can, and a much better bundle
	// than one that is silent about the gap.
	set(Category{Name: CatDeviceLabel, Describes: "the label this device is registered under (PRD §3.8)",
		State: StateUndetermined, Reason: ReasonNotInThisBuild,
		Detail: "this build has no device registry, so the device's label could not be read; this is not a report that the device has no label"})

	// ---- hub (PRD §4.4; no network connection is opened) --------------------------------------
	hubConfigured := getenv(health.HubEnv) != ""
	fhub, err := writeCollected(dir, "hub.json", map[string]any{
		"configured":            hubConfigured,
		"configuration_source":  health.HubEnv,
		"contacted":             false,
		"contacted_explanation": "producing a bundle opens no network connection and transmits the bundle nowhere",
	})
	if err != nil {
		return man, err
	}
	set(Category{Name: CatHubConfiguration, Describes: "whether a hub is configured for this machine",
		State: StateCollected, Items: 1, Files: []string{fhub}})
	if hubConfigured {
		set(Category{Name: CatHubDerivedState, Describes: "state that only the hub holds — membership, the published index",
			State: StateUndetermined, Reason: ReasonNoNetwork,
			Detail: "a hub is configured, and producing a bundle opens no network connection, so hub-side state was not established here"})
	} else {
		set(Category{Name: CatHubDerivedState, Describes: "state that only the hub holds — membership, the published index",
			State: StateUndetermined, Reason: ReasonNoHub,
			Detail: "no hub is configured on this machine, so hub-side state is unavailable; this is not a finding about the hub"})
	}

	// ---- environment ------------------------------------------------------------------------
	//
	// NAMES, NEVER VALUES. Which of the product's variables are set is diagnostic; what they are set
	// to may be a key, a path into somebody's home directory, or a hub token.
	setVars := make([]string, 0, len(configVars))
	for _, v := range configVars {
		if getenv(v) != "" {
			setVars = append(setVars, v)
		}
	}
	sort.Strings(setVars)
	fe, err := writeCollected(dir, "config.json", map[string]any{
		"variables_set": setVars,
		"note":          "variable names only; no value of any environment variable is in this bundle",
	})
	if err != nil {
		return man, err
	}
	set(Category{Name: CatEnvConfiguration, Describes: "which of the product's configuration variables are set — names only, never values",
		State: StateCollected, Items: len(setVars), Files: []string{fe}})

	if err := man.complete(); err != nil {
		return man, err
	}
	return man, nil
}

// configVars are the product's configuration variables whose PRESENCE is diagnostic.
var configVars = []string{store.PathEnv, health.HubEnv, "XDG_DATA_HOME", "HOME"}

// newManifest seeds every category as undetermined-because-nothing-ran.
//
// THE SEED IS WHY A CATEGORY CANNOT GO SILENTLY MISSING. Every name in categoryNames is present
// before any gathering happens, so a gather path that returns early, panics past a set, or forgets
// a branch leaves an explicit "this was not reached" line rather than a hole. complete() then
// refuses any manifest still carrying the seed, so the seed can never ship either.
func newManifest(at time.Time, bodies bool) Manifest {
	req := BodiesNotRequested
	if bodies {
		req = BodiesRequested
	}
	m := Manifest{
		Format:      manifestFormat,
		Product:     "omw",
		GeneratedAt: at.UTC(),
		// SET FROM THE SAME VALUE THE GATHER IS DRIVEN BY. Criterion 7 — the manifest cannot
		// understate what the bundle holds, because there is one bool and both readings come from it.
		BodiesIncluded: bodies,
		BodiesRequest:  req,
	}
	for _, n := range categoryNames {
		m.Categories = append(m.Categories, Category{
			Name: n, State: StateUndetermined, Reason: seedReason,
			Detail: "this category was not reached by the run that produced this bundle",
		})
	}
	return m
}

// seedReason marks a category no gather branch has spoken for. It never survives a successful run.
const seedReason Reason = "not-reached-by-this-run"

func (m *Manifest) set(c Category) {
	if c.State == StateCollected {
		c.Reason = ReasonNone
	}
	for i := range m.Categories {
		if m.Categories[i].Name == c.Name {
			m.Categories[i] = c
			return
		}
	}
	// A category name not in the fixed list. Appended rather than dropped — an unnamed category in
	// the bundle is the failure this whole file is against — and complete() then rejects it.
	m.Categories = append(m.Categories, c)
}

// complete refuses a manifest that would ship a lie or a hole.
func (m *Manifest) complete() error {
	seen := map[string]bool{}
	for _, c := range m.Categories {
		if !known(c.Name) {
			return fmt.Errorf("the bundle produced a category the manifest does not know: %q", c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("the manifest names %q twice", c.Name)
		}
		seen[c.Name] = true
		if c.Reason == seedReason {
			return fmt.Errorf("no branch of the run spoke for category %q, so the bundle would have shipped an unexplained gap", c.Name)
		}
		if c.State != StateCollected && c.Detail == "" {
			return fmt.Errorf("category %q is %s with no reason a person could read", c.Name, c.State)
		}
		if c.State != StateCollected && len(c.Files) != 0 {
			return fmt.Errorf("category %q is %s and yet names files in the bundle", c.Name, c.State)
		}
		if c.State == StateCollected && len(c.Files) == 0 {
			return fmt.Errorf("category %q is collected and names no file, so a reader cannot find it", c.Name)
		}
	}
	for _, n := range categoryNames {
		if !seen[n] {
			return fmt.Errorf("the manifest is missing category %q", n)
		}
	}
	return nil
}

func known(name string) bool {
	for _, n := range categoryNames {
		if n == name {
			return true
		}
	}
	return false
}

// writeCollected writes one payload file and returns its bundle-relative name — the same string the
// manifest records, produced by the same call, so the two cannot be written independently.
func writeCollected(dir, rel string, payload any) (string, error) {
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", fmt.Errorf("preparing %s in the bundle: %w", rel, err)
	}
	if err := writeJSON(full, payload); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func writeJSON(path string, payload any) error {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", filepath.Base(path), err)
	}
	body = append(body, '\n')
	// 0o600: a support bundle holds diagnostic facts about a person's machine and, on request,
	// their material. It is theirs to hand over, not the machine's to share.
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ReadManifest reads a bundle's manifest. It is the operation a person who has ONLY the bundle
// performs (criterion 1), and it opens nothing else in the bundle to do it.
func ReadManifest(bundle string) (Manifest, error) {
	body, err := os.ReadFile(filepath.Join(bundle, manifestName))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
