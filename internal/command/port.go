package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/apiv3"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

const remediationDecisionTTL = 10 * time.Minute

type remediationDecisionRepository interface {
	LockPlanIn(context.Context, remediation.PersistenceTX, string) (*remediation.RemediationPlan, error)
	RecordDecisionIn(context.Context, remediation.PersistenceTX, string, uint64, *remediation.Approval) error
}

// Port implements the domain-owned V3 command transitions. Every durable
// effect, task enqueue, Timeline event, and idempotent response shares one
// MySQL transaction.
type Port struct {
	idempotency  *Store
	tasks        *asyncjob.Repository
	remediations remediationDecisionRepository
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
	remediations, err := remediationmysql.NewV3RemediationRepository(db)
	if err != nil {
		return nil, err
	}
	return &Port{idempotency: idempotency, tasks: tasks, remediations: remediations}, nil
}

func (p *Port) Execute(ctx context.Context, request apiv3.CommandRequest) (apiv3.CommandResult, error) {
	if p == nil || p.idempotency == nil || p.tasks == nil || p.remediations == nil {
		return apiv3.CommandResult{}, apiv3.ErrUnavailable
	}
	if request.ResourceID == "" || request.IdempotencyKey == "" || request.ExpectedVersion == 0 || len(request.CanonicalBody) == 0 {
		return apiv3.CommandResult{}, apiv3.ErrInvalidArgument
	}
	if _, err := uuid.Parse(request.ResourceID); err != nil {
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
			result, commandErr = p.decideRemediation(ctx, tx, request)
		default:
			commandErr = apiv3.ErrInvalidArgument
		}
		return storedResponse(commandResourceType(request.Kind), request.ResourceID, result, commandErr)
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
	body, err := decodeStartInvestigationCommand(request)
	if err != nil {
		return apiv3.CommandResult{}, err
	}
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
	authorization, budget, err := businessbudget.AuthorizeAgentRun(ctx, tx, incident.ID, incident.CycleNo, businessbudget.Actor{
		Provider: request.Actor.Provider, Login: request.Actor.Login, Role: request.Actor.Role,
		Reason: body.Reason, RequestID: request.RequestID,
	})
	if errors.Is(err, businessbudget.ErrInvalidAuthorization) {
		return apiv3.CommandResult{}, apiv3.ErrInvalidArgument
	}
	if errors.Is(err, businessbudget.ErrAuthorizationConflict) {
		return apiv3.CommandResult{}, apiv3.ErrConflict
	}
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if budget.Outcome == businessbudget.OutcomeHardExhausted {
		if err := businessbudget.MarkExhausted(ctx, tx, budget, incident.ID, incident.CycleNo, "operator.investigation.start"); err != nil {
			return apiv3.CommandResult{}, err
		}
		return apiv3.CommandResult{}, apiv3.ErrInvalidTransition
	}
	dedupeParts := []string{"task", incident.PublicID, fmt.Sprint(incident.CycleNo), "investigation.start", fmt.Sprint(incident.Version)}
	payloadBody := map[string]any{"mode": "start", "incident_id": incident.PublicID, "cycle_no": incident.CycleNo}
	if authorization.ID != 0 {
		dedupeParts = append(dedupeParts, authorization.PublicID)
		payloadBody["business_budget_authorization_id"] = authorization.PublicID
		metadata, _ := json.Marshal(map[string]any{
			"authorization_id": authorization.PublicID, "slot": authorization.Slot,
			"reason": body.Reason, "request_id": request.RequestID,
		})
		if err := appendCommandEvent(ctx, tx, incident, "agent_run_retry_authorized", request.Actor, metadata); err != nil {
			return apiv3.CommandResult{}, err
		}
	}
	dedupe := canonicalHash(dedupeParts...)
	payload, _ := json.Marshal(payloadBody)
	task, err := p.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: incident.ID, CycleNo: incident.CycleNo,
		Type: asyncjob.TaskInvestigationAdvance, SubjectType: "incident", SubjectID: incident.ID,
		Transition: "investigation.start", ExpectedSubjectVersion: incident.Version,
		PayloadSchemaVersion: 1, Payload: payload, DedupeKey: dedupe, Priority: 100, MaxAttempts: 5,
	})
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	metadataBody := map[string]any{"task_id": task.PublicID, "request_id": request.RequestID}
	if authorization.ID != 0 {
		metadataBody["authorization_id"] = authorization.PublicID
		metadataBody["authorization_slot"] = authorization.Slot
	}
	metadata, _ := json.Marshal(metadataBody)
	if err := appendCommandEvent(ctx, tx, incident, "investigation_requested", request.Actor, metadata); err != nil {
		return apiv3.CommandResult{}, err
	}
	return apiv3.CommandResult{HTTPStatus: http.StatusAccepted, ResourceID: incident.PublicID, Status: "accepted", Version: incident.Version, Cycle: uint64(incident.CycleNo)}, nil
}

type startInvestigationCommandBody struct {
	ExpectedVersion uint64 `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

func decodeStartInvestigationCommand(request apiv3.CommandRequest) (startInvestigationCommandBody, error) {
	if len(request.CanonicalBody) == 0 || len(request.CanonicalBody) > 4096 {
		return startInvestigationCommandBody{}, apiv3.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(request.CanonicalBody))
	decoder.DisallowUnknownFields()
	var body startInvestigationCommandBody
	if err := decoder.Decode(&body); err != nil {
		return startInvestigationCommandBody{}, apiv3.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return startInvestigationCommandBody{}, apiv3.ErrInvalidArgument
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.ExpectedVersion == 0 || body.ExpectedVersion != request.ExpectedVersion || len(body.Reason) > 1024 {
		return startInvestigationCommandBody{}, apiv3.ErrInvalidArgument
	}
	return body, nil
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

type remediationDecisionCommandBody struct {
	Decision        string `json:"decision"`
	ExpectedVersion uint64 `json:"expected_version"`
	ExpectedHash    string `json:"expected_hash"`
	Reason          string `json:"reason"`
}

func (p *Port) decideRemediation(ctx context.Context, tx *sql.Tx, request apiv3.CommandRequest) (apiv3.CommandResult, error) {
	body, err := decodeRemediationDecisionCommand(request)
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if request.Actor.Provider != "github" || request.Actor.Role != "operator" ||
		request.Actor.Login == "" || request.Actor.Login != strings.TrimSpace(request.Actor.Login) ||
		len(request.Actor.Login) > 128 {
		return apiv3.CommandResult{}, apiv3.ErrForbidden
	}
	if request.RequestID == "" || request.RequestID != strings.TrimSpace(request.RequestID) || len(request.RequestID) > 128 {
		return apiv3.CommandResult{}, apiv3.ErrInvalidArgument
	}

	plan, err := p.remediations.LockPlanIn(ctx, tx, request.ResourceID)
	if errors.Is(err, remediation.ErrNotFound) {
		return apiv3.CommandResult{}, apiv3.ErrNotFound
	}
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if plan.RowVersion != request.ExpectedVersion || plan.CanonicalPlanHash != request.ExpectedHash {
		return apiv3.CommandResult{}, apiv3.ErrStaleVersion
	}
	if plan.Status != remediation.PlanAwaitingApproval {
		return apiv3.CommandResult{}, apiv3.ErrInvalidTransition
	}
	if plan.CycleNo == 0 || plan.CycleNo > math.MaxUint32 || plan.RowVersion == math.MaxUint64 {
		return apiv3.CommandResult{}, apiv3.ErrInvalidArgument
	}
	incident, err := loadIncident(ctx, tx, plan.IncidentPublicID)
	if err != nil {
		return apiv3.CommandResult{}, err
	}
	if incident.ID != plan.IncidentID || uint64(incident.CycleNo) != plan.CycleNo ||
		incident.Version != plan.IncidentVersion+1 {
		return apiv3.CommandResult{}, apiv3.ErrConflict
	}
	if incident.Status != "awaiting_approval" {
		return apiv3.CommandResult{}, apiv3.ErrInvalidTransition
	}
	var databaseNow time.Time
	if err := tx.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
		return apiv3.CommandResult{}, err
	}
	databaseNow = databaseNow.UTC()
	if !databaseNow.Before(plan.ExpiresAt) {
		return apiv3.CommandResult{}, apiv3.ErrConflict
	}
	decisionExpiresAt := databaseNow.Add(remediationDecisionTTL)
	if plan.ExpiresAt.Before(decisionExpiresAt) {
		decisionExpiresAt = plan.ExpiresAt
	}
	decision, err := remediation.NewV3Decision(
		*plan,
		remediation.Decision(body.Decision),
		request.Actor.Provider,
		request.Actor.Login,
		request.Actor.Role,
		body.Reason,
		request.RequestID,
		databaseNow,
		decisionExpiresAt,
	)
	if err != nil {
		return apiv3.CommandResult{}, apiv3.ErrInvalidArgument
	}
	if err := p.remediations.RecordDecisionIn(ctx, tx, plan.PublicID, plan.RowVersion, &decision); err != nil {
		// The repository can fail after its first write. Returning the original
		// error forces the owning command transaction to roll back instead of
		// durably recording a partial Decision without its Plan/task effects.
		return apiv3.CommandResult{}, err
	}

	nextPlanVersion := plan.RowVersion + 1
	metadata := map[string]any{
		"decision":    body.Decision,
		"decision_id": decision.PublicID,
		"plan_hash":   plan.CanonicalPlanHash,
		"plan_id":     plan.PublicID,
		"request_id":  request.RequestID,
	}
	eventType := "remediation_plan_" + body.Decision
	if decision.Decision == remediation.DecisionApproved {
		payload, err := json.Marshal(struct {
			PlanID string `json:"plan_id"`
		}{PlanID: plan.PublicID})
		if err != nil {
			return apiv3.CommandResult{}, err
		}
		task, err := p.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
			IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo),
			Type: asyncjob.TaskChangeEnsurePR, SubjectType: "remediation_plan", SubjectID: plan.ID,
			Transition: "change.ensure_pr", ExpectedSubjectVersion: nextPlanVersion,
			PayloadSchemaVersion: 1, Payload: payload,
			DedupeKey: canonicalHash("change.ensure_pr", plan.PublicID, plan.CanonicalPlanHash, fmt.Sprint(nextPlanVersion)),
			Priority:  90, MaxAttempts: 5,
		})
		if err != nil {
			return apiv3.CommandResult{}, err
		}
		metadata["task_id"] = task.PublicID
	} else {
		updated, err := tx.ExecContext(ctx, `UPDATE incidents
SET status = 'DIAGNOSING', v3_status = 'investigating', version = version + 1,
    needs_attention = FALSE, blocking_reason_code = NULL, blocked_at = NULL,
    updated_at = NOW(6)
WHERE id = ? AND domain_schema_version = 3 AND cycle_no = ? AND version = ?
  AND v3_status = 'awaiting_approval'`, incident.ID, incident.CycleNo, incident.Version)
		if err != nil {
			return apiv3.CommandResult{}, err
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return apiv3.CommandResult{}, errors.New("remediation rejection lost the locked Incident transition")
		}
		incident.Status = "investigating"
		incident.Version++
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil || len(metadataJSON) > 8192 {
		return apiv3.CommandResult{}, errors.New("remediation Decision Timeline metadata is invalid")
	}
	if err := appendCommandEvent(ctx, tx, incident, eventType, request.Actor, metadataJSON); err != nil {
		return apiv3.CommandResult{}, err
	}
	return apiv3.CommandResult{
		HTTPStatus: http.StatusAccepted,
		ResourceID: plan.PublicID,
		Status:     body.Decision,
		Version:    nextPlanVersion,
		Cycle:      plan.CycleNo,
	}, nil
}

func decodeRemediationDecisionCommand(request apiv3.CommandRequest) (remediationDecisionCommandBody, error) {
	if len(request.CanonicalBody) == 0 || len(request.CanonicalBody) > 4096 {
		return remediationDecisionCommandBody{}, apiv3.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(request.CanonicalBody))
	decoder.DisallowUnknownFields()
	var body remediationDecisionCommandBody
	if err := decoder.Decode(&body); err != nil {
		return remediationDecisionCommandBody{}, apiv3.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return remediationDecisionCommandBody{}, apiv3.ErrInvalidArgument
	}
	if body.Decision != string(remediation.DecisionApproved) && body.Decision != string(remediation.DecisionRejected) {
		return remediationDecisionCommandBody{}, apiv3.ErrInvalidTransition
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.ExpectedVersion == 0 || body.ExpectedVersion != request.ExpectedVersion ||
		body.ExpectedHash != request.ExpectedHash || !validCommandSHA256(body.ExpectedHash) ||
		body.Reason == "" || len(body.Reason) > 1024 {
		return remediationDecisionCommandBody{}, apiv3.ErrInvalidArgument
	}
	return body, nil
}

func validCommandSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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

func storedResponse(resourceType, resourceID string, result apiv3.CommandResult, commandErr error) (Response, error) {
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
		case errors.Is(commandErr, apiv3.ErrForbidden):
			status, code = http.StatusForbidden, "forbidden"
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
	return Response{HTTPStatus: status, Body: body, ResourceType: resourceType, ResourcePublicID: resourceID}, nil
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
	case "forbidden":
		return apiv3.ErrForbidden
	default:
		return apiv3.ErrUnavailable
	}
}

func commandResourceType(kind apiv3.CommandKind) string {
	if kind == apiv3.CommandDecideRemediation {
		return "remediation_plan"
	}
	return "incident"
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
