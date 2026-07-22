package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
	"github.com/google/uuid"
)

type legacyVerificationRunRow struct {
	id                      uint64
	publicID                string
	incidentID              uint64
	incidentPublicID        string
	incidentVersion         uint64
	remediationPlanID       sql.NullInt64
	changeRequestID         sql.NullInt64
	ownerIncidentID         sql.NullInt64
	ownerPlanID             sql.NullInt64
	status                  verification.RunStatus
	rowVersion              uint64
	attempt                 uint64
	triggerType             string
	targetRevision          string
	sourceRevision          string
	imageDigest             string
	gitopsRevision          string
	profileVersion          uint64
	profileHash             string
	planJSON                []byte
	commonWindow            time.Duration
	commonSuccessSince      *time.Time
	commonWindowCompletedAt *time.Time
	completedAt             sql.NullTime
	createdAt               time.Time
	updatedAt               time.Time
}

func convertVerificationRuns(ctx context.Context, tx *sql.Tx, at time.Time) (returnSummary conversionSummary, retErr error) {
	if tx == nil {
		return conversionSummary{}, errors.New("legacy Verification converter transaction is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.id,r.public_id,r.incident_id,i.public_id,i.version,
	r.remediation_plan_id,r.change_request_id,p.incident_id,c.plan_id,r.status,
	r.row_version,r.attempt,r.trigger_type,r.target_revision,r.source_revision,r.image_digest,r.gitops_revision,
r.verification_profile_version,r.verification_profile_hash,r.plan_json,r.common_stability_window_ms,
	r.common_success_since,r.common_window_completed_at,r.completed_at,r.created_at,r.updated_at
	FROM verification_runs r JOIN incidents i ON i.id=r.incident_id
	LEFT JOIN remediation_plans p ON p.id=r.remediation_plan_id
	LEFT JOIN change_requests c ON c.id=r.change_request_id
	WHERE r.domain_schema_version IS NULL ORDER BY r.id FOR UPDATE`)
	if err != nil {
		return conversionSummary{}, err
	}
	defer joinRowsCloseError(&retErr, rows, "close legacy VerificationRun rows")
	runs := make([]legacyVerificationRunRow, 0)
	for rows.Next() {
		var row legacyVerificationRunRow
		var trigger, target, source, image, gitops, profileHash sql.NullString
		var profileVersion, windowMS sql.NullInt64
		var commonStart, commonEnd sql.NullTime
		if err := rows.Scan(&row.id, &row.publicID, &row.incidentID, &row.incidentPublicID, &row.incidentVersion,
			&row.remediationPlanID, &row.changeRequestID, &row.ownerIncidentID, &row.ownerPlanID,
			&row.status, &row.rowVersion, &row.attempt, &trigger, &target, &source, &image, &gitops,
			&profileVersion, &profileHash, &row.planJSON, &windowMS, &commonStart, &commonEnd, &row.completedAt,
			&row.createdAt, &row.updatedAt); err != nil {
			return conversionSummary{}, err
		}
		row.triggerType, row.targetRevision, row.sourceRevision = trigger.String, target.String, source.String
		row.imageDigest, row.gitopsRevision, row.profileHash = image.String, gitops.String, profileHash.String
		if profileVersion.Valid && profileVersion.Int64 > 0 {
			row.profileVersion = uint64(profileVersion.Int64)
		}
		if windowMS.Valid && windowMS.Int64 > 0 {
			row.commonWindow = time.Duration(windowMS.Int64) * time.Millisecond
		}
		if commonStart.Valid {
			value := commonStart.Time.UTC()
			row.commonSuccessSince = &value
		}
		if commonEnd.Valid {
			value := commonEnd.Time.UTC()
			row.commonWindowCompletedAt = &value
		}
		runs = append(runs, row)
	}
	if err := rows.Err(); err != nil {
		return conversionSummary{}, err
	}
	var summary conversionSummary
	for _, row := range runs {
		plan := hydrateLegacyVerificationIdentity(&row)
		checks, err := loadLegacyVerificationChecks(ctx, tx, row.id, plan)
		if err != nil {
			return summary, err
		}
		hydrateLegacyVerificationWindow(&row, checks)
		samples, err := loadLegacyVerificationSamples(ctx, tx, row.id)
		if err != nil {
			return summary, err
		}
		if len(samples) == 0 && row.status == verification.RunPassed {
			samples = synthesizeLegacyVerificationSamples(checks)
		}
		conversion := ConvertVerification(VerificationConversionInput{
			SourceSchemaVersion: 1, TargetSchemaVersion: 3, RunPublicID: row.publicID,
			IncidentPublicID: row.incidentPublicID, OwnershipValid: legacyVerificationOwnershipValid(row, plan),
			CycleNo: 1, IncidentVersion: row.incidentVersion + 1,
			RunVersion: row.rowVersion, Attempt: row.attempt, RunStatus: row.status,
			TriggerType: row.triggerType, TargetRevision: row.targetRevision, SourceRevision: row.sourceRevision,
			ImageDigest: row.imageDigest, GitOpsRevision: row.gitopsRevision, ProfileVersion: row.profileVersion,
			ProfileHash: row.profileHash, PlanJSON: row.planJSON, Checks: checks, Samples: samples,
			CommonWindow: row.commonWindow, CommonSuccessSince: row.commonSuccessSince,
			CommonWindowCompletedAt: row.commonWindowCompletedAt,
		})
		active := row.status == verification.RunPending || row.status == verification.RunRunning
		if err := archiveVerificationRun(ctx, tx, row, checks, samples, conversion, active, at); err != nil {
			return summary, err
		}
		if err := persistConvertedVerificationRun(ctx, tx, row, checks, samples, conversion, active, at); err != nil {
			return summary, err
		}
		status := "failed"
		antiJoin := "not-applicable"
		var targetTaskID uint64
		if conversion.Compatible {
			status = "passed"
		}
		if conversion.Compatible && active {
			payload, err := canonicalTaskPayload(map[string]any{
				"verification_run_id": row.publicID, "cycle_no": 1,
			})
			if err != nil {
				return summary, err
			}
			outcome, err := ensureConversionTask(ctx, tx, conversionTaskSpec{
				IncidentID: row.incidentID, CycleNo: 1, TaskType: asyncjob.TaskVerificationAdvance,
				SubjectType: "verification_run", SubjectID: row.id, Transition: "verification.advance",
				ExpectedSubjectVersion: row.rowVersion + 1, Payload: payload,
				LegacySubjectType: "verification_run", LegacySubjectID: row.id, LegacySourceVersion: row.rowVersion,
				ConverterVersion: VerificationConverterVersion, MigratedLegacy: true,
				MigratedLegacyContext: true, Priority: 60,
			}, at)
			if err != nil {
				return summary, err
			}
			targetTaskID, antiJoin = outcome.TaskID, outcome.AntiJoin
		}
		if _, err := recordConversion(ctx, tx, conversionRecordInput{
			SubjectType: "verification_run", SubjectID: row.id, IncidentID: row.incidentID, CycleNo: 1,
			ConverterVersion: VerificationConverterVersion, InputHash: conversion.InputHash,
			OutputHash: conversion.OutputHash, Status: status, ReasonCode: conversion.ReasonCode,
			TargetTaskID: targetTaskID, AntiJoinResult: antiJoin, SourceSchemaVersion: 1,
			TargetSchemaVersion: 3, SourceTable: "verification_runs+verification_checks+verification_samples",
			TargetTable: "verification_runs+async_tasks", MigratedLegacyContext: true, CreatedAt: at,
		}); err != nil {
			return summary, err
		}
		summary.add(status, antiJoin)
	}
	return summary, nil
}

func hydrateLegacyVerificationIdentity(row *legacyVerificationRunRow) verification.Plan {
	if row == nil || len(row.planJSON) == 0 {
		return verification.Plan{}
	}
	var plan verification.Plan
	if strictJSONDecode(row.planJSON, &plan) != nil {
		return verification.Plan{}
	}
	if row.triggerType == "" {
		switch plan.TriggerType {
		case "post_delivery":
			row.triggerType = "post_delivery"
		case "no_change", "no_change_signal":
			row.triggerType = "no_change_signal"
		}
	}
	if row.targetRevision == "" {
		row.targetRevision = plan.TargetRevision
	}
	if row.sourceRevision == "" {
		row.sourceRevision = plan.SourceRevision
	}
	if row.imageDigest == "" {
		row.imageDigest = plan.ImageDigest
	}
	if row.gitopsRevision == "" {
		row.gitopsRevision = plan.GitOpsRevision
	}
	if row.profileVersion == 0 && plan.ProfileVersion > 0 {
		row.profileVersion = uint64(plan.ProfileVersion)
	}
	if row.profileHash == "" {
		row.profileHash = plan.ProfileHash
	}
	if row.commonWindow == 0 {
		row.commonWindow = verification.V3CommonStabilityWindow
	}
	return plan
}

func legacyVerificationOwnershipValid(row legacyVerificationRunRow, plan verification.Plan) bool {
	if plan.TriggerType != "post_delivery" || !row.remediationPlanID.Valid || !row.changeRequestID.Valid ||
		!row.ownerIncidentID.Valid || !row.ownerPlanID.Valid {
		return false
	}
	return row.remediationPlanID.Int64 > 0 && row.changeRequestID.Int64 > 0 &&
		uint64(row.ownerIncidentID.Int64) == row.incidentID &&
		row.ownerPlanID.Int64 == row.remediationPlanID.Int64
}

func hydrateLegacyVerificationWindow(row *legacyVerificationRunRow, checks []LegacyVerificationCheck) {
	if row == nil || row.status != verification.RunPassed {
		return
	}
	if row.commonSuccessSince == nil {
		latest := time.Time{}
		for _, check := range checks {
			if check.Required && check.ConsecutiveSuccessSince != nil && check.ConsecutiveSuccessSince.After(latest) {
				latest = check.ConsecutiveSuccessSince.UTC()
			}
		}
		if !latest.IsZero() {
			row.commonSuccessSince = &latest
		}
	}
	if row.commonWindowCompletedAt == nil && row.completedAt.Valid {
		value := row.completedAt.Time.UTC()
		row.commonWindowCompletedAt = &value
	}
}

func hydrateLegacyVerificationCheck(item *LegacyVerificationCheck, plan verification.Plan) {
	if item == nil || item.ProfileID != "" || item.TemplateID != "" || item.TemplateVersion != "" ||
		item.SourceIdentity != "" || item.MinSamples != 0 || item.SampleUnit != "" || item.FailureMode != "" {
		return
	}
	for _, spec := range plan.Checks {
		if spec.Type != item.Type {
			continue
		}
		item.ProfileID, item.TemplateID, item.TemplateVersion = spec.ProfileID, spec.TemplateID, spec.TemplateVersion
		item.SourceIdentity, item.Comparison, item.Threshold = spec.SourceIdentity, spec.Comparison, spec.Threshold
		item.InitialDelay, item.MinSamples, item.SampleUnit = spec.InitialDelay, spec.MinSamples, spec.SampleUnit
		item.FailureMode = spec.FailureMode
		return
	}
}

func synthesizeLegacyVerificationSamples(checks []LegacyVerificationCheck) []LegacyVerificationSample {
	result := make([]LegacyVerificationSample, 0, len(checks))
	for _, check := range checks {
		if check.Status != verification.CheckPassed || len(check.Observed) == 0 || !json.Valid(check.Observed) {
			continue
		}
		var observation verification.Observation
		if json.Unmarshal(check.Observed, &observation) != nil {
			continue
		}
		sampledAt := observation.SampledAt.UTC()
		if sampledAt.IsZero() && check.LastCheckedAt != nil {
			sampledAt = check.LastCheckedAt.UTC()
		}
		if sampledAt.IsZero() && check.PassedAt != nil {
			sampledAt = check.PassedAt.UTC()
		}
		if sampledAt.IsZero() {
			continue
		}
		contentHash, canonicalObserved, err := canonicalHashJSON(check.Observed)
		if err != nil {
			continue
		}
		var from, to *time.Time
		if check.ConsecutiveSuccessSince != nil {
			fromValue, toValue := check.ConsecutiveSuccessSince.UTC(), sampledAt
			from, to = &fromValue, &toValue
		}
		result = append(result, LegacyVerificationSample{
			CheckType: check.Type, Sequence: 1, Status: verification.SamplePassed,
			SampleUnit: check.SampleUnit, Count: observation.SampleCount, WindowFrom: from, WindowTo: to,
			SampledAt: sampledAt, ContentHash: contentHash, Observed: canonicalObserved,
		})
	}
	return result
}

func loadLegacyVerificationChecks(ctx context.Context, tx *sql.Tx, runID uint64, plan verification.Plan) (result []LegacyVerificationCheck, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT check_type,status,required_check,subject_json,expected_json,
profile_id,template_id,template_version,comparison,threshold,source_identity,initial_delay_ms,lookback_ms,
poll_interval_ms,timeout_ms,stability_window_ms,min_samples,sample_unit,failure_mode,consecutive_success_since,
observed_json,last_checked_at,passed_at
FROM verification_checks WHERE verification_run_id=? ORDER BY id FOR SHARE`, runID)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close legacy VerificationCheck rows")
	result = make([]LegacyVerificationCheck, 0)
	for rows.Next() {
		var item LegacyVerificationCheck
		var subjectJSON, expectedJSON []byte
		var profileID, templateID, templateVersion, comparison, sourceIdentity, sampleUnit, failureMode sql.NullString
		var threshold sql.NullFloat64
		var initialMS, lookbackMS, pollMS, timeoutMS, stabilityMS, minSamples sql.NullInt64
		var successSince, lastChecked, passedAt sql.NullTime
		var observed []byte
		if err := rows.Scan(&item.Type, &item.Status, &item.Required, &subjectJSON, &expectedJSON,
			&profileID, &templateID, &templateVersion, &comparison, &threshold, &sourceIdentity, &initialMS,
			&lookbackMS, &pollMS, &timeoutMS, &stabilityMS, &minSamples, &sampleUnit, &failureMode,
			&successSince, &observed, &lastChecked, &passedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(subjectJSON, &item.Subject)
		item.Expected = append([]byte(nil), expectedJSON...)
		item.ProfileID, item.TemplateID, item.TemplateVersion = profileID.String, templateID.String, templateVersion.String
		item.Comparison, item.SourceIdentity = verification.Comparison(comparison.String), sourceIdentity.String
		item.SampleUnit, item.FailureMode = sampleUnit.String, verification.FailureMode(failureMode.String)
		if threshold.Valid {
			item.Threshold = threshold.Float64
		}
		item.InitialDelay = nullableMilliseconds(initialMS)
		item.Lookback = nullableMilliseconds(lookbackMS)
		item.PollInterval = nullableMilliseconds(pollMS)
		item.Timeout = nullableMilliseconds(timeoutMS)
		item.StabilityWindow = nullableMilliseconds(stabilityMS)
		if minSamples.Valid && minSamples.Int64 > 0 {
			item.MinSamples = int(minSamples.Int64)
		}
		if successSince.Valid {
			value := successSince.Time.UTC()
			item.ConsecutiveSuccessSince = &value
		}
		item.Observed = append([]byte(nil), observed...)
		if lastChecked.Valid {
			value := lastChecked.Time.UTC()
			item.LastCheckedAt = &value
		}
		if passedAt.Valid {
			value := passedAt.Time.UTC()
			item.PassedAt = &value
		}
		hydrateLegacyVerificationCheck(&item, plan)
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadLegacyVerificationSamples(ctx context.Context, tx *sql.Tx, runID uint64) (result []LegacyVerificationSample, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.check_type,c.sample_unit,s.sample_sequence,s.status,s.observed_json,
s.window_start_at,s.window_end_at,s.sampled_at,s.content_hash
FROM verification_samples s JOIN verification_checks c ON c.id=s.verification_check_id
WHERE s.verification_run_id=? ORDER BY s.verification_check_id,s.sample_sequence FOR SHARE`, runID)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close legacy VerificationSample rows")
	result = make([]LegacyVerificationSample, 0)
	for rows.Next() {
		var item LegacyVerificationSample
		var sampleUnit sql.NullString
		var observed []byte
		var from, to sql.NullTime
		if err := rows.Scan(&item.CheckType, &sampleUnit, &item.Sequence, &item.Status, &observed,
			&from, &to, &item.SampledAt, &item.ContentHash); err != nil {
			return nil, err
		}
		item.SampleUnit = sampleUnit.String
		item.Observed = append([]byte(nil), observed...)
		var observation verification.Observation
		if err := json.Unmarshal(observed, &observation); err != nil {
			item.Count = -1
		} else {
			item.Count = observation.SampleCount
		}
		if from.Valid {
			value := from.Time.UTC()
			item.WindowFrom = &value
		}
		if to.Valid {
			value := to.Time.UTC()
			item.WindowTo = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func archiveVerificationRun(ctx context.Context, tx *sql.Tx, row legacyVerificationRunRow, checks []LegacyVerificationCheck, samples []LegacyVerificationSample, conversion VerificationConversion, active bool, at time.Time) error {
	checksJSON, err := json.Marshal(checks)
	if err != nil {
		return err
	}
	samplesJSON, err := json.Marshal(samples)
	if err != nil {
		return err
	}
	profileHash := legacyVerificationArchiveProfileHash(row)
	profileCanonical, err := canonicalJSON(row.planJSON)
	if err != nil {
		return fmt.Errorf("canonicalize legacy Verification profile id=%d: %w", row.id, err)
	}
	sourceContentHash := canonicalHashFields(
		"legacy-verification-source/v1", row.publicID, fmt.Sprint(row.incidentID), string(row.status),
		fmt.Sprint(row.rowVersion), fmt.Sprint(row.attempt), row.triggerType, row.targetRevision,
		row.sourceRevision, row.imageDigest, row.gitopsRevision, fmt.Sprint(row.profileVersion),
		profileHash, string(profileCanonical), canonicalComponent(checks), canonicalComponent(samples),
		fmt.Sprint(row.commonWindow), canonicalComponent(row.commonSuccessSince), canonicalComponent(row.commonWindowCompletedAt),
	)
	archiveStatus := "failed"
	if conversion.Compatible {
		archiveStatus = "passed"
	} else if active {
		archiveStatus = "cancelled"
	}
	var targetSchema, outputHash any
	if conversion.Compatible {
		targetSchema, outputHash = 3, conversion.OutputHash
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO legacy_verification_archive (
	source_verification_run_id,incident_id,source_status,source_schema_version,target_schema_version,
	trigger_type,target_revision,source_revision,image_digest,gitops_revision,profile_json,profile_hash,source_content_hash,
	checks_json,samples_json,output_hash,converter_version,conversion_status,reason_code,
	source_created_at,source_updated_at,archived_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON DUPLICATE KEY UPDATE source_verification_run_id=VALUES(source_verification_run_id)`, row.id, row.incidentID,
		row.status, 1, targetSchema, nullableString(row.triggerType), nullableString(row.targetRevision),
		nullableString(row.sourceRevision), nullableString(row.imageDigest), nullableString(row.gitopsRevision),
		row.planJSON, profileHash, sourceContentHash, checksJSON, samplesJSON, outputHash, VerificationConverterVersion,
		archiveStatus, conversion.ReasonCode, row.createdAt.UTC(), row.updatedAt.UTC(), at.UTC())
	if err != nil {
		return fmt.Errorf("archive legacy VerificationRun id=%d: %w", row.id, err)
	}
	var archivedProfileHash, archivedContentHash string
	var archivedProfile, archivedChecks, archivedSamples []byte
	var archivedOutput sql.NullString
	var status, reason string
	if err := tx.QueryRowContext(ctx, `SELECT profile_json,profile_hash,source_content_hash,checks_json,samples_json,
	output_hash,conversion_status,reason_code
	FROM legacy_verification_archive WHERE source_verification_run_id=? FOR UPDATE`, row.id).Scan(
		&archivedProfile, &archivedProfileHash, &archivedContentHash, &archivedChecks, &archivedSamples,
		&archivedOutput, &status, &reason); err != nil {
		return err
	}
	archivedProfileCanonical, profileErr := canonicalJSON(archivedProfile)
	archivedChecksCanonical, checksErr := canonicalJSON(archivedChecks)
	archivedSamplesCanonical, samplesErr := canonicalJSON(archivedSamples)
	expectedChecksCanonical, expectedChecksErr := canonicalJSON(checksJSON)
	expectedSamplesCanonical, expectedSamplesErr := canonicalJSON(samplesJSON)
	if profileErr != nil || checksErr != nil || samplesErr != nil || expectedChecksErr != nil || expectedSamplesErr != nil ||
		string(archivedProfileCanonical) != string(profileCanonical) ||
		string(archivedChecksCanonical) != string(expectedChecksCanonical) ||
		string(archivedSamplesCanonical) != string(expectedSamplesCanonical) ||
		archivedProfileHash != profileHash || archivedContentHash != sourceContentHash || status != archiveStatus || reason != conversion.ReasonCode ||
		(conversion.Compatible && archivedOutput.String != conversion.OutputHash) {
		return fmt.Errorf("legacy Verification archive drift id=%d", row.id)
	}
	return nil
}

func persistConvertedVerificationRun(ctx context.Context, tx *sql.Tx, row legacyVerificationRunRow,
	checks []LegacyVerificationCheck, samples []LegacyVerificationSample, conversion VerificationConversion, active bool, at time.Time) error {
	if !conversion.Compatible {
		if !row.changeRequestID.Valid || row.changeRequestID.Int64 <= 0 {
			return fmt.Errorf("incompatible legacy VerificationRun id=%d has no legal post-delivery trigger owner", row.id)
		}
		status := verification.RunCancelled
		var completed any
		if active {
			completed = at.UTC()
		}
		profileVersion := row.profileVersion
		if profileVersion == 0 {
			profileVersion = 1
		}
		result, err := tx.ExecContext(ctx, `UPDATE verification_runs SET status=?,domain_schema_version=3,cycle_no=1,
	v3_status='cancelled',expected_subject_version=row_version+1,migrated_legacy=TRUE,migrated_legacy_context=TRUE,
	trigger_type='post_delivery',verification_profile_version=?,verification_profile_hash=?,
	lease_owner='',lease_expires_at=NULL,heartbeat_at=NULL,completed_at=COALESCE(?,completed_at),
	failure_reason=?,row_version=row_version+1,updated_at=? WHERE id=? AND domain_schema_version IS NULL`,
			status, profileVersion, legacyVerificationArchiveProfileHash(row), completed, conversion.ReasonCode, at.UTC(), row.id)
		if err != nil {
			return fmt.Errorf("cancel incompatible legacy VerificationRun id=%d: %w", row.id, err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("legacy VerificationRun id=%d changed during conversion", row.id)
		}
		return nil
	}
	encodedPlan, err := json.Marshal(conversion.Plan)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE verification_runs SET domain_schema_version=3,cycle_no=1,
	v3_status=?,expected_subject_version=row_version+1,trigger_type=?,target_revision=?,source_revision=?,image_digest=?,
	gitops_revision=?,plan_json=?,verification_profile_version=?,verification_profile_hash=?,
	verification_contract_version=1,verification_profile_id=?,common_stability_window_ms=60000,
	common_success_since=?,common_window_completed_at=?,
	migrated_legacy=TRUE,migrated_legacy_context=TRUE,lease_owner='',lease_expires_at=NULL,heartbeat_at=NULL,
	row_version=row_version+1,updated_at=? WHERE id=? AND domain_schema_version IS NULL`, row.status,
		row.triggerType, conversion.Plan.TargetRevision, conversion.Plan.SourceRevision, conversion.Plan.ImageDigest,
		conversion.Plan.GitOpsRevision, encodedPlan, conversion.Plan.ProfileVersion, conversion.Plan.ProfileHash,
		conversion.Plan.ProfileID, nullableTimePointer(row.commonSuccessSince), nullableTimePointer(row.commonWindowCompletedAt),
		at.UTC(), row.id)
	if err != nil {
		return fmt.Errorf("convert legacy VerificationRun id=%d: %w", row.id, err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("legacy VerificationRun id=%d changed during conversion", row.id)
	}
	if err := persistConvertedVerificationChecks(ctx, tx, row, conversion.Plan, checks, at); err != nil {
		return err
	}
	if err := persistConvertedVerificationSamples(ctx, tx, row, samples, at); err != nil {
		return err
	}
	return nil
}

func legacyVerificationArchiveProfileHash(row legacyVerificationRunRow) string {
	if isSHA256(row.profileHash) {
		return row.profileHash
	}
	if hash, _, err := canonicalHashJSON(row.planJSON); err == nil {
		return hash
	}
	return rawSHA256(row.planJSON)
}

func persistConvertedVerificationChecks(ctx context.Context, tx *sql.Tx, row legacyVerificationRunRow,
	plan verification.Plan, checks []LegacyVerificationCheck, at time.Time) error {
	if len(plan.Checks) != len(checks) {
		return errors.New("converted Verification check projection count differs")
	}
	for _, spec := range plan.Checks {
		var comparison, threshold any
		if spec.Comparison != "" {
			comparison, threshold = string(spec.Comparison), spec.Threshold
		}
		result, err := tx.ExecContext(ctx, `UPDATE verification_checks SET domain_schema_version=3,incident_id=?,cycle_no=1,
check_spec_schema_version=1,profile_id=?,template_id=?,template_version=?,comparison=?,threshold=?,source_identity=?,
initial_delay_ms=?,min_samples=?,sample_unit=?,failure_mode=?,migrated_legacy=TRUE,migrated_legacy_context=TRUE,
updated_at=? WHERE verification_run_id=? AND check_type=? AND domain_schema_version IS NULL`, row.incidentID,
			spec.ProfileID, spec.TemplateID, spec.TemplateVersion, comparison, threshold, spec.SourceIdentity,
			spec.InitialDelay.Milliseconds(), spec.MinSamples, spec.SampleUnit, spec.FailureMode, at.UTC(), row.id, spec.Type)
		if err != nil {
			return fmt.Errorf("convert legacy Verification check run=%d type=%s: %w", row.id, spec.Type, err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("legacy Verification check run=%d type=%s changed during conversion", row.id, spec.Type)
		}
	}
	return nil
}

func persistConvertedVerificationSamples(ctx context.Context, tx *sql.Tx, row legacyVerificationRunRow,
	samples []LegacyVerificationSample, at time.Time) error {
	for _, sample := range samples {
		var checkID uint64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM verification_checks
WHERE verification_run_id=? AND check_type=? AND domain_schema_version=3 FOR UPDATE`, row.id, sample.CheckType).Scan(&checkID); err != nil {
			return fmt.Errorf("load converted Verification check for sample: %w", err)
		}
		observed := sample.Observed
		if len(observed) == 0 {
			encoded, err := json.Marshal(verification.Observation{Status: verification.ObservationAvailable,
				SampleCount: sample.Count, SampledAt: sample.SampledAt.UTC(), QueryValid: true, SourceHealthy: true})
			if err != nil {
				return err
			}
			observed = encoded
		}
		if !json.Valid(observed) || !isSHA256(sample.ContentHash) {
			return errors.New("converted Verification sample archive identity is invalid")
		}
		publicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("phase7a-verification-sample:%d:%s:%d", row.id, sample.CheckType, sample.Sequence))).String()
		_, err := tx.ExecContext(ctx, `INSERT INTO verification_samples (
public_id,domain_schema_version,sample_schema_version,incident_id,cycle_no,verification_run_id,verification_check_id,
sample_sequence,status,observed_json,source_reference,reason_code,window_start_at,window_end_at,sampled_at,content_hash,
migrated_legacy,migrated_legacy_context,created_at)
VALUES (?,3,1,?,1,?,?,?,?,?,'','',?,?,?,?,TRUE,TRUE,?)
ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, publicID, row.incidentID, row.id, checkID, sample.Sequence,
			sample.Status, []byte(observed), nullableTimePointer(sample.WindowFrom), nullableTimePointer(sample.WindowTo),
			sample.SampledAt.UTC(), sample.ContentHash, at.UTC())
		if err != nil {
			return fmt.Errorf("persist converted Verification sample run=%d type=%s sequence=%d: %w",
				row.id, sample.CheckType, sample.Sequence, err)
		}
		var storedRun, storedCheck, storedSequence uint64
		var storedStatus, storedHash string
		var migrated, migratedContext bool
		if err := tx.QueryRowContext(ctx, `SELECT verification_run_id,verification_check_id,sample_sequence,status,
content_hash,migrated_legacy,migrated_legacy_context FROM verification_samples
WHERE verification_check_id=? AND sample_sequence=? FOR UPDATE`, checkID, sample.Sequence).Scan(
			&storedRun, &storedCheck, &storedSequence, &storedStatus, &storedHash, &migrated, &migratedContext); err != nil {
			return err
		}
		if storedRun != row.id || storedCheck != checkID || storedSequence != sample.Sequence ||
			storedStatus != string(sample.Status) || storedHash != sample.ContentHash || !migrated || !migratedContext {
			return fmt.Errorf("converted Verification sample drift run=%d type=%s sequence=%d", row.id, sample.CheckType, sample.Sequence)
		}
	}
	return nil
}

func nullableTimePointer(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullableMilliseconds(value sql.NullInt64) time.Duration {
	if !value.Valid || value.Int64 < 0 {
		return 0
	}
	return time.Duration(value.Int64) * time.Millisecond
}

func legacyVerificationStatusActive(status verification.RunStatus) bool {
	return status == verification.RunPending || status == verification.RunRunning
}
