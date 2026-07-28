package tracer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Cluster        string
	Namespace      string
	PodUID         string
	WorkloadKind   string
	WorkloadName   string
	ScenarioID     string
	SourceRevision string
	OTLPEndpoint   string
	SampleRate     float64
}

func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	endpoint := strings.TrimSpace(cfg.OTLPEndpoint)
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	attributes := []attribute.KeyValue{attribute.String("service.name", cfg.ServiceName)}
	for _, item := range []struct{ key, value string }{
		{"service.version", cfg.ServiceVersion},
		{"deployment.environment.name", cfg.Environment},
		{"k8s.cluster.name", cfg.Cluster},
		{"k8s.namespace.name", cfg.Namespace},
		{"k8s.pod.uid", cfg.PodUID},
		{"k8s.workload.kind", cfg.WorkloadKind},
		{"k8s.workload.name", cfg.WorkloadName},
		{"cloudops.scenario.id", cfg.ScenarioID},
		{"cloudops.source.revision", cfg.SourceRevision},
	} {
		if strings.TrimSpace(item.value) != "" {
			attributes = append(attributes, attribute.String(item.key, item.value))
		}
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", attributes...),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider.Shutdown, nil
}
