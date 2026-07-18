package command

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

// Port implements the Phase 2 domain-owned V3 commands. Remediation decisions
// stay explicitly unimplemented until their owning phase.
type Port struct {
	idempotency *Store
	tasks       *asyncjob.Repository
}

func NewPort(db *sql.DB) (*Port, error) {
	idempotency, err := NewStore(db)
	if err != nil {
		return nil, err
	}
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		return nil, err
	}
	return &Port{idempotency: idempotency, tasks: tasks}, nil
}

func (p *Port) Execute(ctx context.Context, request apiv3.CommandRequest) (apiv3.CommandResult, error) {
	if p == nil || p.idempotency == nil || p.tasks == nil {
		return apiv3.CommandResult{}, apiv3.ErrUnavailable
	}
	if request.ResourceID == "" || request.IdempotencyKey == "" || request.ExpectedVersion == 0 || len(request.CanonicalBody) == 0 {
		return apiv3.CommandResult{}, apiv3.ErrInvalidArgument
	}
	actorHash := canonicalHash("actor", request.Actor.Provider, request.Actor.Login, request.Actor.Subject)
	requestHash := sha256.Sum256(request.CanonicalBody)
	commandRequest := Request{
		ActorIdentityHash: actorHash,
		CommandScope:      string(request.Kind) + ":" + request.ResourceID,
		IdempotencyKey:    request.IdempotencyKey,
		RequestHash:       hex.EncodeToString(requestHash[:]),
	}
	response, replayed, err := p.idempotency.Execute(ctx, commandRequest, func(ctx context.Context, tx *sql.Tx) (Response, error) {
		var result apiv3.CommandResult
		var commandErr error
		switch request.Kind {
		case apiv3.CommandStartInvestigation:
			result, commandErr = p.startInvestigation(ctx, tx, request)
		case apiv3.CommandCloseIncident:
			result, commandErr = p.closeIncident(ctx, tx, request)
		case apiv3.CommandDecideRemediation:
			commandErr = apiv3.ErrNotImplemented
		default:
			commandErr = apiv3.ErrInvalidArgument
		}
		return storedResponse(request.ResourceID, result, commandErr)
	})
	if errors.Is(err, ErrPayloadConflict) {
		return apiv3.CommandResult{}, apiv3.ErrConflict
	}
	if err != nil {
		return apiv3.CommandResult{}, fmt.Errorf("%w: %v", apiv3.ErrUnavailable, err)
	}
	var stored struct {
		Result apiv3.CommandResult `json:"result"`
		Error  string              `json:"error,omitempty"`
	}
	if err := json.Unmarshal(response.Body, &stored); err != nil {
		return apiv3.CommandResult{}, fmt.Errorf("%w: decode command response", apiv3.ErrUnavailable)
	}
	stored.Result.Replayed = replayed
	if stored.Error != "" {
		return stored.Result, errorFromCode(stored.Error)
	}
	return stored.Result, nil
}

type lockedIncident struct {
	ID       uint64
	PublicID string
	CycleNo  uint32
	Status   string
	Version  uint64
}

func loadIncident(ctx context.Context, tx *sql.Tx, publicID string) (lockedIncident, error) {
	var incident lockedIncident
	err := tx.QueryRowContext(ctx, `
SELECT id, public_id, cycle_no, v3_status, version
FROM incidents
WHERE public_id = ? AND domain_schema_version = 3
FOR UPDATE`, publicID).Scan(&incident.ID, &incident.PublicID, &incident.CycleNo, &incident.Status, &incident.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return lockedIncident{}, apiv3.ErrNotFound
	}
	return incident, err
}

func (p *Port) startInvestigation(ctx context.Context, tx *sql.Tx, request apiv3.CommandRequest) (apiv3.CommandResult, error) {
	incident, err := loadIncident(ctx, tx, request.ResourceID)
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if incident.Version != request.ExpectedVersion {
		return apiv3.CommandResult{}, apiv3.ErrStaleVersion
	}
	if incident.Status != "detected" && incident.Status != "investigating" {
		return apiv3.CommandResult{}, apiv3.ErrInvalidTransition
	}
	dedupe := canonicalHash("task", incident.PublicID, fmt.Sprint(incident.CycleNo), "investigation.start", fmt.Sprint(incident.Version))
	payload, _ := json.Marshal(map[string]any{"mode": "start", "incident_id": incident.PublicID, "cycle_no": incident.CycleNo})
	task, err := p.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: incident.ID, CycleNo: incident.CycleNo,
		Type: asyncjob.TaskInvestigationAdvance, SubjectType: "incident", SubjectID: incident.ID,
		Transition: "investigation.start", ExpectedSubjectVersion: incident.Version,
		PayloadSchemaVersion: 1, Payload: payload, DedupeKey: dedupe, Priority: 100, MaxAttempts: 5,
	})
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	metadata, _ := json.Marshal(map[string]any{"task_id": task.PublicID, "request_id": request.RequestID})
	if err := appendCommandEvent(ctx, tx, incident, "investigation_requested", request.Actor, metadata); err != nil {
		return apiv3.CommandResult{}, err
	}
	return apiv3.CommandResult{HTTPStatus: http.StatusAccepted, ResourceID: incident.PublicID, Status: "accepted", Version: incident.Version, Cycle: uint64(incident.CycleNo)}, nil
}

func (p *Port) closeIncident(ctx context.Context, tx *sql.Tx, request apiv3.CommandRequest) (apiv3.CommandResult, error) {
	incident, err := loadIncident(ctx, tx, request.ResourceID)
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if incident.Version != request.ExpectedVersion {
		return apiv3.CommandResult{}, apiv3.ErrStaleVersion
	}
	if incident.Status != "detected" && incident.Status != "investigating" && incident.Status != "awaiting_approval" {
		return apiv3.CommandResult{}, apiv3.ErrInvalidTransition
	}
	var unsafe int
	if err := tx.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM change_requests
   WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ?)
  +
  (SELECT COUNT(*) FROM verification_runs
   WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ? AND v3_status IN ('pending','running'))
  +
  (SELECT COUNT(*) FROM async_tasks
   WHERE incident_id = ? AND cycle_no = ? AND status IN ('ready','running')
     AND task_type IN ('change.ensure_pr','delivery.observe','verification.advance'))`,
		incident.ID, incident.CycleNo, incident.ID, incident.CycleNo, incident.ID, incident.CycleNo).Scan(&unsafe); err != nil {
		return apiv3.CommandResult{}, err
	}
	if unsafe != 0 {
		return apiv3.CommandResult{}, apiv3.ErrInvalidTransition
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_runs
SET v3_status = 'cancelled', status = 'CANCELLED', row_version = row_version + 1,
    cancel_requested_at = NOW(6), completed_at = NOW(6), updated_at = NOW(6)
WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ? AND v3_status IN ('pending','running')`,
		incident.ID, incident.CycleNo); err != nil {
		return apiv3.CommandResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE remediation_plans
SET v3_status = 'cancelled', status = 'cancelled', row_version = row_version + 1, updated_at = NOW(6)
WHERE domain_schema_version = 3 AND incident_id = ? AND cycle_no = ?
  AND v3_status IN ('awaiting_approval','approved')`, incident.ID, incident.CycleNo); err != nil {
		return apiv3.CommandResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE async_task_attempts attempt
JOIN async_tasks task
  ON task.id = attempt.task_id
 AND task.attempt = attempt.attempt
 AND task.lease_owner = attempt.lease_owner
 AND task.lease_generation = attempt.lease_generation
 AND task.expected_subject_version = attempt.expected_subject_version
SET attempt.status = 'cancelled', attempt.finished_at = NOW(6),
    attempt.error_code = 'cancelled', attempt.error_summary = 'task cancelled by Incident close'
WHERE task.incident_id = ? AND task.cycle_no = ? AND task.status = 'running'
  AND task.task_type IN ('investigation.advance','remediation.prepare')
  AND attempt.status = 'running'`, incident.ID, incident.CycleNo); err != nil {
		return apiv3.CommandResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE async_tasks
SET status = 'cancelled', lease_owner = NULL, lease_expires_at = NULL,
    heartbeat_at = NULL, lease_generation = lease_generation + 1,
    last_error_code = 'cancelled', last_error_summary = 'task cancelled by Incident close',
    cancelled_at = NOW(6), updated_at = NOW(6)
WHERE incident_id = ? AND cycle_no = ? AND status IN ('ready','running')
  AND task_type IN ('investigation.advance','remediation.prepare')`, incident.ID, incident.CycleNo); err != nil {
		return apiv3.CommandResult{}, err
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE incidents
SET v3_status = 'closed', status = 'CLOSED_NO_ACTION', version = version + 1,
    terminal_at = NOW(6), resolved_at = NULL, updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ?
  AND v3_status IN ('detected','investigating','awaiting_approval')`,
		incident.ID, incident.CycleNo, incident.Version)
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return apiv3.CommandResult{}, apiv3.ErrStaleVersion
	}
	incident.Status = "closed"
	incident.Version++
	metadata, _ := json.Marshal(map[string]any{"request_id": request.RequestID})
	if err := appendCommandEvent(ctx, tx, incident, "incident_closed", request.Actor, metadata); err != nil {
		return apiv3.CommandResult{}, err
	}
	return apiv3.CommandResult{HTTPStatus: http.StatusAccepted, ResourceID: incident.PublicID, Status: "closed", Version: incident.Version, Cycle: uint64(incident.CycleNo)}, nil
}

func appendCommandEvent(ctx context.Context, tx *sql.Tx, incident lockedIncident, eventType string, actor apiv3.Identity, metadata []byte) error {
	idempotency := canonicalHash("event", incident.PublicID, fmt.Sprint(incident.CycleNo), eventType, string(metadata))
	_, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
    (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
     event_type, idempotency_key, actor_type, actor_id, summary, metadata_json,
     occurred_at, created_at)
VALUES (?, ?, 3, ?, 1, ?, ?, 'user', ?, ?, ?, NOW(6), NOW(6))`,
		uuid.NewString(), incident.ID, incident.CycleNo, eventType, idempotency,
		actor.Provider+":"+actor.Login, strings.ReplaceAll(eventType, "_", " "), metadata)
	return err
}

func storedResponse(resourceID string, result apiv3.CommandResult, commandErr error) (Response, error) {
	status := http.StatusAccepted
	code := ""
	if commandErr != nil {
		switch {
		case errors.Is(commandErr, apiv3.ErrNotFound):
			status, code = http.StatusNotFound, "not_found"
		case errors.Is(commandErr, apiv3.ErrStaleVersion):
			status, code = http.StatusConflict, "stale"
		case errors.Is(commandErr, apiv3.ErrConflict):
			status, code = http.StatusConflict, "conflict"
		case errors.Is(commandErr, apiv3.ErrInvalidTransition):
			status, code = http.StatusUnprocessableEntity, "invalid_transition"
		case errors.Is(commandErr, apiv3.ErrNotImplemented):
			status, code = http.StatusNotImplemented, "not_implemented"
		case errors.Is(commandErr, apiv3.ErrInvalidArgument):
			status, code = http.StatusBadRequest, "invalid_argument"
		default:
			return Response{}, commandErr
		}
	}
	body, err := json.Marshal(struct {
		Result apiv3.CommandResult `json:"result"`
		Error  string              `json:"error,omitempty"`
	}{Result: result, Error: code})
	if err != nil {
		return Response{}, err
	}
	return Response{HTTPStatus: status, Body: body, ResourceType: "incident", ResourcePublicID: resourceID}, nil
}

func errorFromCode(code string) error {
	switch code {
	case "not_found":
		return apiv3.ErrNotFound
	case "stale":
		return apiv3.ErrStaleVersion
	case "conflict":
		return apiv3.ErrConflict
	case "invalid_transition":
		return apiv3.ErrInvalidTransition
	case "not_implemented":
		return apiv3.ErrNotImplemented
	case "invalid_argument":
		return apiv3.ErrInvalidArgument
	default:
		return apiv3.ErrUnavailable
	}
}

func canonicalHash(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
