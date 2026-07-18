package bootstrap

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"

	apibootstrap "github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/api"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
)

func TestRuntimeStartsWithDMLOnlyMySQLAndDoesNotMutateSchema(t *testing.T) {
	dsn := os.Getenv("CLOUDOPS_TEST_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLOUDOPS_TEST_RUNTIME_MYSQL_DSN is not set; requires a migrated disposable MySQL database and DML-only user")
	}
	verifier, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = verifier.Close() }()
	before := runtimeTableCount(t, verifier)
	exerciseRuntime(t, runtimeApplication(t, dsn), http.StatusOK)
	after := runtimeTableCount(t, verifier)
	if before != after {
		t.Fatalf("runtime startup changed table count: before=%d after=%d", before, after)
	}
}

func TestRuntimeDoesNotAutoMigrateUnmigratedDatabase(t *testing.T) {
	dsn := os.Getenv("CLOUDOPS_TEST_UNMIGRATED_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLOUDOPS_TEST_UNMIGRATED_MYSQL_DSN is not set; requires an empty disposable MySQL database and DML-only user")
	}
	verifier, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = verifier.Close() }()
	before := runtimeTableCount(t, verifier)
	exerciseRuntime(t, runtimeApplication(t, dsn), http.StatusServiceUnavailable)
	after := runtimeTableCount(t, verifier)
	if before != after {
		t.Fatalf("unmigrated runtime startup changed table count: before=%d after=%d", before, after)
	}
}

func runtimeApplication(t *testing.T, dsn string) appconfig.Config {
	t.Helper()
	driverConfig, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	mysqlHost, mysqlPort, err := net.SplitHostPort(driverConfig.Addr)
	if err != nil {
		t.Fatalf("parse MySQL address %q: %v", driverConfig.Addr, err)
	}

	application := appconfig.Load()
	application.AuthEnabled = false
	application.MySQLHost = mysqlHost
	application.MySQLPort = mysqlPort
	application.MySQLUser = driverConfig.User
	application.MySQLPassword = driverConfig.Passwd
	application.MySQLDatabase = driverConfig.DBName
	application.ListenAddr = "127.0.0.1:18080"
	application.StaticDir = ""
	application.RedisAddr = ""
	application.KafkaBrokers = nil
	application.K8SEnabled = false
	application.IncidentAgentEnabled = false
	application.ChangeIntelligenceEnabled = false
	application.RemediationEnabled = false
	application.DeliveryTrackingEnabled = false
	application.VerificationEnabled = false
	application.FastDemoEnabled = false
	return application
}

func exerciseRuntime(t *testing.T, application appconfig.Config, expectedReadiness int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	api, err := apibootstrap.NewAPI(ctx, apibootstrap.APIConfig{Application: application})
	if err != nil {
		t.Fatalf("start API with DML-only user: %v", err)
	}
	apiListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiDone := make(chan error, 1)
	go func() { apiDone <- api.Serve(ctx, apiListener) }()
	waitForHTTPStatus(t, "http://"+apiListener.Addr().String()+"/readyz", expectedReadiness)
	cancel()
	if err := <-apiDone; err != nil {
		t.Fatal(err)
	}

	workerContext, stopWorker := context.WithCancel(context.Background())
	workerConfig := WorkerConfig{
		Application:       application,
		ManagementAddr:    "127.0.0.1:18081",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
	}
	worker, err := NewWorker(workerContext, workerConfig)
	if err != nil {
		t.Fatalf("start Worker with DML-only user: %v", err)
	}
	workerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	workerDone := make(chan error, 1)
	go func() { workerDone <- worker.Serve(workerContext, workerListener) }()
	waitForHTTPStatus(t, "http://"+workerListener.Addr().String()+"/readyz", expectedReadiness)
	stopWorker()
	if err := <-workerDone; err != nil {
		t.Fatal(err)
	}

}

func runtimeTableCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
