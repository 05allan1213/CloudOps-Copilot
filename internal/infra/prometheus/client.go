package promclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      []byte
}

var (
	ErrInvalid          = errors.New("invalid prometheus request")
	ErrUnavailable      = errors.New("prometheus unavailable")
	ErrResponseTooLarge = errors.New("prometheus response exceeds byte limit")
	ErrResultLimit      = errors.New("prometheus result exceeds series or sample limit")
)

type apiResponse struct {
	Status    string      `json:"status"`
	ErrorType string      `json:"errorType"`
	Error     string      `json:"error"`
	Data      queryResult `json:"data"`
	Warnings  []string    `json:"warnings"`
}

type queryResult struct {
	ResultType string         `json:"resultType"`
	Result     []vectorResult `json:"result"`
}

type vectorResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
	Values [][]interface{}   `json:"values"`
}

type RangePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type RangeSeries struct {
	Metric map[string]string `json:"metric"`
	Values []RangePoint      `json:"values"`
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

func NewBoundedClient(rawBaseURL string, token []byte, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBaseURL), "/"))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("%w: Prometheus endpoint must be a fixed HTTP URL", ErrInvalid)
	}
	if timeout < time.Second || timeout > time.Minute || len(token) > 64*1024 {
		return nil, fmt.Errorf("%w: Prometheus timeout or credential size is invalid", ErrInvalid)
	}
	return &Client{
		baseURL: strings.TrimRight(parsed.String(), "/"), token: append([]byte(nil), token...),
		httpClient: &http.Client{
			Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("prometheus redirects are disabled")
			},
		},
	}, nil
}

func (c *Client) Close() {
	if c == nil {
		return
	}
	for index := range c.token {
		c.token[index] = 0
	}
	c.token = nil
}

func (c *Client) Ready(ctx context.Context) (retErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/-/ready", nil)
	if err != nil {
		return fmt.Errorf("build prometheus readiness request: %w", err)
	}
	c.authorize(request)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("prometheus readiness check failed: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, response.Body.Close()) }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus readiness check returned status %d", response.StatusCode)
	}

	return nil
}

func (c *Client) BuildInfo(ctx context.Context, maxResponseBytes int64) (string, int, error) {
	body, status, err := c.get(ctx, "/api/v1/status/buildinfo", nil, maxResponseBytes)
	if err != nil {
		return "", status, err
	}
	var response struct {
		Status string `json:"status"`
		Data   struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Status != "success" {
		return "", status, fmt.Errorf("%w: malformed build information", ErrUnavailable)
	}
	return strings.TrimSpace(response.Data.Version), status, nil
}

func (c *Client) MetricNames(ctx context.Context, limit int, maxResponseBytes int64) ([]string, int, error) {
	if limit < 1 || limit > 10_000 {
		return nil, 0, fmt.Errorf("%w: metric catalog limit is invalid", ErrInvalid)
	}
	body, status, err := c.get(ctx, "/api/v1/label/__name__/values", url.Values{"limit": []string{strconv.Itoa(limit)}}, maxResponseBytes)
	if err != nil {
		return nil, status, err
	}
	var response struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Status != "success" {
		return nil, status, fmt.Errorf("%w: malformed metric catalog", ErrUnavailable)
	}
	if len(response.Data) > limit {
		response.Data = response.Data[:limit]
	}
	result := make([]string, 0, len(response.Data))
	for _, name := range response.Data {
		name = strings.TrimSpace(name)
		if name != "" && len(name) <= 256 {
			result = append(result, name)
		}
	}
	return result, len(body), nil
}

func (c *Client) QueryRangeBounded(ctx context.Context, query string, start, end time.Time, step time.Duration, maxResponseBytes int64, maxSeries, maxSamples int) ([]RangeSeries, int, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || start.IsZero() || end.IsZero() || !end.After(start) || step <= 0 ||
		maxResponseBytes < 1024 || maxResponseBytes > 4*1024*1024 || maxSeries < 1 || maxSamples < 1 {
		return nil, 0, false, fmt.Errorf("%w: bounded range query is invalid", ErrInvalid)
	}
	values := url.Values{
		"query": []string{query}, "start": []string{formatPrometheusTime(start)},
		"end": []string{formatPrometheusTime(end)}, "step": []string{formatPrometheusStep(step)},
	}
	body, _, err := c.get(ctx, "/api/v1/query_range", values, maxResponseBytes)
	if err != nil {
		return nil, 0, false, err
	}
	var payload apiResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, len(body), false, fmt.Errorf("%w: decode range response", ErrUnavailable)
	}
	if payload.Status != "success" {
		return nil, len(body), false, fmt.Errorf("%w: Prometheus rejected bounded range query", ErrUnavailable)
	}
	if len(payload.Data.Result) > maxSeries {
		return nil, len(body), false, ErrResultLimit
	}
	results := make([]RangeSeries, 0, len(payload.Data.Result))
	samples := 0
	for _, item := range payload.Data.Result {
		series, err := parseRangeResult(item)
		if err != nil {
			return nil, len(body), false, fmt.Errorf("%w: malformed range series", ErrUnavailable)
		}
		samples += len(series.Values)
		if samples > maxSamples {
			return nil, len(body), false, ErrResultLimit
		}
		results = append(results, series)
	}
	return results, len(body), len(payload.Warnings) > 0, nil
}

func (c *Client) get(ctx context.Context, path string, values url.Values, maxResponseBytes int64) ([]byte, int, error) {
	if c == nil || c.httpClient == nil || maxResponseBytes < 1 {
		return nil, 0, ErrInvalid
	}
	endpoint := c.baseURL + path
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: build Prometheus request", ErrInvalid)
	}
	request.Header.Set("Accept", "application/json")
	c.authorize(request)
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, response.StatusCode, ErrUnavailable
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, response.StatusCode, ErrResponseTooLarge
	}
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, ErrUnavailable
	}
	return body, response.StatusCode, nil
}

func (c *Client) authorize(request *http.Request) {
	if c != nil && request != nil && len(c.token) > 0 {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(c.token)))
	}
}

func (c *Client) QueryRangeRaw(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]RangeSeries, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("prometheus query is required")
	}

	return c.queryRange(ctx, query, start, end, step, 1024*1024)
}

func (c *Client) queryRange(ctx context.Context, query string, start, end time.Time, step time.Duration, maxResponseBytes int64) (series []RangeSeries, retErr error) {
	if start.IsZero() || end.IsZero() {
		return nil, fmt.Errorf("range query start and end are required")
	}
	if !end.After(start) {
		return nil, fmt.Errorf("range query end must be after start")
	}
	if step <= 0 {
		return nil, fmt.Errorf("range query step must be positive")
	}

	endpoint := c.baseURL + "/api/v1/query_range"
	values := url.Values{}
	values.Set("query", query)
	values.Set("start", formatPrometheusTime(start))
	values.Set("end", formatPrometheusTime(end))
	values.Set("step", formatPrometheusStep(step))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build prometheus range query request: %w", err)
	}
	c.authorize(request)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("range query prometheus failed: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, response.Body.Close()) }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("range query prometheus returned status %d", response.StatusCode)
	}

	payload, err := decodeRangeResponse(response.Body, maxResponseBytes)
	if err != nil {
		return nil, err
	}

	if payload.Status != "success" {
		if payload.Error != "" {
			return nil, fmt.Errorf("prometheus range query error: %s", payload.Error)
		}
		return nil, fmt.Errorf("prometheus range query failed with status %s", payload.Status)
	}

	results := make([]RangeSeries, 0, len(payload.Data.Result))
	for _, item := range payload.Data.Result {
		series, err := parseRangeResult(item)
		if err != nil {
			return nil, err
		}
		results = append(results, series)
	}

	return results, nil
}

func decodeRangeResponse(body io.Reader, maxResponseBytes int64) (apiResponse, error) {
	var payload apiResponse
	if maxResponseBytes <= 0 {
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			return apiResponse{}, fmt.Errorf("decode prometheus range response: %w", err)
		}
		return payload, nil
	}

	data, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return apiResponse{}, fmt.Errorf("read prometheus range response: %w", err)
	}
	if int64(len(data)) > maxResponseBytes {
		return apiResponse{}, fmt.Errorf("prometheus range response too large")
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return apiResponse{}, fmt.Errorf("decode prometheus range response: %w", err)
	}
	return payload, nil
}

func parseRangeResult(item vectorResult) (RangeSeries, error) {
	points := make([]RangePoint, 0, len(item.Values))
	for _, rawPoint := range item.Values {
		if len(rawPoint) != 2 {
			return RangeSeries{}, fmt.Errorf("unexpected prometheus range value format")
		}

		timestamp, err := parseTimestamp(rawPoint[0])
		if err != nil {
			return RangeSeries{}, err
		}

		value, err := parseFloat(rawPoint[1])
		if err != nil {
			return RangeSeries{}, err
		}

		points = append(points, RangePoint{
			Timestamp: timestamp,
			Value:     value,
		})
	}

	return RangeSeries{
		Metric: item.Metric,
		Values: points,
	}, nil
}

func parseTimestamp(value interface{}) (time.Time, error) {
	floatValue, err := parseFloat(value)
	if err != nil {
		return time.Time{}, err
	}
	sec := int64(floatValue)
	nsec := int64((floatValue - float64(sec)) * 1e9)
	return time.Unix(sec, nsec), nil
}

func parseFloat(value interface{}) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, fmt.Errorf("parse float value: %w", err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unexpected value type %T", value)
	}
}

func formatPrometheusTime(ts time.Time) string {
	return strconv.FormatFloat(float64(ts.UnixNano())/1e9, 'f', 3, 64)
}

func formatPrometheusStep(step time.Duration) string {
	return strconv.FormatFloat(step.Seconds(), 'f', -1, 64)
}
