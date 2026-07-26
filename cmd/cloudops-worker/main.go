// Command cloudops-worker owns the bounded MySQL-backed async task runtime.
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
	var err error
	switch {
	case len(os.Args) == 1:
		err = bootstrap.RunWorker(ctx)
	case len(os.Args) == 2 && os.Args[1] == "baseline-verify":
		err = bootstrap.RunBaselineVerifier(ctx)
	default:
		err = fmt.Errorf("usage: cloudops-worker [baseline-verify]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-worker failed:", err)
		os.Exit(1)
	}
}
