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

type scanFunc func(...any) error

func (f scanFunc) Scan(dest ...any) error { return f(dest...) }

type markerReaderFunc func(context.Context) (Marker, bool, error)

func (f markerReaderFunc) CutoverMarker(ctx context.Context) (Marker, bool, error) {
	return f(ctx)
}

func TestSQLMarkerReaderUsesOneSnapshotAndTypedTimes(t *testing.T) {
	marker := validMarker()
	calls := 0
	var query string
	reader := &SQLMarkerReader{queryRow: func(_ context.Context, statement string, args ...any) rowScanner {
		calls++
		query = statement
		if len(args) != 1 || args[0] != MarkerOperation {
			t.Fatalf("query args=%v", args)
		}
		return scanMarker(marker, 1)
	}}

	got, exists, err := reader.CutoverMarker(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !exists || calls != 1 {
		t.Fatalf("exists=%t calls=%d", exists, calls)
	}
	if got.StartedAt != marker.StartedAt || got.CompletedAt != marker.CompletedAt {
		t.Fatalf("typed times changed: got=%s..%s want=%s..%s", got.StartedAt, got.CompletedAt, marker.StartedAt, marker.CompletedAt)
	}
	if !strings.Contains(query, "COUNT(*) OVER ()") || strings.Contains(query, "DATE_FORMAT") {
		t.Fatalf("marker query is not a single typed snapshot: %s", query)
	}
}

func TestSQLMarkerReaderDistinguishesAbsentAndAmbiguousRows(t *testing.T) {
	absent := &SQLMarkerReader{queryRow: func(context.Context, string, ...any) rowScanner {
		return scanFunc(func(...any) error { return sql.ErrNoRows })
	}}
	if marker, exists, err := absent.CutoverMarker(context.Background()); err != nil || exists || marker != (Marker{}) {
		t.Fatalf("absent marker result=(%+v,%t,%v)", marker, exists, err)
	}

	ambiguous := &SQLMarkerReader{queryRow: func(context.Context, string, ...any) rowScanner {
		return scanMarker(validMarker(), 2)
	}}
	if _, exists, err := ambiguous.CutoverMarker(context.Background()); err == nil || !exists || !strings.Contains(err.Error(), "rows=2") {
		t.Fatalf("ambiguous marker result exists=%t err=%v", exists, err)
	}
}

func TestMarkerValidateRequiresCompleteIrreversibleLedgerUnit(t *testing.T) {
	if err := validMarker().Validate(schemaversion.Latest); err != nil {
		t.Fatalf("valid marker rejected: %v", err)
	}
	one := uint64(1)
	tests := []struct {
		name string
		edit func(*Marker)
	}{
		{name: "public id", edit: func(value *Marker) { value.PublicID = strings.ToUpper(value.PublicID) }},
		{name: "previous attempt", edit: func(value *Marker) { value.PreviousLedgerID = &one }},
		{name: "batch range", edit: func(value *Marker) { value.IDMin = &one }},
		{name: "source table", edit: func(value *Marker) { value.SourceTable = "" }},
		{name: "target table", edit: func(value *Marker) { value.TargetTable = "bad table" }},
		{name: "schema", edit: func(value *Marker) { value.TargetSchemaVersion-- }},
		{name: "count", edit: func(value *Marker) { value.RejectedCount = 1 }},
		{name: "source hash", edit: func(value *Marker) { value.SourceHash = strings.Repeat("A", 64) }},
		{name: "converter", edit: func(value *Marker) { value.ConverterVersion = "legacy" }},
		{name: "completion", edit: func(value *Marker) { value.CompletedAt = value.StartedAt.Add(-time.Microsecond) }},
		{name: "reason", edit: func(value *Marker) { value.ReasonCode = "forced" }},
		{name: "summary", edit: func(value *Marker) { value.BoundedSummary = "" }},
		{name: "source sha", edit: func(value *Marker) { value.SourceExactSHA = strings.Repeat("a", 41) }},
		{name: "image digest", edit: func(value *Marker) { value.BinaryImageDigest = "latest" }},
		{name: "release identity hash", edit: func(value *Marker) { value.ReleaseIdentityHash = strings.Repeat("e", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			marker := validMarker()
			test.edit(&marker)
			if err := marker.Validate(schemaversion.Latest); err == nil {
				t.Fatalf("invalid marker accepted: %+v", marker)
			}
		})
	}
}

func TestRuntimeMarkerAllowsLaterForwardSchemaAndRejectsFutureOrMismatchedSchema(t *testing.T) {
	marker := validMarker()
	marker.SourceSchemaVersion--
	marker.TargetSchemaVersion--
	marker.ReleaseIdentityHash = releaseIdentityHash(
		marker.SourceExactSHA,
		marker.BinaryImageDigest,
		marker.SourceSchemaVersion,
		marker.TargetSchemaVersion,
	)
	if err := marker.ValidateForRuntime(schemaversion.Latest); err != nil {
		t.Fatalf("prior-schema marker rejected after forward migration: %v", err)
	}
	if err := marker.Validate(schemaversion.Latest); err == nil {
		t.Fatal("exact cutover writer validation accepted a prior-schema marker")
	}

	future := validMarker()
	future.SourceSchemaVersion++
	future.TargetSchemaVersion++
	future.ReleaseIdentityHash = releaseIdentityHash(
		future.SourceExactSHA,
		future.BinaryImageDigest,
		future.SourceSchemaVersion,
		future.TargetSchemaVersion,
	)
	if err := future.ValidateForRuntime(schemaversion.Latest); err == nil {
		t.Fatal("runtime accepted a future-schema marker")
	}

	mismatched := validMarker()
	mismatched.SourceSchemaVersion--
	if err := mismatched.ValidateForRuntime(schemaversion.Latest); err == nil {
		t.Fatal("runtime accepted a marker with different source and target schemas")
	}
}

func TestRuntimeGenerationGuardsFailClosed(t *testing.T) {
	valid := validMarker()
	absent := markerReaderFunc(func(context.Context) (Marker, bool, error) { return Marker{}, false, nil })
	present := markerReaderFunc(func(context.Context) (Marker, bool, error) { return valid, true, nil })
	invalid := markerReaderFunc(func(context.Context) (Marker, bool, error) {
		marker := valid
		marker.Status = "running"
		return marker, true, nil
	})
	unavailable := markerReaderFunc(func(context.Context) (Marker, bool, error) {
		return Marker{}, false, errors.New("database unavailable")
	})

	if _, err := CheckRuntime(context.Background(), RuntimeCompatibility, absent); err != nil {
		t.Fatalf("compatibility runtime rejected absent marker: %v", err)
	}
	if _, err := CheckRuntime(context.Background(), RuntimeCompatibility, present); !errors.Is(err, ErrCompatibilityRefused) {
		t.Fatalf("compatibility runtime accepted marker: %v", err)
	}
	if _, err := CheckRuntime(context.Background(), RuntimeCompatibility, unavailable); !errors.Is(err, ErrCompatibilityRefused) {
		t.Fatalf("compatibility runtime accepted ambiguous read: %v", err)
	}
	if _, err := CheckRuntime(context.Background(), RuntimeV3, absent); !errors.Is(err, ErrMarkerRequired) {
		t.Fatalf("V3 runtime accepted absent marker: %v", err)
	}
	if _, err := CheckRuntime(context.Background(), RuntimeV3, invalid); !errors.Is(err, ErrMarkerRequired) {
		t.Fatalf("V3 runtime accepted invalid marker: %v", err)
	}
	marker, err := CheckRuntime(context.Background(), RuntimeV3, present)
	if err != nil || marker.PublicID != valid.PublicID {
		t.Fatalf("V3 runtime rejected valid marker: marker=%+v err=%v", marker, err)
	}
	if _, err := CheckRuntime(context.Background(), RuntimeGeneration("unknown"), present); err == nil {
		t.Fatal("unknown runtime generation was accepted")
	}
	if CurrentRuntimeGeneration != RuntimeV3 {
		t.Fatalf("current exact-SHA runtime generation=%q, want v3", CurrentRuntimeGeneration)
	}
}

func validMarker() Marker {
	started := time.Date(2026, 7, 21, 1, 2, 3, 456000000, time.UTC)
	marker := Marker{
		PublicID:            "12345678-1234-4234-8234-123456789abc",
		PlanVersion:         1,
		Stage:               MarkerStage,
		Operation:           MarkerOperation,
		Attempt:             1,
		SourceSchemaVersion: uint64(schemaversion.Latest),
		TargetSchemaVersion: uint64(schemaversion.Latest),
		SourceTable:         "migration_ledger",
		TargetTable:         "runtime_generation",
		SourceCount:         1,
		TargetCount:         1,
		SourceHash:          strings.Repeat("a", 64),
		TargetHash:          strings.Repeat("b", 64),
		ConverterVersion:    MarkerConverterVersion,
		StartedAt:           started,
		CompletedAt:         started.Add(time.Second),
		Status:              "passed",
		BoundedSummary:      "all pre-cutover ledger units passed and runtime generation advanced",
		SourceExactSHA:      strings.Repeat("c", 40),
		BinaryImageDigest:   "sha256:" + strings.Repeat("d", 64),
	}
	marker.ReleaseIdentityHash = releaseIdentityHash(
		marker.SourceExactSHA,
		marker.BinaryImageDigest,
		marker.SourceSchemaVersion,
		marker.TargetSchemaVersion,
	)
	return marker
}

func scanMarker(marker Marker, count uint64) rowScanner {
	return scanFunc(func(dest ...any) error {
		if len(dest) != 29 {
			return errors.New("unexpected marker scan width")
		}
		*dest[0].(*string) = marker.PublicID
		*dest[1].(*uint64) = marker.PlanVersion
		*dest[2].(*string) = marker.Stage
		*dest[3].(*string) = marker.Operation
		*dest[4].(*uint64) = marker.Attempt
		*dest[5].(*sql.NullInt64) = nullableInt64(marker.PreviousLedgerID)
		*dest[6].(*uint64) = marker.SourceSchemaVersion
		*dest[7].(*uint64) = marker.TargetSchemaVersion
		*dest[8].(*string) = marker.SourceTable
		*dest[9].(*string) = marker.TargetTable
		*dest[10].(*uint64) = marker.BatchNo
		*dest[11].(*sql.NullInt64) = nullableInt64(marker.IDMin)
		*dest[12].(*sql.NullInt64) = nullableInt64(marker.IDMax)
		*dest[13].(*uint64) = marker.SourceCount
		*dest[14].(*uint64) = marker.TargetCount
		*dest[15].(*uint64) = marker.SkippedCount
		*dest[16].(*uint64) = marker.RejectedCount
		*dest[17].(*sql.NullString) = sql.NullString{String: marker.SourceHash, Valid: marker.SourceHash != ""}
		*dest[18].(*sql.NullString) = sql.NullString{String: marker.TargetHash, Valid: marker.TargetHash != ""}
		*dest[19].(*string) = marker.ConverterVersion
		*dest[20].(*time.Time) = marker.StartedAt
		*dest[21].(*sql.NullTime) = sql.NullTime{Time: marker.CompletedAt, Valid: !marker.CompletedAt.IsZero()}
		*dest[22].(*string) = marker.Status
		*dest[23].(*sql.NullString) = sql.NullString{String: marker.ReasonCode, Valid: marker.ReasonCode != ""}
		*dest[24].(*sql.NullString) = sql.NullString{String: marker.BoundedSummary, Valid: marker.BoundedSummary != ""}
		*dest[25].(*string) = marker.SourceExactSHA
		*dest[26].(*string) = marker.BinaryImageDigest
		*dest[27].(*string) = marker.ReleaseIdentityHash
		*dest[28].(*uint64) = count
		return nil
	})
}

func nullableInt64(value *uint64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}
