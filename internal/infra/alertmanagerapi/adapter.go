// Package alertmanagerapi owns bounded Alertmanager API operations in Worker.
package alertmanagerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/google/uuid"
)

const maxProviderResponseBytes = 512 * 1024

var labelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type accessStore interface {
	ProviderAccess(context.Context, string, settings.Provider) (settings.ProviderAccess, error)
}

type Adapter struct {
	configuration accessStore
}

func New(configuration accessStore) (*Adapter, error) {
	if configuration == nil {
		return nil, errors.New("alertmanager adapter requires Operational Configuration")
	}
	return &Adapter{configuration: configuration}, nil
}

type providerSilence struct {
	ID      string `json:"id"`
	Comment string `json:"comment"`
	Status  struct {
		State string `json:"state"`
	} `json:"status"`
}

type createResponse struct {
	SilenceID string `json:"silenceID"`
}

type providerMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

func (a *Adapter) CreateSilence(ctx context.Context, request alertdomain.SilenceProviderRequest) (string, error) {
	if err := validateCreateRequest(request); err != nil {
		return "", err
	}
	access, err := a.configuration.ProviderAccess(ctx, request.ConfigurationRevisionID, settings.ProviderAlertmanager)
	if err != nil {
		return "", alertdomain.ErrProviderUnavailable
	}
	defer access.Clear()
	if !access.Configuration.Enabled {
		return "", alertdomain.ErrProviderDisabled
	}
	client, base, err := providerClient(access)
	if err != nil {
		return "", err
	}
	operationCtx, cancel := context.WithTimeout(ctx, time.Duration(access.Configuration.TimeoutMS)*time.Millisecond)
	defer cancel()
	var existing []providerSilence
	if err := doJSON(operationCtx, client, http.MethodGet, endpoint(base, "/api/v2/silences"), access.Credential, nil, &existing); err != nil {
		return "", err
	}
	for _, silence := range existing {
		if silence.Comment == request.Comment && silence.ID != "" &&
			(silence.Status.State == "active" || silence.Status.State == "pending") {
			return silence.ID, nil
		}
	}
	matchers := make([]providerMatcher, 0, len(request.Matchers))
	for _, matcher := range request.Matchers {
		matchers = append(matchers, providerMatcher{
			Name: matcher.Name, Value: matcher.Value, IsRegex: matcher.IsRegex, IsEqual: matcher.IsEqual,
		})
	}
	payload := map[string]any{
		"matchers": matchers, "startsAt": request.StartsAt.UTC().Format(time.RFC3339Nano),
		"endsAt": request.EndsAt.UTC().Format(time.RFC3339Nano), "createdBy": request.CreatedBy,
		"comment": request.Comment,
	}
	var response createResponse
	if err := doJSON(operationCtx, client, http.MethodPost, endpoint(base, "/api/v2/silences"), access.Credential, payload, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.SilenceID) == "" || len(response.SilenceID) > 128 {
		return "", alertdomain.ErrProviderUnavailable
	}
	return response.SilenceID, nil
}

func (a *Adapter) ExpireSilence(ctx context.Context, providerID, revisionID string) error {
	providerID, revisionID = strings.TrimSpace(providerID), strings.TrimSpace(revisionID)
	if providerID == "" || len(providerID) > 128 {
		return alertdomain.ErrInvalid
	}
	if _, err := uuid.Parse(revisionID); err != nil {
		return alertdomain.ErrInvalid
	}
	access, err := a.configuration.ProviderAccess(ctx, revisionID, settings.ProviderAlertmanager)
	if err != nil {
		return alertdomain.ErrProviderUnavailable
	}
	defer access.Clear()
	if !access.Configuration.Enabled {
		return alertdomain.ErrProviderDisabled
	}
	client, base, err := providerClient(access)
	if err != nil {
		return err
	}
	operationCtx, cancel := context.WithTimeout(ctx, time.Duration(access.Configuration.TimeoutMS)*time.Millisecond)
	defer cancel()
	return doJSON(operationCtx, client, http.MethodDelete,
		endpoint(base, "/api/v2/silence/"+url.PathEscape(providerID)), access.Credential, nil, nil)
}

func validateCreateRequest(request alertdomain.SilenceProviderRequest) error {
	if !strings.HasPrefix(request.ExternalID, "cloudops-silence:") || request.Comment != request.ExternalID ||
		request.CreatedBy != "owner" || len(request.Matchers) < 1 || len(request.Matchers) > 8 {
		return alertdomain.ErrInvalid
	}
	if _, err := uuid.Parse(strings.TrimPrefix(request.ExternalID, "cloudops-silence:")); err != nil {
		return alertdomain.ErrInvalid
	}
	if _, err := uuid.Parse(request.ConfigurationRevisionID); err != nil {
		return alertdomain.ErrInvalid
	}
	duration := request.EndsAt.Sub(request.StartsAt)
	if request.StartsAt.IsZero() || request.EndsAt.IsZero() || duration < alertdomain.MinimumSilenceDuration || duration > alertdomain.MaximumSilenceDuration {
		return alertdomain.ErrInvalid
	}
	seen := make(map[string]struct{}, len(request.Matchers))
	for _, matcher := range request.Matchers {
		if !labelNamePattern.MatchString(matcher.Name) || strings.TrimSpace(matcher.Value) == "" ||
			len(matcher.Value) > 1024 || matcher.IsRegex || !matcher.IsEqual {
			return alertdomain.ErrInvalid
		}
		if _, exists := seen[matcher.Name]; exists {
			return alertdomain.ErrInvalid
		}
		seen[matcher.Name] = struct{}{}
	}
	return nil
}

func providerClient(access settings.ProviderAccess) (*http.Client, *url.URL, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(access.Configuration.Endpoint), "/"))
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" ||
		base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, nil, alertdomain.ErrInvalid
	}
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("alertmanager redirects are disabled") },
	}, base, nil
}

func endpoint(base *url.URL, path string) string {
	result := *base
	result.Path = strings.TrimRight(result.Path, "/") + path
	return result.String()
}

func doJSON(ctx context.Context, client *http.Client, method, endpoint string, credential []byte, body, target any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil || len(encoded) > 32*1024 {
			return alertdomain.ErrInvalid
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("create Alertmanager request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if len(credential) > 0 {
		request.Header.Set("Authorization", "Bearer "+string(credential))
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return alertdomain.ErrProviderUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(content) > maxProviderResponseBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
		return alertdomain.ErrProviderUnavailable
	}
	if target != nil && len(content) > 0 {
		if err := json.Unmarshal(content, target); err != nil {
			return alertdomain.ErrProviderUnavailable
		}
	}
	return nil
}

var _ alertdomain.SilenceProvider = (*Adapter)(nil)
