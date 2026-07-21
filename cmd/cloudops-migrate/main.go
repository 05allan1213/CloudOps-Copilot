// Command cloudops-migrate applies forward-only Goose migrations, performs
// the read-only runtime-generation check, or writes the fail-closed Phase 7A
// irreversible marker after explicit prerequisite audits.
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
