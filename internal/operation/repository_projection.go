package operation

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
)

const executionSelect = `SELECT execution.public_id,execution.subject_type,
COALESCE(card.public_id,plan.public_id),run.public_id,incident.public_id,revision.public_id,
COALESCE(card.action_type,plan.operation_type),execution.expected_content_hash,execution.status,execution.attempt,
execution.external_effect_started_at,execution.result_json,execution.failure_code,execution.failure_summary,
execution.created_at,execution.started_at,execution.completed_at
FROM operation_executions execution
LEFT JOIN agent_action_cards card ON card.id=execution.action_card_id
LEFT JOIN agent_operation_plans plan ON plan.id=execution.operation_plan_id
JOIN agent_runs run ON run.id=COALESCE(card.agent_run_id,plan.agent_run_id)
LEFT JOIN incidents incident ON incident.id=run.incident_id
JOIN configuration_revisions revision ON revision.id=execution.configuration_revision_id`

func (r *Repository) Execution(ctx context.Context, publicID string) (Execution, error) {
	item, err := scanExecution(r.db.QueryRowContext(ctx, executionSelect+` WHERE execution.public_id=?`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if err != nil {
		return Execution{}, err
	}
	if err = r.enrichExecution(ctx, &item); err != nil {
		return Execution{}, err
	}
	return item, nil
}

func (r *Repository) Executions(ctx context.Context, limit int) ([]Execution, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, executionSelect+` ORDER BY execution.created_at DESC,execution.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Execution, 0, limit)
	for rows.Next() {
		item, scanErr := scanExecution(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		if err = r.enrichExecution(ctx, &items[index]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanExecution(scanner rowScanner) (Execution, error) {
	var item Execution
	var incidentID, failureCode, failureSummary sql.NullString
	var externalStarted, startedAt, completedAt sql.NullTime
	var result []byte
	err := scanner.Scan(&item.ID, &item.SubjectType, &item.SubjectID, &item.RunID, &incidentID,
		&item.ConfigurationRevisionID, &item.OperationType, &item.ExpectedContentHash, &item.Status, &item.Attempt,
		&externalStarted, &result, &failureCode, &failureSummary, &item.CreatedAt, &startedAt, &completedAt)
	if err != nil {
		return Execution{}, err
	}
	if incidentID.Valid {
		item.IncidentID = incidentID.String
	}
	if externalStarted.Valid {
		value := externalStarted.Time.UTC()
		item.ExternalEffectStartedAt = &value
	}
	if len(result) > 0 {
		item.Result = append(item.Result[:0], result...)
	}
	if failureCode.Valid {
		item.FailureCode = failureCode.String
	}
	if failureSummary.Valid {
		item.FailureSummary = failureSummary.String
	}
	item.CreatedAt = item.CreatedAt.UTC()
	if startedAt.Valid {
		value := startedAt.Time.UTC()
		item.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		item.CompletedAt = &value
	}
	item.Events = []AuditEvent{}
	item.Links = []ContextLink{}
	return item, nil
}

func (r *Repository) enrichExecution(ctx context.Context, item *Execution) error {
	if item == nil {
		return ErrInvalidArgument
	}
	events, err := r.executionEvents(ctx, item.ID)
	if err != nil {
		return err
	}
	item.Events = events
	verification, err := r.executionVerification(ctx, item.ID)
	if err != nil {
		return err
	}
	item.Verification = verification
	item.Links = append(item.Links, ContextLink{
		Kind: "agent", Label: "Agent Investigation", Href: "/agent?investigation=" + url.QueryEscape(item.RunID),
	})
	item.Links = append(item.Links, ContextLink{
		Kind: "verification", Label: "Current Evidence Verify",
		Href:   "/devops?operation=" + url.QueryEscape(item.ID) + "#verification",
		Status: verificationStatus(verification),
	})
	if item.IncidentID != "" {
		item.Links = append(item.Links, ContextLink{
			Kind: "incident", Label: "Incident Recovery Verification",
			Href: "/incidents/" + url.PathEscape(item.IncidentID) + "#recovery-zone", Status: "follow_up_required",
		})
	}
	return nil
}

func verificationStatus(value *VerificationObservation) string {
	if value == nil {
		return "pending"
	}
	return value.Status
}

func (r *Repository) executionEvents(ctx context.Context, executionPublicID string) ([]AuditEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT event.public_id,event.sequence_no,event.event_type,event.payload_json,
event.content_hash,event.occurred_at FROM operation_events event
JOIN operation_executions execution ON execution.id=event.execution_id
WHERE execution.public_id=? ORDER BY event.sequence_no`, executionPublicID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]AuditEvent, 0)
	for rows.Next() {
		var item AuditEvent
		if err = rows.Scan(&item.ID, &item.Sequence, &item.Type, &item.Payload, &item.ContentHash, &item.OccurredAt); err != nil {
			return nil, err
		}
		item.OccurredAt = item.OccurredAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) executionVerification(ctx context.Context, executionPublicID string) (*VerificationObservation, error) {
	var item VerificationObservation
	err := r.db.QueryRowContext(ctx, `SELECT observation.public_id,observation.source,observation.status,
observation.provider_identity_json,observation.evidence_json,observation.content_hash,
observation.summary,observation.observed_at
FROM operation_verification_observations observation
JOIN operation_executions execution ON execution.id=observation.execution_id
WHERE execution.public_id=?`, executionPublicID).Scan(&item.ID, &item.Source, &item.Status,
		&item.ProviderIdentity, &item.Evidence, &item.ContentHash, &item.Summary, &item.ObservedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.ObservedAt = item.ObservedAt.UTC()
	return &item, nil
}
