// Command cloudops-api serves the existing CloudOps HTTP API without starting
// any legacy Agent, remediation, delivery, or verification worker loop.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	apibootstrap "github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/api"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := apibootstrap.RunAPI(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-api failed:", err)
		os.Exit(1)
	}
}
