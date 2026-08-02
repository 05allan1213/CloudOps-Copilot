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

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/recovery"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

const remediationDecisionTTL = 10 * time.Minute

type remediationDecisionRepository interface {
	LockPlanIn(context.Context, remediation.PersistenceTX, string) (*remediation.RemediationPlan, error)
	RecordDecisionIn(context.Context, remediation.PersistenceTX, string, uint64, *remediation.Approval) error
}

// Port implements the domain-owned command transitions. Every durable
// effect, task enqueue, Timeline event, and idempotent response shares one
// MySQL transaction.
type Port struct {
	idempotency  *Store
	tasks        *asyncjob.Repository
	workspace    *agent.WorkspaceRepository
	remediations remediationDecisionRepository
	recovery     *recovery.Coordinator
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
	workspace, err := agent.NewWorkspaceRepository(db)
	if err != nil {
		return nil, err
	}
	remediations, err := remediationmysql.NewRepository(db)
	if err != nil {
		return nil, err
	}
	recoveryCoordinator, err := recovery.NewCoordinator(db)
	if err != nil {
		return nil, err
	}
	return &Port{idempotency: idempotency, tasks: tasks, workspace: workspace, remediations: remediations, recovery: recoveryCoordinator}, nil
}

func (p *Port) Execute(ctx context.Context, request api.CommandRequest) (api.CommandResult, error) {
	if p == nil || p.idempotency == nil || p.tasks == nil || p.workspace == nil || p.remediations == nil || p.recovery == nil {
		return api.CommandResult{}, api.ErrUnavailable
	}
	if request.ResourceID == "" || request.IdempotencyKey == "" || request.ExpectedVersion == 0 || len(request.CanonicalBody) == 0 {
		return api.CommandResult{}, api.ErrInvalidArgument
	}
	if !isLocalOwner(request.Actor) {
		return api.CommandResult{}, api.ErrForbidden
	}
	if _, err := uuid.Parse(request.ResourceID); err != nil {
		return api.CommandResult{}, api.ErrInvalidArgument
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
		var result api.CommandResult
		var commandErr error
		switch request.Kind {
		case api.CommandStartInvestigation:
			result, commandErr = p.startInvestigation(ctx, tx, request)
		case api.CommandDecideRecovery:
			result, commandErr = p.decideRecovery(ctx, tx, request)
		case api.CommandCloseIncident:
			result, commandErr = p.closeIncident(ctx, tx, request)
		case api.CommandDecideRemediation:
			result, commandErr = p.decideRemediation(ctx, tx, request)
		default:
			commandErr = api.ErrInvalidArgument
		}
		return storedResponse(commandResourceType(request.Kind), request.ResourceID, result, commandErr)
	})
	if errors.Is(err, ErrPayloadConflict) {
		return api.CommandResult{}, api.ErrConflict
	}
	if err != nil {
		return api.CommandResult{}, fmt.Errorf("%w: %v", api.ErrUnavailable, err)
	}
	var stored struct {
		Result api.CommandResult `json:"result"`
		Error  string            `json:"error,omitempty"`
	}
	if err := json.Unmarshal(response.Body, &stored); err != nil {
		return api.CommandResult{}, fmt.Errorf("%w: decode command response", api.ErrUnavailable)
	}
	stored.Result.Replayed = replayed
	if stored.Error != "" {
		return stored.Result, errorFromCode(stored.Error)
	}
	return stored.Result, nil
}

type recoveryDecisionCommandBody struct {
	Decision        string `json:"decision"`
	ExpectedVersion uint64 `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (p *Port) decideRecovery(ctx context.Context, tx *sql.Tx, request api.CommandRequest) (api.CommandResult, error) {
	body, err := decodeRecoveryDecisionCommand(request)
	if err != nil {
		return api.CommandResult{}, err
	}
	result, err := p.recovery.DecideIn(ctx, tx, recovery.DecisionInput{
		IncidentID: request.ResourceID, ExpectedVersion: request.ExpectedVersion,
		Decision: body.Decision, Reason: body.Reason,
		ActorProvider: request.Actor.Provider, ActorLogin: request.Actor.Login, ActorRole: request.Actor.Role,
		RequestID: request.RequestID, IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, recovery.ErrNotFound):
			return api.CommandResult{}, api.ErrNotFound
		case errors.Is(err, recovery.ErrStaleVersion):
			return api.CommandResult{}, api.ErrStaleVersion
		case errors.Is(err, recovery.ErrInvalidTransition):
			return api.CommandResult{}, api.ErrInvalidTransition
		case errors.Is(err, recovery.ErrConflict):
			return api.CommandResult{}, api.ErrConflict
		case errors.Is(err, recovery.ErrInvalid):
			return api.CommandResult{}, api.ErrInvalidArgument
		default:
			return api.CommandResult{}, err
		}
	}
	return api.CommandResult{
		HTTPStatus: http.StatusAccepted, ResourceID: result.IncidentID, Status: result.Status,
		Version: result.Version, Cycle: result.Cycle,
	}, nil
}

func decodeRecoveryDecisionCommand(request api.CommandRequest) (recoveryDecisionCommandBody, error) {
	if len(request.CanonicalBody) == 0 || len(request.CanonicalBody) > 4096 {
		return recoveryDecisionCommandBody{}, api.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(request.CanonicalBody))
	decoder.DisallowUnknownFields()
	var body recoveryDecisionCommandBody
	if err := decoder.Decode(&body); err != nil {
		return recoveryDecisionCommandBody{}, api.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return recoveryDecisionCommandBody{}, api.ErrInvalidArgument
	}
	body.Decision = strings.ToLower(strings.TrimSpace(body.Decision))
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Decision != recovery.DecisionVerifyRecovery || body.ExpectedVersion == 0 ||
		body.ExpectedVersion != request.ExpectedVersion || body.Reason == "" || len(body.Reason) > 1024 {
		return recoveryDecisionCommandBody{}, api.ErrInvalidArgument
	}
	return body, nil
}

func isLocalOwner(actor api.OwnerIdentity) bool {
	return actor.Subject == "local-owner" && actor.Provider == "local" &&
		actor.Login == "owner" && actor.Role == "owner"
}

type lockedIncident struct {
	ID                    uint64
	PublicID              string
	CycleNo               uint32
	Status                string
	Version               uint64
	MigratedLegacy        bool
	MigratedLegacyContext bool
}

func loadIncident(ctx context.Context, tx *sql.Tx, publicID string) (lockedIncident, error) {
	var incident lockedIncident
	err := tx.QueryRowContext(ctx, `
SELECT id, public_id, cycle_no, status, version, migrated_legacy, migrated_legacy_context
FROM incidents
WHERE public_id = ?
FOR UPDATE`, publicID).Scan(&incident.ID, &incident.PublicID, &incident.CycleNo, &incident.Status, &incident.Version,
		&incident.MigratedLegacy, &incident.MigratedLegacyContext)
	if errors.Is(err, sql.ErrNoRows) {
		return lockedIncident{}, api.ErrNotFound
	}
	return incident, err
}

func (p *Port) startInvestigation(ctx context.Context, tx *sql.Tx, request api.CommandRequest) (api.CommandResult, error) {
	body, err := decodeStartInvestigationCommand(request)
	if err != nil {
		return api.CommandResult{}, err
	}
	incident, err := loadIncident(ctx, tx, request.ResourceID)
	if err != nil {
		return api.CommandResult{}, err
	}
	if incident.Version != request.ExpectedVersion {
		return api.CommandResult{}, api.ErrStaleVersion
	}
	if incident.Status != "detected" && incident.Status != "investigating" {
		return api.CommandResult{}, api.ErrInvalidTransition
	}
	activeRunID, err := businessbudget.ActiveAgentRunForCycle(ctx, tx, incident.ID, incident.CycleNo)
	if err != nil {
		return api.CommandResult{}, err
	}
	if activeRunID != 0 {
		reconciled, reconcileErr := reconcileDeadInvestigationRun(ctx, tx, incident, activeRunID)
		if reconcileErr != nil {
			return api.CommandResult{}, reconcileErr
		}
		if !reconciled {
			return api.CommandResult{}, api.ErrConflict
		}
	}
	legacyStart, err := lockLiveInvestigationStart(ctx, tx, incident)
	if err != nil {
		return api.CommandResult{}, err
	}
	if legacyStart != nil && legacyStart.Status != string(asyncjob.StatusReady) {
		return api.CommandResult{}, api.ErrConflict
	}
	authorization, budget, err := businessbudget.AuthorizeAgentRun(ctx, tx, incident.ID, incident.CycleNo, businessbudget.Actor{
		Provider: request.Actor.Provider, Login: request.Actor.Login, Role: request.Actor.Role,
		Reason: body.Reason, RequestID: request.RequestID,
	})
	if errors.Is(err, businessbudget.ErrInvalidAuthorization) {
		return api.CommandResult{}, api.ErrInvalidArgument
	}
	if errors.Is(err, businessbudget.ErrAuthorizationConflict) {
		return api.CommandResult{}, api.ErrConflict
	}
	if err != nil {
		return api.CommandResult{}, err
	}
	if budget.Outcome == businessbudget.OutcomeHardExhausted {
		if err := businessbudget.MarkExhausted(ctx, tx, budget, incident.ID, incident.CycleNo, "owner.investigation.start"); err != nil {
			return api.CommandResult{}, err
		}
		return api.CommandResult{}, api.ErrInvalidTransition
	}
	if authorization.ID != 0 {
		metadata, _ := json.Marshal(map[string]any{
			"authorization_id": authorization.PublicID, "slot": authorization.Slot,
			"reason": body.Reason, "request_id": request.RequestID,
		})
		if err := appendCommandEvent(ctx, tx, incident, "agent_run_retry_authorized", request.Actor, metadata); err != nil {
			return api.CommandResult{}, err
		}
	}
	if legacyStart != nil {
		if err := cancelReadyInvestigationStart(ctx, tx, *legacyStart); err != nil {
			return api.CommandResult{}, err
		}
	}
	runID, err := p.workspace.StartIncidentInvestigationTx(
		ctx, tx, incident.PublicID, request.IdempotencyKey, body.Reason,
		incident.Version, authorization.ID,
	)
	if err != nil {
		switch {
		case errors.Is(err, agent.ErrNotFound):
			return api.CommandResult{}, api.ErrNotFound
		case errors.Is(err, agent.ErrConflict):
			return api.CommandResult{}, api.ErrConflict
		case errors.Is(err, agent.ErrInvalidArgument):
			return api.CommandResult{}, api.ErrInvalidArgument
		default:
			return api.CommandResult{}, err
		}
	}
	metadataBody := map[string]any{"agent_run_id": runID, "request_id": request.RequestID}
	if legacyStart != nil {
		metadataBody["superseded_async_task_id"] = legacyStart.PublicID
	}
	if authorization.ID != 0 {
		metadataBody["authorization_id"] = authorization.PublicID
		metadataBody["authorization_slot"] = authorization.Slot
	}
	metadata, _ := json.Marshal(metadataBody)
	if err := appendCommandEvent(ctx, tx, incident, "investigation_requested", request.Actor, metadata); err != nil {
		return api.CommandResult{}, err
	}
	return api.CommandResult{HTTPStatus: http.StatusAccepted, ResourceID: incident.PublicID, Status: "investigation_started", Version: incident.Version + 1, Cycle: uint64(incident.CycleNo)}, nil
}

type liveInvestigationStart struct {
	ID       uint64
	PublicID string
	Status   string
	Version  uint64
}

func lockLiveInvestigationStart(ctx context.Context, tx *sql.Tx, incident lockedIncident) (*liveInvestigationStart, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, public_id, status, expected_subject_version
	FROM async_tasks
	WHERE incident_id = ? AND cycle_no = ?
	  AND task_type = 'investigation.advance'
	  AND subject_type = 'incident' AND subject_id = ?
	  AND transition = 'investigation.start'
	  AND status IN ('ready','running')
	ORDER BY id
	FOR UPDATE`, incident.ID, incident.CycleNo, incident.ID)
	if err != nil {
		return nil, fmt.Errorf("lock live investigation.start task: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result *liveInvestigationStart
	for rows.Next() {
		if result != nil {
			return nil, fmt.Errorf("incident %s has multiple live investigation.start tasks", incident.PublicID)
		}
		var item liveInvestigationStart
		if err := rows.Scan(&item.ID, &item.PublicID, &item.Status, &item.Version); err != nil {
			return nil, err
		}
		result = &item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func cancelReadyInvestigationStart(ctx context.Context, tx *sql.Tx, task liveInvestigationStart) error {
	if task.ID == 0 || task.Status != string(asyncjob.StatusReady) || task.Version == 0 {
		return api.ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE async_tasks
	SET status='cancelled', cancelled_at=NOW(6),
	    last_error_code='superseded_by_workspace',
	    last_error_summary='Incident Investigation migrated to the settings-backed Agent Workspace runtime',
	    updated_at=NOW(6)
	WHERE id=? AND status='ready' AND expected_subject_version=?`, task.ID, task.Version)
	if err != nil {
		return fmt.Errorf("cancel superseded investigation.start task: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return api.ErrConflict
	}
	return nil
}

// reconcileDeadInvestigationRun converts a technical dead task into a terminal
// business Run only when the Owner has explicitly chosen a new investigation.
// A live replay for the same Run version always wins and preserves the conflict.
func reconcileDeadInvestigationRun(ctx context.Context, tx *sql.Tx, incident lockedIncident, runID uint64) (bool, error) {
	var runPublicID, taskPublicID string
	var rowVersion uint64
	var errorCode, errorSummary sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT r.public_id, r.row_version, dead.public_id,
	       dead.last_error_code, dead.last_error_summary
	FROM agent_runs r
	JOIN async_tasks dead
	  ON dead.subject_type = 'agent_run' AND dead.subject_id = r.id
	 AND dead.transition = 'investigation.step' AND dead.status = 'dead'
	 AND dead.expected_subject_version = r.row_version
	LEFT JOIN async_tasks live
	  ON live.subject_type = 'agent_run' AND live.subject_id = r.id
	 AND live.transition = 'investigation.step' AND live.status IN ('ready','running')
	 AND live.expected_subject_version = r.row_version
	WHERE r.id = ? AND r.incident_id = ? AND r.cycle_no = ?
	  AND r.status IN ('pending','running')
	  AND live.id IS NULL
	ORDER BY dead.replay_generation DESC, dead.id DESC
	LIMIT 1
	FOR UPDATE`, runID, incident.ID, incident.CycleNo).
		Scan(&runPublicID, &rowVersion, &taskPublicID, &errorCode, &errorSummary)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load dead investigation task for reconciliation: %w", err)
	}
	reason := strings.TrimSpace(errorCode.String)
	if reason == "" {
		reason = "investigation_task_dead"
	}
	summary := strings.TrimSpace(errorSummary.String)
	if summary == "" {
		summary = "investigation task entered dead state before the AgentRun reached a terminal state"
	}

	result, err := tx.ExecContext(ctx, `UPDATE agent_runs
	SET status = 'failed', outcome = 'failed', failure_code = ?, failure_summary = ?,
	    completed_at = NOW(6), row_version = row_version + 1, updated_at = NOW(6)
	WHERE id = ? AND incident_id = ? AND cycle_no = ?
	  AND row_version = ? AND status IN ('pending','running')`,
		reason, summary, runID, incident.ID, incident.CycleNo, rowVersion)
	if err != nil {
		return false, fmt.Errorf("reconcile dead investigation AgentRun: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return false, api.ErrConflict
	}
	metadata, _ := json.Marshal(map[string]any{
		"agent_run_id": runPublicID, "task_public_id": taskPublicID,
		"reason": reason, "reconciled_before_command": string(api.CommandStartInvestigation),
	})
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO incident_events
	    (public_id, incident_id, cycle_no, event_schema_version,
	     event_type, idempotency_key, migrated_legacy_context, migrated_legacy,
	     actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
	VALUES (?, ?, ?, 1, 'agent_run_failed', ?, ?, ?, 'system', 'command-reconciler',
	        'investigation AgentRun reconciled after terminal task', ?, NOW(6), NOW(6))`,
		uuid.NewString(), incident.ID, incident.CycleNo,
		canonicalHash("event", taskPublicID, "agent_run_failed"),
		incident.MigratedLegacyContext, incident.MigratedLegacy, metadata); err != nil {
		return false, fmt.Errorf("append reconciled AgentRun failure event: %w", err)
	}
	return true, nil
}

type startInvestigationCommandBody struct {
	ExpectedVersion uint64 `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

func decodeStartInvestigationCommand(request api.CommandRequest) (startInvestigationCommandBody, error) {
	if len(request.CanonicalBody) == 0 || len(request.CanonicalBody) > 4096 {
		return startInvestigationCommandBody{}, api.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(request.CanonicalBody))
	decoder.DisallowUnknownFields()
	var body startInvestigationCommandBody
	if err := decoder.Decode(&body); err != nil {
		return startInvestigationCommandBody{}, api.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return startInvestigationCommandBody{}, api.ErrInvalidArgument
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.ExpectedVersion == 0 || body.ExpectedVersion != request.ExpectedVersion || len(body.Reason) > 1024 {
		return startInvestigationCommandBody{}, api.ErrInvalidArgument
	}
	return body, nil
}

func (p *Port) closeIncident(ctx context.Context, tx *sql.Tx, request api.CommandRequest) (api.CommandResult, error) {
	incident, err := loadIncident(ctx, tx, request.ResourceID)
	if err != nil {
		return api.CommandResult{}, err
	}
	if incident.Version != request.ExpectedVersion {
		return api.CommandResult{}, api.ErrStaleVersion
	}
	if incident.Status != "resolved" {
		return api.CommandResult{}, api.ErrInvalidTransition
	}
	var (
		verificationPublicID  string
		reportPublicID        string
		activeVerification    int
		activeTask            int
		activeExternalEffect  int
		unknownExternalEffect int
	)
	if err := tx.QueryRowContext(ctx, `
SELECT
	COALESCE((SELECT verification.public_id
	 FROM resolution_reports report
	 JOIN verification_runs verification
	   ON verification.id = report.verification_run_id
	  AND verification.incident_id = report.incident_id
	  AND verification.cycle_no = report.cycle_no
	 WHERE report.incident_id = ? AND report.cycle_no = ?
	   AND verification.status = 'passed'
	   AND verification.common_success_since IS NOT NULL
	   AND verification.common_window_completed_at IS NOT NULL
	 ORDER BY report.id DESC LIMIT 1), ''),
	COALESCE((SELECT report.public_id
	 FROM resolution_reports report
	 JOIN verification_runs verification
	   ON verification.id = report.verification_run_id
	  AND verification.incident_id = report.incident_id
	  AND verification.cycle_no = report.cycle_no
	 WHERE report.incident_id = ? AND report.cycle_no = ?
	   AND verification.status = 'passed'
	   AND verification.common_success_since IS NOT NULL
	   AND verification.common_window_completed_at IS NOT NULL
	 ORDER BY report.id DESC LIMIT 1), ''),
	(SELECT COUNT(*) FROM verification_runs
	 WHERE incident_id = ? AND cycle_no = ? AND status IN ('pending','running')),
	(SELECT COUNT(*) FROM async_tasks
	 WHERE incident_id = ? AND cycle_no = ? AND status IN ('ready','running')),
	(SELECT COUNT(*) FROM change_requests
	 WHERE incident_id = ? AND cycle_no = ?
	   AND status IN ('pending','pr_open','merged','syncing','rolling_out')),
	(SELECT COUNT(*) FROM change_requests
	 WHERE incident_id = ? AND cycle_no = ?
	   AND external_write_started_at IS NOT NULL
	   AND status NOT IN ('delivered','superseded'))`,
		incident.ID, incident.CycleNo,
		incident.ID, incident.CycleNo,
		incident.ID, incident.CycleNo,
		incident.ID, incident.CycleNo,
		incident.ID, incident.CycleNo,
		incident.ID, incident.CycleNo,
	).Scan(
		&verificationPublicID, &reportPublicID, &activeVerification, &activeTask,
		&activeExternalEffect, &unknownExternalEffect,
	); err != nil {
		return api.CommandResult{}, err
	}
	if verificationPublicID == "" || reportPublicID == "" || activeVerification != 0 ||
		activeTask != 0 || activeExternalEffect != 0 || unknownExternalEffect != 0 {
		return api.CommandResult{}, api.ErrInvalidTransition
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE incidents
SET status = 'closed', version = version + 1,
	    terminal_at = NOW(6), needs_attention = FALSE,
	    blocking_reason_code = NULL, blocked_at = NULL, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND version = ?
	  AND status = 'resolved' AND resolved_at IS NOT NULL`,
		incident.ID, incident.CycleNo, incident.Version)
	if err != nil {
		return api.CommandResult{}, err
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return api.CommandResult{}, api.ErrStaleVersion
	}
	incident.Status = "closed"
	incident.Version++
	metadata, _ := json.Marshal(map[string]any{
		"request_id": request.RequestID, "verification_run_id": verificationPublicID,
		"resolution_report_id": reportPublicID, "resolved_history_preserved": true,
	})
	if err := appendCommandEvent(ctx, tx, incident, "incident_closed", request.Actor, metadata); err != nil {
		return api.CommandResult{}, err
	}
	return api.CommandResult{HTTPStatus: http.StatusAccepted, ResourceID: incident.PublicID, Status: "closed", Version: incident.Version, Cycle: uint64(incident.CycleNo)}, nil
}

type remediationDecisionCommandBody struct {
	Decision        string `json:"decision"`
	ExpectedVersion uint64 `json:"expected_version"`
	ExpectedHash    string `json:"expected_hash"`
	Reason          string `json:"reason"`
}

func (p *Port) decideRemediation(ctx context.Context, tx *sql.Tx, request api.CommandRequest) (api.CommandResult, error) {
	body, err := decodeRemediationDecisionCommand(request)
	if err != nil {
		return api.CommandResult{}, err
	}
	if !isLocalOwner(request.Actor) {
		return api.CommandResult{}, api.ErrForbidden
	}
	if request.RequestID == "" || request.RequestID != strings.TrimSpace(request.RequestID) || len(request.RequestID) > 128 {
		return api.CommandResult{}, api.ErrInvalidArgument
	}

	plan, err := p.remediations.LockPlanIn(ctx, tx, request.ResourceID)
	if errors.Is(err, remediation.ErrNotFound) {
		return api.CommandResult{}, api.ErrNotFound
	}
	if err != nil {
		return api.CommandResult{}, err
	}
	if plan.RowVersion != request.ExpectedVersion || plan.CanonicalPlanHash != request.ExpectedHash {
		return api.CommandResult{}, api.ErrStaleVersion
	}
	if plan.Status != remediation.PlanAwaitingApproval {
		return api.CommandResult{}, api.ErrInvalidTransition
	}
	if plan.CycleNo == 0 || plan.CycleNo > math.MaxUint32 || plan.RowVersion == math.MaxUint64 {
		return api.CommandResult{}, api.ErrInvalidArgument
	}
	incident, err := loadIncident(ctx, tx, plan.IncidentPublicID)
	if err != nil {
		return api.CommandResult{}, err
	}
	if incident.ID != plan.IncidentID || uint64(incident.CycleNo) != plan.CycleNo ||
		incident.Version != plan.IncidentVersion+1 {
		return api.CommandResult{}, api.ErrConflict
	}
	if incident.Status != "awaiting_approval" {
		return api.CommandResult{}, api.ErrInvalidTransition
	}
	var databaseNow time.Time
	if err := tx.QueryRowContext(ctx, "SELECT NOW(6)").Scan(&databaseNow); err != nil {
		return api.CommandResult{}, err
	}
	databaseNow = databaseNow.UTC()
	if !databaseNow.Before(plan.ExpiresAt) {
		return api.CommandResult{}, api.ErrConflict
	}
	decisionExpiresAt := databaseNow.Add(remediationDecisionTTL)
	if plan.ExpiresAt.Before(decisionExpiresAt) {
		decisionExpiresAt = plan.ExpiresAt
	}
	decision, err := remediation.NewDecision(
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
		return api.CommandResult{}, api.ErrInvalidArgument
	}
	if err := p.remediations.RecordDecisionIn(ctx, tx, plan.PublicID, plan.RowVersion, &decision); err != nil {
		// The repository can fail after its first write. Returning the original
		// error forces the owning command transaction to roll back instead of
		// durably recording a partial Decision without its Plan/task effects.
		return api.CommandResult{}, err
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
			return api.CommandResult{}, err
		}
		task, err := p.tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
			IncidentID: plan.IncidentID, CycleNo: uint32(plan.CycleNo),
			Type: asyncjob.TaskChangeEnsurePR, SubjectType: "remediation_plan", SubjectID: plan.ID,
			Transition: "change.ensure_pr", ExpectedSubjectVersion: nextPlanVersion,
			PayloadSchemaVersion: 1, Payload: payload,
			DedupeKey:      canonicalHash("change.ensure_pr", plan.PublicID, plan.CanonicalPlanHash, fmt.Sprint(nextPlanVersion)),
			MigratedLegacy: plan.MigratedLegacy, MigratedLegacyContext: plan.MigratedLegacyContext,
			Priority: 90, MaxAttempts: 5,
		})
		if err != nil {
			return api.CommandResult{}, err
		}
		metadata["task_id"] = task.PublicID
	} else {
		updated, err := tx.ExecContext(ctx, `UPDATE incidents
SET status = 'investigating', version = version + 1,
    needs_attention = FALSE, blocking_reason_code = NULL, blocked_at = NULL,
    updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND version = ?
  AND status = 'awaiting_approval'`, incident.ID, incident.CycleNo, incident.Version)
		if err != nil {
			return api.CommandResult{}, err
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return api.CommandResult{}, errors.New("remediation rejection lost the locked Incident transition")
		}
		incident.Status = "investigating"
		incident.Version++
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil || len(metadataJSON) > 8192 {
		return api.CommandResult{}, errors.New("remediation Decision Timeline metadata is invalid")
	}
	if err := appendCommandEvent(ctx, tx, incident, eventType, request.Actor, metadataJSON); err != nil {
		return api.CommandResult{}, err
	}
	return api.CommandResult{
		HTTPStatus: http.StatusAccepted,
		ResourceID: plan.PublicID,
		Status:     body.Decision,
		Version:    nextPlanVersion,
		Cycle:      plan.CycleNo,
	}, nil
}

func decodeRemediationDecisionCommand(request api.CommandRequest) (remediationDecisionCommandBody, error) {
	if len(request.CanonicalBody) == 0 || len(request.CanonicalBody) > 4096 {
		return remediationDecisionCommandBody{}, api.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(request.CanonicalBody))
	decoder.DisallowUnknownFields()
	var body remediationDecisionCommandBody
	if err := decoder.Decode(&body); err != nil {
		return remediationDecisionCommandBody{}, api.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return remediationDecisionCommandBody{}, api.ErrInvalidArgument
	}
	if body.Decision != string(remediation.DecisionApproved) && body.Decision != string(remediation.DecisionRejected) {
		return remediationDecisionCommandBody{}, api.ErrInvalidTransition
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.ExpectedVersion == 0 || body.ExpectedVersion != request.ExpectedVersion ||
		body.ExpectedHash != request.ExpectedHash || !validCommandSHA256(body.ExpectedHash) ||
		body.Reason == "" || len(body.Reason) > 1024 {
		return remediationDecisionCommandBody{}, api.ErrInvalidArgument
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

func appendCommandEvent(ctx context.Context, tx *sql.Tx, incident lockedIncident, eventType string, actor api.OwnerIdentity, metadata []byte) error {
	idempotency := canonicalHash("event", incident.PublicID, fmt.Sprint(incident.CycleNo), eventType, string(metadata))
	_, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
    (public_id, incident_id, cycle_no, event_schema_version,
     event_type, idempotency_key, migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json,
     occurred_at, created_at)
VALUES (?, ?, ?, 1, ?, ?, ?, ?, 'user', ?, ?, ?, NOW(6), NOW(6))`,
		uuid.NewString(), incident.ID, incident.CycleNo, eventType, idempotency,
		incident.MigratedLegacyContext, incident.MigratedLegacy,
		actor.Provider+":"+actor.Login, strings.ReplaceAll(eventType, "_", " "), metadata)
	return err
}

func storedResponse(resourceType, resourceID string, result api.CommandResult, commandErr error) (Response, error) {
	status := http.StatusAccepted
	code := ""
	if commandErr != nil {
		switch {
		case errors.Is(commandErr, api.ErrNotFound):
			status, code = http.StatusNotFound, "not_found"
		case errors.Is(commandErr, api.ErrStaleVersion):
			status, code = http.StatusConflict, "stale"
		case errors.Is(commandErr, api.ErrConflict):
			status, code = http.StatusConflict, "conflict"
		case errors.Is(commandErr, api.ErrInvalidTransition):
			status, code = http.StatusUnprocessableEntity, "invalid_transition"
		case errors.Is(commandErr, api.ErrNotImplemented):
			status, code = http.StatusNotImplemented, "not_implemented"
		case errors.Is(commandErr, api.ErrInvalidArgument):
			status, code = http.StatusBadRequest, "invalid_argument"
		case errors.Is(commandErr, api.ErrForbidden):
			status, code = http.StatusForbidden, "forbidden"
		default:
			return Response{}, commandErr
		}
	}
	body, err := json.Marshal(struct {
		Result api.CommandResult `json:"result"`
		Error  string            `json:"error,omitempty"`
	}{Result: result, Error: code})
	if err != nil {
		return Response{}, err
	}
	return Response{HTTPStatus: status, Body: body, ResourceType: resourceType, ResourcePublicID: resourceID}, nil
}

func errorFromCode(code string) error {
	switch code {
	case "not_found":
		return api.ErrNotFound
	case "stale":
		return api.ErrStaleVersion
	case "conflict":
		return api.ErrConflict
	case "invalid_transition":
		return api.ErrInvalidTransition
	case "not_implemented":
		return api.ErrNotImplemented
	case "invalid_argument":
		return api.ErrInvalidArgument
	case "forbidden":
		return api.ErrForbidden
	default:
		return api.ErrUnavailable
	}
}

func commandResourceType(kind api.CommandKind) string {
	if kind == api.CommandDecideRemediation {
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
