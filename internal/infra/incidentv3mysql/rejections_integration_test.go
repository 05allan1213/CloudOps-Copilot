package incidentv3mysql

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMySQLIncidentV3TargetRejectionIsDurableAndIdempotent(t *testing.T) {
	db := openIncidentIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	rejection := RejectionInput{
		Source: "alertmanager", SourceEventID: "v2:" + strings.Repeat("a", 64),
		Fingerprint: "abcdef0123456789", AlertInstanceKey: strings.Repeat("b", 64),
		ReasonCode: "target_not_allowlisted",
		Details:    map[string]string{"labels_hash": strings.Repeat("c", 64), "status": "firing"},
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := store.RecordRejections(ctx, []RejectionInput{rejection}); err != nil {
			t.Fatal(err)
		}
	}
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM signal_rejections WHERE source = ? AND source_event_id = ? AND reason_code = ?", 1, rejection.Source, rejection.SourceEventID, rejection.ReasonCode)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM incidents WHERE domain_schema_version = 3", 0)
	assertIncidentIntegrationCount(t, ctx, db, "SELECT COUNT(*) FROM async_tasks WHERE incident_id IS NOT NULL", 0)
}
