// Command cloudops-worker owns the bounded MySQL-backed V3 async task runtime.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.RunWorker(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-worker failed:", err)
		os.Exit(1)
	}
}
