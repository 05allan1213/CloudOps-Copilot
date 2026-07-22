package cutover

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const OutboxRegistryVersion uint16 = 2

type OutboxRegistryEntry struct {
	EventType     string
	SchemaVersion uint32
	AggregateType string
	ArchiveMapper string
	ExternalWrite bool
	Fixture       json.RawMessage
	FixtureHash   string
}

type LegacyOutboxRow struct {
	ID            uint64
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	SchemaVersion uint32
	Payload       json.RawMessage
	OccurredAt    time.Time
	PublishedAt   *time.Time
	Attempts      uint64
	LastError     string
	CreatedAt     time.Time
}

type OutboxArchiveDecision struct {
	RegistryVersion        uint16
	Publication            string
	RowHash                string
	Snapshot               json.RawMessage
	RequiresReconciliation bool
	ReasonCode             string
}

var outboxEventRegistry = buildOutboxRegistry()

func OutboxRegistry() []OutboxRegistryEntry {
	result := make([]OutboxRegistryEntry, 0, len(outboxEventRegistry))
	for _, item := range outboxEventRegistry {
		copyItem := item
		copyItem.Fixture = append(json.RawMessage(nil), item.Fixture...)
		result = append(result, copyItem)
	}
	slices.SortFunc(result, func(left, right OutboxRegistryEntry) int {
		if value := strings.Compare(left.EventType, right.EventType); value != 0 {
			return value
		}
		if left.SchemaVersion < right.SchemaVersion {
			return -1
		}
		if left.SchemaVersion > right.SchemaVersion {
			return 1
		}
		return 0
	})
	return result
}

func ValidateOutboxArchive(row LegacyOutboxRow, externalReconciled bool) (OutboxArchiveDecision, error) {
	key := outboxRegistryKey(row.EventType, row.SchemaVersion)
	entry, ok := outboxEventRegistry[key]
	if !ok || entry.AggregateType != row.AggregateType {
		return OutboxArchiveDecision{}, errors.New("unknown_outbox_type_or_schema")
	}
	if row.ID == 0 || strings.TrimSpace(row.EventID) == "" || strings.TrimSpace(row.AggregateID) == "" || row.OccurredAt.IsZero() || row.CreatedAt.IsZero() || !json.Valid(row.Payload) {
		return OutboxArchiveDecision{}, errors.New("invalid_outbox_row")
	}
	var payload any
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return OutboxArchiveDecision{}, errors.New("invalid_outbox_payload")
	}
	if _, ok := payload.(map[string]any); !ok {
		return OutboxArchiveDecision{}, errors.New("invalid_outbox_payload_schema")
	}
	if entry.ExternalWrite && !externalReconciled {
		return OutboxArchiveDecision{}, errors.New("outbox_external_write_unreconciled")
	}
	snapshot, err := json.Marshal(map[string]any{
		"id": row.ID, "event_id": row.EventID, "aggregate_type": row.AggregateType,
		"aggregate_id": row.AggregateID, "event_type": row.EventType,
		"schema_version": row.SchemaVersion, "payload": json.RawMessage(row.Payload),
		"occurred_at":  row.OccurredAt.UTC().Format(time.RFC3339Nano),
		"published_at": canonicalOptionalTime(row.PublishedAt), "attempts": row.Attempts,
		"last_error": row.LastError, "created_at": row.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return OutboxArchiveDecision{}, err
	}
	hash, canonical, err := canonicalHashJSON(snapshot)
	if err != nil {
		return OutboxArchiveDecision{}, err
	}
	publication := "unpublished"
	if row.PublishedAt != nil {
		publication = "published"
	}
	return OutboxArchiveDecision{
		RegistryVersion: OutboxRegistryVersion, Publication: publication, RowHash: hash,
		Snapshot: canonical, RequiresReconciliation: entry.ExternalWrite,
		ReasonCode: "outbox-archived-" + publication,
	}, nil
}

func buildOutboxRegistry() map[string]OutboxRegistryEntry {
	entries := []struct {
		name     string
		external bool
	}{
		{"incident.created", false}, {"incident.updated", false}, {"incident.signal_resolved", false}, {"incident.status_changed", false},
		{"incident.diagnosis_completed", false},
		{"remediation_plan_policy_rejected", false}, {"remediation_planning_started", false}, {"remediation_plan_awaiting_approval", false},
		{"remediation_plan_approved", false}, {"remediation_plan_rejected", false}, {"remediation_draft_pr_created", true},
		{"controlled_direct_execution_delivered", true}, {"delivery_argocd_revision_detected", false}, {"delivery_pending", false},
		{"delivery_delivering", false}, {"delivery_pr_created", true}, {"delivery_ci_pending", false}, {"delivery_ci_passed", false},
		{"delivery_ci_failed", false}, {"delivery_merge_pending", false}, {"delivery_pr_merged", true}, {"delivery_pr_closed", false},
		{"delivery_argocd_pending", false}, {"delivery_argocd_sync_started", false}, {"delivery_argocd_sync_succeeded", false},
		{"delivery_argocd_sync_failed", false}, {"delivery_argocd_timeout", false}, {"delivery_kubernetes_rollout_started", false},
		{"delivery_rollout_failed", false}, {"delivery_completed", false}, {"delivery_merge_timeout", false},
		{"delivery_revision_mismatch", false}, {"delivery_delivery_cancelled", false}, {"delivery_failed", false},
		{"verification_started", false}, {"verification_check_pending", false}, {"verification_check_running", false},
		{"verification_check_passed", false}, {"verification_check_failed", false}, {"verification_check_timed_out", false},
		{"verification_check_unavailable", false}, {"verification_check_invalid", false}, {"verification_check_cancelled", false},
		{"verification_failed", false}, {"verification_passed", false}, {"incident_resolved_after_verification", false},
		{"incident_returned_to_investigation", false}, {"verification_timed_out", false},
	}
	result := make(map[string]OutboxRegistryEntry, len(entries))
	for _, item := range entries {
		fixture := json.RawMessage(fmt.Sprintf(`{"event_type":%q,"schema_version":1,"payload":{}}`, item.name))
		hash, canonical, err := canonicalHashJSON(fixture)
		if err != nil {
			panic(err)
		}
		mapper := "archive-only/v2"
		if item.external {
			mapper = "archive-and-reconcile/v2"
		}
		entry := OutboxRegistryEntry{EventType: item.name, SchemaVersion: 1, AggregateType: "incident", ArchiveMapper: mapper, ExternalWrite: item.external, Fixture: canonical, FixtureHash: hash}
		result[outboxRegistryKey(item.name, 1)] = entry
	}
	return result
}

func outboxRegistryKey(eventType string, schema uint32) string {
	return strings.TrimSpace(eventType) + "\x00" + fmt.Sprint(schema)
}

func canonicalOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
