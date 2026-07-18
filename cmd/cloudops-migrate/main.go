// Command cloudops-migrate applies forward-only Goose migrations under a
// MySQL advisory lock.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	migratebootstrap "github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/migrate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := migratebootstrap.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-migrate failed:", err)
		os.Exit(1)
	}
}
