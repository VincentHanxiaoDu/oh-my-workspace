package daemon

import (
	"sort"
	"sync"
	"time"
)

// Background is work the daemon does for as long as it runs.
//
// WHY THIS EXISTS (Issue #6). PRD §3.1 and §2.1: ingesting the person's channels is a property of
// the daemon RUNNING, not of a command being typed. There was no way to say that: the daemon's only
// periodic work was its own write probe, so the capability that has to happen continuously had
// nowhere to happen. This is that place, and it is deliberately the narrowest one — a name, an
// interval, and a function given the store path.
//
// IT IS A REGISTRY AND NOT AN OPTION FIELD for the reason [cli.Register] is: the daemon must not
// import the capabilities that run inside it. `internal/channels` imports `internal/daemon` and
// registers from an init; the daemon knows only that something asked to be run.
//
// A TASK IS NOT ALLOWED TO STOP THE DAEMON, and this is on purpose rather than by omission. Run
// returns nothing. The daemon stops when IT cannot write (§4.3); a channel whose credential was
// rejected is a fact about that channel, reported on that channel, and a person whose Teams token
// expired must not find their daemon has exited and their notes have stopped being watched.
type Background struct {
	// Name is what this work is, for a person reading about it.
	Name string
	// Interval is how often Run is called. Zero means [DefaultInterval].
	Interval time.Duration
	// Run does one pass. It is called on its own goroutine, never concurrently with itself, and its
	// panics are its own business — see runBackground.
	Run func(storePath string)
}

var (
	bgMu    sync.Mutex
	bgTasks []Background
)

// RegisterBackground adds work for every daemon started after this call. It is called from an init
// in the owning capability's package.
func RegisterBackground(b Background) {
	if b.Name == "" || b.Run == nil {
		panic("daemon.RegisterBackground: background work needs a name and something to run")
	}
	bgMu.Lock()
	defer bgMu.Unlock()
	bgTasks = append(bgTasks, b)
}

// Backgrounds returns the registered work, ordered by name. Exported so a command can tell a person
// what a running daemon is doing, and so a test can assert the set.
func Backgrounds() []Background {
	bgMu.Lock()
	defer bgMu.Unlock()
	out := append([]Background(nil), bgTasks...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// startBackground launches every registered task and returns a function that stops them all and
// waits for them.
//
// THE FIRST PASS RUNS IMMEDIATELY, before any waiting. A daemon whose first ingestion happens one
// interval after it starts is a daemon that is demonstrably not keeping up for that interval, and
// "it will be right shortly" is the shape of answer this product does not give.
func (d *Daemon) startBackground() func() {
	tasks := Backgrounds()
	if len(tasks) == 0 {
		return func() {}
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, t := range tasks {
		interval := t.Interval
		if interval <= 0 {
			interval = DefaultInterval
		}
		run, path := t.Run, d.opts.StorePath
		wg.Add(1)
		go func() {
			defer wg.Done()
			run(path)
			tick := time.NewTicker(interval)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					run(path)
				}
			}
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		wg.Wait()
	}
}
