// @title           Server Monitor API
// @version         1.0
// @description     服务器监控平台 API，提供主机监控、告警管理、规则配置等功能。
// @host             localhost:8080
// @BasePath         /api/v1
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

	exitCode := runApp(app)
	shutdownApp(app)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
