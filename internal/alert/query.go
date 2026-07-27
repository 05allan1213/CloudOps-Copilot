package alert

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type alertQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type alertCursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        uint64    `json:"id"`
}

func encodeAlertCursor(value alertCursor) string {
	encoded, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeAlertCursor(raw string) (alertCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return alertCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return alertCursor{}, ErrInvalid
	}
	var value alertCursor
	if err := json.Unmarshal(decoded, &value); err != nil || value.ID == 0 || value.UpdatedAt.IsZero() {
		return alertCursor{}, ErrInvalid
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, request ListRequest) (Page, error) {
	if s == nil || s.db == nil {
		return Page{}, ErrProviderUnavailable
	}
	if request.Limit == 0 {
		request.Limit = defaultPageSize
	}
	if request.Limit < 1 || request.Limit > maxPageSize {
		return Page{}, ErrInvalid
	}
	if request.Status != "" && request.Status != "firing" && request.Status != "resolved" {
		return Page{}, ErrInvalid
	}
	if request.Severity != "" && severityRank(request.Severity) == 1 && request.Severity != "unknown" {
		return Page{}, ErrInvalid
	}
	if len(request.Namespace) > 255 || len(request.Search) > 255 {
		return Page{}, ErrInvalid
	}
	cursor, err := decodeAlertCursor(request.Cursor)
	if err != nil {
		return Page{}, err
	}

	query := "SELECT " + alertColumns + " FROM alerts WHERE 1 = 1"
	args := make([]any, 0, 8)
	if request.Status != "" {
		query += " AND status = ?"
		args = append(args, request.Status)
	}
	if request.Severity != "" {
		query += " AND severity = ?"
		args = append(args, request.Severity)
	}
	if request.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, request.Namespace)
	}
	if request.Search != "" {
		search := "%" + strings.TrimSpace(request.Search) + "%"
		query += " AND (summary LIKE ? OR category LIKE ? OR target_name LIKE ? OR service_name LIKE ?)"
		args = append(args, search, search, search, search)
	}
	if cursor.ID != 0 {
		query += " AND (updated_at < ? OR (updated_at = ? AND id < ?))"
		args = append(args, cursor.UpdatedAt.UTC(), cursor.UpdatedAt.UTC(), cursor.ID)
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT ?"
	args = append(args, request.Limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	defer func() { _ = rows.Close() }()

	internal := make([]alertRow, 0, request.Limit+1)
	for rows.Next() {
		row, scanErr := scanAlertRow(rows)
		if scanErr != nil {
			return Page{}, scanErr
		}
		internal = append(internal, row)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{Items: make([]View, 0, min(len(internal), request.Limit))}
	if len(internal) > request.Limit {
		last := internal[request.Limit-1]
		page.NextCursor = encodeAlertCursor(alertCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
		internal = internal[:request.Limit]
	}
	for _, row := range internal {
		view, viewErr := s.projectAlert(ctx, s.db, row)
		if viewErr != nil {
			return Page{}, viewErr
		}
		page.Items = append(page.Items, view)
	}
	return page, nil
}

func (s *Service) Detail(ctx context.Context, publicID string) (Detail, error) {
	if _, err := uuid.Parse(strings.TrimSpace(publicID)); err != nil {
		return Detail{}, ErrInvalid
	}
	row, err := scanAlertRow(s.db.QueryRowContext(ctx, "SELECT "+alertColumns+" FROM alerts WHERE public_id = ?", publicID))
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	view, err := s.projectAlert(ctx, s.db, row)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{Alert: view, Signals: []SignalView{}, Events: []EventView{}}
	signals, err := s.db.QueryContext(ctx, `SELECT source_signal.public_id, source_signal.source_event_id,
source_signal.alert_instance_key, source_signal.status, source_signal.severity, source_signal.summary, source_signal.labels_json,
source_signal.annotations_json, source_signal.starts_at, source_signal.ends_at, source_signal.occurred_at, source_signal.received_at,
link.provenance
FROM alert_signal_links link JOIN incident_signals source_signal ON source_signal.id = link.signal_id
WHERE link.alert_id = ? ORDER BY source_signal.occurred_at DESC, source_signal.id DESC`, row.ID)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = signals.Close() }()
	for signals.Next() {
		var item SignalView
		var endsAt sql.NullTime
		if err := signals.Scan(&item.ID, &item.SourceEventID, &item.AlertInstanceKey, &item.Status,
			&item.Severity, &item.Summary, &item.Labels, &item.Annotations, &item.StartsAt,
			&endsAt, &item.OccurredAt, &item.ReceivedAt, &item.Provenance); err != nil {
			return Detail{}, err
		}
		if endsAt.Valid {
			value := endsAt.Time.UTC()
			item.EndsAt = &value
		}
		detail.Signals = append(detail.Signals, item)
	}
	if err := signals.Err(); err != nil {
		return Detail{}, err
	}
	events, err := s.db.QueryContext(ctx, `SELECT public_id, event_type, actor_type, actor_id,
summary, metadata_json, occurred_at FROM alert_events WHERE alert_id = ?
ORDER BY occurred_at DESC, id DESC`, row.ID)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = events.Close() }()
	for events.Next() {
		var item EventView
		if err := events.Scan(&item.ID, &item.Type, &item.ActorType, &item.ActorID,
			&item.Summary, &item.Metadata, &item.OccurredAt); err != nil {
			return Detail{}, err
		}
		detail.Events = append(detail.Events, item)
	}
	return detail, events.Err()
}

func (s *Service) projectAlert(ctx context.Context, queryer alertQueryer, row alertRow) (View, error) {
	view := View{
		ID: row.PublicID, Status: row.Status, Severity: row.Severity, Summary: row.Summary,
		Category: row.Category, Source: row.Source, Fingerprint: row.Fingerprint,
		CorrelationKey: row.CorrelationKey, Cluster: row.Cluster, Environment: row.Environment,
		Namespace: row.Namespace, ServiceName: row.Service, TargetKind: row.TargetKind,
		TargetName: row.TargetName, FirstSeenAt: row.FirstSeen.UTC(), LastSeenAt: row.LastSeen.UTC(),
		StartsAt: row.StartsAt.UTC(), RecurrenceCount: row.Recurrence, SignalCount: row.SignalCount,
		Version: row.Version, IncidentLinks: []IncidentLink{}, Investigations: []InvestigationLink{},
		ContextLink: ContextLink{Workspace: "alerts", Path: "/alerts/" + row.PublicID,
			Query: map[string]string{"cluster_id": row.Cluster, "namespace": row.Namespace}},
		MigratedLegacy: row.MigratedLegacy, MigratedLegacyContext: row.MigratedLegacyContext,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
	if row.ResolvedAt.Valid {
		resolved := row.ResolvedAt.Time.UTC()
		view.ResolvedAt = &resolved
	}
	_ = queryer.QueryRowContext(ctx, `SELECT scope.public_id FROM active_configuration active
JOIN operational_scopes scope ON scope.configuration_revision_id = active.configuration_revision_id
WHERE active.singleton_id = 1 AND scope.cluster_id = ? ORDER BY scope.is_default DESC, scope.id LIMIT 1`, row.Cluster).
		Scan(&view.ContextLink.OperationalScopeID)

	var acknowledgement Acknowledgement
	err := queryer.QueryRowContext(ctx, `SELECT public_id, recurrence_no, alert_version,
actor_provider, actor_login, actor_role, reason, created_at FROM alert_acknowledgements
WHERE alert_id = ? AND recurrence_no = ? ORDER BY id DESC LIMIT 1`, row.ID, row.Recurrence).
		Scan(&acknowledgement.ID, &acknowledgement.RecurrenceNo, &acknowledgement.AlertVersion,
			&acknowledgement.Actor.Provider, &acknowledgement.Actor.Login, &acknowledgement.Actor.Role,
			&acknowledgement.Reason, &acknowledgement.CreatedAt)
	if err == nil {
		view.Acknowledgement = &acknowledgement
	} else if !errors.Is(err, sql.ErrNoRows) {
		return View{}, err
	}

	var silence Silence
	var matchers []byte
	var expiredAt sql.NullTime
	var providerID, providerCode sql.NullString
	err = queryer.QueryRowContext(ctx, `SELECT silence.public_id, silence.provider_silence_id,
silence.status, silence.matchers_json, silence.reason, revision.public_id, silence.starts_at,
silence.ends_at, silence.expired_at, silence.provider_error_code, silence.created_at
FROM alert_silences silence JOIN configuration_revisions revision ON revision.id = silence.configuration_revision_id
WHERE silence.alert_id = ? ORDER BY (silence.status IN ('pending','active')) DESC, silence.id DESC LIMIT 1`, row.ID).
		Scan(&silence.ID, &providerID, &silence.Status, &matchers, &silence.Reason,
			&silence.ConfigurationRevisionID, &silence.StartsAt, &silence.EndsAt, &expiredAt,
			&providerCode, &silence.CreatedAt)
	if err == nil {
		if unmarshalErr := json.Unmarshal(matchers, &silence.Matchers); unmarshalErr != nil {
			return View{}, unmarshalErr
		}
		silence.ProviderSilenceID = providerID.String
		silence.ProviderErrorCode = providerCode.String
		if expiredAt.Valid {
			value := expiredAt.Time.UTC()
			silence.ExpiredAt = &value
		}
		view.Silence = &silence
	} else if !errors.Is(err, sql.ErrNoRows) {
		return View{}, err
	}

	links, err := queryer.QueryContext(ctx, `SELECT relation.public_id, incident.public_id,
relation.incident_cycle_no, incident.status, relation.provenance, revision.public_id,
policy.public_id, relation.created_at
FROM alert_incident_links relation JOIN incidents incident ON incident.id = relation.incident_id
LEFT JOIN configuration_revisions revision ON revision.id = relation.configuration_revision_id
LEFT JOIN escalation_policies policy ON policy.id = relation.escalation_policy_id
WHERE relation.alert_id = ? ORDER BY relation.id DESC`, row.ID)
	if err != nil {
		return View{}, err
	}
	for links.Next() {
		var item IncidentLink
		var revisionID, policyID sql.NullString
		if err := links.Scan(&item.ID, &item.IncidentID, &item.IncidentCycle, &item.IncidentStatus,
			&item.Provenance, &revisionID, &policyID, &item.CreatedAt); err != nil {
			_ = links.Close()
			return View{}, err
		}
		item.ConfigurationRevisionID = revisionID.String
		item.EscalationPolicyID = policyID.String
		view.IncidentLinks = append(view.IncidentLinks, item)
	}
	if err := links.Close(); err != nil {
		return View{}, err
	}

	investigations, err := queryer.QueryContext(ctx, `SELECT run.public_id, incident.public_id,
run.status, run.created_at FROM alert_incident_links relation
JOIN incidents incident ON incident.id = relation.incident_id
JOIN agent_runs run ON run.incident_id = incident.id AND run.cycle_no = relation.incident_cycle_no
WHERE relation.alert_id = ? ORDER BY run.created_at DESC, run.id DESC`, row.ID)
	if err != nil {
		return View{}, err
	}
	for investigations.Next() {
		var item InvestigationLink
		if err := investigations.Scan(&item.ID, &item.IncidentID, &item.Status, &item.CreatedAt); err != nil {
			_ = investigations.Close()
			return View{}, err
		}
		view.Investigations = append(view.Investigations, item)
	}
	if err := investigations.Close(); err != nil {
		return View{}, err
	}
	return view, nil
}
