package remediation

import (
	"errors"
	"strings"
	"testing"
)

const deploymentYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: api
          image: registry.example/acme/api:v2
        - name: sidecar
          image: registry.example/sidecar:v1
`

func TestPlannerSchemaRejectsInjectedAuthority(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	valid := `{"operation_type":"set_replicas","target_resource":{"api_version":"apps/v1","kind":"Deployment","namespace":"prod","name":"api"},"proposed_value":{"replicas":4},"evidence_ids":["` + id + `"]}`
	if _, err := DecodePlannerOutput([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, injected := range []string{
		strings.TrimSuffix(valid, "}") + `,"repository":"evil/repo"}`,
		strings.Replace(valid, `"proposed_value":{"replicas":4}`, `"proposed_value":{"replicas":4,"workflow":"deploy.yml"}`, 1),
		strings.Replace(valid, `"operation_type":"set_replicas"`, `"operation_type":"delete_resource"`, 1),
	} {
		if _, err := DecodePlannerOutput([]byte(injected)); err == nil {
			t.Fatalf("injected authority accepted: %s", injected)
		}
	}
}

func TestYAMLASTMutatesOnlyAllowlistedTypedField(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	target := TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api", Container: "api"}
	result, err := RenderPatch([]byte(deploymentYAML), OperationRollbackImage, Parameters{Target: target, ProposedValue: ProposedValue{ImageDigest: digest}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Content), "registry.example/acme/api@"+digest) || !strings.Contains(string(result.Content), "registry.example/sidecar:v1") || result.FileCount != 1 || len(result.PatchHash) != 64 {
		t.Fatalf("unexpected rendered patch: %s", result.Content)
	}
	second, err := RenderPatch([]byte(deploymentYAML), OperationRollbackImage, Parameters{Target: target, ProposedValue: ProposedValue{ImageDigest: digest}})
	if err != nil || second.PatchHash != result.PatchHash || second.Diff != result.Diff {
		t.Fatal("patch rendering is not deterministic")
	}
}

func TestPolicyFailsClosedForSecurityBoundaries(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	replicas := 4
	patch, err := RenderPatch([]byte(deploymentYAML), OperationSetReplicas, Parameters{Target: TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api"}, ProposedValue: ProposedValue{Replicas: &replicas}})
	if err != nil {
		t.Fatal(err)
	}
	base := PolicyConfig{AllowedRepositories: []string{"acme/gitops"}, AllowedPaths: []string{"apps/api.yaml"}, AllowedOperations: []OperationType{OperationRollbackImage, OperationSetReplicas}, MaxPatchBytes: 4096, MaxFiles: 1, MaxRisk: RiskMedium, MinReplicas: 1, MaxReplicas: 5}
	evidence := []EvidenceFact{{PublicID: "e", IncidentID: 7, Valid: true, ConfirmedChange: true, RegistryVerified: true, DeployedDigests: []string{digest}}}
	decision, err := EvaluatePolicy(base, PolicyInput{IncidentID: 7, Repository: "acme/gitops", Path: "apps/api.yaml", Operation: OperationSetReplicas, Parameters: Parameters{Target: TargetResource{Namespace: "prod", Kind: "Deployment", Name: "api"}, ProposedValue: ProposedValue{Replicas: &replicas}}, Evidence: evidence, Patch: patch})
	if err != nil || !decision.Allowed || len(decision.PolicySnapshotHash) != 64 {
		t.Fatalf("allowed decision=%+v err=%v", decision, err)
	}
	base.HPATargets = []string{"prod/Deployment/api"}
	decision, _ = EvaluatePolicy(base, PolicyInput{IncidentID: 7, Repository: "acme/gitops", Path: "apps/api.yaml", Operation: OperationSetReplicas, Parameters: Parameters{Target: TargetResource{Namespace: "prod", Kind: "Deployment", Name: "api"}, ProposedValue: ProposedValue{Replicas: &replicas}}, Evidence: evidence, Patch: patch})
	if decision.Allowed || !contains(decision.ReasonCodes, ReasonHPAControlled) {
		t.Fatalf("HPA target allowed: %+v", decision)
	}
	decision, _ = EvaluatePolicy(base, PolicyInput{IncidentID: 7, Repository: "acme/gitops", Path: ".github/workflows/deploy.yml", Operation: OperationSetReplicas, Parameters: Parameters{Target: TargetResource{Namespace: "prod", Kind: "Deployment", Name: "api"}, ProposedValue: ProposedValue{Replicas: &replicas}}, Evidence: evidence, Patch: patch})
	if decision.Allowed || !contains(decision.ReasonCodes, ReasonSensitivePath) {
		t.Fatalf("workflow path allowed: %+v", decision)
	}
}

func TestPlanStateMachineStopsAtCI(t *testing.T) {
	plan := &RemediationPlan{Status: PlanDraft, RowVersion: 1}
	for _, next := range []PlanStatus{PlanAwaitingApproval, PlanApproved, PlanDeliveryPending, PlanDelivering, PlanPRCreated, PlanCIPending, PlanCIPassed} {
		if err := plan.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	if err := plan.Transition(PlanApproved); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("terminal CI state unexpectedly transitioned")
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
