// The `omw daemon` command: start the daemon on purpose, stop it on purpose, and ask it what it
// thinks it is doing — including how its last run ended (PRD §2.1, §4.2, §4.3, §4.6; Issue #2).
//
// WHY `start` IS NOT `run`. Criterion 1 says that after the start command RETURNS SUCCESSFULLY the
// daemon is running, and a foreground process never returns. So `start` launches `omw daemon run`
// as a child, waits for it to report that it has taken the lock, and only then returns — so a
// non-zero exit from `start` means the daemon is genuinely not running, and a zero exit means it
// genuinely is. `run` is the daemon itself and is a documented command rather than a hidden one:
// a person under a service manager wants the foreground process, and a hidden command is a thing
// people discover from strings(1).
//
// NOTHING ELSE IN THIS FILE STARTS ANYTHING (criterion 18). `stop` and `status` never launch a
// process, and `status` against a store with no daemon reads the disk and says so.
//
// (Detached from the package clause on purpose: doc.go carries this package's doc comment.)

package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "daemon",
		Summary: "start, stop, or ask the daemon what state it is in",
		Run:     runDaemon,
	})
}

// readyFDEnv names the descriptor `omw daemon run` writes its readiness line to.
//
// A PIPE RATHER THAN A POLL. `start` has to distinguish "it is up" from "it refused, and here is
// why", and polling a lock file cannot tell those apart — it only ever learns that nothing
// happened. The child writes one line and the parent prints its reason verbatim, so a refusal
// reaches the person who typed `start` instead of a log nobody opens.
const readyFDEnv = "OMW_DAEMON_READY_FD"

const daemonUsage = `usage: omw daemon <start|run|stop|status> [path]

  start    start the daemon against this device's store, and return once it is running.
  run      BE the daemon, in the foreground. This is what 'start' launches.
  stop     stop the running daemon and release the store's write lock.
  status   report the daemon's state, including how its last run ended. Starts nothing.

Nothing else in omw starts the daemon (PRD §4.2). If it is not running, commands say so.

The store comes from $` + store.PathEnv + `, else this device's registered store, else a
per-user data directory. A path may be given to override all three.
`

func runDaemon(env cli.Env) int {
	if len(env.Args) == 0 {
		fmt.Fprint(env.Stderr, daemonUsage)
		return cli.ExitUsage
	}
	sub, rest := env.Args[0], env.Args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprint(env.Stdout, daemonUsage)
		return cli.Success
	case "start":
		return daemonStart(env, rest)
	case "run":
		return daemonRun(env, rest)
	case "stop":
		return daemonStop(env, rest)
	case "status":
		return daemonStatus(env, rest)
	default:
		fmt.Fprintf(env.Stderr, "omw daemon: unknown subcommand %q\n", sub)
		fmt.Fprint(env.Stderr, daemonUsage)
		return cli.ExitUsage
	}
}

// resolveStore works out which store this invocation is about, WITHOUT creating one.
func resolveStore(env cli.Env, args []string) (string, int) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" && a != "--" {
			fmt.Fprintf(env.Stderr, "omw daemon: unknown option %q; run 'omw daemon --help'.\n", a)
			return "", cli.ExitUsage
		}
	}
	var positional []string
	for _, a := range args {
		if a != "--" {
			positional = append(positional, a)
		}
	}
	if len(positional) > 1 {
		fmt.Fprintf(env.Stderr, "omw daemon: more than one path was given; a daemon runs against one store.\n")
		return "", cli.ExitUsage
	}
	if len(positional) == 1 {
		abs, err := filepath.Abs(positional[0])
		if err != nil {
			fmt.Fprintf(env.Stderr, "omw daemon: %v\n", err)
			return "", cli.ExitFailure
		}
		return abs, cli.Success
	}
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		// UNDETERMINED, NOT MISSING. Not knowing where the store lives is not the same as knowing
		// there is not one, and the exit code says which.
		fmt.Fprintf(env.Stderr, "omw daemon: %v\n", err)
		if errors.Is(err, store.ErrPathUndetermined) {
			return "", cli.ExitUndetermined
		}
		return "", cli.ExitFailure
	}
	return path, cli.Success
}

// startKind is which kind of start failure happened, as a token that survives a pipe.
//
// IT EXISTS BECAUSE THE SENTENCES ARE WRITTEN IN THE WRONG PROCESS OTHERWISE. `start` launches a
// child, and the child is the one that meets the error — but the child's stderr goes nowhere, so
// everything below about naming the lock conflict apart from a missing store was being written by
// a process nobody was reading. The child sends this token and the parent writes the sentence, so
// the wording criterion 6 requires is produced exactly once, in the process the person is looking
// at. Found by mutating the lock-conflict sentence into the missing-store sentence and watching
// every test stay green.
type startKind string

const (
	kindNoStore          startKind = "no-store"
	kindLockHeld         startKind = "lock-held"
	kindLockUndetermined startKind = "lock-undetermined"
	kindStoreUnreadable  startKind = "store-unreadable"
	kindSomethingElse    startKind = "other"
	startKindSeparator             = "|"
)

func classifyStartError(err error) startKind {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return kindNoStore
	case errors.Is(err, daemon.ErrLockHeld):
		return kindLockHeld
	case errors.Is(err, daemon.ErrLockUndetermined):
		return kindLockUndetermined
	case errors.Is(err, store.ErrUnreadable), errors.Is(err, store.ErrPermissionDenied):
		return kindStoreUnreadable
	default:
		return kindSomethingElse
	}
}

// startFailure turns a Start error into the sentence and the exit code criterion 6 asks for: the
// lock conflict, the missing store and everything else are told apart by output AND by code.
func startFailure(env cli.Env, path string, err error) int {
	return startFailureOf(env, path, classifyStartError(err), err.Error())
}

func startFailureOf(env cli.Env, path string, kind startKind, message string) int {
	switch kind {
	case kindNoStore:
		// SAID THE WAY THE STORE COMMAND SAYS IT (criterion 3, and Issue #2's note about Issue #3):
		// no store means no daemon, and it is one fact, not two different-sounding failures.
		fmt.Fprintf(env.Stderr, "omw daemon start: %v: %s\n", store.ErrNotFound, path)
		fmt.Fprintf(env.Stderr, "  The daemon does not create a store and nothing else will either.\n")
		fmt.Fprintf(env.Stderr, "  Run 'omw store create' to create it on purpose.\n")
		return cli.ExitFailure
	case kindLockHeld:
		// CRITERION 5 AND 6. Its own sentence, naming the conflict, and it does not touch the
		// daemon that holds the store.
		fmt.Fprintf(env.Stderr, "omw daemon start: %s\n", message)
		fmt.Fprintf(env.Stderr, "  One daemon per store (PRD §2.1). The daemon already running has not been\n")
		fmt.Fprintf(env.Stderr, "  affected; run 'omw daemon status' to see it, or 'omw daemon stop' to stop it.\n")
		return cli.ExitFailure
	case kindLockUndetermined:
		// NEITHER "IT IS FREE" NOR "SOMEBODY HOLDS IT" (criterion 8). Its own exit code, because
		// `could not determine` and `determined to be nothing` never share one.
		fmt.Fprintf(env.Stderr, "omw daemon start: %s\n", message)
		return cli.ExitUndetermined
	default:
		fmt.Fprintf(env.Stderr, "omw daemon start: %s\n", message)
		return cli.ExitFailure
	}
}

// daemonStart launches the daemon and waits for it to say it is up.
func daemonStart(env cli.Env, args []string) int {
	path, code := resolveStore(env, args)
	if code != cli.Success {
		return code
	}

	// REFUSED BEFORE A PROCESS IS SPAWNED WHERE THE ANSWER IS ALREADY KNOWN. A missing store and a
	// held lock are both visible from here, and the sentence a person gets should not depend on
	// whether a child process managed to relay it.
	if _, err := store.Open(path); err != nil {
		return startFailure(env, path, err)
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw daemon start: could not find this program on disk to launch it: %v\n", err)
		return cli.ExitFailure
	}
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw daemon start: %v\n", err)
		return cli.ExitFailure
	}
	defer r.Close()

	cmd := exec.Command(self, "daemon", "run", "--", path)
	cmd.Env = append(os.Environ(), readyFDEnv+"=3")
	cmd.ExtraFiles = []*os.File{w}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	detachChild(cmd)
	if err := cmd.Start(); err != nil {
		w.Close()
		fmt.Fprintf(env.Stderr, "omw daemon start: could not launch the daemon: %v\n", err)
		return cli.ExitFailure
	}
	// The parent's copy of the write end must go, or reading the pipe would never see EOF when the
	// child dies without writing — and `start` would hang instead of reporting a failure.
	w.Close()

	line, readErr := bufio.NewReader(r).ReadString('\n')
	line = strings.TrimRight(line, "\n")
	_ = cmd.Process.Release()

	switch {
	case strings.HasPrefix(line, "ready "):
		fmt.Fprintf(env.Stdout, "the daemon is running against %s\n", path)
		fmt.Fprintf(env.Stdout, "%s\n", strings.TrimPrefix(line, "ready "))
		return cli.Success
	case strings.HasPrefix(line, "failed "):
		kind, message, ok := strings.Cut(strings.TrimPrefix(line, "failed "), startKindSeparator)
		if !ok {
			message, kind = kind, string(kindSomethingElse)
		}
		return startFailureOf(env, path, startKind(kind), message)
	default:
		// THE CHILD SAID NOTHING. Whether it started is exactly what could not be determined, and
		// saying "it is running" or "it is not" here would be inventing one.
		fmt.Fprintf(env.Stderr, "omw daemon start: the daemon was launched and %s: it exited or said nothing (%v)\n",
			tri.Undetermined, readErr)
		return cli.ExitUndetermined
	}
}

// daemonRun is the daemon itself.
func daemonRun(env cli.Env, args []string) int {
	path, code := resolveStore(env, args)
	if code != cli.Success {
		return code
	}
	ready := readyPipe(env)

	d, err := daemon.Start(daemon.Options{StorePath: path})
	if err != nil {
		say(ready, "failed "+string(classifyStartError(err))+startKindSeparator+err.Error())
		closeReady(ready)
		return startFailure(env, path, err)
	}

	// WHAT THE CONTROL API DID IS PART OF STARTING SUCCESSFULLY, not a separate command's problem.
	// Criterion 23: the refusal is said, in wording that is neither "not running" nor "running
	// normally". The daemon runs either way (Issue #1's carried-forward criterion 14).
	state, detail := d.ControlState()
	msg := "its control API is open, and its socket was confirmed owner-only"
	if state != tri.Yes {
		msg = "it is running WITHOUT a control API: " + detail
	}
	if d.StaleLockFound {
		msg = d.StaleLockDetail + "\n" + msg
	}
	say(ready, "ready "+strings.ReplaceAll(msg, "\n", "; "))
	closeReady(ready)

	// SIGTERM AND SIGINT ARE AN EXPLICIT STOP. A service manager's SIGTERM and a person's Ctrl-C
	// are both somebody deciding this run is over, so both record "ended by an explicit stop"
	// rather than being left to look like a crash.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		<-sigs
		d.Stop()
	}()

	// PROJECTS ARE WATCHED FOR AS LONG AS THIS RUN LASTS (Issue #4 criterion 4). One line, and it
	// moves to daemon.RegisterBackground when Issue #6 lands that registry — see
	// projects_daemon.go, which explains why it is here and not there yet.
	stopProjects := startProjectsPolling(path, env.Getenv, env.Stderr)

	serveErr := d.Serve()
	stopProjects()
	d.Close()
	if serveErr != nil {
		fmt.Fprintf(env.Stderr, "omw daemon run: stopped because it could not write to the store: %v\n", serveErr)
		return cli.ExitFailure
	}
	return cli.Success
}

func readyPipe(env cli.Env) *os.File {
	fd := env.Getenv(readyFDEnv)
	if fd != "3" {
		return nil
	}
	return os.NewFile(3, "ready")
}

func say(w *os.File, line string) {
	if w == nil {
		return
	}
	_, _ = w.WriteString(line + "\n")
}

func closeReady(w *os.File) {
	if w != nil {
		_ = w.Close()
	}
}

// daemonStop stops the running daemon, and says something different when there was none.
func daemonStop(env cli.Env, args []string) int {
	path, code := resolveStore(env, args)
	if code != cli.Success {
		return code
	}
	before := daemon.Inspect(path)
	switch before.Running {
	case tri.No:
		// CRITERION 4. Its own sentence, which no successful stop ever prints.
		fmt.Fprintf(env.Stdout, "the daemon is not running against %s — there was nothing to stop\n", path)
		fmt.Fprintf(env.Stdout, "last run: %s\n", before.LastRun)
		return cli.Success
	case tri.Undetermined:
		fmt.Fprintf(env.Stderr, "omw daemon stop: whether a daemon is running against %s %s\n", path, tri.Undetermined)
		if before.HealthDetail != "" {
			fmt.Fprintf(env.Stderr, "  %s\n", before.HealthDetail)
		}
		return cli.ExitUndetermined
	}
	if before.PID <= 0 {
		fmt.Fprintf(env.Stderr, "omw daemon stop: a daemon holds %s and which process it is %s\n", path, tri.Undetermined)
		return cli.ExitUndetermined
	}
	proc, err := os.FindProcess(before.PID)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw daemon stop: %v\n", err)
		return cli.ExitUndetermined
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(env.Stderr, "omw daemon stop: could not ask pid %d to stop: %v\n", before.PID, err)
		return cli.ExitFailure
	}

	// WAITED FOR, NOT ASSUMED. Criterion 2 says that after this command returns the lock is
	// released and a subsequent start succeeds, so this returns when that is true rather than when
	// the signal was delivered.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		after := daemon.Inspect(path)
		if after.Running == tri.No {
			fmt.Fprintf(env.Stdout, "the daemon holding %s has stopped; its write lock is released\n", path)
			fmt.Fprintf(env.Stdout, "last run: %s\n", after.LastRun)
			return cli.Success
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintf(env.Stderr, "omw daemon stop: pid %d was asked to stop and whether it did %s within 10s\n",
		before.PID, tri.Undetermined)
	return cli.ExitUndetermined
}

// daemonStatus reports the daemon's state and STARTS NOTHING (criteria 9, 13, 18).
func daemonStatus(env cli.Env, args []string) int {
	path, code := resolveStore(env, args)
	if code != cli.Success {
		return code
	}
	rep := daemon.Inspect(path)
	_, _ = rep.WriteTo(env.Stdout)

	// THE EXIT CODE CARRIES THE DISTINCTION. Answering "not running" is a successful answer, so it
	// exits zero; an answer that could not be determined exits ExitUndetermined, because those two
	// must never share a code. A daemon that is running and NOT healthy is a failure state a
	// script should notice, so it exits non-zero too.
	switch {
	case !rep.Running.Determined(), !rep.LastRun.Determined(), !rep.Control.Determined():
		return cli.ExitUndetermined
	case rep.Running == tri.Yes && rep.Healthy == tri.No:
		return cli.ExitFailure
	case rep.Running == tri.Yes && !rep.Healthy.Determined():
		return cli.ExitUndetermined
	default:
		return cli.Success
	}
}
