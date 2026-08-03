package remediation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pmezard/go-difflib/difflib"
)

const LocalScenarioRestorePolicyVersion = "local-scenario-restore-required-env/v1"

type LocalScenarioCompileRequest struct {
	IncidentPublicID    string
	IncidentID          uint64
	CycleNo             uint64
	IncidentVersion     uint64
	CreatedByAgentRunID string
	DiagnosisHash       string
	ScenarioID          string
	ClusterID           string
	Environment         string
	ResourceUID         string
	ResourceVersion     string
	Generation          int64
	Target              TargetResource
	EnvNames            []string
	Evidence            []EvidenceBinding
	CreatedAt           time.Time
	ExpiresAt           time.Time
	PlanVersion         int
}

type localScenarioPolicy struct {
	Version       string         `json:"version"`
	SourceType    PlanSourceType `json:"source_type"`
	ScenarioID    string         `json:"scenario_id"`
	ClusterID     string         `json:"cluster_id"`
	Environment   string         `json:"environment"`
	Target        TargetResource `json:"target"`
	EnvKey        string         `json:"env_key"`
	ProposedValue string         `json:"proposed_value"`
	DecisionMode  string         `json:"decision_mode"`
}

type localScenarioState struct {
	SchemaVersion   int            `json:"schema_version"`
	SourceType      PlanSourceType `json:"source_type"`
	ScenarioID      string         `json:"scenario_id"`
	ClusterID       string         `json:"cluster_id"`
	Environment     string         `json:"environment"`
	ResourceUID     string         `json:"resource_uid"`
	ResourceVersion string         `json:"resource_version"`
	Generation      int64          `json:"generation"`
	Target          TargetResource `json:"target"`
	EnvNames        []string       `json:"env_names"`
}

type localScenarioPatch struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Name string `json:"name"`
					Env  []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"env"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

type localScenarioChangeManifest struct {
	SourceType          PlanSourceType  `json:"source_type"`
	PatchType           string          `json:"patch_type"`
	TargetLocator       string          `json:"target_locator"`
	RuntimeSnapshotHash string          `json:"runtime_snapshot_hash"`
	PatchHash           string          `json:"patch_hash"`
	Patch               json.RawMessage `json:"patch"`
	PostImageHash       string          `json:"post_image_hash"`
}

func CompileLocalScenarioRestoreRequiredEnv(request LocalScenarioCompileRequest) (RemediationPlan, error) {
	if err := validateLocalScenarioCompileRequest(request); err != nil {
		return RemediationPlan{}, err
	}
	envNames := stableLocalEnvNames(request.EnvNames)
	current := localScenarioState{
		SchemaVersion: 1, SourceType: PlanSourceLocalScenario, ScenarioID: request.ScenarioID,
		ClusterID: request.ClusterID, Environment: request.Environment, ResourceUID: request.ResourceUID,
		ResourceVersion: request.ResourceVersion, Generation: request.Generation, Target: request.Target, EnvNames: envNames,
	}
	desired := current
	desired.EnvNames = stableLocalEnvNames(append(append([]string(nil), envNames...), "REQUIRED_ENV"))
	currentJSON, err := canonicalJSON(current)
	if err != nil {
		return RemediationPlan{}, err
	}
	desiredJSON, err := canonicalJSON(desired)
	if err != nil {
		return RemediationPlan{}, err
	}
	patch := localScenarioPatch{APIVersion: "apps/v1", Kind: "Deployment"}
	patch.Metadata.Name, patch.Metadata.Namespace = request.Target.Name, request.Target.Namespace
	patch.Spec.Template.Spec.Containers = make([]struct {
		Name string `json:"name"`
		Env  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"env"`
	}, 1)
	patch.Spec.Template.Spec.Containers[0].Name = request.Target.Container
	patch.Spec.Template.Spec.Containers[0].Env = make([]struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}, 1)
	patch.Spec.Template.Spec.Containers[0].Env[0].Name = "REQUIRED_ENV"
	patch.Spec.Template.Spec.Containers[0].Env[0].Value = "present"
	patchJSON, err := canonicalJSON(patch)
	if err != nil {
		return RemediationPlan{}, err
	}
	targetPath := fmt.Sprintf("kubernetes://%s/apps/v1/namespaces/%s/deployments/%s", request.ClusterID, request.Target.Namespace, request.Target.Name)
	manifestJSON, err := canonicalJSON(localScenarioChangeManifest{
		SourceType: PlanSourceLocalScenario, PatchType: "application/strategic-merge-patch+json",
		TargetLocator: targetPath, RuntimeSnapshotHash: hashBytes(currentJSON), PatchHash: hashBytes(patchJSON),
		Patch: patchJSON, PostImageHash: hashBytes(desiredJSON),
	})
	if err != nil {
		return RemediationPlan{}, err
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(string(currentJSON) + "\n"), B: difflib.SplitLines(string(desiredJSON) + "\n"),
		FromFile: "runtime/current", ToFile: "scenario/proposed", Context: 3,
	})
	if err != nil || diff == "" || len(diff) > MaxPlanDiffBytes {
		return RemediationPlan{}, fmt.Errorf("%w: local Scenario diff generation failed", ErrInvalidArgument)
	}
	policyJSON, err := canonicalJSON(localScenarioPolicy{
		Version: LocalScenarioRestorePolicyVersion, SourceType: PlanSourceLocalScenario,
		ScenarioID: request.ScenarioID, ClusterID: request.ClusterID, Environment: request.Environment,
		Target: request.Target, EnvKey: "REQUIRED_ENV", ProposedValue: "present", DecisionMode: "reject_only",
	})
	if err != nil {
		return RemediationPlan{}, err
	}
	verificationJSON, err := canonicalJSON(map[string]any{
		"schema_version": 1, "mode": "decision_only", "approve_allowed": false,
		"required_decision": "rejected", "scenario_id": request.ScenarioID,
	})
	if err != nil {
		return RemediationPlan{}, err
	}
	evidence := append([]EvidenceBinding(nil), request.Evidence...)
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].ID == evidence[j].ID {
			return evidence[i].ContentHash < evidence[j].ContentHash
		}
		return evidence[i].ID < evidence[j].ID
	})
	evidenceSetHash, err := lengthPrefixedHash(evidenceBytes(evidence))
	if err != nil {
		return RemediationPlan{}, err
	}
	createdAt, expiresAt := normalizeTime(request.CreatedAt), normalizeTime(request.ExpiresAt)
	plan := RemediationPlan{
		PublicID: uuid.NewString(), IncidentID: request.IncidentID, IncidentPublicID: request.IncidentPublicID,
		CycleNo: request.CycleNo, IncidentVersion: request.IncidentVersion, PlanVersion: request.PlanVersion,
		CreatedByAgentRunID: request.CreatedByAgentRunID, DiagnosisHash: strings.ToLower(request.DiagnosisHash),
		Status: PlanAwaitingApproval, OperationType: OperationRestoreRequiredEnv, SourceType: PlanSourceLocalScenario,
		RuntimeBaseHash: hashBytes(currentJSON), TargetPath: targetPath,
		Parameters: Parameters{Target: request.Target}, EvidenceReferences: evidenceIDs(evidence), RiskLevel: RiskLow,
		PolicySnapshotHash: hashBytes(policyJSON), ExpectedBeforeHash: hashBytes(currentJSON),
		ExpectedPostImageHash: hashBytes(desiredJSON), ProposedPatchHash: hashBytes(manifestJSON),
		PatchSummary:   fmt.Sprintf("propose REQUIRED_ENV=present for bounded local Scenario Deployment %s/%s", request.Target.Namespace, request.Target.Name),
		RollbackPlan:   "This local Scenario Plan is not executable and accepts rejection only.",
		ValidationPlan: "Persist and reload the exact Reject Decision; no Kubernetes, GitHub, GitOps, or ArgoCD write is permitted.",
		RowVersion:     1, CreatedAt: createdAt, UpdatedAt: createdAt, ExpiresAt: expiresAt,
		HashSchemaVersion: HashSchemaVersion, PlanContentSchemaVersion: LocalScenarioPlanSchemaVersion,
		TargetFieldRef:          "spec.template.spec.containers[name=" + request.Target.Container + "].env[name=REQUIRED_ENV]",
		CanonicalChangeManifest: manifestJSON, BoundedDiff: diff, PostImage: desiredJSON,
		PolicyVersion: LocalScenarioRestorePolicyVersion, PolicySnapshot: policyJSON,
		VerificationPlan: verificationJSON, VerificationPlanHash: hashBytes(verificationJSON),
		EvidenceBindings: evidence, EvidenceSetHash: evidenceSetHash,
	}
	canonicalHash, err := CanonicalPlanHash(plan)
	if err != nil {
		return RemediationPlan{}, err
	}
	plan.CanonicalPlanHash, plan.PlanHash = canonicalHash, canonicalHash
	return plan, ValidatePlan(plan)
}

func validateLocalScenarioCompileRequest(request LocalScenarioCompileRequest) error {
	if _, err := uuid.Parse(request.IncidentPublicID); err != nil || request.IncidentID == 0 || request.CycleNo == 0 ||
		request.IncidentVersion == 0 || request.PlanVersion <= 0 {
		return fmt.Errorf("%w: local Scenario Incident identity is invalid", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(request.CreatedByAgentRunID); err != nil || len(request.DiagnosisHash) != 64 || !isLowerHex(request.DiagnosisHash) {
		return fmt.Errorf("%w: local Scenario diagnosis identity is invalid", ErrInvalidArgument)
	}
	if !strings.HasPrefix(request.ScenarioID, "scenario-") || request.ClusterID != "cloudops-local" || request.Environment != "local" ||
		request.Target.APIVersion != "apps/v1" || request.Target.Kind != "Deployment" || request.Target.Namespace != "demo" ||
		request.Target.Name != "cloudops-scenario-fault" || request.Target.Container != "scenario" ||
		strings.TrimSpace(request.ResourceUID) == "" || strings.TrimSpace(request.ResourceVersion) == "" || request.Generation <= 0 {
		return fmt.Errorf("%w: local Scenario target is outside the exact allowlist", ErrInvalidArgument)
	}
	if request.CreatedAt.IsZero() || request.ExpiresAt.IsZero() || !request.ExpiresAt.After(request.CreatedAt) ||
		len(request.Evidence) == 0 || len(request.Evidence) > MaxEvidenceBindings {
		return fmt.Errorf("%w: local Scenario Plan time or Evidence is invalid", ErrInvalidArgument)
	}
	for _, name := range request.EnvNames {
		if strings.TrimSpace(name) == "REQUIRED_ENV" {
			return fmt.Errorf("%w: REQUIRED_ENV is already present", ErrDrift)
		}
	}
	for _, binding := range request.Evidence {
		if _, err := uuid.Parse(binding.ID); err != nil || len(binding.ContentHash) != 64 || !isLowerHex(binding.ContentHash) {
			return fmt.Errorf("%w: local Scenario Evidence binding is invalid", ErrInvalidArgument)
		}
	}
	return nil
}

func validateLocalScenarioManifest(plan RemediationPlan) error {
	var manifest localScenarioChangeManifest
	if err := json.Unmarshal(plan.CanonicalChangeManifest, &manifest); err != nil ||
		manifest.SourceType != PlanSourceLocalScenario || manifest.PatchType != "application/strategic-merge-patch+json" ||
		manifest.TargetLocator != plan.TargetPath || manifest.RuntimeSnapshotHash != plan.RuntimeBaseHash ||
		manifest.PostImageHash != plan.ExpectedPostImageHash || len(manifest.PatchHash) != 64 || !isLowerHex(manifest.PatchHash) ||
		len(manifest.Patch) == 0 || !json.Valid(manifest.Patch) || hashBytes(manifest.Patch) != manifest.PatchHash {
		return fmt.Errorf("%w: local Scenario change manifest binding", ErrInvalidArgument)
	}
	return nil
}

func stableLocalEnvNames(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
