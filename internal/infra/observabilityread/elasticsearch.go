package observabilityread

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type ElasticConfig struct {
	BaseURL             string
	KibanaURL           string
	IndexPattern        string
	BearerTokenFile     string
	UsernameFile        string
	PasswordFile        string
	CAFile              string
	Timeout             time.Duration
	MaxResponseBytes    int64
	MaxSamples          int
	MaxLookback         time.Duration
	AllowedServices     map[string]struct{}
	AllowedNamespaces   map[string]struct{}
	AllowedEnvironments map[string]struct{}
	AllowHTTPForTests   bool
	HTTPClient          *http.Client
}

type ElasticQuery struct {
	Service     string
	Namespace   string
	Environment string
	Workload    string
	Lookback    time.Duration
	Severity    string
	Keyword     string
	TraceID     string
	Limit       int
}

type Elastic struct {
	base, kibana *url.URL
	index        string
	bearerFile   string
	usernameFile string
	passwordFile string
	http         *http.Client
	maxBytes     int64
	maxSamples   int
	maxLookback  time.Duration
	services     map[string]struct{}
	namespaces   map[string]struct{}
	environments map[string]struct{}
}

var traceIDPattern = regexp.MustCompile(`^[a-f0-9]{16}(?:[a-f0-9]{16})?$`)

func NewElasticsearch(cfg ElasticConfig) (*Elastic, error) {
	base, err := fixedEndpoint(cfg.BaseURL, cfg.AllowHTTPForTests)
	if err != nil {
		return nil, fmt.Errorf("invalid fixed Elasticsearch endpoint")
	}
	var kibana *url.URL
	if strings.TrimSpace(cfg.KibanaURL) != "" {
		kibana, err = fixedEndpoint(cfg.KibanaURL, cfg.AllowHTTPForTests)
		if err != nil {
			return nil, fmt.Errorf("invalid fixed Kibana endpoint")
		}
	}
	if cfg.IndexPattern != "logs-cloudops-*" || cfg.Timeout < time.Second || cfg.Timeout > time.Minute ||
		cfg.MaxLookback < time.Minute || cfg.MaxLookback > 24*time.Hour || len(cfg.AllowedServices) == 0 ||
		len(cfg.AllowedNamespaces) == 0 || len(cfg.AllowedEnvironments) == 0 {
		return nil, fmt.Errorf("invalid Elasticsearch bounds")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxBytes
	}
	if cfg.MaxSamples == 0 {
		cfg.MaxSamples = 20
	}
	if cfg.MaxResponseBytes < 1024 || cfg.MaxResponseBytes > 1024*1024 || cfg.MaxSamples < 1 || cfg.MaxSamples > 100 {
		return nil, fmt.Errorf("invalid Elasticsearch limits")
	}
	bearer := strings.TrimSpace(cfg.BearerTokenFile) != ""
	basic := strings.TrimSpace(cfg.UsernameFile) != "" || strings.TrimSpace(cfg.PasswordFile) != ""
	if bearer && basic || basic && (strings.TrimSpace(cfg.UsernameFile) == "" || strings.TrimSpace(cfg.PasswordFile) == "") {
		return nil, fmt.Errorf("configure exactly one complete Elasticsearch authentication mode")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if strings.TrimSpace(cfg.CAFile) != "" {
			ca, readErr := os.ReadFile(cfg.CAFile)
			if readErr != nil || len(ca) == 0 || len(ca) > 1024*1024 {
				return nil, fmt.Errorf("read Elasticsearch CA: %w", readErr)
			}
			pool, _ := x509.SystemCertPool()
			if pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(ca) {
				return nil, errors.New("elasticsearch CA contains no certificate")
			}
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
		}
		httpClient = &http.Client{Timeout: cfg.Timeout, Transport: transport}
	}
	base.Path = strings.TrimRight(base.Path, "/")
	return &Elastic{
		base: base, kibana: kibana, index: cfg.IndexPattern, bearerFile: cfg.BearerTokenFile,
		usernameFile: cfg.UsernameFile, passwordFile: cfg.PasswordFile, http: httpClient,
		maxBytes: cfg.MaxResponseBytes, maxSamples: cfg.MaxSamples, maxLookback: cfg.MaxLookback,
		services: cfg.AllowedServices, namespaces: cfg.AllowedNamespaces, environments: cfg.AllowedEnvironments,
	}, nil
}

func fixedEndpoint(raw string, allowHTTP bool) (*url.URL, error) {
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" ||
		(value.Scheme != "https" && (!allowHTTP || value.Scheme != "http")) {
		return nil, errors.New("invalid endpoint")
	}
	return value, nil
}

func (e *Elastic) ObserveLogErrorRate(ctx context.Context, query verification.SignalQuery) (verification.SignalResult, error) {
	observation, err := e.Search(ctx, ElasticQuery{
		Service: query.Service, Namespace: query.Namespace, Environment: query.Environment, Workload: query.Service,
		Lookback: query.Lookback, Severity: "error", Keyword: "required_env_missing", Limit: min(e.maxSamples, query.MaxSamples),
	})
	return verification.SignalResult{
		Value: float64(observation.MatchedCount), SampleCount: observation.SampleCount,
		SeriesCount: observation.SeriesCount, Observation: observation,
	}, err
}

func (e *Elastic) Search(ctx context.Context, query ElasticQuery) (verification.Observation, error) {
	if e == nil || e.http == nil {
		return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_unavailable"}, verification.ErrUnavailable
	}
	if _, ok := e.services[query.Service]; !ok {
		return verification.Observation{}, verification.ErrNotAllowed
	}
	if _, ok := e.namespaces[query.Namespace]; !ok {
		return verification.Observation{}, verification.ErrNotAllowed
	}
	if _, ok := e.environments[query.Environment]; !ok {
		return verification.Observation{}, verification.ErrNotAllowed
	}
	query.Workload = strings.TrimSpace(query.Workload)
	query.Severity = strings.ToLower(strings.TrimSpace(query.Severity))
	query.Keyword = strings.ToLower(strings.TrimSpace(query.Keyword))
	query.TraceID = strings.ToLower(strings.TrimSpace(query.TraceID))
	if query.Lookback < time.Minute || query.Lookback > e.maxLookback || query.Workload == "" || len(query.Workload) > 253 ||
		(query.Severity != "all" && query.Severity != "error" && query.Severity != "warning") ||
		(query.Keyword != "required_env_missing" && query.Keyword != "request_failure") ||
		(query.TraceID != "" && !traceIDPattern.MatchString(query.TraceID)) {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	if query.Limit <= 0 {
		query.Limit = 3
	}
	if query.Limit > e.maxSamples {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	now := time.Now().UTC()
	filters := []any{
		map[string]any{"range": map[string]any{"@timestamp": map[string]any{"gte": now.Add(-query.Lookback).Format(time.RFC3339Nano), "lte": now.Format(time.RFC3339Nano)}}},
		map[string]any{"term": map[string]any{"kubernetes.namespace": query.Namespace}},
		map[string]any{"prefix": map[string]any{"kubernetes.pod.name": query.Workload + "-"}},
		map[string]any{"match_phrase": map[string]any{"message": query.Keyword}},
	}
	switch query.Severity {
	case "error":
		filters = append(filters, map[string]any{"bool": map[string]any{"should": []any{
			map[string]any{"term": map[string]any{"log.level": "error"}}, map[string]any{"match_phrase": map[string]any{"message": "\"level\":\"error\""}},
		}, "minimum_should_match": 1}})
	case "warning":
		filters = append(filters, map[string]any{"bool": map[string]any{"should": []any{
			map[string]any{"term": map[string]any{"log.level": "warn"}}, map[string]any{"term": map[string]any{"log.level": "warning"}},
			map[string]any{"match_phrase": map[string]any{"message": "\"level\":\"warn\""}}, map[string]any{"match_phrase": map[string]any{"message": "\"level\":\"warning\""}},
		}, "minimum_should_match": 1}})
	}
	if query.TraceID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"trace.id": query.TraceID}})
	}
	body, _ := json.Marshal(map[string]any{
		"size": query.Limit, "track_total_hits": true,
		"sort":    []any{map[string]any{"@timestamp": map[string]any{"order": "desc"}}},
		"_source": []string{"@timestamp", "message", "kubernetes.pod.name", "container.name", "service.version", "trace.id"},
		"query":   map[string]any{"bool": map[string]any{"filter": filters}},
	})
	endpoint := *e.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + e.index + "/_search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if err := e.authorize(req); err != nil {
		return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_credentials_unavailable"}, err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return verification.Observation{}, ctx.Err()
		}
		return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_unavailable"}, verification.ErrUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, e.maxBytes+1))
	if readErr != nil || int64(len(data)) > e.maxBytes {
		return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "response_limit"}, verification.ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_forbidden"}, verification.ErrNotAllowed
		case http.StatusNotFound:
			return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_not_found"}, verification.ErrNotFound
		default:
			return verification.Observation{Status: verification.ObservationUnavailable, ReasonCode: "provider_unavailable"}, verification.ErrUnavailable
		}
	}
	observation, err := parseElasticSearch(data, now)
	if err != nil {
		return verification.Observation{Status: verification.ObservationMalformed, ReasonCode: "malformed_response"}, err
	}
	observation.QueryValid, observation.SourceHealthy, observation.RetentionCovered = true, true, true
	observation.SourceReference = "elasticsearch://" + e.base.Host + "/" + e.index
	return observation, nil
}

func (e *Elastic) authorize(req *http.Request) error {
	if strings.TrimSpace(e.bearerFile) != "" {
		value, err := boundedSecret(e.bearerFile)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+value)
		return nil
	}
	if strings.TrimSpace(e.usernameFile) != "" {
		username, err := boundedSecret(e.usernameFile)
		if err != nil {
			return err
		}
		password, err := boundedSecret(e.passwordFile)
		if err != nil {
			return err
		}
		req.SetBasicAuth(username, password)
	}
	return nil
}

func boundedSecret(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil || len(value) == 0 || len(value) > 16*1024 {
		return "", verification.ErrUnavailable
	}
	result := strings.TrimSpace(string(value))
	if result == "" {
		return "", verification.ErrUnavailable
	}
	return result, nil
}

type elasticResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			Source map[string]any `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

func parseElasticSearch(data []byte, sampledAt time.Time) (verification.Observation, error) {
	var payload elasticResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload.Hits.Total.Value < 0 {
		return verification.Observation{}, errors.New("malformed Elasticsearch response")
	}
	examples := make([]string, 0, len(payload.Hits.Hits))
	pods := map[string]struct{}{}
	for _, hit := range payload.Hits.Hits {
		message, _ := hit.Source["message"].(string)
		message = boundText(Redact(message), 240)
		if message != "" {
			examples = append(examples, message)
		}
		if kubernetes, ok := hit.Source["kubernetes"].(map[string]any); ok {
			if pod, ok := kubernetes["pod"].(map[string]any); ok {
				if name, _ := pod["name"].(string); name != "" {
					pods[name] = struct{}{}
				}
			}
		}
	}
	status := verification.ObservationAvailable
	if payload.Hits.Total.Value == 0 {
		status = verification.ObservationNoData
	}
	return verification.Observation{
		Status: status, Value: float64(payload.Hits.Total.Value), MatchedCount: payload.Hits.Total.Value,
		SampleCount: 1, SeriesCount: len(pods), SampledAt: sampledAt.UTC(), RedactedExamples: examples,
	}, nil
}

func boundText(value string, limit int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "?")
	value = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value)
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit]
}
