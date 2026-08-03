// Command omw-hub is the hub a company runs: one process, holding published notes, answering
// questions about them (PRD §2.2).
//
// This file stays tiny for the same reason cmd/omw/main.go does — everything it could hold is
// testable only through a process, and everything in internal/hubd is testable directly.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hubd"
)

func main() {
	// A signal STOPS the hub; it does not start anything and it does not restart it. PRD §4.2.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(hubd.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
