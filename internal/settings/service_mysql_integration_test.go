package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

func TestMySQLConfigurationRevisionSecretAndWorkerBoundary(t *testing.T) {
	dsn := os.Getenv("CLOUDOPS_TEST_SETTINGS_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLOUDOPS_TEST_SETTINGS_MYSQL_DSN is not set; requires a disposable MySQL 8 database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close MySQL integration database: %v", closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	runner, err := migration.NewRunner(ctx, db, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(ctx); err != nil {
		t.Fatal(err)
	}

	dataDir := filepath.Join(t.TempDir(), "cloudops-data")
	service, err := NewService(db, dataDir, BootstrapDiagnostics{MySQLDatabase: "settings-integration"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := Draft{
		Summary: "Update local LLM model",
		General: initial.General,
		Scope:   initial.Scope,
		Scopes: append(append([]OperationalScope{}, initial.Scopes...), OperationalScope{
			Name: "Secondary cluster", ClusterID: "cluster-secondary", Environment: "test", Namespaces: []string{"demo"},
		}),
		Providers:  initial.Providers,
		SecretRefs: initial.SecretRefs,
	}
	for index := range draft.Providers {
		if draft.Providers[index].Provider == ProviderLLM {
			draft.Providers[index].Model = "deepseek-reasoner"
		}
	}
	validation, err := service.Validate(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || validation.DraftHash == "" || len(validation.Errors) != 0 {
		t.Fatalf("validation=%+v", validation)
	}
	revision, err := service.Apply(ctx, validation.ID, draft, RevisionExpectation{ID: initial.ID, Hash: initial.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if !revision.Active || revision.Number != initial.Number+1 || revision.Hash != validation.DraftHash {
		t.Fatalf("applied revision=%+v initial=%+v", revision, initial)
	}
	if len(revision.Scopes) != 2 || revision.Scope.ClusterID != initial.Scope.ClusterID {
		t.Fatalf("revision scopes=%+v active=%+v", revision.Scopes, revision.Scope)
	}
	var secondaryID string
	for _, scope := range revision.Scopes {
		if scope.ClusterID == "cluster-secondary" {
			secondaryID = scope.ID
		}
	}
	selected, err := service.ActivateScope(ctx, secondaryID)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ClusterID != "cluster-secondary" || !selected.Active {
		t.Fatalf("selected scope=%+v", selected)
	}
	replayed, err := service.Apply(ctx, validation.ID, draft, RevisionExpectation{ID: initial.ID, Hash: initial.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != revision.ID {
		t.Fatalf("idempotent apply revision=%s want=%s", replayed.ID, revision.ID)
	}

	staleDraft := draft
	staleDraft.Summary = "Different unvalidated draft"
	if _, err := service.Apply(ctx, validation.ID, staleDraft, RevisionExpectation{ID: initial.ID, Hash: initial.Hash}); !errors.Is(err, ErrValidationStale) {
		t.Fatalf("stale apply error=%v", err)
	}
	secondDraft := draft
	secondDraft.Summary = "Concurrent configuration based on the initial revision"
	secondValidation, err := service.Validate(ctx, secondDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(ctx, secondValidation.ID, secondDraft, RevisionExpectation{ID: initial.ID, Hash: initial.Hash}); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("concurrent apply error=%v", err)
	}
	active, err := service.ActiveRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != revision.ID {
		t.Fatalf("failed apply changed active revision=%s want=%s", active.ID, revision.ID)
	}

	secretValue := "integration-secret-value"
	secret, err := service.WriteSecret(ctx, SecretInput{Provider: ProviderLLM, Purpose: "api_key", Value: secretValue})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretValue) || strings.Contains(string(encoded), `"value"`) {
		t.Fatalf("secret response leaked write-only value: %s", encoded)
	}
	secretPath := filepath.Join(dataDir, "secrets", string(ProviderLLM), "api_key", secret.ID)
	info, err := os.Lstat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode=%v", info.Mode())
	}

	activation, err := NewActivationRunner(service, "settings-integration-worker")
	if err != nil {
		t.Fatal(err)
	}
	activation.interval = 10 * time.Millisecond
	if err := activation.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = activation.Stop(stopCtx)
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		observed, loadErr := service.Revision(ctx, revision.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if observed.WorkerBoundary != nil && observed.WorkerBoundary.Status == "succeeded" {
			if observed.WorkerBoundary.ObservedHash != revision.Hash || observed.WorkerBoundary.ObservedAt == nil {
				t.Fatalf("worker boundary=%+v revision=%+v", observed.WorkerBoundary, revision)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("worker did not observe the applied Configuration Revision")
}
