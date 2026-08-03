package publish

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// ---------------------------------------------------------------------------
// Doubles
// ---------------------------------------------------------------------------

// grantingGate is the gate for tests that are about the TRANSFER and not about the gate.
//
// IT IS EXPLICIT ON PURPOSE, and it is why [Config.Gate] has no permissive default: every test that
// publishes has to say out loud that it meant to be allowed to. A zero Config that published would
// make every one of those tests evidence for a machine that does not gate.
type grantingGate struct{}

func (grantingGate) MayLeave(*drafts.Outbox, hub.NoteID) Decision {
	return Decision{Permission: PermissionGranted}
}

// fixedReviewer is a stand-in for the person's model. Issue #18 owns the real one.
type fixedReviewer struct {
	answer string
	err    error
}

func (r fixedReviewer) Review(rules, body string) (string, error) { return r.answer, r.err }

// gateWorld builds a real store with a mode and rules recorded, plus the outbox beside it.
func gateWorld(t *testing.T, mode drafts.Mode) (*store.Store, *client) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := store.Create(root)
	if err != nil {
		t.Fatalf("creating the store this test drives against: %v", err)
	}
	if err := drafts.WriteMode(s, mode); err != nil {
		t.Fatalf("recording the mode: %v", err)
	}
	if err := drafts.WriteRules(s, "never mention customer names"); err != nil {
		t.Fatalf("recording the rules: %v", err)
	}
	c := newClient(t)
	return s, c
}

// ---------------------------------------------------------------------------
// The four directions product named
// ---------------------------------------------------------------------------

// TestTheGateDecidesAllFourDirectionsAndUndeterminedNeverPublishes is product's ruling of
// 2026-08-03, driven in every direction it names.
//
// The defect it pins: `omw outbox publish` ran the gate and `omw publish note` did not, so a draft
// in `review` mode with no model — one the client itself called "NOT checked and will not be
// published" — reached a real hub with exit 0. The gate lived in the caller and a second caller
// appeared. It now lives in [Transfer], which is the only way to a hub.
//
// THE FOURTH DIRECTION IS THE ONE TO GET WRONG. "Could not determine whether this may leave" must
// not fall through to "may leave". It is asserted here as its own outcome, distinct from BOTH a
// grant and a refusal, with the note still in the outbox and the hub empty.
func TestTheGateDecidesAllFourDirectionsAndUndeterminedNeverPublishes(t *testing.T) {
	cases := []struct {
		name     string
		mode     drafts.Mode
		model    drafts.ModelConfig
		reviewer drafts.Reviewer
		want     Attempt
		wantHub  int
	}{{
		// 1. An unchecked `review` draft is REFUSED, naming the mode.
		name:    "review with no model is refused",
		mode:    drafts.ModeReview,
		model:   drafts.ModelConfig{Configured: tri.No, Missing: "no model and no key are configured"},
		want:    AttemptGateRefused,
		wantHub: 0,
	}, {
		// 2. An `auto` draft still publishes.
		name:    "auto still publishes",
		mode:    drafts.ModeAuto,
		want:    AttemptPublished,
		wantHub: 1,
	}, {
		// 3. A CHECKED `review` draft still publishes.
		name:     "review that passes still publishes",
		mode:     drafts.ModeReview,
		model:    drafts.ModelConfig{Configured: tri.Yes, Name: "a-model"},
		reviewer: fixedReviewer{answer: "pass"},
		want:     AttemptPublished,
		wantHub:  1,
	}, {
		// 4. Reviewability CANNOT BE DETERMINED — and it does NOT publish.
		name:     "a model that cannot be reached is undetermined and does not publish",
		mode:     drafts.ModeReview,
		model:    drafts.ModelConfig{Configured: tri.Yes, Name: "a-model"},
		reviewer: fixedReviewer{err: errors.New("dial tcp: no route to host")},
		want:     AttemptGateUndetermined,
		wantHub:  0,
	}, {
		// 4b. The model answered, and not with a verdict. Also undetermined — "if it did not say
		// refuse then it passed" is a default-open gate.
		name:     "an answer that is not a verdict is undetermined and does not publish",
		mode:     drafts.ModeReview,
		model:    drafts.ModelConfig{Configured: tri.Yes, Name: "a-model"},
		reviewer: fixedReviewer{answer: "I'm sorry, I can't help with that."},
		want:     AttemptGateUndetermined,
		wantHub:  0,
	}, {
		// 4c. Whether a model is configured could not be determined. Not "no model" (which would be
		// a refusal) and emphatically not a pass.
		name:    "a model whose configuration is undetermined does not publish",
		mode:    drafts.ModeReview,
		model:   drafts.ModelConfig{Configured: tri.Undetermined, Why: "the key file could not be read"},
		want:    AttemptGateUndetermined,
		wantHub: 0,
	}, {
		// The rules were checked and they said no. A determined refusal, and NOT the hub's.
		name:     "rules that refuse are a determined refusal",
		mode:     drafts.ModeReview,
		model:    drafts.ModelConfig{Configured: tri.Yes, Name: "a-model"},
		reviewer: fixedReviewer{answer: "refuse: this names a customer"},
		want:     AttemptGateRefused,
		wantHub:  0,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, c := gateWorld(t, tc.mode)
			h := newHub(t) // a REAL, REACHABLE hub — so nothing but the gate can stop this
			draft(t, c, "n", "the body")

			res := Transfer(c.l, c.o, "n", Config{
				HubAddr: h.addr, Author: author, Scopes: publisher, Title: "t",
				Gate: ReviewGate{Store: s, Model: tc.model, Reviewer: tc.reviewer},
			})

			if res.Attempt != tc.want {
				t.Errorf("attempt = %v, want %v (detail: %s)", res.Attempt, tc.want, res.Detail)
			}
			if got := h.store.Count(); got != tc.wantHub {
				t.Errorf("the hub holds %d note(s), want %d", got, tc.wantHub)
			}
			// NOTHING THAT DID NOT PUBLISH MAY HAVE LEFT THE OUTBOX. "Never neither" applies to a
			// gate refusal exactly as it applies to a hub refusal.
			if tc.wantHub == 0 && !res.Report.InOutbox() {
				t.Errorf("this draft did not publish and is not in the outbox either — it is nowhere")
			}
		})
	}
}

// TestAGateRefusalAndAGateUndeterminedAreDifferentOutcomes compares the two answers TO EACH OTHER.
// Asserting each against a literal passes just as happily once the two have collapsed onto one.
func TestAGateRefusalAndAGateUndeterminedAreDifferentOutcomes(t *testing.T) {
	run := func(model drafts.ModelConfig, r drafts.Reviewer) Result {
		s, c := gateWorld(t, drafts.ModeReview)
		h := newHub(t)
		draft(t, c, "n", "the body")
		return Transfer(c.l, c.o, "n", Config{
			HubAddr: h.addr, Author: author, Scopes: publisher, Title: "t",
			Gate: ReviewGate{Store: s, Model: model, Reviewer: r},
		})
	}
	refused := run(drafts.ModelConfig{Configured: tri.No, Missing: "none configured"}, nil)
	undetermined := run(drafts.ModelConfig{Configured: tri.Yes, Name: "m"}, fixedReviewer{err: errors.New("unreachable")})

	if refused.Attempt == undetermined.Attempt {
		t.Fatalf("a gate refusal and an undetermined gate both report %v — `determined to be no` and "+
			"`could not determine` have collapsed", refused.Attempt)
	}
	if refused.Code == undetermined.Code {
		t.Errorf("both carry the code %q; a caller cannot branch on it", refused.Code)
	}
	// AND NEITHER IS THE HUB'S REFUSAL. The hub was never asked in either case.
	if refused.Attempt == AttemptRefused || undetermined.Attempt == AttemptRefused {
		t.Errorf("the person's own gate reported %v, which means the HUB refused — it was never asked",
			AttemptRefused)
	}
}

// TestATransferWithNoGateSuppliedDoesNotPublish is the fail-safe at the seam where the next caller
// arrives. Issue #16's agent API is the next caller, and it will construct a Config.
//
// An absent gate is UNDETERMINED, not permitted: "nobody told me about a gate" is not "there is no
// gate". If this ever goes green by publishing, the ruling has been undone by omission.
func TestATransferWithNoGateSuppliedDoesNotPublish(t *testing.T) {
	_, c := gateWorld(t, drafts.ModeAuto) // even in `auto`, the most permissive mode there is
	h := newHub(t)
	draft(t, c, "n", "the body")

	res := Transfer(c.l, c.o, "n", Config{HubAddr: h.addr, Author: author, Scopes: publisher, Title: "t"})

	if res.Attempt != AttemptGateUndetermined {
		t.Errorf("attempt = %v, want %v — a Config with no Gate must not publish", res.Attempt, AttemptGateUndetermined)
	}
	if res.Code != ErrNoGate.Code {
		t.Errorf("code = %q, want %q", res.Code, ErrNoGate.Code)
	}
	if got := h.store.Count(); got != 0 {
		t.Errorf("a transfer with no gate put %d note(s) on the hub", got)
	}
}

// ---------------------------------------------------------------------------
// The structural guard
// ---------------------------------------------------------------------------

// TestEveryPathToTheHubPassesTheGate is the guard product asked for: it fails if ANY publish path
// reaches the hub without passing the gate.
//
// It is structural rather than behavioural because the failure it guards against is a NEW caller —
// one that does not exist yet and therefore has no test. What it establishes: the only function in
// this package that dials a hub is [send], every call to [send] is inside [Transfer], and [Transfer]
// consults the gate before it. Break any of those three and the note could go out ungated.
//
// It carries its own control — see the planted violation below — because a guard that has never
// been watched go red is a guard that might be checking nothing at all.
func TestEveryPathToTheHubPassesTheGate(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}

	var senders []string // functions that call send()
	var gated []string   // functions that consult the gate
	checked := 0

	for _, p := range pkg {
		for name, file := range p.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				checked++
				callsSend, callsGate := false, false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch f := call.Fun.(type) {
					case *ast.Ident:
						if f.Name == "send" {
							callsSend = true
						}
						if f.Name == "gateDecision" {
							callsGate = true
						}
					case *ast.SelectorExpr:
						if f.Sel.Name == "MayLeave" {
							callsGate = true
						}
					}
					return true
				})
				where := fn.Name.Name + " (" + filepath.Base(name) + ")"
				if callsSend {
					senders = append(senders, where)
				}
				if callsGate {
					gated = append(gated, where)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("this guard examined no functions at all, so its result means nothing")
	}
	// EXACTLY ONE FUNCTION MAY REACH THE HUB, and it must be the gated one.
	if len(senders) != 1 {
		t.Fatalf("%d function(s) call send() and reach a hub: %v\n"+
			"Every path to a hub must pass the gate, so there is exactly one and it is Transfer.",
			len(senders), senders)
	}
	if !strings.HasPrefix(senders[0], "Transfer ") {
		t.Fatalf("the function that reaches a hub is %q, not Transfer — the gate is in Transfer, so "+
			"this path does not pass it", senders[0])
	}
	gatedSet := strings.Join(gated, " ")
	if !strings.Contains(gatedSet, "Transfer ") {
		t.Fatalf("Transfer reaches a hub and does NOT consult the gate. Gated functions: %v\n"+
			"This is the defect product's ruling of 2026-08-03 is about: a publish path that "+
			"reaches the hub without asking whether the draft may leave.", gated)
	}
	t.Logf("examined %d function(s); one path to a hub (%s), and it is gated", checked, senders[0])
}

// TestTheGateGuardFiresWhenAPathIsPlanted is the guard's control.
//
// It writes a second `send` caller into the package directory, runs the SAME analysis, and requires
// it to go red. Product asked for this explicitly, and this project has shipped three guards that
// were checking nothing. A guard nobody has watched fail is a comment.
func TestTheGateGuardFiresWhenAPathIsPlanted(t *testing.T) {
	// The violation: a second, ungated route to a hub — exactly the shape `omw publish note` had
	// before the ruling.
	planted := []byte(`package publish

func qaUngatedPublishProbe(cfg Config, body string) (Response, bool, error) {
	return send(cfg.HubAddr, Request{Author: string(cfg.Author), Body: body})
}
`)
	path := filepath.Join(".", "zz_qa_planted_violation.go")
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s already exists; refusing to overwrite it", path)
	}
	if err := os.WriteFile(path, planted, 0o600); err != nil {
		t.Fatalf("planting the violation: %v", err)
	}
	defer os.Remove(path)

	// Re-run the guard's analysis over the package as it now stands.
	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	var senders []string
	for _, p := range pkg {
		for name, file := range p.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "send" {
							senders = append(senders, fn.Name.Name+" ("+filepath.Base(name)+")")
						}
					}
					return true
				})
			}
		}
	}

	// THE CONTROL. With the violation planted the guard MUST see two paths to a hub.
	if len(senders) != 2 {
		t.Fatalf("a second, ungated path to the hub was planted and the guard saw %d path(s): %v\n"+
			"The guard does not detect the thing it exists to detect.", len(senders), senders)
	}
	found := false
	for _, s := range senders {
		if strings.HasPrefix(s, "qaUngatedPublishProbe ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the guard saw two paths but did not name the planted one: %v", senders)
	}
	t.Logf("guard fires: with a violation planted it reports %v", senders)
}
