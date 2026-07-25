// Package cutover owns the irreversible Phase 7A runtime-generation marker
// reader, startup guards, and fail-closed writer. It does not perform data
// conversion, change runtime generation, or call external systems.
package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	"github.com/google/uuid"
)

const (
	MarkerOperation        = "CUTOVER-V3"
	MarkerStage            = "cutover"
	MarkerConverterVersion = "cutover-marker/v1"

	RuntimeCompatibility RuntimeGeneration = "compatibility"
	RuntimeV3            RuntimeGeneration = "v3"

	// CurrentRuntimeGeneration is intentionally source-bound. This exact-SHA
	// Release A build is the V3-only binary; operators cannot turn a
	// compatibility image into a V3 image with an environment variable.
	CurrentRuntimeGeneration = RuntimeV3
)

var (
	ErrCompatibilityRefused = errors.New("compatibility runtime refused after CUTOVER-V3")
	ErrMarkerRequired       = errors.New("CUTOVER-V3 marker is required")

	lowerHexPattern    = regexp.MustCompile(`^[0-9a-f]+$`)
	imageDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	ledgerReference    = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,128}$`)
)

// RuntimeGeneration identifies which side of the irreversible marker a
// compiled API/Worker belongs to.
type RuntimeGeneration string

// Marker is the immutable migration_ledger row that changes the allowed
// runtime generation. A marker row is not a backfill batch or a dry-run row.
type Marker struct {
	PublicID            string
	PlanVersion         uint64
	Stage               string
	Operation           string
	Attempt             uint64
	PreviousLedgerID    *uint64
	SourceSchemaVersion uint64
	TargetSchemaVersion uint64
	SourceTable         string
	TargetTable         string
	BatchNo             uint64
	IDMin               *uint64
	IDMax               *uint64
	SourceCount         uint64
	TargetCount         uint64
	SkippedCount        uint64
	RejectedCount       uint64
	SourceHash          string
	TargetHash          string
	ConverterVersion    string
	StartedAt           time.Time
	CompletedAt         time.Time
	Status              string
	ReasonCode          string
	BoundedSummary      string
	SourceExactSHA      string
	BinaryImageDigest   string
	ReleaseIdentityHash string
}

// MarkerReader is the read-only startup boundary used by both runtime
// generations and by the cutover-check CLI.
type MarkerReader interface {
	CutoverMarker(context.Context) (Marker, bool, error)
}

type rowScanner interface {
	Scan(...any) error
}

type queryRowFunc func(context.Context, string, ...any) rowScanner

// SQLMarkerReader reads the marker from the existing migration_ledger. It has
// no write method so compatibility startup cannot accidentally advance cutover.
type SQLMarkerReader struct {
	queryRow queryRowFunc
}

func NewSQLMarkerReader(db *sql.DB) (*SQLMarkerReader, error) {
	if db == nil {
		return nil, errors.New("cutover marker database is required")
	}
	return &SQLMarkerReader{
		queryRow: func(ctx context.Context, query string, args ...any) rowScanner {
			return db.QueryRowContext(ctx, query, args...)
		},
	}, nil
}

func (r *SQLMarkerReader) CutoverMarker(ctx context.Context) (Marker, bool, error) {
	if r == nil || r.queryRow == nil {
		return Marker{}, false, errors.New("cutover marker reader is not initialized")
	}
	var marker Marker
	var previousLedgerID, idMin, idMax sql.NullInt64
	var sourceHash, targetHash, reasonCode, boundedSummary sql.NullString
	var completedAt sql.NullTime
	var markerCount uint64
	err := r.queryRow(ctx, `SELECT
	  public_id, plan_version, stage, operation, attempt, previous_ledger_id,
	  source_schema_version, target_schema_version, source_table, target_table, batch_no, id_min, id_max,
	  source_count, target_count, skipped_count, rejected_count,
	  source_hash, target_hash, converter_version,
	  started_at, completed_at,
	  status, reason_code, bounded_summary, source_exact_sha, binary_image_digest, release_identity_hash,
	  COUNT(*) OVER () AS marker_count
	FROM migration_ledger
	WHERE operation = ?
	ORDER BY id
	LIMIT 1`, MarkerOperation).Scan(
		&marker.PublicID, &marker.PlanVersion, &marker.Stage, &marker.Operation, &marker.Attempt, &previousLedgerID,
		&marker.SourceSchemaVersion, &marker.TargetSchemaVersion, &marker.SourceTable, &marker.TargetTable, &marker.BatchNo, &idMin, &idMax,
		&marker.SourceCount, &marker.TargetCount, &marker.SkippedCount, &marker.RejectedCount,
		&sourceHash, &targetHash, &marker.ConverterVersion,
		&marker.StartedAt, &completedAt,
		&marker.Status, &reasonCode, &boundedSummary, &marker.SourceExactSHA, &marker.BinaryImageDigest, &marker.ReleaseIdentityHash, &markerCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Marker{}, false, nil
	}
	if err != nil {
		return Marker{}, true, fmt.Errorf("read cutover marker row: %w", err)
	}
	if markerCount != 1 {
		return Marker{}, true, fmt.Errorf("irreversible cutover marker rows=%d, want exactly 1", markerCount)
	}
	var optionalErr error
	marker.PreviousLedgerID, optionalErr = optionalUint64(previousLedgerID)
	if optionalErr != nil {
		return Marker{}, true, fmt.Errorf("read cutover marker previous_ledger_id: %w", optionalErr)
	}
	marker.IDMin, optionalErr = optionalUint64(idMin)
	if optionalErr != nil {
		return Marker{}, true, fmt.Errorf("read cutover marker id_min: %w", optionalErr)
	}
	marker.IDMax, optionalErr = optionalUint64(idMax)
	if optionalErr != nil {
		return Marker{}, true, fmt.Errorf("read cutover marker id_max: %w", optionalErr)
	}
	marker.SourceHash = sourceHash.String
	marker.TargetHash = targetHash.String
	marker.ReasonCode = reasonCode.String
	marker.BoundedSummary = boundedSummary.String
	if completedAt.Valid {
		marker.CompletedAt = completedAt.Time
	}
	return marker, true, nil
}

func optionalUint64(value sql.NullInt64) (*uint64, error) {
	if !value.Valid {
		return nil, nil
	}
	if value.Int64 < 0 {
		return nil, errors.New("value is negative")
	}
	result := uint64(value.Int64)
	return &result, nil
}

// RuntimeGuard is a read-only startup/readiness check. It cannot create or
// mutate migration_ledger rows.
type RuntimeGuard struct {
	generation RuntimeGeneration
	reader     MarkerReader
}

func NewRuntimeGuard(generation RuntimeGeneration, reader MarkerReader) (*RuntimeGuard, error) {
	if err := generation.Validate(); err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("cutover marker reader is required")
	}
	return &RuntimeGuard{generation: generation, reader: reader}, nil
}

func NewSQLRuntimeGuard(db *sql.DB, generation RuntimeGeneration) (*RuntimeGuard, error) {
	reader, err := NewSQLMarkerReader(db)
	if err != nil {
		return nil, err
	}
	return NewRuntimeGuard(generation, reader)
}

func (g *RuntimeGuard) Check(ctx context.Context) error {
	if g == nil {
		return errors.New("runtime generation guard is not initialized")
	}
	_, err := CheckRuntime(ctx, g.generation, g.reader)
	return err
}

func CheckRuntime(ctx context.Context, generation RuntimeGeneration, reader MarkerReader) (Marker, error) {
	if err := generation.Validate(); err != nil {
		return Marker{}, err
	}
	switch generation {
	case RuntimeCompatibility:
		return Marker{}, RefuseCompatibilityRuntime(ctx, reader)
	case RuntimeV3:
		return RequireV3Runtime(ctx, reader)
	default:
		return Marker{}, fmt.Errorf("unsupported runtime generation %q", generation)
	}
}

func (g RuntimeGeneration) Validate() error {
	switch g {
	case RuntimeCompatibility, RuntimeV3:
		return nil
	default:
		return fmt.Errorf("unsupported runtime generation %q", g)
	}
}

// RefuseCompatibilityRuntime is called before a compatibility process exposes
// routes or starts task claim loops. Any marker read ambiguity is a refusal.
func RefuseCompatibilityRuntime(ctx context.Context, reader MarkerReader) error {
	if reader == nil {
		return errors.Join(ErrCompatibilityRefused, errors.New("cutover marker reader is required"))
	}
	marker, exists, err := reader.CutoverMarker(ctx)
	if err != nil {
		return errors.Join(ErrCompatibilityRefused, fmt.Errorf("inspect cutover marker: %w", err))
	}
	if !exists {
		return nil
	}
	if err := marker.ValidateForRuntime(schemaversion.Latest); err != nil {
		return errors.Join(ErrCompatibilityRefused, fmt.Errorf("invalid cutover marker: %w", err))
	}
	return fmt.Errorf("%w: source_exact_sha=%s binary_image_digest=%s", ErrCompatibilityRefused, marker.SourceExactSHA, marker.BinaryImageDigest)
}

// RequireV3Runtime is the complementary Phase 7A startup contract used when
// the source-bound runtime generation is switched to RuntimeV3.
func RequireV3Runtime(ctx context.Context, reader MarkerReader) (Marker, error) {
	if reader == nil {
		return Marker{}, errors.Join(ErrMarkerRequired, errors.New("cutover marker reader is required"))
	}
	marker, exists, err := reader.CutoverMarker(ctx)
	if err != nil {
		return Marker{}, errors.Join(ErrMarkerRequired, fmt.Errorf("inspect cutover marker: %w", err))
	}
	if !exists {
		return Marker{}, ErrMarkerRequired
	}
	if err := marker.ValidateForRuntime(schemaversion.Latest); err != nil {
		return Marker{}, errors.Join(ErrMarkerRequired, fmt.Errorf("invalid cutover marker: %w", err))
	}
	return marker, nil
}

func (m Marker) Validate(expectedSchemaVersion int64) error {
	if expectedSchemaVersion <= 0 {
		return errors.New("expected schema version must be positive")
	}
	parsedPublicID, err := uuid.Parse(m.PublicID)
	if err != nil || parsedPublicID.String() != m.PublicID {
		return errors.New("public_id must be a canonical lowercase UUID")
	}
	if m.Stage != MarkerStage || m.Operation != MarkerOperation || m.Status != "passed" {
		return fmt.Errorf("marker identity stage=%q operation=%q status=%q", m.Stage, m.Operation, m.Status)
	}
	if m.PlanVersion == 0 || m.Attempt != 1 || m.PreviousLedgerID != nil || m.BatchNo != 0 || m.IDMin != nil || m.IDMax != nil {
		return fmt.Errorf("marker unit plan_version=%d attempt=%d batch_no=%d", m.PlanVersion, m.Attempt, m.BatchNo)
	}
	if !ledgerReference.MatchString(m.SourceTable) || !ledgerReference.MatchString(m.TargetTable) {
		return errors.New("marker source_table and target_table must be bounded ledger references")
	}
	expected := uint64(expectedSchemaVersion)
	if m.SourceSchemaVersion != expected || m.TargetSchemaVersion != expected {
		return fmt.Errorf("marker schema source=%d target=%d want=%d", m.SourceSchemaVersion, m.TargetSchemaVersion, expected)
	}
	if m.SourceCount != 1 || m.TargetCount != 1 || m.SkippedCount != 0 || m.RejectedCount != 0 {
		return fmt.Errorf("marker counts source=%d target=%d skipped=%d rejected=%d", m.SourceCount, m.TargetCount, m.SkippedCount, m.RejectedCount)
	}
	if !isSHA256(m.SourceHash) || !isSHA256(m.TargetHash) {
		return errors.New("marker source_hash and target_hash must be lowercase SHA-256 values")
	}
	if m.ConverterVersion != MarkerConverterVersion {
		return fmt.Errorf("marker converter_version=%q, want %q", m.ConverterVersion, MarkerConverterVersion)
	}
	if m.StartedAt.IsZero() || m.CompletedAt.IsZero() || m.CompletedAt.Before(m.StartedAt) {
		return errors.New("marker completion time is missing or precedes start time")
	}
	if m.ReasonCode != "" {
		return fmt.Errorf("passed marker has reason_code=%q", m.ReasonCode)
	}
	if strings.TrimSpace(m.BoundedSummary) == "" || len(m.BoundedSummary) > 2048 {
		return errors.New("passed marker requires a bounded_summary")
	}
	if !isExactSHA(m.SourceExactSHA) {
		return errors.New("source_exact_sha must be a lowercase 40- or 64-character Git object id")
	}
	if !imageDigestPattern.MatchString(m.BinaryImageDigest) {
		return errors.New("binary_image_digest must be an exact sha256 digest")
	}
	expectedReleaseIdentityHash := releaseIdentityHash(
		m.SourceExactSHA,
		m.BinaryImageDigest,
		m.SourceSchemaVersion,
		m.TargetSchemaVersion,
	)
	if m.ReleaseIdentityHash != expectedReleaseIdentityHash {
		return errors.New("release_identity_hash does not match marker release identity")
	}
	return nil
}

// ValidateForRuntime preserves the immutable schema identity recorded at
// cutover while allowing later forward migrations. A marker from a future
// schema or one that crossed different source/target versions remains invalid.
func (m Marker) ValidateForRuntime(currentSchemaVersion int64) error {
	if currentSchemaVersion <= 0 {
		return errors.New("current schema version must be positive")
	}
	current := uint64(currentSchemaVersion)
	if m.SourceSchemaVersion == 0 || m.SourceSchemaVersion != m.TargetSchemaVersion {
		return fmt.Errorf("marker schema source=%d target=%d must match", m.SourceSchemaVersion, m.TargetSchemaVersion)
	}
	if m.TargetSchemaVersion > current {
		return fmt.Errorf("marker schema version=%d is newer than runtime schema=%d", m.TargetSchemaVersion, current)
	}
	return m.Validate(int64(m.TargetSchemaVersion))
}

func isSHA256(value string) bool {
	return len(value) == 64 && lowerHexPattern.MatchString(value)
}

func isExactSHA(value string) bool {
	return (len(value) == 40 || len(value) == 64) && lowerHexPattern.MatchString(value)
}
