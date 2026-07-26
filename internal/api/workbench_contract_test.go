package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	workbenchPlanID     = "223e4567-e89b-12d3-a456-426614174001"
	workbenchAgentRunID = "223e4567-e89b-12d3-a456-426614174002"
	workbenchEvidenceID = "223e4567-e89b-12d3-a456-426614174003"
	workbenchDecisionID = "223e4567-e89b-12d3-a456-426614174004"
	workbenchDeliveryID = "223e4567-e89b-12d3-a456-426614174005"
	workbenchRunID      = "223e4567-e89b-12d3-a456-426614174006"
	workbenchCheckID    = "223e4567-e89b-12d3-a456-426614174007"
	workbenchSampleID   = "223e4567-e89b-12d3-a456-426614174008"
)

func TestTypedWorkbenchProjectionEndpoints(t *testing.T) {
	projection := validWorkbenchProjection(t)
	engine := newContractEngine(NewHandler(Config{Queries: projection}))

	tests := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{
			path:      "/api/v1/incidents/" + contractIncidentID + "/remediation-plans",
			required:  []string{`"bounded_diff"`, `"target"`, `"canonical_manifest"`, `"policy_snapshot"`, `"verification_plan"`, `"evidence_bindings"`, `"decision"`},
			forbidden: []string{`"lease_owner"`, `"checkpoint"`, `"raw_prompt"`, `"internal_id"`, `"numeric_id"`, `"secret"`},
		},
		{
			path:      "/api/v1/incidents/" + contractIncidentID + "/delivery",
			required:  []string{`"repository"`, `"head_branch"`, `"commit_sha"`, `"pr_number"`, `"ci_status"`, `"target_revision"`, `"detected_revision"`, `"argocd_application"`, `"resource_health"`, `"desired_replicas"`, `"delivery_deadline_at"`},
			forbidden: []string{`"lease_owner"`, `"next_poll_at"`, `"write_phase"`, `"external_write_marker"`, `"secret"`},
		},
		{
			path:      "/api/v1/incidents/" + contractIncidentID + "/verifications",
			required:  []string{`"profile"`, `"revisions"`, `"deadline_at"`, `"common_window"`, `"checks"`, `"samples"`, workbenchCheckID, workbenchSampleID},
			forbidden: []string{`"lease_owner"`, `"lease_expires_at"`, `"heartbeat_at"`, `"checkpoint"`, `"internal_id"`, `"numeric_id"`, `"secret"`},
		},
		{
			path:      "/api/v1/incidents/" + contractIncidentID + "/resolution-report",
			required:  []string{`"resolution_reason"`, `"verification_profile"`, `"stability"`, `"verification"`, `"agent_usage"`},
			forbidden: []string{`"lease_owner"`, `"checkpoint"`, `"raw_prompt"`, `"internal_id"`, `"numeric_id"`, `"secret"`},
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != JSONMediaType {
				t.Fatalf("status/content-type=%d/%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			body := response.Body.String()
			for _, required := range test.required {
				if !strings.Contains(body, required) {
					t.Errorf("response is missing %s: %s", required, body)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(strings.ToLower(body), forbidden) {
					t.Errorf("response exposes forbidden field %s: %s", forbidden, body)
				}
			}
		})
	}
}

func TestOptionalWorkbenchProjectionsReturnExplicitEmptyResources(t *testing.T) {
	projection := NewMemoryQueryPort()
	if err := projection.PutIncident(IncidentView{
		ID: contractIncidentID, Cycle: 1, Status: "investigating", Severity: "warning", Version: 1,
	}); err != nil {
		t.Fatal(err)
	}
	engine := newContractEngine(NewHandler(Config{Queries: projection}))
	for _, path := range []string{
		"/api/v1/incidents/" + contractIncidentID + "/delivery",
		"/api/v1/incidents/" + contractIncidentID + "/resolution-report",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != JSONMediaType {
				t.Fatalf("status/content-type=%d/%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			if strings.TrimSpace(response.Body.String()) != `{"resource":null}` {
				t.Fatalf("optional resource body=%s", response.Body.String())
			}
		})
	}
}

func TestTypedWorkbenchProjectionRejectsUnsafeOrUnboundedData(t *testing.T) {
	plan := validRemediationPlanFixture()
	plan.PolicySnapshot = json.RawMessage(`{"raw_prompt":"do not expose"}`)
	if err := validateRemediationPlanView(&plan); err == nil {
		t.Fatal("remediation plan raw prompt was accepted")
	}

	plan = validRemediationPlanFixture()
	plan.BoundedDiff = strings.Repeat("x", maxWorkbenchDiffBytes+1)
	if err := validateRemediationPlanView(&plan); err == nil {
		t.Fatal("oversized remediation diff was accepted")
	}

	plan = validRemediationPlanFixture()
	plan.Decision.Actor.Role = "operator"
	if err := validateRemediationPlanView(&plan); err == nil {
		t.Fatal("non-product Decision role was accepted")
	}

	plan = validRemediationPlanFixture()
	plan.EvidenceBindings = []EvidenceBindingView{
		{ID: "323e4567-e89b-12d3-a456-426614174003", ContentHash: strings.Repeat("e", 64)},
		{ID: workbenchEvidenceID, ContentHash: strings.Repeat("d", 64)},
	}
	plan.EvidenceSetHash = workbenchEvidenceSetHash(plan.EvidenceBindings)
	if err := validateRemediationPlanView(&plan); err == nil {
		t.Fatal("non-canonical Evidence binding order was accepted")
	}

	delivery := validDeliveryFixture()
	delivery.ResourceHealth = json.RawMessage(`{"secret":"do not expose"}`)
	if err := validateDeliveryView(&delivery); err == nil {
		t.Fatal("delivery resource-health secret was accepted")
	}

	run := validVerificationFixture()
	run.Checks[0].Observed = json.RawMessage(`{"internal_id":42}`)
	if err := validateVerificationRunView(&run); err == nil {
		t.Fatal("verification internal numeric ID was accepted")
	}

	run = validVerificationFixture()
	run.Checks = append(run.Checks, make([]VerificationCheckView, maxWorkbenchChecksPerRun)...)
	if err := validateVerificationRunView(&run); err == nil {
		t.Fatal("unbounded verification checks were accepted")
	}
}

func validWorkbenchProjection(t *testing.T) *MemoryQueryPort {
	t.Helper()
	projection := NewMemoryQueryPort()
	if err := projection.PutIncident(IncidentView{
		ID: contractIncidentID, Cycle: 1, Status: "verifying", Severity: "critical", Version: 4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := projection.PutRemediationPlans(contractIncidentID, []RemediationPlanView{validRemediationPlanFixture()}); err != nil {
		t.Fatal(err)
	}
	if err := projection.PutDelivery(contractIncidentID, validDeliveryFixture()); err != nil {
		t.Fatal(err)
	}
	if err := projection.PutVerifications(contractIncidentID, []VerificationRunView{validVerificationFixture()}); err != nil {
		t.Fatal(err)
	}
	if err := projection.PutResolutionReport(contractIncidentID, *validPostDeliveryResolutionReport()); err != nil {
		t.Fatal(err)
	}
	return projection
}

func validRemediationPlanFixture() RemediationPlanView {
	created := time.Date(2026, 7, 21, 2, 0, 0, 0, time.UTC)
	manifest := json.RawMessage(`{"base_blob_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","file_mode":"100644","path":"apps/api.yaml","post_image_hash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}`)
	policy := json.RawMessage(`{"version":"restore-required-env/v1"}`)
	verificationPlan := json.RawMessage(`{"profile_id":"golden-required-env/v1","schema_version":3}`)
	evidence := []EvidenceBindingView{{ID: workbenchEvidenceID, ContentHash: strings.Repeat("d", 64)}}
	item := RemediationPlanView{
		ID: workbenchPlanID, Kind: "remediation_plan", Cycle: 1, Status: "approved", Version: 2,
		PlanVersion: 1, PlanContentSchemaVersion: 2, IncidentVersion: 3,
		CreatedByAgentRunID: workbenchAgentRunID, OperationType: "restore_required_env", RiskLevel: "low",
		PatchSummary: "restore REQUIRED_ENV", RollbackPlan: "submit a new reviewed plan",
		ValidationPlan: "run golden-required-env/v1",
		Target: RemediationTargetView{
			Repository: "acme/gitops", BaseBranch: "main",
			BaseRevision: strings.Repeat("a", 40), LastKnownGoodRevision: strings.Repeat("9", 40),
			BaseBlobSHA: strings.Repeat("b", 40), FileMode: "100644", Path: "apps/api.yaml",
			FieldRef: "spec.template.spec.containers[name=api].env[name=REQUIRED_ENV]",
			Resource: RemediationTargetResourceView{
				APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "api", Container: "api",
			},
		},
		HashSchemaVersion: 1, DiagnosisHash: strings.Repeat("1", 64),
		CanonicalPlanHash: strings.Repeat("2", 64), ExpectedBeforeHash: strings.Repeat("3", 64),
		ExpectedPostImageHash: strings.Repeat("c", 64), ExpectedTreeHash: strings.Repeat("4", 40),
		ProposedPatchHash: sha256Hex(manifest), CanonicalManifest: manifest,
		BoundedDiff:   "--- a/apps/api.yaml\n+++ b/apps/api.yaml\n+ REQUIRED_ENV=value\n",
		PolicyVersion: "restore-required-env/v1", PolicyHash: sha256Hex(policy), PolicySnapshot: policy,
		VerificationPlan: verificationPlan, VerificationPlanHash: sha256Hex(verificationPlan),
		EvidenceBindings: evidence, EvidenceSetHash: workbenchEvidenceSetHash(evidence),
		ExpiresAt: created.Add(30 * time.Minute), CreatedAt: created, UpdatedAt: created.Add(2 * time.Minute),
	}
	item.Decision = &RemediationDecisionView{
		ID: workbenchDecisionID, DecisionSchemaVersion: 1, PlanVersion: item.PlanVersion,
		Decision: "approved", Actor: RemediationDecisionActorView{Provider: "local", Login: "owner", Role: "owner"},
		Reason: "reviewed exact immutable plan", RequestID: "workbench-request-1",
		RequestAuthenticatedAt: created.Add(time.Minute), ExpiresAt: item.ExpiresAt,
		ApprovedHashSchemaVersion: 1, ApprovedPlanHash: item.CanonicalPlanHash,
		ApprovedBaseSHA: item.Target.BaseRevision, ApprovedPostImageHash: item.ExpectedPostImageHash,
		ApprovedTreeHash: item.ExpectedTreeHash, ApprovedPatchHash: item.ProposedPatchHash,
		ApprovedPolicyHash: item.PolicyHash, ApprovedVerificationHash: item.VerificationPlanHash,
		ApprovedEvidenceSetHash: item.EvidenceSetHash, CreatedAt: created.Add(2 * time.Minute),
	}
	return item
}

func validDeliveryFixture() DeliveryView {
	created := time.Date(2026, 7, 21, 2, 5, 0, 0, time.UTC)
	started := created.Add(time.Minute)
	deadline := started.Add(5 * time.Minute)
	lastObserved := started.Add(30 * time.Second)
	return DeliveryView{
		ID: workbenchDeliveryID, Kind: "delivery", Cycle: 1, Status: "rolling_out", Version: 5,
		RemediationPlanID: workbenchPlanID, Repository: "acme/gitops", BaseRevision: strings.Repeat("a", 40),
		HeadBranch: "cloudops/incident-fix", CommitSHA: strings.Repeat("5", 40), PRNumber: 42,
		PRURL: "https://github.com/acme/gitops/pull/42", PRState: "open", CIStatus: "passing",
		MergedCommitSHA: strings.Repeat("6", 40), TargetRevision: strings.Repeat("6", 40),
		DetectedRevision: strings.Repeat("6", 40), ArgoApplication: "api", ArgoProject: "default",
		ArgoSyncStatus: "Synced", ArgoOperationPhase: "Succeeded", ArgoHealthStatus: "Progressing",
		ResourceHealth: json.RawMessage(`{"deployment":"Progressing"}`), Cluster: "demo",
		Environment: "staging", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "api",
		DeploymentGeneration: 8, ObservedGeneration: 8, RolloutRevision: strings.Repeat("6", 40),
		DesiredReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 1, UnavailableReplicas: 1,
		DeliveryStartedAt: &started, DeliveryDeadlineAt: &deadline, LastObservedAt: &lastObserved,
		CreatedAt: created, UpdatedAt: lastObserved,
	}
}

func validVerificationFixture() VerificationRunView {
	created := time.Date(2026, 7, 21, 2, 10, 0, 0, time.UTC)
	started := created.Add(time.Second)
	sampled := created.Add(10 * time.Second)
	target := strings.Repeat("6", 40)
	return VerificationRunView{
		ID: workbenchRunID, Kind: "verification", Cycle: 1, Status: "running", Version: 3,
		TriggerType: "post_delivery", RemediationPlanID: workbenchPlanID, ChangeRequestID: workbenchDeliveryID,
		Attempt: 1,
		Profile: VerificationProfileView{
			ID: "golden-required-env/v1", Version: 1, Hash: strings.Repeat("7", 64), ContractVersion: 1,
		},
		Revisions: VerificationRevisionsView{
			TargetRevision: target, SourceRevision: strings.Repeat("8", 40),
			ImageDigest: "sha256:" + strings.Repeat("9", 64), GitOpsRevision: target,
		},
		StartedAt: &started, DeadlineAt: created.Add(5 * time.Minute),
		CommonWindow: VerificationCommonWindowView{StabilityWindowMS: 60000},
		Checks: []VerificationCheckView{{
			ID: workbenchCheckID, SpecSchemaVersion: 1, Type: "argocd_exact_revision", Status: "running",
			Required: true, ProfileID: "golden-required-env/v1", TemplateID: "argocd_exact_revision/v1",
			TemplateVersion: "v1", Subject: VerificationSubjectView{
				Repository: "acme/gitops", PullRequest: 42, Revision: target,
				ArgoApplication: "api", ArgoProject: "default", Cluster: "demo", Environment: "staging",
				Namespace: "default", Service: "api", WorkloadKind: "Deployment", WorkloadName: "api",
			},
			Expected:       json.RawMessage(`{"revision":"` + target + `"}`),
			Observed:       json.RawMessage(`{"operation_phase":"Running"}`),
			SourceIdentity: "argocd_read", StabilityWindowMS: 60000, TimeoutMS: 180000,
			PollIntervalMS: 5000, MinSamples: 1, SampleUnit: "observation", FailureMode: "immediate",
			AttemptCount: 1,
			Samples: []VerificationSampleView{{
				ID: workbenchSampleID, SchemaVersion: 1, Sequence: 1, Status: "pending",
				Observed: json.RawMessage(`{"operation_phase":"Running"}`), SampledAt: sampled,
				ContentHash: strings.Repeat("a", 64), CreatedAt: sampled,
			}},
			CreatedAt: created, UpdatedAt: sampled,
		}},
		CreatedAt: created, UpdatedAt: sampled,
	}
}
