package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type incidentNavigationContext struct {
	IncidentID         uint64
	PublicID           string
	Cycle              uint64
	OperationalScopeID string
	Cluster            string
	Environment        string
	Namespace          string
	Service            string
	Resource           IncidentResourceRefView
	From               time.Time
	To                 time.Time
}

// enrichIncidentViews keeps the list and detail payloads on the same durable,
// current-cycle coordination projection. The single batched query avoids a
// per-Incident fan-out on the list route.
func (p *MySQLQueryPort) enrichIncidentViews(ctx context.Context, items []IncidentView) error {
	if len(items) == 0 {
		return nil
	}
	positions := make(map[string]int, len(items))
	args := make([]any, 0, len(items))
	marks := make([]string, 0, len(items))
	for index := range items {
		positions[items[index].ID] = index
		args = append(args, items[index].ID)
		marks = append(marks, "?")
	}
	rows, err := p.db.QueryContext(ctx, `
SELECT i.public_id, i.cluster, i.environment, i.namespace, i.service_name,
       i.target_kind, i.target_name, i.first_seen_at, i.last_seen_at, i.resolved_at,
       COALESCE(scope.public_id, ''), COALESCE(resource.resource_id, ''),
       (SELECT COUNT(*) FROM alert_incident_links relation
        WHERE relation.incident_id = i.id AND relation.incident_cycle_no = i.cycle_no),
       COALESCE(latest_verification.public_id, ''), COALESCE(latest_verification.status, ''),
       latest_verification.common_success_since, latest_verification.common_window_completed_at,
       (SELECT COUNT(*) FROM verification_runs attempt
        WHERE attempt.incident_id = i.id AND attempt.cycle_no = i.cycle_no),
       (SELECT COUNT(*) FROM verification_runs attempt
        WHERE attempt.incident_id = i.id AND attempt.cycle_no = i.cycle_no
          AND attempt.status IN ('failed','timed_out','inconclusive')),
       COALESCE(report.public_id, ''),
       EXISTS(SELECT 1 FROM remediation_plans plan
              WHERE plan.incident_id = i.id AND plan.cycle_no = i.cycle_no),
       COALESCE(latest_investigation.public_id, ''),
       EXISTS(
         SELECT 1 FROM change_requests change_request
         WHERE change_request.incident_id = i.id AND change_request.cycle_no = i.cycle_no
           AND (change_request.status IN ('pending','pr_open','merged','syncing','rolling_out')
             OR (change_request.external_write_started_at IS NOT NULL
                 AND change_request.status NOT IN ('delivered','superseded')))
         UNION ALL
         SELECT 1 FROM verification_runs active_verification
         WHERE active_verification.incident_id = i.id AND active_verification.cycle_no = i.cycle_no
           AND active_verification.status IN ('pending','running')
         UNION ALL
         SELECT 1 FROM async_tasks active_task
         WHERE active_task.incident_id = i.id AND active_task.cycle_no = i.cycle_no
           AND active_task.status IN ('ready','running')
       )
FROM incidents i
LEFT JOIN active_configuration active ON active.singleton_id = 1
LEFT JOIN operational_scopes scope
  ON scope.configuration_revision_id = active.configuration_revision_id
 AND scope.cluster_id = i.cluster
LEFT JOIN resource_identities resource ON resource.id = (
  SELECT candidate.id FROM resource_identities candidate
  WHERE candidate.cluster_id = i.cluster AND candidate.namespace = i.namespace
    AND candidate.kind = i.target_kind AND candidate.name = i.target_name
  ORDER BY candidate.last_seen_at DESC, candidate.id DESC LIMIT 1
)
LEFT JOIN verification_runs latest_verification ON latest_verification.id = (
  SELECT candidate.id FROM verification_runs candidate
  WHERE candidate.incident_id = i.id AND candidate.cycle_no = i.cycle_no
  ORDER BY candidate.created_at DESC, candidate.id DESC LIMIT 1
)
LEFT JOIN resolution_reports report
  ON report.incident_id = i.id AND report.cycle_no = i.cycle_no
LEFT JOIN agent_runs latest_investigation ON latest_investigation.id = (
  SELECT candidate.id FROM agent_runs candidate
  WHERE candidate.incident_id = i.id AND candidate.cycle_no = i.cycle_no
    AND candidate.subject_type = 'incident'
  ORDER BY candidate.created_at DESC, candidate.id DESC LIMIT 1
)
WHERE i.public_id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("load Incident coordination projection: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			publicID, cluster, environment, namespace, service, kind, name string
			scopeID, resourceID, latestVerificationID, latestStatus        string
			reportID, latestInvestigationID                                string
			firstSeen, lastSeen                                            time.Time
			resolvedAt, commonStartedAt, commonCompletedAt                 sql.NullTime
			relatedAlerts, attempts, failedAttempts                        uint64
			hasAction, hasCloseBlocker                                     bool
		)
		if err := rows.Scan(
			&publicID, &cluster, &environment, &namespace, &service, &kind, &name,
			&firstSeen, &lastSeen, &resolvedAt, &scopeID, &resourceID, &relatedAlerts,
			&latestVerificationID, &latestStatus, &commonStartedAt, &commonCompletedAt,
			&attempts, &failedAttempts, &reportID, &hasAction, &latestInvestigationID,
			&hasCloseBlocker,
		); err != nil {
			return fmt.Errorf("scan Incident coordination projection: %w", err)
		}
		index, ok := positions[publicID]
		if !ok {
			continue
		}
		item := &items[index]
		item.FirstSeenAt = firstSeen.UTC()
		item.LastSeenAt = lastSeen.UTC()
		if resolvedAt.Valid {
			value := resolvedAt.Time.UTC()
			item.ResolvedAt = &value
		}
		item.RelatedAlertCount = relatedAlerts
		item.OperationalContext = IncidentOperationalContextView{
			OperationalScopeID: scopeID,
			Cluster:            cluster,
			Environment:        environment,
			Namespace:          namespace,
			Service:            service,
			Resource: IncidentResourceRefView{
				ID: resourceID, Kind: kind, Namespace: namespace, Name: name,
			},
			TimeRange: IncidentTimeRangeView{From: firstSeen.UTC(), To: lastSeen.UTC()},
		}
		item.Attention = IncidentAttentionView{
			Required: item.NeedsAttention, ReasonCode: item.BlockingReasonCode,
			Stage: incidentCoordinationStage(item.Status),
		}
		item.Recovery = IncidentRecoveryView{
			State:                    incidentRecoveryState(item.Status, latestStatus, reportID),
			VerificationAttempts:     attempts,
			FailedVerificationCount:  failedAttempts,
			LatestVerificationID:     latestVerificationID,
			LatestVerificationStatus: latestStatus,
			ResolutionReportID:       reportID,
		}
		if commonStartedAt.Valid {
			value := commonStartedAt.Time.UTC()
			item.Recovery.CommonWindowStartedAt = &value
		}
		if commonCompletedAt.Valid {
			value := commonCompletedAt.Time.UTC()
			item.Recovery.CommonWindowCompletedAt = &value
		}
		item.Recovery.CanClose = item.Status == "resolved" && latestStatus == "passed" &&
			commonCompletedAt.Valid && reportID != "" && !hasCloseBlocker
		item.ContextLinks = incidentContextLinks(incidentNavigationContext{
			PublicID: publicID, Cycle: item.Cycle, OperationalScopeID: scopeID,
			Cluster: cluster, Environment: environment, Namespace: namespace, Service: service,
			Resource: item.OperationalContext.Resource, From: firstSeen.UTC(), To: lastSeen.UTC(),
		}, latestInvestigationID, hasAction)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate Incident coordination projection: %w", err)
	}
	return nil
}

func incidentCoordinationStage(status string) string {
	switch status {
	case "detected":
		return "detect"
	case "investigating":
		return "investigate"
	case "awaiting_approval":
		return "decide"
	case "delivering":
		return "act"
	case "verifying":
		return "verify"
	case "resolved":
		return "recovered"
	case "closed":
		return "closed"
	default:
		return "unknown"
	}
}

func incidentRecoveryState(incidentStatus, verificationStatus, reportID string) string {
	if (incidentStatus == "resolved" || incidentStatus == "closed") && reportID != "" {
		return "recovered"
	}
	if incidentStatus == "verifying" || verificationStatus == "pending" || verificationStatus == "running" {
		return "verifying"
	}
	if verificationStatus == "failed" || verificationStatus == "timed_out" || verificationStatus == "inconclusive" {
		return "investigate"
	}
	if incidentStatus == "delivering" {
		return "awaiting_verification"
	}
	return "not_started"
}

func incidentContextLinks(context incidentNavigationContext, investigationID string, hasAction bool) []IncidentContextLinkView {
	if context.OperationalScopeID == "" || context.PublicID == "" || context.From.IsZero() || context.To.IsZero() {
		return []IncidentContextLinkView{}
	}
	base := map[string]string{
		"cluster_id": context.Cluster,
		"namespace":  context.Namespace,
		"resource":   context.Resource.ID,
		"from":       context.From.UTC().Format(time.RFC3339Nano),
		"to":         context.To.UTC().Format(time.RFC3339Nano),
	}
	links := make([]IncidentContextLinkView, 0, 6)
	for _, target := range []struct{ workspace, path string }{
		{"monitoring", "/monitoring"}, {"logs", "/logs"}, {"traces", "/traces"},
	} {
		links = append(links, coordinationLink(target.workspace, target.path, base, context.OperationalScopeID))
	}
	agentQuery := cloneStringMap(base)
	agentQuery["incident"] = context.PublicID
	if investigationID != "" {
		agentQuery["investigation"] = investigationID
	}
	links = append(links, coordinationLink("agent", "/agent", agentQuery, context.OperationalScopeID))
	links = append(links, coordinationLink("alerts", "/alerts", map[string]string{
		"incident": context.PublicID,
	}, context.OperationalScopeID))
	if hasAction {
		links = append(links, coordinationLink("devops", "/devops", map[string]string{
			"incident": context.PublicID,
		}, context.OperationalScopeID))
	}
	return links
}

func coordinationLink(workspace, path string, query map[string]string, scopeID string) IncidentContextLinkView {
	return IncidentContextLinkView{
		Workspace: workspace, Path: path, Query: cloneStringMap(query),
		OperationalScopeID: scopeID, External: false,
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		if strings.TrimSpace(value) != "" {
			result[key] = value
		}
	}
	return result
}

func (p *MySQLQueryPort) loadIncidentNavigationContext(ctx context.Context, publicID string) (incidentNavigationContext, error) {
	id, err := ParsePublicUUID(publicID)
	if err != nil {
		return incidentNavigationContext{}, err
	}
	var result incidentNavigationContext
	err = p.db.QueryRowContext(ctx, `
SELECT i.id, i.public_id, i.cycle_no, i.cluster, i.environment, i.namespace,
       i.service_name, COALESCE(resource.resource_id, ''), i.target_kind,
       i.target_name, i.first_seen_at, i.last_seen_at, COALESCE(scope.public_id, '')
FROM incidents i
LEFT JOIN active_configuration active ON active.singleton_id = 1
LEFT JOIN operational_scopes scope
  ON scope.configuration_revision_id = active.configuration_revision_id
 AND scope.cluster_id = i.cluster
LEFT JOIN resource_identities resource ON resource.id = (
  SELECT candidate.id FROM resource_identities candidate
  WHERE candidate.cluster_id = i.cluster AND candidate.namespace = i.namespace
    AND candidate.kind = i.target_kind AND candidate.name = i.target_name
  ORDER BY candidate.last_seen_at DESC, candidate.id DESC LIMIT 1
)
WHERE i.public_id = ?`, id).Scan(
		&result.IncidentID, &result.PublicID, &result.Cycle, &result.Cluster,
		&result.Environment, &result.Namespace, &result.Service, &result.Resource.ID,
		&result.Resource.Kind, &result.Resource.Name, &result.From, &result.To,
		&result.OperationalScopeID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return incidentNavigationContext{}, ErrNotFound
	}
	if err != nil {
		return incidentNavigationContext{}, fmt.Errorf("load Incident navigation context: %w", err)
	}
	result.Resource.Namespace = result.Namespace
	result.From = result.From.UTC()
	result.To = result.To.UTC()
	return result, nil
}

func (p *MySQLQueryPort) listIncidentAlertRelations(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	contextView, err := p.loadIncidentNavigationContext(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	where := []string{"relation.incident_id = ?", "relation.incident_cycle_no = ?"}
	args := []any{contextView.IncidentID, contextView.Cycle}
	if request.Cursor != "" {
		position, cursorErr := p.coordinationCursor(ctx, contextView, "alert_incident_links", "created_at", request.Cursor)
		if cursorErr != nil {
			return QueryResponse{}, cursorErr
		}
		where = append(where, "(relation.created_at < ? OR (relation.created_at = ? AND relation.id < ?))")
		args = append(args, position.At, position.At, position.ID)
	}
	args = append(args, request.Limit+1)
	rows, err := p.db.QueryContext(ctx, `
SELECT relation.id, relation.public_id, relation.incident_cycle_no,
       alert.public_id, alert.status, alert.severity, alert.summary, alert.category,
       alert.source, alert.cluster, alert.environment, alert.namespace,
       alert.service_name, alert.target_kind, alert.target_name,
       alert.first_seen_at, alert.last_seen_at, alert.resolved_at,
       relation.provenance, revision.public_id, policy.public_id,
       alert.migrated_legacy, alert.migrated_legacy_context, relation.created_at
FROM alert_incident_links relation
JOIN alerts alert ON alert.id = relation.alert_id
LEFT JOIN configuration_revisions revision ON revision.id = relation.configuration_revision_id
LEFT JOIN escalation_policies policy ON policy.id = relation.escalation_policy_id
WHERE `+strings.Join(where, " AND ")+`
ORDER BY relation.created_at DESC, relation.id DESC
LIMIT ?`, args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list Incident Alert relations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type relationRow struct {
		NumericID uint64
		View      IncidentAlertRelationView
	}
	values := make([]relationRow, 0, request.Limit+1)
	for rows.Next() {
		var row relationRow
		var resolvedAt sql.NullTime
		var revisionID, policyID sql.NullString
		if err := rows.Scan(
			&row.NumericID, &row.View.ID, &row.View.Cycle, &row.View.AlertID,
			&row.View.Status, &row.View.Severity, &row.View.Summary, &row.View.Category,
			&row.View.Source, &row.View.Cluster, &row.View.Environment, &row.View.Namespace,
			&row.View.Service, &row.View.TargetKind, &row.View.TargetName,
			&row.View.FirstSeenAt, &row.View.LastSeenAt, &resolvedAt, &row.View.Provenance,
			&revisionID, &policyID, &row.View.MigratedLegacy, &row.View.MigratedLegacyContext,
			&row.View.CreatedAt,
		); err != nil {
			return QueryResponse{}, fmt.Errorf("scan Incident Alert relation: %w", err)
		}
		if resolvedAt.Valid {
			value := resolvedAt.Time.UTC()
			row.View.ResolvedAt = &value
		}
		row.View.ConfigurationRevisionID = revisionID.String
		row.View.EscalationPolicyID = policyID.String
		row.View.FirstSeenAt = row.View.FirstSeenAt.UTC()
		row.View.LastSeenAt = row.View.LastSeenAt.UTC()
		row.View.CreatedAt = row.View.CreatedAt.UTC()
		row.View.ContextLink = coordinationLink("alerts", "/alerts/"+row.View.AlertID, map[string]string{
			"cluster_id": row.View.Cluster, "namespace": row.View.Namespace,
		}, contextView.OperationalScopeID)
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate Incident Alert relations: %w", err)
	}
	response := QueryResponse{AlertRelations: []IncidentAlertRelationView{}}
	for index := 0; index < len(values) && index < request.Limit; index++ {
		response.AlertRelations = append(response.AlertRelations, values[index].View)
	}
	if len(values) > request.Limit && request.Limit > 0 {
		response.NextCursor = values[request.Limit-1].View.ID
	}
	return response, nil
}

func (p *MySQLQueryPort) listIncidentEvidence(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	contextView, err := p.loadIncidentNavigationContext(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	where := []string{"evidence.incident_id = ?", "evidence.cycle_no = ?"}
	args := []any{contextView.IncidentID, contextView.Cycle}
	if request.Cursor != "" {
		position, cursorErr := p.coordinationCursor(ctx, contextView, "evidence_items", "collected_at", request.Cursor)
		if cursorErr != nil {
			return QueryResponse{}, cursorErr
		}
		where = append(where, "(evidence.collected_at < ? OR (evidence.collected_at = ? AND evidence.id < ?))")
		args = append(args, position.At, position.At, position.ID)
	}
	args = append(args, request.Limit+1)
	rows, err := p.db.QueryContext(ctx, `
SELECT evidence.id, evidence.public_id, evidence.cycle_no, evidence.type, evidence.source,
       evidence.producer_type, evidence.producer_id, evidence.producer_version,
       evidence.tool_name, evidence.resource_ref, evidence.time_range_json,
       evidence.query_text, evidence.summary, evidence.content_hash,
       evidence.provenance_json, evidence.valid, evidence.truncated,
       evidence.collected_at, evidence.observed_at,
       evidence.migrated_legacy, evidence.migrated_legacy_context
FROM evidence_items evidence
WHERE `+strings.Join(where, " AND ")+`
ORDER BY evidence.collected_at DESC, evidence.id DESC
LIMIT ?`, args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list Incident Evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type evidenceRow struct {
		NumericID uint64
		View      IncidentEvidenceView
	}
	values := make([]evidenceRow, 0, request.Limit+1)
	for rows.Next() {
		var row evidenceRow
		var producerType, producerID, producerVersion, contentHash sql.NullString
		var timeRange, provenance []byte
		var observedAt sql.NullTime
		if err := rows.Scan(
			&row.NumericID, &row.View.ID, &row.View.Cycle, &row.View.Type, &row.View.Source,
			&producerType, &producerID, &producerVersion, &row.View.ToolName,
			&row.View.ResourceRef, &timeRange, &row.View.QueryText, &row.View.Summary,
			&contentHash, &provenance, &row.View.Valid, &row.View.Truncated,
			&row.View.CollectedAt, &observedAt, &row.View.MigratedLegacy,
			&row.View.MigratedLegacyContext,
		); err != nil {
			return QueryResponse{}, fmt.Errorf("scan Incident Evidence: %w", err)
		}
		row.View.ProducerType = producerType.String
		row.View.ProducerID = producerID.String
		row.View.ProducerVersion = producerVersion.String
		row.View.ContentHash = contentHash.String
		row.View.TimeRange = validRawJSON(timeRange)
		row.View.Provenance = validRawJSON(provenance)
		row.View.CollectedAt = row.View.CollectedAt.UTC()
		if observedAt.Valid {
			value := observedAt.Time.UTC()
			row.View.ObservedAt = &value
		}
		workspace := evidenceWorkspace(row.View)
		query := incidentWorkspaceQuery(contextView)
		query["evidence"] = row.View.ID
		row.View.ContextLink = coordinationLink(workspace, "/"+workspace, query, contextView.OperationalScopeID)
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate Incident Evidence: %w", err)
	}
	response := QueryResponse{Evidence: []IncidentEvidenceView{}}
	for index := 0; index < len(values) && index < request.Limit; index++ {
		response.Evidence = append(response.Evidence, values[index].View)
	}
	if len(values) > request.Limit && request.Limit > 0 {
		response.NextCursor = values[request.Limit-1].View.ID
	}
	return response, nil
}

func evidenceWorkspace(item IncidentEvidenceView) string {
	identity := strings.ToLower(strings.Join([]string{item.Type, item.Source, item.ToolName}, " "))
	switch {
	case strings.Contains(identity, "trace"), strings.Contains(identity, "tempo"):
		return "traces"
	case strings.Contains(identity, "log"), strings.Contains(identity, "elastic"):
		return "logs"
	default:
		return "monitoring"
	}
}

func validRawJSON(value []byte) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func (p *MySQLQueryPort) listIncidentInvestigations(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	contextView, err := p.loadIncidentNavigationContext(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	where := []string{"run.incident_id = ?", "run.cycle_no = ?", "run.subject_type = 'incident'"}
	args := []any{contextView.IncidentID, contextView.Cycle}
	if request.Cursor != "" {
		position, cursorErr := p.coordinationCursor(ctx, contextView, "agent_runs", "created_at", request.Cursor)
		if cursorErr != nil {
			return QueryResponse{}, cursorErr
		}
		where = append(where, "(run.created_at < ? OR (run.created_at = ? AND run.id < ?))")
		args = append(args, position.At, position.At, position.ID)
	}
	args = append(args, request.Limit+1)
	rows, err := p.db.QueryContext(ctx, `
SELECT run.id, run.public_id, run.cycle_no, run.status, run.row_version,
       run.objective, run.outcome, run.failure_code, run.failure_summary,
       run.model_provider, run.actual_model, run.prompt_version,
       run.used_steps, run.max_steps, run.started_at, run.completed_at,
       run.created_at, run.updated_at, run.migrated_legacy, run.migrated_legacy_context
FROM agent_runs run
WHERE `+strings.Join(where, " AND ")+`
ORDER BY run.created_at DESC, run.id DESC
LIMIT ?`, args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list Incident Investigations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type investigationRow struct {
		NumericID uint64
		View      IncidentInvestigationView
	}
	values := make([]investigationRow, 0, request.Limit+1)
	for rows.Next() {
		var row investigationRow
		var outcome, failureCode, provider, actualModel sql.NullString
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&row.NumericID, &row.View.ID, &row.View.Cycle, &row.View.Status,
			&row.View.Version, &row.View.Objective, &outcome, &failureCode,
			&row.View.FailureSummary, &provider, &actualModel, &row.View.PromptVersion,
			&row.View.UsedSteps, &row.View.MaxSteps, &startedAt, &completedAt,
			&row.View.CreatedAt, &row.View.UpdatedAt, &row.View.MigratedLegacy,
			&row.View.MigratedLegacyContext,
		); err != nil {
			return QueryResponse{}, fmt.Errorf("scan Incident Investigation: %w", err)
		}
		row.View.Outcome = outcome.String
		row.View.FailureCode = failureCode.String
		row.View.ModelProvider = provider.String
		row.View.ActualModel = actualModel.String
		if startedAt.Valid {
			value := startedAt.Time.UTC()
			row.View.StartedAt = &value
		}
		if completedAt.Valid {
			value := completedAt.Time.UTC()
			row.View.CompletedAt = &value
		}
		row.View.CreatedAt = row.View.CreatedAt.UTC()
		row.View.UpdatedAt = row.View.UpdatedAt.UTC()
		query := incidentWorkspaceQuery(contextView)
		query["investigation"] = row.View.ID
		row.View.ContextLink = coordinationLink("agent", "/agent", query, contextView.OperationalScopeID)
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate Incident Investigations: %w", err)
	}
	response := QueryResponse{Investigations: []IncidentInvestigationView{}}
	for index := 0; index < len(values) && index < request.Limit; index++ {
		response.Investigations = append(response.Investigations, values[index].View)
	}
	if len(values) > request.Limit && request.Limit > 0 {
		response.NextCursor = values[request.Limit-1].View.ID
	}
	return response, nil
}

func (p *MySQLQueryPort) getIncidentDecision(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	contextView, err := p.loadIncidentNavigationContext(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	var (
		investigationID, outcome, diagnosisSummary                     sql.NullString
		planID, planStatus, decisionID, decision, reason, actor        sql.NullString
		deliveryID, deliveryStatus, verificationID, verificationStatus sql.NullString
		verificationTrigger                                            sql.NullString
		recoveryDecisionID, recoveryDecision, recoveryReason           sql.NullString
		recoveryActor, recoverySummary, recoveryInvestigationID        sql.NullString
		decisionAt, verificationAt                                     sql.NullTime
		recoveryDecisionAt                                             sql.NullTime
	)
	err = p.db.QueryRowContext(ctx, `
SELECT investigation.public_id, investigation.outcome,
       JSON_UNQUOTE(JSON_EXTRACT(investigation.final_diagnosis, '$.summary')),
       plan.public_id, plan.status, decision.public_id, decision.decision,
       decision.reason, decision.actor_login, decision.created_at,
       change_request.public_id, change_request.status,
	       verification.public_id, verification.status, verification.trigger_type,
	       verification.created_at,
	       recovery_decision.public_id,
	       JSON_UNQUOTE(JSON_EXTRACT(recovery_decision.metadata_json, '$.decision')),
	       JSON_UNQUOTE(JSON_EXTRACT(recovery_decision.metadata_json, '$.reason')),
	       recovery_decision.actor_id, recovery_decision.summary,
	       JSON_UNQUOTE(JSON_EXTRACT(recovery_decision.metadata_json, '$.investigation_id')),
	       recovery_decision.occurred_at
FROM incidents incident
LEFT JOIN agent_runs investigation ON investigation.id = (
  SELECT candidate.id FROM agent_runs candidate
  WHERE candidate.incident_id = incident.id AND candidate.cycle_no = incident.cycle_no
    AND candidate.subject_type = 'incident'
  ORDER BY candidate.created_at DESC, candidate.id DESC LIMIT 1
)
LEFT JOIN remediation_plans plan ON plan.id = (
  SELECT candidate.id FROM remediation_plans candidate
  WHERE candidate.incident_id = incident.id AND candidate.cycle_no = incident.cycle_no
  ORDER BY candidate.created_at DESC, candidate.id DESC LIMIT 1
)
LEFT JOIN remediation_decisions decision ON decision.plan_id = plan.id AND decision.imported_history = FALSE
LEFT JOIN change_requests change_request ON change_request.plan_id = plan.id
LEFT JOIN incident_events recovery_decision ON recovery_decision.id = (
  SELECT candidate.id FROM incident_events candidate
  WHERE candidate.incident_id = incident.id AND candidate.cycle_no = incident.cycle_no
    AND candidate.event_type = 'incident_recovery_decided'
  ORDER BY candidate.occurred_at DESC, candidate.id DESC LIMIT 1
)
LEFT JOIN verification_runs verification ON verification.id = (
  SELECT candidate.id FROM verification_runs candidate
  WHERE candidate.incident_id = incident.id AND candidate.cycle_no = incident.cycle_no
  ORDER BY candidate.created_at DESC, candidate.id DESC LIMIT 1
)
WHERE incident.id = ? AND incident.cycle_no = ?`, contextView.IncidentID, contextView.Cycle).Scan(
		&investigationID, &outcome, &diagnosisSummary, &planID, &planStatus,
		&decisionID, &decision, &reason, &actor, &decisionAt, &deliveryID,
		&deliveryStatus, &verificationID, &verificationStatus, &verificationTrigger,
		&verificationAt, &recoveryDecisionID, &recoveryDecision, &recoveryReason,
		&recoveryActor, &recoverySummary, &recoveryInvestigationID, &recoveryDecisionAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return QueryResponse{}, ErrNotFound
	}
	if err != nil {
		return QueryResponse{}, fmt.Errorf("load Incident decision projection: %w", err)
	}
	if !investigationID.Valid && !planID.Valid && !verificationID.Valid && !recoveryDecisionID.Valid {
		return QueryResponse{}, nil
	}
	item := &IncidentDecisionView{
		Cycle: contextView.Cycle, Kind: "pending", Status: "pending",
		InvestigationID: investigationID.String, RemediationPlanID: planID.String,
		DecisionID: decisionID.String, Decision: decision.String, Reason: reason.String,
		Actor: actor.String, DeliveryID: deliveryID.String, VerificationID: verificationID.String,
	}
	switch {
	case recoveryDecisionID.Valid:
		item.Kind = "recovery"
		item.Status = firstNonEmpty(verificationStatus.String, "decided")
		item.InvestigationID = firstNonEmpty(recoveryInvestigationID.String, investigationID.String)
		item.DecisionID = recoveryDecisionID.String
		item.Decision = firstNonEmpty(recoveryDecision.String, "verify_recovery")
		item.Reason = recoveryReason.String
		item.Actor = recoveryActor.String
		item.Summary = firstNonEmpty(recoverySummary.String, diagnosisSummary.String, "Owner 已决定进入恢复验证")
	case planID.Valid:
		item.Kind = "action"
		item.Status = firstNonEmpty(decision.String, planStatus.String)
		item.Summary = firstNonEmpty(reason.String, diagnosisSummary.String, "动作方案等待 Owner 决策")
	case verificationTrigger.String == "no_change_signal":
		item.Kind = "no_change"
		item.Status = verificationStatus.String
		item.Decision = "no_change"
		item.Summary = firstNonEmpty(diagnosisSummary.String, "未执行变更，进入恢复验证")
	default:
		item.Status = firstNonEmpty(outcome.String, verificationStatus.String, "pending")
		item.Summary = firstNonEmpty(diagnosisSummary.String, "调查结果等待决策")
	}
	if recoveryDecisionID.Valid && recoveryDecisionAt.Valid {
		value := recoveryDecisionAt.Time.UTC()
		item.DecidedAt = &value
	} else if decisionAt.Valid {
		value := decisionAt.Time.UTC()
		item.DecidedAt = &value
	} else if verificationAt.Valid && verificationTrigger.String == "no_change_signal" {
		value := verificationAt.Time.UTC()
		item.DecidedAt = &value
	}
	if deliveryID.Valid {
		link := coordinationLink("devops", "/devops", map[string]string{
			"incident": contextView.PublicID, "delivery": deliveryID.String,
		}, contextView.OperationalScopeID)
		item.ContextLink = &link
	} else if item.InvestigationID != "" {
		query := incidentWorkspaceQuery(contextView)
		query["investigation"] = item.InvestigationID
		link := coordinationLink("agent", "/agent", query, contextView.OperationalScopeID)
		item.ContextLink = &link
	}
	return QueryResponse{Decision: item}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func incidentWorkspaceQuery(contextView incidentNavigationContext) map[string]string {
	return cloneStringMap(map[string]string{
		"cluster_id": contextView.Cluster,
		"namespace":  contextView.Namespace,
		"resource":   contextView.Resource.ID,
		"from":       contextView.From.UTC().Format(time.RFC3339Nano),
		"to":         contextView.To.UTC().Format(time.RFC3339Nano),
		"incident":   contextView.PublicID,
	})
}

func (p *MySQLQueryPort) coordinationCursor(
	ctx context.Context,
	contextView incidentNavigationContext,
	table, sortColumn, publicID string,
) (mysqlQueryPosition, error) {
	id, err := ParsePublicUUID(publicID)
	if err != nil {
		return mysqlQueryPosition{}, err
	}
	allowed := map[string]string{
		"alert_incident_links": "incident_cycle_no",
		"evidence_items":       "cycle_no",
		"agent_runs":           "cycle_no",
	}
	cycleColumn, ok := allowed[table]
	if !ok || (sortColumn != "created_at" && sortColumn != "collected_at") {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	var result mysqlQueryPosition
	query := fmt.Sprintf(`SELECT id, %s FROM %s
WHERE public_id = ? AND incident_id = ? AND %s = ?`, sortColumn, table, cycleColumn)
	err = p.db.QueryRowContext(ctx, query, id, contextView.IncidentID, contextView.Cycle).Scan(&result.ID, &result.At)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	if err != nil {
		return mysqlQueryPosition{}, fmt.Errorf("load Incident coordination cursor: %w", err)
	}
	return result, nil
}
