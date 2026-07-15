package observabilityread

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"server-web/internal/verification"
)

func testConfig(server *httptest.Server) Config {
	return Config{BaseURL: server.URL, Timeout: time.Second, MaxResponseBytes: 4096, MaxSamples: 10, MaxSeries: 2, MaxTraces: 2, MaxLookback: time.Hour, Retries: 1, AllowedServices: map[string]struct{}{"checkout\"api": {}}, AllowedNamespaces: map[string]struct{}{"shop": {}}, AllowedEnvironments: map[string]struct{}{"staging": {}}, AllowHTTPForTests: true}
}

func query(check verification.CheckType) verification.SignalQuery {
	return verification.SignalQuery{Template: string(check), Service: "checkout\"api", Namespace: "shop", Environment: "staging", Lookback: 5 * time.Minute, Step: 10 * time.Second, MaxSeries: 2, MaxSamples: 10}
}

func TestPrometheusFixedTemplateBoundsAndStatuses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		want    verification.ObservationStatus
		wantErr bool
	}{
		{"success", 200, fmt.Sprintf(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[%f,"0.05"]]}]}}`, float64(time.Now().Unix())), verification.ObservationAvailable, false},
		{"no_data", 200, `{"status":"success","data":{"resultType":"matrix","result":[]}}`, verification.ObservationNoData, false},
		{"malformed", 200, `{`, verification.ObservationMalformed, true},
		{"401", 401, `{}`, verification.ObservationUnavailable, true},
		{"403", 403, `{}`, verification.ObservationUnavailable, true},
		{"404", 404, `{}`, verification.ObservationUnavailable, true},
		{"429", 429, `{}`, verification.ObservationUnavailable, true},
		{"500", 500, `{}`, verification.ObservationUnavailable, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/query_range" {
					t.Errorf("write or wrong endpoint: %s %s", r.Method, r.URL.Path)
				}
				if !strings.Contains(r.URL.Query().Get("query"), `service="checkout\"api"`) {
					t.Errorf("label not escaped in trusted template")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewPrometheus(testConfig(server))
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.ObserveMetric(context.Background(), query(verification.CheckMetricErrorRateBelow))
			if (err != nil) != tc.wantErr || result.Observation.Status != tc.want {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if (tc.status == 429 || tc.status >= 500) && requests.Load() != 2 {
				t.Fatalf("bounded retry count=%d", requests.Load())
			}
		})
	}
}

func TestLokiAndTempoBoundedFacts(t *testing.T) {
	now := time.Now().Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/query_range":
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"values":[[%d,"2"]]}]}}`, now)
		case "/api/search":
			_, _ = fmt.Fprintf(w, `{"traces":[{"startTimeUnixNano":"%d","durationMs":125,"rootServiceName":"checkout"}]}`, time.Now().UnixNano())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := testConfig(server)
	loki, err := NewLoki(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := loki.ObserveLogErrorRate(context.Background(), query(verification.CheckLogErrorAbsent))
	if err != nil || logs.Observation.MatchedCount != 2 || len(logs.Observation.RedactedExamples) != 0 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	tempo, err := NewTempo(cfg)
	if err != nil {
		t.Fatal(err)
	}
	traces, err := tempo.ObserveTraceErrorRate(context.Background(), query(verification.CheckTraceLatencyP95Below))
	if err != nil || traces.Observation.Value != .125 || traces.Observation.SampleCount != 1 {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
}

func TestCancellationOversizeLimitsAndAuthority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 5000))) }))
	defer server.Close()
	client, err := NewPrometheus(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ObserveMetric(context.Background(), query(verification.CheckMetricErrorRateBelow))
	if err == nil || result.Observation.Status != verification.ObservationUnavailable {
		t.Fatalf("oversize=%+v err=%v", result, err)
	}
	bad := query(verification.CheckMetricErrorRateBelow)
	bad.Service = "foreign"
	if _, err := client.ObserveMetric(context.Background(), bad); err == nil {
		t.Fatal("foreign service accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ObserveMetric(ctx, query(verification.CheckMetricErrorRateBelow)); err == nil {
		t.Fatal("cancelled request accepted")
	}
	if !strings.Contains(Redact("Authorization=Bearer super-secret token=abc"), "[REDACTED]") {
		t.Fatal("secret redaction failed")
	}
}

func TestRejectsMutableOrInsecureEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	cfg := testConfig(server)
	cfg.AllowHTTPForTests = false
	if _, err := NewTempo(cfg); err == nil {
		t.Fatal("insecure endpoint accepted")
	}
	cfg.AllowHTTPForTests = true
	cfg.BaseURL += "?target=https://evil.example"
	if _, err := NewLoki(cfg); err == nil {
		t.Fatal("mutable query endpoint accepted")
	}
}

func TestProviderCardinalityAndNonFiniteLimits(t *testing.T) {
	now := float64(time.Now().Unix())
	tooManySeries := fmt.Sprintf(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[%f,"1"]]},{"values":[[%f,"1"]]}]}}`, now, now)
	if _, err := parsePrometheus([]byte(tooManySeries), 1, 10); err == nil {
		t.Fatal("series limit accepted")
	}
	tooManySamples := fmt.Sprintf(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[%f,"1"],[%f,"2"]]}]}}`, now, now)
	if _, err := parsePrometheus([]byte(tooManySamples), 2, 1); err == nil {
		t.Fatal("sample limit accepted")
	}
	nonFinite := fmt.Sprintf(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[%f,"NaN"]]}]}}`, now)
	if _, err := parsePrometheus([]byte(nonFinite), 2, 10); err == nil {
		t.Fatal("NaN accepted")
	}
	traces := fmt.Sprintf(`{"traces":[{"startTimeUnixNano":"%d","durationMs":1},{"startTimeUnixNano":"%d","durationMs":2}]}`, time.Now().UnixNano(), time.Now().UnixNano())
	if _, err := parseTempo([]byte(traces), 1, string(verification.CheckTraceLatencyP95Below)); err == nil {
		t.Fatal("trace limit accepted")
	}
}
