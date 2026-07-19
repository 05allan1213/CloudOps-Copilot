// Package demoapp implements the small workload used by the V3 golden
// incident scenario. It intentionally stays independent of CloudOps domain
// state so the failure is injected only by changing its GitOps env node.
package demoapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
)

type Config struct {
	ListenAddr     string
	ServiceName    string
	ServiceVersion string
	SourceRevision string
	Environment    string
	RequiredEnv    string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
}

type Server struct {
	cfg       Config
	server    *http.Server
	registry  *prometheus.Registry
	requests  *prometheus.CounterVec
	durations *prometheus.HistogramVec
	ready     prometheus.Gauge
	closeOnce sync.Once
}

func New(cfg Config) (*Server, error) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = ":8080"
	}
	if strings.TrimSpace(cfg.ServiceName) == "" {
		cfg.ServiceName = "cloudops-demo-workload"
	}
	if strings.TrimSpace(cfg.ServiceVersion) == "" {
		cfg.ServiceVersion = "dev"
	}
	if strings.TrimSpace(cfg.Environment) == "" {
		cfg.Environment = "local-demo"
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 5 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_demo_http_requests_total", Help: "Demo workload HTTP requests."}, []string{"route", "status"})
	durations := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cloudops_demo_http_request_duration_seconds", Help: "Demo workload HTTP request duration."}, []string{"route"})
	ready := prometheus.NewGauge(prometheus.GaugeOpts{Name: "cloudops_demo_workload_ready", Help: "Whether REQUIRED_ENV is present."})
	for _, collector := range []prometheus.Collector{requests, durations, ready} {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register demo metrics: %w", err)
		}
	}
	s := &Server{cfg: cfg, registry: registry, requests: requests, durations: durations, ready: ready}
	s.server = &http.Server{Addr: cfg.ListenAddr, Handler: s.Handler(), ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout}
	s.updateReady()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", s.livez)
	mux.HandleFunc("/readyz", s.readyz)
	mux.HandleFunc("/version", s.version)
	mux.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{EnableOpenMetrics: false}))
	mux.HandleFunc("/", s.work)
	return otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		route := knownRoute(r.URL.Path)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(recorder, r)
		s.requests.WithLabelValues(route, fmt.Sprint(recorder.status)).Inc()
		s.durations.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}), "cloudops-demo-http")
}

func (s *Server) Serve(ctx context.Context) error {
	if s == nil || s.server == nil {
		return fmt.Errorf("demo server is not initialized")
	}
	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	var err error
	s.closeOnce.Do(func() { err = s.server.Shutdown(ctx) })
	return err
}

func (s *Server) updateReady() {
	if strings.TrimSpace(s.cfg.RequiredEnv) == "" {
		s.ready.Set(0)
		return
	}
	s.ready.Set(1)
}

func (s *Server) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"live": true, "service": s.cfg.ServiceName})
}

func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	s.updateReady()
	if strings.TrimSpace(s.cfg.RequiredEnv) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false, "reason": "required_env_missing"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true, "service": s.cfg.ServiceName})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"service": s.cfg.ServiceName, "version": s.cfg.ServiceVersion, "source_revision": s.cfg.SourceRevision, "environment": s.cfg.Environment})
}

func (s *Server) work(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(s.cfg.ServiceName).Start(r.Context(), "demo.request")
	defer span.End()
	if strings.TrimSpace(s.cfg.RequiredEnv) == "" {
		logger.FromContext(ctx).Warn("demo request failed", zap.String("reason", "required_env_missing"), zap.String("route", knownRoute(r.URL.Path)))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "required_env_missing"})
		return
	}
	logger.FromContext(ctx).Info("demo request served", zap.String("route", knownRoute(r.URL.Path)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": s.cfg.ServiceName})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	return r.ResponseWriter.Write(body)
}

func knownRoute(path string) string {
	switch path {
	case "/", "/livez", "/readyz", "/version", "/metrics":
		return path
	default:
		return "/other"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
