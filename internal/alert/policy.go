package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type escalationPolicyRow struct {
	ID                   uint64
	PublicID             string
	Name                 string
	Severities           []string
	Namespaces           []string
	LabelMatchers        map[string]string
	MinimumFiringSeconds uint64
	MinimumRecurrence    uint64
}

func (s *Service) evaluateEscalationPolicies(ctx context.Context, tx *sql.Tx, alertKey string) error {
	row, err := loadAlertByKey(ctx, tx, alertKey, true)
	if err != nil || row.Status != "firing" {
		return err
	}
	var revisionInternalID uint64
	var revisionPublicID string
	var enabled bool
	err = tx.QueryRowContext(ctx, `SELECT revision.id, revision.public_id,
revision.automatic_escalation_enabled FROM active_configuration active
JOIN configuration_revisions revision ON revision.id = active.configuration_revision_id
WHERE active.singleton_id = 1`).Scan(&revisionInternalID, &revisionPublicID, &enabled)
	if err != nil || !enabled {
		return err
	}
	policies, err := tx.QueryContext(ctx, `SELECT id, public_id, name, severities_json,
namespaces_json, label_matchers_json, minimum_firing_seconds, minimum_recurrence_count
FROM escalation_policies WHERE configuration_revision_id = ? AND enabled = 1
AND create_incident = 1 ORDER BY id`, revisionInternalID)
	if err != nil {
		return err
	}
	defer func() { _ = policies.Close() }()
	var labels map[string]string
	if err := json.Unmarshal(row.Labels, &labels); err != nil {
		return err
	}
	for policies.Next() {
		var policy escalationPolicyRow
		var severities, namespaces, matchers []byte
		if err := policies.Scan(&policy.ID, &policy.PublicID, &policy.Name, &severities,
			&namespaces, &matchers, &policy.MinimumFiringSeconds, &policy.MinimumRecurrence); err != nil {
			return err
		}
		if err := json.Unmarshal(severities, &policy.Severities); err != nil {
			return err
		}
		if err := json.Unmarshal(namespaces, &policy.Namespaces); err != nil {
			return err
		}
		if err := json.Unmarshal(matchers, &policy.LabelMatchers); err != nil {
			return err
		}
		if !policyMatches(policy, row, labels, s.now().UTC()) {
			continue
		}
		incident, err := ensureActiveIncident(ctx, tx, row)
		if err != nil {
			return err
		}
		created, err := linkAlertIncident(ctx, tx, row, incident, "escalation_policy", revisionInternalID, policy.ID)
		if err != nil {
			return err
		}
		if !created {
			return nil
		}
		if err := incrementAlertVersion(ctx, tx, &row); err != nil {
			return err
		}
		eventKey := hashCanonical("alert-escalation-policy", row.PublicID, revisionPublicID,
			policy.PublicID, fmt.Sprint(row.Recurrence))
		if err := appendAlertEvent(ctx, tx, row, "alert_escalated", "system", "escalation-policy",
			"Alert promoted by a versioned Escalation Policy", nil, s.now().UTC(), map[string]any{
				"incident_id": incident.PublicID, "configuration_revision_id": revisionPublicID,
				"escalation_policy_id": policy.PublicID, "escalation_policy_name": policy.Name,
			}, eventKey); err != nil {
			return err
		}
		if err := appendIncidentLinkEvent(ctx, tx, incident, row, "escalation_policy", "escalation-policy", eventKey); err != nil {
			return err
		}
		return nil
	}
	return policies.Err()
}

func policyMatches(policy escalationPolicyRow, row alertRow, labels map[string]string, now time.Time) bool {
	if !containsString(policy.Severities, row.Severity) ||
		(len(policy.Namespaces) > 0 && !containsString(policy.Namespaces, row.Namespace)) ||
		row.Recurrence < policy.MinimumRecurrence {
		return false
	}
	if policy.MinimumFiringSeconds > 0 && now.Before(row.StartsAt.Add(time.Duration(policy.MinimumFiringSeconds)*time.Second)) {
		return false
	}
	for name, value := range policy.LabelMatchers {
		if labels[name] != value {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
