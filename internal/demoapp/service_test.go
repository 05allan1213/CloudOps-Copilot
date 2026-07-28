package demoapp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMissingRequiredEnvStaysLiveAndEmitsBoundedFailureSignals(t *testing.T) {
	server, err := New(Config{RequiredEnv: "", ServiceVersion: "v1", SourceRevision: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	assertDemoStatus(t, handler, "/livez", http.StatusOK)
	ready := assertDemoStatus(t, handler, "/readyz", http.StatusServiceUnavailable)
	if !strings.Contains(ready, "required_env_missing") {
		t.Fatalf("readiness body=%s", ready)
	}
	failure := assertDemoStatus(t, handler, "/", http.StatusInternalServerError)
	if !strings.Contains(failure, "required_env_missing") {
		t.Fatalf("work body=%s", failure)
	}
	metrics := assertDemoStatus(t, handler, "/metrics", http.StatusOK)
	for _, expected := range []string{
		"cloudops_demo_workload_ready 0",
		`cloudops_demo_http_requests_total{route="/",status="500"} 1`,
		`cloudops_demo_http_errors_total{route="/",status="500"} 1`,
		`cloudops_demo_http_requests_total{route="/readyz",status="503"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
}

func TestHealthyDemoExposesVersionWithoutLeakingRequiredValue(t *testing.T) {
	server, err := New(Config{RequiredEnv: "do-not-return-this-value", ServiceName: "demo", ServiceVersion: "v2", SourceRevision: strings.Repeat("b", 40), Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	assertDemoStatus(t, handler, "/readyz", http.StatusOK)
	body := assertDemoStatus(t, handler, "/", http.StatusOK)
	version := assertDemoStatus(t, handler, "/version", http.StatusOK)
	if strings.Contains(body+version, server.cfg.RequiredEnv) || !strings.Contains(version, strings.Repeat("b", 40)) || !strings.Contains(version, `"version":"v2"`) {
		t.Fatalf("unsafe or incomplete response: body=%s version=%s", body, version)
	}
	metrics := assertDemoStatus(t, handler, "/metrics", http.StatusOK)
	if !strings.Contains(metrics, "cloudops_demo_workload_ready 1") {
		t.Fatalf("healthy gauge missing:\n%s", metrics)
	}
}

func TestUnknownPathsUseOneBoundedMetricLabel(t *testing.T) {
	server, err := New(Config{RequiredEnv: "present"})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	assertDemoStatus(t, handler, "/arbitrary/one", http.StatusOK)
	assertDemoStatus(t, handler, "/arbitrary/two", http.StatusOK)
	metrics := assertDemoStatus(t, handler, "/metrics", http.StatusOK)
	if !strings.Contains(metrics, `cloudops_demo_http_requests_total{route="/other",status="200"} 2`) {
		t.Fatalf("bounded route counter missing:\n%s", metrics)
	}
	if strings.Contains(metrics, "arbitrary") {
		t.Fatal("unbounded request path leaked into a metric label")
	}
}

func assertDemoStatus(t *testing.T, handler http.Handler, path string, want int) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("GET %s status=%d want=%d body=%s", path, response.Code, want, response.Body.String())
	}
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
