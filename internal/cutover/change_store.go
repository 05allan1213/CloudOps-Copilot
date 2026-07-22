package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

type legacyChangeRequestRow struct {
	id              uint64
	publicID        string
	planID          uint64
	incidentID      uint64
	status          string
	rowVersion      uint64
	repository      string
	baseRevision    string
	headBranch      string
	commitSHA       string
	prNumber        int64
	prURL           string
	prState         string
	mergedCommitSHA string
	createdAt       time.Time
	updatedAt       time.Time
	hasApproval     bool
	snapshot        []byte
	snapshotHash    string
}

func convertChangeRequests(ctx context.Context, tx *sql.Tx, reconciler LegacyChangeReconciler, at time.Time) (conversionSummary, error) {
	if tx == nil {
		return conversionSummary{}, errors.New("legacy ChangeRequest converter transaction is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.public_id,c.plan_id,p.incident_id,c.status,c.row_version,
c.repository,c.base_revision,c.head_branch,c.commit_sha,c.pr_number,c.pr_url,c.pr_state,c.merged_commit_sha,
c.created_at,c.updated_at,EXISTS(SELECT 1 FROM remediation_approvals a WHERE a.plan_id=c.plan_id),
CAST(JSON_OBJECT('id',c.id,'public_id',c.public_id,'plan_id',c.plan_id,'incident_id',p.incident_id,
'status',c.status,'repository',c.repository,'base_revision',c.base_revision,'head_branch',c.head_branch,
'commit_sha',c.commit_sha,'pr_number',c.pr_number,'pr_url',c.pr_url,'pr_state',c.pr_state,
'merged_commit_sha',c.merged_commit_sha,'ci_status',c.ci_status,'idempotency_key',c.idempotency_key,
'attempts',c.attempts,'failure_code',c.failure_code,'row_version',c.row_version,
'created_at',c.created_at,'updated_at',c.updated_at) AS CHAR)
FROM change_requests c JOIN remediation_plans p ON p.id=c.plan_id
WHERE c.domain_schema_version IS NULL ORDER BY c.id FOR UPDATE`)
	if err != nil {
		return conversionSummary{}, err
	}
	defer rows.Close()
	items := make([]legacyChangeRequestRow, 0)
	for rows.Next() {
		var row legacyChangeRequestRow
		if err := rows.Scan(&row.id, &row.publicID, &row.planID, &row.incidentID, &row.status,
			&row.rowVersion, &row.repository, &row.baseRevision, &row.headBranch, &row.commitSHA,
			&row.prNumber, &row.prURL, &row.prState, &row.mergedCommitSHA, &row.createdAt,
			&row.updatedAt, &row.hasApproval, &row.snapshot); err != nil {
			return conversionSummary{}, err
		}
		hash, canonical, err := canonicalHashJSON(row.snapshot)
		if err != nil {
			return conversionSummary{}, fmt.Errorf("canonicalize legacy ChangeRequest id=%d: %w", row.id, err)
		}
		row.snapshot, row.snapshotHash = canonical, hash
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return conversionSummary{}, err
	}
	var summary conversionSummary
	for _, row := range items {
		artifact := LegacyExternalArtifact{Repository: row.repository, PullRequest: row.prNumber, URL: row.prURL,
			BaseRevision: strings.ToLower(strings.TrimSpace(row.baseRevision)), HeadBranch: row.headBranch,
			HeadRevision: strings.ToLower(strings.TrimSpace(row.commitSHA)), State: strings.ToLower(strings.TrimSpace(row.prState)),
			MergedCommitSHA: strings.ToLower(strings.TrimSpace(row.mergedCommitSHA))}
		var reconciled *ReconciledPullRequest
		if legacyArtifactMentionsPR(artifact) {
			if reconciler == nil {
				return summary, fmt.Errorf("legacy ChangeRequest id=%d requires configured read-only GitHub reconciliation", row.id)
			}
			observed, err := reconciler.ReconcilePullRequest(ctx, artifact)
			if err != nil {
				return summary, fmt.Errorf("read-only reconcile legacy ChangeRequest id=%d: %w", row.id, err)
			}
			reconciled = &observed
		}
		conversion := ConvertLegacyChange(LegacyChangeInput{SubjectID: row.id, SubjectVersion: row.rowVersion,
			IncidentID: row.incidentID, CycleNo: 1, SourceStatus: row.status,
			HasLegacyPlan: true, HasApproval: row.hasApproval,
			Artifact: artifact, Reconciled: reconciled})
		if conversion.Class == ChangeAmbiguousExternal {
			return summary, fmt.Errorf("legacy ChangeRequest id=%d external state is ambiguous: %s", row.id, conversion.ReasonCode)
		}
		if err := archiveChangeRequest(ctx, tx, row, artifact, reconciled, conversion, at); err != nil {
			return summary, err
		}
		if err := persistConvertedChangeRequest(ctx, tx, row, artifact, reconciled, conversion, at); err != nil {
			return summary, err
		}
		status := "failed"
		antiJoin := "not-applicable"
		var targetTaskID uint64
		if conversion.Compatible {
			status = "passed"
		}
		if conversion.CreateObserve {
			payload, err := canonicalTaskPayload(map[string]any{
				"change_request_id": row.publicID, "phase": "observe", "legacy_read_only": true,
			})
			if err != nil {
				return summary, err
			}
			outcome, err := ensureConversionTask(ctx, tx, conversionTaskSpec{
				IncidentID: row.incidentID, CycleNo: 1, TaskType: asyncjob.TaskDeliveryObserve,
				SubjectType: "change_request", SubjectID: row.id, Transition: "delivery.observe",
				ExpectedSubjectVersion: row.rowVersion + 1, Payload: payload,
				LegacySubjectType: "change_request", LegacySubjectID: row.id, LegacySourceVersion: row.rowVersion,
				ConverterVersion: ChangeRequestConverterVersion, MigratedLegacy: true,
				MigratedLegacyContext: true, Priority: 40,
			}, at)
			if err != nil {
				return summary, err
			}
			targetTaskID, antiJoin = outcome.TaskID, outcome.AntiJoin
		}
		if _, err := recordConversion(ctx, tx, conversionRecordInput{
			SubjectType: "change_request", SubjectID: row.id, IncidentID: row.incidentID, CycleNo: 1,
			ConverterVersion: ChangeRequestConverterVersion, InputHash: conversion.InputHash,
			OutputHash: conversion.OutputHash, Status: status, ReasonCode: conversion.ReasonCode,
			TargetTaskID: targetTaskID, AntiJoinResult: antiJoin, SourceSchemaVersion: 1, TargetSchemaVersion: 3,
			SourceTable: "change_requests+remediation_plans+remediation_approvals",
			TargetTable: "change_requests+async_tasks", MigratedLegacyContext: true, CreatedAt: at,
		}); err != nil {
			return summary, err
		}
		summary.add(status, antiJoin)
	}
	return summary, nil
}

func legacyArtifactMentionsPR(artifact LegacyExternalArtifact) bool {
	return artifact.PullRequest > 0 || strings.TrimSpace(artifact.URL) != "" || strings.TrimSpace(artifact.State) != "" || strings.TrimSpace(artifact.MergedCommitSHA) != ""
}

func archiveChangeRequest(ctx context.Context, tx *sql.Tx, row legacyChangeRequestRow, artifact LegacyExternalArtifact, reconciled *ReconciledPullRequest, conversion LegacyChangeConversion, at time.Time) error {
	status := "failed"
	if conversion.Compatible {
		status = "passed"
	}
	observed := artifact
	if reconciled != nil {
		observed.Repository, observed.PullRequest, observed.URL = reconciled.Repository, reconciled.PullRequest, reconciled.URL
		observed.BaseRevision, observed.HeadBranch, observed.HeadRevision = reconciled.BaseRevision, reconciled.HeadBranch, reconciled.HeadRevision
		observed.State, observed.MergedCommitSHA = strings.ToLower(reconciled.State), strings.ToLower(reconciled.MergedCommitSHA)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_change_request_archive (
source_change_request_id,incident_id,source_status,snapshot_json,snapshot_hash,source_content_hash,
repository,pr_number,pr_url,base_revision,head_branch,head_revision,pr_state,merged_commit_sha,
external_state,conversion_status,reason_code,source_created_at,source_updated_at,archived_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE source_change_request_id=VALUES(source_change_request_id)`, row.id, row.incidentID,
		row.status, row.snapshot, row.snapshotHash, row.snapshotHash, observed.Repository, observed.PullRequest,
		observed.URL, observed.BaseRevision, observed.HeadBranch, observed.HeadRevision, observed.State,
		observed.MergedCommitSHA, conversion.Class, status, conversion.ReasonCode, row.createdAt.UTC(),
		row.updatedAt.UTC(), at.UTC())
	if err != nil {
		return fmt.Errorf("archive legacy ChangeRequest id=%d: %w", row.id, err)
	}
	var archivedHash, archivedState, archivedStatus, reason string
	if err := tx.QueryRowContext(ctx, `SELECT source_content_hash,external_state,conversion_status,reason_code
FROM legacy_change_request_archive WHERE source_change_request_id=? FOR UPDATE`, row.id).Scan(
		&archivedHash, &archivedState, &archivedStatus, &reason); err != nil {
		return err
	}
	if archivedHash != row.snapshotHash || archivedState != string(conversion.Class) || archivedStatus != status || reason != conversion.ReasonCode {
		return fmt.Errorf("legacy ChangeRequest archive drift id=%d", row.id)
	}
	return nil
}

func persistConvertedChangeRequest(ctx context.Context, tx *sql.Tx, row legacyChangeRequestRow, artifact LegacyExternalArtifact, reconciled *ReconciledPullRequest, conversion LegacyChangeConversion, at time.Time) error {
	status, v3Status := "failed", "superseded"
	writePhase := any(nil)
	prState, mergedSHA := artifact.State, artifact.MergedCommitSHA
	var externalStarted, externalMarker any
	failureCode, failureReason := conversion.ReasonCode, "Legacy ChangeRequest was safely converged without authorizing a V3 external write"
	if strings.TrimSpace(artifact.HeadBranch) != "" || strings.TrimSpace(artifact.HeadRevision) != "" || legacyArtifactMentionsPR(artifact) {
		externalStarted = row.createdAt.UTC()
		externalMarker = canonicalHashFields("legacy-external-artifact/v1", row.repository, fmt.Sprint(row.prNumber), row.headBranch, row.commitSHA)
	}
	if conversion.Class == ChangeApprovalOnly {
		status, v3Status = "delivery_cancelled", "cancelled"
	}
	if conversion.Compatible && reconciled != nil && conversion.CreateObserve {
		status, v3Status, writePhase = "pr_created", "pr_open", "observe"
		prState, mergedSHA = strings.ToLower(reconciled.State), strings.ToLower(reconciled.MergedCommitSHA)
		failureCode, failureReason = "", ""
		if reconciled.Merged || strings.EqualFold(reconciled.State, "merged") {
			status, v3Status = "merged", "merged"
		}
	} else if conversion.Compatible && reconciled != nil {
		status, v3Status = row.status, "superseded"
		prState, mergedSHA = strings.ToLower(reconciled.State), strings.ToLower(reconciled.MergedCommitSHA)
		failureCode = "legacy_terminal_external_artifact_archived"
		failureReason = "Terminal legacy ChangeRequest external identity was reconciled and archived without creating a Task"
	}
	logical := canonicalHashFields("legacy-change-request/v2", fmt.Sprint(row.id), fmt.Sprint(row.rowVersion), ChangeRequestConverterVersion)
	result, err := tx.ExecContext(ctx, `UPDATE change_requests SET domain_schema_version=3,incident_id=?,cycle_no=1,
v3_status=?,write_phase=?,status=?,expected_subject_version=row_version+1,logical_operation_key=?,
external_write_started_at=?,external_write_marker=?,pr_state=?,merged_commit_sha=?,migrated_legacy=TRUE,migrated_legacy_context=TRUE,
lease_owner='',lease_expires_at=NULL,heartbeat_at=NULL,failure_code=?,failure_reason=?,
row_version=row_version+1,updated_at=? WHERE id=? AND domain_schema_version IS NULL`, row.incidentID,
		v3Status, writePhase, status, logical, externalStarted, externalMarker, prState, mergedSHA,
		failureCode, failureReason, at.UTC(), row.id)
	if err != nil {
		return fmt.Errorf("convert legacy ChangeRequest id=%d: %w", row.id, err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("legacy ChangeRequest id=%d changed during conversion", row.id)
	}
	return nil
}
