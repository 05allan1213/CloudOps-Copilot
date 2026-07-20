package apiv3

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MySQLQueryPort serves V3 product queries from durable projections only. It
// deliberately has no adapter dependencies, so a GET cannot reconcile or
// access Kubernetes, GitHub, Argo CD, or observability backends.
type MySQLQueryPort struct {
	db *sql.DB
}

var _ QueryPort = (*MySQLQueryPort)(nil)

func NewMySQLQueryPort(db *sql.DB) (*MySQLQueryPort, error) {
	if db == nil {
		return nil, errors.New("V3 query database is required")
	}
	return &MySQLQueryPort{db: db}, nil
}

func (p *MySQLQueryPort) Query(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	if p == nil || p.db == nil {
		return QueryResponse{}, ErrUnavailable
	}
	request.Limit = boundedQueryLimit(request.Limit)
	switch request.Kind {
	case QueryIncidents:
		return p.listIncidents(ctx, request)
	case QueryIncident:
		return p.getIncident(ctx, request.IncidentID)
	case QuerySignals, QueryEvidence, QueryInvestigations, QueryRemediationPlans, QueryVerifications:
		return p.listIncidentResources(ctx, request)
	case QueryTimeline:
		return p.listTimeline(ctx, request)
	case QueryDelivery:
		return p.getIncidentResource(ctx, request)
	case QueryResolutionReport:
		return p.getResolutionReport(ctx, request)
	case QueryEvents:
		return p.listEvents(ctx, request)
	default:
		return QueryResponse{}, ErrInvalidArgument
	}
}

type mysqlIncidentRef struct {
	ID      uint64
	CycleNo uint64
}

func (p *MySQLQueryPort) incidentRef(ctx context.Context, publicID string) (mysqlIncidentRef, error) {
	id, err := ParsePublicUUID(publicID)
	if err != nil {
		return mysqlIncidentRef{}, err
	}
	var result mysqlIncidentRef
	err = p.db.QueryRowContext(ctx, `
SELECT id, cycle_no
FROM incidents
WHERE public_id = ? AND domain_schema_version = 3`, id).Scan(&result.ID, &result.CycleNo)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlIncidentRef{}, ErrNotFound
	}
	if err != nil {
		return mysqlIncidentRef{}, fmt.Errorf("load V3 Incident identity: %w", err)
	}
	return result, nil
}

func (p *MySQLQueryPort) listIncidents(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	where := []string{"domain_schema_version = 3"}
	args := make([]any, 0, 8)
	if request.Status != "" {
		if !validIncidentStatus(request.Status) {
			return QueryResponse{}, ErrInvalidArgument
		}
		where = append(where, "v3_status = ?")
		args = append(args, request.Status)
	}
	if request.Severity != "" {
		if !validSeverity(request.Severity) {
			return QueryResponse{}, ErrInvalidArgument
		}
		where = append(where, "severity = ?")
		args = append(args, request.Severity)
	}
	if request.Service != "" {
		if len(request.Service) > 255 || containsControl(request.Service) {
			return QueryResponse{}, ErrInvalidArgument
		}
		where = append(where, "service_name = ?")
		args = append(args, request.Service)
	}
	if request.Cursor != "" {
		cursor, err := p.incidentCursor(ctx, request.Cursor)
		if err != nil {
			return QueryResponse{}, err
		}
		where = append(where, "(updated_at < ? OR (updated_at = ? AND id < ?))")
		args = append(args, cursor.At, cursor.At, cursor.ID)
	}
	args = append(args, request.Limit+1)
	rows, err := p.db.QueryContext(ctx, `
SELECT id, public_id, cycle_no, v3_status, severity, summary, version,
       needs_attention, blocking_reason_code, created_at, updated_at
FROM incidents
WHERE `+strings.Join(where, " AND ")+`
ORDER BY updated_at DESC, id DESC
LIMIT ?`, args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list V3 Incidents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type resultRow struct {
		ID        uint64
		PublicID  string
		UpdatedAt time.Time
		View      IncidentView
	}
	items := make([]resultRow, 0, request.Limit+1)
	for rows.Next() {
		var row resultRow
		var blocking sql.NullString
		if err := rows.Scan(&row.ID, &row.PublicID, &row.View.Cycle, &row.View.Status,
			&row.View.Severity, &row.View.Summary, &row.View.Version,
			&row.View.NeedsAttention, &blocking, &row.View.CreatedAt, &row.View.UpdatedAt); err != nil {
			return QueryResponse{}, fmt.Errorf("scan V3 Incident: %w", err)
		}
		row.View.ID = row.PublicID
		row.View.Summary = boundProjectionText(row.View.Summary, 2048)
		if blocking.Valid {
			row.View.BlockingReasonCode = boundProjectionText(blocking.String, 128)
		}
		if err := validateIncidentView(&row.View); err != nil {
			return QueryResponse{}, fmt.Errorf("invalid V3 Incident projection: %w", err)
		}
		row.UpdatedAt = row.View.UpdatedAt
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate V3 Incidents: %w", err)
	}
	response := QueryResponse{Incidents: make([]IncidentView, 0, min(len(items), request.Limit))}
	for index := 0; index < len(items) && index < request.Limit; index++ {
		response.Incidents = append(response.Incidents, items[index].View)
	}
	if len(items) > request.Limit && request.Limit > 0 {
		last := items[request.Limit-1]
		response.NextCursor = last.PublicID
	}
	return response, nil
}

func (p *MySQLQueryPort) getIncident(ctx context.Context, publicID string) (QueryResponse, error) {
	id, err := ParsePublicUUID(publicID)
	if err != nil {
		return QueryResponse{}, err
	}
	var item IncidentView
	var blocking sql.NullString
	err = p.db.QueryRowContext(ctx, `
SELECT public_id, cycle_no, v3_status, severity, summary, version,
       needs_attention, blocking_reason_code, created_at, updated_at
FROM incidents
WHERE public_id = ? AND domain_schema_version = 3`, id).Scan(
		&item.ID, &item.Cycle, &item.Status, &item.Severity, &item.Summary,
		&item.Version, &item.NeedsAttention, &blocking, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return QueryResponse{}, ErrNotFound
	}
	if err != nil {
		return QueryResponse{}, fmt.Errorf("get V3 Incident: %w", err)
	}
	item.Summary = boundProjectionText(item.Summary, 2048)
	if blocking.Valid {
		item.BlockingReasonCode = boundProjectionText(blocking.String, 128)
	}
	if err := validateIncidentView(&item); err != nil {
		return QueryResponse{}, fmt.Errorf("invalid V3 Incident projection: %w", err)
	}
	return QueryResponse{Incident: &item}, nil
}

type mysqlResourceRow struct {
	ID       uint64
	PublicID string
	Cycle    uint64
	Status   sql.NullString
	Version  sql.NullInt64
	Summary  sql.NullString
	Hash     sql.NullString
	Created  time.Time
	Updated  time.Time
	SortAt   time.Time
}

type resourceQuerySpec struct {
	Kind       string
	Table      string
	Status     string
	Version    string
	Summary    string
	Hash       string
	CreatedAt  string
	UpdatedAt  string
	SortColumn string
}

var mysqlResourceQueries = map[QueryKind]resourceQuerySpec{
	QuerySignals: {
		Kind: "signal", Table: "incident_signals", Status: "status", Version: "NULL",
		Summary: "summary", Hash: "NULL", CreatedAt: "created_at", UpdatedAt: "created_at", SortColumn: "occurred_at",
	},
	QueryEvidence: {
		Kind: "evidence", Table: "evidence_items", Status: "CASE WHEN valid THEN 'valid' ELSE 'invalid' END", Version: "NULL",
		Summary: "summary", Hash: "content_hash", CreatedAt: "created_at", UpdatedAt: "created_at", SortColumn: "collected_at",
	},
	QueryInvestigations: {
		Kind: "investigation", Table: "agent_runs", Status: "v3_status", Version: "row_version",
		Summary: "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(final_diagnosis, '$.summary')), failure_summary, failure_code, '')",
		Hash:    "NULL", CreatedAt: "created_at", UpdatedAt: "updated_at", SortColumn: "created_at",
	},
	QueryRemediationPlans: {
		Kind: "remediation_plan", Table: "remediation_plans", Status: "v3_status", Version: "row_version",
		Summary: "patch_summary", Hash: "canonical_plan_hash", CreatedAt: "created_at", UpdatedAt: "updated_at", SortColumn: "created_at",
	},
	QueryVerifications: {
		Kind: "verification", Table: "verification_runs", Status: "v3_status", Version: "row_version",
		Summary: "result_summary", Hash: "verification_profile_hash", CreatedAt: "created_at", UpdatedAt: "updated_at", SortColumn: "created_at",
	},
}

func (p *MySQLQueryPort) listIncidentResources(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	spec, ok := mysqlResourceQueries[request.Kind]
	if !ok {
		return QueryResponse{}, ErrInvalidArgument
	}
	where := []string{"incident_id = ?", "cycle_no = ?", "domain_schema_version = 3", "public_id IS NOT NULL"}
	args := []any{incident.ID, incident.CycleNo}
	if request.Cursor != "" {
		cursor, err := p.resourceCursor(ctx, incident, spec, request.Cursor)
		if err != nil {
			return QueryResponse{}, err
		}
		where = append(where, "("+spec.SortColumn+" < ? OR ("+spec.SortColumn+" = ? AND id < ?))")
		args = append(args, cursor.At, cursor.At, cursor.ID)
	}
	args = append(args, request.Limit+1)
	query := fmt.Sprintf(`
SELECT id, public_id, cycle_no, %s, %s, %s, %s, %s, %s, %s
FROM %s
WHERE %s
ORDER BY %s DESC, id DESC
LIMIT ?`, spec.Status, spec.Version, spec.Summary, spec.Hash, spec.CreatedAt, spec.UpdatedAt,
		spec.SortColumn, spec.Table, strings.Join(where, " AND "), spec.SortColumn)
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list V3 %s resources: %w", spec.Kind, err)
	}
	defer func() { _ = rows.Close() }()
	items, next, err := scanMySQLResourceRows(rows, spec.Kind, request.Limit)
	if err != nil {
		return QueryResponse{}, err
	}
	return QueryResponse{Items: items, NextCursor: next}, nil
}

func (p *MySQLQueryPort) listTimeline(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	after := strings.TrimSpace(request.AfterID)
	if after == "" {
		after = strings.TrimSpace(request.Cursor)
	}
	var afterNumeric uint64
	if after != "" {
		afterNumeric, err = p.eventNumericID(ctx, incident, after)
		if err != nil {
			return QueryResponse{}, err
		}
	}
	rows, err := p.db.QueryContext(ctx, `
SELECT id, public_id, cycle_no, event_type, NULL, summary, NULL,
       created_at, created_at, occurred_at
FROM incident_events
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
  AND public_id IS NOT NULL AND id > ?
ORDER BY id ASC
LIMIT ?`, incident.ID, incident.CycleNo, afterNumeric, request.Limit+1)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list V3 Incident timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, more, err := scanMySQLResourceRows(rows, "timeline_event", request.Limit)
	if err != nil {
		return QueryResponse{}, err
	}
	next := ""
	if more != "" && len(items) > 0 {
		// The scanner drops the look-ahead row; its opaque public UUID is the
		// correct monotonic resume cursor for the returned page.
		next = items[len(items)-1].ID
	}
	return QueryResponse{Items: items, NextCursor: next}, nil
}

func (p *MySQLQueryPort) getIncidentResource(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	var query, kind string
	switch request.Kind {
	case QueryDelivery:
		kind = "delivery"
		query = `
SELECT id, public_id, cycle_no, v3_status, row_version,
       COALESCE(NULLIF(failure_reason, ''), NULLIF(failure_code, ''), ''), NULL,
       created_at, updated_at, created_at
FROM change_requests
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
  AND public_id IS NOT NULL
ORDER BY created_at DESC, id DESC
LIMIT 1`
	default:
		return QueryResponse{}, ErrInvalidArgument
	}
	rows, err := p.db.QueryContext(ctx, query, incident.ID, incident.CycleNo)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("get V3 %s resource: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()
	items, _, err := scanMySQLResourceRows(rows, kind, 1)
	if err != nil {
		return QueryResponse{}, err
	}
	if len(items) == 0 {
		return QueryResponse{}, ErrNotFound
	}
	return QueryResponse{Resource: &items[0]}, nil
}

const resolutionReportQuery = `
SELECT r.public_id, r.report_schema_version, r.cycle_no, r.trigger_type,
       r.resolution_reason, r.service, r.workload, r.environment,
       r.impact_summary, r.cycle_started_at, r.resolved_at,
       r.measured_duration_ms, r.bad_gitops_revision, r.fix_gitops_revision,
       r.source_revision, r.image_digest, r.gitops_revision,
       r.verification_profile_id, r.verification_profile_hash,
       r.common_window_started_at, r.common_window_completed_at,
       r.trigger_signal_json, r.diagnosis_json, r.evidence_json,
       r.remediation_plan_json, r.remediation_decision_json, r.delivery_json,
       r.verification_json, r.timeline_json, r.agent_usage_json,
       r.summary, r.content_hash, r.generated_at
FROM resolution_reports r
JOIN incidents i
  ON i.id = r.incident_id
 AND i.domain_schema_version = 3
 AND i.cycle_no = r.cycle_no
WHERE i.public_id = ?
  AND r.domain_schema_version = 3
  AND r.cycle_no = i.cycle_no
LIMIT 1`

type resolutionReportScanner interface {
	Scan(...any) error
}

type mysqlResolutionReportRow struct {
	PublicID                string
	ReportSchemaVersion     uint64
	Cycle                   uint64
	TriggerType             string
	ResolutionReason        string
	Service                 string
	Workload                string
	Environment             string
	ImpactSummary           string
	CycleStartedAt          time.Time
	ResolvedAt              time.Time
	MeasuredDurationMS      uint64
	BadGitOpsRevision       sql.NullString
	FixGitOpsRevision       sql.NullString
	SourceRevision          string
	ImageDigest             string
	GitOpsRevision          string
	VerificationProfileID   string
	VerificationProfileHash string
	CommonWindowStartedAt   time.Time
	CommonWindowCompletedAt time.Time
	TriggerSignalJSON       []byte
	DiagnosisJSON           []byte
	EvidenceJSON            []byte
	RemediationPlanJSON     []byte
	RemediationDecisionJSON []byte
	DeliveryJSON            []byte
	VerificationJSON        []byte
	TimelineJSON            []byte
	AgentUsageJSON          []byte
	Summary                 string
	ContentHash             string
	GeneratedAt             time.Time
}

func (p *MySQLQueryPort) getResolutionReport(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	id, err := ParsePublicUUID(request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	item, err := scanResolutionReport(p.db.QueryRowContext(ctx, resolutionReportQuery, id))
	if errors.Is(err, sql.ErrNoRows) {
		return QueryResponse{}, ErrNotFound
	}
	if err != nil {
		return QueryResponse{}, fmt.Errorf("get V3 ResolutionReport: %w", err)
	}
	return QueryResponse{ResolutionReport: item}, nil
}

func scanResolutionReport(scanner resolutionReportScanner) (*ResolutionReportView, error) {
	if scanner == nil {
		return nil, ErrUnavailable
	}
	var row mysqlResolutionReportRow
	if err := scanner.Scan(
		&row.PublicID, &row.ReportSchemaVersion, &row.Cycle, &row.TriggerType,
		&row.ResolutionReason, &row.Service, &row.Workload, &row.Environment,
		&row.ImpactSummary, &row.CycleStartedAt, &row.ResolvedAt,
		&row.MeasuredDurationMS, &row.BadGitOpsRevision, &row.FixGitOpsRevision,
		&row.SourceRevision, &row.ImageDigest, &row.GitOpsRevision,
		&row.VerificationProfileID, &row.VerificationProfileHash,
		&row.CommonWindowStartedAt, &row.CommonWindowCompletedAt,
		&row.TriggerSignalJSON, &row.DiagnosisJSON, &row.EvidenceJSON,
		&row.RemediationPlanJSON, &row.RemediationDecisionJSON, &row.DeliveryJSON,
		&row.VerificationJSON, &row.TimelineJSON, &row.AgentUsageJSON,
		&row.Summary, &row.ContentHash, &row.GeneratedAt,
	); err != nil {
		return nil, err
	}
	if row.ReportSchemaVersion == 0 {
		return nil, fmt.Errorf("invalid V3 ResolutionReport schema version: %w", ErrInvalidArgument)
	}
	item := &ResolutionReportView{
		ID:                 row.PublicID,
		Kind:               string(QueryResolutionReport),
		Status:             "resolved",
		Cycle:              row.Cycle,
		TriggerType:        row.TriggerType,
		ResolutionReason:   row.ResolutionReason,
		Service:            row.Service,
		Workload:           row.Workload,
		Environment:        row.Environment,
		ImpactSummary:      row.ImpactSummary,
		Summary:            row.Summary,
		Hash:               row.ContentHash,
		CycleStartedAt:     row.CycleStartedAt,
		ResolvedAt:         row.ResolvedAt,
		MeasuredDurationMS: row.MeasuredDurationMS,
		GeneratedAt:        row.GeneratedAt,
		Revisions: ResolutionRevisionsView{
			SourceRevision: row.SourceRevision,
			ImageDigest:    row.ImageDigest,
			GitOpsRevision: row.GitOpsRevision,
		},
		VerificationProfile: ResolutionVerificationProfileView{
			ID: row.VerificationProfileID, Hash: row.VerificationProfileHash,
		},
		Stability: ResolutionStabilityView{
			CommonWindowStartedAt: row.CommonWindowStartedAt, CommonWindowCompletedAt: row.CommonWindowCompletedAt,
		},
		TriggerSignal:       append([]byte(nil), row.TriggerSignalJSON...),
		Diagnosis:           append([]byte(nil), row.DiagnosisJSON...),
		Evidence:            append([]byte(nil), row.EvidenceJSON...),
		RemediationPlan:     append([]byte(nil), row.RemediationPlanJSON...),
		RemediationDecision: append([]byte(nil), row.RemediationDecisionJSON...),
		Delivery:            append([]byte(nil), row.DeliveryJSON...),
		Verification:        append([]byte(nil), row.VerificationJSON...),
		Timeline:            append([]byte(nil), row.TimelineJSON...),
		AgentUsage:          append([]byte(nil), row.AgentUsageJSON...),
	}
	if row.BadGitOpsRevision.Valid {
		item.Revisions.BadGitOpsRevision = row.BadGitOpsRevision.String
	}
	if row.FixGitOpsRevision.Valid {
		item.Revisions.FixGitOpsRevision = row.FixGitOpsRevision.String
	}
	if err := validateResolutionReportView(item); err != nil {
		return nil, fmt.Errorf("invalid V3 ResolutionReport projection: %w", err)
	}
	return item, nil
}

func (p *MySQLQueryPort) listEvents(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	var after uint64
	if request.LastEventID != "" {
		after, err = p.eventNumericID(ctx, incident, request.LastEventID)
		if err != nil {
			return QueryResponse{}, err
		}
	}
	rows, err := p.db.QueryContext(ctx, `
SELECT id, public_id, event_type
FROM incident_events
WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
  AND public_id IS NOT NULL AND id > ?
ORDER BY id ASC
LIMIT ?`, incident.ID, incident.CycleNo, after, request.Limit)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list V3 Incident events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]RefreshEvent, 0, request.Limit)
	for rows.Next() {
		var numericID uint64
		var publicID, eventType string
		if err := rows.Scan(&numericID, &publicID, &eventType); err != nil {
			return QueryResponse{}, fmt.Errorf("scan V3 Incident event: %w", err)
		}
		if _, err := ParsePublicUUID(publicID); err != nil {
			return QueryResponse{}, fmt.Errorf("invalid V3 Incident event public ID: %w", err)
		}
		events = append(events, RefreshEvent{
			Cursor: publicID, IncidentID: request.IncidentID, Resource: eventRefreshResource(eventType),
		})
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate V3 Incident events: %w", err)
	}
	return QueryResponse{Events: events}, nil
}

func (p *MySQLQueryPort) eventNumericID(ctx context.Context, incident mysqlIncidentRef, publicID string) (uint64, error) {
	id, err := ParsePublicUUID(strings.TrimSpace(publicID))
	if err != nil {
		return 0, ErrInvalidArgument
	}
	var numeric uint64
	err = p.db.QueryRowContext(ctx, `
SELECT id FROM incident_events
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		id, incident.ID, incident.CycleNo).Scan(&numeric)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInvalidArgument
	}
	if err != nil {
		return 0, fmt.Errorf("resolve V3 event cursor: %w", err)
	}
	return numeric, nil
}

func scanMySQLResourceRows(rows *sql.Rows, kind string, limit int) ([]ResourceView, string, error) {
	if rows == nil {
		return nil, "", ErrUnavailable
	}
	all := make([]mysqlResourceRow, 0, limit+1)
	for rows.Next() {
		var item mysqlResourceRow
		if err := rows.Scan(&item.ID, &item.PublicID, &item.Cycle, &item.Status, &item.Version,
			&item.Summary, &item.Hash, &item.Created, &item.Updated, &item.SortAt); err != nil {
			return nil, "", fmt.Errorf("scan V3 %s projection: %w", kind, err)
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate V3 %s projection: %w", kind, err)
	}
	items := make([]ResourceView, 0, min(len(all), limit))
	for index := 0; index < len(all) && index < limit; index++ {
		row := all[index]
		item := ResourceView{
			ID: row.PublicID, Kind: kind, Cycle: row.Cycle,
			CreatedAt: row.Created.UTC(), UpdatedAt: row.Updated.UTC(),
		}
		if row.Status.Valid {
			item.Status = boundProjectionText(row.Status.String, 64)
		}
		if row.Version.Valid && row.Version.Int64 > 0 {
			item.Version = uint64(row.Version.Int64)
		}
		if row.Summary.Valid {
			item.Summary = boundProjectionText(row.Summary.String, 2048)
		}
		if row.Hash.Valid {
			item.Hash = row.Hash.String
		}
		if err := validateResource(&item); err != nil {
			return nil, "", fmt.Errorf("invalid V3 %s projection: %w", kind, err)
		}
		items = append(items, item)
	}
	next := ""
	if len(all) > limit && limit > 0 {
		last := all[limit-1]
		next = last.PublicID
	}
	return items, next, nil
}

type mysqlQueryPosition struct {
	ID uint64
	At time.Time
}

func (p *MySQLQueryPort) incidentCursor(ctx context.Context, publicID string) (mysqlQueryPosition, error) {
	id, err := ParsePublicUUID(strings.TrimSpace(publicID))
	if err != nil {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	var cursor mysqlQueryPosition
	err = p.db.QueryRowContext(ctx, `
SELECT id, updated_at
FROM incidents
WHERE public_id = ? AND domain_schema_version = 3`, id).Scan(&cursor.ID, &cursor.At)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	if err != nil {
		return mysqlQueryPosition{}, fmt.Errorf("resolve V3 Incident cursor: %w", err)
	}
	return cursor, nil
}

func (p *MySQLQueryPort) resourceCursor(ctx context.Context, incident mysqlIncidentRef, spec resourceQuerySpec, publicID string) (mysqlQueryPosition, error) {
	id, err := ParsePublicUUID(strings.TrimSpace(publicID))
	if err != nil {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	query := fmt.Sprintf(`
SELECT id, %s
FROM %s
WHERE public_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		spec.SortColumn, spec.Table)
	var cursor mysqlQueryPosition
	err = p.db.QueryRowContext(ctx, query, id, incident.ID, incident.CycleNo).Scan(&cursor.ID, &cursor.At)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	if err != nil {
		return mysqlQueryPosition{}, fmt.Errorf("resolve V3 %s cursor: %w", spec.Kind, err)
	}
	if cursor.ID == 0 || cursor.At.IsZero() {
		return mysqlQueryPosition{}, ErrInvalidArgument
	}
	return cursor, nil
}

func boundedQueryLimit(limit int) int {
	if limit <= 0 {
		return defaultPageSize
	}
	if limit > maxPageSize {
		return maxPageSize
	}
	return limit
}

func eventRefreshResource(eventType string) string {
	value := strings.ToLower(eventType)
	switch {
	case strings.Contains(value, "signal"):
		return "signals"
	case strings.Contains(value, "evidence"):
		return "evidence"
	case strings.Contains(value, "investigation"), strings.Contains(value, "agent"):
		return "investigations"
	case strings.Contains(value, "approval"), strings.Contains(value, "decision"), strings.Contains(value, "plan"), strings.Contains(value, "remediation"):
		return "remediation_plans"
	case strings.Contains(value, "delivery"), strings.Contains(value, "change"), strings.Contains(value, "pull_request"), strings.Contains(value, "argo"), strings.Contains(value, "rollout"):
		return "delivery"
	case strings.Contains(value, "verification"):
		return "verifications"
	case strings.Contains(value, "resolution_report"):
		return "resolution_report"
	default:
		return "incident"
	}
}

func boundProjectionText(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
