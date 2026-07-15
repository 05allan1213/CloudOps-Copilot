package remediation

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

type PolicyConfig struct {
	AllowedRepositories []string
	AllowedPaths        []string
	AllowedOperations   []OperationType
	MaxPatchBytes       int
	MaxFiles            int
	MaxRisk             RiskLevel
	MinReplicas         int
	MaxReplicas         int
	HPATargets          []string
}

type PolicyInput struct {
	IncidentID uint64
	Repository string
	Path       string
	Operation  OperationType
	Parameters Parameters
	Evidence   []EvidenceFact
	Patch      PatchResult
}

type PolicyDecision struct {
	Allowed            bool      `json:"allowed"`
	RiskLevel          RiskLevel `json:"risk_level"`
	ReasonCodes        []string  `json:"stable_reason_codes"`
	PolicySnapshotHash string    `json:"policy_snapshot_hash"`
}

const (
	ReasonAllowed            = "allowed"
	ReasonEvidenceInvalid    = "evidence_invalid"
	ReasonChangeUnconfirmed  = "change_unconfirmed"
	ReasonRegistryUnverified = "registry_unverified"
	ReasonRepositoryDenied   = "repository_not_allowlisted"
	ReasonPathDenied         = "path_not_allowlisted"
	ReasonSensitivePath      = "sensitive_path_denied"
	ReasonOperationDenied    = "operation_not_allowlisted"
	ReasonPatchTooLarge      = "patch_too_large"
	ReasonFileLimit          = "file_limit_exceeded"
	ReasonRiskExceeded       = "risk_exceeded"
	ReasonDigestNotDeployed  = "digest_not_deployed"
	ReasonReplicaBounds      = "replica_bounds"
	ReasonHPAControlled      = "hpa_controlled"
	ReasonHashInvalid        = "hash_invalid"
)

func EvaluatePolicy(cfg PolicyConfig, input PolicyInput) (PolicyDecision, error) {
	decision := PolicyDecision{RiskLevel: riskFor(input.Operation)}
	add := func(code string) { decision.ReasonCodes = append(decision.ReasonCodes, code) }
	if !slices.Contains(cfg.AllowedRepositories, input.Repository) {
		add(ReasonRepositoryDenied)
	}
	cleanPath := path.Clean(strings.TrimSpace(input.Path))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "../") || !slices.Contains(cfg.AllowedPaths, cleanPath) {
		add(ReasonPathDenied)
	}
	if sensitiveGitOpsPath(cleanPath) {
		add(ReasonSensitivePath)
	}
	if !slices.Contains(cfg.AllowedOperations, input.Operation) {
		add(ReasonOperationDenied)
	}
	if len(input.Patch.Diff) == 0 || len(input.Patch.PatchHash) != 64 || len(input.Patch.BeforeHash) != 64 {
		add(ReasonHashInvalid)
	}
	if cfg.MaxPatchBytes <= 0 || len(input.Patch.Diff) > cfg.MaxPatchBytes {
		add(ReasonPatchTooLarge)
	}
	if cfg.MaxFiles != 1 || input.Patch.FileCount != 1 {
		add(ReasonFileLimit)
	}
	if riskRank(decision.RiskLevel) > riskRank(cfg.MaxRisk) {
		add(ReasonRiskExceeded)
	}
	deployed := make(map[string]struct{})
	if len(input.Evidence) == 0 {
		add(ReasonEvidenceInvalid)
	}
	for _, evidence := range input.Evidence {
		if evidence.IncidentID != input.IncidentID || !evidence.Valid || evidence.Truncated {
			add(ReasonEvidenceInvalid)
		}
		if !evidence.ConfirmedChange {
			add(ReasonChangeUnconfirmed)
		}
		if !evidence.RegistryVerified {
			add(ReasonRegistryUnverified)
		}
		for _, digest := range evidence.DeployedDigests {
			deployed[strings.ToLower(digest)] = struct{}{}
		}
	}
	if input.Operation == OperationRollbackImage {
		if _, ok := deployed[strings.ToLower(input.Parameters.ProposedValue.ImageDigest)]; !ok {
			add(ReasonDigestNotDeployed)
		}
	}
	if input.Operation == OperationSetReplicas {
		value := input.Parameters.ProposedValue.Replicas
		if value == nil || *value < cfg.MinReplicas || *value > cfg.MaxReplicas || cfg.MinReplicas < 0 || cfg.MaxReplicas < cfg.MinReplicas {
			add(ReasonReplicaBounds)
		}
		target := input.Parameters.Target.Namespace + "/" + input.Parameters.Target.Kind + "/" + input.Parameters.Target.Name
		if slices.Contains(cfg.HPATargets, target) {
			add(ReasonHPAControlled)
		}
	}
	slices.Sort(decision.ReasonCodes)
	decision.ReasonCodes = slices.Compact(decision.ReasonCodes)
	decision.Allowed = len(decision.ReasonCodes) == 0
	if decision.Allowed {
		decision.ReasonCodes = []string{ReasonAllowed}
	}
	snapshot := struct {
		Config PolicyConfig
		Result []string
		Risk   RiskLevel
	}{cfg, decision.ReasonCodes, decision.RiskLevel}
	hash, err := CanonicalHash(snapshot)
	if err != nil {
		return PolicyDecision{}, fmt.Errorf("%w: policy snapshot", err)
	}
	decision.PolicySnapshotHash = hash
	return decision, nil
}

func sensitiveGitOpsPath(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, ".github/") || strings.Contains(lower, "/.github/") || strings.Contains(lower, "workflow") || strings.Contains(lower, "secret") || strings.Contains(lower, "clusterrole") || strings.Contains(lower, "rolebinding") || strings.Contains(lower, "serviceaccount")
}

func riskFor(operation OperationType) RiskLevel {
	if operation == OperationRollbackImage {
		return RiskMedium
	}
	return RiskLow
}

func riskRank(risk RiskLevel) int {
	switch risk {
	case RiskLow:
		return 1
	case RiskMedium:
		return 2
	case RiskHigh:
		return 3
	default:
		return 0
	}
}
