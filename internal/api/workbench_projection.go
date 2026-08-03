package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxWorkbenchDiffBytes             = 64 * 1024
	maxWorkbenchTargetJSONBytes       = 4 * 1024
	maxWorkbenchManifestJSONBytes     = 4 * 1024
	maxWorkbenchPolicyJSONBytes       = 16 * 1024
	maxWorkbenchVerificationJSONBytes = 16 * 1024
	maxWorkbenchEvidenceBindings      = 40
	maxWorkbenchResourceHealthBytes   = 16 * 1024
	maxWorkbenchChecksPerRun          = 16
	maxWorkbenchSamplesPerRun         = 2000
	maxWorkbenchJSONDepth             = 64
)

func validateRemediationPlanViews(items []RemediationPlanView) error {
	for index := range items {
		if err := validateRemediationPlanView(&items[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateRemediationPlanView(item *RemediationPlanView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil {
		return err
	}
	creatorID, err := ParsePublicUUID(item.CreatedByAgentRunID)
	if err != nil {
		return err
	}
	if item.Kind != "remediation_plan" || item.Cycle == 0 || item.Version == 0 || item.PlanVersion == 0 ||
		item.PlanContentSchemaVersion == 0 || item.IncidentVersion == 0 || item.HashSchemaVersion == 0 ||
		!validRemediationPlanStatus(item.Status) || item.OperationType != "restore_required_env" ||
		(item.SourceType != "gitops" && item.SourceType != "local_scenario") ||
		(item.RiskLevel != "low" && item.RiskLevel != "medium" && item.RiskLevel != "high") {
		return fmt.Errorf("%w: invalid remediation plan identity", ErrInvalidArgument)
	}
	if !validWorkbenchText(item.PatchSummary, 2048, true) ||
		!validWorkbenchText(item.RollbackPlan, 4096, true) ||
		!validWorkbenchText(item.ValidationPlan, 4096, true) ||
		!validWorkbenchText(item.PolicyVersion, 64, true) {
		return fmt.Errorf("%w: invalid remediation plan text", ErrInvalidArgument)
	}
	if !validWorkbenchDiff(item.BoundedDiff) {
		return fmt.Errorf("%w: invalid bounded remediation diff", ErrInvalidArgument)
	}
	if err := validateRemediationTarget(&item.Target, item.SourceType); err != nil {
		return err
	}
	for _, hash := range []string{
		item.DiagnosisHash, item.CanonicalPlanHash, item.ExpectedBeforeHash,
		item.ExpectedPostImageHash, item.ProposedPatchHash, item.PolicyHash,
		item.VerificationPlanHash, item.EvidenceSetHash,
	} {
		if validateExpectedHash(hash) != nil {
			return fmt.Errorf("%w: invalid remediation plan hash", ErrInvalidArgument)
		}
	}
	switch item.SourceType {
	case "gitops":
		if item.PlanContentSchemaVersion > 2 || item.RuntimeBaseHash != "" || !validResolutionRevision(item.ExpectedTreeHash) {
			return fmt.Errorf("%w: invalid GitOps remediation identity", ErrInvalidArgument)
		}
	case "local_scenario":
		if item.PlanContentSchemaVersion != 3 || validateExpectedHash(item.RuntimeBaseHash) != nil || item.ExpectedTreeHash != "" {
			return fmt.Errorf("%w: invalid local Scenario remediation identity", ErrInvalidArgument)
		}
	}
	item.CanonicalManifest, err = projectWorkbenchJSON(item.CanonicalManifest, maxWorkbenchManifestJSONBytes, true, true)
	if err != nil {
		return err
	}
	if item.SourceType == "gitops" {
		var manifest workbenchCanonicalManifest
		if err := decodeWorkbenchObject(item.CanonicalManifest, maxWorkbenchManifestJSONBytes, &manifest); err != nil ||
			manifest.Path != item.Target.Path || manifest.BaseBlobSHA != item.Target.BaseBlobSHA ||
			manifest.FileMode != item.Target.FileMode || manifest.PostImageHash != item.ExpectedPostImageHash {
			return fmt.Errorf("%w: remediation manifest does not match its target", ErrInvalidArgument)
		}
	} else {
		var manifest workbenchLocalScenarioManifest
		if err := decodeWorkbenchObject(item.CanonicalManifest, maxWorkbenchManifestJSONBytes, &manifest); err != nil {
			return fmt.Errorf("%w: local Scenario remediation manifest does not match its target", ErrInvalidArgument)
		}
		manifest.Patch, err = projectWorkbenchJSON(manifest.Patch, maxWorkbenchManifestJSONBytes, true, true)
		if err != nil || manifest.PatchType != "application/strategic-merge-patch+json" ||
			manifest.SourceType != item.SourceType || manifest.TargetLocator != item.Target.Path ||
			manifest.RuntimeSnapshotHash != item.RuntimeBaseHash || manifest.PostImageHash != item.ExpectedPostImageHash ||
			validateExpectedHash(manifest.PatchHash) != nil || sha256Hex(manifest.Patch) != manifest.PatchHash {
			return fmt.Errorf("%w: local Scenario remediation manifest does not match its target", ErrInvalidArgument)
		}
	}
	item.PolicySnapshot, err = projectWorkbenchJSON(item.PolicySnapshot, maxWorkbenchPolicyJSONBytes, true, true)
	if err != nil {
		return err
	}
	var policyEnvelope struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(item.PolicySnapshot, &policyEnvelope); err != nil || policyEnvelope.Version != item.PolicyVersion {
		return fmt.Errorf("%w: remediation policy version mismatch", ErrInvalidArgument)
	}
	item.VerificationPlan, err = projectWorkbenchVerificationPlanJSON(item.VerificationPlan, maxWorkbenchVerificationJSONBytes, true, true)
	if err != nil {
		return err
	}
	if sha256Hex(item.CanonicalManifest) != item.ProposedPatchHash ||
		sha256Hex(item.PolicySnapshot) != item.PolicyHash ||
		sha256Hex(item.VerificationPlan) != item.VerificationPlanHash {
		return fmt.Errorf("%w: remediation snapshot hash mismatch", ErrInvalidArgument)
	}
	if len(item.EvidenceBindings) == 0 || len(item.EvidenceBindings) > maxWorkbenchEvidenceBindings {
		return fmt.Errorf("%w: invalid remediation evidence bindings", ErrInvalidArgument)
	}
	seenEvidence := make(map[string]struct{}, len(item.EvidenceBindings))
	for index := range item.EvidenceBindings {
		bindingID, err := ParsePublicUUID(item.EvidenceBindings[index].ID)
		if err != nil || validateExpectedHash(item.EvidenceBindings[index].ContentHash) != nil {
			return fmt.Errorf("%w: invalid remediation evidence binding", ErrInvalidArgument)
		}
		if _, duplicate := seenEvidence[bindingID]; duplicate {
			return fmt.Errorf("%w: duplicate remediation evidence binding", ErrInvalidArgument)
		}
		seenEvidence[bindingID] = struct{}{}
		item.EvidenceBindings[index].ID = bindingID
		if index > 0 {
			previous := item.EvidenceBindings[index-1]
			current := item.EvidenceBindings[index]
			if previous.ID > current.ID || (previous.ID == current.ID && previous.ContentHash >= current.ContentHash) {
				return fmt.Errorf("%w: remediation evidence bindings are not canonical", ErrInvalidArgument)
			}
		}
	}
	if workbenchEvidenceSetHash(item.EvidenceBindings) != item.EvidenceSetHash {
		return fmt.Errorf("%w: remediation evidence set hash mismatch", ErrInvalidArgument)
	}
	if item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() || item.ExpiresAt.IsZero() ||
		item.UpdatedAt.Before(item.CreatedAt) || !item.ExpiresAt.After(item.CreatedAt) {
		return fmt.Errorf("%w: invalid remediation plan time", ErrInvalidArgument)
	}
	if item.Decision != nil {
		if err := validateRemediationDecision(item.Decision, item); err != nil {
			return err
		}
	}
	item.ID = id
	item.CreatedByAgentRunID = creatorID
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	item.ExpiresAt = item.ExpiresAt.UTC()
	return nil
}

func validateRemediationTarget(target *RemediationTargetView, sourceType string) error {
	if target == nil || !validWorkbenchText(target.Path, 1024, true) || !validWorkbenchText(target.FieldRef, 1024, true) {
		return fmt.Errorf("%w: invalid remediation target", ErrInvalidArgument)
	}
	if sourceType == "gitops" {
		if !validWorkbenchText(target.Repository, 255, true) || !validWorkbenchText(target.BaseBranch, 255, true) ||
			!validWorkbenchText(target.FileMode, 16, true) || !validResolutionRevision(target.BaseRevision) ||
			!validResolutionRevision(target.LastKnownGoodRevision) || !validResolutionRevision(target.BaseBlobSHA) {
			return fmt.Errorf("%w: invalid GitOps remediation target", ErrInvalidArgument)
		}
	} else if sourceType == "local_scenario" {
		if target.Repository != "" || target.BaseBranch != "" || target.BaseRevision != "" ||
			target.LastKnownGoodRevision != "" || target.BaseBlobSHA != "" || target.FileMode != "" {
			return fmt.Errorf("%w: local Scenario remediation target contains Git identity", ErrInvalidArgument)
		}
	} else {
		return fmt.Errorf("%w: invalid remediation source type", ErrInvalidArgument)
	}
	resource := target.Resource
	if !validWorkbenchText(resource.APIVersion, 64, true) ||
		!validWorkbenchText(resource.Kind, 64, true) ||
		!validWorkbenchText(resource.Namespace, 255, true) ||
		!validWorkbenchText(resource.Name, 255, true) ||
		!validWorkbenchText(resource.Container, 255, false) {
		return fmt.Errorf("%w: invalid remediation target resource", ErrInvalidArgument)
	}
	return nil
}

func validateRemediationDecision(decision *RemediationDecisionView, plan *RemediationPlanView) error {
	decisionID, err := ParsePublicUUID(decision.ID)
	if err != nil {
		return err
	}
	if decision.DecisionSchemaVersion == 0 || decision.PlanVersion != plan.PlanVersion ||
		(decision.Decision != "approved" && decision.Decision != "rejected") ||
		decision.ApprovedHashSchemaVersion == 0 ||
		decision.Actor.Provider != "local" ||
		decision.Actor.Login != "owner" ||
		decision.Actor.Role != "owner" ||
		!validWorkbenchText(decision.Reason, 1024, true) ||
		!validWorkbenchText(decision.RequestID, 128, true) {
		return fmt.Errorf("%w: invalid remediation decision", ErrInvalidArgument)
	}
	expectedDecisionSchema, expectedBaseSHA, expectedTreeHash := uint64(1), plan.Target.BaseRevision, plan.ExpectedTreeHash
	if plan.SourceType == "local_scenario" {
		expectedDecisionSchema, expectedBaseSHA, expectedTreeHash = 2, "", ""
		if decision.Decision != "rejected" {
			return fmt.Errorf("%w: local Scenario remediation Plans are reject-only", ErrInvalidArgument)
		}
	}
	if decision.DecisionSchemaVersion != expectedDecisionSchema || decision.ApprovedPlanHash != plan.CanonicalPlanHash ||
		decision.ApprovedBaseSHA != expectedBaseSHA ||
		decision.ApprovedPostImageHash != plan.ExpectedPostImageHash ||
		decision.ApprovedTreeHash != expectedTreeHash ||
		decision.ApprovedPatchHash != plan.ProposedPatchHash ||
		decision.ApprovedPolicyHash != plan.PolicyHash ||
		decision.ApprovedVerificationHash != plan.VerificationPlanHash ||
		decision.ApprovedEvidenceSetHash != plan.EvidenceSetHash {
		return fmt.Errorf("%w: remediation decision hash mismatch", ErrInvalidArgument)
	}
	if decision.RequestAuthenticatedAt.IsZero() || decision.CreatedAt.IsZero() || decision.ExpiresAt.IsZero() ||
		decision.RequestAuthenticatedAt.Before(plan.CreatedAt) ||
		decision.CreatedAt.Before(decision.RequestAuthenticatedAt) ||
		!decision.ExpiresAt.After(decision.CreatedAt) || decision.ExpiresAt.After(plan.ExpiresAt) {
		return fmt.Errorf("%w: invalid remediation decision time", ErrInvalidArgument)
	}
	decision.ID = decisionID
	decision.RequestAuthenticatedAt = decision.RequestAuthenticatedAt.UTC()
	decision.ExpiresAt = decision.ExpiresAt.UTC()
	decision.CreatedAt = decision.CreatedAt.UTC()
	return nil
}

func validateDeliveryView(item *DeliveryView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil {
		return err
	}
	planID, err := ParsePublicUUID(item.RemediationPlanID)
	if err != nil {
		return err
	}
	if item.Kind != "delivery" || item.Cycle == 0 || item.Version == 0 || !validDeliveryStatus(item.Status) ||
		!validWorkbenchText(item.Repository, 255, true) ||
		!validWorkbenchText(item.HeadBranch, 255, true) ||
		!validResolutionRevision(item.BaseRevision) || !validOptionalRevision(item.CommitSHA) ||
		!validOptionalRevision(item.MergedCommitSHA) || !validOptionalRevision(item.TargetRevision) ||
		!validOptionalRevision(item.DetectedRevision) || !validOptionalRevision(item.RolloutRevision) ||
		item.PRNumber < 0 || item.DeploymentGeneration < 0 || item.ObservedGeneration < 0 ||
		item.DesiredReplicas < 0 || item.UpdatedReplicas < 0 || item.AvailableReplicas < 0 || item.UnavailableReplicas < 0 {
		return fmt.Errorf("%w: invalid delivery identity", ErrInvalidArgument)
	}
	if item.CIStatus != "pending" && item.CIStatus != "passing" && item.CIStatus != "failing" && item.CIStatus != "cancelled" {
		return fmt.Errorf("%w: invalid delivery CI status", ErrInvalidArgument)
	}
	for value, maxBytes := range map[string]int{
		item.PRURL: 1024, item.PRState: 16, item.ArgoApplication: 255, item.ArgoProject: 255,
		item.ArgoSyncStatus: 32, item.ArgoOperationPhase: 32, item.ArgoHealthStatus: 32,
		item.Cluster: 255, item.Environment: 255, item.Namespace: 255,
		item.WorkloadKind: 64, item.WorkloadName: 255, item.FailureCode: 64, item.FailureReason: 128,
	} {
		if !validWorkbenchText(value, maxBytes, false) {
			return fmt.Errorf("%w: invalid delivery text", ErrInvalidArgument)
		}
	}
	item.ResourceHealth, err = projectWorkbenchJSON(item.ResourceHealth, maxWorkbenchResourceHealthBytes, false, false)
	if err != nil {
		return err
	}
	if item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() || item.UpdatedAt.Before(item.CreatedAt) {
		return fmt.Errorf("%w: invalid delivery time", ErrInvalidArgument)
	}
	if (item.SyncCompletedAt != nil && item.SyncStartedAt == nil) ||
		(item.SyncStartedAt != nil && item.SyncStartedAt.Before(item.CreatedAt)) ||
		(item.SyncCompletedAt != nil && item.SyncCompletedAt.Before(*item.SyncStartedAt)) ||
		(item.DeliveryDeadlineAt != nil && item.DeliveryStartedAt == nil) ||
		(item.DeliveryCompletedAt != nil && item.DeliveryStartedAt == nil) ||
		(item.DeliveryStartedAt != nil && item.DeliveryStartedAt.Before(item.CreatedAt)) ||
		(item.DeliveryCompletedAt != nil && item.DeliveryCompletedAt.Before(*item.DeliveryStartedAt)) ||
		(item.LastObservedAt != nil && item.LastObservedAt.Before(item.CreatedAt)) {
		return fmt.Errorf("%w: invalid delivery observation window", ErrInvalidArgument)
	}
	item.ID = id
	item.RemediationPlanID = planID
	normalizeTime(&item.CreatedAt)
	normalizeTime(&item.UpdatedAt)
	normalizeOptionalTime(item.SyncStartedAt)
	normalizeOptionalTime(item.SyncCompletedAt)
	normalizeOptionalTime(item.DeliveryStartedAt)
	normalizeOptionalTime(item.DeliveryDeadlineAt)
	normalizeOptionalTime(item.DeliveryCompletedAt)
	normalizeOptionalTime(item.LastObservedAt)
	return nil
}

func validateVerificationRunViews(items []VerificationRunView) error {
	for index := range items {
		if err := validateVerificationRunView(&items[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateVerificationRunView(item *VerificationRunView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil {
		return err
	}
	if item.Kind != "verification" || item.Cycle == 0 || item.Version == 0 || item.Attempt == 0 ||
		!validVerificationRunStatus(item.Status) || item.Profile.Version == 0 ||
		item.Profile.ContractVersion == 0 || validateExpectedHash(item.Profile.Hash) != nil ||
		(item.Profile.ID != "golden-required-env/v1" && item.Profile.ID != "no-change/v1" &&
			item.Profile.ID != "operational-recovery/v1") {
		return fmt.Errorf("%w: invalid verification run identity", ErrInvalidArgument)
	}
	if err := validateVerificationRelations(item); err != nil {
		return err
	}
	if item.DeadlineAt.IsZero() || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() ||
		item.DeadlineAt.Before(item.CreatedAt) || item.UpdatedAt.Before(item.CreatedAt) ||
		item.CommonWindow.StabilityWindowMS != 60000 {
		return fmt.Errorf("%w: invalid verification run time", ErrInvalidArgument)
	}
	if (item.StartedAt != nil && item.StartedAt.Before(item.CreatedAt)) ||
		(item.CompletedAt != nil && item.CompletedAt.Before(item.CreatedAt)) ||
		(item.CompletedAt != nil && item.StartedAt != nil && item.CompletedAt.Before(*item.StartedAt)) {
		return fmt.Errorf("%w: invalid verification execution window", ErrInvalidArgument)
	}
	if !validWorkbenchText(item.ResultSummary, 2048, false) || !validWorkbenchText(item.FailureReason, 128, false) {
		return fmt.Errorf("%w: invalid verification run text", ErrInvalidArgument)
	}
	if item.CommonWindow.CompletedAt != nil && item.CommonWindow.SuccessSince == nil {
		return fmt.Errorf("%w: invalid verification common window", ErrInvalidArgument)
	}
	if item.CommonWindow.CompletedAt != nil && item.CommonWindow.CompletedAt.Before(*item.CommonWindow.SuccessSince) {
		return fmt.Errorf("%w: invalid verification common window", ErrInvalidArgument)
	}
	if len(item.Checks) == 0 || len(item.Checks) > maxWorkbenchChecksPerRun {
		return fmt.Errorf("%w: verification check count exceeds its bound", ErrInvalidArgument)
	}
	seenChecks := make(map[string]struct{}, len(item.Checks))
	totalSamples := 0
	for index := range item.Checks {
		if err := validateVerificationCheck(&item.Checks[index], item); err != nil {
			return err
		}
		if _, duplicate := seenChecks[item.Checks[index].Type]; duplicate {
			return fmt.Errorf("%w: duplicate verification check", ErrInvalidArgument)
		}
		seenChecks[item.Checks[index].Type] = struct{}{}
		totalSamples += len(item.Checks[index].Samples)
		if totalSamples > maxWorkbenchSamplesPerRun {
			return fmt.Errorf("%w: verification sample count exceeds its bound", ErrInvalidArgument)
		}
	}
	item.ID = id
	normalizeTime(&item.DeadlineAt)
	normalizeTime(&item.CreatedAt)
	normalizeTime(&item.UpdatedAt)
	normalizeOptionalTime(item.StartedAt)
	normalizeOptionalTime(item.CompletedAt)
	normalizeOptionalTime(item.CommonWindow.SuccessSince)
	normalizeOptionalTime(item.CommonWindow.CompletedAt)
	return nil
}

func validateVerificationRelations(item *VerificationRunView) error {
	parseOptional := func(value *string) error {
		if *value == "" {
			return nil
		}
		id, err := ParsePublicUUID(*value)
		if err != nil {
			return err
		}
		*value = id
		return nil
	}
	if err := parseOptional(&item.RemediationPlanID); err != nil {
		return err
	}
	if err := parseOptional(&item.ChangeRequestID); err != nil {
		return err
	}
	if err := parseOptional(&item.TriggerSignalID); err != nil {
		return err
	}
	switch item.TriggerType {
	case "post_delivery":
		if item.Profile.ID != "golden-required-env/v1" || item.RemediationPlanID == "" || item.ChangeRequestID == "" || item.TriggerSignalID != "" ||
			item.RecoveryProvenance != nil || !validVerificationRevisions(item.Revisions) {
			return fmt.Errorf("%w: invalid post-delivery verification relation", ErrInvalidArgument)
		}
	case "no_change_signal":
		if item.Profile.ID != "no-change/v1" || item.RemediationPlanID != "" || item.ChangeRequestID != "" || item.TriggerSignalID == "" ||
			item.RecoveryProvenance != nil || !validVerificationRevisions(item.Revisions) {
			return fmt.Errorf("%w: invalid no-change verification relation", ErrInvalidArgument)
		}
	case "operational_recovery":
		if item.Profile.ID != "operational-recovery/v1" || item.RemediationPlanID != "" || item.ChangeRequestID != "" || item.TriggerSignalID != "" ||
			item.Revisions != (VerificationRevisionsView{}) || validateRecoveryProvenance(item.RecoveryProvenance) != nil {
			return fmt.Errorf("%w: invalid operational recovery verification relation", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: invalid verification trigger", ErrInvalidArgument)
	}
	return nil
}

func validVerificationRevisions(revisions VerificationRevisionsView) bool {
	return validResolutionRevision(revisions.TargetRevision) &&
		validResolutionRevision(revisions.SourceRevision) &&
		validResolutionImageDigest(revisions.ImageDigest) &&
		validResolutionRevision(revisions.GitOpsRevision)
}

func validateRecoveryProvenance(item *RecoveryProvenanceView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	identities := []*string{
		&item.ConfigurationRevisionID, &item.OperationalScopeID, &item.InvestigationID, &item.DecisionID,
	}
	for _, identity := range identities {
		value, err := ParsePublicUUID(*identity)
		if err != nil {
			return err
		}
		*identity = value
	}
	return nil
}

func validateVerificationCheck(check *VerificationCheckView, run *VerificationRunView) error {
	id, err := ParsePublicUUID(check.ID)
	if err != nil {
		return err
	}
	if check.SpecSchemaVersion == 0 || !validVerificationCheckStatus(check.Status) ||
		check.ProfileID != run.Profile.ID || !validWorkbenchText(check.Type, 64, true) ||
		!validWorkbenchText(check.TemplateID, 128, true) ||
		!validWorkbenchText(check.TemplateVersion, 64, true) ||
		!validWorkbenchText(check.SourceReference, 1024, false) ||
		!validWorkbenchText(check.SourceIdentity, 255, true) ||
		!validWorkbenchText(check.SampleUnit, 32, true) ||
		!validWorkbenchText(check.FailureReason, 128, false) ||
		check.StabilityWindowMS == 0 || check.TimeoutMS < check.StabilityWindowMS ||
		check.PollIntervalMS == 0 || check.MinSamples == 0 {
		return fmt.Errorf("%w: invalid verification check", ErrInvalidArgument)
	}
	if check.FailureMode != "resets" && check.FailureMode != "immediate" {
		return fmt.Errorf("%w: invalid verification failure mode", ErrInvalidArgument)
	}
	if check.Comparison == "" {
		if check.Threshold != nil {
			return fmt.Errorf("%w: invalid verification threshold", ErrInvalidArgument)
		}
	} else if check.Threshold == nil || !validVerificationComparison(check.Comparison) ||
		math.IsNaN(*check.Threshold) || math.IsInf(*check.Threshold, 0) {
		return fmt.Errorf("%w: invalid verification threshold", ErrInvalidArgument)
	}
	if err := validateVerificationSubject(&check.Subject, run.Revisions.TargetRevision); err != nil {
		return err
	}
	check.Expected, err = projectWorkbenchJSON(check.Expected, maxWorkbenchVerificationJSONBytes, true, true)
	if err != nil {
		return err
	}
	check.Observed, err = projectWorkbenchJSON(check.Observed, maxWorkbenchVerificationJSONBytes, false, true)
	if err != nil {
		return err
	}
	seenSamples := make(map[uint64]struct{}, len(check.Samples))
	if check.CreatedAt.IsZero() || check.UpdatedAt.IsZero() || check.UpdatedAt.Before(check.CreatedAt) ||
		(check.FirstCheckedAt != nil && check.FirstCheckedAt.Before(check.CreatedAt)) ||
		(check.LastCheckedAt != nil && check.LastCheckedAt.Before(check.CreatedAt)) ||
		(check.LastCheckedAt != nil && check.FirstCheckedAt != nil && check.LastCheckedAt.Before(*check.FirstCheckedAt)) ||
		(check.PassedAt != nil && check.PassedAt.Before(check.CreatedAt)) ||
		(check.ConsecutiveSuccessSince != nil && check.ConsecutiveSuccessSince.Before(check.CreatedAt)) {
		return fmt.Errorf("%w: invalid verification check time", ErrInvalidArgument)
	}
	for index := range check.Samples {
		if err := validateVerificationSample(&check.Samples[index]); err != nil {
			return err
		}
		if _, duplicate := seenSamples[check.Samples[index].Sequence]; duplicate {
			return fmt.Errorf("%w: duplicate verification sample sequence", ErrInvalidArgument)
		}
		seenSamples[check.Samples[index].Sequence] = struct{}{}
	}
	check.ID = id
	normalizeTime(&check.CreatedAt)
	normalizeTime(&check.UpdatedAt)
	normalizeOptionalTime(check.FirstCheckedAt)
	normalizeOptionalTime(check.LastCheckedAt)
	normalizeOptionalTime(check.PassedAt)
	normalizeOptionalTime(check.ConsecutiveSuccessSince)
	return nil
}

func validateVerificationSubject(subject *VerificationSubjectView, targetRevision string) error {
	if subject == nil || subject.PullRequest < 0 || subject.Revision != targetRevision ||
		!validWorkbenchText(subject.Repository, 255, false) ||
		!validWorkbenchText(subject.ArgoApplication, 255, false) ||
		!validWorkbenchText(subject.ArgoProject, 255, false) ||
		!validWorkbenchText(subject.Cluster, 255, false) ||
		!validWorkbenchText(subject.Environment, 255, false) ||
		!validWorkbenchText(subject.Namespace, 255, false) ||
		!validWorkbenchText(subject.Service, 255, false) ||
		!validWorkbenchText(subject.WorkloadKind, 64, false) ||
		!validWorkbenchText(subject.WorkloadName, 255, false) ||
		!validWorkbenchText(subject.AlertFingerprint, 128, false) {
		return fmt.Errorf("%w: invalid verification subject", ErrInvalidArgument)
	}
	return nil
}

func validateVerificationSample(sample *VerificationSampleView) error {
	id, err := ParsePublicUUID(sample.ID)
	if err != nil {
		return err
	}
	if sample.SchemaVersion == 0 || sample.Sequence == 0 || !validVerificationSampleStatus(sample.Status) ||
		validateExpectedHash(sample.ContentHash) != nil ||
		!validWorkbenchText(sample.SourceReference, 1024, false) ||
		!validWorkbenchText(sample.ReasonCode, 128, false) ||
		sample.SampledAt.IsZero() || sample.CreatedAt.IsZero() {
		return fmt.Errorf("%w: invalid verification sample", ErrInvalidArgument)
	}
	sample.Observed, err = projectWorkbenchJSON(sample.Observed, maxWorkbenchVerificationJSONBytes, true, true)
	if err != nil {
		return err
	}
	if (sample.WindowStartAt == nil) != (sample.WindowEndAt == nil) ||
		(sample.WindowStartAt != nil && (sample.WindowEndAt.Before(*sample.WindowStartAt) || sample.SampledAt.Before(*sample.WindowEndAt))) {
		return fmt.Errorf("%w: invalid verification sample window", ErrInvalidArgument)
	}
	sample.ID = id
	normalizeTime(&sample.SampledAt)
	normalizeTime(&sample.CreatedAt)
	normalizeOptionalTime(sample.WindowStartAt)
	normalizeOptionalTime(sample.WindowEndAt)
	return nil
}

func projectWorkbenchJSON(raw json.RawMessage, maxBytes int, required, objectOnly bool) (json.RawMessage, error) {
	return projectWorkbenchJSONWithSemanticProfileID(raw, maxBytes, required, objectOnly, false)
}

func projectWorkbenchVerificationPlanJSON(raw json.RawMessage, maxBytes int, required, objectOnly bool) (json.RawMessage, error) {
	return projectWorkbenchJSONWithSemanticProfileID(raw, maxBytes, required, objectOnly, true)
}

func projectWorkbenchJSONWithSemanticProfileID(raw json.RawMessage, maxBytes int, required, objectOnly, allowSemanticProfileID bool) (json.RawMessage, error) {
	if len(raw) == 0 {
		if required {
			return nil, fmt.Errorf("%w: required Workbench JSON is missing", ErrInvalidArgument)
		}
		return nil, nil
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("%w: Workbench JSON exceeds its bound", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: malformed Workbench JSON", ErrInvalidArgument)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: malformed Workbench JSON", ErrInvalidArgument)
	}
	if objectOnly {
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("%w: Workbench JSON must be an object", ErrInvalidArgument)
		}
	}
	if err := validateWorkbenchJSONValue(value, 0, "", allowSemanticProfileID); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > maxBytes {
		return nil, fmt.Errorf("%w: invalid bounded Workbench JSON", ErrInvalidArgument)
	}
	return json.RawMessage(canonical), nil
}

type workbenchCanonicalManifest struct {
	Path          string `json:"path"`
	BaseBlobSHA   string `json:"base_blob_sha"`
	FileMode      string `json:"file_mode"`
	PostImageHash string `json:"post_image_hash"`
}

type workbenchLocalScenarioManifest struct {
	SourceType          string          `json:"source_type"`
	PatchType           string          `json:"patch_type"`
	TargetLocator       string          `json:"target_locator"`
	RuntimeSnapshotHash string          `json:"runtime_snapshot_hash"`
	PatchHash           string          `json:"patch_hash"`
	Patch               json.RawMessage `json:"patch"`
	PostImageHash       string          `json:"post_image_hash"`
}

func validateWorkbenchNextCursor(value string) error {
	if value == "" {
		return nil
	}
	_, err := ParsePublicUUID(value)
	return err
}

func validateWorkbenchJSONValue(value any, depth int, path string, allowSemanticProfileID bool) error {
	if depth > maxWorkbenchJSONDepth {
		return fmt.Errorf("%w: Workbench JSON nesting exceeds its bound", ErrInvalidArgument)
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := normalizeResolutionJSONKey(key)
			childPath := normalized
			if path != "" {
				childPath = path + "." + normalized
			}
			if forbiddenResolutionJSONKey(normalized) {
				return fmt.Errorf("%w: forbidden Workbench JSON field %q", ErrInvalidArgument, key)
			}
			if allowSemanticProfileID && childPath == "profile.id" {
				identity, ok := child.(string)
				if !ok || !validWorkbenchText(identity, 128, true) {
					return fmt.Errorf("%w: invalid Workbench semantic profile identity", ErrInvalidArgument)
				}
			} else if workbenchPublicIDKey(normalized) {
				identity, ok := child.(string)
				if !ok {
					return fmt.Errorf("%w: malformed Workbench public identity", ErrInvalidArgument)
				}
				if _, err := ParsePublicUUID(identity); err != nil {
					return fmt.Errorf("%w: invalid Workbench public identity", ErrInvalidArgument)
				}
			} else if strings.HasSuffix(normalized, "_id") {
				switch child.(type) {
				case json.Number, float64:
					return fmt.Errorf("%w: numeric identity in Workbench JSON", ErrInvalidArgument)
				}
			}
			if err := validateWorkbenchJSONValue(child, depth+1, childPath, allowSemanticProfileID); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for _, child := range typed {
			if err := validateWorkbenchJSONValue(child, depth+1, path, allowSemanticProfileID); err != nil {
				return err
			}
		}
		return nil
	case string, json.Number, float64, bool, nil:
		return nil
	default:
		return fmt.Errorf("%w: unsupported Workbench JSON value", ErrInvalidArgument)
	}
}

func workbenchPublicIDKey(key string) bool {
	switch key {
	case "id", "public_id", "incident_id", "run_id", "verification_run_id", "remediation_plan_id",
		"change_request_id", "trigger_signal_id", "agent_run_id", "created_by_agent_run_id", "evidence_id":
		return true
	default:
		return strings.HasSuffix(key, "_public_id")
	}
}

func decodeWorkbenchObject(raw []byte, maxBytes int, target any) error {
	projected, err := projectWorkbenchJSON(raw, maxBytes, true, true)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(projected))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid typed Workbench JSON", ErrInvalidArgument)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("%w: invalid typed Workbench JSON", ErrInvalidArgument)
	}
	return nil
}

func decodeWorkbenchArray(raw []byte, maxBytes int, target any) error {
	projected, err := projectWorkbenchJSON(raw, maxBytes, true, false)
	if err != nil {
		return err
	}
	var root any
	if err := json.Unmarshal(projected, &root); err != nil {
		return fmt.Errorf("%w: invalid typed Workbench JSON", ErrInvalidArgument)
	}
	if _, ok := root.([]any); !ok {
		return fmt.Errorf("%w: Workbench JSON must be an array", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(projected))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid typed Workbench JSON", ErrInvalidArgument)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("%w: invalid typed Workbench JSON", ErrInvalidArgument)
	}
	return nil
}

func workbenchEvidenceSetHash(bindings []EvidenceBindingView) string {
	canonical := make([]map[string]string, 0, len(bindings))
	for _, binding := range bindings {
		canonical = append(canonical, map[string]string{"content_hash": binding.ContentHash, "id": binding.ID})
	}
	encoded, _ := json.Marshal(canonical)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	payload := append(length[:], encoded...)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func validWorkbenchText(value string, maxBytes int, required bool) bool {
	if required && value == "" {
		return false
	}
	return len(value) <= maxBytes && utf8.ValidString(value) && !containsControl(value)
}

func validWorkbenchDiff(value string) bool {
	if len(value) == 0 || len(value) > maxWorkbenchDiffBytes || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	}) < 0
}

func validOptionalRevision(value string) bool {
	return value == "" || validResolutionRevision(value)
}

func validRemediationPlanStatus(value string) bool {
	switch value {
	case "awaiting_approval", "approved", "rejected", "superseded", "cancelled", "consumed", "invalidated", "policy_rejected":
		return true
	default:
		return false
	}
}

func validDeliveryStatus(value string) bool {
	switch value {
	case "pending", "pr_open", "merged", "syncing", "rolling_out", "delivered", "failed", "cancelled", "superseded":
		return true
	default:
		return false
	}
}

func validVerificationRunStatus(value string) bool {
	switch value {
	case "pending", "running", "passed", "failed", "inconclusive", "timed_out", "cancelled":
		return true
	default:
		return false
	}
}

func validVerificationCheckStatus(value string) bool {
	switch value {
	case "pending", "running", "passed", "failed", "timed_out", "unavailable", "invalid", "cancelled":
		return true
	default:
		return false
	}
}

func validVerificationSampleStatus(value string) bool {
	switch value {
	case "passed", "failed", "pending", "unavailable", "invalid", "timed_out":
		return true
	default:
		return false
	}
}

func validVerificationComparison(value string) bool {
	switch value {
	case "lt", "lte", "gt", "gte", "absent":
		return true
	default:
		return false
	}
}

func normalizeTime(value *time.Time) {
	if value != nil {
		*value = value.UTC()
	}
}

func normalizeOptionalTime(value *time.Time) {
	normalizeTime(value)
}

func nonNilRemediationPlans(items []RemediationPlanView) []RemediationPlanView {
	if items == nil {
		return []RemediationPlanView{}
	}
	return items
}

func nonNilVerificationRuns(items []VerificationRunView) []VerificationRunView {
	if items == nil {
		return []VerificationRunView{}
	}
	return items
}

func nonNilVerificationChecks(items []VerificationCheckView) []VerificationCheckView {
	if items == nil {
		return []VerificationCheckView{}
	}
	return items
}

func nonNilVerificationSamples(items []VerificationSampleView) []VerificationSampleView {
	if items == nil {
		return []VerificationSampleView{}
	}
	return items
}
