package main

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

func main() {
	log, err := logger.Init("alert-service")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	app, err := initApp(context.Background())
	if err != nil {
		zap.L().Fatal("alert-service init failed", zap.Error(err))
	}

	exitCode := runApp(app)
	shutdownApp(app)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
