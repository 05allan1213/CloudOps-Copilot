package alertmanageringress

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentv3mysql"
)

const (
	maxTargets          = 32
	maxMatchLabels      = 16
	maxConfigFieldBytes = 253
	maxBearerFileBytes  = 4096
)

// Target is a server-owned mapping from bounded Alertmanager labels to one
// canonical workload identity. Alert labels select an entry but never become
// trusted Incident dimensions directly.
type Target struct {
	ClusterID    string            `json:"cluster_id"`
	Environment  string            `json:"environment"`
	Namespace    string            `json:"namespace"`
	WorkloadKind string            `json:"workload_kind"`
	WorkloadName string            `json:"workload_name"`
	ServiceName  string            `json:"service_name"`
	MatchLabels  map[string]string `json:"match_labels"`
}

// Config owns the INTERNAL webhook transport and normalization boundaries.
type Config struct {
	Store          Store
	Targets        []Target
	MaxBodyBytes   int64
	RequestTimeout time.Duration
	BearerToken    []byte
}

// Store is the complete durable ingress contract. Rejections are durable
// facts, not log-only validation failures.
type Store interface {
	Ready(ctx context.Context) error
	IngestBatch(ctx context.Context, signals []incidentv3mysql.SignalInput) ([]incidentv3mysql.IngestResult, error)
	RecordRejections(ctx context.Context, rejections []incidentv3mysql.RejectionInput) error
}

// ParseTargetAllowlist strictly decodes and validates the server-owned target
// mappings. Ambiguous identical selectors fail startup.
func ParseTargetAllowlist(raw string) ([]Target, error) {
	if err := validateUniqueJSONKeys([]byte(raw)); err != nil {
		return nil, fmt.Errorf("decode SIGNAL_TARGET_ALLOWLIST_JSON: %w", err)
	}
	if err := validateExactTargetFields([]byte(raw)); err != nil {
		return nil, fmt.Errorf("decode SIGNAL_TARGET_ALLOWLIST_JSON: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var targets []Target
	if err := decoder.Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode SIGNAL_TARGET_ALLOWLIST_JSON: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode SIGNAL_TARGET_ALLOWLIST_JSON: %w", err)
	}
	if len(targets) == 0 || len(targets) > maxTargets {
		return nil, fmt.Errorf("SIGNAL_TARGET_ALLOWLIST_JSON must contain 1..%d targets", maxTargets)
	}
	selectors := make(map[string]struct{}, len(targets))
	for index := range targets {
		var err error
		targets[index], err = normalizeTarget(targets[index])
		if err != nil {
			return nil, fmt.Errorf("SIGNAL_TARGET_ALLOWLIST_JSON target %d: %w", index, err)
		}
		if err := validateTarget(targets[index]); err != nil {
			return nil, fmt.Errorf("SIGNAL_TARGET_ALLOWLIST_JSON target %d: %w", index, err)
		}
		key := selectorKey(targets[index].MatchLabels)
		if _, exists := selectors[key]; exists {
			return nil, fmt.Errorf("SIGNAL_TARGET_ALLOWLIST_JSON target %d duplicates a match_labels selector", index)
		}
		selectors[key] = struct{}{}
	}
	return targets, nil
}

func validateExactTargetFields(contents []byte) error {
	var targets []map[string]json.RawMessage
	if err := json.Unmarshal(contents, &targets); err != nil {
		return err
	}
	allowedFields := map[string]struct{}{
		"cluster_id": {}, "environment": {}, "namespace": {}, "workload_kind": {},
		"workload_name": {}, "service_name": {}, "match_labels": {},
	}
	for index, target := range targets {
		for field := range target {
			if _, allowed := allowedFields[field]; !allowed {
				return fmt.Errorf("target %d contains unknown field %q", index, field)
			}
		}
	}
	return nil
}

// ReadBearerToken reads the Phase 3 credential-file contract. An empty path is
// the explicit Phase 2 compatibility boundary and leaves bearer auth disabled.
func ReadBearerToken(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Alertmanager bearer token file: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat Alertmanager bearer token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBearerFileBytes {
		return nil, errors.New("alertmanager bearer token file must be a non-empty bounded regular file")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxBearerFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Alertmanager bearer token file: %w", err)
	}
	if len(contents) > maxBearerFileBytes {
		return nil, errors.New("alertmanager bearer token file exceeds 4096 bytes")
	}
	token := []byte(strings.TrimSpace(string(contents)))
	if err := validateBearerToken(token); err != nil {
		return nil, err
	}
	return token, nil
}

func validateBearerToken(token []byte) error {
	if len(token) < 16 || len(token) > maxBearerFileBytes {
		return errors.New("alertmanager bearer token must contain 16..4096 bytes")
	}
	for _, value := range token {
		if value <= 0x20 || value >= 0x7f {
			return errors.New("alertmanager bearer token must contain visible ASCII without whitespace")
		}
	}
	return nil
}

type bearerVerifier struct {
	required bool
	digest   [sha256.Size]byte
}

func newBearerVerifier(token []byte) bearerVerifier {
	if len(token) == 0 {
		return bearerVerifier{}
	}
	return bearerVerifier{required: true, digest: sha256.Sum256(token)}
}

func (v bearerVerifier) verify(authorization string) bool {
	if !v.required {
		return true
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) || len(authorization) == len(prefix) {
		return false
	}
	digest := sha256.Sum256([]byte(authorization[len(prefix):]))
	return subtle.ConstantTimeCompare(v.digest[:], digest[:]) == 1
}

func normalizeTarget(target Target) (Target, error) {
	target.ClusterID = strings.TrimSpace(target.ClusterID)
	target.Environment = strings.TrimSpace(target.Environment)
	target.Namespace = strings.TrimSpace(target.Namespace)
	target.WorkloadKind = strings.TrimSpace(target.WorkloadKind)
	target.WorkloadName = strings.TrimSpace(target.WorkloadName)
	target.ServiceName = strings.TrimSpace(target.ServiceName)
	if target.ServiceName == "" {
		target.ServiceName = target.WorkloadName
	}
	matchLabels := make(map[string]string, len(target.MatchLabels))
	for key, value := range target.MatchLabels {
		key = strings.TrimSpace(key)
		if _, exists := matchLabels[key]; exists {
			return Target{}, errors.New("match_labels contains keys that collide after normalization")
		}
		matchLabels[key] = strings.TrimSpace(value)
	}
	target.MatchLabels = matchLabels
	return target, nil
}

func validateTarget(target Target) error {
	fields := map[string]string{
		"cluster_id": target.ClusterID, "environment": target.Environment,
		"namespace": target.Namespace, "workload_kind": target.WorkloadKind,
		"workload_name": target.WorkloadName, "service_name": target.ServiceName,
	}
	for name, value := range fields {
		if value == "" || strings.EqualFold(value, "unknown") {
			return fmt.Errorf("%s is required and cannot be unknown", name)
		}
		if len(value) > maxConfigFieldBytes || hasControl(value) {
			return fmt.Errorf("%s exceeds its safe bound", name)
		}
	}
	if len(target.MatchLabels) == 0 || len(target.MatchLabels) > maxMatchLabels {
		return fmt.Errorf("match_labels must contain 1..%d entries", maxMatchLabels)
	}
	for key, value := range target.MatchLabels {
		if key == "" || value == "" || len(key) > 128 || len(value) > maxConfigFieldBytes || hasControl(key) || hasControl(value) {
			return errors.New("match_labels contains an invalid key or value")
		}
	}
	return nil
}

func selectorKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&builder, "%d:%s=%d:%s;", len(key), key, len(labels[key]), labels[key])
	}
	return builder.String()
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}
