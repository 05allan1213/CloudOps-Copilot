// Package baseline contains the immutable, provider-neutral contract for a
// verified Deployment baseline. Provider adapters produce a Snapshot; the
// MySQL repository is the only component allowed to activate it.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/google/uuid"
)

const (
	DomainSchemaVersion      uint16 = 3
	BaselineSchemaVersion    uint16 = 1
	ObservationSchemaVersion uint16 = 1

	VerificationPolicyVersion = "golden-required-env-baseline/v1"
	MaxObservationBytes       = 16 * 1024
	MaxObservationCount       = 16
)

type ObservationType string

const (
	ObservationArgoRevision        ObservationType = "argocd_revision"
	ObservationKubernetesReadiness ObservationType = "kubernetes_readiness"
	ObservationAlertState          ObservationType = "alert_state"
	ObservationMetric              ObservationType = "metric"
	ObservationLog                 ObservationType = "log"
	ObservationTrace               ObservationType = "trace"
	ObservationConfigBlob          ObservationType = "config_blob"
)

var (
	hashPattern   = regexp.MustCompile("^[0-9a-f]{64}$")
	digestPattern = regexp.MustCompile("^sha256:[0-9a-f]{64}$")
)

// Target is the immutable identity of one deployment/configuration edge.
type Target struct {
	Cluster       string `json:"cluster"`
	Environment   string `json:"environment"`
	Namespace     string `json:"namespace"`
	WorkloadKind  string `json:"workload_kind"`
	WorkloadName  string `json:"workload_name"`
	ContainerName string `json:"container_name"`
	Repository    string `json:"repository"`
	BaseBranch    string `json:"base_branch"`
	TargetPath    string `json:"target_path"`
}

func (t Target) Validate() error {
	values := map[string]string{
		"cluster": t.Cluster, "environment": t.Environment, "namespace": t.Namespace,
		"workload kind": t.WorkloadKind, "workload name": t.WorkloadName,
		"container name": t.ContainerName, "repository": t.Repository,
		"base branch": t.BaseBranch, "target path": t.TargetPath,
	}
	for name, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 1024 {
			return fmt.Errorf("%w: baseline %s is required and bounded", change.ErrInvalidArgument, name)
		}
	}
	if !strings.EqualFold(strings.TrimSpace(t.WorkloadKind), "Deployment") {
		return fmt.Errorf("%w: baseline workload kind must be Deployment", change.ErrInvalidArgument)
	}
	repositoryParts := strings.Split(strings.Trim(strings.TrimSpace(t.Repository), "/"), "/")
	if len(repositoryParts) != 2 || repositoryParts[0] == "" || repositoryParts[1] == "" {
		return fmt.Errorf("%w: baseline repository must be owner/name", change.ErrInvalidArgument)
	}
	path := strings.TrimPrefix(strings.TrimSpace(t.TargetPath), "./")
	if path == "" || strings.Contains(path, "..") || strings.HasPrefix(path, "/") || change.SensitivePath(path, nil) {
		return fmt.Errorf("%w: baseline target path is outside the non-secret allowlist", change.ErrInvalidArgument)
	}
	return nil
}

func (t Target) Normalized() Target {
	t.Cluster = strings.TrimSpace(t.Cluster)
	t.Environment = strings.TrimSpace(t.Environment)
	t.Namespace = strings.TrimSpace(t.Namespace)
	t.WorkloadKind = "Deployment"
	t.WorkloadName = strings.TrimSpace(t.WorkloadName)
	t.ContainerName = strings.TrimSpace(t.ContainerName)
	t.Repository = strings.ToLower(strings.Trim(strings.TrimSpace(t.Repository), "/"))
	t.BaseBranch = strings.TrimSpace(t.BaseBranch)
	t.TargetPath = strings.TrimPrefix(strings.TrimSpace(t.TargetPath), "./")
	return t
}

func (t Target) IdentityHash() (string, error) {
	t = t.Normalized()
	if err := t.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("%w: baseline target hash", change.ErrInvalidArgument)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type Observation struct {
	Type           ObservationType
	SourceIdentity string
	ObservedJSON   json.RawMessage
	ContentHash    string
	DedupeKey      string
	ObservedAt     time.Time
}

func (o Observation) Validate(targetHash, configHash string) error {
	if !hashPattern.MatchString(targetHash) {
		return fmt.Errorf("%w: observation target hash", change.ErrInvalidArgument)
	}
	if !validObservationType(o.Type) || strings.TrimSpace(o.SourceIdentity) == "" || len(o.SourceIdentity) > 255 {
		return fmt.Errorf("%w: observation identity", change.ErrInvalidArgument)
	}
	if len(o.ObservedJSON) == 0 || len(o.ObservedJSON) > MaxObservationBytes || !json.Valid(o.ObservedJSON) {
		return fmt.Errorf("%w: observation payload", change.ErrInvalidArgument)
	}
	var object map[string]any
	if err := json.Unmarshal(o.ObservedJSON, &object); err != nil || object == nil {
		return fmt.Errorf("%w: observation payload must be a JSON object", change.ErrInvalidArgument)
	}
	if o.ObservedAt.IsZero() {
		return fmt.Errorf("%w: observation timestamp", change.ErrInvalidArgument)
	}
	if !hashPattern.MatchString(strings.ToLower(o.ContentHash)) || !hashPattern.MatchString(strings.ToLower(o.DedupeKey)) {
		return fmt.Errorf("%w: observation hash identity", change.ErrInvalidArgument)
	}
	if o.Type == ObservationConfigBlob && !strings.EqualFold(o.ContentHash, configHash) {
		return fmt.Errorf("%w: config observation is not bound to config_hash", change.ErrInvalidArgument)
	}
	if o.Type != ObservationConfigBlob {
		sum := sha256.Sum256(o.ObservedJSON)
		if o.ContentHash != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("%w: observation content hash mismatch", change.ErrInvalidArgument)
		}
	}
	return nil
}

type Snapshot struct {
	Target                    Target
	TargetIdentityHash        string
	SourceRevision            string
	ImageDigest               string
	GitOpsRevision            string
	ConfigHash                string
	VerificationPolicyVersion string
	VerificationHash          string
	VerifiedAt                time.Time
	Observations              []Observation
}

func (s *Snapshot) Finalize() error {
	if s == nil {
		return fmt.Errorf("%w: nil baseline snapshot", change.ErrInvalidArgument)
	}
	s.Target = s.Target.Normalized()
	targetHash, err := s.Target.IdentityHash()
	if err != nil {
		return err
	}
	if s.TargetIdentityHash != "" && !strings.EqualFold(strings.TrimSpace(s.TargetIdentityHash), targetHash) {
		return fmt.Errorf("%w: baseline target identity hash mismatch", change.ErrInvalidArgument)
	}
	s.TargetIdentityHash = targetHash
	s.SourceRevision = strings.ToLower(strings.TrimSpace(s.SourceRevision))
	s.GitOpsRevision = strings.ToLower(strings.TrimSpace(s.GitOpsRevision))
	s.ImageDigest = strings.ToLower(strings.TrimSpace(s.ImageDigest))
	s.ConfigHash = strings.ToLower(strings.TrimSpace(s.ConfigHash))
	s.VerificationPolicyVersion = strings.TrimSpace(s.VerificationPolicyVersion)
	if !change.ValidExactGitObjectID(s.SourceRevision) || !change.ValidExactGitObjectID(s.GitOpsRevision) ||
		!digestPattern.MatchString(s.ImageDigest) || !hashPattern.MatchString(s.ConfigHash) {
		return fmt.Errorf("%w: baseline revision or digest identity", change.ErrInvalidArgument)
	}
	if s.VerificationPolicyVersion == "" {
		s.VerificationPolicyVersion = VerificationPolicyVersion
	}
	if len(s.VerificationPolicyVersion) > 64 || s.VerifiedAt.IsZero() {
		return fmt.Errorf("%w: baseline verification envelope", change.ErrInvalidArgument)
	}
	if len(s.Observations) == 0 || len(s.Observations) > MaxObservationCount {
		return fmt.Errorf("%w: baseline observation count", change.ErrInvalidArgument)
	}
	seenTypes := map[ObservationType]struct{}{}
	seenDedupe := map[string]struct{}{}
	for index := range s.Observations {
		observation := &s.Observations[index]
		observation.ContentHash = strings.ToLower(strings.TrimSpace(observation.ContentHash))
		observation.DedupeKey = strings.ToLower(strings.TrimSpace(observation.DedupeKey))
		if observation.ContentHash == "" {
			sum := sha256.Sum256(observation.ObservedJSON)
			observation.ContentHash = hex.EncodeToString(sum[:])
		}
		if observation.DedupeKey == "" {
			observation.DedupeKey = hashParts(targetHash, string(observation.Type), observation.SourceIdentity, observation.ContentHash)
		}
		if err := observation.Validate(targetHash, s.ConfigHash); err != nil {
			return err
		}
		if _, ok := seenTypes[observation.Type]; ok {
			return fmt.Errorf("%w: duplicate observation type %s", change.ErrInvalidArgument, observation.Type)
		}
		if _, ok := seenDedupe[observation.DedupeKey]; ok {
			return fmt.Errorf("%w: duplicate observation dedupe key", change.ErrInvalidArgument)
		}
		seenTypes[observation.Type], seenDedupe[observation.DedupeKey] = struct{}{}, struct{}{}
	}
	for _, required := range []ObservationType{
		ObservationArgoRevision, ObservationKubernetesReadiness, ObservationAlertState,
		ObservationMetric, ObservationLog, ObservationTrace, ObservationConfigBlob,
	} {
		if _, ok := seenTypes[required]; !ok {
			return fmt.Errorf("%w: required observation %s is missing", change.ErrInvalidArgument, required)
		}
	}
	sort.Slice(s.Observations, func(i, j int) bool { return s.Observations[i].Type < s.Observations[j].Type })
	s.VerificationHash = s.computeVerificationHash()
	return nil
}

func (s Snapshot) Validate() error {
	copy := s
	expected := strings.ToLower(strings.TrimSpace(copy.VerificationHash))
	if err := copy.Finalize(); err != nil {
		return err
	}
	if expected != "" && expected != copy.VerificationHash {
		return fmt.Errorf("%w: baseline verification hash mismatch", change.ErrInvalidArgument)
	}
	return nil
}

func (s Snapshot) PublicID() string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("cloudops-baseline\x00"+s.TargetIdentityHash+"\x00"+s.GitOpsRevision+"\x00"+s.ConfigHash)).String()
}

func (s Snapshot) computeVerificationHash() string {
	type observationHash struct {
		Type, Source, Content, Dedupe, Payload string
		At                                     time.Time
	}
	observations := make([]observationHash, 0, len(s.Observations))
	for _, item := range s.Observations {
		payloadHash := sha256.Sum256(item.ObservedJSON)
		observations = append(observations, observationHash{
			Type: string(item.Type), Source: item.SourceIdentity, Content: item.ContentHash,
			Dedupe: item.DedupeKey, Payload: hex.EncodeToString(payloadHash[:]), At: item.ObservedAt.UTC(),
		})
	}
	payload, _ := json.Marshal(struct {
		Target       string
		Source       string
		Image        string
		GitOps       string
		Config       string
		Policy       string
		Observations []observationHash
	}{
		Target: s.TargetIdentityHash, Source: s.SourceRevision, Image: s.ImageDigest,
		GitOps: s.GitOpsRevision, Config: s.ConfigHash, Policy: s.VerificationPolicyVersion,
		Observations: observations,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func hashParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func validObservationType(value ObservationType) bool {
	switch value {
	case ObservationArgoRevision, ObservationKubernetesReadiness, ObservationAlertState,
		ObservationMetric, ObservationLog, ObservationTrace, ObservationConfigBlob:
		return true
	default:
		return false
	}
}
