// Command cloudops-demo runs the bounded Demonstration Scenario workload.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/tracer"
	"github.com/05allan1213/CloudOps-Copilot/internal/demoapp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
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
	serviceName := env("SERVICE_NAME", "cloudops-scenario")
	workloadName := env("K8S_WORKLOAD_NAME", serviceName)
	scenarioID := strings.TrimSpace(os.Getenv("SCENARIO_ID"))
	shutdownTrace, err := tracer.Init(ctx, tracer.Config{
		ServiceName: serviceName, ServiceVersion: serviceVersion,
		Environment: env("DEPLOYMENT_ENVIRONMENT", "local-demo"), Cluster: env("K8S_CLUSTER_NAME", "cloudops-local"),
		Namespace: env("K8S_NAMESPACE", "demo"), PodUID: env("K8S_POD_UID", "unknown"), WorkloadKind: "Deployment", WorkloadName: workloadName,
		ScenarioID:     scenarioID,
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
	if env("DEMO_MODE", "workload") == "traffic" {
		if err := runTraffic(ctx, log, scenarioID); err != nil {
			fmt.Fprintln(os.Stderr, "cloudops-demo traffic failed:", err)
			os.Exit(1)
		}
		return
	}
	server, err := demoapp.New(demoapp.Config{
		ListenAddr: env("LISTEN_ADDR", ":8080"), ServiceName: serviceName,
		ServiceVersion: serviceVersion, SourceRevision: revision, Environment: env("DEPLOYMENT_ENVIRONMENT", "local-demo"),
		RequiredEnv: os.Getenv("REQUIRED_ENV"), ScenarioID: scenarioID,
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

func runTraffic(ctx context.Context, log *zap.Logger, scenarioID string) error {
	targets := splitTargets(os.Getenv("TRAFFIC_TARGETS"))
	if len(targets) == 0 {
		return fmt.Errorf("TRAFFIC_TARGETS must contain at least one fixed HTTP endpoint")
	}
	interval, err := envDuration("TRAFFIC_INTERVAL", time.Second)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for _, target := range targets {
			requestCtx, span := otel.Tracer("cloudops-scenario-traffic").Start(ctx, "scenario.traffic")
			request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
			if requestErr != nil {
				span.RecordError(requestErr)
				span.End()
				return requestErr
			}
			response, requestErr := client.Do(request)
			if requestErr != nil {
				span.RecordError(requestErr)
				log.Warn("scenario traffic request failed", zap.String("scenario_id", scenarioID), zap.String("target", target), zap.Error(requestErr))
			} else {
				_ = response.Body.Close()
				log.Info("scenario traffic request completed", zap.String("scenario_id", scenarioID), zap.String("target", target), zap.Int("status", response.StatusCode))
			}
			span.End()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func splitTargets(raw string) []string {
	result := make([]string, 0, 4)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if strings.HasPrefix(item, "http://") && !strings.ContainsAny(item, "\r\n\t ") {
			result = append(result, item)
		}
	}
	return result
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 100*time.Millisecond || value > time.Minute {
		return 0, fmt.Errorf("%s must be within [100ms,1m]", name)
	}
	return value, nil
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
