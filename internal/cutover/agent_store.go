package cutover

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

type legacyAgentRunRow struct {
	id                uint64
	publicID          string
	incidentID        uint64
	incidentPublicID  string
	incidentVersion   uint64
	status            string
	rowVersion        uint64
	checkpoint        []byte
	checkpointVersion uint64
	checkpointSchema  uint32
	checkpointHash    string
	limits            agent.Limits
	usage             agent.Usage
	createdAt         time.Time
	updatedAt         time.Time
	startedAt         sql.NullTime
	deadlineAt        sql.NullTime
}

func convertAgentRuns(ctx context.Context, tx *sql.Tx, at time.Time) (summary conversionSummary, retErr error) {
	if tx == nil {
		return conversionSummary{}, errors.New("legacy Agent converter transaction is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.id,r.public_id,r.incident_id,i.public_id,i.version,r.status,r.row_version,
r.current_checkpoint,r.checkpoint_version,r.checkpoint_schema_version,r.checkpoint_hash,
r.max_steps,r.max_tool_calls,r.max_model_calls,r.token_budget,r.max_evidence_items,r.max_runtime_ms,
r.tool_timeout_ms,r.max_evidence_bytes,r.max_checkpoint_bytes,r.max_step_retries,
r.used_steps,r.used_tool_calls,r.used_model_calls,r.input_tokens,r.output_tokens,r.used_evidence_items,
r.created_at,r.updated_at,r.started_at,r.deadline_at
FROM agent_runs r JOIN incidents i ON i.id=r.incident_id
WHERE r.domain_schema_version IS NULL ORDER BY r.id FOR UPDATE`)
	if err != nil {
		return conversionSummary{}, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close legacy Agent run rows: %w", closeErr))
		}
	}()
	runs := make([]legacyAgentRunRow, 0)
	for rows.Next() {
		var row legacyAgentRunRow
		var checkpoint []byte
		var maxRuntimeMS, toolTimeoutMS int64
		if err := rows.Scan(&row.id, &row.publicID, &row.incidentID, &row.incidentPublicID, &row.incidentVersion,
			&row.status, &row.rowVersion, &checkpoint, &row.checkpointVersion, &row.checkpointSchema,
			&row.checkpointHash, &row.limits.MaxSteps, &row.limits.MaxToolCalls, &row.limits.MaxModelCalls,
			&row.limits.TokenBudget, &row.limits.MaxEvidenceItems, &maxRuntimeMS, &toolTimeoutMS,
			&row.limits.MaxEvidenceBytes, &row.limits.MaxCheckpointSize, &row.limits.MaxStepRetries,
			&row.usage.Steps, &row.usage.ToolCalls, &row.usage.ModelCalls, &row.usage.InputTokens,
			&row.usage.OutputTokens, &row.usage.Evidence, &row.createdAt, &row.updatedAt,
			&row.startedAt, &row.deadlineAt); err != nil {
			return conversionSummary{}, err
		}
		row.checkpoint = append([]byte(nil), checkpoint...)
		row.limits.MaxRuntime = time.Duration(maxRuntimeMS) * time.Millisecond
		row.limits.ToolTimeout = time.Duration(toolTimeoutMS) * time.Millisecond
		runs = append(runs, row)
	}
	if err := rows.Err(); err != nil {
		return conversionSummary{}, err
	}
	for _, row := range runs {
		evidence, err := loadLegacyAgentEvidence(ctx, tx, row, at)
		if err != nil {
			return summary, err
		}
		signatures, err := loadLegacyCompletedToolSignatures(ctx, tx, row.id)
		if err != nil {
			return summary, err
		}
		conversion := ConvertAgentCheckpoint(AgentCheckpointInput{
			SourceSchemaVersion: row.checkpointSchema, TargetSchemaVersion: agentCheckpointTargetSchema,
			RunPublicID: row.publicID, IncidentPublicID: row.incidentPublicID, CycleNo: 1,
			IncidentVersion: row.incidentVersion + 1, CheckpointVersion: row.checkpointVersion,
			CheckpointHash: strings.ToLower(strings.TrimSpace(row.checkpointHash)), Checkpoint: row.checkpoint,
			Limits: row.limits, Usage: row.usage, Evidence: evidence, CompletedSignatures: signatures,
			MaxCheckpointBytes: row.limits.MaxCheckpointSize,
		})
		active := legacyAgentStatusActive(row.status)
		if err := archiveAgentCheckpoint(ctx, tx, row, conversion, active, at); err != nil {
			return summary, err
		}
		if err := persistConvertedAgentRun(ctx, tx, row, conversion, active, at); err != nil {
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
				"mode": conversion.NextMode, "agent_run_id": row.publicID, "cycle_no": 1,
				"basis_checkpoint_version": conversion.State.CheckpointVersion,
			})
			if err != nil {
				return summary, err
			}
			outcome, err := ensureConversionTask(ctx, tx, conversionTaskSpec{
				IncidentID: row.incidentID, CycleNo: 1, TaskType: asyncjob.TaskInvestigationAdvance,
				SubjectType: "agent_run", SubjectID: row.id, Transition: "investigation.step",
				ExpectedSubjectVersion: row.rowVersion + 1, Payload: payload,
				LegacySubjectType: "agent_run", LegacySubjectID: row.id, LegacySourceVersion: row.rowVersion,
				ConverterVersion: AgentCheckpointConverterVersion, MigratedLegacy: true,
				MigratedLegacyContext: true, Priority: 50,
			}, at)
			if err != nil {
				return summary, err
			}
			targetTaskID, antiJoin = outcome.TaskID, outcome.AntiJoin
		}
		if _, err := recordConversion(ctx, tx, conversionRecordInput{
			SubjectType: "agent_run", SubjectID: row.id, IncidentID: row.incidentID, CycleNo: 1,
			ConverterVersion: AgentCheckpointConverterVersion, InputHash: conversion.InputHash,
			OutputHash: conversion.OutputHash, Status: status, ReasonCode: conversion.ReasonCode,
			TargetTaskID: targetTaskID, AntiJoinResult: antiJoin, SourceSchemaVersion: uint64(row.checkpointSchema),
			TargetSchemaVersion: uint64(agentCheckpointTargetSchema), SourceTable: "agent_runs",
			TargetTable: "agent_runs+async_tasks", MigratedLegacyContext: true, CreatedAt: at,
		}); err != nil {
			return summary, err
		}
		summary.add(status, antiJoin)
	}
	return summary, nil
}

func loadLegacyAgentEvidence(ctx context.Context, tx *sql.Tx, run legacyAgentRunRow, at time.Time) (result []LegacyAgentEvidence, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.public_id,e.type,e.valid,e.content_hash,e.collected_at,e.migrated_legacy,
i.public_id,e.cycle_no
FROM evidence_items e JOIN incidents i ON i.id=e.incident_id
WHERE e.agent_run_id=? ORDER BY e.id FOR SHARE`, run.id)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close legacy Agent evidence rows: %w", closeErr))
		}
	}()
	result = make([]LegacyAgentEvidence, 0)
	freshFrom := run.createdAt
	if run.startedAt.Valid {
		freshFrom = run.startedAt.Time
	}
	freshTo := run.updatedAt
	if freshTo.IsZero() {
		freshTo = at
	}
	if run.deadlineAt.Valid && run.deadlineAt.Time.Before(freshTo) {
		freshTo = run.deadlineAt.Time
	}
	for rows.Next() {
		var item LegacyAgentEvidence
		var cycle sql.NullInt64
		if err := rows.Scan(&item.PublicID, &item.FactType, &item.Valid, &item.ContentHash, &item.CollectedAt,
			&item.MigratedLegacy, &item.IncidentID, &cycle); err != nil {
			return nil, err
		}
		if cycle.Valid && cycle.Int64 > 0 {
			item.CycleNo = uint64(cycle.Int64)
		}
		item.Fresh = !item.CollectedAt.Before(freshFrom) && !item.CollectedAt.After(freshTo)
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadLegacyCompletedToolSignatures(ctx context.Context, tx *sql.Tx, runID uint64) (result []string, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT selected_tool,arguments_hash FROM agent_steps
WHERE agent_run_id=? AND status='COMPLETED' ORDER BY sequence,id FOR SHARE`, runID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close legacy completed tool signature rows: %w", closeErr))
		}
	}()
	result = make([]string, 0)
	for rows.Next() {
		var tool, argumentsHash string
		if err := rows.Scan(&tool, &argumentsHash); err != nil {
			return nil, err
		}
		if strings.TrimSpace(tool) == "" || strings.TrimSpace(argumentsHash) == "" {
			return nil, errors.New("completed legacy AgentStep has no tool signature identity")
		}
		result = append(result, canonicalHashFields("legacy-tool-signature/v1", tool, argumentsHash))
	}
	return result, rows.Err()
}

func archiveAgentCheckpoint(ctx context.Context, tx *sql.Tx, row legacyAgentRunRow, conversion AgentCheckpointConversion, active bool, at time.Time) error {
	archiveStatus := "failed"
	if conversion.Compatible {
		archiveStatus = "passed"
	} else if active {
		archiveStatus = "cancelled"
	}
	var targetSchema, targetVersion, targetJSON, targetHash any
	if conversion.Compatible {
		targetSchema, targetVersion, targetJSON, targetHash = agentCheckpointTargetSchema, conversion.State.CheckpointVersion,
			[]byte(conversion.Output), conversion.OutputHash
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_agent_checkpoint_archive (
source_agent_run_id,incident_id,source_status,source_schema_version,source_checkpoint_version,
checkpoint_json,checkpoint_hash,source_checkpoint_canonical_hash,target_schema_version,target_checkpoint_version,target_checkpoint_json,
target_checkpoint_hash,converter_version,conversion_status,reason_code,source_created_at,source_updated_at,archived_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE source_agent_run_id=VALUES(source_agent_run_id)`, row.id, row.incidentID, row.status,
		row.checkpointSchema, row.checkpointVersion, nullableJSON(row.checkpoint), row.checkpointHash,
		nullableString(conversion.CheckpointCanonicalHash), targetSchema,
		targetVersion, targetJSON, targetHash, AgentCheckpointConverterVersion, archiveStatus,
		conversion.ReasonCode, row.createdAt.UTC(), row.updatedAt.UTC(), at.UTC())
	if err != nil {
		return fmt.Errorf("archive legacy AgentRun id=%d: %w", row.id, err)
	}
	var checkpointHash, canonicalHash, outputHash sql.NullString
	var status, reason string
	if err := tx.QueryRowContext(ctx, `SELECT checkpoint_hash,source_checkpoint_canonical_hash,target_checkpoint_hash,conversion_status,reason_code
FROM legacy_agent_checkpoint_archive WHERE source_agent_run_id=? FOR UPDATE`, row.id).Scan(&checkpointHash, &canonicalHash, &outputHash, &status, &reason); err != nil {
		return err
	}
	if checkpointHash.String != row.checkpointHash || status != archiveStatus || reason != conversion.ReasonCode ||
		(conversion.CheckpointCanonicalHash != "" && canonicalHash.String != conversion.CheckpointCanonicalHash) ||
		(conversion.Compatible && outputHash.String != conversion.OutputHash) {
		return fmt.Errorf("legacy Agent checkpoint archive drift id=%d", row.id)
	}
	return nil
}

func persistConvertedAgentRun(ctx context.Context, tx *sql.Tx, row legacyAgentRunRow, conversion AgentCheckpointConversion, active bool, at time.Time) error {
	v3Status := legacyAgentTerminalV3Status(row.status)
	status := row.status
	checkpoint, checkpointHash, checkpointSchema := any(nil), any(row.checkpointHash), any(row.checkpointSchema)
	completedAt := any(nil)
	failureCode, failureSummary := any(nil), any(nil)
	if conversion.Compatible {
		checkpoint, checkpointHash, checkpointSchema = []byte(conversion.Output), conversion.OutputHash, agentCheckpointTargetSchema
		if active {
			v3Status = strings.ToLower(strings.TrimSpace(row.status))
		}
	} else if active {
		status, v3Status = "CANCELLED", "cancelled"
		completedAt = at.UTC()
		failureCode, failureSummary = conversion.ReasonCode, "Legacy Agent checkpoint was archived; a new V3 Run must be created"
	}
	if v3Status == "" {
		v3Status = "cancelled"
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET domain_schema_version=3,v3_status=?,cycle_no=1,
expected_incident_version=?,status=?,current_checkpoint=?,checkpoint_schema_version=?,checkpoint_hash=?,
migrated_legacy=TRUE,migrated_legacy_context=TRUE,lease_owner='',lease_expires_at=NULL,heartbeat_at=NULL,
completed_at=COALESCE(?,completed_at),failure_code=COALESCE(?,failure_code),
failure_summary=COALESCE(?,failure_summary),row_version=row_version+1,updated_at=?
WHERE id=? AND domain_schema_version IS NULL`, v3Status, row.incidentVersion+1, status, checkpoint,
		checkpointSchema, checkpointHash, completedAt, failureCode, failureSummary, at.UTC(), row.id)
	if err != nil {
		return fmt.Errorf("convert legacy AgentRun id=%d: %w", row.id, err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("legacy AgentRun id=%d changed during conversion", row.id)
	}
	return nil
}

func legacyAgentStatusActive(status string) bool {
	status = strings.ToUpper(strings.TrimSpace(status))
	return status == "PENDING" || status == "RUNNING"
}

func legacyAgentTerminalV3Status(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETED":
		return "completed"
	case "FAILED":
		return "failed"
	case "CANCELLED":
		return "cancelled"
	default:
		return ""
	}
}

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func rawSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
