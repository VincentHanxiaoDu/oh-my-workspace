package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// undeterminedTarget builds a location whose sync status genuinely cannot be determined: the store's
// grandparent is traversable but not listable, so the probe can reach the path and cannot see what
// is beside it.
func undeterminedTarget(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read a directory whose permissions forbid it")
	}
	opaque := filepath.Join(t.TempDir(), "opaque")
	if err := os.MkdirAll(filepath.Join(opaque, "here"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(opaque, 0o111); err != nil {
		t.Skipf("this filesystem will not take the permission bits: %v", err)
	}
	t.Cleanup(func() { os.Chmod(opaque, 0o755) })
	return filepath.Join(opaque, "here", "store")
}

func dropboxTarget(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Dropbox")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dropbox"), []byte("marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "store")
}

// CRITERION 23: the explicit override creates the store at an undetermined path, and the creation is
// otherwise a normal success — one store, absolute path printed, exit zero.
func TestTheOverrideCreatesAStoreAtAnUndeterminedLocation(t *testing.T) {
	target := undeterminedTarget(t)
	env := envFor(t, target)

	// Without it, the halt (criterion 21).
	if code, _, _ := runStoreCmd(t, env, "create"); code != cli.ExitUndetermined {
		t.Fatalf("without the override, create exited %d; want %d", code, cli.ExitUndetermined)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the halt left something at %s", target)
	}

	// With it, a normal success.
	code, stdout, stderr := runStoreCmd(t, env, "create", overrideFlag)
	if code != cli.Success {
		t.Fatalf("with the override, create exited %d; want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, target) {
		t.Errorf("the success does not print the absolute path it created:\n%s", stdout)
	}
	if store.Exists(target) != tri.Yes {
		t.Errorf("no store at %s after an overridden create", target)
	}
	// Criterion 1 in full: the store works.
	if code, out, _ := runStoreCmd(t, env, "status"); code == cli.Success && !strings.Contains(out, "present:  yes") {
		t.Errorf("the overridden store does not report as present:\n%s", out)
	}
}

// CRITERION 24: the override does NOT work for a path known to synchronise. §4.1's refusal is not
// overridable; only the undetermined case is.
func TestTheOverrideDoesNotWorkForAKnownSynchronisingPath(t *testing.T) {
	target := dropboxTarget(t)
	env := envFor(t, target)

	plainCode, _, plainErr := runStoreCmd(t, env, "create")
	overrideCode, overrideOut, overrideErr := runStoreCmd(t, env, "create", overrideFlag)

	if overrideCode == cli.Success {
		t.Fatalf("the override created a store inside Dropbox — §4.1's refusal is not overridable\n%s", overrideOut)
	}
	if overrideCode != plainCode {
		t.Errorf("with the override create exits %d, without it %d; criterion 24 asks for the SAME non-zero outcome either way", overrideCode, plainCode)
	}
	if !strings.Contains(overrideErr, "Dropbox") {
		t.Errorf("the overridden refusal no longer names Dropbox:\n%s", overrideErr)
	}
	if overrideErr != plainErr {
		t.Errorf("the override changed the refusal's wording:\nwith:    %s\nwithout: %s", overrideErr, plainErr)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("the overridden refusal left something at %s", target)
	}
}

// CRITERION 25: after an override the reported location state stays undetermined — never recorded or
// rendered as confirmed non-synchronising. Driven by comparing the two reports directly.
func TestAfterAnOverrideTheLocationStillReportsUndetermined(t *testing.T) {
	overridden := undeterminedTarget(t)
	overriddenEnv := envFor(t, overridden)
	if code, _, e := runStoreCmd(t, overriddenEnv, "create", overrideFlag); code != cli.Success {
		t.Fatalf("overridden create exited %d: %s", code, e)
	}

	confirmedEnv, _ := storeEnv(t)
	if code, _, e := runStoreCmd(t, confirmedEnv, "create"); code != cli.Success {
		t.Fatalf("create at a confirmed location exited %d: %s", code, e)
	}

	overriddenCode, overriddenOut, _ := runStoreCmd(t, overriddenEnv, "status")
	confirmedCode, confirmedOut, _ := runStoreCmd(t, confirmedEnv, "status")

	if !strings.Contains(overriddenOut, "could not be determined") {
		t.Errorf("after an override the location does not report as undetermined:\n%s", overriddenOut)
	}
	if strings.Contains(overriddenOut, "confirmed off the sync path") {
		t.Fatalf("after an override the location reports as CONFIRMED non-synchronising — the person's decision was recorded as a determination:\n%s", overriddenOut)
	}
	if !strings.Contains(confirmedOut, "confirmed off the sync path") {
		t.Errorf("the confirmed store does not report as confirmed:\n%s", confirmedOut)
	}
	if overriddenOut == confirmedOut {
		t.Fatal("the post-override report is identical to the report for a store at a confirmed non-synchronising path")
	}
	if overriddenCode == confirmedCode {
		t.Errorf("both report on exit %d; an undetermined location and a confirmed one are different answers", overriddenCode)
	}
	// The provenance is shown too, so nobody reads the undetermined line as a transient glitch.
	if !strings.Contains(overriddenOut, overrideFlag) {
		t.Errorf("the report does not say the store was created with the override:\n%s", overriddenOut)
	}
}

// FINDING 2: a flag-shaped argument must never become a store path.
//
// Before this, `omw store create --help` exited zero having created a store in a directory called
// `--help`, silently discarding $OMW_STORE. A person typing `--help` is asking what the command
// does; they would believe they had succeeded, at a path they never chose.
func TestFlagShapedArgumentsAreRejectedAndCreateNothing(t *testing.T) {
	for _, arg := range []string{"--nonsense", "-x", "--store", "--accept-undetermined", "-hh"} {
		t.Run(arg, func(t *testing.T) {
			// Stand somewhere disposable: a build that DOES treat the flag as a path creates a
			// directory named after it in the working directory, and that must not be the repo.
			t.Chdir(t.TempDir())
			wanted := filepath.Join(t.TempDir(), "wanted")
			env := envFor(t, wanted)

			code, stdout, stderr := runStoreCmd(t, env, "create", arg)
			if code == cli.Success {
				t.Fatalf("create %s exited 0:\n%s", arg, stdout)
			}
			if code != cli.ExitUsage {
				t.Errorf("create %s exited %d; want %d — a mistyped invocation is a usage error", arg, code, cli.ExitUsage)
			}
			if !strings.Contains(stderr, arg) {
				t.Errorf("the error does not echo the argument back:\n%s", stderr)
			}
			// Nothing anywhere: not at the flag-shaped path, and not at the real one either.
			if _, err := os.Stat(arg); err == nil {
				t.Errorf("a store was created at the flag-shaped path %q", arg)
			}
			if store.Exists(wanted) != tri.No {
				t.Errorf("a store was created at %s by an invocation that should have been refused", wanted)
			}
		})
	}
}

// `--help` is a question, not an instruction to create anything.
func TestHelpPrintsAndCreatesNothing(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			t.Chdir(t.TempDir())
			wanted := filepath.Join(t.TempDir(), "wanted")
			env := envFor(t, wanted)

			code, stdout, stderr := runStoreCmd(t, env, "create", arg)
			if code != cli.Success {
				t.Errorf("store create %s exited %d; want 0 — asking what a command does is not an error\n%s", arg, code, stderr)
			}
			if !strings.Contains(stdout, "usage: omw store") {
				t.Errorf("store create %s printed no usage:\n%s%s", arg, stdout, stderr)
			}
			if !strings.Contains(stdout, overrideFlag) {
				t.Errorf("the help does not document the override:\n%s", stdout)
			}
			if store.Exists(wanted) != tri.No {
				t.Errorf("store create %s created a store at %s", arg, wanted)
			}
			if _, err := os.Stat(arg); err == nil {
				t.Errorf("store create %s created a store at %q", arg, arg)
			}
		})
	}
}

// The specific trap beside the override: the words a person reaches for after being told they cannot
// proceed. These must fail loudly and name the real flag — never become a directory.
func TestForceAndYesAreRefusedAndPointAtTheRealFlag(t *testing.T) {
	for _, arg := range []string{"--force", "--yes", "-f", "-y"} {
		t.Run(arg, func(t *testing.T) {
			target := undeterminedTarget(t)
			env := envFor(t, target)

			code, stdout, stderr := runStoreCmd(t, env, "create", arg)
			if code == cli.Success {
				t.Fatalf("create %s exited 0:\n%s", arg, stdout)
			}
			if !strings.Contains(stderr, overrideFlag) {
				t.Errorf("the error does not name the flag that actually works:\n%s", stderr)
			}
			if !strings.Contains(stderr, "KNOWN to synchronise") {
				t.Errorf("the error does not say what the override will NOT do:\n%s", stderr)
			}
			if store.Exists(target) != tri.No {
				t.Errorf("create %s created a store at %s", arg, target)
			}
		})
	}
}

// A path that legitimately begins with a dash is still reachable, after `--`.
func TestADashPathIsReachableAfterTheTerminator(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "-weird")
	env := envFor(t, filepath.Join(dir, "unused"))

	code, stdout, stderr := runStoreCmd(t, env, "create", "--", target)
	if code != cli.Success {
		t.Fatalf("create -- %s exited %d:\n%s%s", target, code, stdout, stderr)
	}
	if store.Exists(target) != tri.Yes {
		t.Errorf("no store at %s", target)
	}
}

// FINDING 3 / CRITERION 4: exactly one store per device.
func TestASecondStoreAtADifferentPathIsRefused(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "A"), filepath.Join(dir, "B")
	envA := map[string]string{store.PathEnv: a, "HOME": home}
	envB := map[string]string{store.PathEnv: b, "HOME": home}

	if code, _, e := runStoreCmd(t, envA, "create"); code != cli.Success {
		t.Fatalf("the first create exited %d: %s", code, e)
	}
	code, stdout, stderr := runStoreCmd(t, envB, "create")
	if code == cli.Success {
		t.Fatalf("a second store was created at a different path:\n%s", stdout)
	}
	if !strings.Contains(stderr, a) {
		t.Errorf("the refusal does not say where the one store already is:\n%s", stderr)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Errorf("something was left at %s", b)
	}
}

// The other half of finding 3: creation must register what it made, so every later command finds it
// from anywhere, including with no $OMW_STORE set (criterion 4).
func TestCreationRegistersTheStoreSoLaterCommandsFindIt(t *testing.T) {
	home := t.TempDir()
	chosen := filepath.Join(t.TempDir(), "somewhere-of-my-own")

	if code, _, e := runStoreCmd(t, map[string]string{store.PathEnv: chosen, "HOME": home}, "create"); code != cli.Success {
		t.Fatalf("create exited %d: %s", code, e)
	}

	// A later command, with nothing but HOME — as a person's next shell invocation has.
	code, stdout, stderr := runStoreCmd(t, map[string]string{"HOME": home}, "path")
	if code != cli.Success {
		t.Fatalf("store path exited %d after a create elsewhere:\n%s%s", code, stdout, stderr)
	}
	if strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0]) != chosen {
		t.Fatalf("store path resolves to %q, not to the store that was just created at %q — creation did not register it",
			strings.SplitN(stdout, "\n", 2)[0], chosen)
	}
}

// A stale pointer must not brick the machine: if the registered store is gone, another can be made.
func TestAStorePointerToSomethingGoneIsNotBinding(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "A"), filepath.Join(dir, "B")

	if code, _, e := runStoreCmd(t, map[string]string{store.PathEnv: a, "HOME": home}, "create"); code != cli.Success {
		t.Fatalf("first create exited %d: %s", code, e)
	}
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runStoreCmd(t, map[string]string{store.PathEnv: b, "HOME": home}, "create")
	if code != cli.Success {
		t.Fatalf("after the registered store was deleted, creating another exited %d — the machine is unusable with no way out\n%s%s", code, stdout, stderr)
	}
}

// FINDING 4: on a fresh machine the default location's parent does not exist. Creating the store's
// own containing directory is part of the explicit act, and the command says it did it.
func TestTheDefaultLocationIsReachableOnAFreshMachine(t *testing.T) {
	home := t.TempDir() // A machine that has never run omw: no product directory at all.
	env := map[string]string{"HOME": home}

	code, stdout, stderr := runStoreCmd(t, env, "create")
	if code != cli.Success {
		t.Fatalf("create with no arguments on a fresh machine exited %d — the default location is unreachable on exactly the machine the default exists for\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "created the directory the store lives in") {
		t.Errorf("the command made a directory without saying so:\n%s", stdout)
	}
	if code, out, _ := runStoreCmd(t, env, "status"); code != cli.Success {
		t.Errorf("status on the freshly created default store exited %d:\n%s", code, out)
	}
}

// But a path the PERSON typed is theirs: a missing parent there is a mistyped path, not a tree to
// conjure. This is criterion 6's "this path does not exist", and it must survive the fix above.
func TestAPersonsOwnPathIsNotConjured(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "parent", "store")

	for _, how := range []string{"argument", "environment"} {
		t.Run(how, func(t *testing.T) {
			env := map[string]string{"HOME": t.TempDir()}
			args := []string{"create"}
			if how == "argument" {
				args = append(args, missing)
			} else {
				env[store.PathEnv] = missing
			}
			code, stdout, stderr := runStoreCmd(t, env, args...)
			if code == cli.Success {
				t.Fatalf("a store was conjured at a path the person mistyped:\n%s", stdout)
			}
			if !strings.Contains(stderr, "does not exist") {
				t.Errorf("the error does not say the path does not exist:\n%s", stderr)
			}
			if _, err := os.Stat(missing); !os.IsNotExist(err) {
				t.Errorf("something was created at %s", missing)
			}
		})
	}
}
