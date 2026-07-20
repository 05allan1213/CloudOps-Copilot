package remediation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const currentRequiredEnvYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: cloudops-demo
spec:
  template:
    spec:
      containers:
        - name: demo
          image: example/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          env:
            - name: OTHER_ENV
              value: keep
`

const baselineRequiredEnvYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: cloudops-demo
spec:
  template:
    spec:
      containers:
        - name: demo
          image: example/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          env:
            - name: OTHER_ENV
              value: keep
            - name: REQUIRED_ENV
              value: healthy
`

func TestV3RemediationHintRejectsModelSuppliedAuthority(t *testing.T) {
	baselineID := "11111111-1111-4111-8111-111111111111"
	supportID := "22222222-2222-4222-8222-222222222222"
	valid := `{"operation_hint":"restore_required_env","target_field_ref":"deployment/demo/container/demo/env/REQUIRED_ENV","last_known_good_evidence_id":"` + baselineID + `","supporting_evidence_ids":["` + supportID + `"]}`
	hint, err := DecodeV3RemediationHint([]byte(valid))
	if err != nil || hint.OperationHint != OperationRestoreRequiredEnv {
		t.Fatalf("hint=%+v err=%v", hint, err)
	}
	for _, injected := range []string{
		strings.TrimSuffix(valid, "}") + `,"value":"attacker-controlled"}`,
		strings.TrimSuffix(valid, "}") + `,"repository":"other/repo"}`,
		strings.Replace(valid, "restore_required_env", "rollback_image", 1),
	} {
		if _, err := DecodeV3RemediationHint([]byte(injected)); err == nil {
			t.Fatalf("model authority injection accepted: %s", injected)
		}
	}
}

func TestCompileRestoreRequiredEnvBindsCompletePlan(t *testing.T) {
	request := validRestoreRequest()
	plan, err := CompileRestoreRequiredEnv(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OperationType != OperationRestoreRequiredEnv || plan.Status != PlanAwaitingApproval || len(plan.CanonicalPlanHash) != 64 || plan.PlanHash != plan.CanonicalPlanHash {
		t.Fatalf("unexpected plan identity: %+v", plan)
	}
	if !strings.Contains(string(plan.PostImage), "name: REQUIRED_ENV") || !strings.Contains(string(plan.PostImage), "value: healthy") || !strings.Contains(plan.BoundedDiff, "+            - name: REQUIRED_ENV") {
		t.Fatalf("post image or full diff missing restored node:\n%s\n%s", plan.PostImage, plan.BoundedDiff)
	}
	if plan.DomainSchemaVersion != V3DomainSchemaVersion || plan.TargetBaseBranch != "main" || plan.IncidentVersion != 9 || plan.PlanContentSchemaVersion != V3PlanContentSchemaVersion || plan.TargetFieldRef != "spec.template.spec.containers[name=demo].env[name=REQUIRED_ENV]" || len(plan.ExpectedPostImageHash) != 64 || len(plan.ProposedPatchHash) != 64 || len(plan.EvidenceSetHash) != 64 || len(plan.VerificationPlanHash) != 64 {
		t.Fatalf("plan bindings missing: %+v", plan)
	}
	if err := ValidateV3Plan(plan); err != nil {
		t.Fatalf("compiled Plan failed its persistence contract: %v", err)
	}

	second := request
	second.Evidence = append([]EvidenceBinding(nil), request.Evidence...)
	second.Evidence[0], second.Evidence[1] = second.Evidence[1], second.Evidence[0]
	secondPlan, err := CompileRestoreRequiredEnv(second)
	if err != nil || secondPlan.CanonicalPlanHash != plan.CanonicalPlanHash {
		t.Fatalf("evidence ordering changed canonical hash: first=%s second=%s err=%v", plan.CanonicalPlanHash, secondPlan.CanonicalPlanHash, err)
	}
	changed := plan
	changed.BoundedDiff += "\n"
	changedHash, err := CanonicalV3PlanHash(changed)
	if err != nil || changedHash == plan.CanonicalPlanHash {
		t.Fatal("exact diff bytes are not bound by canonical plan hash")
	}
	changed = plan
	changed.PlanContentSchemaVersion++
	changedHash, err = CanonicalV3PlanHash(changed)
	if err != nil || changedHash == plan.CanonicalPlanHash {
		t.Fatal("plan content schema version is not bound by canonical plan hash")
	}
}

func TestCompileRestoreRequiredEnvUsesMySQLTimePrecision(t *testing.T) {
	request := validRestoreRequest()
	request.CreatedAt = request.CreatedAt.Add(987654321 * time.Nanosecond)
	request.ExpiresAt = request.CreatedAt.Add(30*time.Minute + 123*time.Nanosecond)
	plan, err := CompileRestoreRequiredEnv(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CreatedAt.Nanosecond()%1000 != 0 || plan.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatalf("Plan times exceed MySQL DATETIME(6) precision: created=%s expires=%s", plan.CreatedAt, plan.ExpiresAt)
	}
	if err := ValidateV3Plan(plan); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreEnvRejectsSecretExistingAndUnboundPolicy(t *testing.T) {
	target := validRestoreRequest().Target
	secretBaseline := strings.Replace(baselineRequiredEnvYAML, "value: healthy", "valueFrom:\n                secretKeyRef:\n                  name: demo\n                  key: required", 1)
	if _, err := RenderRestoreRequiredEnv([]byte(currentRequiredEnvYAML), []byte(secretBaseline), target, "REQUIRED_ENV"); err == nil {
		t.Fatal("Secret-backed baseline env was accepted")
	}
	if _, err := RenderRestoreRequiredEnv([]byte(baselineRequiredEnvYAML), []byte(baselineRequiredEnvYAML), target, "REQUIRED_ENV"); err == nil {
		t.Fatal("current env overwrite was accepted")
	}
	request := validRestoreRequest()
	request.Policy.AllowedPath = ".github/workflows/attack.yml"
	if _, err := CompileRestoreRequiredEnv(request); err == nil {
		t.Fatal("policy target mismatch was accepted")
	}
	request = validRestoreRequest()
	request.BaselineIsAncestor = false
	if _, err := CompileRestoreRequiredEnv(request); err == nil {
		t.Fatal("unverified baseline ancestry was accepted")
	}
}

func TestV3ApprovalBindsEveryImmutableHash(t *testing.T) {
	plan, err := CompileRestoreRequiredEnv(validRestoreRequest())
	if err != nil {
		t.Fatal(err)
	}
	now := plan.CreatedAt.Add(10 * time.Minute)
	approval, err := NewV3Approval(plan, "github", "operator-login", "operator", "reviewed exact diff", "request-1", now, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateV3ApprovalBinding(plan, approval, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if approval.DomainSchemaVersion != V3DomainSchemaVersion || approval.DecisionSchemaVersion != V3DecisionSchemaVersion || approval.IncidentID != plan.IncidentID || approval.CycleNo != plan.CycleNo || approval.PlanVersion != plan.PlanVersion {
		t.Fatalf("decision ownership or schema binding missing: %+v", approval)
	}
	approval.ApprovedTreeHash = strings.Repeat("f", 40)
	if err := ValidateV3ApprovalBinding(plan, approval, now.Add(time.Minute)); !errors.Is(err, ErrApprovalMismatch) {
		t.Fatalf("mutated approval error=%v", err)
	}
	approval.ApprovedTreeHash = plan.ExpectedTreeHash
	if err := ValidateV3ApprovalBinding(plan, approval, approval.ExpiresAt); !errors.Is(err, ErrApprovalMismatch) {
		t.Fatalf("expired approval error=%v", err)
	}
}

func validRestoreRequest() RestoreEnvCompileRequest {
	created := time.Date(2026, 7, 19, 3, 0, 0, 0, time.UTC)
	policy := RestoreEnvPolicy{
		Version: "restore-required-env-policy/v1", Repository: "acme/cloudops-gitops-demo", BaseBranch: "main",
		AllowedPath: "apps/demo/deployment.yaml", APIVersion: "apps/v1", Namespace: "cloudops-demo",
		Workload: "demo", Container: "demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: MaxV3PlanDiffBytes, MaxPostImageBytes: MaxV3PostImageBytes, VerificationVersion: "golden-required-env/v1",
	}
	return RestoreEnvCompileRequest{
		IncidentPublicID: "33333333-3333-4333-8333-333333333333", IncidentID: 7, CycleNo: 2, IncidentVersion: 9,
		CreatedByAgentRunID: "44444444-4444-4444-8444-444444444444", DiagnosisHash: strings.Repeat("d", 64),
		Repository: policy.Repository, BaseBranch: policy.BaseBranch, BaseRevision: strings.Repeat("a", 40),
		LastKnownGoodRevision: strings.Repeat("b", 40), TargetPath: policy.AllowedPath,
		BaseBlobSHA: strings.Repeat("c", 40), ExpectedTreeHash: strings.Repeat("e", 40), FileMode: "100644",
		Target: TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "cloudops-demo", Name: "demo", Container: "demo"},
		EnvKey: "REQUIRED_ENV", CurrentContent: []byte(currentRequiredEnvYAML), BaselineContent: []byte(baselineRequiredEnvYAML),
		Policy: policy, VerificationPlan: json.RawMessage(`{"profile":"golden-required-env/v1","stability_window_seconds":60}`),
		Evidence: []EvidenceBinding{
			{ID: "11111111-1111-4111-8111-111111111111", ContentHash: strings.Repeat("1", 64)},
			{ID: "22222222-2222-4222-8222-222222222222", ContentHash: strings.Repeat("2", 64)},
		},
		BaselineIsAncestor: true, CreatedAt: created, ExpiresAt: created.Add(30 * time.Minute), PlanVersion: 1,
	}
}
