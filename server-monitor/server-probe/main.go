package main

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

func main() {
	log, err := logger.Init("server-probe")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(log)

	app, err := initApp(context.Background())
	if err != nil {
		zap.L().Error("server-probe init failed", zap.Error(err))
		os.Exit(1)
	}

	exitCode := runApp(app)
	shutdownApp(app)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
