package cutover

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
)

type fakeMarkerWriteStore struct {
	tx    *fakeMarkerWriteTx
	calls int
	err   error
}

func (s *fakeMarkerWriteStore) WithLockedTransaction(ctx context.Context, fn func(markerWriteTx) error) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	return fn(s.tx)
}

type fakeMarkerWriteTx struct {
	version        uint64
	markerStatuses []string
	prerequisites  map[string]prerequisite
	activeLeases   uint64
	phase7AErr     error
	now            time.Time
	inserted       *Marker
	markerCount    uint64
	err            error
}

func (t *fakeMarkerWriteTx) DatabaseSchemaVersion(context.Context) (uint64, error) {
	return t.version, t.err
}
func (t *fakeMarkerWriteTx) MarkerStatusesForUpdate(context.Context) ([]string, error) {
	return append([]string(nil), t.markerStatuses...), nil
}
func (t *fakeMarkerWriteTx) PrerequisiteForUpdate(_ context.Context, id string) (prerequisite, error) {
	row, ok := t.prerequisites[id]
	if !ok {
		return prerequisite{}, sql.ErrNoRows
	}
	return row, nil
}
func (t *fakeMarkerWriteTx) ValidatePhase7APreparation(context.Context, WriteRequest) error {
	return t.phase7AErr
}
func (t *fakeMarkerWriteTx) LegacyActiveLeaseCount(context.Context) (uint64, error) {
	return t.activeLeases, nil
}
func (t *fakeMarkerWriteTx) DatabaseTime(context.Context) (time.Time, error) { return t.now, nil }
func (t *fakeMarkerWriteTx) InsertMarker(_ context.Context, marker Marker) error {
	if t.err != nil {
		return t.err
	}
	copy := marker
	t.inserted = &copy
	return nil
}
func (t *fakeMarkerWriteTx) MarkerCount(context.Context) (uint64, error) {
	return t.markerCount, nil
}

func TestMarkerWriterWritesOnePassedReleaseBoundMarker(t *testing.T) {
	request := validWriteRequest()
	tx := validWriteTx(request)
	store := &fakeMarkerWriteStore{tx: tx}
	marker, err := newMarkerWriter(store).Write(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || tx.inserted == nil || marker.PublicID != tx.inserted.PublicID {
		t.Fatalf("transaction/insert mismatch calls=%d marker=%+v inserted=%+v", store.calls, marker, tx.inserted)
	}
	if err := marker.Validate(schemaversion.Latest); err != nil {
		t.Fatalf("written marker invalid: %v", err)
	}
	if marker.SourceExactSHA != request.SourceExactSHA || marker.BinaryImageDigest != request.BinaryImageDigest ||
		marker.SourceSchemaVersion != request.SourceSchemaVersion || marker.TargetSchemaVersion != request.TargetSchemaVersion {
		t.Fatalf("marker lost release identity: %+v", marker)
	}
	for _, id := range []string{request.QuiesceLedgerPublicID, request.ReconciliationLedgerPublicID, request.ConverterAuditLedgerPublicID} {
		if !strings.Contains(marker.BoundedSummary, id) {
			t.Fatalf("marker summary does not bind prerequisite %s", id)
		}
	}
	second := buildMarker(request, tx.now)
	if marker.SourceHash != second.SourceHash || marker.TargetHash != second.TargetHash {
		t.Fatal("marker prerequisite hashes are not deterministic")
	}
	if CurrentRuntimeGeneration != RuntimeV3 {
		t.Fatalf("current exact-SHA runtime generation=%q, want v3", CurrentRuntimeGeneration)
	}
}

func TestMarkerWriterFailsClosedBeforeInsert(t *testing.T) {
	base := validWriteRequest()
	tests := []struct {
		name   string
		edit   func(*WriteRequest, *fakeMarkerWriteTx)
		needle string
	}{
		{name: "unknown old workers", edit: func(r *WriteRequest, _ *fakeMarkerWriteTx) { r.OldWorkerCount = -1 }, needle: "unknown"},
		{name: "old workers present", edit: func(r *WriteRequest, _ *fakeMarkerWriteTx) { r.OldWorkerCount = 1 }, needle: "want zero"},
		{name: "confirmation missing", edit: func(r *WriteRequest, _ *fakeMarkerWriteTx) { r.Confirmation = "" }, needle: "confirmation"},
		{name: "database schema mismatch", edit: func(_ *WriteRequest, tx *fakeMarkerWriteTx) { tx.version-- }, needle: "database schema"},
		{name: "duplicate marker", edit: func(_ *WriteRequest, tx *fakeMarkerWriteTx) { tx.markerStatuses = []string{"passed"} }, needle: "already exists"},
		{name: "running prerequisite", edit: func(r *WriteRequest, tx *fakeMarkerWriteTx) {
			row := tx.prerequisites[r.QuiesceLedgerPublicID]
			row.Status = "running"
			row.CompletedAt = sql.NullTime{}
			tx.prerequisites[r.QuiesceLedgerPublicID] = row
		}, needle: "status=\"running\""},
		{name: "failed prerequisite", edit: func(r *WriteRequest, tx *fakeMarkerWriteTx) {
			row := tx.prerequisites[r.ReconciliationLedgerPublicID]
			row.Status = "failed"
			tx.prerequisites[r.ReconciliationLedgerPublicID] = row
		}, needle: "status=\"failed\""},
		{name: "wrong prerequisite operation", edit: func(r *WriteRequest, tx *fakeMarkerWriteTx) {
			row := tx.prerequisites[r.ConverterAuditLedgerPublicID]
			row.Operation = "BACKFILL-V3"
			tx.prerequisites[r.ConverterAuditLedgerPublicID] = row
		}, needle: "operation"},
		{name: "release mismatch", edit: func(r *WriteRequest, tx *fakeMarkerWriteTx) {
			row := tx.prerequisites[r.ConverterAuditLedgerPublicID]
			row.SourceExactSHA = strings.Repeat("e", 40)
			tx.prerequisites[r.ConverterAuditLedgerPublicID] = row
		}, needle: "identity mismatch"},
		{name: "preparation drift", edit: func(_ *WriteRequest, tx *fakeMarkerWriteTx) {
			tx.phase7AErr = errors.New("archive parity drift")
		}, needle: "archive parity drift"},
		{name: "active legacy lease", edit: func(_ *WriteRequest, tx *fakeMarkerWriteTx) { tx.activeLeases = 1 }, needle: "active lease count=1"},
		{name: "ambiguous inserted marker", edit: func(_ *WriteRequest, tx *fakeMarkerWriteTx) { tx.markerCount = 2 }, needle: "rows=2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			tx := validWriteTx(request)
			test.edit(&request, tx)
			_, err := newMarkerWriter(&fakeMarkerWriteStore{tx: tx}).Write(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("error=%v, want %q", err, test.needle)
			}
			if tx.inserted != nil && test.name != "ambiguous inserted marker" {
				t.Fatalf("failed cutover inserted marker: %+v", tx.inserted)
			}
		})
	}
}

func TestMarkerWriterPropagatesUnknownDatabaseEvidence(t *testing.T) {
	request := validWriteRequest()
	tx := validWriteTx(request)
	tx.err = errors.New("database unavailable")
	if _, err := newMarkerWriter(&fakeMarkerWriteStore{tx: tx}).Write(context.Background(), request); err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("unknown database evidence was accepted: %v", err)
	}
}

func TestSQLMarkerWriterLocksPrerequisitesAndQueriesEveryLegacyLeaseOwner(t *testing.T) {
	for name, query := range map[string]string{
		"marker":       markerStatusesForUpdateSQL,
		"prerequisite": prerequisiteForUpdateSQL,
	} {
		if !strings.HasSuffix(strings.TrimSpace(query), "FOR UPDATE") {
			t.Fatalf("%s query is not transaction-locked: %s", name, query)
		}
	}
	for _, table := range []string{"agent_runs", "change_requests", "verification_runs"} {
		if !strings.Contains(legacyActiveLeaseCountSQL, "FROM "+table) {
			t.Fatalf("legacy lease query omits %s", table)
		}
	}
	if strings.Count(legacyActiveLeaseCountSQL, "UTC_TIMESTAMP(6)") != 3 ||
		strings.Count(legacyActiveLeaseCountSQL, "lease_owner <> ''") != 3 {
		t.Fatalf("legacy lease query is not active-owner bounded: %s", legacyActiveLeaseCountSQL)
	}
}

func validWriteRequest() WriteRequest {
	return WriteRequest{
		PlanVersion: 7, SourceExactSHA: strings.Repeat("c", 40), BinaryImageDigest: "sha256:" + strings.Repeat("d", 64),
		SourceSchemaVersion: uint64(schemaversion.Latest), TargetSchemaVersion: uint64(schemaversion.Latest),
		QuiesceLedgerPublicID:        "11111111-1111-4111-8111-111111111111",
		ReconciliationLedgerPublicID: "22222222-2222-4222-8222-222222222222",
		ConverterAuditLedgerPublicID: "33333333-3333-4333-8333-333333333333",
		OldWorkerCount:               0, Confirmation: IrreversibleConfirmation,
	}
}

func validWriteTx(request WriteRequest) *fakeMarkerWriteTx {
	completed := sql.NullTime{Time: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC), Valid: true}
	rows := map[string]prerequisite{}
	for id, operation := range map[string]string{
		request.QuiesceLedgerPublicID:        QuiesceOperation,
		request.ReconciliationLedgerPublicID: ReconciliationOperation,
		request.ConverterAuditLedgerPublicID: ConverterAuditOperation,
	} {
		rows[id] = prerequisite{PublicID: id, PlanVersion: request.PlanVersion, Operation: operation,
			SourceSchemaVersion: request.SourceSchemaVersion, TargetSchemaVersion: request.TargetSchemaVersion,
			Status: "passed", SourceExactSHA: request.SourceExactSHA, BinaryImageDigest: request.BinaryImageDigest, CompletedAt: completed}
	}
	return &fakeMarkerWriteTx{version: request.SourceSchemaVersion, prerequisites: rows, now: completed.Time.Add(time.Minute), markerCount: 1}
}
