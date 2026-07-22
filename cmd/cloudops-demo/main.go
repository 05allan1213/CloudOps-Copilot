// Command cloudops-demo runs the bounded workload used by the V3 golden flow.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/tracer"
	"github.com/05allan1213/CloudOps-Copilot/internal/demoapp"
)

var (
	version        = "dev"
	sourceRevision = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log, err := logger.Init("demo")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-demo logger failed:", err)
		os.Exit(1)
	}
	defer logger.Sync(log)
	sampleRate, err := envFloat("TRACE_SAMPLE_RATE", 1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-demo config failed:", err)
		os.Exit(1)
	}
	serviceVersion := env("SERVICE_VERSION", version)
	revision := env("SOURCE_REVISION", sourceRevision)
	shutdownTrace, err := tracer.Init(ctx, tracer.Config{
		ServiceName: "demo", ServiceVersion: serviceVersion,
		Environment: env("DEPLOYMENT_ENVIRONMENT", "local-demo"), Cluster: env("K8S_CLUSTER_NAME", "kind-cloudops-v3"),
		Namespace: env("K8S_NAMESPACE", "demo"), PodUID: env("K8S_POD_UID", "unknown"), WorkloadKind: "Deployment", WorkloadName: "demo",
		SourceRevision: revision, OTLPEndpoint: strings.TrimSpace(os.Getenv("TRACE_OTLP_ENDPOINT")), SampleRate: sampleRate,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-demo tracing failed:", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTrace(shutdownCtx)
	}()
	server, err := demoapp.New(demoapp.Config{
		ListenAddr: env("LISTEN_ADDR", ":8080"), ServiceName: "demo",
		ServiceVersion: serviceVersion, SourceRevision: revision, Environment: env("DEPLOYMENT_ENVIRONMENT", "local-demo"),
		RequiredEnv: os.Getenv("REQUIRED_ENV"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-demo startup failed:", err)
		os.Exit(1)
	}
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "cloudops-demo failed:", err)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envFloat(name string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 || value > 1 {
		return 0, fmt.Errorf("%s must be within [0,1]", name)
	}
	return value, nil
}
