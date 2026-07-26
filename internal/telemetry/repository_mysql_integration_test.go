package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	drivermysql "github.com/go-sql-driver/mysql"

	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func TestMySQLTelemetryMetadataEvidenceAndFrozenContext(t *testing.T) {
	db := openTelemetryIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settingsService, err := settings.NewService(db, t.TempDir(), settings.BootstrapDiagnostics{MySQLDatabase: "telemetry-integration"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := settingsService.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Scopes) == 0 || len(revision.Scopes[0].Namespaces) == 0 {
		t.Fatalf("active revision has no bounded scope: %#v", revision)
	}
	scope := revision.Scopes[0]
	namespace := scope.Namespaces[0]
	resource := ResourceReference{
		ID:   "kubernetes://" + scope.ClusterID + "/apps/v1/namespaces/" + namespace + "/deployments/cloudops-api",
		Kind: "Deployment", Namespace: namespace, Name: "cloudops-api",
	}
	to := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	prepared := newPrepared("elasticsearch", "logs", revision.ID, ModeGuided,
		`{"bool":{"filter":[],"must":[]}}`, scope, resource, TimeRange{From: to.Add(-15 * time.Minute), To: to},
		QueryBounds{MaxLookbackSeconds: 3600, TimeoutMS: 5000, MaxResponseBytes: 1024 * 1024, MaxResults: 100, ConcurrencyLimit: 2})
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := repository.CreateExecution(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRunning(ctx, execution.ID); err != nil {
		t.Fatal(err)
	}
	source := ProviderSource{Provider: "elasticsearch", Identity: "http://elasticsearch:9200/logs-cloudops-*", ServerVersion: "9.4.3", CollectedAt: to}
	if err := repository.Complete(ctx, execution.ID, "logs", source, 2, 2048, false, true, nil); err != nil {
		t.Fatal(err)
	}
	execution, err = repository.Execution(ctx, execution.ID, "elasticsearch")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "succeeded" || execution.Kind != "logs" || execution.ResultCount != 2 || !execution.Truncated {
		t.Fatalf("execution=%#v", execution)
	}

	facts, _ := json.Marshal(map[string]any{"kind": "log", "facts": []any{map[string]any{"id": "selected-row", "message": "bounded fact"}}})
	provenance, _ := json.Marshal(map[string]any{"provider": "elasticsearch", "query_execution_id": execution.ID})
	evidenceInput := evidenceInsert{
		Type: "log_selection", Summary: "one selected log", Facts: facts, FactCount: 1,
		ContentHash: sha256Bytes(facts), ScopeHash: sha256Text(scope.ClusterID), ArgumentsHash: sha256Text(execution.Query),
		Provenance: provenance, ProvenanceHash: sha256Bytes(provenance), Truncated: true, ObservedAt: to.Add(-time.Minute),
	}
	evidence, err := repository.RetainEvidence(ctx, execution, evidenceInput)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.RetainEvidence(ctx, execution, evidenceInput)
	if err != nil || replayed.ID != evidence.ID {
		t.Fatalf("idempotent Evidence replay=%#v error=%v", replayed, err)
	}

	request := CreateConsultationRequest{
		Title: "Investigate selected log", ClusterID: scope.ClusterID, Environment: scope.Environment,
		Namespaces: []string{namespace}, Resources: []ResourceReference{resource}, From: execution.TimeRange.From,
		To: execution.TimeRange.To, QueryIDs: []string{execution.ID}, EvidenceIDs: []string{evidence.ID},
	}
	if err := repository.ValidateSnapshotReferences(ctx, revision.ID, request); err != nil {
		t.Fatal(err)
	}
	consultation, err := repository.CreateConsultation(ctx, revision.ID, request, sha256Text("immutable-context"))
	if err != nil {
		t.Fatal(err)
	}
	if consultation.Snapshot.ContentHash != sha256Text("immutable-context") || consultation.Snapshot.QueryIDs[0] != execution.ID {
		t.Fatalf("consultation=%#v", consultation)
	}

	mismatch := request
	mismatch.Resources = []ResourceReference{{ID: "other", Kind: "Deployment", Namespace: namespace, Name: "other"}}
	if err := repository.ValidateSnapshotReferences(ctx, revision.ID, mismatch); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched frozen context error=%v", err)
	}

	assertTelemetryCount(t, ctx, db, `SELECT COUNT(*) FROM query_execution_events qee JOIN query_executions qe ON qe.id=qee.query_execution_id WHERE qe.public_id=?`, 3, execution.ID)
	assertTelemetryCount(t, ctx, db, `SELECT COUNT(*) FROM evidence_items WHERE query_execution_id IS NOT NULL AND incident_id IS NULL`, 1)
	assertTelemetryCount(t, ctx, db, `SELECT COUNT(*) FROM context_snapshots WHERE public_id=? AND JSON_LENGTH(query_execution_refs_json)=1 AND JSON_LENGTH(evidence_refs_json)=1`, 1, consultation.Snapshot.ID)
	assertTelemetryCount(t, ctx, db, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='query_executions' AND column_name IN ('raw_result','result_json','provider_response')`, 0)
	var storedFacts []byte
	if err := db.QueryRowContext(ctx, `SELECT facts_json FROM evidence_items WHERE public_id=?`, evidence.ID).Scan(&storedFacts); err != nil {
		t.Fatal(err)
	}
	var storedValue, expectedValue any
	if json.Unmarshal(storedFacts, &storedValue) != nil || json.Unmarshal(facts, &expectedValue) != nil || !reflect.DeepEqual(storedValue, expectedValue) {
		t.Fatalf("stored Evidence=%s want selected facts=%s", storedFacts, facts)
	}
}

func openTelemetryIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	name := fmt.Sprintf("cloudops_telemetry_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		_ = admin.Close()
	})
	config, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName, config.ParseTime, config.MultiStatements = name, true, true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	runner, err := migrationrunner.NewRunner(ctx, db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertTelemetryCount(t *testing.T, ctx context.Context, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d query=%s", got, want, query)
	}
}
