package cutover

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestConvertAgentCheckpointCompatibleMapsLegacyGraphAndCanonicalHashes(t *testing.T) {
	input, signature := validAgentCheckpointInput(t)
	conversion := ConvertAgentCheckpoint(input)
	if !conversion.Compatible {
		t.Fatalf("compatible checkpoint rejected: %s", conversion.ReasonCode)
	}
	if conversion.CheckpointCanonicalHash == "" || !isSHA256(conversion.CheckpointCanonicalHash) {
		t.Fatalf("canonical source checkpoint hash missing: %q", conversion.CheckpointCanonicalHash)
	}
	if conversion.OutputHash == "" || !isSHA256(conversion.OutputHash) || len(conversion.Output) == 0 {
		t.Fatalf("canonical output missing: hash=%q output=%s", conversion.OutputHash, conversion.Output)
	}
	outputHash, canonical, err := canonicalHashJSON(conversion.Output)
	if err != nil || outputHash != conversion.OutputHash || string(canonical) != string(conversion.Output) {
		t.Fatalf("output is not canonical: hash=%s/%s err=%v", outputHash, conversion.OutputHash, err)
	}
	if conversion.State.RunID != input.RunPublicID || conversion.State.IncidentID != input.IncidentPublicID ||
		conversion.State.CycleNo != input.CycleNo || conversion.State.IncidentVersion != input.IncidentVersion {
		t.Fatalf("ownership was not mapped: %+v", conversion.State)
	}
	if len(conversion.State.Questions) != 1 || conversion.State.Questions[0].Question != "which source changed?" {
		t.Fatalf("legacy questions were not mapped: %+v", conversion.State.Questions)
	}
	if len(conversion.State.Evidence) != 1 || conversion.State.Evidence[0].ID != input.Evidence[0].PublicID {
		t.Fatalf("legacy Evidence was not mapped: %+v", conversion.State.Evidence)
	}
	if len(conversion.State.ToolAttempts) != 1 || conversion.State.ToolAttempts[0].Signature != signature {
		t.Fatalf("completed tool signature was not mapped: %+v", conversion.State.ToolAttempts)
	}
}

func TestConvertAgentCheckpointRejectsContractViolations(t *testing.T) {
	tests := []struct {
		name string
		edit func(*AgentCheckpointInput)
		want string
	}{
		{"missing checkpoint field", func(input *AgentCheckpointInput) {
			graph := decodeGraph(t, input.Checkpoint)
			graph.Objective = ""
			input.Checkpoint = marshalGraph(t, graph)
			input.CheckpointHash = rawSHA256(input.Checkpoint)
		}, "checkpoint_fields_invalid"},
		{"corrupt checkpoint hash", func(input *AgentCheckpointInput) { input.CheckpointHash = strings.Repeat("0", 64) }, "checkpoint_hash_mismatch"},
		{"stale Evidence", func(input *AgentCheckpointInput) { input.Evidence[0].Fresh = false }, "checkpoint_evidence_stale_or_invalid"},
		{"cross-cycle Evidence", func(input *AgentCheckpointInput) { input.Evidence[0].CycleNo = input.CycleNo + 1 }, "checkpoint_evidence_ownership_invalid"},
		{"duplicate tool signature", func(input *AgentCheckpointInput) {
			input.CompletedSignatures = append(input.CompletedSignatures, input.CompletedSignatures[0])
		}, "checkpoint_completed_signature_duplicate"},
		{"budget over limit", func(input *AgentCheckpointInput) { input.Usage.Steps = input.Limits.MaxSteps + 1 }, "checkpoint_budget_invalid"},
		{"invalid next node", func(input *AgentCheckpointInput) {
			graph := decodeGraph(t, input.Checkpoint)
			graph.NextNode = agent.Node("invalid")
			input.Checkpoint = marshalGraph(t, graph)
			input.CheckpointHash = rawSHA256(input.Checkpoint)
		}, "checkpoint_next_node_invalid"},
		{"unrecoverable pending tool", func(input *AgentCheckpointInput) {
			graph := decodeGraph(t, input.Checkpoint)
			graph.NextNode = agent.NodeExecuteTool
			input.Checkpoint = marshalGraph(t, graph)
			input.CheckpointHash = rawSHA256(input.Checkpoint)
		}, "checkpoint_pending_tool_action_unrecoverable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, _ := validAgentCheckpointInput(t)
			test.edit(&input)
			got := ConvertAgentCheckpoint(input)
			if got.Compatible || got.ReasonCode != test.want {
				t.Fatalf("conversion=%+v, want incompatible reason %q", got, test.want)
			}
		})
	}
}

func TestConvertVerificationRejectsNegativeFixturesAndAcceptsCompatibleRun(t *testing.T) {
	base := validVerificationInput(t, verification.RunRunning, true)
	if conversion := ConvertVerification(base); !conversion.Compatible {
		t.Fatalf("compatible Verification rejected: %s", conversion.ReasonCode)
	}
	tests := []struct {
		name string
		edit func(*VerificationConversionInput)
		want string
	}{
		{"legacy Loki profile", func(input *VerificationConversionInput) { input.PlanJSON = json.RawMessage(`{"profile":"loki"}`) }, "legacy_loki_profile_incompatible"},
		{"cross-incident ownership", func(input *VerificationConversionInput) { input.OwnershipValid = false }, "verification_ownership_or_version_invalid"},
		{"missing revision", func(input *VerificationConversionInput) { input.SourceRevision = "" }, "verification_revision_missing_or_conflicting"},
		{"conflicting revision", func(input *VerificationConversionInput) { input.GitOpsRevision = strings.Repeat("f", 40) }, "verification_revision_missing_or_conflicting"},
		{"sample unit error", func(input *VerificationConversionInput) { input.Samples[0].SampleUnit = "wrong-unit" }, "verification_sample_unit_invalid"},
		{"min samples insufficient", func(input *VerificationConversionInput) {
			input.Samples[0].Status = verification.SamplePassed
			input.Samples[0].Count = 0
		}, "verification_min_samples_invalid"},
		{"common window invalid", func(input *VerificationConversionInput) { input.CommonWindow = 30 * time.Second }, "verification_common_window_invalid"},
		{"duplicate check", func(input *VerificationConversionInput) { input.Checks[1].Type = input.Checks[0].Type }, "verification_check_duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneVerificationInput(base)
			test.edit(&input)
			got := ConvertVerification(input)
			if got.Compatible || got.ReasonCode != test.want {
				t.Fatalf("conversion=%+v, want incompatible reason %q", got, test.want)
			}
		})
	}

	passing := validVerificationInput(t, verification.RunPassed, true)
	if conversion := ConvertVerification(passing); !conversion.Compatible {
		t.Fatalf("passing compatible Verification rejected: %s", conversion.ReasonCode)
	}
	passing.CommonWindowCompletedAt = nil
	if conversion := ConvertVerification(passing); conversion.Compatible || conversion.ReasonCode != "verification_common_window_invalid" {
		t.Fatalf("incomplete passing window accepted: %+v", conversion)
	}
}

func TestConvertLegacyChangeClassifiesExternalArtifactsAndTerminalNoTask(t *testing.T) {
	artifact := validLegacyArtifact()
	reconciled := &ReconciledPullRequest{Repository: artifact.Repository, PullRequest: artifact.PullRequest, URL: artifact.URL,
		BaseRevision: artifact.BaseRevision, HeadBranch: artifact.HeadBranch, HeadRevision: artifact.HeadRevision, State: "open"}
	active := LegacyChangeInput{SubjectID: 1, SubjectVersion: 2, IncidentID: 3, CycleNo: 1, SourceStatus: "pr_created",
		HasLegacyPlan: true, Artifact: artifact, Reconciled: reconciled}
	got := ConvertLegacyChange(active)
	if got.Class != ChangeObserveExistingPR || !got.Compatible || !got.CreateObserve {
		t.Fatalf("complete Draft PR was not classified read-only: %+v", got)
	}
	merged := active
	merged.SourceStatus = "merged"
	merged.Reconciled = &ReconciledPullRequest{Repository: artifact.Repository, PullRequest: artifact.PullRequest, URL: artifact.URL,
		BaseRevision: artifact.BaseRevision, HeadBranch: artifact.HeadBranch, HeadRevision: artifact.HeadRevision,
		State: "merged", Merged: true, MergedCommitSHA: strings.Repeat("d", 40)}
	merged.Artifact.MergedCommitSHA = strings.Repeat("d", 40)
	if got := ConvertLegacyChange(merged); got.Class != ChangeObserveExistingPR || !got.CreateObserve {
		t.Fatalf("merged PR was not classified read-only: %+v", got)
	}
	terminal := active
	terminal.SourceStatus = "failed"
	if got := ConvertLegacyChange(terminal); got.Class != ChangeObserveExistingPR || got.CreateObserve || got.ReasonCode != "legacy_pr_terminal_archived" {
		t.Fatalf("terminal ChangeRequest created a task: %+v", got)
	}
	partial := active
	partial.Artifact = LegacyExternalArtifact{HeadBranch: "feature/partial"}
	partial.Reconciled = nil
	if got := ConvertLegacyChange(partial); got.Class != ChangePartialWrite || got.CreateObserve {
		t.Fatalf("partial branch/commit was not fail-closed: %+v", got)
	}
	approvalOnly := active
	approvalOnly.Artifact = LegacyExternalArtifact{}
	approvalOnly.Reconciled = nil
	approvalOnly.HasApproval = true
	if got := ConvertLegacyChange(approvalOnly); got.Class != ChangeApprovalOnly || got.CreateObserve || got.ReasonCode != "legacy_approval_incomplete" {
		t.Fatalf("legacy Approval authorized a write: %+v", got)
	}
	ambiguous := active
	ambiguous.Reconciled = nil
	if got := ConvertLegacyChange(ambiguous); got.Class != ChangeAmbiguousExternal || got.CreateObserve || got.ReasonCode != "legacy_external_state_ambiguous" {
		t.Fatalf("ambiguous external state was accepted: %+v", got)
	}
	mismatched := active
	bad := *reconciled
	bad.HeadBranch = "feature/other"
	mismatched.Reconciled = &bad
	if got := ConvertLegacyChange(mismatched); got.Class != ChangeAmbiguousExternal || got.ReasonCode != "legacy_external_identity_mismatch" {
		t.Fatalf("mismatched external identity was accepted: %+v", got)
	}
}

func TestConvertIncidentStateCoversExactMappingAndFailedPriority(t *testing.T) {
	base := LegacyIncidentStateInput{IncidentID: 1, IncidentPublicID: "incident-1", SourceVersion: 2, CycleNo: 1}
	for _, item := range []struct {
		source, target string
	}{
		{"DETECTED", "detected"}, {"CORRELATING", "investigating"}, {"DIAGNOSING", "investigating"},
		{"DIAGNOSIS_COMPLETED", "investigating"}, {"PLANNING_REMEDIATION", "investigating"},
		{"AWAITING_APPROVAL", "awaiting_approval"}, {"CLOSED_NO_ACTION", "closed"},
	} {
		input := base
		input.SourceStatus = item.source
		got := ConvertIncidentState(input)
		if got.TargetV3Status != item.target {
			t.Fatalf("source=%s target=%s want=%s reason=%s", item.source, got.TargetV3Status, item.target, got.ReasonCode)
		}
	}
	failed := base
	failed.SourceStatus = "FAILED"
	failed.ActiveVerification = true
	failed.CompatibleActiveVerification = false
	if got := ConvertIncidentState(failed); got.TargetV3Status != "verifying" || got.ReasonCode != "legacy_failed_blocked" {
		t.Fatalf("FAILED active Verification priority was lost: %+v", got)
	}
	failed.ActiveVerification = false
	failed.ObservedExternalChange = true
	if got := ConvertIncidentState(failed); got.TargetV3Status != "delivering" || got.ReasonCode != "legacy_failed_blocked" {
		t.Fatalf("FAILED external Change priority was lost: %+v", got)
	}
	failed.ObservedExternalChange = false
	failed.PlanApprovalWithoutWrite = true
	if got := ConvertIncidentState(failed); got.TargetV3Status != "investigating" || got.ReasonCode != "legacy_approval_incomplete" {
		t.Fatalf("FAILED Approval priority was lost: %+v", got)
	}
	resolved := base
	resolved.SourceStatus = "RESOLVED"
	resolved.CompatiblePassingVerification = true
	resolved.VerificationTriggerValid = true
	resolved.VerificationRevisionsValid = true
	if got := ConvertIncidentState(resolved); got.TargetV3Status != "resolved" || !got.PreserveResolved {
		t.Fatalf("compatible RESOLVED state was not preserved: %+v", got)
	}
	resolved.VerificationRevisionsValid = false
	if got := ConvertIncidentState(resolved); got.TargetV3Status != "investigating" || got.ReasonCode != "legacy_resolution_unverified" || !got.NeedsAttention {
		t.Fatalf("unverified RESOLVED state was accepted: %+v", got)
	}
}

func TestValidateOutboxArchiveRejectsUnknownAndPreservesPublicationState(t *testing.T) {
	now := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	row := LegacyOutboxRow{ID: 1, EventID: "event-1", AggregateType: "incident", AggregateID: "incident-1",
		EventType: "incident.created", SchemaVersion: 1, Payload: json.RawMessage(`{"version":1}`), OccurredAt: now,
		CreatedAt: now}
	decision, err := ValidateOutboxArchive(row, false)
	if err != nil || decision.Publication != "unpublished" || decision.ReasonCode != "outbox-archived-unpublished" {
		t.Fatalf("unpublished archive=%+v err=%v", decision, err)
	}
	published := row
	published.PublishedAt = ptrTime(now.Add(time.Second))
	decision, err = ValidateOutboxArchive(published, false)
	if err != nil || decision.Publication != "published" || decision.ReasonCode != "outbox-archived-published" {
		t.Fatalf("published archive=%+v err=%v", decision, err)
	}
	for _, mutate := range []func(*LegacyOutboxRow){
		func(value *LegacyOutboxRow) { value.EventType = "unknown.event" },
		func(value *LegacyOutboxRow) { value.SchemaVersion = 99 },
		func(value *LegacyOutboxRow) { value.Payload = json.RawMessage(`not-json`) },
	} {
		invalid := row
		mutate(&invalid)
		if _, err := ValidateOutboxArchive(invalid, false); err == nil {
			t.Fatalf("invalid outbox row was accepted: %+v", invalid)
		}
	}
	external := row
	external.EventType = "delivery_pr_created"
	if _, err := ValidateOutboxArchive(external, false); err == nil || !strings.Contains(err.Error(), "external") {
		t.Fatalf("unreconciled external event was accepted: %v", err)
	}
}

func validAgentCheckpointInput(t *testing.T) (AgentCheckpointInput, string) {
	t.Helper()
	now := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	limits := agent.Limits{MaxSteps: 10, MaxToolCalls: 10, MaxModelCalls: 10, TokenBudget: 1000, MaxEvidenceItems: 4,
		MaxRuntime: time.Minute, ToolTimeout: time.Second, MaxEvidenceBytes: 16 * 1024, MaxCheckpointSize: 64 * 1024, MaxStepRetries: 2}
	usage := agent.Usage{Steps: 1, ToolCalls: 1, ModelCalls: 1, InputTokens: 10, OutputTokens: 20, Evidence: 1}
	argumentHash := strings.Repeat("b", 64)
	signature := canonicalHashFields("legacy-tool-signature/v1", "inspect_workload", argumentHash)
	graph := agent.GraphState{SchemaVersion: 1, RunPublicID: "run-1", IncidentPublicID: "incident-1", NextNode: agent.NodeSelectAction,
		Objective: "determine the triggering change", Incident: agent.IncidentContext{PublicID: "incident-1", Cluster: "kind", Namespace: "default", TargetKind: "Deployment", TargetName: "demo", Summary: "required env"},
		Plan:         agent.Plan{Summary: "bounded", Questions: []string{"which source changed?"}},
		Observations: []agent.Observation{{Tool: "inspect_workload", ArgumentsHash: argumentHash, Summary: "observed", ResultHash: strings.Repeat("c", 64), Valid: true, ObservedAt: now}},
		Usage:        usage, Limits: limits, CheckpointVersion: 7, StartedAt: now, DeadlineAt: now.Add(time.Minute)}
	checkpoint := marshalGraph(t, graph)
	return AgentCheckpointInput{SourceSchemaVersion: 1, TargetSchemaVersion: agentCheckpointTargetSchema, RunPublicID: "run-1", IncidentPublicID: "incident-1",
		CycleNo: 1, IncidentVersion: 4, CheckpointVersion: 7, CheckpointHash: rawSHA256(checkpoint), Checkpoint: checkpoint,
		Limits: limits, Usage: usage, Evidence: []LegacyAgentEvidence{{PublicID: "evidence-1", IncidentID: "incident-1", CycleNo: 1,
			FactType: "deployment", Fresh: true, Valid: true, ContentHash: strings.Repeat("e", 64), CollectedAt: now, MigratedLegacy: true}},
		CompletedSignatures: []string{signature}, MaxCheckpointBytes: limits.MaxCheckpointSize}, signature
}

func decodeGraph(t *testing.T, raw []byte) agent.GraphState {
	t.Helper()
	var graph agent.GraphState
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatal(err)
	}
	return graph
}

func marshalGraph(t *testing.T, graph agent.GraphState) []byte {
	t.Helper()
	raw, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func validVerificationInput(t *testing.T, status verification.RunStatus, passing bool) VerificationConversionInput {
	t.Helper()
	target, source, gitops := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	digest := "sha256:" + strings.Repeat("d", 64)
	plan, err := verification.CompileV3VerificationPlan(verification.V3CompileInput{TriggerType: "post_delivery", Repository: "acme/app", PullRequest: 7,
		TargetRevision: target, SourceRevision: source, ImageDigest: digest, GitOpsRevision: gitops, ArgoApplication: "demo", ArgoProject: "default",
		Cluster: "kind", Environment: "test", Namespace: "default", Service: "demo", WorkloadName: "demo", AlertNames: []string{"alert"}})
	if err != nil {
		t.Fatal(err)
	}
	checks := make([]LegacyVerificationCheck, 0, len(plan.Checks))
	samples := make([]LegacyVerificationSample, 0, len(plan.Checks))
	now := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	commonStart := now.Add(-2 * time.Minute)
	for _, spec := range plan.Checks {
		checkStatus := verification.CheckPending
		if passing {
			checkStatus = verification.CheckPassed
		}
		checks = append(checks, LegacyVerificationCheck{Type: spec.Type, Subject: spec.Subject, Expected: spec.Expected, ProfileID: spec.ProfileID,
			TemplateID: spec.TemplateID, TemplateVersion: spec.TemplateVersion, SourceIdentity: spec.SourceIdentity, Comparison: spec.Comparison,
			Threshold: spec.Threshold, InitialDelay: spec.InitialDelay, Lookback: spec.Lookback, PollInterval: spec.PollInterval,
			Timeout: spec.Timeout, StabilityWindow: spec.StabilityWindow, MinSamples: spec.MinSamples, SampleUnit: spec.SampleUnit,
			FailureMode: spec.FailureMode, Required: spec.Required, Status: checkStatus, ConsecutiveSuccessSince: func() *time.Time {
				if !passing {
					return nil
				}
				value := commonStart
				return &value
			}()})
		if passing {
			sampled := commonStart.Add(61 * time.Second)
			samples = append(samples, LegacyVerificationSample{CheckType: spec.Type, Sequence: 1, Status: verification.SamplePassed,
				SampleUnit: spec.SampleUnit, Count: spec.MinSamples, WindowFrom: &commonStart, WindowTo: func() *time.Time { value := commonStart.Add(time.Minute); return &value }(),
				SampledAt: sampled, ContentHash: strings.Repeat("e", 64)})
		}
	}
	commonEnd := commonStart.Add(time.Minute)
	return VerificationConversionInput{SourceSchemaVersion: 1, TargetSchemaVersion: 3, RunPublicID: "verification-1", IncidentPublicID: "incident-1", CycleNo: 1,
		OwnershipValid:  true,
		IncidentVersion: 4, RunVersion: 2, Attempt: 1, RunStatus: status, TriggerType: "post_delivery", TargetRevision: target,
		SourceRevision: source, ImageDigest: digest, GitOpsRevision: gitops, ProfileVersion: uint64(plan.ProfileVersion), ProfileHash: plan.ProfileHash,
		PlanJSON: mustJSON(t, plan), Checks: checks, Samples: samples, CommonWindow: verification.V3CommonStabilityWindow,
		CommonSuccessSince: &commonStart, CommonWindowCompletedAt: &commonEnd}
}

func cloneVerificationInput(input VerificationConversionInput) VerificationConversionInput {
	copyInput := input
	copyInput.PlanJSON = append(json.RawMessage(nil), input.PlanJSON...)
	copyInput.Checks = append([]LegacyVerificationCheck(nil), input.Checks...)
	copyInput.Samples = append([]LegacyVerificationSample(nil), input.Samples...)
	return copyInput
}

func validLegacyArtifact() LegacyExternalArtifact {
	return LegacyExternalArtifact{Repository: "acme/app", PullRequest: 7, URL: "https://github.com/acme/app/pull/7",
		BaseRevision: strings.Repeat("a", 40), HeadBranch: "feature/fix", HeadRevision: strings.Repeat("b", 40), State: "open"}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func ptrTime(value time.Time) *time.Time { return &value }
