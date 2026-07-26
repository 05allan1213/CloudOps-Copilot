package baselinemysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"

	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

func TestMySQLBaselineActivationIsAtomicIdempotentAndUnique(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := openBaselineSQL(t, adminDSN)
	databaseName := fmt.Sprintf("cloudops_v3_baseline_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"`"); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	db := openBaselineSQL(t, baselineDatabaseDSN(t, adminDSN, databaseName))
	defer func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + databaseName + "`")
		_ = admin.Close()
	}()
	runner, err := migrationrunner.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatalf("migrate baseline database: %v", err)
	}
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	type activation struct {
		result baseline.ActivationResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan activation, 2)
	snapshots := []baseline.Snapshot{
		mysqlBaselineSnapshot(t, "a", "healthy-v1", time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)),
		mysqlBaselineSnapshot(t, "a", "healthy-v1", time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)),
	}
	for _, snapshot := range snapshots {
		go func(snapshot baseline.Snapshot) {
			<-start
			result, activateErr := repository.Activate(ctx, snapshot)
			results <- activation{result: result, err: activateErr}
		}(snapshot)
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent activation errors: first=%v second=%v", first.err, second.err)
	}
	if first.result.BaselineID == 0 || first.result.BaselineID != second.result.BaselineID || first.result.Created == second.result.Created {
		t.Fatalf("concurrent activation results are not idempotent: first=%+v second=%+v", first.result, second.result)
	}
	assertBaselineCounts(t, ctx, db, 1, 1, 0, 7)
	assertStrictBaselineReplay(t, ctx, db, repository, snapshots[0], first.result.BaselineID, first.result.ObservationIDs[0])

	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_baseline_observation
BEFORE INSERT ON baseline_observations FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'forced baseline rollback'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Activate(ctx, mysqlBaselineSnapshot(t, "b", "healthy-v2", time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC))); err == nil {
		t.Fatal("forced observation failure unexpectedly committed")
	}
	assertBaselineCounts(t, ctx, db, 1, 1, 0, 7)
	if _, err := db.ExecContext(ctx, "DROP TRIGGER fail_baseline_observation"); err != nil {
		t.Fatal(err)
	}

	newResult, err := repository.Activate(ctx, mysqlBaselineSnapshot(t, "b", "healthy-v2", time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)))
	if err != nil || !newResult.Created || newResult.SupersededBaselineID != first.result.BaselineID {
		t.Fatalf("superseding activation result=%+v err=%v", newResult, err)
	}
	assertBaselineCounts(t, ctx, db, 2, 1, 1, 14)
	replayed, err := repository.Activate(ctx, mysqlBaselineSnapshot(t, "b", "healthy-v2", time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)))
	if err != nil || replayed.Created || replayed.BaselineID != newResult.BaselineID || len(replayed.ObservationIDs) != 7 {
		t.Fatalf("idempotent replay result=%+v err=%v", replayed, err)
	}
	assertBaselineCounts(t, ctx, db, 2, 1, 1, 14)

	transactional := []baseline.Snapshot{
		mysqlBaselineSnapshot(t, "c", "healthy-v3", time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)),
		mysqlBaselineSnapshot(t, "d", "healthy-v4", time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)),
	}
	transactionalResults := make(chan activation, len(transactional))
	start = make(chan struct{})
	for _, snapshot := range transactional {
		go func(snapshot baseline.Snapshot) {
			<-start
			tx, beginErr := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if beginErr != nil {
				transactionalResults <- activation{err: beginErr}
				return
			}
			result, activateErr := repository.ActivateIn(ctx, tx, snapshot)
			if activateErr == nil {
				activateErr = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			transactionalResults <- activation{result: result, err: activateErr}
		}(snapshot)
	}
	close(start)
	for range transactional {
		result := <-transactionalResults
		if result.err != nil || !result.result.Created || result.result.SupersededBaselineID == 0 {
			t.Fatalf("transaction-bound concurrent activation result=%+v err=%v", result.result, result.err)
		}
	}
	assertBaselineCounts(t, ctx, db, 4, 1, 3, 28)
}

func assertStrictBaselineReplay(t *testing.T, ctx context.Context, db *sql.DB, repository *Repository, snapshot baseline.Snapshot, baselineID, observationID uint64) {
	t.Helper()
	assertConflict := func(name string) {
		t.Helper()
		if _, err := repository.Activate(ctx, snapshot); !errors.Is(err, baseline.ErrConflict) {
			t.Fatalf("%s drift replay err=%v", name, err)
		}
	}
	if _, err := db.ExecContext(ctx, "UPDATE deployment_baselines SET verification_hash = ? WHERE id = ?", strings.Repeat("0", 64), baselineID); err != nil {
		t.Fatal(err)
	}
	assertConflict("verification_hash")
	if _, err := db.ExecContext(ctx, "UPDATE deployment_baselines SET verification_hash = ? WHERE id = ?", snapshot.VerificationHash, baselineID); err != nil {
		t.Fatal(err)
	}

	var source, contentHash, dedupeKey string
	var observed []byte
	var observedAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT source_identity, CAST(observed_json AS CHAR), content_hash, dedupe_key, observed_at FROM baseline_observations WHERE id = ?`, observationID).Scan(&source, &observed, &contentHash, &dedupeKey, &observedAt); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name    string
		mutate  func() error
		restore func() error
	}{
		{"source_identity", func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET source_identity = ? WHERE id = ?", source+"-drift", observationID)
			return err
		}, func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET source_identity = ? WHERE id = ?", source, observationID)
			return err
		}},
		{"observed_json", func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET observed_json = JSON_OBJECT('drift', TRUE) WHERE id = ?", observationID)
			return err
		}, func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET observed_json = ? WHERE id = ?", observed, observationID)
			return err
		}},
		{"content_hash", func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET content_hash = ? WHERE id = ?", strings.Repeat("0", 64), observationID)
			return err
		}, func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET content_hash = ? WHERE id = ?", contentHash, observationID)
			return err
		}},
		{"dedupe_key", func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET dedupe_key = ? WHERE id = ?", strings.Repeat("0", 64), observationID)
			return err
		}, func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET dedupe_key = ? WHERE id = ?", dedupeKey, observationID)
			return err
		}},
		{"observed_at", func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET observed_at = DATE_ADD(observed_at, INTERVAL 1 MICROSECOND) WHERE id = ?", observationID)
			return err
		}, func() error {
			_, err := db.ExecContext(ctx, "UPDATE baseline_observations SET observed_at = ? WHERE id = ?", observedAt, observationID)
			return err
		}},
	}
	for _, mutation := range mutations {
		if err := mutation.mutate(); err != nil {
			t.Fatal(err)
		}
		assertConflict(mutation.name)
		if err := mutation.restore(); err != nil {
			t.Fatal(err)
		}
	}
	if replay, err := repository.Activate(ctx, snapshot); err != nil || replay.Created {
		t.Fatalf("strict replay did not recover after restoring immutable rows: result=%+v err=%v", replay, err)
	}
}

func mysqlBaselineSnapshot(t *testing.T, revisionSeed, config string, observedAt time.Time) baseline.Snapshot {
	t.Helper()
	revision := strings.Repeat(revisionSeed, 40)
	configBytes := []byte(config)
	configHash := mysqlBaselineHash(configBytes)
	snapshot := baseline.Snapshot{
		Target: baseline.Target{
			Cluster: "kind-cloudops-local", Environment: "local-demo", Namespace: "demo", WorkloadKind: "Deployment",
			WorkloadName: "demo", ContainerName: "demo", Repository: "acme/gitops",
			BaseBranch: "main", TargetPath: "apps/demo/deployment.yaml",
		},
		SourceRevision: strings.Repeat("c", 40), ImageDigest: "sha256:" + strings.Repeat("d", 64),
		GitOpsRevision: revision, ConfigHash: configHash, VerifiedAt: observedAt,
	}
	payloads := map[baseline.ObservationType]any{
		baseline.ObservationArgoRevision:        map[string]any{"revision": revision},
		baseline.ObservationKubernetesReadiness: map[string]any{"ready": 2, "desired": 2},
		baseline.ObservationAlertState:          map[string]any{"firing": 0},
		baseline.ObservationMetric:              map[string]any{"error_rate": 0.001, "availability": 0.999},
		baseline.ObservationLog:                 map[string]any{"required_env_missing": 0},
		baseline.ObservationTrace:               map[string]any{"error_rate": 0.001},
		baseline.ObservationConfigBlob:          map[string]any{"content_hash": configHash},
	}
	for typ, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		contentHash := mysqlBaselineHash(raw)
		if typ == baseline.ObservationConfigBlob {
			contentHash = configHash
		}
		snapshot.Observations = append(snapshot.Observations, baseline.Observation{
			Type: typ, SourceIdentity: "mysql-test/" + string(typ), ObservedJSON: raw,
			ContentHash: contentHash, ObservedAt: observedAt,
		})
	}
	if err := snapshot.Finalize(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertBaselineCounts(t *testing.T, ctx context.Context, db *sql.DB, total, active, superseded, observations int) {
	t.Helper()
	var gotTotal, gotActive, gotSuperseded, gotObservations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),
  COALESCE(SUM(status = 'active'), 0), COALESCE(SUM(status = 'superseded'), 0)
FROM deployment_baselines`).Scan(&gotTotal, &gotActive, &gotSuperseded); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM baseline_observations").Scan(&gotObservations); err != nil {
		t.Fatal(err)
	}
	if gotTotal != total || gotActive != active || gotSuperseded != superseded || gotObservations != observations {
		t.Fatalf("baseline counts total=%d active=%d superseded=%d observations=%d, want %d/%d/%d/%d",
			gotTotal, gotActive, gotSuperseded, gotObservations, total, active, superseded, observations)
	}
}

func mysqlBaselineHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func openBaselineSQL(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func baselineDatabaseDSN(t *testing.T, adminDSN, databaseName string) string {
	t.Helper()
	config, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName = databaseName
	config.ParseTime = true
	config.MultiStatements = true
	config.Loc = time.UTC
	return config.FormatDSN()
}
