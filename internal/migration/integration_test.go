package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	legacymodel "github.com/05allan1213/CloudOps-Copilot/internal/model"
)

var legacyTables = []string{
	"users",
	"host_groups",
	"host_group_members",
	"alert_rules",
	"notification_channels",
	"alert_histories",
	"diagnosis_reports",
	"diagnosis_feedback",
	"pending_actions",
	"audit_logs",
}

func TestPhase1MigrationFreshExistingParityAndLock(t *testing.T) {
	adminDSN := os.Getenv("CLOUDOPS_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("CLOUDOPS_TEST_MYSQL_ADMIN_DSN is not set; requires disposable MySQL 8 admin scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	admin := openSQL(t, adminDSN)
	defer func() { _ = admin.Close() }()
	var version string
	if err := admin.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "8.") {
		t.Fatalf("requires MySQL 8, got %q", version)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	names := map[string]string{
		"fresh":      "cloudops_phase1_fresh_" + suffix,
		"existing":   "cloudops_phase1_existing_" + suffix,
		"concurrent": "cloudops_phase1_concurrent_" + suffix,
		"locked":     "cloudops_phase1_locked_" + suffix,
		"failure":    "cloudops_phase1_failure_" + suffix,
	}
	for _, name := range names {
		createDatabase(t, ctx, admin, name)
		defer dropDatabase(t, admin, name)
	}

	fresh := openSQL(t, databaseDSN(t, adminDSN, names["fresh"]))
	defer func() { _ = fresh.Close() }()
	existing := openSQL(t, databaseDSN(t, adminDSN, names["existing"]))
	defer func() { _ = existing.Close() }()

	freshRunner := newTestRunner(t, ctx, fresh, 5*time.Second)
	results, err := freshRunner.Up(ctx)
	if err != nil {
		t.Fatalf("fresh current forward migrations: %v", err)
	}
	if len(results) != int(LatestVersion) {
		t.Fatalf("fresh applied migrations=%d, want %d", len(results), LatestVersion)
	}
	assertVersion(t, ctx, freshRunner, LatestVersion)

	existingRunner := newTestRunner(t, ctx, existing, 5*time.Second)
	if _, err := existingRunner.provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("existing 00001-00006: %v", err)
	}
	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: existing, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gormDB.AutoMigrate(legacymodel.AllModels()...); err != nil {
		t.Fatalf("create existing V2 AutoMigrate schema fixture: %v", err)
	}
	insertLegacySentinels(t, ctx, existing)
	dataBefore := dataSnapshot(t, ctx, existing, legacyTables)
	if _, err := existingRunner.Up(ctx); err != nil {
		t.Fatalf("existing upgrade through current forward migrations: %v", err)
	}
	assertVersion(t, ctx, existingRunner, LatestVersion)
	dataAfter := dataSnapshot(t, ctx, existing, legacyTables)
	if dataBefore != dataAfter {
		t.Fatalf("legacy data changed across 00007: before=%s after=%s", dataBefore, dataAfter)
	}

	freshSchema := schemaSnapshot(t, ctx, fresh)
	existingSchema := schemaSnapshot(t, ctx, existing)
	if freshSchema != existingSchema {
		t.Fatalf("fresh/existing schema mismatch\n--- fresh ---\n%s\n--- existing ---\n%s", freshSchema, existingSchema)
	}
	repeatSchema := schemaSnapshot(t, ctx, existing)
	repeatResults, err := existingRunner.Up(ctx)
	if err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if len(repeatResults) != 0 {
		t.Fatalf("repeat migration applied %d migrations", len(repeatResults))
	}
	if repeatSchema != schemaSnapshot(t, ctx, existing) || dataAfter != dataSnapshot(t, ctx, existing, legacyTables) {
		t.Fatal("repeat migration changed schema or data")
	}

	t.Run("concurrent up", func(t *testing.T) {
		dsn := databaseDSN(t, adminDSN, names["concurrent"])
		db1, db2 := openSQL(t, dsn), openSQL(t, dsn)
		defer func() { _ = db1.Close() }()
		defer func() { _ = db2.Close() }()
		// MySQL 8 may spend more than ten seconds applying the forward DDL batch
		// on a loaded disposable instance; the competing runner must wait for
		// that legitimate migration rather than turn load into a false failure.
		runners := []*Runner{newTestRunner(t, ctx, db1, 60*time.Second), newTestRunner(t, ctx, db2, 60*time.Second)}
		start := make(chan struct{})
		errorsByRunner := make([]error, len(runners))
		var wait sync.WaitGroup
		for index, runner := range runners {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				_, errorsByRunner[index] = runner.Up(ctx)
			}()
		}
		close(start)
		wait.Wait()
		for index, err := range errorsByRunner {
			if err != nil {
				t.Fatalf("concurrent runner %d: %v", index, err)
			}
		}
		assertVersion(t, ctx, runners[0], LatestVersion)
		var applied int
		if err := db1.QueryRowContext(ctx, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = ? AND is_applied = 1", LatestVersion).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied != 1 {
			t.Fatalf("latest migration applied rows=%d, want 1", applied)
		}
	})

	t.Run("advisory lock blocks and releases", func(t *testing.T) {
		dsn := databaseDSN(t, adminDSN, names["locked"])
		db := openSQL(t, dsn)
		defer func() { _ = db.Close() }()
		runner := newTestRunner(t, ctx, db, time.Second)
		if _, err := runner.provider.UpTo(ctx, 6); err != nil {
			t.Fatal(err)
		}
		lockConn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = lockConn.Close() }()
		lockName := LockName(names["locked"])
		var acquired int
		if err := lockConn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 1)", lockName).Scan(&acquired); err != nil || acquired != 1 {
			t.Fatalf("pre-acquire lock: acquired=%d err=%v", acquired, err)
		}
		if _, err := runner.Up(ctx); err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("migration under held lock err=%v", err)
		}
		assertDatabaseVersion(t, ctx, db, 6)
		var released int
		if err := lockConn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&released); err != nil || released != 1 {
			t.Fatalf("release held lock: released=%d err=%v", released, err)
		}
		if _, err := runner.Up(ctx); err != nil {
			t.Fatalf("migration after lock release: %v", err)
		}
		assertVersion(t, ctx, runner, LatestVersion)
	})

	t.Run("migration failure releases advisory lock", func(t *testing.T) {
		db := openSQL(t, databaseDSN(t, adminDSN, names["failure"]))
		defer func() { _ = db.Close() }()
		lockName := LockName(names["failure"])
		provider, err := goose.NewProvider(
			goose.DialectMySQL,
			db,
			fstest.MapFS{"00001_invalid.sql": {Data: []byte("-- +goose Up\nTHIS IS NOT SQL;\n")}},
			goose.WithSessionLocker(&mysqlSessionLocker{name: lockName, timeoutSeconds: 1}),
			goose.WithDisableGlobalRegistry(true),
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Up(ctx); err == nil {
			t.Fatal("invalid migration unexpectedly succeeded")
		}
		probe, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = probe.Close() }()
		var acquired int
		if err := probe.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lockName).Scan(&acquired); err != nil || acquired != 1 {
			t.Fatalf("lock was not released after migration failure: acquired=%d err=%v", acquired, err)
		}
		var released int
		if err := probe.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&released); err != nil || released != 1 {
			t.Fatalf("release failure-test lock: released=%d err=%v", released, err)
		}
	})

	t.Logf("mysql_version=%s fresh_existing_schema_sha256=%s legacy_data_sha256=%s", version, hashString(freshSchema), dataAfter)
}

func newTestRunner(t *testing.T, ctx context.Context, db *sql.DB, timeout time.Duration) *Runner {
	t.Helper()
	runner, err := NewRunner(ctx, db, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func assertVersion(t *testing.T, ctx context.Context, runner *Runner, expected int64) {
	t.Helper()
	version, err := runner.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version != expected {
		t.Fatalf("schema version=%d, want %d", version, expected)
	}
}

func assertDatabaseVersion(t *testing.T, ctx context.Context, db *sql.DB, expected int64) {
	t.Helper()
	var version int64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != expected {
		t.Fatalf("schema version=%d, want %d", version, expected)
	}
}

func openSQL(t *testing.T, dsn string) *sql.DB {
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

func databaseDSN(t *testing.T, adminDSN, databaseName string) string {
	t.Helper()
	cfg, err := drivermysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = databaseName
	cfg.ParseTime = true
	cfg.MultiStatements = true
	return cfg.FormatDSN()
}

func createDatabase(t *testing.T, ctx context.Context, admin *sql.DB, name string) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
}

func dropDatabase(t *testing.T, admin *sql.DB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
		t.Errorf("drop disposable database %s: %v", name, err)
	}
}

func insertLegacySentinels(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	statements := []string{
		"INSERT INTO users (username,password,role,token_version) VALUES ('phase1-user','hash','viewer',3)",
		"INSERT INTO host_groups (name,description) VALUES ('phase1-group','sentinel')",
		"INSERT INTO host_group_members (group_id,instance) VALUES (1,'phase1-instance')",
		"INSERT INTO alert_rules (name,expr,duration,severity,summary,description,enabled) VALUES ('phase1-rule','up == 0','2m','warning','sentinel','sentinel',1)",
		"INSERT INTO notification_channels (name,type,url,enabled) VALUES ('phase1-channel','webhook','https://example.invalid/hook',1)",
		"INSERT INTO alert_histories (fingerprint,alert_name,instance,severity,status,summary,labels_json,fired_at) VALUES ('phase1-fp','Down','phase1-instance','critical','firing','sentinel','{}','2026-07-18 00:00:00.000')",
		"INSERT INTO diagnosis_reports (summary,root_cause,evidence_json,runbooks_json,recommended_actions_json,rule_analysis_json) VALUES ('sentinel','sentinel','{}','[]','[]','{}')",
		"INSERT INTO diagnosis_feedback (diagnosis_id,rating,comment,created_by) VALUES (1,'useful','sentinel',7)",
		"INSERT INTO pending_actions (action_type,target_kind,target_name,namespace,params_json,dedupe_key,risk_level,result_json,error_message) VALUES ('noop','Deployment','demo','default','{}','phase1-action','low','{}','')",
		"INSERT INTO audit_logs (actor,actor_role,action,resource_type,resource_id,request_json,result,error_message,trace_id) VALUES ('phase1','viewer','read','incident','phase1','{}','success','','trace-phase1')",
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("insert sentinel: %v", err)
		}
	}
}

func dataSnapshot(t *testing.T, ctx context.Context, db *sql.DB, tables []string) string {
	t.Helper()
	hash := sha256.New()
	for _, table := range tables {
		rows, err := db.QueryContext(ctx, "SELECT * FROM `"+table+"` ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(hash, "table:%d:%s columns:%d\n", len(table), table, len(columns))
		for rows.Next() {
			values := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for index := range values {
				destinations[index] = &values[index]
			}
			if err := rows.Scan(destinations...); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			for index, value := range values {
				encoded := canonicalSQLValue(value)
				_, _ = fmt.Fprintf(hash, "%d:%s=%d:%s\n", len(columns[index]), columns[index], len(encoded), encoded)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func canonicalSQLValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case []byte:
		return "bytes:" + string(typed)
	case time.Time:
		return "time:" + typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%T:%v", value, value)
	}
}

func schemaSnapshot(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	queries := []string{
		`SELECT table_name, engine, table_collation FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version' ORDER BY table_name`,
		`SELECT table_name, ordinal_position, column_name, column_type, is_nullable, IF(column_default IS NULL, '<NULL>', CONCAT('<VALUE>', column_default)), extra, IFNULL(collation_name, '') FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version' ORDER BY table_name, ordinal_position`,
		`SELECT table_name, index_name, non_unique, seq_in_index, column_name, IFNULL(collation, ''), IFNULL(sub_part, 0), index_type FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version' ORDER BY table_name, index_name, seq_in_index`,
		`SELECT table_name, constraint_name, constraint_type FROM information_schema.table_constraints WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version' ORDER BY table_name, constraint_name`,
		`SELECT table_name, constraint_name, column_name, ordinal_position, IFNULL(referenced_table_name, ''), IFNULL(referenced_column_name, '') FROM information_schema.key_column_usage WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version' ORDER BY table_name, constraint_name, ordinal_position`,
		`SELECT constraint_name, table_name, referenced_table_name, update_rule, delete_rule FROM information_schema.referential_constraints WHERE constraint_schema = DATABASE() ORDER BY table_name, constraint_name`,
	}
	parts := make([]string, 0, len(queries))
	for _, query := range queries {
		parts = append(parts, querySnapshot(t, ctx, db, query))
	}
	return strings.Join(parts, "\n--\n")
}

func querySnapshot(t *testing.T, ctx context.Context, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		encoded := make([]string, len(values))
		for index, value := range values {
			encoded[index] = canonicalSQLValue(value)
		}
		lines = append(lines, strings.Join(encoded, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
