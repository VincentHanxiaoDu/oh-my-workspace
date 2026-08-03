package commands

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/drafts"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/model"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// Issue #18 criteria 5, 12 and 13, and the PRD §5.2 ruling: "`review` runs on the client against
// rules the person wrote, using their own model and key. The hub accepts or refuses on
// visibility/scope grounds only."

// CRITERION 12: "with no hub configured at all, `review` mode fully evaluates a draft and reaches a
// verdict — the hub is not consulted and no network connection opens to any hub."
//
// The verdict is REACHED, not merely attempted: a pass and a refusal are both driven, because a
// review that could only ever fail would satisfy a weaker reading of this and would be useless.
func TestReviewReachesAVerdictWithNoHubConfiguredAtAll(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answer   string
		wantCode int
		wantSaid string
	}{
		{"passes", "pass", cli.Success, "passed"},
		{"refuses", "refuse: this names a customer", cli.ExitFailure, "refused"},
	} {
		env := mdWorld(t)
		if env["OMW_HUB"] != "" {
			t.Fatal("this test's environment has a hub, so it is not driving what it names")
		}
		env[model.EnvProvider] = "acme"
		env[model.EnvCredential] = mdSecret

		prev := outboxReviewer
		outboxReviewer = func(cli.Env, model.Config) drafts.Reviewer {
			return mdScripted{answer: tc.answer}
		}

		mdSetUpReviewDraft(t, env)
		got := mdRunAny(t, env, "outbox", "review", "a-draft")
		outboxReviewer = prev

		if got.code != tc.wantCode {
			t.Errorf("%s: review exited %d, want %d\n%s", tc.name, got.code, tc.wantCode, got.all())
		}
		if !strings.Contains(got.all(), tc.wantSaid) {
			t.Errorf("%s: review did not reach a verdict:\n%s", tc.name, got.all())
		}
		// NOTHING MENTIONS A HUB, because nothing consulted one. A review that said "no hub
		// configured" anywhere would be this capability half-working for want of something §5.2
		// rules it does not need.
		if strings.Contains(strings.ToLower(got.all()), "hub") {
			t.Errorf("%s: review mentioned a hub, and §5.2 says it runs on the client:\n%s", tc.name, got.all())
		}
	}
}

// CRITERION 13: "The hub never supplies, selects, or overrides the model or credential used for
// `review`."
//
// It is driven by CONFIGURING A HUB and asserting the model that reaches the reviewer is the local
// one, byte for byte — including the credential, which the seam receives and which nothing else
// may have substituted.
func TestTheHubNeverSuppliesOrOverridesTheModel(t *testing.T) {
	env := mdWorld(t)
	env["OMW_HUB"] = "https://hub.example.invalid"
	env[model.EnvProvider] = "acme"
	env[model.EnvCredential] = mdSecret

	var sawProvider, sawCredential string
	prev := outboxReviewer
	outboxReviewer = func(_ cli.Env, cfg model.Config) drafts.Reviewer {
		sawProvider, sawCredential = cfg.Name, cfg.Secret()
		return mdScripted{answer: "pass"}
	}
	t.Cleanup(func() { outboxReviewer = prev })

	mdSetUpReviewDraft(t, env)
	got := mdRunAny(t, env, "outbox", "review", "a-draft")
	if got.code != cli.Success {
		t.Fatalf("review with a hub configured exited %d:\n%s", got.code, got.all())
	}
	if sawProvider != "acme" {
		t.Errorf("review ran against provider %q, want the locally configured acme", sawProvider)
	}
	if sawCredential != mdSecret {
		t.Errorf("review ran with a credential that is not the local one (%q)", sawCredential)
	}
}

// CRITERION 5: "no hub response can install, replace, or clear a locally configured credential …
// assert the locally configured provider and credential are unchanged after a full hub sync."
//
// WHAT IS REAL AND WHAT IS NOT, SAID PLAINLY. This build has no hub transport — that is Issue #10 —
// so there is no live sync to run and no outbound request to inspect. What IS real is every
// hub-touching operation the binary has today: they are all run with a hub configured, and the
// locally resolved provider and credential must be identical before and afterwards. The half this
// cannot drive is stated in the pull request rather than papered over with a fake hub whose
// responses this test would have written itself.
func TestNoHubTouchingOperationChangesTheLocalModelConfiguration(t *testing.T) {
	env := mdWorld(t)
	env["OMW_HUB"] = "https://hub.example.invalid"
	mdMustRun(t, env, "use", "acme")
	env[model.EnvCredential] = mdSecret

	read := func() (string, string) {
		s, err := store.Open(env[store.PathEnv])
		if err != nil {
			t.Fatal(err)
		}
		cfg := model.Read(func(k string) string { return env[k] }, s)
		return cfg.Name, cfg.Secret()
	}

	beforeName, beforeCred := read()
	if beforeName != "acme" || beforeCred != mdSecret {
		t.Fatalf("the configuration under test did not resolve (%q), so this test proves nothing", beforeName)
	}

	for _, c := range cli.Commands() {
		for _, args := range [][]string{{c.Name}, {c.Name, "list"}, {c.Name, "status"}} {
			var out, errb bytes.Buffer
			_ = cli.Run(args, &out, &errb, func(k string) string { return env[k] })
			if strings.Contains(out.String()+errb.String(), mdSecret) {
				t.Errorf("omw %s emitted the credential with a hub configured:\n%s%s",
					strings.Join(args, " "), out.String(), errb.String())
			}
		}
	}

	afterName, afterCred := read()
	if afterName != beforeName || afterCred != beforeCred {
		t.Errorf("running every hub-touching operation changed the local model configuration:\n"+
			"  before: provider=%q credential-present=%v\n  after:  provider=%q credential-present=%v",
			beforeName, beforeCred != "", afterName, afterCred != "")
	}
}

// CRITERION 12, STRUCTURALLY: the review gate does not read the hub.
//
// The behavioural test above shows one run did not consult a hub. This shows the code CANNOT: the
// function that decides whether a draft may leave names no hub environment variable and calls
// nothing hub-shaped. A behavioural test passes on the day somebody adds a hub read behind a
// condition it does not happen to take.
func TestTheReviewGateNamesNoHub(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "outbox_cmd.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing outbox_cmd.go: %v", err)
	}

	var gate *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "outboxReviewGate" {
			gate = fn
		}
	}
	if gate == nil {
		t.Fatal("outboxReviewGate is not in this file any more, so this guard is not guarding the gate")
	}

	found := 0
	ast.Inspect(gate, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING && strings.Contains(strings.ToLower(v.Value), "hub") {
				t.Errorf("outboxReviewGate names %s; §5.2 says review runs on the client and consults no hub", v.Value)
			}
		case *ast.Ident:
			found++
			if strings.Contains(v.Name, "HubIsConfigured") || v.Name == "outboxEnvHub" {
				t.Errorf("outboxReviewGate reaches for the hub through %s; §5.2 forbids it", v.Name)
			}
		}
		return true
	})
	if found == 0 {
		t.Fatal("the walk of outboxReviewGate visited nothing, so its pass proves nothing")
	}
}
