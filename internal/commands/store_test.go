package commands

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// runStoreCmd drives `omw store ...` the way a person does, and returns what they would see.
//
// Everything below asserts on the exit code AND the text, because Issue #3 states most of its
// criteria as "distinguishable by exit code alone" and the rest as "says which".
func runStoreCmd(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(append([]string{"store"}, args...), &out, &errb, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

// envFor is a sandboxed environment naming one store.
//
// HOME is redirected as well as OMW_STORE, because this device's store pointer (criterion 4) lives
// under HOME and no test may read or write the pointer belonging to the machine it runs on.
func envFor(t *testing.T, storePath string) map[string]string {
	t.Helper()
	return map[string]string{store.PathEnv: storePath, "HOME": t.TempDir()}
}

func storeEnv(t *testing.T) (map[string]string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	return envFor(t, root), root
}

// CRITERION 1: one store, and the absolute path printed on success.
func TestStoreCreatePrintsTheAbsolutePathItCreated(t *testing.T) {
	env, root := storeEnv(t)
	code, stdout, stderr := runStoreCmd(t, env, "create")
	if code != cli.Success {
		t.Fatalf("exit %d; want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, root) {
		t.Errorf("the output does not name the path it created.\nwant to see: %s\ngot:\n%s", root, stdout)
	}
	if store.Exists(root) != tri.Yes {
		t.Errorf("no store at %s after a successful create", root)
	}
}

// CRITERION 3: the second run differs from the first BY EXIT CODE ALONE, and changes nothing.
func TestStoreCreateTwiceIsRefusedByExitCodeAlone(t *testing.T) {
	env, root := storeEnv(t)
	first, _, _ := runStoreCmd(t, env, "create")
	if first != cli.Success {
		t.Fatalf("first create exited %d", first)
	}
	before := treeSnapshot(t, root)

	second, stdout, stderr := runStoreCmd(t, env, "create")
	if second == cli.Success {
		t.Fatalf("the second create succeeded; criterion 3 requires a non-zero exit")
	}
	if second == first {
		t.Fatalf("both runs exited %d — the two outcomes must be distinguishable by exit code alone", first)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("the refusal does not say a store is already there:\n%s%s", stdout, stderr)
	}
	after := treeSnapshot(t, root)
	if len(before) != len(after) {
		t.Fatalf("the store gained or lost files: %d -> %d", len(before), len(after))
	}
	for name, content := range before {
		if after[name] != content {
			t.Errorf("%s is not byte-identical after the refused second create", name)
		}
	}
}

// CRITERION 5: a synchronising location is refused, non-zero, with nothing left behind.
func TestStoreCreateRefusesASynchronisingLocation(t *testing.T) {
	dropbox := filepath.Join(t.TempDir(), "Dropbox")
	if err := os.MkdirAll(dropbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropbox, ".dropbox"), []byte("marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dropbox, "store")

	code, _, stderr := runStoreCmd(t, envFor(t, target), "create")
	if code == cli.Success {
		t.Fatalf("creating inside a Dropbox root succeeded")
	}
	if !strings.Contains(stderr, "Dropbox") {
		t.Errorf("the refusal does not name Dropbox:\n%s", stderr)
	}
	if !strings.Contains(stderr, dropbox) {
		t.Errorf("the refusal does not name where the evidence was found:\n%s", stderr)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("something was left at %s", target)
	}
}

// CRITERION 6: "this is Dropbox", "this path does not exist" and "I lack permission to write here"
// are THREE outputs, not one shared "could not create the store".
func TestTheThreeCreationFailuresReadDifferently(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which cannot be denied write permission")
	}

	dropbox := filepath.Join(t.TempDir(), "Dropbox")
	if err := os.MkdirAll(dropbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropbox, ".dropbox"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}

	readonly := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readonly, 0o755) })

	cases := map[string]struct {
		target string
		says   string
		notSay []string
	}{
		"this is Dropbox": {
			target: filepath.Join(dropbox, "store"),
			says:   "Dropbox",
			notSay: []string{"does not exist", "permission"},
		},
		"this path does not exist": {
			target: filepath.Join(t.TempDir(), "no", "such", "parent", "store"),
			says:   "does not exist",
			notSay: []string{"Dropbox", "permission"},
		},
		"I lack permission to write here": {
			target: filepath.Join(readonly, "store"),
			says:   "permission",
			notSay: []string{"Dropbox", "does not exist"},
		},
	}
	seen := map[string]string{}
	for name, c := range cases {
		code, _, stderr := runStoreCmd(t, envFor(t, c.target), "create")
		if code == cli.Success {
			t.Fatalf("%s: create succeeded", name)
		}
		if !strings.Contains(stderr, c.says) {
			t.Errorf("%s: the output never says %q:\n%s", name, c.says, stderr)
		}
		for _, no := range c.notSay {
			if strings.Contains(stderr, no) {
				t.Errorf("%s: the output also says %q, so this failure is not distinguishable from another:\n%s", name, no, stderr)
			}
		}
		for other, text := range seen {
			if text == stderr {
				t.Errorf("%q and %q produce identical output:\n%s", name, other, stderr)
			}
		}
		seen[name] = stderr
	}
}

// CRITERION 9 AND THE OPEN DECISION. Undetermined renders as neither of the settled outcomes, has
// its own exit code, and says out loud that the product has not ruled on what to do here.
func TestUndeterminedSyncIsItsOwnOutcome(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	opaque := filepath.Join(t.TempDir(), "opaque")
	if err := os.MkdirAll(filepath.Join(opaque, "here"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(opaque, 0o111); err != nil {
		t.Skipf("permissions not honoured here: %v", err)
	}
	t.Cleanup(func() { os.Chmod(opaque, 0o755) })
	target := filepath.Join(opaque, "here", "store")

	// The two settled outcomes, for comparison.
	okEnv, _ := storeEnv(t)
	createdCode, createdOut, _ := runStoreCmd(t, okEnv, "create")

	dropbox := filepath.Join(t.TempDir(), "Dropbox")
	os.MkdirAll(dropbox, 0o755)
	os.WriteFile(filepath.Join(dropbox, ".dropbox"), []byte("m"), 0o644)
	refusedCode, _, refusedErr := runStoreCmd(t, envFor(t, filepath.Join(dropbox, "store")), "create")

	code, stdout, stderr := runStoreCmd(t, envFor(t, target), "create")

	if code == createdCode {
		t.Errorf("undetermined exits %d, the same as 'confirmed local, created' — criterion 9 requires the two be distinguishable", code)
	}
	if code == refusedCode {
		t.Errorf("undetermined exits %d, the same as 'confirmed synchronising, refused' — criterion 9 requires the two be distinguishable", code)
	}
	if code != cli.ExitUndetermined {
		t.Errorf("undetermined exits %d; want %d, the code reserved so that 'could not determine' is never scripted as 'the answer is no'", code, cli.ExitUndetermined)
	}
	if strings.TrimSpace(stdout+stderr) == "" {
		t.Fatal("the undetermined case is silent, and silence is not one of the three answers (§4.3)")
	}
	if !strings.Contains(stderr, "could not be determined") {
		t.Errorf("the output never says the state could not be determined:\n%s", stderr)
	}
	// THE RULING IS HALT-WITH-AN-OVERRIDE, so the refusal has to tell the person the exact thing
	// they can type. A halt that does not is a dead end.
	if !strings.Contains(stderr, overrideFlag) {
		t.Errorf("the refusal does not tell the person how to proceed on purpose:\n%s", stderr)
	}
	if strings.Contains(stderr, "no ruling") || strings.Contains(stderr, "Blocked on a decision") {
		t.Errorf("the output still claims the product has not ruled on this:\n%s", stderr)
	}
	if stderr == refusedErr || stdout == createdOut {
		t.Error("the undetermined output is identical to one of the settled outcomes")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("a store was created at %s despite the undetermined probe", target)
	}
}

// CRITERION 2, as far as this branch can drive it: every command surface that exists here, run with
// no store present, creates none — and each says the absence of a store is what stopped it.
//
// HONESTLY SCOPED. The criterion lists status, a project listing, a ticket listing, health and the
// control API entry point; none of those exists on this branch. What is driven is the whole of the
// registered command surface at the time this test runs, so a command added later that quietly
// creates a store fails this test rather than slipping past it.
func TestNoCommandSurfaceCreatesAStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	env := envFor(t, root)

	surfaces := [][]string{
		{"store", "path"},
		{"store", "status"},
		{"help"},
	}
	for _, args := range surfaces {
		var out, errb bytes.Buffer
		code := cli.Run(args, &out, &errb, func(k string) string { return env[k] })
		text := out.String() + errb.String()

		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("%v created a store at %s — only 'store create' may do that", args, root)
		}
		if args[0] == "help" {
			continue
		}
		if code == cli.Success {
			t.Errorf("%v exited 0 with no store present; the absence is the reason it could do no more", args)
		}
		if !strings.Contains(text, "no store") {
			t.Errorf("%v does not name the absence of a store as the reason:\n%s", args, text)
		}
		// A store that is absent and a store that is present and empty are different facts.
		if strings.Contains(text, "the store is empty") {
			t.Errorf("%v describes a MISSING store as an empty one:\n%s", args, text)
		}
	}
}

// An empty store and an absent store must not read the same (criterion 2, second half).
func TestAnEmptyStoreReadsDifferentlyFromAnAbsentOne(t *testing.T) {
	env, _ := storeEnv(t)
	absentCode, absentOut, absentErr := runStoreCmd(t, env, "status")
	if code, _, e := runStoreCmd(t, env, "create"); code != cli.Success {
		t.Fatalf("create exited %d: %s", code, e)
	}
	emptyCode, emptyOut, emptyErr := runStoreCmd(t, env, "status")

	if absentOut+absentErr == emptyOut+emptyErr {
		t.Fatalf("an absent store and an empty store produce the same report:\n%s", emptyOut)
	}
	if absentCode == emptyCode {
		t.Errorf("both exit %d; 'there is no store' and 'the store is here and empty' are different answers", absentCode)
	}
	if !strings.Contains(emptyOut, "empty") {
		t.Errorf("the empty store's report does not say it is empty:\n%s", emptyOut)
	}
}

// CRITERION 8: a store whose location becomes synchronising afterwards is reported as such.
func TestStatusReportsALocationThatBecomesSynchronising(t *testing.T) {
	env, root := storeEnv(t)
	if code, _, errText := runStoreCmd(t, env, "create"); code != cli.Success {
		t.Fatalf("create exited %d: %s", code, errText)
	}
	okCode, okOut, _ := runStoreCmd(t, env, "status")
	if okCode != cli.Success {
		t.Fatalf("status on a healthy store exited %d:\n%s", okCode, okOut)
	}
	if !strings.Contains(okOut, "confirmed off the sync path") {
		t.Errorf("status does not confirm the location is off the sync path:\n%s", okOut)
	}

	// The store is placed under a sync root after the fact.
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), ".dropbox"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errText := runStoreCmd(t, env, "status")
	if code == cli.Success {
		t.Errorf("status exits 0 for a store that is now synchronising off the machine")
	}
	if !strings.Contains(out, "synchronises off this machine") || !strings.Contains(out, "Dropbox") {
		t.Errorf("status does not report the location as synchronising:\n%s%s", out, errText)
	}
	if out == okOut {
		t.Error("the two location reports are identical")
	}
}

// CRITERION 13 at the command surface: an unreadable store is never presented as an empty one.
func TestStatusOnAnUnreadableStoreSaysSoAndExitsNonZero(t *testing.T) {
	env, root := storeEnv(t)
	if code, _, e := runStoreCmd(t, env, "create"); code != cli.Success {
		t.Fatalf("create exited %d: %s", code, e)
	}
	if err := os.WriteFile(filepath.Join(root, "store.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errText := runStoreCmd(t, env, "status")
	if code == cli.Success {
		t.Errorf("status on an unreadable store exited 0")
	}
	text := out + errText
	if !strings.Contains(text, "cannot be read") {
		t.Errorf("status does not say the store cannot be read:\n%s", text)
	}
	if strings.Contains(text, "the store is empty") || strings.Contains(text, "no store has been created") {
		t.Errorf("an unreadable store is presented as an empty or absent one:\n%s", text)
	}
}

// CRITERION 15: the product exposes the store's path.
func TestStorePathExposesWhereTheStoreIs(t *testing.T) {
	env, root := storeEnv(t)
	runStoreCmd(t, env, "create")
	code, out, _ := runStoreCmd(t, env, "path")
	if code != cli.Success {
		t.Fatalf("store path exited %d", code)
	}
	if strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]) != root {
		t.Fatalf("store path printed %q; want %q on the first line so a script can read it", out, root)
	}
}

// A location that cannot be worked out at all is undetermined, not "no store".
func TestAnUndeterminedLocationIsNotAnAbsentStore(t *testing.T) {
	code, _, stderr := runStoreCmd(t, map[string]string{}, "create")
	if code != cli.ExitUndetermined {
		t.Fatalf("create with no HOME and no %s exited %d; want %d", store.PathEnv, code, cli.ExitUndetermined)
	}
	if !strings.Contains(stderr, "could not be determined") {
		t.Errorf("the output does not say the location could not be determined:\n%s", stderr)
	}
}

// CRITERIA 17, 19 AND 20, driven through the real binary: creating a store starts no daemon, leaves
// no process behind, and the store is usable by the NEXT command — with no hub, no model, and no
// control API in the picture.
func TestCreatingAStoreStartsNothingAndIsUsableByTheNextCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "omw")
	build := exec.Command(goTool, "build", "-o", bin, "./cmd/omw")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building omw: %v\n%s", err, out)
	}

	root := filepath.Join(t.TempDir(), "store")
	sandbox := t.TempDir()
	run := func(args ...string) (int, string, *os.ProcessState) {
		cmd := exec.Command(bin, args...)
		// Its own process group, so "did it leave anything running?" is a question with an answer.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		// THE DEVICE POINTER MUST BE SANDBOXED TOO, NOT JUST THE STORE. This spawns the real
		// binary, and `store create` records which store is this device's store in a per-user
		// file resolved from XDG_DATA_HOME, else HOME. Inheriting the developer's environment
		// pointed that file at a t.TempDir() which is then deleted — so on a machine with a real
		// store, running the tests left the product reporting NO STORE while the person's tickets
		// and drafts sat on disk unreferenced. A present thing rendered as an absent one (§4.3),
		// done to the sole home of unpublished data (§3.14), by the test suite.
		//
		// Both variables are set because productDir() reads XDG_DATA_HOME first and falls back to
		// HOME: setting only one leaves the other path live on the platform that uses it, which is
		// the could-not-check-reading-as-checked shape this project exists to remove.
		cmd.Env = append(os.Environ(),
			store.PathEnv+"="+root, "OMW_HUB=", "OMW_MODEL=",
			"XDG_DATA_HOME="+sandbox, "HOME="+sandbox,
		)
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out), cmd.ProcessState
	}

	code, out, state := run("store", "create")
	if code != 0 {
		t.Fatalf("omw store create exited %d:\n%s", code, out)
	}

	// NOTHING IS RUNNING AFTERWARDS (criterion 17). The child had its own process group; if a
	// daemon had been started it would still be in it.
	if pgrep, err := exec.LookPath("pgrep"); err == nil {
		left, _ := exec.Command(pgrep, "-g", strconv.Itoa(state.Pid())).Output()
		for _, line := range strings.Fields(string(left)) {
			if line != "" && line != strconv.Itoa(state.Pid()) {
				t.Errorf("a process is still running in the create command's process group after it exited: pid %s", line)
			}
		}
	} else {
		t.Log("pgrep is not on PATH; the leftover-process check was not run")
	}

	// No socket was left in the store either — the control API is not part of creating one.
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSocket != 0 {
			t.Errorf("creating a store left a socket at %s", p)
		}
		return nil
	})

	// THE NEXT COMMAND FINDS IT (criterion 19: it does not half-work).
	code, out, _ = run("store", "status")
	if code != 0 {
		t.Fatalf("omw store status after create exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "present:  yes") {
		t.Errorf("the next command does not find the store that was just reported as created:\n%s", out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", dir, err)
	}
	return out
}

// THE SUITE MUST NOT TOUCH THE DEVELOPER'S OWN DEVICE POINTER, and this is checked STRUCTURALLY.
//
// The defect: a spawned `omw store create` inherited the real environment and rewrote
// `~/Library/Application Support/omw/device-store.json` to a t.TempDir() that is deleted when the
// test ends. On a machine with a real store the product then reported NO STORE while the person's
// tickets and drafts sat on disk unreferenced — §4.3's "a present thing rendered as an absent one",
// done to §3.14's sole home of unpublished data, by `make ci`.
//
// WHY STRUCTURAL AND NOT BEHAVIOURAL. The obvious test — read the real pointer, run the suite,
// read it again — cannot be written honestly: to observe the damage it must first allow the
// damage, on the machine of whoever runs it. And a version that merely reads the file before and
// after doing nothing passes unconditionally, which is worse than no test. So this reads the
// source instead and requires every process spawn in this package to sandbox the pointer's
// location. It fails when the NEXT test to spawn the binary forgets, which is the case that
// matters — the original was written by someone who did not think to look.
func TestEveryProcessSpawnSandboxesTheDevicePointer(t *testing.T) {
	// TEST FILES ONLY, AND THIS IS THE WHOLE POINT OF THE FILTER. The first version of this check
	// walked every file in the package and flagged `daemon.go`, where `omw daemon start` spawns the
	// real daemon — production code that MUST inherit the real environment, because the daemon has
	// to find the person's actual store. Sandboxing HOME there would be the bug, not the fix.
	//
	// A gate that is red for a reason its author cannot act on is worse than no gate: it trains a
	// reader to ignore a red. This one applies to test files, which are the only place a spawn has
	// any business pointing somewhere other than the person's real environment.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}

	// The pointer resolves from XDG_DATA_HOME, else HOME (see store.productDir). BOTH must be set:
	// setting only one leaves the other path live on the platform that uses it, which is the
	// could-not-check-reading-as-checked shape this project exists to remove.
	// MATCHED WITH THEIR OPENING QUOTE. `strings.Contains(src, "HOME=")` is satisfied by
	// `XDG_DATA_HOME=`, so the half-fix — sandboxing one and not the other — passed this check
	// when it was written without the quote. Caught by driving the half-fix and watching this
	// test stay green, which is the only way that class of error surfaces.
	required := []string{`"XDG_DATA_HOME=`, `"HOME=`}

	found := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok || len(assign.Lhs) != 1 {
					return true
				}
				sel, ok := assign.Lhs[0].(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Env" {
					return true
				}
				found++
				var b strings.Builder
				if err := printer.Fprint(&b, fset, assign.Rhs[0]); err != nil {
					t.Fatalf("printing the env expression: %v", err)
				}
				src := b.String()
				if !strings.Contains(src, "os.Environ()") {
					return true // a fully-constructed env inherits nothing; nothing to sandbox.
				}
				for _, want := range required {
					if !strings.Contains(src, want) {
						t.Errorf("%s:%d: this spawn inherits os.Environ() but does not set %s\n"+
							"  It will write the DEVELOPER'S OWN device pointer at $XDG_DATA_HOME/omw or\n"+
							"  $HOME/.../omw, repointing their real store at a t.TempDir() that is then\n"+
							"  deleted — the product reports no store while their tickets remain on disk.\n"+
							"  Set both XDG_DATA_HOME and HOME to a t.TempDir() in this command's env.\n"+
							"  env expression: %s",
							filepath.Base(name), fset.Position(assign.Pos()).Line, want, src)
					}
				}
				return true
			})
		}
	}

	// A CONTROL. If the walk found no spawns at all it would pass vacuously, and a green would say
	// "every spawn is sandboxed" when it had examined none.
	if found == 0 {
		t.Fatal("found no cmd.Env assignment in this package — the check examined nothing, " +
			"so its pass says nothing. Fix the walk, do not delete the test.")
	}
	t.Logf("examined %d process-spawn environment(s)", found)
}
