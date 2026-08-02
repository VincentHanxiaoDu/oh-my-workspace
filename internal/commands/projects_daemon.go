// The seam that makes a running daemon actually watch a person's projects (Issue #4 criterion 4).
//
// WHY THIS FILE EXISTS, AND WHY IT SHOULD NOT EXIST FOR LONG.
//
// `projects.Run` is the whole contract the daemon has with `internal/projects`, and until now
// NOTHING CALLED IT. The daemon started, `omw projects list` correctly reported "watching: yes", and
// every row correctly reported "examined during this command" — because that was the truth. Nothing
// was lying; nothing was polling either. Criterion 4 says it in as many words: "reflecting a change
// only when a listing command is run is a failure of this criterion."
//
// THE RIGHT HOME FOR THIS IS `daemon.RegisterBackground`, which is a registry exactly like
// [cli.Register] — a capability registers from an init in its own package and the daemon never
// imports the capability. It is on Issue #6's branch (PR #40) and is not on `main`, so it cannot be
// used from here yet. This file is the narrowest thing that works without it: one function, called
// from one line in daemonRun.
//
// THE MIGRATION IS ONE LINE AND ONE FILE, and it is deliberately shaped that way. When #40 lands,
// this whole file becomes an init in `internal/projects`:
//
//	func init() {
//		daemon.RegisterBackground(daemon.Background{
//			Name:     "projects",
//			Interval: projects.PollInterval,
//			Run:      func(storePath string) { ... one poll ... },
//		})
//	}
//
// and the call in daemonRun is deleted. It MUST be migrated rather than left alongside: two ways to
// run daemon background work is a second mechanism, and this branch has already been refused once
// for shipping a second answer to a question the product answers elsewhere. A note on the pull
// request says so; this comment says so where whoever merges #40 will actually be standing.

package commands

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/projects"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
)

// startProjectsPolling begins watching the registered projects and returns a function that stops
// the watching and waits for it to finish.
//
// It is called ONLY from the daemon's own run command. Nothing else in the product calls it, and a
// structural test asserts that — a listing that could start polling would be the daemon starting
// itself on a person's behalf, which criterion 11 and PRD §4.2 forbid.
//
// A STORE IT CANNOT OPEN IS SAID, NOT SWALLOWED, AND IS NOT FATAL. The daemon proves its own ability
// to write against this same store and stops itself when it cannot (PRD §4.3), so a second exit
// decision here would be made on less evidence than the one already being made. What must not
// happen is silence: a daemon running with its project polling dead, saying nothing, is a person
// told "watching: yes" while nothing is watched.
func startProjectsPolling(storePath string, getenv func(string) string, stderr io.Writer) func() {
	s, err := store.Open(storePath)
	if err != nil {
		fmt.Fprintf(stderr, "omw daemon: projects are NOT being watched this run: "+
			"the store at %s could not be opened for polling (%v).\n"+
			"  A project listing will examine the directories itself and say that it did.\n",
			storePath, err)
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = projects.Run(ctx, s, getenv)
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
