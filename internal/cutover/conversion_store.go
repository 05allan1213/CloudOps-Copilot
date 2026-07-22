package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/google/uuid"
)

type conversionSummary struct {
	SubjectDerived   uint64
	ConversionFailed uint64
	TaskCreated      uint64
	ExistingTask     uint64
	AntiJoinSkipped  uint64
}

func (s *conversionSummary) add(status, antiJoin string) {
	s.SubjectDerived++
	if status == "failed" {
		s.ConversionFailed++
	}
	switch antiJoin {
	case "created":
		s.TaskCreated++
	case "existing-target-task":
		s.ExistingTask++
	case "anti-join-skipped":
		s.AntiJoinSkipped++
	}
}

type conversionTaskSpec struct {
	IncidentID             uint64
	CycleNo                uint64
	TaskType               asyncjob.TaskType
	SubjectType            string
	SubjectID              uint64
	Transition             string
	ExpectedSubjectVersion uint64
	Payload                json.RawMessage
	LegacySubjectType      string
	LegacySubjectID        uint64
	LegacySourceVersion    uint64
	ConverterVersion       string
	Priority               int
	MaxAttempts            uint32
	CheckpointSchema       *uint32
	CheckpointVersion      *uint64
	CheckpointHash         string
	Checkpoint             json.RawMessage
	MigratedLegacy         bool
	MigratedLegacyContext  bool
}

type conversionTaskOutcome struct {
	TaskID   uint64
	AntiJoin string
}

func ensureConversionTask(ctx context.Context, tx *sql.Tx, spec conversionTaskSpec, at time.Time) (conversionTaskOutcome, error) {
	if tx == nil || spec.CycleNo == 0 || spec.CycleNo > math.MaxUint32 || spec.LegacySubjectID == 0 || spec.LegacySourceVersion == 0 || strings.TrimSpace(spec.ConverterVersion) == "" {
		return conversionTaskOutcome{}, errors.New("legacy conversion task identity is incomplete")
	}
	if !spec.MigratedLegacyContext {
		return conversionTaskOutcome{}, errors.New("legacy conversion task must preserve migrated legacy context")
	}
	checkpointPresent := spec.CheckpointSchema != nil || spec.CheckpointVersion != nil || spec.CheckpointHash != "" || len(spec.Checkpoint) != 0
	if checkpointPresent && (spec.CheckpointSchema == nil || spec.CheckpointVersion == nil || *spec.CheckpointSchema == 0 ||
		*spec.CheckpointVersion == 0 || !isSHA256(spec.CheckpointHash) || len(spec.Checkpoint) == 0 || !json.Valid(spec.Checkpoint)) {
		return conversionTaskOutcome{}, errors.New("legacy conversion task checkpoint identity is incomplete")
	}
	if spec.MaxAttempts == 0 {
		spec.MaxAttempts = 8
	}
	dedupe, logical := conversionTaskKeys(spec)
	newTask := asyncjob.NewTask{
		IncidentID: spec.IncidentID, CycleNo: uint32(spec.CycleNo), Type: spec.TaskType,
		SubjectType: spec.SubjectType, SubjectID: spec.SubjectID, Transition: spec.Transition,
		ExpectedSubjectVersion: spec.ExpectedSubjectVersion, PayloadSchemaVersion: 1,
		Payload: spec.Payload, DedupeKey: dedupe, LogicalOperationKey: logical,
		MigratedLegacy: spec.MigratedLegacy, MigratedLegacyContext: spec.MigratedLegacyContext,
		Priority: spec.Priority, MaxAttempts: spec.MaxAttempts,
	}
	if err := newTask.Validate(); err != nil {
		return conversionTaskOutcome{}, fmt.Errorf("validate legacy conversion task: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT id,queue,migrated_legacy,migrated_legacy_context,status,
expected_subject_version,payload_schema_version,payload_json,dedupe_key,logical_operation_key,max_attempts,
checkpoint_schema_version,checkpoint_version,checkpoint_hash,checkpoint_json
FROM async_tasks
WHERE incident_id=? AND cycle_no=? AND task_type=? AND subject_type=? AND subject_id=? AND transition=?
  AND replay_generation=0
ORDER BY id FOR UPDATE`, spec.IncidentID, spec.CycleNo, spec.TaskType, spec.SubjectType, spec.SubjectID, spec.Transition)
	if err != nil {
		return conversionTaskOutcome{}, err
	}
	defer rows.Close()
	type existingTask struct {
		id                uint64
		queue             asyncjob.Queue
		migrated          bool
		migratedContext   bool
		status            asyncjob.Status
		expectedVersion   uint64
		payloadSchema     uint32
		payload           []byte
		dedupe            string
		logical           sql.NullString
		maxAttempts       uint32
		checkpointSchema  sql.NullInt64
		checkpointVersion sql.NullInt64
		checkpointHash    sql.NullString
		checkpoint        []byte
	}
	var existing []existingTask
	for rows.Next() {
		var item existingTask
		if err := rows.Scan(&item.id, &item.queue, &item.migrated, &item.migratedContext, &item.status,
			&item.expectedVersion, &item.payloadSchema, &item.payload, &item.dedupe, &item.logical, &item.maxAttempts,
			&item.checkpointSchema, &item.checkpointVersion, &item.checkpointHash, &item.checkpoint); err != nil {
			return conversionTaskOutcome{}, err
		}
		existing = append(existing, item)
	}
	if err := rows.Err(); err != nil {
		return conversionTaskOutcome{}, err
	}
	if len(existing) > 1 {
		return conversionTaskOutcome{}, fmt.Errorf("legacy subject %s/%d has %d target Tasks", spec.LegacySubjectType, spec.LegacySubjectID, len(existing))
	}
	if len(existing) == 1 {
		item := existing[0]
		queue, _ := asyncjob.QueueForTaskType(spec.TaskType)
		if item.queue != queue || !validExistingTaskStatus(item.status) || item.payloadSchema != 1 || item.maxAttempts == 0 {
			return conversionTaskOutcome{}, errors.New("existing target Task runtime identity is invalid")
		}
		if item.expectedVersion != spec.ExpectedSubjectVersion {
			return conversionTaskOutcome{}, fmt.Errorf("existing target Task expected_subject_version=%d want=%d", item.expectedVersion, spec.ExpectedSubjectVersion)
		}
		existingPayloadHash, _, existingPayloadErr := canonicalHashJSON(item.payload)
		expectedPayloadHash, _, expectedPayloadErr := canonicalHashJSON(spec.Payload)
		if existingPayloadErr != nil || expectedPayloadErr != nil || existingPayloadHash != expectedPayloadHash {
			return conversionTaskOutcome{}, errors.New("existing target Task payload differs from converted payload")
		}
		if spec.CheckpointSchema != nil {
			existingCheckpointHash, _, existingCheckpointErr := canonicalHashJSON(item.checkpoint)
			expectedCheckpointHash, _, expectedCheckpointErr := canonicalHashJSON(spec.Checkpoint)
			if !item.checkpointSchema.Valid || !item.checkpointVersion.Valid || !item.checkpointHash.Valid ||
				uint32(item.checkpointSchema.Int64) != *spec.CheckpointSchema || uint64(item.checkpointVersion.Int64) != *spec.CheckpointVersion ||
				item.checkpointHash.String != spec.CheckpointHash || existingCheckpointErr != nil || expectedCheckpointErr != nil ||
				existingCheckpointHash != expectedCheckpointHash {
				return conversionTaskOutcome{}, errors.New("existing target Task checkpoint differs from converted checkpoint")
			}
		}
		antiJoin := "existing-target-task"
		converterDedupe := item.dedupe == dedupe
		converterLogical := item.logical.Valid && item.logical.String == logical
		if converterDedupe != converterLogical {
			return conversionTaskOutcome{}, errors.New("existing target Task has a partial conversion identity")
		}
		if converterDedupe {
			if item.migrated != spec.MigratedLegacy || item.migratedContext != spec.MigratedLegacyContext {
				return conversionTaskOutcome{}, errors.New("existing migrated target Task conversion identity differs")
			}
			antiJoin = "anti-join-skipped"
		} else if item.migrated {
			return conversionTaskOutcome{}, errors.New("existing migrated target Task is not bound to this converter")
		}
		return conversionTaskOutcome{TaskID: item.id, AntiJoin: antiJoin}, nil
	}

	queue, _ := asyncjob.QueueForTaskType(spec.TaskType)
	var checkpointSchema, checkpointVersion any
	var checkpointHash, checkpoint any
	if spec.CheckpointSchema != nil {
		checkpointSchema, checkpointVersion = *spec.CheckpointSchema, *spec.CheckpointVersion
		checkpointHash, checkpoint = spec.CheckpointHash, []byte(spec.Checkpoint)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO async_tasks (
public_id,incident_id,cycle_no,queue,task_type,subject_type,subject_id,transition,expected_subject_version,
payload_schema_version,payload_json,checkpoint_schema_version,checkpoint_version,checkpoint_hash,checkpoint_json,
dedupe_key,replay_generation,logical_operation_key,migrated_legacy,migrated_legacy_context,status,priority,
available_at,attempt,max_attempts,lease_generation,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,1,?,?,?,?,?,?,0,?,?,?,'ready',?,?,0,?,0,?,?)`, uuid.NewString(),
		spec.IncidentID, spec.CycleNo, queue, spec.TaskType, spec.SubjectType, spec.SubjectID, spec.Transition,
		spec.ExpectedSubjectVersion, []byte(spec.Payload), checkpointSchema, checkpointVersion, checkpointHash,
		checkpoint, dedupe, logical, spec.MigratedLegacy, spec.MigratedLegacyContext,
		spec.Priority, at.UTC(), spec.MaxAttempts, at.UTC(), at.UTC())
	if err != nil {
		return conversionTaskOutcome{}, fmt.Errorf("insert legacy conversion Task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return conversionTaskOutcome{}, fmt.Errorf("read legacy conversion Task id: %w", err)
	}
	return conversionTaskOutcome{TaskID: uint64(id), AntiJoin: "created"}, nil
}

func validExistingTaskStatus(status asyncjob.Status) bool {
	switch status {
	case asyncjob.StatusReady, asyncjob.StatusRunning, asyncjob.StatusSucceeded, asyncjob.StatusDead, asyncjob.StatusCancelled:
		return true
	default:
		return false
	}
}

func conversionTaskKeys(spec conversionTaskSpec) (string, string) {
	dedupe := canonicalHashFields(
		"phase7a-task-dedupe/v2",
		spec.LegacySubjectType,
		fmt.Sprint(spec.LegacySubjectID),
		fmt.Sprint(spec.LegacySourceVersion),
		fmt.Sprint(spec.CycleNo),
		spec.Transition,
		spec.ConverterVersion,
	)
	logical := canonicalHashFields(
		"phase7a-logical-operation/v2",
		spec.LegacySubjectType,
		fmt.Sprint(spec.LegacySubjectID),
		fmt.Sprint(spec.CycleNo),
		spec.Transition,
		spec.ConverterVersion,
	)
	return dedupe, logical
}

type conversionRecordInput struct {
	SubjectType           string
	SubjectID             uint64
	IncidentID            uint64
	CycleNo               uint64
	ConverterVersion      string
	InputHash             string
	OutputHash            string
	Status                string
	ReasonCode            string
	TargetTaskID          uint64
	AntiJoinResult        string
	SourceSchemaVersion   uint64
	TargetSchemaVersion   uint64
	SourceTable           string
	TargetTable           string
	MigratedLegacyContext bool
	CreatedAt             time.Time
}

func recordConversion(ctx context.Context, tx *sql.Tx, input conversionRecordInput) (uint64, error) {
	if tx == nil || input.SubjectID == 0 || input.IncidentID == 0 || input.CycleNo == 0 ||
		!isSHA256(input.InputHash) || !isSHA256(input.OutputHash) || strings.TrimSpace(input.ConverterVersion) == "" ||
		strings.TrimSpace(input.SourceTable) == "" || strings.TrimSpace(input.TargetTable) == "" {
		return 0, errors.New("legacy conversion record identity is incomplete")
	}
	if input.Status != "passed" && input.Status != "failed" && input.Status != "skipped" {
		return 0, errors.New("legacy conversion record status is invalid")
	}
	switch input.AntiJoinResult {
	case "created", "existing-target-task", "anti-join-skipped", "not-applicable":
	default:
		return 0, errors.New("legacy conversion anti-join result is invalid")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,attempt,input_hash,output_hash,status,reason_code,
COALESCE(target_task_id,0),anti_join_result,source_schema_version,target_schema_version,source_table,target_table,
migrated_legacy_context
FROM legacy_conversion_records
WHERE subject_type=? AND subject_id=? AND converter_version=?
ORDER BY attempt FOR UPDATE`, input.SubjectType, input.SubjectID, input.ConverterVersion)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var previousID, previousAttempt uint64
	for rows.Next() {
		var id, attempt, taskID, sourceSchema, targetSchema uint64
		var inputHash, outputHash, status, reason, antiJoin, sourceTable, targetTable string
		var migratedContext bool
		if err := rows.Scan(&id, &attempt, &inputHash, &outputHash, &status, &reason, &taskID, &antiJoin,
			&sourceSchema, &targetSchema, &sourceTable, &targetTable, &migratedContext); err != nil {
			return 0, err
		}
		previousID, previousAttempt = id, attempt
		if inputHash == input.InputHash && outputHash == input.OutputHash && status == input.Status && reason == input.ReasonCode &&
			taskID == input.TargetTaskID && antiJoin == input.AntiJoinResult && sourceSchema == input.SourceSchemaVersion &&
			targetSchema == input.TargetSchemaVersion && sourceTable == input.SourceTable && targetTable == input.TargetTable &&
			migratedContext == input.MigratedLegacyContext && input.Status != "failed" {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	attempt := previousAttempt + 1
	if attempt == 0 {
		attempt = 1
	}
	var previous any
	if previousID != 0 {
		previous = previousID
	}
	var targetTask any
	if input.TargetTaskID != 0 {
		targetTask = input.TargetTaskID
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO legacy_conversion_records (
public_id,subject_type,subject_id,incident_id,cycle_no,converter_version,attempt,previous_conversion_id,
source_schema_version,target_schema_version,source_table,target_table,input_hash,output_hash,status,reason_code,
target_task_id,anti_join_result,migrated_legacy_context,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), input.SubjectType, input.SubjectID,
		input.IncidentID, input.CycleNo, input.ConverterVersion, attempt, previous, input.SourceSchemaVersion,
		input.TargetSchemaVersion, input.SourceTable, input.TargetTable, input.InputHash, input.OutputHash,
		input.Status, input.ReasonCode, targetTask, input.AntiJoinResult, input.MigratedLegacyContext, input.CreatedAt.UTC())
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("read legacy conversion record id: %w", err)
	}
	return uint64(id), nil
}

func canonicalTaskPayload(values map[string]any) (json.RawMessage, error) {
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > 8*1024 || !json.Valid(encoded) {
		return nil, errors.New("legacy conversion Task payload exceeds its bound")
	}
	return encoded, nil
}
