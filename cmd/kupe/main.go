// Binary kupe is the command-line interface for the Kupe managed Kubernetes
// platform. See https://kupe.cloud and the internal docs under docs/ for usage.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kupecloud/kupe-cli/internal/cmd"
)

func main() {
	os.Exit(run())
}

// run sets up the signal-aware context and dispatches to the command tree,
// returning the exit code. Keeping this out of main() lets deferred cleanup
// (signal.Stop via stop()) run before os.Exit terminates the process.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cmd.Execute(ctx)
}
