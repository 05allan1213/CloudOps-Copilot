// @title           CloudOps-Copilot Incident API
// @version         2.0
// @description     Incident-centric APIs for signal ingestion, bounded Agent investigation, approval-bound remediation, deterministic verification, postmortem, and Workbench queries.
// @host             localhost:8080
// @BasePath         /api/v2
// @schemes          http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
package main

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

func main() {
	log, err := logger.Init("server-web")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	app, err := initApp(context.Background())
	if err != nil {
		zap.L().Error("server-web init failed", zap.Error(err))
		os.Exit(1)
	}

	// runApp blocks until an exit condition, then shutdownApp performs the
	// bounded four-phase shutdown for HTTP traffic, consumers, external clients,
	// and in-process hubs.
	exitCode := runApp(app)
	shutdownApp(app)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
